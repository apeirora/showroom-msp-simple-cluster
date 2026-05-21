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

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
)

func statusMap(entries ...*showroomv1alpha1.ResourceStatus) map[string]*showroomv1alpha1.ResourceStatus {
	out := make(map[string]*showroomv1alpha1.ResourceStatus, len(entries))
	for _, e := range entries {
		out[e.Name] = e
	}
	return out
}

func st(name string, phase showroomv1alpha1.Phase, lastError ...string) *showroomv1alpha1.ResourceStatus {
	rs := &showroomv1alpha1.ResourceStatus{Name: name, Phase: phase}
	if len(lastError) > 0 {
		rs.LastError = lastError[0]
	}
	return rs
}

func TestAggregatePhase(t *testing.T) {
	cases := []struct {
		name     string
		order    []string
		statuses map[string]*showroomv1alpha1.ResourceStatus
		want     showroomv1alpha1.Phase
	}{
		{
			name:     "all Ready",
			order:    []string{"a", "b"},
			statuses: statusMap(st("a", showroomv1alpha1.PhaseReady), st("b", showroomv1alpha1.PhaseReady)),
			want:     showroomv1alpha1.PhaseReady,
		},
		{
			name:     "any Failed beats Provisioning",
			order:    []string{"a", "b"},
			statuses: statusMap(st("a", showroomv1alpha1.PhaseProvisioning), st("b", showroomv1alpha1.PhaseFailed)),
			want:     showroomv1alpha1.PhaseFailed,
		},
		{
			name:     "Ready + Provisioning",
			order:    []string{"a", "b"},
			statuses: statusMap(st("a", showroomv1alpha1.PhaseReady), st("b", showroomv1alpha1.PhaseProvisioning)),
			want:     showroomv1alpha1.PhaseProvisioning,
		},
		{
			name:     "Pending counted as not-Ready",
			order:    []string{"a", "b"},
			statuses: statusMap(st("a", showroomv1alpha1.PhaseReady), st("b", showroomv1alpha1.PhasePending)),
			want:     showroomv1alpha1.PhaseProvisioning,
		},
		{
			name:     "missing status counted as not-Ready",
			order:    []string{"a", "b"},
			statuses: statusMap(st("a", showroomv1alpha1.PhaseReady)),
			want:     showroomv1alpha1.PhaseProvisioning,
		},
		{
			name:     "Failed wins even if others Ready",
			order:    []string{"a", "b"},
			statuses: statusMap(st("a", showroomv1alpha1.PhaseReady), st("b", showroomv1alpha1.PhaseFailed)),
			want:     showroomv1alpha1.PhaseFailed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := aggregatePhase(tc.order, tc.statuses)
			if got != tc.want {
				t.Errorf("aggregatePhase=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestAggregateMessage(t *testing.T) {
	order := []string{"a", "b"}
	statuses := statusMap(
		st("a", showroomv1alpha1.PhaseReady),
		st("b", showroomv1alpha1.PhaseFailed, "boom"),
	)
	msg := aggregateMessage(order, statuses)
	if !strings.Contains(msg, "b: boom") {
		t.Errorf("expected failure last-error in message, got %q", msg)
	}
	if strings.Contains(msg, "a:") {
		t.Errorf("expected Ready resources omitted, got %q", msg)
	}
}

func TestAggregateMessage_AllReady(t *testing.T) {
	order := []string{"a"}
	statuses := statusMap(st("a", showroomv1alpha1.PhaseReady))
	msg := aggregateMessage(order, statuses)
	if msg != "all resources Ready" {
		t.Errorf("expected 'all resources Ready', got %q", msg)
	}
}

func TestOrderedStatuses(t *testing.T) {
	order := []string{"a", "b", "c"}
	statuses := statusMap(
		st("a", showroomv1alpha1.PhaseReady),
		st("c", showroomv1alpha1.PhaseFailed, "boom"),
	)
	got := orderedStatuses(order, statuses)
	if len(got) != 3 {
		t.Fatalf("len=%d want 3", len(got))
	}
	if got[0].Name != "a" || got[1].Name != "b" || got[2].Name != "c" {
		t.Errorf("unexpected order: %+v", got)
	}
	if got[1].Phase != showroomv1alpha1.PhasePending {
		t.Errorf("missing status should yield Pending, got %v", got[1].Phase)
	}
	if got[2].LastError != "boom" {
		t.Errorf("expected LastError preserved, got %q", got[2].LastError)
	}
}
