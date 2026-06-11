/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package ironcore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	ironcoreapiv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ironcore/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Ignition v3.2.0 minimal structs (no external dependency).

type ignitionConfig struct {
	Ignition ignitionMeta     `json:"ignition"`
	Passwd   *ignitionPasswd  `json:"passwd,omitempty"`
	Storage  *ignitionStorage `json:"storage,omitempty"`
	Systemd  *ignitionSystemd `json:"systemd,omitempty"`
}

type ignitionMeta struct {
	Version string `json:"version"`
}

type ignitionPasswd struct {
	Users []ignitionUser `json:"users,omitempty"`
}

type ignitionUser struct {
	Name              string   `json:"name"`
	SSHAuthorizedKeys []string `json:"sshAuthorizedKeys,omitempty"`
}

type ignitionStorage struct {
	Files []ignitionFile `json:"files,omitempty"`
}

type ignitionFile struct {
	Path     string              `json:"path"`
	Mode     *int                `json:"mode,omitempty"`
	Contents *ignitionFileSource `json:"contents,omitempty"`
}

type ignitionFileSource struct {
	Source string `json:"source"`
}

type ignitionSystemd struct {
	Units []ignitionUnit `json:"units,omitempty"`
}

type ignitionUnit struct {
	Name     string  `json:"name"`
	Enabled  *bool   `json:"enabled,omitempty"`
	Contents *string `json:"contents,omitempty"`
}

// BuildIgnition produces a minimal Ignition v3.2.0 JSON document that
// injects sshPublicKey into the authorized_keys for sshUser. If userData is
// non-empty, a storage file and a oneshot systemd unit are added to execute it.
// Version 3.2.0 matches the gardenlinux ignition parser used by this landscape's
// running VMs; only fields present since 3.0 are used, so this is forward-safe.
func BuildIgnition(sshUser, sshPublicKey, userData string) ([]byte, error) {
	cfg := ignitionConfig{
		Ignition: ignitionMeta{Version: "3.2.0"},
		Passwd: &ignitionPasswd{
			Users: []ignitionUser{
				{
					Name:              sshUser,
					SSHAuthorizedKeys: []string{strings.TrimSpace(sshPublicKey)},
				},
			},
		},
	}

	if userData != "" {
		encoded := "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte(userData))
		mode := 0o644
		cfg.Storage = &ignitionStorage{
			Files: []ignitionFile{
				{
					Path: "/var/lib/ironcore/user-data",
					Mode: &mode,
					Contents: &ignitionFileSource{
						Source: encoded,
					},
				},
			},
		}
		enabled := true
		unitContents := `[Unit]
Description=Run user-data once
ConditionPathExists=/var/lib/ironcore/user-data
After=network.target

[Service]
Type=oneshot
ExecStart=/bin/bash /var/lib/ironcore/user-data
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
`
		cfg.Systemd = &ignitionSystemd{
			Units: []ignitionUnit{
				{
					Name:     "ironcore-user-data.service",
					Enabled:  &enabled,
					Contents: &unitContents,
				},
			},
		}
	}

	return json.Marshal(cfg)
}

// EnsureIgnitionSecret creates or updates an Opaque Secret with key
// "ignition.yaml" containing the given ignition data in the IronCore cluster.
func EnsureIgnitionSecret(ctx context.Context, c *Clients, name string, data []byte, labels map[string]string) error {
	key := types.NamespacedName{Namespace: c.Namespace, Name: name}
	var existing corev1.Secret
	err := c.Client.Get(ctx, key, &existing)
	if err == nil {
		patch := client.MergeFrom(existing.DeepCopy())
		existing.Data = map[string][]byte{
			ironcoreapiv1alpha1.IgnitionSecretKey: data,
		}
		if err := c.Client.Patch(ctx, &existing, patch); err != nil {
			return fmt.Errorf("patch ignition Secret %s/%s: %w", c.Namespace, name, err)
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get ignition Secret %s/%s: %w", c.Namespace, name, err)
	}

	obj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: c.Namespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			ironcoreapiv1alpha1.IgnitionSecretKey: data,
		},
	}
	if err := c.Client.Create(ctx, obj); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create ignition Secret %s/%s: %w", c.Namespace, name, err)
	}
	return nil
}
