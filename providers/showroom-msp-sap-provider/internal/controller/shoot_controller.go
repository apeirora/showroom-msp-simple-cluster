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
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
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

	providerRequeue     = 5 * time.Minute
	provisioningRequeue = 30 * time.Second

	// kubeconfigKey is the Secret data key holding the admin kubeconfig,
	// matching the simple-cluster convention.
	kubeconfigKey = "kubeconfig"

	phasePending      = "Pending"
	phaseProvisioning = "Provisioning"
	phaseReady        = "Ready"
	phaseFailed       = "Failed"
	phaseDeleting     = "Deleting"
)

// FlavorShoot is the common surface both SAP Integration provider CRs
// (OpenStackShoot, IronCoreShoot) present to the generic reconciler.
type FlavorShoot interface {
	client.Object
	GetSpecText() string
	GetShootStatus() *sapv1alpha1.ShootStatus
}

// ShootReconciler manages one flavor's provider CR as a standalone cluster
// service: each CR provisions a real Gardener Shoot on the provider's Gardener
// (spec is the Shoot manifest; when empty the flavor's embedded default
// template is used), mirrors the Shoot's lifecycle into status and maintains
// an admin-kubeconfig Secret next to the CR. The flavor's SCPC is still
// created by the NamespaceReconciler on bind and is reported via
// status.providerConfigs, but the CR no longer influences it.
type ShootReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// SCPCName is the SimpleClusterProviderConfig this flavor materialises.
	SCPCName string
	// KindName is the flavor CR Kind, part of the Gardener Shoot name hash.
	KindName string
	// Manifest is the flavor's embedded SCPC skeleton; the Shoot defaults
	// (garden namespace, template, input defaults) are parsed from it.
	Manifest []byte
	// New returns a fresh empty CR of the flavor's Kind.
	New func() FlavorShoot
	// ControllerName names the controller (and its For() resource).
	ControllerName string

	// KubeconfigSource and the secret coordinates locate the Gardener admin
	// kubeconfig, exactly as for the NamespaceReconciler.
	KubeconfigSource                KubeconfigSource
	GlobalKubeconfigSecretName      string
	GlobalKubeconfigSecretNamespace string
	ClientKubeconfigSecretName      string

	// NewGardener builds the Gardener access; tests inject a fake.
	NewGardener GardenerFactory

	defaults *flavorDefaults
}

// +kubebuilder:rbac:groups=sap.gardener,resources=openstackshoots;ironcoreshoots,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sap.gardener,resources=openstackshoots/status;ironcoreshoots/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sap.gardener,resources=openstackshoots/finalizers;ironcoreshoots/finalizers,verbs=update
// +kubebuilder:rbac:groups=showroom.apeirora.eu,resources=simpleclusterproviderconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=showroom.apeirora.eu,resources=simpleclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

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

	g, err := r.gardener(ctx, sh.GetNamespace())
	if err != nil {
		return ctrl.Result{RequeueAfter: providerRequeue},
			r.patchStatus(ctx, sh, scpcPresent, phasePending, "GardenerUnavailable", err.Error())
	}

	shootName := gardenShootName(r.KindName, sh.GetNamespace(), sh.GetName())
	live, err := g.GetShoot(ctx, shootName)
	if apierrors.IsNotFound(err) {
		if err := r.createShoot(ctx, g, sh, shootName); err != nil {
			return ctrl.Result{RequeueAfter: providerRequeue},
				r.patchStatus(ctx, sh, scpcPresent, phaseFailed, "ShootCreateFailed", err.Error())
		}
		log.Info("Gardener Shoot created", "shoot", shootName, "namespace", sh.GetNamespace(), "name", sh.GetName())
		return ctrl.Result{RequeueAfter: provisioningRequeue},
			r.patchStatus(ctx, sh, scpcPresent, phaseProvisioning, "ShootCreated",
				fmt.Sprintf("Gardener Shoot %q created", shootName))
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	phase, message := shootPhase(live)
	if phase == phaseReady {
		if err := r.ensureKubeconfig(ctx, g, sh, shootName); err != nil {
			return ctrl.Result{RequeueAfter: provisioningRequeue},
				r.patchStatus(ctx, sh, scpcPresent, phaseReady, "KubeconfigFailed", err.Error())
		}
		return ctrl.Result{RequeueAfter: providerRequeue},
			r.patchStatus(ctx, sh, scpcPresent, phaseReady, "ShootReady", message)
	}

	reason := "ShootProvisioning"
	if phase == phaseFailed {
		reason = "ShootFailed"
	}
	return ctrl.Result{RequeueAfter: provisioningRequeue},
		r.patchStatus(ctx, sh, scpcPresent, phase, reason, message)
}

