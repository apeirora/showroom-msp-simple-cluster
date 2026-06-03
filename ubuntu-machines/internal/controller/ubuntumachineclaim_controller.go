/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	umv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/ubuntu-machines/api/v1alpha1"
)

// ConditionReady is the type SCPC consumers read by default
// (status.conditions[?(@.type=="Ready")].status). True once the claim
// is Bound; False with a reason while still Pending.
const ConditionReady = "Ready"

// UbuntuMachineClaimReconciler binds an UbuntuMachineClaim to one
// eligible Available UbuntuMachine in the same namespace. PVC parity:
// one claim → one machine.
type UbuntuMachineClaimReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=ubuntumachines.apeirora.eu,resources=ubuntumachineclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ubuntumachines.apeirora.eu,resources=ubuntumachineclaims/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ubuntumachines.apeirora.eu,resources=ubuntumachineclaims/finalizers,verbs=update
// +kubebuilder:rbac:groups=ubuntumachines.apeirora.eu,resources=ubuntumachines,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=ubuntumachines.apeirora.eu,resources=ubuntumachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

func (r *UbuntuMachineClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var claim umv1alpha1.UbuntuMachineClaim
	if err := r.Get(ctx, req.NamespacedName, &claim); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !claim.DeletionTimestamp.IsZero() {
		if err := r.releaseAll(ctx, &claim); err != nil {
			return ctrl.Result{}, err
		}
		controllerutil.RemoveFinalizer(&claim, umv1alpha1.FinalizerName)
		if err := r.Update(ctx, &claim); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		return ctrl.Result{}, nil
	}

	if controllerutil.AddFinalizer(&claim, umv1alpha1.FinalizerName) {
		if err := r.Update(ctx, &claim); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Self-heal step: if any machine already carries
	// boundClaimRef -> this claim, that's the authoritative bind.
	// Adopt the first one (by name) and release any extras left over
	// from a prior partial reconcile so the claim and machines agree.
	owned, err := r.ownedMachines(ctx, &claim)
	if err != nil {
		return ctrl.Result{}, err
	}
	if len(owned) > 0 {
		sort.Slice(owned, func(i, j int) bool { return owned[i].Name < owned[j].Name })
		if len(owned) > 1 {
			logger.Info("orphan machines detected, releasing extras",
				"keep", owned[0].Name, "release", len(owned)-1)
			for i := 1; i < len(owned); i++ {
				if err := r.releaseOne(ctx, &claim, owned[i]); err != nil {
					return ctrl.Result{}, err
				}
			}
		}
		return r.markBound(ctx, &claim, owned[0])
	}

	if claim.Status.Phase == umv1alpha1.ClaimReady && claim.Status.Machine != nil {
		// Already-Bound claim with no machine pointing back is stale
		// (e.g. machine was deleted). Drop to Pending and re-bind.
		logger.Info("bound claim has no owning machine; re-binding",
			"previous", claim.Status.Machine.Name)
	}

	candidates, err := r.eligibleMachines(ctx, &claim)
	if err != nil {
		return ctrl.Result{}, err
	}

	if len(candidates) == 0 {
		logger.Info("no eligible machines")
		r.event(&claim, corev1.EventTypeNormal, "Pending",
			"waiting for an eligible UbuntuMachine to become Available")
		return r.markPending(ctx, &claim, "NoEligibleMachine",
			"waiting for an eligible UbuntuMachine to become Available")
	}

	picked := candidates[0]
	bound, bindErr := r.bind(ctx, &claim, picked)
	if bindErr != nil {
		logger.Info("bind aborted, will retry", "reason", bindErr.Error())
		if _, err := r.markPending(ctx, &claim, "BindConflict", bindErr.Error()); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// markBound writes the claim status. If the write fails (typically
	// a resourceVersion conflict on the claim), release the machine
	// we just bound — otherwise it would be orphaned. Next reconcile
	// will retry from scratch.
	if res, err := r.markBound(ctx, &claim, bound); err != nil {
		if relErr := r.releaseOne(ctx, &claim, bound); relErr != nil {
			logger.Error(relErr, "rollback after claim status update failure failed",
				"machine", bound.Name)
		}
		return ctrl.Result{}, err
	} else {
		return res, nil
	}
}

// markBound writes status.phase=Bound + status.machine + Ready=True.
// Used both on a fresh bind and when adopting an orphan.
func (r *UbuntuMachineClaimReconciler) markBound(
	ctx context.Context,
	claim *umv1alpha1.UbuntuMachineClaim,
	m umv1alpha1.UbuntuMachine,
) (ctrl.Result, error) {
	if err := r.ensureCredentialSecret(ctx, claim, m); err != nil {
		return ctrl.Result{}, err
	}
	claim.Status.Phase = umv1alpha1.ClaimReady
	claim.Status.Machine = &umv1alpha1.BoundMachine{
		Name:            m.Name,
		Address:         m.Spec.Address,
		ExternalAddress: m.Spec.ExternalAddress,
		SSH:             m.Spec.SSH,
	}
	claim.Status.ObservedGeneration = claim.Generation
	setReady(claim, metav1.ConditionTrue, "Bound",
		fmt.Sprintf("bound to UbuntuMachine %q", m.Name))
	if err := r.Status().Update(ctx, claim); err != nil {
		return ctrl.Result{}, err
	}
	r.event(claim, corev1.EventTypeNormal, "Bound",
		fmt.Sprintf("bound to UbuntuMachine %q", m.Name))
	return ctrl.Result{}, nil
}

// ensureCredentialSecret mirrors the bound machine's SSH credential Secret
// into the claim's namespace under the claim's name, owned by the claim so
// Kubernetes garbage-collects it when the claim is deleted. Consumers then
// read the credential from a Secret local to their namespace, named after the
// claim. Idempotent: re-run on every reconcile so a rotated source propagates.
func (r *UbuntuMachineClaimReconciler) ensureCredentialSecret(
	ctx context.Context,
	claim *umv1alpha1.UbuntuMachineClaim,
	m umv1alpha1.UbuntuMachine,
) error {
	srcRef := m.Spec.SSH.PrivateKeySecretRef
	if srcRef == nil {
		srcRef = m.Spec.SSH.PasswordSecretRef
	}
	if srcRef == nil {
		return fmt.Errorf("machine %q has no SSH credential secret reference", m.Name)
	}

	var src corev1.Secret
	srcKey := types.NamespacedName{Namespace: srcRef.Namespace, Name: srcRef.Name}
	if err := r.Get(ctx, srcKey, &src); err != nil {
		return fmt.Errorf("get source credential Secret %s/%s: %w", srcRef.Namespace, srcRef.Name, err)
	}

	copySecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: claim.Name, Namespace: claim.Namespace},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, copySecret, func() error {
		if err := controllerutil.SetControllerReference(claim, copySecret, r.Scheme); err != nil {
			return err
		}
		copySecret.Type = src.Type
		copySecret.Data = src.Data
		return nil
	})
	return err
}

