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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
)

func identityTestSCPC(name, namespace, targetName string) *showroomv1alpha1.SimpleClusterProviderConfig {
	return &showroomv1alpha1.SimpleClusterProviderConfig{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{
			Target: showroomv1alpha1.TargetSpec{Namespace: "default"},
			Resources: []showroomv1alpha1.ResourceSpec{{
				Name:         "config",
				APIVersion:   "v1",
				Kind:         "ConfigMap",
				NameTemplate: targetName,
				Template: `apiVersion: v1
kind: ConfigMap
metadata:
  name: ignored
data:
  marker: identity-test
`,
			}},
			KubeconfigSource: showroomv1alpha1.KubeconfigSource{
				ResourceName: "config",
				Inline:       &showroomv1alpha1.KubeconfigInlineSource{KubeconfigPath: "data.kubeconfig"},
			},
		},
	}
}

func identityTestSC(name, providerName string) *showroomv1alpha1.SimpleCluster {
	return &showroomv1alpha1.SimpleCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: showroomv1alpha1.SimpleClusterSpec{
			Workers:            showroomv1alpha1.WorkersSpec{Nodes: ptr.To[int32](1)},
			ProviderConfigRefs: []string{providerName},
		},
	}
}

func identityTestReconciler(fallbackNamespace string) *SimpleClusterReconciler {
	return &SimpleClusterReconciler{
		Client:                k8sClient,
		APIReader:             k8sClient,
		Scheme:                k8sClient.Scheme(),
		RESTConfig:            cfg,
		FallbackSCPCNamespace: fallbackNamespace,
	}
}

func setLegacyProviderConfigStatus(
	ctx context.Context,
	key types.NamespacedName,
	providerName string,
) {
	sc := &showroomv1alpha1.SimpleCluster{}
	Expect(k8sClient.Get(ctx, key, sc)).To(Succeed())
	patch := client.MergeFrom(sc.DeepCopy())
	sc.Status.ProviderConfigRef = &corev1.LocalObjectReference{Name: providerName}
	Expect(k8sClient.Status().Patch(ctx, sc, patch)).To(Succeed())
}

func cleanupIdentityTest(
	ctx context.Context,
	scKey types.NamespacedName,
	targetKeys, scpcKeys []types.NamespacedName,
	namespace string,
) {
	for _, key := range targetKeys {
		obj := &corev1.ConfigMap{}
		if err := k8sClient.Get(ctx, key, obj); err == nil {
			obj.Finalizers = nil
			_ = k8sClient.Update(ctx, obj)
			_ = k8sClient.Delete(ctx, obj)
		}
	}
	sc := &showroomv1alpha1.SimpleCluster{}
	if err := k8sClient.Get(ctx, scKey, sc); err == nil {
		sc.Finalizers = nil
		_ = k8sClient.Update(ctx, sc)
		_ = k8sClient.Delete(ctx, sc)
	}
	for _, key := range scpcKeys {
		scpc := &showroomv1alpha1.SimpleClusterProviderConfig{}
		if err := k8sClient.Get(ctx, key, scpc); err == nil {
			_ = k8sClient.Delete(ctx, scpc)
		}
	}
	if namespace != "" {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		_ = k8sClient.Delete(ctx, ns)
	}
}

