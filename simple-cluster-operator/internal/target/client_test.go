/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package target

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type countingReadCloser struct {
	reader *strings.Reader
	read   int
	closed bool
}

func (r *countingReadCloser) Read(data []byte) (int, error) {
	n, err := r.reader.Read(data)
	r.read += n
	return n, err
}

func (r *countingReadCloser) Close() error {
	r.closed = true
	return nil
}

func TestStrictMetadataClientDoesNotAdvertiseFullObjectFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.Header.Get("Accept"); got != partialObjectMetadataAccept {
			t.Errorf("Accept = %q, want exactly %q", got, partialObjectMetadataAccept)
			http.Error(w, "strict PartialObjectMetadata negotiation required", http.StatusNotAcceptable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
  "apiVersion":"meta.k8s.io/v1",
  "kind":"PartialObjectMetadata",
  "metadata":{"name":"claim-kubeconfig","namespace":"account-1","uid":"secret-uid","resourceVersion":"7"}
}`)
	}))
	t.Cleanup(server.Close)

	metadataClient, err := newStrictMetadataClient(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatalf("newStrictMetadataClient() error = %v", err)
	}
	secretGVR := schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
	got, err := metadataClient.Resource(secretGVR).Namespace("account-1").Get(
		context.Background(), "claim-kubeconfig", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("strict metadata GET error = %v", err)
	}
	if got.GetUID() != "secret-uid" || got.GetResourceVersion() != "7" {
		t.Fatalf("strict metadata GET returned UID=%q resourceVersion=%q", got.GetUID(), got.GetResourceVersion())
	}
}

func TestStrictMetadataClientRejectsUnverifiedResponseEnvelope(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		wantErr     string
	}{
		{
			name:        "noncanonical content type",
			contentType: "application/json; charset=utf-8",
			body: `{
  "apiVersion":"meta.k8s.io/v1",
  "kind":"PartialObjectMetadata",
  "metadata":{"name":"claim-kubeconfig"}
}`,
			wantErr: `Content-Type "application/json; charset=utf-8", want "application/json"`,
		},
		{
			name:        "full Secret response",
			contentType: "application/json",
			body: `{
  "apiVersion":"v1",
  "kind":"Secret",
  "metadata":{"name":"claim-kubeconfig"},
  "data":{"kubeconfig":"c2VjcmV0"}
}`,
			wantErr: "refusing non-metadata response v1 Secret",
		},
		{
			name:        "wrong type metadata",
			contentType: "application/json",
			body: `{
  "apiVersion":"v1",
  "kind":"Secret",
  "metadata":{"name":"claim-kubeconfig"}
}`,
			wantErr: "want meta.k8s.io/v1 PartialObjectMetadata",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				_, _ = io.WriteString(w, tt.body)
			}))
			t.Cleanup(server.Close)

			metadataClient, err := newStrictMetadataClient(&rest.Config{Host: server.URL})
			if err != nil {
				t.Fatalf("newStrictMetadataClient() error = %v", err)
			}
			secretGVR := schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
			_, err = metadataClient.Resource(secretGVR).Namespace("account-1").Get(
				context.Background(), "claim-kubeconfig", metav1.GetOptions{})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("strict metadata GET error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestStrictMetadataClientRejectsDataObjectsBeforePayload(t *testing.T) {
	tests := []struct {
		name       string
		payload    string
		payloadKey string
		wantKind   string
	}{
		{
			name:       "Secret data",
			payload:    `{"apiVersion":"v1","kind":"Secret","metadata":{"name":"credential"},"data":{"token":"do-not-read"}}`,
			payloadKey: `"data"`,
			wantKind:   "Secret",
		},
		{
			name:       "Secret stringData",
			payload:    `{"kind":"Secret","apiVersion":"v1","metadata":{"name":"credential"},"stringData":{"token":"do-not-read"}}`,
			payloadKey: `"stringData"`,
			wantKind:   "Secret",
		},
		{
			name:       "SecretList items",
			payload:    `{"apiVersion":"v1","kind":"SecretList","metadata":{},"items":[{"data":{"token":"do-not-read"}}]}`,
			payloadKey: `"items"`,
			wantKind:   "SecretList",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &countingReadCloser{reader: strings.NewReader(tt.payload)}
			transport := strictMetadataRoundTripper{next: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if got := request.Header.Get("Accept"); got != partialObjectMetadataAccept {
					t.Fatalf("Accept = %q, want exactly %q", got, partialObjectMetadataAccept)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       source,
					Request:    request,
				}, nil
			})}
			request, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
				"https://target.example/api/v1/namespaces/account-1/secrets/credential", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Accept", "application/json")
			response, err := transport.RoundTrip(request)
			if err != nil {
				t.Fatal(err)
			}
			data, readErr := io.ReadAll(response.Body)
			if readErr == nil || !strings.Contains(readErr.Error(), "refusing non-metadata response v1 "+tt.wantKind) {
				t.Fatalf("unsafe response was not rejected: data=%q error=%v", data, readErr)
			}
			if len(data) != 0 {
				t.Fatalf("unsafe bytes reached the caller: %q", data)
			}
			if !source.closed {
				t.Fatal("unsafe response body was not closed")
			}
			payloadOffset := strings.Index(tt.payload, tt.payloadKey)
			if payloadOffset < 0 || source.read >= payloadOffset {
				t.Fatalf("guard read payload field before rejecting TypeMeta: read=%d payloadOffset=%d",
					source.read, payloadOffset)
			}
		})
	}
}

func TestStrictMetadataClientBoundsTypeMetaPrefix(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "leading whitespace",
			payload: strings.Repeat(" ", maxMetadataTypeMetaPrefixBytes+128) + `{"apiVersion":"v1","kind":"Secret","data":{"token":"do-not-read"}}`,
		},
		{
			name:    "oversized TypeMeta string",
			payload: `{"apiVersion":"` + strings.Repeat("x", maxMetadataTypeMetaPrefixBytes+128) + `","kind":"Secret","data":{"token":"do-not-read"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &countingReadCloser{reader: strings.NewReader(tt.payload)}
			transport := strictMetadataRoundTripper{next: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       source,
					Request:    request,
				}, nil
			})}
			request, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
				"https://target.example/api/v1/namespaces/account-1/secrets/credential", nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := transport.RoundTrip(request)
			if err != nil {
				t.Fatal(err)
			}
			data, readErr := io.ReadAll(response.Body)
			if readErr == nil || !strings.Contains(readErr.Error(), "TypeMeta prefix exceeds 4096 bytes") {
				t.Fatalf("oversized prefix was not rejected: data=%q error=%v", data, readErr)
			}
			if len(data) != 0 {
				t.Fatalf("oversized prefix bytes reached the caller: %d", len(data))
			}
			if !source.closed {
				t.Fatal("oversized response body was not closed")
			}
			if source.read != maxMetadataTypeMetaPrefixBytes {
				t.Fatalf("source read %d bytes, want bounded read of %d", source.read, maxMetadataTypeMetaPrefixBytes)
			}
		})
	}
}

func TestStrictMetadataClientDoesNotRetryWithFullObjects(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests.Add(1)
		if got := req.Header.Get("Accept"); got != partialObjectMetadataAccept {
			t.Errorf("Accept = %q, want exactly %q", got, partialObjectMetadataAccept)
		}
		http.Error(w, "PartialObjectMetadata is not supported", http.StatusNotAcceptable)
	}))
	t.Cleanup(server.Close)

	metadataClient, err := newStrictMetadataClient(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatalf("newStrictMetadataClient() error = %v", err)
	}
	secretGVR := schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
	_, err = metadataClient.Resource(secretGVR).Namespace("account-1").Get(
		context.Background(), "claim-kubeconfig", metav1.GetOptions{})
	if err == nil {
		t.Fatal("strict metadata GET succeeded against a server that rejected PartialObjectMetadata")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("metadata GET made %d requests, want one request and no full-object retry", got)
	}
}
