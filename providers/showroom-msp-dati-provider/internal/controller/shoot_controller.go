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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dativ1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/providers/dati-provider/api/v1alpha1"
)

const (
	datiShootFinalizer = "dati.gardener/provider"

	conditionReady = "Ready"

	providerRequeue = 5 * time.Minute
	inUseRequeue    = 30 * time.Second
)

// ShootReconciler manages the dati.gardener Shoot, an optional "extra service"
// marker: its spec is the Gardener Shoot manifest that overrides the SCPC
// resource template (the SCPC itself is created by the NamespaceReconciler on
// bind, not here). The reconciler adds a finalizer that blocks Shoot deletion
// while SimpleClusters still reference dati, and reports whether the dati SCPC
// is provisioned in the namespace.
type ShootReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=dati.gardener,resources=shoots,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dati.gardener,resources=shoots/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dati.gardener,resources=shoots/finalizers,verbs=update
// +kubebuilder:rbac:groups=showroom.apeirora.eu,resources=simpleclusterproviderconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=showroom.apeirora.eu,resources=simpleclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *ShootReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	sh := &dativ1alpha1.Shoot{}
	if err := r.Get(ctx, req.NamespacedName, sh); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !sh.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, sh)
	}

	if !controllerutil.ContainsFinalizer(sh, datiShootFinalizer) {
		controllerutil.AddFinalizer(sh, datiShootFinalizer)
		if err := r.Update(ctx, sh); err != nil {
			return ctrl.Result{}, err
		}
	}

	scpcPresent, err := r.datiSCPCExists(ctx, sh.Namespace)
	if err != nil {
		return ctrl.Result{}, err
	}
	log.V(1).Info("Shoot reconciled", "namespace", sh.Namespace, "scpcPresent", scpcPresent)

	return ctrl.Result{RequeueAfter: providerRequeue}, r.patchStatus(ctx, sh, scpcPresent)
}

func (r *ShootReconciler) reconcileDelete(ctx context.Context, sh *dativ1alpha1.Shoot) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(sh, datiShootFinalizer) {
		return ctrl.Result{}, nil
	}

	users, err := simpleClustersUsingSCPC(ctx, r.Client, sh.Namespace)
	if err != nil {
		return ctrl.Result{}, err
	}
	if len(users) > 0 {
		apimeta.SetStatusCondition(&sh.Status.Conditions, metav1.Condition{
			Type:               conditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "SCPCInUse",
			Message:            fmt.Sprintf("deletion blocked: SimpleClusters still reference %q: %v", datiSCPCName, users),
			ObservedGeneration: sh.Generation,
		})
		if err := r.Status().Update(ctx, sh); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: inUseRequeue}, nil
	}

	controllerutil.RemoveFinalizer(sh, datiShootFinalizer)
	return ctrl.Result{}, r.Update(ctx, sh)
}

// datiSCPCExists reports whether the NamespaceReconciler has materialised the
// dati SCPC in the namespace. A missing CRD counts as "not present".
func (r *ShootReconciler) datiSCPCExists(ctx context.Context, namespace string) (bool, error) {
	scpc := &unstructured.Unstructured{}
	scpc.SetGroupVersionKind(scpcGVK)
	err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: datiSCPCName}, scpc)
	if apierrors.IsNotFound(err) || apimeta.IsNoMatchError(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *ShootReconciler) patchStatus(ctx context.Context, sh *dativ1alpha1.Shoot, scpcPresent bool) error {
	sh.Status.ObservedGeneration = sh.Generation

	ready := metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "SCPCReady",
		Message:            fmt.Sprintf("SimpleClusterProviderConfig %q available in namespace %s", datiSCPCName, sh.Namespace),
		ObservedGeneration: sh.Generation,
	}
	sh.Status.ProviderConfigs = []string{datiSCPCName}
	if !scpcPresent {
		ready.Status = metav1.ConditionFalse
		ready.Reason = "SCPCNotProvisioned"
		ready.Message = fmt.Sprintf(
			"SimpleClusterProviderConfig %q not present; is SimpleCluster bound in namespace %s?",
			datiSCPCName, sh.Namespace)
		sh.Status.ProviderConfigs = nil
	}
	apimeta.SetStatusCondition(&sh.Status.Conditions, ready)

	return r.Status().Update(ctx, sh)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ShootReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dativ1alpha1.Shoot{}).
		Named("shoot").
		Complete(r)
}
