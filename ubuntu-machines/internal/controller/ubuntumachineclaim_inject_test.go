/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Machine-pool authorized_keys lifecycle tests (F-SK-2, F-SK-4).
//
// Grew out of the pipeline repros pipeline/F-SK-2/repro/fsk2_leak_test.go
// and pipeline/F-SK-4/repro/f_sk4_rotation_test.go. They drive the real
// UbuntuMachineClaimReconciler.Reconcile against a fake client plus one
// in-process SSH server per machine, whose in-memory content stands in for
// /root/.ssh/authorized_keys (written through the real setAuthorizedKeys
// code path). The sequence asserts the fixed behaviour: injection is keyed
// on the stored key fingerprint (re-inject on rotation and re-bind), every
// release path scrubs, and a failed scrub is retried instead of swallowed.
package controller

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"

	gossh "golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	umv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/ubuntu-machines/api/v1alpha1"
)

const (
	injectTestKeyA = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4f"
	injectTestKeyB = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICAhIiMkJSYnKCkqKywtLi8wMTIzNDU2Nzg5Ojs8PT4/"
	injectTestKeyC = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEBBQkNERUZHSElKS0xNTk9QUVJTVFVWV1hZWltcXV5f"
)

// injectTestHost is one fake pool machine: TCP SSH server plus the
// in-memory authorized_keys file it stands for.
type injectTestHost struct {
	mu          sync.Mutex
	addr        string
	content     string
	writes      int
	dials       int
	unreachable bool
}

func (h *injectTestHost) get() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.content
}

func (h *injectTestHost) counts() (dials, writes int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.dials, h.writes
}

func (h *injectTestHost) setUnreachable(v bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.unreachable = v
}

func (h *injectTestHost) seed(s string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.content = s
}

func startInjectTestHost(t *testing.T) *injectTestHost {
	t.Helper()
	h := &injectTestHost{}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &gossh.ServerConfig{
		PasswordCallback: func(gossh.ConnMetadata, []byte) (*gossh.Permissions, error) {
			return &gossh.Permissions{}, nil
		},
	}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h.addr = ln.Addr().String()
	go func() {
		for {
			ncc, err := ln.Accept()
			if err != nil {
				return
			}
			go handleInjectTestConn(ncc, cfg, h)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return h
}

// handleInjectTestConn emulates what setAuthorizedKeys does on a real host:
// exec request, stdin content becomes the whole authorized_keys file.
func handleInjectTestConn(ncc net.Conn, cfg *gossh.ServerConfig, h *injectTestHost) {
	sc, chans, reqs, err := gossh.NewServerConn(ncc, cfg)
	if err != nil {
		ncc.Close()
		return
	}
	defer sc.Close()
	go func() {
		for r := range reqs {
			if r.WantReply {
				r.Reply(false, nil)
			}
		}
	}()
	for nc := range chans {
		if nc.ChannelType() != "session" {
			nc.Reject(gossh.Prohibited, "only session channels")
			continue
		}
		ch, creqs, err := nc.Accept()
		if err != nil {
			return
		}
		go func() {
			go func() {
				for r := range creqs {
					if r.Type == "exec" {
						r.Reply(true, nil)
					} else if r.WantReply {
						r.Reply(false, nil)
					}
				}
			}()
			data, _ := io.ReadAll(ch)
			h.mu.Lock()
			h.content = string(data)
			h.writes++
			h.mu.Unlock()
			ch.SendRequest("exit-status", false, gossh.Marshal(&struct{ Status uint32 }{0}))
			ch.Close()
		}()
	}
}

func injectTestSSHKey(ns, name, pub string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "sshkeys.apeirora.eu/v1alpha1",
		"kind":       "SSHKey",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": ns,
		},
		"publicKey": pub,
		"status":    map[string]interface{}{"ready": true},
	}}
}

