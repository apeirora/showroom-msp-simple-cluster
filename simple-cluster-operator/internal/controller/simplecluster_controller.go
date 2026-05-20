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
	"strconv"
	"strings"
	"time"

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
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/target"
	rendertpl "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/template"
)

const (
	scRequeueReady   = 1 * time.Minute
	scRequeueWorking = 30 * time.Second
)

// SimpleClusterReconciler resolves an SC + SCPC pair into a concrete
// target resource R, then mirrors R's coarse phase back into SC.Status.
// See DESIGN.md §5.
type SimpleClusterReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	RESTConfig *rest.Config
}

// +kubebuilder:rbac:groups=showroom.apeirora.eu,resources=simpleclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=showroom.apeirora.eu,resources=simpleclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=showroom.apeirora.eu,resources=simpleclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=showroom.apeirora.eu,resources=simpleclusterproviderconfigs,verbs=get;list;watch

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
		// fall through; the patched object is in-memory consistent
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

	targetName := rendertpl.SanitiseTargetName(scpc.Spec.Target.NamePrefix, sc.Name)
	desired, err := rendertpl.Render(&scpc, &sc, targetName)
	if err != nil {
		return r.failSC(ctx, &sc, "RenderError", err.Error())
	}

	tc, err := target.New(ctx, r.Client, r.RESTConfig, r.RESTMapper(), scpc.Spec.Target.KubeconfigSecretRef)
	if err != nil {
		return r.failSC(ctx, &sc, "TargetClientError", err.Error())
	}

	gvk := desired.GroupVersionKind()
	mapping, err := tc.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return r.failSC(ctx, &sc, "GVKNotFound",
			fmt.Sprintf("RESTMapping %s: %v", gvk, err))
	}
	resource := tc.ResourceFor(mapping, desired.GetNamespace())

	existing, err := resource.Get(ctx, desired.GetName(), metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		created, cerr := resource.Create(ctx, desired, metav1.CreateOptions{})
		if cerr != nil {
			return r.failSC(ctx, &sc, "CreateError", cerr.Error())
		}
		log.Info("created target resource", "gvk", gvk.String(), "name", created.GetName(), "namespace", created.GetNamespace())
		existing = created
	case err != nil:
		return r.failSC(ctx, &sc, "GetTargetError", err.Error())
	default:
		// v0.1: do not patch existing R. Drift requires user to delete R.
	}

	phase, lastOp, lastErr := extractPhase(scpc.Spec.StatusMap, existing)

	patch := client.MergeFrom(sc.DeepCopy())
	if sc.Status.ProviderConfigRef == nil {
		sc.Status.ProviderConfigRef = sc.Spec.ProviderConfigRef.DeepCopy()
	}
	sc.Status.TargetGVK = gvk.String()
	sc.Status.TargetName = desired.GetName()
	sc.Status.TargetNamespace = desired.GetNamespace()
	sc.Status.TargetCluster = "local"
	if scpc.Spec.Target.KubeconfigSecretRef != nil {
		sc.Status.TargetCluster = scpc.Name
	}
	sc.Status.Phase = phase
	sc.Status.LastOperation = lastOp
	sc.Status.LastError = lastErr
	sc.Status.ObservedGeneration = sc.Generation

	cond := metav1.Condition{
		Type:    showroomv1alpha1.ConditionReady,
		Status:  metav1.ConditionFalse,
		Reason:  string(phase),
		Message: lastOp,
	}
	if phase == showroomv1alpha1.PhaseReady {
		cond.Status = metav1.ConditionTrue
	}
	apimeta.SetStatusCondition(&sc.Status.Conditions, cond)
	// Clear any prior Error condition since we got through reconcile.
	apimeta.RemoveStatusCondition(&sc.Status.Conditions, showroomv1alpha1.ConditionError)

	if err := r.Status().Patch(ctx, &sc, patch); err != nil {
		return ctrl.Result{}, err
	}

	requeue := scRequeueReady
	if phase == showroomv1alpha1.PhaseFailed || phase == showroomv1alpha1.PhaseProvisioning {
		requeue = scRequeueWorking
	}
	return ctrl.Result{RequeueAfter: requeue}, nil
}

