package target

import (
	"context"
	"strings"
	"testing"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

var bindingGVR = schema.GroupVersionResource{Group: "apis.kcp.io", Version: "v1alpha2", Resource: "apibindings"}
var arbitraryGVK = schema.GroupVersionKind{Group: "example.test", Version: "v1", Kind: "ArbitraryChild"}

func permissionFixture() map[string]any {
	return map[string]any{"group": "example.test", "resource": "arbitrarychildren", "identityHash": "child-identity", "verbs": []any{"get", "create", "patch", "delete"}}
}
func bindingFixture(name, group, resource, identity string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "apis.kcp.io/v1alpha2", "kind": "APIBinding", "metadata": map[string]any{"name": name, "uid": name + "-uid"}, "status": map[string]any{"phase": "Bound", "conditions": []any{map[string]any{"type": "InitialBindingCompleted", "status": "True"}, map[string]any{"type": "BindingUpToDate", "status": "True"}}, "boundResources": []any{map[string]any{"group": group, "resource": resource, "schema": map[string]any{"identityHash": identity}}}}}}
}
func authorizationFixture(t *testing.T) (*unstructured.Unstructured, *unstructured.Unstructured, apimeta.RESTMapper) {
	t.Helper()
	provider := bindingFixture("simplecluster", "showroom.apeirora.eu", "simpleclusters", "own-identity")
	requested := permissionFixture()
	accepted := runtime.DeepCopyJSON(requested)
	accepted["state"] = "Accepted"
	accepted["selector"] = map[string]any{"matchAll": true}
	applied := runtime.DeepCopyJSON(requested)
	applied["selector"] = map[string]any{"matchAll": true}
	provider.Object["spec"] = map[string]any{"permissionClaims": []any{accepted}}
	provider.Object["status"].(map[string]any)["exportPermissionClaims"] = []any{requested}
	provider.Object["status"].(map[string]any)["appliedPermissionClaims"] = []any{applied}
	mapper := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{arbitraryGVK.GroupVersion(), {Group: "apis.kcp.io", Version: "v1alpha2"}, {Version: "v1"}})
	mapper.AddSpecific(arbitraryGVK, schema.GroupVersionResource{Group: "example.test", Version: "v1", Resource: "arbitrarychildren"}, schema.GroupVersionResource{Group: "example.test", Version: "v1", Resource: "arbitrarychild"}, apimeta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Group: "apis.kcp.io", Version: "v1alpha2", Kind: "APIBinding"}, apimeta.RESTScopeRoot)
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Secret"}, apimeta.RESTScopeNamespace)
	return provider, bindingFixture("child", "example.test", "arbitrarychildren", "child-identity"), mapper
}
func firstClaim(binding *unstructured.Unstructured, path ...string) map[string]any {
	items, _, _ := unstructured.NestedFieldNoCopy(binding.Object, path...)
	return items.([]any)[0].(map[string]any)
}

