package controller

import (
	"context"
	"strings"
	"time"

	"github.com/apeirora/showroom-msp-simple-cluster/bind-watcher-operator/internal/index"
	"github.com/go-logr/logr"
	apisv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

// ControllerName is used for logging and controller registration.
const ControllerName = "bind-watcher"

const cleanupTimeout = 30 * time.Second

// Config controls how KCP bindings are projected onto MSP namespaces.
type Config struct {
	// ClusterLabelKey selects the MSP namespace that "owns" a KCP logical
	// cluster: namespace.metadata.labels[ClusterLabelKey] == <logical cluster>.
	// This stays a label because it is used as a server-side selector.
	ClusterLabelKey string
	// ExportsAnnotationKey is written on the matched namespace(s) with the
	// sorted, separated list of exports currently bound in that cluster.
	ExportsAnnotationKey string
	// Separator joins export names in the annotation value. Annotation values
	// allow commas, so this defaults to ",".
	Separator string
	// ProviderSeparator is the multi-provider cluster-name separator ("#" by
	// default). It is stripped from the multicluster request name to recover
	// the real KCP logical cluster name.
	ProviderSeparator string
}

// BindingController reconciles APIBindings observed through KCP APIExport
// virtual workspaces and keeps the exports annotation on the mapped MSP
// namespace in sync with the set of bound exports. It is also registered as a
// multicluster.Aware runnable so it can clear that annotation when a workspace
// disengages (unbinds).
type BindingController struct {
	root  context.Context
	mgr   mcmanager.Manager
	msp   client.Client
	index *index.ExportIndex
	cfg   Config
	log   logr.Logger
}

// New constructs a BindingController. root is the operator's root context (used
// to distinguish a real disengage from operator shutdown); msp is a direct
// client to the local MSP cluster where namespaces are annotated.
func New(root context.Context, mgr mcmanager.Manager, msp client.Client, idx *index.ExportIndex, cfg Config) *BindingController {
	return &BindingController{
		root:  root,
		mgr:   mgr,
		msp:   msp,
		index: idx,
		cfg:   cfg,
		log:   ctrllog.Log.WithName(ControllerName),
	}
}

// Reconcile updates the in-memory export index for the changed APIBinding and
// re-renders the exports annotation on the owning MSP namespace.
func (c *BindingController) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	mcCluster := req.ClusterName
	realCluster := c.realCluster(mcCluster)

	cl, err := c.mgr.GetCluster(ctx, mcCluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	binding := &apisv1alpha1.APIBinding{}
	err = cl.GetClient().Get(ctx, req.Request.NamespacedName, binding)
	switch {
	case apierrors.IsNotFound(err):
		c.index.Remove(realCluster, mcCluster, req.Name)
	case err != nil:
		return ctrl.Result{}, err
	case !binding.DeletionTimestamp.IsZero():
		c.index.Remove(realCluster, mcCluster, binding.Name)
	default:
		c.index.Set(realCluster, mcCluster, binding.Name, exportName(binding))
	}

	return ctrl.Result{}, c.syncNamespaceAnnotation(ctx, realCluster)
}

// Engage is called when a workspace joins the fleet. It arms a cleanup that runs
// when the per-cluster context is cancelled, i.e. on unbind/disengage.
func (c *BindingController) Engage(ctx context.Context, name string, _ cluster.Cluster) error {
	mcCluster := name
	realCluster := c.realCluster(name)
	go func() {
		<-ctx.Done()
		// Operator shutdown cancels every cluster context too; that is not an
		// unbind, so leave the annotations untouched.
		if c.root.Err() != nil {
			return
		}
		c.index.RemoveCluster(realCluster, mcCluster)

		cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		if err := c.syncNamespaceAnnotation(cleanupCtx, realCluster); err != nil {
			c.log.Error(err, "failed to clean up namespace annotation on disengage", "cluster", realCluster)
		}
	}()
	return nil
}

// Start satisfies manager.Runnable; the work happens in Engage/Reconcile.
func (c *BindingController) Start(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// syncNamespaceAnnotation writes the current export list onto every MSP
// namespace that maps to the given KCP logical cluster.
func (c *BindingController) syncNamespaceAnnotation(ctx context.Context, realCluster string) error {
	nsList := &corev1.NamespaceList{}
	if err := c.msp.List(ctx, nsList, client.MatchingLabels{c.cfg.ClusterLabelKey: realCluster}); err != nil {
		return err
	}
	if len(nsList.Items) == 0 {
		c.log.V(1).Info("no MSP namespace maps to KCP cluster", "cluster", realCluster, "labelKey", c.cfg.ClusterLabelKey)
		return nil
	}
	for i := range nsList.Items {
		if err := c.writeAnnotation(ctx, nsList.Items[i].Name, realCluster); err != nil {
			return err
		}
	}
	return nil
}

// writeAnnotation renders the current export list for realCluster onto nsName,
// retrying on optimistic-lock conflicts so concurrent providers cannot lose an
// update.
func (c *BindingController) writeAnnotation(ctx context.Context, nsName, realCluster string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		ns := &corev1.Namespace{}
		if err := c.msp.Get(ctx, client.ObjectKey{Name: nsName}, ns); err != nil {
			return client.IgnoreNotFound(err)
		}

		value := strings.Join(c.index.Exports(realCluster), c.cfg.Separator)
		current, has := ns.Annotations[c.cfg.ExportsAnnotationKey]
		if (value == "" && !has) || value == current {
			return nil
		}

		base := ns.DeepCopy()
		if ns.Annotations == nil {
			ns.Annotations = map[string]string{}
		}
		if value == "" {
			delete(ns.Annotations, c.cfg.ExportsAnnotationKey)
		} else {
			ns.Annotations[c.cfg.ExportsAnnotationKey] = value
		}
		patch := client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})
		if err := c.msp.Patch(ctx, ns, patch); err != nil {
			return err
		}
		c.log.Info("updated namespace exports annotation", "namespace", nsName, "exports", value)
		return nil
	})
}

// realCluster strips the multi-provider prefix ("kcp-0#") to recover the KCP
// logical cluster name, which is also the value stored on the mapping label.
func (c *BindingController) realCluster(mcName string) string {
	sep := c.cfg.ProviderSeparator
	if sep == "" {
		sep = "#"
	}
	if idx := strings.Index(mcName, sep); idx >= 0 {
		return mcName[idx+len(sep):]
	}
	return mcName
}

func exportName(b *apisv1alpha1.APIBinding) string {
	if b.Spec.Reference.Export != nil && b.Spec.Reference.Export.Name != "" {
		return b.Spec.Reference.Export.Name
	}
	return b.Name
}
