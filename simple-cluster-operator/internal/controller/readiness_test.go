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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
)

func TestExtractPhasePreservesLegacyPathReadiness(t *testing.T) {
	check := &showroomv1alpha1.ReadinessCheck{
		PhasePath:         "status.operation.state",
		PhaseReadyValue:   "Succeeded",
		PhaseFailedValues: []string{"Failed", "Error"},
		LastOperationPath: "status.operation.message",
		LastErrorPath:     "status.errors[0].message",
	}

	tests := []struct {
		name          string
		status        map[string]any
		wantPhase     showroomv1alpha1.Phase
		wantOperation string
		wantError     string
	}{
		{
			name: "ready",
			status: map[string]any{
				"operation": map[string]any{"state": "Succeeded", "message": "cluster available"},
			},
			wantPhase:     showroomv1alpha1.PhaseReady,
			wantOperation: "cluster available",
		},
		{
			name: "failed",
			status: map[string]any{
				"operation": map[string]any{"state": "Failed", "message": "reconcile stopped"},
				"errors":    []any{map[string]any{"message": "invalid region"}},
			},
			wantPhase:     showroomv1alpha1.PhaseFailed,
			wantOperation: "reconcile stopped",
			wantError:     "invalid region",
		},
		{
			name: "provisioning",
			status: map[string]any{
				"operation": map[string]any{"state": "Processing", "message": "creating workers"},
			},
			wantPhase:     showroomv1alpha1.PhaseProvisioning,
			wantOperation: "creating workers",
		},
		{
			name:      "pending when state is missing",
			status:    map[string]any{},
			wantPhase: showroomv1alpha1.PhasePending,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &unstructured.Unstructured{Object: map[string]any{"status": tt.status}}
			phase, operation, failure := extractPhase(check, obj)
			if phase != tt.wantPhase || operation != tt.wantOperation || failure != tt.wantError {
				t.Fatalf("extractPhase() = (%q, %q, %q), want (%q, %q, %q)",
					phase, operation, failure, tt.wantPhase, tt.wantOperation, tt.wantError)
			}
		})
	}
}

