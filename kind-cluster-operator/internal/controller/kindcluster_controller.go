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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	kindcluster "sigs.k8s.io/kind/pkg/cluster"

	kindv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/kind-cluster-operator/api/v1alpha1"
)

const (
	requeueReady   = 2 * time.Minute
	requeueWorking = 20 * time.Second

	secretSuffix     = "-kubeconfig"
	secretDataKey    = "kubeconfig"
	clusterNameLabel = "kindcluster.apeirora.eu/sc-name"
)

// KindClusterReconciler stands real kind clusters up on the host docker
// daemon (DooD via /var/run/docker.sock mounted into this pod) for each
// KindCluster CR.
type KindClusterReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Provider *kindcluster.Provider
}

// +kubebuilder:rbac:groups=kind.apeirora.eu,resources=kindclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kind.apeirora.eu,resources=kindclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kind.apeirora.eu,resources=kindclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

// nolint:gocyclo // straight-line orchestration
func (r *KindClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var kc kindv1alpha1.KindCluster
	if err := r.Get(ctx, req.NamespacedName, &kc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !kc.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &kc)
	}

	if !controllerutil.ContainsFinalizer(&kc, kindv1alpha1.FinalizerName) {
		patch := client.MergeFrom(kc.DeepCopy())
		controllerutil.AddFinalizer(&kc, kindv1alpha1.FinalizerName)
		if err := r.Patch(ctx, &kc, patch); err != nil {
			return ctrl.Result{}, err
		}
	}

	clusterName := effectiveClusterName(&kc)
	existing, err := r.Provider.List()
	if err != nil {
		return r.fail(ctx, &kc, "DockerError", fmt.Sprintf("list kind clusters: %v", err))
	}

	if !contains(existing, clusterName) {
		log.Info("creating kind cluster on host docker", "name", clusterName, "workers", kc.Spec.Workers)
		if err := r.setPhase(ctx, &kc, kindv1alpha1.PhaseProvisioning, "Creating", fmt.Sprintf("kind create cluster --name %s", clusterName)); err != nil {
			return ctrl.Result{}, err
		}

		opts := []kindcluster.CreateOption{
			kindcluster.CreateWithRawConfig([]byte(renderKindConfig(kc.Spec.Workers))),
			kindcluster.CreateWithWaitForReady(0), // don't block reconcile on readiness
		}
		if kc.Spec.NodeImage != "" {
			opts = append(opts, kindcluster.CreateWithNodeImage(kc.Spec.NodeImage))
		}
		if err := r.Provider.Create(clusterName, opts...); err != nil {
			return r.fail(ctx, &kc, "CreateError", err.Error())
		}
		log.Info("kind cluster created", "name", clusterName)
	}

	// Fetch the kubeconfig and mirror it into a Secret in this namespace.
	// Use the internal form so docker-network-internal addresses
	// (kind-control-plane:6443) are reachable from other pods on the
	// same docker network.
	kubeconfig, err := r.Provider.KubeConfig(clusterName, true)
	if err != nil {
		return r.fail(ctx, &kc, "KubeconfigError", err.Error())
	}
	secretName, err := r.ensureSecret(ctx, &kc, []byte(kubeconfig))
	if err != nil {
		return r.fail(ctx, &kc, "SecretError", err.Error())
	}

	patch := client.MergeFrom(kc.DeepCopy())
	kc.Status.Phase = kindv1alpha1.PhaseReady
	kc.Status.ClusterName = clusterName
	kc.Status.KubeconfigSecretRef = &corev1.LocalObjectReference{Name: secretName}
	kc.Status.ObservedGeneration = kc.Generation
	apimeta.SetStatusCondition(&kc.Status.Conditions, metav1.Condition{
		Type:    kindv1alpha1.ConditionReady,
		Status:  metav1.ConditionTrue,
		Reason:  "Ready",
		Message: fmt.Sprintf("kind cluster %q is up; kubeconfig in Secret %s", clusterName, secretName),
	})
	apimeta.RemoveStatusCondition(&kc.Status.Conditions, kindv1alpha1.ConditionError)
	if err := r.Status().Patch(ctx, &kc, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueReady}, nil
}

