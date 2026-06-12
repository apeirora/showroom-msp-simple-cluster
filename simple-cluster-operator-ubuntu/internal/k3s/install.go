/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package k3s wraps the small set of remote actions that turn an
// Ubuntu host into a working k3s server or agent. Everything goes
// through one of:
//
//   InstallServerBootstrap  — first CP, `k3s server --cluster-init`
//   InstallServerJoin       — additional CP, joins via the bootstrap CP
//   InstallAgent            — workers
//   FetchToken              — read /var/lib/rancher/k3s/server/node-token
//   FetchKubeconfig         — read /etc/rancher/k3s/k3s.yaml + rewrite the API URL
//   UninstallServer / UninstallAgent — k3s-uninstall.sh / k3s-agent-uninstall.sh
//   IsInstalled             — `which k3s` probe
//   ReportedVersion         — `k3s --version` parsed
package k3s

import (
	"context"
	"fmt"
	"strings"

	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ubuntu/internal/ssh"
)

// InstallOptions carries the few knobs the reconciler needs to forward.
type InstallOptions struct {
	// Version is the k3s release tag (e.g. "v1.31.4+k3s1"). Empty = installer default.
	Version string
	// ExtraArgs are appended to the installer-script positional args
	// (i.e. they reach `k3s server` / `k3s agent` as flags).
	ExtraArgs []string
	// TLSSAN, when non-empty, is appended as `--tls-san=<value>` to
	// server installs so the API cert is valid for the LB/VIP address.
	TLSSAN string
	// NodeIP, when non-empty, becomes `--node-ip=<value>` (server and
	// agent) and `--advertise-address=<value>` (server only). Pins
	// kube-apiserver and kubelet to the host's internal address so
	// they don't fall back to localhost or the default route IP.
	NodeIP string
}

// InstallServerBootstrap runs `k3s server --cluster-init` on the first
// CP host. Idempotent: if k3s is already installed the call is a no-op
// returning a friendly message.
func InstallServerBootstrap(ctx context.Context, c *ssh.Client, opts InstallOptions) error {
	if installed, err := IsInstalled(ctx, c); err != nil {
		return err
	} else if installed {
		return nil
	}
	args := append([]string{"server", "--cluster-init"}, serverArgs(opts)...)
	return runInstallScript(ctx, c, opts, args, nil)
}

// InstallServerJoin runs `k3s server --server <bootstrap>` on subsequent
// CP hosts. Idempotent on already-installed hosts.
func InstallServerJoin(ctx context.Context, c *ssh.Client, opts InstallOptions, bootstrapURL, token string) error {
	if installed, err := IsInstalled(ctx, c); err != nil {
		return err
	} else if installed {
		return nil
	}
	args := append([]string{
		"server",
		"--server=" + bootstrapURL,
	}, serverArgs(opts)...)
	env := map[string]string{
		"K3S_TOKEN": token,
	}
	return runInstallScript(ctx, c, opts, args, env)
}

// InstallAgent runs the k3s agent installer on a worker host.
func InstallAgent(ctx context.Context, c *ssh.Client, opts InstallOptions, serverURL, token string) error {
	if installed, err := IsInstalled(ctx, c); err != nil {
		return err
	} else if installed {
		return nil
	}
	env := map[string]string{
		"K3S_URL":   serverURL,
		"K3S_TOKEN": token,
	}
	return runInstallScript(ctx, c, opts, agentArgs(opts), env)
}

