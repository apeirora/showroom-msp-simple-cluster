/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package ironcore

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"

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
