/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package template

import (
	"errors"

	"k8s.io/apimachinery/pkg/api/resource"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
)

// ErrNoProfile is returned when a from=profile input has no SC override and no
// SCPC profile is large enough for the SC's requested per-node resources. The
// controller maps it to Phase=NoHardware.
var ErrNoProfile = errors.New("no profile large enough for requested resources")

// requestedCapacity is the per-node cpu/memory a profile must satisfy: the
// larger of the worker and (when present) control-plane pool asks.
func requestedCapacity(sc *showroomv1alpha1.SimpleCluster) (cpu, mem resource.Quantity) {
	cpu = sc.Spec.Workers.Requested.CPU
	mem = sc.Spec.Workers.Requested.Memory
	if cp := sc.Spec.ControlPlane; cp != nil {
		if cp.Requested.CPU.Cmp(cpu) > 0 {
			cpu = cp.Requested.CPU
		}
		if cp.Requested.Memory.Cmp(mem) > 0 {
			mem = cp.Requested.Memory
		}
	}
	return cpu, mem
}

// SelectProfile returns the name of the smallest SCPC profile whose cpu and
// memory both satisfy the SC's requested per-node resources. "Smallest" orders
// by cpu, then memory. It returns ErrNoProfile when no profile is large enough.
func SelectProfile(
	scpc *showroomv1alpha1.SimpleClusterProviderConfig,
	sc *showroomv1alpha1.SimpleCluster,
) (string, error) {
	reqCPU, reqMem := requestedCapacity(sc)
	var chosen *showroomv1alpha1.Profile
	for i := range scpc.Spec.Profiles {
		p := &scpc.Spec.Profiles[i]
		if p.CPU.Cmp(reqCPU) < 0 || p.Memory.Cmp(reqMem) < 0 {
			continue
		}
		if chosen == nil || smallerProfile(p, chosen) {
			chosen = p
		}
	}
	if chosen == nil {
		return "", ErrNoProfile
	}
	return chosen.Name, nil
}

// smallerProfile orders profiles by cpu, then memory.
func smallerProfile(a, b *showroomv1alpha1.Profile) bool {
	if c := a.CPU.Cmp(b.CPU); c != 0 {
		return c < 0
	}
	return a.Memory.Cmp(b.Memory) < 0
}
