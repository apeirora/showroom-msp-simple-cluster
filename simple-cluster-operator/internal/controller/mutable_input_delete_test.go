package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	rendertpl "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/template"
)

var _ = Describe("Mutable-field invalid-input deletion", func() {
	DescribeTable("does not confuse current SC fields with historical target identities", func(repeated, recorded bool) {
		ctx := context.Background()
		scpc := &showroomv1alpha1.SimpleClusterProviderConfig{ObjectMeta: metav1.ObjectMeta{Name: "mutable-delete-provider", Namespace: "default"}, Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{
			Target: showroomv1alpha1.TargetSpec{Namespace: "{{ .SC.namespace }}"},
			Inputs: []showroomv1alpha1.InputDeclaration{
				{Name: "region", Type: showroomv1alpha1.InputTypeEnum, Default: "mdb", Values: []showroomv1alpha1.InputEnumValue{{Key: "mdb"}}},
			},
			Resources: []showroomv1alpha1.ResourceSpec{{Name: "claim", APIVersion: "v1", Kind: "ConfigMap",
				NameTemplate: "{{ printf \"%s-%d\" .SC.name .SC.workers.nodes }}",
				Template:     "apiVersion: v1\nkind: ConfigMap\ndata:\n  region: '{{ .Inputs.region }}'\n",
			}},
			KubeconfigSource: showroomv1alpha1.KubeconfigSource{ResourceName: "claim", Inline: &showroomv1alpha1.KubeconfigInlineSource{KubeconfigPath: "data.kubeconfig"}},
		}}
		if repeated {
			scpc.Spec.Resources[0].NameTemplate = "{{ .SC.name }}"
			scpc.Spec.Resources[0].Repeat = "{{ .SC.workers.nodes }}"
		}
		Expect(k8sClient.Create(ctx, scpc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, scpc) })
		sc := &showroomv1alpha1.SimpleCluster{ObjectMeta: metav1.ObjectMeta{Name: "mutable-delete-cluster", Namespace: "default"}, Spec: showroomv1alpha1.SimpleClusterSpec{
			ProviderConfigRefs: []string{scpc.Name}, Workers: showroomv1alpha1.WorkersSpec{Nodes: ptr.To[int32](2)},
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
		targets, err := rendertpl.Render(scpc, sc, &scpc.Spec.Resources[0], nil)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			for _, target := range targets {
				_ = k8sClient.Delete(ctx, target)
			}
		})
		r := &SimpleClusterReconciler{Client: k8sClient, APIReader: k8sClient, Scheme: k8sClient.Scheme(), RESTConfig: cfg}
		req := reconcile.Request{NamespacedName: scKey}
		_, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		for _, target := range targets {
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(target), &corev1.ConfigMap{})).To(Succeed())
		}
		Expect(k8sClient.Get(ctx, scKey, sc)).To(Succeed())
		Expect(sc.Status.Resources).NotTo(BeEmpty())
		if !recorded {
			// Models successful target Create followed by loss of its status journal.
			base := sc.DeepCopy()
			sc.Status.Resources = nil
			Expect(k8sClient.Status().Patch(ctx, sc, client.MergeFrom(base))).To(Succeed())
		}
		sc.Spec.Workers.Nodes = ptr.To[int32](1)
		sc.Spec.Inputs = map[string]string{"region": "not-supported"}
		Expect(k8sClient.Update(ctx, sc)).To(Succeed())
		_, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, scKey, sc)).To(Succeed())
		Expect(apimeta.FindStatusCondition(sc.Status.Conditions, showroomv1alpha1.ConditionError).Reason).To(Equal("InputError"))
		Expect(k8sClient.Delete(ctx, sc)).To(Succeed())
		for range 3 {
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
		}
		for _, target := range targets {
			readErr := k8sClient.Get(ctx, client.ObjectKeyFromObject(target), &corev1.ConfigMap{})
			GinkgoWriter.Printf("historical target %s/%s after delete: %v\n", target.GetNamespace(), target.GetName(), readErr)
		}
		Expect(k8sClient.Get(ctx, scKey, sc)).To(Succeed(), "mutable current identity incorrectly released the finalizer")
		Expect(sc.Finalizers).To(ContainElement(showroomv1alpha1.FinalizerName))
		Expect(sc.Status.Phase).To(Equal(showroomv1alpha1.PhaseDeleting))
		Expect(apimeta.FindStatusCondition(sc.Status.Conditions, showroomv1alpha1.ConditionError).Reason).To(Equal("CleanupBlocked"))
		for _, target := range targets {
			actual := &corev1.ConfigMap{}
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(target), actual)
			if recorded {
				Expect(apierrors.IsNotFound(err)).To(BeTrue(), "recorded owned target was not cleaned")
			} else {
				Expect(err).NotTo(HaveOccurred(), "unproven target identity was altered")
				Expect(actual.Annotations).NotTo(HaveKey(rendertpl.AnnoAllowDelete))
			}
		}
	},
		Entry("changed worker-based name without journal", false, false),
		Entry("changed worker-based name cleans journal but holds finalizer", false, true),
		Entry("reduced worker-based repeat without journal", true, false),
		Entry("reduced worker-based repeat cleans journal but holds finalizer", true, true),
	)
})
