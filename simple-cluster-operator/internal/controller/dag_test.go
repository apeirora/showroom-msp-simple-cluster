/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	rendertpl "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/template"
)

func scpcWith(resources []showroomv1alpha1.ResourceSpec, kcResourceName string) *showroomv1alpha1.SimpleClusterProviderConfig {
	return &showroomv1alpha1.SimpleClusterProviderConfig{
		Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{
			Resources: resources,
			KubeconfigSource: showroomv1alpha1.KubeconfigSource{
				ResourceName: kcResourceName,
				Inline:       &showroomv1alpha1.KubeconfigInlineSource{KubeconfigPath: "status.kubeconfig"},
			},
		},
	}
}

func res(name string, deps ...string) showroomv1alpha1.ResourceSpec {
	return showroomv1alpha1.ResourceSpec{
		Name:       name,
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Template:   "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: x}\n",
		DependsOn:  deps,
	}
}

func resWithReadiness(check *showroomv1alpha1.ReadinessCheck) showroomv1alpha1.ResourceSpec {
	r := res("a")
	r.Readiness = check
	return r
}

func TestValidateAndTopoSort_HappyPaths(t *testing.T) {
	tests := []struct {
		name      string
		resources []showroomv1alpha1.ResourceSpec
		want      []string
	}{
		{
			name:      "single resource",
			resources: []showroomv1alpha1.ResourceSpec{res("a")},
			want:      []string{"a"},
		},
		{
			name:      "linear chain",
			resources: []showroomv1alpha1.ResourceSpec{res("a"), res("b", "a"), res("c", "b")},
			want:      []string{"a", "b", "c"},
		},
		{
			name:      "diamond DAG",
			resources: []showroomv1alpha1.ResourceSpec{res("a"), res("b", "a"), res("c", "a"), res("d", "b", "c")},
			want:      []string{"a", "b", "c", "d"},
		},
		{
			name:      "multi-root single leaf",
			resources: []showroomv1alpha1.ResourceSpec{res("a"), res("b"), res("c", "a", "b")},
			want:      []string{"a", "b", "c"},
		},
		{
			name: "legacy scalar readiness",
			resources: []showroomv1alpha1.ResourceSpec{resWithReadiness(&showroomv1alpha1.ReadinessCheck{
				PhasePath:       "status.phase",
				PhaseReadyValue: "Ready",
			})},
			want: []string{"a"},
		},
		{
			name: "typed condition readiness",
			resources: []showroomv1alpha1.ResourceSpec{resWithReadiness(&showroomv1alpha1.ReadinessCheck{
				Conditions: &showroomv1alpha1.ConditionReadinessCheck{
					Ready: showroomv1alpha1.ConditionMatch{Type: "Ready", Status: metav1.ConditionTrue},
					Failures: []showroomv1alpha1.ConditionMatch{
						{Type: "Synced", Status: metav1.ConditionFalse},
					},
				},
			})},
			want: []string{"a"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateAndTopoSort(scpcWith(tt.resources, tt.resources[len(tt.resources)-1].Name))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len(order)=%d want %d (got %v)", len(got), len(tt.want), got)
			}
			// Verify dependencies precede dependents rather than asserting
			// an exact order (Kahn's output is one of possibly several
			// valid topological orderings).
			pos := make(map[string]int)
			for i, name := range got {
				pos[name] = i
			}
			for _, r := range tt.resources {
				for _, d := range r.DependsOn {
					if pos[d] >= pos[r.Name] {
						t.Errorf("dependency violated: %s (pos %d) must precede %s (pos %d)",
							d, pos[d], r.Name, pos[r.Name])
					}
				}
			}
		})
	}
}

