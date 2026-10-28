package consigliere

import "testing"

func TestCombineConsigliereSystem(t *testing.T) {
	cases := []struct {
		name     string
		system   string
		snapshot string
		want     string
	}{
		{"both set", "Be terse.", "dir: /tmp", "Be terse.\n\ndir: /tmp"},
		{"system only", "Be terse.", "", "Be terse."},
		{"snapshot only", "", "dir: /tmp", "dir: /tmp"},
		{"neither set", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := combineConsigliereSystem(c.system, c.snapshot); got != c.want {
				t.Errorf("combineConsigliereSystem(%q, %q) = %q, want %q", c.system, c.snapshot, got, c.want)
			}
		})
	}
}
