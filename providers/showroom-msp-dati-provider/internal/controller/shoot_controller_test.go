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
		ns         = "default"
		shName     = "dati"
		secretName = "gardener-dati-kubeconfig"
	)
	shKey := types.NamespacedName{Namespace: ns, Name: shName}

	const clientSecretName = "dati-gardener-kubeconfig"

	// global-mode reconciler: the shared admin secret lives in `ns` as `secretName`.
	newReconciler := func() *ShootReconciler {
		return &ShootReconciler{
			Client:                          k8sClient,
			Scheme:                          k8sClient.Scheme(),
			KubeconfigSource:                KubeconfigSourceGlobal,
			GlobalKubeconfigSecretName:      secretName,
			GlobalKubeconfigSecretNamespace: ns,
			ClientKubeconfigSecretName:      clientSecretName,
		}
	}

	getSCPC := func() (*unstructured.Unstructured, error) {
		scpc := &unstructured.Unstructured{}
		scpc.SetGroupVersionKind(scpcGVK)
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: datiSCPCName}, scpc)
		return scpc, err
	}

	scpcShootTemplate := func(scpc *unstructured.Unstructured) string {
		resources, _, _ := unstructured.NestedSlice(scpc.Object, "spec", "resources")
		Expect(resources).NotTo(BeEmpty())
		first := resources[0].(map[string]interface{})
		tpl, _ := first["template"].(string)
		return tpl
	}

	cleanup := func() {
		sh := &dativ1alpha1.Shoot{}
		if err := k8sClient.Get(ctx, shKey, sh); err == nil {
			sh.Finalizers = nil
			_ = k8sClient.Update(ctx, sh)
			_ = k8sClient.Delete(ctx, sh)
		}
		if scpc, err := getSCPC(); err == nil {
			_ = k8sClient.Delete(ctx, scpc)
		}
		secret := &corev1.Secret{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: secretName}, secret); err == nil {
			_ = k8sClient.Delete(ctx, secret)
		}
	}

	Context("with the admin kubeconfig Secret present", func() {
		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: ns},
				StringData: map[string]string{"kubeconfig": "apiVersion: v1\nkind: Config\n"},
			})).To(Succeed())
			Expect(k8sClient.Create(ctx, &dativ1alpha1.Shoot{
				ObjectMeta: metav1.ObjectMeta{Name: shName, Namespace: ns},
			})).To(Succeed())
		})

		AfterEach(cleanup)

		It("creates an owned SCPC in the Shoot's namespace and reports Ready", func() {
			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
			Expect(err).NotTo(HaveOccurred())

			scpc, err := getSCPC()
			Expect(err).NotTo(HaveOccurred())
			Expect(scpc.GetOwnerReferences()).To(HaveLen(1))
			Expect(scpc.GetOwnerReferences()[0].Kind).To(Equal("Shoot"))

			refName, _, _ := unstructured.NestedString(scpc.Object, "spec", "target", "kubeconfigSecretRef", "name")
			refNS, _, _ := unstructured.NestedString(scpc.Object, "spec", "target", "kubeconfigSecretRef", "namespace")
			Expect(refName).To(Equal(secretName))
			Expect(refNS).To(Equal(ns))

			renewal, _, _ := unstructured.NestedString(scpc.Object, "spec", "kubeconfigSource", "renewalBefore")
			Expect(renewal).To(Equal("4h"))

			// Empty text spec keeps the built-in default Shoot template.
			Expect(scpcShootTemplate(scpc)).To(ContainSubstring("core.gardener.cloud/v1beta1"))

			sh := &dativ1alpha1.Shoot{}
			Expect(k8sClient.Get(ctx, shKey, sh)).To(Succeed())
			Expect(sh.Status.ProviderConfigs).To(Equal([]string{datiSCPCName}))
			ready := apimeta.FindStatusCondition(sh.Status.Conditions, conditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))
			creds := apimeta.FindStatusCondition(sh.Status.Conditions, conditionCredentialsValid)
			Expect(creds).NotTo(BeNil())
			Expect(creds.Status).To(Equal(metav1.ConditionTrue))
		})

		It("uses the Shoot's text spec as the SCPC shoot template", func() {
			custom := "apiVersion: core.gardener.cloud/v1beta1\nkind: Shoot\nmetadata:\n  name: custom\n  namespace: garden-sap\nspec:\n  region: {{ .Inputs.region }}\n"
			sh := &dativ1alpha1.Shoot{}
			Expect(k8sClient.Get(ctx, shKey, sh)).To(Succeed())
			sh.Spec = custom
			Expect(k8sClient.Update(ctx, sh)).To(Succeed())

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
			Expect(err).NotTo(HaveOccurred())

			scpc, err := getSCPC()
			Expect(err).NotTo(HaveOccurred())
			Expect(scpcShootTemplate(scpc)).To(Equal(custom))
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

	Context("with the admin kubeconfig Secret missing", func() {
		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, &dativ1alpha1.Shoot{
				ObjectMeta: metav1.ObjectMeta{Name: shName, Namespace: ns},
			})).To(Succeed())
		})

		AfterEach(cleanup)

		It("still creates the SCPC but reports CredentialsMissing", func() {
			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
			Expect(err).NotTo(HaveOccurred())

			_, err = getSCPC()
			Expect(err).NotTo(HaveOccurred())

			sh := &dativ1alpha1.Shoot{}
			Expect(k8sClient.Get(ctx, shKey, sh)).To(Succeed())
			ready := apimeta.FindStatusCondition(sh.Status.Conditions, conditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal("CredentialsMissing"))
			creds := apimeta.FindStatusCondition(sh.Status.Conditions, conditionCredentialsValid)
			Expect(creds).NotTo(BeNil())
			Expect(creds.Status).To(Equal(metav1.ConditionFalse))
		})
	})

	Context("kcp kubeconfig source (per-client)", func() {
		newKCPReconciler := func() *ShootReconciler {
			return &ShootReconciler{
				Client:                          k8sClient,
				Scheme:                          k8sClient.Scheme(),
				KubeconfigSource:                KubeconfigSourceKCP,
				GlobalKubeconfigSecretName:      secretName,
				GlobalKubeconfigSecretNamespace: "dati-provider",
				ClientKubeconfigSecretName:      clientSecretName,
			}
		}

		AfterEach(func() {
			cleanup()
			cs := &corev1.Secret{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: clientSecretName}, cs); err == nil {
				_ = k8sClient.Delete(ctx, cs)
			}
		})

		It("targets the client Secret in the Shoot's own namespace", func() {
			Expect(k8sClient.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: clientSecretName, Namespace: ns},
				StringData: map[string]string{"kubeconfig": "apiVersion: v1\nkind: Config\n"},
			})).To(Succeed())
			Expect(k8sClient.Create(ctx, &dativ1alpha1.Shoot{
				ObjectMeta: metav1.ObjectMeta{Name: shName, Namespace: ns},
			})).To(Succeed())

			r := newKCPReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
			Expect(err).NotTo(HaveOccurred())

			scpc, err := getSCPC()
			Expect(err).NotTo(HaveOccurred())
			refName, _, _ := unstructured.NestedString(scpc.Object, "spec", "target", "kubeconfigSecretRef", "name")
			refNS, _, _ := unstructured.NestedString(scpc.Object, "spec", "target", "kubeconfigSecretRef", "namespace")
			Expect(refName).To(Equal(clientSecretName))
			Expect(refNS).To(Equal(ns))

			sh := &dativ1alpha1.Shoot{}
			Expect(k8sClient.Get(ctx, shKey, sh)).To(Succeed())
			ready := apimeta.FindStatusCondition(sh.Status.Conditions, conditionReady)
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		})

		It("honours the per-Shoot annotation override to kcp", func() {
			Expect(k8sClient.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: clientSecretName, Namespace: ns},
				StringData: map[string]string{"kubeconfig": "apiVersion: v1\nkind: Config\n"},
			})).To(Succeed())
			Expect(k8sClient.Create(ctx, &dativ1alpha1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:        shName,
					Namespace:   ns,
					Annotations: map[string]string{kubeconfigSourceAnnotation: string(KubeconfigSourceKCP)},
				},
			})).To(Succeed())

			// Operator default is global, but the annotation forces kcp.
			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
			Expect(err).NotTo(HaveOccurred())

			scpc, err := getSCPC()
			Expect(err).NotTo(HaveOccurred())
			refNS, _, _ := unstructured.NestedString(scpc.Object, "spec", "target", "kubeconfigSecretRef", "namespace")
			Expect(refNS).To(Equal(ns)) // Shoot's own namespace, not the global one
		})

		It("reports InvalidKubeconfigSource for an unknown annotation value", func() {
			Expect(k8sClient.Create(ctx, &dativ1alpha1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:        shName,
					Namespace:   ns,
					Annotations: map[string]string{kubeconfigSourceAnnotation: "bogus"},
				},
			})).To(Succeed())

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
			Expect(err).NotTo(HaveOccurred())

			sh := &dativ1alpha1.Shoot{}
			Expect(k8sClient.Get(ctx, shKey, sh)).To(Succeed())
			ready := apimeta.FindStatusCondition(sh.Status.Conditions, conditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal("InvalidKubeconfigSource"))
		})
	})
})
