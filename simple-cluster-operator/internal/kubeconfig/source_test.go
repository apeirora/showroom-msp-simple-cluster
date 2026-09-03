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

package kubeconfig

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	metadatafake "k8s.io/client-go/metadata/fake"
	clienttesting "k8s.io/client-go/testing"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/target"
)

const (
	testSecretName      = "claim-kubeconfig"
	testSecretNamespace = "account-1"
	testKubeconfig      = "apiVersion: v1\nkind: Config\n"
)

func newSecret(labels, annotations map[string]string, uid types.UID, resourceVersion string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"data": map[string]any{
			"kubeconfig": base64.StdEncoding.EncodeToString([]byte(testKubeconfig)),
		},
	}}
	obj.SetNamespace(testSecretNamespace)
	obj.SetName(testSecretName)
	obj.SetUID(uid)
	obj.SetResourceVersion(resourceVersion)
	obj.SetLabels(labels)
	obj.SetAnnotations(annotations)
	return obj
}

func partialMetadataFor(obj metav1.Object) *metav1.PartialObjectMetadata {
	return &metav1.PartialObjectMetadata{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{
			Name:            obj.GetName(),
			Namespace:       obj.GetNamespace(),
			UID:             obj.GetUID(),
			ResourceVersion: obj.GetResourceVersion(),
			Labels:          obj.GetLabels(),
			Annotations:     obj.GetAnnotations(),
		},
	}
}

func fakeTargetClient(
	t *testing.T,
	metadataObj *metav1.PartialObjectMetadata,
	secret *unstructured.Unstructured,
) (*target.Client, *dynamicfake.FakeDynamicClient, *metadatafake.FakeMetadataClient) {
	t.Helper()
	metadataScheme := runtime.NewScheme()
	if err := metav1.AddMetaToScheme(metadataScheme); err != nil {
		t.Fatalf("add metadata types to test scheme: %v", err)
	}
	metadataClient := metadatafake.NewSimpleMetadataClient(metadataScheme, metadataObj)
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), secret)
	return &target.Client{Dynamic: dynamicClient, Metadata: metadataClient}, dynamicClient, metadataClient
}

func secretRefFixtures() (
	*showroomv1alpha1.SimpleClusterProviderConfig,
	*showroomv1alpha1.SimpleCluster,
	*showroomv1alpha1.ResourceSpec,
	*unstructured.Unstructured,
) {
	scpc := &showroomv1alpha1.SimpleClusterProviderConfig{Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{
		Target: showroomv1alpha1.TargetSpec{Namespace: "{{ .SC.namespace }}"},
	}}
	sc := &showroomv1alpha1.SimpleCluster{ObjectMeta: metav1.ObjectMeta{
		Name: "demo", Namespace: testSecretNamespace,
	}}
	resource := &showroomv1alpha1.ResourceSpec{
		Name: "shoot", APIVersion: "provider.example/v1", Kind: "Claim", NameTemplate: "claim",
	}
	live := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "provider.example/v1",
		"kind":       "Claim",
		"metadata": map[string]any{
			"name": "claim", "namespace": testSecretNamespace,
		},
		"status": map[string]any{
			"kubeconfig": map[string]any{"secretRef": map[string]any{"name": testSecretName}},
		},
	}}
	return scpc, sc, resource, live
}

