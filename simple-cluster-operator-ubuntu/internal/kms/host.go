/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package kms prepares a control-plane host for OpenBao KMS encryption the
// way hack/openbao-kms/push-to-host.sh does by hand: it streams
// hack/openbao-kms/install.sh to the host over SSH and feeds it the
// connection data on stdin, so nothing lands in argv, the journal or a file
// the login user can read. The script is embedded from a copy kept
// identical to the canonical one by a test.
package kms

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ubuntu/internal/ssh"
)

//go:embed scripts/install.sh
var installScript string

// InstallScript is the embedded hack/openbao-kms/install.sh.
func InstallScript() string { return installScript }

// Paths install.sh writes; checked to decide whether a host is prepared.
const (
	K3sSnippetPath = "/etc/rancher/k3s/config.yaml.d/90-vault-kms.yaml"
	EnvFilePath    = "/etc/vault-kms/env"
	PluginUnit     = "vault-kubernetes-kms"
)

// Input is the connection data install.sh reads: one `key=<base64>` line per
// field. Keys must stay in step with the script's allowlist (tested).
type Input struct {
	Address         string
	Namespace       string
	TransitMount    string
	TransitKey      string
	ApproleMount    string
	ApproleRole     string
	RoleID          string
	SecretIDWrapped string
	CACert          string
}

// Payload renders the stdin lines, base64 per value, empty fields omitted.
func (in Input) Payload() []byte {
	var b bytes.Buffer
	add := func(k, v string) {
		if v == "" {
			return
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(base64.StdEncoding.EncodeToString([]byte(v)))
		b.WriteByte('\n')
	}
	add("address", in.Address)
	add("namespace", in.Namespace)
	add("transitMount", in.TransitMount)
	add("transitKey", in.TransitKey)
	add("approleMount", in.ApproleMount)
	add("approleRole", in.ApproleRole)
	add("roleID", in.RoleID)
	add("secretIDWrapped", in.SecretIDWrapped)
	add("caCert", in.CACert)
	return b.Bytes()
}

var accessorRe = regexp.MustCompile(`its accessor is (\S+)`)

// Prepare installs or updates the plugin on the host, before k3s. It
// returns the accessor of the secret ID the host unwrapped ("" when the
// script kept an existing secret ID) and the script's output.
func Prepare(ctx context.Context, c *ssh.Client, in Input) (accessor, output string, err error) {
	dir, err := c.RunWithStdin(ctx, `d=$(mktemp -d) && cat > "$d/install.sh" && echo "$d"`, strings.NewReader(installScript))
	if err != nil {
		return "", "", fmt.Errorf("upload install.sh: %w", err)
	}
	if dir.ExitCode != 0 {
		return "", "", fmt.Errorf("upload install.sh: exit %d: %s", dir.ExitCode, strings.TrimSpace(dir.Stderr))
	}
	remote := strings.TrimSpace(dir.Stdout)
	if remote == "" || strings.ContainsAny(remote, " \t\n'\"") {
		return "", "", fmt.Errorf("upload install.sh: unexpected temp dir %q", remote)
	}
	res, err := c.RunWithStdin(ctx,
		fmt.Sprintf(`sudo -n bash '%s/install.sh'; rc=$?; rm -rf '%s'; exit $rc`, remote, remote),
		bytes.NewReader(in.Payload()))
	if err != nil {
		return "", "", fmt.Errorf("run install.sh: %w", err)
	}
	output = strings.TrimSpace(res.Stdout)
	// The script prints the accessor right after it unwrapped the secret
	// ID, before anything that can still fail. Parse it first so a failed
	// run never leaves the operator without the handle to destroy it.
	if m := accessorRe.FindStringSubmatch(output); m != nil {
		accessor = m[1]
	}
	if res.ExitCode != 0 {
		return accessor, output, fmt.Errorf("install.sh exited %d: %s", res.ExitCode, strings.TrimSpace(res.Stderr+"\n"+res.Stdout))
	}
	return accessor, output, nil
}

// IsPrepared reports whether install.sh completed on this host: the k3s
// snippet and the env file exist and the plugin unit is enabled (the
// script's last steps). Used to avoid minting a secret ID on every retry
// and to refuse a running server that never had the plugin. It says nothing
// about WHICH cluster the host was prepared for: see MatchesCluster.
func IsPrepared(ctx context.Context, c *ssh.Client) (bool, error) {
	res, err := c.RunSudo(ctx, fmt.Sprintf("test -f %s -a -f %s && systemctl is-enabled --quiet %s", K3sSnippetPath, EnvFilePath, PluginUnit))
	if err != nil {
		return false, err
	}
	return res.ExitCode == 0, nil
}

// MatchesCluster reports whether a prepared host carries THIS cluster's
// connection settings: OpenBao address, namespace, Transit key and role ID.
// It compares by exit status of fixed-string greps on the root-only env
// file; no file content travels back, and the secret ID is never matched.
// A reused pool host still configured for an earlier cluster fails this.
func MatchesCluster(ctx context.Context, c *ssh.Client, in Input) (bool, error) {
	want := []string{
		"VAULT_KMS_VAULT_ADDR=" + in.Address,
		"VAULT_KMS_TRANSIT_KEY=" + in.TransitKey,
		"VAULT_KMS_APPROLE_ROLE_ID=" + in.RoleID,
		"VAULT_KMS_TRANSIT_MOUNT=" + in.TransitMount,
		"VAULT_KMS_APPROLE_MOUNT=" + in.ApproleMount,
	}
	if in.Namespace != "" {
		want = append(want, "VAULT_KMS_VAULT_NAMESPACE="+in.Namespace)
	}
	var b strings.Builder
	for i, w := range want {
		if strings.ContainsAny(w, "'\n") {
			return false, fmt.Errorf("refusing to compare a value with quotes or newlines")
		}
		if i > 0 {
			b.WriteString(" && ")
		}
		fmt.Fprintf(&b, "grep -qxF -- '%s' %s", w, EnvFilePath)
	}
	res, err := c.RunSudo(ctx, "sh -c "+shellQuote(b.String()))
	if err != nil {
		return false, err
	}
	return res.ExitCode == 0, nil
}

// shellQuote wraps s in single quotes for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Verify is the fail-closed check after k3s starts: the plugin unit is
// active and the kube-apiserver's kms-providers health check is ok. Without
// both, Secret writes would fail or, worse, the cluster would be running
// without the plugin.
func Verify(ctx context.Context, c *ssh.Client) error {
	if _, err := c.MustSucceedSudo(ctx, "systemctl is-active --quiet "+PluginUnit); err != nil {
		return fmt.Errorf("plugin unit %s is not active: %w", PluginUnit, err)
	}
	out, err := c.MustSucceedSudo(ctx, "k3s kubectl get --raw='/readyz?verbose'")
	if err != nil {
		return fmt.Errorf("kube-apiserver readyz: %w", err)
	}
	if !strings.Contains(out, "[+]kms-providers ok") {
		return fmt.Errorf("kube-apiserver does not report kms-providers ok")
	}
	return nil
}
