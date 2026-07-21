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
	_ "embed"
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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	dativ1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/providers/dati-provider/api/v1alpha1"
)

// scpcDatiManifest is the SCPC skeleton materialised for each dati.gardener
// Shoot. The controller rewrites metadata.namespace, target.kubeconfigSecretRef
// and — when the Shoot's text spec is non-empty — the Shoot resource template.
// The remaining body (inputs, readiness, kubeconfigSource) stays as authored;
// simple-cluster {{ .SC }}/{{ .Inputs }} expressions are opaque strings here.
//
//go:embed scpc-dati.yaml
var scpcDatiManifest []byte

const (
	datiShootFinalizer = "dati.gardener/provider"
	datiSCPCName       = "dati"

	conditionReady            = "Ready"
	conditionCredentialsValid = "CredentialsValid"

	providerRequeue = 5 * time.Minute
	inUseRequeue    = 30 * time.Second

	// kubeconfigSourceAnnotation lets an individual Shoot override the
	// operator-wide default kubeconfig source (see KubeconfigSource).
	kubeconfigSourceAnnotation = "dati.gardener/kubeconfig-source"
)

// KubeconfigSource selects where the Gardener kubeconfig that the SCPC targets
// comes from.
type KubeconfigSource string

const (
	// KubeconfigSourceGlobal: one admin-managed kubeconfig Secret shared by all
	// tenants, at a fixed name/namespace on the service cluster. Default.
	KubeconfigSourceGlobal KubeconfigSource = "global"
	// KubeconfigSourceKCP: each client brings its own kubeconfig from its KCP
	// workspace; the Secret is looked up (by a fixed name) in the Shoot's own
	// namespace on the service cluster, where the sync agent delivers it.
	KubeconfigSourceKCP KubeconfigSource = "kcp"
)

// ValidKubeconfigSource reports whether s is a recognised source.
func ValidKubeconfigSource(s KubeconfigSource) bool {
	return s == KubeconfigSourceGlobal || s == KubeconfigSourceKCP
}

var (
	scpcGVK = schema.GroupVersionKind{
		Group: "showroom.apeirora.eu", Version: "v1alpha1", Kind: "SimpleClusterProviderConfig",
	}
	scListGVK = schema.GroupVersionKind{
		Group: "showroom.apeirora.eu", Version: "v1alpha1", Kind: "SimpleClusterList",
	}
)

// ShootReconciler materialises a namespaced SimpleClusterProviderConfig for
// every dati.gardener Shoot synced into this cluster. The SCPC lands in the
// Shoot's namespace so SimpleClusters there resolve providerConfigRefs: [dati]
// without any cluster-scoped state.
type ShootReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// KubeconfigSource is the operator-wide default source for the Gardener
	// kubeconfig the SCPC targets. A Shoot may override it per-object via the
	// dati.gardener/kubeconfig-source annotation.
	KubeconfigSource KubeconfigSource
	// GlobalKubeconfigSecretName/Namespace locate the shared admin-managed
	// kubeconfig Secret used in "global" mode.
	GlobalKubeconfigSecretName      string
	GlobalKubeconfigSecretNamespace string
	// ClientKubeconfigSecretName is the Secret name looked up in the Shoot's
	// own namespace in "kcp" mode (the per-client kubeconfig from the workspace).
	ClientKubeconfigSecretName string
}

// kubeconfigSourceFor returns the effective source for a Shoot: its annotation
// override if valid, else the operator default.
func (r *ShootReconciler) kubeconfigSourceFor(sh *dativ1alpha1.Shoot) (KubeconfigSource, error) {
	src := r.KubeconfigSource
	if v := sh.Annotations[kubeconfigSourceAnnotation]; v != "" {
		src = KubeconfigSource(v)
	}
	if !ValidKubeconfigSource(src) {
		return "", fmt.Errorf("invalid kubeconfig source %q (want %q or %q)",
			src, KubeconfigSourceGlobal, KubeconfigSourceKCP)
	}
	return src, nil
}

