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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sshkeysv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/sshkey-operator/api/v1alpha1"
)

var _ = Describe("SSHKey Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // Modify as needed
		}
		sshkey := &sshkeysv1alpha1.SSHKey{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind SSHKey")
			err := k8sClient.Get(ctx, typeNamespacedName, sshkey)
			if err != nil && errors.IsNotFound(err) {
				resource := &sshkeysv1alpha1.SSHKey{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					// Specify other spec details if needed.
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// Cleanup logic after each test, like removing the resource instance.
			resource := &sshkeysv1alpha1.SSHKey{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance SSHKey")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &SSHKeyReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("generating the key pair with a name@namespace comment")
			generated := &sshkeysv1alpha1.SSHKey{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, generated)).To(Succeed())
			Expect(generated.PublicKey).To(ContainSubstring(resourceName + "@default"))
			Expect(generated.PrivateKey).To(ContainSubstring("OPENSSH PRIVATE KEY"))
			Expect(generated.Status.Ready).To(BeTrue())
		})

		It("should export to a secret and report ExportSynced", func() {
			By("requesting an export to secret")
			resource := &sshkeysv1alpha1.SSHKey{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			resource.Spec.ExportToSecret = corev1.LocalObjectReference{Name: "exported-keypair"}
			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			controllerReconciler := &SSHKeyReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("creating an ssh-auth secret matching the key")
			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "exported-keypair", Namespace: "default"}, secret)).To(Succeed())
			Expect(secret.Type).To(Equal(corev1.SecretTypeSSHAuth))

			updated := &sshkeysv1alpha1.SSHKey{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			Expect(secret.Data[corev1.SSHAuthPrivateKey]).To(Equal([]byte(updated.PrivateKey)))
			Expect(meta.IsStatusConditionTrue(updated.Status.Conditions, sshkeysv1alpha1.ConditionExportSynced)).To(BeTrue())
		})

		It("should regenerate the key when the key-defining spec changes", func() {
			controllerReconciler := &SSHKeyReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			before := &sshkeysv1alpha1.SSHKey{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, before)).To(Succeed())

			By("changing the key type")
			before.Spec.KeyType = sshkeysv1alpha1.KeyTypeRSA
			before.Spec.KeySize = 2048
			Expect(k8sClient.Update(ctx, before)).To(Succeed())

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			after := &sshkeysv1alpha1.SSHKey{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, after)).To(Succeed())
			Expect(after.PublicKey).To(HavePrefix("ssh-rsa "))
			Expect(after.PrivateKey).NotTo(Equal(before.PrivateKey))
			Expect(after.Status.KeyHash).NotTo(Equal(before.Status.KeyHash))
		})

		It("should reject a passphrase with a format that does not support it", func() {
			resource := &sshkeysv1alpha1.SSHKey{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			resource.Spec.KeyFormat = sshkeysv1alpha1.KeyFormatRFC4716
			resource.Spec.PassPhaseSecret = corev1.SecretReference{Name: "any-secret"}
			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			controllerReconciler := &SSHKeyReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			rejected := &sshkeysv1alpha1.SSHKey{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, rejected)).To(Succeed())
			Expect(rejected.PrivateKey).To(BeEmpty())
			Expect(rejected.Status.Ready).To(BeFalse())
			cond := meta.FindStatusCondition(rejected.Status.Conditions, sshkeysv1alpha1.ConditionPassPhaseSecret)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal("FormatUnsupported"))
		})
	})
})
