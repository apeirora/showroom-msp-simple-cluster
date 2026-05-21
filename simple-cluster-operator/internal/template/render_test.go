/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package template

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
)

func minimalSCPC(inputs []showroomv1alpha1.InputDeclaration) *showroomv1alpha1.SimpleClusterProviderConfig {
	return &showroomv1alpha1.SimpleClusterProviderConfig{
		Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{
			Target: showroomv1alpha1.TargetSpec{Namespace: "default"},
			Inputs: inputs,
		},
	}
}

func minimalSC(workers int32, inputs map[string]string) *showroomv1alpha1.SimpleCluster {
	sc := &showroomv1alpha1.SimpleCluster{}
	sc.Name = "foo"
	sc.Namespace = "users"
	sc.Spec.Workers = workers
	sc.Spec.Inputs = inputs
	return sc
}

func TestRender_SingleResource(t *testing.T) {
	scpc := minimalSCPC([]showroomv1alpha1.InputDeclaration{
		{Name: "region", Type: showroomv1alpha1.InputTypeString, Default: "eu-de-1"},
		{Name: "max", Type: showroomv1alpha1.InputTypeInt, Default: "5"},
	})
	sc := minimalSC(3, map[string]string{"region": "us-east-1"})
	r := &showroomv1alpha1.ResourceSpec{
		Name:       "config",
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Template: `apiVersion: v1
kind: ConfigMap
metadata:
  name: ignored
data:
  workers: "{{ .SC.workers }}"
  region: "{{ .Inputs.region }}"
  max: "{{ .Inputs.max }}"
`,
	}

	obj, err := Render(scpc, sc, r, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if obj.GetAPIVersion() != "v1" || obj.GetKind() != "ConfigMap" {
		t.Errorf("apiVersion/kind not enforced: got %s/%s", obj.GetAPIVersion(), obj.GetKind())
	}
	if obj.GetName() != "foo-config" {
		t.Errorf("name not enforced: got %q", obj.GetName())
	}
	if obj.GetNamespace() != "default" {
		t.Errorf("namespace not enforced: got %q", obj.GetNamespace())
	}
	annos := obj.GetAnnotations()
	if annos[AnnoSCName] != "foo" || annos[AnnoResourceName] != "config" {
		t.Errorf("tracking annotations missing: %+v", annos)
	}
	data, _, _ := unstructured.NestedStringMap(obj.Object, "data")
	if data["workers"] != "3" {
		t.Errorf("SC.workers not substituted: %q", data["workers"])
	}
	if data["region"] != "us-east-1" {
		t.Errorf("input override not applied: %q", data["region"])
	}
	if data["max"] != "5" {
		t.Errorf("input default not applied: %q", data["max"])
	}
}

func TestRender_WithPredecessor(t *testing.T) {
	scpc := minimalSCPC(nil)
	sc := minimalSC(1, nil)

	predecessor := &unstructured.Unstructured{Object: map[string]any{
		"status": map[string]any{
			"networkRef": map[string]any{"id": "net-1234"},
		},
	}}
	r := &showroomv1alpha1.ResourceSpec{
		Name:       "shoot",
		APIVersion: "v1",
		Kind:       "ConfigMap",
		DependsOn:  []string{"network"},
		Template: `apiVersion: v1
kind: ConfigMap
metadata:
  name: x
data:
  network: "{{ .Resources.network.status.networkRef.id }}"
`,
	}

	obj, err := Render(scpc, sc, r, map[string]*unstructured.Unstructured{"network": predecessor})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	data, _, _ := unstructured.NestedStringMap(obj.Object, "data")
	if data["network"] != "net-1234" {
		t.Errorf("predecessor field not read: %q", data["network"])
	}
}

func TestRender_MissingPredecessorField(t *testing.T) {
	// missingkey=error: referencing a missing field on a predecessor
	// (or referencing a predecessor not in DependsOn) must fail loudly.
	scpc := minimalSCPC(nil)
	sc := minimalSC(1, nil)
	r := &showroomv1alpha1.ResourceSpec{
		Name:       "shoot",
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Template: `apiVersion: v1
kind: ConfigMap
metadata: {name: x}
data:
  net: "{{ .Resources.network.status.id }}"
`,
	}
	_, err := Render(scpc, sc, r, nil)
	if err == nil {
		t.Fatal("expected error for missing predecessor")
	}
}

func TestRender_UnknownInputRejected(t *testing.T) {
	scpc := minimalSCPC([]showroomv1alpha1.InputDeclaration{
		{Name: "region", Type: showroomv1alpha1.InputTypeString, Default: "x"},
	})
	sc := minimalSC(1, map[string]string{"ghost": "bar"})
	r := &showroomv1alpha1.ResourceSpec{
		Name:       "config",
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Template:   "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: x}\n",
	}
	_, err := Render(scpc, sc, r, nil)
	if err == nil || !strings.Contains(err.Error(), `unknown input "ghost"`) {
		t.Fatalf("expected unknown-input error, got %v", err)
	}
}

func TestRender_InputTypes(t *testing.T) {
	scpc := minimalSCPC([]showroomv1alpha1.InputDeclaration{
		{Name: "i", Type: showroomv1alpha1.InputTypeInt, Default: "7"},
		{Name: "b", Type: showroomv1alpha1.InputTypeBool, Default: "true"},
		{Name: "d", Type: showroomv1alpha1.InputTypeDuration, Default: "5m"},
	})
	sc := minimalSC(1, nil)
	r := &showroomv1alpha1.ResourceSpec{
		Name:       "config",
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Template: `apiVersion: v1
kind: ConfigMap
metadata: {name: x}
data:
  i: "{{ add .Inputs.i 1 }}"
  b: "{{ if .Inputs.b }}yes{{ else }}no{{ end }}"
  d: "{{ .Inputs.d }}"
`,
	}
	obj, err := Render(scpc, sc, r, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	data, _, _ := unstructured.NestedStringMap(obj.Object, "data")
	if data["i"] != "8" {
		t.Errorf("int + 1 expected 8, got %q", data["i"])
	}
	if data["b"] != "yes" {
		t.Errorf("bool eval expected yes, got %q", data["b"])
	}
	if data["d"] != "5m0s" {
		t.Errorf("duration default expected 5m0s, got %q", data["d"])
	}
}

func TestRender_EnvFuncRemoved(t *testing.T) {
	scpc := minimalSCPC(nil)
	sc := minimalSC(1, nil)
	r := &showroomv1alpha1.ResourceSpec{
		Name:       "x",
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Template:   `{{ env "HOME" }}`,
	}
	_, err := Render(scpc, sc, r, nil)
	if err == nil {
		t.Fatal("expected error: env should be removed from sprig funcs")
	}
}

func TestSanitiseTargetName(t *testing.T) {
	cases := []struct {
		prefix, base, want string
	}{
		{"", "MyCluster", "mycluster"},
		{"ap-", "Foo_Bar", "ap-foo-bar"},
		{"--", "x", "x"},
		{"", "", "sc"},
		{"", strings.Repeat("a", 80), strings.Repeat("a", 63)},
	}
	for _, tc := range cases {
		got := SanitiseTargetName(tc.prefix, tc.base)
		if got != tc.want {
			t.Errorf("Sanitise(%q,%q)=%q want %q", tc.prefix, tc.base, got, tc.want)
		}
	}
}
