// Package index keeps an in-memory view of which APIExports are currently bound
// in each KCP logical cluster. It is the single source of truth from which the
// aggregated namespace annotation is rendered, so bind/unbind of one export
// never clobbers the others.
//
// Entries are keyed by (realCluster, mcCluster, binding): the same KCP workspace
// (realCluster) may be observed through several APIExport providers, each of
// which multicluster-runtime engages under its own prefixed name (mcCluster).
// Keeping the mcCluster level lets a single provider disengage without dropping
// exports still held by the others.
package index

import (
	"sort"
	"sync"
)

// ExportIndex maps realCluster -> mcCluster -> bindingName -> exportName.
type ExportIndex struct {
	mu   sync.Mutex
	data map[string]map[string]map[string]string
}

// New returns an empty ExportIndex.
func New() *ExportIndex {
	return &ExportIndex{data: map[string]map[string]map[string]string{}}
}

// Set records that APIBinding bindingName, observed via mcCluster, binds
// exportName in realCluster.
func (i *ExportIndex) Set(realCluster, mcCluster, bindingName, exportName string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	rc := i.data[realCluster]
	if rc == nil {
		rc = map[string]map[string]string{}
		i.data[realCluster] = rc
	}
	mc := rc[mcCluster]
	if mc == nil {
		mc = map[string]string{}
		rc[mcCluster] = mc
	}
	mc[bindingName] = exportName
}

// Remove drops a single APIBinding from the index.
func (i *ExportIndex) Remove(realCluster, mcCluster, bindingName string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.removeLocked(realCluster, mcCluster, bindingName)
}

// RemoveCluster drops everything observed via mcCluster (used on disengage).
func (i *ExportIndex) RemoveCluster(realCluster, mcCluster string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	rc := i.data[realCluster]
	if rc == nil {
		return
	}
	delete(rc, mcCluster)
	if len(rc) == 0 {
		delete(i.data, realCluster)
	}
}

func (i *ExportIndex) removeLocked(realCluster, mcCluster, bindingName string) {
	rc := i.data[realCluster]
	if rc == nil {
		return
	}
	mc := rc[mcCluster]
	if mc == nil {
		return
	}
	delete(mc, bindingName)
	if len(mc) == 0 {
		delete(rc, mcCluster)
	}
	if len(rc) == 0 {
		delete(i.data, realCluster)
	}
}

// Exports returns the sorted, de-duplicated export names bound in realCluster
// across all providers that observe it.
func (i *ExportIndex) Exports(realCluster string) []string {
	i.mu.Lock()
	defer i.mu.Unlock()
	set := map[string]struct{}{}
	for _, mc := range i.data[realCluster] {
		for _, exp := range mc {
			set[exp] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for exp := range set {
		out = append(out, exp)
	}
	sort.Strings(out)
	return out
}
