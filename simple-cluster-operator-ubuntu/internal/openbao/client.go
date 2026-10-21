/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package openbao is the small OpenBao client the ubuntu operator needs for
// SimpleCluster KMS encryption: log in with its own ServiceAccount token
// through a JWT auth mount, keep one Transit key and one AppRole role per
// cluster, hand each control-plane host a response-wrapped secret ID, and
// clean up roles and secret IDs when a cluster goes away.
//
// It speaks the HTTP API directly (no SDK) and never logs or returns a
// secret ID: hosts receive a single-use wrapping token and unwrap it
// themselves. Everything it may do is bounded by the fixed operator policy
// of the platform's simple-cluster tenancy (showroom-infra), which is why
// the request bodies below carry exactly the parameters that policy allows.
package openbao

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Config locates the tenancy. Zero values take the defaults below.
type Config struct {
	// Address is the OpenBao URL; https only, TLS always verified.
	Address string
	// Namespace is the tenancy namespace (X-Vault-Namespace); "" is root.
	Namespace string
	// CAFile holds extra PEM roots for Address when its certificate is not
	// publicly trusted. The same PEM goes to the hosts for the plugin.
	CAFile string
	// TokenFile is the projected ServiceAccount token (audience = the JWT
	// mount's bound audience) the operator logs in with.
	TokenFile string
	// JWTMount and JWTRole are the auth mount and role for that login.
	JWTMount, JWTRole string
	// TransitMount and ApproleMount are the tenancy's shared mounts.
	TransitMount, ApproleMount string
	// PluginPolicy is the only policy the operator may pin host roles to.
	PluginPolicy string
	// TokenPeriod is the hosts' periodic service token lifetime.
	TokenPeriod time.Duration
	// SecretIDTTL bounds how long a host's secret ID stays valid.
	SecretIDTTL time.Duration
	// WrapTTL is how long a host has to unwrap its secret ID.
	WrapTTL time.Duration
	// Timeout bounds each HTTP call.
	Timeout time.Duration
}

const (
	DefaultTokenFile    = "/var/run/secrets/openbao/token"
	DefaultJWTMount     = "simplecluster-kms-jwt"
	DefaultJWTRole      = "simplecluster-operator"
	DefaultTransitMount = "simplecluster-kms"
	DefaultApproleMount = "simplecluster-kms-approle"
	DefaultPluginPolicy = "sc-kms-plugin"
	DefaultTokenPeriod  = time.Hour
	DefaultSecretIDTTL  = 90 * 24 * time.Hour
	DefaultWrapTTL      = 5 * time.Minute
	DefaultTimeout      = 30 * time.Second
)

// Enabled reports whether an address is configured at all.
func (c Config) Enabled() bool { return c.Address != "" }

func (c Config) withDefaults() Config {
	def := func(s *string, d string) {
		if *s == "" {
			*s = d
		}
	}
	defd := func(d *time.Duration, v time.Duration) {
		if *d == 0 {
			*d = v
		}
	}
	def(&c.TokenFile, DefaultTokenFile)
	def(&c.JWTMount, DefaultJWTMount)
	def(&c.JWTRole, DefaultJWTRole)
	def(&c.TransitMount, DefaultTransitMount)
	def(&c.ApproleMount, DefaultApproleMount)
	def(&c.PluginPolicy, DefaultPluginPolicy)
	defd(&c.TokenPeriod, DefaultTokenPeriod)
	defd(&c.SecretIDTTL, DefaultSecretIDTTL)
	defd(&c.WrapTTL, DefaultWrapTTL)
	defd(&c.Timeout, DefaultTimeout)
	return c
}

// Client is one logged-in session. Create one per reconcile with New and
// Login, and RevokeSelf when done; tokens are short-lived by policy anyway.
type Client struct {
	cfg   Config
	http  *http.Client
	token string
	// caPEM is the CA bundle read from CAFile, forwarded to hosts.
	caPEM string
}

// APIError is a non-2xx answer from OpenBao with its error list.
type APIError struct {
	Status int
	Path   string
	Errors []string
}

func (e *APIError) Error() string {
	msg := strings.Join(e.Errors, "; ")
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	return fmt.Sprintf("openbao %s: HTTP %d: %s", e.Path, e.Status, msg)
}

// IsNotFound reports a 404 from OpenBao.
func IsNotFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusNotFound
}