var _ = Describe("SimpleCluster provider config identity", func() {
	ctx := context.Background()

	It("pins a fallback SCPC before work and ignores a later same-name tenant SCPC", func() {
		const (
			fallbackNamespace = "identity-shadow-fallback"
			providerName      = "identity-shadow-provider"
			scName            = "identity-shadow-sc"
		)
		scKey := types.NamespacedName{Name: scName, Namespace: "default"}
		fallbackKey := types.NamespacedName{Name: providerName, Namespace: fallbackNamespace}
		tenantKey := types.NamespacedName{Name: providerName, Namespace: "default"}
		fallbackTarget := types.NamespacedName{Name: "identity-shadow-fallback-target", Namespace: "default"}
		tenantTarget := types.NamespacedName{Name: "identity-shadow-tenant-target", Namespace: "default"}
		DeferCleanup(cleanupIdentityTest, ctx, scKey, []types.NamespacedName{fallbackTarget, tenantTarget},
			[]types.NamespacedName{fallbackKey, tenantKey}, fallbackNamespace)

		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: fallbackNamespace}})).To(Succeed())
		fallback := identityTestSCPC(providerName, fallbackNamespace, fallbackTarget.Name)
		Expect(k8sClient.Create(ctx, fallback)).To(Succeed())
		Expect(k8sClient.Create(ctx, identityTestSC(scName, providerName))).To(Succeed())
		r := identityTestReconciler(fallbackNamespace)
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
		Expect(err).NotTo(HaveOccurred())

		locked := &showroomv1alpha1.SimpleCluster{}
		Expect(k8sClient.Get(ctx, scKey, locked)).To(Succeed())
		Expect(locked.Status.ProviderConfigRef).NotTo(BeNil())
		Expect(locked.Status.ProviderConfigRef.Name).To(Equal(providerName))
		Expect(locked.Status.ProviderConfigNamespace).To(Equal(fallbackNamespace))
		Expect(locked.Status.ProviderConfigUID).To(Equal(fallback.UID))
		Expect(k8sClient.Get(ctx, fallbackTarget, &corev1.ConfigMap{})).To(Succeed())

		tenant := identityTestSCPC(providerName, "default", tenantTarget.Name)
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
		Expect(err).NotTo(HaveOccurred())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, tenantTarget, &corev1.ConfigMap{}))).To(BeTrue())

		Expect(k8sClient.Delete(ctx, locked)).To(Succeed())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
		Expect(err).NotTo(HaveOccurred())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, fallbackTarget, &corev1.ConfigMap{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, tenantTarget, &corev1.ConfigMap{}))).To(BeTrue())
	})

	It("rejects a same-name SCPC replacement for reconcile and delete", func() {
		const (
			fallbackNamespace = "identity-replace-fallback"
			providerName      = "identity-replace-provider"
			scName            = "identity-replace-sc"
		)
		scKey := types.NamespacedName{Name: scName, Namespace: "default"}
		scpcKey := types.NamespacedName{Name: providerName, Namespace: fallbackNamespace}
		originalTarget := types.NamespacedName{Name: "identity-replace-original", Namespace: "default"}
		replacementTarget := types.NamespacedName{Name: "identity-replace-new", Namespace: "default"}
		DeferCleanup(cleanupIdentityTest, ctx, scKey, []types.NamespacedName{originalTarget, replacementTarget},
			[]types.NamespacedName{scpcKey}, fallbackNamespace)

		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: fallbackNamespace}})).To(Succeed())
		original := identityTestSCPC(providerName, fallbackNamespace, originalTarget.Name)
		Expect(k8sClient.Create(ctx, original)).To(Succeed())
		originalUID := original.UID
		Expect(k8sClient.Create(ctx, identityTestSC(scName, providerName))).To(Succeed())
		r := identityTestReconciler(fallbackNamespace)
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, originalTarget, &corev1.ConfigMap{})).To(Succeed())

		Expect(k8sClient.Delete(ctx, original)).To(Succeed())
		replacement := identityTestSCPC(providerName, fallbackNamespace, replacementTarget.Name)
		Expect(k8sClient.Create(ctx, replacement)).To(Succeed())
		Expect(replacement.UID).NotTo(Equal(originalUID))
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
		Expect(err).NotTo(HaveOccurred())

		failed := &showroomv1alpha1.SimpleCluster{}
		Expect(k8sClient.Get(ctx, scKey, failed)).To(Succeed())
		Expect(failed.Status.Phase).To(Equal(showroomv1alpha1.PhaseFailed))
		errCondition := apimeta.FindStatusCondition(failed.Status.Conditions, showroomv1alpha1.ConditionError)
		Expect(errCondition).NotTo(BeNil())
		Expect(errCondition.Reason).To(Equal("SCPCIdentityMismatch"))
		readyCondition := apimeta.FindStatusCondition(failed.Status.Conditions, showroomv1alpha1.ConditionReady)
		Expect(readyCondition).NotTo(BeNil())
		Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(readyCondition.Reason).To(Equal("SCPCIdentityMismatch"))
		Expect(failed.Status.ProviderConfigUID).To(Equal(originalUID))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, replacementTarget, &corev1.ConfigMap{}))).To(BeTrue())
		Expect(k8sClient.Get(ctx, originalTarget, &corev1.ConfigMap{})).To(Succeed())

		Expect(k8sClient.Delete(ctx, failed)).To(Succeed())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
		Expect(err).NotTo(HaveOccurred())
		blocked := &showroomv1alpha1.SimpleCluster{}
		Expect(k8sClient.Get(ctx, scKey, blocked)).To(Succeed())
		Expect(blocked.Finalizers).To(ContainElement(showroomv1alpha1.FinalizerName))
		Expect(k8sClient.Get(ctx, originalTarget, &corev1.ConfigMap{})).To(Succeed())
	})

	It("retains the finalizer when the exact locked SCPC is missing", func() {
		const (
			fallbackNamespace = "identity-missing-fallback"
			providerName      = "identity-missing-provider"
			scName            = "identity-missing-sc"
		)
		scKey := types.NamespacedName{Name: scName, Namespace: "default"}
		scpcKey := types.NamespacedName{Name: providerName, Namespace: fallbackNamespace}
		targetKey := types.NamespacedName{Name: "identity-missing-target", Namespace: "default"}
		DeferCleanup(cleanupIdentityTest, ctx, scKey, []types.NamespacedName{targetKey},
			[]types.NamespacedName{scpcKey}, fallbackNamespace)

		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: fallbackNamespace}})).To(Succeed())
		scpc := identityTestSCPC(providerName, fallbackNamespace, targetKey.Name)
		Expect(k8sClient.Create(ctx, scpc)).To(Succeed())
		Expect(k8sClient.Create(ctx, identityTestSC(scName, providerName))).To(Succeed())
		r := identityTestReconciler(fallbackNamespace)
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, targetKey, &corev1.ConfigMap{})).To(Succeed())
		Expect(k8sClient.Delete(ctx, scpc)).To(Succeed())

		sc := &showroomv1alpha1.SimpleCluster{}
		Expect(k8sClient.Get(ctx, scKey, sc)).To(Succeed())
		Expect(k8sClient.Delete(ctx, sc)).To(Succeed())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
		Expect(err).NotTo(HaveOccurred())

		blocked := &showroomv1alpha1.SimpleCluster{}
		Expect(k8sClient.Get(ctx, scKey, blocked)).To(Succeed())
		Expect(blocked.Finalizers).To(ContainElement(showroomv1alpha1.FinalizerName))
		errCondition := apimeta.FindStatusCondition(blocked.Status.Conditions, showroomv1alpha1.ConditionError)
		Expect(errCondition).NotTo(BeNil())
		Expect(errCondition.Reason).To(Equal("SCPCNotFound"))
		Expect(k8sClient.Get(ctx, targetKey, &corev1.ConfigMap{})).To(Succeed())
	})

	It("backfills a legacy name-only reference when exactly one candidate exists", func() {
		const (
			fallbackNamespace = "identity-legacy-fallback"
			providerName      = "identity-legacy-provider"
			scName            = "identity-legacy-sc"
		)
		scKey := types.NamespacedName{Name: scName, Namespace: "default"}
		scpcKey := types.NamespacedName{Name: providerName, Namespace: fallbackNamespace}
		targetKey := types.NamespacedName{Name: "identity-legacy-target", Namespace: "default"}
		DeferCleanup(cleanupIdentityTest, ctx, scKey, []types.NamespacedName{targetKey},
			[]types.NamespacedName{scpcKey}, fallbackNamespace)

		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: fallbackNamespace}})).To(Succeed())
		scpc := identityTestSCPC(providerName, fallbackNamespace, targetKey.Name)
		Expect(k8sClient.Create(ctx, scpc)).To(Succeed())
		Expect(k8sClient.Create(ctx, identityTestSC(scName, providerName))).To(Succeed())
		setLegacyProviderConfigStatus(ctx, scKey, providerName)

		_, err := identityTestReconciler(fallbackNamespace).Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
		Expect(err).NotTo(HaveOccurred())
		backfilled := &showroomv1alpha1.SimpleCluster{}
		Expect(k8sClient.Get(ctx, scKey, backfilled)).To(Succeed())
		Expect(backfilled.Status.ProviderConfigNamespace).To(Equal(fallbackNamespace))
		Expect(backfilled.Status.ProviderConfigUID).To(Equal(scpc.UID))
		Expect(k8sClient.Get(ctx, targetKey, &corev1.ConfigMap{})).To(Succeed())
	})

	It("refuses to backfill a legacy name-only reference when both candidates exist", func() {
		const (
			fallbackNamespace = "identity-ambiguous-fallback"
			providerName      = "identity-ambiguous-provider"
			scName            = "identity-ambiguous-sc"
		)
		scKey := types.NamespacedName{Name: scName, Namespace: "default"}
		fallbackKey := types.NamespacedName{Name: providerName, Namespace: fallbackNamespace}
		tenantKey := types.NamespacedName{Name: providerName, Namespace: "default"}
		fallbackTarget := types.NamespacedName{Name: "identity-ambiguous-fallback-target", Namespace: "default"}
		tenantTarget := types.NamespacedName{Name: "identity-ambiguous-tenant-target", Namespace: "default"}
		DeferCleanup(cleanupIdentityTest, ctx, scKey, []types.NamespacedName{fallbackTarget, tenantTarget},
			[]types.NamespacedName{fallbackKey, tenantKey}, fallbackNamespace)

		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: fallbackNamespace}})).To(Succeed())
		Expect(k8sClient.Create(ctx, identityTestSCPC(providerName, fallbackNamespace, fallbackTarget.Name))).To(Succeed())
		Expect(k8sClient.Create(ctx, identityTestSCPC(providerName, "default", tenantTarget.Name))).To(Succeed())
		Expect(k8sClient.Create(ctx, identityTestSC(scName, providerName))).To(Succeed())
		setLegacyProviderConfigStatus(ctx, scKey, providerName)

		_, err := identityTestReconciler(fallbackNamespace).Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
		Expect(err).NotTo(HaveOccurred())
		refused := &showroomv1alpha1.SimpleCluster{}
		Expect(k8sClient.Get(ctx, scKey, refused)).To(Succeed())
		Expect(refused.Status.Phase).To(Equal(showroomv1alpha1.PhaseFailed))
		Expect(refused.Status.ProviderConfigNamespace).To(BeEmpty())
		Expect(refused.Status.ProviderConfigUID).To(BeEmpty())
		errCondition := apimeta.FindStatusCondition(refused.Status.Conditions, showroomv1alpha1.ConditionError)
		Expect(errCondition).NotTo(BeNil())
		Expect(errCondition.Reason).To(Equal("SCPCReferenceAmbiguous"))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, fallbackTarget, &corev1.ConfigMap{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, tenantTarget, &corev1.ConfigMap{}))).To(BeTrue())

		Expect(k8sClient.Delete(ctx, refused)).To(Succeed())
		_, err = identityTestReconciler(fallbackNamespace).Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
		Expect(err).NotTo(HaveOccurred())
		blocked := &showroomv1alpha1.SimpleCluster{}
		Expect(k8sClient.Get(ctx, scKey, blocked)).To(Succeed())
		Expect(blocked.Finalizers).To(ContainElement(showroomv1alpha1.FinalizerName))
	})

	It("never adopts or deletes an unrelated deterministic-name collision", func() {
		const (
			providerName = "identity-collision-provider"
			scName       = "identity-collision-sc"
		)
		scKey := types.NamespacedName{Name: scName, Namespace: "default"}
		scpcKey := types.NamespacedName{Name: providerName, Namespace: "default"}
		targetKey := types.NamespacedName{Name: "identity-collision-target", Namespace: "default"}
		DeferCleanup(cleanupIdentityTest, ctx, scKey, []types.NamespacedName{targetKey}, []types.NamespacedName{scpcKey}, "")

		Expect(k8sClient.Create(ctx, identityTestSCPC(providerName, "default", targetKey.Name))).To(Succeed())
		Expect(k8sClient.Create(ctx, identityTestSC(scName, providerName))).To(Succeed())
		Expect(k8sClient.Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name: targetKey.Name, Namespace: targetKey.Namespace,
		}})).To(Succeed())
		r := identityTestReconciler("")
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
		Expect(err).NotTo(HaveOccurred())

		failed := &showroomv1alpha1.SimpleCluster{}
		Expect(k8sClient.Get(ctx, scKey, failed)).To(Succeed())
		errCondition := apimeta.FindStatusCondition(failed.Status.Conditions, showroomv1alpha1.ConditionError)
		Expect(errCondition).NotTo(BeNil())
		Expect(errCondition.Reason).To(Equal("TargetOwnershipConflict"))
		Expect(failed.Status.Resources).To(BeEmpty())

		Expect(k8sClient.Delete(ctx, failed)).To(Succeed())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
		Expect(err).NotTo(HaveOccurred())
		blocked := &showroomv1alpha1.SimpleCluster{}
		Expect(k8sClient.Get(ctx, scKey, blocked)).To(Succeed())
		Expect(blocked.Finalizers).To(ContainElement(showroomv1alpha1.FinalizerName))
		Expect(k8sClient.Get(ctx, targetKey, &corev1.ConfigMap{})).To(Succeed())
	})
})
