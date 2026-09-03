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
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	rendertpl "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/template"
)

var (
	ownershipGVR = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	ownershipGVK = schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}
)

func ownershipFixtures(recorded bool) (*showroomv1alpha1.SimpleCluster, *showroomv1alpha1.SimpleClusterProviderConfig) {
	sc := &showroomv1alpha1.SimpleCluster{ObjectMeta: metav1.ObjectMeta{
		Name: "cluster", Namespace: "account", UID: types.UID("sc-uid"),
	}}
	if recorded {
		sc.Status.Resources = []showroomv1alpha1.ResourceStatus{{
			Name: "claim", GVK: ownershipGVK.String(), TargetName: "target", TargetNamespace: "provider",
		}}
	}
	scpc := &showroomv1alpha1.SimpleClusterProviderConfig{ObjectMeta: metav1.ObjectMeta{
		Name: "telekom", Namespace: "provider-system", UID: types.UID("scpc-uid"),
	}}
	return sc, scpc
}

func ownershipTarget(uid, resourceVersion string, annotations map[string]string) *unstructured.Unstructured {
	object := &unstructured.Unstructured{}
	object.SetAPIVersion("v1")
	object.SetKind("ConfigMap")
	object.SetName("target")
	object.SetNamespace("provider")
	object.SetUID(types.UID(uid))
	object.SetResourceVersion(resourceVersion)
	object.SetAnnotations(annotations)
	return object
}

func legacyOwnershipAnnotations(sc *showroomv1alpha1.SimpleCluster, scpc *showroomv1alpha1.SimpleClusterProviderConfig) map[string]string {
	return map[string]string{
		rendertpl.AnnoSimpleCluster:         sc.Namespace + "/" + sc.Name,
		rendertpl.AnnoSimpleClusterProvider: scpc.Name,
	}
}

func exactOwnershipAnnotations(sc *showroomv1alpha1.SimpleCluster, scpc *showroomv1alpha1.SimpleClusterProviderConfig) map[string]string {
	annotations := legacyOwnershipAnnotations(sc, scpc)
	for key, value := range immutableOwnershipAnnotations(sc, scpc) {
		annotations[key] = value
	}
	return annotations
}

func TestEnsureTargetOwnershipMigratesOnlyRecordedLegacyTarget(t *testing.T) {
	ctx := context.Background()
	sc, scpc := ownershipFixtures(true)
	legacy := ownershipTarget("target-uid", "7", legacyOwnershipAnnotations(sc, scpc))
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), legacy)
	ri := dynamicClient.Resource(ownershipGVR).Namespace("provider")

	migrated, err := (&SimpleClusterReconciler{}).ensureTargetOwnership(
		ctx, ri, legacy, sc, scpc, "claim", "provider",
	)
	if err != nil || !migrated {
		t.Fatalf("recorded legacy migration = %v, %v; want true, nil", migrated, err)
	}
	patched, err := ri.Get(ctx, "target", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if classifyTargetOwnership(patched, sc, scpc) != targetOwnershipExact {
		t.Fatalf("migrated annotations = %#v, want exact ownership", patched.GetAnnotations())
	}

	migrated, err = (&SimpleClusterReconciler{}).ensureTargetOwnership(
		ctx, ri, patched, sc, scpc, "claim", "provider",
	)
	if err != nil || migrated {
		t.Fatalf("exact ownership check = %v, %v; want false, nil", migrated, err)
	}
}

func TestEnsureTargetOwnershipRejectsUnrecordedOrPartialLegacyTarget(t *testing.T) {
	for _, tt := range []struct {
		name        string
		recorded    bool
		annotations func(*showroomv1alpha1.SimpleCluster, *showroomv1alpha1.SimpleClusterProviderConfig) map[string]string
	}{
		{name: "unrecorded legacy target", recorded: false, annotations: legacyOwnershipAnnotations},
		{name: "partial immutable identity", recorded: true, annotations: func(
			sc *showroomv1alpha1.SimpleCluster,
			scpc *showroomv1alpha1.SimpleClusterProviderConfig,
		) map[string]string {
			annotations := legacyOwnershipAnnotations(sc, scpc)
			annotations[rendertpl.AnnoSimpleClusterUID] = string(sc.UID)
			return annotations
		}},
		{name: "mismatched immutable identity", recorded: true, annotations: func(
			sc *showroomv1alpha1.SimpleCluster,
			scpc *showroomv1alpha1.SimpleClusterProviderConfig,
		) map[string]string {
			annotations := exactOwnershipAnnotations(sc, scpc)
			annotations[rendertpl.AnnoSimpleClusterProviderUID] = "replacement-scpc-uid"
			return annotations
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sc, scpc := ownershipFixtures(tt.recorded)
			object := ownershipTarget("target-uid", "7", tt.annotations(sc, scpc))
			dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), object)
			migrated, err := (&SimpleClusterReconciler{}).ensureTargetOwnership(
				context.Background(), dynamicClient.Resource(ownershipGVR).Namespace("provider"),
				object, sc, scpc, "claim", "provider",
			)
			if migrated || !errors.Is(err, errTargetOwnershipConflict) {
				t.Fatalf("ownership result = %v, %v; want false, ownership conflict", migrated, err)
			}
			if len(dynamicClient.Actions()) != 0 {
				t.Fatalf("rejected target was mutated: %#v", dynamicClient.Actions())
			}
		})
	}
}

