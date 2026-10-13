/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/secretpolicy"
)

const (
	scpcRequeue         = 5 * time.Minute
	kubeconfigSecretKey = "kubeconfig"
)

// SimpleClusterProviderConfigReconciler validates the SCPC DAG, resolves
// every ResourceSpec's GVK against the target cluster, and reports
// reachability. It does not create the downstream resources R — that lives
// on the SimpleCluster reconciler.
type SimpleClusterProviderConfigReconciler struct {
	client.Client
	SecretPolicy secretpolicy.Policy
	Scheme       *runtime.Scheme
}

// +kubebuilder:rbac:groups=showroom.apeirora.eu,resources=simpleclusterproviderconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=showroom.apeirora.eu,resources=simpleclusterproviderconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=showroom.apeirora.eu,resources=simpleclusterproviderconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *SimpleClusterProviderConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var scpc showroomv1alpha1.SimpleClusterProviderConfig
	if err := r.Get(ctx, req.NamespacedName, &scpc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	resolved, reachable, cond, retryErr := r.probe(ctx, &scpc)

	patch := client.MergeFrom(scpc.DeepCopy())
	scpc.Status.Resources = resolved
	scpc.Status.Reachable = &reachable
	apimeta.SetStatusCondition(&scpc.Status.Conditions, cond)

	if err := r.Status().Patch(ctx, &scpc, patch); err != nil {
		log.Error(err, "patch SCPC status failed")
		return ctrl.Result{}, err
	}

	if retryErr != nil {
		return ctrl.Result{}, retryErr
	}
	return ctrl.Result{RequeueAfter: scpcRequeue}, nil
}

// probe validates the SCPC DAG, then resolves each ResourceSpec's GVK
// against the target cluster. Returns the resolved entries, the
// reachability flag, the Ready condition, and an error that should
// trigger an immediate retry (transient only — terminal errors live in
// the condition and return nil).
func (r *SimpleClusterProviderConfigReconciler) probe(
	ctx context.Context, scpc *showroomv1alpha1.SimpleClusterProviderConfig,
) ([]showroomv1alpha1.ResolvedResource, bool, metav1.Condition, error) {
	if _, err := validateAndTopoSort(scpc); err != nil {
		return nil, false, readyCond(metav1.ConditionFalse, "InvalidSCPC", err.Error()), nil
	}

	mapper, condErr, retryErr := r.targetMapper(ctx, scpc)
	if condErr != nil {
		return nil, false, *condErr, retryErr
	}

	resolved := make([]showroomv1alpha1.ResolvedResource, 0, len(scpc.Spec.Resources))
	for _, rs := range scpc.Spec.Resources {
		gv, err := schema.ParseGroupVersion(rs.APIVersion)
		if err != nil {
			return nil, false, readyCond(metav1.ConditionFalse, "InvalidTarget",
				fmt.Sprintf("resource %q: %v", rs.Name, err)), nil
		}
		gvk := gv.WithKind(rs.Kind)
		if _, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version); err != nil {
			return nil, false, readyCond(metav1.ConditionFalse, "GVKNotFound",
				fmt.Sprintf("resource %q: RESTMapping %s: %v", rs.Name, gvk, err)), nil
		}
		resolved = append(resolved, showroomv1alpha1.ResolvedResource{
			Name: rs.Name,
			GVK:  gvk.String(),
		})
	}

	location := "management cluster"
	if scpc.Spec.Target.KubeconfigSecretRef != nil {
		location = "remote cluster"
	}
	return resolved, true,
		readyCond(metav1.ConditionTrue, "Reachable",
			fmt.Sprintf("%d resource(s) resolved on %s", len(resolved), location)),
		nil
}

// targetMapper returns a RESTMapper for the SCPC's target cluster. Local
// (no kubeconfigSecretRef) reuses the manager's own RESTMapper. The optional
// *metav1.Condition signals a terminal status outcome to the caller; the
// error is for transient retry.
func (r *SimpleClusterProviderConfigReconciler) targetMapper(
	ctx context.Context, scpc *showroomv1alpha1.SimpleClusterProviderConfig,
) (apimeta.RESTMapper, *metav1.Condition, error) {
	if scpc.Spec.Target.KubeconfigSecretRef == nil {
		return r.RESTMapper(), nil, nil
	}

	ref := scpc.Spec.Target.KubeconfigSecretRef
	var secret corev1.Secret
	switch err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, &secret); {
	case apierrors.IsNotFound(err):
		c := readyCond(metav1.ConditionFalse, "SecretMissing",
			fmt.Sprintf("kubeconfig Secret %s/%s not found", ref.Namespace, ref.Name))
		return nil, &c, nil
	case err != nil:
		c := readyCond(metav1.ConditionFalse, "SecretReadError", err.Error())
		return nil, &c, err
	}

	data, ok := secret.Data[kubeconfigSecretKey]
	if !ok || len(data) == 0 {
		c := readyCond(metav1.ConditionFalse, "SecretKeyMissing",
			fmt.Sprintf("Secret %s/%s has no %q key", ref.Namespace, ref.Name, kubeconfigSecretKey))
		return nil, &c, nil
	}

	cfg, err := clientcmd.RESTConfigFromKubeConfig(data)
	if err != nil {
		c := readyCond(metav1.ConditionFalse, "BadKubeconfig", err.Error())
		return nil, &c, nil
	}

	mapper, err := newDiscoveryMapper(cfg)
	if err != nil {
		c := readyCond(metav1.ConditionFalse, "Unreachable", err.Error())
		return nil, &c, nil
	}
	return mapper, nil, nil
}

func newDiscoveryMapper(cfg *rest.Config) (apimeta.RESTMapper, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("discovery client: %w", err)
	}
	groups, err := restmapper.GetAPIGroupResources(dc)
	if err != nil {
		return nil, fmt.Errorf("discover API groups: %w", err)
	}
	return restmapper.NewDiscoveryRESTMapper(groups), nil
}

func readyCond(status metav1.ConditionStatus, reason, message string) metav1.Condition {
	return metav1.Condition{
		Type:    showroomv1alpha1.ConditionReady,
		Status:  status,
		Reason:  reason,
		Message: message,
	}
}

func (r *SimpleClusterProviderConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&showroomv1alpha1.SimpleClusterProviderConfig{}).
		Named("simpleclusterproviderconfig").
		Complete(r)
}
