/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"strconv"
	"strings"
	"testing"

	cryptossh "golang.org/x/crypto/ssh"
)

// newTestServerKey returns an ssh host key whose authorized_keys line is
// distinctive per index.
func newTestServerKey(t *testing.T, b byte) cryptossh.PublicKey {
	t.Helper()
	priv := ed25519.PrivateKey(make([]byte, ed25519.PrivateKeySize))
	for i := range priv {
		priv[i] = byte(i) ^ b
	}
	signer, err := cryptossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer.PublicKey()
}

func TestHostKeyPinsTOFU(t *testing.T) {
	pins := &hostKeyPins{keys: map[string]string{}}
	cb := pins.callbackFor("10.0.0.1:22")
	keyA := newTestServerKey(t, 1)
	keyB := newTestServerKey(t, 2)

	if err := cb("", nil, keyA); err != nil {
		t.Fatalf("first use must trust the key (TOFU), got %v", err)
	}
	if err := cb("", nil, keyA); err != nil {
		t.Fatalf("pinned key must be accepted, got %v", err)
	}
	err := cb("", nil, keyB)
	if err == nil {
		t.Fatal("a changed host key must be refused")
	}
	if !strings.Contains(err.Error(), "changed") {
		t.Fatalf("error should explain the pin mismatch, got %v", err)
	}
	// A different address starts its own TOFU window.
	if err := pins.callbackFor("10.0.0.2:22")("", nil, keyB); err != nil {
		t.Fatalf("other address should be unpinned, got %v", err)
	}
}

// TestDialPinsHostKey: Dial consults the process-wide pin store, records
// the first key it sees and reuses it on later connections.
func TestDialPinsHostKey(t *testing.T) {
	resetHostKeyPins()
	t.Cleanup(resetHostKeyPins)

	addr, hostKey := startTestSSHServer(t)
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	cred := Credential{Password: "x"}

	for i := 0; i < 2; i++ {
		c, err := Dial(context.Background(), "127.0.0.1", mustPort(t, port), "root", cred)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}

	pinned := strings.TrimSpace(string(cryptossh.MarshalAuthorizedKey(hostKey)))
	if got := hostKeys.keys[addr]; got != pinned {
		t.Fatalf("pin store = %q, want %q", got, pinned)
	}
}

func mustPort(t *testing.T, port string) int {
	t.Helper()
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// startTestSSHServer boots an in-process SSH server on a random loopback
// port that accepts password auth and answers one exec channel.
func startTestSSHServer(t *testing.T) (addr string, hostKey cryptossh.PublicKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := cryptossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &cryptossh.ServerConfig{
		PasswordCallback: func(cryptossh.ConnMetadata, []byte) (*cryptossh.Permissions, error) {
			return &cryptossh.Permissions{}, nil
		},
	}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			ncc, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				sc, chans, reqs, err := cryptossh.NewServerConn(ncc, cfg)
				if err != nil {
					ncc.Close()
					return
				}
				defer sc.Close()
				go func() {
					for r := range reqs {
						if r.WantReply {
							r.Reply(false, nil)
						}
					}
				}()
				for nc := range chans {
					nc.Reject(cryptossh.Prohibited, "no channels")
				}
			}()
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String(), signer.PublicKey()
}

func TestBriefCommand(t *testing.T) {
	if got := briefCommand("true"); got != `"true"` {
		t.Fatalf("short command = %q", got)
	}
	long := strings.Repeat("x", 400)
	got := briefCommand(long)
	if !strings.HasPrefix(got, `"xxxx`) || !strings.HasSuffix(got, `"...(truncated)`) {
		t.Fatalf("long command not truncated: %q", got)
	}
	if len(got) > 200 {
		t.Fatalf("truncated command still too long: %d", len(got))
	}
}