// kubeconfigSecretRef resolves which Secret the SCPC target should reference,
// based on the effective kubeconfig source for the Shoot.
func (r *ShootReconciler) kubeconfigSecretRef(sh *dativ1alpha1.Shoot) (corev1.SecretReference, error) {
	src, err := r.kubeconfigSourceFor(sh)
	if err != nil {
		return corev1.SecretReference{}, err
	}
	switch src {
	case KubeconfigSourceKCP:
		// Per-client kubeconfig, delivered by the sync agent into the Shoot's
		// own namespace on the service cluster.
		return corev1.SecretReference{Name: r.ClientKubeconfigSecretName, Namespace: sh.Namespace}, nil
	default: // KubeconfigSourceGlobal
		return corev1.SecretReference{Name: r.GlobalKubeconfigSecretName, Namespace: r.GlobalKubeconfigSecretNamespace}, nil
	}
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

	secretRef, err := r.kubeconfigSecretRef(sh)
	if err != nil {
		return ctrl.Result{RequeueAfter: providerRequeue},
			r.patchStatus(ctx, sh, nil, false, secretRef, "InvalidKubeconfigSource", err.Error())
	}
	credsOK, err := r.kubeconfigSecretExists(ctx, secretRef)
	if err != nil {
		return ctrl.Result{}, err
	}

	scpc, err := r.renderSCPC(sh, secretRef)
	if err != nil {
		return ctrl.Result{RequeueAfter: providerRequeue},
			r.patchStatus(ctx, sh, nil, credsOK, secretRef, "InvalidTemplate", err.Error())
	}
	if err := r.applySCPC(ctx, sh, scpc); err != nil {
		if apimeta.IsNoMatchError(err) {
			// simple-cluster is not installed on this cluster.
			return ctrl.Result{RequeueAfter: providerRequeue},
				r.patchStatus(ctx, sh, nil, credsOK, secretRef, "SCPCCRDMissing",
					"SimpleClusterProviderConfig CRD not installed; is simple-cluster deployed?")
		}
		return ctrl.Result{}, err
	}
	log.V(1).Info("SCPC ensured", "namespace", sh.Namespace, "name", datiSCPCName)

	// Periodic requeue re-creates the SCPC if it is removed out-of-band
	// (no Owns() watch: the SCPC CRD may legitimately be absent).
	return ctrl.Result{RequeueAfter: providerRequeue},
		r.patchStatus(ctx, sh, []string{datiSCPCName}, credsOK, secretRef, "", "")
}

func (r *ShootReconciler) reconcileDelete(ctx context.Context, sh *dativ1alpha1.Shoot) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(sh, datiShootFinalizer) {
		return ctrl.Result{}, nil
	}

	users, err := r.simpleClustersUsingSCPC(ctx, sh.Namespace)
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

	// The SCPC carries an ownerReference to this Shoot and is
	// garbage-collected once the finalizer is gone.
	controllerutil.RemoveFinalizer(sh, datiShootFinalizer)
	return ctrl.Result{}, r.Update(ctx, sh)
}

func (r *ShootReconciler) kubeconfigSecretExists(ctx context.Context, ref corev1.SecretReference) (bool, error) {
	if ref.Name == "" {
		return false, nil
	}
	var secret corev1.Secret
	err := r.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, &secret)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

func (r *ShootReconciler) renderSCPC(sh *dativ1alpha1.Shoot, ref corev1.SecretReference) (*unstructured.Unstructured, error) {
	var obj map[string]interface{}
	if err := yaml.Unmarshal(scpcDatiManifest, &obj); err != nil {
		return nil, fmt.Errorf("unmarshal embedded SCPC manifest: %w", err)
	}
	scpc := &unstructured.Unstructured{Object: obj}
	scpc.SetName(datiSCPCName)
	scpc.SetNamespace(sh.Namespace)
	if err := unstructured.SetNestedMap(scpc.Object, map[string]interface{}{
		"name":      ref.Name,
		"namespace": ref.Namespace,
	}, "spec", "target", "kubeconfigSecretRef"); err != nil {
		return nil, err
	}

	if strings.TrimSpace(sh.Spec) != "" {
		if err := setShootTemplate(scpc, sh.Spec); err != nil {
			return nil, err
		}
	}
	return scpc, nil
}

