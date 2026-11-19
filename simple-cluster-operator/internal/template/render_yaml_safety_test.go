/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// YAML-safety tests for SCPC template interpolation (F-SC-1/F-SC-2).
//
// type-string .Inputs values and predecessor (.Resources) status fields are
// free text: they are substituted into the template as raw runes before the
// result is yaml.Unmarshal'd. An unquoted value containing a newline can add
// keys or even a duplicate top-level "spec:"; the unstructured map decode
// keeps the LAST duplicate key, so the tenant silently replaces the whole
// rendered spec. The shipped SCPCs quote such values (sprig's `quote` emits a
// JSON-escaped double-quoted scalar, which YAML accepts verbatim); these tests
// pin that convention against the real chart and sample files.

package template

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
)

// repoRoot is the module-relative path to the monorepo root, from which the
// chart and sample SCPCs are read verbatim.
const repoRoot = "../../.."

// loadSCPCFile parses a shipped SCPC file into the API type. Helm chart files
// wrap the SCPC in `{{ `…` }}` raw-string blocks; only those backtick blocks
// hold SCPC YAML, so they are concatenated (Helm `{{- if }}` guards between
// them emit nothing for default values, and the optional blocks that are
// skipped are still valid template body content when present).
func loadSCPCFile(t *testing.T, path string) *showroomv1alpha1.SimpleClusterProviderConfig {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc string
	for _, block := range regexp.MustCompile("(?s)`([^`]*)`").FindAllStringSubmatch(string(raw), -1) {
		doc += block[1]
	}
	if doc == "" {
		doc = string(raw) // plain samples, applied as-is without Helm
	}
	scpc := &showroomv1alpha1.SimpleClusterProviderConfig{}
	if err := yaml.Unmarshal([]byte(doc), scpc); err != nil {
		t.Fatalf("parse SCPC from %s: %v", path, err)
	}
	return scpc
}

func resourceSpec(t *testing.T, scpc *showroomv1alpha1.SimpleClusterProviderConfig, name string) *showroomv1alpha1.ResourceSpec {
	t.Helper()
	for i := range scpc.Spec.Resources {
		if scpc.Spec.Resources[i].Name == name {
			return &scpc.Spec.Resources[i]
		}
	}
	t.Fatalf("resource %q not found in SCPC %s", name, scpc.Name)
	return nil
}

// wholeSpecPayload is the F-SC-1 worst case: it closes the scalar line and
// dedents a second top-level spec: block, so an unquoted interpolation lets
// the tenant author the child spec themselves (duplicate key, last wins).
const wholeSpecPayload = "kindest/node:v1.31.0\nspec:\n  clusterName: tenant-replaced\n  attackerField: pwned\n"

func yamlSafeSC(nodeImage string) *showroomv1alpha1.SimpleCluster {
	workers := int32(2)
	sc := &showroomv1alpha1.SimpleCluster{}
	sc.Name = "foo"
	sc.Namespace = "users"
	sc.Spec.Workers = showroomv1alpha1.WorkersSpec{Nodes: &workers}
	sc.Spec.Inputs = map[string]string{"nodeImage": nodeImage}
	return sc
}

// TestShippedKindSCPCQuotesTenantInput drives the real kind SCPC bodies from
// the chart (enabled by default) and the samples through Render with a
// newline-carrying input value.
func TestShippedKindSCPCQuotesTenantInput(t *testing.T) {
	for _, path := range []string{
		"charts/simple-cluster-scpc-config/templates/scpc-kind.yaml",
		"samples/kind/scpc-kind.yaml",
	} {
		t.Run(path, func(t *testing.T) {
			scpc := loadSCPCFile(t, path)
			res := resourceSpec(t, scpc, "cluster")

			objs, err := Render(scpc, yamlSafeSC(wholeSpecPayload), res, nil)
			if err != nil {
				t.Fatalf("render with newline-carrying input: %v", err)
			}
			obj := objs[0].Object
			if _, injected := obj["status"]; injected {
				t.Fatalf("value escaped its line and injected a top-level key: %#v", obj)
			}
			spec, ok := obj["spec"].(map[string]any)
			if !ok {
				t.Fatalf("no spec map: %#v", obj)
			}
			// The template's own fields survived: no whole-spec replacement.
			if spec["clusterName"] != "foo-cluster" {
				t.Errorf("spec was replaced, not quoted: clusterName=%v", spec["clusterName"])
			}
			if fmt.Sprint(spec["workers"]) != "2" {
				t.Errorf("workers lost: %v", spec["workers"])
			}
			if spec["nodeImage"] != wholeSpecPayload {
				t.Errorf("nodeImage did not round-trip as one string: %q", spec["nodeImage"])
			}
		})
	}
}

// TestUnquotedInterpolationStillInjects documents the hazard the quoting
// convention guards against: with the same payload on an unquoted action, the
// tenant-authored spec wins (duplicate key, last wins).
func TestUnquotedInterpolationStillInjects(t *testing.T) {
	scpc := minimalSCPC([]showroomv1alpha1.InputDeclaration{
		{Name: "nodeImage", Type: showroomv1alpha1.InputTypeString, AllowEmpty: true},
	})
	res := &showroomv1alpha1.ResourceSpec{
		Name:       "cluster",
		APIVersion: "kind.apeirora.eu/v1alpha1",
		Kind:       "KindCluster",
		Template: "apiVersion: kind.apeirora.eu/v1alpha1\nkind: KindCluster\nspec:\n" +
			"  clusterName: {{ .Target.name }}\n  nodeImage: {{ .Inputs.nodeImage }}\n",
	}
	objs, err := Render(scpc, yamlSafeSC(wholeSpecPayload), res, nil)
	if err != nil {
		t.Fatalf("unquoted injection should parse (last-wins), got: %v", err)
	}
	spec := objs[0].Object["spec"].(map[string]any)
	if spec["clusterName"] != "tenant-replaced" || spec["attackerField"] != "pwned" {
		t.Fatalf("duplicate top-level spec did not collapse last-wins: %#v", spec)
	}
}

