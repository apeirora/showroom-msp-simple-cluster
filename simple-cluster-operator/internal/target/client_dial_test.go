/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Dial-safety tests for target.New (F-KC-01, F-KC-02).
//
// The dialed kubeconfig lives in a tenant-writable Secret, so the operator
// must not execute what it finds there: an exec credential plugin in those
// bytes used to be fork+exec'd inside the operator pod on every discovery
// round, and the server URL was dialed unchecked (any scheme, any host).

package target

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/secretpolicy"
)

// dialRecorder is what the fake target cluster (a possible attacker
// listener) saw on the wire.
type dialRecorder struct {
	mu     sync.Mutex
	paths  []string
	authed []string
}

func (rec *dialRecorder) count() int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return len(rec.paths)
}

func (rec *dialRecorder) sawAuth() bool {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, a := range rec.authed {
		if a != "" {
			return true
		}
	}
	return false
}

// discoveryServer answers the GETs GetAPIGroupResources issues so target.New
// can complete, recording method/path/auth for every request. When delay is
// non-zero every response waits that long first (slow-endpoint tests).
func discoveryServer(t *testing.T, delay time.Duration) (*httptest.Server, *dialRecorder) {
	t.Helper()
	rec := &dialRecorder{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		rec.mu.Lock()
		rec.paths = append(rec.paths, r.URL.Path)
		rec.authed = append(rec.authed, r.Header.Get("Authorization"))
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api":
			_, _ = w.Write([]byte(`{"kind":"APIVersions","apiVersion":"v1","versions":["v1"],"serverAddressByClientCIDRs":[{"clientCIDR":"0.0.0.0/0","serverAddress":"127.0.0.1"}]}`))
		case "/apis":
			_, _ = w.Write([]byte(`{"kind":"APIGroupList","apiVersion":"v1","groups":[]}`))
		case "/api/v1":
			_, _ = w.Write([]byte(`{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"v1","resources":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"kind":"Status","status":"Failure","code":404}`))
		}
	})
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	return srv, rec
}

