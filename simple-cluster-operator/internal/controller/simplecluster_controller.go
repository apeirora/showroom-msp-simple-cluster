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
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"
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
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"sigs.k8s.io/yaml"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/kubeconfig"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/pathwalk"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/target"
	rendertpl "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/template"
)

const (
	scRequeueReady   = 1 * time.Minute
	scRequeueWorking = 30 * time.Second

	// fieldManager owns server-side applies the operator makes onto
	// provisioned clusters for spec.resources.
	fieldManager = "simple-cluster-operator"

	// forceCleanupGrace is how long the operator waits after a target
	// resource enters Terminating before forcibly stripping its
	// finalizers. Only honoured when ResourceSpec.ForceCleanupOnCascade
	// is set.
	forceCleanupGrace = 30 * time.Second
)

var (
	errSCPCNotFound            = errors.New("provider config not found")
	errSCPCAmbiguous           = errors.New("provider config reference is ambiguous")
	errSCPCIdentityChanged     = errors.New("provider config identity changed")
	errSCPCReferenceInvalid    = errors.New("provider config reference is invalid")
	errTargetOwnershipConflict = errors.New("target ownership does not match")
)

// SimpleClusterReconciler walks the SCPC's DAG of ResourceSpecs and
// mirrors per-resource state into SC.Status.Resources. See DESIGN.md §5.
type SimpleClusterReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	RESTConfig *rest.Config
	// APIReader reads straight from the API server, bypassing the cache. It
	// backs provider identity resolution and the final pre-create re-read of
	// both the SimpleCluster and SCPC so replacement, relevant metadata or spec
	// changes, deletion, or provider retirement visible at the API server block
	// new infrastructure.
	APIReader client.Reader
	// FallbackSCPCNamespace is consulted when an SCPC is not found in the
	// SimpleCluster's own namespace. Transitional: it lets chart-managed
	// shared SCPCs keep working while providers migrate to creating
	// per-workspace SCPCs. Empty disables the fallback.
	FallbackSCPCNamespace string
}

// +kubebuilder:rbac:groups=showroom.apeirora.eu,resources=simpleclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=showroom.apeirora.eu,resources=simpleclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=showroom.apeirora.eu,resources=simpleclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=showroom.apeirora.eu,resources=simpleclusterproviderconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;create;update;patch;delete

// Templated target resources rendered from SCPC.spec.resources[].template
// may belong to any of the in-tree downstream operators. Grant CRUD across
// each known group so the controller can Get/Create/Patch/Delete the
// rendered objects (no narrower carve-out is possible because the SCPC
// chooses the GVK at runtime).
// +kubebuilder:rbac:groups=arsys.eu,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cloudferro.eu,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ubuntu.apeirora.eu,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ubuntumachines.apeirora.eu,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kind.apeirora.eu,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sshkeys.apeirora.eu,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.gardener.cloud,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=authentication.gardener.cloud,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sap.eu,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dati.eu,resources=*,verbs=get;list;watch;create;update;patch;delete

// nolint:gocyclo // straight-line orchestration; extracting fragments hurts readability more than it helps
func (r *SimpleClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var sc showroomv1alpha1.SimpleCluster
	if err := r.Get(ctx, req.NamespacedName, &sc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !sc.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &sc)
	}

	if !controllerutil.ContainsFinalizer(&sc, showroomv1alpha1.FinalizerName) {
		patch := client.MergeFrom(sc.DeepCopy())
		controllerutil.AddFinalizer(&sc, showroomv1alpha1.FinalizerName)
		if err := r.Patch(ctx, &sc, patch); err != nil {
			return ctrl.Result{}, err
		}
	}

	scpcFound, lockRequired, err := r.resolveSCPC(ctx, &sc, true)
	if err != nil {
		reason := providerConfigErrorReason(err)
		if reason == "" {
			return ctrl.Result{}, err
		}
		if sc.Status.ProviderConfigRef != nil ||
			sc.Status.ProviderConfigNamespace != "" || sc.Status.ProviderConfigUID != "" {
			return r.failLockedSCPC(ctx, &sc, reason, err.Error())
		}
		return r.failSC(ctx, &sc, reason, err.Error())
	}
	scpc := *scpcFound
	if lockRequired {
		if err := r.lockSCPC(ctx, &sc, &scpc); err != nil {
			return ctrl.Result{}, err
		}
	}

	order, err := validateAndTopoSort(&scpc)
	if err != nil {
		return r.failSC(ctx, &sc, "InvalidSCPC", err.Error())
	}

	// Resolve inputs up front so a from=profile input with no fitting profile
	// parks the SC in Phase=NoHardware before any resource R is created.
	if _, err := rendertpl.ResolveInputs(&scpc, &sc); err != nil {
		if errors.Is(err, rendertpl.ErrNoProfile) {
			return r.noHardwareSC(ctx, &sc, err.Error())
		}
		return r.failSC(ctx, &sc, "InputError", err.Error())
	}

	tc, err := target.New(ctx, r.Client, r.RESTConfig, r.RESTMapper(), scpc.Spec.Target.KubeconfigSecretRef)
	if err != nil {
		return r.failSC(ctx, &sc, "TargetClientError", err.Error())
	}

	resourceByName := make(map[string]*showroomv1alpha1.ResourceSpec, len(scpc.Spec.Resources))
	for i := range scpc.Spec.Resources {
		rs := &scpc.Spec.Resources[i]
		resourceByName[rs.Name] = rs
	}

	statuses := make(map[string]*showroomv1alpha1.ResourceStatus, len(order))
	live := make(map[string][]*unstructured.Unstructured, len(order))

	for _, name := range order {
		rs := resourceByName[name]

		predecessors := make(map[string]rendertpl.Predecessor, len(rs.DependsOn))
		var waitingOn []string
		for _, dep := range rs.DependsOn {
			depStatus := statuses[dep]
			if depStatus == nil || depStatus.Phase != showroomv1alpha1.PhaseReady {
				waitingOn = append(waitingOn, dep)
				continue
			}
			depRS := resourceByName[dep]
			predecessors[dep] = rendertpl.Predecessor{
				Repeated: depRS != nil && depRS.Repeat != "",
				Objects:  live[dep],
			}
		}
		if len(waitingOn) > 0 {
			statuses[name] = &showroomv1alpha1.ResourceStatus{
				Name:    name,
				Phase:   showroomv1alpha1.PhasePending,
				Reason:  showroomv1alpha1.ReasonWaitingForDependencies,
				Message: "waiting on: " + strings.Join(waitingOn, ", "),
			}
			continue
		}

		rendered, rerr := rendertpl.Render(&scpc, &sc, rs, predecessors)
		if rerr != nil {
			return r.failSC(ctx, &sc, "RenderError",
				fmt.Sprintf("resource %q: %v", name, rerr))
		}
		repeated := rs.Repeat != ""
		if len(rendered) == 0 {
			statuses[name] = &showroomv1alpha1.ResourceStatus{
				Name:    name,
				Phase:   showroomv1alpha1.PhaseReady,
				Reason:  showroomv1alpha1.ReasonReady,
				Message: "repeat=0; nothing to create",
			}
			continue
		}

		instances := make([]showroomv1alpha1.InstanceStatus, 0, len(rendered))
		instanceLive := make([]*unstructured.Unstructured, 0, len(rendered))
		missingIndices := make([]int, 0)
		var gvkStr string

		for idx, desired := range rendered {
			gvk := desired.GroupVersionKind()
			gvkStr = gvk.String()
			mapping, mperr := tc.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
			if mperr != nil {
				return r.failSC(ctx, &sc, "GVKNotFound",
					fmt.Sprintf("resource %q[%d]: RESTMapping %s: %v", name, idx, gvk, mperr))
			}
			ri := tc.ResourceFor(mapping, desired.GetNamespace())

			existing, gerr := ri.Get(ctx, desired.GetName(), metav1.GetOptions{})
			switch {
			case apierrors.IsNotFound(gerr):
				if !scpc.Spec.AllowsProvisioning() {
					// Keep scanning: a prior pass may have created a later
					// instance and crashed before status was persisted. Every
					// extant identity must be observed for eventual cleanup.
					missingIndices = append(missingIndices, idx)
					continue
				}
				// Re-check deletion straight from the API server immediately before
				// creating: a target Create can order real infrastructure. This is
				// the last available guard before crossing into the target system.
				allowed, aerr := r.creationAllowed(ctx, &sc, &scpc)
				if aerr != nil {
					return ctrl.Result{}, aerr
				}
				if !allowed {
					// Requeue so a raced SCPC retirement is surfaced as
					// ProvisioningDisabled (or a raced SC deletion enters the
					// delete path) after the informer cache catches up.
					return ctrl.Result{RequeueAfter: scRequeueWorking}, nil
				}
				created, cerr := ri.Create(ctx, desired, metav1.CreateOptions{})
				if cerr != nil {
					return r.failSC(ctx, &sc, "CreateError",
						fmt.Sprintf("resource %q[%d]: %v", name, idx, cerr))
				}
				log.Info("created target resource", "resource", name, "index", idx,
					"gvk", gvk.String(), "name", created.GetName(), "namespace", created.GetNamespace())
				existing = created
			case gerr != nil:
				return r.failSC(ctx, &sc, "GetTargetError",
					fmt.Sprintf("resource %q[%d]: %v", name, idx, gerr))
			}
			migrated, ownershipErr := r.ensureTargetOwnership(
				ctx, ri, existing, &sc, &scpc, name, desired.GetNamespace(),
			)
			if ownershipErr != nil {
				if errors.Is(ownershipErr, errTargetOwnershipConflict) {
					return r.failSC(ctx, &sc, "TargetOwnershipConflict",
						fmt.Sprintf("resource %q[%d]: %v", name, idx, ownershipErr))
				}
				return ctrl.Result{}, ownershipErr
			}
			if migrated {
				// Re-read the target after the atomic legacy migration before
				// trusting it for readiness, predecessor context, or credentials.
				return ctrl.Result{RequeueAfter: scRequeueWorking}, nil
			}
			// v0.x: do not patch existing R. Drift requires user to delete R.

			phase, lastOp, lastErr := extractPhase(rs.Readiness, existing)
			instances = append(instances, showroomv1alpha1.InstanceStatus{
				Index:           int32(idx),
				TargetName:      existing.GetName(),
				TargetNamespace: existing.GetNamespace(),
				Phase:           phase,
				LastOperation:   lastOp,
				LastError:       lastErr,
			})
			instanceLive = append(instanceLive, existing)
		}
		if len(missingIndices) > 0 {
			msg := fmt.Sprintf(
				"SimpleClusterProviderConfig %q is retired for new provisioning; resource %q instances %v are absent",
				scpc.Name, name, missingIndices)
			return r.failSCUnavailable(ctx, &sc, "ProvisioningDisabled", msg, unavailableTarget{
				resourceName:      name,
				observedResources: completedResourceStatuses(order, statuses),
				observedInstances: instances,
				missingIndices:    missingIndices,
				gvk:               gvkStr,
				repeated:          repeated,
			})
		}

		statuses[name] = aggregateResource(name, gvkStr, repeated, instances)
		live[name] = instanceLive
	}

	aggregate := aggregatePhase(order, statuses)

	patch := client.MergeFrom(sc.DeepCopy())
	sc.Status.TargetCluster = "local"
	if scpc.Spec.Target.KubeconfigSecretRef != nil {
		sc.Status.TargetCluster = scpc.Name
	}
	sc.Status.Resources = orderedStatuses(order, statuses)
	sc.Status.Phase = aggregate
	sc.Status.ObservedGeneration = sc.Generation
	setRoleCounts(&sc)

	cond := metav1.Condition{
		Type:    showroomv1alpha1.ConditionReady,
		Status:  metav1.ConditionFalse,
		Reason:  string(aggregate),
		Message: aggregateMessage(order, statuses),
	}
	if aggregate == showroomv1alpha1.PhaseReady {
		cond.Status = metav1.ConditionTrue
	}
	apimeta.SetStatusCondition(&sc.Status.Conditions, cond)
	apimeta.RemoveStatusCondition(&sc.Status.Conditions, showroomv1alpha1.ConditionError)
	setAutoscalingCondition(&sc, &scpc)

	// Kubeconfig propagation: once the named source resource is Ready,
	// fetch its kubeconfig and mirror it into a Secret in SC.Namespace.
	// For SubResource the renewal-before window gates re-issuance so we
	// don't request a fresh credential every reconcile.
	kcName := scpc.Spec.KubeconfigSource.ResourceName
	if kcLive, ok := live[kcName]; ok && len(kcLive) > 0 {
		if kcStatus := statuses[kcName]; kcStatus != nil && kcStatus.Phase == showroomv1alpha1.PhaseReady {
			if kubeconfig.NeedsRefresh(&sc, &scpc) {
				r.syncKubeconfig(ctx, &sc, &scpc, resourceByName[kcName], kcLive[0], tc)
			}
		}
	}

	// Post-creation add-ons: apply spec.resources onto the provisioned
	// cluster over its synced kubeconfig. Best-effort — outcomes land in
	// per-resource conditions and never affect Phase.
	r.applyClusterResources(ctx, &sc)

	// Mirror the provisioned cluster's nodes (and their external IPs) into
	// status. Best-effort over the same synced kubeconfig; never affects Phase.
	r.collectNodes(ctx, &sc)

	if err := r.Status().Patch(ctx, &sc, patch); err != nil {
		return ctrl.Result{}, err
	}

	requeue := scRequeueReady
	if aggregate != showroomv1alpha1.PhaseReady {
		requeue = scRequeueWorking
	}
	return ctrl.Result{RequeueAfter: requeue}, nil
}

