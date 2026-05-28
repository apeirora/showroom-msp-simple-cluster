/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package ssh wraps golang.org/x/crypto/ssh for the operator's narrow
// use case: dial, run a small set of remote commands, read short files
// out of /etc and /var/lib via sudo.
package ssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	cryptossh "golang.org/x/crypto/ssh"
)

// DefaultPort is what the operator dials when SSHAuth.Port is unset.
const DefaultPort = 22

// dialTimeout caps how long a TCP dial may block. Hosts that don't
// respond quickly stay in Pending — the next reconcile retries.
const dialTimeout = 15 * time.Second

// Client wraps an *ssh.Client and a small set of conveniences.
type Client struct {
	conn *cryptossh.Client
}

// Dial opens an SSH connection. The caller MUST call Close.
// Host accepts both "1.2.3.4" and "host.example.com". When port == 0
// DefaultPort is used. The user is whatever sudo-capable login the
// caller already chose. acceptAnyHostKey is intentional — at this
// point in the lifecycle we have no way to pin host keys; production
// callers should layer a HostKeyCallback that consults a Secret.
func Dial(ctx context.Context, host string, port int, user string, privateKeyPEM []byte) (*Client, error) {
	if port == 0 {
		port = DefaultPort
	}
	signer, err := cryptossh.ParsePrivateKey(privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse ssh private key: %w", err)
	}
	cfg := &cryptossh.ClientConfig{
		User:            user,
		Auth:            []cryptossh.AuthMethod{cryptossh.PublicKeys(signer)},
		HostKeyCallback: cryptossh.InsecureIgnoreHostKey(),
		Timeout:         dialTimeout,
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	// Honour ctx for the initial dial by racing it against a goroutine.
	type dialResult struct {
		c   *cryptossh.Client
		err error
	}
	ch := make(chan dialResult, 1)
	go func() {
		c, err := cryptossh.Dial("tcp", addr, cfg)
		ch <- dialResult{c, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("ssh dial %s@%s: %w", user, addr, r.err)
		}
		return &Client{conn: r.c}, nil
	}
}

// Close shuts down the underlying SSH connection.
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Result captures stdout/stderr plus the remote exit status.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Run executes a remote command and returns the combined result.
// Non-zero exit is NOT treated as an error here — the caller decides
// what to do with ExitCode (e.g. probing for missing binaries).
// Network / SSH-level failures are returned as err.
func (c *Client) Run(ctx context.Context, command string) (*Result, error) {
	sess, err := c.conn.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh session: %w", err)
	}
	defer sess.Close()

	var stdout, stderr bytes.Buffer
	sess.Stdout = &stdout
	sess.Stderr = &stderr

	done := make(chan error, 1)
	go func() { done <- sess.Run(command) }()
	select {
	case <-ctx.Done():
		_ = sess.Signal(cryptossh.SIGKILL)
		return nil, ctx.Err()
	case err := <-done:
		res := &Result{Stdout: stdout.String(), Stderr: stderr.String()}
		if err != nil {
			var ex *cryptossh.ExitError
			if errors.As(err, &ex) {
				res.ExitCode = ex.ExitStatus()
				return res, nil
			}
			return res, fmt.Errorf("ssh run %q: %w", command, err)
		}
		return res, nil
	}
}

// RunSudo wraps Run with `sudo -n` so we get a hard failure rather than a
// password prompt if the login is not in sudoers.
func (c *Client) RunSudo(ctx context.Context, command string) (*Result, error) {
	return c.Run(ctx, "sudo -n "+command)
}

// MustSucceed is the typical caller flow: run, fail loudly if the remote
// exit code is non-zero, returning the combined stdout/stderr in the error.
func (c *Client) MustSucceed(ctx context.Context, command string) (string, error) {
	res, err := c.Run(ctx, command)
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return res.Stdout, fmt.Errorf("remote %q exited %d: %s", command, res.ExitCode, trimStderr(res.Stderr, res.Stdout))
	}
	return res.Stdout, nil
}

// MustSucceedSudo is MustSucceed via sudo -n.
func (c *Client) MustSucceedSudo(ctx context.Context, command string) (string, error) {
	res, err := c.RunSudo(ctx, command)
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return res.Stdout, fmt.Errorf("remote %q (sudo) exited %d: %s", command, res.ExitCode, trimStderr(res.Stderr, res.Stdout))
	}
	return res.Stdout, nil
}

func trimStderr(stderr, stdout string) string {
	out := stderr
	if out == "" {
		out = stdout
	}
	const maxLen = 400
	if len(out) > maxLen {
		out = out[:maxLen] + "...(truncated)"
	}
	return out
}