func TestAccountAuthorizationRejectsInvalidContractsBeforeWrites(t *testing.T) {
	for _, scenario := range []string{"allowed", "unbound-child", "pending-child", "stale-child", "deleting-child", "missing-own-binding", "wrong-own-identity", "missing-request", "missing-accepted", "rejected", "missing-applied", "wrong-child-identity", "wrong-claim-identity", "wrong-applied-identity", "wrong-requested-identity", "missing-verb", "legacy-fields-on-v2", "malformed-selector", "empty-selector", "false-matchall", "selector-mismatch", "applied-selector-mismatch", "ambiguous-own-binding"} {
		t.Run(scenario, func(t *testing.T) {
			provider, child, mapper := authorizationFixture(t)
			objects := []runtime.Object{provider, child}
			accepted := firstClaim(provider, "spec", "permissionClaims")
			switch scenario {
			case "unbound-child":
				objects = objects[:1]
			case "pending-child":
				child.Object["status"].(map[string]any)["phase"] = "Binding"
			case "stale-child":
				child.Object["status"].(map[string]any)["conditions"] = []any{map[string]any{"type": "InitialBindingCompleted", "status": "True"}, map[string]any{"type": "BindingUpToDate", "status": "False"}}
			case "deleting-child":
				now := metav1.Now()
				child.SetDeletionTimestamp(&now)
			case "missing-own-binding":
				objects = objects[1:]
			case "wrong-own-identity":
				objects[0] = bindingFixture("foreign", "showroom.apeirora.eu", "simpleclusters", "foreign")
			case "missing-request":
				delete(provider.Object["status"].(map[string]any), "exportPermissionClaims")
			case "missing-accepted":
				provider.Object["spec"] = map[string]any{}
			case "rejected":
				accepted["state"] = "Rejected"
			case "missing-applied":
				delete(provider.Object["status"].(map[string]any), "appliedPermissionClaims")
			case "wrong-child-identity":
				objects[1] = bindingFixture("foreign-child", "example.test", "arbitrarychildren", "foreign")
			case "wrong-claim-identity":
				accepted["identityHash"] = "foreign"
			case "wrong-applied-identity":
				firstClaim(provider, "status", "appliedPermissionClaims")["identityHash"] = "foreign"
			case "wrong-requested-identity":
				firstClaim(provider, "status", "exportPermissionClaims")["identityHash"] = "foreign"
			case "legacy-fields-on-v2":
				delete(accepted, "verbs")
				delete(accepted, "selector")
				accepted["all"] = true
			case "missing-verb":
				accepted["verbs"] = []any{"get", "create"}
			case "malformed-selector":
				accepted["selector"] = map[string]any{"unknown": true}
			case "empty-selector":
				accepted["selector"] = map[string]any{}
			case "false-matchall":
				accepted["selector"] = map[string]any{"matchAll": false}
			case "selector-mismatch":
				accepted["selector"] = map[string]any{"matchLabels": map[string]any{"owner": "foreign"}}
			case "applied-selector-mismatch":
				firstClaim(provider, "status", "appliedPermissionClaims")["selector"] = map[string]any{"matchLabels": map[string]any{"owner": "foreign"}}
			case "ambiguous-own-binding":
				other := provider.DeepCopy()
				other.SetName("duplicate")
				other.SetUID("duplicate")
				objects = append(objects, other)
			}
			dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{bindingGVR: "APIBindingList"}, objects...)
			authorization, err := ResolveAccountResource(context.Background(), dyn, mapper, "own-identity", arbitraryGVK, "get", "create", "patch", "delete")
			if err == nil {
				object := &unstructured.Unstructured{}
				object.SetGroupVersionKind(arbitraryGVK)
				object.SetNamespace("default")
				object.SetName("demo")
				object.SetLabels(map[string]string{"owner": "parent"})
				err = authorization.CheckObject(object)
			}
			if (err == nil) != (scenario == "allowed") {
				t.Fatalf("scenario=%s, err=%v", scenario, err)
			}
			for _, action := range dyn.Actions() {
				if action.GetVerb() != "list" {
					t.Fatalf("authorization mutated or read a child: %s", action.GetVerb())
				}
			}
		})
	}
}

func TestAccountAuthorizationSelectorMatchAndCoreClaims(t *testing.T) {
	for _, core := range []bool{false, true} {
		provider, child, mapper := authorizationFixture(t)
		gvk := arbitraryGVK
		if core {
			gvk = schema.GroupVersionKind{Version: "v1", Kind: "Secret"}
			for _, path := range [][]string{{"status", "exportPermissionClaims"}, {"spec", "permissionClaims"}, {"status", "appliedPermissionClaims"}} {
				claim := firstClaim(provider, path...)
				delete(claim, "group")
				claim["resource"] = "secrets"
				delete(claim, "identityHash")
			}
		}
		for _, path := range [][]string{{"spec", "permissionClaims"}, {"status", "appliedPermissionClaims"}} {
			firstClaim(provider, path...)["selector"] = map[string]any{"matchExpressions": []any{map[string]any{"key": "owner", "operator": "In", "values": []any{"parent"}}}}
		}
		dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{bindingGVR: "APIBindingList"}, provider, child)
		authorization, err := ResolveAccountResource(context.Background(), dyn, mapper, "own-identity", gvk, "get", "create", "patch", "delete")
		if err != nil {
			t.Fatal(err)
		}
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		obj.SetNamespace("default")
		obj.SetName("demo")
		obj.SetLabels(map[string]string{"owner": "parent"})
		if err := authorization.CheckObject(obj); err != nil {
			t.Fatal(err)
		}
		obj.SetLabels(nil)
		if err := authorization.CheckObject(obj); err == nil || !strings.Contains(err.Error(), "selector") {
			t.Fatal("accepted missing selector labels")
		}
	}
}
