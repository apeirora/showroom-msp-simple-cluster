package controller

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"

	api "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ubuntu/api/v1alpha1"
	sshpkg "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ubuntu/internal/ssh"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAccountCredentialReconcile(t *testing.T) {
	for _, namespace := range []string{"", "default", "foreign"} {
		t.Run("reference-"+namespace, func(t *testing.T) {
			c := clusterFixture()
			digest := sha256.Sum256([]byte("account-a/default"))
			c.Namespace = fmt.Sprintf("uc-%x", digest[:20])
			c.Labels = map[string]string{"syncagent.kcp.io/remote-object-cluster": "account-a"}
			c.Annotations = map[string]string{"syncagent.kcp.io/remote-object-namespace": "default"}
			c.Spec.SSH.PasswordSecretRef.Namespace = namespace
			c.Spec.Workers.Hosts[0].PasswordSecretRef = &corev1.SecretReference{Name: "login", Namespace: namespace}
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "login", Namespace: c.Namespace}, Data: map[string][]byte{"password": []byte("fixture-password")}}
			scheme := testScheme(t)
			spy := &secretReadSpy{Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(c).WithObjects(c, secret).Build()}
			calls := 0
			r := &UbuntuClusterReconciler{Client: spy, Scheme: scheme, sshDial: func(context.Context, string, int, string, sshpkg.Credential) (*sshpkg.Client, error) {
				calls++
				return nil, errors.New("dial spy")
			}}
			if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: c.Name, Namespace: c.Namespace}}); err != nil {
				t.Fatal(err)
			}
			if namespace == "foreign" {
				if calls != 0 || len(spy.reads) != 0 {
					t.Fatalf("foreign reference: dials=%d reads=%d", calls, len(spy.reads))
				}
				return
			}
			// The host Secret pre-check reads the worker Secret too. Every read must stay in the account namespace.
			if calls != 1 || len(spy.reads) == 0 {
				t.Fatalf("dials=%d reads=%v", calls, spy.reads)
			}
			for _, read := range spy.reads {
				if read.Namespace != c.Namespace {
					t.Fatalf("read outside the account namespace: %v", spy.reads)
				}
			}
			_, cred, err := r.hostCredential(context.Background(), c, c.Spec.Workers.Hosts[0], sshpkg.Credential{})
			if err != nil || cred.Password == "" {
				t.Fatalf("host credential: error=%v", err)
			}
		})
	}
}

func TestAccountKubeconfigSecret(t *testing.T) {
	c := clusterFixture()
	c.Namespace = "physical-account"
	c.UID = "cluster-uid"
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &UbuntuClusterReconciler{Client: cl, Scheme: scheme}
	for _, value := range []string{"fixture-config", "fixture-config", "rotated-config"} {
		name, err := r.ensureKubeconfigSecret(context.Background(), c, []byte(value))
		if err != nil {
			t.Fatal(err)
		}
		var secret corev1.Secret
		if err := cl.Get(context.Background(), client.ObjectKey{Namespace: c.Namespace, Name: name}, &secret); err != nil {
			t.Fatal(err)
		}
		if name != c.Name+api.KubeconfigSecretSuffix || string(secret.Data[api.KubeconfigSecretKey]) != value {
			t.Fatal("kubeconfig data or reference differs")
		}
		if len(secret.OwnerReferences) != 1 || secret.OwnerReferences[0].UID != c.UID {
			t.Fatal("kubeconfig Secret has the wrong owner")
		}
	}
}

func TestKubeconfigSecretRejectsForeignOwner(t *testing.T) {
	for _, sameData := range []bool{false, true} {
		c := clusterFixture()
		c.UID = "new-cluster"
		for _, owner := range []string{"", "old-cluster"} {
			scheme := testScheme(t)
			value := "old-config"
			if sameData {
				value = "new-config"
			}
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: c.Name + api.KubeconfigSecretSuffix, Namespace: c.Namespace}, Data: map[string][]byte{api.KubeconfigSecretKey: []byte(value)}}
			if owner != "" {
				yes := true
				secret.OwnerReferences = []metav1.OwnerReference{{APIVersion: api.GroupVersion.String(), Kind: "UbuntuCluster", Name: c.Name, UID: types.UID(owner), Controller: &yes}}
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
			r := &UbuntuClusterReconciler{Client: cl, Scheme: scheme}
			if _, err := r.ensureKubeconfigSecret(context.Background(), c, []byte("new-config")); err == nil {
				t.Fatal("foreign Secret accepted")
			}
			var got corev1.Secret
			if err := cl.Get(context.Background(), client.ObjectKeyFromObject(secret), &got); err != nil {
				t.Fatal(err)
			}
			if string(got.Data[api.KubeconfigSecretKey]) != value {
				t.Fatal("foreign Secret data changed")
			}
		}
	}
}
