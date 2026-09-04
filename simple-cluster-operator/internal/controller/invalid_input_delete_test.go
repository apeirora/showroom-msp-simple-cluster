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
	rendertpl "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/template"
)

var _ = Describe("Invalid-input deletion", func() {
	DescribeTable("preserves cleanup safety when inputs become invalid", func(created, recorded, inputDependent, foreign bool) {
		ctx := context.Background()
		scpc := &showroomv1alpha1.SimpleClusterProviderConfig{ObjectMeta: metav1.ObjectMeta{Name: "invalid-delete-provider", Namespace: "default"}, Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{
			Target: showroomv1alpha1.TargetSpec{Namespace: "{{ .SC.namespace }}"},
			Inputs: []showroomv1alpha1.InputDeclaration{
				{Name: "region", Type: showroomv1alpha1.InputTypeEnum, Default: "mdb", Values: []showroomv1alpha1.InputEnumValue{{Key: "mdb"}}},
				{Name: "targetNamespace", Type: showroomv1alpha1.InputTypeEnum, Default: "default", Values: []showroomv1alpha1.InputEnumValue{{Key: "default"}}},
			},
			Resources: []showroomv1alpha1.ResourceSpec{{Name: "claim", APIVersion: "v1", Kind: "ConfigMap",
				NameTemplate: `{{ printf "%s/%s" .SC.namespace .SC.name | sha256sum | trunc 20 | printf "sc-%s" }}`,
				Template:     "apiVersion: v1\nkind: ConfigMap\ndata:\n  region: '{{ .Inputs.region }}'\n",
			}},
			KubeconfigSource: showroomv1alpha1.KubeconfigSource{ResourceName: "claim", Inline: &showroomv1alpha1.KubeconfigInlineSource{KubeconfigPath: "data.kubeconfig"}},
		}}
		if inputDependent {
			scpc.Spec.Target.Namespace = "{{ .Inputs.targetNamespace }}"
		}
		Expect(k8sClient.Create(ctx, scpc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, scpc) })
		sc := &showroomv1alpha1.SimpleCluster{ObjectMeta: metav1.ObjectMeta{Name: "invalid-delete-cluster", Namespace: "default"}, Spec: showroomv1alpha1.SimpleClusterSpec{
			ProviderConfigRefs: []string{scpc.Name}, Workers: showroomv1alpha1.WorkersSpec{Nodes: ptr.To[int32](1)},
		}}
		Expect(k8sClient.Create(ctx, sc)).To(Succeed())
		scKey := client.ObjectKeyFromObject(sc)
		DeferCleanup(func() {
			if err := k8sClient.Get(ctx, scKey, sc); err == nil {
				sc.Finalizers = nil
				_ = k8sClient.Update(ctx, sc)
				_ = k8sClient.Delete(ctx, sc)
			}
		})
		targetName, targetNamespace, err := rendertpl.ResolveTarget(scpc, sc, &scpc.Spec.Resources[0], 0)
		Expect(err).NotTo(HaveOccurred())
		targetKey := types.NamespacedName{Name: targetName, Namespace: targetNamespace}
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: targetName, Namespace: targetNamespace}})
		})
		r := &SimpleClusterReconciler{Client: k8sClient, APIReader: k8sClient, Scheme: k8sClient.Scheme(), RESTConfig: cfg}
		req := reconcile.Request{NamespacedName: scKey}
		if created {
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, targetKey, &corev1.ConfigMap{})).To(Succeed())
			Expect(k8sClient.Get(ctx, scKey, sc)).To(Succeed())
			Expect(sc.Status.Resources).NotTo(BeEmpty())
			if !recorded {
				// Models a successful target Create whose identities never reached status.
				base := sc.DeepCopy()
				sc.Status.Resources = nil
				Expect(k8sClient.Status().Patch(ctx, sc, client.MergeFrom(base))).To(Succeed())
			}
		}
		Expect(k8sClient.Get(ctx, scKey, sc)).To(Succeed())
		sc.Spec.Inputs = map[string]string{"region": "not-supported"}
		if inputDependent {
			sc.Spec.Inputs["targetNamespace"] = "not-supported"
		}
		Expect(k8sClient.Update(ctx, sc)).To(Succeed())
		_, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, scKey, sc)).To(Succeed())
		Expect(apimeta.FindStatusCondition(sc.Status.Conditions, showroomv1alpha1.ConditionError).Reason).To(Equal("InputError"))
		if foreign {
			Expect(k8sClient.Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: targetName, Namespace: targetNamespace}})).To(Succeed())
		}
		Expect(k8sClient.Delete(ctx, sc)).To(Succeed())
		for range 3 {
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
		}
		if inputDependent || foreign {
			Expect(k8sClient.Get(ctx, scKey, sc)).To(Succeed())
			Expect(sc.Finalizers).To(ContainElement(showroomv1alpha1.FinalizerName))
			Expect(sc.Status.Phase).To(Equal(showroomv1alpha1.PhaseDeleting))
			Expect(apimeta.FindStatusCondition(sc.Status.Conditions, showroomv1alpha1.ConditionError).Reason).To(Equal("CleanupBlocked"))
		} else {
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, scKey, sc))).To(BeTrue(), "deterministic cleanup did not release the finalizer")
		}
		if foreign || (inputDependent && !recorded) {
			target := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, targetKey, target)).To(Succeed())
			Expect(target.Annotations).NotTo(HaveKey(rendertpl.AnnoAllowDelete))
		} else {
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, targetKey, &corev1.ConfigMap{}))).To(BeTrue(), "owned target was not cleaned")
		}
	},
		Entry("fresh invalid request with no target", false, false, false, false),
		Entry("existing target edited invalid with recorded identity", true, true, false, false),
		Entry("created target missing status then inputs edited invalid", true, false, false, false),
		Entry("input-dependent namespace cleans recorded target but holds finalizer", true, true, true, false),
		Entry("input-dependent namespace without journal fails closed", true, false, true, false),
		Entry("foreign deterministic target must not be deleted", false, false, false, true),
	)
})
