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
	"net/url"
	"strings"
	"time"

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

// DialOptions constrain how the tenant-supplied kubeconfig Secret may be
// dialed. The Secret is mirrored into (and created in) the tenant's own
// namespace, where tenant RBAC can edit it, so its content is untrusted
// input on every dial.
type DialOptions struct {
	// ServerAllowlist restricts the hostname of cluster.server in the
	// kubeconfig. Empty is permissive: any https host may be dialed, which
	// is the pre-allowlist behaviour operators keep by default. Entries are
	// exact hostnames (ports in the URL are ignored), matched
	// case-insensitively.
	ServerAllowlist []string

	// Timeout bounds client construction (the discovery round trips) and,
	// through rest.Config.Timeout, every request served with the resulting
	// config. A non-positive value selects DefaultDialTimeout. Without a
	// bound, one tenant's edited kubeconfig pointing at a black-hole
	// endpoint freezes a reconcile worker forever (F-KC-04).
	Timeout time.Duration
}

// DefaultDialTimeout is the DialOptions.Timeout fallback. The SimpleCluster
// controller runs a single reconcile worker by default, so an unbounded
// dial against a tenant-chosen endpoint stalls every other SimpleCluster
// behind it; 30s matches client-go's own connect-timeout precedent and is
// far below a healthy provider's discovery latency.
const DefaultDialTimeout = 30 * time.Second

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
	opts DialOptions,
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
	clientCfg, err := clientcmd.NewClientConfigFromBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}
	cfg, err := clientCfg.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}
	// Disable exec credential plugins (F-KC-01): clientcmd copies user.exec
	// into ExecProvider and client-go would fork+exec that binary, with the
	// kubeconfig's args and env, inside the operator pod on every dial. The
	// kubeconfig bytes are tenant-writable, so no plugin may run here.
	// client-go v0.37 offers no DisableExec on this parse path, so drop
	// the provider before any transport is built. Static token and
	// client-certificate auth are untouched, and insecure-skip-tls-verify
	// from the tenant kubeconfig stays honoured: the check below fixes
	// scheme and host only, so a tenant can still skip certificate trust
	// but never downgrade the dial to plaintext.
	cfg.ExecProvider = nil
	if err := validateServerURL(cfg.Host, opts.ServerAllowlist); err != nil {
		return nil, err
	}
	// Bound the dial (F-KC-04): rest.Config.Timeout covers every request
	// made with this config (discovery here, SSA and node listing in the
	// controller), and the derived context additionally caps construction
	// itself. It used to be zero unless overridden at the clientcmd call
	// site; the kubeconfig's own timeout: field is never applied on this
	// parse path, so the operator owns the bound.
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultDialTimeout
	}
	cfg.Timeout = timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	d, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("discovery client: %w", err)
	}
	// Contextual discovery: GetAPIGroupResources would dial with
	// context.Background(), ignoring both the reconcile cancellation and
	// the construction deadline set above.
	groups, err := restmapper.GetAPIGroupResourcesWithContext(ctx, dc)
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

// validateServerURL checks the tenant-supplied cluster.server before the
// first dial (F-KC-02). https-only: plaintext http would carry the tenant's
// own token unencrypted and makes the SSRF oracle trivially reachable. The
// allowlist is optional; empty keeps the permissive default so existing
// deployments dial whatever the provider provisions, while a restrictive
// operator can pin the provider's endpoint(s). Error strings land in
// tenant-visible status conditions, so they stay factual.
func validateServerURL(raw string, allowlist []string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse target server URL %q: %w", raw, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("target server %q: only https is supported", raw)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("target server %q: missing host", raw)
	}
	if len(allowlist) == 0 {
		return nil
	}
	for _, allowed := range allowlist {
		if strings.EqualFold(strings.TrimSpace(allowed), host) {
			return nil
		}
	}
	return fmt.Errorf("target server host %q is not in the operator's server allowlist", host)
}
