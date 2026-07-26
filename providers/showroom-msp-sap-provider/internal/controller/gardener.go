/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/template"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"
)

// Gardener manages the provider-owned Shoots on the target Gardener. It is an
// interface so tests can substitute a fake.
type Gardener interface {
	GetShoot(ctx context.Context, name string) (*unstructured.Unstructured, error)
	CreateShoot(ctx context.Context, shoot *unstructured.Unstructured) error
	// DeleteShoot stamps the deletion-confirmation annotation and deletes the
	// Shoot. It returns true once the Shoot is gone.
	DeleteShoot(ctx context.Context, name string) (bool, error)
	// AdminKubeconfig mints an admin kubeconfig for the Shoot via the
	// adminkubeconfig subresource.
	AdminKubeconfig(ctx context.Context, name string, validity time.Duration) (kubeconfig []byte, expires time.Time, err error)
}

// GardenerFactory builds a Gardener from a kubeconfig and the garden project
// namespace the Shoots live in.
type GardenerFactory func(kubeconfig []byte, namespace string) (Gardener, error)

var shootGVR = schema.GroupVersionResource{
	Group: "core.gardener.cloud", Version: "v1beta1", Resource: "shoots",
}

const (
	shootDeletionConfirmation = "confirmation.gardener.cloud/deletion"
	// shootOwnerAnnotation back-links a provider-owned Shoot to its CR.
	shootOwnerAnnotation = "sap.gardener/owner"

	// kubeconfigValidity / kubeconfigRenewalBefore mirror the SCPC settings:
	// Gardener caps AdminKubeconfigRequest expiration at ~24h.
	kubeconfigValidity      = 24 * time.Hour
	kubeconfigRenewalBefore = 4 * time.Hour
)

type gardenerClient struct {
	dyn        dynamic.Interface
	restConfig *rest.Config
	namespace  string
}