// New validates the configuration and prepares the HTTP client. It does
// not talk to OpenBao.
func New(cfg Config) (*Client, error) {
	cfg = cfg.withDefaults()
	if !strings.HasPrefix(cfg.Address, "https://") {
		return nil, fmt.Errorf("openbao address %q must be https://", cfg.Address)
	}
	for _, v := range []string{cfg.Address, cfg.Namespace, cfg.TransitMount, cfg.ApproleMount, cfg.JWTMount, cfg.JWTRole} {
		if strings.ContainsAny(v, " \t\n\"'\\") {
			return nil, fmt.Errorf("openbao configuration value %q contains whitespace, quotes or backslashes", v)
		}
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	var caPEM string
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read openbao CA file: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("openbao CA file %s holds no PEM certificate", cfg.CAFile)
		}
		tlsCfg.RootCAs = pool
		caPEM = strings.TrimSpace(string(pem))
	}
	return &Client{
		cfg:   cfg,
		caPEM: caPEM,
		http: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: &http.Transport{TLSClientConfig: tlsCfg, Proxy: http.ProxyFromEnvironment},
		},
	}, nil
}

// Config returns the effective configuration (defaults applied).
func (c *Client) Config() Config { return c.cfg }

// CAPEM is the CA bundle hosts need to trust Address, or "" for public CAs.
func (c *Client) CAPEM() string { return c.caPEM }

type response struct {
	Data     map[string]any `json:"data"`
	WrapInfo *struct {
		Token string `json:"token"`
	} `json:"wrap_info"`
	Auth *struct {
		ClientToken string `json:"client_token"`
	} `json:"auth"`
	Errors []string `json:"errors"`
}

// do runs one API call. A nil body sends no body; a 404 on GET/DELETE is
// returned as *APIError so callers can treat it as absent.
func (c *Client) do(ctx context.Context, method, path string, body any, headers map[string]string) (*response, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.Address+"/v1/"+path, rd)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("X-Vault-Token", c.token)
	}
	if c.cfg.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", c.cfg.Namespace)
	}
	req.Header.Set("X-Vault-Request", "true")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openbao %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("openbao %s: read response: %w", path, err)
	}
	out := &response{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if jerr := json.Unmarshal(raw, out); jerr != nil && resp.StatusCode < 300 {
			return nil, fmt.Errorf("openbao %s: unreadable response: %w", path, jerr)
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{Status: resp.StatusCode, Path: path, Errors: out.Errors}
	}
	return out, nil
}

// Login reads the projected ServiceAccount token and logs in through the
// JWT mount. The resulting token carries the fixed operator policy.
func (c *Client) Login(ctx context.Context) error {
	jwt, err := os.ReadFile(c.cfg.TokenFile)
	if err != nil {
		return fmt.Errorf("read operator token: %w", err)
	}
	resp, err := c.do(ctx, http.MethodPost, "auth/"+c.cfg.JWTMount+"/login",
		map[string]string{"role": c.cfg.JWTRole, "jwt": strings.TrimSpace(string(jwt))}, nil)
	if err != nil {
		return fmt.Errorf("operator login: %w", err)
	}
	if resp.Auth == nil || resp.Auth.ClientToken == "" {
		return errors.New("operator login: no client token in response")
	}
	c.token = resp.Auth.ClientToken
	return nil
}

// RevokeSelf ends the session. Errors are returned but harmless: the token
// expires by itself.
func (c *Client) RevokeSelf(ctx context.Context) error {
	if c.token == "" {
		return nil
	}
	_, err := c.do(ctx, http.MethodPost, "auth/token/revoke-self", nil, nil)
	c.token = ""
	return err
}

// EnsureTransitKey creates the cluster's key when it does not exist and
// returns its latest version. Transit leaves an existing key untouched on a
// repeated create, which is what the operator policy relies on (it may not
// change key configuration). A soft-deleted key is an error: the cluster
// must not come up with a key it cannot use.
func (c *Client) EnsureTransitKey(ctx context.Context, name string, autoRotate time.Duration) (int64, error) {
	body := map[string]any{
		"type":                   "aes256-gcm96",
		"exportable":             false,
		"allow_plaintext_backup": false,
	}
	if autoRotate > 0 {
		body["auto_rotate_period"] = autoRotate.String()
	}
	if _, err := c.do(ctx, http.MethodPost, c.cfg.TransitMount+"/keys/"+name, body, nil); err != nil {
		return 0, fmt.Errorf("ensure transit key %q: %w", name, err)
	}
	resp, err := c.do(ctx, http.MethodGet, c.cfg.TransitMount+"/keys/"+name, nil, nil)
	if err != nil {
		return 0, fmt.Errorf("read transit key %q: %w", name, err)
	}
	if sd, _ := resp.Data["soft_deleted"].(bool); sd {
		return 0, fmt.Errorf("transit key %q is soft-deleted; an OpenBao admin must restore it before this cluster can encrypt", name)
	}
	if exp, _ := resp.Data["exportable"].(bool); exp {
		return 0, fmt.Errorf("transit key %q is exportable; refusing to use it", name)
	}
	ver, _ := resp.Data["latest_version"].(float64)
	return int64(ver), nil
}

