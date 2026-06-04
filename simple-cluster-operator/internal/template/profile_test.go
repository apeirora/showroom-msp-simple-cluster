/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package template

import (
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
)

func profile(name, cpu, mem string) showroomv1alpha1.Profile {
	return showroomv1alpha1.Profile{
		Name:   name,
		CPU:    resource.MustParse(cpu),
		Memory: resource.MustParse(mem),
	}
}

func scWithRequest(cpu, mem string, cp *showroomv1alpha1.ControlPlaneSpec) *showroomv1alpha1.SimpleCluster {
	sc := minimalSC(3, nil)
	sc.Spec.Workers.Requested = showroomv1alpha1.NodeResources{
		CPU:    resource.MustParse(cpu),
		Memory: resource.MustParse(mem),
	}
	sc.Spec.ControlPlane = cp
	return sc
}

func TestSelectProfile_SmallestFit(t *testing.T) {
	scpc := minimalSCPC(nil)
	scpc.Spec.Profiles = []showroomv1alpha1.Profile{
		profile("xl", "16", "64Gi"),
		profile("s", "2", "8Gi"),
		profile("m", "4", "16Gi"),
		profile("l", "8", "32Gi"),
	}
	sc := scWithRequest("3", "12Gi", nil)

	name, err := SelectProfile(scpc, sc)
	if err != nil {
		t.Fatalf("SelectProfile: %v", err)
	}
	if name != "m" {
		t.Errorf("want smallest fitting profile %q, got %q", "m", name)
	}
}

func TestSelectProfile_ExactFit(t *testing.T) {
	scpc := minimalSCPC(nil)
	scpc.Spec.Profiles = []showroomv1alpha1.Profile{
		profile("s", "2", "8Gi"),
		profile("m", "4", "16Gi"),
	}
	sc := scWithRequest("2", "8Gi", nil)

	name, err := SelectProfile(scpc, sc)
	if err != nil {
		t.Fatalf("SelectProfile: %v", err)
	}
	if name != "s" {
		t.Errorf("want %q, got %q", "s", name)
	}
}

func TestSelectProfile_ControlPlaneDrivesSize(t *testing.T) {
	scpc := minimalSCPC(nil)
	scpc.Spec.Profiles = []showroomv1alpha1.Profile{
		profile("s", "2", "8Gi"),
		profile("m", "4", "16Gi"),
		profile("l", "8", "32Gi"),
	}
	// Workers fit in "s", but the control plane needs "l".
	cp := &showroomv1alpha1.ControlPlaneSpec{
		Nodes: 1,
		Requested: showroomv1alpha1.NodeResources{
			CPU:    resource.MustParse("6"),
			Memory: resource.MustParse("20Gi"),
		},
	}
	sc := scWithRequest("2", "8Gi", cp)

	name, err := SelectProfile(scpc, sc)
	if err != nil {
		t.Fatalf("SelectProfile: %v", err)
	}
	if name != "l" {
		t.Errorf("control plane should drive size to %q, got %q", "l", name)
	}
}

func TestSelectProfile_NoneBigEnough(t *testing.T) {
	scpc := minimalSCPC(nil)
	scpc.Spec.Profiles = []showroomv1alpha1.Profile{
		profile("s", "2", "8Gi"),
		profile("m", "4", "16Gi"),
	}
	sc := scWithRequest("8", "8Gi", nil) // cpu exceeds every profile

	if _, err := SelectProfile(scpc, sc); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("want ErrNoProfile, got %v", err)
	}
}

func TestResolveInputs_FromProfile(t *testing.T) {
	scpc := minimalSCPC([]showroomv1alpha1.InputDeclaration{
		{Name: "machineType", From: showroomv1alpha1.InputSourceProfile},
	})
	scpc.Spec.Profiles = []showroomv1alpha1.Profile{
		profile("s", "2", "8Gi"),
		profile("m", "4", "16Gi"),
	}
	sc := scWithRequest("3", "10Gi", nil)

	inputs, err := ResolveInputs(scpc, sc)
	if err != nil {
		t.Fatalf("ResolveInputs: %v", err)
	}
	if inputs["machineType"] != "m" {
		t.Errorf("from=profile input should resolve to %q, got %v", "m", inputs["machineType"])
	}
}

func TestResolveInputs_FromProfileOverridePassthrough(t *testing.T) {
	scpc := minimalSCPC([]showroomv1alpha1.InputDeclaration{
		{Name: "machineType", From: showroomv1alpha1.InputSourceProfile},
	})
	scpc.Spec.Profiles = []showroomv1alpha1.Profile{profile("s", "2", "8Gi")}
	sc := scWithRequest("3", "10Gi", nil) // would not fit any profile
	sc.Spec.Inputs = map[string]string{"machineType": "custom-flavor"}

	inputs, err := ResolveInputs(scpc, sc)
	if err != nil {
		t.Fatalf("ResolveInputs: %v", err)
	}
	if inputs["machineType"] != "custom-flavor" {
		t.Errorf("explicit override should pass through unchanged, got %v", inputs["machineType"])
	}
}

func TestResolveInputs_FromProfileNoHardware(t *testing.T) {
	scpc := minimalSCPC([]showroomv1alpha1.InputDeclaration{
		{Name: "machineType", From: showroomv1alpha1.InputSourceProfile},
	})
	scpc.Spec.Profiles = []showroomv1alpha1.Profile{profile("s", "2", "8Gi")}
	sc := scWithRequest("16", "8Gi", nil)

	if _, err := ResolveInputs(scpc, sc); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("want ErrNoProfile, got %v", err)
	}
}
