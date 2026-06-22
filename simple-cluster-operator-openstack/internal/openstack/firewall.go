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
	"fmt"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/rules"
)

// httpIngressPorts are the inbound TCP ports the operator guarantees on every
// referenced security group so cluster workloads (ingress controllers, the
// dashboard, ...)
var httpIngressPorts = []int{80, 443}

// EnsureIngressPorts makes sure each named security group has an ingress TCP
// rule covering every port in httpIngressPorts, from any source. Idempotent:
// a port already covered by an existing rule is left untouched, and a
// concurrent create that loses the race (409) is treated as success. Groups
// are resolved by name (the CloudFerro/OpenStack convention, e.g. "default")
// or passed through when already a UUID.
func EnsureIngressPorts(ctx context.Context, network *gophercloud.ServiceClient, sgNames []string) error {
	for _, name := range sgNames {
		id, err := securityGroupID(ctx, network, name)
		if err != nil {
			return err
		}
		covered, err := ingressTCPCoverage(ctx, network, id)
		if err != nil {
			return err
		}
		for _, port := range httpIngressPorts {
			if covered[port] {
				continue
			}
			if err := createIngressTCPRule(ctx, network, id, port); err != nil {
				return err
			}
		}
	}
	return nil
}

// securityGroupID resolves a security-group name to its UUID (pass-through
// when already a UUID). A miss lists every visible group so the cause is
// obvious in the CR's Condition.
func securityGroupID(ctx context.Context, network *gophercloud.ServiceClient, nameOrID string) (string, error) {
	if looksLikeUUID(nameOrID) {
		return nameOrID, nil
	}
	pages, err := groups.List(network, groups.ListOpts{Name: nameOrID}).AllPages(ctx)
	if err != nil {
		return "", fmt.Errorf("list security groups: %w", err)
	}
	all, err := groups.ExtractGroups(pages)
	if err != nil {
		return "", fmt.Errorf("extract security groups: %w", err)
	}
	names := make([]string, 0, len(all))
	for _, g := range all {
		if g.Name == nameOrID {
			return g.ID, nil
		}
		names = append(names, g.Name)
	}
	return "", notFoundError("security group", nameOrID, names)
}

// ingressTCPCoverage returns, for each port in httpIngressPorts, whether the
// group already has an ingress TCP rule whose range covers it.
func ingressTCPCoverage(ctx context.Context, network *gophercloud.ServiceClient, sgID string) (map[int]bool, error) {
	pages, err := rules.List(network, rules.ListOpts{
		SecGroupID: sgID,
		Direction:  string(rules.DirIngress),
		Protocol:   string(rules.ProtocolTCP),
	}).AllPages(ctx)
	if err != nil {
		return nil, fmt.Errorf("list security group rules: %w", err)
	}
	list, err := rules.ExtractRules(pages)
	if err != nil {
		return nil, fmt.Errorf("extract security group rules: %w", err)
	}
	covered := make(map[int]bool, len(httpIngressPorts))
	for _, port := range httpIngressPorts {
		for _, ru := range list {
			if portCovered(ru, port) {
				covered[port] = true
				break
			}
		}
	}
	return covered, nil
}

// portCovered reports whether a rule admits the port. A rule with no range
// (0/0) opens all ports for its protocol.
func portCovered(ru rules.SecGroupRule, port int) bool {
	if ru.PortRangeMin == 0 && ru.PortRangeMax == 0 {
		return true
	}
	return ru.PortRangeMin <= port && port <= ru.PortRangeMax
}

// createIngressTCPRule opens a single inbound TCP port from any IPv4 source.
// A 409 means another reconcile (or operator) added an equivalent rule first
// — that is the desired end state, so it is not an error.
func createIngressTCPRule(ctx context.Context, network *gophercloud.ServiceClient, sgID string, port int) error {
	_, err := rules.Create(ctx, network, rules.CreateOpts{
		Direction:      rules.DirIngress,
		EtherType:      rules.EtherType4,
		Protocol:       rules.ProtocolTCP,
		PortRangeMin:   port,
		PortRangeMax:   port,
		RemoteIPPrefix: "0.0.0.0/0",
		SecGroupID:     sgID,
		Description:    "simple-cluster: workload ingress",
	}).Extract()
	if err != nil && !gophercloud.ResponseCodeIs(err, 409) {
		return fmt.Errorf("create ingress tcp/%d rule on %s: %w", port, sgID, err)
	}
	return nil
}
