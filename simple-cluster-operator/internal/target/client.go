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
)

const KubeconfigKey = "kubeconfig"

// Client bundles a dynamic client with the matching RESTMapper and the
// underlying *rest.Config (needed for raw subresource POSTs that
// dynamic.Interface does not expose).
type Client struct {
	Dynamic    dynamic.Interface
	Mapper     apimeta.RESTMapper
	RESTConfig *rest.Config
}

// New builds a target Client. If ref is nil, returns a client for the
// management cluster (reusing mgmtCfg and mgmtMapper). Otherwise loads the
// kubeconfig from the named Secret and builds a fresh dynamic+mapper pair.
func New(
	ctx context.Context,
	mgmt client.Client,
	mgmtCfg *rest.Config,
	mgmtMapper apimeta.RESTMapper,
	ref *corev1.SecretReference,
) (*Client, error) {
	if ref == nil {
		d, err := dynamic.NewForConfig(mgmtCfg)
		if err != nil {
			return nil, fmt.Errorf("dynamic client (local): %w", err)
		}
		return &Client{Dynamic: d, Mapper: mgmtMapper, RESTConfig: mgmtCfg}, nil
	}

	var secret corev1.Secret
	if err := mgmt.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, &secret); err != nil {
		return nil, fmt.Errorf("get Secret %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	data := secret.Data[KubeconfigKey]
	if len(data) == 0 {
		return nil, fmt.Errorf("secret %s/%s missing %q key", ref.Namespace, ref.Name, KubeconfigKey)
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
		Dynamic:    d,
		Mapper:     restmapper.NewDiscoveryRESTMapper(groups),
		RESTConfig: cfg,
	}, nil
}

// ResourceFor returns the dynamic ResourceInterface scoped (namespaced or
// cluster-wide) appropriate for obj's namespace+GVK.
func (c *Client) ResourceFor(mapping *apimeta.RESTMapping, namespace string) dynamic.ResourceInterface {
	if mapping.Scope.Name() == apimeta.RESTScopeNameNamespace {
		return c.Dynamic.Resource(mapping.Resource).Namespace(namespace)
	}
	return c.Dynamic.Resource(mapping.Resource)
}
