package controller

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/apeirora/showroom-msp-simple-cluster/bind-watcher-operator/internal/index"
	"github.com/go-logr/logr"
	apisv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	"github.com/kcp-dev/logicalcluster/v3"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	// CreateNamespaces makes the operator create the MSP namespace (named after
	// the logical cluster) when a cluster has bound exports but no namespace
	// exists yet — e.g. an account that bound a provider without creating an
	// instance. Namespaces we create carry ManagedLabelKey.
	CreateNamespaces bool
	// ManagedLabelKey marks namespaces created by this operator, so they can be
	// told apart from sync-agent-created ones.
	ManagedLabelKey string
}

// BindingController reconciles APIBindings observed through KCP APIExport
// virtual workspaces and keeps the exports annotation on the mapped MSP
// namespace in sync with the set of bound exports. It is also registered as a
// multicluster.Aware runnable so it can clear that annotation when a workspace
// disengages (unbinds).
type BindingController struct {
	mu sync.Mutex
	// Inventory must be configured before starting the controller. Individual
	// events are triggers, never a complete inventory or proof of an unbind.
	Inventory interface {
		WithSnapshot(context.Context, func([]apisv1alpha1.APIBinding) error) error
	}
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

// Reconcile treats an event only as a trigger to publish a complete snapshot.
func (c *BindingController) Reconcile(ctx context.Context, _ mcreconcile.Request) (ctrl.Result, error) {
	return ctrl.Result{}, c.SyncAll(ctx)
}

// SyncAll serializes complete snapshots and annotation writes. Initial informer
// event order cannot publish a partial index. Listing annotated namespaces also
// reconciles unbinds that happened while the operator was stopped.
func (c *BindingController) SyncAll(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Inventory == nil {
		return fmt.Errorf("binding inventory is not configured")
	}
	return c.Inventory.WithSnapshot(ctx, func(bindings []apisv1alpha1.APIBinding) error {
		idx := index.New()
		clusters := map[string]struct{}{}
		seen := map[string]string{}
		for i := range bindings {
			b := &bindings[i]
			realCluster := logicalcluster.From(b).String()
			if realCluster == "" {
				return fmt.Errorf("binding %s is missing its logical cluster identity", b.Name)
			}
			if !b.DeletionTimestamp.IsZero() {
				continue
			}
			key := realCluster + "/" + b.Name
			if previous, ok := seen[key]; ok && previous != exportName(b) {
				return fmt.Errorf("binding %s has conflicting export identities across caches", key)
			}
			seen[key] = exportName(b)
			clusters[realCluster] = struct{}{}
			idx.SetPath(realCluster, b.Annotations["kcp.io/path"])
			// Multiple virtual workspaces can return the same binding. Its
			// logical cluster and name are the authoritative identity.
			idx.Set(realCluster, realCluster, b.Name, exportName(b))
		}
		namespaces := &corev1.NamespaceList{}
		if err := c.msp.List(ctx, namespaces); err != nil {
			return err
		}
		for i := range namespaces.Items {
			ns := &namespaces.Items[i]
			if _, annotated := ns.Annotations[c.cfg.ExportsAnnotationKey]; annotated {
				if id := ns.Labels[c.cfg.ClusterLabelKey]; id != "" {
					clusters[id] = struct{}{}
				}
			}
		}
		c.index = idx
		for id := range clusters {
			if err := c.syncNamespaceAnnotation(ctx, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// Engage requests a fresh snapshot on disengagement. Cancellation can be a
// transport failure or reconnect, so it must never delete index entries itself.
func (c *BindingController) Engage(ctx context.Context, name string, _ cluster.Cluster) error {
	realCluster := c.realCluster(name)
	go func() {
		<-ctx.Done()
		// Operator shutdown cancels every cluster context too; that is not an
		// unbind, so leave the annotations untouched.
		if c.root.Err() != nil {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		if err := c.SyncAll(cleanupCtx); err != nil {
			c.log.Error(err, "preserving namespace annotation on disengage until complete inventory is available", "cluster", realCluster)
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
		// Nothing bound and no namespace: nothing to record.
		if len(c.index.Exports(realCluster)) == 0 {
			c.log.V(1).Info("no MSP namespace and no exports for KCP cluster", "cluster", realCluster, "workspace", c.index.Path(realCluster))
			return nil
		}
		if !c.cfg.CreateNamespaces {
			c.log.V(1).Info("no MSP namespace maps to KCP cluster", "cluster", realCluster, "workspace", c.index.Path(realCluster), "labelKey", c.cfg.ClusterLabelKey)
			return nil
		}
		if err := c.ensureNamespace(ctx, realCluster); err != nil {
			return err
		}
		return c.writeAnnotation(ctx, realCluster, realCluster)
	}
	for i := range nsList.Items {
		if err := c.writeAnnotation(ctx, nsList.Items[i].Name, realCluster); err != nil {
			return err
		}
	}
	return nil
}

// ensureNamespace creates the MSP namespace named after the logical cluster,
// tagged with the managed marker. It is a no-op if the namespace already exists.
func (c *BindingController) ensureNamespace(ctx context.Context, name string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{c.cfg.ManagedLabelKey: "true", c.cfg.ClusterLabelKey: name},
		},
	}
	switch err := c.msp.Create(ctx, ns); {
	case apierrors.IsAlreadyExists(err):
		return nil
	case err != nil:
		return err
	}
	c.log.Info("created MSP namespace for KCP cluster", "namespace", name, "workspace", c.index.Path(name))
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
		c.log.Info("updated namespace exports annotation", "namespace", nsName, "workspace", c.index.Path(realCluster), "exports", value)
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
