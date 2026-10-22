package controller

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/target"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type accountFixture struct {
	group        string
	kind         string
	resource     string
	namespaced   bool
	bindingReads int
	revokeAt     int
	credential   []byte

	loseCreateResponse bool

	mu sync.Mutex

	server                    *httptest.Server
	child                     *unstructured.Unstructured
	parentUID                 string
	parentDeleting            bool
	forbidden                 bool
	creates, patches, deletes int
	deletedChild              bool
	paths                     []string
}

func newAccountFixture(t *testing.T, groups ...string) *accountFixture {
	t.Helper()
	group := "sap.eu"
	if len(groups) > 0 {
		group = groups[0]
	}
	f := &accountFixture{parentUID: "account-parent", group: group, kind: "IronCoreShoot", resource: "ironcoreshoots"}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

//nolint:gocyclo // HTTP fixture covers discovery, authorization and resource operations.
func (f *accountFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	var obj any
	base := "/services/apiexport/provider/simpleclusters.showroom.apeirora.eu/clusters/account-a"
	if r.URL.Path == "/physical/apis/"+f.group+"/v1alpha1/namespaces/account-a/ironcoreshoots/sc-child" {
		obj = f.child
	} else if r.URL.Path == "/physical/api/v1/namespaces/account-a/secrets/sc-child-ironcoreshoot-kubeconfig" {
		obj = &corev1.Secret{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}, ObjectMeta: metav1.ObjectMeta{Name: "sc-child-ironcoreshoot-kubeconfig", Namespace: "account-a"}, Data: map[string][]byte{"kubeconfig": f.credential}}
	} else if r.URL.Path == "/clusters/provider/apis/apis.kcp.io/v1alpha2/apiexports/simpleclusters.showroom.apeirora.eu" {
		obj = map[string]any{"apiVersion": "apis.kcp.io/v1alpha2", "kind": "APIExport", "metadata": map[string]any{"name": "simpleclusters.showroom.apeirora.eu"}, "status": map[string]any{"identityHash": "own-identity"}}
	} else if r.URL.Path == "/clusters/provider/apis/apis.kcp.io/v1alpha1/apiexportendpointslices" {
		obj = map[string]any{"apiVersion": "apis.kcp.io/v1alpha1", "kind": "APIExportEndpointSliceList", "items": []any{map[string]any{"spec": map[string]any{"export": map[string]any{"name": "simpleclusters.showroom.apeirora.eu"}}, "status": map[string]any{"endpoints": []any{map[string]any{"url": f.server.URL + "/services/apiexport/provider/simpleclusters.showroom.apeirora.eu"}}}}}}
	} else if !strings.HasPrefix(r.URL.Path, base+"/") {
		w.WriteHeader(http.StatusForbidden)
		obj = metav1.Status{Status: "Failure", Reason: metav1.StatusReasonForbidden, Code: http.StatusForbidden}
	} else {
		route := strings.TrimPrefix(r.URL.Path, base)
		switch route {
		case "/api":
			obj = metav1.APIVersions{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "APIVersions"}, Versions: []string{"v1"}}
		case "/api/v1":
			obj = metav1.APIResourceList{GroupVersion: "v1", APIResources: []metav1.APIResource{{Name: "secrets", Kind: "Secret", Namespaced: true}}}
		case "/apis":
			groups := make([]metav1.APIGroup, 0, 2)
			for _, group := range []string{f.group, "showroom.apeirora.eu", "apis.kcp.io"} {
				gv := metav1.GroupVersionForDiscovery{GroupVersion: group + "/v1alpha1", Version: "v1alpha1"}
				groups = append(groups, metav1.APIGroup{Name: group, Versions: []metav1.GroupVersionForDiscovery{gv}, PreferredVersion: gv})
			}
			obj = metav1.APIGroupList{Groups: groups}
		case "/apis/apis.kcp.io/v1alpha1":
			obj = metav1.APIResourceList{GroupVersion: "apis.kcp.io/v1alpha1", APIResources: []metav1.APIResource{{Name: "apibindings", Kind: "APIBinding", Namespaced: false}}}
		case "/apis/apis.kcp.io/v1alpha1/apibindings":
			f.bindingReads++
			list := accountBindingList(f.group)
			items := list["items"].([]any)
			own := items[0].(map[string]any)
			child := items[1].(map[string]any)
			child["status"].(map[string]any)["boundResources"].([]any)[0].(map[string]any)["resource"] = f.resource
			for _, claims := range []any{own["spec"].(map[string]any)["permissionClaims"], own["status"].(map[string]any)["exportPermissionClaims"], own["status"].(map[string]any)["appliedPermissionClaims"]} {
				claims.([]any)[0].(map[string]any)["resource"] = f.resource
			}
			if f.revokeAt > 0 && f.bindingReads >= f.revokeAt {
				own["spec"].(map[string]any)["permissionClaims"].([]any)[0].(map[string]any)["state"] = "Rejected"
			}
			obj = list
		case "/apis/" + f.group + "/v1alpha1":
			obj = metav1.APIResourceList{GroupVersion: f.group + "/v1alpha1", APIResources: []metav1.APIResource{{Name: f.resource, Kind: f.kind, Namespaced: f.namespaced}}}
		case "/apis/showroom.apeirora.eu/v1alpha1":
			obj = metav1.APIResourceList{GroupVersion: "showroom.apeirora.eu/v1alpha1", APIResources: []metav1.APIResource{{Name: "simpleclusters", Kind: "SimpleCluster", Namespaced: true}}}
		case "/api/v1/namespaces/default/secrets/sc-child-ironcoreshoot-kubeconfig":
			obj = &corev1.Secret{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}, ObjectMeta: metav1.ObjectMeta{Name: "sc-child-ironcoreshoot-kubeconfig", Namespace: "default"}, Data: map[string][]byte{"kubeconfig": f.credential}}
		case "/apis/showroom.apeirora.eu/v1alpha1/namespaces/default/simpleclusters/demo":
			parent := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "showroom.apeirora.eu/v1alpha1", "kind": "SimpleCluster", "metadata": map[string]any{"namespace": "default", "name": "demo", "uid": f.parentUID}}}
			if f.parentDeleting {
				now := metav1.Now()
				parent.SetDeletionTimestamp(&now)
			}
			obj = parent
		case f.childPath():
			if r.Method != "POST" {
				w.WriteHeader(405)
				return
			}
			if f.forbidden {
				w.WriteHeader(http.StatusForbidden)
				obj = metav1.Status{Status: "Failure", Reason: metav1.StatusReasonForbidden, Code: http.StatusForbidden}
				break
			}
			var child unstructured.Unstructured
			if err := json.NewDecoder(r.Body).Decode(&child); err != nil {
				w.WriteHeader(400)
				return
			}
			f.creates++
			child.SetUID("child-uid")
			child.SetResourceVersion("1")
			child.Object["status"] = map[string]any{"phase": "Provisioning"}
			f.child = &child
			if f.loseCreateResponse {
				w.WriteHeader(http.StatusInternalServerError)
				obj = metav1.Status{Status: "Failure", Reason: metav1.StatusReasonInternalError, Code: http.StatusInternalServerError}
				break
			}
			w.WriteHeader(http.StatusCreated)
			obj = f.child
		case f.childPath() + "/sc-child":
			if f.child == nil || f.deletedChild {
				w.WriteHeader(http.StatusNotFound)
				obj = metav1.Status{Status: "Failure", Reason: metav1.StatusReasonNotFound, Code: http.StatusNotFound}
				break
			}
			switch r.Method {
			case "GET":
				obj = f.child
			case "PATCH":
				patch, _ := io.ReadAll(r.Body)
				var body map[string]any
				if err := json.Unmarshal(patch, &body); err != nil {
					w.WriteHeader(400)
					return
				}
				metadata, _ := body["metadata"].(map[string]any)
				if metadata["uid"] != string(f.child.GetUID()) || metadata["resourceVersion"] != f.child.GetResourceVersion() {
					w.WriteHeader(http.StatusConflict)
					obj = metav1.Status{Status: "Failure", Reason: metav1.StatusReasonConflict, Code: http.StatusConflict}
					break
				}
				f.patches++
				if strings.Contains(string(patch), "allow-delete") {
					annotations := f.child.GetAnnotations()
					annotations["showroom.apeirora.eu/allow-delete"] = "true"
					f.child.SetAnnotations(annotations)
				}
				obj = f.child
			case "DELETE":
				var options metav1.DeleteOptions
				if err := json.NewDecoder(r.Body).Decode(&options); err != nil {
					w.WriteHeader(400)
					return
				}
				if options.Preconditions == nil || options.Preconditions.UID == nil || *options.Preconditions.UID != f.child.GetUID() {
					w.WriteHeader(http.StatusConflict)
					obj = metav1.Status{Status: "Failure", Reason: metav1.StatusReasonConflict, Code: http.StatusConflict}
					break
				}
				f.deletes++
				now := metav1.Now()
				f.child.SetDeletionTimestamp(&now)
				obj = f.child
			default:
				w.WriteHeader(405)
				return
			}
		default:
			w.WriteHeader(http.StatusNotFound)
			obj = metav1.Status{Status: "Failure", Reason: metav1.StatusReasonNotFound, Code: http.StatusNotFound}
		}
	}
	_ = json.NewEncoder(w).Encode(obj)
}

