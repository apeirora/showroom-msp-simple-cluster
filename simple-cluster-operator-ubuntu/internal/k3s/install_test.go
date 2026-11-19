/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package k3s

import (
	"reflect"
	"strings"
	"testing"
)

func TestServerArgs(t *testing.T) {
	cases := []struct {
		name string
		opts InstallOptions
		want []string
	}{
		{
			name: "all knobs set",
			opts: InstallOptions{
				ExtraArgs:      []string{"--disable=traefik"},
				TLSSAN:         "203.0.113.30",
				NodeIP:         "192.168.168.48",
				NodeExternalIP: "203.0.113.30",
			},
			want: []string{
				"--disable=traefik",
				"--tls-san=203.0.113.30",
				"--node-ip=192.168.168.48",
				"--advertise-address=192.168.168.48",
				"--node-external-ip=203.0.113.30",
			},
		},
		{
			name: "no external ip",
			opts: InstallOptions{
				TLSSAN: "10.0.0.1",
				NodeIP: "10.0.0.1",
			},
			want: []string{
				"--tls-san=10.0.0.1",
				"--node-ip=10.0.0.1",
				"--advertise-address=10.0.0.1",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := serverArgs(tc.opts); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("serverArgs = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAgentArgs(t *testing.T) {
	cases := []struct {
		name string
		opts InstallOptions
		want []string
	}{
		{
			name: "external ip set",
			opts: InstallOptions{
				NodeIP:         "192.168.168.91",
				NodeExternalIP: "203.0.113.31",
			},
			want: []string{
				"--node-ip=192.168.168.91",
				"--node-external-ip=203.0.113.31",
			},
		},
		{
			name: "no external ip",
			opts: InstallOptions{
				NodeIP: "192.168.168.91",
			},
			want: []string{
				"--node-ip=192.168.168.91",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := agentArgs(tc.opts); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("agentArgs = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestBuildInstallScriptQuoting (F-UB-1, F-UB-3): values reach the remote
// bash layer single-quoted so $() and backticks stay literal, and secret
// values live only in the session stdin, never in the script text.
func TestBuildInstallScriptQuoting(t *testing.T) {
	opts := InstallOptions{Version: "v$(touch /tmp/pwn)+k3s1"}
	args := []string{"server", "--node-name=`id`"}
	env := map[string]string{"K3S_URL": "https://10.0.0.1:6443"}
	secretEnv := map[string]string{"K3S_TOKEN": "K10secrettoken::abcdef"}

	script, stdin := buildInstallScript(opts, args, env, secretEnv)

	for _, want := range []string{
		`export INSTALL_K3S_VERSION='v$(touch /tmp/pwn)+k3s1'; `,
		`export K3S_URL='https://10.0.0.1:6443'; `,
		"sh -s - 'server' '--node-name=`id`'",
		"IFS= read -r K3S_TOKEN; export K3S_TOKEN; ",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "K10secrettoken") || strings.Contains(script, "touch /tmp/pwn\"") {
		t.Fatalf("secret leaked into the script text:\n%s", script)
	}
	if stdin != "K10secrettoken::abcdef\n" {
		t.Fatalf("stdin = %q, want the token line", stdin)
	}

	// Ordinary values are unaffected.
	script, stdin = buildInstallScript(InstallOptions{Version: "v1.31.4+k3s1"}, []string{"server"}, nil, nil)
	if !strings.Contains(script, `export INSTALL_K3S_VERSION='v1.31.4+k3s1'; `) || stdin != "" {
		t.Fatalf("ordinary install changed:\n%s stdin=%q", script, stdin)
	}
}

// TestBuildInstallScriptSecretOrder: multiple secrets are read in sorted
// key order, matching the stdin writer.
func TestBuildInstallScriptSecretOrder(t *testing.T) {
	script, stdin := buildInstallScript(
		InstallOptions{}, nil, nil,
		map[string]string{"ZZ_SECRET": "z", "AA_SECRET": "a"})
	if stdin != "a\nz\n" {
		t.Fatalf("stdin = %q, want a then z", stdin)
	}
	if strings.Index(script, "read -r AA_SECRET") > strings.Index(script, "read -r ZZ_SECRET") {
		t.Fatalf("read order does not match stdin order:\n%s", script)
	}
}
