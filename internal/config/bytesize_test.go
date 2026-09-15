package config

import "testing"

func TestParseByteSizeAcceptsEveryUnitCaseInsensitively(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"10485760", 10485760},
		{"10MB", 10 << 20},
		{"10mb", 10 << 20},
		{"10 MB", 10 << 20},
		{"500KB", 500 << 10},
		{"500K", 500 << 10},
		{"1GB", 1 << 30},
		{"1G", 1 << 30},
		{"2TB", 2 << 40},
		{"512B", 512},
		{"1.5MB", int64(1.5 * (1 << 20))},
		{"0", 0},
	}
	for _, c := range cases {
		got, err := ParseByteSize(c.in)
		if err != nil {
			t.Errorf("ParseByteSize(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseByteSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseByteSizeRejectsGarbage(t *testing.T) {
	cases := []string{"", "abc", "-5MB", "10XB", "MB"}
	for _, in := range cases {
		if _, err := ParseByteSize(in); err == nil {
			t.Errorf("ParseByteSize(%q) returned no error, want one", in)
		}
	}
}

func TestFormatByteSizeRoundTripsThroughParseByteSize(t *testing.T) {
	cases := []int64{0, 512, 1 << 10, 500 << 10, 10 << 20, 1 << 30, 2 << 40}
	for _, n := range cases {
		formatted := FormatByteSize(n)
		got, err := ParseByteSize(formatted)
		if err != nil {
			t.Fatalf("ParseByteSize(FormatByteSize(%d)=%q): %v", n, formatted, err)
		}
		if got != n {
			t.Errorf("round trip for %d: FormatByteSize -> %q -> ParseByteSize -> %d", n, formatted, got)
		}
	}
}

func TestFormatByteSizePicksTheLargestCleanUnit(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{500, "500B"},
		{10 << 20, "10MB"},
		{int64(1.5 * (1 << 20)), "1.5MB"},
	}
	for _, c := range cases {
		if got := FormatByteSize(c.in); got != c.want {
			t.Errorf("FormatByteSize(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