func injectTestMachine(name string, phase umv1alpha1.MachinePhase, ref *umv1alpha1.ClaimReference) *umv1alpha1.UbuntuMachine {
	return &umv1alpha1.UbuntuMachine{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: umv1alpha1.UbuntuMachineSpec{
			Address: "127.0.0.1",
			AdminSSH: umv1alpha1.SSHAuth{
				User:              "root",
				PasswordSecretRef: &corev1.SecretReference{Namespace: "shared", Name: "admin"},
			},
		},
		Status: umv1alpha1.UbuntuMachineStatus{Phase: phase, BoundClaimRef: ref},
	}
}

// injectTestFixture wires reconciler + fake cluster + named in-process hosts.
type injectTestFixture struct {
	reconciler *UbuntuMachineClaimReconciler
	client     client.Client
	hosts      map[string]*injectTestHost
}

func newInjectTestFixture(t *testing.T, machines []*umv1alpha1.UbuntuMachine, claims ...client.Object) *injectTestFixture {
	t.Helper()
	scheme := testScheme(t)
	hosts := map[string]*injectTestHost{}
	for _, m := range machines {
		hosts[m.Name] = startInjectTestHost(t)
	}

	dialer := func(m *umv1alpha1.UbuntuMachine, _ *corev1.Secret) (*gossh.Client, error) {
		h, ok := hosts[m.Name]
		if !ok {
			return nil, fmt.Errorf("unknown machine %s", m.Name)
		}
		h.mu.Lock()
		h.dials++
		gone := h.unreachable
		h.mu.Unlock()
		if gone {
			return nil, fmt.Errorf("dial %s: connection refused", m.Name)
		}
		return gossh.Dial("tcp", h.addr, &gossh.ClientConfig{
			User:            "root",
			Auth:            []gossh.AuthMethod{gossh.Password("x")},
			HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		})
	}

	admin := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "shared", Name: "admin"},
		Data:       map[string][]byte{umv1alpha1.SSHSecretPasswordKey: []byte("x")},
	}
	objs := []client.Object{admin}
	for _, m := range machines {
		objs = append(objs, m)
	}
	objs = append(objs, claims...)
	subresources := make([]client.Object, 0, len(machines)+len(claims))
	for _, m := range machines {
		subresources = append(subresources, m)
	}
	subresources = append(subresources, claims...)
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(subresources...).
		WithObjects(objs...).
		Build()
	return &injectTestFixture{
		reconciler: &UbuntuMachineClaimReconciler{
			Client:       cl,
			Scheme:       scheme,
			SecretPolicy: policyEntry("shared", "admin", "use"),
			sshDial:      dialer,
		},
		client: cl,
		hosts:  hosts,
	}
}

func claimRequest(namespace, name string) ctrl.Request {
	return ctrl.Request{NamespacedName: client.ObjectKey{Name: name, Namespace: namespace}}
}

func getClaim(t *testing.T, cl client.Client, namespace, name string) *umv1alpha1.UbuntuMachineClaim {
	t.Helper()
	var claim umv1alpha1.UbuntuMachineClaim
	if err := cl.Get(context.Background(), client.ObjectKey{Name: name, Namespace: namespace}, &claim); err != nil {
		t.Fatal(err)
	}
	return &claim
}

func claimWithFinalizer(namespace, name string) *umv1alpha1.UbuntuMachineClaim {
	return &umv1alpha1.UbuntuMachineClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Finalizers: []string{umv1alpha1.FinalizerName},
		},
	}
}

