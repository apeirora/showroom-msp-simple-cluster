package target

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
)

func TestAccountIdentityRejectsUntrustedScope(t *testing.T) {
	for _, tc := range []struct{ name, cluster, namespace, remoteName string }{
		{"missing", "", "default", "demo"}, {"other_account", "other", "default", "demo"},
		{"wildcard", "*", "default", "demo"}, {"path", "account/../other", "default", "demo"},
		{"namespace_path", "account", "../other", "demo"}, {"other_parent", "account", "default", "other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sc := &showroomv1alpha1.SimpleCluster{ObjectMeta: metav1.ObjectMeta{Namespace: "account", Name: "demo", Labels: map[string]string{RemoteClusterLabel: tc.cluster}, Annotations: map[string]string{RemoteNamespaceAnnotation: tc.namespace, RemoteNameAnnotation: tc.remoteName}}}
			if _, err := AccountIdentity(sc); err == nil {
				t.Fatal("accepted untrusted account provenance")
			}
		})
	}
}

func TestAccountProviderScopesDiscoveryAndRequests(t *testing.T) {
	var server *httptest.Server
	var paths []string
	var mu sync.Mutex
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		var body any
		switch {
		case strings.HasSuffix(r.URL.Path, "/apiexportendpointslices"):
			body = map[string]any{"apiVersion": "apis.kcp.io/v1alpha1", "kind": "APIExportEndpointSliceList", "items": []any{map[string]any{"spec": map[string]any{"export": map[string]any{"name": "simpleclusters"}}, "status": map[string]any{"endpoints": []any{map[string]any{"url": server.URL + "/services/apiexport/provider/simpleclusters"}}}}}}
		case strings.HasSuffix(r.URL.Path, "/api"):
			body = map[string]any{"apiVersion": "v1", "kind": "APIVersions", "versions": []string{"v1"}}
		case strings.HasSuffix(r.URL.Path, "/api/v1"):
			body = map[string]any{"apiVersion": "v1", "kind": "APIResourceList", "groupVersion": "v1", "resources": []any{}}
		case strings.HasSuffix(r.URL.Path, "/apis"):
			body = map[string]any{"apiVersion": "v1", "kind": "APIGroupList", "groups": []any{map[string]any{"name": "showroom.apeirora.eu", "versions": []any{map[string]string{"groupVersion": "showroom.apeirora.eu/v1alpha1", "version": "v1alpha1"}}, "preferredVersion": map[string]string{"groupVersion": "showroom.apeirora.eu/v1alpha1", "version": "v1alpha1"}}}}
		case strings.HasSuffix(r.URL.Path, "/apis/showroom.apeirora.eu/v1alpha1"):
			body = map[string]any{"apiVersion": "v1", "kind": "APIResourceList", "groupVersion": "showroom.apeirora.eu/v1alpha1", "resources": []any{map[string]any{"name": "simpleclusters", "kind": "SimpleCluster", "namespaced": true, "verbs": []string{"get"}}}}
		case strings.HasSuffix(r.URL.Path, "/namespaces/default/simpleclusters/demo"):
			body = map[string]any{"apiVersion": "showroom.apeirora.eu/v1alpha1", "kind": "SimpleCluster", "metadata": map[string]string{"namespace": "default", "name": "demo", "uid": "parent"}}
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	provider := &AccountProvider{Config: &rest.Config{Host: server.URL + "/clusters/provider"}, APIExportName: "simpleclusters"}
	for _, cluster := range []string{"account-a", "account-b"} {
		tc, err := provider.ForAccount(context.Background(), cluster)
		if err != nil {
			t.Fatal(err)
		}
		ref := &showroomv1alpha1.AccountTargetStatus{Cluster: cluster, Namespace: "default", Name: "demo"}
		if _, err := tc.Parent(context.Background(), ref); err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(tc.RESTConfig.Host, "/clusters/"+cluster) {
			t.Fatal("unscoped account client")
		}
	}
	for _, path := range paths {
		if strings.Contains(path, "/namespaces/default/") && !strings.Contains(path, "/services/apiexport/provider/simpleclusters/clusters/account-") {
			t.Fatalf("parent read escaped scoped virtual workspace: %s", path)
		}
	}
	for _, bad := range []string{"*", "root:orgs:other", "../other", "", "other/x"} {
		if _, err := provider.ForAccount(context.Background(), bad); err == nil {
			t.Fatalf("accepted cluster %q", bad)
		}
	}
}

func TestAccountProviderRejectsForeignEndpoint(t *testing.T) {
	for _, endpoint := range []string{"https://other.invalid/services/apiexport/provider/simpleclusters", "/services/apiexport/provider/simpleclusters", "https://other.invalid/clusters/account"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"apiVersion":"apis.kcp.io/v1alpha1","kind":"APIExportEndpointSliceList","items":[{"spec":{"export":{"name":"simpleclusters"}},"status":{"endpoints":[{"url":"` + endpoint + `"}]}}]}`))
		}))
		p := &AccountProvider{Config: &rest.Config{Host: server.URL + "/clusters/provider"}, APIExportName: "simpleclusters"}
		if _, err := p.ForAccount(context.Background(), "account"); err == nil {
			t.Fatal("accepted untrusted endpoint")
		}
		server.Close()
	}
}

func TestAccountParentOwnershipRejectsReplacement(t *testing.T) {
	ref := &showroomv1alpha1.AccountTargetStatus{Cluster: "account", Namespace: "default", Name: "demo", UID: "original"}
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "sap.eu/v1alpha1", "kind": "IronCoreShoot", "metadata": map[string]any{"name": "shoot"}}}
	obj.SetAnnotations(parentAnnotations(ref))
	if err := ValidateParent(obj, ref); err != nil {
		t.Fatal(err)
	}
	other := *ref
	other.UID = "replacement"
	if err := ValidateParent(obj, &other); err == nil {
		t.Fatal("accepted another parent UID")
	}
	other = *ref
	other.Cluster = "other"
	if err := ValidateParent(obj, &other); err == nil {
		t.Fatal("accepted another account")
	}
	obj.SetAnnotations(nil)
	if err := ValidateParent(obj, ref); err == nil {
		t.Fatal("implicitly adopted an unowned object")
	}
}
