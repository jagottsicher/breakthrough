package config

import (
	"fmt"
	"strconv"
	"strings"
)

// byteSizeUnits maps a case-insensitive suffix to its own power-of-1024
// multiplier — the same base this app's own humanSize (internal/ui)
// already uses throughout, so a value written here means the same
// thing everywhere else a size is shown.
var byteSizeUnits = map[string]int64{
	"b":  1,
	"k":  1 << 10,
	"kb": 1 << 10,
	"m":  1 << 20,
	"mb": 1 << 20,
	"g":  1 << 30,
	"gb": 1 << 30,
	"t":  1 << 40,
	"tb": 1 << 40,
}

// ParseByteSize reads a size like "10MB", "10 MB", "500K", or a bare
// "10485760" (plain bytes, the unit-less fallback) into its own byte
// count — case-insensitive, whitespace around the number and the unit
// both tolerated. remote_archive_confirm_size is the one setting this
// exists for, and the one place in this app's own config file format
// that accepts a unit suffix at all (see its own SettingDoc): a
// download-threshold size is naturally something a person thinks and
// types in KB/MB/GB, unlike every other numeric setting here, which is
// a bare integer (a count, a percentage, a millisecond figure) with no
// unit ambiguity to begin with.
func ParseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}

	i := 0
	for i < len(s) && (s[i] == '.' || s[i] == '-' || (s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	numPart := strings.TrimSpace(s[:i])
	unitPart := strings.ToLower(strings.TrimSpace(s[i:]))
	if numPart == "" {
		return 0, fmt.Errorf("invalid size %q: no number found", s)
	}

	n, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("invalid size %q: must not be negative", s)
	}

	multiplier := int64(1)
	if unitPart != "" {
		m, ok := byteSizeUnits[unitPart]
		if !ok {
			return 0, fmt.Errorf("invalid size %q: unrecognized unit %q", s, unitPart)
		}
		multiplier = m
	}
	return int64(n * float64(multiplier)), nil
}

// FormatByteSize renders n back as a human-typeable size — the exact
// inverse of ParseByteSize, so a value round-trips through the Options
// screen and the config file unchanged. Picks the largest unit that
// divides n evenly (no fractional digit shown for a whole number of
// that unit), one decimal place otherwise, and falls back to a bare
// byte count below 1024 — never invents a unit a value doesn't cleanly
// need, e.g. 500 renders as "500B", not "0.5KB".
func FormatByteSize(n int64) string {
	units := []struct {
		suffix string
		size   int64
	}{
		{"TB", 1 << 40},
		{"GB", 1 << 30},
		{"MB", 1 << 20},
		{"KB", 1 << 10},
	}
	for _, u := range units {
		if n < u.size {
			continue
		}
		v := float64(n) / float64(u.size)
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d%s", int64(v), u.suffix)
		}
		return fmt.Sprintf("%.1f%s", v, u.suffix)
	}
	return fmt.Sprintf("%dB", n)
}
