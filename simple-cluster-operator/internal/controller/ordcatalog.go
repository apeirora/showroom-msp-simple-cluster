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
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
)

const (
	ORDConfigurationKey = "configuration.json"
	ORDDocumentKey      = "simple-cluster.json"
	ordDefinitionPrefix = "provider-config-"
	ordDefinitionSuffix = ".json"
	ordMarkerLabel      = "showroom.apeirora.eu/ord-catalog"

	ordPackageID       = "apeirora.eu:package:simple-cluster:v1"
	ordProviderAPIID   = "apeirora.eu:apiResource:simple-cluster:v1"
	ordCapabilityType  = "apeirora.eu:simple-cluster-provider-config:v1"
	ordDefinitionType  = "apeirora.eu:simple-cluster-provider-config-definition:v1"
	ordProvisionedBy   = "apeirora.eu:provisioned-by"
	ordUsesResource    = "apeirora.eu:uses-resource"
	ordConfigurationV1 = "1.16"
)

// ORDCatalog reflects the installed SimpleClusterProviderConfigs as descriptive ORD
// metadata. The SCPC controller remains the only runtime orchestrator; this runnable
// only reads SCPCs and writes public metadata for the UI to serve.
type ORDCatalog struct {
	Reader          client.Reader
	Writer          client.Client
	TargetNamespace string
	TargetName      string
	SystemVersion   string
	Interval        time.Duration
}

func (a *ORDCatalog) NeedLeaderElection() bool { return true }

func (a *ORDCatalog) Start(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("ord-catalog")
	if err := a.Publish(ctx); err != nil {
		log.Error(err, "publishing the ORD catalogue failed")
	}

	ticker := time.NewTicker(a.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := a.Publish(ctx); err != nil {
				// Keep serving the last valid document. Discovery metadata must never
				// affect SimpleCluster reconciliation or provisioning.
				log.Error(err, "publishing the ORD catalogue failed")
			}
		}
	}
}

func (a *ORDCatalog) Publish(ctx context.Context) error {
	var list showroomv1alpha1.SimpleClusterProviderConfigList
	if err := a.Reader.List(ctx, &list); err != nil {
		return fmt.Errorf("listing SimpleClusterProviderConfigs: %w", err)
	}

	files, count, err := buildORDCatalog(list.Items, a.SystemVersion)
	if err != nil {
		return err
	}

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: a.TargetName, Namespace: a.TargetNamespace,
	}}
	result, err := controllerutil.CreateOrUpdate(ctx, a.Writer, cm, func() error {
		if cm.Labels == nil {
			cm.Labels = map[string]string{}
		}
		cm.Labels[ordMarkerLabel] = "aggregate"
		cm.Data = files
		return nil
	})
	if err != nil {
		return fmt.Errorf("writing %s/%s: %w", a.TargetNamespace, a.TargetName, err)
	}
	if result != controllerutil.OperationResultNone {
		logf.FromContext(ctx).WithName("ord-catalog").Info(
			"ORD catalogue updated", "providerConfigurations", count, "result", result)
	}
	return nil
}

type ordDocument struct {
	Schema                 string           `json:"$schema"`
	OpenResourceDiscovery  string           `json:"openResourceDiscovery"`
	Perspective            string           `json:"perspective"`
	DescribedSystemVersion ordSystemVersion `json:"describedSystemVersion"`
	Vendors                []ordVendor      `json:"vendors"`
	Products               []ordProduct     `json:"products"`
	Packages               []ordPackage     `json:"packages"`
	APIResources           []ordAPIResource `json:"apiResources"`
	Capabilities           []ordCapability  `json:"capabilities"`
}

type ordSystemVersion struct {
	Version string `json:"version"`
}

type ordVendor struct {
	ORDID string `json:"ordId"`
	Title string `json:"title"`
}

type ordProduct struct {
	ORDID            string `json:"ordId"`
	Title            string `json:"title"`
	ShortDescription string `json:"shortDescription"`
	Vendor           string `json:"vendor"`
}

