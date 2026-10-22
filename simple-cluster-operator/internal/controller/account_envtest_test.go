package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/target"
)

var envtestAccountProvider *target.AccountProvider

// accountProjectionClient adds sync-agent provenance to test parent projections.
type accountProjectionClient struct{ client.Client }

func (c accountProjectionClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if sc, ok := obj.(*showroomv1alpha1.SimpleCluster); ok {
		sc.Labels = map[string]string{target.RemoteClusterLabel: sc.Namespace}
		sc.Annotations = map[string]string{target.RemoteNamespaceAnnotation: sc.Namespace, target.RemoteNameAnnotation: sc.Name}
	}
	return c.Client.Create(ctx, obj, opts...)
}

// startEnvtestAccountProvider serves KCP metadata and forwards child operations to the real API server.
// The tests therefore exercise real resourceVersion, UID, namespace and finalizer semantics.
func startEnvtestAccountProvider(config *rest.Config) (*httptest.Server, error) {
	destination, err := url.Parse(config.Host)
	if err != nil {
		return nil, err
	}
	transport, err := rest.TransportFor(config)
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(destination)
	proxy.Transport = transport
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		if path == "/clusters/provider/apis/apis.kcp.io/v1alpha2/apiexports/simpleclusters.showroom.apeirora.eu" {
			_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "apis.kcp.io/v1alpha2", "kind": "APIExport", "status": map[string]any{"identityHash": "own-identity"}})
			return
		}
		if path == "/clusters/provider/apis/apis.kcp.io/v1alpha1/apiexportendpointslices" {
			_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "apis.kcp.io/v1alpha1", "kind": "APIExportEndpointSliceList", "items": []any{map[string]any{"spec": map[string]any{"export": map[string]any{"name": "simpleclusters.showroom.apeirora.eu"}}, "status": map[string]any{"endpoints": []any{map[string]any{"url": server.URL + "/services/apiexport/provider/simpleclusters.showroom.apeirora.eu"}}}}}})
			return
		}
		base := "/services/apiexport/provider/simpleclusters.showroom.apeirora.eu/clusters/default"
		if !strings.HasPrefix(path, base+"/") {
			http.Error(w, "account scope rejected", http.StatusForbidden)
			return
		}
		r.URL.Path = strings.TrimPrefix(path, base)
		switch r.URL.Path {
		case "/apis":
			dc, err := discovery.NewDiscoveryClientForConfig(config)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			groups, err := dc.ServerGroups()
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			version := metav1.GroupVersionForDiscovery{GroupVersion: "apis.kcp.io/v1alpha1", Version: "v1alpha1"}
			groups.Groups = append(groups.Groups, metav1.APIGroup{Name: "apis.kcp.io", Versions: []metav1.GroupVersionForDiscovery{version}, PreferredVersion: version})
			_ = json.NewEncoder(w).Encode(groups)
			return
		case "/apis/apis.kcp.io/v1alpha1":
			_ = json.NewEncoder(w).Encode(metav1.APIResourceList{GroupVersion: "apis.kcp.io/v1alpha1", APIResources: []metav1.APIResource{{Name: "apibindings", Kind: "APIBinding"}}})
			return
		case "/apis/apis.kcp.io/v1alpha1/apibindings":
			bindings := accountBindingList("unused.test")
			own := bindings["items"].([]any)[0].(map[string]any)
			claims := []any{}
			accepted := []any{}
			for _, resource := range []string{"configmaps", "secrets"} {
				claim := map[string]any{"group": "", "resource": resource, "all": true}
				claims = append(claims, claim)
				accepted = append(accepted, map[string]any{"group": "", "resource": resource, "all": true, "state": "Accepted"})
			}
			own["spec"] = map[string]any{"permissionClaims": accepted}
			own["status"].(map[string]any)["exportPermissionClaims"] = claims
			own["status"].(map[string]any)["appliedPermissionClaims"] = claims
			_ = json.NewEncoder(w).Encode(bindings)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	envtestAccountProvider = &target.AccountProvider{Config: &rest.Config{Host: server.URL + "/clusters/provider"}, APIExportName: "simpleclusters.showroom.apeirora.eu"}
	return server, nil
}