func TestEnsureTargetOwnershipRejectsReplacementBeforeMigrationPatch(t *testing.T) {
	ctx := context.Background()
	sc, scpc := ownershipFixtures(true)
	original := ownershipTarget("original-uid", "7", legacyOwnershipAnnotations(sc, scpc))
	replacement := ownershipTarget("replacement-uid", "8", legacyOwnershipAnnotations(sc, scpc))
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), original)
	var raced atomic.Bool
	dynamicClient.PrependReactor("patch", "configmaps", func(clienttesting.Action) (bool, runtime.Object, error) {
		if raced.CompareAndSwap(false, true) {
			if err := dynamicClient.Tracker().Delete(ownershipGVR, "provider", "target"); err != nil {
				t.Fatal(err)
			}
			if err := dynamicClient.Tracker().Add(replacement); err != nil {
				t.Fatal(err)
			}
		}
		return false, nil, nil
	})

	migrated, err := (&SimpleClusterReconciler{}).ensureTargetOwnership(
		ctx, dynamicClient.Resource(ownershipGVR).Namespace("provider"),
		original, sc, scpc, "claim", "provider",
	)
	if migrated || err == nil {
		t.Fatalf("migration result = %v, %v; want false with guarded-patch error", migrated, err)
	}
	got, getErr := dynamicClient.Tracker().Get(ownershipGVR, "provider", "target")
	if getErr != nil {
		t.Fatal(getErr)
	}
	gotReplacement := got.(*unstructured.Unstructured)
	if gotReplacement.GetUID() != replacement.GetUID() ||
		classifyTargetOwnership(gotReplacement, sc, scpc) != targetOwnershipLegacy {
		t.Fatalf("replacement was migrated or lost: %#v", gotReplacement.Object)
	}
}

func TestEnsureTargetOwnershipRequiresRecordedGVKForLegacyMigration(t *testing.T) {
	for _, recordedGVK := range []string{"", schema.GroupVersionKind{Version: "v1", Kind: "Secret"}.String()} {
		t.Run("recorded GVK "+recordedGVK, func(t *testing.T) {
			sc, scpc := ownershipFixtures(true)
			sc.Status.Resources[0].GVK = recordedGVK
			object := ownershipTarget("target-uid", "7", legacyOwnershipAnnotations(sc, scpc))
			dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), object)
			migrated, err := (&SimpleClusterReconciler{}).ensureTargetOwnership(
				context.Background(), dynamicClient.Resource(ownershipGVR).Namespace("provider"),
				object, sc, scpc, "claim", "provider",
			)
			if migrated || !errors.Is(err, errTargetOwnershipConflict) {
				t.Fatalf("ownership result = %v, %v; want false, ownership conflict", migrated, err)
			}
			if len(dynamicClient.Actions()) != 0 {
				t.Fatalf("GVK-mismatched target was mutated: %#v", dynamicClient.Actions())
			}
		})
	}
}

func TestDeleteOneTargetRejectsReplacementBeforeGuardedPatch(t *testing.T) {
	ctx := context.Background()
	sc, scpc := ownershipFixtures(true)
	original := ownershipTarget("original-uid", "7", exactOwnershipAnnotations(sc, scpc))
	replacement := ownershipTarget("replacement-uid", "8", legacyOwnershipAnnotations(sc, scpc))
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), original)
	var raced atomic.Bool
	dynamicClient.PrependReactor("patch", "configmaps", func(clienttesting.Action) (bool, runtime.Object, error) {
		if raced.CompareAndSwap(false, true) {
			if err := dynamicClient.Tracker().Delete(ownershipGVR, "provider", "target"); err != nil {
				t.Fatal(err)
			}
			if err := dynamicClient.Tracker().Add(replacement); err != nil {
				t.Fatal(err)
			}
		}
		return false, nil, nil
	})

	exists, err := (&SimpleClusterReconciler{}).deleteOneTarget(
		ctx, dynamicClient.Resource(ownershipGVR).Namespace("provider"),
		&showroomv1alpha1.ResourceSpec{}, ownershipGVK, "claim", "target", "provider", sc, scpc,
	)
	if !exists || err == nil {
		t.Fatalf("delete result = %v, %v; want existing with guarded-patch error", exists, err)
	}
	for _, action := range dynamicClient.Actions() {
		if action.GetVerb() == "delete" {
			t.Fatalf("delete followed failed identity guard: %#v", dynamicClient.Actions())
		}
	}
	got, getErr := dynamicClient.Tracker().Get(ownershipGVR, "provider", "target")
	if getErr != nil {
		t.Fatal(getErr)
	}
	gotReplacement := got.(*unstructured.Unstructured)
	if gotReplacement.GetUID() != replacement.GetUID() ||
		gotReplacement.GetAnnotations()[rendertpl.AnnoAllowDelete] != "" {
		t.Fatalf("replacement was modified or lost: %#v", gotReplacement.Object)
	}
}