// setShootTemplate replaces the template of the "shoot" resource in the SCPC
// skeleton with the manifest text carried by the dati.gardener Shoot spec.
func setShootTemplate(scpc *unstructured.Unstructured, template string) error {
	resources, found, err := unstructured.NestedSlice(scpc.Object, "spec", "resources")
	if err != nil || !found || len(resources) == 0 {
		return fmt.Errorf("embedded SCPC manifest has no spec.resources (err: %v)", err)
	}
	first, ok := resources[0].(map[string]interface{})
	if !ok || first["name"] != "shoot" {
		return fmt.Errorf("embedded SCPC manifest: first resource is not %q", "shoot")
	}
	first["template"] = template
	resources[0] = first
	return unstructured.SetNestedSlice(scpc.Object, resources, "spec", "resources")
}

func (r *ShootReconciler) applySCPC(ctx context.Context, sh *dativ1alpha1.Shoot, desired *unstructured.Unstructured) error {
	if err := controllerutil.SetControllerReference(sh, desired, r.Scheme); err != nil {
		return err
	}

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(scpcGVK)
	err := r.Get(ctx, types.NamespacedName{Namespace: desired.GetNamespace(), Name: desired.GetName()}, existing)
	if apierrors.IsNotFound(err) || apimeta.IsNoMatchError(err) {
		if apimeta.IsNoMatchError(err) {
			return err
		}
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

// simpleClustersUsingSCPC returns names of SimpleClusters in the namespace
// that reference the dati SCPC via spec.providerConfigRefs or
// status.providerConfigRef.
func (r *ShootReconciler) simpleClustersUsingSCPC(ctx context.Context, namespace string) ([]string, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(scListGVK)
	if err := r.List(ctx, list, client.InNamespace(namespace)); err != nil {
		if apimeta.IsNoMatchError(err) {
			return nil, nil
		}
		return nil, err
	}

	var users []string
	for i := range list.Items {
		sc := &list.Items[i]
		refs, _, _ := unstructured.NestedStringSlice(sc.Object, "spec", "providerConfigRefs")
		statusRef, _, _ := unstructured.NestedString(sc.Object, "status", "providerConfigRef", "name")
		for _, ref := range refs {
			if ref == datiSCPCName {
				users = append(users, sc.GetName())
				break
			}
		}
		if statusRef == datiSCPCName && (len(users) == 0 || users[len(users)-1] != sc.GetName()) {
			users = append(users, sc.GetName())
		}
	}
	return users, nil
}

func (r *ShootReconciler) patchStatus(
	ctx context.Context, sh *dativ1alpha1.Shoot,
	providerConfigs []string, credsOK bool, ref corev1.SecretReference,
	notReadyReason, notReadyMessage string,
) error {
	sh.Status.ProviderConfigs = providerConfigs
	sh.Status.ObservedGeneration = sh.Generation

	creds := metav1.Condition{
		Type:               conditionCredentialsValid,
		Status:             metav1.ConditionTrue,
		Reason:             "SecretFound",
		Message:            fmt.Sprintf("kubeconfig Secret %s/%s present", ref.Namespace, ref.Name),
		ObservedGeneration: sh.Generation,
	}
	if !credsOK {
		creds.Status = metav1.ConditionFalse
		creds.Reason = "SecretMissing"
		creds.Message = fmt.Sprintf("kubeconfig Secret %s/%s not found", ref.Namespace, ref.Name)
	}
	apimeta.SetStatusCondition(&sh.Status.Conditions, creds)

	ready := metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "SCPCReady",
		Message:            fmt.Sprintf("SimpleClusterProviderConfig %q available in namespace %s", datiSCPCName, sh.Namespace),
		ObservedGeneration: sh.Generation,
	}
	switch {
	case notReadyReason != "":
		ready.Status = metav1.ConditionFalse
		ready.Reason = notReadyReason
		ready.Message = notReadyMessage
	case !credsOK:
		ready.Status = metav1.ConditionFalse
		ready.Reason = "CredentialsMissing"
		ready.Message = creds.Message
	}
	apimeta.SetStatusCondition(&sh.Status.Conditions, ready)

	return r.Status().Update(ctx, sh)
}

// SetupWithManager sets up the controller with the Manager. No Owns() watch
// on the SCPC: its CRD is absent on clusters without simple-cluster and a
// missing-informer would prevent the whole manager from starting. Drift is
// healed by the periodic requeue instead.
func (r *ShootReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dativ1alpha1.Shoot{}).
		Named("shoot").
		Complete(r)
}
