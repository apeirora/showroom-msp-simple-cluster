package arsys

import "testing"

func model(id string, core, cpp, ram int32, state, dc string) BaremetalModel {
	return BaremetalModel{
		ID:           id,
		Name:         id,
		State:        state,
		Hardware:     BaremetalHardware{Core: core, CoresPerProcessor: cpp, RAM: ram, Unit: "GB"},
		Availability: []BaremetalAvailability{{DatacenterID: dc, Available: true}},
	}
}

func TestSmallestModel(t *testing.T) {
	const dc = "dc1"
	models := []BaremetalModel{
		model("s", 1, 4, 16, StateEnabled, dc),  // 4c/16
		model("m", 1, 6, 32, StateEnabled, dc),  // 6c/32
		model("m2", 1, 6, 64, StateEnabled, dc), // 6c/64
		model("big", 2, 8, 128, StateEnabled, dc),
		model("disabled", 1, 4, 16, "DISABLED", dc),
		model("otherdc", 1, 4, 16, StateEnabled, "dc2"),
	}

	tests := []struct {
		name             string
		minCores, minRAM int32
		dc               string
		want             string
	}{
		{"smallest fits all", 1, 1, dc, "s"},
		{"needs 6 cores", 6, 1, dc, "m"},
		{"6 cores 33GB skips m", 6, 33, dc, "m2"},
		{"tie breaks on ram", 5, 1, dc, "m"},
		{"none fits", 100, 1, dc, ""},
		{"wrong datacenter", 1, 1, "dc2", "otherdc"},
		{"disabled excluded", 4, 16, "dc1", "s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SmallestModel(models, tt.dc, tt.minCores, tt.minRAM)
			if tt.want == "" {
				if got != nil {
					t.Fatalf("want nil, got %s", got.ID)
				}
				return
			}
			if got == nil || got.ID != tt.want {
				t.Fatalf("want %s, got %v", tt.want, got)
			}
		})
	}
}

func TestRAMGB(t *testing.T) {
	cases := []struct {
		ram  int32
		unit string
		want int32
	}{
		{16, "GB", 16},
		{16, "", 16},
		{2048, "MB", 2},
		{1, "TB", 1024},
	}
	for _, c := range cases {
		if got := (BaremetalHardware{RAM: c.ram, Unit: c.unit}).RAMGB(); got != c.want {
			t.Errorf("RAMGB(%d %s) = %d, want %d", c.ram, c.unit, got, c.want)
		}
	}
}
