/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Tests for the F-SK-1 / F-SK-3 hardening: spec.keySize bounds (CRD plus
// controller clamp) and the public-key format round-trip that keeps a
// hand-written, tenant-writable publicKey from being exported or injected.
// Plain fake-client tests, like secret_references_test.go; no envtest.
package controller

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	api "github.com/apeirora/showroom-msp-simple-cluster/sshkey-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/sshkey-operator/internal/secretpolicy"
)

func secretpolicyAllowNone() secretpolicy.Policy { return secretpolicy.Policy{} }

func ed25519Pub(t *testing.T) ed25519.PublicKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv.Public().(ed25519.PublicKey)
}

func TestEffectiveKeySize(t *testing.T) {
	for _, tc := range []struct {
		name      string
		keySize   int
		want      int
		wantClamp bool
	}{
		{"default when unset", 0, defaultKeySize, false},
		{"minimum accepted", minKeySize, minKeySize, false},
		{"maximum accepted", maxKeySize, maxKeySize, false},
		{"below minimum clamped up", 512, minKeySize, true},
		{"negative clamped up", -8, minKeySize, true},
		{"above maximum clamped down", 100_000_000, maxKeySize, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := &api.SSHKey{Spec: api.SSHKeySpec{KeyType: api.KeyTypeRSA, KeySize: tc.keySize}}
			got, clamped := effectiveKeySize(key)
			if got != tc.want || clamped != tc.wantClamp {
				t.Fatalf("effectiveKeySize(%d) = %d/%v, want %d/%v", tc.keySize, got, clamped, tc.want, tc.wantClamp)
			}
		})
	}
}

func TestPublicKeyValid(t *testing.T) {
	const valid = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4f comment"
	for _, tc := range []struct {
		name   string
		format api.KeyFormat
		pub    string
		want   bool
	}{
		{"empty", "", "", false},
		{"valid single line", "", valid, true},
		{"valid with newline", "", valid + "\n", true},
		{"multi-line blob smuggles extra lines", "", valid + "\nssh-rsa AAAA second line\n", false},
		{"garbage", "", "not-a-key at all", false},
		{"trailing text is the ssh comment", "", valid + " junk\x00more", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := &api.SSHKey{Spec: api.SSHKeySpec{KeyFormat: tc.format}, PublicKey: tc.pub}
			if got := publicKeyValid(key); got != tc.want {
				t.Fatalf("publicKeyValid = %v, want %v", got, tc.want)
			}
		})
	}

	// RFC4716: the value formatPublicKey itself emits must round-trip.
	sshPub, err := ssh.NewPublicKey(ed25519Pub(t))
	if err != nil {
		t.Fatal(err)
	}
	rfc := formatPublicKey(sshPub, "k@ns", api.KeyFormatRFC4716)
	key := &api.SSHKey{Spec: api.SSHKeySpec{KeyFormat: api.KeyFormatRFC4716}, PublicKey: rfc}
	if !publicKeyValid(key) {
		t.Fatalf("generated RFC4716 key rejected:\n%s", rfc)
	}
	key.PublicKey = "---- BEGIN SSH2 PUBLIC KEY ----\n!!!! not base64 ----\n---- END SSH2 PUBLIC KEY ----\n"
	if publicKeyValid(key) {
		t.Fatal("corrupt RFC4716 body accepted")
	}
}

// TestClampedKeySizeSurfacesOnStatus: an SSHKey whose stored key looks done
// but whose spec.keySize is out of range gets a clamped-size key plus a
// KeySizeClamped condition (below-minimum size keeps the test fast: the
// clamp target 1024 bits generates quickly; the pure function covers the
// above-maximum side).
func TestClampedKeySizeSurfacesOnStatus(t *testing.T) {
	ctx := context.Background()
	scheme := testScheme(t)
	key := &api.SSHKey{
		ObjectMeta: metav1.ObjectMeta{Name: "small", Namespace: "account"},
		Spec:       api.SSHKeySpec{KeyType: api.KeyTypeRSA, KeySize: 512},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&api.SSHKey{}).
		WithObjects(key).Build()
	r := &SSHKeyReconciler{Client: cl, SecretPolicy: secretpolicyAllowNone(), Scheme: scheme}

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "small", Namespace: "account"}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var got api.SSHKey
	if err := cl.Get(ctx, types.NamespacedName{Name: "small", Namespace: "account"}, &got); err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode([]byte(got.PrivateKey))
	if block == nil {
		t.Fatalf("private key is not PEM: %q", got.PrivateKey)
	}
	raw, err := ssh.ParseRawPrivateKey([]byte(got.PrivateKey))
	if err != nil {
		t.Fatalf("parse private key: %v", err)
	}
	rsaKey, ok := raw.(*rsa.PrivateKey)
	if !ok {
		t.Fatalf("expected *rsa.PrivateKey, got %T", raw)
	}
	if rsaKey.N.BitLen() != minKeySize {
		t.Fatalf("key generated at %d bits, want clamped to %d", rsaKey.N.BitLen(), minKeySize)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, api.ConditionKeySizeClamped)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Fatalf("KeySizeClamped condition missing: %+v", got.Status.Conditions)
	}
	if !strings.Contains(cond.Message, "512") || !strings.Contains(cond.Message, "1024") {
		t.Fatalf("condition message does not name the clamp: %q", cond.Message)
	}
}