// NewGardener is the production GardenerFactory.
func NewGardener(kubeconfig []byte, namespace string) (Gardener, error) {
	cfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("parse gardener kubeconfig: %w", err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &gardenerClient{dyn: dyn, restConfig: cfg, namespace: namespace}, nil
}

func (g *gardenerClient) GetShoot(ctx context.Context, name string) (*unstructured.Unstructured, error) {
	return g.dyn.Resource(shootGVR).Namespace(g.namespace).Get(ctx, name, metav1.GetOptions{})
}

func (g *gardenerClient) CreateShoot(ctx context.Context, shoot *unstructured.Unstructured) error {
	_, err := g.dyn.Resource(shootGVR).Namespace(g.namespace).Create(ctx, shoot, metav1.CreateOptions{})
	return err
}

func (g *gardenerClient) DeleteShoot(ctx context.Context, name string) (bool, error) {
	live, err := g.GetShoot(ctx, name)
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if live.GetAnnotations()[shootDeletionConfirmation] != "true" {
		patch := []byte(fmt.Sprintf(`{"metadata":{"annotations":{%q:"true"}}}`, shootDeletionConfirmation))
		if _, err := g.dyn.Resource(shootGVR).Namespace(g.namespace).Patch(
			ctx, name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
			return false, err
		}
	}
	if live.GetDeletionTimestamp().IsZero() {
		if err := g.dyn.Resource(shootGVR).Namespace(g.namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return false, err
		}
	}
	return false, nil
}

func (g *gardenerClient) AdminKubeconfig(ctx context.Context, name string, validity time.Duration) ([]byte, time.Time, error) {
	body, err := json.Marshal(map[string]interface{}{
		"apiVersion": "authentication.gardener.cloud/v1alpha1",
		"kind":       "AdminKubeconfigRequest",
		"spec":       map[string]interface{}{"expirationSeconds": int64(validity.Seconds())},
	})
	if err != nil {
		return nil, time.Time{}, err
	}

	httpClient, err := rest.HTTPClientFor(g.restConfig)
	if err != nil {
		return nil, time.Time{}, err
	}
	url := fmt.Sprintf("%s/apis/core.gardener.cloud/v1beta1/namespaces/%s/shoots/%s/adminkubeconfig",
		strings.TrimSuffix(g.restConfig.Host, "/"), g.namespace, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, time.Time{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, time.Time{}, fmt.Errorf("adminkubeconfig request for %q: HTTP %d: %s", name, resp.StatusCode, string(respBody))
	}

	var out struct {
		Status struct {
			Kubeconfig          []byte      `json:"kubeconfig"`
			ExpirationTimestamp metav1.Time `json:"expirationTimestamp"`
		} `json:"status"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, time.Time{}, fmt.Errorf("decode adminkubeconfig response: %w", err)
	}
	if len(out.Status.Kubeconfig) == 0 {
		return nil, time.Time{}, fmt.Errorf("adminkubeconfig response for %q has empty status.kubeconfig", name)
	}
	return out.Status.Kubeconfig, out.Status.ExpirationTimestamp.Time, nil
}

// gardenShootName derives the deterministic Gardener Shoot name for a provider
// CR: sh-<first 8 hex chars of sha256("kind/namespace/name")>. The kind keeps
// same-named CRs of both flavors apart. Gardener caps len(project)+len(shoot)
// at 21 characters, so the 11-char name leaves room for projects up to 10.
func gardenShootName(kind, namespace, name string) string {
	sum := sha256.Sum256([]byte(kind + "/" + namespace + "/" + name))
	return "sh-" + hex.EncodeToString(sum[:])[:8]
}

// flavorDefaults are the pieces of the embedded SCPC skeleton the Shoot
// reconciler reuses to build a standalone Gardener Shoot.
type flavorDefaults struct {
	// targetNamespace is the garden project namespace (spec.target.namespace).
	targetNamespace string
	// template is the Shoot manifest template of the "shoot" resource.
	template string
	// inputs are the SCPC input defaults keyed by input name; machineType
	// (from: profile) defaults to the smallest profile.
	inputs map[string]string
}

// parseFlavorDefaults extracts targetNamespace, the shoot template and the
// input defaults from an embedded SCPC skeleton.
func parseFlavorDefaults(manifest []byte) (*flavorDefaults, error) {
	var obj map[string]interface{}
	if err := yaml.Unmarshal(manifest, &obj); err != nil {
		return nil, fmt.Errorf("unmarshal embedded SCPC manifest: %w", err)
	}
	scpc := &unstructured.Unstructured{Object: obj}

	targetNS, _, _ := unstructured.NestedString(scpc.Object, "spec", "target", "namespace")
	if targetNS == "" {
		return nil, fmt.Errorf("embedded SCPC manifest has no spec.target.namespace")
	}

	resources, found, err := unstructured.NestedSlice(scpc.Object, "spec", "resources")
	if err != nil || !found || len(resources) == 0 {
		return nil, fmt.Errorf("embedded SCPC manifest has no spec.resources (err: %v)", err)
	}
	first, ok := resources[0].(map[string]interface{})
	if !ok || first["name"] != "shoot" {
		return nil, fmt.Errorf("embedded SCPC manifest: first resource is not %q", "shoot")
	}
	tmpl, _ := first["template"].(string)
	if strings.TrimSpace(tmpl) == "" {
		return nil, fmt.Errorf("embedded SCPC manifest: shoot resource has no template")
	}

	inputs := map[string]string{}
	rawInputs, _, _ := unstructured.NestedSlice(scpc.Object, "spec", "inputs")
	for _, ri := range rawInputs {
		in, ok := ri.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := in["name"].(string)
		if name == "" {
			continue
		}
		if def, ok := in["default"]; ok {
			inputs[name] = fmt.Sprintf("%v", def)
		}
	}
	// Inputs sourced from profiles (from: profile) default to the smallest
	// profile: fewest CPUs, then least memory, as listed.
	if _, ok := inputs["machineType"]; !ok {
		if name := smallestProfile(scpc); name != "" {
			inputs["machineType"] = name
		}
	}

	return &flavorDefaults{targetNamespace: targetNS, template: tmpl, inputs: inputs}, nil
}

// smallestProfile returns the first profile name (profiles are authored
// smallest-first in the embedded skeletons).
func smallestProfile(scpc *unstructured.Unstructured) string {
	profiles, _, _ := unstructured.NestedSlice(scpc.Object, "spec", "profiles")
	if len(profiles) == 0 {
		return ""
	}
	p, ok := profiles[0].(map[string]interface{})
	if !ok {
		return ""
	}
	name, _ := p["name"].(string)
	return name
}

// renderDefaultShootManifest renders the flavor's embedded default Shoot
// template with the synthetic context a standalone provider Shoot gets: the
// SCPC input defaults and a single-node worker pool. It is used only when the
// CR spec is empty (backward compatibility for CRs created without a spec).
func renderDefaultShootManifest(defaults *flavorDefaults, namespace, name string) (*unstructured.Unstructured, error) {
	tmpl, err := template.New("shoot").Option("missingkey=error").Parse(defaults.template)
	if err != nil {
		return nil, fmt.Errorf("parse default shoot template: %w", err)
	}
	ctxData := map[string]interface{}{
		"Inputs": defaults.inputs,
		"SC": map[string]interface{}{
			"namespace": namespace,
			"name":      name,
			"workers":   map[string]interface{}{"min": 1, "max": 1},
		},
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctxData); err != nil {
		return nil, fmt.Errorf("render default shoot template: %w", err)
	}
	return decodeShootManifest(buf.Bytes())
}

// parseShootManifest parses a plain Gardener Shoot manifest — the CR spec text,
// used verbatim with no templating applied — defaulting apiVersion/kind.
func parseShootManifest(text string) (*unstructured.Unstructured, error) {
	return decodeShootManifest([]byte(text))
}

// decodeShootManifest unmarshals a Shoot manifest and defaults apiVersion/kind.
func decodeShootManifest(manifest []byte) (*unstructured.Unstructured, error) {
	var obj map[string]interface{}
	if err := yaml.Unmarshal(manifest, &obj); err != nil {
		return nil, fmt.Errorf("unmarshal shoot manifest: %w", err)
	}
	shoot := &unstructured.Unstructured{Object: obj}
	if shoot.GetAPIVersion() == "" {
		shoot.SetAPIVersion("core.gardener.cloud/v1beta1")
	}
	if shoot.GetKind() == "" {
		shoot.SetKind("Shoot")
	}
	return shoot, nil
}
