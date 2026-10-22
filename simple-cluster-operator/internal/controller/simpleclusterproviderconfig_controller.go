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

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/secretpolicy"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/target"
)

const (
	scpcRequeue = 5 * time.Minute
)

// SimpleClusterProviderConfigReconciler validates the SCPC DAG, resolves
// every ResourceSpec's GVK against the target cluster, and reports
// reachability. It does not create the downstream resources R — that lives
// on the SimpleCluster reconciler.
type SimpleClusterProviderConfigReconciler struct {
	AccountProvider *target.AccountProvider

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

	resolved, reachable, cond := r.probe(ctx, &scpc)

	patch := client.MergeFrom(scpc.DeepCopy())
	scpc.Status.Resources = resolved
	scpc.Status.Reachable = &reachable
	apimeta.SetStatusCondition(&scpc.Status.Conditions, cond)

	if err := r.Status().Patch(ctx, &scpc, patch); err != nil {
		log.Error(err, "patch SCPC status failed")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: scpcRequeue}, nil
}

// probe validates the graph and discovers every resource kind in the account.
// Failures remain visible in the condition and retry on the regular interval.
func (r *SimpleClusterProviderConfigReconciler) probe(
	ctx context.Context, scpc *showroomv1alpha1.SimpleClusterProviderConfig,
) ([]showroomv1alpha1.ResolvedResource, bool, metav1.Condition) {
	if _, err := validateAndTopoSort(scpc); err != nil {
		return nil, false, readyCond(metav1.ConditionFalse, "InvalidSCPC", err.Error())
	}

	mapper, condErr := r.targetMapper(ctx, scpc)
	if condErr != nil {
		return nil, false, *condErr
	}

	resolved := make([]showroomv1alpha1.ResolvedResource, 0, len(scpc.Spec.Resources))
	for _, rs := range scpc.Spec.Resources {
		gv, err := schema.ParseGroupVersion(rs.APIVersion)
		if err != nil {
			return nil, false, readyCond(metav1.ConditionFalse, "InvalidTarget",
				fmt.Sprintf("resource %q: %v", rs.Name, err))
		}
		gvk := gv.WithKind(rs.Kind)
		if _, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version); err != nil {
			return nil, false, readyCond(metav1.ConditionFalse, "GVKNotFound",
				fmt.Sprintf("resource %q: RESTMapping %s: %v", rs.Name, gvk, err))
		}
		resolved = append(resolved, showroomv1alpha1.ResolvedResource{
			Name: rs.Name,
			GVK:  gvk.String(),
		})
	}

	return resolved, true, readyCond(metav1.ConditionTrue, "Reachable", fmt.Sprintf("%d resource(s) resolved in account %s", len(resolved), scpc.Namespace))
}

// targetMapper discovers APIs only through the consumer account virtual workspace.
func (r *SimpleClusterProviderConfigReconciler) targetMapper(
	ctx context.Context, scpc *showroomv1alpha1.SimpleClusterProviderConfig,
) (apimeta.RESTMapper, *metav1.Condition) {
	mapper, err := accountMapper(ctx, r.AccountProvider, scpc)
	if err != nil {
		c := readyCond(metav1.ConditionFalse, "AccountTargetUnavailable", err.Error())
		return nil, &c
	}
	return mapper, nil
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
