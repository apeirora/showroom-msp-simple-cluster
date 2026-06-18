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
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	ironcorev1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ironcore/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ironcore/internal/ironcore"
)

const (
	requeueReady    = 2 * time.Minute
	requeueWorking  = 20 * time.Second
	requeueDeleting = 10 * time.Second

	sshSecretSuffix = "-ssh"
)

// IronCoreVMReconciler drives one IronCore Machine per IronCoreVM CR.
type IronCoreVMReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=ironcore.apeirora.eu,resources=ironcorevms,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ironcore.apeirora.eu,resources=ironcorevms/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ironcore.apeirora.eu,resources=ironcorevms/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=sshkeys.apeirora.eu,resources=sshkeys,verbs=get;list;watch

func (r *IronCoreVMReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var vm ironcorev1alpha1.IronCoreVM
	if err := r.Get(ctx, req.NamespacedName, &vm); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !vm.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &vm)
	}

	if !controllerutil.ContainsFinalizer(&vm, ironcorev1alpha1.FinalizerName) {
		patch := client.MergeFrom(vm.DeepCopy())
		controllerutil.AddFinalizer(&vm, ironcorev1alpha1.FinalizerName)
		if err := r.Patch(ctx, &vm, patch); err != nil {
			return ctrl.Result{}, err
		}
	}

	if vm.Status.Phase == "" {
		if err := r.setPhase(ctx, &vm, ironcorev1alpha1.PhaseInit, "Initialising", "operator picked up the CR"); err != nil {
			return ctrl.Result{}, err
		}
	}

	clients, err := r.ironcoreClients(ctx, &vm)
	if err != nil {
		return r.fail(ctx, &vm, "AuthError", err.Error())
	}

	sshSecretRef, publicKey, err := r.ensureSSHKeypair(ctx, &vm)
	if err != nil {
		return r.fail(ctx, &vm, "SSHKeyError", err.Error())
	}
	sshSecretName := sshSecretRef.Name

	vmLabels := ironcore.VMLabels(vm.Namespace, vm.Name)

	ignData, err := ironcore.BuildIgnition(vm.Spec.SSHUser, publicKey, vm.Spec.UserData)
	if err != nil {
		return r.fail(ctx, &vm, "IgnitionError", err.Error())
	}
	ignName := ironcore.IgnitionSecretName(vm.Namespace, vm.Name)
	if err := ironcore.EnsureIgnitionSecret(ctx, clients, ignName, ignData, vmLabels); err != nil {
		return r.fail(ctx, &vm, "IgnitionError", err.Error())
	}

	if err := ironcore.EnsureNetwork(ctx, clients, vm.Spec.Network); err != nil {
		return r.fail(ctx, &vm, "NetworkError", err.Error())
	}

	poolName := vm.Spec.IPPool
	if poolName == "" {
		poolName = vm.Spec.Network
		if err := ironcore.EnsurePrefix(ctx, clients, poolName, vm.Spec.NetworkCIDR); err != nil {
			return r.fail(ctx, &vm, "PrefixError", err.Error())
		}
	}

	var pick *ironcore.ClassPick
	switch {
	case vm.Spec.CloudProfile != "":
		pick, err = ironcore.GetMachineClass(ctx, clients.Client, vm.Spec.CloudProfile)
	case vm.Spec.Resources != nil:
		pick, err = ironcore.PickMachineClass(ctx, clients.Client,
			vm.Spec.Resources.CPU, vm.Spec.Resources.Memory, vm.Spec.Resources.MachineClassPattern)
	default:
		err = fmt.Errorf("set exactly one of spec.cloudProfile or spec.resources")
	}
	if err != nil {
		_ = r.setCloudProfileCondition(ctx, &vm, metav1.ConditionFalse, "Unresolved", err.Error())
		r.eventf(&vm, corev1.EventTypeWarning, "CloudProfileError", err.Error())
		return r.fail(ctx, &vm, "MachineClassError", err.Error())
	}
	if err := r.setCloudProfileCondition(ctx, &vm, metav1.ConditionTrue, "Resolved",
		fmt.Sprintf("resolved to MachineClass %s (%s)", pick.Name, pick.Description)); err != nil {
		return ctrl.Result{}, err
	}
	resolved := ironcorev1alpha1.ResolvedMachineClass{
		Name:        pick.Name,
		CPU:         pick.CPU,
		Memory:      pick.MemoryHuman,
		Description: pick.Description,
		Image:       vm.Spec.Image,
	}

	vipName := ""
	if vm.Spec.PublicIP {
		vipName = ironcore.VIPName(vm.Namespace, vm.Name)
		if err := ironcore.EnsureVirtualIP(ctx, clients, vipName, vmLabels); err != nil {
			return r.fail(ctx, &vm, "VirtualIPError", err.Error())
		}
	}

	nicName := ironcore.NICName(vm.Namespace, vm.Name)
	if err := ironcore.EnsureNetworkInterface(ctx, clients, nicName, vm.Spec.Network, poolName, vipName, vmLabels); err != nil {
		return r.fail(ctx, &vm, "NICError", err.Error())
	}

	machine, err := ironcore.FindMachineByVM(ctx, clients, vmLabels)
	if err != nil {
		return r.fail(ctx, &vm, "ListError", err.Error())
	}

	if machine == nil {
		machineName := ironcore.MachineName(vm.Namespace, vm.Name)
		log.Info("creating IronCore Machine", "name", machineName, "class", pick.Name)
		r.eventf(&vm, corev1.EventTypeNormal, "CreatingMachine",
			"creating %s (class %s, image %s)", machineName, pick.Name, vm.Spec.Image)
		machine, err = ironcore.CreateMachine(ctx, clients, machineName, pick.Name,
			nicName, ignName, vm.Spec.Image, vm.Spec.MachinePool, vmLabels)
		if err != nil {
			return r.fail(ctx, &vm, "CreateError", err.Error())
		}
		r.eventf(&vm, corev1.EventTypeNormal, "Created",
			"created IronCore Machine %s (class %s)", machineName, pick.Name)
		if machine == nil {
			return r.requeueProvisioning(ctx, &vm, resolved, sshSecretName)
		}
	}

	internalIP, publicIP, err := ironcore.NICStatus(ctx, clients, nicName)
	if err != nil {
		log.Error(err, "NIC status read failed, requeuing")
	}

	patch := client.MergeFrom(vm.DeepCopy())
	vm.Status.MachineID = machine.Status.MachineID
	vm.Status.MachineState = string(machine.Status.State)
	vm.Status.InternalAddress = internalIP
	vm.Status.ExternalAddress = publicIP
	vm.Status.ResolvedMachineClass = resolved
	vm.Status.SSHSecretRef = &corev1.LocalObjectReference{Name: sshSecretName}
	vm.Status.ObservedGeneration = vm.Generation

	if machine.Status.State == "Running" {
		vm.Status.Phase = ironcorev1alpha1.PhaseReady
		apimeta.SetStatusCondition(&vm.Status.Conditions, metav1.Condition{
			Type:               ironcorev1alpha1.ConditionReady,
			Status:             metav1.ConditionTrue,
			Reason:             "MachineRunning",
			Message:            fmt.Sprintf("machine %s is Running", machine.Name),
			ObservedGeneration: vm.Generation,
		})
		apimeta.RemoveStatusCondition(&vm.Status.Conditions, ironcorev1alpha1.ConditionError)
	} else {
		vm.Status.Phase = ironcorev1alpha1.PhaseProvisioning
		apimeta.SetStatusCondition(&vm.Status.Conditions, metav1.Condition{
			Type:               ironcorev1alpha1.ConditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "MachineNotReady",
			Message:            fmt.Sprintf("waiting for machine %s to reach Running (currently %s)", machine.Name, machine.Status.State),
			ObservedGeneration: vm.Generation,
		})
	}

	if err := r.Status().Patch(ctx, &vm, patch); err != nil {
		return ctrl.Result{}, err
	}

	if vm.Status.Phase == ironcorev1alpha1.PhaseReady {
		return ctrl.Result{RequeueAfter: requeueReady}, nil
	}
	return ctrl.Result{RequeueAfter: requeueWorking}, nil
}

