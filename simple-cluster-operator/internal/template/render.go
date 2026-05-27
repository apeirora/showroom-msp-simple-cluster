/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package template renders an SCPC ResourceSpec into the concrete target
// resource R. See DESIGN.md §4.
package template

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
)

const (
	AnnoSCName       = "simplecluster.apeirora.eu/sc-name"
	AnnoSCNamespace  = "simplecluster.apeirora.eu/sc-namespace"
	AnnoSCPC         = "simplecluster.apeirora.eu/scpc"
	AnnoResourceName = "simplecluster.apeirora.eu/resource-name"
)

// Render expands resource.Template against (SCPC, SC, predecessors) and
// returns the resulting object as Unstructured, with apiVersion/kind/name/
// namespace forced from the ResourceSpec and tracking annotations applied.
// predecessors[name] is the live unstructured object of each name listed in
// resource.DependsOn; it is exposed to the template as .Resources.<name>.
func Render(
	scpc *showroomv1alpha1.SimpleClusterProviderConfig,
	sc *showroomv1alpha1.SimpleCluster,
	resource *showroomv1alpha1.ResourceSpec,
	predecessors map[string]*unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	ctx, err := BuildContext(scpc, sc, resource, predecessors)
	if err != nil {
		return nil, err
	}

	body, err := RenderString(resource.Name, resource.Template, ctx)
	if err != nil {
		return nil, err
	}

	obj := &unstructured.Unstructured{}
	if err := yaml.Unmarshal([]byte(body), &obj.Object); err != nil {
		return nil, fmt.Errorf("parse rendered yaml: %w", err)
	}

	targetName := ctx["Target"].(map[string]any)["name"].(string)
	targetNamespace := ctx["Target"].(map[string]any)["namespace"].(string)

	obj.SetAPIVersion(resource.APIVersion)
	obj.SetKind(resource.Kind)
	obj.SetName(targetName)
	if targetNamespace != "" {
		obj.SetNamespace(targetNamespace)
	}

	annos := obj.GetAnnotations()
	if annos == nil {
		annos = map[string]string{}
	}
	annos[AnnoSCName] = sc.Name
	annos[AnnoSCNamespace] = sc.Namespace
	annos[AnnoSCPC] = scpc.Name
	annos[AnnoResourceName] = resource.Name
	obj.SetAnnotations(annos)

	return obj, nil
}

// BuildContext builds the render context (.SC, .Inputs, .Target, .Resources)
// used by Render. Exported so callers (notably the kubeconfig SecretRef
// strategy) can render auxiliary templates with the same semantics.
func BuildContext(
	scpc *showroomv1alpha1.SimpleClusterProviderConfig,
	sc *showroomv1alpha1.SimpleCluster,
	resource *showroomv1alpha1.ResourceSpec,
	predecessors map[string]*unstructured.Unstructured,
) (map[string]any, error) {
	inputs, err := buildInputs(scpc.Spec.Inputs, sc.Spec.Inputs)
	if err != nil {
		return nil, err
	}

	targetNamespace := resource.Namespace
	if targetNamespace == "" {
		targetNamespace = scpc.Spec.Target.Namespace
	}
	targetName := SanitiseTargetName(resource.NamePrefix, sc.Name+"-"+resource.Name)

	resourcesCtx := make(map[string]any, len(predecessors))
	for name, obj := range predecessors {
		if obj != nil {
			resourcesCtx[name] = obj.Object
		}
	}

	scCtx := map[string]any{
		"name":      sc.Name,
		"namespace": sc.Namespace,
		"uid":       string(sc.UID),
		"workers":   workersContext(sc.Spec.Workers),
		"labels":    sc.Labels,
	}
	if sc.Spec.ControlPlane != nil {
		scCtx["controlPlane"] = controlPlaneContext(*sc.Spec.ControlPlane)
	}

	return map[string]any{
		"SC":     scCtx,
		"Inputs": inputs,
		"Target": map[string]any{
			"name":      targetName,
			"namespace": targetNamespace,
			"kind":      resource.Kind,
		},
		"Resources": resourcesCtx,
	}, nil
}