// creationAllowed re-reads both the SimpleCluster and its resolved SCPC
// uncached (via APIReader) immediately before a target Create. It returns
// false once deletion has begun, either object is gone or replaced, either
// spec changed, or provisioning has been disabled. This narrows the race
// between a provider unbind or spec edit and an in-flight reconcile that
// already rendered a billable target; the management and target APIs cannot
// provide one atomic transaction across both systems.
func (r *SimpleClusterReconciler) creationAllowed(
	ctx context.Context,
	sc *showroomv1alpha1.SimpleCluster,
	scpc *showroomv1alpha1.SimpleClusterProviderConfig,
) (bool, error) {
	reader := r.APIReader
	if reader == nil {
		reader = r.Client
	}
	var fresh showroomv1alpha1.SimpleCluster
	if err := reader.Get(ctx, client.ObjectKeyFromObject(sc), &fresh); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if fresh.UID != sc.UID || fresh.Generation != sc.Generation ||
		!maps.Equal(fresh.Labels, sc.Labels) ||
		!sameProviderConfigIdentity(&fresh, sc) {
		return false, nil
	}
	if !fresh.DeletionTimestamp.IsZero() ||
		!controllerutil.ContainsFinalizer(&fresh, showroomv1alpha1.FinalizerName) {
		return false, nil
	}

	var freshSCPC showroomv1alpha1.SimpleClusterProviderConfig
	if err := reader.Get(ctx, client.ObjectKeyFromObject(scpc), &freshSCPC); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if freshSCPC.UID != scpc.UID || freshSCPC.Generation != scpc.Generation {
		return false, nil
	}
	if !freshSCPC.DeletionTimestamp.IsZero() {
		return false, nil
	}
	return freshSCPC.Spec.AllowsProvisioning(), nil
}

func sameProviderConfigIdentity(a, b *showroomv1alpha1.SimpleCluster) bool {
	if (a.Status.ProviderConfigRef == nil) != (b.Status.ProviderConfigRef == nil) {
		return false
	}
	if a.Status.ProviderConfigRef != nil &&
		a.Status.ProviderConfigRef.Name != b.Status.ProviderConfigRef.Name {
		return false
	}
	return a.Status.ProviderConfigNamespace == b.Status.ProviderConfigNamespace &&
		a.Status.ProviderConfigUID == b.Status.ProviderConfigUID
}

// syncKubeconfig fetches the kubeconfig from the source resource, writes
// it into a Secret in SC.Namespace, and updates SC.Status.Kubeconfig*.
// Failures are recorded via the KubeconfigValid condition but do not stop
// the reconcile — the resource itself is Ready and that's what matters.
func (r *SimpleClusterReconciler) syncKubeconfig(
	ctx context.Context,
	sc *showroomv1alpha1.SimpleCluster,
	scpc *showroomv1alpha1.SimpleClusterProviderConfig,
	resource *showroomv1alpha1.ResourceSpec,
	live *unstructured.Unstructured,
	tc *target.Client,
) {
	log := logf.FromContext(ctx)

	data, sourceExpiry, kerr := kubeconfig.Fetch(ctx, tc, scpc, sc, resource, live)
	if kerr != nil {
		apimeta.SetStatusCondition(&sc.Status.Conditions, metav1.Condition{
			Type:    showroomv1alpha1.ConditionKubeconfigValid,
			Status:  metav1.ConditionFalse,
			Reason:  "KubeconfigFetchError",
			Message: kerr.Error(),
		})
		log.Info("kubeconfig fetch failed", "error", kerr.Error())
		return
	}

	res, eerr := kubeconfig.EnsureSecret(ctx, r.Client, r.Scheme, sc, scpc, data, sourceExpiry)
	if eerr != nil {
		apimeta.SetStatusCondition(&sc.Status.Conditions, metav1.Condition{
			Type:    showroomv1alpha1.ConditionKubeconfigValid,
			Status:  metav1.ConditionFalse,
			Reason:  "KubeconfigWriteError",
			Message: eerr.Error(),
		})
		log.Error(eerr, "ensure kubeconfig secret")
		return
	}

	sc.Status.KubeconfigSecretRef = &corev1.LocalObjectReference{Name: res.SecretName}
	sc.Status.Kubeconfig = string(data)
	if res.IssuedAt != nil {
		sc.Status.KubeconfigIssuedAt = &metav1.Time{Time: *res.IssuedAt}
	} else {
		sc.Status.KubeconfigIssuedAt = nil
	}
	if res.ExpiresAt != nil {
		sc.Status.KubeconfigExpiresAt = &metav1.Time{Time: *res.ExpiresAt}
	} else {
		sc.Status.KubeconfigExpiresAt = nil
	}
	apimeta.SetStatusCondition(&sc.Status.Conditions, metav1.Condition{
		Type:    showroomv1alpha1.ConditionKubeconfigValid,
		Status:  metav1.ConditionTrue,
		Reason:  "Synced",
		Message: fmt.Sprintf("kubeconfig mirrored from resource %q", resource.Name),
	})
	if res.Changed {
		log.Info("kubeconfig secret updated", "secret", res.SecretName)
	}
}

// applyClusterResources applies spec.resources onto the freshly-provisioned
// cluster using its synced kubeconfig. Best-effort: every resource's outcome
// is recorded in a "Resource-<name>" condition and a failure never changes
// Phase. No-op until the kubeconfig Secret exists.
func (r *SimpleClusterReconciler) applyClusterResources(ctx context.Context, sc *showroomv1alpha1.SimpleCluster) {
	if len(sc.Spec.Resources) == 0 || sc.Status.KubeconfigSecretRef == nil {
		return
	}
	log := logf.FromContext(ctx)

	tc, err := target.New(ctx, r.Client, r.RESTConfig, r.RESTMapper(), &corev1.SecretReference{
		Name:      sc.Status.KubeconfigSecretRef.Name,
		Namespace: sc.Namespace,
	})
	if err != nil {
		// Cluster not reachable yet — surface on every resource, retry next loop.
		for i := range sc.Spec.Resources {
			setResourceCondition(sc, sc.Spec.Resources[i].Name, metav1.ConditionFalse,
				"ClusterUnreachable", err.Error())
		}
		log.Info("apply resources: cannot reach provisioned cluster", "error", err.Error())
		return
	}

	for i := range sc.Spec.Resources {
		res := sc.Spec.Resources[i]
		if aerr := applyOneClusterResource(ctx, tc, res); aerr != nil {
			setResourceCondition(sc, res.Name, metav1.ConditionFalse, "ApplyFailed", aerr.Error())
			log.Info("apply resource failed", "resource", res.Name, "error", aerr.Error())
			continue
		}
		setResourceCondition(sc, res.Name, metav1.ConditionTrue, "Applied",
			fmt.Sprintf("resource %q applied to the provisioned cluster", res.Name))
	}
}

