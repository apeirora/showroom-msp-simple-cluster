// Package provider wires KCP APIExport virtual workspaces into the multicluster
// manager. Each endpoint becomes one apiexport provider whose engaged clusters
// are exactly the workspaces that bound that export.
package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	"github.com/pkg/errors"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	multiclusterruntime "sigs.k8s.io/multicluster-runtime"
	"sigs.k8s.io/multicluster-runtime/providers/multi"
)

// Options configures the KCP source providers.
type Options struct {
	// Kubeconfig is the path to a kubeconfig granting access to the APIExport
	// virtual workspaces (auth only; the host is overridden per endpoint).
	Kubeconfig string
	// Endpoints are APIExport virtual-workspace base URLs (up to but excluding
	// the '/clusters/*' suffix), taken from APIExportEndpointSlice status.
	Endpoints []string
	Scheme    *runtime.Scheme
}

// SplitEndpoints parses a comma-separated endpoint list.
func SplitEndpoints(csv string) []string {
	var out []string
	for _, e := range strings.Split(csv, ",") {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}

// AddKCPExportProviders registers one apiexport provider per endpoint under the
// prefixes "kcp-0", "kcp-1", ... on the given multi-provider.
func AddKCPExportProviders(ctx context.Context, mp *multi.Provider, opts Options) error {
	if len(opts.Endpoints) == 0 {
		return errors.New("no KCP APIExport endpoints configured")
	}
	base, err := loadConfig(opts.Kubeconfig)
	if err != nil {
		return err
	}

	for i, endpoint := range opts.Endpoints {
		cfg := rest.CopyConfig(base)
		cfg.Host = endpoint

		prov, err := apiexport.New(cfg, apiexport.Options{Scheme: opts.Scheme})
		if err != nil {
			return errors.Wrapf(err, "creating apiexport provider for %s", endpoint)
		}
		name := fmt.Sprintf("kcp-%d", i)
		if err := mp.AddProvider(ctx, name, prov, func(ctx context.Context, mgr multiclusterruntime.Manager) error {
			return prov.Run(ctx, mgr)
		}); err != nil {
			return errors.Wrapf(err, "adding provider %s", name)
		}
	}
	return nil
}

func loadConfig(path string) (*rest.Config, error) {
	loader := clientcmd.NewDefaultClientConfigLoadingRules()
	loader.ExplicitPath = path
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loader, &clientcmd.ConfigOverrides{}).ClientConfig()
}
