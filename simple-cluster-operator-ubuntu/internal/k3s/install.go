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
//   IsServerRunning         — `systemctl is-active k3s` (server unit up)
//   IsAgentRunning          — `systemctl is-active k3s-agent` (agent unit up)
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
	// NodeExternalIP, when non-empty, becomes `--node-external-ip=<value>`
	// (server and agent). Registers the host's public address on the Node
	// so k3s ServiceLB advertises it in LoadBalancer Service status —
	// without it, Services on hosts with floating IPs publish only the
	// private address and stay unreachable from outside the provider network.
	NodeExternalIP string
	// NodeName, when non-empty, becomes `--node-name=<value>` (server and
	// agent). Pins the Kubernetes Node name to the host's logical name
	// instead of letting k3s fall back to the OS hostname — some provider
	// images default every VM's hostname to "localhost", which makes the
	// second node fail to register ("duplicate hostname, node password
	// rejected").
	NodeName string
}

// InstallServerBootstrap runs `k3s server --cluster-init` on the first
// CP host. Skipped only when the k3s server unit is already active — a
// partial install (binary present, server never started) is re-run, since
// get.k3s.io is idempotent and completes it.
func InstallServerBootstrap(ctx context.Context, c *ssh.Client, opts InstallOptions) error {
	if running, err := IsServerRunning(ctx, c); err != nil {
		return err
	} else if running {
		return nil
	}
	args := append([]string{"server", "--cluster-init"}, serverArgs(opts)...)
	return runInstallScript(ctx, c, opts, args, nil)
}

// InstallServerJoin runs `k3s server --server <bootstrap>` on subsequent
// CP hosts. Skipped only when the k3s server unit is already active;
// a partial install is re-run.
func InstallServerJoin(ctx context.Context, c *ssh.Client, opts InstallOptions, bootstrapURL, token string) error {
	if running, err := IsServerRunning(ctx, c); err != nil {
		return err
	} else if running {
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

// InstallAgent runs the k3s agent installer on a worker host. Skipped only
// when the k3s-agent unit is already active; a partial install is re-run.
func InstallAgent(ctx context.Context, c *ssh.Client, opts InstallOptions, serverURL, token string) error {
	if running, err := IsAgentRunning(ctx, c); err != nil {
		return err
	} else if running {
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

// IsServerRunning reports whether the k3s server systemd unit is active.
// This is the install-completion gate for control-plane hosts: unlike a
// bare `command -v k3s` probe it is false for a partial/interrupted
// install (binary on disk, unit never created or server never started),
// so the reconciler re-runs the idempotent installer to finish the job.
func IsServerRunning(ctx context.Context, c *ssh.Client) (bool, error) {
	return unitActive(ctx, c, "k3s")
}

// IsAgentRunning reports whether the k3s-agent systemd unit is active —
// the install-completion gate for worker hosts.
func IsAgentRunning(ctx context.Context, c *ssh.Client) (bool, error) {
	return unitActive(ctx, c, "k3s-agent")
}

// unitActive runs `systemctl is-active --quiet <unit>` (exit 0 = active).
// No sudo: is-active is a read-only query.
func unitActive(ctx context.Context, c *ssh.Client, unit string) (bool, error) {
	res, err := c.Run(ctx, "systemctl is-active --quiet "+unit)
	if err != nil {
		return false, err
	}
	return res.ExitCode == 0, nil
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
// --tls-san + --node-ip + --advertise-address + --node-external-ip
// when each is set.
func serverArgs(opts InstallOptions) []string {
	out := append([]string{}, opts.ExtraArgs...)
	if opts.TLSSAN != "" {
		out = append(out, "--tls-san="+opts.TLSSAN)
	}
	if opts.NodeIP != "" {
		out = append(out, "--node-ip="+opts.NodeIP, "--advertise-address="+opts.NodeIP)
	}
	if opts.NodeExternalIP != "" {
		out = append(out, "--node-external-ip="+opts.NodeExternalIP)
	}
	if opts.NodeName != "" {
		out = append(out, "--node-name="+opts.NodeName)
	}
	return out
}

// agentArgs builds the k3s agent flag tail: user's ExtraArgs +
// --node-ip + --node-external-ip + --node-name when each is set.
func agentArgs(opts InstallOptions) []string {
	out := append([]string{}, opts.ExtraArgs...)
	if opts.NodeIP != "" {
		out = append(out, "--node-ip="+opts.NodeIP)
	}
	if opts.NodeExternalIP != "" {
		out = append(out, "--node-external-ip="+opts.NodeExternalIP)
	}
	if opts.NodeName != "" {
		out = append(out, "--node-name="+opts.NodeName)
	}
	return out
}

// strconvQuote is a tiny shell-quote — we cannot use strconv.Quote
// because bash strips Go's hex escapes.
func strconvQuote(s string) string {
	// Replace single quotes with the standard '\'' trick then wrap.
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