// applyOneClusterResource server-side-applies one manifest to the target
// (provisioned) cluster. Create-or-update, idempotent across reconciles.
func applyOneClusterResource(ctx context.Context, tc *target.Client, res showroomv1alpha1.ClusterResource) error {
	obj := &unstructured.Unstructured{}
	if err := yaml.Unmarshal([]byte(res.Content), &obj.Object); err != nil {
		return fmt.Errorf("parse yaml: %w", err)
	}
	gvk := obj.GroupVersionKind()
	if gvk.Kind == "" || gvk.Version == "" || obj.GetName() == "" {
		return fmt.Errorf("content must set apiVersion, kind and metadata.name")
	}
	mapping, err := tc.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("RESTMapping %s: %w", gvk, err)
	}
	data, err := obj.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	force := true
	_, err = tc.ResourceFor(mapping, obj.GetNamespace()).Patch(
		ctx, obj.GetName(), types.ApplyPatchType, data,
		metav1.PatchOptions{FieldManager: fieldManager, Force: &force},
	)
	return err
}

// nodeGVR is the cluster-scoped GroupVersionResource of core/v1 Node.
var nodeGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "nodes"}

// collectNodes lists the provisioned cluster's nodes over its synced
// kubeconfig and mirrors name/role/addresses into SC.Status.Nodes. It is
// best-effort: when the cluster is not yet reachable the previous list is
// left in place, and it never affects Phase.
func (r *SimpleClusterReconciler) collectNodes(ctx context.Context, sc *showroomv1alpha1.SimpleCluster) {
	if sc.Status.KubeconfigSecretRef == nil {
		return
	}
	log := logf.FromContext(ctx)

	tc, err := target.New(ctx, r.Client, r.RESTConfig, r.RESTMapper(), &corev1.SecretReference{
		Name:      sc.Status.KubeconfigSecretRef.Name,
		Namespace: sc.Namespace,
	})
	if err != nil {
		log.Info("collect nodes: cannot reach provisioned cluster", "error", err.Error())
		return
	}

	list, err := tc.Dynamic.Resource(nodeGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Info("collect nodes: list failed", "error", err.Error())
		return
	}

	nodes := make([]showroomv1alpha1.NodeStatus, 0, len(list.Items))
	for i := range list.Items {
		var node corev1.Node
		if cerr := runtime.DefaultUnstructuredConverter.FromUnstructured(list.Items[i].Object, &node); cerr != nil {
			log.Info("collect nodes: decode failed", "node", list.Items[i].GetName(), "error", cerr.Error())
			continue
		}
		nodes = append(nodes, nodeStatusOf(&node))
	}
	sortNodes(nodes)
	sc.Status.Nodes = nodes
}

// nodeStatusOf projects a core/v1 Node onto a NodeStatus.
func nodeStatusOf(n *corev1.Node) showroomv1alpha1.NodeStatus {
	ns := showroomv1alpha1.NodeStatus{Name: n.Name, Role: nodeRole(n.Labels)}
	for _, a := range n.Status.Addresses {
		switch a.Type {
		case corev1.NodeExternalIP:
			ns.ExternalIP = a.Address
		case corev1.NodeInternalIP:
			ns.InternalIP = a.Address
		}
	}
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			ns.Ready = c.Status == corev1.ConditionTrue
		}
	}
	return ns
}

// nodeRole returns "control-plane" when the node carries a control-plane (or
// legacy master) role label, otherwise "worker".
func nodeRole(labels map[string]string) string {
	for _, k := range []string{"node-role.kubernetes.io/control-plane", "node-role.kubernetes.io/master"} {
		if _, ok := labels[k]; ok {
			return "control-plane"
		}
	}
	return "worker"
}

// sortNodes orders control-plane nodes before workers, then by name, so the
// list is stable across reconciles.
func sortNodes(nodes []showroomv1alpha1.NodeStatus) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Role != nodes[j].Role {
			return nodes[i].Role == "control-plane"
		}
		return nodes[i].Name < nodes[j].Name
	})
}

// setResourceCondition records one ClusterResource's apply outcome as a
// condition of type "Resource-<name>".
func setResourceCondition(sc *showroomv1alpha1.SimpleCluster, name string, status metav1.ConditionStatus, reason, msg string) {
	apimeta.SetStatusCondition(&sc.Status.Conditions, metav1.Condition{
		Type:               showroomv1alpha1.ConditionResourcePrefix + name,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: sc.Generation,
	})
}

