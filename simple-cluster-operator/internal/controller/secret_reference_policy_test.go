package controller

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/secretpolicy"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/target"
)

type referenceClientSpy struct {
	client.Client
	secretReads, writes int
}

func (s *referenceClientSpy) Get(ctx context.Context, key types.NamespacedName, obj client.Object, options ...client.GetOption) error {
	if _, ok := obj.(*corev1.Secret); ok {
		s.secretReads++
	}
	return s.Client.Get(ctx, key, obj, options...)
}
func (s *referenceClientSpy) Create(ctx context.Context, obj client.Object, options ...client.CreateOption) error {
	s.writes++
	return s.Client.Create(ctx, obj, options...)
}
func (s *referenceClientSpy) Update(ctx context.Context, obj client.Object, options ...client.UpdateOption) error {
	s.writes++
	return s.Client.Update(ctx, obj, options...)
}
func (s *referenceClientSpy) Patch(ctx context.Context, obj client.Object, patch client.Patch, options ...client.PatchOption) error {
	s.writes++
	return s.Client.Patch(ctx, obj, patch, options...)
}

func referenceFixtures(t *testing.T) (*runtime.Scheme, *showroomv1alpha1.SimpleCluster, *showroomv1alpha1.SimpleClusterProviderConfig) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := showroomv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	sc := &showroomv1alpha1.SimpleCluster{ObjectMeta: metav1.ObjectMeta{Namespace: "account", Name: "demo", UID: types.UID("request")}, Spec: showroomv1alpha1.SimpleClusterSpec{ProviderConfigRefs: []string{"provider"}}}
	pc := &showroomv1alpha1.SimpleClusterProviderConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "account", Name: "provider"}, Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{Target: showroomv1alpha1.TargetSpec{Namespace: "account"}, Resources: []showroomv1alpha1.ResourceSpec{{Name: "seed", APIVersion: "v1", Kind: "ConfigMap", Template: "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: seed}\n"}}, KubeconfigSource: showroomv1alpha1.KubeconfigSource{ResourceName: "seed", SecretRef: &showroomv1alpha1.KubeconfigSecretSource{Name: "key", Namespace: "foreign", DataKey: "password"}}}}
	return scheme, sc, pc
}

func TestReferenceDeniedBeforeReconcileOrDeleteMutations(t *testing.T) {
	for _, dynamicSecret := range []bool{false, true} {
		for _, deleting := range []bool{false, true} {
			t.Run(map[bool]string{false: "target", true: "dynamic"}[dynamicSecret]+map[bool]string{false: "_create", true: "_delete"}[deleting], func(t *testing.T) {
				scheme, sc, pc := referenceFixtures(t)
				if dynamicSecret {
					pc.Spec.Resources[0].APIVersion = "v1"
					pc.Spec.Resources[0].Kind = "Secret"
					pc.Spec.Resources[0].Namespace = "foreign"
				} else {
					pc.Spec.Target.KubeconfigSecretRef = &corev1.SecretReference{Namespace: "foreign", Name: "key"}
				}
				if deleting {
					now := metav1.Now()
					sc.DeletionTimestamp = &now
					sc.Finalizers = []string{showroomv1alpha1.FinalizerName}
				}
				spy := &referenceClientSpy{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(sc, pc).Build()}
				var networkCalls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					networkCalls.Add(1)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","code":403}`))
				}))
				defer server.Close()
				r := &SimpleClusterReconciler{Client: spy, Scheme: scheme, RESTConfig: &rest.Config{Host: server.URL}}
				_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sc)})
				if !errors.Is(err, secretpolicy.ErrDenied) {
					t.Fatalf("expected denial, got %v", err)
				}
				if spy.secretReads != 0 || spy.writes != 0 || networkCalls.Load() != 0 {
					t.Fatalf("denial touched a Secret, changed a request, or called a server: reads=%d writes=%d network=%d", spy.secretReads, spy.writes, networkCalls.Load())
				}
			})
		}
	}
}

func TestReferenceDeniedSCPCProbeDoesNotRead(t *testing.T) {
	scheme, _, pc := referenceFixtures(t)
	pc.Spec.Target.KubeconfigSecretRef = &corev1.SecretReference{Namespace: "foreign", Name: "key"}
	spy := &referenceClientSpy{Client: fake.NewClientBuilder().WithScheme(scheme).Build()}
	r := &SimpleClusterProviderConfigReconciler{Client: spy, Scheme: scheme}
	_, condition, err := r.targetMapper(context.Background(), pc)
	if err != nil || condition == nil || condition.Reason != "SecretReferenceDenied" {
		t.Fatalf("missing denial condition: condition=%v error=%v", condition, err)
	}
	if spy.secretReads != 0 || spy.writes != 0 {
		t.Fatalf("probe accessed denied reference: reads=%d writes=%d", spy.secretReads, spy.writes)
	}
}

func TestReferenceDeniedSourceDoesNotCreateOutput(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		t.Run(map[bool]string{false: "denied", true: "allowed"}[allowed], func(t *testing.T) {
			scheme, sc, pc := referenceFixtures(t)
			secret := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": "key", "namespace": "foreign"}, "data": map[string]any{"password": base64.StdEncoding.EncodeToString([]byte("opaque-value"))}}}
			live := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "seed", "namespace": "account"}}}
			dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), secret)
			policy := secretpolicy.Policy{}
			if allowed {
				policy.Allowlist = []secretpolicy.Entry{{Namespace: "foreign", Name: "key", Purpose: secretpolicy.Copy}}
			}
			spy := &referenceClientSpy{Client: fake.NewClientBuilder().WithScheme(scheme).Build()}
			r := &SimpleClusterReconciler{Client: spy, Scheme: scheme, SecretPolicy: policy}
			tc := &target.Client{Dynamic: dynamic, SecretPolicy: policy, OwnerNamespace: sc.Namespace}
			r.syncKubeconfig(context.Background(), sc, pc, &pc.Spec.Resources[0], live, tc)
			if !allowed {
				if len(dynamic.Actions()) != 0 || spy.secretReads != 0 || spy.writes != 0 || sc.Status.KubeconfigSecretRef != nil {
					t.Fatalf("denied source produced output: actions=%v reads=%d writes=%d", dynamic.Actions(), spy.secretReads, spy.writes)
				}
			} else {
				var output corev1.Secret
				if err := spy.Client.Get(context.Background(), types.NamespacedName{Namespace: "account", Name: "demo-kubeconfig"}, &output); err != nil {
					t.Fatal(err)
				}
				if string(output.Data["kubeconfig"]) != "opaque-value" {
					t.Fatal("approved output content differs")
				}
			}
		})
	}
}
