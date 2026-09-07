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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
)

func TestCreationAllowedRejectsStaleIdentityOrSpec(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := showroomv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	freshSC := &showroomv1alpha1.SimpleCluster{ObjectMeta: metav1.ObjectMeta{
		Name:       "cluster",
		Namespace:  "account",
		UID:        types.UID("sc-uid"),
		Generation: 4,
		Labels:     map[string]string{"region": "eu"},
		Finalizers: []string{showroomv1alpha1.FinalizerName},
	}, Status: showroomv1alpha1.SimpleClusterStatus{
		ProviderConfigRef:       &corev1.LocalObjectReference{Name: "provider"},
		ProviderConfigNamespace: "account",
		ProviderConfigUID:       types.UID("scpc-uid"),
	}}
	freshSCPC := &showroomv1alpha1.SimpleClusterProviderConfig{ObjectMeta: metav1.ObjectMeta{
		Name: "provider", Namespace: "account", UID: types.UID("scpc-uid"), Generation: 7,
	}}
	tests := []struct {
		name       string
		sc         *showroomv1alpha1.SimpleCluster
		scpc       *showroomv1alpha1.SimpleClusterProviderConfig
		storedSC   *showroomv1alpha1.SimpleCluster
		storedSCPC *showroomv1alpha1.SimpleClusterProviderConfig
		want       bool
	}{
		{name: "current objects", sc: freshSC.DeepCopy(), scpc: freshSCPC.DeepCopy(), want: true},
		{name: "SimpleCluster generation changed", sc: staleSC(freshSC, "sc-uid", 3), scpc: freshSCPC.DeepCopy()},
		{name: "SimpleCluster was replaced", sc: staleSC(freshSC, "old-sc-uid", 4), scpc: freshSCPC.DeepCopy()},
		{name: "SimpleCluster render label changed", sc: freshSC.DeepCopy(), scpc: freshSCPC.DeepCopy(), storedSC: scWithLabels(freshSC, map[string]string{"region": "us"})},
		{name: "SimpleCluster provider lock changed", sc: freshSC.DeepCopy(), scpc: freshSCPC.DeepCopy(), storedSC: scWithProviderUID(freshSC, "replacement-uid")},
		{name: "SimpleCluster finalizer was removed", sc: freshSC.DeepCopy(), scpc: freshSCPC.DeepCopy(), storedSC: scWithoutFinalizer(freshSC)},
		{name: "SCPC generation changed", sc: freshSC.DeepCopy(), scpc: staleSCPC(freshSCPC, "scpc-uid", 6)},
		{name: "SCPC was replaced", sc: freshSC.DeepCopy(), scpc: staleSCPC(freshSCPC, "old-scpc-uid", 7)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			storedSC := test.storedSC
			if storedSC == nil {
				storedSC = freshSC
			}
			storedSCPC := test.storedSCPC
			if storedSCPC == nil {
				storedSCPC = freshSCPC
			}
			reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(storedSC, storedSCPC).Build()
			reconciler := &SimpleClusterReconciler{Client: reader, APIReader: reader}
			got, err := reconciler.creationAllowed(context.Background(), test.sc, test.scpc)
			if err != nil {
				t.Fatalf("creationAllowed() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("creationAllowed() = %t, want %t", got, test.want)
			}
		})
	}
}

func scWithLabels(current *showroomv1alpha1.SimpleCluster, labels map[string]string) *showroomv1alpha1.SimpleCluster {
	stale := current.DeepCopy()
	stale.Labels = labels
	return stale
}

func scWithoutFinalizer(current *showroomv1alpha1.SimpleCluster) *showroomv1alpha1.SimpleCluster {
	stale := current.DeepCopy()
	stale.Finalizers = nil
	return stale
}

func scWithProviderUID(current *showroomv1alpha1.SimpleCluster, uid string) *showroomv1alpha1.SimpleCluster {
	stale := current.DeepCopy()
	stale.Status.ProviderConfigUID = types.UID(uid)
	return stale
}

func staleSC(current *showroomv1alpha1.SimpleCluster, uid string, generation int64) *showroomv1alpha1.SimpleCluster {
	stale := current.DeepCopy()
	stale.UID = types.UID(uid)
	stale.Generation = generation
	return stale
}

func staleSCPC(current *showroomv1alpha1.SimpleClusterProviderConfig, uid string, generation int64) *showroomv1alpha1.SimpleClusterProviderConfig {
	stale := current.DeepCopy()
	stale.UID = types.UID(uid)
	stale.Generation = generation
	return stale
}