// reconcileDelete cascades the SC deletion by walking the SCPC DAG in
// reverse topological order, applying each resource's deletionAnnotations
// and deleting R, then removing the finalizer. The kubeconfig Secret is
// garbage-collected by Kubernetes via its OwnerReference on SC.
func (r *SimpleClusterReconciler) reconcileDelete(ctx context.Context, sc *showroomv1alpha1.SimpleCluster) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if sc.Status.Phase != showroomv1alpha1.PhaseDeleting {
		patch := client.MergeFrom(sc.DeepCopy())
		sc.Status.Phase = showroomv1alpha1.PhaseDeleting
		for i := range sc.Status.Resources {
			sc.Status.Resources[i].Reason = showroomv1alpha1.ReasonDeleting
			sc.Status.Resources[i].Message = "deletion in progress"
		}
		if err := r.Status().Patch(ctx, sc, patch); err != nil {
			log.Error(err, "patch SC status to Deleting failed; proceeding")
		}
	}

	if !controllerutil.ContainsFinalizer(sc, showroomv1alpha1.FinalizerName) {
		return ctrl.Result{}, nil
	}

	scpc, lockRequired, err := r.resolveSCPC(ctx, sc, false)
	if err != nil {
		reason := providerConfigErrorReason(err)
		if reason == "" {
			log.Error(err, "resolve provider config for delete failed; will retry")
			return ctrl.Result{RequeueAfter: scRequeueWorking}, nil
		}
		return r.failSC(ctx, sc, reason, err.Error())
	}
	if lockRequired {
		if err := r.lockSCPC(ctx, sc, scpc); err != nil {
			log.Error(err, "lock provider config identity for delete failed; will retry")
			return ctrl.Result{RequeueAfter: scRequeueWorking}, nil
		}
	}

	done, err := r.deleteTargetResources(ctx, sc, scpc)
	if err != nil {
		log.Error(err, "delete target resources failed; will retry")
		return r.failSC(ctx, sc, "CleanupBlocked", err.Error())
	}
	if !done {
		// At least one resource at the current reverse-topo level is
		// still terminating. Requeue without removing the finalizer so
		// downstream operators have time to finish their teardown
		// before we delete the predecessors they depend on
		// (e.g. UbuntuCluster's k3s uninstall must complete before
		// the OpenStackVMs it SSHes into are torn down).
		return ctrl.Result{RequeueAfter: scRequeueWorking}, nil
	}

	patch := client.MergeFrom(sc.DeepCopy())
	controllerutil.RemoveFinalizer(sc, showroomv1alpha1.FinalizerName)
	if err := r.Patch(ctx, sc, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// deleteTargetResources walks the DAG in reverse topological order and
// drives the cascade. Each call processes one DAG level: it fires Delete on
// every target at that level (idempotent — only patches deletionAnnotations
// and calls Delete on first sight), then returns done=false if any target
// at that level still exists. Predecessors are only touched once the
// current level is fully gone. This barrier lets downstream operators
// finish their own cleanup (e.g. UbuntuCluster SSHing into a VM to
// uninstall k3s) BEFORE the resources they depend on disappear.
// Returns (true, nil) once all levels are clear.
func (r *SimpleClusterReconciler) deleteTargetResources(
	ctx context.Context,
	sc *showroomv1alpha1.SimpleCluster,
	scpc *showroomv1alpha1.SimpleClusterProviderConfig,
) (bool, error) {
	log := logf.FromContext(ctx)

	order, err := validateAndTopoSort(scpc)
	if err != nil {
		// SCPC has gone invalid since creation; best-effort delete by status.
		log.Error(err, "SCPC invalid during delete; falling back to status-tracked order")
		order = orderFromStatus(sc)
	}

	tc, err := target.New(ctx, r.Client, r.RESTConfig, r.RESTMapper(), scpc.Spec.Target.KubeconfigSecretRef)
	if err != nil {
		return false, fmt.Errorf("build target client: %w", err)
	}

	resourceByName := make(map[string]*showroomv1alpha1.ResourceSpec, len(scpc.Spec.Resources))
	for i := range scpc.Spec.Resources {
		rs := &scpc.Spec.Resources[i]
		resourceByName[rs.Name] = rs
	}
	statusByName := make(map[string]*showroomv1alpha1.ResourceStatus, len(sc.Status.Resources))
	for i := range sc.Status.Resources {
		s := &sc.Status.Resources[i]
		statusByName[s.Name] = s
	}

	for i := len(order) - 1; i >= 0; i-- {
		name := order[i]
		rs := resourceByName[name]
		if rs == nil {
			continue
		}
		gv, gerr := schema.ParseGroupVersion(rs.APIVersion)
		if gerr != nil {
			return false, fmt.Errorf("resource %q: parse apiVersion: %w", name, gerr)
		}
		gvk := gv.WithKind(rs.Kind)
		mapping, mperr := tc.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if mperr != nil {
			return false, fmt.Errorf("resource %q: RESTMapping %s: %w", name, gvk, mperr)
		}

		defaultNs, nerr := rendertpl.ResolveNamespaceForCleanup(scpc, sc, rs)

		targets, fallbackErr, terr := deletionTargets(scpc, sc, rs, statusByName[name], defaultNs, nerr)
		if terr != nil {
			return false, fmt.Errorf("resource %q: %w", name, terr)
		}
		if fallbackErr != nil {
			log.Info("target identities cannot be fully reconstructed during delete; using recorded identities",
				"resource", name, "error", fallbackErr.Error())
		}

		levelDone := true
		for _, t := range targets {
			ri := tc.ResourceFor(mapping, t.namespace)
			exists, derr := r.deleteOneTarget(
				ctx, ri, rs, gvk, name, t.name, t.namespace, sc, scpc,
			)
			if derr != nil {
				return false, derr
			}
			if exists {
				levelDone = false
			}
		}
		if !levelDone {
			log.Info("waiting for level to terminate before deleting predecessors",
				"resource", name)
			return false, nil
		}
		if fallbackErr != nil {
			return false, fmt.Errorf(
				"resource %q: cleanup cannot prove that recorded identities are complete: %w",
				name, fallbackErr)
		}
	}
	return true, nil
}

type namespacedTarget struct {
	name      string
	namespace string
}

// deletionTargets returns the union of status-recorded and reconstructible
// target identities. Every candidate must prove ownership before deletion;
// status is an identity journal, not authority to delete an unrelated object.
// If reconstruction fails but status contains identities, fallbackErr explains
// why only those recorded identities can be used.
func deletionTargets(
	scpc *showroomv1alpha1.SimpleClusterProviderConfig,
	sc *showroomv1alpha1.SimpleCluster,
	rs *showroomv1alpha1.ResourceSpec,
	status *showroomv1alpha1.ResourceStatus,
	defaultNamespace string,
	namespaceErr error,
) (targets []namespacedTarget, fallbackErr, err error) {
	seenTargets := make(map[namespacedTarget]struct{})
	add := func(candidate namespacedTarget) {
		if candidate.name == "" {
			return
		}
		if _, seen := seenTargets[candidate]; seen {
			return
		}
		seenTargets[candidate] = struct{}{}
		targets = append(targets, candidate)
	}

	if status != nil && len(status.Instances) > 0 {
		for _, instance := range status.Instances {
			namespace := instance.TargetNamespace
			if namespace == "" {
				if namespaceErr != nil {
					fallbackErr = namespaceErr
					continue
				}
				namespace = defaultNamespace
			}
			add(namespacedTarget{name: instance.TargetName, namespace: namespace})
		}
	} else if status != nil && status.TargetName != "" {
		namespace := status.TargetNamespace
		if namespace == "" {
			if namespaceErr != nil {
				return nil, nil, fmt.Errorf("recorded target has no namespace and cleanup cannot resolve one: %w", namespaceErr)
			}
			namespace = defaultNamespace
		}
		add(namespacedTarget{name: status.TargetName, namespace: namespace})
	}

	count := 1
	if rs.Repeat != "" {
		var repeatErr error
		count, repeatErr = rendertpl.EvaluateRepeatForCleanup(scpc, sc, rs)
		if repeatErr != nil {
			if len(targets) == 0 {
				return nil, nil, fmt.Errorf("resolve repeat for delete fallback: %w", repeatErr)
			}
			return targets, repeatErr, nil
		}
	}
	for index := 0; index < count; index++ {
		name, namespace, resolveErr := rendertpl.ResolveTargetForCleanup(scpc, sc, rs, index)
		if resolveErr != nil {
			if len(targets) == 0 {
				return nil, nil, fmt.Errorf("resolve target for delete fallback: %w", resolveErr)
			}
			return targets, resolveErr, nil
		}
		add(namespacedTarget{name: name, namespace: namespace})
	}
	return targets, fallbackErr, nil
}

// deleteOneTarget issues (or progresses) the deletion of a single target
// object. It reports whether the object still exists — i.e. whether the caller
// must keep waiting before deleting this level's predecessors. On error the
// returned bool is meaningless; the caller bails out on err.
func (r *SimpleClusterReconciler) deleteOneTarget(
	ctx context.Context,
	ri dynamic.ResourceInterface,
	rs *showroomv1alpha1.ResourceSpec,
	gvk schema.GroupVersionKind,
	resourceName, targetName, targetNamespace string,
	sc *showroomv1alpha1.SimpleCluster,
	scpc *showroomv1alpha1.SimpleClusterProviderConfig,
) (bool, error) {
	log := logf.FromContext(ctx)

	existing, err := ri.Get(ctx, targetName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("resource %q: get %s: %w", resourceName, targetName, err)
	}
	migrated, ownershipErr := r.ensureTargetOwnership(
		ctx, ri, existing, sc, scpc, resourceName, targetNamespace,
	)
	if ownershipErr != nil {
		return true, fmt.Errorf("resource %q: %w", resourceName, ownershipErr)
	}
	if migrated {
		// Do not combine legacy adoption and deletion in one API observation.
		// The next pass must re-read and verify the immutable identity stamps.
		return true, nil
	}

	// First time we see this target after SC was marked for deletion: announce that this
	// child may go (the admission guard refuses deletes of labelled children otherwise, and
	// the sync-agent carries the annotation to the object's KCP mirror), patch
	// deletionAnnotations (Gardener-style opt-in) and issue Delete. Subsequent
	// reconciles skip the work — a non-zero deletionTimestamp means our prior
	// Delete already landed.
	if existing.GetDeletionTimestamp().IsZero() {
		annotations := map[string]string{rendertpl.AnnoAllowDelete: "true"}
		for k, v := range rs.DeletionAnnotations {
			annotations[k] = v
		}
		patched, perr := guardedAnnotationPatch(ctx, ri, existing, annotations)
		if perr != nil {
			return true, fmt.Errorf("resource %q: patch deletion annotations: %w", resourceName, perr)
		}
		uid := patched.GetUID()
		if uid == "" {
			return true, fmt.Errorf("resource %q: patched target %s/%s has no UID",
				resourceName, patched.GetNamespace(), patched.GetName())
		}
		if derr := ri.Delete(ctx, targetName, metav1.DeleteOptions{
			Preconditions: &metav1.Preconditions{UID: &uid},
		}); derr != nil && !apierrors.IsNotFound(derr) {
			return true, fmt.Errorf("resource %q: delete %s: %w", resourceName, targetName, derr)
		}
		log.Info("target resource delete requested",
			"resource", resourceName, "name", targetName, "gvk", gvk.String())
		return true, nil
	}

	// Force-cleanup escape hatch: R is opted in via SCPC and has been
	// Terminating long enough that we no longer expect the downstream
	// controller to make progress on its own (typically because the
	// infrastructure it relied on is also gone). Strip all finalizers so K8s GC
	// removes R immediately.
	if rs.ForceCleanupOnCascade && len(existing.GetFinalizers()) > 0 &&
		time.Since(existing.GetDeletionTimestamp().Time) >= forceCleanupGrace {
		if _, perr := guardedFinalizerPatch(ctx, ri, existing, nil); perr != nil {
			return true, fmt.Errorf("resource %q: force-strip finalizers: %w", resourceName, perr)
		}
		log.Info("force-stripped finalizers after grace window",
			"resource", resourceName, "name", targetName, "gvk", gvk.String(),
			"grace", forceCleanupGrace)
	}

	return true, nil
}

type targetOwnershipState uint8

const (
	targetOwnershipMismatch targetOwnershipState = iota
	targetOwnershipLegacy
	targetOwnershipExact
)

type jsonPatchOperation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}

// ensureTargetOwnership accepts only the exact SimpleCluster and SCPC
// incarnations stamped at create time. Targets made by an older release carry
// only the two name annotations. Such a target is migrated atomically only
// when the controller-owned SimpleCluster status already records its exact
// resource/GVK/name/namespace identity; reconstruction alone is not adoption
// authority. Callers must requeue after a successful migration.
func (r *SimpleClusterReconciler) ensureTargetOwnership(
	ctx context.Context,
	ri dynamic.ResourceInterface,
	object *unstructured.Unstructured,
	sc *showroomv1alpha1.SimpleCluster,
	scpc *showroomv1alpha1.SimpleClusterProviderConfig,
	resourceName, targetNamespace string,
) (bool, error) {
	switch classifyTargetOwnership(object, sc, scpc) {
	case targetOwnershipExact:
		return false, nil
	case targetOwnershipMismatch:
		return false, fmt.Errorf(
			"%w: target %s/%s lacks the exact SimpleCluster and provider identity",
			errTargetOwnershipConflict, object.GetNamespace(), object.GetName())
	case targetOwnershipLegacy:
		if !statusRecordsTarget(
			sc, resourceName, object.GroupVersionKind().String(), object.GetName(), targetNamespace,
		) {
			return false, fmt.Errorf(
				"%w: legacy target %s/%s is not recorded in SimpleCluster status; verify it manually",
				errTargetOwnershipConflict, object.GetNamespace(), object.GetName())
		}
		patched, err := guardedAnnotationPatch(ctx, ri, object, immutableOwnershipAnnotations(sc, scpc))
		if err != nil {
			return false, fmt.Errorf("migrate legacy target %s/%s ownership: %w",
				object.GetNamespace(), object.GetName(), err)
		}
		if classifyTargetOwnership(patched, sc, scpc) != targetOwnershipExact {
			return false, fmt.Errorf("migrate legacy target %s/%s ownership: patched object did not retain exact identity",
				object.GetNamespace(), object.GetName())
		}
		return true, nil
	default:
		return false, fmt.Errorf("%w: target %s/%s has unknown ownership state",
			errTargetOwnershipConflict, object.GetNamespace(), object.GetName())
	}
}

