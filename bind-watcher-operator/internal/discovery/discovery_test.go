package discovery

import "testing"

func TestKcpBase(t *testing.T) {
	cases := map[string]string{
		"https://kcp.example:443/clusters/root:providers:dati": "https://kcp.example:443",
		"https://kcp.example:443/clusters/root":                "https://kcp.example:443",
		"https://kcp.example:443/":                             "https://kcp.example:443",
		"https://kcp.example:443":                              "https://kcp.example:443",
	}
	for in, want := range cases {
		if got := kcpBase(in); got != want {
			t.Errorf("kcpBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPrefixNoSeparator(t *testing.T) {
	// The multi-provider splits cluster names on '#', so the prefix must not
	// contain it (nor the internal key separators).
	p := prefix("root:providers:dati|shoots.dati.eu")
	for _, bad := range []string{"#", "|", ":"} {
		if containsRune(p, bad) {
			t.Errorf("prefix %q contains forbidden %q", p, bad)
		}
	}
}

func containsRune(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