func accountReconciler(t *testing.T, f *accountFixture) (*SimpleClusterReconciler, *showroomv1alpha1.SimpleCluster, *showroomv1alpha1.SimpleClusterProviderConfig) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := showroomv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	sc := &showroomv1alpha1.SimpleCluster{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "account-a", UID: "physical-parent", Labels: map[string]string{target.RemoteClusterLabel: "account-a"}, Annotations: map[string]string{target.RemoteNamespaceAnnotation: "default", target.RemoteNameAnnotation: "demo"}}, Spec: showroomv1alpha1.SimpleClusterSpec{ProviderConfigRefs: []string{"provider"}}}
	pc := &showroomv1alpha1.SimpleClusterProviderConfig{ObjectMeta: metav1.ObjectMeta{Name: "provider", Namespace: "account-a"}, Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{Target: showroomv1alpha1.TargetSpec{KCPAccount: true}, Resources: []showroomv1alpha1.ResourceSpec{{Name: "shoot", APIVersion: f.group + "/v1alpha1", Kind: f.kind, NameTemplate: "sc-child", Readiness: &showroomv1alpha1.ReadinessCheck{PhasePath: "status.phase", PhaseReadyValue: "Ready"}, Template: "apiVersion: " + f.group + "/v1alpha1\nkind: " + f.kind + "\nmetadata: {}\nspec: plain-gardener-spec\n"}}, KubeconfigSource: showroomv1alpha1.KubeconfigSource{ResourceName: "shoot", SecretRef: &showroomv1alpha1.KubeconfigSecretSource{Name: "{{ .Target.name }}-ironcoreshoot-kubeconfig", Namespace: "default"}}}}
	mapper := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: f.group, Version: "v1alpha1"}})
	mapper.Add(schema.GroupVersionKind{Group: f.group, Version: "v1alpha1", Kind: "IronCoreShoot"}, apimeta.RESTScopeNamespace)
	physical := fake.NewClientBuilder().WithScheme(scheme).WithRESTMapper(mapper).WithStatusSubresource(sc, pc).WithObjects(sc, pc).Build()
	return &SimpleClusterReconciler{Client: physical, APIReader: physical, Scheme: scheme, RESTConfig: &rest.Config{Host: f.server.URL + "/physical"}, AccountProvider: &target.AccountProvider{Config: &rest.Config{Host: f.server.URL + "/clusters/provider"}, APIExportName: "simpleclusters.showroom.apeirora.eu"}}, sc, pc
}