func classifyTargetOwnership(
	object *unstructured.Unstructured,
	sc *showroomv1alpha1.SimpleCluster,
	scpc *showroomv1alpha1.SimpleClusterProviderConfig,
) targetOwnershipState {
	annotations := object.GetAnnotations()
	if annotations[rendertpl.AnnoSimpleCluster] != sc.Namespace+"/"+sc.Name ||
		annotations[rendertpl.AnnoSimpleClusterProvider] != scpc.Name {
		return targetOwnershipMismatch
	}

	expected := immutableOwnershipAnnotations(sc, scpc)
	present := 0
	for key, value := range expected {
		actual, ok := annotations[key]
		if !ok {
			continue
		}
		present++
		if actual != value {
			return targetOwnershipMismatch
		}
	}
	if present == 0 {
		return targetOwnershipLegacy
	}
	if present != len(expected) {
		return targetOwnershipMismatch
	}
	if sc.UID == "" || scpc.Namespace == "" || scpc.UID == "" {
		return targetOwnershipMismatch
	}
	return targetOwnershipExact
}

func immutableOwnershipAnnotations(
	sc *showroomv1alpha1.SimpleCluster,
	scpc *showroomv1alpha1.SimpleClusterProviderConfig,
) map[string]string {
	return map[string]string{
		rendertpl.AnnoSimpleClusterUID:               string(sc.UID),
		rendertpl.AnnoSimpleClusterProviderNamespace: scpc.Namespace,
		rendertpl.AnnoSimpleClusterProviderUID:       string(scpc.UID),
	}
}

func statusRecordsTarget(
	sc *showroomv1alpha1.SimpleCluster,
	resourceName, targetGVK, targetName, targetNamespace string,
) bool {
	if targetGVK == "" {
		return false
	}
	match := func(name, namespace string) bool {
		if namespace == "" {
			namespace = targetNamespace
		}
		return name == targetName && namespace == targetNamespace
	}
	for i := range sc.Status.Resources {
		status := &sc.Status.Resources[i]
		if status.Name != resourceName || status.GVK == "" || status.GVK != targetGVK {
			continue
		}
		if match(status.TargetName, status.TargetNamespace) {
			return true
		}
		for _, instance := range status.Instances {
			if match(instance.TargetName, instance.TargetNamespace) {
				return true
			}
		}
		return false
	}
	return false
}

func guardedAnnotationPatch(
	ctx context.Context,
	ri dynamic.ResourceInterface,
	object *unstructured.Unstructured,
	annotations map[string]string,
) (*unstructured.Unstructured, error) {
	operations, err := metadataIdentityTests(object)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(annotations))
	for key := range annotations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		operations = append(operations, jsonPatchOperation{
			Op:    "add",
			Path:  "/metadata/annotations/" + escapeJSONPointerToken(key),
			Value: annotations[key],
		})
	}
	return guardedJSONPatch(ctx, ri, object, operations)
}

func guardedFinalizerPatch(
	ctx context.Context,
	ri dynamic.ResourceInterface,
	object *unstructured.Unstructured,
	finalizers []string,
) (*unstructured.Unstructured, error) {
	operations, err := metadataIdentityTests(object)
	if err != nil {
		return nil, err
	}
	operations = append(operations, jsonPatchOperation{
		Op: "replace", Path: "/metadata/finalizers", Value: finalizers,
	})
	return guardedJSONPatch(ctx, ri, object, operations)
}

func metadataIdentityTests(object *unstructured.Unstructured) ([]jsonPatchOperation, error) {
	if object.GetUID() == "" || object.GetResourceVersion() == "" {
		return nil, fmt.Errorf("target %s/%s has no UID or resourceVersion",
			object.GetNamespace(), object.GetName())
	}
	return []jsonPatchOperation{
		{Op: "test", Path: "/metadata/uid", Value: string(object.GetUID())},
		{Op: "test", Path: "/metadata/resourceVersion", Value: object.GetResourceVersion()},
	}, nil
}

func guardedJSONPatch(
	ctx context.Context,
	ri dynamic.ResourceInterface,
	object *unstructured.Unstructured,
	operations []jsonPatchOperation,
) (*unstructured.Unstructured, error) {
	patchBytes, err := json.Marshal(operations)
	if err != nil {
		return nil, fmt.Errorf("marshal guarded JSON patch: %w", err)
	}
	patched, err := ri.Patch(ctx, object.GetName(), types.JSONPatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return nil, err
	}
	if patched.GetUID() != object.GetUID() || patched.GetResourceVersion() == "" {
		return nil, fmt.Errorf("guarded JSON patch returned a different or unversioned target")
	}
	return patched, nil
}

func escapeJSONPointerToken(value string) string {
	value = strings.ReplaceAll(value, "~", "~0")
	return strings.ReplaceAll(value, "/", "~1")
}

// resolveSCPC returns the exact provider config selected for this
// SimpleCluster and whether its identity still needs to be persisted. New
// active reconciles preserve tenant-first selection. Existing incomplete
// status, including deletion from an older release, is backfilled only when a
// single candidate can be proven.
func (r *SimpleClusterReconciler) resolveSCPC(
	ctx context.Context,
	sc *showroomv1alpha1.SimpleCluster,
	allowNewSelection bool,
) (*showroomv1alpha1.SimpleClusterProviderConfig, bool, error) {
	ref := sc.Status.ProviderConfigRef
	if ref == nil {
		if sc.Status.ProviderConfigNamespace != "" || sc.Status.ProviderConfigUID != "" {
			return nil, false, fmt.Errorf(
				"%w: provider config namespace or UID is set without status.providerConfigRef",
				errSCPCReferenceInvalid)
		}
		name := selectProvider(sc.Spec.ProviderConfigRefs)
		if name == "" {
			return nil, false, fmt.Errorf("%w: providerConfigRefs must list at least one SCPC name", errSCPCReferenceInvalid)
		}
		if allowNewSelection {
			scpc, err := r.getPreferredSCPC(ctx, sc, name)
			return scpc, err == nil, err
		}
		ref = &corev1.LocalObjectReference{Name: name}
	}
	if ref.Name == "" {
		return nil, false, fmt.Errorf("%w: status.providerConfigRef.name is empty", errSCPCReferenceInvalid)
	}

	if sc.Status.ProviderConfigNamespace != "" {
		scpc, err := r.getExactSCPC(ctx, ref.Name, sc.Status.ProviderConfigNamespace)
		if err != nil {
			return nil, false, err
		}
		if sc.Status.ProviderConfigUID != "" && sc.Status.ProviderConfigUID != scpc.UID {
			return nil, false, fmt.Errorf(
				"%w: locked SimpleClusterProviderConfig %s/%s has UID %q, current object has UID %q",
				errSCPCIdentityChanged, scpc.Namespace, scpc.Name, sc.Status.ProviderConfigUID, scpc.UID)
		}
		return scpc, sc.Status.ProviderConfigUID == "", nil
	}

	candidates, err := r.findSCPCandidates(ctx, sc, ref.Name)
	if err != nil {
		return nil, false, err
	}
	if sc.Status.ProviderConfigUID != "" {
		for _, candidate := range candidates {
			if candidate.UID == sc.Status.ProviderConfigUID {
				return candidate, true, nil
			}
		}
		if len(candidates) == 0 {
			return nil, false, fmt.Errorf(
				"%w: locked SimpleClusterProviderConfig %q with UID %q is absent",
				errSCPCNotFound, ref.Name, sc.Status.ProviderConfigUID)
		}
		return nil, false, fmt.Errorf(
			"%w: no SimpleClusterProviderConfig named %q has locked UID %q",
			errSCPCIdentityChanged, ref.Name, sc.Status.ProviderConfigUID)
	}

	switch len(candidates) {
	case 0:
		return nil, false, fmt.Errorf(
			"%w: SimpleClusterProviderConfig %q is absent from namespace %q and fallback namespace %q",
			errSCPCNotFound, ref.Name, sc.Namespace, r.FallbackSCPCNamespace)
	case 1:
		return candidates[0], true, nil
	default:
		return nil, false, fmt.Errorf(
			"%w: legacy name-only SimpleClusterProviderConfig %q exists in both %q and %q",
			errSCPCAmbiguous, ref.Name, candidates[0].Namespace, candidates[1].Namespace)
	}
}

// getPreferredSCPC implements the selection contract for a new
// SimpleCluster: its own namespace wins, then FallbackSCPCNamespace.
func (r *SimpleClusterReconciler) getPreferredSCPC(
	ctx context.Context,
	sc *showroomv1alpha1.SimpleCluster,
	name string,
) (*showroomv1alpha1.SimpleClusterProviderConfig, error) {
	reader := r.providerConfigReader()
	var scpc showroomv1alpha1.SimpleClusterProviderConfig
	err := reader.Get(ctx, types.NamespacedName{Namespace: sc.Namespace, Name: name}, &scpc)
	if err == nil {
		return &scpc, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, err
	}
	if r.FallbackSCPCNamespace == "" || r.FallbackSCPCNamespace == sc.Namespace {
		return nil, fmt.Errorf(
			"%w: SimpleClusterProviderConfig %q is absent from namespace %q and fallback namespace %q",
			errSCPCNotFound, name, sc.Namespace, r.FallbackSCPCNamespace)
	}
	err = reader.Get(ctx, types.NamespacedName{Namespace: r.FallbackSCPCNamespace, Name: name}, &scpc)
	if apierrors.IsNotFound(err) {
		return nil, fmt.Errorf(
			"%w: SimpleClusterProviderConfig %q is absent from namespace %q and fallback namespace %q",
			errSCPCNotFound, name, sc.Namespace, r.FallbackSCPCNamespace)
	}
	if err != nil {
		return nil, err
	}
	return &scpc, nil
}

func (r *SimpleClusterReconciler) getExactSCPC(
	ctx context.Context,
	name, namespace string,
) (*showroomv1alpha1.SimpleClusterProviderConfig, error) {
	reader := r.providerConfigReader()
	var scpc showroomv1alpha1.SimpleClusterProviderConfig
	if err := reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &scpc); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf(
				"%w: locked SimpleClusterProviderConfig %s/%s is absent",
				errSCPCNotFound, namespace, name)
		}
		return nil, err
	}
	return &scpc, nil
}

