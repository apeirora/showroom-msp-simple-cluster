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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dativ1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/providers/dati-provider/api/v1alpha1"
)

// fakeGardener records Shoots in memory and mints canned kubeconfigs.
type fakeGardener struct {
	shoots  map[string]*unstructured.Unstructured
	expires time.Time
}

func newFakeGardener() *fakeGardener {
	return &fakeGardener{
		shoots:  map[string]*unstructured.Unstructured{},
		expires: time.Now().Add(24 * time.Hour),
	}
}

func (f *fakeGardener) GetShoot(_ context.Context, name string) (*unstructured.Unstructured, error) {
	s, ok := f.shoots[name]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: "core.gardener.cloud", Resource: "shoots"}, name)
	}
	return s, nil
}

func (f *fakeGardener) CreateShoot(_ context.Context, shoot *unstructured.Unstructured) error {
	f.shoots[shoot.GetName()] = shoot
	return nil
}

func (f *fakeGardener) DeleteShoot(_ context.Context, name string) (bool, error) {
	if _, ok := f.shoots[name]; !ok {
		return true, nil
	}
	delete(f.shoots, name)
	return false, nil // gone on the next reconcile, like a real teardown
}

func (f *fakeGardener) AdminKubeconfig(_ context.Context, name string, _ time.Duration) ([]byte, time.Time, error) {
	return []byte("kubeconfig-for-" + name), f.expires, nil
}

func (f *fakeGardener) setState(name, state string) {
	s := f.shoots[name]
	_ = unstructured.SetNestedField(s.Object, state, "status", "lastOperation", "state")
}

