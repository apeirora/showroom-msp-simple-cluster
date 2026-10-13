package controller

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/secretpolicy"
	rendertpl "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/template"
)

// preflightSecretReferences rejects known references before request mutations.
func (r *SimpleClusterReconciler) preflightSecretReferences(ctx context.Context, sc *showroomv1alpha1.SimpleCluster) error {
	name := selectProvider(sc.Spec.ProviderConfigRefs)
	if sc.Status.ProviderConfigRef != nil {
		name = sc.Status.ProviderConfigRef.Name
	}
	if name == "" {
		return nil
	}
	scpc, err := r.getSCPC(ctx, sc, name)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if ref := scpc.Spec.Target.KubeconfigSecretRef; ref != nil {
		if _, err := r.SecretPolicy.Resolve(sc.Namespace, ref.Namespace, ref.Name, secretpolicy.Use); err != nil {
			return err
		}
	}
	for i := range scpc.Spec.Resources {
		resource := &scpc.Spec.Resources[i]
		if resource.APIVersion != "v1" || resource.Kind != "Secret" {
			continue
		}
		renderContext, err := rendertpl.BuildContext(scpc, sc, resource, nil)
		if err != nil {
			return err
		}
		targetContext := renderContext["Target"].(map[string]any)
		namespace := targetContext["namespace"].(string)
		name := targetContext["name"].(string)
		if _, err := r.SecretPolicy.Resolve(sc.Namespace, namespace, name, secretpolicy.Copy); err != nil {
			return err
		}
	}
	// Source templates can depend on live predecessor data. Fetch authorizes the
	// fully resolved reference immediately before its Secret read.
	return nil
}
