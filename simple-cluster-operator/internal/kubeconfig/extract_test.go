/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package kubeconfig

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"testing"
	"time"
)

func makeCertPEM(t *testing.T, notBefore, notAfter time.Time) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func makeJWT(payload map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	body, _ := json.Marshal(payload)
	bodyEnc := base64.RawURLEncoding.EncodeToString(body)
	return header + "." + bodyEnc + ".sig"
}

func kubeconfigYAML(authBlock string) []byte {
	return []byte(fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: ctx
contexts:
- name: ctx
  context:
    cluster: c
    user: u
clusters:
- name: c
  cluster:
    server: https://example.invalid
users:
- name: u
  user:
%s
`, authBlock))
}

func TestTokenTimes_X509Cert(t *testing.T) {
	notBefore := time.Now().Add(-1 * time.Hour).UTC().Truncate(time.Second)
	notAfter := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	certPEM := makeCertPEM(t, notBefore, notAfter)
	certB64 := base64.StdEncoding.EncodeToString(certPEM)
	kubeconfig := kubeconfigYAML(fmt.Sprintf("    client-certificate-data: %s", certB64))

	iss, exp, err := TokenTimes(kubeconfig)
	if err != nil {
		t.Fatalf("TokenTimes: %v", err)
	}
	if iss == nil || !iss.Equal(notBefore) {
		t.Errorf("issuedAt=%v want %v", iss, notBefore)
	}
	if exp == nil || !exp.Equal(notAfter) {
		t.Errorf("expiresAt=%v want %v", exp, notAfter)
	}
}

func TestTokenTimes_JWT(t *testing.T) {
	iat := time.Now().Add(-10 * time.Minute).Unix()
	expUnix := time.Now().Add(2 * time.Hour).Unix()
	token := makeJWT(map[string]any{"iat": iat, "exp": expUnix})
	kubeconfig := kubeconfigYAML(fmt.Sprintf("    token: %s", token))

	iss, exp, err := TokenTimes(kubeconfig)
	if err != nil {
		t.Fatalf("TokenTimes: %v", err)
	}
	if iss == nil || iss.Unix() != iat {
		t.Errorf("issuedAt=%v want unix %d", iss, iat)
	}
	if exp == nil || exp.Unix() != expUnix {
		t.Errorf("expiresAt=%v want unix %d", exp, expUnix)
	}
}

func TestTokenTimes_OpaqueToken(t *testing.T) {
	// Static token (not a JWT). TokenTimes should return all-nil with no error.
	kubeconfig := kubeconfigYAML("    token: not-a-jwt")
	iss, exp, err := TokenTimes(kubeconfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if iss != nil || exp != nil {
		t.Errorf("expected nil times for opaque token, got iss=%v exp=%v", iss, exp)
	}
}

func TestTokenTimes_NoCredentials(t *testing.T) {
	kubeconfig := kubeconfigYAML("    username: u\n    password: p")
	iss, exp, err := TokenTimes(kubeconfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if iss != nil || exp != nil {
		t.Errorf("expected nil times for basic auth, got iss=%v exp=%v", iss, exp)
	}
}

func TestTokenTimes_MalformedKubeconfig(t *testing.T) {
	_, _, err := TokenTimes([]byte("not yaml: :::"))
	if err == nil {
		t.Fatal("expected parse error for malformed kubeconfig")
	}
}
