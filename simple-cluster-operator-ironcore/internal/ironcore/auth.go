/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package ironcore wraps IronCore API calls used by the controller.
package ironcore

import (
	"context"
	"fmt"

	computev1alpha1 "github.com/ironcore-dev/ironcore/api/compute/v1alpha1"
	corev1alpha1 "github.com/ironcore-dev/ironcore/api/core/v1alpha1"
	ipamv1alpha1 "github.com/ironcore-dev/ironcore/api/ipam/v1alpha1"
	networkingv1alpha1 "github.com/ironcore-dev/ironcore/api/networking/v1alpha1"
	storagev1alpha1 "github.com/ironcore-dev/ironcore/api/storage/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ironcoreapiv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ironcore/api/v1alpha1"
)

// Clients holds a controller-runtime client targeting the IronCore cluster
// and the namespace where remote resources live.
type Clients struct {
	Client    client.Client
	Namespace string
}

// NewClientsFromSecret builds a Clients from the kubeconfig stored in
// creds.Data["kubeconfig"]. namespace is the IronCore cluster namespace.
func NewClientsFromSecret(_ context.Context, creds *corev1.Secret, namespace string) (*Clients, error) {
	kubeconfigBytes, ok := creds.Data[ironcoreapiv1alpha1.CredentialsKubeconfigKey]
	if !ok || len(kubeconfigBytes) == 0 {
		return nil, fmt.Errorf("credentials Secret %s/%s missing key %q",
			creds.Namespace, creds.Name, ironcoreapiv1alpha1.CredentialsKubeconfigKey)
	}

	cfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return nil, fmt.Errorf("parse kubeconfig from Secret %s/%s: %w",
			creds.Namespace, creds.Name, err)
	}

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("add client-go scheme: %w", err)
	}
	if err := computev1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("add compute scheme: %w", err)
	}
	if err := networkingv1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("add networking scheme: %w", err)
	}
	if err := storagev1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("add storage scheme: %w", err)
	}
	if err := ipamv1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("add ipam scheme: %w", err)
	}
	if err := corev1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("add core scheme: %w", err)
	}

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("build IronCore client: %w", err)
	}

	ns := namespace
	if ns == "" {
		ns = "default"
	}

	return &Clients{
		Client:    c,
		Namespace: ns,
	}, nil
}
