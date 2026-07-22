package main

import (
	"context"
	"errors"

	"github.com/apeirora/showroom-msp-simple-cluster/bind-watcher-operator/internal/controller"
	"github.com/apeirora/showroom-msp-simple-cluster/bind-watcher-operator/internal/index"
	"github.com/apeirora/showroom-msp-simple-cluster/bind-watcher-operator/internal/provider"
	"github.com/m-szalik/goutils"
	apisv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"

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
	// labelled. A direct (uncached) client is enough for list + patch.
	mspCfg, err := config.GetConfig()
	goutils.ExitOnErrorf(err, 1, "cannot load MSP (in-cluster) config")
	mspClient, err := client.New(mspCfg, client.Options{Scheme: controller.Scheme})
	goutils.ExitOnErrorf(err, 1, "cannot create MSP client")

	// KCP sources: one APIExport virtual-workspace provider per endpoint.
	mp := multi.New(multi.Options{})
	mgr, err := mcmanager.New(&rest.Config{}, mp, mcmanager.Options{Scheme: controller.Scheme})
	goutils.ExitOnErrorf(err, 1, "cannot create multicluster manager")
	mp.SetManager(mgr)

	err = provider.AddKCPExportProviders(ctx, mp, provider.Options{
		Kubeconfig: goutils.EnvRequired[string]("KCP_KUBECONFIG"),
		Endpoints:  provider.SplitEndpoints(goutils.EnvRequired[string]("KCP_API_EXPORT_ENDPOINTS")),
		Scheme:     controller.Scheme,
	})
	goutils.ExitOnErrorf(err, 1, "cannot add KCP APIExport providers")

	bc := controller.New(ctx, mgr, mspClient, index.New(), controller.Config{
		ClusterLabelKey:      goutils.Env("CLUSTER_LABEL_KEY", "kcp.io/cluster"),
		ExportsAnnotationKey: goutils.Env("EXPORTS_ANNOTATION_KEY", "bind.simple-cluster.apeirora.eu/exports"),
		Separator:            goutils.Env("EXPORTS_SEPARATOR", ","),
		ProviderSeparator:    "#",
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
