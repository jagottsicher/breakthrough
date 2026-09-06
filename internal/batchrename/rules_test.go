package batchrename

import "testing"

func TestSplitNameHandlesTheCommonShapes(t *testing.T) {
	cases := []struct {
		name     string
		wantBase string
		wantExt  string
	}{
		{"photo.jpg", "photo", ".jpg"},
		{"archive.tar.gz", "archive.tar", ".gz"},
		{"makefile", "makefile", ""},
		// A dotfile's leading dot is the hidden-file marker, not an
		// extension separator — filepath.Ext gets this one wrong (see
		// splitName's own doc comment).
		{".bashrc", ".bashrc", ""},
		{"..hidden.txt", "..hidden", ".txt"},
		{"", "", ""},
	}
	for _, c := range cases {
		base, ext := splitName(c.name)
		if base != c.wantBase || ext != c.wantExt {
			t.Errorf("splitName(%q) = (%q, %q), want (%q, %q)", c.name, base, ext, c.wantBase, c.wantExt)
		}
	}
}

func TestRenameIsANoOpAtTheZeroValue(t *testing.T) {
	got, err := Rename(Rules{}, "Report Final.TXT", 0)
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got != "Report Final.TXT" {
		t.Errorf("Rename with zero Rules = %q, want the name unchanged", got)
	}
}

func TestFindReplacePlain(t *testing.T) {
	rules := Rules{Find: "vacation", Replace: "trip"}
	got, err := Rename(rules, "vacation photo.jpg", 0)
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got != "trip photo.jpg" {
		t.Errorf("got %q", got)
	}
}

func TestFindReplaceDoesNotTouchTheExtension(t *testing.T) {
	rules := Rules{Find: "jpg", Replace: "png"}
	got, err := Rename(rules, "vacation.jpg", 0)
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got != "vacation.jpg" {
		t.Errorf("got %q, want the extension left alone (see ExtensionMode for that job)", got)
	}
}

func TestFindReplaceRegex(t *testing.T) {
	rules := Rules{Find: `(\d+)`, Replace: "[$1]", Regex: true}
	got, err := Rename(rules, "img42.jpg", 0)
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got != "img[42].jpg" {
		t.Errorf("got %q", got)
	}
}

func TestFindReplaceInvalidRegexFails(t *testing.T) {
	rules := Rules{Find: `(unclosed`, Regex: true}
	if _, err := Rename(rules, "a.txt", 0); err == nil {
		t.Fatal("expected an error for an invalid regex, got nil")
	}
}

func TestCaseTransforms(t *testing.T) {
	cases := []struct {
		mode CaseMode
		name string
		want string
	}{
		{CaseUpper, "my_report-v2.txt", "MY_REPORT-V2.txt"},
		{CaseLower, "MY_REPORT-V2.txt", "my_report-v2.txt"},
		{CaseTitle, "my_report-v2.txt", "My_Report-V2.txt"},
		{CaseSentence, "MY REPORT.txt", "My report.txt"},
		// The first letter found is capitalized even with leading
		// digits/punctuation ahead of it — the same behavior common
		// "Sentence case" transforms (e.g. word processors) already use.
		{CaseSentence, "123 report.txt", "123 Report.txt"},
	}
	for _, c := range cases {
		got, err := Rename(Rules{Case: c.mode}, c.name, 0)
		if err != nil {
			t.Fatalf("Rename: %v", err)
		}
		if got != c.want {
			t.Errorf("case mode %v on %q = %q, want %q", c.mode, c.name, got, c.want)
		}
	}
}

func TestCaseLeavesTheExtensionAlone(t *testing.T) {
	got, err := Rename(Rules{Case: CaseUpper}, "report.PDF", 0)
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got != "REPORT.PDF" {
		t.Errorf("got %q, want the extension's own case untouched", got)
	}
}

func TestTrim(t *testing.T) {
	cases := []struct {
		front, back int
		name        string
		want        string
	}{
		{4, 0, "IMG_0012.jpg", "0012.jpg"},
		{0, 2, "report99.txt", "report.txt"},
		{2, 2, "abcdef.txt", "cd.txt"},
		// Trimming more than exists empties the base rather than
		// panicking.
		{99, 0, "abc.txt", ".txt"},
	}
	for _, c := range cases {
		got, err := Rename(Rules{TrimFront: c.front, TrimBack: c.back}, c.name, 0)
		if err != nil {
			t.Fatalf("Rename: %v", err)
		}
		if got != c.want {
			t.Errorf("trim(%d,%d) on %q = %q, want %q", c.front, c.back, c.name, got, c.want)
		}
	}
}

