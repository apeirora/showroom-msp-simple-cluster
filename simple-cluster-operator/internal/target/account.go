package target

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
)

const (
	legacyBindingVersion      = "v1alpha1"
	RemoteClusterLabel        = "syncagent.kcp.io/remote-object-cluster"
	RemoteNamespaceAnnotation = "syncagent.kcp.io/remote-object-namespace"
	RemoteNameAnnotation      = "syncagent.kcp.io/remote-object-name"
	ParentClusterAnnotation   = "showroom.apeirora.eu/parent-cluster"
	ParentNamespaceAnnotation = "showroom.apeirora.eu/parent-namespace"
	ParentNameAnnotation      = "showroom.apeirora.eu/parent-name"
	ParentUIDAnnotation       = "showroom.apeirora.eu/parent-uid"
	AccountMode               = "Account"
	LegacyLocalMode           = "LegacyLocal"
)

var (
	endpointSlices = schema.GroupVersionResource{Group: "apis.kcp.io", Version: legacyBindingVersion, Resource: "apiexportendpointslices"}
	simpleClusters = schema.GroupVersionResource{Group: "showroom.apeirora.eu", Version: legacyBindingVersion, Resource: "simpleclusters"}
)

// AccountProvider uses the existing provider credential and export endpoint.
// It never obtains credentials from a consumer account.
type AccountProvider struct {
	Config           *rest.Config
	APIExportName    string
	LegacyParentUIDs map[string]bool
}

// ForAccount returns a client restricted to one logical cluster.
func (p *AccountProvider) ForAccount(ctx context.Context, cluster string) (*Client, error) {
	if p == nil || p.Config == nil || !validName(p.APIExportName) {
		return nil, fmt.Errorf("KCP account target is not configured")
	}
	if !validName(cluster) || strings.Contains(cluster, ":") {
		return nil, fmt.Errorf("invalid account logical cluster")
	}
	cfg := rest.CopyConfig(p.Config)
	cfg.Timeout = 30 * time.Second
	dc, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("provider discovery client: %w", err)
	}
	slices, err := dc.Resource(endpointSlices).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("discover provider export endpoint: %w", err)
	}
	base, err := url.Parse(p.Config.Host)
	if err != nil {
		return nil, fmt.Errorf("invalid provider server")
	}
	var endpoint string
	for i := range slices.Items {
		item := &slices.Items[i]
		export, _, _ := unstructured.NestedString(item.Object, "spec", "export", "name")
		path, _, _ := unstructured.NestedString(item.Object, "spec", "export", "path")
		if export != p.APIExportName || (path != "" && base.Path != "/clusters/"+path) {
			continue
		}
		endpoints, _, _ := unstructured.NestedSlice(item.Object, "status", "endpoints")
		for _, e := range endpoints {
			entry, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			// The provider-owned slice advertises the shard endpoint. Its host can differ from the front proxy.
			raw, _ := entry["url"].(string)
			u, err := url.Parse(raw)
			if err != nil || u.Scheme != base.Scheme || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
				continue
			}
			suffix := "/" + p.APIExportName
			if !strings.Contains(u.Path, "/services/apiexport/") || !strings.HasSuffix(strings.TrimRight(u.Path, "/"), suffix) {
				continue
			}
			endpoint = strings.TrimRight(raw, "/")
			break
		}
		if endpoint != "" {
			break
		}
	}
	if endpoint == "" {
		return nil, fmt.Errorf("no usable endpoint for provider APIExport %q", p.APIExportName)
	}
	cfg.Host = endpoint + "/clusters/" + cluster
	cfg.Timeout = 30 * time.Second
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, err
	}
	groups, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		return nil, fmt.Errorf("discover account APIs: %w", err)
	}
	return &Client{Dynamic: dyn, Mapper: restmapper.NewDiscoveryRESTMapper(groups), RESTConfig: cfg}, nil
}

func validName(name string) bool { return name != "" && len(validation.IsDNS1123Subdomain(name)) == 0 }

// AccountIdentity checks the provider-side provenance before selecting an account.
func AccountIdentity(sc *showroomv1alpha1.SimpleCluster) (*showroomv1alpha1.AccountTargetStatus, error) {
	cluster := sc.Labels[RemoteClusterLabel]
	ns := sc.Annotations[RemoteNamespaceAnnotation]
	name := sc.Annotations[RemoteNameAnnotation]
	if cluster != sc.Namespace || !validName(cluster) || !validName(ns) || !validName(name) || name != sc.Name {
		return nil, fmt.Errorf("SimpleCluster lacks valid account synchronization provenance")
	}
	return &showroomv1alpha1.AccountTargetStatus{Cluster: cluster, Namespace: ns, Name: name}, nil
}

// Parent reads the authoritative account UID instead of using the physical UID.
func (c *Client) Parent(ctx context.Context, ref *showroomv1alpha1.AccountTargetStatus) (*unstructured.Unstructured, error) {
	obj, err := c.Dynamic.Resource(simpleClusters).Namespace(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("read account SimpleCluster: %w", err)
	}
	if obj.GetUID() == "" || obj.GetNamespace() != ref.Namespace || obj.GetName() != ref.Name {
		return nil, fmt.Errorf("invalid account parent identity")
	}
	return obj, nil
}

// StampParent records explicit ownership without an invalid cross-scope ownerReference.
func (c *Client) StampParent(obj *unstructured.Unstructured) error {
	if c.AccountParent == nil {
		return nil
	}
	if !IsAccountIronCoreShoot(obj.GetAPIVersion(), obj.GetKind()) {
		return fmt.Errorf("KCP account target supports only sap.eu/v1alpha1 or apeiro.eu/v1alpha1 IronCoreShoot")
	}
	mapping, err := c.Mapper.RESTMapping(obj.GroupVersionKind().GroupKind(), obj.GroupVersionKind().Version)
	if err != nil {
		return err
	}
	if mapping.Scope.Name() != apimeta.RESTScopeNameRoot {
		return fmt.Errorf("account IronCoreShoot must be cluster-scoped")
	}
	obj.SetNamespace("")
	obj.SetOwnerReferences(nil)
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	for key, value := range parentAnnotations(c.AccountParent) {
		annotations[key] = value
	}
	obj.SetAnnotations(annotations)
	return nil
}

func parentAnnotations(ref *showroomv1alpha1.AccountTargetStatus) map[string]string {
	return map[string]string{ParentClusterAnnotation: ref.Cluster, ParentNamespaceAnnotation: ref.Namespace, ParentNameAnnotation: ref.Name, ParentUIDAnnotation: ref.UID}
}

// ValidateParent refuses existing objects owned by another request, including same-name replacements.
func ValidateParent(obj *unstructured.Unstructured, ref *showroomv1alpha1.AccountTargetStatus) error {
	if ref == nil {
		return nil
	}
	for key, value := range parentAnnotations(ref) {
		if value == "" || obj.GetAnnotations()[key] != value {
			return fmt.Errorf("target resource does not belong to this account SimpleCluster")
		}
	}
	if len(obj.GetOwnerReferences()) != 0 {
		return fmt.Errorf("account child must not have ownerReferences")
	}
	return nil
}

// IsAccountIronCoreShoot identifies the supported account child APIs.
func IsAccountIronCoreShoot(apiVersion, kind string) bool {
	return kind == "IronCoreShoot" && (apiVersion == "sap.eu/v1alpha1" || apiVersion == "apeiro.eu/v1alpha1")
}