func (r *SimpleClusterReconciler) findSCPCandidates(
	ctx context.Context,
	sc *showroomv1alpha1.SimpleCluster,
	name string,
) ([]*showroomv1alpha1.SimpleClusterProviderConfig, error) {
	reader := r.providerConfigReader()
	namespaces := []string{sc.Namespace}
	if r.FallbackSCPCNamespace != "" && r.FallbackSCPCNamespace != sc.Namespace {
		namespaces = append(namespaces, r.FallbackSCPCNamespace)
	}
	candidates := make([]*showroomv1alpha1.SimpleClusterProviderConfig, 0, len(namespaces))
	for _, namespace := range namespaces {
		var scpc showroomv1alpha1.SimpleClusterProviderConfig
		err := reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &scpc)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, &scpc)
	}
	return candidates, nil
}

func (r *SimpleClusterReconciler) providerConfigReader() client.Reader {
	if r.APIReader != nil {
		return r.APIReader
	}
	return r.Client
}

// lockSCPC persists the name, namespace and UID together before any target
// client is constructed. This is the cleanup authority after a later crash.
func (r *SimpleClusterReconciler) lockSCPC(
	ctx context.Context,
	sc *showroomv1alpha1.SimpleCluster,
	scpc *showroomv1alpha1.SimpleClusterProviderConfig,
) error {
	if scpc.Name == "" || scpc.Namespace == "" || scpc.UID == "" {
		return fmt.Errorf(
			"%w: selected SimpleClusterProviderConfig must have name, namespace and UID",
			errSCPCReferenceInvalid)
	}
	patch := client.MergeFrom(sc.DeepCopy())
	sc.Status.ProviderConfigRef = &corev1.LocalObjectReference{Name: scpc.Name}
	sc.Status.ProviderConfigNamespace = scpc.Namespace
	sc.Status.ProviderConfigUID = scpc.UID
	if err := r.Status().Patch(ctx, sc, patch); err != nil {
		return fmt.Errorf("persist provider config identity: %w", err)
	}
	return nil
}

func providerConfigErrorReason(err error) string {
	switch {
	case errors.Is(err, errSCPCNotFound):
		return "SCPCNotFound"
	case errors.Is(err, errSCPCAmbiguous):
		return "SCPCReferenceAmbiguous"
	case errors.Is(err, errSCPCIdentityChanged):
		return "SCPCIdentityMismatch"
	case errors.Is(err, errSCPCReferenceInvalid):
		return "InvalidSpec"
	default:
		return ""
	}
}

// selectProvider picks an SCPC name from the SC's candidate list. v0.x
// returns the first non-empty entry; future revisions will probe each
// SCPC's status (reachability, capacity) before picking.
func selectProvider(refs []string) string {
	for _, r := range refs {
		if r != "" {
			return r
		}
	}
	return ""
}

// orderFromStatus reconstructs a best-effort ordering when the live SCPC
// is no longer valid for sorting: use whatever order the status records.
func orderFromStatus(sc *showroomv1alpha1.SimpleCluster) []string {
	out := make([]string, 0, len(sc.Status.Resources))
	for _, s := range sc.Status.Resources {
		out = append(out, s.Name)
	}
	return out
}

// failSC patches an Error condition on the SC and stops the reconcile.
// Returns a result that re-queues on the working cadence.
func (r *SimpleClusterReconciler) failSC(
	ctx context.Context, sc *showroomv1alpha1.SimpleCluster, reason, msg string,
) (ctrl.Result, error) {
	patch := client.MergeFrom(sc.DeepCopy())
	apimeta.SetStatusCondition(&sc.Status.Conditions, metav1.Condition{
		Type:    showroomv1alpha1.ConditionError,
		Status:  metav1.ConditionTrue,
		Reason:  reason,
		Message: msg,
	})
	if sc.Status.Phase == "" {
		sc.Status.Phase = showroomv1alpha1.PhasePending
	}
	setRoleCounts(sc)
	if err := r.Status().Patch(ctx, sc, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: scRequeueWorking}, nil
}

// failLockedSCPC clears stale readiness when the exact provider identity can
// no longer be resolved. No target is observed, created or deleted in this
// state; the lock remains unchanged for recovery or verified manual cleanup.
func (r *SimpleClusterReconciler) failLockedSCPC(
	ctx context.Context,
	sc *showroomv1alpha1.SimpleCluster,
	reason, msg string,
) (ctrl.Result, error) {
	patch := client.MergeFrom(sc.DeepCopy())
	sc.Status.Phase = showroomv1alpha1.PhaseFailed
	sc.Status.ObservedGeneration = sc.Generation
	apimeta.SetStatusCondition(&sc.Status.Conditions, metav1.Condition{
		Type:    showroomv1alpha1.ConditionReady,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: msg,
	})
	apimeta.SetStatusCondition(&sc.Status.Conditions, metav1.Condition{
		Type:    showroomv1alpha1.ConditionError,
		Status:  metav1.ConditionTrue,
		Reason:  reason,
		Message: msg,
	})
	setRoleCounts(sc)
	if err := r.Status().Patch(ctx, sc, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: scRequeueWorking}, nil
}

// failSCUnavailable records a failure that proves the provisioned cluster is
// not currently available. Unlike failSC, which preserves the last observed
// phase for transient reconciliation errors, this clears a stale Ready report:
// a missing target that cannot be recreated while its SCPC is retired is not a
// healthy cluster.
type unavailableTarget struct {
	resourceName      string
	observedResources []showroomv1alpha1.ResourceStatus
	observedInstances []showroomv1alpha1.InstanceStatus
	missingIndices    []int
	gvk               string
	repeated          bool
}

func (r *SimpleClusterReconciler) failSCUnavailable(
	ctx context.Context,
	sc *showroomv1alpha1.SimpleCluster,
	reason, msg string,
	unavailable unavailableTarget,
) (ctrl.Result, error) {
	patch := client.MergeFrom(sc.DeepCopy())
	mergeObservedResourceStatuses(sc, unavailable.observedResources)
	markResourceUnavailable(sc, unavailable, msg)
	sc.Status.Phase = showroomv1alpha1.PhaseFailed
	sc.Status.ObservedGeneration = sc.Generation
	apimeta.SetStatusCondition(&sc.Status.Conditions, metav1.Condition{
		Type:    showroomv1alpha1.ConditionReady,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: msg,
	})
	apimeta.SetStatusCondition(&sc.Status.Conditions, metav1.Condition{
		Type:    showroomv1alpha1.ConditionError,
		Status:  metav1.ConditionTrue,
		Reason:  reason,
		Message: msg,
	})
	setRoleCounts(sc)
	if err := r.Status().Patch(ctx, sc, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: scRequeueWorking}, nil
}

// mergeObservedResourceStatuses persists resources that were authoritatively
// observed earlier in the same pass before a later retired resource proved
// unavailable. Preserve untouched rows from prior passes: they may be the only
// cleanup identity for resources the current pass did not reach.
func mergeObservedResourceStatuses(
	sc *showroomv1alpha1.SimpleCluster,
	observed []showroomv1alpha1.ResourceStatus,
) {
	indices := make(map[string]int, len(sc.Status.Resources))
	for i := range sc.Status.Resources {
		indices[sc.Status.Resources[i].Name] = i
	}
	for _, status := range observed {
		if index, ok := indices[status.Name]; ok {
			sc.Status.Resources[index] = status
			continue
		}
		indices[status.Name] = len(sc.Status.Resources)
		sc.Status.Resources = append(sc.Status.Resources, status)
	}
}

// markResourceUnavailable replaces a stale per-resource Ready report when a
// target is authoritatively absent and policy forbids recreating it. Keep the
// observed target identities in status so cleanup can find everything that
// existed before the failure. Unobserved rendered identities deliberately do
// not become adopted status entries.
func markResourceUnavailable(
	sc *showroomv1alpha1.SimpleCluster,
	unavailable unavailableTarget,
	msg string,
) {
	var status *showroomv1alpha1.ResourceStatus
	for i := range sc.Status.Resources {
		if sc.Status.Resources[i].Name == unavailable.resourceName {
			status = &sc.Status.Resources[i]
			break
		}
	}
	if status == nil {
		sc.Status.Resources = append(sc.Status.Resources, showroomv1alpha1.ResourceStatus{Name: unavailable.resourceName})
		status = &sc.Status.Resources[len(sc.Status.Resources)-1]
	}

	status.GVK = unavailable.gvk
	status.Phase = showroomv1alpha1.PhaseFailed
	status.Reason = showroomv1alpha1.ReasonFailed
	status.Message = msg
	status.LastError = msg
	if !unavailable.repeated {
		return
	}
	status.TargetName = ""
	status.TargetNamespace = ""

	// The reconcile loop scans past missing instances while the SCPC is retired,
	// so this union includes later objects left by an interrupted active pass.
	known := make(map[int32]showroomv1alpha1.InstanceStatus, len(status.Instances)+len(unavailable.observedInstances))
	for _, instance := range status.Instances {
		known[instance.Index] = instance
	}
	for _, instance := range unavailable.observedInstances {
		known[instance.Index] = instance
	}
	for _, index := range unavailable.missingIndices {
		if instance, observedBefore := known[int32(index)]; observedBefore {
			instance.Phase = showroomv1alpha1.PhaseFailed
			instance.LastError = msg
			known[int32(index)] = instance
		}
	}
	indices := make([]int, 0, len(known))
	for index := range known {
		indices = append(indices, int(index))
	}
	sort.Ints(indices)
	instances := make([]showroomv1alpha1.InstanceStatus, 0, len(indices))
	for _, index := range indices {
		instances = append(instances, known[int32(index)])
	}
	status.Instances = instances
}

