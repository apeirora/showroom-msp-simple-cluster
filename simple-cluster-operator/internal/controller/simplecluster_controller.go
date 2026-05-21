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
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/kubeconfig"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/pathwalk"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/target"
	rendertpl "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/template"
)

const (
	scRequeueReady   = 1 * time.Minute
	scRequeueWorking = 30 * time.Second
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
		scpcRef = sc.Spec.ProviderConfigRef
	}
	if scpcRef == nil || scpcRef.Name == "" {
		return r.failSC(ctx, &sc, "InvalidSpec", "providerConfigRef is required")
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
	live := make(map[string]*unstructured.Unstructured, len(order))

	for _, name := range order {
		rs := resourceByName[name]

		predecessors := make(map[string]*unstructured.Unstructured, len(rs.DependsOn))
		allReady := true
		for _, dep := range rs.DependsOn {
			depStatus := statuses[dep]
			if depStatus == nil || depStatus.Phase != showroomv1alpha1.PhaseReady {
				allReady = false
				break
			}
			predecessors[dep] = live[dep]
		}
		if !allReady {
			statuses[name] = &showroomv1alpha1.ResourceStatus{
				Name:  name,
				Phase: showroomv1alpha1.PhasePending,
			}
			continue
		}

		desired, rerr := rendertpl.Render(&scpc, &sc, rs, predecessors)
		if rerr != nil {
			return r.failSC(ctx, &sc, "RenderError",
				fmt.Sprintf("resource %q: %v", name, rerr))
		}

		gvk := desired.GroupVersionKind()
		mapping, mperr := tc.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if mperr != nil {
			return r.failSC(ctx, &sc, "GVKNotFound",
				fmt.Sprintf("resource %q: RESTMapping %s: %v", name, gvk, mperr))
		}
		ri := tc.ResourceFor(mapping, desired.GetNamespace())

		existing, gerr := ri.Get(ctx, desired.GetName(), metav1.GetOptions{})
		switch {
		case apierrors.IsNotFound(gerr):
			created, cerr := ri.Create(ctx, desired, metav1.CreateOptions{})
			if cerr != nil {
				return r.failSC(ctx, &sc, "CreateError",
					fmt.Sprintf("resource %q: %v", name, cerr))
			}
			log.Info("created target resource", "resource", name, "gvk", gvk.String(),
				"name", created.GetName(), "namespace", created.GetNamespace())
			existing = created
		case gerr != nil:
			return r.failSC(ctx, &sc, "GetTargetError",
				fmt.Sprintf("resource %q: %v", name, gerr))
		}
		// v0.x: do not patch existing R. Drift requires user to delete R.

		phase, lastOp, lastErr := extractPhase(rs.Readiness, existing)
		statuses[name] = &showroomv1alpha1.ResourceStatus{
			Name:            name,
			GVK:             gvk.String(),
			TargetName:      existing.GetName(),
			TargetNamespace: existing.GetNamespace(),
			Phase:           phase,
			LastOperation:   lastOp,
			LastError:       lastErr,
		}
		live[name] = existing
	}

	aggregate := aggregatePhase(order, statuses)

	patch := client.MergeFrom(sc.DeepCopy())
	if sc.Status.ProviderConfigRef == nil {
		sc.Status.ProviderConfigRef = sc.Spec.ProviderConfigRef.DeepCopy()
	}
	sc.Status.TargetCluster = "local"
	if scpc.Spec.Target.KubeconfigSecretRef != nil {
		sc.Status.TargetCluster = scpc.Name
	}
	sc.Status.Resources = orderedStatuses(order, statuses)
	sc.Status.Phase = aggregate
	sc.Status.ObservedGeneration = sc.Generation

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
	if kcLive, ok := live[kcName]; ok {
		if kcStatus := statuses[kcName]; kcStatus != nil && kcStatus.Phase == showroomv1alpha1.PhaseReady {
			if kubeconfig.NeedsRefresh(&sc, &scpc) {
				r.syncKubeconfig(ctx, &sc, &scpc, resourceByName[kcName], kcLive, tc)
			}
		}
	}

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

	res, eerr := kubeconfig.EnsureSecret(ctx, r.Client, r.Scheme, sc, data, sourceExpiry)
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

// reconcileDelete cascades the SC deletion by walking the SCPC DAG in
// reverse topological order, applying each resource's deletionAnnotations
// and deleting R, then removing the finalizer. The kubeconfig Secret is
// garbage-collected by Kubernetes via its OwnerReference on SC.
func (r *SimpleClusterReconciler) reconcileDelete(ctx context.Context, sc *showroomv1alpha1.SimpleCluster) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(sc, showroomv1alpha1.FinalizerName) {
		return ctrl.Result{}, nil
	}

	scpcRef := sc.Status.ProviderConfigRef
	if scpcRef == nil {
		scpcRef = sc.Spec.ProviderConfigRef
	}

	if scpcRef != nil && scpcRef.Name != "" {
		if err := r.deleteTargetResources(ctx, sc, scpcRef.Name); err != nil {
			log.Error(err, "delete target resources failed; will retry")
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
// deletes each resource. NotFound is treated as success.
func (r *SimpleClusterReconciler) deleteTargetResources(ctx context.Context, sc *showroomv1alpha1.SimpleCluster, scpcName string) error {
	log := logf.FromContext(ctx)

	var scpc showroomv1alpha1.SimpleClusterProviderConfig
	if err := r.Get(ctx, types.NamespacedName{Name: scpcName}, &scpc); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("SCPC no longer present; skipping remote delete", "scpc", scpcName)
			return nil
		}
		return fmt.Errorf("get SCPC %s: %w", scpcName, err)
	}

	order, err := validateAndTopoSort(&scpc)
	if err != nil {
		// SCPC has gone invalid since creation; best-effort delete by status.
		log.Error(err, "SCPC invalid during delete; falling back to status-tracked order")
		order = orderFromStatus(sc)
	}

	tc, err := target.New(ctx, r.Client, r.RESTConfig, r.RESTMapper(), scpc.Spec.Target.KubeconfigSecretRef)
	if err != nil {
		return fmt.Errorf("build target client: %w", err)
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
			return fmt.Errorf("resource %q: parse apiVersion: %w", name, gerr)
		}
		gvk := gv.WithKind(rs.Kind)
		mapping, mperr := tc.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if mperr != nil {
			return fmt.Errorf("resource %q: RESTMapping %s: %w", name, gvk, mperr)
		}

		// Prefer the actual name/namespace recorded in SC.Status, fall back
		// to the computed values when status is missing.
		var targetName, targetNamespace string
		if s := statusByName[name]; s != nil {
			targetName = s.TargetName
			targetNamespace = s.TargetNamespace
		}
		if targetName == "" {
			targetName = rendertpl.SanitiseTargetName(rs.NamePrefix, sc.Name+"-"+name)
		}
		if targetNamespace == "" {
			targetNamespace = rs.Namespace
			if targetNamespace == "" {
				targetNamespace = scpc.Spec.Target.Namespace
			}
		}
		ri := tc.ResourceFor(mapping, targetNamespace)

		if len(rs.DeletionAnnotations) > 0 {
			patchBytes, perr := json.Marshal(map[string]any{
				"metadata": map[string]any{"annotations": rs.DeletionAnnotations},
			})
			if perr != nil {
				return fmt.Errorf("resource %q: marshal deletionAnnotations: %w", name, perr)
			}
			_, perr = ri.Patch(ctx, targetName, types.MergePatchType, patchBytes, metav1.PatchOptions{})
			if perr != nil && !apierrors.IsNotFound(perr) {
				return fmt.Errorf("resource %q: patch deletionAnnotations: %w", name, perr)
			}
		}

		if err := ri.Delete(ctx, targetName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("resource %q: delete %s: %w", name, targetName, err)
		}
		log.Info("target resource delete requested",
			"resource", name, "name", targetName, "gvk", gvk.String())
	}
	return nil
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
	if err := r.Status().Patch(ctx, sc, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: scRequeueWorking}, nil
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
	var parts []string
	for _, name := range order {
		s := statuses[name]
		if s == nil {
			continue
		}
		switch s.Phase {
		case showroomv1alpha1.PhaseFailed:
			parts = append(parts, fmt.Sprintf("%s: %s", name, s.LastError))
		case showroomv1alpha1.PhaseReady:
		default:
			parts = append(parts, fmt.Sprintf("%s: %s", name, s.Phase))
		}
	}
	if len(parts) == 0 {
		return "all resources Ready"
	}
	return strings.Join(parts, "; ")
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

	conds, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, c := range conds {
		cm, ok := c.(map[string]any)
		if !ok || cm["type"] != "Ready" {
			continue
		}
		status, _ := cm["status"].(string)
		msg, _ := cm["message"].(string)
		switch status {
		case "True":
			return showroomv1alpha1.PhaseReady, msg, ""
		case "False":
			reason, _ := cm["reason"].(string)
			if isTerminalReason(reason) {
				return showroomv1alpha1.PhaseFailed, msg, msg
			}
			return showroomv1alpha1.PhaseProvisioning, msg, ""
		}
	}
	return showroomv1alpha1.PhasePending, "", ""
}

func isTerminalReason(reason string) bool {
	switch reason {
	case "Failed", "Error", "InvalidConfiguration":
		return true
	}
	return false
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
