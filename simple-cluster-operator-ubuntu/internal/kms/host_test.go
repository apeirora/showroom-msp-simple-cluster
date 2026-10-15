/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package kms

import (
	"encoding/base64"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The embedded script must be the canonical one, byte for byte, or the
// operator and the documented manual path drift apart.
func TestEmbeddedScriptMatchesCanonical(t *testing.T) {
	canon, err := os.ReadFile("../../../hack/openbao-kms/install.sh")
	if err != nil {
		t.Skipf("canonical script not available: %v", err)
	}
	if string(canon) != installScript {
		t.Fatal("internal/kms/scripts/install.sh differs from hack/openbao-kms/install.sh; copy it over")
	}
}

// Every key Payload can emit must be in install.sh's allowlist, and every
// required key of the script must be one Payload emits.
func TestPayloadKeysMatchScriptAllowlist(t *testing.T) {
	re := regexp.MustCompile(`(?m)^\s*(address\|[A-Za-z|]+)\) ;;`)
	m := re.FindStringSubmatch(installScript)
	if m == nil {
		t.Fatal("cannot find the input allowlist in install.sh")
	}
	allowed := map[string]bool{}
	for _, k := range strings.Split(m[1], "|") {
		allowed[k] = true
	}
	full := Input{
		Address: "a", Namespace: "n", TransitMount: "t", TransitKey: "k", ApproleMount: "m",
		ApproleRole: "r", RoleID: "id", SecretIDWrapped: "w", CACert: "c",
	}
	for _, line := range strings.Split(strings.TrimSpace(string(full.Payload())), "\n") {
		key := line[:strings.IndexByte(line, '=')]
		if !allowed[key] {
			t.Errorf("payload key %q is not accepted by install.sh", key)
		}
	}
	for _, req := range []string{"address", "transitMount", "transitKey", "approleMount", "roleID", "secretIDWrapped"} {
		if !strings.Contains(string(full.Payload()), req+"=") {
			t.Errorf("payload lacks required key %q", req)
		}
	}
}

func TestPayloadEncodesAndOmitsEmpty(t *testing.T) {
	p := string(Input{Address: "https://bao.example", TransitMount: "tm", TransitKey: "c1", ApproleMount: "am", RoleID: "rid", SecretIDWrapped: "hvs.wrap"}.Payload())
	want := "address=" + base64.StdEncoding.EncodeToString([]byte("https://bao.example")) + "\n"
	if !strings.HasPrefix(p, want) {
		t.Fatalf("payload does not start with the encoded address:\n%s", p)
	}
	if strings.Contains(p, "namespace=") || strings.Contains(p, "caCert=") {
		t.Fatalf("empty fields must be omitted:\n%s", p)
	}
	if !strings.HasSuffix(p, "\n") {
		t.Fatal("payload must end with a newline so the last line is read")
	}
}

func TestAccessorParsing(t *testing.T) {
	out := "unwrapped a single-use secret ID; its accessor is 1a2b-3c4d\nvault-kubernetes-kms v1.4.0 installed"
	m := accessorRe.FindStringSubmatch(out)
	if m == nil || m[1] != "1a2b-3c4d" {
		t.Fatalf("accessor not parsed from %q", out)
	}
}