var _ = Describe("Account child receipts", func() {
	newGraph := func(name string, repeated, dependent bool) (*SimpleClusterReconciler, *showroomv1alpha1.SimpleCluster, *showroomv1alpha1.SimpleClusterProviderConfig) {
		resource := showroomv1alpha1.ResourceSpec{Name: "base", APIVersion: "v1", Kind: "ConfigMap", Template: "apiVersion: v1\nkind: ConfigMap\nmetadata: {}\ndata: {value: test}\n", Readiness: &showroomv1alpha1.ReadinessCheck{PhasePath: "kind", PhaseReadyValue: "ConfigMap"}}
		if repeated {
			resource.Repeat = "{{ .SC.workers.nodes }}"
		}
		pc := &showroomv1alpha1.SimpleClusterProviderConfig{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}, Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{Resources: []showroomv1alpha1.ResourceSpec{resource}, KubeconfigSource: showroomv1alpha1.KubeconfigSource{ResourceName: "base", Inline: &showroomv1alpha1.KubeconfigInlineSource{KubeconfigPath: "data.kubeconfig"}}}}
		if dependent {
			leaf := resource
			leaf.Name = "leaf"
			leaf.Repeat = ""
			leaf.DependsOn = []string{"base"}
			pc.Spec.Resources = append(pc.Spec.Resources, leaf)
		}
		sc := &showroomv1alpha1.SimpleCluster{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}, Spec: showroomv1alpha1.SimpleClusterSpec{ProviderConfigRefs: []string{name}, Workers: showroomv1alpha1.WorkersSpec{Nodes: ptr.To[int32](2)}}}
		Expect(k8sClient.Create(ctx, pc)).To(Succeed())
		Expect(k8sClient.Create(ctx, sc)).To(Succeed())
		r := &SimpleClusterReconciler{Client: k8sClient, APIReader: k8sClient, Scheme: k8sClient.Scheme(), RESTConfig: cfg, AccountProvider: envtestAccountProvider}
		DeferCleanup(func() {
			current := &showroomv1alpha1.SimpleCluster{}
			if k8sClient.Get(ctx, client.ObjectKeyFromObject(sc), current) == nil {
				current.Finalizers = nil
				_ = k8sClient.Update(ctx, current)
				_ = k8sClient.Delete(ctx, current)
			}
			for _, childName := range []string{name + "-base", name + "-base-0", name + "-base-1", name + "-leaf"} {
				cm := &corev1.ConfigMap{}
				if k8sClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: childName}, cm) == nil {
					cm.Finalizers = nil
					_ = k8sClient.Update(ctx, cm)
					_ = k8sClient.Delete(ctx, cm)
				}
			}
			_ = k8sClient.Delete(ctx, pc)
		})
		return r, sc, pc
	}
	reconcileGraph := func(r *SimpleClusterReconciler, sc *showroomv1alpha1.SimpleCluster) {
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sc)})
		Expect(err).NotTo(HaveOccurred())
	}
	It("deletes the recorded graph in reverse order after the SCPC disappears", func() {
		r, sc, pc := newGraph("receipt-order", false, true)
		reconcileGraph(r, sc)
		leaf := &corev1.ConfigMap{}
		leafKey := client.ObjectKey{Namespace: "default", Name: sc.Name + "-leaf"}
		baseKey := client.ObjectKey{Namespace: "default", Name: sc.Name + "-base"}
		Expect(k8sClient.Get(ctx, leafKey, leaf)).To(Succeed())
		leaf.Finalizers = []string{"test.example/hold"}
		Expect(k8sClient.Update(ctx, leaf)).To(Succeed())
		Expect(k8sClient.Delete(ctx, pc)).To(Succeed())
		Expect(k8sClient.Delete(ctx, sc)).To(Succeed())
		reconcileGraph(r, sc)
		base := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, baseKey, base)).To(Succeed())
		Expect(base.DeletionTimestamp.IsZero()).To(BeTrue())
		Expect(k8sClient.Get(ctx, leafKey, leaf)).To(Succeed())
		Expect(leaf.DeletionTimestamp.IsZero()).To(BeFalse())
		leaf.Finalizers = nil
		Expect(k8sClient.Update(ctx, leaf)).To(Succeed())
		reconcileGraph(r, sc)
		Expect(k8sClient.Get(ctx, baseKey, base)).To(Succeed())
		reconcileGraph(r, sc)
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, baseKey, base))).To(BeTrue())
		reconcileGraph(r, sc)
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(sc), &showroomv1alpha1.SimpleCluster{}))).To(BeTrue())
	})
	It("cleans recorded children after an SCPC changes to an unrelated graph", func() {
		r, sc, pc := newGraph("receipt-changed", false, false)
		reconcileGraph(r, sc)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pc), pc)).To(Succeed())
		pc.Spec.Resources[0].Kind = "Secret"
		pc.Spec.Resources[0].Namespace = "foreign"
		pc.Spec.Target.KubeconfigSecretRef = &corev1.SecretReference{Name: "unrelated", Namespace: "foreign"}
		Expect(k8sClient.Update(ctx, pc)).To(Succeed())
		Expect(k8sClient.Delete(ctx, sc)).To(Succeed())
		reconcileGraph(r, sc)
		reconcileGraph(r, sc)
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: sc.Name + "-base"}, &corev1.ConfigMap{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(sc), &showroomv1alpha1.SimpleCluster{}))).To(BeTrue())
	})

	It("removes only excess leaf instances during scale-down and preserves surviving UIDs", func() {
		r, sc, _ := newGraph("receipt-scale", true, false)
		reconcileGraph(r, sc)
		kept := &corev1.ConfigMap{}
		key := client.ObjectKey{Namespace: "default", Name: sc.Name + "-base-0"}
		Expect(k8sClient.Get(ctx, key, kept)).To(Succeed())
		uid := kept.UID
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(sc), sc)).To(Succeed())
		sc.Spec.Workers.Nodes = ptr.To[int32](1)
		Expect(k8sClient.Update(ctx, sc)).To(Succeed())
		for i := 0; i < 3; i++ {
			reconcileGraph(r, sc)
		}
		Expect(k8sClient.Get(ctx, key, kept)).To(Succeed())
		Expect(kept.UID).To(Equal(uid))
		removed := client.ObjectKey{Namespace: "default", Name: sc.Name + "-base-1"}
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, removed, &corev1.ConfigMap{}))).To(BeTrue())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(sc), sc)).To(Succeed())
		Expect(sc.Status.AccountResources).To(HaveLen(1))
	})
	It("blocks scale-down while a graph dependent still uses the instances", func() {
		r, sc, _ := newGraph("receipt-scale-block", true, true)
		reconcileGraph(r, sc)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(sc), sc)).To(Succeed())
		sc.Spec.Workers.Nodes = ptr.To[int32](1)
		Expect(k8sClient.Update(ctx, sc)).To(Succeed())
		reconcileGraph(r, sc)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(sc), sc)).To(Succeed())
		Expect(accountErrorReason(sc)).To(Equal("ScaleDownBlocked"))
		for _, name := range []string{sc.Name + "-base-0", sc.Name + "-base-1", sc.Name + "-leaf"} {
			child := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: name}, child)).To(Succeed())
			Expect(child.DeletionTimestamp.IsZero()).To(BeTrue())
		}
	})
})
