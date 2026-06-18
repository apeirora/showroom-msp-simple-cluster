package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	openstackv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-openstack/api/v1alpha1"
)

func ptr(s string) *string { return &s }

func TestIsSSHKeyResourceRef(t *testing.T) {
	cases := []struct {
		name string
		ref  *corev1.TypedObjectReference
		want bool
	}{
		{"nil", nil, false},
		{"secret", &corev1.TypedObjectReference{Kind: "Secret", Name: "s"}, false},
		{"sshkey", &corev1.TypedObjectReference{APIGroup: ptr(openstackv1alpha1.SSHKeyAPIGroup), Kind: "SSHKey", Name: "k"}, true},
		{"sshkey-wrong-group", &corev1.TypedObjectReference{APIGroup: ptr("other"), Kind: "SSHKey", Name: "k"}, false},
	}
	for _, c := range cases {
		if got := isSSHKeyResourceRef(c.ref); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestSecretKeyRef_DefaultAndExplicit(t *testing.T) {
	vm := &openstackv1alpha1.OpenStackVM{}
	vm.Name = "myvm"
	vm.Namespace = "ns1"

	nn, shared := secretKeyRef(vm)
	if shared || nn.Name != "myvm-ssh" || nn.Namespace != "ns1" {
		t.Fatalf("default: got %v shared=%v", nn, shared)
	}

	vm.Spec.SSHKeyRef = &corev1.TypedObjectReference{Kind: "Secret", Name: "shared-ssh"}
	nn, shared = secretKeyRef(vm)
	if !shared || nn.Name != "shared-ssh" || nn.Namespace != "ns1" {
		t.Fatalf("explicit: got %v shared=%v", nn, shared)
	}
}

func newSSHKey(ns, name, pub, exportSecret string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": openstackv1alpha1.SSHKeyAPIGroup + "/" + openstackv1alpha1.SSHKeyVersion,
		"kind":       openstackv1alpha1.SSHKeyKind,
		"metadata":   map[string]any{"namespace": ns, "name": name},
		"publicKey":  pub,
	}}
	if exportSecret != "" {
		_ = unstructured.SetNestedField(u.Object, exportSecret, "spec", "exportToSecret", "name")
	}
	return u
}

func fakeReconciler(objs ...runtime.Object) *OpenStackVMReconciler {
	scheme := runtime.NewScheme()
	gvk := schema.GroupVersionKind{Group: openstackv1alpha1.SSHKeyAPIGroup, Version: openstackv1alpha1.SSHKeyVersion, Kind: openstackv1alpha1.SSHKeyKind}
	scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(gvk.GroupVersion().WithKind(openstackv1alpha1.SSHKeyKind+"List"), &unstructured.UnstructuredList{})
	c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
	return &OpenStackVMReconciler{Client: c, Scheme: scheme}
}

func TestResolveSSHKeyResource(t *testing.T) {
	vm := &openstackv1alpha1.OpenStackVM{}
	vm.Name = "vm"
	vm.Namespace = "ns1"
	vm.Spec.SSHKeyRef = &corev1.TypedObjectReference{APIGroup: ptr(openstackv1alpha1.SSHKeyAPIGroup), Kind: "SSHKey", Name: "shared"}

	r := fakeReconciler(newSSHKey("ns1", "shared", "ssh-ed25519 AAAA vm@ns1", "shared-ssh"))
	nn, pub, err := r.resolveSSHKeyResource(context.Background(), vm)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if pub != "ssh-ed25519 AAAA vm@ns1" {
		t.Errorf("pub = %q", pub)
	}
	if nn.Name != "shared-ssh" || nn.Namespace != "ns1" {
		t.Errorf("secret ref = %v, want shared-ssh/ns1", nn)
	}
}

func TestResolveSSHKeyResource_NotReady(t *testing.T) {
	vm := &openstackv1alpha1.OpenStackVM{}
	vm.Name = "vm"
	vm.Namespace = "ns1"
	vm.Spec.SSHKeyRef = &corev1.TypedObjectReference{APIGroup: ptr(openstackv1alpha1.SSHKeyAPIGroup), Kind: "SSHKey", Name: "fresh"}

	r := fakeReconciler(newSSHKey("ns1", "fresh", "", ""))
	if _, _, err := r.resolveSSHKeyResource(context.Background(), vm); err == nil {
		t.Fatal("expected error for SSHKey without public key")
	}
}
