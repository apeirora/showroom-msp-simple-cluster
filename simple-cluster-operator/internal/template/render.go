/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package template renders an SCPC body into the concrete target resource R.
// See DESIGN.md §4.
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
	AnnoSCName      = "simplecluster.apeirora.eu/sc-name"
	AnnoSCNamespace = "simplecluster.apeirora.eu/sc-namespace"
	AnnoSCPC        = "simplecluster.apeirora.eu/scpc"
)

// Render expands SCPC.spec.template against SC and returns the resulting
// object as Unstructured, with apiVersion/kind/name/namespace forced from
// Target and tracking annotations applied.
func Render(
	scpc *showroomv1alpha1.SimpleClusterProviderConfig,
	sc *showroomv1alpha1.SimpleCluster,
	targetName string,
) (*unstructured.Unstructured, error) {
	inputs, err := buildInputs(scpc.Spec.Inputs, sc.Spec.Inputs)
	if err != nil {
		return nil, err
	}

	ctx := map[string]any{
		"SC": map[string]any{
			"name":      sc.Name,
			"namespace": sc.Namespace,
			"uid":       string(sc.UID),
			"workers":   sc.Spec.Workers,
			"labels":    sc.Labels,
		},
		"Inputs": inputs,
		"Target": map[string]any{
			"name":      targetName,
			"namespace": scpc.Spec.Target.Namespace,
			"kind":      scpc.Spec.Target.Kind,
		},
	}

	funcs := sprig.TxtFuncMap()
	delete(funcs, "env")
	delete(funcs, "expandenv")

	tmpl, err := template.New("scpc").
		Option("missingkey=error").
		Funcs(funcs).
		Parse(scpc.Spec.Template)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}

	obj := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(buf.Bytes(), &obj.Object); err != nil {
		return nil, fmt.Errorf("parse rendered yaml: %w", err)
	}

	obj.SetAPIVersion(scpc.Spec.Target.APIVersion)
	obj.SetKind(scpc.Spec.Target.Kind)
	obj.SetName(targetName)
	if scpc.Spec.Target.Namespace != "" {
		obj.SetNamespace(scpc.Spec.Target.Namespace)
	}

	annos := obj.GetAnnotations()
	if annos == nil {
		annos = map[string]string{}
	}
	annos[AnnoSCName] = sc.Name
	annos[AnnoSCNamespace] = sc.Namespace
	annos[AnnoSCPC] = scpc.Name
	obj.SetAnnotations(annos)

	return obj, nil
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
