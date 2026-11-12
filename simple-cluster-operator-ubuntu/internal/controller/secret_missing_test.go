package controller

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	api "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ubuntu/api/v1alpha1"
	sshpkg "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ubuntu/internal/ssh"
)

// Login Secrets are often synced into the namespace after the UbuntuCluster
// exists, and can be gone again before it is deleted. Neither case may
// leave the cluster Failed or stuck behind its finalizer.

type secretMissingHarness struct {
	r        *UbuntuClusterReconciler
	cl       client.Client
	recorder *record.FakeRecorder
	dials    []sshpkg.Credential
}

func newSecretMissingHarness(t *testing.T, c *api.UbuntuCluster, objs ...client.Object) *secretMissingHarness {
	t.Helper()
	scheme := testScheme(t)
	h := &secretMissingHarness{recorder: record.NewFakeRecorder(100)}
	h.cl = fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(c).WithObjects(append(objs, c)...).Build()
	dialErr := errors.New("dial spy")
	h.r = &UbuntuClusterReconciler{Client: h.cl, Scheme: scheme, Recorder: h.recorder,
		sshDial: func(_ context.Context, _ string, _ int, _ string, cred sshpkg.Credential) (*sshpkg.Client, error) {
			h.dials = append(h.dials, cred)
			return nil, dialErr
		},
		sshDialVia: func(_ context.Context, _ *sshpkg.Client, _ string, _ int, _ string, cred sshpkg.Credential) (*sshpkg.Client, error) {
			h.dials = append(h.dials, cred)
			return nil, dialErr
		},
	}
	return h
}

func (h *secretMissingHarness) reconcile(t *testing.T, c *api.UbuntuCluster) ctrl.Result {
	t.Helper()
	res, err := h.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: c.Namespace, Name: c.Name}})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (h *secretMissingHarness) get(t *testing.T, c *api.UbuntuCluster) (*api.UbuntuCluster, bool) {
	t.Helper()
	var stored api.UbuntuCluster
	err := h.cl.Get(context.Background(), client.ObjectKeyFromObject(c), &stored)
	if apierrors.IsNotFound(err) {
		return nil, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return &stored, true
}

func (h *secretMissingHarness) events() string {
	var out []string
	for {
		select {
		case e := <-h.recorder.Events:
			out = append(out, e)
		default:
			return strings.Join(out, "\n")
		}
	}
}

func loginSecret() *corev1.Secret {
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "account", Name: "login"}, Data: map[string][]byte{"password": []byte("value")}}
}

func deleting(c *api.UbuntuCluster) *api.UbuntuCluster {
	now := metav1.Now()
	c.DeletionTimestamp = &now
	return c
}

func TestCreateWaitsForMissingLoginSecret(t *testing.T) {
	c := clusterFixture()
	// State left by an operator version that treated the missing Secret as fatal.
	c.Status.Phase = api.PhaseFailed
	c.Status.Conditions = []metav1.Condition{{Type: api.ConditionError, Status: metav1.ConditionTrue, Reason: reasonSSHKeyError, Message: "old", LastTransitionTime: metav1.Now()}}
	h := newSecretMissingHarness(t, c)

	res := h.reconcile(t, c)
	if res.RequeueAfter <= 0 {
		t.Fatalf("missing Secret must requeue, got %+v", res)
	}
	if len(h.dials) != 0 {
		t.Fatalf("dialed without a credential: %d", len(h.dials))
	}
	stored, _ := h.get(t, c)
	if stored.Status.Phase != api.PhaseProvisioning {
		t.Fatalf("phase=%q, want %q", stored.Status.Phase, api.PhaseProvisioning)
	}
	ready := apimeta.FindStatusCondition(stored.Status.Conditions, api.ConditionReady)
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != reasonWaitingForSecret || !strings.Contains(ready.Message, "account/login") {
		t.Fatalf("ready condition=%+v", ready)
	}
	if apimeta.FindStatusCondition(stored.Status.Conditions, api.ConditionError) != nil {
		t.Fatalf("stale SSHKeyError condition kept: %+v", stored.Status.Conditions)
	}
}

func TestCreateKeepsOtherErrorConditionsWhileWaiting(t *testing.T) {
	c := clusterFixture()
	c.Status.Conditions = []metav1.Condition{{Type: api.ConditionError, Status: metav1.ConditionTrue, Reason: "EncryptionMismatch", Message: "old", LastTransitionTime: metav1.Now()}}
	h := newSecretMissingHarness(t, c)
	h.reconcile(t, c)
	stored, _ := h.get(t, c)
	if e := apimeta.FindStatusCondition(stored.Status.Conditions, api.ConditionError); e == nil || e.Reason != "EncryptionMismatch" {
		t.Fatalf("unrelated Error condition changed: %+v", stored.Status.Conditions)
	}
}

func TestCreateWithLoginSecretProceeds(t *testing.T) {
	c := clusterFixture()
	h := newSecretMissingHarness(t, c, loginSecret())

	res := h.reconcile(t, c)
	if res.RequeueAfter <= 0 {
		t.Fatalf("result=%+v", res)
	}
	if len(h.dials) != 1 || h.dials[0].Password != "value" {
		t.Fatalf("expected one bootstrap dial with the Secret's password, got %+v", h.dials)
	}
	stored, _ := h.get(t, c)
	if stored.Status.Phase != api.PhaseProvisioning {
		t.Fatalf("phase=%q", stored.Status.Phase)
	}
	if len(stored.Status.HostStatuses) != 1 || stored.Status.HostStatuses[0].Phase != hostPhaseFailed || !strings.Contains(stored.Status.HostStatuses[0].Message, "dial spy") {
		t.Fatalf("host statuses=%+v", stored.Status.HostStatuses)
	}
}