// createShoot renders the manifest (spec text, or the flavor default when
// empty) and creates the Shoot on the Gardener.
func (r *ShootReconciler) createShoot(ctx context.Context, g Gardener, sh FlavorShoot, shootName string) error {
	text := sh.GetSpecText()
	if strings.TrimSpace(text) == "" {
		text = r.defaults.template
	}
	shoot, err := renderShootManifest(text, r.defaults, sh.GetNamespace(), sh.GetName())
	if err != nil {
		return err
	}
	shoot.SetName(shootName)
	shoot.SetNamespace(r.defaults.targetNamespace)
	annotations := shoot.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[shootOwnerAnnotation] = sh.GetNamespace() + "/" + sh.GetName()
	shoot.SetAnnotations(annotations)
	return g.CreateShoot(ctx, shoot)
}

// shootPhase maps the Gardener Shoot lastOperation to a CR phase, mirroring
// the SCPC readiness contract (Succeeded -> Ready, Failed/Error -> Failed).
func shootPhase(shoot *unstructured.Unstructured) (string, string) {
	state, _, _ := unstructured.NestedString(shoot.Object, "status", "lastOperation", "state")
	description, _, _ := unstructured.NestedString(shoot.Object, "status", "lastOperation", "description")
	switch state {
	case "":
		return phasePending, "Gardener has not started reconciling the Shoot yet"
	case "Succeeded":
		return phaseReady, "Gardener Shoot is ready"
	case "Failed", "Error":
		if lastErrors, _, _ := unstructured.NestedSlice(shoot.Object, "status", "lastErrors"); len(lastErrors) > 0 {
			if m, ok := lastErrors[0].(map[string]interface{}); ok {
				if d, ok := m["description"].(string); ok && d != "" {
					description = d
				}
			}
		}
		return phaseFailed, description
	default:
		return phaseProvisioning, description
	}
}

// ensureKubeconfig mints (and renews ahead of expiry) the admin kubeconfig for
// the Shoot and stores it in a Secret next to the CR, recorded in status.
func (r *ShootReconciler) ensureKubeconfig(ctx context.Context, g Gardener, sh FlavorShoot, shootName string) error {
	st := sh.GetShootStatus()
	secretName := kubeconfigSecretName(sh.GetName(), r.KindName)

	current := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Namespace: sh.GetNamespace(), Name: secretName}, current)
	notFound := apierrors.IsNotFound(err)
	if err != nil && !notFound {
		return err
	}
	fresh := notFound ||
		st.KubeconfigExpires == nil ||
		time.Now().Add(kubeconfigRenewalBefore).After(st.KubeconfigExpires.Time)
	if !fresh {
		return nil
	}

	kubeconfig, expires, err := g.AdminKubeconfig(ctx, shootName, kubeconfigValidity)
	if err != nil {
		return err
	}

	if notFound {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: sh.GetNamespace(),
				Labels:    map[string]string{"apeirora.eu/kubeconfig": "true"},
			},
			Data: map[string][]byte{kubeconfigKey: kubeconfig},
		}
		if err := controllerutil.SetControllerReference(sh, secret, r.Scheme); err != nil {
			return err
		}
		if err := r.Create(ctx, secret); err != nil {
			return err
		}
	} else {
		current.Data = map[string][]byte{kubeconfigKey: kubeconfig}
		if err := controllerutil.SetControllerReference(sh, current, r.Scheme); err != nil {
			return err
		}
		if err := r.Update(ctx, current); err != nil {
			return err
		}
	}

	st.KubeconfigSecretRef = &sapv1alpha1.KubeconfigSecretRef{Name: secretName}
	st.KubeconfigExpires = &metav1.Time{Time: expires}
	return nil
}