type ordPackage struct {
	ORDID            string   `json:"ordId"`
	Title            string   `json:"title"`
	ShortDescription string   `json:"shortDescription"`
	Description      string   `json:"description"`
	Version          string   `json:"version"`
	Vendor           string   `json:"vendor"`
	PartOfProducts   []string `json:"partOfProducts"`
	Tags             []string `json:"tags"`
}

type ordAPIResource struct {
	ORDID                        string `json:"ordId"`
	Title                        string `json:"title"`
	ShortDescription             string `json:"shortDescription"`
	Description                  string `json:"description"`
	Version                      string `json:"version"`
	Visibility                   string `json:"visibility"`
	ReleaseStatus                string `json:"releaseStatus"`
	PartOfPackage                string `json:"partOfPackage"`
	APIProtocol                  string `json:"apiProtocol"`
	ImplementationStandard       string `json:"implementationStandard"`
	CustomImplementationStandard string `json:"customImplementationStandard"`
}

type ordCapability struct {
	ORDID               string                  `json:"ordId"`
	Type                string                  `json:"type"`
	Title               string                  `json:"title"`
	ShortDescription    string                  `json:"shortDescription"`
	Description         string                  `json:"description"`
	Version             string                  `json:"version"`
	Visibility          string                  `json:"visibility"`
	ReleaseStatus       string                  `json:"releaseStatus"`
	PartOfPackage       string                  `json:"partOfPackage"`
	Disabled            bool                    `json:"disabled"`
	RelatedAPIResources []ordRelatedAPIResource `json:"relatedApiResources"`
	Definitions         []ordDefinition         `json:"definitions"`
}

type ordRelatedAPIResource struct {
	ORDID        string `json:"ordId"`
	RelationType string `json:"relationType"`
}

type ordDefinition struct {
	Type             string              `json:"type"`
	MediaType        string              `json:"mediaType"`
	URL              string              `json:"url"`
	AccessStrategies []ordAccessStrategy `json:"accessStrategies"`
}

type ordAccessStrategy struct {
	Type string `json:"type"`
}

type providerGraph struct {
	SchemaVersion          string              `json:"schemaVersion"`
	Name                   string              `json:"name"`
	Available              bool                `json:"available"`
	AvailabilityReason     string              `json:"availabilityReason"`
	Resources              []providerGraphNode `json:"resources"`
	KubeconfigResourceName string              `json:"kubeconfigResourceName"`
}

type providerGraphNode struct {
	Name       string   `json:"name"`
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	DependsOn  []string `json:"dependsOn"`
}

type providerVariant struct {
	graph     providerGraph
	available bool
	reason    string
}

