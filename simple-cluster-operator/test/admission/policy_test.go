/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package admission exercises the ValidatingAdmissionPolicy shipped by
// charts/simple-cluster-system against a real API server. The policy is rendered from
// the chart itself, so the test cannot drift away from what is deployed.
package admission

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

const (
	chartPath  = "../../../charts/simple-cluster-system"
	namespace  = "simple-cluster-system"
	label      = "showroom.apeirora.eu/simple-cluster"
	allowAnno  = "showroom.apeirora.eu/allow-delete"
	operatorSA = "system:serviceaccount:" + namespace + ":simple-cluster-operator-controller-manager"
	denyText   = "managed by SimpleCluster"
)

func TestChildDeleteGuard(t *testing.T) {
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm not installed; cannot render the policy from the chart")
	}

	env := &envtest.Environment{}
	cfg, err := env.Start()
	if err != nil {
		t.Skipf("envtest unavailable (run via `make test`): %v", err)
	}
	t.Cleanup(func() { _ = env.Stop() })

	ctx := context.Background()
	admin := kubernetes.NewForConfigOrDie(cfg)
	installPolicy(t, ctx, admin, render(t, helm))

	// Two identities: the operator, which owns the children, and anyone else.
	operator := clientFor(t, ctx, env, admin, operatorSA)
	user := clientFor(t, ctx, env, admin, "someone-else")
	nsController := clientFor(t, ctx, env, admin, "system:serviceaccount:kube-system:namespace-controller")

	// Enforcement starts once the API server has compiled the policy and its binding.
	var probe string
	deadline := time.Now().Add(90 * time.Second)
	for {
		probe = create(t, ctx, admin, "probe", withLabel("demo"))
		err := user.CoreV1().ConfigMaps("default").Delete(ctx, probe, metav1.DeleteOptions{})
		if err != nil && strings.Contains(err.Error(), denyText) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("policy never took effect; last delete returned %v", err)
		}
		time.Sleep(time.Second)
	}

	t.Run("denial names the owning SimpleCluster", func(t *testing.T) {
		name := create(t, ctx, admin, "child", withLabel("my-cluster"))
		err := user.CoreV1().ConfigMaps("default").Delete(ctx, name, metav1.DeleteOptions{})
		if err == nil || !strings.Contains(err.Error(), `managed by SimpleCluster "my-cluster"`) {
			t.Fatalf("expected a denial naming my-cluster, got %v", err)
		}
	})

	// The policy matches every group, version and resource with failurePolicy: Fail, so the
	// blast radius of a mistake is the whole cluster. These check that it stays inert
	// everywhere it should: another namespace, a system namespace, a cluster-scoped Kind.
	t.Run("unlabelled objects stay deletable", func(t *testing.T) {
		for _, ns := range []string{"default", "kube-system"} {
			name := create(t, ctx, admin, "direct", inNamespace(ns))
			if err := user.CoreV1().ConfigMaps(ns).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
				t.Fatalf("delete of an unlabelled object in %s was refused: %v", ns, err)
			}
		}
	})

	t.Run("cluster-scoped objects stay deletable", func(t *testing.T) {
		ns, err := admin.CoreV1().Namespaces().Create(ctx,
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "guard-"}}, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("create namespace: %v", err)
		}
		if err := user.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{}); err != nil {
			t.Fatalf("delete of an unlabelled namespace was refused: %v", err)
		}
	})

	t.Run("the policy compiles cleanly", func(t *testing.T) {
		policy, err := admin.AdmissionregistrationV1().ValidatingAdmissionPolicies().
			Get(ctx, "simple-cluster-child-delete-guard", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get policy: %v", err)
		}
		if w := policy.Status.TypeChecking; w != nil && len(w.ExpressionWarnings) > 0 {
			t.Fatalf("policy has type-checking warnings: %v", w.ExpressionWarnings)
		}
		for _, c := range policy.Status.Conditions {
			if c.Type == "TypeChecked" && c.Status != "True" {
				t.Fatalf("policy did not type-check: %s", c.Message)
			}
		}
	})

	t.Run("the operator deletes its own children", func(t *testing.T) {
		name := create(t, ctx, admin, "cascade", withLabel("demo"))
		if err := operator.CoreV1().ConfigMaps("default").Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
			t.Fatalf("operator cascade delete was refused: %v", err)
		}
	})

	t.Run("break-glass annotation releases the object", func(t *testing.T) {
		name := create(t, ctx, admin, "breakglass", withLabel("demo"), withAnnotation(allowAnno, "true"))
		if err := user.CoreV1().ConfigMaps("default").Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
			t.Fatalf("annotated object was not released: %v", err)
		}
	})

	t.Run("namespace teardown is not blocked", func(t *testing.T) {
		name := create(t, ctx, admin, "teardown", withLabel("demo"))
		if err := nsController.CoreV1().ConfigMaps("default").Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
			t.Fatalf("the namespace controller was refused: %v", err)
		}
	})

	t.Run("a bulk delete cannot sweep a child away", func(t *testing.T) {
		name := create(t, ctx, admin, "bulk", withLabel("demo"))
		err := user.CoreV1().ConfigMaps("default").DeleteCollection(ctx, metav1.DeleteOptions{},
			metav1.ListOptions{LabelSelector: label})
		if err == nil || !strings.Contains(err.Error(), denyText) {
			t.Fatalf("expected the collection delete to be refused, got %v", err)
		}
		if _, err := admin.CoreV1().ConfigMaps("default").Get(ctx, name, metav1.GetOptions{}); err != nil {
			t.Fatalf("child did not survive the bulk delete: %v", err)
		}
	})

	t.Run("a terminating object can still be force-deleted", func(t *testing.T) {
		name := create(t, ctx, admin, "wedged", withLabel("demo"), withFinalizer("test.local/wedge"))
		if err := operator.CoreV1().ConfigMaps("default").Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
			t.Fatalf("operator could not start deletion: %v", err)
		}
		if err := user.CoreV1().ConfigMaps("default").Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
			t.Fatalf("delete of a terminating object was refused: %v", err)
		}
		cm, err := admin.CoreV1().ConfigMaps("default").Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		cm.Finalizers = nil
		if _, err := admin.CoreV1().ConfigMaps("default").Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
			t.Fatalf("cannot drop the finalizer: %v", err)
		}
	})
}

