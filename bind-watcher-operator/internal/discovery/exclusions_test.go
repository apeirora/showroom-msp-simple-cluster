package discovery

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	apisv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestExcludedEndpointsValidation(t *testing.T) {
	for _, tt := range []struct {
		name             string
		workspaces, keys []string
		wantError        bool
	}{
		{name: "default excludes nothing"},
		{name: "exact configured endpoint", workspaces: []string{"root:providers:telekom"}, keys: []string{"root:providers:telekom|crossplane.internal.t-systems.com"}},
		{name: "kubeconfig workspace", keys: []string{"|unrelated"}},
		{name: "unknown workspace", workspaces: []string{"one"}, keys: []string{"two|slice"}, wantError: true},
		{name: "missing separator", workspaces: []string{"one"}, keys: []string{"slice"}, wantError: true},
		{name: "extra separator", workspaces: []string{"one"}, keys: []string{"one|two|slice"}, wantError: true},
		{name: "empty slice", workspaces: []string{"one"}, keys: []string{"one|"}, wantError: true},
		{name: "wildcard slice", workspaces: []string{"one"}, keys: []string{"one|*"}, wantError: true},
		{name: "wildcard workspace", workspaces: []string{"one"}, keys: []string{"*|slice"}, wantError: true},
		{name: "configured wildcard workspace", workspaces: []string{"*"}, keys: []string{"*|slice"}, wantError: true},
		{name: "malformed configured workspace", workspaces: []string{"root::invalid"}, keys: []string{"root::invalid|slice"}, wantError: true},
		{name: "invalid slice", workspaces: []string{"one"}, keys: []string{"one|Uppercase"}, wantError: true},
		{name: "duplicate key", workspaces: []string{"one"}, keys: []string{"one|slice", "one|slice"}, wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := excludedEndpoints(tt.workspaces, tt.keys)
			if (err != nil) != tt.wantError {
				t.Fatalf("error=%v wantError=%v", err, tt.wantError)
			}
			if err == nil && len(got) != len(tt.keys) {
				t.Fatalf("unexpected exclusion count %d", len(got))
			}
		})
	}
}

func TestExactExclusionLeavesEveryOtherBarrierIntact(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := apisv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	unrelated := &apisv1alpha1.APIExportEndpointSlice{ObjectMeta: metav1.ObjectMeta{Name: "crossplane.internal.t-systems.com"}}
	serving := &apisv1alpha1.APIExportEndpointSlice{ObjectMeta: metav1.ObjectMeta{Name: "telekom"}, Status: apisv1alpha1.APIExportEndpointSliceStatus{
		APIExportEndpoints: []apisv1alpha1.APIExportEndpoint{{URL: "https://example.test/telekom"}},
	}}
	cl := &discoveryClient{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(unrelated, serving).Build()}
	cache := &inventoryCache{}
	d := &Discovery{log: logr.Discard(), clients: map[string]client.Client{"root:providers:telekom": cl}, active: map[string]*endpoint{}, lastListErr: map[string]string{},
		register: func(ctx context.Context, key, url string) (*endpoint, error) {
			return &endpoint{ctx: ctx, url: url, prefix: key, cache: cache}, nil
		},
	}
	d.reconcile(ctx)
	if d.ready || len(d.active) != 0 {
		t.Fatal("default silently ignored an unready endpoint")
	}
	var err error
	d.excluded, err = excludedEndpoints([]string{"root:providers:telekom"}, []string{"root:providers:telekom|crossplane.internal.t-systems.com"})
	if err != nil {
		t.Fatal(err)
	}
	d.reconcile(ctx)
	if !d.ready || len(d.active) != 1 || d.active["root:providers:telekom|telekom"] == nil {
		t.Fatal("exact exclusion did not select only the serving endpoint")
	}
	if err := cl.Get(ctx, client.ObjectKey{Name: unrelated.Name}, &apisv1alpha1.APIExportEndpointSlice{}); err != nil {
		t.Fatalf("excluded object must remain untouched: %v", err)
	}
	called := false
	publish := func([]apisv1alpha1.APIBinding) error { called = true; return nil }
	if err := d.WithSnapshot(ctx, publish); err == nil || called {
		t.Fatal("exclusion bypassed serving provider's initial cache barrier")
	}
	cache.synced = true
	if err := d.WithSnapshot(ctx, publish); err != nil || !called {
		t.Fatalf("complete selected inventory did not publish: %v", err)
	}
	// A second workspace with the same slice name is not excluded.
	d.clients["another"] = fake.NewClientBuilder().WithScheme(scheme).WithObjects(unrelated.DeepCopy()).Build()
	d.reconcile(ctx)
	if d.ready {
		t.Fatal("exclusion matched a different workspace")
	}
	delete(d.clients, "another")
	cl.err = errors.New("workspace LIST unavailable")
	d.reconcile(ctx)
	if d.ready || len(d.active) != 1 {
		t.Fatal("exclusion hid a discovery error or removed the previous provider")
	}
}
