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
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
)

var _ = Describe("SimpleClusterProviderConfig Controller", func() {
	ctx := context.Background()

	Context("with a valid local-cluster SCPC", func() {
		const name = "valid-scpc"
		key := types.NamespacedName{Name: name}

		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, &showroomv1alpha1.SimpleClusterProviderConfig{
				ObjectMeta: metav1.ObjectMeta{Name: name},
				Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{
					Target: showroomv1alpha1.TargetSpec{Namespace: "default"},
					Resources: []showroomv1alpha1.ResourceSpec{{
						Name:       "config",
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Template:   "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: x}\n",
					}},
					KubeconfigSource: showroomv1alpha1.KubeconfigSource{
						ResourceName: "config",
						Inline:       &showroomv1alpha1.KubeconfigInlineSource{KubeconfigPath: "data.kubeconfig"},
					},
				},
			})).To(Succeed())
		})

		AfterEach(func() {
			scpc := &showroomv1alpha1.SimpleClusterProviderConfig{}
			if err := k8sClient.Get(ctx, key, scpc); err == nil {
				_ = k8sClient.Delete(ctx, scpc)
			}
		})

		It("reports Reachable + resolves every resource GVK", func() {
			r := &SimpleClusterProviderConfigReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			updated := &showroomv1alpha1.SimpleClusterProviderConfig{}
			Expect(k8sClient.Get(ctx, key, updated)).To(Succeed())

			Expect(updated.Status.Reachable).NotTo(BeNil())
			Expect(*updated.Status.Reachable).To(BeTrue())
			Expect(updated.Status.Resources).To(HaveLen(1))
			Expect(updated.Status.Resources[0].Name).To(Equal("config"))
			Expect(updated.Status.Resources[0].GVK).To(ContainSubstring("ConfigMap"))

			ready := apimeta.FindStatusCondition(updated.Status.Conditions, showroomv1alpha1.ConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))
			Expect(ready.Reason).To(Equal("Reachable"))
		})
	})

	Context("with an invalid DAG", func() {
		const name = "invalid-scpc"
		key := types.NamespacedName{Name: name}

		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, &showroomv1alpha1.SimpleClusterProviderConfig{
				ObjectMeta: metav1.ObjectMeta{Name: name},
				Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{
					Target: showroomv1alpha1.TargetSpec{Namespace: "default"},
					Resources: []showroomv1alpha1.ResourceSpec{
						{
							Name:       "a",
							APIVersion: "v1",
							Kind:       "ConfigMap",
							Template:   "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: x}\n",
							DependsOn:  []string{"b"},
						},
						{
							Name:       "b",
							APIVersion: "v1",
							Kind:       "ConfigMap",
							Template:   "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: x}\n",
							DependsOn:  []string{"a"},
						},
					},
					KubeconfigSource: showroomv1alpha1.KubeconfigSource{
						ResourceName: "a",
						Inline:       &showroomv1alpha1.KubeconfigInlineSource{KubeconfigPath: "data.kubeconfig"},
					},
				},
			})).To(Succeed())
		})

		AfterEach(func() {
			scpc := &showroomv1alpha1.SimpleClusterProviderConfig{}
			if err := k8sClient.Get(ctx, key, scpc); err == nil {
				_ = k8sClient.Delete(ctx, scpc)
			}
		})

		It("surfaces InvalidSCPC in ConditionReady=False", func() {
			r := &SimpleClusterProviderConfigReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			updated := &showroomv1alpha1.SimpleClusterProviderConfig{}
			Expect(k8sClient.Get(ctx, key, updated)).To(Succeed())

			Expect(updated.Status.Resources).To(BeEmpty())
			ready := apimeta.FindStatusCondition(updated.Status.Conditions, showroomv1alpha1.ConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal("InvalidSCPC"))
			Expect(ready.Message).To(ContainSubstring("cycle"))
		})
	})
})
