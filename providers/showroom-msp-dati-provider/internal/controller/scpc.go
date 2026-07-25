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
	_ "embed"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

// scpcDatiManifest is the SCPC skeleton materialised for each account
// namespace. The renderer rewrites metadata.namespace and
// target.kubeconfigSecretRef. The remaining body (inputs, resources, readiness,
// kubeconfigSource) stays as authored; simple-cluster .SC/.Inputs expressions
// are opaque strings here. The Shoot reconciler reuses the same skeleton for
// its standalone Gardener Shoots (parseFlavorDefaults).
//
//go:embed scpc-dati.yaml
var scpcDatiManifest []byte

const (
	// datiSCPCName is the fixed name of the SimpleClusterProviderConfig the
	// dati provider materialises in each bound account namespace.
	datiSCPCName = "dati"

	// datiManagedByLabelKey / datiManagedByNamespaceBind stamp the SCPC so the
	// NamespaceReconciler can unambiguously identify and clean up the objects
	// it owns.
	datiManagedByLabelKey      = "dati.gardener/managed-by"
	datiManagedByNamespaceBind = "namespace-bind"
)

var scpcGVK = schema.GroupVersionKind{
	Group: "showroom.apeirora.eu", Version: "v1alpha1", Kind: "SimpleClusterProviderConfig",
}

// KubeconfigSource selects where the Gardener kubeconfig that the SCPC targets
// comes from.
type KubeconfigSource string

const (
	// KubeconfigSourceGlobal: one admin-managed kubeconfig Secret shared by all
	// tenants, at a fixed name/namespace on the service cluster. Default.
	KubeconfigSourceGlobal KubeconfigSource = "global"
	// KubeconfigSourceKCP: each client brings its own kubeconfig from its KCP
	// workspace; the Secret is looked up (by a fixed name) in the account's own
	// namespace on the service cluster, where the sync agent delivers it.
	KubeconfigSourceKCP KubeconfigSource = "kcp"
)

// ValidKubeconfigSource reports whether s is a recognised source.
func ValidKubeconfigSource(s KubeconfigSource) bool {
	return s == KubeconfigSourceGlobal || s == KubeconfigSourceKCP
}

// resolveKubeconfigSecretRef returns which Secret the SCPC target should
// reference for the given account namespace and source.
func resolveKubeconfigSecretRef(
	src KubeconfigSource, namespace, globalName, globalNamespace, clientName string,
) (corev1.SecretReference, error) {
	if !ValidKubeconfigSource(src) {
		return corev1.SecretReference{}, fmt.Errorf("invalid kubeconfig source %q (want %q or %q)",
			src, KubeconfigSourceGlobal, KubeconfigSourceKCP)
	}
	switch src {
	case KubeconfigSourceKCP:
		// Per-client kubeconfig, delivered by the sync agent into the account's
		// own namespace on the service cluster.
		return corev1.SecretReference{Name: clientName, Namespace: namespace}, nil
	default: // KubeconfigSourceGlobal
		return corev1.SecretReference{Name: globalName, Namespace: globalNamespace}, nil
	}
}

// renderDatiSCPC materialises the dati SCPC for an account namespace: it sets
// the name/namespace, the kubeconfigSecretRef target and the managed-by label.
func renderDatiSCPC(namespace string, ref corev1.SecretReference) (*unstructured.Unstructured, error) {
	var obj map[string]interface{}
	if err := yaml.Unmarshal(scpcDatiManifest, &obj); err != nil {
		return nil, fmt.Errorf("unmarshal embedded SCPC manifest: %w", err)
	}
	scpc := &unstructured.Unstructured{Object: obj}
	scpc.SetName(datiSCPCName)
	scpc.SetNamespace(namespace)

	labels := scpc.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[datiManagedByLabelKey] = datiManagedByNamespaceBind
	scpc.SetLabels(labels)

	if err := unstructured.SetNestedMap(scpc.Object, map[string]interface{}{
		"name":      ref.Name,
		"namespace": ref.Namespace,
	}, "spec", "target", "kubeconfigSecretRef"); err != nil {
		return nil, err
	}

	return scpc, nil
}
