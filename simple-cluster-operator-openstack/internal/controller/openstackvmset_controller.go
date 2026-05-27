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
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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

// OpenStackVMSetReconciler keeps Nova servers, SSH keypair, and floating
// IPs in sync with the OpenStackVMSet CR.
type OpenStackVMSetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=openstack.apeirora.eu,resources=openstackvmsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=openstack.apeirora.eu,resources=openstackvmsets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=openstack.apeirora.eu,resources=openstackvmsets/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

// nolint:gocyclo // sequential orchestration; splitting hides intent
func (r *OpenStackVMSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var vmset openstackv1alpha1.OpenStackVMSet
	if err := r.Get(ctx, req.NamespacedName, &vmset); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !vmset.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &vmset)
	}

	if !controllerutil.ContainsFinalizer(&vmset, openstackv1alpha1.FinalizerName) {
		patch := client.MergeFrom(vmset.DeepCopy())
		controllerutil.AddFinalizer(&vmset, openstackv1alpha1.FinalizerName)
		if err := r.Patch(ctx, &vmset, patch); err != nil {
			return ctrl.Result{}, err
		}
	}

	clients, err := r.openstackClients(ctx, &vmset)
	if err != nil {
		return r.fail(ctx, &vmset, "AuthError", err.Error())
	}

	sshSecretName, publicKey, err := r.ensureSSHKeypair(ctx, &vmset)
	if err != nil {
		return r.fail(ctx, &vmset, "SSHKeyError", err.Error())
	}
	keypairName := vmset.Name
	if err := openstack.EnsureKeypair(ctx, clients.Compute, keypairName, publicKey); err != nil {
		return r.fail(ctx, &vmset, "KeypairError", err.Error())
	}

	networkID, err := openstack.ResolveNetwork(ctx, clients.Network, vmset.Spec.Network)
	if err != nil {
		return r.fail(ctx, &vmset, "NetworkError", err.Error())
	}

	vmsetKey := openstack.VMSetKey(vmset.Namespace, vmset.Name)
	owned, err := openstack.ListByVMSet(ctx, clients.Compute, vmsetKey)
	if err != nil {
		return r.fail(ctx, &vmset, "ListError", err.Error())
	}

	if err := r.setPhase(ctx, &vmset, openstackv1alpha1.PhaseProvisioning, "Reconciling", "ensuring servers"); err != nil {
		return ctrl.Result{}, err
	}

	byGroup := indexByGroup(owned)
	groupStatuses := make([]openstackv1alpha1.GroupStatus, 0, len(vmset.Spec.Groups))
	allReady := true

	for _, g := range vmset.Spec.Groups {
		existing := byGroup[g.Name]
		sort.Slice(existing, func(i, j int) bool { return existing[i].Name < existing[j].Name })

		for i := int32(len(existing)); i < g.Count; i++ {
			name := openstack.SanitizeServerName(fmt.Sprintf("%s-%s-%d", vmset.Name, g.Name, i))
			log.Info("creating Nova server", "vmset", vmsetKey, "group", g.Name, "name", name)
			if _, err := openstack.CreateServer(ctx, clients, vmsetKey, g, name, networkID, keypairName, vmset.Spec.SecurityGroups); err != nil {
				return r.fail(ctx, &vmset, "CreateError", err.Error())
			}
		}

		// Surplus: delete the highest-indexed servers to converge down to Count.
		for i := g.Count; i < int32(len(existing)); i++ {
			log.Info("deleting surplus server", "id", existing[i].ID, "name", existing[i].Name)
			s := existing[i]
			if err := openstack.ReleaseFloatingIPsForServer(ctx, clients, &s); err != nil {
				return r.fail(ctx, &vmset, "FIPCleanupError", err.Error())
			}
			if err := openstack.DeleteServer(ctx, clients.Compute, s.ID); err != nil {
				return r.fail(ctx, &vmset, "DeleteError", err.Error())
			}
		}

		fresh, err := openstack.ListByVMSet(ctx, clients.Compute, vmsetKey)
		if err != nil {
			return r.fail(ctx, &vmset, "ListError", err.Error())
		}
		owned = fresh
		current := pickGroup(owned, g.Name)

		hosts := make([]openstackv1alpha1.HostStatus, 0, len(current))
		ready := int32(0)
		for i := range current {
			s := &current[i]
			if s.Status == "ACTIVE" {
				ready++
			}
			if g.FloatingIP && s.Status == "ACTIVE" && vmset.Spec.FloatingIPPool != "" {
				fip, err := openstack.EnsureFloatingIP(ctx, clients, s, vmset.Spec.FloatingIPPool, vmsetKey)
				if err != nil {
					log.Error(err, "floating IP allocation deferred", "server", s.Name)
				} else {
					s.FloatingIP = fip
				}
			}
			hosts = append(hosts, openstackv1alpha1.HostStatus{
				Name:       s.Name,
				ID:         s.ID,
				InternalIP: s.InternalIP,
				FloatingIP: s.FloatingIP,
				Status:     s.Status,
			})
		}
		if ready < g.Count {
			allReady = false
		}
		groupStatuses = append(groupStatuses, openstackv1alpha1.GroupStatus{
			Name:  g.Name,
			Ready: ready,
			Hosts: hosts,
		})
	}

	patch := client.MergeFrom(vmset.DeepCopy())
	vmset.Status.Groups = groupStatuses
	vmset.Status.KeypairName = keypairName
	vmset.Status.SSHSecretRef = &corev1.LocalObjectReference{Name: sshSecretName}
	vmset.Status.ObservedGeneration = vmset.Generation
	if allReady {
		vmset.Status.Phase = openstackv1alpha1.PhaseReady
		apimeta.SetStatusCondition(&vmset.Status.Conditions, metav1.Condition{
			Type:    openstackv1alpha1.ConditionReady,
			Status:  metav1.ConditionTrue,
			Reason:  "AllServersActive",
			Message: "every requested server is ACTIVE",
		})
		apimeta.RemoveStatusCondition(&vmset.Status.Conditions, openstackv1alpha1.ConditionError)
	} else {
		vmset.Status.Phase = openstackv1alpha1.PhaseProvisioning
		apimeta.SetStatusCondition(&vmset.Status.Conditions, metav1.Condition{
			Type:    openstackv1alpha1.ConditionReady,
			Status:  metav1.ConditionFalse,
			Reason:  "ServersNotReady",
			Message: "waiting for servers to reach ACTIVE",
		})
	}
	if err := r.Status().Patch(ctx, &vmset, patch); err != nil {
		return ctrl.Result{}, err
	}

	if allReady {
		return ctrl.Result{RequeueAfter: requeueReady}, nil
	}
	return ctrl.Result{RequeueAfter: requeueWorking}, nil
}

