package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	arsysv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/arsys-ubuntu-machine-operator/api/v1alpha1"
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
		{"sshkey", &corev1.TypedObjectReference{APIGroup: ptr(arsysv1alpha1.SSHKeyAPIGroup), Kind: "SSHKey", Name: "k"}, true},
		{"sshkey-wrong-group", &corev1.TypedObjectReference{APIGroup: ptr("other"), Kind: "SSHKey", Name: "k"}, false},
	}
	for _, c := range cases {
		if got := isSSHKeyResourceRef(c.ref); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func newSSHKey(ns, name, pub string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": arsysv1alpha1.SSHKeyAPIGroup + "/" + arsysv1alpha1.SSHKeyVersion,
		"kind":       arsysv1alpha1.SSHKeyKind,
		"metadata":   map[string]any{"namespace": ns, "name": name},
		"publicKey":  pub,
	}}
}

func fakeReconciler(objs ...runtime.Object) *ArsysUbuntuMachineReconciler {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	gvk := schema.GroupVersionKind{Group: arsysv1alpha1.SSHKeyAPIGroup, Version: arsysv1alpha1.SSHKeyVersion, Kind: arsysv1alpha1.SSHKeyKind}
	scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(gvk.GroupVersion().WithKind(arsysv1alpha1.SSHKeyKind+"List"), &unstructured.UnstructuredList{})
	c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
	return &ArsysUbuntuMachineReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10)}
}

func machineWithRef(ref *corev1.TypedObjectReference) *arsysv1alpha1.ArsysUbuntuMachine {
	aum := &arsysv1alpha1.ArsysUbuntuMachine{}
	aum.Name = "m"
	aum.Namespace = "ns1"
	aum.Spec.Server.SSHKeyRef = ref
	return aum
}

func TestCollectSSHPublicKey_FromSSHKeyResource(t *testing.T) {
	aum := machineWithRef(&corev1.TypedObjectReference{APIGroup: ptr(arsysv1alpha1.SSHKeyAPIGroup), Kind: "SSHKey", Name: "k"})
	r := fakeReconciler(newSSHKey("ns1", "k", "ssh-ed25519 AAAA m@ns1"))

	got, err := r.collectSSHPublicKey(context.Background(), aum)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "ssh-ed25519 AAAA m@ns1" {
		t.Errorf("got %q", got)
	}
}

func TestCollectSSHPublicKey_FromSecretSkipsPrivateKey(t *testing.T) {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "k", Namespace: "ns1"},
		Data: map[string][]byte{
			"ssh-publickey":  []byte("ssh-ed25519 AAAA m@ns1"),
			"ssh-privatekey": []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nx\n-----END OPENSSH PRIVATE KEY-----"),
		},
	}
	aum := machineWithRef(&corev1.TypedObjectReference{Kind: "Secret", Name: "k"})
	r := fakeReconciler(sec)

	got, err := r.collectSSHPublicKey(context.Background(), aum)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "ssh-ed25519 AAAA m@ns1" {
		t.Errorf("got %q, want the public key only (private key must be skipped)", got)
	}
}
