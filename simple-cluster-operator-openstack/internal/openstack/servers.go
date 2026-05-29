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
	"regexp"
	"sort"
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
	"k8s.io/apimachinery/pkg/api/resource"
)

// MetaVMKey is the Nova metadata key the operator stamps on every server
// it owns. Nova's metadata-key regex is `^[a-zA-Z0-9-_:. ]{1,255}$` — no
// slashes — so we can't reuse the k8s-label form `osvm.apeirora.eu/vm`
// here. Value is `<namespace>/<vm-cr-name>` formatted by VMKey.
const MetaVMKey = "osvm.apeirora.eu.vm"

// Server is the slice of Nova state the controller needs.
type Server struct {
	ID         string
	Name       string
	Status     string
	InternalIP string
	FloatingIP string
	PortID     string
}

// VMKey formats the metadata tag value used to claim ownership.
func VMKey(namespace, name string) string {
	return namespace + "/" + name
}

// FindByVM returns the server tagged with this VM's key, if one exists.
// Returns (nil, nil) when no server has been created yet.
func FindByVM(ctx context.Context, compute *gophercloud.ServiceClient, vmKey string) (*Server, error) {
	pages, err := servers.List(compute, servers.ListOpts{}).AllPages(ctx)
	if err != nil {
		return nil, fmt.Errorf("list servers: %w", err)
	}
	all, err := servers.ExtractServers(pages)
	if err != nil {
		return nil, fmt.Errorf("extract servers: %w", err)
	}
	for _, s := range all {
		if s.Metadata[MetaVMKey] != vmKey {
			continue
		}
		return &Server{
			ID:         s.ID,
			Name:       s.Name,
			Status:     s.Status,
			InternalIP: firstFixedIP(s),
		}, nil
	}
	return nil, nil
}

