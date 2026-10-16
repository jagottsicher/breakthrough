package filterexpr

import "testing"

func matchStr(t *testing.T, expr, source, message string) bool {
	t.Helper()
	f, err := ParseQuery(expr)
	if err != nil {
		t.Fatalf("ParseQuery(%q): %v", expr, err)
	}
	return f.Match(source, message)
}

func TestParseQueryBareTermMatchesSourceOrMessage(t *testing.T) {
	cases := []struct {
		expr, source, message string
		want                  bool
	}{
		{"foo", "FOO", "bar", true},
		{"foo", "bar", "a foo b", true},
		{"foo", "bar", "baz", false},
		{"FOO", "bar", "a foo b", true}, // case-insensitive
	}
	for _, c := range cases {
		if got := matchStr(t, c.expr, c.source, c.message); got != c.want {
			t.Errorf("ParseQuery(%q).Match(source=%q, message=%q) = %v, want %v", c.expr, c.source, c.message, got, c.want)
		}
	}
}

func TestParseQueryImplicitAndBetweenAdjacentTerms(t *testing.T) {
	cases := []struct {
		source, message string
		want            bool
	}{
		{"nginx", "foo bar", true},
		{"nginx", "foo only", false},
		{"nginx", "bar only", false},
	}
	for _, c := range cases {
		if got := matchStr(t, "foo bar", c.source, c.message); got != c.want {
			t.Errorf("ParseQuery(%q).Match(%q,%q) = %v, want %v", "foo bar", c.source, c.message, got, c.want)
		}
	}
}

func TestParseQueryExplicitAndOr(t *testing.T) {
	cases := []struct {
		expr, message string
		want          bool
	}{
		{"foo and bar", "foo bar", true},
		{"foo and bar", "foo only", false},
		{"foo or bar", "foo only", true},
		{"foo or bar", "bar only", true},
		{"foo or bar", "neither", false},
	}
	for _, c := range cases {
		if got := matchStr(t, c.expr, "", c.message); got != c.want {
			t.Errorf("ParseQuery(%q).Match(%q) = %v, want %v", c.expr, c.message, got, c.want)
		}
	}
}

// TestParseQueryUserExample pins the exact example the user asked for:
// "keyword, keyword1 AND (keyword2 OR keyword3)" — spelled here with
// explicit AND/OR, since the comma in the user's own prose was English
// punctuation, not query syntax (ParseQuery has no comma operator).
func TestParseQueryUserExample(t *testing.T) {
	const expr = "keyword1 AND (keyword2 OR keyword3)"
	cases := []struct {
		message string
		want    bool
	}{
		{"keyword1 keyword2", true},
		{"keyword1 keyword3", true},
		{"keyword1 keyword2 keyword3", true},
		{"keyword1 only", false},     // keyword1 present, but neither keyword2 nor keyword3
		{"keyword2 keyword3", false}, // keyword1 missing entirely
	}
	for _, c := range cases {
		if got := matchStr(t, expr, "", c.message); got != c.want {
			t.Errorf("ParseQuery(%q).Match(%q) = %v, want %v", expr, c.message, got, c.want)
		}
	}
}

func TestParseQueryNandNor(t *testing.T) {
	cases := []struct {
		expr, message string
		want          bool
	}{
		{"foo nand bar", "foo bar", false}, // both present -> NAND false
		{"foo nand bar", "foo only", true},
		{"foo nand bar", "neither", true},
		{"foo nor bar", "neither", true},
		{"foo nor bar", "foo only", false},
		{"foo nor bar", "foo bar", false},
	}
	for _, c := range cases {
		if got := matchStr(t, c.expr, "", c.message); got != c.want {
			t.Errorf("ParseQuery(%q).Match(%q) = %v, want %v", c.expr, c.message, got, c.want)
		}
	}
}

func TestParseQueryNot(t *testing.T) {
	cases := []struct {
		expr, message string
		want          bool
	}{
		{"not foo", "bar", true},
		{"not foo", "foo", false},
		{"foo and not bar", "foo only", true},
		{"foo and not bar", "foo bar", false},
	}
	for _, c := range cases {
		if got := matchStr(t, c.expr, "", c.message); got != c.want {
			t.Errorf("ParseQuery(%q).Match(%q) = %v, want %v", c.expr, c.message, got, c.want)
		}
	}
}

func TestParseQueryPrecedenceWithoutParens(t *testing.T) {
	// "a or b and c" must read as "a or (b and c)" (AND binds tighter
	// than OR) — matching "a" alone is enough regardless of b/c.
	const expr = "a or b and c"
	if !matchStr(t, expr, "", "a only") {
		t.Errorf("ParseQuery(%q).Match(%q) = false, want true (AND should bind tighter than OR)", expr, "a only")
	}
	if matchStr(t, expr, "", "b only") {
		t.Errorf("ParseQuery(%q).Match(%q) = true, want false (b alone, without c, should not satisfy b and c)", expr, "b only")
	}
	if !matchStr(t, expr, "", "b c") {
		t.Errorf("ParseQuery(%q).Match(%q) = false, want true (b and c both present)", expr, "b c")
	}
}

func TestParseQueryQuotedLiteralOperatorWord(t *testing.T) {
	// Quoting "and" makes it a literal term, not the AND operator.
	const expr = `"and"`
	if !matchStr(t, expr, "", "rock and roll") {
		t.Error(`ParseQuery(` + "`" + `"and"` + "`" + `) should match the literal word "and" in the message`)
	}
	if matchStr(t, expr, "", "nothing here") {
		t.Error(`ParseQuery(` + "`" + `"and"` + "`" + `) should not match text without the literal word "and"`)
	}
}

func TestParseQueryParens(t *testing.T) {
	const expr = "(foo or bar) and baz"
	if !matchStr(t, expr, "", "foo baz") {
		t.Errorf("ParseQuery(%q) should match %q", expr, "foo baz")
	}
	if matchStr(t, expr, "", "foo only") {
		t.Errorf("ParseQuery(%q) should not match %q (missing baz)", expr, "foo only")
	}
}

func TestParseQueryErrors(t *testing.T) {
	cases := []string{
		"",        // empty
		"(foo",    // unterminated paren
		"foo)",    // stray closing paren
		"and foo", // operator with no left operand
		`"unterminated`,
		`""`, // empty quoted term
	}
	for _, expr := range cases {
		if _, err := ParseQuery(expr); err == nil {
			t.Errorf("ParseQuery(%q) = nil error, want an error", expr)
		}
	}
}

func TestQueryFilterPositiveTerms(t *testing.T) {
	cases := []struct {
		expr string
		want []string
	}{
		{"foo", []string{"foo"}},
		{"foo and bar", []string{"foo", "bar"}},
		{"foo or bar", []string{"foo", "bar"}},
		{"foo and not bar", []string{"foo"}},                 // bar is negated, excluded
		{"not (foo or bar)", nil},                            // both negated via the outer NOT
		{"foo nand bar", nil},                                // NAND negates both operands
		{"foo nor bar", nil},                                 // NOR negates both operands
		{"foo and (bar or not baz)", []string{"foo", "bar"}}, // baz negated, bar is not
	}
	for _, c := range cases {
		f, err := ParseQuery(c.expr)
		if err != nil {
			t.Fatalf("ParseQuery(%q): %v", c.expr, err)
		}
		got := f.PositiveTerms()
		if !stringSlicesEqual(got, c.want) {
			t.Errorf("ParseQuery(%q).PositiveTerms() = %v, want %v", c.expr, got, c.want)
		}
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
