/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package ssh

import (
	"fmt"
	"net"
	"strings"
	"sync"

	cryptossh "golang.org/x/crypto/ssh"
)

// hostKeyPins implements the trust-on-first-use (TOFU) host key policy
// for every dial this operator makes (F-UB-2).
//
// Why pinning matters here: the dialed addresses come from the CR
// (tenant- or provider-authored) and the sessions carry credentials
// (password logins, the k3s node-token, kubeconfig fetches). With
// InsecureIgnoreHostKey an on-path attacker could not only capture the
// credential but also forge every answer (install success, token,
// kubeconfig) for the whole install. The first connection to a host
// trusts whatever key it sees (TOFU window); every later connection
// must present the same key or the dial fails loudly.
//
// Tradeoff, stated on purpose: the pin store is in-memory, so an
// operator restart opens one fresh TOFU window per host per process
// lifetime rather than trusting every connection unconditionally. A
// legitimate host-key rotation is therefore picked up by a restart,
// while an mid-life attack is fatal (reconcile errors until an
// operator resolves it). Persisting pins in a Secret (the pattern the
// ubuntu-machines admin credentials use) is the natural follow-up and
// needs a small API decision first; until then the window is bounded
// by the operator's uptime.
type hostKeyPins struct {
	mu   sync.Mutex
	keys map[string]string
}

// callbackFor returns the HostKeyCallback for a dial to addr: the first
// call for an address records the key, later calls must match it.
func (p *hostKeyPins) callbackFor(addr string) cryptossh.HostKeyCallback {
	return func(_ string, _ net.Addr, key cryptossh.PublicKey) error {
		got := strings.TrimSpace(string(cryptossh.MarshalAuthorizedKey(key)))
		p.mu.Lock()
		defer p.mu.Unlock()
		if want, seen := p.keys[addr]; seen {
			if want != got {
				return fmt.Errorf("ssh host key for %s changed: pinned %s, got %s (restart the operator to re-trust a legitimately rotated host key)", addr, want, got)
			}
			return nil
		}
		p.keys[addr] = got
		return nil
	}
}

// hostKeys is the process-wide pin store shared by Dial and DialVia.
var hostKeys = &hostKeyPins{keys: map[string]string{}}

// resetHostKeyPins clears the in-memory pin store (tests).
func resetHostKeyPins() {
	hostKeys.mu.Lock()
	defer hostKeys.mu.Unlock()
	hostKeys.keys = map[string]string{}
}