func TestDeleteOneTargetUIDPreconditionProtectsReplacementAfterPatch(t *testing.T) {
	ctx := context.Background()
	sc, scpc := ownershipFixtures(true)
	original := ownershipTarget("original-uid", "7", exactOwnershipAnnotations(sc, scpc))
	replacement := ownershipTarget("replacement-uid", "8", legacyOwnershipAnnotations(sc, scpc))
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), original)
	var deleteCalled atomic.Bool
	dynamicClient.PrependReactor("delete", "configmaps", func(action clienttesting.Action) (bool, runtime.Object, error) {
		deleteCalled.Store(true)
		deleteAction, ok := action.(clienttesting.DeleteAction)
		if !ok {
			t.Fatalf("delete action type = %T", action)
		}
		preconditions := deleteAction.GetDeleteOptions().Preconditions
		if preconditions == nil || preconditions.UID == nil || *preconditions.UID != original.GetUID() {
			t.Fatalf("delete preconditions = %#v, want UID %q", preconditions, original.GetUID())
		}
		if err := dynamicClient.Tracker().Delete(ownershipGVR, "provider", "target"); err != nil {
			t.Fatal(err)
		}
		if err := dynamicClient.Tracker().Add(replacement); err != nil {
			t.Fatal(err)
		}
		return true, nil, apierrors.NewConflict(
			schema.GroupResource{Resource: "configmaps"}, "target",
			errors.New("UID precondition rejected replacement"),
		)
	})

	exists, err := (&SimpleClusterReconciler{}).deleteOneTarget(
		ctx, dynamicClient.Resource(ownershipGVR).Namespace("provider"),
		&showroomv1alpha1.ResourceSpec{}, ownershipGVK, "claim", "target", "provider", sc, scpc,
	)
	if !exists || err == nil || !strings.Contains(err.Error(), "UID precondition") || !deleteCalled.Load() {
		t.Fatalf("delete result = %v, %v, called=%v; want guarded conflict", exists, err, deleteCalled.Load())
	}
	got, getErr := dynamicClient.Tracker().Get(ownershipGVR, "provider", "target")
	if getErr != nil {
		t.Fatal(getErr)
	}
	gotReplacement := got.(*unstructured.Unstructured)
	if gotReplacement.GetUID() != replacement.GetUID() ||
		gotReplacement.GetAnnotations()[rendertpl.AnnoAllowDelete] != "" {
		t.Fatalf("replacement was modified or lost: %#v", gotReplacement.Object)
	}
}

func TestForceCleanupRejectsReplacementBeforeFinalizerPatch(t *testing.T) {
	ctx := context.Background()
	sc, scpc := ownershipFixtures(true)
	original := ownershipTarget("original-uid", "7", exactOwnershipAnnotations(sc, scpc))
	original.SetFinalizers([]string{"original.example/hold"})
	original.SetDeletionTimestamp(&metav1.Time{Time: time.Now().Add(-2 * forceCleanupGrace)})
	replacement := ownershipTarget("replacement-uid", "8", exactOwnershipAnnotations(sc, scpc))
	replacement.SetFinalizers([]string{"replacement.example/hold"})
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), original)
	var raced atomic.Bool
	dynamicClient.PrependReactor("patch", "configmaps", func(clienttesting.Action) (bool, runtime.Object, error) {
		if raced.CompareAndSwap(false, true) {
			if err := dynamicClient.Tracker().Delete(ownershipGVR, "provider", "target"); err != nil {
				t.Fatal(err)
			}
			if err := dynamicClient.Tracker().Add(replacement); err != nil {
				t.Fatal(err)
			}
		}
		return false, nil, nil
	})

	exists, err := (&SimpleClusterReconciler{}).deleteOneTarget(
		ctx, dynamicClient.Resource(ownershipGVR).Namespace("provider"),
		&showroomv1alpha1.ResourceSpec{ForceCleanupOnCascade: true},
		ownershipGVK, "claim", "target", "provider", sc, scpc,
	)
	if !exists || err == nil {
		t.Fatalf("force cleanup result = %v, %v; want guarded-patch error", exists, err)
	}
	got, getErr := dynamicClient.Tracker().Get(ownershipGVR, "provider", "target")
	if getErr != nil {
		t.Fatal(getErr)
	}
	gotReplacement := got.(*unstructured.Unstructured)
	if gotReplacement.GetUID() != replacement.GetUID() ||
		len(gotReplacement.GetFinalizers()) != 1 ||
		gotReplacement.GetFinalizers()[0] != "replacement.example/hold" {
		t.Fatalf("replacement finalizers were modified: %#v", gotReplacement.Object)
	}
}