func TestValidateAndTopoSort_Errors(t *testing.T) {
	tests := []struct {
		name      string
		scpc      *showroomv1alpha1.SimpleClusterProviderConfig
		errSubstr string
	}{
		{
			name:      "empty resources",
			scpc:      scpcWith(nil, ""),
			errSubstr: "resources must not be empty",
		},
		{
			name:      "duplicate name",
			scpc:      scpcWith([]showroomv1alpha1.ResourceSpec{res("a"), res("a")}, "a"),
			errSubstr: `duplicate resource name "a"`,
		},
		{
			name:      "self reference",
			scpc:      scpcWith([]showroomv1alpha1.ResourceSpec{res("a", "a")}, "a"),
			errSubstr: "self-reference",
		},
		{
			name:      "unknown dep",
			scpc:      scpcWith([]showroomv1alpha1.ResourceSpec{res("a", "ghost")}, "a"),
			errSubstr: `unknown resource "ghost"`,
		},
		{
			name:      "duplicate dependsOn entry",
			scpc:      scpcWith([]showroomv1alpha1.ResourceSpec{res("a"), res("b", "a", "a")}, "b"),
			errSubstr: `duplicate dependsOn entry "a"`,
		},
		{
			name:      "cycle",
			scpc:      scpcWith([]showroomv1alpha1.ResourceSpec{res("a", "b"), res("b", "a")}, "a"),
			errSubstr: "cycle detected",
		},
		{
			name:      "missing kubeconfigSource.resourceName",
			scpc:      scpcWith([]showroomv1alpha1.ResourceSpec{res("a")}, ""),
			errSubstr: "kubeconfigSource.resourceName is required",
		},
		{
			name:      "kubeconfigSource.resourceName mismatch",
			scpc:      scpcWith([]showroomv1alpha1.ResourceSpec{res("a")}, "ghost"),
			errSubstr: `does not match any resource`,
		},
		{
			name: "bad apiVersion",
			scpc: scpcWith([]showroomv1alpha1.ResourceSpec{{
				Name:       "a",
				APIVersion: "core/gardener/cloud/v1beta1", // too many slashes
				Kind:       "Shoot",
				Template:   "...",
			}}, "a"),
			errSubstr: "bad apiVersion",
		},
		{
			name: "missing kind",
			scpc: scpcWith([]showroomv1alpha1.ResourceSpec{{
				Name:       "a",
				APIVersion: "v1",
				Template:   "...",
			}}, "a"),
			errSubstr: "kind is required",
		},
		{
			name: "reserved lifecycle deletion annotation",
			scpc: func() *showroomv1alpha1.SimpleClusterProviderConfig {
				resource := res("a")
				resource.DeletionAnnotations = map[string]string{
					"showroom.apeirora.eu/simple-cluster-uid": "overridden",
				}
				return scpcWith([]showroomv1alpha1.ResourceSpec{resource}, "a")
			}(),
			errSubstr: "is reserved by the SimpleCluster lifecycle",
		},
		{
			name:      "empty readiness mode",
			scpc:      scpcWith([]showroomv1alpha1.ResourceSpec{resWithReadiness(&showroomv1alpha1.ReadinessCheck{})}, "a"),
			errSubstr: "phasePath and phaseReadyValue are required",
		},
		{
			name: "scalar readiness missing ready value",
			scpc: scpcWith([]showroomv1alpha1.ResourceSpec{resWithReadiness(&showroomv1alpha1.ReadinessCheck{
				PhasePath: "status.phase",
			})}, "a"),
			errSubstr: "phasePath and phaseReadyValue are required",
		},
		{
			name: "mixed scalar and condition readiness",
			scpc: scpcWith([]showroomv1alpha1.ResourceSpec{resWithReadiness(&showroomv1alpha1.ReadinessCheck{
				PhasePath: "status.phase",
				Conditions: &showroomv1alpha1.ConditionReadinessCheck{
					Ready: showroomv1alpha1.ConditionMatch{Type: "Ready", Status: metav1.ConditionTrue},
				},
			})}, "a"),
			errSubstr: "conditions cannot be combined with scalar readiness fields",
		},
		{
			name: "condition readiness missing ready type",
			scpc: scpcWith([]showroomv1alpha1.ResourceSpec{resWithReadiness(&showroomv1alpha1.ReadinessCheck{
				Conditions: &showroomv1alpha1.ConditionReadinessCheck{
					Ready: showroomv1alpha1.ConditionMatch{Status: metav1.ConditionTrue},
				},
			})}, "a"),
			errSubstr: "conditions.ready.type is required",
		},
		{
			name: "condition readiness has invalid status",
			scpc: scpcWith([]showroomv1alpha1.ResourceSpec{resWithReadiness(&showroomv1alpha1.ReadinessCheck{
				Conditions: &showroomv1alpha1.ConditionReadinessCheck{
					Ready: showroomv1alpha1.ConditionMatch{Type: "Ready", Status: "yes"},
				},
			})}, "a"),
			errSubstr: "must be True, False, or Unknown",
		},
		{
			name: "duplicate failure condition type",
			scpc: scpcWith([]showroomv1alpha1.ResourceSpec{resWithReadiness(&showroomv1alpha1.ReadinessCheck{
				Conditions: &showroomv1alpha1.ConditionReadinessCheck{
					Ready: showroomv1alpha1.ConditionMatch{Type: "Ready", Status: metav1.ConditionTrue},
					Failures: []showroomv1alpha1.ConditionMatch{
						{Type: "Synced", Status: metav1.ConditionFalse},
						{Type: "Synced", Status: metav1.ConditionUnknown},
					},
				},
			})}, "a"),
			errSubstr: "duplicate failure condition type",
		},
		{
			name: "ready matcher repeated as failure",
			scpc: scpcWith([]showroomv1alpha1.ResourceSpec{resWithReadiness(&showroomv1alpha1.ReadinessCheck{
				Conditions: &showroomv1alpha1.ConditionReadinessCheck{
					Ready: showroomv1alpha1.ConditionMatch{Type: "Ready", Status: metav1.ConditionTrue},
					Failures: []showroomv1alpha1.ConditionMatch{
						{Type: "Ready", Status: metav1.ConditionTrue},
					},
				},
			})}, "a"),
			errSubstr: "ready matcher must not also be a failure matcher",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateAndTopoSort(tt.scpc)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errSubstr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.errSubstr)
			}
		})
	}
}

func TestValidateDeletionAnnotationsRejectsEveryLifecycleKey(t *testing.T) {
	reserved := []string{
		rendertpl.AnnoSimpleCluster,
		rendertpl.AnnoSimpleClusterProvider,
		rendertpl.AnnoSimpleClusterUID,
		rendertpl.AnnoSimpleClusterProviderNamespace,
		rendertpl.AnnoSimpleClusterProviderUID,
		rendertpl.AnnoAllowDelete,
	}
	for _, key := range reserved {
		t.Run(key, func(t *testing.T) {
			if err := validateDeletionAnnotations(map[string]string{key: "overridden"}); err == nil {
				t.Fatalf("reserved annotation %q was accepted", key)
			}
		})
	}
	if err := validateDeletionAnnotations(map[string]string{"provider.example/confirm": "true"}); err != nil {
		t.Fatalf("provider deletion annotation was rejected: %v", err)
	}
}