// TestFingerprintInjectOnceThenIdempotent: a fresh bind injects the key and
// records its fingerprint; steady-state reconciles rewrite nothing.
func TestFingerprintInjectOnceThenIdempotent(t *testing.T) {
	ctx := context.Background()
	m1 := injectTestMachine("m1", umv1alpha1.MachineAvailable, nil)
	f := newInjectTestFixture(t,
		[]*umv1alpha1.UbuntuMachine{m1},
		claimWithFinalizer("tenant-a", "claim-a"),
		injectTestSSHKey("tenant-a", "claim-a-sshkey", injectTestKeyA),
	)

	if _, err := f.reconciler.Reconcile(ctx, claimRequest("tenant-a", "claim-a")); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if f.hosts["m1"].get() != injectTestKeyA+"\n" {
		t.Fatalf("key not injected: %q", f.hosts["m1"].get())
	}
	claim := getClaim(t, f.client, "tenant-a", "claim-a")
	if claim.Status.SSHKeyFingerprint != sshKeyFingerprint(injectTestKeyA) {
		t.Fatalf("fingerprint not recorded: %q", claim.Status.SSHKeyFingerprint)
	}
	if d, w := f.hosts["m1"].counts(); d != 1 || w != 1 {
		t.Fatalf("bind: want 1 dial/1 write, got %d/%d", d, w)
	}

	if _, err := f.reconciler.Reconcile(ctx, claimRequest("tenant-a", "claim-a")); err != nil {
		t.Fatalf("steady: %v", err)
	}
	if _, w := f.hosts["m1"].counts(); w != 1 {
		t.Fatalf("steady-state re-reconcile rewrote authorized_keys (writes=%d)", w)
	}
}

// TestRotationReinjects (F-SK-4): rekeying the SSHKey of a bound claim
// replaces the key on the machine on the next reconcile instead of leaving
// the rotated-away key valid for the rest of the lease.
func TestRotationReinjects(t *testing.T) {
	ctx := context.Background()
	m1 := injectTestMachine("m1", umv1alpha1.MachineAvailable, nil)
	f := newInjectTestFixture(t,
		[]*umv1alpha1.UbuntuMachine{m1},
		claimWithFinalizer("tenant-a", "claim-a"),
		injectTestSSHKey("tenant-a", "claim-a-sshkey", injectTestKeyA),
	)

	if _, err := f.reconciler.Reconcile(ctx, claimRequest("tenant-a", "claim-a")); err != nil {
		t.Fatalf("bind: %v", err)
	}

	// Simulate the sshkey-operator rekeying the bound claim (spec change or
	// rotation): the top-level publicKey is replaced in place.
	var keyObj unstructured.Unstructured
	keyObj.SetGroupVersionKind(sshKeyGVK)
	if err := f.client.Get(ctx, client.ObjectKey{Name: "claim-a-sshkey", Namespace: "tenant-a"}, &keyObj); err != nil {
		t.Fatal(err)
	}
	if err := unstructured.SetNestedField(keyObj.Object, injectTestKeyB, "publicKey"); err != nil {
		t.Fatal(err)
	}
	if err := f.client.Update(ctx, &keyObj); err != nil {
		t.Fatal(err)
	}

	if _, err := f.reconciler.Reconcile(ctx, claimRequest("tenant-a", "claim-a")); err != nil {
		t.Fatalf("post-rotation reconcile: %v", err)
	}
	if f.hosts["m1"].get() != injectTestKeyB+"\n" {
		t.Fatalf("rotated key not re-injected: %q", f.hosts["m1"].get())
	}
	claim := getClaim(t, f.client, "tenant-a", "claim-a")
	if claim.Status.SSHKeyFingerprint != sshKeyFingerprint(injectTestKeyB) {
		t.Fatalf("fingerprint not updated: %q", claim.Status.SSHKeyFingerprint)
	}

	// Steady again: no third write.
	if _, err := f.reconciler.Reconcile(ctx, claimRequest("tenant-a", "claim-a")); err != nil {
		t.Fatalf("steady: %v", err)
	}
	if _, w := f.hosts["m1"].counts(); w != 2 {
		t.Fatalf("expected exactly the two injections, writes=%d", w)
	}
}

