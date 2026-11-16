package charttests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func chartObjects(t *testing.T, chart string, args ...string) map[string]*unstructured.Unstructured {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm is required for chart tests")
	}
	path := filepath.Join("..", "..", "..", "charts", chart)
	command := []string{"template", "account-test", path, "--namespace", "sync-system"}
	command = append(command, args...)
	output, err := exec.Command("helm", command...).CombinedOutput()
	if err != nil {
		t.Fatalf("helm: %v: %s", err, output)
	}
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(output), 4096)
	objects := map[string]*unstructured.Unstructured{}
	for {
		obj := &unstructured.Unstructured{}
		if err := decoder.Decode(obj); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if obj.GetKind() != "" {
			objects[obj.GetKind()+"/"+obj.GetName()] = obj
		}
	}
	return objects
}
func stringAt(t *testing.T, obj map[string]any, path ...string) string {
	t.Helper()
	value, found, err := unstructured.NestedString(obj, path...)
	if err != nil || !found {
		t.Fatalf("missing %s: %v", strings.Join(path, "."), err)
	}
	return value
}
func render(t *testing.T, value, cluster, namespace, name string) string {
	t.Helper()
	funcs := template.FuncMap{"sha256sum": func(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }, "trunc": func(n int, s string) string {
		if len(s) > n {
			return s[:n]
		}
		return s
	}}
	tpl, err := template.New("name").Funcs(funcs).Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = tpl.Execute(&out, map[string]any{"ClusterName": cluster, "Object": map[string]any{"metadata": map[string]any{"namespace": namespace, "name": name}}})
	if err != nil {
		t.Fatal(err)
	}
	return out.String()
}
func TestAccountPublishedResources(t *testing.T) {
	objects := chartObjects(t, "simple-cluster-sync-agent")
	physical := map[string]bool{}
	for _, resource := range []struct {
		name, kind, group string
		refs              map[string]string
	}{
		{"sshkeys", "SSHKey", "sshkeys.apeirora.eu", map[string]string{"spec.exportToSecret.name": "service", "spec.passPhaseSecret.name": "kcp"}},
		{"ubuntuclusters", "UbuntuCluster", "ubuntu.apeirora.eu", map[string]string{"status.kubeconfigSecretRef.name": "service", "spec.ssh.privateKeySecretRef.name": "kcp", "spec.ssh.passwordSecretRef.name": "kcp", "spec.controlPlane.hosts.#.passwordSecretRef.name": "kcp", "spec.workers.hosts.#.passwordSecretRef.name": "kcp"}},
	} {
		obj := objects["PublishedResource/publish-"+resource.name]
		if obj == nil {
			t.Fatalf("missing %s", resource.kind)
		}
		for _, test := range []struct {
			path []string
			want string
		}{
			{[]string{"spec", "resource", "kind"}, resource.kind}, {[]string{"spec", "resource", "apiGroup"}, resource.group}, {[]string{"spec", "projection", "scope"}, "Namespaced"},
		} {
			if got := stringAt(t, obj.Object, test.path...); got != test.want {
				t.Fatalf("got %s want %s", got, test.want)
			}
		}
		namespace := stringAt(t, obj.Object, "spec", "naming", "namespace")
		for _, cluster := range []string{"account-a", "account-b"} {
			for _, ns := range []string{"default", "other"} {
				got := render(t, namespace, cluster, ns, "same-name")
				if physical[got] {
					t.Fatalf("physical namespace collision: %s", got)
				}
				physical[got] = true
				if len(got) > 63 {
					t.Fatal("namespace exceeds Kubernetes limit")
				}
				if render(t, stringAt(t, obj.Object, "spec", "naming", "name"), cluster, ns, "same-name") != "same-name" {
					t.Fatal("object name changed")
				}
			}
		}
		related, _, err := unstructured.NestedSlice(obj.Object, "spec", "related")
		if err != nil {
			t.Fatal(err)
		}
		if len(related) != len(resource.refs) {
			t.Fatalf("related resources: got %d want %d", len(related), len(resource.refs))
		}
		for _, value := range related {
			rel := value.(map[string]any)
			path := stringAt(t, rel, "object", "reference", "path")
			if stringAt(t, rel, "origin") != resource.refs[path] {
				t.Fatalf("wrong Secret origin for %s", path)
			}
			if stringAt(t, rel, "object", "namespace", "reference", "path") != "metadata.namespace" {
				t.Fatal("Secret namespace map is missing")
			}
		}
	}
	disabled := chartObjects(t, "simple-cluster-sync-agent", "--set", "publishedResources.enabled=false")
	for _, obj := range disabled {
		if obj.GetKind() == "PublishedResource" {
			t.Fatal("disabled publication still renders")
		}
	}
}

