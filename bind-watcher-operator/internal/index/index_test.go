package index

import (
	"reflect"
	"testing"
)

func TestExportIndexSingleProvider(t *testing.T) {
	i := New()

	// Two bindings observed via the same provider (mc0) in cluster X.
	i.Set("X", "mc0#X", "b1", "simplecluster")
	i.Set("X", "mc0#X", "b2", "dati")
	if got, want := i.Exports("X"), []string{"dati", "simplecluster"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("X exports = %v, want %v", got, want)
	}

	// Unbind one binding.
	i.Remove("X", "mc0#X", "b2")
	if got, want := i.Exports("X"), []string{"simplecluster"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("X exports after remove = %v, want %v", got, want)
	}

	// Remove the last binding -> empty.
	i.Remove("X", "mc0#X", "b1")
	if got := i.Exports("X"); len(got) != 0 {
		t.Fatalf("X exports = %v, want empty", got)
	}
}

// TestExportIndexMultiProviderDisengage is the important case: the same
// workspace is bound to two exports through two providers; disengaging one must
// not drop the other's export.
func TestExportIndexMultiProviderDisengage(t *testing.T) {
	i := New()
	i.Set("X", "kcp-0#X", "b-sc", "simplecluster")
	i.Set("X", "kcp-1#X", "b-dati", "dati")

	if got, want := i.Exports("X"), []string{"dati", "simplecluster"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("X exports = %v, want %v", got, want)
	}

	// Provider kcp-0 disengages (simplecluster unbound). dati must survive.
	i.RemoveCluster("X", "kcp-0#X")
	if got, want := i.Exports("X"), []string{"dati"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("X exports after kcp-0 disengage = %v, want %v", got, want)
	}

	// kcp-1 disengages too -> empty.
	i.RemoveCluster("X", "kcp-1#X")
	if got := i.Exports("X"); len(got) != 0 {
		t.Fatalf("X exports = %v, want empty", got)
	}
}
