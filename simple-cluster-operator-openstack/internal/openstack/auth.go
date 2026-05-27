/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package openstack wraps gophercloud calls used by the controller.
package openstack

import (
	"context"
	"fmt"

	"github.com/gophercloud/gophercloud/v2"
	gophercloudopenstack "github.com/gophercloud/gophercloud/v2/openstack"
	corev1 "k8s.io/api/core/v1"

	openstackv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-openstack/api/v1alpha1"
)

// Clients holds the gophercloud service clients the controller needs.
type Clients struct {
	Region   string
	Provider *gophercloud.ProviderClient
	Compute  *gophercloud.ServiceClient
	Network  *gophercloud.ServiceClient
	Image    *gophercloud.ServiceClient
}

// NewClientsFromSecret authenticates with Keystone v3 application
// credentials read from `creds.Data` and returns the service clients.
func NewClientsFromSecret(ctx context.Context, creds *corev1.Secret) (*Clients, error) {
	get := func(key string) (string, error) {
		v, ok := creds.Data[key]
		if !ok || len(v) == 0 {
			return "", fmt.Errorf("credentials Secret %s/%s missing key %q",
				creds.Namespace, creds.Name, key)
		}
		return string(v), nil
	}

	authURL, err := get(openstackv1alpha1.CredentialsAuthURLKey)
	if err != nil {
		return nil, err
	}
	region, err := get(openstackv1alpha1.CredentialsRegionKey)
	if err != nil {
		return nil, err
	}
	credID, err := get(openstackv1alpha1.CredentialsAppCredIDKey)
	if err != nil {
		return nil, err
	}
	credSecret, err := get(openstackv1alpha1.CredentialsAppCredSecretKey)
	if err != nil {
		return nil, err
	}

	opts := gophercloud.AuthOptions{
		IdentityEndpoint:            authURL,
		ApplicationCredentialID:     credID,
		ApplicationCredentialSecret: credSecret,
		AllowReauth:                 true,
	}

	provider, err := gophercloudopenstack.AuthenticatedClient(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("keystone auth: %w", err)
	}

	endpointOpts := gophercloud.EndpointOpts{Region: region, Availability: gophercloud.AvailabilityPublic}

	compute, err := gophercloudopenstack.NewComputeV2(provider, endpointOpts)
	if err != nil {
		return nil, fmt.Errorf("compute service client: %w", err)
	}
	network, err := gophercloudopenstack.NewNetworkV2(provider, endpointOpts)
	if err != nil {
		return nil, fmt.Errorf("network service client: %w", err)
	}
	image, err := gophercloudopenstack.NewImageV2(provider, endpointOpts)
	if err != nil {
		return nil, fmt.Errorf("image service client: %w", err)
	}

	return &Clients{
		Region:   region,
		Provider: provider,
		Compute:  compute,
		Network:  network,
		Image:    image,
	}, nil
}
