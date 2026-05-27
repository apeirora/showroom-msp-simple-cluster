/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package openstack

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/keypairs"
	"golang.org/x/crypto/ssh"
)

// GenerateEd25519Keypair returns a fresh ed25519 keypair encoded for
// Kubernetes consumption (PEM private + OpenSSH single-line public).
func GenerateEd25519Keypair(comment string) (privatePEM, publicAuthorized []byte, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate ed25519 key: %w", err)
	}

	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal openssh private key: %w", err)
	}
	privatePEM = pem.EncodeToMemory(block)

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, nil, fmt.Errorf("wrap ed25519 public key: %w", err)
	}
	publicAuthorized = ssh.MarshalAuthorizedKey(sshPub)
	return privatePEM, publicAuthorized, nil
}

// EnsureKeypair uploads `publicKey` under `name` in Nova if it is not
// already present. If a keypair with the same name exists, no replacement
// is attempted (Nova does not allow overwriting; the caller is expected
// to delete first if intent changed).
func EnsureKeypair(ctx context.Context, compute *gophercloud.ServiceClient, name, publicKey string) error {
	if _, err := keypairs.Get(ctx, compute, name, nil).Extract(); err == nil {
		return nil
	} else if !gophercloud.ResponseCodeIs(err, 404) {
		return fmt.Errorf("get keypair %q: %w", name, err)
	}

	// Type ("ssh") would need Nova microversion 2.2; CloudFerro's default
	// endpoint rejects it as an unknown property. Default is "ssh" anyway.
	_, err := keypairs.Create(ctx, compute, keypairs.CreateOpts{
		Name:      name,
		PublicKey: publicKey,
	}).Extract()
	if err != nil {
		return fmt.Errorf("create keypair %q: %w", name, err)
	}
	return nil
}

// DeleteKeypair removes the Nova keypair if present.
func DeleteKeypair(ctx context.Context, compute *gophercloud.ServiceClient, name string) error {
	err := keypairs.Delete(ctx, compute, name, nil).ExtractErr()
	if err == nil {
		return nil
	}
	if gophercloud.ResponseCodeIs(err, 404) {
		return nil
	}
	var ge gophercloud.ErrUnexpectedResponseCode
	if errors.As(err, &ge) && ge.Actual == 404 {
		return nil
	}
	return fmt.Errorf("delete keypair %q: %w", name, err)
}
