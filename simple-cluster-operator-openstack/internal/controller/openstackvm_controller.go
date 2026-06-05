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
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	openstackv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-openstack/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-openstack/internal/openstack"
)

const (
	requeueReady   = 2 * time.Minute
	requeueWorking = 20 * time.Second

	sshSecretSuffix = "-ssh"
)

// OpenStackVMReconciler drives one Nova server per OpenStackVM CR.
// PVC-style: one CR maps to one Nova server, one keypair Secret, and at
// most one floating IP. Failures of one VM never block siblings — they
// live in their own reconcile loop.
type OpenStackVMReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=openstack.apeirora.eu,resources=openstackvms,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=openstack.apeirora.eu,resources=openstackvms/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=openstack.apeirora.eu,resources=openstackvms/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *OpenStackVMReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var vm openstackv1alpha1.OpenStackVM
	if err := r.Get(ctx, req.NamespacedName, &vm); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !vm.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &vm)
	}

	if !controllerutil.ContainsFinalizer(&vm, openstackv1alpha1.FinalizerName) {
		patch := client.MergeFrom(vm.DeepCopy())
		controllerutil.AddFinalizer(&vm, openstackv1alpha1.FinalizerName)
		if err := r.Patch(ctx, &vm, patch); err != nil {
			return ctrl.Result{}, err
		}
	}

	if vm.Status.Phase == "" {
		if err := r.setPhase(ctx, &vm, openstackv1alpha1.PhaseInit, "Initialising",
			"operator picked up the CR"); err != nil {
			return ctrl.Result{}, err
		}
	}

	clients, err := r.openstackClients(ctx, &vm)
	if err != nil {
		return r.fail(ctx, &vm, "AuthError", err.Error())
	}

	sshSecretRef, publicKey, err := r.ensureSSHKeypair(ctx, &vm)
	if err != nil {
		return r.fail(ctx, &vm, "SSHKeyError", err.Error())
	}
	keypairName := keypairNameFor(sshSecretRef)
	if err := openstack.EnsureKeypair(ctx, clients.Compute, keypairName, publicKey); err != nil {
		return r.fail(ctx, &vm, "KeypairError", err.Error())
	}
	sshSecretName := sshSecretRef.Name

	networkID, err := openstack.ResolveNetwork(ctx, clients.Network, vm.Spec.Network)
	if err != nil {
		return r.fail(ctx, &vm, "NetworkError", err.Error())
	}

	flavor, err := openstack.PickFlavor(ctx, clients.Compute,
		vm.Spec.Resources.CPU, vm.Spec.Resources.Memory, vm.Spec.Resources.FlavorPattern)
	if err != nil {
		return r.fail(ctx, &vm, "FlavorError", err.Error())
	}
	resolvedFlavor := openstackv1alpha1.ResolvedFlavor{
		Flavor:            flavor.Name,
		FlavorDescription: flavor.Description,
		CPU:               int32(flavor.VCPUs),
		Memory:            flavor.MemoryHuman,
		Image:             vm.Spec.Image,
	}

	imageID, err := openstack.ResolveImage(ctx, clients.Image, vm.Spec.Image)
	if err != nil {
		return r.fail(ctx, &vm, "ImageError", err.Error())
	}

	vmKey := openstack.VMKey(vm.Namespace, vm.Name)
	server, err := openstack.FindByVM(ctx, clients.Compute, vmKey)
	if err != nil {
		return r.fail(ctx, &vm, "ListError", err.Error())
	}

	if server == nil {
		serverName := openstack.SanitizeServerName(vm.Name)
		log.Info("creating Nova server", "vm", vmKey, "name", serverName, "flavor", flavor.Name)
		r.eventf(&vm, corev1.EventTypeNormal, "CreatingVM",
			"creating %s (flavor %s, image %s)", serverName, flavor.Name, vm.Spec.Image)
		if _, err := openstack.CreateServer(
			ctx, clients, vmKey,
			serverName, networkID, keypairName, flavor.ID, imageID,
			vm.Spec.SecurityGroups, vm.Spec.UserData,
		); err != nil {
			return r.fail(ctx, &vm, "CreateError", err.Error())
		}
		// Re-list so the next steps see the freshly-created server.
		server, err = openstack.FindByVM(ctx, clients.Compute, vmKey)
		if err != nil || server == nil {
			// Eventual consistency: come back shortly to pick it up.
			return r.requeueProvisioning(ctx, &vm, resolvedFlavor, sshSecretName, keypairName)
		}
	}

	if vm.Spec.FloatingIP && server.Status == "ACTIVE" && vm.Spec.FloatingIPPool != "" {
		fip, err := openstack.EnsureFloatingIP(ctx, clients, server, vm.Spec.FloatingIPPool, vmKey)
		if err != nil {
			log.Error(err, "floating IP allocation deferred", "server", server.Name)
		} else {
			server.FloatingIP = fip
		}
	}

	patch := client.MergeFrom(vm.DeepCopy())
	vm.Status.ServerID = server.ID
	vm.Status.NovaStatus = server.Status
	vm.Status.InternalAddress = server.InternalIP
	vm.Status.ExternalAddress = server.FloatingIP
	vm.Status.ResolvedFlavor = resolvedFlavor
	vm.Status.KeypairName = keypairName
	vm.Status.SSHSecretRef = &corev1.LocalObjectReference{Name: sshSecretName}
	vm.Status.ObservedGeneration = vm.Generation

	if server.Status == "ACTIVE" {
		vm.Status.Phase = openstackv1alpha1.PhaseReady
		apimeta.SetStatusCondition(&vm.Status.Conditions, metav1.Condition{
			Type:               openstackv1alpha1.ConditionReady,
			Status:             metav1.ConditionTrue,
			Reason:             "ServerActive",
			Message:            fmt.Sprintf("server %s is ACTIVE", server.Name),
			ObservedGeneration: vm.Generation,
		})
		apimeta.RemoveStatusCondition(&vm.Status.Conditions, openstackv1alpha1.ConditionError)
	} else {
		vm.Status.Phase = openstackv1alpha1.PhaseProvisioning
		apimeta.SetStatusCondition(&vm.Status.Conditions, metav1.Condition{
			Type:               openstackv1alpha1.ConditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "ServerNotReady",
			Message:            fmt.Sprintf("waiting for server %s to reach ACTIVE (currently %s)", server.Name, server.Status),
			ObservedGeneration: vm.Generation,
		})
	}
	if err := r.Status().Patch(ctx, &vm, patch); err != nil {
		return ctrl.Result{}, err
	}

	if vm.Status.Phase == openstackv1alpha1.PhaseReady {
		return ctrl.Result{RequeueAfter: requeueReady}, nil
	}
	return ctrl.Result{RequeueAfter: requeueWorking}, nil
}

