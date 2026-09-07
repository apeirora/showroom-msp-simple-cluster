package discovery

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	apisv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type inventoryInformer struct {
	cache.Informer
	synced bool
}

func (i inventoryInformer) HasSynced() bool { return i.synced }

type inventoryCache struct {
	cache.Cache
	synced    bool
	bindings  []apisv1alpha1.APIBinding
	err       error
	afterList func()
}

func (c *inventoryCache) GetInformer(context.Context, client.Object, ...cache.InformerGetOption) (cache.Informer, error) {
	return inventoryInformer{synced: c.synced}, nil
}

func (c *inventoryCache) List(_ context.Context, list client.ObjectList, _ ...client.ListOption) error {
	if c.afterList != nil {
		c.afterList()
	}
	if c.err != nil {
		return c.err
	}
	list.(*apisv1alpha1.APIBindingList).Items = c.bindings
	return nil
}

func TestSnapshotWaitsForEveryInitialCache(t *testing.T) {
	ctx := context.Background()
	first := &inventoryCache{synced: true, bindings: []apisv1alpha1.APIBinding{{ObjectMeta: metav1.ObjectMeta{Name: "arsys"}}}}
	second := &inventoryCache{bindings: []apisv1alpha1.APIBinding{{ObjectMeta: metav1.ObjectMeta{Name: "sap"}}}}
	d := &Discovery{ready: true, active: map[string]*endpoint{
		"first": {ctx: ctx, cache: first}, "second": {ctx: ctx, cache: second},
	}}
	called := false
	publish := func(bindings []apisv1alpha1.APIBinding) error {
		called = true
		if len(bindings) != 2 {
			t.Fatalf("partial inventory published: %v", bindings)
		}
		return nil
	}
	if err := d.WithSnapshot(ctx, publish); err == nil || called {
		t.Fatalf("unsynchronized cache must prevent ALL writes: called=%v err=%v", called, err)
	}
	second.synced = true
	if err := d.WithSnapshot(ctx, publish); err != nil || !called {
		t.Fatalf("complete cache inventory was not published: called=%v err=%v", called, err)
	}
}

func TestSnapshotFailuresNeverPublishPartialInventory(t *testing.T) {
	for _, failure := range []string{"discovery", "cache", "stopped", "stopped-during-list"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := &inventoryCache{synced: true}
			d := &Discovery{ready: true, active: map[string]*endpoint{"provider": {ctx: ctx, cache: c}}}
			switch failure {
			case "discovery":
				d.ready = false
			case "cache":
				c.err = errors.New("transient list failure")
			case "stopped":
				cancel()
			case "stopped-during-list":
				c.afterList = cancel
			}
			called := false
			err := d.WithSnapshot(context.Background(), func([]apisv1alpha1.APIBinding) error { called = true; return nil })
			if err == nil || called {
				t.Fatalf("incomplete snapshot published: called=%v err=%v", called, err)
			}
		})
	}
}

type discoveryClient struct {
	client.Client
	err error
}

func (c *discoveryClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if c.err != nil {
		return c.err
	}
	return c.Client.List(ctx, list, opts...)
}