// fsc2Claim builds a live UbuntuMachineClaim-shaped predecessor with the
// status.machine.ssh.* tree the ubuntu SCPCs read.
func fsc2Claim(user, privName, addr, extAddr string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "ubuntumachines.apeirora.eu/v1alpha1",
		"kind":       "UbuntuMachineClaim",
		"metadata":   map[string]any{"name": "foo-cp-0", "namespace": "users"},
		"status": map[string]any{
			"machine": map[string]any{
				"address":         addr,
				"externalAddress": extAddr,
				"ssh": map[string]any{
					"user":                user,
					"privateKeySecretRef": map[string]any{"name": privName},
				},
			},
		},
	}}
}

// TestShippedUbuntuMachinesSCPCQuotesPredecessorStatus drives the chart's
// ubuntu-machines `cluster` resource with predecessor status values that
// carry newlines (F-SC-2): the tenant edits children in its own namespace, so
// re-read status re-enters the next render unescaped.
func TestShippedUbuntuMachinesSCPCQuotesPredecessorStatus(t *testing.T) {
	scpc := loadSCPCFile(t, "charts/simple-cluster-scpc-config/templates/scpc-ubuntu-machines.yaml")
	res := resourceSpec(t, scpc, "cluster")

	const userPayload = "ubuntu\nstatus:\n  phase: TenantInjected\n"
	workers := int32(1)
	sc := &showroomv1alpha1.SimpleCluster{}
	sc.Name = "foo"
	sc.Namespace = "users"
	sc.Spec.Workers = showroomv1alpha1.WorkersSpec{Nodes: &workers}

	preds := map[string]Predecessor{
		"cp": {Repeated: true, Objects: []*unstructured.Unstructured{
			fsc2Claim(userPayload, "foo-cp-0-ssh", "10.0.0.1\ninjected: true\n", "203.0.113.1"),
		}},
		"worker": {Repeated: true, Objects: []*unstructured.Unstructured{
			fsc2Claim("ubuntu", "foo-worker-0-ssh", "10.0.1.1", "203.0.113.9"),
		}},
	}

	objs, err := Render(scpc, sc, res, preds)
	if err != nil {
		t.Fatalf("render with newline-carrying status: %v", err)
	}
	obj := objs[0].Object
	if _, injected := obj["status"]; injected {
		t.Fatalf("predecessor status escaped its line and injected a top-level key: %#v", obj)
	}
	spec, ok := obj["spec"].(map[string]any)
	if !ok {
		t.Fatalf("no spec map: %#v", obj)
	}
	ssh, _ := spec["ssh"].(map[string]any)
	if ssh == nil {
		t.Fatalf("spec.ssh missing: %#v", spec)
	}
	if ssh["user"] != userPayload {
		t.Errorf("ssh.user did not round-trip as one string: %q", ssh["user"])
	}
	cp, _ := spec["controlPlane"].(map[string]any)
	hosts, _ := cp["hosts"].([]any)
	if len(hosts) != 1 {
		t.Fatalf("controlPlane.hosts = %#v", cp)
	}
	host, _ := hosts[0].(map[string]any)
	if host["address"] != "10.0.0.1\ninjected: true\n" {
		t.Errorf("address did not round-trip as one string: %q", host["address"])
	}
}

// rawInterpolation matches a template action with no pipeline: either a bare
// tenant-derived interpolation or a naively quote-wrapped one.
var rawInterpolation = regexp.MustCompile(`\{\{ ([^{}|]+) \}\}`)

// TestShippedSCPCsQuoteTenantDerivedInterpolations is the convention test:
// every .Inputs / .Resources / loop-status interpolation in a shipped SCPC is
// YAML-scoped by a pipeline (quote, or trim where the value lives inside a
// block scalar). .SC identity fields are structurally safe and exempt.
func TestShippedSCPCsQuoteTenantDerivedInterpolations(t *testing.T) {
	dirs := []string{
		filepath.Join(repoRoot, "charts/simple-cluster-scpc-config/templates"),
		filepath.Join(repoRoot, "samples"),
	}
	var checked int
	for _, dir := range dirs {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasPrefix(info.Name(), "scpc-") || !strings.HasSuffix(info.Name(), ".yaml") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(raw), "\n") {
				for _, m := range rawInterpolation.FindAllStringSubmatch(line, -1) {
					action := m[1]
					tenantDerived := strings.Contains(action, ".Inputs.") ||
						strings.Contains(action, ".Resources.") ||
						strings.Contains(action, "$vm.") || strings.Contains(action, "$cl.")
					if !tenantDerived {
						continue
					}
					rel, _ := filepath.Rel(repoRoot, path)
					t.Errorf("%s:%d: tenant-derived interpolation %q is not YAML-quoted "+
						"(add `| quote`; see the duplicate-key comment in scpc-kind.yaml)", rel, i+1, action)
				}
			}
			checked++
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	if checked < 6 {
		t.Fatalf("only %d SCPC files checked; the walk lost the chart or samples directory", checked)
	}
	t.Log(fmt.Sprintf("checked %d shipped SCPC files for raw tenant-derived interpolations", checked))
}
