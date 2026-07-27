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
	"strings"
	"time"

	gossh "golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	umv1alpha1 "github.com/apeirora/showroom-simple-cluster/ubuntu-machines/api/v1alpha1"
)

// ConditionReady is the type SCPC consumers read by default
// (status.conditions[?(@.type=="Ready")].status). True once the claim
// is Bound; False with a reason while still Pending.
const ConditionReady = "Ready"

var sshKeyGVK = schema.GroupVersionKind{
	Group:   "sshkeys.apeirora.eu",
	Version: "v1alpha1",
	Kind:    "SSHKey",
}

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
// +kubebuilder:rbac:groups=sshkeys.apeirora.eu,resources=sshkeys,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

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

// markBound ensures a per-claim SSH key is created, injected into the machine's
// root account, and then updates claim status to Ready.
func (r *UbuntuMachineClaimReconciler) markBound(
	ctx context.Context,
	claim *umv1alpha1.UbuntuMachineClaim,
	m umv1alpha1.UbuntuMachine,
) (ctrl.Result, error) {
	sshKey, err := r.ensureClaimSSHKey(ctx, claim)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !sshKeyReady(sshKey) {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	if claim.Status.SSHSecretRef == nil {
		claimPubKey := sshKeyPublicKey(sshKey)
		if claimPubKey == "" {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		adminSecret, err := r.getAdminSecret(ctx, &m)
		if err != nil {
			return ctrl.Result{}, err
		}
		adminPubKey, err := adminPublicKeyLine(adminSecret, &m)
		if err != nil {
			return ctrl.Result{}, err
		}
		sshClient, err := dialMachine(&m, adminSecret)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("dial machine %s: %w", m.Name, err)
		}
		defer sshClient.Close()
		if err := setAuthorizedKeys(sshClient, adminPubKey, claimPubKey); err != nil {
			return ctrl.Result{}, fmt.Errorf("set authorized_keys on %s: %w", m.Name, err)
		}
	}

	claim.Status.Phase = umv1alpha1.ClaimReady
	claim.Status.Machine = &umv1alpha1.BoundMachine{
		Name:            m.Name,
		Address:         m.Spec.Address,
		ExternalAddress: m.Spec.ExternalAddress,
	}
	claim.Status.SSHSecretRef = &corev1.LocalObjectReference{Name: claim.Name + "-ssh"}
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

// ensureClaimSSHKey creates the SSHKey CR in the claim's namespace if it
// doesn't exist yet, then returns the current object.
func (r *UbuntuMachineClaimReconciler) ensureClaimSSHKey(
	ctx context.Context,
	claim *umv1alpha1.UbuntuMachineClaim,
) (*unstructured.Unstructured, error) {
	keyName := claim.Name + "-sshkey"
	key := types.NamespacedName{Namespace: claim.Namespace, Name: keyName}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(sshKeyGVK)
	if err := r.Get(ctx, key, obj); err == nil {
		return obj, nil
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	obj = &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "sshkeys.apeirora.eu/v1alpha1",
			"kind":       "SSHKey",
			"metadata": map[string]interface{}{
				"name":      keyName,
				"namespace": claim.Namespace,
			},
			"spec": map[string]interface{}{
				"keyType": "ed25519",
				"exportToSecret": map[string]interface{}{
					"name": claim.Name + "-ssh",
				},
			},
		},
	}
	if err := r.Create(ctx, obj); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create SSHKey %s: %w", keyName, err)
	}

	fresh := &unstructured.Unstructured{}
	fresh.SetGroupVersionKind(sshKeyGVK)
	if err := r.Get(ctx, key, fresh); err != nil {
		return nil, err
	}
	return fresh, nil
}

// removeClaimSSHKey resets /root/.ssh/authorized_keys on the bound machine to
// contain only the operator's admin public key (or nothing for password auth),
// removing the claim's key. This ensures only the operator can access root.
func (r *UbuntuMachineClaimReconciler) removeClaimSSHKey(
	ctx context.Context,
	claim *umv1alpha1.UbuntuMachineClaim,
) error {
	if claim.Status.Machine == nil {
		return nil
	}

	var machine umv1alpha1.UbuntuMachine
	if err := r.Get(ctx, types.NamespacedName{Name: claim.Status.Machine.Name}, &machine); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	adminSecret, err := r.getAdminSecret(ctx, &machine)
	if err != nil {
		return err
	}
	adminPubKey, err := adminPublicKeyLine(adminSecret, &machine)
	if err != nil {
		return err
	}
	sshClient, err := dialMachine(&machine, adminSecret)
	if err != nil {
		return fmt.Errorf("dial machine %s: %w", machine.Name, err)
	}
	defer sshClient.Close()
	return setAuthorizedKeys(sshClient, adminPubKey)
}