func TestPublishedStatusSchemas(t *testing.T) {
	for _, resource := range []struct{ module, file string }{
		{"sshkey-operator", "sshkeys.apeirora.eu_sshkeys.yaml"}, {"simple-cluster-operator-ubuntu", "ubuntu.apeirora.eu_ubuntuclusters.yaml"},
	} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", resource.module, "config", "crd", "bases", resource.file))
		if err != nil {
			t.Fatal(err)
		}
		obj := &unstructured.Unstructured{}
		if err := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096).Decode(obj); err != nil {
			t.Fatal(err)
		}
		versions, _, _ := unstructured.NestedSlice(obj.Object, "spec", "versions")
		for _, value := range versions {
			version := value.(map[string]any)
			if _, found, _ := unstructured.NestedMap(version, "subresources", "status"); !found {
				t.Fatal("status return requires the status subresource")
			}
			if resource.module == "sshkey-operator" {
				props, _, _ := unstructured.NestedMap(version, "schema", "openAPIV3Schema", "properties", "status", "properties")
				if props["publicKey"] == nil || props["privateKey"] != nil {
					t.Fatal("status must expose the public key only")
				}
			}
		}
	}
}

// The same account Secret travels out of SSHKey and into UbuntuCluster.
// Those two physical copies must never share a Kubernetes identity.
func TestSSHKeyExportSurvivesUbuntuCredentialDelete(t *testing.T) {
	objects := chartObjects(t, "simple-cluster-sync-agent")
	sk := objects["PublishedResource/publish-sshkeys"]
	uc := objects["PublishedResource/publish-ubuntuclusters"]
	skNamespace := render(t, stringAt(t, sk.Object, "spec", "naming", "namespace"), "account-a", "default", "key")
	ucNamespace := render(t, stringAt(t, uc.Object, "spec", "naming", "namespace"), "account-a", "default", "cluster")
	if skNamespace == ucNamespace {
		t.Fatal("SSHKey and UbuntuCluster share a physical namespace")
	}
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	source := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ssh-key", Namespace: skNamespace}, Data: map[string][]byte{"ssh-privatekey": []byte("provider-fixture")}}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(source).Build()
	ctx := context.Background()
	// A stale account value can update only the Ubuntu copy, never the provider export.
	for _, value := range []string{"stale-account-fixture", "provider-fixture", "rotated-fixture"} {
		dest := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: source.Name, Namespace: ucNamespace}}
		err := cl.Get(ctx, client.ObjectKeyFromObject(dest), dest)
		dest.Data = map[string][]byte{"ssh-privatekey": []byte(value)}
		if err != nil {
			err = cl.Create(ctx, dest)
		} else {
			err = cl.Update(ctx, dest)
		}
		if err != nil {
			t.Fatal(err)
		}
		var export corev1.Secret
		if err := cl.Get(ctx, client.ObjectKeyFromObject(source), &export); err != nil {
			t.Fatal(err)
		}
		if string(export.Data["ssh-privatekey"]) != "provider-fixture" {
			t.Fatal("account input overwrites the SSHKey export")
		}
	}
	// Sync Agent removes the origin:kcp physical copy when UbuntuCluster is deleted.
	if err := cl.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: source.Name, Namespace: ucNamespace}}); err != nil {
		t.Fatal(err)
	}
	if err := cl.Get(ctx, client.ObjectKeyFromObject(source), &corev1.Secret{}); err != nil {
		t.Fatalf("SSHKey export disappeared: %v", err)
	}
}