func buildORDCatalog(scpcs []showroomv1alpha1.SimpleClusterProviderConfig, systemVersion string) (map[string]string, int, error) {
	if systemVersion == "" {
		return nil, 0, fmt.Errorf("ORD system version must not be empty")
	}

	grouped := make(map[string][]showroomv1alpha1.SimpleClusterProviderConfig)
	for i := range scpcs {
		grouped[scpcs[i].Name] = append(grouped[scpcs[i].Name], scpcs[i])
	}

	names := make([]string, 0, len(grouped))
	for name := range grouped {
		names = append(names, name)
	}
	sort.Strings(names)

	files := map[string]string{}
	capabilities := make([]ordCapability, 0, len(names))
	contracts := make(map[string]providerGraphNode)
	for _, name := range names {
		variant, err := mergeProviderVariants(name, grouped[name])
		if err != nil {
			return nil, 0, err
		}
		filename := ordDefinitionPrefix + name + ordDefinitionSuffix
		definition, err := json.Marshal(variant.graph)
		if err != nil {
			return nil, 0, fmt.Errorf("rendering provider graph %q: %w", name, err)
		}
		files[filename] = string(definition)
		relatedResources := []ordRelatedAPIResource{{
			ORDID: ordProviderAPIID, RelationType: ordProvisionedBy,
		}}
		for _, resource := range variant.graph.Resources {
			id := providerContractID(resource.APIVersion, resource.Kind)
			contracts[id] = resource
			relatedResources = append(relatedResources, ordRelatedAPIResource{
				ORDID: id, RelationType: ordUsesResource,
			})
		}
		capabilities = append(capabilities, ordCapability{
			ORDID:               "apeirora.eu:capability:simple-cluster-" + name + ":v1",
			Type:                ordCapabilityType,
			Title:               displayName(name),
			ShortDescription:    "Simple Cluster provider configuration " + name,
			Description:         "Descriptive view of the resources and dependency edges declared by the " + name + " SimpleClusterProviderConfig. Simple Cluster, not ORD, owns provisioning and ordering.",
			Version:             "1.0.0",
			Visibility:          "public",
			ReleaseStatus:       "active",
			PartOfPackage:       ordPackageID,
			Disabled:            !variant.available,
			RelatedAPIResources: relatedResources,
			Definitions: []ordDefinition{{
				Type: ordDefinitionType, MediaType: "application/json",
				URL:              "/ord/" + filename,
				AccessStrategies: []ordAccessStrategy{{Type: "open"}},
			}},
		})
	}

	apiResources := []ordAPIResource{{
		ORDID: ordProviderAPIID, Title: "SimpleCluster API",
		ShortDescription: "Kubernetes API for requesting provider-backed clusters",
		Description:      "The Kubernetes Resource Model API used to create, inspect, and delete SimpleCluster resources.",
		Version:          "1.0.0", Visibility: "public", ReleaseStatus: "active",
		PartOfPackage: ordPackageID, APIProtocol: "rest",
		ImplementationStandard:       "custom",
		CustomImplementationStandard: "apeirora.eu:kubernetes-resource-model:v1",
	}}
	contractIDs := make([]string, 0, len(contracts))
	for id := range contracts {
		contractIDs = append(contractIDs, id)
	}
	sort.Strings(contractIDs)
	for _, id := range contractIDs {
		contract := contracts[id]
		apiResources = append(apiResources, ordAPIResource{
			ORDID: id, Title: contract.Kind + " API",
			ShortDescription: "Kubernetes API contract " + contract.APIVersion + " " + contract.Kind,
			Description:      "A provider-specific Kubernetes Resource Model contract referenced by a Simple Cluster provider configuration.",
			Version:          "1.0.0", Visibility: "public", ReleaseStatus: "active",
			PartOfPackage: ordPackageID, APIProtocol: "rest",
			ImplementationStandard:       "custom",
			CustomImplementationStandard: "apeirora.eu:kubernetes-resource-model:v1",
		})
	}

	document := ordDocument{
		Schema:                 "https://open-resource-discovery.org/spec-v1/interfaces/Document.schema.json",
		OpenResourceDiscovery:  ordConfigurationV1,
		Perspective:            "system-version",
		DescribedSystemVersion: ordSystemVersion{Version: systemVersion},
		Vendors:                []ordVendor{{ORDID: "apeirora:vendor:apeirora:", Title: "ApeiroRA"}},
		Products: []ordProduct{{
			ORDID: "apeirora.eu:product:simple-cluster:", Title: "Simple Cluster",
			ShortDescription: "Provider-independent Kubernetes cluster provisioning",
			Vendor:           "apeirora:vendor:apeirora:",
		}},
		Packages: []ordPackage{{
			ORDID: ordPackageID, Title: "Simple Cluster",
			ShortDescription: "Kubernetes clusters provisioned through selectable infrastructure providers",
			Description:      "Simple Cluster selects an enabled provider configuration and orchestrates its declared resource DAG.",
			Version:          "1.0.0", Vendor: "apeirora:vendor:apeirora:",
			PartOfProducts: []string{"apeirora.eu:product:simple-cluster:"},
			Tags:           []string{"kubernetes", "cluster", "infrastructure", "showroom"},
		}},
		APIResources: apiResources,
		Capabilities: capabilities,
	}
	documentJSON, err := json.Marshal(document)
	if err != nil {
		return nil, 0, fmt.Errorf("rendering ORD document: %w", err)
	}
	files[ORDDocumentKey] = string(documentJSON)

	configuration := map[string]any{
		"$schema": "https://open-resource-discovery.org/spec-v1/interfaces/Configuration.schema.json",
		"openResourceDiscoveryV1": map[string]any{"documents": []any{map[string]any{
			"url": "/ord/" + ORDDocumentKey, "perspective": "system-version",
			"accessStrategies": []any{map[string]any{"type": "open"}},
		}}},
	}
	configurationJSON, err := json.Marshal(configuration)
	if err != nil {
		return nil, 0, fmt.Errorf("rendering ORD configuration: %w", err)
	}
	files[ORDConfigurationKey] = string(configurationJSON)
	return files, len(names), nil
}

