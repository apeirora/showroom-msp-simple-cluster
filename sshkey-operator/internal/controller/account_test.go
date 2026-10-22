package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	api "github.com/apeirora/showroom-msp-simple-cluster/sshkey-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"
)

func TestAccountSSHKeyReconcile(t *testing.T) {
	for _, namespace := range []string{"", "default", "foreign"} {
		t.Run("reference-"+namespace, func(t *testing.T) {
			ctx := context.Background()
			digest := sha256.Sum256([]byte("account-a/default"))
			physical := fmt.Sprintf("sk-%x", digest[:20])
			key := &api.SSHKey{ObjectMeta: metav1.ObjectMeta{Name: "key", Namespace: physical, UID: "key-uid", Labels: map[string]string{"syncagent.kcp.io/remote-object-cluster": "account-a"}, Annotations: map[string]string{"syncagent.kcp.io/remote-object-namespace": "default"}}, Spec: api.SSHKeySpec{PassPhaseSecret: corev1.SecretReference{Name: "pass", Namespace: namespace}, ExportToSecret: corev1.LocalObjectReference{Name: "export"}}}
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pass", Namespace: physical}, Data: map[string][]byte{"password": []byte("fixture-passphrase")}}
			scheme := testScheme(t)
			spy := &secretReadSpy{Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(key).WithObjects(key, secret).Build()}
			r := &SSHKeyReconciler{Client: spy, Scheme: scheme}
			request := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: physical, Name: "key"}}
			if _, err := r.Reconcile(ctx, request); err != nil {
				t.Fatal(err)
			}
			var got api.SSHKey
			if err := spy.Client.Get(ctx, client.ObjectKeyFromObject(key), &got); err != nil {
				t.Fatal(err)
			}
			if namespace == "foreign" {
				if got.Status.Ready || len(spy.reads) != 0 {
					t.Fatalf("foreign reference: ready=%t reads=%d", got.Status.Ready, len(spy.reads))
				}
				return
			}
			if !got.Status.Ready || got.Status.PublicKey == "" || got.Status.PublicKey != got.PublicKey {
				t.Fatal("public key status is missing")
			}
			var exported corev1.Secret
			if err := spy.Client.Get(ctx, types.NamespacedName{Namespace: physical, Name: "export"}, &exported); err != nil {
				t.Fatal(err)
			}
			if len(exported.Data[corev1.SSHAuthPrivateKey]) == 0 || len(exported.Data["ssh-publickey"]) == 0 {
				t.Fatal("exported key data is missing")
			}
			if len(exported.OwnerReferences) != 1 || exported.OwnerReferences[0].UID != key.UID {
				t.Fatal("exported Secret has the wrong owner")
			}
			original := got.PrivateKey
			if _, err := r.Reconcile(ctx, request); err != nil {
				t.Fatal(err)
			}
			if err := spy.Client.Get(ctx, client.ObjectKeyFromObject(key), &got); err != nil {
				t.Fatal(err)
			}
			if got.PrivateKey != original {
				t.Fatal("key changes on unchanged reconcile")
			}
		})
	}
}

func TestAccountSSHKeyWaitsForPassphrase(t *testing.T) {
	ctx := context.Background()
	digest := sha256.Sum256([]byte("account-a/default"))
	physical := fmt.Sprintf("sk-%x", digest[:20])
	key := &api.SSHKey{ObjectMeta: metav1.ObjectMeta{Name: "key", Namespace: physical, Labels: map[string]string{"syncagent.kcp.io/remote-object-cluster": "account-a"}, Annotations: map[string]string{"syncagent.kcp.io/remote-object-namespace": "default"}}, Spec: api.SSHKeySpec{PassPhaseSecret: corev1.SecretReference{Name: "pass", Namespace: "default"}}}
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(key).WithObjects(key).Build()
	r := &SSHKeyReconciler{Client: cl, Scheme: scheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: key.Name, Namespace: key.Namespace}}
	result, err := r.Reconcile(ctx, request)
	if err != nil || result.RequeueAfter == 0 {
		t.Fatalf("missing passphrase must retry: result=%v error=%v", result, err)
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pass", Namespace: physical}, Data: map[string][]byte{"password": []byte("fixture-passphrase")}}
	if err := cl.Create(ctx, secret); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	var got api.SSHKey
	if err := cl.Get(ctx, client.ObjectKeyFromObject(key), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Status.Ready {
		t.Fatal("key remains blocked after the passphrase arrives")
	}
}
