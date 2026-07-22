// Package discovery watches APIExportEndpointSlices in a single KCP workspace
// and keeps one apiexport provider registered per ready endpoint, so the
// operator learns which exports to observe without any hard-coded VW URLs.
package discovery

import (
	"context"
	"strings"
	"time"

	"github.com/go-logr/logr"
	apisv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	"github.com/kcp-dev/multicluster-provider/apiexport"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	multiclusterruntime "sigs.k8s.io/multicluster-runtime"
	"sigs.k8s.io/multicluster-runtime/providers/multi"
)

// Discovery periodically reconciles the set of apiexport providers against the
// APIExportEndpointSlices found in the workspace the base config points at.
type Discovery struct {
	kcp      client.Client // lists APIExportEndpointSlice in the workspace
	mp       *multi.Provider
	baseCfg  *rest.Config // auth/base; host is overridden per endpoint
	scheme   *runtime.Scheme
	interval time.Duration
	log      logr.Logger

	active map[string]string // sliceName -> URL currently registered
}

// New creates a Discovery. baseCfg must target the workspace that holds the
// APIExportEndpointSlices (typically the provider workspace).
func New(kcp client.Client, mp *multi.Provider, baseCfg *rest.Config, scheme *runtime.Scheme, interval time.Duration) *Discovery {
	return &Discovery{
		kcp:      kcp,
		mp:       mp,
		baseCfg:  baseCfg,
		scheme:   scheme,
		interval: interval,
		log:      ctrllog.Log.WithName("discovery"),
		active:   map[string]string{},
	}
}

// Start runs the discovery loop until ctx is cancelled.
func (d *Discovery) Start(ctx context.Context) error {
	t := time.NewTicker(d.interval)
	defer t.Stop()
	d.reconcile(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			d.reconcile(ctx)
		}
	}
}

func (d *Discovery) reconcile(ctx context.Context) {
	slices := &apisv1alpha1.APIExportEndpointSliceList{}
	if err := d.kcp.List(ctx, slices); err != nil {
		d.log.Error(err, "listing APIExportEndpointSlices")
		return
	}

	desired := map[string]string{}
	for i := range slices.Items {
		if url := firstURL(&slices.Items[i]); url != "" {
			desired[slices.Items[i].Name] = url
		}
	}

	// Register new endpoints and re-register those whose URL changed.
	for name, url := range desired {
		if d.active[name] == url {
			continue
		}
		if _, ok := d.active[name]; ok {
			d.mp.RemoveProvider(prefix(name))
		}
		if err := d.add(ctx, name, url); err != nil {
			d.log.Error(err, "registering provider", "slice", name, "url", url)
			continue
		}
		d.active[name] = url
		d.log.Info("watching export endpoint", "slice", name, "url", url)
	}

	// Drop endpoints that disappeared.
	for name := range d.active {
		if _, ok := desired[name]; ok {
			continue
		}
		d.mp.RemoveProvider(prefix(name))
		delete(d.active, name)
		d.log.Info("stopped watching export endpoint", "slice", name)
	}
}

func (d *Discovery) add(ctx context.Context, name, url string) error {
	cfg := rest.CopyConfig(d.baseCfg)
	cfg.Host = url
	prov, err := apiexport.New(cfg, apiexport.Options{Scheme: d.scheme})
	if err != nil {
		return err
	}
	return d.mp.AddProvider(ctx, prefix(name), prov, func(ctx context.Context, mgr multiclusterruntime.Manager) error {
		return prov.Run(ctx, mgr)
	})
}

func firstURL(s *apisv1alpha1.APIExportEndpointSlice) string {
	for _, e := range s.Status.APIExportEndpoints {
		if e.URL != "" {
			return e.URL
		}
	}
	return ""
}

// prefix is the multi-provider prefix for a slice. It must not contain the
// multi-provider separator '#'.
func prefix(sliceName string) string {
	return "kcp-" + strings.ReplaceAll(sliceName, "#", "-")
}

// LoadKubeconfig loads a rest.Config from a kubeconfig file path.
func LoadKubeconfig(path string) (*rest.Config, error) {
	loader := clientcmd.NewDefaultClientConfigLoadingRules()
	loader.ExplicitPath = path
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loader, &clientcmd.ConfigOverrides{}).ClientConfig()
}
