/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package kubeconfig

import (
	"bytes"
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	rendertpl "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/template"
)

// SecretSuffix is appended to SC.Name to form the kubeconfig Secret name.
const SecretSuffix = "-kubeconfig"

// SecretDataKey is the key inside the destination Secret holding the
// kubeconfig bytes.
const SecretDataKey = "kubeconfig"

// SecretResult bundles the outcome of EnsureSecret for the caller to
// mirror into SC.Status.
type SecretResult struct {
	SecretName string
	IssuedAt   *time.Time
	ExpiresAt  *time.Time
	Changed    bool
}

// EnsureSecret writes kubeconfig bytes into a Secret in SC.Namespace, sets
// SC as owner (for GC on SC deletion), and reports the resulting metadata.
// Existing Secrets with identical contents are left untouched.
//
// sourceExpiry is the expiry hint published by the source strategy (e.g. a
// Gardener AdminKubeconfigRequest reports its own expiration). It is used
// when TokenTimes cannot extract a time from the kubeconfig bytes.
func EnsureSecret(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	sc *showroomv1alpha1.SimpleCluster,
	scpc *showroomv1alpha1.SimpleClusterProviderConfig,
	kubeconfigBytes []byte,
	sourceExpiry *time.Time,
) (*SecretResult, error) {
	name := sc.Name + SecretSuffix

	issuedAt, expiresAt, terr := TokenTimes(kubeconfigBytes)
	if terr != nil {
		issuedAt, expiresAt = nil, nil
	}
	if expiresAt == nil && sourceExpiry != nil {
		expiresAt = sourceExpiry
	}

	var existing corev1.Secret
	getErr := c.Get(ctx, types.NamespacedName{Name: name, Namespace: sc.Namespace}, &existing)
	if getErr != nil && !apierrors.IsNotFound(getErr) {
		return nil, fmt.Errorf("get Secret %s/%s: %w", sc.Namespace, name, getErr)
	}

	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: sc.Namespace,
			Annotations: map[string]string{
				rendertpl.AnnoSimpleCluster:         sc.Namespace + "/" + sc.Name,
				rendertpl.AnnoSimpleClusterProvider: scpc.Name,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{SecretDataKey: kubeconfigBytes},
	}
	if err := controllerutil.SetControllerReference(sc, desired, scheme); err != nil {
		return nil, fmt.Errorf("set owner ref: %w", err)
	}

	result := &SecretResult{
		SecretName: name,
		IssuedAt:   issuedAt,
		ExpiresAt:  expiresAt,
	}

	if apierrors.IsNotFound(getErr) {
		if err := c.Create(ctx, desired); err != nil {
			return nil, fmt.Errorf("create Secret %s/%s: %w", sc.Namespace, name, err)
		}
		result.Changed = true
		return result, nil
	}

	if bytes.Equal(existing.Data[SecretDataKey], kubeconfigBytes) {
		return result, nil
	}

	existing.Data = desired.Data
	existing.Type = desired.Type
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	for k, v := range desired.Labels {
		existing.Labels[k] = v
	}
	if err := controllerutil.SetControllerReference(sc, &existing, scheme); err != nil {
		return nil, fmt.Errorf("set owner ref: %w", err)
	}
	if err := c.Update(ctx, &existing); err != nil {
		return nil, fmt.Errorf("update Secret %s/%s: %w", sc.Namespace, name, err)
	}
	result.Changed = true
	return result, nil
}
