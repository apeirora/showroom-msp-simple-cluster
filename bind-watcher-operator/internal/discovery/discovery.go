// Package discovery watches APIExportEndpointSlices in one or more KCP
// workspaces and keeps one apiexport provider registered per ready endpoint, so
// the operator learns which exports to observe without any hard-coded VW URLs.
package discovery

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	apisv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/kcp-dev/multicluster-provider/apiexport"
	"github.com/pkg/errors"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	multiclusterruntime "sigs.k8s.io/multicluster-runtime"
	"sigs.k8s.io/multicluster-runtime/providers/multi"
)

// Discovery periodically reconciles the set of apiexport providers against the
// APIExportEndpointSlices found in one or more KCP workspaces.
type Discovery struct {
	mu       sync.RWMutex
	mp       *multi.Provider
	baseCfg  *rest.Config
	scheme   *runtime.Scheme
	interval time.Duration
	log      logr.Logger

	// clients lists the workspaces to scan: workspace path -> client. A single
	// "" key means "use the kubeconfig's own workspace as-is".
	clients     map[string]client.Client
	active      map[string]*endpoint // "workspace|slice" -> current provider generation
	lastListErr map[string]string    // workspace -> last logged list error (dedup)
	excluded    map[string]struct{}  // exact "workspace|slice" keys explicitly out of scope
	ready       bool
	generation  uint64
	// Sync publishes an aggregate inventory after discovery, including when no
	// APIBinding events exist to trigger reconciliation. Set before Start.
	Sync     func(context.Context) error
	register func(context.Context, string, string) (*endpoint, error)
	remove   func(string)
}

type endpoint struct {
	url    string
	prefix string
	ctx    context.Context
	cache  cache.Cache
}

// New builds a Discovery. If workspaces is empty the kubeconfig's own workspace
// is scanned; otherwise each listed workspace path is scanned (same auth, host
// rewritten to that workspace).
func New(mp *multi.Provider, baseCfg *rest.Config, scheme *runtime.Scheme, workspaces []string, interval time.Duration, excludedKeys []string) (*Discovery, error) {
	excluded, err := excludedEndpoints(workspaces, excludedKeys)
	if err != nil {
		return nil, err
	}
	clients := map[string]client.Client{}
	if len(workspaces) == 0 {
		c, err := client.New(baseCfg, client.Options{Scheme: scheme})
		if err != nil {
			return nil, errors.Wrap(err, "creating discovery client")
		}
		clients[""] = c
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
		}
	}
	return &Discovery{
		mp:          mp,
		baseCfg:     baseCfg,
		scheme:      scheme,
		interval:    interval,
		log:         ctrllog.Log.WithName("discovery"),
		clients:     clients,
		active:      map[string]*endpoint{},
		lastListErr: map[string]string{},
		excluded:    excluded,
	}, nil
}

// Start runs the discovery loop until ctx is cancelled.
func (d *Discovery) Start(ctx context.Context) error {
	t := time.NewTicker(d.interval)
	defer t.Stop()
	d.reconcile(ctx)
	d.publish(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			d.reconcile(ctx)
			d.publish(ctx)
		}
	}
}

func (d *Discovery) reconcile(ctx context.Context) {
	d.mu.Lock()
	defer d.mu.Unlock()
	// Incomplete discovery is not evidence of an unbind. Keep existing
	// providers and annotations until every configured workspace is readable.
	d.ready = false
	complete := true
	desired := map[string]string{} // "workspace|slice" -> url
	for ws, cl := range d.clients {
		slices := &apisv1alpha1.APIExportEndpointSliceList{}
		if err := cl.List(ctx, slices); err != nil {
			// Log the same error only once (e.g. persistent Forbidden) instead
			// of every tick.
			if d.lastListErr[ws] != err.Error() {
				d.log.Error(err, "listing APIExportEndpointSlices", "workspace", wsLabel(ws))
				d.lastListErr[ws] = err.Error()
			}
			complete = false
			continue
		}
		delete(d.lastListErr, ws)
		for i := range slices.Items {
			key := ws + "|" + slices.Items[i].Name
			if _, excluded := d.excluded[key]; excluded {
				continue
			}
			if url := firstURL(&slices.Items[i]); url != "" {
				desired[key] = url
			} else {
				complete = false
			}
		}
	}
	if !complete {
		return
	}

	// Register new endpoints and re-register those whose URL changed.
	for key, url := range desired {
		if old := d.active[key]; old != nil && old.url == url && old.ctx.Err() == nil {
			continue
		}
		if old := d.active[key]; old != nil {
			d.removeProvider(old.prefix)
		}
		register := d.register
		if register == nil {
			register = d.add
		}
		ep, err := register(ctx, key, url)
		if err != nil {
			d.log.Error(err, "registering provider", "endpoint", key, "url", url)
			complete = false
			continue
		}
		d.active[key] = ep
		d.log.Info("watching export endpoint", "endpoint", key, "url", url)
	}

	// Drop endpoints that disappeared.
	for key := range d.active {
		if _, ok := desired[key]; ok {
			continue
		}
		d.removeProvider(d.active[key].prefix)
		delete(d.active, key)
		d.log.Info("stopped watching export endpoint", "endpoint", key)
	}
	d.ready = complete
}

