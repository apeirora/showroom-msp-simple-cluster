/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package target builds a dynamic.Interface + RESTMapper for the target
// cluster described by an SCPC. See DESIGN.md §2 ("target client").
package target

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/secretpolicy"
)

const KubeconfigKey = "kubeconfig"

// Client bundles a dynamic client with the matching RESTMapper and the
// underlying *rest.Config (needed for raw subresource POSTs that
// dynamic.Interface does not expose).
type Client struct {
	AccountParent           *showroomv1alpha1.AccountTargetStatus
	ProviderIdentity        string
	CredentialAuthorization *AccountAuthorization

	Dynamic        dynamic.Interface
	Mapper         apimeta.RESTMapper
	RESTConfig     *rest.Config
	SecretPolicy   secretpolicy.Policy
	OwnerNamespace string
}

// New builds a client from an explicit workload kubeconfig Secret.
// A nil reference never selects the provider cluster.
// The management config and mapper parameters remain for caller compatibility.
func New(
	ctx context.Context,
	mgmt client.Client,
	mgmtCfg *rest.Config,
	mgmtMapper apimeta.RESTMapper,
	ref *corev1.SecretReference,
	ownerNamespace string,
	policy secretpolicy.Policy,
) (*Client, error) {
	if ref == nil {
		return nil, fmt.Errorf("provider cluster child destinations are disabled; configure the account provider")
	}

	namespace, err := policy.Resolve(ownerNamespace, ref.Namespace, ref.Name, secretpolicy.Use)
	if err != nil {
		return nil, err
	}
	var secret corev1.Secret
	if err := mgmt.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: namespace}, &secret); err != nil {
		return nil, fmt.Errorf("get Secret %s/%s: %w", namespace, ref.Name, err)
	}
	data := secret.Data[KubeconfigKey]
	if len(data) == 0 {
		return nil, fmt.Errorf("secret %s/%s missing %q key", namespace, ref.Name, KubeconfigKey)
	}
	cfg, err := clientcmd.RESTConfigFromKubeConfig(data)
	if err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}

	d, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("discovery client: %w", err)
	}
	groups, err := restmapper.GetAPIGroupResources(dc)
	if err != nil {
		return nil, fmt.Errorf("discover API groups: %w", err)
	}
	return &Client{
		Dynamic:        d,
		SecretPolicy:   policy,
		OwnerNamespace: ownerNamespace,
		Mapper:         restmapper.NewDiscoveryRESTMapper(groups),
		RESTConfig:     cfg,
	}, nil
}

// ResourceFor returns the dynamic ResourceInterface scoped (namespaced or
// cluster-wide) appropriate for obj's namespace+GVK.
func (c *Client) ResourceFor(mapping *apimeta.RESTMapping, namespace, name, purpose string) (dynamic.ResourceInterface, error) {
	if mapping.Resource.Group == "" && mapping.Resource.Resource == "secrets" {
		var err error
		namespace, err = c.SecretPolicy.Resolve(c.OwnerNamespace, namespace, name, purpose)
		if err != nil {
			return nil, err
		}
	}
	if mapping.Scope.Name() == apimeta.RESTScopeNameNamespace {
		return c.Dynamic.Resource(mapping.Resource).Namespace(namespace), nil
	}
	return c.Dynamic.Resource(mapping.Resource), nil
}
