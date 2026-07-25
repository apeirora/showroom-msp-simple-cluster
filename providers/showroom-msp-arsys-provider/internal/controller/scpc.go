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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

// The SCPC skeleton materialised for each account namespace. The renderer sets
// metadata.name/namespace and stamps the managed-by label; the Arsys VMs are
// created on the local (management) cluster, so there is no target kubeconfig to
// rewrite. The body's .SC/.Inputs expressions are opaque strings here and are
// consumed later by the simple-cluster operator.
//
//go:embed scpc-arsys.yaml
var arsysSCPCManifest []byte

const (
	// arsysSCPCName is the fixed name of the SimpleClusterProviderConfig the
	// Arsys provider materialises in each bound account namespace.
	arsysSCPCName = "arsys-virtual"

	// managedByLabelKey / managedByNamespaceBind stamp the SCPC so the
	// NamespaceReconciler can unambiguously identify and clean up the object it
	// owns.
	managedByLabelKey      = "arsys.providers.apeirora.eu/managed-by"
	managedByNamespaceBind = "namespace-bind"
)

// flavor describes one Arsys SimpleCluster provider variant. Arsys ships a
// single (virtual) flavor; the slice keeps the reconciler symmetric with the
// multi-flavor providers.
type flavor struct {
	// scpcName is the SimpleClusterProviderConfig name for this flavor.
	scpcName string
	// manifest is the embedded SCPC skeleton.
	manifest []byte
}

// flavors is the full set materialised on bind, in a deterministic order.
var flavors = []flavor{{scpcName: arsysSCPCName, manifest: arsysSCPCManifest}}

var scpcGVK = schema.GroupVersionKind{
	Group: "showroom.apeirora.eu", Version: "v1alpha1", Kind: "SimpleClusterProviderConfig",
}

// allSCPCNames returns every SCPC name the provider manages.
func allSCPCNames() []string {
	names := make([]string, 0, len(flavors))
	for _, f := range flavors {
		names = append(names, f.scpcName)
	}
	return names
}

// renderSCPC materialises one flavor's SCPC for an account namespace: it sets
// the name/namespace and the managed-by label. The target namespace inside the
// body stays "{{ .SC.namespace }}" so the resources land next to the owning
// SimpleCluster on the local cluster.
func renderSCPC(f flavor, namespace string) (*unstructured.Unstructured, error) {
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

	return scpc, nil
}
