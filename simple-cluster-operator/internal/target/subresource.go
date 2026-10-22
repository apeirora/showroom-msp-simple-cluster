package target

import (
	"context"
	"fmt"
	"slices"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/discovery"
)

// ResolveAccountSubresource requires parent create permission and exact account discovery.
func ResolveAccountSubresource(ctx context.Context, tc *Client, parent schema.GroupVersionKind, subresource string) (*AccountAuthorization, error) {
	if tc.RESTConfig == nil || tc.Dynamic == nil || tc.Mapper == nil {
		return nil, fmt.Errorf("account credential authorization requires discovery and binding clients")
	}
	if len(validation.IsDNS1123Label(subresource)) != 0 {
		return nil, fmt.Errorf("invalid account credential subresource name")
	}
	// KCP v1alpha2 types_apiexport.go:315-331 forbids '/' in claim resources.
	// Its authorizer binding.go:126-133 matches the parent GetResource(), not GetSubresource().
	// A subresource POST therefore requires the parent resource's create claim.
	authorization, err := ResolveAccountResource(ctx, tc.Dynamic, tc.Mapper, tc.ProviderIdentity, parent, "create")
	if err != nil {
		return nil, err
	}
	dc, err := discovery.NewDiscoveryClientForConfig(tc.RESTConfig)
	if err != nil {
		return nil, err
	}
	resources, err := dc.ServerResourcesForGroupVersion(parent.GroupVersion().String())
	if err != nil {
		return nil, fmt.Errorf("discover account credential subresource: %w", err)
	}
	for _, resource := range resources.APIResources {
		if resource.Name == authorization.Mapping.Resource.Resource+"/"+subresource && slices.Contains(resource.Verbs, "create") &&
			resource.Namespaced == (authorization.Mapping.Scope.Name() == apimeta.RESTScopeNameNamespace) {
			return authorization, nil
		}
	}
	return nil, fmt.Errorf("account discovery does not advertise credential subresource %s/%s with create", authorization.Mapping.Resource.Resource, subresource)
}
