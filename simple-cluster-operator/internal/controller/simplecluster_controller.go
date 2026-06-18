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

// SimpleClusterReconciler walks the SCPC's DAG of ResourceSpecs and
// mirrors per-resource state into SC.Status.Resources. See DESIGN.md §5.
type SimpleClusterReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	RESTConfig *rest.Config
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
// +kubebuilder:rbac:groups=arsys.apeirora.eu,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=openstack.apeirora.eu,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ubuntu.apeirora.eu,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ubuntumachines.apeirora.eu,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kind.apeirora.eu,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.gardener.cloud,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=authentication.gardener.cloud,resources=*,verbs=get;list;watch;create;update;patch;delete

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

	scpcRef := sc.Status.ProviderConfigRef
	if scpcRef == nil {
		chosen := selectProvider(sc.Spec.ProviderConfigRefs)
		if chosen == "" {
			return r.failSC(ctx, &sc, "InvalidSpec", "providerConfigRefs must list at least one SCPC name")
		}
		scpcRef = &corev1.LocalObjectReference{Name: chosen}
	}

	var scpc showroomv1alpha1.SimpleClusterProviderConfig
	if err := r.Get(ctx, types.NamespacedName{Name: scpcRef.Name}, &scpc); err != nil {
		if apierrors.IsNotFound(err) {
			return r.failSC(ctx, &sc, "SCPCNotFound",
				fmt.Sprintf("SimpleClusterProviderConfig %q not found", scpcRef.Name))
		}
		return ctrl.Result{}, err
	}

	order, err := validateAndTopoSort(&scpc)
	if err != nil {
		return r.failSC(ctx, &sc, "InvalidSCPC", err.Error())
	}

	// Resolve inputs up front so a from=profile input with no fitting profile
	// parks the SC in Phase=NoHardware before any resource R is created.
	if _, err := rendertpl.ResolveInputs(&scpc, &sc); err != nil {
		if errors.Is(err, rendertpl.ErrNoProfile) {
			return r.noHardwareSC(ctx, &sc, scpc.Name, err.Error())
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

		statuses[name] = aggregateResource(name, gvkStr, repeated, instances)
		live[name] = instanceLive
	}

	aggregate := aggregatePhase(order, statuses)

	patch := client.MergeFrom(sc.DeepCopy())
	if sc.Status.ProviderConfigRef == nil {
		sc.Status.ProviderConfigRef = &corev1.LocalObjectReference{Name: scpc.Name}
	}
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

	scpcRef := sc.Status.ProviderConfigRef
	if scpcRef == nil {
		if name := selectProvider(sc.Spec.ProviderConfigRefs); name != "" {
			scpcRef = &corev1.LocalObjectReference{Name: name}
		}
	}

	if scpcRef != nil && scpcRef.Name != "" {
		done, err := r.deleteTargetResources(ctx, sc, scpcRef.Name)
		if err != nil {
			log.Error(err, "delete target resources failed; will retry")
			return ctrl.Result{RequeueAfter: scRequeueWorking}, nil
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
func (r *SimpleClusterReconciler) deleteTargetResources(ctx context.Context, sc *showroomv1alpha1.SimpleCluster, scpcName string) (bool, error) {
	log := logf.FromContext(ctx)

	var scpc showroomv1alpha1.SimpleClusterProviderConfig
	if err := r.Get(ctx, types.NamespacedName{Name: scpcName}, &scpc); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("SCPC no longer present; skipping remote delete", "scpc", scpcName)
			return true, nil
		}
		return false, fmt.Errorf("get SCPC %s: %w", scpcName, err)
	}

	order, err := validateAndTopoSort(&scpc)
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

		defaultNs, nerr := rendertpl.ResolveNamespace(&scpc, sc, rs)
		if nerr != nil {
			return false, fmt.Errorf("resource %q: resolve namespace: %w", name, nerr)
		}

		// Enumerate the (name, namespace) pairs to delete. For repeated
		// specs, Instances carries one entry per index. For non-repeated,
		// the top-level TargetName is the single name. Fall back to the
		// computed base name when status is missing entirely.
		type nsName struct{ name, namespace string }
		var targets []nsName
		s := statusByName[name]
		switch {
		case s != nil && len(s.Instances) > 0:
			for _, inst := range s.Instances {
				ns := inst.TargetNamespace
				if ns == "" {
					ns = defaultNs
				}
				targets = append(targets, nsName{inst.TargetName, ns})
			}
		case s != nil && s.TargetName != "":
			ns := s.TargetNamespace
			if ns == "" {
				ns = defaultNs
			}
			targets = []nsName{{s.TargetName, ns}}
		default:
			targets = []nsName{{
				rendertpl.SanitiseTargetName(rs.NamePrefix, sc.Name+"-"+name),
				defaultNs,
			}}
		}

		levelDone := true
		for _, t := range targets {
			ri := tc.ResourceFor(mapping, t.namespace)
			exists, derr := r.deleteOneTarget(ctx, ri, rs, gvk, name, t.name)
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
	}
	return true, nil
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
	resourceName, targetName string,
) (bool, error) {
	log := logf.FromContext(ctx)

	existing, err := ri.Get(ctx, targetName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("resource %q: get %s: %w", resourceName, targetName, err)
	}

	// First time we see this target after SC was marked for deletion: patch
	// deletionAnnotations (Gardener-style opt-in) and issue Delete. Subsequent
	// reconciles skip the work — a non-zero deletionTimestamp means our prior
	// Delete already landed.
	if existing.GetDeletionTimestamp().IsZero() {
		if len(rs.DeletionAnnotations) > 0 {
			patchBytes, perr := json.Marshal(map[string]any{
				"metadata": map[string]any{"annotations": rs.DeletionAnnotations},
			})
			if perr != nil {
				return true, fmt.Errorf("resource %q: marshal deletionAnnotations: %w", resourceName, perr)
			}
			if _, perr := ri.Patch(ctx, targetName, types.MergePatchType, patchBytes, metav1.PatchOptions{}); perr != nil && !apierrors.IsNotFound(perr) {
				return true, fmt.Errorf("resource %q: patch deletionAnnotations: %w", resourceName, perr)
			}
		}
		if derr := ri.Delete(ctx, targetName, metav1.DeleteOptions{}); derr != nil && !apierrors.IsNotFound(derr) {
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
		patchBytes, perr := json.Marshal(map[string]any{
			"metadata": map[string]any{"finalizers": []string{}},
		})
		if perr != nil {
			return true, fmt.Errorf("resource %q: marshal finalizer strip: %w", resourceName, perr)
		}
		if _, perr := ri.Patch(ctx, targetName, types.MergePatchType, patchBytes, metav1.PatchOptions{}); perr != nil && !apierrors.IsNotFound(perr) {
			return true, fmt.Errorf("resource %q: force-strip finalizers: %w", resourceName, perr)
		}
		log.Info("force-stripped finalizers after grace window",
			"resource", resourceName, "name", targetName, "gvk", gvk.String(),
			"grace", forceCleanupGrace)
	}

	return true, nil
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

// noHardwareSC parks the SC in Phase=NoHardware: a from=profile input could
// not be satisfied by any SCPC profile. No resource R is created. The SC is
// re-queued so it recovers automatically once a large-enough profile is added
// or the request shrinks.
func (r *SimpleClusterReconciler) noHardwareSC(
	ctx context.Context, sc *showroomv1alpha1.SimpleCluster, scpcName, msg string,
) (ctrl.Result, error) {
	patch := client.MergeFrom(sc.DeepCopy())
	if sc.Status.ProviderConfigRef == nil {
		sc.Status.ProviderConfigRef = &corev1.LocalObjectReference{Name: scpcName}
	}
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
func setRoleCounts(sc *showroomv1alpha1.SimpleCluster) {
	wkrDeclared := int(sc.Spec.Workers.Nodes)
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
	sc.Status.WKR = fmt.Sprintf("%d/%d", wkrReady, wkrDeclared)
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

// extractPhase maps R's status into the universal Phase enum, either via
// the resource's ReadinessCheck or the default Ready-condition walker.
// See DESIGN.md §5.
func extractPhase(rc *showroomv1alpha1.ReadinessCheck, obj *unstructured.Unstructured) (showroomv1alpha1.Phase, string, string) {
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
