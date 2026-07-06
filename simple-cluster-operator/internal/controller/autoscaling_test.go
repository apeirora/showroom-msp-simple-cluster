package controller

import (
	"testing"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/utils/ptr"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
)

func TestSetRoleCounts(t *testing.T) {
	cases := []struct {
		name    string
		workers showroomv1alpha1.WorkersSpec
		phase   showroomv1alpha1.Phase
		wantWKR string
	}{
		{
			name:    "fixed count provisioning",
			workers: showroomv1alpha1.WorkersSpec{Nodes: ptr.To[int32](4)},
			phase:   showroomv1alpha1.PhaseProvisioning,
			wantWKR: "0/4",
		},
		{
			name:    "fixed count ready",
			workers: showroomv1alpha1.WorkersSpec{Nodes: ptr.To[int32](4)},
			phase:   showroomv1alpha1.PhaseReady,
			wantWKR: "4/4",
		},
		{
			name: "autoscaling range",
			workers: showroomv1alpha1.WorkersSpec{
				Autoscaling: &showroomv1alpha1.AutoscalingSpec{Min: 2, Max: 5},
			},
			phase:   showroomv1alpha1.PhaseReady,
			wantWKR: "2/2-5",
		},
		{
			name: "autoscaling degenerate range",
			workers: showroomv1alpha1.WorkersSpec{
				Autoscaling: &showroomv1alpha1.AutoscalingSpec{Min: 3, Max: 3},
			},
			phase:   showroomv1alpha1.PhaseProvisioning,
			wantWKR: "0/3",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sc := &showroomv1alpha1.SimpleCluster{}
			sc.Spec.Workers = tc.workers
			sc.Status.Phase = tc.phase
			setRoleCounts(sc)
			if sc.Status.WKR != tc.wantWKR {
				t.Errorf("WKR: got %q, want %q", sc.Status.WKR, tc.wantWKR)
			}
			if sc.Status.CP != "-/-" {
				t.Errorf("CP: got %q, want %q", sc.Status.CP, "-/-")
			}
		})
	}
}

func TestSetAutoscalingCondition(t *testing.T) {
	scpc := &showroomv1alpha1.SimpleClusterProviderConfig{}
	scpc.Name = "flat"

	sc := &showroomv1alpha1.SimpleCluster{}
	sc.Spec.Workers = showroomv1alpha1.WorkersSpec{Nodes: ptr.To[int32](2)}
	setAutoscalingCondition(sc, scpc)
	if apimeta.FindStatusCondition(sc.Status.Conditions, showroomv1alpha1.ConditionAutoscalingSupported) != nil {
		t.Error("condition present without an autoscaling request")
	}

	sc.Spec.Workers = showroomv1alpha1.WorkersSpec{
		Autoscaling: &showroomv1alpha1.AutoscalingSpec{Min: 1, Max: 3},
	}
	setAutoscalingCondition(sc, scpc)
	if !apimeta.IsStatusConditionFalse(sc.Status.Conditions, showroomv1alpha1.ConditionAutoscalingSupported) {
		t.Error("condition not False for an SCPC without autoscaling support")
	}

	scpc.Spec.Autoscaling = true
	setAutoscalingCondition(sc, scpc)
	if !apimeta.IsStatusConditionTrue(sc.Status.Conditions, showroomv1alpha1.ConditionAutoscalingSupported) {
		t.Error("condition not True for an SCPC with autoscaling support")
	}

	sc.Spec.Workers = showroomv1alpha1.WorkersSpec{Nodes: ptr.To[int32](2)}
	setAutoscalingCondition(sc, scpc)
	if apimeta.FindStatusCondition(sc.Status.Conditions, showroomv1alpha1.ConditionAutoscalingSupported) != nil {
		t.Error("condition not removed after the autoscaling request went away")
	}
}
