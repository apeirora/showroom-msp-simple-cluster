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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dativ1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/providers/dati-provider/api/v1alpha1"
)

var _ = Describe("Namespace Controller", func() {
	ctx := context.Background()

	const (
		globalSecretName      = "gardener-dati-kubeconfig"
		globalSecretNamespace = "dati-provider"
		bothExports           = DefaultSimpleClusterExportName + "," + DefaultDatiExportName
	)

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
			Client:                          k8sClient,
			Scheme:                          k8sClient.Scheme(),
			KubeconfigSource:                KubeconfigSourceGlobal,
			GlobalKubeconfigSecretName:      globalSecretName,
			GlobalKubeconfigSecretNamespace: globalSecretNamespace,
			ClientKubeconfigSecretName:      "dati-gardener-kubeconfig",
			SimpleClusterExportName:         DefaultSimpleClusterExportName,
			DatiExportName:                  DefaultDatiExportName,
		}
	}

	getSCPC := func(namespace string) (*unstructured.Unstructured, error) {
		scpc := &unstructured.Unstructured{}
		scpc.SetGroupVersionKind(scpcGVK)
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: datiSCPCName}, scpc)
		return scpc, err
	}

	scpcShootTemplate := func(scpc *unstructured.Unstructured) string {
		resources, _, _ := unstructured.NestedSlice(scpc.Object, "spec", "resources")
		Expect(resources).NotTo(BeEmpty())
		first := resources[0].(map[string]interface{})
		tpl, _ := first["template"].(string)
		return tpl
	}

	reconcileNS := func(name string) {
		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name}})
		Expect(err).NotTo(HaveOccurred())
	}

	It("creates a namespace-owned SCPC when both exports are bound", func() {
		name := newNamespace(bothExports)
		reconcileNS(name)

		scpc, err := getSCPC(name)
		Expect(err).NotTo(HaveOccurred())

		Expect(scpc.GetLabels()).To(HaveKeyWithValue(datiManagedByLabelKey, datiManagedByNamespaceBind))

		owners := scpc.GetOwnerReferences()
		Expect(owners).To(HaveLen(1))
		Expect(owners[0].Kind).To(Equal("Namespace"))
		Expect(owners[0].Name).To(Equal(name))

		refName, _, _ := unstructured.NestedString(scpc.Object, "spec", "target", "kubeconfigSecretRef", "name")
		refNS, _, _ := unstructured.NestedString(scpc.Object, "spec", "target", "kubeconfigSecretRef", "namespace")
		Expect(refName).To(Equal(globalSecretName))
		Expect(refNS).To(Equal(globalSecretNamespace))

		// Empty override keeps the built-in default Shoot template.
		Expect(scpcShootTemplate(scpc)).To(ContainSubstring("core.gardener.cloud/v1beta1"))
	})

	It("does not create an SCPC when the dati export is absent", func() {
		name := newNamespace(DefaultSimpleClusterExportName)
		reconcileNS(name)

		_, err := getSCPC(name)
		Expect(err).To(HaveOccurred())
	})

	It("removes the managed SCPC when the dati binding is dropped", func() {
		name := newNamespace(bothExports)
		reconcileNS(name)
		_, err := getSCPC(name)
		Expect(err).NotTo(HaveOccurred())

		// Unbind dati: the annotation now lists only SimpleCluster.
		setExports(name, DefaultSimpleClusterExportName)
		reconcileNS(name)

		_, err = getSCPC(name)
		Expect(err).To(HaveOccurred())
	})

	It("keeps the embedded SCPC template even when a Shoot carries a spec", func() {
		name := newNamespace(bothExports)
		custom := "apiVersion: core.gardener.cloud/v1beta1\nkind: Shoot\nmetadata:\n  name: custom\nspec:\n  region: eu\n"
		Expect(k8sClient.Create(ctx, &dativ1alpha1.Shoot{
			ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: name},
			Spec:       custom,
		})).To(Succeed())

		reconcileNS(name)

		// The CR spec feeds the CR's own Gardener Shoot, never the SCPC.
		scpc, err := getSCPC(name)
		Expect(err).NotTo(HaveOccurred())
		Expect(scpcShootTemplate(scpc)).NotTo(Equal(custom))
		Expect(scpcShootTemplate(scpc)).To(ContainSubstring("core.gardener.cloud/v1beta1"))
	})
})