func reconcileAccount(t *testing.T, r *SimpleClusterReconciler, sc *showroomv1alpha1.SimpleCluster) error {
	t.Helper()
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sc)})
	return err
}

func TestAccountReconcileCreatesScopedChildAndReusesIt(t *testing.T) {
	for _, group := range []string{"sap.eu", "apeiro.eu"} {
		t.Run(group, func(t *testing.T) {
			f := newAccountFixture(t, group)
			r, sc, _ := accountReconciler(t, f)
			for i := 0; i < 2; i++ {
				if err := reconcileAccount(t, r, sc); err != nil {
					t.Fatal(err)
				}
			}
			if f.creates != 1 || f.child == nil {
				t.Fatalf("child creates=%d", f.creates)
			}
			if f.child.GetNamespace() != "" || len(f.child.GetOwnerReferences()) != 0 {
				t.Fatal("invalid child scope or ownerReference")
			}
			if f.child.GetAnnotations()[target.ParentUIDAnnotation] != "account-parent" {
				t.Fatal("used physical parent UID")
			}
			if f.child.GetLabels()["showroom.apeirora.eu/simple-cluster"] != "demo" {
				t.Fatal("lost parent label")
			}
			current := &showroomv1alpha1.SimpleCluster{}
			if err := r.Get(context.Background(), client.ObjectKeyFromObject(sc), current); err != nil {
				t.Fatal(err)
			}
			if current.Status.AccountTarget == nil || current.Status.AccountTarget.Mode != target.AccountMode || current.Status.TargetCluster != "kcp:account-a" {
				t.Fatalf("target not pinned: %+v", current.Status.AccountTarget)
			}
		})
	}

}

