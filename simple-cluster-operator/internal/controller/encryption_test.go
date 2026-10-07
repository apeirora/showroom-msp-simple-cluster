package controller

import (
	"context"
	"testing"
	"time"

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

func openBaoEncryption() *showroomv1alpha1.EncryptionSpec {
	return &showroomv1alpha1.EncryptionSpec{
		Provider: showroomv1alpha1.EncryptionProvider{Type: showroomv1alpha1.EncryptionProviderOpenBao},
	}
}

func TestSetEncryptionCondition(t *testing.T) {
	scpc := &showroomv1alpha1.SimpleClusterProviderConfig{}
	scpc.Name = "flat"

	sc := &showroomv1alpha1.SimpleCluster{}
	setEncryptionCondition(sc, scpc)
	if apimeta.FindStatusCondition(sc.Status.Conditions, showroomv1alpha1.ConditionEncryptionReady) != nil {
		t.Error("condition present without an encryption request")
	}

	sc.Spec.Encryption = openBaoEncryption()
	setEncryptionCondition(sc, scpc)
	cond := apimeta.FindStatusCondition(sc.Status.Conditions, showroomv1alpha1.ConditionEncryptionReady)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "UnsupportedByProvider" {
		t.Errorf("unsupported provider: got %+v", cond)
	}

	scpc.Spec.EncryptionProviders = []showroomv1alpha1.EncryptionProviderType{showroomv1alpha1.EncryptionProviderOpenBao}
	sc.Status.Phase = showroomv1alpha1.PhaseProvisioning
	setEncryptionCondition(sc, scpc)
	cond = apimeta.FindStatusCondition(sc.Status.Conditions, showroomv1alpha1.ConditionEncryptionReady)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "Provisioning" {
		t.Errorf("provisioning: got %+v", cond)
	}

	sc.Status.Phase = showroomv1alpha1.PhaseReady
	setEncryptionCondition(sc, scpc)
	if !apimeta.IsStatusConditionTrue(sc.Status.Conditions, showroomv1alpha1.ConditionEncryptionReady) {
		t.Error("condition not True for a Ready cluster on a supporting SCPC")
	}
}

var _ = Describe("SimpleCluster encryption opt-in", func() {
	ctx := context.Background()

	newSC := func(name, scpcName string, enc *showroomv1alpha1.EncryptionSpec) *showroomv1alpha1.SimpleCluster {
		return &showroomv1alpha1.SimpleCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: showroomv1alpha1.SimpleClusterSpec{
				Workers:            showroomv1alpha1.WorkersSpec{Nodes: ptr.To[int32](1)},
				ProviderConfigRefs: []string{scpcName},
				Encryption:         enc,
			},
		}
	}

	// extra is spliced into the ConfigMap at top level; an unknown field there
	// stands in for spec.encryption on an outdated target CRD.
	newSCPC := func(name, extra string, providers ...showroomv1alpha1.EncryptionProviderType) *showroomv1alpha1.SimpleClusterProviderConfig {
		return &showroomv1alpha1.SimpleClusterProviderConfig{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{
				Target:              showroomv1alpha1.TargetSpec{Namespace: "default"},
				EncryptionProviders: providers,
				Resources: []showroomv1alpha1.ResourceSpec{{
					Name:       "config",
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Readiness: &showroomv1alpha1.ReadinessCheck{
						PhasePath:       "kind",
						PhaseReadyValue: "ConfigMap",
					},
					Template: `apiVersion: v1
kind: ConfigMap
metadata:
  name: ignored
` + extra + `data:
  encryption: "{{ .SC.encryption.enabled }}/{{ .SC.encryption.provider }}"
  kubeconfig: |
    apiVersion: v1
    kind: Config
    users:
    - name: u
      user:
        token: opaque-not-a-jwt
`,
				}},
				KubeconfigSource: showroomv1alpha1.KubeconfigSource{
					ResourceName: "config",
					Inline:       &showroomv1alpha1.KubeconfigInlineSource{KubeconfigPath: "data.kubeconfig"},
				},
			},
		}
	}

	reconcileTwice := func(name string) {
		r := &SimpleClusterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), RESTConfig: cfg}
		key := types.NamespacedName{Name: name, Namespace: "default"}
		for range 2 {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
		}
	}

	getSC := func(name string) *showroomv1alpha1.SimpleCluster {
		sc := &showroomv1alpha1.SimpleCluster{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, sc)).To(Succeed())
		return sc
	}

	configMapErr := func(scName string) error {
		return k8sClient.Get(ctx, types.NamespacedName{Name: scName + "-config", Namespace: "default"}, &corev1.ConfigMap{})
	}

	cleanup := func(scNames []string, scpcNames []string) {
		for _, name := range scNames {
			sc := &showroomv1alpha1.SimpleCluster{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, sc); err == nil {
				sc.Finalizers = nil
				_ = k8sClient.Update(ctx, sc)
				_ = k8sClient.Delete(ctx, sc)
			}
			for _, obj := range []client.Object{
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name + "-config", Namespace: "default"}},
				&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name + "-kubeconfig", Namespace: "default"}},
			} {
				_ = k8sClient.Delete(ctx, obj)
			}
		}
		for _, name := range scpcNames {
			_ = k8sClient.Delete(ctx, &showroomv1alpha1.SimpleClusterProviderConfig{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			})
		}
	}

	Context("API validation", func() {
		AfterEach(func() { cleanup([]string{"enc-api-on", "enc-api-off"}, nil) })

		It("rejects a rotation period below 1h and unknown providers", func() {
			sc := newSC("enc-api-short", "any", openBaoEncryption())
			sc.Spec.Encryption.Provider.KeyRotationPeriod = &metav1.Duration{Duration: 30 * time.Minute}
			err := k8sClient.Create(ctx, sc)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("keyRotationPeriod must be at least 1h"))

			sc = newSC("enc-api-vault", "any", openBaoEncryption())
			sc.Spec.Encryption.Provider.Type = "Vault"
			err = k8sClient.Create(ctx, sc)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Unsupported value"))
		})

		It("keeps encryption immutable after creation", func() {
			on := newSC("enc-api-on", "any", openBaoEncryption())
			on.Spec.Encryption.Provider.KeyRotationPeriod = &metav1.Duration{Duration: 720 * time.Hour}
			Expect(k8sClient.Create(ctx, on)).To(Succeed())

			changed := getSC("enc-api-on")
			changed.Spec.Encryption.Provider.KeyRotationPeriod = &metav1.Duration{Duration: 1440 * time.Hour}
			err := k8sClient.Update(ctx, changed)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("encryption is immutable"))

			removed := getSC("enc-api-on")
			removed.Spec.Encryption = nil
			err = k8sClient.Update(ctx, removed)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("encryption cannot be added or removed after creation"))

			off := newSC("enc-api-off", "any", nil)
			Expect(k8sClient.Create(ctx, off)).To(Succeed())
			added := getSC("enc-api-off")
			added.Spec.Encryption = openBaoEncryption()
			err = k8sClient.Update(ctx, added)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("encryption cannot be added or removed after creation"))

			By("still allowing other spec changes")
			resized := getSC("enc-api-on")
			resized.Spec.Workers.Nodes = ptr.To[int32](2)
			Expect(k8sClient.Update(ctx, resized)).To(Succeed())
		})
	})

	Context("with an SCPC that does not list the provider", func() {
		const scpcName, scName = "enc-flat", "enc-refused"
		AfterEach(func() { cleanup([]string{scName}, []string{scpcName}) })

		It("refuses before creating anything and recovers once the SCPC declares it", func() {
			Expect(k8sClient.Create(ctx, newSCPC(scpcName, ""))).To(Succeed())
			Expect(k8sClient.Create(ctx, newSC(scName, scpcName, openBaoEncryption()))).To(Succeed())
			reconcileTwice(scName)

			sc := getSC(scName)
			Expect(apierrors.IsNotFound(configMapErr(scName))).To(BeTrue())
			Expect(sc.Status.Phase).To(Equal(showroomv1alpha1.PhasePending))
			Expect(sc.Status.ProviderConfigRef).To(BeNil())
			errCond := apimeta.FindStatusCondition(sc.Status.Conditions, showroomv1alpha1.ConditionError)
			Expect(errCond).NotTo(BeNil())
			Expect(errCond.Reason).To(Equal("EncryptionUnsupported"))
			encCond := apimeta.FindStatusCondition(sc.Status.Conditions, showroomv1alpha1.ConditionEncryptionReady)
			Expect(encCond).NotTo(BeNil())
			Expect(encCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(encCond.Reason).To(Equal("UnsupportedByProvider"))

			By("declaring OpenBao on the SCPC")
			scpc := &showroomv1alpha1.SimpleClusterProviderConfig{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: scpcName, Namespace: "default"}, scpc)).To(Succeed())
			scpc.Spec.EncryptionProviders = []showroomv1alpha1.EncryptionProviderType{showroomv1alpha1.EncryptionProviderOpenBao}
			Expect(k8sClient.Update(ctx, scpc)).To(Succeed())
			reconcileTwice(scName)

			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: scName + "-config", Namespace: "default"}, cm)).To(Succeed())
			Expect(cm.Data["encryption"]).To(Equal("true/OpenBao"))
			sc = getSC(scName)
			Expect(sc.Status.Phase).To(Equal(showroomv1alpha1.PhaseReady))
			Expect(apimeta.FindStatusCondition(sc.Status.Conditions, showroomv1alpha1.ConditionError)).To(BeNil())
			encCond = apimeta.FindStatusCondition(sc.Status.Conditions, showroomv1alpha1.ConditionEncryptionReady)
			Expect(encCond).NotTo(BeNil())
			Expect(encCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(encCond.Reason).To(Equal("Encrypted"))
		})
	})

	Context("with a template the target API does not fully accept", func() {
		const scpcName = "enc-outdated"
		const encrypted, plain = "enc-strict", "enc-plain"
		AfterEach(func() { cleanup([]string{encrypted, plain}, []string{scpcName}) })

		It("fails visibly for an encrypted cluster and leaves unencrypted ones unchanged", func() {
			Expect(k8sClient.Create(ctx, newSCPC(scpcName,
				"spec:\n  encryption: \"{{ .SC.encryption.provider }}\"\n",
				showroomv1alpha1.EncryptionProviderOpenBao))).To(Succeed())

			Expect(k8sClient.Create(ctx, newSC(encrypted, scpcName, openBaoEncryption()))).To(Succeed())
			reconcileTwice(encrypted)
			Expect(apierrors.IsNotFound(configMapErr(encrypted))).To(BeTrue())
			sc := getSC(encrypted)
			errCond := apimeta.FindStatusCondition(sc.Status.Conditions, showroomv1alpha1.ConditionError)
			Expect(errCond).NotTo(BeNil())
			Expect(errCond.Reason).To(Equal("CreateError"))
			Expect(errCond.Message).To(ContainSubstring(`unknown field "spec"`))

			By("pruning the field as before when encryption is not requested")
			Expect(k8sClient.Create(ctx, newSC(plain, scpcName, nil))).To(Succeed())
			reconcileTwice(plain)
			Expect(configMapErr(plain)).To(Succeed())
			sc = getSC(plain)
			Expect(sc.Status.Phase).To(Equal(showroomv1alpha1.PhaseReady))
			Expect(apimeta.FindStatusCondition(sc.Status.Conditions, showroomv1alpha1.ConditionError)).To(BeNil())
			Expect(apimeta.FindStatusCondition(sc.Status.Conditions, showroomv1alpha1.ConditionEncryptionReady)).To(BeNil())
		})
	})
})