// render produces the policy exactly as the chart deploys it. --api-versions is needed
// because `helm template` has no cluster to ask whether the API is served.
func render(t *testing.T, helm string) []byte {
	t.Helper()
	out, err := exec.Command(helm, "template", "scs", chartPath,
		"--namespace", namespace,
		"--api-versions", "admissionregistration.k8s.io/v1/ValidatingAdmissionPolicy",
		"--show-only", "templates/child-delete-guard.yaml").CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	return out
}

func installPolicy(t *testing.T, ctx context.Context, admin *kubernetes.Clientset, manifest []byte) {
	t.Helper()
	var policy admissionregistrationv1.ValidatingAdmissionPolicy
	var binding admissionregistrationv1.ValidatingAdmissionPolicyBinding
	decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(string(manifest)), 4096)
	for _, target := range []any{&policy, &binding} {
		if err := decoder.Decode(target); err != nil {
			t.Fatalf("decode rendered manifest: %v", err)
		}
	}
	if _, err := admin.AdmissionregistrationV1().ValidatingAdmissionPolicies().
		Create(ctx, &policy, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create policy: %v", err)
	}
	if _, err := admin.AdmissionregistrationV1().ValidatingAdmissionPolicyBindings().
		Create(ctx, &binding, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create binding: %v", err)
	}
}

// clientFor mints a client certificate for username and grants it cluster-admin, so that
// every rejection the test sees comes from the policy and never from RBAC.
func clientFor(
	t *testing.T,
	ctx context.Context,
	env *envtest.Environment,
	admin *kubernetes.Clientset,
	username string,
) *kubernetes.Clientset {
	t.Helper()
	authed, err := env.AddUser(envtest.User{Name: username}, &rest.Config{})
	if err != nil {
		t.Fatalf("add user %s: %v", username, err)
	}
	_, err = admin.RbacV1().ClusterRoleBindings().Create(ctx, &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "test-"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "cluster-admin"},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.UserKind, APIGroup: rbacv1.GroupName, Name: username}},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("grant %s: %v", username, err)
	}
	return kubernetes.NewForConfigOrDie(authed.Config())
}

type option func(*corev1.ConfigMap)

func withLabel(sc string) option {
	return func(cm *corev1.ConfigMap) { cm.Labels = map[string]string{label: sc} }
}

func withAnnotation(key, value string) option {
	return func(cm *corev1.ConfigMap) { cm.Annotations = map[string]string{key: value} }
}

func withFinalizer(name string) option {
	return func(cm *corev1.ConfigMap) { cm.Finalizers = []string{name} }
}

func inNamespace(ns string) option {
	return func(cm *corev1.ConfigMap) { cm.Namespace = ns }
}

func create(t *testing.T, ctx context.Context, admin *kubernetes.Clientset, prefix string, opts ...option) string {
	t.Helper()
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{GenerateName: prefix + "-", Namespace: "default"}}
	for _, o := range opts {
		o(cm)
	}
	created, err := admin.CoreV1().ConfigMaps(cm.Namespace).Create(ctx, cm, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create %s: %v", prefix, err)
	}
	return created.Name
}