// kubeconfigSecretName is the flavor-qualified Secret name next to the CR.
func kubeconfigSecretName(name, kind string) string {
	return name + "-" + strings.ToLower(kind) + "-kubeconfig"
}

func (r *ShootReconciler) reconcileDelete(ctx context.Context, sh FlavorShoot) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(sh, shootFinalizer) {
		return ctrl.Result{}, nil
	}

	g, err := r.gardener(ctx, sh.GetNamespace())
	if err != nil {
		return ctrl.Result{RequeueAfter: providerRequeue},
			r.patchDeleteStatus(ctx, sh, "GardenerUnavailable", err.Error())
	}

	shootName := gardenShootName(r.KindName, sh.GetNamespace(), sh.GetName())
	gone, err := g.DeleteShoot(ctx, shootName)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !gone {
		return ctrl.Result{RequeueAfter: provisioningRequeue},
			r.patchDeleteStatus(ctx, sh, "ShootDeleting",
				fmt.Sprintf("waiting for Gardener Shoot %q to be deleted", shootName))
	}

	controllerutil.RemoveFinalizer(sh, shootFinalizer)
	return ctrl.Result{}, r.Update(ctx, sh)
}

// gardener loads the Gardener kubeconfig Secret and builds the client.
func (r *ShootReconciler) gardener(ctx context.Context, namespace string) (Gardener, error) {
	ref, err := resolveKubeconfigSecretRef(r.KubeconfigSource, namespace,
		r.GlobalKubeconfigSecretName, r.GlobalKubeconfigSecretNamespace, r.ClientKubeconfigSecretName)
	if err != nil {
		return nil, err
	}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, secret); err != nil {
		return nil, fmt.Errorf("gardener kubeconfig Secret %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	kubeconfig, ok := secret.Data[kubeconfigKey]
	if !ok || len(kubeconfig) == 0 {
		return nil, fmt.Errorf("gardener kubeconfig Secret %s/%s has no %q key", ref.Namespace, ref.Name, kubeconfigKey)
	}
	return r.NewGardener(kubeconfig, r.defaults.targetNamespace)
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

func (r *ShootReconciler) patchStatus(ctx context.Context, sh FlavorShoot, scpcPresent bool, phase, reason, message string) error {
	st := sh.GetShootStatus()
	st.ObservedGeneration = sh.GetGeneration()
	st.Phase = phase
	st.ShootName = gardenShootName(r.KindName, sh.GetNamespace(), sh.GetName())

	st.ProviderConfigs = []string{r.SCPCName}
	if !scpcPresent {
		st.ProviderConfigs = nil
	}

	status := metav1.ConditionFalse
	if phase == phaseReady {
		status = metav1.ConditionTrue
	}
	apimeta.SetStatusCondition(&st.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: sh.GetGeneration(),
	})

	return r.Status().Update(ctx, sh)
}

func (r *ShootReconciler) patchDeleteStatus(ctx context.Context, sh FlavorShoot, reason, message string) error {
	st := sh.GetShootStatus()
	st.Phase = phaseDeleting
	apimeta.SetStatusCondition(&st.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: sh.GetGeneration(),
	})
	return r.Status().Update(ctx, sh)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ShootReconciler) SetupWithManager(mgr ctrl.Manager) error {
	defaults, err := parseFlavorDefaults(r.Manifest)
	if err != nil {
		return fmt.Errorf("parse flavor defaults for %s: %w", r.ControllerName, err)
	}
	r.defaults = defaults
	if r.NewGardener == nil {
		r.NewGardener = NewGardener
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(r.New()).
		Named(r.ControllerName).
		Complete(r)
}