// TestRebindReinjectsKeyOnNewMachine (F-SK-2): a Ready claim whose machine
// was externally re-pointed to another machine gets its own key injected
// there, instead of serving a whole lease on a machine it never keyed.
func TestRebindReinjectsKeyOnNewMachine(t *testing.T) {
	ctx := context.Background()
	ref := &umv1alpha1.ClaimReference{Namespace: "tenant-b", Name: "claim-b"}
	m1 := injectTestMachine("m1", umv1alpha1.MachineBound, ref)
	m2 := injectTestMachine("m2", umv1alpha1.MachineAvailable, nil)
	claimB := claimWithFinalizer("tenant-b", "claim-b")
	claimB.Status = umv1alpha1.UbuntuMachineClaimStatus{
		Phase:             umv1alpha1.ClaimReady,
		Machine:           &umv1alpha1.BoundMachine{Name: "m1"},
		SSHSecretRef:      &corev1.LocalObjectReference{Name: "claim-b-ssh"},
		SSHKeyFingerprint: sshKeyFingerprint(injectTestKeyB),
	}
	f := newInjectTestFixture(t,
		[]*umv1alpha1.UbuntuMachine{m1, m2},
		claimB,
		injectTestSSHKey("tenant-b", "claim-b-sshkey", injectTestKeyB),
	)
	f.hosts["m1"].seed(injectTestKeyA + "\n") // prior tenant A's key survived there

	// Admin recovery edit: ownership moves from m1 to m2.
	m1.Status.Phase = umv1alpha1.MachineAvailable
	m1.Status.BoundClaimRef = nil
	if err := f.client.Status().Update(ctx, m1); err != nil {
		t.Fatal(err)
	}
	m2.Status.Phase = umv1alpha1.MachineBound
	m2.Status.BoundClaimRef = ref
	if err := f.client.Status().Update(ctx, m2); err != nil {
		t.Fatal(err)
	}

	if _, err := f.reconciler.Reconcile(ctx, claimRequest("tenant-b", "claim-b")); err != nil {
		t.Fatalf("rebind: %v", err)
	}
	claim := getClaim(t, f.client, "tenant-b", "claim-b")
	if claim.Status.Machine == nil || claim.Status.Machine.Name != "m2" {
		t.Fatalf("claim should have re-bound to m2: %+v", claim.Status.Machine)
	}
	if f.hosts["m2"].get() != injectTestKeyB+"\n" {
		t.Fatalf("re-bind did not inject the claim key on m2: %q", f.hosts["m2"].get())
	}
}

// TestReleaseRetriesScrubWhenUnreachable (F-SK-2): deleting a claim while
// its machine is SSH-unreachable requeues instead of swallowing the error,
// and the retry scrubs before the finalizer is dropped, so the key never
// survives the release.
func TestReleaseRetriesScrubWhenUnreachable(t *testing.T) {
	ctx := context.Background()
	m1 := injectTestMachine("m1", umv1alpha1.MachineAvailable, nil)
	f := newInjectTestFixture(t,
		[]*umv1alpha1.UbuntuMachine{m1},
		claimWithFinalizer("tenant-a", "claim-a"),
		injectTestSSHKey("tenant-a", "claim-a-sshkey", injectTestKeyA),
	)
	if _, err := f.reconciler.Reconcile(ctx, claimRequest("tenant-a", "claim-a")); err != nil {
		t.Fatalf("bind: %v", err)
	}

	f.hosts["m1"].setUnreachable(true)
	if err := f.client.Delete(ctx, &umv1alpha1.UbuntuMachineClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim-a", Namespace: "tenant-a"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.reconciler.Reconcile(ctx, claimRequest("tenant-a", "claim-a")); err == nil {
		t.Fatal("expected the unreachable scrub to be retried, not swallowed")
	}
	if err := f.client.Get(ctx, client.ObjectKey{Name: "claim-a", Namespace: "tenant-a"}, &umv1alpha1.UbuntuMachineClaim{}); err != nil {
		t.Fatalf("claim must keep its finalizer until the scrub succeeds: %v", err)
	}
	if !strings.Contains(f.hosts["m1"].get(), injectTestKeyA) {
		t.Fatalf("precondition: key should still be on the unreachable host, got %q", f.hosts["m1"].get())
	}

	f.hosts["m1"].setUnreachable(false)
	if _, err := f.reconciler.Reconcile(ctx, claimRequest("tenant-a", "claim-a")); err != nil {
		t.Fatalf("retry after host came back: %v", err)
	}
	if err := f.client.Get(ctx, client.ObjectKey{Name: "claim-a", Namespace: "tenant-a"}, &umv1alpha1.UbuntuMachineClaim{}); !apierrors.IsNotFound(err) {
		t.Fatalf("claim should be gone after a successful scrub, got %v", err)
	}
	if got := f.hosts["m1"].get(); got != "" {
		t.Fatalf("scrub must leave only the admin key (none for password auth), got %q", got)
	}
	var gotM1 umv1alpha1.UbuntuMachine
	if err := f.client.Get(ctx, client.ObjectKey{Name: "m1"}, &gotM1); err != nil {
		t.Fatal(err)
	}
	if gotM1.Status.Phase != umv1alpha1.MachineAvailable || gotM1.Status.BoundClaimRef != nil {
		t.Fatalf("m1 should be Available with no owner: %+v", gotM1.Status)
	}
}