// TestInvalidPublicKeyIsRegenerated (F-SK-1): a hand-written (tenant-writable)
// publicKey that does not parse is treated as missing; the reconcile replaces
// both key fields and re-exports the secret instead of exporting garbage.
func TestInvalidPublicKeyIsRegenerated(t *testing.T) {
	ctx := context.Background()
	scheme := testScheme(t)
	key := &api.SSHKey{
		ObjectMeta: metav1.ObjectMeta{Name: "hand", Namespace: "account"},
		Spec:       api.SSHKeySpec{KeyType: api.KeyTypeED25519, ExportToSecret: corev1.LocalObjectReference{Name: "hand-ssh"}},
		PrivateKey: "pretend-private",
		PublicKey:  "ssh-ed25519 AAAA\nsecond line\n",
	}
	// Make the spec hash and rotation look satisfied so only the format
	// check can trigger regeneration.
	key.Status.KeyHash = specHash(key)
	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&api.SSHKey{}).
		WithObjects(key).Build()
	r := &SSHKeyReconciler{Client: cl, SecretPolicy: secretpolicyAllowNone(), Scheme: scheme}

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "hand", Namespace: "account"}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var got api.SSHKey
	if err := cl.Get(ctx, types.NamespacedName{Name: "hand", Namespace: "account"}, &got); err != nil {
		t.Fatal(err)
	}
	if got.PrivateKey == "pretend-private" {
		t.Fatal("unparseable publicKey was kept instead of regenerated")
	}
	if !publicKeyValid(&got) {
		t.Fatalf("regenerated publicKey invalid: %q", got.PublicKey)
	}
	if !got.Status.Ready || got.Status.PublicKey != got.PublicKey {
		t.Fatalf("status not updated: %+v", got.Status)
	}
	var secret corev1.Secret
	if err := cl.Get(ctx, types.NamespacedName{Name: "hand-ssh", Namespace: "account"}, &secret); err != nil {
		t.Fatal(err)
	}
	if string(secret.Data["ssh-publickey"]) != got.PublicKey {
		t.Fatal("exported secret does not carry the regenerated public key")
	}
}

// TestRekeyUpdatesTopLevelAndSecret (operator half of F-SK-4): a spec change
// on a bound claim's SSHKey replaces the top-level key pair and the exported
// Secret in the same reconcile; the injector-side fingerprint re-injection is
// covered by the ubuntu-machines controller tests.
func TestRekeyUpdatesTopLevelAndSecret(t *testing.T) {
	ctx := context.Background()
	scheme := testScheme(t)
	key := &api.SSHKey{
		ObjectMeta: metav1.ObjectMeta{Name: "rot", Namespace: "account"},
		Spec: api.SSHKeySpec{
			KeyType:        api.KeyTypeED25519,
			ExportToSecret: corev1.LocalObjectReference{Name: "rot-ssh"},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&api.SSHKey{}).
		WithObjects(key).Build()
	r := &SSHKeyReconciler{Client: cl, SecretPolicy: secretpolicyAllowNone(), Scheme: scheme}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "rot", Namespace: "account"}}

	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	var first api.SSHKey
	if err := cl.Get(ctx, req.NamespacedName, &first); err != nil {
		t.Fatal(err)
	}

	// Tenant patches the key-defining spec: hash mismatch triggers the rekey.
	first.Spec.KeyType = api.KeyTypeECDSA
	if err := cl.Update(ctx, &first); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("rekey reconcile: %v", err)
	}
	var second api.SSHKey
	if err := cl.Get(ctx, req.NamespacedName, &second); err != nil {
		t.Fatal(err)
	}
	if second.PublicKey == first.PublicKey || second.PrivateKey == first.PrivateKey {
		t.Fatal("spec change did not rekey")
	}
	var secret corev1.Secret
	if err := cl.Get(ctx, types.NamespacedName{Name: "rot-ssh", Namespace: "account"}, &secret); err != nil {
		t.Fatal(err)
	}
	if string(secret.Data["ssh-publickey"]) != second.PublicKey {
		t.Fatal("secret still carries the superseded public key")
	}
}
