/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package openstack

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/flavors"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/keypairs"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/v2/pagination"

	openstackv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-openstack/api/v1alpha1"
)

// Metadata keys the operator stamps on every server it owns.
const (
	MetaVMSetKey = "osvms.apeirora.eu/vmset"
	MetaGroupKey = "osvms.apeirora.eu/group"
)

// Server is the slice of Nova state the controller needs.
type Server struct {
	ID         string
	Name       string
	Group      string
	Status     string
	InternalIP string
	FloatingIP string
	PortID     string
}

// VMSetKey formats the metadata tag value used to claim ownership.
func VMSetKey(namespace, name string) string {
	return namespace + "/" + name
}

// ListByVMSet returns every server tagged with the given VMSet key.
func ListByVMSet(ctx context.Context, compute *gophercloud.ServiceClient, vmsetKey string) ([]Server, error) {
	pages, err := servers.List(compute, servers.ListOpts{}).AllPages(ctx)
	if err != nil {
		return nil, fmt.Errorf("list servers: %w", err)
	}
	all, err := servers.ExtractServers(pages)
	if err != nil {
		return nil, fmt.Errorf("extract servers: %w", err)
	}
	out := make([]Server, 0, len(all))
	for _, s := range all {
		if s.Metadata[MetaVMSetKey] != vmsetKey {
			continue
		}
		out = append(out, Server{
			ID:         s.ID,
			Name:       s.Name,
			Group:      s.Metadata[MetaGroupKey],
			Status:     s.Status,
			InternalIP: firstFixedIP(s),
		})
	}
	return out, nil
}

// CreateServer launches one Nova server tagged with the VMSet.
func CreateServer(
	ctx context.Context,
	clients *Clients,
	vmsetKey string,
	group openstackv1alpha1.VMGroup,
	name, networkID, keypairName string,
	securityGroups []string,
) (string, error) {
	flavorID, err := resolveFlavor(ctx, clients.Compute, group.Flavor)
	if err != nil {
		return "", err
	}
	imageID, err := ResolveImage(ctx, clients.Image, group.Image)
	if err != nil {
		return "", err
	}

	base := servers.CreateOpts{
		Name:           name,
		FlavorRef:      flavorID,
		ImageRef:       imageID,
		Networks:       []servers.Network{{UUID: networkID}},
		SecurityGroups: securityGroups,
		Metadata: map[string]string{
			MetaVMSetKey: vmsetKey,
			MetaGroupKey: group.Name,
		},
	}
	if group.UserData != "" {
		base.UserData = []byte(base64.StdEncoding.EncodeToString([]byte(group.UserData)))
	}

	opts := keypairs.CreateOptsExt{
		CreateOptsBuilder: base,
		KeyName:           keypairName,
	}
	created, err := servers.Create(ctx, clients.Compute, opts, nil).Extract()
	if err != nil {
		return "", fmt.Errorf("create server %q: %w", name, err)
	}
	return created.ID, nil
}

// DeleteServer removes a Nova server. 404 is treated as success.
func DeleteServer(ctx context.Context, compute *gophercloud.ServiceClient, id string) error {
	err := servers.Delete(ctx, compute, id).ExtractErr()
	if err == nil || gophercloud.ResponseCodeIs(err, 404) {
		return nil
	}
	return fmt.Errorf("delete server %s: %w", id, err)
}

// ResolveNetwork returns the Neutron network UUID for the spec'd name (or
// passes the UUID through unchanged).
func ResolveNetwork(ctx context.Context, network *gophercloud.ServiceClient, nameOrID string) (string, error) {
	if looksLikeUUID(nameOrID) {
		return nameOrID, nil
	}
	pages, err := networks.List(network, networks.ListOpts{Name: nameOrID}).AllPages(ctx)
	if err != nil {
		return "", fmt.Errorf("list networks: %w", err)
	}
	all, err := networks.ExtractNetworks(pages)
	if err != nil {
		return "", fmt.Errorf("extract networks: %w", err)
	}
	for _, n := range all {
		if n.Name == nameOrID {
			return n.ID, nil
		}
	}
	return "", fmt.Errorf("network %q not found", nameOrID)
}

// ResolveImage resolves a Glance image name to UUID. Pass-through when
// the value already looks like a UUID.
func ResolveImage(ctx context.Context, image *gophercloud.ServiceClient, nameOrID string) (string, error) {
	if looksLikeUUID(nameOrID) {
		return nameOrID, nil
	}
	pages, err := images.List(image, images.ListOpts{Name: nameOrID}).AllPages(ctx)
	if err != nil {
		return "", fmt.Errorf("list images: %w", err)
	}
	list, err := images.ExtractImages(pages)
	if err != nil {
		return "", fmt.Errorf("extract images: %w", err)
	}
	for _, img := range list {
		if img.Name == nameOrID {
			return img.ID, nil
		}
	}
	return "", fmt.Errorf("image %q not found", nameOrID)
}

