package template

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
)

func TestRenderString_QuantityComparison(t *testing.T) {
	for _, tc := range []struct {
		name, left, right, want string
	}{
		{"equivalent CPU", "2000m", "2", "0"},
		{"equivalent memory", "4096Mi", "4Gi", "0"},
		{"fractional CPU", "1500m", "2", "-1"},
		{"larger memory", "5Gi", "4Gi", "1"},
		{"decimal versus binary", "4G", "4Gi", "-1"},
		{"zero", "0", "0", "0"},
		{"negative", "-1", "0", "-1"},
		{"exact large integer", "9007199254740993", "9007199254740992", "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := RenderString("quantity", `{{ quantityCmp .left .right }}`, map[string]any{"left": tc.left, "right": tc.right})
			if err != nil || got != tc.want {
				t.Fatalf("comparison = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestRenderString_InvalidQuantityFails(t *testing.T) {
	for _, tc := range []struct{ name, left, right, operand string }{
		{"left", "not-a-quantity", "2", "left"},
		{"right", "2", "4GB", "right"},
		{"empty", "", "2", "left"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := RenderString("quantity", `prefix{{ quantityCmp .left .right }}`, map[string]any{"left": tc.left, "right": tc.right})
			if err == nil || !strings.Contains(err.Error(), "parse "+tc.operand+" quantity") || got != "" {
				t.Fatalf("render = %q, %v; want an operand-specific error and no output", got, err)
			}
		})
	}
}

func TestRender_FixedCapacityRejectsOversizedRequests(t *testing.T) {
	scpc := minimalSCPC(nil)
	r := &showroomv1alpha1.ResourceSpec{
		Name: "machine", APIVersion: "example.com/v1", Kind: "Machine",
		Template: `{{- if or (le (quantityCmp .SC.workers.requested.cpu "0") 0) (gt (quantityCmp .SC.workers.requested.cpu "2") 0) (le (quantityCmp .SC.workers.requested.memory "0") 0) (gt (quantityCmp .SC.workers.requested.memory "4Gi") 0) }}{{ fail "unsupported capacity" }}{{ end }}
spec:
  machineType: fixed-small
`,
	}
	for _, tc := range []struct {
		name, cpu, memory string
		accepted          bool
	}{
		{"equivalent capacity", "2000m", "4096Mi", true},
		{"smaller request", "1500m", "3Gi", true},
		{"CPU too large", "2001m", "4Gi", false},
		{"memory too large", "2", "4097Mi", false},
		{"zero CPU", "0", "4Gi", false},
		{"negative memory", "2", "-1Gi", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sc := minimalSC(1, nil)
			sc.Spec.Workers.Requested.CPU = resource.MustParse(tc.cpu)
			sc.Spec.Workers.Requested.Memory = resource.MustParse(tc.memory)
			objects, err := Render(scpc, sc, r, nil)
			if tc.accepted {
				if err != nil || len(objects) != 1 {
					t.Fatalf("render = %v, %v; want one machine", objects, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "unsupported capacity") || len(objects) != 0 {
				t.Fatalf("render = %v, %v; want capacity rejection with no objects", objects, err)
			}
		})
	}
}
