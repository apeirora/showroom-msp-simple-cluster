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

	sapv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/providers/sap-provider/api/v1alpha1"
)

const (
	shootFinalizer = "sap.gardener/provider"

	conditionReady = "Ready"

	providerRequeue = 5 * time.Minute
	inUseRequeue    = 30 * time.Second
)

// FlavorShoot is the common surface both SAP Gardener provider CRs
// (OpenStackShoot, IronCoreShoot) present to the generic reconciler.
type FlavorShoot interface {
	client.Object
	GetSpecText() string
	GetShootStatus() *sapv1alpha1.ShootStatus
}

// ShootReconciler manages one flavor's provider CR, an optional "extra service"
// marker: its spec is the Gardener Shoot manifest that overrides the SCPC
// resource template (the SCPC itself is created by the NamespaceReconciler on
// bind, not here). The reconciler adds a finalizer that blocks CR deletion
// while SimpleClusters still reference the flavor's SCPC, and reports whether
// that SCPC is provisioned in the namespace.
type ShootReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// SCPCName is the SimpleClusterProviderConfig this flavor materialises.
	SCPCName string
	// New returns a fresh empty CR of the flavor's Kind.
	New func() FlavorShoot
	// ControllerName names the controller (and its For() resource).
	ControllerName string
}

// +kubebuilder:rbac:groups=sap.gardener,resources=openstackshoots;ironcoreshoots,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sap.gardener,resources=openstackshoots/status;ironcoreshoots/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sap.gardener,resources=openstackshoots/finalizers;ironcoreshoots/finalizers,verbs=update
// +kubebuilder:rbac:groups=showroom.apeirora.eu,resources=simpleclusterproviderconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=showroom.apeirora.eu,resources=simpleclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *ShootReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	sh := r.New()
	if err := r.Get(ctx, req.NamespacedName, sh); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !sh.GetDeletionTimestamp().IsZero() {
		return r.reconcileDelete(ctx, sh)
	}

	if !controllerutil.ContainsFinalizer(sh, shootFinalizer) {
		controllerutil.AddFinalizer(sh, shootFinalizer)
		if err := r.Update(ctx, sh); err != nil {
			return ctrl.Result{}, err
		}
	}

	scpcPresent, err := r.scpcExists(ctx, sh.GetNamespace())
	if err != nil {
		return ctrl.Result{}, err
	}
	log.V(1).Info("Shoot reconciled", "namespace", sh.GetNamespace(), "scpc", r.SCPCName, "scpcPresent", scpcPresent)

	return ctrl.Result{RequeueAfter: providerRequeue}, r.patchStatus(ctx, sh, scpcPresent)
}

func (r *ShootReconciler) reconcileDelete(ctx context.Context, sh FlavorShoot) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(sh, shootFinalizer) {
		return ctrl.Result{}, nil
	}

	users, err := simpleClustersUsingSCPC(ctx, r.Client, sh.GetNamespace(), r.SCPCName)
	if err != nil {
		return ctrl.Result{}, err
	}
	if len(users) > 0 {
		st := sh.GetShootStatus()
		apimeta.SetStatusCondition(&st.Conditions, metav1.Condition{
			Type:               conditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "SCPCInUse",
			Message:            fmt.Sprintf("deletion blocked: SimpleClusters still reference %q: %v", r.SCPCName, users),
			ObservedGeneration: sh.GetGeneration(),
		})
		if err := r.Status().Update(ctx, sh); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: inUseRequeue}, nil
	}

	controllerutil.RemoveFinalizer(sh, shootFinalizer)
	return ctrl.Result{}, r.Update(ctx, sh)
}

// scpcExists reports whether the NamespaceReconciler has materialised this
// flavor's SCPC in the namespace. A missing CRD counts as "not present".
func (r *ShootReconciler) scpcExists(ctx context.Context, namespace string) (bool, error) {
	scpc := &unstructured.Unstructured{}
	scpc.SetGroupVersionKind(scpcGVK)
	err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: r.SCPCName}, scpc)
	if apierrors.IsNotFound(err) || apimeta.IsNoMatchError(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *ShootReconciler) patchStatus(ctx context.Context, sh FlavorShoot, scpcPresent bool) error {
	st := sh.GetShootStatus()
	st.ObservedGeneration = sh.GetGeneration()

	ready := metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "SCPCReady",
		Message:            fmt.Sprintf("SimpleClusterProviderConfig %q available in namespace %s", r.SCPCName, sh.GetNamespace()),
		ObservedGeneration: sh.GetGeneration(),
	}
	st.ProviderConfigs = []string{r.SCPCName}
	if !scpcPresent {
		ready.Status = metav1.ConditionFalse
		ready.Reason = "SCPCNotProvisioned"
		ready.Message = fmt.Sprintf(
			"SimpleClusterProviderConfig %q not present; is SimpleCluster bound in namespace %s?",
			r.SCPCName, sh.GetNamespace())
		st.ProviderConfigs = nil
	}
	apimeta.SetStatusCondition(&st.Conditions, ready)

	return r.Status().Update(ctx, sh)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ShootReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(r.New()).
		Named(r.ControllerName).
		Complete(r)
}
