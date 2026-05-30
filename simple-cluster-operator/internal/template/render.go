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
	// AnnoSimpleCluster names the owning SimpleCluster as "<ns>/<name>".
	AnnoSimpleCluster = "simple-cluster-name"
	// AnnoSimpleClusterProvider names the SCPC whose template produced R.
	AnnoSimpleClusterProvider = "simple-cluster-provider"
)

// Predecessor is a sibling ResourceSpec's rendered output, plumbed to a
// downstream ResourceSpec's Template via .Resources.<name>. Objects holds the
// live (server-side) object(s); for non-repeated resources len(Objects)==1
// and Repeated is false, in which case .Resources.<name> is the single object
// map. For repeated resources Repeated is true and .Resources.<name> is the
// slice of N object maps in index order.
type Predecessor struct {
	Repeated bool
	Objects  []*unstructured.Unstructured
}

// Render expands resource.Template against (SCPC, SC, predecessors). When
// resource.Repeat is unset it returns a single-element slice; when set, the
// Repeat template is evaluated to N and the body is rendered N times with
// .Index and .Prev in scope. Each returned object has apiVersion/kind/name/
// namespace forced from the ResourceSpec and tracking annotations applied.
func Render(
	scpc *showroomv1alpha1.SimpleClusterProviderConfig,
	sc *showroomv1alpha1.SimpleCluster,
	resource *showroomv1alpha1.ResourceSpec,
	predecessors map[string]Predecessor,
) ([]*unstructured.Unstructured, error) {
	n := 1
	repeated := resource.Repeat != ""
	if repeated {
		var err error
		n, err = EvaluateRepeat(scpc, sc, resource, predecessors)
		if err != nil {
			return nil, err
		}
	}

	out := make([]*unstructured.Unstructured, 0, n)
	prev := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		obj, err := renderOne(scpc, sc, resource, predecessors, repeated, i, prev)
		if err != nil {
			return nil, err
		}
		out = append(out, obj)
		prev = append(prev, obj.Object)
	}
	return out, nil
}

func renderOne(
	scpc *showroomv1alpha1.SimpleClusterProviderConfig,
	sc *showroomv1alpha1.SimpleCluster,
	resource *showroomv1alpha1.ResourceSpec,
	predecessors map[string]Predecessor,
	repeated bool,
	index int,
	prev []map[string]any,
) (*unstructured.Unstructured, error) {
	ctx, err := buildContext(scpc, sc, resource, predecessors, repeated, index, prev)
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

	target := ctx["Target"].(map[string]any)
	obj.SetAPIVersion(resource.APIVersion)
	obj.SetKind(resource.Kind)
	obj.SetName(target["name"].(string))
	if ns := target["namespace"].(string); ns != "" {
		obj.SetNamespace(ns)
	}

	annos := obj.GetAnnotations()
	if annos == nil {
		annos = map[string]string{}
	}
	annos[AnnoSimpleCluster] = sc.Namespace + "/" + sc.Name
	annos[AnnoSimpleClusterProvider] = scpc.Name
	obj.SetAnnotations(annos)
	return obj, nil
}

// EvaluateRepeat renders resource.Repeat and parses the result as a
// non-negative integer. .Index / .Prev are NOT in scope.
func EvaluateRepeat(
	scpc *showroomv1alpha1.SimpleClusterProviderConfig,
	sc *showroomv1alpha1.SimpleCluster,
	resource *showroomv1alpha1.ResourceSpec,
	predecessors map[string]Predecessor,
) (int, error) {
	ctx, err := buildContext(scpc, sc, resource, predecessors, false, 0, nil)
	if err != nil {
		return 0, err
	}
	raw, err := RenderString(resource.Name+".repeat", resource.Repeat, ctx)
	if err != nil {
		return 0, fmt.Errorf("render repeat: %w", err)
	}
	raw = strings.TrimSpace(raw)
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("repeat %q is not an integer", raw)
	}
	if n < 0 {
		return 0, fmt.Errorf("repeat %d must be >= 0", n)
	}
	return n, nil
}

// BuildContext builds the render context (.SC, .Inputs, .Target, .Resources,
// .Index, .Prev) used by Render. Exported so callers (notably the kubeconfig
// SecretRef strategy) can render auxiliary templates with the same semantics.
// For auxiliary templates the resource is treated as non-repeated.
func BuildContext(
	scpc *showroomv1alpha1.SimpleClusterProviderConfig,
	sc *showroomv1alpha1.SimpleCluster,
	resource *showroomv1alpha1.ResourceSpec,
	predecessors map[string]Predecessor,
) (map[string]any, error) {
	return buildContext(scpc, sc, resource, predecessors, false, 0, nil)
}

func buildContext(
	scpc *showroomv1alpha1.SimpleClusterProviderConfig,
	sc *showroomv1alpha1.SimpleCluster,
	resource *showroomv1alpha1.ResourceSpec,
	predecessors map[string]Predecessor,
	repeated bool,
	index int,
	prev []map[string]any,
) (map[string]any, error) {
	inputs, err := buildInputs(scpc.Spec.Inputs, sc.Spec.Inputs)
	if err != nil {
		return nil, err
	}

	targetNamespace := resource.Namespace
	if targetNamespace == "" {
		targetNamespace = scpc.Spec.Target.Namespace
	}
	baseName := SanitiseTargetName(resource.NamePrefix, sc.Name+"-"+resource.Name)
	targetName := baseName
	if repeated {
		targetName = fmt.Sprintf("%s-%d", baseName, index)
	}

	resourcesCtx := make(map[string]any, len(predecessors))
	for name, p := range predecessors {
		if p.Repeated {
			objs := make([]any, 0, len(p.Objects))
			for _, o := range p.Objects {
				if o != nil {
					objs = append(objs, o.Object)
				}
			}
			resourcesCtx[name] = objs
			continue
		}
		if len(p.Objects) > 0 && p.Objects[0] != nil {
			resourcesCtx[name] = p.Objects[0].Object
		}
	}

	scCtx := map[string]any{
		"name":      sc.Name,
		"namespace": sc.Namespace,
		"uid":       string(sc.UID),
		"workers": map[string]any{
			"nodes":     sc.Spec.Workers.Nodes,
			"requested": nodeResourcesContext(sc.Spec.Workers.Requested),
		},
		"labels": sc.Labels,
	}
	if sc.Spec.ControlPlane != nil {
		scCtx["controlPlane"] = map[string]any{
			"nodes":     sc.Spec.ControlPlane.Nodes,
			"requested": nodeResourcesContext(sc.Spec.ControlPlane.Requested),
		}
	}

	prevAny := make([]any, 0, len(prev))
	for _, p := range prev {
		prevAny = append(prevAny, p)
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
		"Index":     index,
		"Prev":      prevAny,
	}, nil
}

// nodeResourcesContext stringifies CPU/Memory quantities into the
// .SC.{controlPlane,workers}.requested template map.
func nodeResourcesContext(r showroomv1alpha1.NodeResources) map[string]any {
	return map[string]any{
		"cpu":    r.CPU.String(),
		"memory": r.Memory.String(),
	}
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
