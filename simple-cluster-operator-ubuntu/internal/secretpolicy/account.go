package secretpolicy

import (
	"crypto/sha256"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ResolveForObject keeps account references inside their projected namespace.
// Sync Agent supplies these labels and annotations on the physical copy.
func (p Policy) ResolveForObject(owner metav1.Object, namespace, name, purpose string) (string, error) {
	cluster := owner.GetLabels()["syncagent.kcp.io/remote-object-cluster"]
	sourceNamespace := owner.GetAnnotations()["syncagent.kcp.io/remote-object-namespace"]
	if cluster == "" && sourceNamespace == "" {
		return p.Resolve(owner.GetNamespace(), namespace, name, purpose)
	}
	digest := sha256.Sum256([]byte(cluster + "/" + sourceNamespace))
	expectedNamespace := fmt.Sprintf("uc-%x", digest[:20])
	if cluster == "" || sourceNamespace == "" || owner.GetNamespace() != expectedNamespace {
		return "", fmt.Errorf("%w: invalid account namespace projection", ErrDenied)
	}
	if namespace != "" && namespace != sourceNamespace {
		return "", fmt.Errorf("%w: account Secret must use the resource namespace", ErrDenied)
	}
	return p.Resolve(owner.GetNamespace(), "", name, purpose)
}
