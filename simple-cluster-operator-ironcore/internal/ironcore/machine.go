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
	"fmt"
	"strings"

	commonv1alpha1 "github.com/ironcore-dev/ironcore/api/common/v1alpha1"
	computev1alpha1 "github.com/ironcore-dev/ironcore/api/compute/v1alpha1"
	networkingv1alpha1 "github.com/ironcore-dev/ironcore/api/networking/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VMLabels returns the label set the operator stamps on every remote resource
// it owns, keyed by the management-cluster VM namespace and name.
func VMLabels(namespace, name string) map[string]string {
	return map[string]string{
		"ironcore.apeirora.eu/vm-namespace": namespace,
		"ironcore.apeirora.eu/vm-name":      name,
	}
}

// MachineName derives a deterministic IronCore Machine name from the VM CR key.
func MachineName(namespace, name string) string {
	return SanitizeName(namespace + "-" + name)
}

// NICName derives the NetworkInterface name for a VM.
func NICName(namespace, name string) string {
	return SanitizeName(namespace + "-" + name + "-nic")
}

// VIPName derives the VirtualIP name for a VM.
func VIPName(namespace, name string) string {
	return SanitizeName(namespace + "-" + name + "-vip")
}

// IgnitionSecretName derives the ignition Secret name for a VM.
func IgnitionSecretName(namespace, name string) string {
	return SanitizeName(namespace + "-" + name + "-ign")
}

// SanitizeName trims a candidate name to RFC1123 DNS label form (<=63 chars,
// lowercase alphanumeric and hyphens, no leading/trailing hyphens).
func SanitizeName(in string) string {
	in = strings.ToLower(in)
	in = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, in)
	if len(in) > 63 {
		in = in[:63]
	}
	return strings.Trim(in, "-")
}

// FindMachineByVM returns the Machine tagged with the given labels, or nil
// if no such Machine exists yet.
func FindMachineByVM(ctx context.Context, c *Clients, labels map[string]string) (*computev1alpha1.Machine, error) {
	var list computev1alpha1.MachineList
	if err := c.Client.List(ctx, &list,
		client.InNamespace(c.Namespace),
		client.MatchingLabels(labels),
	); err != nil {
		return nil, fmt.Errorf("list Machines in %s: %w", c.Namespace, err)
	}
	if len(list.Items) == 0 {
		return nil, nil
	}
	return &list.Items[0], nil
}

// CreateMachine creates an IronCore Machine with the given parameters.
// machinePool may be empty to let the scheduler decide.
func CreateMachine(
	ctx context.Context,
	c *Clients,
	name, machineClassName, nicName, ignitionSecretName, image, machinePool string,
	labels map[string]string,
) (*computev1alpha1.Machine, error) {
	machine := &computev1alpha1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: c.Namespace,
			Labels:    labels,
		},
		Spec: computev1alpha1.MachineSpec{
			MachineClassRef: corev1.LocalObjectReference{Name: machineClassName},
			Power:           computev1alpha1.PowerOn,
			NetworkInterfaces: []computev1alpha1.NetworkInterface{
				{
					Name: "primary",
					NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
						NetworkInterfaceRef: &corev1.LocalObjectReference{Name: nicName},
					},
				},
			},
			Volumes: []computev1alpha1.Volume{
				{
					Name: "root",
					VolumeSource: computev1alpha1.VolumeSource{
						LocalDisk: &computev1alpha1.LocalDiskVolumeSource{
							Image: image,
						},
					},
				},
			},
			IgnitionRef: &commonv1alpha1.SecretKeySelector{
				Name: ignitionSecretName,
			},
		},
	}
	if machinePool != "" {
		machine.Spec.MachinePoolRef = &corev1.LocalObjectReference{Name: machinePool}
	}

	if err := c.Client.Create(ctx, machine); err != nil {
		return nil, fmt.Errorf("create Machine %s/%s: %w", c.Namespace, name, err)
	}
	return machine, nil
}

// DeleteByVM deletes the Machine, NetworkInterface, VirtualIP, and ignition
// Secret that belong to the given VM labels. NotFound errors are ignored.
func DeleteByVM(ctx context.Context, c *Clients, labels map[string]string) error {
	if err := deleteAllOf(ctx, c, &computev1alpha1.Machine{}, labels); err != nil {
		return fmt.Errorf("delete Machines: %w", err)
	}
	if err := deleteAllOf(ctx, c, &networkingv1alpha1.NetworkInterface{}, labels); err != nil {
		return fmt.Errorf("delete NetworkInterfaces: %w", err)
	}
	if err := deleteAllOf(ctx, c, &networkingv1alpha1.VirtualIP{}, labels); err != nil {
		return fmt.Errorf("delete VirtualIPs: %w", err)
	}
	if err := deleteAllOf(ctx, c, &corev1.Secret{}, labels); err != nil {
		return fmt.Errorf("delete ignition Secrets: %w", err)
	}
	return nil
}

// deleteAllOf deletes all resources of a given type matching the labels.
// NotFound is treated as success.
func deleteAllOf(ctx context.Context, c *Clients, obj client.Object, labels map[string]string) error {
	err := c.Client.DeleteAllOf(ctx, obj,
		client.InNamespace(c.Namespace),
		client.MatchingLabels(labels),
	)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}
