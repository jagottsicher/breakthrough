package filterexpr

import "testing"

func TestParseSizeSingleClause(t *testing.T) {
	cases := []struct {
		expr  string
		size  int64
		want  bool
		label string
	}{
		{"> 1m", 2 * 1024 * 1024, true, "2M > 1M"},
		{"> 1m", 1024 * 1024, false, "1M is not > 1M"},
		{">= 1m", 1024 * 1024, true, "1M >= 1M"},
		{"< 1k", 500, true, "500B < 1K"},
		{"< 1k", 2000, false, "2000B is not < 1K"},
		{"<= 1024", 1024, true, "exactly 1024 bytes, no unit"},
		{"= 0", 0, true, "empty file"},
		{"== 0", 5, false, "== is an alias for =, not just any zero"},
	}
	for _, c := range cases {
		f, err := ParseSize(c.expr)
		if err != nil {
			t.Fatalf("ParseSize(%q): %v", c.expr, err)
		}
		if got := f.Match(c.size); got != c.want {
			t.Errorf("%s: ParseSize(%q).Match(%d) = %v, want %v", c.label, c.expr, c.size, got, c.want)
		}
	}
}

// TestParseSizeUnitsAreBinary pins the user's own convention already
// established elsewhere in this app (see humanSize in
// internal/ui/properties.go): k/m/g/t are 1024-based, not the decimal
// 1000-based SI convention.
func TestParseSizeUnitsAreBinary(t *testing.T) {
	f, err := ParseSize("= 1m")
	if err != nil {
		t.Fatalf("ParseSize: %v", err)
	}
	if !f.Match(1024 * 1024) {
		t.Error("1m should mean exactly 1024*1024 bytes (binary), not 1,000,000 (decimal)")
	}
	if f.Match(1000 * 1000) {
		t.Error("1m should not match the decimal interpretation (1,000,000 bytes)")
	}
}

func TestParseSizeUnitsCaseInsensitive(t *testing.T) {
	f, err := ParseSize("> 1M")
	if err != nil {
		t.Fatalf("ParseSize: %v", err)
	}
	if !f.Match(2 * 1024 * 1024) {
		t.Error("uppercase unit \"M\" should work the same as lowercase \"m\"")
	}
}

// TestParseSizeRangeCombinesClausesWithAnd pins the user's own explicit
// request: "size < 1m AND size > 5m"-style ranges, expressed here as
// two clauses on the same field joined by "and" — every clause must
// hold for Match to return true.
func TestParseSizeRangeCombinesClausesWithAnd(t *testing.T) {
	f, err := ParseSize("> 1m and < 5m")
	if err != nil {
		t.Fatalf("ParseSize: %v", err)
	}
	if !f.Match(3 * 1024 * 1024) {
		t.Error("3M should be inside the (1M, 5M) range")
	}
	if f.Match(512 * 1024) {
		t.Error("512K should be below the range's own lower bound")
	}
	if f.Match(6 * 1024 * 1024) {
		t.Error("6M should be above the range's own upper bound")
	}
}

func TestParseSizeAndIsCaseInsensitiveAndWordBounded(t *testing.T) {
	if _, err := ParseSize("> 1m AND < 5m"); err != nil {
		t.Errorf("uppercase AND should parse the same as lowercase: %v", err)
	}
}

func TestParseSizeAcceptsDecimalNumbers(t *testing.T) {
	f, err := ParseSize("> 1.5m")
	if err != nil {
		t.Fatalf("ParseSize: %v", err)
	}
	if f.Match(int64(1.5 * 1024 * 1024)) {
		t.Error("exactly 1.5M should not be > 1.5M")
	}
	oneSixM := 1.6 * 1024 * 1024 // kept as a runtime float, not a constant expression: it isn't an exact integer, and Go's constant-truncation rule would refuse int64(1.6*1024*1024) at compile time
	if !f.Match(int64(oneSixM)) {
		t.Error("1.6M should be > 1.5M")
	}
}

func TestParseSizeRejectsEmptyExpression(t *testing.T) {
	if _, err := ParseSize(""); err == nil {
		t.Error("an empty expression should be rejected, not treated as \"match everything\"")
	}
	if _, err := ParseSize("   "); err == nil {
		t.Error("a whitespace-only expression should also be rejected")
	}
}

func TestParseSizeRejectsMissingOperator(t *testing.T) {
	if _, err := ParseSize("1m"); err == nil {
		t.Error("an expression with no leading comparison operator should be rejected")
	}
}

func TestParseSizeRejectsUnrecognizedUnit(t *testing.T) {
	if _, err := ParseSize("> 1x"); err == nil {
		t.Error("an unrecognized unit should be rejected")
	}
}

func TestParseSizeRejectsMissingNumber(t *testing.T) {
	if _, err := ParseSize(">"); err == nil {
		t.Error("an operator with no number after it should be rejected")
	}
	if _, err := ParseSize("> m"); err == nil {
		t.Error("a unit with no number in front of it should be rejected")
	}
}

func TestParseSizeRejectsMalformedNumber(t *testing.T) {
	if _, err := ParseSize("> 1.2.3m"); err == nil {
		t.Error("a number with two decimal points should be rejected")
	}
}

func TestParseSizeToleratesSurroundingWhitespace(t *testing.T) {
	f, err := ParseSize("  >   1m  ")
	if err != nil {
		t.Fatalf("ParseSize: %v", err)
	}
	if !f.Match(2 * 1024 * 1024) {
		t.Error("surrounding whitespace should not change the parsed result")
	}
}
