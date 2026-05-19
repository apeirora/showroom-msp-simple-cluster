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
)

const (
	scpcRequeue         = 5 * time.Minute
	kubeconfigSecretKey = "kubeconfig"
)

// SimpleClusterProviderConfigReconciler resolves the target GVK declared by an
// SCPC and probes connectivity to its target cluster. It does not create the
// downstream resource R — that lives on the SimpleCluster reconciler.
type SimpleClusterProviderConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
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

	target, reachable, cond, retryErr := r.probe(ctx, &scpc)

	patch := client.MergeFrom(scpc.DeepCopy())
	scpc.Status.Target = target
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

// probe computes the next status for the SCPC. It returns the friendly
// target summary (empty when unresolved), the reachable flag, the Ready
// condition, and an error that should trigger an immediate retry
// (transient-only — terminal errors live in the condition and return nil).
func (r *SimpleClusterProviderConfigReconciler) probe(
	ctx context.Context, scpc *showroomv1alpha1.SimpleClusterProviderConfig,
) (string, bool, metav1.Condition, error) {
	gvk, err := parseGVK(scpc.Spec.Target.APIVersion, scpc.Spec.Target.Kind)
	if err != nil {
		return "", false, readyCond(metav1.ConditionFalse, "InvalidTarget", err.Error()), nil
	}

	mapper, retryErr, condErr := r.targetMapper(ctx, scpc)
	if condErr != nil {
		return "", false, *condErr, retryErr
	}

	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return "", false, readyCond(metav1.ConditionFalse, "GVKNotFound",
			fmt.Sprintf("RESTMapping %s: %v", gvk, err)), nil
	}

	location := "management cluster"
	if scpc.Spec.Target.KubeconfigSecretRef != nil {
		location = "remote cluster"
	}
	return targetSummary(scpc.Spec.Target, mapping), true,
		readyCond(metav1.ConditionTrue, "Reachable", "Target GVK resolved on "+location),
		nil
}

// targetSummary renders "<namespace>/<kind>" for namespaced targets, just
// "<kind>" when the target is cluster-scoped per RESTMapper.
func targetSummary(t showroomv1alpha1.TargetSpec, mapping *apimeta.RESTMapping) string {
	if mapping.Scope.Name() == apimeta.RESTScopeNameNamespace && t.Namespace != "" {
		return t.Namespace + "/" + t.Kind
	}
	return t.Kind
}

// targetMapper returns a RESTMapper for the SCPC's target cluster. Local
// (no kubeconfigSecretRef) reuses the manager's own RESTMapper.
func (r *SimpleClusterProviderConfigReconciler) targetMapper(
	ctx context.Context, scpc *showroomv1alpha1.SimpleClusterProviderConfig,
) (apimeta.RESTMapper, error, *metav1.Condition) {
	if scpc.Spec.Target.KubeconfigSecretRef == nil {
		return r.RESTMapper(), nil, nil
	}

	ref := scpc.Spec.Target.KubeconfigSecretRef
	var secret corev1.Secret
	switch err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, &secret); {
	case apierrors.IsNotFound(err):
		c := readyCond(metav1.ConditionFalse, "SecretMissing",
			fmt.Sprintf("kubeconfig Secret %s/%s not found", ref.Namespace, ref.Name))
		return nil, nil, &c
	case err != nil:
		c := readyCond(metav1.ConditionFalse, "SecretReadError", err.Error())
		return nil, err, &c
	}

	data, ok := secret.Data[kubeconfigSecretKey]
	if !ok || len(data) == 0 {
		c := readyCond(metav1.ConditionFalse, "SecretKeyMissing",
			fmt.Sprintf("Secret %s/%s has no %q key", ref.Namespace, ref.Name, kubeconfigSecretKey))
		return nil, nil, &c
	}

	cfg, err := clientcmd.RESTConfigFromKubeConfig(data)
	if err != nil {
		c := readyCond(metav1.ConditionFalse, "BadKubeconfig", err.Error())
		return nil, nil, &c
	}

	mapper, err := newDiscoveryMapper(cfg)
	if err != nil {
		c := readyCond(metav1.ConditionFalse, "Unreachable", err.Error())
		return nil, nil, &c
	}
	return mapper, nil, nil
}

func parseGVK(apiVersion, kind string) (schema.GroupVersionKind, error) {
	if apiVersion == "" || kind == "" {
		return schema.GroupVersionKind{}, fmt.Errorf("spec.target.apiVersion and kind are required")
	}
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return schema.GroupVersionKind{}, err
	}
	return gv.WithKind(kind), nil
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