func TestAccountReconcileDeniedClaimDoesNotUseLocalTarget(t *testing.T) {
	for _, group := range []string{"sap.eu", "apeiro.eu"} {
		t.Run(group, func(t *testing.T) {
			f := newAccountFixture(t, group)
			f.forbidden = true
			r, sc, _ := accountReconciler(t, f)
			if err := reconcileAccount(t, r, sc); err != nil {
				t.Fatal(err)
			}
			current := &showroomv1alpha1.SimpleCluster{}
			if err := r.Get(context.Background(), client.ObjectKeyFromObject(sc), current); err != nil {
				t.Fatal(err)
			}
			if f.creates != 0 || accountErrorReason(current) != "CreateError" {
				t.Fatal("claim denial did not fail closed")
			}
			for _, path := range f.paths {
				if strings.Contains(path, "/physical/") {
					t.Fatal("fell back to physical target")
				}
			}
		})
	}

}

func TestAccountReconcileRejectsExistingChildOwnership(t *testing.T) {
	for _, group := range []string{"sap.eu", "apeiro.eu"} {
		t.Run(group, func(t *testing.T) {
			f := newAccountFixture(t, group)
			r, sc, _ := accountReconciler(t, f)
			f.child = &unstructured.Unstructured{Object: map[string]any{"apiVersion": group + "/v1alpha1", "kind": "IronCoreShoot", "metadata": map[string]any{"name": "sc-child", "uid": "someone-elses-child"}}}
			if err := reconcileAccount(t, r, sc); err != nil {
				t.Fatal(err)
			}
			current := &showroomv1alpha1.SimpleCluster{}
			if err := r.Get(context.Background(), client.ObjectKeyFromObject(sc), current); err != nil {
				t.Fatal(err)
			}
			if accountErrorReason(current) != "ChildOwnershipConflict" || f.creates+f.patches+f.deletes != 0 {
				t.Fatal("implicitly adopted another child")
			}
		})
	}

}

func TestAccountParentDeletionPreventsCreation(t *testing.T) {
	for _, group := range []string{"sap.eu", "apeiro.eu"} {
		t.Run(group, func(t *testing.T) {
			f := newAccountFixture(t, group)
			f.parentDeleting = true
			r, sc, _ := accountReconciler(t, f)
			if err := reconcileAccount(t, r, sc); err != nil {
				t.Fatal(err)
			}
			if f.creates != 0 {
				t.Fatal("created child for deleted account parent")
			}
		})
	}

}

func TestAccountDeletionWaitsForChildFinalizer(t *testing.T) {
	for _, group := range []string{"sap.eu", "apeiro.eu"} {
		t.Run(group, func(t *testing.T) {
			f := newAccountFixture(t, group)
			r, sc, _ := accountReconciler(t, f)
			if err := reconcileAccount(t, r, sc); err != nil {
				t.Fatal(err)
			}
			current := &showroomv1alpha1.SimpleCluster{}
			if err := r.Get(context.Background(), client.ObjectKeyFromObject(sc), current); err != nil {
				t.Fatal(err)
			}
			if err := r.Delete(context.Background(), current); err != nil {
				t.Fatal(err)
			}
			f.parentDeleting = true
			if err := reconcileAccount(t, r, sc); err != nil {
				t.Fatal(err)
			}
			if f.deletes != 1 || f.patches != 1 {
				t.Fatalf("deletes=%d patches=%d", f.deletes, f.patches)
			}
			if err := r.Get(context.Background(), client.ObjectKeyFromObject(sc), current); err != nil {
				t.Fatal("parent removed before child cleanup")
			}
			if !containsString(current.Finalizers, showroomv1alpha1.FinalizerName) {
				t.Fatal("removed parent finalizer too early")
			}
			if err := reconcileAccount(t, r, sc); err != nil {
				t.Fatal(err)
			}
			if f.deletes != 1 {
				t.Fatal("repeated delete while child finalizer is pending")
			}
			f.deletedChild = true
			if err := reconcileAccount(t, r, sc); err != nil {
				t.Fatal(err)
			}
			if err := r.Get(context.Background(), types.NamespacedName{Namespace: sc.Namespace, Name: sc.Name}, current); err == nil {
				t.Fatal("parent finalizer remained after child disappeared")
			}
		})
	}

}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func accountErrorReason(sc *showroomv1alpha1.SimpleCluster) string {
	condition := apimeta.FindStatusCondition(sc.Status.Conditions, showroomv1alpha1.ConditionError)
	if condition == nil {
		return ""
	}
	return condition.Reason
}

