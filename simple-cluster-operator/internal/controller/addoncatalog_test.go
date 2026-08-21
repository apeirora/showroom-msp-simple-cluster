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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func catalogSource(name, body string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "kube-system",
			Labels:    map[string]string{CatalogSourceLabel: "true"},
		},
		Data: map[string]string{catalogEntryKey: body},
	}
}

const cryptoBrokerEntry = `
id: crypto-broker
label: showroom.apeirora.eu/crypto-broker
displayName: Crypto Broker
description: Key management on the cluster.
bindName: crypto-broker
entitlement:
  group: crypto-broker.apeirora.eu
  version: v1alpha1
  listField: CryptoBrokers
  namespaced: true
targetKinds:
  - apiVersion: showroom.apeirora.eu/v1alpha1
    kind: SimpleCluster
`

func newCatalog(objects ...client.Object) (*AddonCatalog, client.Client) {
	c := fake.NewClientBuilder().WithObjects(objects...).Build()
	return &AddonCatalog{
		Reader:          c,
		Writer:          c,
		SourceNamespace: "kube-system",
		TargetNamespace: "simple-cluster-system",
		TargetName:      "simple-cluster-addons",
	}, c
}

func aggregated(t *testing.T, c client.Client) map[string]any {
	t.Helper()
	var cm corev1.ConfigMap
	key := client.ObjectKey{Namespace: "simple-cluster-system", Name: "simple-cluster-addons"}
	if err := c.Get(context.Background(), key, &cm); err != nil {
		t.Fatalf("reading the aggregate: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(cm.Data[CatalogOutputKey]), &out); err != nil {
		t.Fatalf("aggregate is not JSON: %v", err)
	}
	return out
}

func TestCatalogCarriesEverythingTheDialogNeeds(t *testing.T) {
	a, c := newCatalog(catalogSource("cluster-addon-crypto-broker", cryptoBrokerEntry))

	if err := a.Aggregate(context.Background()); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	addons, _ := aggregated(t, c)["addons"].([]any)
	if len(addons) != 1 {
		t.Fatalf("expected one entry, got %d", len(addons))
	}
	entry, _ := addons[0].(map[string]any)
	// The label is the whole point: everything else is presentation, but a checkbox
	// carrying the wrong label installs nothing at all.
	if entry["label"] != "showroom.apeirora.eu/crypto-broker" {
		t.Fatalf("label did not survive: %+v", entry)
	}
	if entry["bindName"] != "crypto-broker" {
		t.Fatalf("bindName did not survive: %+v", entry)
	}
	ent, _ := entry["entitlement"].(map[string]any)
	// Namespaced must survive as a boolean: a probe that drops it queries without a
	// namespace argument, which is a schema error that looks exactly like "not bound".
	if ent["namespaced"] != true || ent["listField"] != "CryptoBrokers" {
		t.Fatalf("entitlement did not survive: %+v", entry["entitlement"])
	}
	kinds, _ := entry["targetKinds"].([]any)
	if len(kinds) != 1 {
		t.Fatalf("targetKinds did not survive: %+v", entry)
	}
}

// One extension shipping a broken entry must not cost every other extension its
// checkbox -- they come from different charts and nobody owns all of them.
func TestCatalogSkipsMalformedEntries(t *testing.T) {
	a, c := newCatalog(
		catalogSource("cluster-addon-broken", "id: no-label\n"),
		catalogSource("cluster-addon-unparseable", "\tnot: yaml: at all\n"),
		catalogSource("cluster-addon-crypto-broker", cryptoBrokerEntry),
	)

	if err := a.Aggregate(context.Background()); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	addons, _ := aggregated(t, c)["addons"].([]any)
	if len(addons) != 1 {
		t.Fatalf("expected the one good entry to survive, got %d", len(addons))
	}
}

// An extension that says nothing about itself still gets a name to show.
func TestCatalogDefaultsDisplayNameToID(t *testing.T) {
	a, c := newCatalog(catalogSource("cluster-addon-minimal", "id: minimal\nlabel: x.eu/minimal\n"))

	if err := a.Aggregate(context.Background()); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	addons, _ := aggregated(t, c)["addons"].([]any)
	entry, _ := addons[0].(map[string]any)
	if entry["displayName"] != "minimal" {
		t.Fatalf("displayName was not defaulted: %+v", entry)
	}
}

// Nothing installed is a legitimate answer, and it must be an empty list rather than a
// missing file -- the UI has to be able to tell "no extensions" from "never ran".
func TestCatalogWritesAnEmptyListWhenNothingIsInstalled(t *testing.T) {
	a, c := newCatalog()

	if err := a.Aggregate(context.Background()); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	addons, ok := aggregated(t, c)["addons"].([]any)
	if !ok || len(addons) != 0 {
		t.Fatalf("expected an empty addons list, got %v", aggregated(t, c)["addons"])
	}
}

// Rebuilt on a timer, so an unchanged catalogue has to render byte-identical -- two
// entries in a different list order would otherwise rewrite the ConfigMap every tick
// and roll the UI's mounted file for no reason.
func TestCatalogRendersDeterministically(t *testing.T) {
	a, c := newCatalog(
		catalogSource("cluster-addon-zebra", "id: zebra\nlabel: x.eu/zebra\n"),
		catalogSource("cluster-addon-crypto-broker", cryptoBrokerEntry),
	)

	if err := a.Aggregate(context.Background()); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	var first corev1.ConfigMap
	key := client.ObjectKey{Namespace: "simple-cluster-system", Name: "simple-cluster-addons"}
	if err := c.Get(context.Background(), key, &first); err != nil {
		t.Fatalf("reading the aggregate: %v", err)
	}
	if !strings.HasPrefix(first.Data[CatalogOutputKey], `{"addons":[{"id":"crypto-broker"`) {
		t.Fatalf("entries are not sorted by id: %s", first.Data[CatalogOutputKey])
	}

	if err := a.Aggregate(context.Background()); err != nil {
		t.Fatalf("second Aggregate: %v", err)
	}
	var second corev1.ConfigMap
	if err := c.Get(context.Background(), key, &second); err != nil {
		t.Fatalf("re-reading the aggregate: %v", err)
	}
	if first.ResourceVersion != second.ResourceVersion {
		t.Fatal("an unchanged catalogue rewrote the ConfigMap")
	}
}