func TestTrimTreatsANegativeCountAsZero(t *testing.T) {
	got, err := Rename(Rules{TrimFront: -3, TrimBack: -1}, "report.txt", 0)
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got != "report.txt" {
		t.Errorf("got %q, want the name untouched", got)
	}
}

func TestTrimIsRuneAware(t *testing.T) {
	// "café" is 4 runes but 5 bytes (é is 2 bytes in UTF-8) — trimming
	// one character off the back must drop the é whole, not a stray byte
	// of it.
	got, err := Rename(Rules{TrimBack: 1}, "café.txt", 0)
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got != "caf.txt" {
		t.Errorf("got %q", got)
	}
}

func TestNumbering(t *testing.T) {
	rules := Rules{NumberPosition: NumberSuffix, NumberStart: 1, NumberStep: 1, NumberDigits: 3}
	for i, want := range []string{"photo-001.jpg", "photo-002.jpg", "photo-003.jpg"} {
		got, err := Rename(rules, "photo.jpg", i)
		if err != nil {
			t.Fatalf("Rename: %v", err)
		}
		if got != want {
			t.Errorf("index %d: got %q, want %q", i, got, want)
		}
	}
}

func TestNumberingPrefix(t *testing.T) {
	rules := Rules{NumberPosition: NumberPrefix, NumberStart: 10, NumberStep: 5, NumberDigits: 2}
	got, err := Rename(rules, "photo.jpg", 2) // 10 + 2*5 = 20
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got != "20-photo.jpg" {
		t.Errorf("got %q", got)
	}
}

func TestNumberingWidensPastItsOwnDigitsRatherThanTruncating(t *testing.T) {
	rules := Rules{NumberPosition: NumberSuffix, NumberStart: 99, NumberStep: 1, NumberDigits: 2}
	got, err := Rename(rules, "photo.jpg", 1) // 100, wider than 2 digits
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got != "photo-100.jpg" {
		t.Errorf("got %q, want the counter widened rather than clipped", got)
	}
}

func TestExtensionTransforms(t *testing.T) {
	cases := []struct {
		mode  ExtensionMode
		value string
		name  string
		want  string
	}{
		{ExtensionLower, "", "PHOTO.JPG", "PHOTO.jpg"},
		{ExtensionUpper, "", "photo.jpg", "photo.JPG"},
		{ExtensionRemove, "", "photo.jpg", "photo"},
		{ExtensionSetTo, "png", "photo.jpg", "photo.png"},
		{ExtensionSetTo, ".png", "photo.jpg", "photo.png"}, // leading dot optional
		{ExtensionSetTo, "", "photo.jpg", "photo"},
	}
	for _, c := range cases {
		rules := Rules{ExtensionMode: c.mode, ExtensionValue: c.value}
		got, err := Rename(rules, c.name, 0)
		if err != nil {
			t.Fatalf("Rename: %v", err)
		}
		if got != c.want {
			t.Errorf("extension mode %v on %q = %q, want %q", c.mode, c.name, got, c.want)
		}
	}
}

func TestExtensionSetToOnAnExtensionlessFileAddsOne(t *testing.T) {
	got, err := Rename(Rules{ExtensionMode: ExtensionSetTo, ExtensionValue: "txt"}, "README", 0)
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got != "README.txt" {
		t.Errorf("got %q", got)
	}
}

func TestStepsComposeInTheDocumentedOrder(t *testing.T) {
	// find/replace, then case, then trim, then numbering, then
	// extension — all five at once, each doing visibly one job.
	rules := Rules{
		Find: "vacation", Replace: "trip",
		Case:           CaseUpper,
		TrimBack:       0,
		NumberPosition: NumberSuffix, NumberStart: 1, NumberDigits: 2,
		ExtensionMode: ExtensionLower,
	}
	got, err := Rename(rules, "vacation.JPG", 0)
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got != "TRIP-01.jpg" {
		t.Errorf("got %q, want %q", got, "TRIP-01.jpg")
	}
}