func mergeProviderVariants(name string, scpcs []showroomv1alpha1.SimpleClusterProviderConfig) (providerVariant, error) {
	var canonical []providerGraphNode
	available := false
	reasons := make([]string, 0, len(scpcs))
	for i := range scpcs {
		nodes := graphNodes(scpcs[i].Spec.Resources)
		if canonical == nil {
			canonical = nodes
		} else if !sameGraph(canonical, nodes) {
			return providerVariant{}, fmt.Errorf("provider configuration %q has inconsistent dependency graphs across namespaces", name)
		}
		ready, reason := providerAvailable(&scpcs[i])
		available = available || ready
		if reason != "" {
			reasons = append(reasons, reason)
		}
	}

	reason := "No ready provider configuration was observed."
	if available {
		reason = "At least one installed provider configuration is ready."
	} else if len(reasons) > 0 {
		sort.Strings(reasons)
		reason = reasons[0]
	}
	return providerVariant{
		available: available,
		reason:    reason,
		graph: providerGraph{
			SchemaVersion: "1.0",
			Name:          name, Available: available, AvailabilityReason: reason,
			Resources:              canonical,
			KubeconfigResourceName: scpcs[0].Spec.KubeconfigSource.ResourceName,
		},
	}, nil
}

func graphNodes(resources []showroomv1alpha1.ResourceSpec) []providerGraphNode {
	nodes := make([]providerGraphNode, 0, len(resources))
	for i := range resources {
		dependencies := append([]string{}, resources[i].DependsOn...)
		sort.Strings(dependencies)
		nodes = append(nodes, providerGraphNode{
			Name: resources[i].Name, APIVersion: resources[i].APIVersion,
			Kind: resources[i].Kind, DependsOn: dependencies,
		})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	return nodes
}

func sameGraph(a, b []providerGraphNode) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

func providerAvailable(scpc *showroomv1alpha1.SimpleClusterProviderConfig) (bool, string) {
	if scpc.Status.Reachable != nil && !*scpc.Status.Reachable {
		return false, "The provider target is not reachable."
	}
	condition := apimeta.FindStatusCondition(scpc.Status.Conditions, showroomv1alpha1.ConditionReady)
	if condition != nil {
		if condition.Status == metav1.ConditionTrue {
			return true, "The provider configuration is ready."
		}
		return false, "The provider configuration is not ready."
	}
	if scpc.Status.Reachable != nil {
		return *scpc.Status.Reachable, "Availability is based on the SCPC reachability probe."
	}
	return false, "The provider configuration has not reported readiness yet."
}

func displayName(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' })
	for i := range words {
		if words[i] != "" {
			words[i] = strings.ToUpper(words[i][:1]) + words[i][1:]
		}
	}
	return strings.Join(words, " ")
}

func providerContractID(apiVersion, kind string) string {
	name := strings.NewReplacer("/", ".", ":", ".").Replace(apiVersion) + "-" + kind
	return "apeirora.eu:apiResource:" + name + ":v1"
}
