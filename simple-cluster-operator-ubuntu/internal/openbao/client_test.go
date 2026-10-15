/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package openbao

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fake is a tiny OpenBao that records what the client sends and enforces
// the parts of the operator policy a unit test can see.
type fake struct {
	t        *testing.T
	calls    []string
	bodies   map[string]map[string]any
	keyState map[string]any
}

func (f *fake) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/v1/")
		f.calls = append(f.calls, r.Method+" "+path)
		if r.Header.Get("X-Vault-Namespace") != "simple-cluster" {
			http.Error(w, `{"errors":["missing namespace"]}`, 400)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &body)
			f.bodies[r.Method+" "+path] = body
		}
		write := func(code int, v any) {
			w.WriteHeader(code)
			if v != nil {
				_ = json.NewEncoder(w).Encode(v)
			}
		}
		if path != "auth/simplecluster-kms-jwt/login" && r.Header.Get("X-Vault-Token") != "hvs.operator" {
			write(403, map[string]any{"errors": []string{"permission denied"}})
			return
		}
		switch {
		case path == "auth/simplecluster-kms-jwt/login":
			if body["role"] != "simplecluster-operator" || body["jwt"] != "sa-jwt" {
				write(400, map[string]any{"errors": []string{"bad login"}})
				return
			}
			write(200, map[string]any{"auth": map[string]any{"client_token": "hvs.operator"}})
		case path == "auth/token/revoke-self":
			write(204, nil)
		case r.Method == "POST" && path == "simplecluster-kms/keys/ns1-c1":
			if body["exportable"] != false || body["type"] != "aes256-gcm96" {
				write(400, map[string]any{"errors": []string{"bad key params"}})
				return
			}
			write(204, nil)
		case r.Method == "GET" && path == "simplecluster-kms/keys/ns1-c1":
			write(200, map[string]any{"data": f.keyState})
		case r.Method == "POST" && path == "auth/simplecluster-kms-approle/role/ns1-c1":
			// The real ACL pins token_policies to the plugin policy and
			// refuses the deprecated "policies" field.
			if body["token_policies"] != "sc-kms-plugin" {
				write(403, map[string]any{"errors": []string{"permission denied: token_policies"}})
				return
			}
			if _, has := body["policies"]; has {
				write(403, map[string]any{"errors": []string{"permission denied: policies"}})
				return
			}
			write(204, nil)
		case r.Method == "GET" && path == "auth/simplecluster-kms-approle/role/ns1-c1/role-id":
			write(200, map[string]any{"data": map[string]any{"role_id": "rid-1"}})
		case r.Method == "POST" && path == "auth/simplecluster-kms-approle/role/ns1-c1/secret-id":
			if r.Header.Get("X-Vault-Wrap-TTL") != "300s" {
				write(400, map[string]any{"errors": []string{"not wrapped"}})
				return
			}
			write(200, map[string]any{"wrap_info": map[string]any{"token": "hvs.wrap-1"}})
		case r.Method == "POST" && path == "auth/simplecluster-kms-approle/role/ns1-c1/secret-id-accessor/destroy":
			if body["secret_id_accessor"] == "gone" {
				write(404, map[string]any{"errors": []string{}})
				return
			}
			write(204, nil)
		case r.Method == "DELETE" && path == "auth/simplecluster-kms-approle/role/ns1-c1":
			write(204, nil)
		default:
			write(404, map[string]any{"errors": []string{"no handler for " + r.Method + " " + path}})
		}
	})
}

