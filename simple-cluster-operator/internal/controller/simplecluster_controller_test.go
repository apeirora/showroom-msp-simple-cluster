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

			By("mirroring the kubeconfig onto status so the portal detail view can show it")
			Expect(updated.Status.Kubeconfig).NotTo(BeEmpty())
			Expect(updated.Status.Kubeconfig).To(Equal(string(secret.Data["kubeconfig"])))

			kcCond := apimeta.FindStatusCondition(updated.Status.Conditions, showroomv1alpha1.ConditionKubeconfigValid)
			Expect(kcCond).NotTo(BeNil())
			Expect(kcCond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("blocks a new target when the SCPC is explicitly retired", func() {
			scpc := &showroomv1alpha1.SimpleClusterProviderConfig{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: scpcName, Namespace: "default",
			}, scpc)).To(Succeed())
			scpc.Spec.ProvisioningEnabled = ptr.To(false)
			Expect(k8sClient.Update(ctx, scpc)).To(Succeed())

			r := &SimpleClusterReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				RESTConfig: cfg,
			}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())

			cm := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: "demo-config", Namespace: "default",
			}, cm)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			updated := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, updated)).To(Succeed())
			Expect(updated.Status.Phase).To(Equal(showroomv1alpha1.PhaseFailed))
			Expect(updated.Status.ObservedGeneration).To(Equal(updated.Generation))
			Expect(updated.Status.ProviderConfigRef).NotTo(BeNil())
			Expect(updated.Status.ProviderConfigRef.Name).To(Equal(scpcName))
			ready := apimeta.FindStatusCondition(updated.Status.Conditions, showroomv1alpha1.ConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal("ProvisioningDisabled"))
			Expect(updated.Status.Resources).To(HaveLen(1))
			Expect(updated.Status.Resources[0].Name).To(Equal("config"))
			Expect(updated.Status.Resources[0].Phase).To(Equal(showroomv1alpha1.PhaseFailed))
			Expect(updated.Status.Resources[0].Reason).To(Equal(showroomv1alpha1.ReasonFailed))
			Expect(updated.Status.Resources[0].TargetName).To(BeEmpty())
			errCond := apimeta.FindStatusCondition(updated.Status.Conditions, showroomv1alpha1.ConditionError)
			Expect(errCond).NotTo(BeNil())
			Expect(errCond.Reason).To(Equal("ProvisioningDisabled"))
			Expect(errCond.Message).To(ContainSubstring("retired for new provisioning"))
		})

		It("clears stale readiness when a retired provider cannot recreate a missing target", func() {
			r := &SimpleClusterReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				RESTConfig: cfg,
			}

			By("first reaching Ready with the target present")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())
			readySC := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, readySC)).To(Succeed())
			Expect(readySC.Status.Phase).To(Equal(showroomv1alpha1.PhaseReady))

			By("retiring the provider and removing the target")
			scpc := &showroomv1alpha1.SimpleClusterProviderConfig{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: scpcName, Namespace: "default"}, scpc)).To(Succeed())
			scpc.Spec.ProvisioningEnabled = ptr.To(false)
			Expect(k8sClient.Update(ctx, scpc)).To(Succeed())
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "demo-config", Namespace: "default"}, cm)).To(Succeed())
			Expect(k8sClient.Delete(ctx, cm)).To(Succeed())

			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())

			By("reporting the unavailable cluster instead of preserving Ready=True")
			updated := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, updated)).To(Succeed())
			Expect(updated.Status.Phase).To(Equal(showroomv1alpha1.PhaseFailed))
			ready := apimeta.FindStatusCondition(updated.Status.Conditions, showroomv1alpha1.ConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal("ProvisioningDisabled"))
			Expect(updated.Status.Resources).To(HaveLen(1))
			Expect(updated.Status.Resources[0].Phase).To(Equal(showroomv1alpha1.PhaseFailed))
			Expect(updated.Status.Resources[0].Reason).To(Equal(showroomv1alpha1.ReasonFailed))
			Expect(updated.Status.Resources[0].LastError).To(ContainSubstring("retired for new provisioning"))
			errCond := apimeta.FindStatusCondition(updated.Status.Conditions, showroomv1alpha1.ConditionError)
			Expect(errCond).NotTo(BeNil())
			Expect(errCond.Reason).To(Equal("ProvisioningDisabled"))
		})

		It("retains every repeated target identity across a partial-create retirement failure", func() {
			By("configuring two deterministic target instances")
			scpc := &showroomv1alpha1.SimpleClusterProviderConfig{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: scpcName, Namespace: "default"}, scpc)).To(Succeed())
			scpc.Spec.Resources[0].Repeat = "2"
			Expect(k8sClient.Update(ctx, scpc)).To(Succeed())

			By("simulating a create of index zero followed by a process crash")
			sc := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, sc)).To(Succeed())
			sc.Finalizers = append(sc.Finalizers, showroomv1alpha1.FinalizerName)
			Expect(k8sClient.Update(ctx, sc)).To(Succeed())
			rendered, err := rendertpl.Render(scpc, sc, &scpc.Spec.Resources[0], nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(rendered).To(HaveLen(2))
			rendered[0].SetFinalizers([]string{"test.showroom/hold"})
			Expect(k8sClient.Create(ctx, rendered[0])).To(Succeed())
			createdKey := types.NamespacedName{Name: rendered[0].GetName(), Namespace: rendered[0].GetNamespace()}
			DeferCleanup(func() {
				held := &corev1.ConfigMap{}
				if err := k8sClient.Get(ctx, createdKey, held); err == nil {
					held.Finalizers = nil
					_ = k8sClient.Update(ctx, held)
					_ = k8sClient.Delete(ctx, held)
				}
			})

			By("retiring the provider before the missing second instance can be created")
			scpc.Spec.ProvisioningEnabled = ptr.To(false)
			Expect(k8sClient.Update(ctx, scpc)).To(Succeed())
			r := &SimpleClusterReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				RESTConfig: cfg,
			}
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())

			By("persisting the observed identity without adopting the absent one")
			failed := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, failed)).To(Succeed())
			Expect(failed.Status.Phase).To(Equal(showroomv1alpha1.PhaseFailed))
			Expect(failed.Status.ProviderConfigRef).NotTo(BeNil())
			Expect(failed.Status.ProviderConfigRef.Name).To(Equal(scpcName))
			Expect(failed.Status.Resources).To(HaveLen(1))
			Expect(failed.Status.Resources[0].Instances).To(HaveLen(1))
			Expect(failed.Status.Resources[0].Instances[0].TargetName).To(Equal(rendered[0].GetName()))

			By("deleting the pre-crash target instead of releasing the SC finalizer and leaking it")
			Expect(k8sClient.Delete(ctx, failed)).To(Succeed())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())
			held := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, createdKey, held)).To(Succeed())
			Expect(held.Annotations).To(HaveKeyWithValue(rendertpl.AnnoAllowDelete, "true"))
			Expect(held.DeletionTimestamp).NotTo(BeNil())
		})

		It("marks a previously observed repeated instance failed when it disappears after retirement", func() {
			scpc := &showroomv1alpha1.SimpleClusterProviderConfig{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: scpcName, Namespace: "default"}, scpc)).To(Succeed())
			scpc.Spec.Resources[0].Repeat = "2"
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

			ready := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, ready)).To(Succeed())
			Expect(ready.Status.Resources).To(HaveLen(1))
			Expect(ready.Status.Resources[0].Instances).To(HaveLen(2))
			rendered, err := rendertpl.Render(scpc, ready, &scpc.Spec.Resources[0], nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(rendered).To(HaveLen(2))
			firstKey := types.NamespacedName{Name: rendered[0].GetName(), Namespace: rendered[0].GetNamespace()}
			secondKey := types.NamespacedName{Name: rendered[1].GetName(), Namespace: rendered[1].GetNamespace()}
			DeferCleanup(func() {
				first := &corev1.ConfigMap{}
				if err := k8sClient.Get(ctx, firstKey, first); err == nil {
					_ = k8sClient.Delete(ctx, first)
				}
			})

			scpc.Spec.ProvisioningEnabled = ptr.To(false)
			Expect(k8sClient.Update(ctx, scpc)).To(Succeed())
			second := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, secondKey, second)).To(Succeed())
			Expect(k8sClient.Delete(ctx, second)).To(Succeed())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())

			failed := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, failed)).To(Succeed())
			Expect(failed.Status.Phase).To(Equal(showroomv1alpha1.PhaseFailed))
			Expect(failed.Status.Resources[0].Instances).To(HaveLen(2))
			Expect(failed.Status.Resources[0].Instances[0].Phase).To(Equal(showroomv1alpha1.PhaseReady))
			Expect(failed.Status.Resources[0].Instances[1].Phase).To(Equal(showroomv1alpha1.PhaseFailed))
			Expect(failed.Status.Resources[0].Instances[1].LastError).To(ContainSubstring("retired for new provisioning"))
		})

		It("records later survivors when a retired dynamic repeat has a missing middle instance", func() {
			By("making the repeated count depend on a live predecessor")
			scpc := &showroomv1alpha1.SimpleClusterProviderConfig{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: scpcName, Namespace: "default"}, scpc)).To(Succeed())
			seed := scpc.Spec.Resources[0]
			seed.Name = "seed"
			seed.Template = `apiVersion: v1
kind: ConfigMap
metadata:
  name: ignored
data:
  count: "3"
`
			workers := scpc.Spec.Resources[0]
			workers.Name = "workers"
			workers.DependsOn = []string{"seed"}
			workers.Repeat = `{{ .Resources.seed.data.count }}`
			scpc.Spec.Resources = []showroomv1alpha1.ResourceSpec{seed, workers}
			scpc.Spec.KubeconfigSource.ResourceName = "seed"
			Expect(k8sClient.Update(ctx, scpc)).To(Succeed())

			sc := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, sc)).To(Succeed())
			sc.Finalizers = append(sc.Finalizers, showroomv1alpha1.FinalizerName)
			Expect(k8sClient.Update(ctx, sc)).To(Succeed())

			seedObjects, err := rendertpl.Render(scpc, sc, &scpc.Spec.Resources[0], nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(seedObjects).To(HaveLen(1))
			seedObjects[0].SetFinalizers([]string{"test.showroom/hold"})
			Expect(k8sClient.Create(ctx, seedObjects[0])).To(Succeed())
			predecessors := map[string]rendertpl.Predecessor{
				"seed": {Objects: seedObjects},
			}
			workerObjects, err := rendertpl.Render(scpc, sc, &scpc.Spec.Resources[1], predecessors)
			Expect(err).NotTo(HaveOccurred())
			Expect(workerObjects).To(HaveLen(3))
			for _, index := range []int{0, 2} {
				workerObjects[index].SetFinalizers([]string{"test.showroom/hold"})
				Expect(k8sClient.Create(ctx, workerObjects[index])).To(Succeed())
			}
			allKeys := make([]types.NamespacedName, 0, 3)
			allKeys = append(allKeys, types.NamespacedName{
				Name: seedObjects[0].GetName(), Namespace: seedObjects[0].GetNamespace(),
			})
			for _, index := range []int{0, 2} {
				allKeys = append(allKeys, types.NamespacedName{
					Name: workerObjects[index].GetName(), Namespace: workerObjects[index].GetNamespace(),
				})
			}
			DeferCleanup(func() {
				for _, key := range allKeys {
					held := &corev1.ConfigMap{}
					if err := k8sClient.Get(ctx, key, held); err == nil {
						held.Finalizers = nil
						_ = k8sClient.Update(ctx, held)
						_ = k8sClient.Delete(ctx, held)
					}
				}
			})

			By("retiring and reconciling past the absent middle index")
			scpc.Spec.ProvisioningEnabled = ptr.To(false)
			Expect(k8sClient.Update(ctx, scpc)).To(Succeed())
			r := &SimpleClusterReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				RESTConfig: cfg,
			}
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())

			failed := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, failed)).To(Succeed())
			Expect(failed.Status.Resources).To(HaveLen(2))
			Expect(failed.Status.Resources[0].Name).To(Equal("seed"))
			Expect(failed.Status.Resources[1].Name).To(Equal("workers"))
			Expect(failed.Status.Resources[1].Instances).To(HaveLen(2))
			Expect(failed.Status.Resources[1].Instances[0].Index).To(Equal(int32(0)))
			Expect(failed.Status.Resources[1].Instances[1].Index).To(Equal(int32(2)))

			By("deleting both observed survivors even though repeat cannot render without its predecessor context")
			Expect(k8sClient.Delete(ctx, failed)).To(Succeed())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())
			for _, index := range []int{0, 2} {
				held := &corev1.ConfigMap{}
				key := types.NamespacedName{Name: workerObjects[index].GetName(), Namespace: workerObjects[index].GetNamespace()}
				Expect(k8sClient.Get(ctx, key, held)).To(Succeed())
				Expect(held.Annotations).To(HaveKeyWithValue(rendertpl.AnnoAllowDelete, "true"))
				Expect(held.DeletionTimestamp).NotTo(BeNil())
				held.Finalizers = nil
				Expect(k8sClient.Update(ctx, held)).To(Succeed())
			}

			By("retaining the finalizer because the dynamic repeat cannot prove no unrecorded instance exists")
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())
			blocked := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, blocked)).To(Succeed())
			Expect(blocked.Finalizers).To(ContainElement(showroomv1alpha1.FinalizerName))
			seedHeld := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: seedObjects[0].GetName(), Namespace: seedObjects[0].GetNamespace(),
			}, seedHeld)).To(Succeed())
			Expect(seedHeld.DeletionTimestamp).To(BeNil())
		})

		It("retains earlier dynamic identities when a later retired resource is absent", func() {
			By("creating a dynamic repeated resource and leaving its downstream resource absent")
			scpc := &showroomv1alpha1.SimpleClusterProviderConfig{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: scpcName, Namespace: "default"}, scpc)).To(Succeed())
			seed := scpc.Spec.Resources[0]
			seed.Name = "seed"
			seed.Template = `apiVersion: v1
kind: ConfigMap
metadata:
  name: ignored
data:
  count: "2"
`
			workers := scpc.Spec.Resources[0]
			workers.Name = "workers"
			workers.DependsOn = []string{"seed"}
			workers.Repeat = `{{ .Resources.seed.data.count }}`
			addon := scpc.Spec.Resources[0]
			addon.Name = "addon"
			addon.DependsOn = []string{"workers"}
			scpc.Spec.Resources = []showroomv1alpha1.ResourceSpec{seed, workers, addon}
			scpc.Spec.KubeconfigSource.ResourceName = "seed"
			Expect(k8sClient.Update(ctx, scpc)).To(Succeed())

			sc := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, sc)).To(Succeed())
			sc.Finalizers = append(sc.Finalizers, showroomv1alpha1.FinalizerName)
			Expect(k8sClient.Update(ctx, sc)).To(Succeed())

			seedObjects, err := rendertpl.Render(scpc, sc, &scpc.Spec.Resources[0], nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(seedObjects).To(HaveLen(1))
			seedObjects[0].SetFinalizers([]string{"test.showroom/hold"})
			Expect(k8sClient.Create(ctx, seedObjects[0])).To(Succeed())
			workerObjects, err := rendertpl.Render(scpc, sc, &scpc.Spec.Resources[1], map[string]rendertpl.Predecessor{
				"seed": {Objects: seedObjects},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(workerObjects).To(HaveLen(2))
			for i := range workerObjects {
				workerObjects[i].SetFinalizers([]string{"test.showroom/hold"})
				Expect(k8sClient.Create(ctx, workerObjects[i])).To(Succeed())
			}
			allKeys := make([]types.NamespacedName, 0, 1+len(workerObjects))
			allKeys = append(allKeys, types.NamespacedName{
				Name: seedObjects[0].GetName(), Namespace: seedObjects[0].GetNamespace(),
			})
			for i := range workerObjects {
				allKeys = append(allKeys, types.NamespacedName{
					Name: workerObjects[i].GetName(), Namespace: workerObjects[i].GetNamespace(),
				})
			}
			DeferCleanup(func() {
				for _, key := range allKeys {
					held := &corev1.ConfigMap{}
					if err := k8sClient.Get(ctx, key, held); err == nil {
						held.Finalizers = nil
						_ = k8sClient.Update(ctx, held)
						_ = k8sClient.Delete(ctx, held)
					}
				}
			})

			By("retiring the provider and persisting every resource observed before the missing addon")
			scpc.Spec.ProvisioningEnabled = ptr.To(false)
			Expect(k8sClient.Update(ctx, scpc)).To(Succeed())
			r := &SimpleClusterReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				RESTConfig: cfg,
			}
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())

			failed := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, failed)).To(Succeed())
			statuses := map[string]showroomv1alpha1.ResourceStatus{}
			for _, status := range failed.Status.Resources {
				statuses[status.Name] = status
			}
			Expect(statuses).To(HaveKey("seed"))
			Expect(statuses).To(HaveKey("workers"))
			Expect(statuses).To(HaveKey("addon"))
			Expect(statuses["workers"].Instances).To(HaveLen(2))
			Expect(statuses["addon"].Phase).To(Equal(showroomv1alpha1.PhaseFailed))

			By("using the retained worker identities when dynamic repeat cannot reconstruct during delete")
			Expect(k8sClient.Delete(ctx, failed)).To(Succeed())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())
			for i := range workerObjects {
				held := &corev1.ConfigMap{}
				key := types.NamespacedName{Name: workerObjects[i].GetName(), Namespace: workerObjects[i].GetNamespace()}
				Expect(k8sClient.Get(ctx, key, held)).To(Succeed())
				Expect(held.Annotations).To(HaveKeyWithValue(rendertpl.AnnoAllowDelete, "true"))
				Expect(held.DeletionTimestamp).NotTo(BeNil())
			}
		})

		It("does not delete an unrelated reconstruction-only target", func() {
			collision := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "demo-config", Namespace: "default"},
				Data:       map[string]string{"owner": "someone-else"},
			}
			Expect(k8sClient.Create(ctx, collision)).To(Succeed())

			By("deleting the SC before it has observed or adopted the collision")
			unreconciled := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, unreconciled)).To(Succeed())
			unreconciled.Finalizers = append(unreconciled.Finalizers, showroomv1alpha1.FinalizerName)
			Expect(k8sClient.Update(ctx, unreconciled)).To(Succeed())
			Expect(k8sClient.Delete(ctx, unreconciled)).To(Succeed())

			r := &SimpleClusterReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				RESTConfig: cfg,
			}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())

			stillThere := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "demo-config", Namespace: "default"}, stillThere)).To(Succeed())
			Expect(stillThere.Data).To(HaveKeyWithValue("owner", "someone-else"))
			Expect(stillThere.DeletionTimestamp).To(BeNil())
			Expect(stillThere.Annotations).NotTo(HaveKey(rendertpl.AnnoAllowDelete))
			blocked := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, blocked)).To(Succeed())
			Expect(blocked.Finalizers).To(ContainElement(showroomv1alpha1.FinalizerName))
		})

		It("rechecks the SCPC uncached before a billable create", func() {
			base, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
			Expect(err).NotTo(HaveOccurred())
			apiReader := interceptor.NewClient(base, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey,
					obj client.Object, opts ...client.GetOption) error {
					if err := c.Get(ctx, key, obj, opts...); err != nil {
						return err
					}
					if scpc, ok := obj.(*showroomv1alpha1.SimpleClusterProviderConfig); ok && key.Name == scpcName {
						scpc.Spec.ProvisioningEnabled = ptr.To(false)
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
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: "demo-config", Namespace: "default",
			}, cm)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("blocks a target create when the SCPC deletion has started", func() {
			base, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
			Expect(err).NotTo(HaveOccurred())
			apiReader := interceptor.NewClient(base, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey,
					obj client.Object, opts ...client.GetOption) error {
					if err := c.Get(ctx, key, obj, opts...); err != nil {
						return err
					}
					if scpc, ok := obj.(*showroomv1alpha1.SimpleClusterProviderConfig); ok && key.Name == scpcName {
						scpc.DeletionTimestamp = &metav1.Time{Time: time.Now()}
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
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: "demo-config", Namespace: "default",
			}, cm)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("keeps observing and deleting existing targets after retirement", func() {
			r := &SimpleClusterReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				RESTConfig: cfg,
			}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())

			scpc := &showroomv1alpha1.SimpleClusterProviderConfig{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: scpcName, Namespace: "default",
			}, scpc)).To(Succeed())
			scpc.Spec.ProvisioningEnabled = ptr.To(false)
			Expect(k8sClient.Update(ctx, scpc)).To(Succeed())

			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())
			updated := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, updated)).To(Succeed())
			Expect(updated.Status.Phase).To(Equal(showroomv1alpha1.PhaseReady))

			Expect(k8sClient.Delete(ctx, updated)).To(Succeed())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())

			cm := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: "demo-config", Namespace: "default",
			}, cm)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
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

		It("deletes a NameTemplate target when create crashed before the status patch", func() {
			By("configuring a provider-neutral target identity derived from the SC and input context")
			scpc := &showroomv1alpha1.SimpleClusterProviderConfig{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: scpcName, Namespace: "default"}, scpc)).To(Succeed())
			scpc.Spec.Inputs = []showroomv1alpha1.InputDeclaration{
				{Name: "provider", Type: showroomv1alpha1.InputTypeString, Default: "example"},
				{Name: "targetNamespace", Type: showroomv1alpha1.InputTypeString, Default: "default"},
			}
			scpc.Spec.Target.Namespace = "{{ .Inputs.targetNamespace }}"
			scpc.Spec.Resources[0].NameTemplate = `{{- printf "%s/%s/%s" .SC.namespace .SC.name .Inputs.provider | sha256sum | trunc 20 | printf "sc-%s" }}`
			Expect(k8sClient.Update(ctx, scpc)).To(Succeed())

			By("simulating target Create followed by a process crash before SC status was patched")
			sc := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, sc)).To(Succeed())
			sc.Finalizers = append(sc.Finalizers, showroomv1alpha1.FinalizerName)
			Expect(k8sClient.Update(ctx, sc)).To(Succeed())
			Expect(sc.Status.Resources).To(BeEmpty())

			targets, err := rendertpl.Render(scpc, sc, &scpc.Spec.Resources[0], nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(targets).To(HaveLen(1))
			target := targets[0]
			Expect(target.GetName()).To(HavePrefix("sc-"))
			Expect(target.GetName()).NotTo(Equal("demo-config"))
			target.SetFinalizers([]string{"test.showroom/hold"})
			Expect(k8sClient.Create(ctx, target)).To(Succeed())
			targetKey := types.NamespacedName{Name: target.GetName(), Namespace: target.GetNamespace()}
			DeferCleanup(func() {
				held := &corev1.ConfigMap{}
				if err := k8sClient.Get(ctx, targetKey, held); err == nil {
					held.Finalizers = nil
					_ = k8sClient.Update(ctx, held)
					_ = k8sClient.Delete(ctx, held)
				}
			})

			By("deleting the SC before any target identity reached status")
			Expect(k8sClient.Delete(ctx, sc)).To(Succeed())
			r := &SimpleClusterReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				RESTConfig: cfg,
			}
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())

			By("finding and deleting the hashed target rather than the legacy computed base name")
			held := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, targetKey, held)).To(Succeed())
			Expect(held.Annotations).To(HaveKeyWithValue(rendertpl.AnnoAllowDelete, "true"))
			Expect(held.DeletionTimestamp).NotTo(BeNil())
			legacy := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "demo-config", Namespace: "default"}, legacy)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("deletes every repeated target when create crashed before the status patch", func() {
			By("configuring two deterministic target instances")
			scpc := &showroomv1alpha1.SimpleClusterProviderConfig{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: scpcName, Namespace: "default"}, scpc)).To(Succeed())
			scpc.Spec.Resources[0].Repeat = "2"
			Expect(k8sClient.Update(ctx, scpc)).To(Succeed())

			By("simulating both creates followed by a crash before status was patched")
			sc := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, sc)).To(Succeed())
			sc.Finalizers = append(sc.Finalizers, showroomv1alpha1.FinalizerName)
			Expect(k8sClient.Update(ctx, sc)).To(Succeed())
			Expect(sc.Status.Resources).To(BeEmpty())

			targets, err := rendertpl.Render(scpc, sc, &scpc.Spec.Resources[0], nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(targets).To(HaveLen(2))
			targetKeys := make([]types.NamespacedName, 0, len(targets))
			for _, target := range targets {
				target.SetFinalizers([]string{"test.showroom/hold"})
				Expect(k8sClient.Create(ctx, target)).To(Succeed())
				targetKeys = append(targetKeys, types.NamespacedName{
					Name: target.GetName(), Namespace: target.GetNamespace(),
				})
			}
			DeferCleanup(func() {
				for _, key := range targetKeys {
					held := &corev1.ConfigMap{}
					if err := k8sClient.Get(ctx, key, held); err == nil {
						held.Finalizers = nil
						_ = k8sClient.Update(ctx, held)
						_ = k8sClient.Delete(ctx, held)
					}
				}
			})

			Expect(k8sClient.Delete(ctx, sc)).To(Succeed())
			r := &SimpleClusterReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				RESTConfig: cfg,
			}
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())

			By("reconstructing and deleting both indexed identities")
			for _, key := range targetKeys {
				held := &corev1.ConfigMap{}
				Expect(k8sClient.Get(ctx, key, held)).To(Succeed())
				Expect(held.Annotations).To(HaveKeyWithValue(rendertpl.AnnoAllowDelete, "true"))
				Expect(held.DeletionTimestamp).NotTo(BeNil())
			}
		})

		It("locks the selected provider before create so a crash and spec edit cannot redirect cleanup", func() {
			statusPatches := 0
			base, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
			Expect(err).NotTo(HaveOccurred())
			intercepted := interceptor.NewClient(base, interceptor.Funcs{
				SubResourcePatch: func(
					ctx context.Context,
					c client.Client,
					subResourceName string,
					obj client.Object,
					patch client.Patch,
					opts ...client.SubResourcePatchOption,
				) error {
					if subResourceName == "status" {
						statusPatches++
						if statusPatches == 2 {
							return fmt.Errorf("simulated crash before resource status persistence")
						}
					}
					return c.SubResource(subResourceName).Patch(ctx, obj, patch, opts...)
				},
			})
			r := &SimpleClusterReconciler{
				Client:     intercepted,
				Scheme:     k8sClient.Scheme(),
				RESTConfig: cfg,
			}

			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).To(MatchError(ContainSubstring("simulated crash")))
			Expect(statusPatches).To(Equal(2))

			created := &corev1.ConfigMap{}
			targetKey := types.NamespacedName{Name: "demo-config", Namespace: "default"}
			Expect(k8sClient.Get(ctx, targetKey, created)).To(Succeed())
			crashed := &showroomv1alpha1.SimpleCluster{}
			Expect(k8sClient.Get(ctx, scKey, crashed)).To(Succeed())
			Expect(crashed.Status.ProviderConfigRef).NotTo(BeNil())
			Expect(crashed.Status.ProviderConfigRef.Name).To(Equal(scpcName))
			Expect(crashed.Status.ProviderConfigNamespace).To(Equal("default"))
			lockedSCPC := &showroomv1alpha1.SimpleClusterProviderConfig{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: scpcName, Namespace: "default"}, lockedSCPC)).To(Succeed())
			Expect(crashed.Status.ProviderConfigUID).To(Equal(lockedSCPC.UID))
			Expect(crashed.Status.Resources).To(BeEmpty())

			By("changing the mutable candidate list after the simulated crash")
			crashed.Spec.ProviderConfigRefs = []string{"different-provider"}
			Expect(k8sClient.Update(ctx, crashed)).To(Succeed())
			Expect(k8sClient.Delete(ctx, crashed)).To(Succeed())

			By("still deleting through the provider locked before create")
			normal := &SimpleClusterReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				RESTConfig: cfg,
			}
			_, err = normal.Reconcile(ctx, reconcile.Request{NamespacedName: scKey})
			Expect(err).NotTo(HaveOccurred())
			gone := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, targetKey, gone)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
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
