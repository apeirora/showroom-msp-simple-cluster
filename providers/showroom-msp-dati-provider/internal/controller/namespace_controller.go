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
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// bindExportsAnnotation is written by the bind-watcher-operator onto each
	// MSP account namespace: a comma-separated list of the KCP APIExport names
	// bound to that account.
	bindExportsAnnotation = "bind.simple-cluster.apeirora.eu/exports"

	// DefaultSimpleClusterExportName / DefaultDatiExportName are the APIExport
	// names whose joint presence in the annotation signals that the dati SCPC
	// must exist in the account namespace.
	DefaultSimpleClusterExportName = "simpleclusters.showroom.apeirora.eu"
	DefaultDatiExportName          = "shoots.dati.gardener"
)

// NamespaceReconciler is the sole creator of the dati
// SimpleClusterProviderConfig. It watches account namespaces and, whenever the
// bind-watcher annotation shows that both SimpleCluster and dati are bound,
// materialises the SCPC in that namespace (owned by the namespace so it is
// garbage-collected on unbind by namespace deletion). When the dati binding is
// gone it removes the SCPC it manages.
type NamespaceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// KubeconfigSource is the source for the Gardener kubeconfig the SCPC
	// targets: "global" (shared admin Secret) or "kcp" (per-account Secret in
	// the account namespace).
	KubeconfigSource KubeconfigSource
	// GlobalKubeconfigSecretName/Namespace locate the shared admin-managed
	// kubeconfig Secret used in "global" mode.
	GlobalKubeconfigSecretName      string
	GlobalKubeconfigSecretNamespace string
	// ClientKubeconfigSecretName is the Secret name looked up in the account's
	// own namespace in "kcp" mode.
	ClientKubeconfigSecretName string
	// SimpleClusterExportName / DatiExportName are the APIExport names whose
	// joint presence in the bind annotation triggers SCPC creation.
	SimpleClusterExportName string
	DatiExportName          string
}

// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces/finalizers,verbs=update

func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	ns := &corev1.Namespace{}
	if err := r.Get(ctx, req.NamespacedName, ns); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !ns.DeletionTimestamp.IsZero() {
		// The SCPC is owned by the namespace and garbage-collected with it.
		return ctrl.Result{}, nil
	}

	exports := parseBindExports(ns.Annotations[bindExportsAnnotation])
	bound := slices.Contains(exports, r.SimpleClusterExportName) && slices.Contains(exports, r.DatiExportName)
	if !bound {
		return ctrl.Result{}, r.ensureSCPCAbsent(ctx, ns.Name)
	}

	ref, err := resolveKubeconfigSecretRef(r.KubeconfigSource, ns.Name,
		r.GlobalKubeconfigSecretName, r.GlobalKubeconfigSecretNamespace, r.ClientKubeconfigSecretName)
	if err != nil {
		return ctrl.Result{}, err
	}

	scpc, err := renderDatiSCPC(ns.Name, ref)
	if err != nil {
		return ctrl.Result{RequeueAfter: providerRequeue}, err
	}
	if err := r.applySCPC(ctx, ns, scpc); err != nil {
		if apimeta.IsNoMatchError(err) {
			// simple-cluster is not installed on this cluster yet.
			log.Info("SimpleClusterProviderConfig CRD not installed; retrying",
				"namespace", ns.Name)
			return ctrl.Result{RequeueAfter: providerRequeue}, nil
		}
		return ctrl.Result{}, err
	}
	log.V(1).Info("SCPC ensured", "namespace", ns.Name, "name", datiSCPCName)

	// Periodic requeue re-creates the SCPC if it is removed out-of-band
	// (no Owns() watch: the SCPC CRD may legitimately be absent).
	return ctrl.Result{RequeueAfter: providerRequeue}, nil
}

// parseBindExports splits the comma-separated bind annotation into a trimmed,
// non-empty list of APIExport names.
func parseBindExports(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func (r *NamespaceReconciler) applySCPC(ctx context.Context, ns *corev1.Namespace, desired *unstructured.Unstructured) error {
	// A cluster-scoped Namespace owning a namespaced SCPC is the allowed
	// direction; the SCPC is garbage-collected when the namespace is deleted.
	if err := controllerutil.SetControllerReference(ns, desired, r.Scheme); err != nil {
		return err
	}

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(scpcGVK)
	err := r.Get(ctx, types.NamespacedName{Namespace: desired.GetNamespace(), Name: desired.GetName()}, existing)
	if apimeta.IsNoMatchError(err) {
		return err
	}
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	existing.Object["spec"] = desired.Object["spec"]
	existing.SetLabels(desired.GetLabels())
	existing.SetOwnerReferences(desired.GetOwnerReferences())
	return r.Update(ctx, existing)
}

// ensureSCPCAbsent deletes the dati SCPC in the namespace when it exists and is
// one we manage (stamped with the managed-by label).
func (r *NamespaceReconciler) ensureSCPCAbsent(ctx context.Context, namespace string) error {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(scpcGVK)
	err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: datiSCPCName}, existing)
	if apierrors.IsNotFound(err) || apimeta.IsNoMatchError(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if existing.GetLabels()[datiManagedByLabelKey] != datiManagedByNamespaceBind {
		return nil
	}
	return client.IgnoreNotFound(r.Delete(ctx, existing))
}

// SetupWithManager wires the controller to watch Namespaces (the install
// signal). No Owns() watch on the SCPC: its CRD is absent on clusters without
// simple-cluster and a missing informer would prevent the manager from
// starting. Drift is healed by the periodic requeue instead.
func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Named("namespace").
		Complete(r)
}