func (r *OpenStackVMSetReconciler) reconcileDelete(ctx context.Context, vmset *openstackv1alpha1.OpenStackVMSet) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(vmset, openstackv1alpha1.FinalizerName) {
		return ctrl.Result{}, nil
	}

	clients, err := r.openstackClients(ctx, vmset)
	if err != nil {
		log.Error(err, "openstack auth during delete; retrying")
		return ctrl.Result{RequeueAfter: requeueWorking}, nil
	}

	vmsetKey := openstack.VMSetKey(vmset.Namespace, vmset.Name)
	owned, err := openstack.ListByVMSet(ctx, clients.Compute, vmsetKey)
	if err != nil {
		log.Error(err, "list servers during delete; retrying")
		return ctrl.Result{RequeueAfter: requeueWorking}, nil
	}

	for i := range owned {
		s := owned[i]
		if err := openstack.ReleaseFloatingIPsForServer(ctx, clients, &s); err != nil {
			log.Error(err, "release fips during delete; retrying", "server", s.Name)
			return ctrl.Result{RequeueAfter: requeueWorking}, nil
		}
		if err := openstack.DeleteServer(ctx, clients.Compute, s.ID); err != nil {
			log.Error(err, "delete server during delete; retrying", "server", s.Name)
			return ctrl.Result{RequeueAfter: requeueWorking}, nil
		}
	}

	if err := openstack.DeleteKeypair(ctx, clients.Compute, vmset.Name); err != nil {
		log.Error(err, "delete keypair during delete; retrying")
		return ctrl.Result{RequeueAfter: requeueWorking}, nil
	}

	patch := client.MergeFrom(vmset.DeepCopy())
	controllerutil.RemoveFinalizer(vmset, openstackv1alpha1.FinalizerName)
	if err := r.Patch(ctx, vmset, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *OpenStackVMSetReconciler) openstackClients(ctx context.Context, vmset *openstackv1alpha1.OpenStackVMSet) (*openstack.Clients, error) {
	var creds corev1.Secret
	key := types.NamespacedName{Name: vmset.Spec.CredentialsRef.Name, Namespace: vmset.Namespace}
	if err := r.Get(ctx, key, &creds); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("credentials Secret %s not found", key.String())
		}
		return nil, fmt.Errorf("get credentials Secret %s: %w", key.String(), err)
	}
	return openstack.NewClientsFromSecret(ctx, &creds)
}

