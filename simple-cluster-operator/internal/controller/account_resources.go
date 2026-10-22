package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/secretpolicy"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/target"
	rendertpl "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/template"
)

var accountChildVerbs = []string{"get", "list", "watch", "create", "patch", "delete"}

// authorizeAccountGraph rejects an incomplete graph before the first child write.
func authorizeAccountGraph(ctx context.Context, tc *target.Client, scpc *showroomv1alpha1.SimpleClusterProviderConfig) (map[string]*target.AccountAuthorization, error) {
	result := make(map[string]*target.AccountAuthorization, len(scpc.Spec.Resources))
	for _, rs := range scpc.Spec.Resources {
		gv, err := schema.ParseGroupVersion(rs.APIVersion)
		if err != nil {
			return nil, err
		}
		authorization, err := target.ResolveAccountResource(ctx, tc.Dynamic, tc.Mapper, tc.ProviderIdentity, gv.WithKind(rs.Kind), accountChildVerbs...)
		if err != nil {
			return nil, fmt.Errorf("resource %q: %w", rs.Name, err)
		}
		result[rs.Name] = authorization
	}
	if source := scpc.Spec.KubeconfigSource.SubResource; source != nil {
		for _, rs := range scpc.Spec.Resources {
			if rs.Name != scpc.Spec.KubeconfigSource.ResourceName {
				continue
			}
			gv, err := schema.ParseGroupVersion(rs.APIVersion)
			if err != nil {
				return nil, err
			}
			authorization, err := target.ResolveAccountSubresource(ctx, tc, gv.WithKind(rs.Kind), source.SubResource)
			if err != nil {
				return nil, err
			}
			if authorization.IdentityHash != result[rs.Name].IdentityHash {
				return nil, fmt.Errorf("credential parent API identity changed during graph preflight")
			}
			tc.CredentialAuthorization = authorization
		}
	}
	return result, nil
}

// preflightAccountObjects checks every currently renderable instance before a child write.
// A dependent with unknown predecessor data requires unrestricted selectors in phase one.
// Phase two checks its complete object after predecessor outputs become available.
func preflightAccountObjects(ctx context.Context, tc *target.Client, scpc *showroomv1alpha1.SimpleClusterProviderConfig,
	sc *showroomv1alpha1.SimpleCluster, authorizations map[string]*target.AccountAuthorization) error {
	live, err := readRecordedAccountObjects(ctx, tc, sc, authorizations)
	if err != nil {
		return err
	}
	specs := map[string]*showroomv1alpha1.ResourceSpec{}
	for i := range scpc.Spec.Resources {
		specs[scpc.Spec.Resources[i].Name] = &scpc.Spec.Resources[i]
	}
	order, err := validateAndTopoSort(scpc)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, name := range order {
		rs := specs[name]
		predecessors := map[string]rendertpl.Predecessor{}
		unresolved := false
		for _, dependency := range rs.DependsOn {
			count, resolved := counts[dependency]
			objects := live[dependency]
			if resolved && count == len(objects) {
				predecessors[dependency] = rendertpl.Predecessor{Repeated: specs[dependency].Repeat != "", Objects: objects}
			} else {
				unresolved = true
			}
		}
		unrestricted := authorizations[rs.Name].Unrestricted()
		if rs.Name == scpc.Spec.KubeconfigSource.ResourceName && tc.CredentialAuthorization != nil {
			unrestricted = unrestricted && tc.CredentialAuthorization.Unrestricted()
		}
		if unresolved && !unrestricted {
			return fmt.Errorf("resource %q has unresolved predecessor outputs and restricted selectors; no child write is allowed", rs.Name)
		}
		rendered, err := rendertpl.Render(scpc, sc, rs, predecessors)
		if err != nil {
			if len(rs.DependsOn) > 0 && unrestricted {
				continue
			}
			return fmt.Errorf("resource %q cannot complete selector preflight before child writes: %w", rs.Name, err)
		}
		if !unresolved {
			counts[rs.Name] = len(rendered)
		}
		for _, obj := range rendered {
			if err := tc.StampParent(obj); err != nil {
				return err
			}
			if err := authorizations[rs.Name].CheckObject(obj); err != nil {
				return fmt.Errorf("resource %q selector preflight: %w", rs.Name, err)
			}
			if rs.Name == scpc.Spec.KubeconfigSource.ResourceName && tc.CredentialAuthorization != nil {
				if err := tc.CredentialAuthorization.CheckObject(obj); err != nil {
					return fmt.Errorf("credential parent selector preflight: %w", err)
				}
			}
		}
	}
	return nil
}