// ownedBy reports whether machine m is currently bound to claim. Matches
// on name + namespace because UbuntuMachine is cluster-scoped: two claims
// in different namespaces may share a name.
func ownedBy(m *umv1alpha1.UbuntuMachine, claim *umv1alpha1.UbuntuMachineClaim) bool {
	ref := m.Status.BoundClaimRef
	return ref != nil && ref.Name == claim.Name && ref.Namespace == claim.Namespace
}

// ownedMachines lists UbuntuMachines that already have boundClaimRef
// pointing at this claim. Machines are cluster-scoped, so the list spans
// all namespaces. Used at the start of every reconcile so partial-write
// leaks self-heal.
func (r *UbuntuMachineClaimReconciler) ownedMachines(
	ctx context.Context,
	claim *umv1alpha1.UbuntuMachineClaim,
) ([]umv1alpha1.UbuntuMachine, error) {
	var all umv1alpha1.UbuntuMachineList
	if err := r.List(ctx, &all); err != nil {
		return nil, err
	}
	out := make([]umv1alpha1.UbuntuMachine, 0)
	for i := range all.Items {
		if ownedBy(&all.Items[i], claim) {
			out = append(out, all.Items[i])
		}
	}
	return out, nil
}

// releaseOne flips one machine back to Available. Re-Gets so the
// status write carries a fresh resourceVersion.
func (r *UbuntuMachineClaimReconciler) releaseOne(
	ctx context.Context,
	claim *umv1alpha1.UbuntuMachineClaim,
	m umv1alpha1.UbuntuMachine,
) error {
	var fresh umv1alpha1.UbuntuMachine
	key := types.NamespacedName{Namespace: m.Namespace, Name: m.Name}
	if err := r.Get(ctx, key, &fresh); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if !ownedBy(&fresh, claim) {
		return nil
	}
	fresh.Status.Phase = umv1alpha1.MachineAvailable
	fresh.Status.BoundClaimRef = nil
	return r.Status().Update(ctx, &fresh)
}

// setReady writes (or refreshes) the `Ready` Condition on the claim.
// observedGeneration carries the spec generation that produced this
// signal so SCPC can ignore stale states.
func setReady(claim *umv1alpha1.UbuntuMachineClaim, status metav1.ConditionStatus, reason, message string) {
	apimeta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
		Type:               ConditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: claim.Generation,
	})
}