func TestFetchSecretRefRequiresExactRenderedMetadata(t *testing.T) {
	scpc, sc, resource, live := secretRefFixtures()
	src := &showroomv1alpha1.KubeconfigSecretSource{
		Name: "{{ .Resources.shoot.status.kubeconfig.secretRef.name }}",
		RequiredLabels: map[string]string{
			"sync.example/name":  "{{ .Resources.shoot.status.kubeconfig.secretRef.name }}",
			"sync.example/owner": "{{ .SC.namespace }}",
		},
		RequiredAnnotations: map[string]string{
			"sync.example/origin": "provider#{{ .SC.namespace }}",
			"sync.example/name":   "{{ .Resources.shoot.status.kubeconfig.secretRef.name }}",
		},
	}

	tests := []struct {
		name        string
		labels      map[string]string
		annotations map[string]string
		wantErr     string
	}{
		{
			name: "exact metadata requirements",
			labels: map[string]string{
				"sync.example/name": testSecretName, "sync.example/owner": "account-1",
			},
			annotations: map[string]string{
				"sync.example/origin": "provider#account-1", "sync.example/name": testSecretName,
			},
		},
		{
			name:        "unrelated same-name Secret",
			labels:      map[string]string{"app": "unrelated"},
			annotations: map[string]string{},
			wantErr:     `required label "sync.example/name" does not match`,
		},
		{
			name: "wrong origin route",
			labels: map[string]string{
				"sync.example/name": testSecretName, "sync.example/owner": "account-1",
			},
			annotations: map[string]string{
				"sync.example/origin": "other#account-1", "sync.example/name": testSecretName,
			},
			wantErr: `required annotation "sync.example/origin" does not match`,
		},
		{
			name: "wrong mirrored source name",
			labels: map[string]string{
				"sync.example/name": testSecretName, "sync.example/owner": "account-1",
			},
			annotations: map[string]string{
				"sync.example/origin": "provider#account-1", "sync.example/name": "another-secret",
			},
			wantErr: `required annotation "sync.example/name" does not match`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret := newSecret(tt.labels, tt.annotations, "secret-uid", "7")
			tc, dynamicClient, metadataClient := fakeTargetClient(t, partialMetadataFor(secret), secret)
			got, _, err := fetchSecretRef(context.Background(), tc, src, scpc, sc, resource, live)
			if len(metadataClient.Actions()) != 1 || metadataClient.Actions()[0].GetVerb() != "get" {
				t.Fatalf("metadata actions = %#v, want one GET", metadataClient.Actions())
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("fetchSecretRef() error = %v, want substring %q", err, tt.wantErr)
				}
				if len(dynamicClient.Actions()) != 0 {
					t.Fatalf("full Secret read occurred after metadata mismatch: %#v", dynamicClient.Actions())
				}
				return
			}
			if err != nil {
				t.Fatalf("fetchSecretRef() unexpected error: %v", err)
			}
			if string(got) != testKubeconfig {
				t.Fatalf("fetchSecretRef() = %q, want %q", got, testKubeconfig)
			}
			if len(dynamicClient.Actions()) != 1 || dynamicClient.Actions()[0].GetVerb() != "list" {
				t.Fatalf("dynamic actions = %#v, want one label-filtered LIST", dynamicClient.Actions())
			}
			listAction, ok := dynamicClient.Actions()[0].(clienttesting.ListAction)
			if !ok {
				t.Fatalf("dynamic action = %#v, want ListAction", dynamicClient.Actions()[0])
			}
			restrictions := listAction.GetListRestrictions()
			if got := restrictions.Fields.String(); got != "metadata.name="+testSecretName {
				t.Fatalf("field selector = %q, want metadata.name=%s", got, testSecretName)
			}
			if got := restrictions.Labels.String(); got != "sync.example/name="+testSecretName+",sync.example/owner=account-1" {
				t.Fatalf("label selector = %q, want all rendered requirements", got)
			}
		})
	}
}

func TestFetchSecretRefPreservesEmptyMetadataRequirements(t *testing.T) {
	scpc, sc, resource, live := secretRefFixtures()
	for _, tt := range []struct {
		name string
		src  *showroomv1alpha1.KubeconfigSecretSource
	}{
		{name: "omitted maps", src: &showroomv1alpha1.KubeconfigSecretSource{Name: testSecretName}},
		{name: "explicit empty maps", src: &showroomv1alpha1.KubeconfigSecretSource{
			Name: testSecretName, RequiredLabels: map[string]string{}, RequiredAnnotations: map[string]string{},
		}},
		{name: "annotation-only compatibility", src: &showroomv1alpha1.KubeconfigSecretSource{
			Name: testSecretName, RequiredAnnotations: map[string]string{"sync.example/owner": testSecretNamespace},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			secret := newSecret(nil, map[string]string{"sync.example/owner": testSecretNamespace}, "secret-uid", "7")
			tc, dynamicClient, _ := fakeTargetClient(t, partialMetadataFor(secret), secret)
			got, _, err := fetchSecretRef(context.Background(), tc, tt.src, scpc, sc, resource, live)
			if err != nil {
				t.Fatalf("fetchSecretRef() unexpected error: %v", err)
			}
			if string(got) != testKubeconfig || len(dynamicClient.Actions()) != 1 || dynamicClient.Actions()[0].GetVerb() != "get" {
				t.Fatalf("legacy SecretRef result = %q, dynamic actions = %#v", got, dynamicClient.Actions())
			}
		})
	}
}

