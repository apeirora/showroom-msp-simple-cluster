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

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

const (
	// CatalogSourceLabel marks a ConfigMap as one extension's catalogue entry. Each
	// extension publishes its own from its own chart, so installing an extension is
	// all it takes to make it offerable.
	CatalogSourceLabel = "showroom.apeirora.eu/cluster-addon"

	// catalogEntryKey is the key inside a source ConfigMap holding the entry.
	catalogEntryKey = "addon.yaml"

	// CatalogOutputKey is the file the UI fetches, and the key under which the
	// aggregate is stored.
	CatalogOutputKey = "simple-cluster-addons.json"

	// catalogMarkerLabel is for whoever runs `kubectl get cm -l` and wonders where this
	// ConfigMap came from. Nothing reads it: entries are selected by CatalogSourceLabel
	// in another namespace, so the aggregate can never be mistaken for one of them.
	catalogMarkerLabel = "showroom.apeirora.eu/addon-catalog"
)

// CatalogEntry is what one extension says about itself: the label to set on a cluster
// object to switch it on, and enough to decide whether to offer it and how to name it.
//
// Deliberately mirrored rather than shared: the extension and this operator ship from
// different repositories, and a struct in a Go module neither imports is not a contract
// they can both hold. The contract is the YAML.
//
// Note what that costs: this struct is closed, so a key an extension invents is dropped
// on the way through rather than reaching the UI. Growing the contract means a field
// here and a release of this operator -- which is the price of the aggregate being a
// document with a known shape rather than whatever anybody wrote in kube-system.
type CatalogEntry struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
	// BindName is the APIBinding an account needs for this extension; Entitlement is a
	// Kind that only resolves for an account that has it. Both are passed through for
	// the UI to use whichever it can answer.
	BindName    string              `json:"bindName,omitempty"`
	Entitlement map[string]any      `json:"entitlement,omitempty"`
	TargetKinds []CatalogTargetKind `json:"targetKinds,omitempty"`
}

// CatalogTargetKind is a cluster kind that may carry the addon's label.
type CatalogTargetKind struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
}

// Cluster-wide because the entries live in kube-system, which is nobody's namespace in
// particular and every extension's publishing spot.
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch

// AddonCatalog collects the catalogue entries extensions publish and renders them into
// one document the create dialog fetches.
//
// It polls rather than watches on purpose. Watching would mean putting ConfigMaps into
// the manager cache, and this operator creates ConfigMaps as ordinary downstream
// resources -- a cache filtered to the catalogue label would quietly stop returning
// those, and an unfiltered one would hold every ConfigMap in the cluster. Neither price
// is worth paying to learn about a newly installed extension seconds rather than a
// minute sooner.
type AddonCatalog struct {
	// Reader is uncached for the reason above; the manager's APIReader.
	Reader client.Reader
	// Writer maintains the aggregate.
	Writer client.Client
	// SourceNamespace is where extensions publish their entries.
	SourceNamespace string
	// TargetNamespace and TargetName name the aggregate, which is mounted into the UI
	// pod as a file.
	TargetNamespace string
	TargetName      string
	// Interval is how often the catalogue is rebuilt.
	Interval time.Duration
}

// NeedLeaderElection keeps a multi-replica deployment from writing the aggregate twice.
func (a *AddonCatalog) NeedLeaderElection() bool { return true }

// Start runs until the context is cancelled, rebuilding the aggregate on every tick.
// A failed rebuild is logged and retried on the next one: the aggregate that is already
// published stays valid, and taking the operator down over a malformed ConfigMap
// somebody else installed would be the wrong trade.
func (a *AddonCatalog) Start(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("addon-catalog")

	if err := a.Aggregate(ctx); err != nil {
		log.Error(err, "building the addon catalogue failed")
	}

	ticker := time.NewTicker(a.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := a.Aggregate(ctx); err != nil {
				log.Error(err, "building the addon catalogue failed")
			}
		}
	}
}

// Aggregate reads every published entry and writes the aggregate once.
func (a *AddonCatalog) Aggregate(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("addon-catalog")

	var sources corev1.ConfigMapList
	if err := a.Reader.List(ctx, &sources,
		client.InNamespace(a.SourceNamespace),
		client.MatchingLabels{CatalogSourceLabel: "true"},
	); err != nil {
		return fmt.Errorf("listing catalogue entries in %s: %w", a.SourceNamespace, err)
	}

	entries := make([]CatalogEntry, 0, len(sources.Items))
	for i := range sources.Items {
		cm := &sources.Items[i]
		entry, err := parseCatalogEntry(cm)
		if err != nil {
			// One broken extension must not cost the others their checkbox.
			log.Error(err, "ignoring a malformed catalogue entry", "configMap", cm.Name)
			continue
		}
		entries = append(entries, *entry)
	}

	// Sorted so an unchanged catalogue renders byte-identical and the aggregate is not
	// rewritten on every tick.
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })

	rendered, err := json.Marshal(map[string]any{"addons": entries})
	if err != nil {
		return fmt.Errorf("rendering the catalogue: %w", err)
	}

	aggregate := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: a.TargetName, Namespace: a.TargetNamespace},
	}
	result, err := controllerutil.CreateOrUpdate(ctx, a.Writer, aggregate, func() error {
		if aggregate.Labels == nil {
			aggregate.Labels = map[string]string{}
		}
		aggregate.Labels[catalogMarkerLabel] = "aggregate"
		aggregate.Data = map[string]string{CatalogOutputKey: string(rendered)}
		return nil
	})
	if err != nil {
		return fmt.Errorf("writing %s/%s: %w", a.TargetNamespace, a.TargetName, err)
	}

	if result != controllerutil.OperationResultNone {
		log.Info("addon catalogue updated", "entries", len(entries), "result", result)
	}
	// An empty catalogue renders exactly like an account with no extensions, so say it
	// out loud: it is far more often missing RBAC or a marker-label typo than the truth.
	if len(entries) == 0 {
		log.Info("addon catalogue is empty; no create-time extensions will be offered",
			"namespace", a.SourceNamespace, "label", CatalogSourceLabel)
	}
	return nil
}

// parseCatalogEntry reads one entry, rejecting the two fields without which a checkbox
// would do nothing: what to call it and, above all, which label to set.
func parseCatalogEntry(cm *corev1.ConfigMap) (*CatalogEntry, error) {
	raw, ok := cm.Data[catalogEntryKey]
	if !ok {
		return nil, fmt.Errorf("ConfigMap %s has no %q key", cm.Name, catalogEntryKey)
	}

	var entry CatalogEntry
	if err := yaml.Unmarshal([]byte(raw), &entry); err != nil {
		return nil, fmt.Errorf("parsing %s from %s: %w", catalogEntryKey, cm.Name, err)
	}
	if entry.ID == "" {
		return nil, fmt.Errorf("entry in %s has no id", cm.Name)
	}
	if entry.Label == "" {
		return nil, fmt.Errorf("entry %s in %s sets no label", entry.ID, cm.Name)
	}
	if entry.DisplayName == "" {
		entry.DisplayName = entry.ID
	}
	return &entry, nil
}