// reconcileDelete cascades the SC deletion: apply the SCPC's
// target.deletionAnnotations to R (e.g. Gardener's confirmation annotation),
// delete R on the target cluster, then remove the finalizer.
func (r *SimpleClusterReconciler) reconcileDelete(ctx context.Context, sc *showroomv1alpha1.SimpleCluster) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(sc, showroomv1alpha1.FinalizerName) {
		return ctrl.Result{}, nil
	}

	scpcRef := sc.Status.ProviderConfigRef
	if scpcRef == nil {
		scpcRef = sc.Spec.ProviderConfigRef
	}

	// Only attempt remote delete if we know what R was and where it lives.
	if scpcRef != nil && scpcRef.Name != "" && sc.Status.TargetName != "" {
		if err := r.deleteTargetResource(ctx, sc, scpcRef.Name); err != nil {
			log.Error(err, "delete target resource failed; will retry",
				"target", sc.Status.TargetName, "namespace", sc.Status.TargetNamespace)
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

// deleteTargetResource patches the SCPC-declared deletionAnnotations onto R
// (a no-op when the map is empty) and then deletes R. NotFound is treated
// as success.
func (r *SimpleClusterReconciler) deleteTargetResource(ctx context.Context, sc *showroomv1alpha1.SimpleCluster, scpcName string) error {
	log := logf.FromContext(ctx)

	var scpc showroomv1alpha1.SimpleClusterProviderConfig
	if err := r.Get(ctx, types.NamespacedName{Name: scpcName}, &scpc); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("SCPC no longer present; skipping remote delete", "scpc", scpcName)
			return nil
		}
		return fmt.Errorf("get SCPC %s: %w", scpcName, err)
	}

	tc, err := target.New(ctx, r.Client, r.RESTConfig, r.RESTMapper(), scpc.Spec.Target.KubeconfigSecretRef)
	if err != nil {
		return fmt.Errorf("build target client: %w", err)
	}

	gv, err := schema.ParseGroupVersion(scpc.Spec.Target.APIVersion)
	if err != nil {
		return fmt.Errorf("parse target apiVersion: %w", err)
	}
	gvk := gv.WithKind(scpc.Spec.Target.Kind)
	mapping, err := tc.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("RESTMapping %s: %w", gvk, err)
	}
	resource := tc.ResourceFor(mapping, sc.Status.TargetNamespace)

	if len(scpc.Spec.Target.DeletionAnnotations) > 0 {
		patchBytes, perr := json.Marshal(map[string]any{
			"metadata": map[string]any{"annotations": scpc.Spec.Target.DeletionAnnotations},
		})
		if perr != nil {
			return fmt.Errorf("marshal deletionAnnotations: %w", perr)
		}
		_, perr = resource.Patch(ctx, sc.Status.TargetName, types.MergePatchType, patchBytes, metav1.PatchOptions{})
		if perr != nil && !apierrors.IsNotFound(perr) {
			return fmt.Errorf("patch deletionAnnotations: %w", perr)
		}
	}

	if err := resource.Delete(ctx, sc.Status.TargetName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete %s: %w", sc.Status.TargetName, err)
	}
	log.Info("target resource delete requested", "name", sc.Status.TargetName, "gvk", gvk.String())
	return nil
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
	sc.Status.LastError = msg
	if err := r.Status().Patch(ctx, sc, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: scRequeueWorking}, nil
}

// extractPhase maps R's status into the universal Phase enum, either via
// SCPC.spec.statusMap or the default Ready-condition walker. See DESIGN.md §5.
func extractPhase(sm *showroomv1alpha1.StatusMap, obj *unstructured.Unstructured) (showroomv1alpha1.Phase, string, string) {
	if sm != nil && sm.PhasePath != "" {
		state, _ := walkPath(obj.Object, sm.PhasePath)
		s := toString(state)
		lastOp := walkString(obj.Object, sm.LastOperationPath)
		switch {
		case s == sm.PhaseReadyValue:
			return showroomv1alpha1.PhaseReady, lastOp, ""
		case stringIn(s, sm.PhaseFailedValues):
			return showroomv1alpha1.PhaseFailed, lastOp, walkString(obj.Object, sm.LastErrorPath)
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

// walkPath resolves a dotted path with optional `[N]` list indexing
// (e.g. "status.lastErrors[0].description") against an Unstructured object.
func walkPath(m map[string]any, path string) (any, bool) {
	if path == "" {
		return nil, false
	}
	var cur any = m
	for _, part := range strings.Split(path, ".") {
		key, idx, hasIdx := splitIndex(part)
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		val, ok := mm[key]
		if !ok {
			return nil, false
		}
		if !hasIdx {
			cur = val
			continue
		}
		arr, ok := val.([]any)
		if !ok || idx < 0 || idx >= len(arr) {
			return nil, false
		}
		cur = arr[idx]
	}
	return cur, true
}

func walkString(m map[string]any, path string) string {
	v, ok := walkPath(m, path)
	if !ok {
		return ""
	}
	return toString(v)
}

func splitIndex(part string) (key string, idx int, hasIdx bool) {
	open := strings.IndexByte(part, '[')
	if open < 0 || !strings.HasSuffix(part, "]") {
		return part, 0, false
	}
	n, err := strconv.Atoi(part[open+1 : len(part)-1])
	if err != nil {
		return part, 0, false
	}
	return part[:open], n, true
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
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