// requeueProvisioning writes a Provisioning status snapshot when the
// Nova create call has returned but FindByVM hasn't yet seen the new
// server. Carries the partial resolution (flavor, ssh refs) so the user
// can see we're alive while Nova catches up.
func (r *OpenStackVMReconciler) requeueProvisioning(
	ctx context.Context,
	vm *openstackv1alpha1.OpenStackVM,
	resolved openstackv1alpha1.ResolvedFlavor,
	sshSecretName, keypairName string,
) (ctrl.Result, error) {
	patch := client.MergeFrom(vm.DeepCopy())
	vm.Status.Phase = openstackv1alpha1.PhaseProvisioning
	vm.Status.ResolvedFlavor = resolved
	vm.Status.KeypairName = keypairName
	vm.Status.SSHSecretRef = &corev1.LocalObjectReference{Name: sshSecretName}
	apimeta.SetStatusCondition(&vm.Status.Conditions, metav1.Condition{
		Type:               openstackv1alpha1.ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             "ServerCreating",
		Message:            "Nova create accepted, server not yet visible in listings",
		ObservedGeneration: vm.Generation,
	})
	if err := r.Status().Patch(ctx, vm, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueWorking}, nil
}

func (r *OpenStackVMReconciler) reconcileDelete(ctx context.Context, vm *openstackv1alpha1.OpenStackVM) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(vm, openstackv1alpha1.FinalizerName) {
		return ctrl.Result{}, nil
	}

	clients, err := r.openstackClients(ctx, vm)
	if err != nil {
		log.Error(err, "openstack auth during delete; retrying")
		return ctrl.Result{RequeueAfter: requeueWorking}, nil
	}

	vmKey := openstack.VMKey(vm.Namespace, vm.Name)
	server, err := openstack.FindByVM(ctx, clients.Compute, vmKey)
	if err != nil {
		log.Error(err, "find server during delete; retrying")
		return ctrl.Result{RequeueAfter: requeueWorking}, nil
	}
	if server != nil {
		if err := openstack.ReleaseFloatingIPsForServer(ctx, clients, server); err != nil {
			log.Error(err, "release fips during delete; retrying", "server", server.Name)
			return ctrl.Result{RequeueAfter: requeueWorking}, nil
		}
		if err := openstack.DeleteServer(ctx, clients.Compute, server.ID); err != nil {
			log.Error(err, "delete server during delete; retrying", "server", server.Name)
			return ctrl.Result{RequeueAfter: requeueWorking}, nil
		}
	}

	// Note: Nova keypair cleanup happens on Secret deletion via the
	// SSH-Secret reconciler. Per-VM Secrets have an ownerRef back to the
	// VM so they enter Terminating when the VM is gone; shared Secrets
	// are admin-managed and stay until explicitly deleted.

	patch := client.MergeFrom(vm.DeepCopy())
	controllerutil.RemoveFinalizer(vm, openstackv1alpha1.FinalizerName)
	if err := r.Patch(ctx, vm, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// eventf records a Kubernetes Event on the CR, tolerating a nil
// recorder (envtest / unit tests don't always wire one up).
func (r *OpenStackVMReconciler) eventf(vm *openstackv1alpha1.OpenStackVM, eventType, reason, format string, args ...any) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(vm, eventType, reason, format, args...)
}