func (r *IronCoreVMReconciler) requeueProvisioning(
	ctx context.Context,
	vm *ironcorev1alpha1.IronCoreVM,
	resolved ironcorev1alpha1.ResolvedMachineClass,
	sshSecretName string,
) (ctrl.Result, error) {
	patch := client.MergeFrom(vm.DeepCopy())
	vm.Status.Phase = ironcorev1alpha1.PhaseProvisioning
	vm.Status.ResolvedMachineClass = resolved
	vm.Status.SSHSecretRef = &corev1.LocalObjectReference{Name: sshSecretName}
	apimeta.SetStatusCondition(&vm.Status.Conditions, metav1.Condition{
		Type:               ironcorev1alpha1.ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             "MachineCreating",
		Message:            "Machine create accepted, not yet visible in listings",
		ObservedGeneration: vm.Generation,
	})
	if err := r.Status().Patch(ctx, vm, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueWorking}, nil
}

func (r *IronCoreVMReconciler) reconcileDelete(ctx context.Context, vm *ironcorev1alpha1.IronCoreVM) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(vm, ironcorev1alpha1.FinalizerName) {
		return ctrl.Result{}, nil
	}

	if err := r.cleanupRemote(ctx, vm); err != nil {
		// A Failed VM never provisioned successfully, so an unreachable or
		// forbidden backend must not trap it — drop the finalizer best-effort.
		if vm.Status.Phase == ironcorev1alpha1.PhaseFailed {
			log.Error(err, "remote cleanup failed for a Failed VM; removing finalizer anyway")
			r.eventf(vm, corev1.EventTypeWarning, "ForceDeleted",
				"removing finalizer despite cleanup error: %v", err)
		} else {
			log.Error(err, "remote cleanup during delete; retrying")
			return ctrl.Result{RequeueAfter: requeueDeleting}, nil
		}
	}

	patch := client.MergeFrom(vm.DeepCopy())
	controllerutil.RemoveFinalizer(vm, ironcorev1alpha1.FinalizerName)
	if err := r.Patch(ctx, vm, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// cleanupRemote best-effort deletes the VM's remote IronCore resources. It
// returns an error when the IronCore client cannot be built or a delete fails.
func (r *IronCoreVMReconciler) cleanupRemote(ctx context.Context, vm *ironcorev1alpha1.IronCoreVM) error {
	clients, err := r.ironcoreClients(ctx, vm)
	if err != nil {
		return err
	}
	r.eventf(vm, corev1.EventTypeNormal, "Deleting", "deleting IronCore resources for %s", vm.Name)
	labels := ironcore.VMLabels(vm.Namespace, vm.Name)
	if err := ironcore.DeleteByVM(ctx, clients, labels); err != nil {
		return err
	}
	if exists, err := ironcore.AnyExistForVM(ctx, clients, labels); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("IronCore resources for %s still terminating", vm.Name)
	}
	r.eventf(vm, corev1.EventTypeNormal, "Deleted", "deleted IronCore resources for %s", vm.Name)
	return nil
}

