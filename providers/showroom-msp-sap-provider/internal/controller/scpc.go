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

// The SCPC skeletons materialised for each account namespace, one per flavor.
// The renderer rewrites metadata.name/namespace and target.kubeconfigSecretRef.
// The remaining body (inputs, profiles, readiness, kubeconfigSource) stays as
// authored; simple-cluster .SC/.Inputs expressions are opaque strings here.
// The Shoot reconciler reuses the same skeletons for its standalone Gardener
// Shoots (parseFlavorDefaults).
//
//go:embed scpc-sap-openstack.yaml
var openstackSCPCManifest []byte

//go:embed scpc-sap-ironcore.yaml
var ironcoreSCPCManifest []byte

const (
	// openstackSCPCName / ironcoreSCPCName are the fixed names of the two
	// SimpleClusterProviderConfigs the SAP Integration provider materialises in
	// each bound account namespace.
	openstackSCPCName = "sap-gardener-openstack"
	ironcoreSCPCName  = "sap-gardener-ironcore"

	// managedByLabelKey / managedByNamespaceBind stamp the SCPCs so the
	// NamespaceReconciler can unambiguously identify and clean up the objects
	// it owns.
	managedByLabelKey      = "sap.gardener/managed-by"
	managedByNamespaceBind = "namespace-bind"
)

// flavor describes one SAP Integration SimpleCluster provider variant.
type flavor struct {
	// scpcName is the SimpleClusterProviderConfig name for this flavor.
	scpcName string
	// manifest is the embedded SCPC skeleton.
	manifest []byte
}

var (
	openStackFlavor = flavor{scpcName: openstackSCPCName, manifest: openstackSCPCManifest}
	ironCoreFlavor  = flavor{scpcName: ironcoreSCPCName, manifest: ironcoreSCPCManifest}
	// flavors is the full set materialised on bind, in a deterministic order.
	flavors = []flavor{openStackFlavor, ironCoreFlavor}
)

var scpcGVK = schema.GroupVersionKind{
	Group: "showroom.apeirora.eu", Version: "v1alpha1", Kind: "SimpleClusterProviderConfig",
}

// OpenStackSCPCName / IronCoreSCPCName expose the per-flavor SCPC names to the
// command wiring; the manifest accessors feed the Shoot reconcilers.
func OpenStackSCPCName() string     { return openstackSCPCName }
func IronCoreSCPCName() string      { return ironcoreSCPCName }
func OpenStackSCPCManifest() []byte { return openstackSCPCManifest }
func IronCoreSCPCManifest() []byte  { return ironcoreSCPCManifest }

// allSCPCNames returns every SCPC name the provider manages.
func allSCPCNames() []string {
	names := make([]string, 0, len(flavors))
	for _, f := range flavors {
		names = append(names, f.scpcName)
	}
	return names
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

// renderSCPC materialises one flavor's SCPC for an account namespace: it sets
// the name/namespace, the kubeconfigSecretRef target and the managed-by label.
func renderSCPC(f flavor, namespace string, ref corev1.SecretReference) (*unstructured.Unstructured, error) {
	var obj map[string]interface{}
	if err := yaml.Unmarshal(f.manifest, &obj); err != nil {
		return nil, fmt.Errorf("unmarshal embedded SCPC manifest for %q: %w", f.scpcName, err)
	}
	scpc := &unstructured.Unstructured{Object: obj}
	scpc.SetName(f.scpcName)
	scpc.SetNamespace(namespace)

	labels := scpc.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[managedByLabelKey] = managedByNamespaceBind
	scpc.SetLabels(labels)

	if err := unstructured.SetNestedMap(scpc.Object, map[string]interface{}{
		"name":      ref.Name,
		"namespace": ref.Namespace,
	}, "spec", "target", "kubeconfigSecretRef"); err != nil {
		return nil, err
	}

	return scpc, nil
}