func TestExtractPhaseFromTypedConditions(t *testing.T) {
	check := providerConditionCheck()

	tests := []struct {
		name          string
		generation    int64
		conditions    []metav1.Condition
		wantPhase     showroomv1alpha1.Phase
		wantOperation string
		wantError     string
	}{
		{
			name:       "ready condition can appear between unrelated conditions",
			generation: 4,
			conditions: []metav1.Condition{
				condition("Responsive", metav1.ConditionTrue, 4, "provider reachable", "Responsive"),
				condition("Ready", metav1.ConditionTrue, 4, "cluster available", "Available"),
				condition("Synced", metav1.ConditionTrue, 4, "resource synced", "ReconcileSuccess"),
			},
			wantPhase:     showroomv1alpha1.PhaseReady,
			wantOperation: "cluster available",
		},
		{
			name:       "ready false is provisioning",
			generation: 5,
			conditions: []metav1.Condition{
				condition("Ready", metav1.ConditionFalse, 5, "creating control plane", "Creating"),
				condition("Synced", metav1.ConditionTrue, 5, "resource synced", "ReconcileSuccess"),
			},
			wantPhase:     showroomv1alpha1.PhaseProvisioning,
			wantOperation: "creating control plane",
		},
		{
			name:       "synced false surfaces its message",
			generation: 6,
			conditions: []metav1.Condition{
				condition("Synced", metav1.ConditionFalse, 6, "invalid region", "ReconcileError"),
				condition("Ready", metav1.ConditionFalse, 6, "not ready", "Creating"),
			},
			wantPhase:     showroomv1alpha1.PhaseFailed,
			wantOperation: "not ready",
			wantError:     "invalid region",
		},
		{
			name:       "responsive false surfaces its message",
			generation: 7,
			conditions: []metav1.Condition{
				condition("Ready", metav1.ConditionFalse, 7, "waiting for provider", "Creating"),
				condition("Responsive", metav1.ConditionFalse, 7, "provider offline", "Unresponsive"),
			},
			wantPhase:     showroomv1alpha1.PhaseFailed,
			wantOperation: "waiting for provider",
			wantError:     "provider offline",
		},
		{
			name:       "failures take precedence over ready and none are hidden",
			generation: 8,
			conditions: []metav1.Condition{
				condition("Synced", metav1.ConditionFalse, 8, "sync failed", "ReconcileError"),
				condition("Ready", metav1.ConditionTrue, 8, "last known cluster is available", "Available"),
				condition("Responsive", metav1.ConditionFalse, 8, "provider offline", "Unresponsive"),
			},
			wantPhase:     showroomv1alpha1.PhaseFailed,
			wantOperation: "last known cluster is available",
			wantError:     "provider offline; sync failed",
		},
		{
			name:       "missing ready and no failure is pending",
			generation: 9,
			conditions: []metav1.Condition{
				condition("Synced", metav1.ConditionTrue, 9, "resource synced", "ReconcileSuccess"),
			},
			wantPhase: showroomv1alpha1.PhasePending,
		},
		{
			name:       "ready unknown is provisioning",
			generation: 10,
			conditions: []metav1.Condition{
				condition("Ready", metav1.ConditionUnknown, 10, "provider has not decided", "Unknown"),
			},
			wantPhase:     showroomv1alpha1.PhaseProvisioning,
			wantOperation: "provider has not decided",
		},
		{
			name:       "cross-sync target generation 1 accepts provider observed generation 4",
			generation: 1,
			conditions: []metav1.Condition{
				condition("Ready", metav1.ConditionTrue, 4, "synced provider state is available", "Available"),
			},
			wantPhase:     showroomv1alpha1.PhaseReady,
			wantOperation: "synced provider state is available",
		},
		{
			name:       "cross-cluster claim accepts provider conditions from another generation domain",
			generation: 1,
			conditions: []metav1.Condition{
				condition("Synced", metav1.ConditionTrue, 4, "", "ReconcileSuccess"),
				condition("Ready", metav1.ConditionFalse, 4, "Unready resources: admin-kubeconfig-resource, shoot-cluster-resource", "Creating"),
				condition("Responsive", metav1.ConditionFalse, 4, "Too many watch events; allowing events periodically", "WatchCircuitOpen"),
			},
			wantPhase:     showroomv1alpha1.PhaseFailed,
			wantOperation: "Unready resources: admin-kubeconfig-resource, shoot-cluster-resource",
			wantError:     "Too many watch events; allowing events periodically",
		},
		{
			name:       "zero observed generation remains compatible",
			generation: 13,
			conditions: []metav1.Condition{
				condition("Ready", metav1.ConditionTrue, 0, "available without generation", "Available"),
			},
			wantPhase:     showroomv1alpha1.PhaseReady,
			wantOperation: "available without generation",
		},
		{
			name:       "reason is used when failure message is empty",
			generation: 14,
			conditions: []metav1.Condition{
				condition("Synced", metav1.ConditionFalse, 14, "", "ReconcileError"),
			},
			wantPhase: showroomv1alpha1.PhaseFailed,
			wantError: "ReconcileError",
		},
		{
			name:       "condition identity is used when failure message and reason are empty",
			generation: 15,
			conditions: []metav1.Condition{
				condition("Synced", metav1.ConditionFalse, 15, "", ""),
			},
			wantPhase: showroomv1alpha1.PhaseFailed,
			wantError: "Synced=False",
		},
		{
			name:       "ready reason is used when message is empty",
			generation: 16,
			conditions: []metav1.Condition{
				condition("Ready", metav1.ConditionTrue, 16, "", "Available"),
			},
			wantPhase:     showroomv1alpha1.PhaseReady,
			wantOperation: "Available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := conditionTarget(t, tt.generation, tt.conditions...)
			phase, operation, failure := extractPhase(check, obj)
			if phase != tt.wantPhase || operation != tt.wantOperation || failure != tt.wantError {
				t.Fatalf("extractPhase() = (%q, %q, %q), want (%q, %q, %q)",
					phase, operation, failure, tt.wantPhase, tt.wantOperation, tt.wantError)
			}
		})
	}
}

func TestConditionReadinessObservedGenerationMatchingIsOptIn(t *testing.T) {
	check := providerConditionCheck()
	check.Conditions.MatchObservedGeneration = true

	t.Run("cross-sync generation mismatch is ignored only when opted in", func(t *testing.T) {
		obj := conditionTarget(t, 1,
			condition("Ready", metav1.ConditionTrue, 4, "provider state is available", "Available"),
		)
		phase, operation, failure := extractPhase(check, obj)
		if phase != showroomv1alpha1.PhasePending || operation != "" || failure != "" {
			t.Fatalf("extractPhase() = (%q, %q, %q), want (Pending, empty, empty)", phase, operation, failure)
		}
	})

	t.Run("stale failure is ignored when ready condition is current", func(t *testing.T) {
		obj := conditionTarget(t, 11,
			condition("Synced", metav1.ConditionFalse, 10, "old failure", "ReconcileError"),
			condition("Ready", metav1.ConditionTrue, 11, "current state is available", "Available"),
		)
		phase, operation, failure := extractPhase(check, obj)
		if phase != showroomv1alpha1.PhaseReady || operation != "current state is available" || failure != "" {
			t.Fatalf("extractPhase() = (%q, %q, %q), want (Ready, current state is available, empty)",
				phase, operation, failure)
		}
	})

	t.Run("zero observed generation remains compatible", func(t *testing.T) {
		obj := conditionTarget(t, 13,
			condition("Ready", metav1.ConditionTrue, 0, "available without generation", "Available"),
		)
		phase, operation, failure := extractPhase(check, obj)
		if phase != showroomv1alpha1.PhaseReady || operation != "available without generation" || failure != "" {
			t.Fatalf("extractPhase() = (%q, %q, %q), want (Ready, available without generation, empty)",
				phase, operation, failure)
		}
	})
}

