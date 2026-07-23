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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dativ1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/providers/dati-provider/api/v1alpha1"
)

var _ = Describe("Shoot Controller", func() {
	ctx := context.Background()

	const (
		ns     = "default"
		shName = "dati"
	)
	shKey := types.NamespacedName{Namespace: ns, Name: shName}

	newReconciler := func() *ShootReconciler {
		return &ShootReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
	}

	// seedSCPC materialises a dati SCPC in ns, as the NamespaceReconciler would.
	seedSCPC := func() {
		scpc, err := renderDatiSCPC(ns, corev1.SecretReference{Name: "gardener-dati-kubeconfig", Namespace: ns}, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Create(ctx, scpc)).To(Succeed())
	}

	deleteSCPC := func() {
		scpc := &unstructured.Unstructured{}
		scpc.SetGroupVersionKind(scpcGVK)
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: datiSCPCName}, scpc); err == nil {
			_ = k8sClient.Delete(ctx, scpc)
		}
	}

	cleanup := func() {
		sh := &dativ1alpha1.Shoot{}
		if err := k8sClient.Get(ctx, shKey, sh); err == nil {
			sh.Finalizers = nil
			_ = k8sClient.Update(ctx, sh)
			_ = k8sClient.Delete(ctx, sh)
		}
		deleteSCPC()
	}

	BeforeEach(func() {
		Expect(k8sClient.Create(ctx, &dativ1alpha1.Shoot{
			ObjectMeta: metav1.ObjectMeta{Name: shName, Namespace: ns},
		})).To(Succeed())
	})

	AfterEach(cleanup)

	It("adds the finalizer and reports Ready when the dati SCPC is present", func() {
		seedSCPC()

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
		Expect(err).NotTo(HaveOccurred())

		sh := &dativ1alpha1.Shoot{}
		Expect(k8sClient.Get(ctx, shKey, sh)).To(Succeed())
		Expect(sh.Finalizers).To(ContainElement(datiShootFinalizer))
		Expect(sh.Status.ProviderConfigs).To(Equal([]string{datiSCPCName}))
		ready := apimeta.FindStatusCondition(sh.Status.Conditions, conditionReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
	})

	It("reports SCPCNotProvisioned when no dati SCPC exists in the namespace", func() {
		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
		Expect(err).NotTo(HaveOccurred())

		sh := &dativ1alpha1.Shoot{}
		Expect(k8sClient.Get(ctx, shKey, sh)).To(Succeed())
		Expect(sh.Status.ProviderConfigs).To(BeEmpty())
		ready := apimeta.FindStatusCondition(sh.Status.Conditions, conditionReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal("SCPCNotProvisioned"))
	})

	It("never creates or owns the SCPC", func() {
		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
		Expect(err).NotTo(HaveOccurred())

		scpc := &unstructured.Unstructured{}
		scpc.SetGroupVersionKind(scpcGVK)
		err = k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: datiSCPCName}, scpc)
		Expect(err).To(HaveOccurred()) // the Shoot controller must not create it
	})

	It("removes the finalizer on delete when no SimpleCluster references dati", func() {
		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
		Expect(err).NotTo(HaveOccurred())

		sh := &dativ1alpha1.Shoot{}
		Expect(k8sClient.Get(ctx, shKey, sh)).To(Succeed())
		Expect(k8sClient.Delete(ctx, sh)).To(Succeed())

		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
		Expect(err).NotTo(HaveOccurred())

		err = k8sClient.Get(ctx, shKey, sh)
		Expect(err).To(HaveOccurred()) // finalizer gone => object gone
	})

	It("blocks deletion while a SimpleCluster references dati", func() {
		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
		Expect(err).NotTo(HaveOccurred())

		sc := &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "showroom.apeirora.eu/v1alpha1",
			"kind":       "SimpleCluster",
			"metadata":   map[string]interface{}{"name": "uses-dati", "namespace": ns},
			"spec": map[string]interface{}{
				"workers": map[string]interface{}{
					"nodes":     int64(1),
					"requested": map[string]interface{}{"cpu": "2", "memory": "8Gi"},
				},
				"providerConfigRefs": []interface{}{datiSCPCName},
			},
		}}
		Expect(k8sClient.Create(ctx, sc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, sc) })

		sh := &dativ1alpha1.Shoot{}
		Expect(k8sClient.Get(ctx, shKey, sh)).To(Succeed())
		Expect(k8sClient.Delete(ctx, sh)).To(Succeed())

		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, shKey, sh)).To(Succeed()) // still held by finalizer
		ready := apimeta.FindStatusCondition(sh.Status.Conditions, conditionReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal("SCPCInUse"))

		Expect(k8sClient.Delete(ctx, sc)).To(Succeed())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, shKey, sh)).To(HaveOccurred())
	})
})