// EnsureFloatingIP allocates and attaches a FIP from the named external
// network to the server's port if it does not yet have one. Returns the
// floating address.
func EnsureFloatingIP(
	ctx context.Context,
	clients *Clients,
	server *Server,
	externalNet string,
	vmsetKey string,
) (string, error) {
	if server.FloatingIP != "" {
		return server.FloatingIP, nil
	}
	portID, err := portForServer(ctx, clients.Network, server.ID)
	if err != nil {
		return "", err
	}
	server.PortID = portID

	if existing := findFIPByPort(ctx, clients.Network, portID); existing != "" {
		return existing, nil
	}

	externalID, err := ResolveNetwork(ctx, clients.Network, externalNet)
	if err != nil {
		return "", fmt.Errorf("resolve floatingIPPool: %w", err)
	}
	fip, err := floatingips.Create(ctx, clients.Network, floatingips.CreateOpts{
		FloatingNetworkID: externalID,
		PortID:            portID,
		Description:       "vmset=" + vmsetKey,
	}).Extract()
	if err != nil {
		return "", fmt.Errorf("create floating IP for %s: %w", server.Name, err)
	}
	return fip.FloatingIP, nil
}

// ReleaseFloatingIPsForServer disassociates and deletes every FIP
// pointing at the server's port. No-op when the port is already gone.
func ReleaseFloatingIPsForServer(ctx context.Context, clients *Clients, server *Server) error {
	portID := server.PortID
	if portID == "" {
		var err error
		portID, err = portForServer(ctx, clients.Network, server.ID)
		if err != nil {
			return nil
		}
	}
	pages, err := floatingips.List(clients.Network, floatingips.ListOpts{PortID: portID}).AllPages(ctx)
	if err != nil {
		return fmt.Errorf("list floating IPs for port %s: %w", portID, err)
	}
	all, err := floatingips.ExtractFloatingIPs(pages)
	if err != nil {
		return fmt.Errorf("extract floating IPs: %w", err)
	}
	for _, fip := range all {
		if err := floatingips.Delete(ctx, clients.Network, fip.ID).ExtractErr(); err != nil {
			if !gophercloud.ResponseCodeIs(err, 404) {
				return fmt.Errorf("delete floating IP %s: %w", fip.ID, err)
			}
		}
	}
	return nil
}

// PortForServer returns the Neutron port ID of the server's first
// fixed-IP attachment. Empty string when the server has no port yet.
func PortForServer(ctx context.Context, network *gophercloud.ServiceClient, serverID string) string {
	id, _ := portForServer(ctx, network, serverID)
	return id
}

// FloatingIPForPort returns the floating address attached to the port,
// or empty string if none.
func FloatingIPForPort(ctx context.Context, network *gophercloud.ServiceClient, portID string) string {
	return findFIPByPort(ctx, network, portID)
}

// SanitizeServerName trims unsafe characters from a candidate Nova name.
func SanitizeServerName(in string) string {
	in = strings.ToLower(in)
	in = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, in)
	if len(in) > 63 {
		in = in[:63]
	}
	return strings.Trim(in, "-")
}

// ---- helpers -----------------------------------------------------------

func firstFixedIP(s servers.Server) string {
	for _, addrs := range s.Addresses {
		list, ok := addrs.([]any)
		if !ok {
			continue
		}
		for _, raw := range list {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := m["OS-EXT-IPS:type"].(string); t != "" && t != "fixed" {
				continue
			}
			if addr, _ := m["addr"].(string); addr != "" {
				return addr
			}
		}
	}
	return ""
}

func portForServer(ctx context.Context, network *gophercloud.ServiceClient, serverID string) (string, error) {
	pages, err := ports.List(network, ports.ListOpts{DeviceID: serverID}).AllPages(ctx)
	if err != nil {
		return "", fmt.Errorf("list ports for server %s: %w", serverID, err)
	}
	all, err := ports.ExtractPorts(pages)
	if err != nil {
		return "", fmt.Errorf("extract ports: %w", err)
	}
	if len(all) == 0 {
		return "", fmt.Errorf("no port for server %s yet", serverID)
	}
	return all[0].ID, nil
}

func findFIPByPort(ctx context.Context, network *gophercloud.ServiceClient, portID string) string {
	pages, err := floatingips.List(network, floatingips.ListOpts{PortID: portID}).AllPages(ctx)
	if err != nil {
		return ""
	}
	all, err := floatingips.ExtractFloatingIPs(pages)
	if err != nil || len(all) == 0 {
		return ""
	}
	return all[0].FloatingIP
}

func resolveFlavor(ctx context.Context, compute *gophercloud.ServiceClient, name string) (string, error) {
	if looksLikeUUID(name) {
		return name, nil
	}
	var found string
	err := flavors.ListDetail(compute, nil).EachPage(ctx, func(_ context.Context, page pagination.Page) (bool, error) {
		list, err := flavors.ExtractFlavors(page)
		if err != nil {
			return false, err
		}
		for _, f := range list {
			if f.Name == name {
				found = f.ID
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		return "", fmt.Errorf("list flavors: %w", err)
	}
	if found == "" {
		return "", fmt.Errorf("flavor %q not found", name)
	}
	return found, nil
}

func looksLikeUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}
