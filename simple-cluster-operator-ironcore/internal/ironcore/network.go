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

	commonv1alpha1 "github.com/ironcore-dev/ironcore/api/common/v1alpha1"
	ipamv1alpha1 "github.com/ironcore-dev/ironcore/api/ipam/v1alpha1"
	networkingv1alpha1 "github.com/ironcore-dev/ironcore/api/networking/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// EnsureNetwork creates a Network in the IronCore cluster if it does not
// already exist. Idempotent.
func EnsureNetwork(ctx context.Context, c *Clients, name string) error {
	var net networkingv1alpha1.Network
	err := c.Client.Get(ctx, types.NamespacedName{Namespace: c.Namespace, Name: name}, &net)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get Network %s/%s: %w", c.Namespace, name, err)
	}
	obj := &networkingv1alpha1.Network{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: c.Namespace,
		},
	}
	if err := c.Client.Create(ctx, obj); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create Network %s/%s: %w", c.Namespace, name, err)
	}
	return nil
}

// EnsurePrefix creates a root ipam Prefix (no parent) with the given CIDR if it
// does not already exist. Idempotent. The Prefix is shared infrastructure keyed
// by name, like the Network, and is not deleted with individual VMs.
func EnsurePrefix(ctx context.Context, c *Clients, name, cidr string) error {
	var existing ipamv1alpha1.Prefix
	err := c.Client.Get(ctx, types.NamespacedName{Namespace: c.Namespace, Name: name}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get Prefix %s/%s: %w", c.Namespace, name, err)
	}
	prefix, err := commonv1alpha1.ParseNewIPPrefix(cidr)
	if err != nil {
		return fmt.Errorf("parse networkCIDR %q: %w", cidr, err)
	}
	obj := &ipamv1alpha1.Prefix{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: c.Namespace,
		},
		Spec: ipamv1alpha1.PrefixSpec{
			IPFamily: corev1.IPv4Protocol,
			Prefix:   prefix,
		},
	}
	if err := c.Client.Create(ctx, obj); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create Prefix %s/%s: %w", c.Namespace, name, err)
	}
	return nil
}

// EnsureVirtualIP creates a Public VirtualIP if it does not already exist.
// Idempotent.
func EnsureVirtualIP(ctx context.Context, c *Clients, name string, labels map[string]string) error {
	var vip networkingv1alpha1.VirtualIP
	err := c.Client.Get(ctx, types.NamespacedName{Namespace: c.Namespace, Name: name}, &vip)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get VirtualIP %s/%s: %w", c.Namespace, name, err)
	}
	obj := &networkingv1alpha1.VirtualIP{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: c.Namespace,
			Labels:    labels,
		},
		Spec: networkingv1alpha1.VirtualIPSpec{
			Type:     networkingv1alpha1.VirtualIPTypePublic,
			IPFamily: corev1.IPv4Protocol,
		},
	}
	if err := c.Client.Create(ctx, obj); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create VirtualIP %s/%s: %w", c.Namespace, name, err)
	}
	return nil
}

// EnsureNetworkInterface creates a NetworkInterface if it does not already exist.
// ipPool, when non-empty, allocates an ephemeral /32 from the named ipam Prefix.
// vipName, when non-empty, references an existing VirtualIP.
// FLAGGED: some IronCore clusters may require an explicit ipPool; leaving IPs
// empty relies on provider-level auto-assignment.
func EnsureNetworkInterface(ctx context.Context, c *Clients, name, networkName, ipPool, vipName string, labels map[string]string) error {
	var nic networkingv1alpha1.NetworkInterface
	err := c.Client.Get(ctx, types.NamespacedName{Namespace: c.Namespace, Name: name}, &nic)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get NetworkInterface %s/%s: %w", c.Namespace, name, err)
	}

	spec := networkingv1alpha1.NetworkInterfaceSpec{
		NetworkRef: corev1.LocalObjectReference{Name: networkName},
		IPFamilies: []corev1.IPFamily{corev1.IPv4Protocol},
	}

	if ipPool != "" {
		spec.IPs = []networkingv1alpha1.IPSource{
			{
				Ephemeral: &networkingv1alpha1.EphemeralPrefixSource{
					PrefixTemplate: &ipamv1alpha1.PrefixTemplateSpec{
						Spec: ipamv1alpha1.PrefixSpec{
							IPFamily:     corev1.IPv4Protocol,
							PrefixLength: 32,
							ParentRef:    &corev1.LocalObjectReference{Name: ipPool},
						},
					},
				},
			},
		}
	}

	if vipName != "" {
		spec.VirtualIP = &networkingv1alpha1.VirtualIPSource{
			VirtualIPRef: &corev1.LocalObjectReference{Name: vipName},
		}
	}

	obj := &networkingv1alpha1.NetworkInterface{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: c.Namespace,
			Labels:    labels,
		},
		Spec: spec,
	}
	if err := c.Client.Create(ctx, obj); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create NetworkInterface %s/%s: %w", c.Namespace, name, err)
	}
	return nil
}

// NICStatus returns the internal IP and public IP currently reported by a
// NetworkInterface. Returns ("", "", nil) when the NIC does not exist yet.
func NICStatus(ctx context.Context, c *Clients, name string) (internalIP, publicIP string, err error) {
	var nic networkingv1alpha1.NetworkInterface
	if err := c.Client.Get(ctx, types.NamespacedName{Namespace: c.Namespace, Name: name}, &nic); err != nil {
		if apierrors.IsNotFound(err) {
			return "", "", nil
		}
		return "", "", fmt.Errorf("get NetworkInterface %s/%s: %w", c.Namespace, name, err)
	}

	if len(nic.Status.IPs) > 0 {
		internalIP = nic.Status.IPs[0].String()
	}
	if nic.Status.VirtualIP != nil {
		publicIP = nic.Status.VirtualIP.String()
	}
	return internalIP, publicIP, nil
}
