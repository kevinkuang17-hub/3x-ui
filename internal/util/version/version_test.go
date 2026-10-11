package version

import "testing"

func TestCompareRejectsUnexpectedFormats(t *testing.T) {
	if _, ok := Compare("latest", "2.9.3"); ok {
		t.Fatal("expected non-semver latest tag to be rejected")
	}
	if _, ok := Compare("v2.9", "2.9.3"); ok {
		t.Fatal("expected short version to be rejected")
	}
}

// The fork ships "3.9.0-kN" over upstream's 3.9.0; treating -kN as unparsable made an
// updated panel still offer "update to v3.9.0-k8".
func TestCompareForkRevision(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v3.9.0-k8", "3.9.0-k8", 0},
		{"v3.9.0-k8", "3.9.0", 1},
		{"3.9.0", "3.9.0-k8", -1},
		{"v3.9.0-k10", "3.9.0-k9", 1},
		{"3.9.0-k8", "3.9.0-k9", -1},
		{"v3.9.1", "3.9.0-k12", 1},
		{"3.8.9-k30", "3.9.0", -1},
	}
	for _, tc := range cases {
		got, ok := Compare(tc.a, tc.b)
		if !ok || got != tc.want {
			t.Errorf("Compare(%q, %q) = %d, %v; want %d, true", tc.a, tc.b, got, ok, tc.want)
		}
	}
	for _, bad := range []string{"3.9.0-beta", "3.9.0-k", "3.9.0-k1x", "3.9.0-k-1"} {
		if _, ok := Compare(bad, "3.9.0"); ok {
			t.Errorf("Compare(%q) accepted a non-fork suffix", bad)
		}
	}
}
