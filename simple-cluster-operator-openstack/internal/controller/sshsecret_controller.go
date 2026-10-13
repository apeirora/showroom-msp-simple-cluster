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
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-openstack/internal/secretpolicy"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	openstackv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-openstack/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-openstack/internal/openstack"
)

// SSHSecretReconciler watches Secrets carrying the keypair-cleanup
// finalizer. When such a Secret is deleted the reconciler reads the
// credentials- and keypair-name annotations the OpenStackVM controller
// wrote, deletes the Nova keypair, and removes the finalizer so K8s GC
// completes. Per-VM Secrets reach this path via the OpenStackVM's
// ownerRef cascade; shared Secrets reach it when an admin deletes them.
type SSHSecretReconciler struct {
	client.Client
	SecretPolicy secretpolicy.Policy
	Scheme       *runtime.Scheme
}

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;update;patch

func (r *SSHSecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var secret corev1.Secret
	if err := r.Get(ctx, req.NamespacedName, &secret); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Only act when the Secret carries our finalizer AND is being deleted.
	if !controllerutil.ContainsFinalizer(&secret, openstackv1alpha1.KeypairFinalizerName) {
		return ctrl.Result{}, nil
	}
	if secret.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	credName := secret.Annotations[openstackv1alpha1.SSHSecretCredentialsNameAnno]
	credNs := secret.Annotations[openstackv1alpha1.SSHSecretCredentialsNamespaceAnno]
	keypairName := secret.Annotations[openstackv1alpha1.SSHSecretKeypairNameAnno]

	if credName == "" || credNs == "" || keypairName == "" {
		log.Info("SSH Secret terminating without cleanup metadata; dropping finalizer",
			"secret", req.NamespacedName, "credName", credName, "credNs", credNs, "keypairName", keypairName)
		return r.removeFinalizer(ctx, &secret)
	}

	var creds corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Name: credName, Namespace: credNs}, &creds); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("credentials Secret no longer exists; dropping finalizer without Nova cleanup",
				"secret", req.NamespacedName, "creds", credName)
			return r.removeFinalizer(ctx, &secret)
		}
		return ctrl.Result{}, fmt.Errorf("get credentials Secret %s/%s: %w", credNs, credName, err)
	}

	clients, err := openstack.NewClientsFromSecret(ctx, &creds)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("openstack auth: %w", err)
	}

	if err := openstack.DeleteKeypair(ctx, clients.Compute, keypairName); err != nil {
		return ctrl.Result{}, fmt.Errorf("delete Nova keypair %q: %w", keypairName, err)
	}
	log.Info("Nova keypair cleaned up; dropping finalizer",
		"secret", req.NamespacedName, "keypair", keypairName)

	return r.removeFinalizer(ctx, &secret)
}

func (r *SSHSecretReconciler) removeFinalizer(ctx context.Context, secret *corev1.Secret) (ctrl.Result, error) {
	patch := client.MergeFrom(secret.DeepCopy())
	controllerutil.RemoveFinalizer(secret, openstackv1alpha1.KeypairFinalizerName)
	if err := r.Patch(ctx, secret, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// hasOurFinalizer is a predicate that filters Secret events down to the
// ones we care about, so we don't spin up reconciles for every Secret in
// the cluster.
func hasOurFinalizer(obj client.Object) bool {
	for _, f := range obj.GetFinalizers() {
		if f == openstackv1alpha1.KeypairFinalizerName {
			return true
		}
	}
	return false
}

func (r *SSHSecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc:  func(e event.CreateEvent) bool { return hasOurFinalizer(e.Object) },
			UpdateFunc:  func(e event.UpdateEvent) bool { return hasOurFinalizer(e.ObjectNew) },
			DeleteFunc:  func(e event.DeleteEvent) bool { return false },
			GenericFunc: func(e event.GenericEvent) bool { return hasOurFinalizer(e.Object) },
		})).
		Named("openstackvm-ssh-secret").
		Complete(r)
}