func TestAccountExportAndRBAC(t *testing.T) {
	objects := chartObjects(t, "simple-cluster-pm-integration", "--set", "rbac.enabled=true")
	export := objects["APIExport/simpleclusters.showroom.apeirora.eu"]
	if export == nil {
		t.Fatal("shared APIExport is missing")
	}
	claims, _, _ := unstructured.NestedSlice(export.Object, "spec", "permissionClaims")
	secretClaim := false
	for _, v := range claims {
		claim := v.(map[string]any)
		if claim["group"] == "" && claim["resource"] == "secrets" && claim["verbs"] != nil {
			secretClaim = true
		}
	}
	if !secretClaim {
		t.Fatal("account Secret permission claim is missing")
	}
	for _, chart := range []struct {
		name, role string
		args       []string
	}{
		{"simple-cluster-pm-integration", "syncagent-resources-access", []string{"--set", "rbac.enabled=true"}},
		{"simple-cluster-sync-agent", "api-syncagent:simple-cluster", nil},
	} {
		role := chartObjects(t, chart.name, chart.args...)["ClusterRole/"+chart.role]
		if role == nil {
			t.Fatal("sync role is missing")
		}
		rules, _, _ := unstructured.NestedSlice(role.Object, "rules")
		for _, want := range []string{"sshkeys", "sshkeys/status", "ubuntuclusters", "ubuntuclusters/status", "secrets"} {
			found := false
			for _, v := range rules {
				rule := v.(map[string]any)
				resources, _, _ := unstructured.NestedStringSlice(rule, "resources")
				verbs, _, _ := unstructured.NestedStringSlice(rule, "verbs")
				for _, resource := range resources {
					if resource == want {
						found = true
						for _, verb := range []string{"get", "list", "watch", "create", "update", "patch", "delete"} {
							if !contains(verbs, verb) {
								t.Fatalf("%s lacks %s on %s", chart.role, verb, want)
							}
						}
					}
				}
			}
			if !found {
				t.Fatalf("%s does not grant %s", chart.role, want)
			}
		}
	}
}
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestAPIBindingClaimIsReadOnly(t *testing.T) {
	objects := chartObjects(t, "simple-cluster-pm-integration", "--set", "rbac.enabled=true")
	export := objects["APIExport/simpleclusters.showroom.apeirora.eu"]
	if export.GetAPIVersion() != "apis.kcp.io/v1alpha2" {
		t.Fatal("verb claims require APIExport v1alpha2")
	}
	claims, _, _ := unstructured.NestedSlice(export.Object, "spec", "permissionClaims")
	count := 0
	for _, value := range claims {
		claim := value.(map[string]any)
		if claim["group"] != "apis.kcp.io" || claim["resource"] != "apibindings" {
			continue
		}
		count++
		verbs, _, _ := unstructured.NestedStringSlice(claim, "verbs")
		if len(verbs) != 3 || !contains(verbs, "get") || !contains(verbs, "list") || !contains(verbs, "watch") {
			t.Fatalf("unexpected APIBinding verbs: %v", verbs)
		}
	}
	if count != 1 {
		t.Fatalf("APIBinding claim count: %d", count)
	}
	role := objects["ClusterRole/syncagent-resources-access"]
	rules, _, _ := unstructured.NestedSlice(role.Object, "rules")
	for _, value := range rules {
		rule := value.(map[string]any)
		resources, _, _ := unstructured.NestedStringSlice(rule, "resources")
		if !contains(resources, "apibindings") {
			continue
		}
		verbs, _, _ := unstructured.NestedStringSlice(rule, "verbs")
		if len(verbs) != 3 || !contains(verbs, "get") || !contains(verbs, "list") || !contains(verbs, "watch") {
			t.Fatalf("unexpected APIBinding RBAC verbs: %v", verbs)
		}
	}
}

func TestLegacyAgentClaimSelectors(t *testing.T) {
	export := chartObjects(t, "simple-cluster-pm-integration")["APIExport/simpleclusters.showroom.apeirora.eu"]
	var claims []struct {
		Group    string `json:"group"`
		Resource string `json:"resource"`
		All      bool   `json:"all"`
	}
	if err := json.Unmarshal([]byte(export.GetAnnotations()["apis.v1alpha2.kcp.io/v1alpha1-permission-claims"]), &claims); err != nil {
		t.Fatal(err)
	}
	for _, resource := range []string{"namespaces", "events", "secrets", "apibindings"} {
		found := false
		for _, claim := range claims {
			if claim.Resource == resource && claim.All {
				found = true
			}
		}
		if !found {
			t.Fatalf("legacy claim selector is missing for %s", resource)
		}
	}
}

func TestAgentImageAndEndpoint(t *testing.T) {
	objects := chartObjects(t, "simple-cluster-sync-agent")
	found := false
	for _, obj := range objects {
		if obj.GetKind() != "Deployment" {
			continue
		}
		// The subchart copies image.tag into this label, so the tag must stay a
		// valid label value: no digest suffix (see charts/simple-cluster-sync-agent/values.yaml).
		labels, _, _ := unstructured.NestedStringMap(obj.Object, "spec", "template", "metadata", "labels")
		for key, value := range labels {
			if errs := validation.IsValidLabelValue(value); len(errs) > 0 {
				t.Fatalf("pod template label %s=%q is invalid: %v", key, value, errs)
			}
		}
		containers, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
		for _, value := range containers {
			container := value.(map[string]any)
			if container["image"] != "ghcr.io/kcp-dev/api-syncagent:v0.7.0" {
				continue
			}
			found = true
			args, _, _ := unstructured.NestedStringSlice(container, "args")
			if !contains(args, "--apiexportendpointslice-ref=simpleclusters.showroom.apeirora.eu") {
				t.Fatal("agent does not select the endpoint slice")
			}
		}
	}
	if !found {
		t.Fatal("pinned agent image is missing")
	}
}

func TestSSHKeyChartStatusSchema(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "sshkey-operator", "config", "crd", "bases", "sshkeys.apeirora.eu_sshkeys.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	chart, err := os.ReadFile(filepath.Join("..", "..", "..", "charts", "simple-cluster-system", "charts", "sshkey-operator", "templates", "crd", "sshkeys.apeirora.eu_sshkeys.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	status := strings.SplitN(string(source), "          status:", 2)
	if len(status) != 2 {
		t.Fatal("generated status schema is missing")
	}
	start := strings.Index(status[1], "              publicKey:")
	end := strings.Index(status[1], "              ready:")
	if start < 0 || end <= start || !strings.Contains(string(chart), status[1][start:end]) {
		t.Fatal("chart public key schema differs from the generated CRD")
	}
}