// readRecordedAccountObjects supplies predecessor data only from pinned child identities.
func readRecordedAccountObjects(ctx context.Context, tc *target.Client, sc *showroomv1alpha1.SimpleCluster,
	authorizations map[string]*target.AccountAuthorization) (map[string][]*unstructured.Unstructured, error) {
	known := map[string][]showroomv1alpha1.AccountResourceStatus{}
	for _, receipt := range sc.Status.AccountResources {
		if !receipt.Deleting {
			known[receipt.ResourceName] = append(known[receipt.ResourceName], receipt)
		}
	}
	live := map[string][]*unstructured.Unstructured{}
	for name, receipts := range known {
		authorization := authorizations[name]
		if authorization == nil {
			return nil, fmt.Errorf("recorded resource %q is absent from the graph", name)
		}
		sort.Slice(receipts, func(i, j int) bool { return receipts[i].Index < receipts[j].Index })
		complete := true
		for index, receipt := range receipts {
			if receipt.Index != int32(index) {
				complete = false
			}
			if receipt.Resource != authorization.Mapping.Resource.Resource || receipt.APIIdentityHash != authorization.IdentityHash {
				return nil, fmt.Errorf("recorded resource %q API identity changed", name)
			}
			ri, err := tc.ResourceFor(authorization.Mapping, receipt.Namespace, receipt.Name, secretpolicy.Copy)
			if err != nil {
				return nil, err
			}
			obj, err := ri.Get(ctx, receipt.Name, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				if receipt.UID != "" {
					return nil, fmt.Errorf("recorded resource %q child disappeared before graph preflight", name)
				}
				complete = false
				continue
			}
			if err != nil {
				return nil, err
			}
			if err := authorization.CheckObject(obj); err != nil {
				return nil, err
			}
			if err := target.ValidateParent(obj, tc.AccountParent); err != nil {
				return nil, err
			}
			if (receipt.UID != "" && receipt.UID != string(obj.GetUID())) || obj.GetAnnotations()[target.CreationIDAnnotation] != receipt.CreationID {
				return nil, fmt.Errorf("recorded resource %q child identity changed", name)
			}
			live[name] = append(live[name], obj)
		}
		if !complete {
			delete(live, name)
		}

	}
	return live, nil
}

func receiptIndex(sc *showroomv1alpha1.SimpleCluster, resource string, index int) int {
	for i := range sc.Status.AccountResources {
		receipt := &sc.Status.AccountResources[i]
		if receipt.ResourceName == resource && receipt.Index == int32(index) {
			return i
		}
	}
	return -1
}

func validateReceipt(receipt *showroomv1alpha1.AccountResourceStatus, desired *unstructured.Unstructured, authorization *target.AccountAuthorization) error {
	if receipt.APIVersion != desired.GetAPIVersion() || receipt.Kind != desired.GetKind() || receipt.Resource != authorization.Mapping.Resource.Resource ||
		receipt.Namespace != desired.GetNamespace() || receipt.Name != desired.GetName() || receipt.APIIdentityHash != authorization.IdentityHash || receipt.CreationID == "" {
		return fmt.Errorf("%w: resource identity or API identity differs from its account receipt", ErrMigrationRequired)
	}
	if receipt.Deleting {
		return fmt.Errorf("account child deletion is still in progress")
	}
	return nil
}

func stampCreationID(obj *unstructured.Unstructured, id string) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[target.CreationIDAnnotation] = id
	obj.SetAnnotations(annotations)
}

// recordAccountChild persists a unique creation intent before sending a create.
func (r *SimpleClusterReconciler) recordAccountChild(ctx context.Context, sc *showroomv1alpha1.SimpleCluster,
	rs *showroomv1alpha1.ResourceSpec, desired *unstructured.Unstructured, index int, authorization *target.AccountAuthorization) (int, error) {
	if i := receiptIndex(sc, rs.Name, index); i >= 0 {
		if err := validateReceipt(&sc.Status.AccountResources[i], desired, authorization); err != nil {
			return -1, err
		}
		stampCreationID(desired, sc.Status.AccountResources[i].CreationID)
		return i, nil
	}
	patch := client.MergeFrom(sc.DeepCopy())
	receipt := showroomv1alpha1.AccountResourceStatus{
		ResourceName: rs.Name, Index: int32(index), Repeated: rs.Repeat != "", APIVersion: desired.GetAPIVersion(), Kind: desired.GetKind(),
		Resource: authorization.Mapping.Resource.Resource, Namespace: desired.GetNamespace(), Name: desired.GetName(),
		APIIdentityHash: authorization.IdentityHash, CreationID: string(uuid.NewUUID()), DependsOn: append([]string(nil), rs.DependsOn...),
		DeletionAnnotations: rs.DeletionAnnotations,
	}
	sc.Status.AccountResources = append(sc.Status.AccountResources, receipt)
	if err := r.Status().Patch(ctx, sc, patch); err != nil {
		return -1, fmt.Errorf("record account creation intent: %w", err)
	}
	stampCreationID(desired, receipt.CreationID)
	return len(sc.Status.AccountResources) - 1, nil
}