// getAdminSecret reads the admin SSH credential Secret for a machine.
func (r *UbuntuMachineClaimReconciler) getAdminSecret(
	ctx context.Context,
	machine *umv1alpha1.UbuntuMachine,
) (*corev1.Secret, error) {
	ref := machine.Spec.AdminSSH.PrivateKeySecretRef
	if ref == nil {
		ref = machine.Spec.AdminSSH.PasswordSecretRef
	}
	if ref == nil {
		return nil, fmt.Errorf("machine %q has no adminSSH credential reference", machine.Name)
	}
	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, &secret); err != nil {
		return nil, fmt.Errorf("get admin secret %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	return &secret, nil
}

// dialMachine opens an SSH connection to the machine using adminSSH credentials.
func dialMachine(machine *umv1alpha1.UbuntuMachine, adminSecret *corev1.Secret) (*gossh.Client, error) {
	host := machine.Spec.Address
	port := machine.Spec.AdminSSH.Port
	if port == 0 {
		port = 22
	}

	var authMethods []gossh.AuthMethod
	if machine.Spec.AdminSSH.PrivateKeySecretRef != nil {
		signer, err := gossh.ParsePrivateKey(adminSecret.Data[umv1alpha1.SSHSecretPrivateKeyKey])
		if err != nil {
			return nil, fmt.Errorf("parse admin private key: %w", err)
		}
		authMethods = append(authMethods, gossh.PublicKeys(signer))
	} else {
		authMethods = append(authMethods, gossh.Password(string(adminSecret.Data[umv1alpha1.SSHSecretPasswordKey])))
	}

	cfg := &gossh.ClientConfig{
		User:            machine.Spec.AdminSSH.User,
		Auth:            authMethods,
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec
		Timeout:         30 * time.Second,
	}
	return gossh.Dial("tcp", fmt.Sprintf("%s:%d", host, port), cfg)
}

// setAuthorizedKeys writes exactly the given keys to /root/.ssh/authorized_keys,
// replacing any previous content. Empty strings are ignored. Call with no keys
// (or only empty strings) to clear the file.
// Keys are passed via stdin to avoid shell quoting issues.
func setAuthorizedKeys(c *gossh.Client, keys ...string) error {
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		if k = strings.TrimSpace(k); k != "" {
			lines = append(lines, k)
		}
	}
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	sess, err := c.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()
	sess.Stdin = strings.NewReader(content)
	return sess.Run(`
		mkdir -p /root/.ssh && chmod 700 /root/.ssh
		cat > /root/.ssh/authorized_keys
		chmod 600 /root/.ssh/authorized_keys
	`)
}

// adminPublicKeyLine derives the admin's public key line from the private key in
// the admin secret. Returns an empty string when adminSSH uses password auth.
func adminPublicKeyLine(adminSecret *corev1.Secret, machine *umv1alpha1.UbuntuMachine) (string, error) {
	if machine.Spec.AdminSSH.PrivateKeySecretRef == nil {
		return "", nil
	}
	signer, err := gossh.ParsePrivateKey(adminSecret.Data[umv1alpha1.SSHSecretPrivateKeyKey])
	if err != nil {
		return "", fmt.Errorf("parse admin private key: %w", err)
	}
	return strings.TrimSpace(string(gossh.MarshalAuthorizedKey(signer.PublicKey()))), nil
}

// sshKeyReady reports whether the SSHKey CR has finished generating.
func sshKeyReady(obj *unstructured.Unstructured) bool {
	ready, _, _ := unstructured.NestedBool(obj.Object, "status", "ready")
	return ready
}

// sshKeyPublicKey returns the public key string from the SSHKey CR.
func sshKeyPublicKey(obj *unstructured.Unstructured) string {
	key, _, _ := unstructured.NestedString(obj.Object, "publicKey")
	return strings.TrimSpace(key)
}

// ownedBy reports whether machine m is currently bound to claim.
func ownedBy(m *umv1alpha1.UbuntuMachine, claim *umv1alpha1.UbuntuMachineClaim) bool {
	ref := m.Status.BoundClaimRef
	return ref != nil && ref.Name == claim.Name && ref.Namespace == claim.Namespace
}

// ownedMachines lists UbuntuMachines that already have boundClaimRef
// pointing at this claim.
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

// releaseOne flips one machine back to Available.
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
func setReady(claim *umv1alpha1.UbuntuMachineClaim, status metav1.ConditionStatus, reason, message string) {
	apimeta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
		Type:               ConditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: claim.Generation,
	})
}

// eligibleMachines lists Available UbuntuMachines that match the claim's
// selector and satisfy externalAddressRequired and resource floors.
func (r *UbuntuMachineClaimReconciler) eligibleMachines(
	ctx context.Context,
	claim *umv1alpha1.UbuntuMachineClaim,
) ([]umv1alpha1.UbuntuMachine, error) {
	var all umv1alpha1.UbuntuMachineList
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

// fits returns true when capacity satisfies required. Zero capacity means unconstrained.
func fits(capacity, required resource.Quantity) bool {
	if capacity.IsZero() {
		return true
	}
	return capacity.Cmp(required) >= 0
}

// bind claims the machine for the claim.
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

// releaseAll is the cascade for claim deletion: removes the per-claim SSH key
// from the machine, deletes the SSHKey CR, then returns all bound machines to Available.
func (r *UbuntuMachineClaimReconciler) releaseAll(
	ctx context.Context,
	claim *umv1alpha1.UbuntuMachineClaim,
) error {
	logger := log.FromContext(ctx)

	if claim.Status.SSHSecretRef != nil {
		if err := r.removeClaimSSHKey(ctx, claim); err != nil {
			logger.Error(err, "failed to remove claim SSH key; proceeding with release")
		}
	}

	sshKey := &unstructured.Unstructured{}
	sshKey.SetGroupVersionKind(sshKeyGVK)
	sshKey.SetName(claim.Name + "-sshkey")
	sshKey.SetNamespace(claim.Namespace)
	if err := r.Delete(ctx, sshKey); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete SSHKey: %w", err)
	}

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

// machineToPendingClaims wakes every Pending claim when an UbuntuMachine changes.
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