// dialKubeconfig assembles a minimal single-context kubeconfig. clusterExtra
// and userExtra are pre-indented YAML lines.
func dialKubeconfig(server, clusterExtra, userExtra string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: t
  cluster:
    server: %s
%scontexts:
- name: t
  context:
    cluster: t
    user: t
current-context: t
users:
- name: t
  user:
%s`, server, clusterExtra, userExtra)
}

const (
	skipVerifyExtra = "    insecure-skip-tls-verify: true\n"
	tokenUser       = "    token: tenant-wire-token\n"
)

// dialTenantSecret runs target.New exactly as the controller does (ref
// namespace == ownerNamespace == sc.Namespace) against the given kubeconfig.
func dialTenantSecret(t *testing.T, kubeconfig string, opts DialOptions) (*Client, error) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-ns", Name: "sc-kubeconfig"},
		Data:       map[string][]byte{"kubeconfig": []byte(kubeconfig)},
	}
	mgmt := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	return New(context.Background(), mgmt, nil, nil,
		&corev1.SecretReference{Namespace: "tenant-ns", Name: "sc-kubeconfig"},
		"tenant-ns", secretpolicy.Policy{}, opts)
}

// TestNewDropsExecCredentialPlugin (F-KC-01): an exec plugin in a
// tenant-writable kubeconfig must never run. Before the fix client-go forked
// it into the operator pod during discovery; now the provider is dropped
// before any transport is built, so discovery stays anonymous.
func TestNewDropsExecCredentialPlugin(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	srv, rec := discoveryServer(t, 0)
	kubeconfig := dialKubeconfig(srv.URL, skipVerifyExtra, `    exec:
      apiVersion: client.authentication.k8s.io/v1
      interactiveMode: Never
      command: /bin/sh
      args: ["-c", "touch \"$DIAL_TEST_MARKER\"; printf '{\"kind\":\"ExecCredential\",\"apiVersion\":\"client.authentication.k8s.io/v1\",\"status\":{\"token\":\"poc-token\"}}'"]
      env:
      - name: DIAL_TEST_MARKER
        value: `+marker)

	c, err := dialTenantSecret(t, kubeconfig, DialOptions{})
	if err != nil {
		t.Fatalf("exec-only kubeconfig rejected: %v", err)
	}
	if c.RESTConfig.ExecProvider != nil {
		t.Error("ExecProvider survived; client-go would fork the plugin into the operator pod")
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Error("exec plugin ran despite being disabled")
	}
	if rec.count() == 0 {
		t.Fatal("discovery never reached the dialed endpoint")
	}
	if rec.sawAuth() {
		t.Error("dropping the exec plugin must still dial, just anonymously")
	}
}

// TestNewHangingExecPluginDoesNotBlock (F-KC-04 via F-KC-01): a plugin that
// never exits used to wedge cmd.Run() inside the single reconcile worker.
// With exec disabled the construction must not wait for it at all.
func TestNewHangingExecPluginDoesNotBlock(t *testing.T) {
	srv, _ := discoveryServer(t, 0)
	kubeconfig := dialKubeconfig(srv.URL, skipVerifyExtra, `    exec:
      apiVersion: client.authentication.k8s.io/v1
      interactiveMode: Never
      command: /bin/sleep
      args: ["60"]`)

	start := time.Now()
	if _, err := dialTenantSecret(t, kubeconfig, DialOptions{}); err != nil {
		t.Fatalf("hanging plugin must not fail construction: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("construction blocked on the plugin for %v", elapsed)
	}
}

// TestNewRejectsNonHTTPSServer (F-KC-02): the server URL is validated before
// dialing; plaintext http is refused and nothing leaves the pod.
func TestNewRejectsNonHTTPSServer(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(plain.Close)
	_, err := dialTenantSecret(t, dialKubeconfig(plain.URL, "", tokenUser), DialOptions{})
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("http:// server: expected https-only rejection, got %v", err)
	}
}

// TestNewServerAllowlist: the host allowlist is optional (empty is
// permissive, so existing deployments keep working) and exact on the URL
// hostname, ports ignored.
func TestNewServerAllowlist(t *testing.T) {
	srv, rec := discoveryServer(t, 0)
	base := dialKubeconfig(srv.URL, skipVerifyExtra, tokenUser)
	for _, tc := range []struct {
		label     string
		allowlist []string
		wantErr   bool
	}{
		{label: "unset is permissive"},
		{label: "exact host", allowlist: []string{"127.0.0.1"}},
		{label: "case-insensitive", allowlist: []string{"Localhost", "127.0.0.1"}},
		{label: "other host rejected", allowlist: []string{"api.example.com"}, wantErr: true},
	} {
		t.Run(tc.label, func(t *testing.T) {
			before := rec.count()
			_, err := dialTenantSecret(t, base, DialOptions{ServerAllowlist: tc.allowlist})
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "allowlist") {
					t.Fatalf("expected allowlist rejection, got %v", err)
				}
				if rec.count() != before {
					t.Fatal("denied server was dialed before the allowlist check")
				}
				return
			}
			if err != nil {
				t.Fatalf("allowed server rejected: %v", err)
			}
		})
	}
}

// TestNewHonoursTenantTLSOptions: https-only fixes scheme and host, not
// trust. insecure-skip-tls-verify from the tenant kubeconfig keeps working
// (the server above is self-signed), and without it verification still
// applies.
func TestNewHonoursTenantTLSOptions(t *testing.T) {
	srv, _ := discoveryServer(t, 0)
	if _, err := dialTenantSecret(t, dialKubeconfig(srv.URL, skipVerifyExtra, tokenUser), DialOptions{}); err != nil {
		t.Fatalf("insecure-skip-tls-verify not honoured: %v", err)
	}
	_, err := dialTenantSecret(t, dialKubeconfig(srv.URL, "", tokenUser), DialOptions{})
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("expected trust failure without skip-verify, got %v", err)
	}
}

// TestNewDiscoveryHonoursDialTimeout (F-KC-04): the SimpleCluster controller
// runs a single reconcile worker by default, so an endpoint that accepts the
// connection and never answers must not hold it open forever. Both the
// construction context and rest.Config.Timeout are bounded.
func TestNewDiscoveryHonoursDialTimeout(t *testing.T) {
	srv, _ := discoveryServer(t, 2*time.Second)
	base := dialKubeconfig(srv.URL, skipVerifyExtra, tokenUser)

	_, err := dialTenantSecret(t, base, DialOptions{Timeout: 150 * time.Millisecond})
	if err == nil {
		t.Fatal("slow endpoint did not time out")
	}
	if !strings.Contains(err.Error(), "deadline exceeded") && !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected a timeout failure, got %v", err)
	}

	// The default must be set, not zero: rest.Config.Timeout 0 means the
	// tenant kubeconfig alone decides, and tenants may omit timeout.
	fast, _ := discoveryServer(t, 0)
	fastCfg := dialKubeconfig(fast.URL, skipVerifyExtra, tokenUser)
	c, err := dialTenantSecret(t, fastCfg, DialOptions{})
	if err != nil {
		t.Fatalf("default-timeout construction failed: %v", err)
	}
	if c.RESTConfig.Timeout != DefaultDialTimeout {
		t.Errorf("rest.Config.Timeout = %v, want default %v", c.RESTConfig.Timeout, DefaultDialTimeout)
	}

	// An explicit option overrides the default.
	c, err = dialTenantSecret(t, fastCfg, DialOptions{Timeout: 7 * time.Second})
	if err != nil {
		t.Fatalf("explicit-timeout construction failed: %v", err)
	}
	if c.RESTConfig.Timeout != 7*time.Second {
		t.Errorf("rest.Config.Timeout = %v, want 7s", c.RESTConfig.Timeout)
	}
}