// EnsureRole creates or updates the cluster's AppRole role, pinned to the
// plugin policy. Only token_policies with exactly that policy passes the
// operator's ACL; anything else is denied by OpenBao, not by this code.
func (c *Client) EnsureRole(ctx context.Context, role string) error {
	body := map[string]any{
		"token_policies":     c.cfg.PluginPolicy,
		"token_period":       int(c.cfg.TokenPeriod.Seconds()),
		"secret_id_ttl":      int(c.cfg.SecretIDTTL.Seconds()),
		"secret_id_num_uses": 0,
		"token_type":         "service",
	}
	if _, err := c.do(ctx, http.MethodPost, "auth/"+c.cfg.ApproleMount+"/role/"+role, body, nil); err != nil {
		return fmt.Errorf("ensure approle role %q: %w", role, err)
	}
	return nil
}

// RoleID reads the role's public identifier.
func (c *Client) RoleID(ctx context.Context, role string) (string, error) {
	resp, err := c.do(ctx, http.MethodGet, "auth/"+c.cfg.ApproleMount+"/role/"+role+"/role-id", nil, nil)
	if err != nil {
		return "", fmt.Errorf("read role-id of %q: %w", role, err)
	}
	id, _ := resp.Data["role_id"].(string)
	if id == "" {
		return "", fmt.Errorf("read role-id of %q: empty", role)
	}
	return id, nil
}

// MintWrappedSecretID issues a new secret ID for the role and returns only
// the single-use wrapping token (valid for WrapTTL). The secret ID itself
// never reaches the operator. metadata is attached to the secret ID so an
// admin can tell which host it belonged to.
func (c *Client) MintWrappedSecretID(ctx context.Context, role string, metadata map[string]string) (string, error) {
	body := map[string]any{}
	if len(metadata) > 0 {
		md, err := json.Marshal(metadata)
		if err != nil {
			return "", err
		}
		body["metadata"] = string(md)
	}
	resp, err := c.do(ctx, http.MethodPost, "auth/"+c.cfg.ApproleMount+"/role/"+role+"/secret-id", body,
		map[string]string{"X-Vault-Wrap-TTL": fmt.Sprintf("%ds", int(c.cfg.WrapTTL.Seconds()))})
	if err != nil {
		return "", fmt.Errorf("mint wrapped secret-id for %q: %w", role, err)
	}
	if resp.WrapInfo == nil || resp.WrapInfo.Token == "" {
		return "", fmt.Errorf("mint wrapped secret-id for %q: response was not wrapped", role)
	}
	return resp.WrapInfo.Token, nil
}

// DestroySecretIDAccessor revokes one secret ID by its accessor. An unknown
// accessor is not an error: it may already be gone.
func (c *Client) DestroySecretIDAccessor(ctx context.Context, role, accessor string) error {
	_, err := c.do(ctx, http.MethodPost, "auth/"+c.cfg.ApproleMount+"/role/"+role+"/secret-id-accessor/destroy",
		map[string]string{"secret_id_accessor": accessor}, nil)
	if err != nil && !IsNotFound(err) {
		return fmt.Errorf("destroy secret-id accessor of %q: %w", role, err)
	}
	return nil
}

// DeleteRole removes the role; the hosts' plugins can no longer log in once
// their current tokens expire. A missing role is not an error.
func (c *Client) DeleteRole(ctx context.Context, role string) error {
	_, err := c.do(ctx, http.MethodDelete, "auth/"+c.cfg.ApproleMount+"/role/"+role, nil, nil)
	if err != nil && !IsNotFound(err) {
		return fmt.Errorf("delete approle role %q: %w", role, err)
	}
	return nil
}
