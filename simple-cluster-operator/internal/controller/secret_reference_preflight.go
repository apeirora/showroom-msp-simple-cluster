package controller

import (
	"context"
	"fmt"
	"strings"

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
	deleting := !sc.DeletionTimestamp.IsZero()
	for i := range scpc.Spec.Resources {
		resource := &scpc.Spec.Resources[i]
		if resource.APIVersion != "v1" || resource.Kind != "Secret" {
			continue
		}
		references := trackedSecretReferences(sc, resource.Name)
		if !deleting || len(references) == 0 {
			renderContext, err := rendertpl.BuildContext(scpc, sc, resource, nil)
			if err != nil {
				if deleting {
					return err
				}
				// Normal reconciliation reports input and profile errors. The actual
				// resource caller still authorizes each fully resolved reference.
				continue
			}
			targetContext := renderContext["Target"].(map[string]any)
			namespace := targetContext["namespace"].(string)
			base := targetContext["name"].(string)
			count := 1
			if resource.Repeat != "" {
				count, err = rendertpl.EvaluateRepeat(scpc, sc, resource, nil)
				if err != nil {
					if deleting {
						return err
					}
					continue
				}
			}
			references = nil
			for index := 0; index < count; index++ {
				name := base
				if resource.Repeat != "" {
					name = fmt.Sprintf("%s-%d", base, index)
				}
				references = append(references, secretReference{namespace: namespace, name: name})
			}
		}
		for _, ref := range references {
			if deleting && ref.namespace == "" {
				ref.namespace = resource.Namespace
				if ref.namespace == "" {
					ref.namespace = scpc.Spec.Target.Namespace
				}
				if strings.Contains(ref.namespace, "{{") {
					namespace, err := rendertpl.ResolveNamespace(scpc, sc, resource)
					if err != nil {
						return err
					}
					ref.namespace = namespace
				}
			}
			if _, err := r.SecretPolicy.Resolve(sc.Namespace, ref.namespace, ref.name, secretpolicy.Copy); err != nil {
				return err
			}
			if deleting {
				if _, err := r.SecretPolicy.Resolve(sc.Namespace, ref.namespace, ref.name, secretpolicy.Write); err != nil {
					return err
				}
			}
		}
	}
	// Source templates can depend on live predecessor data. Fetch authorizes the
	// fully resolved reference immediately before its Secret read.
	return nil
}

type secretReference struct{ namespace, name string }

func trackedSecretReferences(sc *showroomv1alpha1.SimpleCluster, name string) []secretReference {
	for _, status := range sc.Status.Resources {
		if status.Name != name {
			continue
		}
		refs := make([]secretReference, 0, len(status.Instances))
		if len(status.Instances) > 0 {
			for _, instance := range status.Instances {
				if instance.TargetName != "" {
					refs = append(refs, secretReference{namespace: instance.TargetNamespace, name: instance.TargetName})
				}
			}
		} else if status.TargetName != "" {
			refs = append(refs, secretReference{namespace: status.TargetNamespace, name: status.TargetName})
		}
		return refs
	}
	return nil
}

func requiresDefaultNamespace(status *showroomv1alpha1.ResourceStatus) bool {
	if status == nil {
		return true
	}
	if len(status.Instances) == 0 {
		return status.TargetNamespace == ""
	}
	for _, instance := range status.Instances {
		if instance.TargetNamespace == "" {
			return true
		}
	}
	return false
}
