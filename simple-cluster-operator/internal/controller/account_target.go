package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/target"
)

func validateAccountTarget(scpc *showroomv1alpha1.SimpleClusterProviderConfig) error {
	if !scpc.Spec.Target.KCPAccount {
		return nil
	}
	if scpc.Spec.Target.KubeconfigSecretRef != nil || scpc.Spec.Target.Namespace != "" {
		return fmt.Errorf("kcpAccount cannot be combined with a target namespace or kubeconfigSecretRef")
	}
	for _, rs := range scpc.Spec.Resources {
		if rs.APIVersion != "sap.eu/v1alpha1" || rs.Kind != "IronCoreShoot" || rs.Namespace != "" || rs.ForceCleanupOnCascade {
			return fmt.Errorf("account targets require cluster-scoped IronCoreShoots without forced cleanup")
		}
	}
	if scpc.Spec.KubeconfigSource.SecretRef == nil || scpc.Spec.KubeconfigSource.SecretRef.Namespace != "default" {
		return fmt.Errorf("account target requires kubeconfigSource.secretRef.namespace default")
	}
	return nil
}

func (r *SimpleClusterReconciler) targetClient(ctx context.Context, sc *showroomv1alpha1.SimpleCluster, scpc *showroomv1alpha1.SimpleClusterProviderConfig) (*target.Client, error) {
	if sc.Status.AccountTarget != nil && !scpc.Spec.Target.KCPAccount {
		return nil, fmt.Errorf("cannot change the recorded account target to another target mode")
	}
	if !scpc.Spec.Target.KCPAccount {
		return target.New(ctx, r.Client, r.RESTConfig, r.RESTMapper(), scpc.Spec.Target.KubeconfigSecretRef, sc.Namespace, r.SecretPolicy)
	}
	if err := validateAccountTarget(scpc); err != nil {
		return nil, err
	}
	ref, err := target.AccountIdentity(sc)
	if err != nil {
		return nil, err
	}
	tc, err := r.AccountProvider.ForAccount(ctx, ref.Cluster)
	if err != nil {
		return nil, err
	}
	parent, err := tc.Parent(ctx, ref)
	if err != nil {
		return nil, err
	}
	ref.UID = string(parent.GetUID())
	ref.Mode = target.AccountMode
	if recorded := sc.Status.AccountTarget; recorded != nil {
		if recorded.Cluster != ref.Cluster || recorded.Namespace != ref.Namespace || recorded.Name != ref.Name || recorded.UID != ref.UID {
			return nil, fmt.Errorf("account parent identity differs from the recorded target")
		}
		ref.Mode = recorded.Mode
		if ref.Mode != target.AccountMode && ref.Mode != target.LegacyLocalMode {
			return nil, fmt.Errorf("invalid recorded account target mode")
		}
	} else {
		if r.AccountProvider.LegacyParentUIDs[ref.UID] {
			ref.Mode = target.LegacyLocalMode
		}
		patch := client.MergeFrom(sc.DeepCopy())
		sc.Status.AccountTarget = ref
		if sc.Status.ProviderConfigRef == nil {
			sc.Status.ProviderConfigRef = &corev1.LocalObjectReference{Name: scpc.Name}
		}
		if err := r.Status().Patch(ctx, sc, patch); err != nil {
			return nil, fmt.Errorf("record account target before resource writes: %w", err)
		}
	}
	if ref.Mode == target.LegacyLocalMode {
		// Only the configured old account UID enters this mode. New UIDs use KCP.
		scpc.Spec.Target.Namespace = sc.Namespace
		scpc.Spec.KubeconfigSource.SecretRef.Namespace = sc.Namespace
		return target.New(ctx, r.Client, r.RESTConfig, r.RESTMapper(), nil, sc.Namespace, r.SecretPolicy)
	}
	tc.AccountParent = ref
	tc.OwnerNamespace = ref.Namespace
	tc.SecretPolicy = r.SecretPolicy
	return tc, nil
}

func accountMapper(ctx context.Context, provider *target.AccountProvider, scpc *showroomv1alpha1.SimpleClusterProviderConfig) (apimeta.RESTMapper, error) {
	if err := validateAccountTarget(scpc); err != nil {
		return nil, err
	}
	tc, err := provider.ForAccount(ctx, scpc.Namespace)
	if err != nil {
		return nil, err
	}
	return tc.Mapper, nil
}

// accountCreationAllowed checks the account parent because the local projection can lag behind deletion.
func accountCreationAllowed(ctx context.Context, tc *target.Client) (bool, error) {
	if tc.AccountParent == nil {
		return true, nil
	}
	parent, err := tc.Parent(ctx, tc.AccountParent)
	if err != nil {
		return false, err
	}
	if string(parent.GetUID()) != tc.AccountParent.UID {
		return false, fmt.Errorf("account parent UID changed before resource creation")
	}
	return parent.GetDeletionTimestamp().IsZero(), nil
}

// recordAccountChild preserves the delete identity before a create can succeed without a response.
func (r *SimpleClusterReconciler) recordAccountChild(ctx context.Context, sc *showroomv1alpha1.SimpleCluster,
	resource *showroomv1alpha1.ResourceSpec, desired *unstructured.Unstructured, index int) error {
	if sc.Status.AccountTarget == nil || sc.Status.AccountTarget.Mode != target.AccountMode {
		return nil
	}
	patch := client.MergeFrom(sc.DeepCopy())
	var status *showroomv1alpha1.ResourceStatus
	for i := range sc.Status.Resources {
		if sc.Status.Resources[i].Name == resource.Name {
			status = &sc.Status.Resources[i]
			break
		}
	}
	if status == nil {
		sc.Status.Resources = append(sc.Status.Resources, showroomv1alpha1.ResourceStatus{Name: resource.Name, GVK: desired.GroupVersionKind().String(), Phase: showroomv1alpha1.PhasePending})
		status = &sc.Status.Resources[len(sc.Status.Resources)-1]
	}
	if resource.Repeat == "" {
		if status.TargetName != "" && status.TargetName != desired.GetName() {
			return fmt.Errorf("account child name differs from recorded identity")
		}
		status.TargetName = desired.GetName()
	} else {
		for _, instance := range status.Instances {
			if instance.Index == int32(index) {
				if instance.TargetName != desired.GetName() {
					return fmt.Errorf("account child name differs from recorded identity")
				}
				return nil
			}
		}
		status.Instances = append(status.Instances, showroomv1alpha1.InstanceStatus{Index: int32(index), TargetName: desired.GetName(), Phase: showroomv1alpha1.PhasePending})
	}
	if err := r.Status().Patch(ctx, sc, patch); err != nil {
		return fmt.Errorf("record account child before create: %w", err)
	}
	return nil
}