func (r *IronCoreVMReconciler) eventf(vm *ironcorev1alpha1.IronCoreVM, eventType, reason, format string, args ...any) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(vm, eventType, reason, format, args...)
}

func (r *IronCoreVMReconciler) ironcoreClients(ctx context.Context, vm *ironcorev1alpha1.IronCoreVM) (*ironcore.Clients, error) {
	var creds corev1.Secret
	key := ironCoreKubeconfigRef(vm)
	if err := r.Get(ctx, key, &creds); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("credentials Secret %s not found", key.String())
		}
		return nil, fmt.Errorf("get credentials Secret %s: %w", key.String(), err)
	}
	return ironcore.NewClientsFromSecret(ctx, &creds, vm.Spec.IronCoreNamespace)
}

func ironCoreKubeconfigRef(vm *ironcorev1alpha1.IronCoreVM) types.NamespacedName {
	ns := vm.Spec.IronCoreKubeconfig.Namespace
	if ns == "" {
		ns = vm.Namespace
	}
	return types.NamespacedName{Name: vm.Spec.IronCoreKubeconfig.Name, Namespace: ns}
}

// isSSHKeyResourceRef reports whether ref points at an SSHKey custom resource
// (as opposed to a core Secret).
func isSSHKeyResourceRef(ref *corev1.TypedObjectReference) bool {
	return ref != nil && ref.Kind == ironcorev1alpha1.SSHKeyKind &&
		ref.APIGroup != nil && *ref.APIGroup == ironcorev1alpha1.SSHKeyAPIGroup
}

func refNamespace(ns *string, fallback string) string {
	if ns != nil && *ns != "" {
		return *ns
	}
	return fallback
}

// secretKeyRef resolves the Secret holding (or to hold) the keypair, for the
// Secret/unset path. shared is true when the VM names an explicit Secret.
func secretKeyRef(vm *ironcorev1alpha1.IronCoreVM) (nn types.NamespacedName, shared bool) {
	if ref := vm.Spec.SSHKeyRef; ref != nil && ref.Name != "" {
		return types.NamespacedName{Name: ref.Name, Namespace: refNamespace(ref.Namespace, vm.Namespace)}, true
	}
	return types.NamespacedName{Name: vm.Name + sshSecretSuffix, Namespace: vm.Namespace}, false
}

