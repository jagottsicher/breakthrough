package replace

import "testing"

func TestBuildScriptLiteralRoundTrip(t *testing.T) {
	script, err := BuildScript("foo", "bar", false, false, false, true)
	if err != nil {
		t.Fatalf("BuildScript: %v", err)
	}
	got, err := RunSed(script, false, []byte("foo and foo again\n"))
	if err != nil {
		t.Fatalf("RunSed(%q): %v", script, err)
	}
	if string(got) != "bar and bar again\n" {
		t.Errorf("RunSed(%q) = %q, want %q", script, got, "bar and bar again\n")
	}
}

func TestBuildScriptLiteralEscapesRegexMetacharacters(t *testing.T) {
	// "a.b" as a literal string must not match "axb" the way the regex
	// "." would - proves escaping actually happened, not just that the
	// unescaped case works.
	script, err := BuildScript("a.b", "X", false, false, false, true)
	if err != nil {
		t.Fatalf("BuildScript: %v", err)
	}
	got, err := RunSed(script, false, []byte("a.b axb\n"))
	if err != nil {
		t.Fatalf("RunSed(%q): %v", script, err)
	}
	if string(got) != "X axb\n" {
		t.Errorf("RunSed(%q) = %q, want %q (only the literal \"a.b\" replaced)", script, got, "X axb\n")
	}
}

func TestBuildScriptLiteralEscapesSlashInFindAndReplace(t *testing.T) {
	// A path-like Find/Replace pair containing "/" must not break the
	// s/// command - pickDelimiter should choose a different delimiter
	// automatically rather than needing the caller to escape anything.
	script, err := BuildScript("/usr/local", "/opt/app", false, false, false, true)
	if err != nil {
		t.Fatalf("BuildScript: %v", err)
	}
	got, err := RunSed(script, false, []byte("path=/usr/local/bin\n"))
	if err != nil {
		t.Fatalf("RunSed(%q): %v", script, err)
	}
	if string(got) != "path=/opt/app/bin\n" {
		t.Errorf("RunSed(%q) = %q, want %q", script, got, "path=/opt/app/bin\n")
	}
}

func TestBuildScriptRegexModeUsesBackreferences(t *testing.T) {
	// (...) and + are ERE group/quantifier syntax - extendedRegex must
	// be true both when building (useRegex=true skips escaping either
	// way) and, critically, when actually running sed, or "(" is just a
	// literal character to it and \1/\2 have nothing to refer to.
	script, err := BuildScript(`(foo)-([0-9]+)`, `\2-\1`, true, true, false, true)
	if err != nil {
		t.Fatalf("BuildScript: %v", err)
	}
	got, err := RunSed(script, true, []byte("foo-42\n"))
	if err != nil {
		t.Fatalf("RunSed(%q): %v", script, err)
	}
	if string(got) != "42-foo\n" {
		t.Errorf("RunSed(%q) = %q, want %q", script, got, "42-foo\n")
	}
}

func TestBuildScriptExtendedRegexEscapesParens(t *testing.T) {
	// With extendedRegex on but useRegex off, a literal "(" in Find must
	// still be escaped - ERE treats a bare "(" as a group start, unlike
	// BRE, so this only exercises anything if extendedRegex actually
	// changes which characters get escaped *and* sed is actually run in
	// -E mode to match (see RunSed's own doc comment on why both matter
	// together).
	script, err := BuildScript("(foo)", "bar", false, true, false, true)
	if err != nil {
		t.Fatalf("BuildScript: %v", err)
	}
	got, err := RunSed(script, true, []byte("(foo) and foo\n"))
	if err != nil {
		t.Fatalf("RunSed(%q): %v", script, err)
	}
	if string(got) != "bar and foo\n" {
		t.Errorf("RunSed(%q) = %q, want %q (only the literal \"(foo)\" replaced)", script, got, "bar and foo\n")
	}
}

func TestBuildScriptGlobalFlag(t *testing.T) {
	global, err := BuildScript("a", "X", false, false, false, true)
	if err != nil {
		t.Fatal(err)
	}
	firstOnly, err := BuildScript("a", "X", false, false, false, false)
	if err != nil {
		t.Fatal(err)
	}

	gotGlobal, err := RunSed(global, false, []byte("a a a\n"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotGlobal) != "X X X\n" {
		t.Errorf("global RunSed = %q, want %q", gotGlobal, "X X X\n")
	}

	gotFirst, err := RunSed(firstOnly, false, []byte("a a a\n"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotFirst) != "X a a\n" {
		t.Errorf("non-global RunSed = %q, want %q", gotFirst, "X a a\n")
	}
}

func TestBuildScriptCaseInsensitiveFlag(t *testing.T) {
	script, err := BuildScript("foo", "X", false, false, true, true)
	if err != nil {
		t.Fatal(err)
	}
	got, err := RunSed(script, false, []byte("FOO foo Foo\n"))
	if err != nil {
		t.Fatalf("RunSed(%q): %v", script, err)
	}
	if string(got) != "X X X\n" {
		t.Errorf("RunSed(%q) = %q, want %q", script, got, "X X X\n")
	}
}

func TestPickDelimiterFallsThroughCandidates(t *testing.T) {
	// "/" and "#" both appear, so pickDelimiter must skip to the next
	// candidate rather than picking one that's actually present.
	d, err := pickDelimiter("a/b#c", "x/y#z")
	if err != nil {
		t.Fatalf("pickDelimiter: %v", err)
	}
	if d == '/' || d == '#' {
		t.Errorf("pickDelimiter returned %q, which appears in the inputs", d)
	}
}
