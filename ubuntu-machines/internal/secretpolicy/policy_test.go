package secretpolicy

import (
	"encoding/json"
	"os"
	"testing"
)

func TestConformance(t *testing.T) {
	raw, err := os.ReadFile("../../../testdata/secret-reference-policy.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name          string  `json:"case"`
		Owner         string  `json:"owner"`
		Namespace     string  `json:"namespace"`
		Reference     string  `json:"name"`
		Purpose       string  `json:"purpose"`
		Entries       []Entry `json:"entries"`
		WantNamespace string  `json:"wantNamespace"`
		Allowed       bool    `json:"allowed"`
		Exact         bool    `json:"exact"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			encoded, err := json.Marshal(tc.Entries)
			if err != nil {
				t.Fatal(err)
			}
			p, err := Parse(string(encoded))
			if err != nil {
				t.Fatal(err)
			}
			var ns string
			if tc.Exact {
				err = p.Require(tc.Namespace, tc.Reference, tc.Purpose)
			} else {
				ns, err = p.Resolve(tc.Owner, tc.Namespace, tc.Reference, tc.Purpose)
			}
			if (err == nil) != tc.Allowed {
				t.Fatalf("allowed=%v, error=%v", tc.Allowed, err)
			}
			if tc.Allowed && !tc.Exact && ns != tc.WantNamespace {
				t.Fatalf("namespace=%q, want %q", ns, tc.WantNamespace)
			}
		})
	}
}

func TestInvalidConfiguration(t *testing.T) {
	for _, raw := range []string{`[{"namespace":"*","name":"key","purpose":"use"}]`, `[{"namespace":"shared","name":"*","purpose":"use"}]`, `[{"namespace":"shared","name":"key","purpose":"unknown"}]`, `[{"namespace":"shared","name":"key","purpose":"use","extra":true}]`, `[] []`, `{}`} {
		if _, err := Parse(raw); err == nil {
			t.Fatalf("accepted invalid allowlist %s", raw)
		}
	}
}
