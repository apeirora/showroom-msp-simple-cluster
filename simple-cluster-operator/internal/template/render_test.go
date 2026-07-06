/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package template

import (
	"strconv"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
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
	sc.Spec.Workers = showroomv1alpha1.WorkersSpec{Nodes: &workers}
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
  workers: "{{ .SC.workers.nodes }}"
  region: "{{ .Inputs.region }}"
  max: "{{ .Inputs.max }}"
`,
	}

	objs, err := Render(scpc, sc, r, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("Render: expected 1 object, got %d", len(objs))
	}
	obj := objs[0]
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
	if annos[AnnoSimpleCluster] != "users/foo" {
		t.Errorf("%s = %q want users/foo", AnnoSimpleCluster, annos[AnnoSimpleCluster])
	}
	data, _, _ := unstructured.NestedStringMap(obj.Object, "data")
	if data["workers"] != "3" {
		t.Errorf("SC.workers.nodes not substituted: %q", data["workers"])
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

	objs, err := Render(scpc, sc, r, map[string]Predecessor{
		"network": {Objects: []*unstructured.Unstructured{predecessor}},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	data, _, _ := unstructured.NestedStringMap(objs[0].Object, "data")
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
	objs, err := Render(scpc, sc, r, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("Render: expected 1 object, got %d", len(objs))
	}
	obj := objs[0]
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

func TestRender_EnumInput(t *testing.T) {
	values := []showroomv1alpha1.InputEnumValue{
		{Key: "eu-de-1", Label: "Frankfurt"},
		{Key: "us-east-1", Label: "N. Virginia"},
	}
	scpc := minimalSCPC([]showroomv1alpha1.InputDeclaration{
		{Name: "region", Type: showroomv1alpha1.InputTypeEnum, Default: "eu-de-1", Values: values},
	})

	t.Run("default key renders", func(t *testing.T) {
		sc := minimalSC(1, nil)
		r := &showroomv1alpha1.ResourceSpec{
			Name: "config", APIVersion: "v1", Kind: "ConfigMap",
			Template: "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: x}\ndata:\n  region: \"{{ .Inputs.region }}\"\n",
		}
		objs, err := Render(scpc, sc, r, nil)
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		data, _, _ := unstructured.NestedStringMap(objs[0].Object, "data")
		if data["region"] != "eu-de-1" {
			t.Errorf("enum default not substituted: %q", data["region"])
		}
	})

	t.Run("valid override renders", func(t *testing.T) {
		sc := minimalSC(1, map[string]string{"region": "us-east-1"})
		r := &showroomv1alpha1.ResourceSpec{
			Name: "config", APIVersion: "v1", Kind: "ConfigMap",
			Template: "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: x}\ndata:\n  region: \"{{ .Inputs.region }}\"\n",
		}
		objs, err := Render(scpc, sc, r, nil)
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		data, _, _ := unstructured.NestedStringMap(objs[0].Object, "data")
		if data["region"] != "us-east-1" {
			t.Errorf("enum override not substituted: %q", data["region"])
		}
	})

	t.Run("unknown key rejected", func(t *testing.T) {
		sc := minimalSC(1, map[string]string{"region": "mars-1"})
		r := &showroomv1alpha1.ResourceSpec{
			Name: "config", APIVersion: "v1", Kind: "ConfigMap",
			Template: "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: x}\n",
		}
		_, err := Render(scpc, sc, r, nil)
		if err == nil || !strings.Contains(err.Error(), "not in allowed set") {
			t.Fatalf("expected enum-membership error, got %v", err)
		}
	})
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

func TestRender_Repeat(t *testing.T) {
	scpc := minimalSCPC(nil)
	sc := minimalSC(3, nil)
	r := &showroomv1alpha1.ResourceSpec{
		Name:       "vm",
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Repeat:     "{{ .SC.workers.nodes }}",
		Template: `apiVersion: v1
kind: ConfigMap
metadata: {name: x}
data:
  idx: "{{ .Index }}"
  priors: "{{ len .Prev }}"
`,
	}
	objs, err := Render(scpc, sc, r, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(objs) != 3 {
		t.Fatalf("expected 3 objects, got %d", len(objs))
	}
	for i, obj := range objs {
		wantName := "foo-vm-" + strconv.Itoa(i)
		if obj.GetName() != wantName {
			t.Errorf("obj %d: name=%q want %q", i, obj.GetName(), wantName)
		}
		data, _, _ := unstructured.NestedStringMap(obj.Object, "data")
		if data["idx"] != strconv.Itoa(i) {
			t.Errorf("obj %d: .Index = %q", i, data["idx"])
		}
		if data["priors"] != strconv.Itoa(i) {
			t.Errorf("obj %d: len(.Prev) = %q want %d", i, data["priors"], i)
		}
	}
}

func TestRender_RepeatedPredecessorSliceExposed(t *testing.T) {
	scpc := minimalSCPC(nil)
	sc := minimalSC(2, nil)
	pred0 := &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"ip": "10.0.0.1"}}}
	pred1 := &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"ip": "10.0.0.2"}}}
	r := &showroomv1alpha1.ResourceSpec{
		Name:       "svc",
		APIVersion: "v1",
		Kind:       "ConfigMap",
		DependsOn:  []string{"vm"},
		Template: `apiVersion: v1
kind: ConfigMap
metadata: {name: x}
data:
  first: "{{ (index .Resources.vm 0).status.ip }}"
  count: "{{ len .Resources.vm }}"
`,
	}
	objs, err := Render(scpc, sc, r, map[string]Predecessor{
		"vm": {Repeated: true, Objects: []*unstructured.Unstructured{pred0, pred1}},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	data, _, _ := unstructured.NestedStringMap(objs[0].Object, "data")
	if data["first"] != "10.0.0.1" {
		t.Errorf("indexed predecessor field = %q", data["first"])
	}
	if data["count"] != "2" {
		t.Errorf("len predecessor slice = %q", data["count"])
	}
}

func TestRender_RepeatZero(t *testing.T) {
	scpc := minimalSCPC(nil)
	sc := minimalSC(1, nil)
	r := &showroomv1alpha1.ResourceSpec{
		Name:       "vm",
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Repeat:     "0",
		Template:   "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: x}\n",
	}
	objs, err := Render(scpc, sc, r, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(objs) != 0 {
		t.Errorf("expected 0 objects, got %d", len(objs))
	}
}

func TestRender_RepeatBadValue(t *testing.T) {
	scpc := minimalSCPC(nil)
	sc := minimalSC(1, nil)
	r := &showroomv1alpha1.ResourceSpec{
		Name:       "vm",
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Repeat:     "abc",
		Template:   "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: x}\n",
	}
	_, err := Render(scpc, sc, r, nil)
	if err == nil || !strings.Contains(err.Error(), "is not an integer") {
		t.Fatalf("expected integer-parse error, got %v", err)
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

func TestRender_NameTemplate(t *testing.T) {
	scpc := minimalSCPC(nil)
	r := &showroomv1alpha1.ResourceSpec{
		Name:         "shoot",
		APIVersion:   "core.gardener.cloud/v1beta1",
		Kind:         "Shoot",
		NameTemplate: `{{- printf "%s/%s" .SC.namespace .SC.name | sha256sum | trunc 18 | printf "sc-%s" }}`,
		Template: `apiVersion: core.gardener.cloud/v1beta1
kind: Shoot
metadata:
  name: placeholder
`,
	}
	sc := minimalSC(1, nil)
	objs, err := Render(scpc, sc, r, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	name := objs[0].GetName()
	if len(name) != 21 {
		t.Errorf("expected 21-char name, got %d: %q", len(name), name)
	}
	if !strings.HasPrefix(name, "sc-") {
		t.Errorf("expected sc- prefix, got %q", name)
	}
	// deterministic
	objs2, _ := Render(scpc, sc, r, nil)
	if objs2[0].GetName() != name {
		t.Errorf("not deterministic: %q vs %q", name, objs2[0].GetName())
	}
}

func TestRender_ControlPlaneDefaultsToWorkers(t *testing.T) {
	scpc := minimalSCPC(nil)
	r := &showroomv1alpha1.ResourceSpec{
		Name:       "cp",
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Template: `apiVersion: v1
kind: ConfigMap
metadata:
  name: cp
data:
  nodes: "{{ .SC.controlPlane.nodes }}"
  cpu: "{{ .SC.controlPlane.requested.cpu }}"
  memory: "{{ .SC.controlPlane.requested.memory }}"
`,
	}

	// ControlPlane omitted: a flat-provider template still renders, defaulting
	// to a single control-plane node mirroring the worker resource ask.
	sc := minimalSC(3, nil)
	sc.Spec.Workers.Requested = showroomv1alpha1.NodeResources{
		CPU:    resource.MustParse("4"),
		Memory: resource.MustParse("8Gi"),
	}
	objs, err := Render(scpc, sc, r, nil)
	if err != nil {
		t.Fatalf("Render (cp omitted): %v", err)
	}
	data, _, _ := unstructured.NestedStringMap(objs[0].Object, "data")
	if data["nodes"] != "1" || data["cpu"] != "4" || data["memory"] != "8Gi" {
		t.Errorf("control-plane default mismatch: %v", data)
	}

	// ControlPlane set: its own values win over the default.
	sc.Spec.ControlPlane = &showroomv1alpha1.ControlPlaneSpec{
		Nodes: 3,
		Requested: showroomv1alpha1.NodeResources{
			CPU:    resource.MustParse("8"),
			Memory: resource.MustParse("16Gi"),
		},
	}
	objs, err = Render(scpc, sc, r, nil)
	if err != nil {
		t.Fatalf("Render (cp set): %v", err)
	}
	data, _, _ = unstructured.NestedStringMap(objs[0].Object, "data")
	if data["nodes"] != "3" || data["cpu"] != "8" || data["memory"] != "16Gi" {
		t.Errorf("explicit control-plane mismatch: %v", data)
	}
}

func TestRender_WorkersAutoscaling(t *testing.T) {
	scpc := minimalSCPC(nil)
	r := &showroomv1alpha1.ResourceSpec{
		Name:       "config",
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Template: `apiVersion: v1
kind: ConfigMap
data:
  nodes: "{{ .SC.workers.nodes }}"
  min: "{{ .SC.workers.min }}"
  max: "{{ .SC.workers.max }}"
  autoscaling: "{{ .SC.workers.autoscaling }}"
`,
	}

	// Fixed count: min and max collapse to nodes.
	sc := minimalSC(3, nil)
	objs, err := Render(scpc, sc, r, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	data, _, _ := unstructured.NestedStringMap(objs[0].Object, "data")
	if data["nodes"] != "3" || data["min"] != "3" || data["max"] != "3" || data["autoscaling"] != "false" {
		t.Errorf("fixed-count workers context mismatch: %v", data)
	}

	// Autoscaled range: nodes mirrors min, max is the ceiling.
	sc.Spec.Workers = showroomv1alpha1.WorkersSpec{
		Autoscaling: &showroomv1alpha1.AutoscalingSpec{Min: 2, Max: 5},
	}
	objs, err = Render(scpc, sc, r, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	data, _, _ = unstructured.NestedStringMap(objs[0].Object, "data")
	if data["nodes"] != "2" || data["min"] != "2" || data["max"] != "5" || data["autoscaling"] != "true" {
		t.Errorf("autoscaled workers context mismatch: %v", data)
	}
}