func TestCreateWaitsForMissingHostLoginSecret(t *testing.T) {
	cases := []struct {
		name   string
		change func(*api.UbuntuCluster)
	}{
		{"bootstrap", func(c *api.UbuntuCluster) {
			c.Spec.ControlPlane.Hosts[0].PasswordSecretRef = &corev1.SecretReference{Name: "cp-password"}
		}},
		{"worker", func(c *api.UbuntuCluster) {
			c.Spec.Workers.Hosts[0].PasswordSecretRef = &corev1.SecretReference{Name: "worker-password"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := clusterFixture()
			tc.change(c)
			h := newSecretMissingHarness(t, c, loginSecret())

			res := h.reconcile(t, c)
			if res.RequeueAfter <= 0 {
				t.Fatalf("missing host Secret must requeue, got %+v", res)
			}
			if len(h.dials) != 0 {
				t.Fatalf("dialed before every host Secret exists: %d", len(h.dials))
			}
			stored, _ := h.get(t, c)
			if stored.Status.Phase != api.PhaseProvisioning || len(stored.Status.HostStatuses) != 0 {
				t.Fatalf("phase=%q hosts=%+v", stored.Status.Phase, stored.Status.HostStatuses)
			}
			ready := apimeta.FindStatusCondition(stored.Status.Conditions, api.ConditionReady)
			if ready == nil || ready.Reason != reasonWaitingForSecret || !strings.Contains(ready.Message, "-password") {
				t.Fatalf("ready condition=%+v", ready)
			}
		})
	}
}

func TestDeleteWithMissingLoginSecretRemovesFinalizer(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*api.UbuntuCluster)
		secrets []client.Object
	}{
		{name: "cluster Secret"},
		{name: "bootstrap host Secret", secrets: []client.Object{loginSecret()}, change: func(c *api.UbuntuCluster) {
			c.Spec.ControlPlane.Hosts[0].PasswordSecretRef = &corev1.SecretReference{Name: "cp-password"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := clusterFixture()
			if tc.change != nil {
				tc.change(c)
			}
			c = deleting(c)
			h := newSecretMissingHarness(t, c, tc.secrets...)

			res := h.reconcile(t, c)
			if res.RequeueAfter != 0 {
				t.Fatalf("delete must finish, got %+v", res)
			}
			if len(h.dials) != 0 {
				t.Fatalf("dialed without a credential: %d", len(h.dials))
			}
			if stored, ok := h.get(t, c); ok {
				t.Fatalf("finalizer kept: %v", stored.Finalizers)
			}
			if ev := h.events(); !strings.Contains(ev, "UninstallSkipped") {
				t.Fatalf("no UninstallSkipped event, got %q", ev)
			}
		})
	}
}

func TestUninstallViaReportsMissingHostSecret(t *testing.T) {
	// reconcileDelete skips (not retries) a host whose own Secret is gone.
	// That branch needs a live bastion, so check the error it keys on here.
	c := deleting(clusterFixture())
	c.Spec.Workers.Hosts[0].PasswordSecretRef = &corev1.SecretReference{Name: "worker-password"}
	h := newSecretMissingHarness(t, c, loginSecret())
	err := h.r.uninstallVia(context.Background(), c, nil, c.Spec.Workers.Hosts[0], sshpkg.Credential{Password: "value"}, false)
	if !errors.Is(err, errSecretMissing) {
		t.Fatalf("error=%v, want errSecretMissing", err)
	}
	if len(h.dials) != 0 {
		t.Fatalf("dialed without a credential: %d", len(h.dials))
	}
}

func TestDeleteWithLoginSecretKeepsCleanupPath(t *testing.T) {
	c := deleting(clusterFixture())
	h := newSecretMissingHarness(t, c, loginSecret())

	res := h.reconcile(t, c)
	if res.RequeueAfter <= 0 {
		t.Fatalf("failed bastion dial must requeue, got %+v", res)
	}
	if len(h.dials) != 1 || h.dials[0].Password != "value" {
		t.Fatalf("expected bootstrap dial with the Secret's password, got %+v", h.dials)
	}
	stored, ok := h.get(t, c)
	if !ok || len(stored.Finalizers) != 1 {
		t.Fatal("finalizer must stay until the uninstall ran")
	}
	if ev := h.events(); strings.Contains(ev, "UninstallSkipped") {
		t.Fatalf("uninstall skipped with a Secret present: %q", ev)
	}
}

func TestDeleteWithMissingLoginSecretStillWaitsForKMSCleanup(t *testing.T) {
	c := encryptedFixture()
	c.Status.Encryption = &api.EncryptionStatus{Provider: api.EncryptionProviderOpenBao, KeyName: "k", RoleName: "r"}
	c = deleting(c)
	// No OpenBao configuration: the KMS cleanup fails and must hold the finalizer.
	h := newSecretMissingHarness(t, c)

	res := h.reconcile(t, c)
	if res.RequeueAfter <= 0 {
		t.Fatalf("result=%+v", res)
	}
	if _, ok := h.get(t, c); !ok {
		t.Fatal("finalizer removed before KMS cleanup succeeded")
	}
}
