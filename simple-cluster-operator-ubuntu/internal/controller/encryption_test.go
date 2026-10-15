/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	api "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ubuntu/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ubuntu/internal/openbao"
)

// Regressions from the 2026-10-15 adversarial review of PR 219. Each one
// failed on the first implementation.

func encryptedFixture() *api.UbuntuCluster {
	c := clusterFixture()
	c.Spec.Encryption = &api.EncryptionSpec{Provider: api.EncryptionProvider{Type: api.EncryptionProviderOpenBao}}
	return c
}

// A failed k3s install after a successful host preparation must persist the
// secret-ID accessor, or it can never be destroyed.
func TestPartialEncryptionStatusPersists(t *testing.T) {
	ctx := context.Background()
	cluster := encryptedFixture()
	cluster.Status.Encryption = &api.EncryptionStatus{Provider: api.EncryptionProviderOpenBao, KeyName: "account_cluster", RoleName: "account_cluster", KeyVersion: 1}
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(cluster).WithObjects(cluster).Build()
	r := &UbuntuClusterReconciler{Client: cl, Scheme: scheme}
	enc := &encryptionSession{keyName: "account_cluster", roleName: "account_cluster", keyVersion: 1, now: time.Now,
		hosts: map[string]api.HostEncryptionStatus{"cp": {Name: "cp", SecretIDAccessor: "acc-1"}}}
	if _, err := r.partialEnc(ctx, cluster, enc, "127.0.0.1", nil, 0, false); err != nil {
		t.Fatal(err)
	}
	var stored api.UbuntuCluster
	if err := cl.Get(ctx, client.ObjectKeyFromObject(cluster), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Status.Encryption == nil || len(stored.Status.Encryption.Hosts) != 1 || stored.Status.Encryption.Hosts[0].SecretIDAccessor != "acc-1" {
		t.Fatalf("host accessor lost after partialEnc: %+v", stored.Status.Encryption)
	}
}

// Two clusters in different namespaces must never share a key and role.
func TestKMSNamesSeparateNamespaces(t *testing.T) {
	a, b := clusterFixture(), clusterFixture()
	a.Namespace, a.Name = "team-a", "demo"
	b.Namespace, b.Name = "team", "a-demo"
	if kmsObjectName(a) == kmsObjectName(b) {
		t.Fatalf("different clusters share key and role: %s", kmsObjectName(a))
	}
	if strings.ContainsAny(kmsObjectName(a), " /\"'\\") {
		t.Fatalf("name must be a single clean path segment: %q", kmsObjectName(a))
	}
	// Recorded names win over the scheme.
	a.Status.Encryption = &api.EncryptionStatus{KeyName: "legacy-key", RoleName: "legacy-role"}
	k, r := kmsNames(a)
	if k != "legacy-key" || r != "legacy-role" {
		t.Fatalf("recorded names not honoured: %s %s", k, r)
	}
}

// Losing the OpenBao configuration under a Ready encrypted cluster must
// clear Ready and EncryptionReady, not leave them True next to Failed.
func TestEncryptionFailureClearsReady(t *testing.T) {
	ctx := context.Background()
	cluster := encryptedFixture()
	cluster.Status.Phase = api.PhaseReady
	cluster.Status.Conditions = []metav1.Condition{
		{Type: api.ConditionReady, Status: metav1.ConditionTrue, Reason: "Ready", LastTransitionTime: metav1.Now()},
		{Type: api.ConditionEncryptionReady, Status: metav1.ConditionTrue, Reason: "Encrypted", LastTransitionTime: metav1.Now()},
	}
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(cluster).WithObjects(cluster).Build()
	r := &UbuntuClusterReconciler{Client: cl, Scheme: scheme}
	if _, result, err := r.beginEncryption(ctx, cluster); result == nil || err != nil {
		t.Fatalf("unexpected result: %v %v", result, err)
	}
	var stored api.UbuntuCluster
	if err := cl.Get(ctx, client.ObjectKeyFromObject(cluster), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Status.Phase != api.PhaseFailed || apimeta.IsStatusConditionTrue(stored.Status.Conditions, api.ConditionReady) || apimeta.IsStatusConditionTrue(stored.Status.Conditions, api.ConditionEncryptionReady) {
		t.Fatalf("Phase=%s with stale conditions: %+v", stored.Status.Phase, stored.Status.Conditions)
	}
}

// fakeBao is the smallest OpenBao that lets beginEncryption log in and then
// fails the first object creation, to prove the names are persisted first.
func fakeBao(t *testing.T) (openbao.Config, *[]string) {
	calls := &[]string{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/v1/")
		*calls = append(*calls, r.Method+" "+path)
		switch {
		case strings.HasSuffix(path, "/login"):
			w.WriteHeader(200)
			_ = json.NewEncoder(w).Encode(map[string]any{"auth": map[string]any{"client_token": "hvs.op"}})
		case strings.HasPrefix(path, "simplecluster-kms/keys/"):
			w.WriteHeader(503)
			_ = json.NewEncoder(w).Encode(map[string]any{"errors": []string{"transit unavailable"}})
		default:
			w.WriteHeader(204)
		}
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.pem")
	_ = os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600)
	tok := filepath.Join(dir, "token")
	_ = os.WriteFile(tok, []byte("jwt"), 0o600)
	return openbao.Config{Address: srv.URL, Namespace: "simple-cluster", CAFile: ca, TokenFile: tok}, calls
}

// The cleanup invariant: status.encryption with the names exists before the
// first OpenBao object is created, so a cluster without it owns nothing and
// a cluster with it is always cleaned up.
func TestNamesPersistedBeforeFirstOpenBaoObject(t *testing.T) {
	ctx := context.Background()
	cfg, calls := fakeBao(t)
	cluster := encryptedFixture()
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(cluster).WithObjects(cluster).Build()
	r := &UbuntuClusterReconciler{Client: cl, Scheme: scheme, OpenBao: cfg}
	s, result, err := r.beginEncryption(ctx, cluster)
	if s != nil || result == nil || err != nil {
		t.Fatalf("expected a pending result, got session=%v result=%v err=%v", s, result, err)
	}
	var stored api.UbuntuCluster
	if err := cl.Get(ctx, client.ObjectKeyFromObject(cluster), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Status.Encryption == nil || stored.Status.Encryption.KeyName != kmsObjectName(cluster) || stored.Status.Encryption.RoleName != kmsObjectName(cluster) {
		t.Fatalf("names not persisted before key creation: %+v", stored.Status.Encryption)
	}
	if !apimeta.IsStatusConditionFalse(stored.Status.Conditions, api.ConditionEncryptionReady) {
		t.Fatalf("EncryptionReady must be False while pending: %+v", stored.Status.Conditions)
	}
	var sawKey bool
	for _, c := range *calls {
		if strings.Contains(c, "simplecluster-kms/keys/") {
			sawKey = true
		}
	}
	if !sawKey {
		t.Fatal("the fake never saw the key creation; test setup is wrong")
	}
	// And cleanup of a cluster that has status but no configuration fails
	// loudly instead of pretending success.
	r2 := &UbuntuClusterReconciler{Client: cl, Scheme: scheme}
	if err := r2.cleanupEncryption(ctx, &stored); err == nil {
		t.Fatal("cleanup must fail when objects may exist and OpenBao is not configured")
	}
	// Whereas a cluster that never got status owns nothing.
	fresh := encryptedFixture()
	if err := r2.cleanupEncryption(ctx, fresh); err != nil {
		t.Fatalf("cluster without status.encryption owns nothing: %v", err)
	}
}

// Secret IDs are replaced before the role TTL runs out; an accessor without
// an issue time is stamped, not renewed at once.
func TestRenewalDue(t *testing.T) {
	cfg := openbao.Config{Address: "https://bao", SecretIDTTL: 100 * time.Hour}
	cl, err := openbao.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	s := &encryptionSession{client: cl, now: func() time.Time { return base }}
	h := api.HostEncryptionStatus{Name: "cp"}
	if s.renewalDue(&h) {
		t.Fatal("no accessor, nothing to renew")
	}
	h.SecretIDAccessor = "acc"
	if s.renewalDue(&h) || h.SecretIDIssuedAt == nil {
		t.Fatal("unknown issue time must be stamped, not renewed")
	}
	s.now = func() time.Time { return base.Add(74 * time.Hour) }
	if s.renewalDue(&h) {
		t.Fatal("renewal before 75% of the TTL")
	}
	s.now = func() time.Time { return base.Add(76 * time.Hour) }
	if !s.renewalDue(&h) {
		t.Fatal("renewal due after 75% of the TTL")
	}
}