var _ = Describe("Shoot Controller", func() {
	ctx := context.Background()

	const (
		ns             = "default"
		shName         = "dati"
		gardenerSecret = "test-gardener-kubeconfig"
	)
	shKey := types.NamespacedName{Namespace: ns, Name: shName}
	shootName := gardenShootName(datiShootKind, ns, shName)

	var gardener *fakeGardener

	newReconciler := func() *ShootReconciler {
		defaults, err := parseFlavorDefaults(scpcDatiManifest)
		Expect(err).NotTo(HaveOccurred())
		return &ShootReconciler{
			Client:                          k8sClient,
			Scheme:                          k8sClient.Scheme(),
			KubeconfigSource:                KubeconfigSourceGlobal,
			GlobalKubeconfigSecretName:      gardenerSecret,
			GlobalKubeconfigSecretNamespace: ns,
			NewGardener:                     func([]byte, string) (Gardener, error) { return gardener, nil },
			defaults:                        defaults,
		}
	}

	seedGardenerSecret := func() {
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: gardenerSecret, Namespace: ns},
			Data:       map[string][]byte{kubeconfigKey: []byte("fake")},
		})).To(Succeed())
	}

	cleanup := func() {
		sh := &dativ1alpha1.Shoot{}
		if err := k8sClient.Get(ctx, shKey, sh); err == nil {
			sh.Finalizers = nil
			_ = k8sClient.Update(ctx, sh)
			_ = k8sClient.Delete(ctx, sh)
		}
		for _, name := range []string{gardenerSecret, kubeconfigSecretName(shName, datiShootKind)} {
			s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
			_ = k8sClient.Delete(ctx, s)
		}
	}

	create := func(spec string) {
		Expect(k8sClient.Create(ctx, &dativ1alpha1.Shoot{
			ObjectMeta: metav1.ObjectMeta{Name: shName, Namespace: ns},
			Spec:       spec,
		})).To(Succeed())
	}

	BeforeEach(func() {
		gardener = newFakeGardener()
	})

	AfterEach(cleanup)

	It("creates the Gardener Shoot from the default template and reports Provisioning", func() {
		seedGardenerSecret()
		create("")

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
		Expect(err).NotTo(HaveOccurred())

		sh := &dativ1alpha1.Shoot{}
		Expect(k8sClient.Get(ctx, shKey, sh)).To(Succeed())
		Expect(sh.Finalizers).To(ContainElement(datiShootFinalizer))
		Expect(sh.Status.Phase).To(Equal(phaseProvisioning))
		Expect(sh.Status.ShootName).To(Equal(shootName))

		created := gardener.shoots[shootName]
		Expect(created).NotTo(BeNil())
		Expect(created.GetNamespace()).To(Equal("garden-sap"))
		Expect(created.GetAnnotations()).To(HaveKeyWithValue(shootOwnerAnnotation, ns+"/"+shName))
		providerType, _, _ := unstructured.NestedString(created.Object, "spec", "provider", "type")
		Expect(providerType).To(Equal("openstack"))
		workers, _, _ := unstructured.NestedSlice(created.Object, "spec", "provider", "workers")
		Expect(workers).To(HaveLen(1))
		machineType, _, _ := unstructured.NestedString(workers[0].(map[string]interface{}), "machine", "type")
		Expect(machineType).To(Equal("4vCPU-8192RAM-100STORAGE")) // default machineType input
	})

	It("uses the CR spec as the Shoot manifest when set", func() {
		seedGardenerSecret()
		create("apiVersion: core.gardener.cloud/v1beta1\nkind: Shoot\nspec:\n  region: custom-region\n")

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
		Expect(err).NotTo(HaveOccurred())

		created := gardener.shoots[shootName]
		Expect(created).NotTo(BeNil())
		region, _, _ := unstructured.NestedString(created.Object, "spec", "region")
		Expect(region).To(Equal("custom-region"))
	})

	It("surfaces a Failed condition when the spec is not plain YAML", func() {
		seedGardenerSecret()
		// The spec is now used verbatim: Go-template syntax is no longer
		// rendered and fails YAML parsing.
		create("apiVersion: core.gardener.cloud/v1beta1\nkind: Shoot\nspec:\n  region: {{ .Inputs.region }}\n")

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
		Expect(err).NotTo(HaveOccurred())

		sh := &dativ1alpha1.Shoot{}
		Expect(k8sClient.Get(ctx, shKey, sh)).To(Succeed())
		Expect(sh.Status.Phase).To(Equal(phaseFailed))
		ready := apimeta.FindStatusCondition(sh.Status.Conditions, conditionReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Reason).To(Equal("ShootCreateFailed"))
		Expect(gardener.shoots).To(BeEmpty())
	})

	It("turns Ready and writes the kubeconfig Secret once the Shoot succeeds", func() {
		seedGardenerSecret()
		create("")

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
		Expect(err).NotTo(HaveOccurred())
		gardener.setState(shootName, "Succeeded")
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
		Expect(err).NotTo(HaveOccurred())

		sh := &dativ1alpha1.Shoot{}
		Expect(k8sClient.Get(ctx, shKey, sh)).To(Succeed())
		Expect(sh.Status.Phase).To(Equal(phaseReady))
		ready := apimeta.FindStatusCondition(sh.Status.Conditions, conditionReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		Expect(sh.Status.KubeconfigSecretRef).NotTo(BeNil())
		Expect(sh.Status.KubeconfigExpires).NotTo(BeNil())

		secret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Namespace: ns, Name: sh.Status.KubeconfigSecretRef.Name,
		}, secret)).To(Succeed())
		Expect(secret.Data[kubeconfigKey]).To(Equal([]byte("kubeconfig-for-" + shootName)))
		Expect(secret.OwnerReferences).To(HaveLen(1))
		Expect(secret.OwnerReferences[0].Name).To(Equal(shName))
	})

	It("reports Failed when the Shoot reconciliation fails", func() {
		seedGardenerSecret()
		create("")

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
		Expect(err).NotTo(HaveOccurred())
		gardener.setState(shootName, "Failed")
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
		Expect(err).NotTo(HaveOccurred())

		sh := &dativ1alpha1.Shoot{}
		Expect(k8sClient.Get(ctx, shKey, sh)).To(Succeed())
		Expect(sh.Status.Phase).To(Equal(phaseFailed))
	})

	It("reports GardenerUnavailable when the kubeconfig Secret is missing", func() {
		create("")

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
		Expect(err).NotTo(HaveOccurred())

		sh := &dativ1alpha1.Shoot{}
		Expect(k8sClient.Get(ctx, shKey, sh)).To(Succeed())
		ready := apimeta.FindStatusCondition(sh.Status.Conditions, conditionReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Reason).To(Equal("GardenerUnavailable"))
		Expect(gardener.shoots).To(BeEmpty())
	})

	It("deletes the Gardener Shoot before releasing the finalizer", func() {
		seedGardenerSecret()
		create("")

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
		Expect(err).NotTo(HaveOccurred())
		Expect(gardener.shoots).To(HaveKey(shootName))

		sh := &dativ1alpha1.Shoot{}
		Expect(k8sClient.Get(ctx, shKey, sh)).To(Succeed())
		Expect(k8sClient.Delete(ctx, sh)).To(Succeed())

		// First pass initiates the Gardener deletion and keeps the finalizer.
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, shKey, sh)).To(Succeed())
		Expect(sh.Status.Phase).To(Equal(phaseDeleting))
		Expect(gardener.shoots).NotTo(HaveKey(shootName))

		// Second pass sees the Shoot gone and releases the CR.
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: shKey})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, shKey, sh)).To(HaveOccurred())
	})
})
