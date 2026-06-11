/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package ironcore

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	computev1alpha1 "github.com/ironcore-dev/ironcore/api/compute/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ClassPick is the result of selecting a MachineClass that satisfies the
// requested CPU/memory floor.
type ClassPick struct {
	Name        string
	CPU         int32
	MemoryHuman string
	Description string
}

// PickMachineClass lists all cluster-scoped MachineClasses and returns the
// smallest one whose cpu and memory capabilities are >= the requested floor.
// namePattern, when non-empty, further restricts the candidates to classes
// whose names match the Go regex.
func PickMachineClass(ctx context.Context, c client.Client, cpu, memory resource.Quantity, namePattern string) (*ClassPick, error) {
	cpuFloor := ceilCPU(cpu)
	memFloorMiB := ceilMiB(memory)

	var nameRE *regexp.Regexp
	if namePattern != "" {
		var err error
		nameRE, err = regexp.Compile(namePattern)
		if err != nil {
			return nil, fmt.Errorf("invalid machineClassPattern %q: %w", namePattern, err)
		}
	}

	var list computev1alpha1.MachineClassList
	if err := c.List(ctx, &list); err != nil {
		return nil, fmt.Errorf("list MachineClasses: %w", err)
	}

	type candidate struct {
		name     string
		cpuCores int
		ramMiB   int
	}
	var (
		all      []candidate
		matching []candidate
	)

	for i := range list.Items {
		mc := &list.Items[i]
		cpuCores, ramMiB, ok := machineClassCapacity(mc)
		if !ok {
			continue
		}
		cand := candidate{name: mc.Name, cpuCores: cpuCores, ramMiB: ramMiB}
		all = append(all, cand)
		if nameRE != nil && !nameRE.MatchString(mc.Name) {
			continue
		}
		if cpuCores >= cpuFloor && ramMiB >= memFloorMiB {
			matching = append(matching, cand)
		}
	}

	if len(matching) == 0 {
		names := make([]string, 0, len(all))
		for _, c := range all {
			names = append(names, fmt.Sprintf("%s (%d vCPU / %s)", c.name, c.cpuCores, formatMiB(c.ramMiB)))
		}
		sort.Strings(names)
		patternHint := ""
		if namePattern != "" {
			patternHint = fmt.Sprintf(", matching /%s/", namePattern)
		}
		return nil, fmt.Errorf("no MachineClass satisfies cpu>=%d, memory>=%dMiB%s; available: %s",
			cpuFloor, memFloorMiB, patternHint, strings.Join(names, ", "))
	}

	sort.Slice(matching, func(i, j int) bool {
		a, b := matching[i], matching[j]
		if a.cpuCores != b.cpuCores {
			return a.cpuCores < b.cpuCores
		}
		if a.ramMiB != b.ramMiB {
			return a.ramMiB < b.ramMiB
		}
		return a.name < b.name
	})

	pick := matching[0]
	memHuman := formatMiB(pick.ramMiB)
	return &ClassPick{
		Name:        pick.name,
		CPU:         int32(pick.cpuCores),
		MemoryHuman: memHuman,
		Description: fmt.Sprintf("%d vCPU / %s RAM", pick.cpuCores, memHuman),
	}, nil
}

// GetMachineClass fetches a named (cluster-scoped) MachineClass and returns it as
// a ClassPick. When the class does not exist the error lists the available
// classes, mirroring PickMachineClass.
func GetMachineClass(ctx context.Context, c client.Client, name string) (*ClassPick, error) {
	var mc computev1alpha1.MachineClass
	if err := c.Get(ctx, client.ObjectKey{Name: name}, &mc); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("get MachineClass %q: %w", name, err)
		}
		return nil, fmt.Errorf("MachineClass %q not found; available: %s", name, availableClasses(ctx, c))
	}
	cpuCores, ramMiB, ok := machineClassCapacity(&mc)
	if !ok {
		return nil, fmt.Errorf("MachineClass %q has no cpu/memory capabilities", name)
	}
	memHuman := formatMiB(ramMiB)
	return &ClassPick{
		Name:        mc.Name,
		CPU:         int32(cpuCores),
		MemoryHuman: memHuman,
		Description: fmt.Sprintf("%d vCPU / %s RAM", cpuCores, memHuman),
	}, nil
}

// machineClassCapacity extracts whole-core CPU and MiB memory from a MachineClass.
// ok is false when either capability is missing.
func machineClassCapacity(mc *computev1alpha1.MachineClass) (cpuCores, ramMiB int, ok bool) {
	cpuQty := mc.Capabilities.CPU()
	memQty := mc.Capabilities.Memory()
	if cpuQty == nil || memQty == nil {
		return 0, 0, false
	}
	cpuCores = int((cpuQty.MilliValue() + 999) / 1000)
	ramMiB = int((memQty.Value() + (1024*1024 - 1)) / (1024 * 1024))
	return cpuCores, ramMiB, true
}

// availableClasses returns a sorted, human-readable list of all MachineClasses.
func availableClasses(ctx context.Context, c client.Client) string {
	var list computev1alpha1.MachineClassList
	if err := c.List(ctx, &list); err != nil {
		return fmt.Sprintf("<list error: %v>", err)
	}
	names := make([]string, 0, len(list.Items))
	for i := range list.Items {
		cpuCores, ramMiB, ok := machineClassCapacity(&list.Items[i])
		if !ok {
			continue
		}
		names = append(names, fmt.Sprintf("%s (%d vCPU / %s)", list.Items[i].Name, cpuCores, formatMiB(ramMiB)))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// ceilCPU rounds a CPU quantity up to whole cores.
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

// formatMiB renders an MiB count as the most readable unit ("4 GiB", "512 MiB").
func formatMiB(mib int) string {
	if mib >= 1024 && mib%1024 == 0 {
		return fmt.Sprintf("%d GiB", mib/1024)
	}
	if mib >= 1024 {
		return fmt.Sprintf("%.1f GiB", float64(mib)/1024)
	}
	return fmt.Sprintf("%d MiB", mib)
}
