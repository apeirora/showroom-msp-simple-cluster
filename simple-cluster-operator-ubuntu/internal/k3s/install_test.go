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
				TLSSAN:         "46.60.18.73",
				NodeIP:         "192.168.168.48",
				NodeExternalIP: "46.60.18.73",
			},
			want: []string{
				"--disable=traefik",
				"--tls-san=46.60.18.73",
				"--node-ip=192.168.168.48",
				"--advertise-address=192.168.168.48",
				"--node-external-ip=46.60.18.73",
			},
		},
		{
			name: "no external ip keeps previous shape",
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
				NodeExternalIP: "46.60.18.74",
			},
			want: []string{
				"--node-ip=192.168.168.91",
				"--node-external-ip=46.60.18.74",
			},
		},
		{
			name: "no external ip keeps previous shape",
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
