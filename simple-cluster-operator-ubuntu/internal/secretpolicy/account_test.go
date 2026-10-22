package secretpolicy

import (
	"crypto/sha256"
	"errors"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestAccountSecretNamespace(t *testing.T) {
	digest := sha256.Sum256([]byte("account-a/default"))
	physical := fmt.Sprintf("uc-%x", digest[:20])
	owner := &metav1.ObjectMeta{Namespace: physical, Labels: map[string]string{"syncagent.kcp.io/remote-object-cluster": "account-a"}, Annotations: map[string]string{"syncagent.kcp.io/remote-object-namespace": "default"}}
	for _, namespace := range []string{"", "default"} {
		got, err := (Policy{}).ResolveForObject(owner, namespace, "login", Use)
		if err != nil || got != physical {
			t.Fatalf("namespace=%s error=%v", got, err)
		}
	}
	for _, namespace := range []string{"foreign", physical} {
		_, err := (Policy{Allowlist: []Entry{{Namespace: namespace, Name: "login", Purpose: Use}}}).ResolveForObject(owner, namespace, "login", Use)
		if !errors.Is(err, ErrDenied) {
			t.Fatalf("foreign namespace accepted: %v", err)
		}
	}
	owner.Namespace = "other-account"
	if _, err := (Policy{}).ResolveForObject(owner, "default", "login", Use); !errors.Is(err, ErrDenied) {
		t.Fatalf("invalid projection accepted: %v", err)
	}
	owner.Namespace = physical
	delete(owner.Labels, "syncagent.kcp.io/remote-object-cluster")
	if _, err := (Policy{}).ResolveForObject(owner, "", "login", Use); !errors.Is(err, ErrDenied) {
		t.Fatalf("missing identity accepted: %v", err)
	}
}