func TestAccountUnmigratedRecordsRequireMigration(t *testing.T) {
	for _, mode := range []string{"local", "remote", target.LegacyLocalMode, target.AccountMode} {
		f := newAccountFixture(t)
		r, sc, pc := accountReconciler(t, f)
		sc.Status.TargetCluster = mode
		sc.Status.Resources = []showroomv1alpha1.ResourceStatus{{Name: "shoot", TargetName: "sc-child"}}
		if mode == target.LegacyLocalMode || mode == target.AccountMode {
			sc.Status.AccountTarget = &showroomv1alpha1.AccountTargetStatus{Mode: mode}
		}
		if _, err := r.targetClient(context.Background(), sc, pc); !errors.Is(err, ErrMigrationRequired) {
			t.Fatalf("mode=%s error=%v", mode, err)
		}
		if f.creates+f.deletes+f.patches != 0 || len(f.paths) != 0 {
			t.Fatal("migration guard accessed account or provider child")
		}
	}
}

func TestAccountRecreatedParentUsesKCPWithDefaultEmptyLegacyList(t *testing.T) {
	f := newAccountFixture(t)
	r, sc, pc := accountReconciler(t, f)
	tc, err := r.targetClient(context.Background(), sc, pc)
	if err != nil {
		t.Fatal(err)
	}
	if tc.AccountParent == nil || sc.Status.AccountTarget.Mode != target.AccountMode {
		t.Fatal("new parent entered local mode")
	}
	f.parentUID = "replacement"
	if _, err := r.targetClient(context.Background(), sc, pc); err == nil {
		t.Fatal("accepted replaced account parent")
	}
}

func TestAccountMissingProviderAndInvalidConfigurationFailClosed(t *testing.T) {
	f := newAccountFixture(t)
	r, sc, pc := accountReconciler(t, f)
	r.AccountProvider = nil
	if _, err := r.targetClient(context.Background(), sc, pc); err == nil {
		t.Fatal("missing provider fell back to local")
	}
	for _, mutate := range []func(*showroomv1alpha1.SimpleClusterProviderConfig){
		func(p *showroomv1alpha1.SimpleClusterProviderConfig) {
			p.Spec.Target.KubeconfigSecretRef = &corev1.SecretReference{Name: "secret"}
		},
		func(p *showroomv1alpha1.SimpleClusterProviderConfig) {
			p.Spec.Resources[0].ForceCleanupOnCascade = true
		},
	} {
		candidate := pc.DeepCopy()
		mutate(candidate)
		if err := validateAccountTarget(candidate); err == nil {
			t.Fatal("accepted unsafe account configuration")
		}
	}
}

func TestAccountDeletionRefusesChildOfAnotherParent(t *testing.T) {
	for _, group := range []string{"sap.eu", "apeiro.eu"} {
		t.Run(group, func(t *testing.T) {
			f := newAccountFixture(t, group)
			r, sc, _ := accountReconciler(t, f)
			if err := reconcileAccount(t, r, sc); err != nil {
				t.Fatal(err)
			}
			annotations := f.child.GetAnnotations()
			annotations[target.ParentUIDAnnotation] = "replacement"
			f.child.SetAnnotations(annotations)
			current := &showroomv1alpha1.SimpleCluster{}
			if err := r.Get(context.Background(), client.ObjectKeyFromObject(sc), current); err != nil {
				t.Fatal(err)
			}
			if err := r.Delete(context.Background(), current); err != nil {
				t.Fatal(err)
			}
			f.parentDeleting = true
			if err := reconcileAccount(t, r, sc); err != nil {
				t.Fatal(err)
			}
			if err := r.Get(context.Background(), client.ObjectKeyFromObject(sc), current); err != nil {
				t.Fatal("parent removed despite foreign child")
			}
			if !containsString(current.Finalizers, showroomv1alpha1.FinalizerName) {
				t.Fatal("released foreign child ownership")
			}
			if f.patches+f.deletes != 0 {
				t.Fatal("changed another parent's child")
			}
		})
	}

}