// resolveSSHKeyResource reads the public key from a referenced SSHKey custom
// resource. The returned name is the SSHKey's exported Secret (for status), or
// the SSHKey name when it exports none.
func (r *IronCoreVMReconciler) resolveSSHKeyResource(ctx context.Context, vm *ironcorev1alpha1.IronCoreVM) (types.NamespacedName, string, error) {
	ref := vm.Spec.SSHKeyRef
	ns := refNamespace(ref.Namespace, vm.Namespace)
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group: ironcorev1alpha1.SSHKeyAPIGroup, Version: ironcorev1alpha1.SSHKeyVersion, Kind: ironcorev1alpha1.SSHKeyKind})
	if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ns}, u); err != nil {
		return types.NamespacedName{}, "", fmt.Errorf("get SSHKey %s/%s: %w", ns, ref.Name, err)
	}
	pub, _, _ := unstructured.NestedString(u.Object, "publicKey")
	if strings.TrimSpace(pub) == "" {
		return types.NamespacedName{}, "", fmt.Errorf("SSHKey %s/%s has no public key yet", ns, ref.Name)
	}
	name, _, _ := unstructured.NestedString(u.Object, "spec", "exportToSecret", "name")
	if name == "" {
		name = ref.Name
	}
	return types.NamespacedName{Name: name, Namespace: ns}, pub, nil
}

func (r *IronCoreVMReconciler) ensureSSHKeypair(ctx context.Context, vm *ironcorev1alpha1.IronCoreVM) (types.NamespacedName, string, error) {
	if isSSHKeyResourceRef(vm.Spec.SSHKeyRef) {
		return r.resolveSSHKeyResource(ctx, vm)
	}
	ref, shared := secretKeyRef(vm)

	var existing corev1.Secret
	err := r.Get(ctx, ref, &existing)
	switch {
	case err == nil:
		pub := existing.Data[ironcorev1alpha1.SSHSecretPublicKeyKey]
		if len(pub) == 0 {
			return ref, "", fmt.Errorf("Secret %s missing %q", ref.String(), ironcorev1alpha1.SSHSecretPublicKeyKey)
		}
		return ref, string(pub), nil
	case !apierrors.IsNotFound(err):
		return ref, "", fmt.Errorf("get Secret %s: %w", ref.String(), err)
	}

	priv, pub, err := ironcore.GenerateEd25519Keypair(vm.Namespace + "/" + vm.Name)
	if err != nil {
		return ref, "", err
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ref.Name,
			Namespace: ref.Namespace,
		},
		Type: corev1.SecretTypeSSHAuth,
		Data: map[string][]byte{
			ironcorev1alpha1.SSHSecretPrivateKeyKey: priv,
			ironcorev1alpha1.SSHSecretPublicKeyKey:  pub,
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

func (r *IronCoreVMReconciler) setPhase(ctx context.Context, vm *ironcorev1alpha1.IronCoreVM, phase ironcorev1alpha1.Phase, reason, msg string) error {
	if vm.Status.Phase == phase {
		return nil
	}
	patch := client.MergeFrom(vm.DeepCopy())
	vm.Status.Phase = phase
	apimeta.SetStatusCondition(&vm.Status.Conditions, metav1.Condition{
		Type:               ironcorev1alpha1.ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: vm.Generation,
	})
	return r.Status().Patch(ctx, vm, patch)
}

func (r *IronCoreVMReconciler) setCloudProfileCondition(ctx context.Context, vm *ironcorev1alpha1.IronCoreVM, status metav1.ConditionStatus, reason, msg string) error {
	patch := client.MergeFrom(vm.DeepCopy())
	apimeta.SetStatusCondition(&vm.Status.Conditions, metav1.Condition{
		Type:               ironcorev1alpha1.ConditionCloudProfile,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: vm.Generation,
	})
	return r.Status().Patch(ctx, vm, patch)
}

func (r *IronCoreVMReconciler) fail(ctx context.Context, vm *ironcorev1alpha1.IronCoreVM, reason, msg string) (ctrl.Result, error) {
	patch := client.MergeFrom(vm.DeepCopy())
	vm.Status.Phase = ironcorev1alpha1.PhaseFailed
	apimeta.SetStatusCondition(&vm.Status.Conditions, metav1.Condition{
		Type:               ironcorev1alpha1.ConditionError,
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

func (r *IronCoreVMReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&ironcorev1alpha1.IronCoreVM{}).
		Named("ironcorevm").
		Complete(r)
}
