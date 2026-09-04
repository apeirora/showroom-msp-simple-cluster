package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/apeirora/showroom-msp-simple-cluster/bind-watcher-operator/internal/controller"
	"github.com/apeirora/showroom-msp-simple-cluster/bind-watcher-operator/internal/discovery"
	"github.com/apeirora/showroom-msp-simple-cluster/bind-watcher-operator/internal/index"
	apisv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	"github.com/m-szalik/goutils"

	"k8s.io/client-go/rest"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
	"sigs.k8s.io/multicluster-runtime/providers/multi"
)

func main() {
	ctrllog.SetLogger(zap.New(zap.UseDevMode(true)))
	ctx := signals.SetupSignalHandler()

	// MSP target: the local cluster this operator runs on, where namespaces are
	// annotated. A direct (uncached) client is enough for get + patch.
	mspCfg, err := config.GetConfig()
	goutils.ExitOnErrorf(err, 1, "cannot load MSP (in-cluster) config")
	mspClient, err := client.New(mspCfg, client.Options{Scheme: controller.Scheme})
	goutils.ExitOnErrorf(err, 1, "cannot create MSP client")

	// KCP source: providers are discovered from APIExportEndpointSlices in the
	// workspace the kubeconfig targets — no hard-coded endpoints.
	baseCfg, err := discovery.LoadKubeconfig(goutils.EnvRequired[string]("KCP_KUBECONFIG"))
	goutils.ExitOnErrorf(err, 1, "cannot load KCP kubeconfig")

	mp := multi.New(multi.Options{})
	mgr, err := mcmanager.New(&rest.Config{}, mp, mcmanager.Options{Scheme: controller.Scheme})
	goutils.ExitOnErrorf(err, 1, "cannot create multicluster manager")
	mp.SetManager(mgr)

	bc := controller.New(ctx, mgr, mspClient, index.New(), controller.Config{
		ClusterLabelKey:      goutils.Env("CLUSTER_LABEL_KEY", "kubernetes.io/metadata.name"),
		ExportsAnnotationKey: goutils.Env("EXPORTS_ANNOTATION_KEY", "bind.simple-cluster.apeirora.eu/exports"),
		Separator:            goutils.Env("EXPORTS_SEPARATOR", ","),
		ProviderSeparator:    "#",
		CreateNamespaces:     goutils.Env("CREATE_NAMESPACES", true),
		ManagedLabelKey:      goutils.Env("MANAGED_LABEL_KEY", "bind-watcher.apeirora.eu/managed"),
	})

	err = mcbuilder.ControllerManagedBy(mgr).
		Named(controller.ControllerName).
		For(&apisv1alpha1.APIBinding{}).
		Complete(mcreconcile.Func(bc.Reconcile))
	goutils.ExitOnErrorf(err, 1, "cannot build controller")

	// Register the controller as a cluster-aware runnable so it is notified of
	// disengage (unbind) and can clear the namespace annotation.
	err = mgr.Add(bc)
	goutils.ExitOnErrorf(err, 1, "cannot register disengage handler")

	workspaces := splitCSV(goutils.Env("KCP_WORKSPACES", ""))
	disc, err := discovery.New(mp, baseCfg, controller.Scheme, workspaces, goutils.Env("KCP_DISCOVERY_INTERVAL", time.Minute))
	goutils.ExitOnErrorf(err, 1, "cannot create discovery")
	bc.Inventory = disc
	disc.Sync = bc.SyncAll
	go func() {
		if err := disc.Start(ctx); err != nil {
			ctrllog.Log.Error(err, "discovery loop stopped")
		}
	}()

	if err := mgr.Start(ctx); ignoreCanceled(err) != nil {
		goutils.ExitOnErrorf(err, 1, "manager exited with error")
	}
}

func ignoreCanceled(err error) error {
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