func TestAccountLostCreateResponseRetainsDeletionIdentity(t *testing.T) {
	f := newAccountFixture(t)
	f.loseCreateResponse = true
	r, sc, _ := accountReconciler(t, f)
	if err := reconcileAccount(t, r, sc); err != nil {
		t.Fatal(err)
	}
	current := &showroomv1alpha1.SimpleCluster{}
	if err := r.Get(context.Background(), client.ObjectKeyFromObject(sc), current); err != nil {
		t.Fatal(err)
	}
	if accountErrorReason(current) != "CreateError" {
		t.Fatal("fixture did not simulate lost creation response")
	}
	if len(current.Status.AccountResources) != 1 || current.Status.AccountResources[0].Name != "sc-child" {
		t.Fatal("lost child identity after failed response")
	}
	if err := r.Delete(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	f.parentDeleting = true
	if err := reconcileAccount(t, r, sc); err != nil {
		t.Fatal(err)
	}
	if f.creates != 1 || f.deletes != 1 {
		t.Fatal("lost-response child was not cleaned through recorded target")
	}
}

func TestAccountStatusAndCredentialReachExistingConsumerSecret(t *testing.T) {
	f := newAccountFixture(t)
	r, sc, _ := accountReconciler(t, f)
	f.credential = []byte("apiVersion: v1\nkind: Config\nclusters:\n- name: workload\n  cluster:\n    server: " + f.server.URL + "/workload\ncontexts:\n- name: workload\n  context: {cluster: workload, user: workload}\ncurrent-context: workload\nusers:\n- name: workload\n  user: {}\n")
	if err := reconcileAccount(t, r, sc); err != nil {
		t.Fatal(err)
	}
	f.child.Object["status"] = map[string]any{"phase": "Ready"}
	if err := reconcileAccount(t, r, sc); err != nil {
		t.Fatal(err)
	}
	current := &showroomv1alpha1.SimpleCluster{}
	if err := r.Get(context.Background(), client.ObjectKeyFromObject(sc), current); err != nil {
		t.Fatal(err)
	}
	if current.Status.Phase != showroomv1alpha1.PhaseReady || current.Status.KubeconfigSecretRef == nil || current.Status.KubeconfigSecretRef.Name != "demo-kubeconfig" {
		t.Fatal("account status or credential reference did not reach SimpleCluster")
	}
	secret := &corev1.Secret{}
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: sc.Namespace, Name: "demo-kubeconfig"}, secret); err != nil {
		t.Fatal(err)
	}
	if string(secret.Data["kubeconfig"]) != string(f.credential) {
		t.Fatal("consumer credential did not match source")
	}
}

func TestAccountPhysicalUIDWithoutLocalRecordsStillRequiresKCP(t *testing.T) {
	f := newAccountFixture(t)
	r, sc, pc := accountReconciler(t, f)
	r.AccountProvider.Config = nil
	if _, err := r.targetClient(context.Background(), sc, pc); err == nil {
		t.Fatal("new request bypassed account client")
	}
	if sc.Status.AccountTarget != nil {
		t.Fatal("new request was marked as legacy")
	}
}

func accountBindingList(group string) map[string]any {
	claim := map[string]any{"group": group, "resource": "ironcoreshoots", "identityHash": "child-identity", "all": true}
	accepted := runtime.DeepCopyJSON(claim)
	accepted["state"] = "Accepted"
	binding := func(name, group, resource, identity string) map[string]any {
		return map[string]any{"apiVersion": "apis.kcp.io/v1alpha1", "kind": "APIBinding", "metadata": map[string]any{"name": name, "uid": name + "-uid"}, "status": map[string]any{"phase": "Bound", "conditions": []any{map[string]any{"type": "InitialBindingCompleted", "status": "True"}, map[string]any{"type": "BindingUpToDate", "status": "True"}}, "boundResources": []any{map[string]any{"group": group, "resource": resource, "schema": map[string]any{"identityHash": identity}}}}}
	}
	own := binding("own", "showroom.apeirora.eu", "simpleclusters", "own-identity")
	own["spec"] = map[string]any{"permissionClaims": []any{accepted}}
	own["status"].(map[string]any)["exportPermissionClaims"] = []any{claim}
	own["status"].(map[string]any)["appliedPermissionClaims"] = []any{claim}
	return map[string]any{"apiVersion": "apis.kcp.io/v1alpha1", "kind": "APIBindingList", "items": []any{own, binding("child", group, "ironcoreshoots", "child-identity")}}
}