func (r *OpenStackVMReconciler) openstackClients(ctx context.Context, vm *openstackv1alpha1.OpenStackVM) (*openstack.Clients, error) {
	var creds corev1.Secret
	key := credentialsRef(vm)
	if err := r.Get(ctx, key, &creds); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("credentials Secret %s not found", key.String())
		}
		return nil, fmt.Errorf("get credentials Secret %s: %w", key.String(), err)
	}
	return openstack.NewClientsFromSecret(ctx, &creds)
}

// credentialsRef resolves the Secret carrying the VM's OpenStack
// credentials. spec.credentialsRef.namespace is optional and defaults
// to the VM's namespace.
func credentialsRef(vm *openstackv1alpha1.OpenStackVM) types.NamespacedName {
	ns := vm.Spec.CredentialsRef.Namespace
	if ns == "" {
		ns = vm.Namespace
	}
	return types.NamespacedName{Name: vm.Spec.CredentialsRef.Name, Namespace: ns}
}

// effectiveSSHKeyRef computes which Secret carries this VM's keypair.
// When spec.sshKeySecretRef is set we use that (with namespace defaulted
// to the VM's namespace); otherwise we fall back to the per-VM Secret
// `<vm-name>-ssh` in vm.Namespace.
func effectiveSSHKeyRef(vm *openstackv1alpha1.OpenStackVM) types.NamespacedName {
	if vm.Spec.SSHKeySecretRef != nil && vm.Spec.SSHKeySecretRef.Name != "" {
		ns := vm.Spec.SSHKeySecretRef.Namespace
		if ns == "" {
			ns = vm.Namespace
		}
		return types.NamespacedName{Name: vm.Spec.SSHKeySecretRef.Name, Namespace: ns}
	}
	return types.NamespacedName{Name: vm.Name + sshSecretSuffix, Namespace: vm.Namespace}
}

// keypairNameFor derives a Nova keypair name from a Secret reference so
// multiple VMs sharing the same Secret target the same Nova keypair.
func keypairNameFor(ref types.NamespacedName) string {
	return ref.Namespace + "-" + ref.Name
}

// ensureSSHKeypair resolves the SSH-key Secret for this VM:
//   - if spec.sshKeySecretRef is set and the Secret exists → reuse it;
//   - if spec.sshKeySecretRef is set and the Secret does not exist →
//     generate the keypair into that ref;
//   - if spec.sshKeySecretRef is unset → use/create `<vm-name>-ssh` in
//     vm.Namespace (per-VM mode, with ownerRef back to the VM).
//
// On every reconcile the Secret is patched to carry the keypair-cleanup
// finalizer and three annotations (credentials-name, credentials-namespace,
// keypair-name) so the SSH-Secret reconciler can authenticate to delete
// the Nova keypair when the Secret is later deleted, even after the VM
// CR is gone.
func (r *OpenStackVMReconciler) ensureSSHKeypair(ctx context.Context, vm *openstackv1alpha1.OpenStackVM) (types.NamespacedName, string, error) {
	ref := effectiveSSHKeyRef(vm)
	shared := vm.Spec.SSHKeySecretRef != nil && vm.Spec.SSHKeySecretRef.Name != ""

	var existing corev1.Secret
	err := r.Get(ctx, ref, &existing)
	switch {
	case err == nil:
		pub := existing.Data[openstackv1alpha1.SSHSecretPublicKeyKey]
		if len(pub) == 0 {
			return ref, "", fmt.Errorf("Secret %s missing %q", ref.String(), openstackv1alpha1.SSHSecretPublicKeyKey)
		}
		if err := r.ensureSecretBookkeeping(ctx, &existing, vm, ref, !shared); err != nil {
			return ref, "", err
		}
		return ref, string(pub), nil
	case !apierrors.IsNotFound(err):
		return ref, "", fmt.Errorf("get Secret %s: %w", ref.String(), err)
	}

	priv, pub, err := openstack.GenerateEd25519Keypair(vm.Namespace + "/" + vm.Name)
	if err != nil {
		return ref, "", err
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ref.Name,
			Namespace: ref.Namespace,
			Annotations: map[string]string{
				openstackv1alpha1.SSHSecretCredentialsNameAnno:      vm.Spec.CredentialsRef.Name,
				openstackv1alpha1.SSHSecretCredentialsNamespaceAnno: credentialsRef(vm).Namespace,
				openstackv1alpha1.SSHSecretKeypairNameAnno:          keypairNameFor(ref),
			},
			Finalizers: []string{openstackv1alpha1.KeypairFinalizerName},
		},
		Type: corev1.SecretTypeSSHAuth,
		Data: map[string][]byte{
			openstackv1alpha1.SSHSecretPrivateKeyKey: priv,
			openstackv1alpha1.SSHSecretPublicKeyKey:  pub,
		},
	}
	if !shared {
		if err := controllerutil.SetControllerReference(vm, secret, r.Scheme); err != nil {
			return ref, "", fmt.Errorf("set owner ref on SSH Secret: %w", err)
		}
	}
	if err := r.Create(ctx, secret); err != nil {
		return ref, "", fmt.Errorf("create SSH Secret %s: %w", ref.String(), err)
	}
	return ref, string(pub), nil
}