// FetchToken reads /var/lib/rancher/k3s/server/node-token from the
// bootstrap CP. Must run after InstallServerBootstrap reports success.
func FetchToken(ctx context.Context, c *ssh.Client) (string, error) {
	out, err := c.MustSucceedSudo(ctx, "cat /var/lib/rancher/k3s/server/node-token")
	if err != nil {
		return "", fmt.Errorf("read k3s node-token: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// FetchKubeconfig reads /etc/rancher/k3s/k3s.yaml and rewrites the
// localhost API endpoint to the supplied externalEndpoint
// ("ip:port" — port is optional, default 6443).
func FetchKubeconfig(ctx context.Context, c *ssh.Client, externalEndpoint string) (string, error) {
	raw, err := c.MustSucceedSudo(ctx, "cat /etc/rancher/k3s/k3s.yaml")
	if err != nil {
		return "", fmt.Errorf("read k3s kubeconfig: %w", err)
	}
	target := externalEndpoint
	if !strings.Contains(target, ":") {
		target = target + ":6443"
	}
	return strings.ReplaceAll(raw, "https://127.0.0.1:6443", "https://"+target), nil
}

// UninstallServer runs /usr/local/bin/k3s-uninstall.sh. Missing script
// (k3s was never installed) is treated as success.
func UninstallServer(ctx context.Context, c *ssh.Client) error {
	res, err := c.RunSudo(ctx, "test -x /usr/local/bin/k3s-uninstall.sh && /usr/local/bin/k3s-uninstall.sh || true")
	if err != nil {
		return fmt.Errorf("uninstall k3s server: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("k3s-uninstall.sh exited %d: %s", res.ExitCode, res.Stderr)
	}
	return nil
}

// UninstallAgent runs /usr/local/bin/k3s-agent-uninstall.sh.
func UninstallAgent(ctx context.Context, c *ssh.Client) error {
	res, err := c.RunSudo(ctx, "test -x /usr/local/bin/k3s-agent-uninstall.sh && /usr/local/bin/k3s-agent-uninstall.sh || true")
	if err != nil {
		return fmt.Errorf("uninstall k3s agent: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("k3s-agent-uninstall.sh exited %d: %s", res.ExitCode, res.Stderr)
	}
	return nil
}

// IsInstalled probes the host for an existing k3s binary. Cheap and
// idempotent — every reconcile calls it.
func IsInstalled(ctx context.Context, c *ssh.Client) (bool, error) {
	res, err := c.Run(ctx, "command -v k3s")
	if err != nil {
		return false, err
	}
	return res.ExitCode == 0 && strings.TrimSpace(res.Stdout) != "", nil
}

// ReportedVersion runs `k3s --version` and parses the first numeric
// component. Returns "" on hosts where k3s is not installed.
func ReportedVersion(ctx context.Context, c *ssh.Client) (string, error) {
	res, err := c.Run(ctx, "k3s --version 2>/dev/null | head -1")
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", nil
	}
	// "k3s version v1.31.4+k3s1 (abc123)" — second "v…" word, not the
	// literal "version" token before it. The k3s tag always carries the
	// `+k3s` suffix; if we don't see it, fall back to whatever the
	// command printed (helps the next debugger).
	parts := strings.Fields(res.Stdout)
	for _, p := range parts {
		if strings.HasPrefix(p, "v") && strings.Contains(p, "+k3s") {
			return p, nil
		}
	}
	return strings.TrimSpace(res.Stdout), nil
}

// runInstallScript pipes get.k3s.io through bash with the supplied env
// (`INSTALL_K3S_VERSION` plus any caller env) and trailing positional
// args (forwarded to the chosen k3s subcommand).
func runInstallScript(ctx context.Context, c *ssh.Client, opts InstallOptions, args []string, env map[string]string) error {
	var b strings.Builder
	b.WriteString("set -euo pipefail; ")
	for k, v := range env {
		fmt.Fprintf(&b, "export %s=%q; ", k, v)
	}
	if opts.Version != "" {
		fmt.Fprintf(&b, "export INSTALL_K3S_VERSION=%q; ", opts.Version)
	}
	b.WriteString("curl -sfL https://get.k3s.io | sh -s -")
	for _, a := range args {
		fmt.Fprintf(&b, " %q", a)
	}
	_, err := c.MustSucceedSudo(ctx, "bash -c "+strconvQuote(b.String()))
	if err != nil {
		return fmt.Errorf("k3s install: %w", err)
	}
	return nil
}

// serverArgs builds the k3s server flag tail: user's ExtraArgs +
// --tls-san + --node-ip + --advertise-address when each is set.
func serverArgs(opts InstallOptions) []string {
	out := append([]string{}, opts.ExtraArgs...)
	if opts.TLSSAN != "" {
		out = append(out, "--tls-san="+opts.TLSSAN)
	}
	if opts.NodeIP != "" {
		out = append(out, "--node-ip="+opts.NodeIP, "--advertise-address="+opts.NodeIP)
	}
	return out
}

// agentArgs builds the k3s agent flag tail: user's ExtraArgs +
// --node-ip when set.
func agentArgs(opts InstallOptions) []string {
	out := append([]string{}, opts.ExtraArgs...)
	if opts.NodeIP != "" {
		out = append(out, "--node-ip="+opts.NodeIP)
	}
	return out
}

// strconvQuote is a tiny shell-quote — we cannot use strconv.Quote
// because bash strips Go's hex escapes.
func strconvQuote(s string) string {
	// Replace single quotes with the standard '\'' trick then wrap.
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
