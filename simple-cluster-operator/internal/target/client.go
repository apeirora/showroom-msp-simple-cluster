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
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	KubeconfigKey = "kubeconfig"
	// maxMetadataTypeMetaPrefixBytes bounds work and memory before the response
	// proves that it is PartialObjectMetadata. A conforming Kubernetes JSON
	// response writes its two short TypeMeta fields first.
	maxMetadataTypeMetaPrefixBytes = 4 << 10

	// partialObjectMetadataAccept deliberately omits application/json without
	// the PartialObjectMetadata parameters. client-go's metadata client
	// otherwise advertises a full-object fallback, which could transfer Secret
	// data before kubeconfig provenance requirements are validated.
	partialObjectMetadataAccept = "application/json;as=PartialObjectMetadata;g=meta.k8s.io;v=v1"
)

type strictMetadataRoundTripper struct {
	next http.RoundTripper
}

func (rt strictMetadataRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	strictReq := req.Clone(req.Context())
	strictReq.Header = req.Header.Clone()
	strictReq.Header.Set("Accept", partialObjectMetadataAccept)
	resp, err := rt.next.RoundTrip(strictReq)
	if err != nil {
		return resp, err
	}
	if resp == nil {
		return nil, fmt.Errorf("strict metadata transport returned no response")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return resp, nil
	}

	// A conforming kube-apiserver writes the negotiated representation with the
	// base JSON Content-Type, so the header alone cannot distinguish a full
	// object from PartialObjectMetadata. Require both the exact Kubernetes JSON
	// media type and exact PartialObjectMetadata TypeMeta before client-go decodes it.
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("strict metadata response Content-Type %q, want %q", got, "application/json")
	}
	// Validate TypeMeta before exposing any response byte to client-go. The
	// inspector stops as soon as apiVersion/kind are known, so an API server or
	// proxy that ignores negotiation and returns a full Secret is closed before
	// its data or stringData field is read. Valid prefix bytes are replayed
	// unchanged and the decoder consumes the remainder normally. This guards an
	// ordinary negotiation fallback; the target-server trust boundary still
	// assumes it does not forge a valid prefix and append duplicate TypeMeta or
	// non-metadata fields.
	resp.Body = newValidatedMetadataBody(resp.Body)
	return resp, nil
}

type validatedMetadataBody struct {
	source io.ReadCloser
	once   sync.Once
	prefix *bytes.Reader
	err    error
}

func newValidatedMetadataBody(source io.ReadCloser) io.ReadCloser {
	return &validatedMetadataBody{source: source}
}

func (b *validatedMetadataBody) Read(p []byte) (int, error) {
	b.once.Do(func() {
		inspector := &metadataPrefixInspector{source: b.source}
		b.err = inspector.inspectPartialObjectMetadata()
		if b.err != nil {
			_ = b.source.Close()
			return
		}
		b.prefix = bytes.NewReader(inspector.prefix.Bytes())
	})
	if b.err != nil {
		return 0, b.err
	}
	if b.prefix != nil && b.prefix.Len() > 0 {
		return b.prefix.Read(p)
	}
	return b.source.Read(p)
}

func (b *validatedMetadataBody) Close() error {
	return b.source.Close()
}

// metadataPrefixInspector reads one byte at a time and rejects an ordinary
// Kubernetes object as soon as its TypeMeta is known. Kubernetes' JSON
// serializer writes TypeMeta before metadata and payload fields; refusing any
// other field before TypeMeta keeps the fallback fail closed as well.
type metadataPrefixInspector struct {
	source io.Reader
	prefix bytes.Buffer
}

func (i *metadataPrefixInspector) readByte() (byte, error) {
	if i.prefix.Len() >= maxMetadataTypeMetaPrefixBytes {
		return 0, fmt.Errorf("TypeMeta prefix exceeds %d bytes", maxMetadataTypeMetaPrefixBytes)
	}
	var one [1]byte
	if _, err := io.ReadFull(i.source, one[:]); err != nil {
		return 0, err
	}
	_ = i.prefix.WriteByte(one[0])
	return one[0], nil
}

func (i *metadataPrefixInspector) nonSpace() (byte, error) {
	for {
		b, err := i.readByte()
		if err != nil {
			return 0, err
		}
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			return b, nil
		}
	}
}

func (i *metadataPrefixInspector) jsonString(first byte) (string, error) {
	if first != '"' {
		return "", fmt.Errorf("expected JSON string, got %q", first)
	}
	raw := []byte{'"'}
	escaped := false
	for {
		b, err := i.readByte()
		if err != nil {
			return "", err
		}
		raw = append(raw, b)
		if escaped {
			escaped = false
			continue
		}
		if b == '\\' {
			escaped = true
			continue
		}
		if b == '"' {
			return strconv.Unquote(string(raw))
		}
	}
}

func (i *metadataPrefixInspector) expect(expected byte) error {
	b, err := i.nonSpace()
	if err != nil {
		return err
	}
	if b != expected {
		return fmt.Errorf("expected %q, got %q", expected, b)
	}
	return nil
}

func (i *metadataPrefixInspector) inspectPartialObjectMetadata() error {
	if err := i.expect('{'); err != nil {
		return fmt.Errorf("cannot inspect strict metadata response: %w", err)
	}
	var apiVersion, kind string
	for {
		first, err := i.nonSpace()
		if err != nil {
			return fmt.Errorf("cannot inspect strict metadata response: %w", err)
		}
		key, err := i.jsonString(first)
		if err != nil {
			return fmt.Errorf("cannot inspect strict metadata response key: %w", err)
		}
		if key != "apiVersion" && key != "kind" {
			return fmt.Errorf("refusing strict metadata response with field %q before TypeMeta", key)
		}
		if err := i.expect(':'); err != nil {
			return err
		}
		valueStart, err := i.nonSpace()
		if err != nil {
			return err
		}
		value, err := i.jsonString(valueStart)
		if err != nil {
			return err
		}
		if key == "apiVersion" {
			apiVersion = value
		} else {
			kind = value
		}
		if apiVersion != "" && kind != "" {
			if apiVersion != "meta.k8s.io/v1" || kind != "PartialObjectMetadata" {
				return fmt.Errorf(
					"refusing non-metadata response %s %s; want meta.k8s.io/v1 PartialObjectMetadata",
					apiVersion, kind)
			}
			return nil
		}
		if err := i.expect(','); err != nil {
			return fmt.Errorf("strict metadata response ended before TypeMeta: %w", err)
		}
	}
}

func newStrictMetadataClient(cfg *rest.Config) (metadata.Interface, error) {
	strictCfg := rest.CopyConfig(cfg)
	strictCfg.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return strictMetadataRoundTripper{next: rt}
	})
	return metadata.NewForConfig(strictCfg)
}

// Client bundles dynamic and metadata clients with the matching RESTMapper
// and the underlying *rest.Config (needed for raw subresource POSTs that
// dynamic.Interface does not expose).
type Client struct {
	Dynamic    dynamic.Interface
	Metadata   metadata.Interface
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
		m, err := newStrictMetadataClient(mgmtCfg)
		if err != nil {
			return nil, fmt.Errorf("metadata client (local): %w", err)
		}
		return &Client{Dynamic: d, Metadata: m, Mapper: mgmtMapper, RESTConfig: mgmtCfg}, nil
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
	m, err := newStrictMetadataClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("metadata client: %w", err)
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
		Metadata:   m,
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
