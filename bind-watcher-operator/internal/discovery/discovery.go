// Package discovery watches APIExportEndpointSlices in one or more KCP
// workspaces and keeps one apiexport provider registered per ready endpoint, so
// the operator learns which exports to observe without any hard-coded VW URLs.
package discovery

import (
	"context"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/kcp-dev/multicluster-provider/apiexport"
	apisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	"github.com/pkg/errors"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"sigs.k8s.io/multicluster-runtime/providers/multi"
)

// Discovery periodically reconciles the set of apiexport providers against the
// APIExportEndpointSlices found in one or more KCP workspaces.
type Discovery struct {
	mp       *multi.Provider
	baseCfg  *rest.Config
	scheme   *runtime.Scheme
	interval time.Duration
	log      logr.Logger

	// clients lists the workspaces to scan: workspace path -> client. A single
	// "" key means "use the kubeconfig's own workspace as-is".
	clients     map[string]client.Client
	cfgs        map[string]*rest.Config // workspace path -> rest config for that workspace
	active      map[string]string       // "workspace|slice" -> URL last seen
	lastListErr map[string]string       // workspace -> last logged list error (dedup)
}

// endpoint identifies one ready APIExportEndpointSlice found during a scan.
type endpoint struct {
	ws, name, url string
}

// New builds a Discovery. If workspaces is empty the kubeconfig's own workspace
// is scanned; otherwise each listed workspace path is scanned (same auth, host
// rewritten to that workspace).
func New(mp *multi.Provider, baseCfg *rest.Config, scheme *runtime.Scheme, workspaces []string, interval time.Duration) (*Discovery, error) {
	clients := map[string]client.Client{}
	cfgs := map[string]*rest.Config{}
	if len(workspaces) == 0 {
		c, err := client.New(baseCfg, client.Options{Scheme: scheme})
		if err != nil {
			return nil, errors.Wrap(err, "creating discovery client")
		}
		clients[""] = c
		cfgs[""] = baseCfg
	} else {
		base := kcpBase(baseCfg.Host)
		for _, ws := range workspaces {
			cfg := rest.CopyConfig(baseCfg)
			cfg.Host = base + "/clusters/" + ws
			c, err := client.New(cfg, client.Options{Scheme: scheme})
			if err != nil {
				return nil, errors.Wrapf(err, "creating discovery client for workspace %s", ws)
			}
			clients[ws] = c
			cfgs[ws] = cfg
		}
	}
	return &Discovery{
		mp:          mp,
		baseCfg:     baseCfg,
		scheme:      scheme,
		interval:    interval,
		log:         ctrllog.Log.WithName("discovery"),
		clients:     clients,
		cfgs:        cfgs,
		active:      map[string]string{},
		lastListErr: map[string]string{},
	}, nil
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
	desired := map[string]endpoint{} // "workspace|slice" -> endpoint
	for ws, cl := range d.clients {
		slices := &apisv1alpha1.APIExportEndpointSliceList{}
		if err := cl.List(ctx, slices); err != nil {
			// Log the same error only once (e.g. persistent Forbidden) instead
			// of every tick.
			if d.lastListErr[ws] != err.Error() {
				d.log.Error(err, "listing APIExportEndpointSlices", "workspace", wsLabel(ws))
				d.lastListErr[ws] = err.Error()
			}
			continue
		}
		delete(d.lastListErr, ws)
		for i := range slices.Items {
			if url := firstURL(&slices.Items[i]); url != "" {
				desired[ws+"|"+slices.Items[i].Name] = endpoint{ws: ws, name: slices.Items[i].Name, url: url}
			}
		}
	}

	// Register new endpoints. URL changes need no re-registration: the provider
	// watches its endpoint slice and follows the URL itself.
	for key, ep := range desired {
		if _, ok := d.active[key]; ok {
			d.active[key] = ep.url
			continue
		}
		if err := d.add(ep); err != nil {
			d.log.Error(err, "registering provider", "endpoint", key, "url", ep.url)
			continue
		}
		d.active[key] = ep.url
		d.log.Info("watching export endpoint", "endpoint", key, "url", ep.url)
	}

	// Drop endpoints that disappeared.
	for key := range d.active {
		if _, ok := desired[key]; ok {
			continue
		}
		d.mp.RemoveProvider(prefix(key))
		delete(d.active, key)
		d.log.Info("stopped watching export endpoint", "endpoint", key)
	}
}

func (d *Discovery) add(ep endpoint) error {
	cfg := rest.CopyConfig(d.cfgs[ep.ws])
	prov, err := apiexport.New(cfg, ep.name, apiexport.Options{Scheme: d.scheme})
	if err != nil {
		return err
	}
	return d.mp.AddProvider(prefix(ep.ws+"|"+ep.name), prov)
}

func firstURL(s *apisv1alpha1.APIExportEndpointSlice) string {
	for _, e := range s.Status.APIExportEndpoints {
		if e.URL != "" {
			return e.URL
		}
	}
	return ""
}

// prefix is the multi-provider prefix for an endpoint key. It must not contain
// the multi-provider separator '#'.
func prefix(key string) string {
	return "kcp-" + strings.NewReplacer("#", "-", "|", "-", ":", "-").Replace(key)
}

// kcpBase strips a trailing '/clusters/<path>' from a KCP host, leaving the base
// URL to which other workspace paths can be appended.
func kcpBase(host string) string {
	if i := strings.Index(host, "/clusters/"); i >= 0 {
		return host[:i]
	}
	return strings.TrimRight(host, "/")
}

func wsLabel(ws string) string {
	if ws == "" {
		return "<kubeconfig>"
	}
	return ws
}

// LoadKubeconfig loads a rest.Config from a kubeconfig file path.
func LoadKubeconfig(path string) (*rest.Config, error) {
	loader := clientcmd.NewDefaultClientConfigLoadingRules()
	loader.ExplicitPath = path
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loader, &clientcmd.ConfigOverrides{}).ClientConfig()
}