// ensureSSHKeypair generates an ed25519 keypair on first reconcile and
// stores it in a Secret owned by the CR; subsequent calls return the
// existing values.
func (r *OpenStackVMSetReconciler) ensureSSHKeypair(ctx context.Context, vmset *openstackv1alpha1.OpenStackVMSet) (string, string, error) {
	name := vmset.Name + sshSecretSuffix
	key := types.NamespacedName{Name: name, Namespace: vmset.Namespace}

	var existing corev1.Secret
	err := r.Get(ctx, key, &existing)
	if err == nil {
		pub := existing.Data[openstackv1alpha1.SSHSecretPublicKeyKey]
		if len(pub) == 0 {
			return "", "", fmt.Errorf("Secret %s missing %q", key.String(), openstackv1alpha1.SSHSecretPublicKeyKey)
		}
		return name, string(pub), nil
	}
	if !apierrors.IsNotFound(err) {
		return "", "", fmt.Errorf("get Secret %s: %w", key.String(), err)
	}

	priv, pub, err := openstack.GenerateEd25519Keypair(vmset.Namespace + "/" + vmset.Name)
	if err != nil {
		return "", "", err
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: vmset.Namespace,
			Labels: map[string]string{
				"osvms.apeirora.eu/vmset": vmset.Name,
			},
		},
		Type: corev1.SecretTypeSSHAuth,
		Data: map[string][]byte{
			openstackv1alpha1.SSHSecretPrivateKeyKey: priv,
			openstackv1alpha1.SSHSecretPublicKeyKey:  pub,
		},
	}
	if err := controllerutil.SetControllerReference(vmset, secret, r.Scheme); err != nil {
		return "", "", fmt.Errorf("set owner ref on SSH Secret: %w", err)
	}
	if err := r.Create(ctx, secret); err != nil {
		return "", "", fmt.Errorf("create SSH Secret %s: %w", key.String(), err)
	}
	return name, string(pub), nil
}

func (r *OpenStackVMSetReconciler) setPhase(ctx context.Context, vmset *openstackv1alpha1.OpenStackVMSet, phase openstackv1alpha1.Phase, reason, msg string) error {
	if vmset.Status.Phase == phase {
		return nil
	}
	patch := client.MergeFrom(vmset.DeepCopy())
	vmset.Status.Phase = phase
	apimeta.SetStatusCondition(&vmset.Status.Conditions, metav1.Condition{
		Type:    openstackv1alpha1.ConditionReady,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: msg,
	})
	return r.Status().Patch(ctx, vmset, patch)
}

func (r *OpenStackVMSetReconciler) fail(ctx context.Context, vmset *openstackv1alpha1.OpenStackVMSet, reason, msg string) (ctrl.Result, error) {
	patch := client.MergeFrom(vmset.DeepCopy())
	vmset.Status.Phase = openstackv1alpha1.PhaseFailed
	apimeta.SetStatusCondition(&vmset.Status.Conditions, metav1.Condition{
		Type:    openstackv1alpha1.ConditionError,
		Status:  metav1.ConditionTrue,
		Reason:  reason,
		Message: msg,
	})
	if err := r.Status().Patch(ctx, vmset, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueWorking}, nil
}

func indexByGroup(servers []openstack.Server) map[string][]openstack.Server {
	out := make(map[string][]openstack.Server)
	for _, s := range servers {
		out[s.Group] = append(out[s.Group], s)
	}
	return out
}

func pickGroup(servers []openstack.Server, group string) []openstack.Server {
	out := make([]openstack.Server, 0)
	for _, s := range servers {
		if s.Group == group {
			out = append(out, s)
		}
	}
	return out
}

func (r *OpenStackVMSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&openstackv1alpha1.OpenStackVMSet{}).
		Named("openstackvmset").
		Complete(r)
}