// excludedEndpoints validates exact operator-selected endpoint keys. Exclusions
// do not relax discovery errors or the initial cache barrier for anything else.
func excludedEndpoints(workspaces, keys []string) (map[string]struct{}, error) {
	configured := map[string]struct{}{}
	if len(workspaces) == 0 {
		configured[""] = struct{}{}
	}
	for _, workspace := range workspaces {
		configured[workspace] = struct{}{}
	}
	excluded := map[string]struct{}{}
	for _, key := range keys {
		workspace, slice, ok := strings.Cut(key, "|")
		if !ok || len(validation.IsDNS1123Subdomain(slice)) != 0 || workspace == "*" || (workspace != "" && !logicalcluster.NewPath(workspace).IsValid()) {
			return nil, errors.Errorf("invalid excluded endpoint %q: expected an exact workspace|slice key", key)
		}
		if _, ok := configured[workspace]; !ok {
			return nil, errors.Errorf("excluded endpoint %q uses an unconfigured discovery workspace", key)
		}
		if _, duplicate := excluded[key]; duplicate {
			return nil, errors.Errorf("duplicate excluded endpoint %q", key)
		}
		excluded[key] = struct{}{}
	}
	return excluded, nil
}

func (d *Discovery) add(ctx context.Context, key, url string) (*endpoint, error) {
	cfg := rest.CopyConfig(d.baseCfg)
	cfg.Host = url
	prov, err := apiexport.New(cfg, apiexport.Options{Scheme: d.scheme})
	if err != nil {
		return nil, err
	}
	// multi.Provider removes a prefix asynchronously when Run returns. A new
	// generation must not reuse it: the previous Run could remove its successor.
	d.generation++
	runCtx, cancel := context.WithCancel(ctx)
	ep := &endpoint{url: url, prefix: fmt.Sprintf("%s-%d", prefix(key), d.generation), ctx: runCtx, cache: prov.GetWildcard()}
	if err := d.mp.AddProvider(runCtx, ep.prefix, prov, func(ctx context.Context, mgr multiclusterruntime.Manager) error {
		defer cancel()
		return prov.Run(ctx, mgr)
	}); err != nil {
		cancel()
		return nil, err
	}
	return ep, nil
}

func (d *Discovery) removeProvider(name string) {
	if d.remove != nil {
		d.remove(name)
		return
	}
	d.mp.RemoveProvider(name)
}

// WithSnapshot calls publish only with the full initial inventory from every
// discovered provider. It holds discovery stable while publication runs. Cache
// or transport failures leave the last published annotation untouched.
func (d *Discovery) WithSnapshot(ctx context.Context, publish func([]apisv1alpha1.APIBinding) error) error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if !d.ready {
		return errors.New("binding inventory is not ready: discovery is incomplete")
	}
	var bindings []apisv1alpha1.APIBinding
	for key, ep := range d.active {
		if ep.ctx.Err() != nil {
			return errors.Errorf("binding inventory is not ready: provider %s stopped", key)
		}
		informer, err := ep.cache.GetInformer(ctx, &apisv1alpha1.APIBinding{}, cache.BlockUntilSynced(false))
		if err != nil {
			return errors.Wrapf(err, "getting binding informer for %s", key)
		}
		if !informer.HasSynced() {
			return errors.Errorf("binding inventory is not ready: initial cache for %s is not synchronized", key)
		}
		list := &apisv1alpha1.APIBindingList{}
		if err := ep.cache.List(ctx, list); err != nil {
			return errors.Wrapf(err, "reading binding cache for %s", key)
		}
		bindings = append(bindings, list.Items...)
	}
	// Check cancellation again after collecting all caches, before publishing.
	for key, ep := range d.active {
		if ep.ctx.Err() != nil {
			return errors.Errorf("binding inventory is not ready: provider %s stopped", key)
		}
	}
	return publish(bindings)
}

func (d *Discovery) publish(ctx context.Context) {
	if d.Sync != nil {
		if err := d.Sync(ctx); err != nil && ctx.Err() == nil {
			d.log.Error(err, "preserving namespace annotations until binding inventory is complete")
		}
	}
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