// acceptAccountChild recovers a lost create response only for the recorded intent.
func (r *SimpleClusterReconciler) acceptAccountChild(ctx context.Context, sc *showroomv1alpha1.SimpleCluster, tc *target.Client,
	existing *unstructured.Unstructured, index int, authorization *target.AccountAuthorization) error {
	if index < 0 {
		return fmt.Errorf("%w: existing account child has no receipt", ErrMigrationRequired)
	}
	receipt := &sc.Status.AccountResources[index]
	if err := validateReceipt(receipt, existing, authorization); err != nil {
		return err
	}
	if err := target.ValidateParent(existing, tc.AccountParent); err != nil {
		return err
	}
	if existing.GetAnnotations()[target.CreationIDAnnotation] != receipt.CreationID || existing.GetUID() == "" {
		return fmt.Errorf("account child does not match its creation intent")
	}
	if receipt.UID != "" && receipt.UID != string(existing.GetUID()) {
		return fmt.Errorf("account child UID differs from its receipt")
	}
	if receipt.UID == "" {
		patch := client.MergeFrom(sc.DeepCopy())
		receipt.UID = string(existing.GetUID())
		if err := r.Status().Patch(ctx, sc, patch); err != nil {
			return fmt.Errorf("record account child UID: %w", err)
		}
	}
	return nil
}

// accountScaleDown permits leaf removal. A live dependent requires an explicit graph handoff.
// This prevents a node-count change from silently destroying a cluster bootstrap resource.
func (r *SimpleClusterReconciler) accountScaleDown(ctx context.Context, sc *showroomv1alpha1.SimpleCluster, tc *target.Client, resource string, count int) (bool, error) {
	excess := false
	for _, receipt := range sc.Status.AccountResources {
		if receipt.ResourceName == resource && receipt.Index >= int32(count) {
			excess = true
		}
	}
	if !excess {
		return true, nil
	}
	for _, receipt := range sc.Status.AccountResources {
		for _, dependency := range receipt.DependsOn {
			if dependency == resource && receipt.ResourceName != resource {
				return false, fmt.Errorf("scale-down of %q requires a handoff of dependent %q", resource, receipt.ResourceName)
			}
		}
	}
	return r.cleanupAccountReceipts(ctx, sc, tc, func(receipt showroomv1alpha1.AccountResourceStatus) bool {
		return receipt.ResourceName == resource && receipt.Index >= int32(count)
	})
}

// cleanupAccountReceipts uses only durable receipts, never a mutable SCPC or rendered fallback name.
// Each call processes the current reverse dependency level and waits for its deletion to finish.
func (r *SimpleClusterReconciler) cleanupAccountReceipts(ctx context.Context, sc *showroomv1alpha1.SimpleCluster, tc *target.Client,
	selected func(showroomv1alpha1.AccountResourceStatus) bool) (bool, error) {
	if len(sc.Status.AccountResources) == 0 {
		return true, nil
	}
	level := make([]int, 0)
	for i, receipt := range sc.Status.AccountResources {
		if !selected(receipt) {
			continue
		}
		dependent := false
		for _, other := range sc.Status.AccountResources {
			for _, dependency := range other.DependsOn {
				if dependency == receipt.ResourceName {
					dependent = true
				}
			}
		}
		if !dependent {
			level = append(level, i)
		}
	}
	if len(level) == 0 {
		return false, fmt.Errorf("account receipt dependencies prevent cleanup")
	}
	removed := map[int]bool{}
	for _, i := range level {
		gone, err := r.deleteAccountReceipt(ctx, sc, tc, i)
		if err != nil {
			return false, err
		}
		if gone {
			removed[i] = true
		}
	}
	if len(removed) > 0 {
		patch := client.MergeFrom(sc.DeepCopy())
		remaining := make([]showroomv1alpha1.AccountResourceStatus, 0, len(sc.Status.AccountResources)-len(removed))
		for i, receipt := range sc.Status.AccountResources {
			if !removed[i] {
				remaining = append(remaining, receipt)
			}
		}
		sc.Status.AccountResources = remaining
		if err := r.Status().Patch(ctx, sc, patch); err != nil {
			return false, err
		}
	}
	for _, receipt := range sc.Status.AccountResources {
		if selected(receipt) {
			return false, nil
		}
	}
	return true, nil
}