func newTestClient(t *testing.T, f *fake) (*Client, *httptest.Server) {
	srv := httptest.NewTLSServer(f.handler())
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	tok := filepath.Join(dir, "token")
	if err := os.WriteFile(tok, []byte("sa-jwt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := New(Config{Address: srv.URL, Namespace: "simple-cluster", CAFile: ca, TokenFile: tok})
	if err != nil {
		t.Fatal(err)
	}
	return c, srv
}

func TestNewRejectsPlainHTTPAndBadValues(t *testing.T) {
	if _, err := New(Config{Address: "http://bao"}); err == nil {
		t.Fatal("http:// must be refused")
	}
	if _, err := New(Config{Address: "https://bao", Namespace: "a b"}); err == nil {
		t.Fatal("whitespace in a value must be refused")
	}
	if _, err := New(Config{Address: "https://bao", CAFile: "/nonexistent"}); err == nil {
		t.Fatal("missing CA file must be refused")
	}
}

func TestDefaults(t *testing.T) {
	c := Config{Address: "https://bao"}.withDefaults()
	if c.JWTMount != DefaultJWTMount || c.TransitMount != DefaultTransitMount || c.WrapTTL != 5*time.Minute || c.PluginPolicy != DefaultPluginPolicy {
		t.Fatalf("defaults not applied: %+v", c)
	}
	if (Config{}).Enabled() || !c.Enabled() {
		t.Fatal("Enabled must follow Address")
	}
}

func TestClusterLifecycle(t *testing.T) {
	f := &fake{t: t, bodies: map[string]map[string]any{},
		keyState: map[string]any{"latest_version": float64(3), "exportable": false, "soft_deleted": false}}
	c, _ := newTestClient(t, f)
	ctx := context.Background()
	if err := c.Login(ctx); err != nil {
		t.Fatal(err)
	}
	if c.CAPEM() == "" || !strings.HasPrefix(c.CAPEM(), "-----BEGIN CERTIFICATE-----") {
		t.Fatal("CAPEM must return the PEM handed to hosts")
	}
	ver, err := c.EnsureTransitKey(ctx, "ns1-c1", 720*time.Hour)
	if err != nil || ver != 3 {
		t.Fatalf("EnsureTransitKey: ver=%d err=%v", ver, err)
	}
	if got := f.bodies["POST simplecluster-kms/keys/ns1-c1"]["auto_rotate_period"]; got != "720h0m0s" {
		t.Fatalf("auto_rotate_period not sent: %v", got)
	}
	if err := c.EnsureRole(ctx, "ns1-c1"); err != nil {
		t.Fatal(err)
	}
	rb := f.bodies["POST auth/simplecluster-kms-approle/role/ns1-c1"]
	if rb["secret_id_num_uses"] != float64(0) || rb["token_period"] != float64(3600) || rb["secret_id_ttl"] != float64(90*24*3600) {
		t.Fatalf("role params: %v", rb)
	}
	rid, err := c.RoleID(ctx, "ns1-c1")
	if err != nil || rid != "rid-1" {
		t.Fatalf("RoleID: %q %v", rid, err)
	}
	wrap, err := c.MintWrappedSecretID(ctx, "ns1-c1", map[string]string{"host": "cp-0"})
	if err != nil || wrap != "hvs.wrap-1" {
		t.Fatalf("MintWrappedSecretID: %q %v", wrap, err)
	}
	md, _ := f.bodies["POST auth/simplecluster-kms-approle/role/ns1-c1/secret-id"]["metadata"].(string)
	if !strings.Contains(md, `"host":"cp-0"`) {
		t.Fatalf("metadata not attached: %q", md)
	}
	if err := c.DestroySecretIDAccessor(ctx, "ns1-c1", "acc-1"); err != nil {
		t.Fatal(err)
	}
	if err := c.DestroySecretIDAccessor(ctx, "ns1-c1", "gone"); err != nil {
		t.Fatalf("404 on destroy must be tolerated: %v", err)
	}
	if err := c.DeleteRole(ctx, "ns1-c1"); err != nil {
		t.Fatal(err)
	}
	if err := c.RevokeSelf(ctx); err != nil {
		t.Fatal(err)
	}
	for _, call := range f.calls {
		if strings.Contains(call, "secret-id/") || strings.Contains(call, "unwrap") {
			t.Fatalf("the operator must never unwrap or read a secret ID: %s", call)
		}
	}
}

func TestEnsureTransitKeyRefusesSoftDeletedAndExportable(t *testing.T) {
	for _, state := range []map[string]any{
		{"latest_version": float64(1), "soft_deleted": true},
		{"latest_version": float64(1), "exportable": true},
	} {
		f := &fake{t: t, bodies: map[string]map[string]any{}, keyState: state}
		c, _ := newTestClient(t, f)
		ctx := context.Background()
		if err := c.Login(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := c.EnsureTransitKey(ctx, "ns1-c1", 0); err == nil {
			t.Fatalf("key state %v must be refused", state)
		}
	}
}

func TestLoginFailureIsReported(t *testing.T) {
	f := &fake{t: t, bodies: map[string]map[string]any{}}
	c, _ := newTestClient(t, f)
	// wrong token file content
	dir := t.TempDir()
	tok := filepath.Join(dir, "token")
	_ = os.WriteFile(tok, []byte("other"), 0o600)
	c.cfg.TokenFile = tok
	err := c.Login(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP 400") {
		t.Fatalf("expected a 400 login error, got %v", err)
	}
	var ae *APIError
	if !asAPIError(err, &ae) || ae.Status != 400 {
		t.Fatalf("APIError not wrapped: %v", err)
	}
}

func asAPIError(err error, target **APIError) bool {
	return errors.As(err, target)
}