// workersContext flattens WorkersSpec into the .SC.workers template map.
// MaxNodes defaults to Nodes when unset (no autoscaling). Requested
// quantities are stringified so templates emit canonical values
// (e.g. "16", "32Gi").
func workersContext(w showroomv1alpha1.WorkersSpec) map[string]any {
	maxNodes := w.Nodes
	if w.MaxNodes != nil {
		maxNodes = *w.MaxNodes
	}
	out := map[string]any{
		"nodes":    w.Nodes,
		"maxNodes": maxNodes,
	}
	if r := nodeResourcesContext(w.Requested); r != nil {
		out["requested"] = r
	}
	return out
}

// controlPlaneContext flattens ControlPlaneSpec into the .SC.controlPlane
// template map.
func controlPlaneContext(cp showroomv1alpha1.ControlPlaneSpec) map[string]any {
	out := map[string]any{
		"nodes": cp.Nodes,
	}
	if r := nodeResourcesContext(cp.Requested); r != nil {
		out["requested"] = r
	}
	return out
}

// nodeResourcesContext stringifies CPU/Memory quantities. Returns nil when
// no fields are set so templates can `{{ if .SC.workers.requested }}`.
func nodeResourcesContext(r *showroomv1alpha1.NodeResources) map[string]any {
	if r == nil {
		return nil
	}
	req := map[string]any{}
	if !r.CPU.IsZero() {
		req["cpu"] = r.CPU.String()
	}
	if !r.Memory.IsZero() {
		req["memory"] = r.Memory.String()
	}
	if len(req) == 0 {
		return nil
	}
	return req
}

// RenderString renders an arbitrary template body against an arbitrary
// context using the same sprig-flavoured engine as Render (with `env` and
// `expandenv` removed).
func RenderString(name, body string, ctx map[string]any) (string, error) {
	funcs := sprig.TxtFuncMap()
	delete(funcs, "env")
	delete(funcs, "expandenv")

	tmpl, err := template.New(name).
		Option("missingkey=error").
		Funcs(funcs).
		Parse(body)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

func buildInputs(decls []showroomv1alpha1.InputDeclaration, overrides map[string]string) (map[string]any, error) {
	declared := make(map[string]showroomv1alpha1.InputDeclaration, len(decls))
	for _, d := range decls {
		declared[d.Name] = d
	}
	for k := range overrides {
		if _, ok := declared[k]; !ok {
			return nil, fmt.Errorf("unknown input %q (not declared in SCPC.spec.inputs)", k)
		}
	}

	out := make(map[string]any, len(decls))
	for _, d := range decls {
		raw, has := overrides[d.Name]
		if !has {
			raw = d.Default
		}
		v, err := parseInput(d, raw)
		if err != nil {
			return nil, fmt.Errorf("input %q: %w", d.Name, err)
		}
		out[d.Name] = v
	}
	return out, nil
}

func parseInput(d showroomv1alpha1.InputDeclaration, raw string) (any, error) {
	t := d.Type
	if t == "" {
		t = showroomv1alpha1.InputTypeString
	}
	switch t {
	case showroomv1alpha1.InputTypeString:
		if raw == "" && !d.AllowEmpty {
			return nil, fmt.Errorf("empty string not allowed (set allowEmpty: true to permit)")
		}
		return raw, nil
	case showroomv1alpha1.InputTypeInt:
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid int %q: %w", raw, err)
		}
		return v, nil
	case showroomv1alpha1.InputTypeBool:
		switch raw {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, fmt.Errorf("invalid bool %q (want true|false)", raw)
	case showroomv1alpha1.InputTypeDuration:
		v, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid duration %q: %w", raw, err)
		}
		return v, nil
	case showroomv1alpha1.InputTypeEnum:
		if len(d.Values) == 0 {
			return nil, fmt.Errorf("enum input declares no values")
		}
		for _, v := range d.Values {
			if v.Key == raw {
				return raw, nil
			}
		}
		keys := make([]string, len(d.Values))
		for i, v := range d.Values {
			keys[i] = v.Key
		}
		return nil, fmt.Errorf("value %q not in allowed set [%s]", raw, strings.Join(keys, ", "))
	}
	return nil, fmt.Errorf("unsupported input type %q", t)
}

// SanitiseTargetName produces a DNS-compliant target name from prefix+base,
// lowercase, alphanumeric and dash only, trimmed and truncated to 63 chars.
func SanitiseTargetName(prefix, base string) string {
	name := prefix + base
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		}
		return '-'
	}, name)
	name = strings.Trim(name, "-")
	if len(name) > 63 {
		name = strings.TrimRight(name[:63], "-")
	}
	if name == "" {
		name = "sc"
	}
	return name
}