// noHardwareSC parks the SC in Phase=NoHardware: a from=profile input could
// not be satisfied by any SCPC profile. No resource R is created. The SC is
// re-queued so it recovers automatically once a large-enough profile is added
// or the request shrinks.
func (r *SimpleClusterReconciler) noHardwareSC(
	ctx context.Context, sc *showroomv1alpha1.SimpleCluster, msg string,
) (ctrl.Result, error) {
	patch := client.MergeFrom(sc.DeepCopy())
	sc.Status.Phase = showroomv1alpha1.PhaseNoHardware
	sc.Status.ObservedGeneration = sc.Generation
	apimeta.SetStatusCondition(&sc.Status.Conditions, metav1.Condition{
		Type:    showroomv1alpha1.ConditionReady,
		Status:  metav1.ConditionFalse,
		Reason:  string(showroomv1alpha1.PhaseNoHardware),
		Message: msg,
	})
	setRoleCounts(sc)
	if err := r.Status().Patch(ctx, sc, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: scRequeueWorking}, nil
}

// setRoleCounts fills the CP / WKR printer columns from the SC spec
// and aggregate Phase. SC has no per-node visibility into NC, so the
// ready side tracks Phase (Ready ⇒ full, otherwise 0). When the SCPC
// owns the control plane (spec.controlPlane omitted), CP is "-/-".
// With autoscaling the declared side is the range, e.g. "2/2-5".
func setRoleCounts(sc *showroomv1alpha1.SimpleCluster) {
	wkrDeclared := int(sc.Spec.Workers.MinNodes())
	cpReady, wkrReady := 0, 0
	if sc.Status.Phase == showroomv1alpha1.PhaseReady {
		wkrReady = wkrDeclared
	}
	if sc.Spec.ControlPlane != nil {
		cpDeclared := int(sc.Spec.ControlPlane.Nodes)
		if sc.Status.Phase == showroomv1alpha1.PhaseReady {
			cpReady = cpDeclared
		}
		sc.Status.CP = fmt.Sprintf("%d/%d", cpReady, cpDeclared)
	} else {
		sc.Status.CP = "-/-"
	}
	if wkrMax := int(sc.Spec.Workers.MaxNodes()); wkrMax > wkrDeclared {
		sc.Status.WKR = fmt.Sprintf("%d/%d-%d", wkrReady, wkrDeclared, wkrMax)
	} else {
		sc.Status.WKR = fmt.Sprintf("%d/%d", wkrReady, wkrDeclared)
	}
}

// setAutoscalingCondition reports whether the chosen SCPC honors the SC's
// workers.autoscaling request. The condition exists only while the SC asks
// for autoscaling; SCs without it (or with the legacy fixed count) carry no
// AutoscalingSupported condition at all.
func setAutoscalingCondition(
	sc *showroomv1alpha1.SimpleCluster, scpc *showroomv1alpha1.SimpleClusterProviderConfig,
) {
	if !sc.Spec.Workers.AutoscalingEnabled() {
		apimeta.RemoveStatusCondition(&sc.Status.Conditions, showroomv1alpha1.ConditionAutoscalingSupported)
		return
	}
	cond := metav1.Condition{
		Type:   showroomv1alpha1.ConditionAutoscalingSupported,
		Status: metav1.ConditionTrue,
		Reason: "ProviderAutoscaling",
		Message: fmt.Sprintf("provider scales workers between %d and %d",
			sc.Spec.Workers.MinNodes(), sc.Spec.Workers.MaxNodes()),
	}
	if !scpc.Spec.Autoscaling {
		cond.Status = metav1.ConditionFalse
		cond.Reason = "UnsupportedByProvider"
		cond.Message = fmt.Sprintf("SCPC %q does not support autoscaling; worker pool fixed at %d",
			scpc.Name, sc.Spec.Workers.MinNodes())
	}
	apimeta.SetStatusCondition(&sc.Status.Conditions, cond)
}

// validateAndTopoSort validates the SCPC DAG and returns a topological
// ordering of ResourceSpec names. Errors are user-facing (used in
// ConditionError messages).
func validateAndTopoSort(scpc *showroomv1alpha1.SimpleClusterProviderConfig) ([]string, error) {
	if len(scpc.Spec.Resources) == 0 {
		return nil, fmt.Errorf("spec.resources must not be empty")
	}

	names := make(map[string]int, len(scpc.Spec.Resources))
	for i, rs := range scpc.Spec.Resources {
		if _, dup := names[rs.Name]; dup {
			return nil, fmt.Errorf("duplicate resource name %q", rs.Name)
		}
		names[rs.Name] = i
		if _, err := schema.ParseGroupVersion(rs.APIVersion); err != nil {
			return nil, fmt.Errorf("resource %q: bad apiVersion %q: %w", rs.Name, rs.APIVersion, err)
		}
		if rs.Kind == "" {
			return nil, fmt.Errorf("resource %q: kind is required", rs.Name)
		}
		if err := validateReadiness(rs.Readiness); err != nil {
			return nil, fmt.Errorf("resource %q: invalid readiness: %w", rs.Name, err)
		}
		if err := validateDeletionAnnotations(rs.DeletionAnnotations); err != nil {
			return nil, fmt.Errorf("resource %q: invalid deletionAnnotations: %w", rs.Name, err)
		}
	}

	if scpc.Spec.KubeconfigSource.ResourceName == "" {
		return nil, fmt.Errorf("kubeconfigSource.resourceName is required")
	}
	if _, ok := names[scpc.Spec.KubeconfigSource.ResourceName]; !ok {
		return nil, fmt.Errorf("kubeconfigSource.resourceName %q does not match any resource",
			scpc.Spec.KubeconfigSource.ResourceName)
	}

	for _, rs := range scpc.Spec.Resources {
		seen := make(map[string]bool, len(rs.DependsOn))
		for _, dep := range rs.DependsOn {
			if dep == rs.Name {
				return nil, fmt.Errorf("resource %q: self-reference in dependsOn", rs.Name)
			}
			if _, ok := names[dep]; !ok {
				return nil, fmt.Errorf("resource %q: dependsOn refers to unknown resource %q", rs.Name, dep)
			}
			if seen[dep] {
				return nil, fmt.Errorf("resource %q: duplicate dependsOn entry %q", rs.Name, dep)
			}
			seen[dep] = true
		}
	}

	// Topological sort using Kahn's algorithm, processing in declaration
	// order to keep output deterministic.
	inDeg := make(map[string]int, len(scpc.Spec.Resources))
	for _, rs := range scpc.Spec.Resources {
		inDeg[rs.Name] = len(rs.DependsOn)
	}
	successors := make(map[string][]string, len(scpc.Spec.Resources))
	for _, rs := range scpc.Spec.Resources {
		for _, dep := range rs.DependsOn {
			successors[dep] = append(successors[dep], rs.Name)
		}
	}

	queue := make([]string, 0)
	for _, rs := range scpc.Spec.Resources {
		if inDeg[rs.Name] == 0 {
			queue = append(queue, rs.Name)
		}
	}
	order := make([]string, 0, len(scpc.Spec.Resources))
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		order = append(order, name)
		for _, succ := range successors[name] {
			inDeg[succ]--
			if inDeg[succ] == 0 {
				queue = append(queue, succ)
			}
		}
	}
	if len(order) != len(scpc.Spec.Resources) {
		return nil, fmt.Errorf("cycle detected in resource dependsOn")
	}
	return order, nil
}

func validateDeletionAnnotations(annotations map[string]string) error {
	reserved := map[string]struct{}{
		rendertpl.AnnoSimpleCluster:                  {},
		rendertpl.AnnoSimpleClusterProvider:          {},
		rendertpl.AnnoSimpleClusterUID:               {},
		rendertpl.AnnoSimpleClusterProviderNamespace: {},
		rendertpl.AnnoSimpleClusterProviderUID:       {},
		rendertpl.AnnoAllowDelete:                    {},
	}
	for key := range annotations {
		if _, found := reserved[key]; found {
			return fmt.Errorf("annotation %q is reserved by the SimpleCluster lifecycle", key)
		}
	}
	return nil
}

func validateReadiness(check *showroomv1alpha1.ReadinessCheck) error {
	if check == nil {
		return nil
	}

	if check.Conditions == nil {
		if check.PhasePath == "" || check.PhaseReadyValue == "" {
			return fmt.Errorf("phasePath and phaseReadyValue are required for scalar readiness")
		}
		return nil
	}

	if check.PhasePath != "" || check.PhaseReadyValue != "" || len(check.PhaseFailedValues) > 0 ||
		check.LastOperationPath != "" || check.LastErrorPath != "" {
		return fmt.Errorf("conditions cannot be combined with scalar readiness fields")
	}

	conditions := check.Conditions
	if err := validateConditionMatch("ready", conditions.Ready); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(conditions.Failures))
	for i, failure := range conditions.Failures {
		if err := validateConditionMatch(fmt.Sprintf("failures[%d]", i), failure); err != nil {
			return err
		}
		if _, duplicate := seen[failure.Type]; duplicate {
			return fmt.Errorf("duplicate failure condition type %q", failure.Type)
		}
		seen[failure.Type] = struct{}{}
		if failure == conditions.Ready {
			return fmt.Errorf("ready matcher must not also be a failure matcher")
		}
	}
	return nil
}

func validateConditionMatch(field string, match showroomv1alpha1.ConditionMatch) error {
	if strings.TrimSpace(match.Type) == "" {
		return fmt.Errorf("conditions.%s.type is required", field)
	}
	switch match.Status {
	case metav1.ConditionTrue, metav1.ConditionFalse, metav1.ConditionUnknown:
		return nil
	default:
		return fmt.Errorf("conditions.%s.status %q must be True, False, or Unknown", field, match.Status)
	}
}