func TestConditionReadinessIsIndependentOfTargetListOrder(t *testing.T) {
	check := providerConditionCheck()
	conditions := []metav1.Condition{
		condition("Ready", metav1.ConditionFalse, 3, "creating", "Creating"),
		condition("Synced", metav1.ConditionFalse, 3, "invalid parameters", "ReconcileError"),
		condition("Responsive", metav1.ConditionTrue, 3, "provider reachable", "Responsive"),
	}

	forward := conditionTarget(t, 3, conditions...)
	reversed := conditionTarget(t, 3, conditions[2], conditions[1], conditions[0])
	forwardPhase, forwardOperation, forwardError := extractPhase(check, forward)
	reversePhase, reverseOperation, reverseError := extractPhase(check, reversed)
	if forwardPhase != reversePhase || forwardOperation != reverseOperation || forwardError != reverseError {
		t.Fatalf("condition order changed result: forward=(%q, %q, %q), reverse=(%q, %q, %q)",
			forwardPhase, forwardOperation, forwardError, reversePhase, reverseOperation, reverseError)
	}
}

func TestConditionReadinessIgnoresMalformedAndAmbiguousConditions(t *testing.T) {
	check := providerConditionCheck()

	t.Run("malformed entry does not hide valid readiness", func(t *testing.T) {
		obj := conditionTarget(t, 2, condition("Ready", metav1.ConditionTrue, 2, "available", "Available"))
		raw := obj.Object["status"].(map[string]any)["conditions"].([]any)
		obj.Object["status"].(map[string]any)["conditions"] = append([]any{"not-a-condition"}, raw...)

		phase, operation, failure := extractPhase(check, obj)
		if phase != showroomv1alpha1.PhaseReady || operation != "available" || failure != "" {
			t.Fatalf("extractPhase() = (%q, %q, %q), want (Ready, available, empty)", phase, operation, failure)
		}
	})

	t.Run("duplicate current type is not selected by list order", func(t *testing.T) {
		first := condition("Ready", metav1.ConditionTrue, 2, "available", "Available")
		second := condition("Ready", metav1.ConditionFalse, 2, "creating", "Creating")
		for _, obj := range []*unstructured.Unstructured{
			conditionTarget(t, 2, first, second),
			conditionTarget(t, 2, second, first),
		} {
			phase, operation, failure := extractPhase(check, obj)
			if phase != showroomv1alpha1.PhasePending || operation != "" || failure != "" {
				t.Fatalf("extractPhase() = (%q, %q, %q), want (Pending, empty, empty)", phase, operation, failure)
			}
		}
	})
}

func providerConditionCheck() *showroomv1alpha1.ReadinessCheck {
	return &showroomv1alpha1.ReadinessCheck{
		Conditions: &showroomv1alpha1.ConditionReadinessCheck{
			Ready: showroomv1alpha1.ConditionMatch{Type: "Ready", Status: metav1.ConditionTrue},
			Failures: []showroomv1alpha1.ConditionMatch{
				{Type: "Synced", Status: metav1.ConditionFalse},
				{Type: "Responsive", Status: metav1.ConditionFalse},
			},
		},
	}
}

func condition(conditionType string, status metav1.ConditionStatus, observedGeneration int64, message, reason string) metav1.Condition {
	return metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: observedGeneration,
		Message:            message,
		Reason:             reason,
	}
}

func conditionTarget(t *testing.T, generation int64, conditions ...metav1.Condition) *unstructured.Unstructured {
	t.Helper()
	raw := make([]any, 0, len(conditions))
	for i := range conditions {
		fields, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&conditions[i])
		if err != nil {
			t.Fatalf("convert condition to unstructured: %v", err)
		}
		raw = append(raw, fields)
	}
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.apeirora.eu/v1",
		"kind":       "ProviderCluster",
		"metadata": map[string]any{
			"name":       "target",
			"generation": generation,
		},
		"status": map[string]any{"conditions": raw},
	}}
	return obj
}