func TestFetchSecretRefRejectsMetadataDataRace(t *testing.T) {
	scpc, sc, resource, live := secretRefFixtures()
	src := &showroomv1alpha1.KubeconfigSecretSource{
		Name: testSecretName,
		RequiredLabels: map[string]string{
			"sync.example/owner": testSecretNamespace,
		},
	}
	metadataSecret := newSecret(map[string]string{"sync.example/owner": testSecretNamespace}, nil, "secret-uid", "7")

	for _, tt := range []struct {
		name            string
		uid             types.UID
		resourceVersion string
	}{
		{name: "updated after metadata read", uid: "secret-uid", resourceVersion: "8"},
		{name: "replaced after metadata read", uid: "replacement-uid", resourceVersion: "1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fullSecret := newSecret(map[string]string{"sync.example/owner": testSecretNamespace}, nil, tt.uid, tt.resourceVersion)
			tc, dynamicClient, _ := fakeTargetClient(t, partialMetadataFor(metadataSecret), fullSecret)
			got, _, err := fetchSecretRef(context.Background(), tc, src, scpc, sc, resource, live)
			if err == nil || !strings.Contains(err.Error(), "changed after its metadata requirements were checked; retry") {
				t.Fatalf("fetchSecretRef() result = %q, error = %v, want retry error", got, err)
			}
			if len(dynamicClient.Actions()) != 1 || dynamicClient.Actions()[0].GetVerb() != "list" {
				t.Fatalf("dynamic actions = %#v, want one guarded full LIST", dynamicClient.Actions())
			}
		})
	}
}

func TestFetchSecretRefDoesNotReadReplacementWithoutRequiredLabel(t *testing.T) {
	scpc, sc, resource, live := secretRefFixtures()
	src := &showroomv1alpha1.KubeconfigSecretSource{
		Name: testSecretName,
		RequiredLabels: map[string]string{
			"sync.example/owner": testSecretNamespace,
		},
	}
	original := newSecret(map[string]string{"sync.example/owner": testSecretNamespace}, nil, "original-uid", "7")
	replacement := newSecret(map[string]string{"app": "unrelated"}, nil, "replacement-uid", "1")
	tc, dynamicClient, _ := fakeTargetClient(t, partialMetadataFor(original), replacement)

	got, _, err := fetchSecretRef(context.Background(), tc, src, scpc, sc, resource, live)
	if err == nil || !strings.Contains(err.Error(), "no longer matches all required labels") {
		t.Fatalf("fetchSecretRef() result = %q, error = %v, want label-filtered refusal", got, err)
	}
	actions := dynamicClient.Actions()
	if len(actions) != 1 || actions[0].GetVerb() != "list" {
		t.Fatalf("dynamic actions = %#v, want one LIST and no name-only GET", actions)
	}
	listAction, ok := actions[0].(clienttesting.ListAction)
	if !ok {
		t.Fatalf("dynamic action = %#v, want ListAction", actions[0])
	}
	restrictions := listAction.GetListRestrictions()
	if got := restrictions.Fields.String(); got != "metadata.name="+testSecretName {
		t.Fatalf("field selector = %q, want metadata.name=%s", got, testSecretName)
	}
	if got := restrictions.Labels.String(); got != "sync.example/owner="+testSecretNamespace {
		t.Fatalf("label selector = %q, want required owner label", got)
	}
}

func TestValidateSecretMetadataRejectsInvalidRequirement(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetLabels(map[string]string{"key": "value"})

	t.Run("empty key", func(t *testing.T) {
		err := validateSecretMetadataRequirements(obj, &showroomv1alpha1.KubeconfigSecretSource{
			RequiredLabels: map[string]string{" ": "value"},
		}, map[string]any{})
		if err == nil || !strings.Contains(err.Error(), "required label key is empty") {
			t.Fatalf("validateSecretMetadataRequirements() error = %v", err)
		}
	})

	t.Run("bad template", func(t *testing.T) {
		err := validateSecretMetadataRequirements(obj, &showroomv1alpha1.KubeconfigSecretSource{
			RequiredLabels: map[string]string{"key": "{{ .Missing }}"},
		}, map[string]any{})
		if err == nil || !strings.Contains(err.Error(), `render required label "key"`) {
			t.Fatalf("validateSecretMetadataRequirements() error = %v", err)
		}
	})
}