func (r *KindClusterReconciler) reconcileDelete(ctx context.Context, kc *kindv1alpha1.KindCluster) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(kc, kindv1alpha1.FinalizerName) {
		return ctrl.Result{}, nil
	}

	clusterName := effectiveClusterName(kc)
	existing, err := r.Provider.List()
	if err != nil {
		log.Error(err, "list kind clusters during delete")
		return ctrl.Result{RequeueAfter: requeueWorking}, nil
	}
	if contains(existing, clusterName) {
		log.Info("deleting kind cluster from host docker", "name", clusterName)
		if err := r.Provider.Delete(clusterName, ""); err != nil {
			log.Error(err, "kind delete cluster failed; retrying")
			return ctrl.Result{RequeueAfter: requeueWorking}, nil
		}
	}

	patch := client.MergeFrom(kc.DeepCopy())
	controllerutil.RemoveFinalizer(kc, kindv1alpha1.FinalizerName)
	if err := r.Patch(ctx, kc, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *KindClusterReconciler) ensureSecret(ctx context.Context, kc *kindv1alpha1.KindCluster, kubeconfig []byte) (string, error) {
	name := kc.Name + secretSuffix
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: kc.Namespace,
			Labels:    map[string]string{clusterNameLabel: kc.Name},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{secretDataKey: kubeconfig},
	}
	if err := controllerutil.SetControllerReference(kc, desired, r.Scheme); err != nil {
		return "", fmt.Errorf("set owner ref: %w", err)
	}

	var existing corev1.Secret
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: kc.Namespace}, &existing)
	if apierrors.IsNotFound(err) {
		if cerr := r.Create(ctx, desired); cerr != nil {
			return "", fmt.Errorf("create Secret %s/%s: %w", kc.Namespace, name, cerr)
		}
		return name, nil
	}
	if err != nil {
		return "", fmt.Errorf("get Secret %s/%s: %w", kc.Namespace, name, err)
	}
	if string(existing.Data[secretDataKey]) == string(kubeconfig) {
		return name, nil
	}
	existing.Data = desired.Data
	existing.Type = desired.Type
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	for k, v := range desired.Labels {
		existing.Labels[k] = v
	}
	if err := controllerutil.SetControllerReference(kc, &existing, r.Scheme); err != nil {
		return "", fmt.Errorf("set owner ref: %w", err)
	}
	if err := r.Update(ctx, &existing); err != nil {
		return "", fmt.Errorf("update Secret %s/%s: %w", kc.Namespace, name, err)
	}
	return name, nil
}

func (r *KindClusterReconciler) setPhase(ctx context.Context, kc *kindv1alpha1.KindCluster, phase kindv1alpha1.Phase, reason, msg string) error {
	if kc.Status.Phase == phase {
		return nil
	}
	patch := client.MergeFrom(kc.DeepCopy())
	kc.Status.Phase = phase
	apimeta.SetStatusCondition(&kc.Status.Conditions, metav1.Condition{
		Type:    kindv1alpha1.ConditionReady,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: msg,
	})
	return r.Status().Patch(ctx, kc, patch)
}

func (r *KindClusterReconciler) fail(ctx context.Context, kc *kindv1alpha1.KindCluster, reason, msg string) (ctrl.Result, error) {
	patch := client.MergeFrom(kc.DeepCopy())
	kc.Status.Phase = kindv1alpha1.PhaseFailed
	apimeta.SetStatusCondition(&kc.Status.Conditions, metav1.Condition{
		Type:    kindv1alpha1.ConditionError,
		Status:  metav1.ConditionTrue,
		Reason:  reason,
		Message: msg,
	})
	if err := r.Status().Patch(ctx, kc, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueWorking}, nil
}

func effectiveClusterName(kc *kindv1alpha1.KindCluster) string {
	if kc.Spec.ClusterName != "" {
		return kc.Spec.ClusterName
	}
	return kc.Name
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// renderKindConfig builds a multi-node kind cluster configuration:
// always 1 control-plane, plus N workers.
func renderKindConfig(workers int32) string {
	var b strings.Builder
	b.WriteString("kind: Cluster\n")
	b.WriteString("apiVersion: kind.x-k8s.io/v1alpha4\n")
	b.WriteString("nodes:\n")
	b.WriteString("  - role: control-plane\n")
	for i := int32(0); i < workers; i++ {
		b.WriteString("  - role: worker\n")
	}
	return b.String()
}

func (r *KindClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kindv1alpha1.KindCluster{}).
		Named("kindcluster").
		Complete(r)
}