func TestDiscoveryFailureRetainsProvidersUntilAuthoritativeRemoval(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := apisv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	slice := &apisv1alpha1.APIExportEndpointSlice{ObjectMeta: metav1.ObjectMeta{Name: "sap"}, Status: apisv1alpha1.APIExportEndpointSliceStatus{
		APIExportEndpoints: []apisv1alpha1.APIExportEndpoint{{URL: "https://example.test/export"}},
	}}
	cl := &discoveryClient{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(slice).Build()}
	removed := 0
	registered := 0
	d := &Discovery{log: logr.Discard(), clients: map[string]client.Client{"sap": cl}, active: map[string]*endpoint{}, lastListErr: map[string]string{},
		remove: func(string) { removed++ },
		register: func(ctx context.Context, key, url string) (*endpoint, error) {
			registered++
			return &endpoint{url: url, prefix: key, ctx: ctx, cache: &inventoryCache{synced: true}}, nil
		},
	}
	d.reconcile(ctx)
	if !d.ready || registered != 1 {
		t.Fatalf("initial discovery failed: ready=%v registrations=%d", d.ready, registered)
	}
	cl.err = errors.New("temporary Forbidden/unreachable")
	d.reconcile(ctx)
	if d.ready || removed != 0 || len(d.active) != 1 {
		t.Fatalf("failed discovery removed a serving provider: ready=%v removed=%d active=%d", d.ready, removed, len(d.active))
	}
	cl.err = nil
	d.reconcile(ctx)
	if !d.ready || registered != 1 || removed != 0 {
		t.Fatal("recovery replaced a healthy provider")
	}
	// A real, successfully observed endpoint deletion must still remove it.
	if err := cl.Delete(ctx, slice); err != nil {
		t.Fatal(err)
	}
	d.reconcile(ctx)
	if !d.ready || removed != 1 || len(d.active) != 0 {
		t.Fatal("authoritative endpoint deletion did not remove provider")
	}
}

func TestDiscoveryWaitsForUnreadyEndpointAndRetriesStoppedProvider(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := apisv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	slice := &apisv1alpha1.APIExportEndpointSlice{ObjectMeta: metav1.ObjectMeta{Name: "sap"}}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(slice).Build()
	runCtx, cancel := context.WithCancel(ctx)
	old := &endpoint{url: "https://example.test/export", prefix: "old", ctx: runCtx}
	registered, removed := 0, 0
	d := &Discovery{log: logr.Discard(), clients: map[string]client.Client{"sap": cl}, active: map[string]*endpoint{"sap|sap": old}, lastListErr: map[string]string{},
		remove: func(string) { removed++ },
		register: func(ctx context.Context, key, url string) (*endpoint, error) {
			registered++
			return &endpoint{ctx: ctx, url: url, prefix: key}, nil
		},
	}
	d.reconcile(ctx)
	if d.ready || removed != 0 {
		t.Fatal("unready endpoint was interpreted as unbind")
	}
	slice.Status.APIExportEndpoints = []apisv1alpha1.APIExportEndpoint{{URL: old.url}}
	if err := cl.Update(ctx, slice); err != nil {
		t.Fatal(err)
	}
	cancel()
	d.reconcile(ctx)
	if !d.ready || registered != 1 || removed != 1 || d.active["sap|sap"].ctx.Err() != nil {
		t.Fatal("stopped provider was not restarted")
	}
}

func TestDiscoveryWaitsForEveryConfiguredWorkspace(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := apisv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	slice := &apisv1alpha1.APIExportEndpointSlice{ObjectMeta: metav1.ObjectMeta{Name: "ready"}, Status: apisv1alpha1.APIExportEndpointSliceStatus{
		APIExportEndpoints: []apisv1alpha1.APIExportEndpoint{{URL: "https://example.test/export"}},
	}}
	ready := fake.NewClientBuilder().WithScheme(scheme).WithObjects(slice).Build()
	unreadable := &discoveryClient{Client: fake.NewClientBuilder().WithScheme(scheme).Build(), err: errors.New("initial LIST failed")}
	registered := 0
	d := &Discovery{log: logr.Discard(), clients: map[string]client.Client{"ready": ready, "unreadable": unreadable}, active: map[string]*endpoint{}, lastListErr: map[string]string{},
		register: func(ctx context.Context, key, url string) (*endpoint, error) {
			registered++
			return &endpoint{ctx: ctx, url: url, prefix: key, cache: &inventoryCache{synced: true}}, nil
		},
	}
	d.reconcile(ctx)
	if d.ready || registered != 0 {
		t.Fatal("partial workspace discovery registered a partial initial inventory")
	}
	called := false
	if err := d.WithSnapshot(ctx, func([]apisv1alpha1.APIBinding) error { called = true; return nil }); err == nil || called {
		t.Fatal("partial workspace discovery allowed annotation publication")
	}
	unreadable.err = nil
	d.reconcile(ctx)
	if !d.ready || registered != 1 {
		t.Fatal("successful discovery of all configured workspaces did not unblock initialization")
	}
}