// CreateServer launches one Nova server tagged with the VM. The caller
// is responsible for resolving the flavor (via PickFlavor) and the
// image (via ResolveImage) — that keeps both outcomes visible to the
// reconciler so it can mirror them into status.
func CreateServer(
	ctx context.Context,
	clients *Clients,
	vmKey string,
	name, networkID, keypairName, flavorID, imageID string,
	securityGroups []string,
	userData string,
) (string, error) {
	base := servers.CreateOpts{
		Name:           name,
		FlavorRef:      flavorID,
		ImageRef:       imageID,
		Networks:       []servers.Network{{UUID: networkID}},
		SecurityGroups: securityGroups,
		Metadata: map[string]string{
			MetaVMKey: vmKey,
		},
	}
	if userData != "" {
		base.UserData = []byte(base64.StdEncoding.EncodeToString([]byte(userData)))
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
// passes the UUID through unchanged). When the name does not resolve, the
// error message includes the names of every visible network so it can be
// surfaced verbatim in the CR's Condition.
func ResolveNetwork(ctx context.Context, network *gophercloud.ServiceClient, nameOrID string) (string, error) {
	if looksLikeUUID(nameOrID) {
		return nameOrID, nil
	}
	pages, err := networks.List(network, networks.ListOpts{}).AllPages(ctx)
	if err != nil {
		return "", fmt.Errorf("list networks: %w", err)
	}
	all, err := networks.ExtractNetworks(pages)
	if err != nil {
		return "", fmt.Errorf("extract networks: %w", err)
	}
	names := make([]string, 0, len(all))
	for _, n := range all {
		if n.Name == nameOrID {
			return n.ID, nil
		}
		names = append(names, n.Name)
	}
	return "", notFoundError("network", nameOrID, names)
}

// ResolveImage resolves a Glance image name to UUID. Pass-through when
// the value already looks like a UUID. Like ResolveNetwork, a miss
// returns an error listing every visible image so the user can pick.
func ResolveImage(ctx context.Context, image *gophercloud.ServiceClient, nameOrID string) (string, error) {
	if looksLikeUUID(nameOrID) {
		return nameOrID, nil
	}
	pages, err := images.List(image, images.ListOpts{}).AllPages(ctx)
	if err != nil {
		return "", fmt.Errorf("list images: %w", err)
	}
	list, err := images.ExtractImages(pages)
	if err != nil {
		return "", fmt.Errorf("extract images: %w", err)
	}
	names := make([]string, 0, len(list))
	for _, img := range list {
		if img.Name == nameOrID {
			return img.ID, nil
		}
		names = append(names, img.Name)
	}
	return "", notFoundError("image", nameOrID, names)
}

// EnsureFloatingIP allocates and attaches a FIP from the named external
// network to the server's port if it does not yet have one. Returns the
// floating address.
func EnsureFloatingIP(
	ctx context.Context,
	clients *Clients,
	server *Server,
	externalNet string,
	vmKey string,
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
		Description:       "vm=" + vmKey,
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

// FlavorPick is the outcome of selecting a flavor for a VMGroup.
type FlavorPick struct {
	ID          string
	Name        string
	Description string
	VCPUs       int
	RAMMiB      int
	DiskGiB     int
	// MemoryHuman is RAM rendered as the most readable unit, e.g.
	// "4 GiB" or "512 MiB". Same value also appears inside Description.
	MemoryHuman string
}

// PickFlavor returns the smallest visible Nova flavor whose VCPUs and RAM
// meet the requested floor. Sort key: VCPUs, RAM, Disk, Name — so the
// pick is deterministic across reconciles and chooses the cheapest
// candidate. Memory floor is rounded up to MiB; CPU floor (which may be
// expressed as millicores) is rounded up to whole vCPUs.
//
// If namePattern is non-empty, only flavor names matching the Go regex
// are considered. Use it to exclude specialised flavors that would
// otherwise win on the size comparison (e.g. CloudFerro's `vm.l40s.*`
// GPU flavors).
func PickFlavor(
	ctx context.Context,
	compute *gophercloud.ServiceClient,
	cpu, memory resource.Quantity,
	namePattern string,
) (*FlavorPick, error) {
	cpuFloor := ceilCPU(cpu)
	memFloorMiB := ceilMiB(memory)

	var nameRE *regexp.Regexp
	if namePattern != "" {
		var err error
		nameRE, err = regexp.Compile(namePattern)
		if err != nil {
			return nil, fmt.Errorf("invalid flavorPattern %q: %w", namePattern, err)
		}
	}

	type candidate struct {
		id, name               string
		vcpus, ramMiB, diskGiB int
	}
	var (
		all      []candidate
		matching []candidate
	)
	err := flavors.ListDetail(compute, nil).EachPage(ctx, func(_ context.Context, page pagination.Page) (bool, error) {
		list, err := flavors.ExtractFlavors(page)
		if err != nil {
			return false, err
		}
		for _, f := range list {
			c := candidate{id: f.ID, name: f.Name, vcpus: f.VCPUs, ramMiB: f.RAM, diskGiB: f.Disk}
			all = append(all, c)
			if nameRE != nil && !nameRE.MatchString(c.name) {
				continue
			}
			if c.vcpus >= cpuFloor && c.ramMiB >= memFloorMiB {
				matching = append(matching, c)
			}
		}
		return true, nil
	})
	if err != nil {
		return nil, fmt.Errorf("list flavors: %w", err)
	}
	if len(matching) == 0 {
		names := make([]string, 0, len(all))
		for _, c := range all {
			names = append(names, fmt.Sprintf("%s (%d vCPU / %d MiB / %d GiB)", c.name, c.vcpus, c.ramMiB, c.diskGiB))
		}
		sort.Strings(names)
		patternHint := ""
		if namePattern != "" {
			patternHint = fmt.Sprintf(", matching /%s/", namePattern)
		}
		return nil, fmt.Errorf("no flavor satisfies cpu>=%d, memory>=%dMiB%s; available: %s",
			cpuFloor, memFloorMiB, patternHint, strings.Join(names, ", "))
	}
	sort.Slice(matching, func(i, j int) bool {
		a, b := matching[i], matching[j]
		switch {
		case a.vcpus != b.vcpus:
			return a.vcpus < b.vcpus
		case a.ramMiB != b.ramMiB:
			return a.ramMiB < b.ramMiB
		case a.diskGiB != b.diskGiB:
			return a.diskGiB < b.diskGiB
		default:
			return a.name < b.name
		}
	})
	pick := matching[0]
	memHuman := formatMiB(pick.ramMiB)
	return &FlavorPick{
		ID:          pick.id,
		Name:        pick.name,
		VCPUs:       pick.vcpus,
		RAMMiB:      pick.ramMiB,
		DiskGiB:     pick.diskGiB,
		MemoryHuman: memHuman,
		Description: fmt.Sprintf("%d vCPU / %s RAM / %d GiB disk", pick.vcpus, memHuman, pick.diskGiB),
	}, nil
}

// ceilCPU rounds a CPU quantity up to whole vCPUs (Nova flavors are
// integer cores).
func ceilCPU(q resource.Quantity) int {
	m := q.MilliValue()
	if m <= 0 {
		return 0
	}
	return int((m + 999) / 1000)
}

// ceilMiB rounds a memory quantity up to whole MiB.
func ceilMiB(q resource.Quantity) int {
	b := q.Value()
	if b <= 0 {
		return 0
	}
	const mib = 1024 * 1024
	return int((b + mib - 1) / mib)
}

// formatMiB renders an MiB count as the most readable unit ("4 GiB",
// "512 MiB"). Nova flavor RAM is always a whole number of MiB.
func formatMiB(mib int) string {
	if mib >= 1024 && mib%1024 == 0 {
		return fmt.Sprintf("%d GiB", mib/1024)
	}
	if mib >= 1024 {
		return fmt.Sprintf("%.1f GiB", float64(mib)/1024)
	}
	return fmt.Sprintf("%d MiB", mib)
}

// notFoundError builds a Condition-friendly error: "<kind> %q not found;
// available: a, b, c". Names are de-duplicated and sorted so two
// reconciles produce identical messages (cleaner status diffs).
func notFoundError(kind, requested string, names []string) error {
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, n := range names {
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return fmt.Errorf("%s %q not found (no %ss visible to this credential)", kind, requested, kind)
	}
	return fmt.Errorf("%s %q not found; available: %s", kind, requested, strings.Join(out, ", "))
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
