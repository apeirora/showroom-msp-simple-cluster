/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
)

func ordSCPC(namespace, name string, ready bool, resources ...showroomv1alpha1.ResourceSpec) *showroomv1alpha1.SimpleClusterProviderConfig {
	status := metav1.ConditionFalse
	if ready {
		status = metav1.ConditionTrue
	}
	return &showroomv1alpha1.SimpleClusterProviderConfig{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{
			Resources:        resources,
			KubeconfigSource: showroomv1alpha1.KubeconfigSource{ResourceName: resources[len(resources)-1].Name},
		},
		Status: showroomv1alpha1.SimpleClusterProviderConfigStatus{
			Reachable: boolPointer(ready),
			Conditions: []metav1.Condition{{
				Type: showroomv1alpha1.ConditionReady, Status: status,
				Reason: "Probe", Message: "provider probe result",
			}},
		},
	}
}

func boolPointer(value bool) *bool { return &value }

func TestORDCatalogPreservesProviderDAGEdgesAndAvailability(t *testing.T) {
	resources := []showroomv1alpha1.ResourceSpec{
		{Name: "cp", APIVersion: "cloudferro.eu/v1alpha1", Kind: "OpenStackVM"},
		{Name: "workers", APIVersion: "cloudferro.eu/v1alpha1", Kind: "OpenStackVM", DependsOn: []string{"cp"}},
		{Name: "cluster", APIVersion: "ubuntu.apeirora.eu/v1alpha1", Kind: "UbuntuCluster", DependsOn: []string{"workers", "cp"}},
	}
	files, count, err := buildORDCatalog([]showroomv1alpha1.SimpleClusterProviderConfig{
		*ordSCPC("account-a", "cloudferro", true, resources...),
		*ordSCPC("account-b", "cloudferro", true, resources...), // same offering is deduplicated
		*ordSCPC("account-a", "dati", false, showroomv1alpha1.ResourceSpec{
			Name: "shoot", APIVersion: "dati.eu/v1alpha1", Kind: "Shoot",
		}),
	}, "0.35.0")
	if err != nil {
		t.Fatalf("buildORDCatalog: %v", err)
	}
	if count != 2 {
		t.Fatalf("provider count = %d, want 2", count)
	}

	var document struct {
		OpenResourceDiscovery string `json:"openResourceDiscovery"`
		Capabilities          []struct {
			Title    string `json:"title"`
			Disabled bool   `json:"disabled"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal([]byte(files[ORDDocumentKey]), &document); err != nil {
		t.Fatalf("ORD document is not JSON: %v", err)
	}
	if document.OpenResourceDiscovery != "1.16" || len(document.Capabilities) != 2 {
		t.Fatalf("unexpected ORD document: %+v", document)
	}
	if document.Capabilities[0].Title != "Cloudferro" || document.Capabilities[0].Disabled {
		t.Fatalf("ready provider availability was lost: %+v", document.Capabilities[0])
	}
	if document.Capabilities[1].Title != "Dati" || !document.Capabilities[1].Disabled {
		t.Fatalf("unavailable provider was not disabled: %+v", document.Capabilities[1])
	}

	var graph providerGraph
	if err := json.Unmarshal([]byte(files["provider-config-cloudferro.json"]), &graph); err != nil {
		t.Fatalf("provider definition is not JSON: %v", err)
	}
	if len(graph.Resources) != 3 {
		t.Fatalf("graph nodes = %d, want 3", len(graph.Resources))
	}
	cluster := graph.Resources[0] // nodes are stable and sorted by name
	if cluster.Name != "cluster" || len(cluster.DependsOn) != 2 || cluster.DependsOn[0] != "cp" || cluster.DependsOn[1] != "workers" {
		t.Fatalf("dependsOn edges were not preserved: %+v", cluster)
	}
	cp := graph.Resources[1]
	if cp.Name != "cp" || cp.DependsOn == nil {
		t.Fatalf("leaf resource dependsOn must be an empty JSON array: %+v", cp)
	}
}

func TestORDCatalogRejectsAmbiguousProviderGraphs(t *testing.T) {
	_, _, err := buildORDCatalog([]showroomv1alpha1.SimpleClusterProviderConfig{
		*ordSCPC("account-a", "provider", true, showroomv1alpha1.ResourceSpec{
			Name: "cluster", APIVersion: "one.eu/v1", Kind: "Cluster",
		}),
		*ordSCPC("account-b", "provider", true, showroomv1alpha1.ResourceSpec{
			Name: "cluster", APIVersion: "two.eu/v1", Kind: "Cluster",
		}),
	}, "0.35.0")
	if err == nil {
		t.Fatal("inconsistent provider graphs produced authoritative metadata")
	}
}

func TestORDCatalogDoesNotAdvertiseAnUnreachableReadyProvider(t *testing.T) {
	scpc := ordSCPC("account-a", "provider", true, showroomv1alpha1.ResourceSpec{
		Name: "cluster", APIVersion: "example.eu/v1", Kind: "Cluster",
	})
	scpc.Status.Reachable = boolPointer(false)
	available, reason := providerAvailable(scpc)
	if available || reason != "The provider target is not reachable." {
		t.Fatalf("unreachable provider availability = %t, %q", available, reason)
	}
}

func TestORDCatalogDoesNotPublishConditionMessages(t *testing.T) {
	scpc := ordSCPC("account-a", "provider", false, showroomv1alpha1.ResourceSpec{
		Name: "cluster", APIVersion: "example.eu/v1", Kind: "Cluster",
	})
	scpc.Status.Reachable = nil
	scpc.Status.Conditions[0].Message = "internal endpoint https://secret.example"
	available, reason := providerAvailable(scpc)
	if available || reason != "The provider configuration is not ready." {
		t.Fatalf("provider availability exposed internal status: %t, %q", available, reason)
	}
}

func TestGeneratedORDDocumentMatchesValidatedFixture(t *testing.T) {
	files, _, err := buildORDCatalog([]showroomv1alpha1.SimpleClusterProviderConfig{
		*ordSCPC("account-a", "cloudferro", true,
			showroomv1alpha1.ResourceSpec{
				Name: "cluster", APIVersion: "cloudferro.eu/v1alpha1", Kind: "OpenStackVM",
			},
		),
	}, "0.35.0")
	if err != nil {
		t.Fatalf("buildORDCatalog: %v", err)
	}
	assertJSONFixture(t, files[ORDDocumentKey], "testdata/ord-document.json")
	assertJSONFixture(t, files[ORDConfigurationKey], "testdata/ord-configuration.json")
}

func assertJSONFixture(t *testing.T, generated, fixturePath string) {
	t.Helper()
	wantBytes, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read validated fixture %s: %v", fixturePath, err)
	}
	var got, want any
	if err := json.Unmarshal([]byte(generated), &got); err != nil {
		t.Fatalf("generated value is not JSON: %v", err)
	}
	if err := json.Unmarshal(wantBytes, &want); err != nil {
		t.Fatalf("validated fixture is not JSON: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("generated JSON drifted from %s\ngot: %s", fixturePath, generated)
	}
}

func TestORDCatalogPublishesEmptyValidDocumentAndDoesNotRewrite(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := showroomv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	catalog := &ORDCatalog{
		Reader: c, Writer: c, TargetNamespace: "simple-cluster-system",
		TargetName: "simple-cluster-ord", SystemVersion: "0.35.0",
	}

	if err := catalog.Publish(context.Background()); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	key := client.ObjectKey{Namespace: catalog.TargetNamespace, Name: catalog.TargetName}
	var first corev1.ConfigMap
	if err := c.Get(context.Background(), key, &first); err != nil {
		t.Fatalf("reading aggregate: %v", err)
	}
	if first.Data[ORDConfigurationKey] == "" || first.Data[ORDDocumentKey] == "" {
		t.Fatalf("empty catalogue omitted discovery files: %v", first.Data)
	}

	if err := catalog.Publish(context.Background()); err != nil {
		t.Fatalf("second Publish: %v", err)
	}
	var second corev1.ConfigMap
	if err := c.Get(context.Background(), key, &second); err != nil {
		t.Fatalf("re-reading aggregate: %v", err)
	}
	if first.ResourceVersion != second.ResourceVersion {
		t.Fatal("unchanged ORD metadata rewrote the ConfigMap")
	}
}
