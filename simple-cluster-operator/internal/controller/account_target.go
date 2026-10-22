package controller

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/target"
)

const reasonMigrationRequired = "MigrationRequired"

var ErrMigrationRequired = errors.New(reasonMigrationRequired)

func validateAccountTarget(scpc *showroomv1alpha1.SimpleClusterProviderConfig) error {
	if scpc.Spec.Target.KubeconfigSecretRef != nil {
		return fmt.Errorf("%w: provider kubeconfig destinations require an explicit account handoff", ErrMigrationRequired)
	}
	for _, resource := range scpc.Spec.Resources {
		if resource.ForceCleanupOnCascade {
			return fmt.Errorf("account children cannot force removal of provider finalizers")
		}
	}
	return nil
}

func (r *SimpleClusterReconciler) targetClient(ctx context.Context, sc *showroomv1alpha1.SimpleCluster, scpc *showroomv1alpha1.SimpleClusterProviderConfig) (*target.Client, error) {
	if recorded := sc.Status.AccountTarget; recorded != nil && (recorded.Mode != target.AccountMode || recorded.ResourceTrackingVersion != 1) {
		return nil, fmt.Errorf("%w: the recorded destination needs a reviewed account handoff", ErrMigrationRequired)
	}
	if sc.Status.AccountTarget == nil && (len(sc.Status.Resources) > 0 || sc.Status.TargetCluster != "") {
		return nil, fmt.Errorf("%w: existing children have no account identity receipts", ErrMigrationRequired)
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
	ref.ResourceTrackingVersion = 1
	if recorded := sc.Status.AccountTarget; recorded != nil {
		if *recorded != *ref {
			return nil, fmt.Errorf("account parent identity differs from the recorded target")
		}
	} else {
		patch := client.MergeFrom(sc.DeepCopy())
		sc.Status.AccountTarget = ref
		if sc.Status.ProviderConfigRef == nil {
			sc.Status.ProviderConfigRef = &corev1.LocalObjectReference{Name: scpc.Name}
		}
		if err := r.Status().Patch(ctx, sc, patch); err != nil {
			return nil, fmt.Errorf("record account target: %w", err)
		}
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