// TestReleaseOneScrubsOrphanExtras: releasing surplus owned machines scrubs
// their authorized_keys, not just the machine in claim status.
func TestReleaseOneScrubsOrphanExtras(t *testing.T) {
	ctx := context.Background()
	ref := &umv1alpha1.ClaimReference{Namespace: "tenant-a", Name: "claim-a"}
	m1 := injectTestMachine("m1", umv1alpha1.MachineBound, ref)
	m2 := injectTestMachine("m2", umv1alpha1.MachineBound, ref)
	claimA := claimWithFinalizer("tenant-a", "claim-a")
	claimA.Status = umv1alpha1.UbuntuMachineClaimStatus{
		Phase:             umv1alpha1.ClaimReady,
		Machine:           &umv1alpha1.BoundMachine{Name: "m1"},
		SSHSecretRef:      &corev1.LocalObjectReference{Name: "claim-a-ssh"},
		SSHKeyFingerprint: sshKeyFingerprint(injectTestKeyA),
	}
	f := newInjectTestFixture(t,
		[]*umv1alpha1.UbuntuMachine{m1, m2},
		claimA,
		injectTestSSHKey("tenant-a", "claim-a-sshkey", injectTestKeyA),
	)
	f.hosts["m1"].seed(injectTestKeyA + "\n")
	f.hosts["m2"].seed(injectTestKeyA + "\n") // stale orphan copy of the key

	if _, err := f.reconciler.Reconcile(ctx, claimRequest("tenant-a", "claim-a")); err != nil {
		t.Fatalf("adopt + release extras: %v", err)
	}
	if got := f.hosts["m2"].get(); got != "" {
		t.Fatalf("released extra m2 should be scrubbed, got %q", got)
	}
	if got := f.hosts["m1"].get(); got != injectTestKeyA+"\n" {
		t.Fatalf("kept machine m1 must still carry the claim key, got %q", got)
	}
	var gotM2 umv1alpha1.UbuntuMachine
	if err := f.client.Get(ctx, client.ObjectKey{Name: "m2"}, &gotM2); err != nil {
		t.Fatal(err)
	}
	if gotM2.Status.Phase != umv1alpha1.MachineAvailable || gotM2.Status.BoundClaimRef != nil {
		t.Fatalf("m2 should be Available with no owner: %+v", gotM2.Status)
	}
}