func (r *SimpleClusterReconciler) deleteAccountReceipt(ctx context.Context, sc *showroomv1alpha1.SimpleCluster, tc *target.Client, index int) (bool, error) {
	receipt := &sc.Status.AccountResources[index]
	gv, err := schema.ParseGroupVersion(receipt.APIVersion)
	if err != nil {
		return false, err
	}
	authorization, err := target.ResolveAccountResource(ctx, tc.Dynamic, tc.Mapper, tc.ProviderIdentity, gv.WithKind(receipt.Kind), "get", "patch", "delete")
	if err != nil {
		return false, err
	}
	if receipt.APIIdentityHash != authorization.IdentityHash || receipt.Resource != authorization.Mapping.Resource.Resource || receipt.CreationID == "" {
		return false, fmt.Errorf("recorded account API identity changed; retain parent finalizer")
	}
	if _, err := tc.ResourceFor(authorization.Mapping, receipt.Namespace, receipt.Name, secretpolicy.Copy); err != nil {
		return false, err
	}
	ri, err := tc.ResourceFor(authorization.Mapping, receipt.Namespace, receipt.Name, secretpolicy.Write)
	if err != nil {
		return false, err
	}
	existing, err := ri.Get(ctx, receipt.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if err := authorization.CheckObject(existing); err != nil {
		return false, err
	}
	if err := target.ValidateParent(existing, tc.AccountParent); err != nil {
		return false, err
	}
	if receipt.UID == "" {
		if err := r.acceptAccountChild(ctx, sc, tc, existing, index, authorization); err != nil {
			return false, err
		}
		receipt = &sc.Status.AccountResources[index]
	}
	if receipt.UID != string(existing.GetUID()) || existing.GetAnnotations()[target.CreationIDAnnotation] != receipt.CreationID {
		return false, fmt.Errorf("account child UID or creation intent changed; retain parent finalizer")
	}
	if !receipt.Deleting {
		patch := client.MergeFrom(sc.DeepCopy())
		receipt.Deleting = true
		if err := r.Status().Patch(ctx, sc, patch); err != nil {
			return false, err
		}
	}
	if !existing.GetDeletionTimestamp().IsZero() {
		return false, nil
	}
	annotations := map[string]string{rendertpl.AnnoAllowDelete: "true"}
	for key, value := range receipt.DeletionAnnotations {
		annotations[key] = value
	}
	uid := types.UID(receipt.UID)
	body, err := json.Marshal(map[string]any{"metadata": map[string]any{"uid": uid, "resourceVersion": existing.GetResourceVersion(), "annotations": annotations}})
	if err != nil {
		return false, err
	}
	if err := reauthorizeAccountWrite(ctx, tc, existing, receipt.APIIdentityHash, "patch"); err != nil {
		return false, err
	}
	if _, err := ri.Patch(ctx, receipt.Name, types.MergePatchType, body, metav1.PatchOptions{}); err != nil {
		return false, err
	}
	if err := reauthorizeAccountWrite(ctx, tc, existing, receipt.APIIdentityHash, "delete"); err != nil {
		return false, err
	}
	err = ri.Delete(ctx, receipt.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
	return false, client.IgnoreNotFound(err)
}

// reauthorizeAccountWrite reads current binding state immediately before a mutation.
func reauthorizeAccountWrite(ctx context.Context, tc *target.Client, obj *unstructured.Unstructured, identity string, verbs ...string) error {
	authorization, err := target.ResolveAccountResource(ctx, tc.Dynamic, tc.Mapper, tc.ProviderIdentity, obj.GroupVersionKind(), verbs...)
	if err != nil {
		return err
	}
	if authorization.IdentityHash != identity {
		return fmt.Errorf("account API identity changed before write")
	}
	return authorization.CheckObject(obj)
}