// eligibleMachines lists Available UbuntuMachines (cluster-wide, since the
// pool is cluster-scoped) that match the claim's label selector and satisfy
// the externalAddressRequired flag and required.cpu/memory floor. Returned
// slice is sorted by name (stable first-fit ordering).
func (r *UbuntuMachineClaimReconciler) eligibleMachines(
	ctx context.Context,
	claim *umv1alpha1.UbuntuMachineClaim,
) ([]umv1alpha1.UbuntuMachine, error) {
	var all umv1alpha1.UbuntuMachineList
	// Selector is applied server-side; an empty selector matches the whole
	// pool. Only eligibility is constrained — ownedMachines/releaseAll find
	// already-bound machines by boundClaimRef regardless of their labels.
	if err := r.List(ctx, &all, client.MatchingLabels(claim.Spec.Selector)); err != nil {
		return nil, err
	}
	out := make([]umv1alpha1.UbuntuMachine, 0, len(all.Items))
	for _, m := range all.Items {
		if !m.DeletionTimestamp.IsZero() {
			continue
		}
		if m.Status.Phase == umv1alpha1.MachineBound {
			continue
		}
		if m.Status.BoundClaimRef != nil {
			continue
		}
		if claim.Spec.ExternalAddressRequired && m.Spec.ExternalAddress == "" {
			continue
		}
		if !fits(m.Spec.Capacity.CPU, claim.Spec.Required.CPU) {
			continue
		}
		if !fits(m.Spec.Capacity.Memory, claim.Spec.Required.Memory) {
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// fits returns true when the machine can satisfy required. Zero capacity
// is treated as "unconstrained" — matches any request.
func fits(capacity, required resource.Quantity) bool {
	if capacity.IsZero() {
		return true
	}
	return capacity.Cmp(required) >= 0
}

// bind claims the given machine for the claim. A resourceVersion
// conflict (someone else won the race) bubbles up — the caller marks
// the claim Pending and retries on the next reconcile.
func (r *UbuntuMachineClaimReconciler) bind(
	ctx context.Context,
	claim *umv1alpha1.UbuntuMachineClaim,
	m umv1alpha1.UbuntuMachine,
) (umv1alpha1.UbuntuMachine, error) {
	m.Status.Phase = umv1alpha1.MachineBound
	m.Status.BoundClaimRef = &umv1alpha1.ClaimReference{Name: claim.Name, Namespace: claim.Namespace}
	m.Status.ObservedGeneration = m.Generation
	if err := r.Status().Update(ctx, &m); err != nil {
		return m, fmt.Errorf("bind %s: %w", m.Name, err)
	}
	return m, nil
}

// releaseAll is the cascade for claim deletion: any machine whose
// boundClaimRef points at this claim returns to Available. We re-Get
// each candidate so the status write carries a current resourceVersion.
func (r *UbuntuMachineClaimReconciler) releaseAll(
	ctx context.Context,
	claim *umv1alpha1.UbuntuMachineClaim,
) error {
	var all umv1alpha1.UbuntuMachineList
	if err := r.List(ctx, &all); err != nil {
		return err
	}
	for i := range all.Items {
		m := all.Items[i]
		if !ownedBy(&m, claim) {
			continue
		}
		var fresh umv1alpha1.UbuntuMachine
		key := types.NamespacedName{Namespace: m.Namespace, Name: m.Name}
		if err := r.Get(ctx, key, &fresh); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		if !ownedBy(&fresh, claim) {
			continue
		}
		fresh.Status.Phase = umv1alpha1.MachineAvailable
		fresh.Status.BoundClaimRef = nil
		if err := r.Status().Update(ctx, &fresh); err != nil {
			return err
		}
	}
	return nil
}

func (r *UbuntuMachineClaimReconciler) markPending(
	ctx context.Context,
	claim *umv1alpha1.UbuntuMachineClaim,
	reason, message string,
) (ctrl.Result, error) {
	existing := apimeta.FindStatusCondition(claim.Status.Conditions, ConditionReady)
	alreadyPending := claim.Status.Phase == umv1alpha1.ClaimPending &&
		claim.Status.ObservedGeneration == claim.Generation &&
		existing != nil &&
		existing.Status == metav1.ConditionFalse &&
		existing.Reason == reason
	if alreadyPending {
		return ctrl.Result{}, nil
	}
	claim.Status.Phase = umv1alpha1.ClaimPending
	claim.Status.Machine = nil
	claim.Status.ObservedGeneration = claim.Generation
	setReady(claim, metav1.ConditionFalse, reason, message)
	if err := r.Status().Update(ctx, claim); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *UbuntuMachineClaimReconciler) event(o client.Object, evType, reason, msg string) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Event(o, evType, reason, msg)
}

// machineToPendingClaims wakes every Pending claim cluster-wide when an
// UbuntuMachine changes. The pool is cluster-scoped, so a newly Available
// machine could unblock a claim in any namespace. Cheap (small N) and
// correct.
func (r *UbuntuMachineClaimReconciler) machineToPendingClaims(
	ctx context.Context,
	_ client.Object,
) []reconcile.Request {
	var claims umv1alpha1.UbuntuMachineClaimList
	if err := r.List(ctx, &claims); err != nil {
		return nil
	}
	out := make([]reconcile.Request, 0)
	for _, c := range claims.Items {
		if c.Status.Phase == umv1alpha1.ClaimReady && c.Status.Machine != nil {
			continue
		}
		out = append(out, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: c.Namespace,
				Name:      c.Name,
			},
		})
	}
	return out
}

func (r *UbuntuMachineClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&umv1alpha1.UbuntuMachineClaim{}).
		Watches(
			&umv1alpha1.UbuntuMachine{},
			handler.EnqueueRequestsFromMapFunc(r.machineToPendingClaims),
		).
		Named("ubuntumachineclaim").
		Complete(r)
}