// ensureSecretBookkeeping makes sure an already-existing SSH Secret has
// the keypair-cleanup finalizer and the credentials/keypair annotations
// that the SSH-Secret reconciler needs to clean up Nova when the Secret
// is later deleted. For per-VM Secrets it also adds the VM ownerRef so
// K8s GC tears the Secret down when the VM is deleted.
func (r *OpenStackVMReconciler) ensureSecretBookkeeping(
	ctx context.Context,
	secret *corev1.Secret,
	vm *openstackv1alpha1.OpenStackVM,
	ref types.NamespacedName,
	wantOwnerRef bool,
) error {
	orig := secret.DeepCopy()
	changed := false

	if !controllerutil.ContainsFinalizer(secret, openstackv1alpha1.KeypairFinalizerName) {
		controllerutil.AddFinalizer(secret, openstackv1alpha1.KeypairFinalizerName)
		changed = true
	}
	if secret.Annotations == nil {
		secret.Annotations = map[string]string{}
	}
	want := map[string]string{
		openstackv1alpha1.SSHSecretCredentialsNameAnno:      vm.Spec.CredentialsRef.Name,
		openstackv1alpha1.SSHSecretCredentialsNamespaceAnno: credentialsRef(vm).Namespace,
		openstackv1alpha1.SSHSecretKeypairNameAnno:          keypairNameFor(ref),
	}
	for k, v := range want {
		if secret.Annotations[k] != v {
			secret.Annotations[k] = v
			changed = true
		}
	}
	if wantOwnerRef && !hasOwner(secret, vm) {
		if err := controllerutil.SetControllerReference(vm, secret, r.Scheme); err != nil {
			return fmt.Errorf("set owner ref on SSH Secret: %w", err)
		}
		changed = true
	}
	if !changed {
		return nil
	}
	if err := r.Patch(ctx, secret, client.MergeFrom(orig)); err != nil {
		return fmt.Errorf("patch SSH Secret bookkeeping: %w", err)
	}
	return nil
}

func hasOwner(secret *corev1.Secret, vm *openstackv1alpha1.OpenStackVM) bool {
	for _, o := range secret.GetOwnerReferences() {
		if o.UID == vm.UID {
			return true
		}
	}
	return false
}

func (r *OpenStackVMReconciler) setPhase(ctx context.Context, vm *openstackv1alpha1.OpenStackVM, phase openstackv1alpha1.Phase, reason, msg string) error {
	if vm.Status.Phase == phase {
		return nil
	}
	patch := client.MergeFrom(vm.DeepCopy())
	vm.Status.Phase = phase
	apimeta.SetStatusCondition(&vm.Status.Conditions, metav1.Condition{
		Type:               openstackv1alpha1.ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: vm.Generation,
	})
	return r.Status().Patch(ctx, vm, patch)
}

func (r *OpenStackVMReconciler) fail(ctx context.Context, vm *openstackv1alpha1.OpenStackVM, reason, msg string) (ctrl.Result, error) {
	patch := client.MergeFrom(vm.DeepCopy())
	vm.Status.Phase = openstackv1alpha1.PhaseFailed
	apimeta.SetStatusCondition(&vm.Status.Conditions, metav1.Condition{
		Type:               openstackv1alpha1.ConditionError,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: vm.Generation,
	})
	if err := r.Status().Patch(ctx, vm, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueWorking}, nil
}

func (r *OpenStackVMReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&openstackv1alpha1.OpenStackVM{}).
		Named("openstackvm").
		Complete(r)
}