func (f *accountFixture) childPath() string {
	path := "/apis/" + f.group + "/v1alpha1"
	if f.namespaced {
		path += "/namespaces/default"
	}
	return path + "/" + f.resource
}

func TestAccountDestinationSupportsArbitraryNamespacedKind(t *testing.T) {
	f := newAccountFixture(t, "arbitrary.example")
	f.kind = "AnyChild"
	f.resource = "anychildren"
	f.namespaced = true
	r, sc, pc := accountReconciler(t, f)
	pc.Spec.Target.KCPAccount = false
	if err := r.Update(context.Background(), pc); err != nil {
		t.Fatal(err)
	}
	if err := reconcileAccount(t, r, sc); err != nil {
		t.Fatal(err)
	}
	if f.creates != 1 || f.child.GetKind() != "AnyChild" || f.child.GetNamespace() != "default" {
		t.Fatalf("account child not created: %+v", f.child)
	}
	current := &showroomv1alpha1.SimpleCluster{}
	if err := r.Get(context.Background(), client.ObjectKeyFromObject(sc), current); err != nil {
		t.Fatal(err)
	}
	if len(current.Status.AccountResources) != 1 || current.Status.AccountResources[0].Namespace != "default" || current.Status.AccountResources[0].UID != "child-uid" {
		t.Fatal("missing durable namespaced UID receipt")
	}
}

func TestAccountClaimRevokedBeforeCreateStopsWrite(t *testing.T) {
	f := newAccountFixture(t)
	f.revokeAt = 2
	r, sc, _ := accountReconciler(t, f)
	if err := reconcileAccount(t, r, sc); err != nil {
		t.Fatal(err)
	}
	if f.creates+f.deletes+f.patches != 0 {
		t.Fatal("wrote after claim revocation")
	}
	current := &showroomv1alpha1.SimpleCluster{}
	if err := r.Get(context.Background(), client.ObjectKeyFromObject(sc), current); err != nil {
		t.Fatal(err)
	}
	if accountErrorReason(current) != "AccountAuthorizationDenied" {
		t.Fatal("revocation not reported")
	}
}

func TestAccountDeletionRefusesSameNameReplacement(t *testing.T) {
	f := newAccountFixture(t)
	r, sc, _ := accountReconciler(t, f)
	if err := reconcileAccount(t, r, sc); err != nil {
		t.Fatal(err)
	}
	f.child.SetUID("replacement")
	current := &showroomv1alpha1.SimpleCluster{}
	if err := r.Get(context.Background(), client.ObjectKeyFromObject(sc), current); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	if err := reconcileAccount(t, r, sc); err != nil {
		t.Fatal(err)
	}
	if f.deletes+f.patches != 0 {
		t.Fatal("changed same-name replacement")
	}
	if err := r.Get(context.Background(), client.ObjectKeyFromObject(sc), current); err != nil {
		t.Fatal("released parent before handoff")
	}
}

func TestAccountClaimRevokedBeforeDeleteStopsDelete(t *testing.T) {
	f := newAccountFixture(t)
	r, sc, _ := accountReconciler(t, f)
	if err := reconcileAccount(t, r, sc); err != nil {
		t.Fatal(err)
	}
	f.revokeAt = f.bindingReads + 3 // resolve cleanup, authorize patch, revoke before delete
	current := &showroomv1alpha1.SimpleCluster{}
	if err := r.Get(context.Background(), client.ObjectKeyFromObject(sc), current); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	if err := reconcileAccount(t, r, sc); err != nil {
		t.Fatal(err)
	}
	if f.patches != 1 || f.deletes != 0 {
		t.Fatalf("patches=%d deletes=%d", f.patches, f.deletes)
	}
}