// TestValidateAuthorizedKeyLine (F-SK-1): the tenant-writable SSHKey
// publicKey is refused unless it is one clean authorized_keys line.
func TestValidateAuthorizedKeyLine(t *testing.T) {
	for _, tc := range []struct {
		name    string
		line    string
		wantErr bool
	}{
		{"valid", injectTestKeyA, false},
		{"valid with options", `command="true",restrict ` + injectTestKeyA + " comment", false},
		{"interior newline smuggles extra root lines", injectTestKeyA + "\nssh-rsa AAAA second", true},
		{"carriage return inside", injectTestKeyA + "\rrest", true},
		{"unparsable blob", "not-a-key at all", true},
		{"trailing text is the ssh comment", injectTestKeyA + " junk\x00more", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAuthorizedKeyLine(tc.line)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateAuthorizedKeyLine(%q) = %v, wantErr=%v", tc.line, err, tc.wantErr)
			}
		})
	}
}

// TestInvalidKeyLineBlocksInjection: a multi-line publicKey keeps the claim
// out of the machine instead of injecting several root lines.
func TestInvalidKeyLineBlocksInjection(t *testing.T) {
	ctx := context.Background()
	m1 := injectTestMachine("m1", umv1alpha1.MachineAvailable, nil)
	f := newInjectTestFixture(t,
		[]*umv1alpha1.UbuntuMachine{m1},
		claimWithFinalizer("tenant-a", "claim-a"),
		injectTestSSHKey("tenant-a", "claim-a-sshkey", injectTestKeyA+"\nssh-rsa AAAA second line"),
	)
	res, err := f.reconciler.Reconcile(ctx, claimRequest("tenant-a", "claim-a"))
	if err != nil {
		t.Fatalf("reconcile should requeue without error, got %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Fatal("expected a delayed requeue for the invalid key line")
	}
	if _, w := f.hosts["m1"].counts(); w != 0 {
		t.Fatalf("nothing should have been written for an invalid key, writes=%d", w)
	}
	claim := getClaim(t, f.client, "tenant-a", "claim-a")
	if claim.Status.Phase == umv1alpha1.ClaimReady {
		t.Fatal("claim must not report Ready with an uninjectable key")
	}
}

// dropped boundClaimRef, claim deletion still scrubs the machine recorded
// in status so the released host does not strand the tenant key.
func TestReleaseAllScrubsUnownedStatusMachine(t *testing.T) {
	ctx := context.Background()
	m1 := injectTestMachine("m1", umv1alpha1.MachineAvailable, nil)
	claimA := claimWithFinalizer("tenant-a", "claim-a")
	claimA.Status = umv1alpha1.UbuntuMachineClaimStatus{
		Phase:             umv1alpha1.ClaimReady,
		Machine:           &umv1alpha1.BoundMachine{Name: "m1"},
		SSHSecretRef:      &corev1.LocalObjectReference{Name: "claim-a-ssh"},
		SSHKeyFingerprint: sshKeyFingerprint(injectTestKeyC),
	}
	f := newInjectTestFixture(t,
		[]*umv1alpha1.UbuntuMachine{m1},
		claimA,
		injectTestSSHKey("tenant-a", "claim-a-sshkey", injectTestKeyC),
	)
	f.hosts["m1"].seed(injectTestKeyC + "\n")

	if err := f.client.Delete(ctx, &umv1alpha1.UbuntuMachineClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim-a", Namespace: "tenant-a"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.reconciler.Reconcile(ctx, claimRequest("tenant-a", "claim-a")); err != nil {
		t.Fatalf("release: %v", err)
	}
	if got := f.hosts["m1"].get(); got != "" {
		t.Fatalf("status machine should have been scrubbed, got %q", got)
	}
	if err := f.client.Get(ctx, client.ObjectKey{Name: "claim-a", Namespace: "tenant-a"}, &umv1alpha1.UbuntuMachineClaim{}); !apierrors.IsNotFound(err) {
		t.Fatalf("claim should be gone, got %v", err)
	}
}
