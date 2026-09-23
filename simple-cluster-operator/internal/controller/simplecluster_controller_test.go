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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	rendertpl "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/template"
)

var _ = Describe("SimpleCluster Controller", func() {
	Context("when the referenced SCPC is missing", func() {
		const scName = "missing-scpc-ref"
		ctx := context.Background()
		key := types.NamespacedName{Name: scName, Namespace: "default"}

		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, &showroomv1alpha1.SimpleCluster{
				ObjectMeta: metav1.ObjectMeta{Name: scName, Namespace: "default"},
				Spec: showroomv1alpha1.SimpleClusterSpec{
					Workers:            showroomv1alpha1.WorkersSpec{Nodes: ptr.To[int32](1)},
					ProviderConfigRefs: []string{"ghost"},
				},
			})).To(Succeed())
		})

		AfterEach(func() {
			sc := &showroomv1alpha1.SimpleCluster{}
			if err := k8sClient.Get(ctx, key, sc); err == nil {
				sc.Finalizers = nil
				_ = k8sClient.Update(ctx, sc)
				_ = k8sClient.Delete(ctx, sc)
			}
		})

		It("records ConditionError(SCPCNotFound) and Phase=Pending", func() {
			r := &SimpleClusterReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				RESTConfig: cfg,
			}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			updated := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, key, updated)).To(Succeed())

			errCond := apimeta.FindStatusCondition(updated.Status.Conditions, showroomv1alpha1.ConditionError)
			Expect(errCond).NotTo(BeNil())
			Expect(errCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(errCond.Reason).To(Equal("SCPCNotFound"))
			Expect(updated.Status.Phase).To(Equal(showroomv1alpha1.PhasePending))
		})
	})

	Context("when a from=profile input has no fitting profile", func() {
		const scpcName = "tiny-profiles"
		const scName = "too-big"
		ctx := context.Background()
		scKey := types.NamespacedName{Name: scName, Namespace: "default"}

		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, &showroomv1alpha1.SimpleClusterProviderConfig{
				ObjectMeta: metav1.ObjectMeta{Name: scpcName, Namespace: "default"},
				Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{
					Target: showroomv1alpha1.TargetSpec{Namespace: "default"},
					Inputs: []showroomv1alpha1.InputDeclaration{
						{Name: "machineType", From: showroomv1alpha1.InputSourceProfile},
					},
					Profiles: []showroomv1alpha1.Profile{
						{Name: "small", CPU: resource.MustParse("2"), Memory: resource.MustParse("8Gi")},
					},
					Resources: []showroomv1alpha1.ResourceSpec{{
						Name:       "config",
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Template: `apiVersion: v1
kind: ConfigMap
metadata:
  name: ignored
data:
  machineType: "{{ .Inputs.machineType }}"
`,
					}},
					KubeconfigSource: showroomv1alpha1.KubeconfigSource{
						ResourceName: "config",
						Inline:       &showroomv1alpha1.KubeconfigInlineSource{KubeconfigPath: "data.kubeconfig"},
					},
				},
			})).To(Succeed())

			Expect(k8sClient.Create(ctx, &showroomv1alpha1.SimpleCluster{
				ObjectMeta: metav1.ObjectMeta{Name: scName, Namespace: "default"},
				Spec: showroomv1alpha1.SimpleClusterSpec{
					Workers: showroomv1alpha1.WorkersSpec{
						Nodes: ptr.To[int32](1),
						Requested: showroomv1alpha1.NodeResources{
							CPU:    resource.MustParse("16"),
							Memory: resource.MustParse("8Gi"),
						},
					},
					ProviderConfigRefs: []string{scpcName},
				},
			})).To(Succeed())
		})

		AfterEach(func() {
			sc := &showroomv1alpha1.SimpleCluster{}
			if err := k8sClient.Get(ctx, scKey, sc); err == nil {
				sc.Finalizers = nil
				_ = k8sClient.Update(ctx, sc)
				_ = k8sClient.Delete(ctx, sc)
			}
			scpc := &showroomv1alpha1.SimpleClusterProviderConfig{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: scpcName, Namespace: "default"}, scpc); err == nil {
				_ = k8sClient.Delete(ctx, scpc)
			}
		})

		It("parks the SC in Phase=NoHardware and creates no resource", func() {
			r := &SimpleClusterReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				RESTConfig: cfg,
			}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())

			updated := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, updated)).To(Succeed())
			Expect(updated.Status.Phase).To(Equal(showroomv1alpha1.PhaseNoHardware))
			Expect(updated.Status.Resources).To(BeEmpty())

			cm := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "too-big-config", Namespace: "default"}, cm)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})
	})

	Context("with a valid SCPC creating a ConfigMap on the local cluster", func() {
		const scpcName = "local-configmap"
		const scName = "demo"
		ctx := context.Background()
		scKey := types.NamespacedName{Name: scName, Namespace: "default"}

		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, &showroomv1alpha1.SimpleClusterProviderConfig{
				ObjectMeta: metav1.ObjectMeta{Name: scpcName, Namespace: "default"},
				Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{
					Target: showroomv1alpha1.TargetSpec{Namespace: "default"},
					Resources: []showroomv1alpha1.ResourceSpec{{
						Name:       "config",
						APIVersion: "v1",
						Kind:       "ConfigMap",
						// kind is always "ConfigMap", so Ready is always true:
						// good fake-readiness signal for ConfigMap (which has no status).
						Readiness: &showroomv1alpha1.ReadinessCheck{
							PhasePath:       "kind",
							PhaseReadyValue: "ConfigMap",
						},
						Template: `apiVersion: v1
kind: ConfigMap
metadata:
  name: ignored
data:
  workers: "{{ .SC.workers.nodes }}"
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
			})).To(Succeed())

			Expect(k8sClient.Create(ctx, &showroomv1alpha1.SimpleCluster{
				ObjectMeta: metav1.ObjectMeta{Name: scName, Namespace: "default"},
				Spec: showroomv1alpha1.SimpleClusterSpec{
					Workers:            showroomv1alpha1.WorkersSpec{Nodes: ptr.To[int32](4)},
					ProviderConfigRefs: []string{scpcName},
				},
			})).To(Succeed())
		})

		AfterEach(func() {
			sc := &showroomv1alpha1.SimpleCluster{}
			if err := k8sClient.Get(ctx, scKey, sc); err == nil {
				sc.Finalizers = nil
				_ = k8sClient.Update(ctx, sc)
				_ = k8sClient.Delete(ctx, sc)
			}
			scpc := &showroomv1alpha1.SimpleClusterProviderConfig{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: scpcName, Namespace: "default"}, scpc); err == nil {
				_ = k8sClient.Delete(ctx, scpc)
			}
			cm := &corev1.ConfigMap{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "demo-config", Namespace: "default"}, cm); err == nil {
				_ = k8sClient.Delete(ctx, cm)
			}
			secret := &corev1.Secret{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "demo-kubeconfig", Namespace: "default"}, secret); err == nil {
				_ = k8sClient.Delete(ctx, secret)
			}
		})

		It("creates the target ConfigMap and mirrors a kubeconfig Secret", func() {
			r := &SimpleClusterReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				RESTConfig: cfg,
			}

			// Reconcile twice: the first call adds the finalizer (and returns
			// before doing anything else); the second walks the DAG.
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())

			By("creating the target ConfigMap on the local cluster")
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "demo-config", Namespace: "default",
			}, cm)).To(Succeed())
			Expect(cm.Data).To(HaveKeyWithValue("workers", "4"))

			By("recording per-resource status and aggregate Phase=Ready on the SC")
			updated := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, updated)).To(Succeed())
			Expect(updated.Status.Phase).To(Equal(showroomv1alpha1.PhaseReady))
			Expect(updated.Status.Resources).To(HaveLen(1))
			Expect(updated.Status.Resources[0].Name).To(Equal("config"))
			Expect(updated.Status.Resources[0].Phase).To(Equal(showroomv1alpha1.PhaseReady))
			Expect(updated.Status.Resources[0].Reason).To(Equal(showroomv1alpha1.ReasonReady))
			Expect(updated.Status.Resources[0].TargetName).To(Equal("demo-config"))

			By("setting ConditionReady=True and clearing any Error condition")
			ready := apimeta.FindStatusCondition(updated.Status.Conditions, showroomv1alpha1.ConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))
			Expect(apimeta.FindStatusCondition(updated.Status.Conditions, showroomv1alpha1.ConditionError)).To(BeNil())

			By("mirroring the kubeconfig into a Secret in SC.Namespace")
			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "demo-kubeconfig", Namespace: "default",
			}, secret)).To(Succeed())
			Expect(secret.Data).To(HaveKey("kubeconfig"))
			Expect(secret.Labels).To(HaveKeyWithValue("apeirora.eu/kubeconfig", "true"))
			Expect(secret.OwnerReferences).NotTo(BeEmpty())
			Expect(secret.OwnerReferences[0].Kind).To(Equal("SimpleCluster"))
			Expect(updated.Status.KubeconfigSecretRef).NotTo(BeNil())
			Expect(updated.Status.KubeconfigSecretRef.Name).To(Equal("demo-kubeconfig"))

			kcCond := apimeta.FindStatusCondition(updated.Status.Conditions, showroomv1alpha1.ConditionKubeconfigValid)
			Expect(kcCond).NotTo(BeNil())
			Expect(kcCond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("sets Phase=Deleting when the SC deletion is requested", func() {
			r := &SimpleClusterReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				RESTConfig: cfg,
			}

			By("reconciling the SC to a steady state with the finalizer in place")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())

			By("pinning the target ConfigMap with a finalizer so the cascade barrier holds")
			cmKey := types.NamespacedName{Name: "demo-config", Namespace: "default"}
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, cmKey, cm)).To(Succeed())
			cm.Finalizers = append(cm.Finalizers, "test.showroom/hold")
			Expect(k8sClient.Update(ctx, cm)).To(Succeed())
			DeferCleanup(func() {
				held := &corev1.ConfigMap{}
				if err := k8sClient.Get(ctx, cmKey, held); err == nil {
					held.Finalizers = nil
					_ = k8sClient.Update(ctx, held)
				}
			})

			By("requesting deletion of the SC")
			sc := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, sc)).To(Succeed())
			Expect(k8sClient.Delete(ctx, sc)).To(Succeed())

			By("reconciling once and observing Phase=Deleting while the cascade is in flight")
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())

			deleting := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, deleting)).To(Succeed())
			Expect(deleting.DeletionTimestamp).NotTo(BeNil())
			Expect(deleting.Status.Phase).To(Equal(showroomv1alpha1.PhaseDeleting))

			By("releasing the child from the delete guard before asking for its deletion")
			held := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, cmKey, held)).To(Succeed())
			Expect(held.Annotations).To(HaveKeyWithValue(rendertpl.AnnoAllowDelete, "true"))
			Expect(held.DeletionTimestamp).NotTo(BeNil())
		})

		It("creates no target resource when the SC is being deleted mid-pass", func() {
			// APIReader reports the SC as deleting while r.Client still sees it
			// alive, mimicking a delete that lands after the pass began reading.
			// The guard must block the create; demo-config must never appear.
			base, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
			Expect(err).NotTo(HaveOccurred())
			apiReader := interceptor.NewClient(base, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey,
					obj client.Object, opts ...client.GetOption) error {
					if err := c.Get(ctx, key, obj, opts...); err != nil {
						return err
					}
					if sc, ok := obj.(*showroomv1alpha1.SimpleCluster); ok && key.Name == scName {
						sc.DeletionTimestamp = &metav1.Time{Time: time.Now()}
					}
					return nil
				},
			})

			r := &SimpleClusterReconciler{
				Client:     k8sClient,
				APIReader:  apiReader,
				Scheme:     k8sClient.Scheme(),
				RESTConfig: cfg,
			}
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())

			cm := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "demo-config", Namespace: "default"}, cm)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("marks dependents WaitingForDependencies until predecessors are Ready", func() {
			// Make the existing `config` resource NOT ready (drop the
			// readiness override — ConfigMaps have no Ready condition, so
			// the default walker returns Pending). Add a `dependent` that
			// dependsOn `config`. We expect dependent to stay
			// WaitingForDependencies with config in its message.
			scpc := &showroomv1alpha1.SimpleClusterProviderConfig{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: scpcName, Namespace: "default"}, scpc)).To(Succeed())
			scpc.Spec.Resources[0].Readiness = nil // config is now never Ready
			scpc.Spec.Resources = append(scpc.Spec.Resources, showroomv1alpha1.ResourceSpec{
				Name:       "dependent",
				APIVersion: "v1",
				Kind:       "ConfigMap",
				DependsOn:  []string{"config"},
				Template:   "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: x}\n",
			})
			Expect(k8sClient.Update(ctx, scpc)).To(Succeed())

			r := &SimpleClusterReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				RESTConfig: cfg,
			}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())

			updated := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, updated)).To(Succeed())
			Expect(updated.Status.Resources).To(HaveLen(2))

			byName := map[string]showroomv1alpha1.ResourceStatus{}
			for _, rs := range updated.Status.Resources {
				byName[rs.Name] = rs
			}
			// config exists but reports no Ready condition → Creating.
			Expect(byName["config"].Phase).To(Equal(showroomv1alpha1.PhasePending))
			Expect(byName["config"].Reason).To(Equal(showroomv1alpha1.ReasonCreating))
			// dependent never got rendered: blocked on config.
			Expect(byName["dependent"].Phase).To(Equal(showroomv1alpha1.PhasePending))
			Expect(byName["dependent"].Reason).To(Equal(showroomv1alpha1.ReasonWaitingForDependencies))
			Expect(byName["dependent"].Message).To(ContainSubstring("waiting on: config"))
		})

		It("propagates SCPC DAG validation failures into ConditionError", func() {
			// Mutate the SCPC into an invalid state (self-reference) and
			// re-reconcile; the SC must surface the error.
			scpc := &showroomv1alpha1.SimpleClusterProviderConfig{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: scpcName, Namespace: "default"}, scpc)).To(Succeed())
			scpc.Spec.Resources[0].DependsOn = []string{"config"} // self-reference
			Expect(k8sClient.Update(ctx, scpc)).To(Succeed())

			r := &SimpleClusterReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				RESTConfig: cfg,
			}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())

			updated := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, updated)).To(Succeed())
			errCond := apimeta.FindStatusCondition(updated.Status.Conditions, showroomv1alpha1.ConditionError)
			Expect(errCond).NotTo(BeNil())
			Expect(errCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(errCond.Reason).To(Equal("InvalidSCPC"))
			Expect(errCond.Message).To(ContainSubstring("self-reference"))
		})
	})
})

// silence linter about unused apierrors import when narrowing tests.
var _ = apierrors.IsNotFound
