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
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("Namespace Controller", func() {
	ctx := context.Background()

	const bothExports = DefaultSimpleClusterExportName + "," + DefaultProviderExportName

	nsCounter := 0
	newNamespace := func(exports string) string {
		nsCounter++
		name := fmt.Sprintf("acct-%d", nsCounter)
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
		if exports != "" {
			ns.Annotations = map[string]string{bindExportsAnnotation: exports}
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		return name
	}

	setExports := func(name, exports string) {
		ns := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, ns)).To(Succeed())
		if ns.Annotations == nil {
			ns.Annotations = map[string]string{}
		}
		ns.Annotations[bindExportsAnnotation] = exports
		Expect(k8sClient.Update(ctx, ns)).To(Succeed())
	}

	newReconciler := func() *NamespaceReconciler {
		return &NamespaceReconciler{
			Client:                  k8sClient,
			Scheme:                  k8sClient.Scheme(),
			SimpleClusterExportName: DefaultSimpleClusterExportName,
			ProviderExportName:      DefaultProviderExportName,
		}
	}

	getSCPC := func(namespace, name string) (*unstructured.Unstructured, error) {
		scpc := &unstructured.Unstructured{}
		scpc.SetGroupVersionKind(scpcGVK)
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, scpc)
		return scpc, err
	}

	// scpcTemplates concatenates every resource template body so tests can assert
	// on the rewritten secret namespaces baked into the embedded SCPC.
	scpcTemplates := func(scpc *unstructured.Unstructured) string {
		resources, _, _ := unstructured.NestedSlice(scpc.Object, "spec", "resources")
		Expect(resources).NotTo(BeEmpty())
		var b strings.Builder
		for _, r := range resources {
			m := r.(map[string]interface{})
			if tpl, ok := m["template"].(string); ok {
				b.WriteString(tpl)
			}
		}
		return b.String()
	}

	reconcileNS := func(name string) {
		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name}})
		Expect(err).NotTo(HaveOccurred())
	}

	It("creates the namespace-owned SCPC when both exports are bound", func() {
		name := newNamespace(bothExports)
		reconcileNS(name)

		scpc, err := getSCPC(name, cloudferroSCPCName)
		Expect(err).NotTo(HaveOccurred())

		Expect(scpc.GetLabels()).To(HaveKeyWithValue(managedByLabelKey, managedByNamespaceBind))

		owners := scpc.GetOwnerReferences()
		Expect(owners).To(HaveLen(1))
		Expect(owners[0].Kind).To(Equal("Namespace"))
		Expect(owners[0].Name).To(Equal(name))

		// The VMs run on the local cluster: the SCPC target namespace stays the
		// SimpleCluster's namespace and there is no kubeconfigSecretRef.
		targetNS, _, _ := unstructured.NestedString(scpc.Object, "spec", "target", "namespace")
		Expect(targetNS).To(Equal("{{ .SC.namespace }}"))
		_, found, _ := unstructured.NestedMap(scpc.Object, "spec", "target", "kubeconfigSecretRef")
		Expect(found).To(BeFalse())

		// The embedded credentials Secret namespace was rewritten to the provider
		// namespace; nothing points at the old scpc-configs namespace anymore.
		tpls := scpcTemplates(scpc)
		Expect(tpls).To(ContainSubstring("namespace: cloudferro-provider"))
		Expect(tpls).NotTo(ContainSubstring("simple-cluster-system-scpc-configs"))
	})

	It("does not create the SCPC when the provider export is absent", func() {
		name := newNamespace(DefaultSimpleClusterExportName)
		reconcileNS(name)

		_, err := getSCPC(name, cloudferroSCPCName)
		Expect(err).To(HaveOccurred())
	})

	It("removes the managed SCPC when the provider binding is dropped", func() {
		name := newNamespace(bothExports)
		reconcileNS(name)
		_, err := getSCPC(name, cloudferroSCPCName)
		Expect(err).NotTo(HaveOccurred())

		// Unbind the provider: the annotation now lists only SimpleCluster.
		setExports(name, DefaultSimpleClusterExportName)
		reconcileNS(name)

		_, err = getSCPC(name, cloudferroSCPCName)
		Expect(err).To(HaveOccurred())
	})

	It("leaves an unmanaged SCPC of the same name untouched when unbound", func() {
		name := newNamespace(DefaultSimpleClusterExportName)

		// An admin-authored SCPC with the same name but no managed-by label.
		unmanaged := &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "showroom.apeirora.eu/v1alpha1",
			"kind":       "SimpleClusterProviderConfig",
			"metadata": map[string]interface{}{
				"name":      cloudferroSCPCName,
				"namespace": name,
			},
			"spec": map[string]interface{}{
				"target": map[string]interface{}{"namespace": name},
				"resources": []interface{}{
					map[string]interface{}{
						"name":       "cluster",
						"apiVersion": "ubuntu.apeirora.eu/v1alpha1",
						"kind":       "UbuntuCluster",
						"template":   "apiVersion: ubuntu.apeirora.eu/v1alpha1\nkind: UbuntuCluster\n",
					},
				},
				"kubeconfigSource": map[string]interface{}{"resourceName": "cluster"},
			},
		}}
		unmanaged.SetGroupVersionKind(scpcGVK)
		Expect(k8sClient.Create(ctx, unmanaged)).To(Succeed())

		reconcileNS(name)

		got, err := getSCPC(name, cloudferroSCPCName)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.GetLabels()).NotTo(HaveKey(managedByLabelKey))
	})
})