// aggregateResource folds N InstanceStatus entries into one ResourceStatus.
// For non-repeated specs (single instance), the top-level TargetName/
// TargetNamespace mirror the single instance's fields and Instances is
// left nil. For repeated specs, the top-level Phase is aggregated across
// all instances and Instances carries the per-index detail.
func aggregateResource(name, gvk string, repeated bool, instances []showroomv1alpha1.InstanceStatus) *showroomv1alpha1.ResourceStatus {
	out := &showroomv1alpha1.ResourceStatus{Name: name, GVK: gvk}
	if !repeated {
		i := instances[0]
		out.TargetName = i.TargetName
		out.TargetNamespace = i.TargetNamespace
		out.Phase = i.Phase
		out.LastOperation = i.LastOperation
		out.LastError = i.LastError
		out.Reason, out.Message = reasonFor(i.Phase, i.LastOperation, i.LastError)
		return out
	}

	anyFailed, anyPending, anyProvisioning, ready := false, false, false, 0
	var firstErr string
	for _, i := range instances {
		switch i.Phase {
		case showroomv1alpha1.PhaseReady:
			ready++
		case showroomv1alpha1.PhaseFailed:
			anyFailed = true
			if firstErr == "" {
				firstErr = i.LastError
			}
		case showroomv1alpha1.PhasePending:
			anyPending = true
		default:
			anyProvisioning = true
		}
	}
	total := len(instances)
	switch {
	case anyFailed:
		out.Phase = showroomv1alpha1.PhaseFailed
		out.LastError = firstErr
	case ready == total:
		out.Phase = showroomv1alpha1.PhaseReady
	case anyProvisioning:
		out.Phase = showroomv1alpha1.PhaseProvisioning
	case anyPending:
		out.Phase = showroomv1alpha1.PhasePending
	default:
		out.Phase = showroomv1alpha1.PhaseProvisioning
	}
	out.LastOperation = fmt.Sprintf("%d/%d instances Ready", ready, total)
	out.Reason, out.Message = reasonFor(out.Phase, out.LastOperation, out.LastError)
	out.Instances = instances
	return out
}

func aggregatePhase(order []string, statuses map[string]*showroomv1alpha1.ResourceStatus) showroomv1alpha1.Phase {
	anyFailed := false
	allReady := true
	for _, name := range order {
		s := statuses[name]
		if s == nil {
			allReady = false
			continue
		}
		switch s.Phase {
		case showroomv1alpha1.PhaseFailed:
			anyFailed = true
		case showroomv1alpha1.PhaseReady:
		default:
			allReady = false
		}
	}
	switch {
	case anyFailed:
		return showroomv1alpha1.PhaseFailed
	case allReady:
		return showroomv1alpha1.PhaseReady
	default:
		return showroomv1alpha1.PhaseProvisioning
	}
}

func aggregateMessage(order []string, statuses map[string]*showroomv1alpha1.ResourceStatus) string {
	parts := make([]string, 0, len(order))
	for _, name := range order {
		s := statuses[name]
		if s == nil {
			continue
		}
		if s.Phase == showroomv1alpha1.PhaseReady {
			continue
		}
		detail := s.Message
		if detail == "" {
			detail = string(s.Phase)
		}
		parts = append(parts, fmt.Sprintf("%s: %s", name, detail))
	}
	if len(parts) == 0 {
		return "all resources Ready"
	}
	return strings.Join(parts, "; ")
}

// reasonFor maps an extractPhase output onto a ResourceReason + human
// message for the SC's per-resource status. PhasePending here means R
// exists but has not yet surfaced a Ready signal — call it Creating
// (WaitingForDependencies is set at the DAG-walk step, never here).
func reasonFor(phase showroomv1alpha1.Phase, lastOp, lastErr string) (showroomv1alpha1.ResourceReason, string) {
	switch phase {
	case showroomv1alpha1.PhaseReady:
		return showroomv1alpha1.ReasonReady, lastOp
	case showroomv1alpha1.PhaseFailed:
		if lastErr != "" {
			return showroomv1alpha1.ReasonFailed, lastErr
		}
		return showroomv1alpha1.ReasonFailed, lastOp
	case showroomv1alpha1.PhaseProvisioning:
		return showroomv1alpha1.ReasonProvisioning, lastOp
	case showroomv1alpha1.PhasePending:
		msg := lastOp
		if msg == "" {
			msg = "resource created; awaiting controller to report Ready"
		}
		return showroomv1alpha1.ReasonCreating, msg
	}
	return "", ""
}

func orderedStatuses(order []string, statuses map[string]*showroomv1alpha1.ResourceStatus) []showroomv1alpha1.ResourceStatus {
	out := make([]showroomv1alpha1.ResourceStatus, 0, len(order))
	for _, name := range order {
		s := statuses[name]
		if s == nil {
			out = append(out, showroomv1alpha1.ResourceStatus{Name: name, Phase: showroomv1alpha1.PhasePending})
		} else {
			out = append(out, *s)
		}
	}
	return out
}

// completedResourceStatuses returns only rows computed in the current pass, in
// DAG order. Unlike orderedStatuses it does not invent Pending rows for
// resources the pass has not reached.
func completedResourceStatuses(order []string, statuses map[string]*showroomv1alpha1.ResourceStatus) []showroomv1alpha1.ResourceStatus {
	out := make([]showroomv1alpha1.ResourceStatus, 0, len(statuses))
	for _, name := range order {
		if status := statuses[name]; status != nil {
			out = append(out, *status)
		}
	}
	return out
}

// extractPhase maps R's status into the universal Phase enum, either via
// the resource's ReadinessCheck or the default status.phase mapping.
func extractPhase(rc *showroomv1alpha1.ReadinessCheck, obj *unstructured.Unstructured) (showroomv1alpha1.Phase, string, string) {
	if rc != nil && rc.Conditions != nil {
		return extractConditionPhase(rc.Conditions, obj)
	}

	if rc != nil && rc.PhasePath != "" {
		state, _ := pathwalk.Walk(obj.Object, rc.PhasePath)
		s := pathwalk.ToString(state)
		lastOp := pathwalk.String(obj.Object, rc.LastOperationPath)
		switch {
		case s == rc.PhaseReadyValue:
			return showroomv1alpha1.PhaseReady, lastOp, ""
		case stringIn(s, rc.PhaseFailedValues):
			return showroomv1alpha1.PhaseFailed, lastOp, pathwalk.String(obj.Object, rc.LastErrorPath)
		case s == "":
			return showroomv1alpha1.PhasePending, lastOp, ""
		}
		return showroomv1alpha1.PhaseProvisioning, lastOp, ""
	}

	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	msg, _, _ := unstructured.NestedString(obj.Object, "status", "message")
	switch phase {
	case "":
		return showroomv1alpha1.PhasePending, msg, ""
	case "Ready":
		return showroomv1alpha1.PhaseReady, msg, ""
	case "Failed", "Error":
		return showroomv1alpha1.PhaseFailed, msg, msg
	}
	return showroomv1alpha1.PhaseProvisioning, msg, ""
}

// extractConditionPhase selects Kubernetes-style conditions by their exact
// type instead of relying on the order of status.conditions. Current failure
// conditions take precedence over readiness so an inconsistent Ready=True,
// Synced=False object cannot be reported as healthy.
func extractConditionPhase(rc *showroomv1alpha1.ConditionReadinessCheck, obj *unstructured.Unstructured) (showroomv1alpha1.Phase, string, string) {
	conditions := currentConditions(obj, rc.MatchObservedGeneration)
	ready, readyFound := conditions[rc.Ready.Type]
	lastOp := ""
	if readyFound {
		lastOp = conditionDetail(ready)
	}

	type matchedFailure struct {
		conditionType string
		detail        string
	}
	matched := make([]matchedFailure, 0, len(rc.Failures))
	for _, matcher := range rc.Failures {
		condition, found := conditions[matcher.Type]
		if !found || condition.Status != matcher.Status {
			continue
		}
		matched = append(matched, matchedFailure{
			conditionType: condition.Type,
			detail:        conditionDetail(condition),
		})
	}
	if len(matched) > 0 {
		// Failures is an associative list in the CRD, so do not attach message
		// priority to its serialized order. Sort by type for stable status.
		sort.Slice(matched, func(i, j int) bool {
			return matched[i].conditionType < matched[j].conditionType
		})
		details := make([]string, 0, len(matched))
		for _, failure := range matched {
			details = append(details, failure.detail)
		}
		return showroomv1alpha1.PhaseFailed, lastOp, strings.Join(details, "; ")
	}

	if !readyFound {
		return showroomv1alpha1.PhasePending, "", ""
	}
	if ready.Status == rc.Ready.Status {
		return showroomv1alpha1.PhaseReady, lastOp, ""
	}
	return showroomv1alpha1.PhaseProvisioning, lastOp, ""
}

// currentConditions converts a target's status.conditions into a map keyed by
// condition type. When generation matching is explicitly enabled, a non-zero
// observedGeneration must match the target's generation; zero remains
// compatible with controllers that omit it. The default deliberately ignores
// generation because cross-cluster sync copies provider conditions into a
// different metadata.generation domain. Duplicate current condition types are
// ambiguous and are omitted rather than making readiness depend on list order.
func currentConditions(obj *unstructured.Unstructured, matchObservedGeneration bool) map[string]metav1.Condition {
	raw, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found {
		return nil
	}

	conditions := make(map[string]metav1.Condition, len(raw))
	ambiguous := make(map[string]struct{})
	for _, entry := range raw {
		fields, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		var condition metav1.Condition
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(fields, &condition); err != nil {
			continue
		}
		if condition.Type == "" || (matchObservedGeneration && condition.ObservedGeneration != 0 && condition.ObservedGeneration != obj.GetGeneration()) {
			continue
		}
		if _, duplicate := conditions[condition.Type]; duplicate {
			delete(conditions, condition.Type)
			ambiguous[condition.Type] = struct{}{}
			continue
		}
		if _, duplicate := ambiguous[condition.Type]; duplicate {
			continue
		}
		conditions[condition.Type] = condition
	}
	return conditions
}

func conditionDetail(condition metav1.Condition) string {
	if condition.Message != "" {
		return condition.Message
	}
	if condition.Reason != "" {
		return condition.Reason
	}
	return fmt.Sprintf("%s=%s", condition.Type, condition.Status)
}

func stringIn(s string, list []string) bool {
	for _, x := range list {
		if s == x {
			return true
		}
	}
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *SimpleClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&showroomv1alpha1.SimpleCluster{}).
		Named("simplecluster").
		Complete(r)
}
