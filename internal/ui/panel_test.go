package ui

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/rivo/tview"
)

func TestBuildHeaderSpans(t *testing.T) {
	text, spans := buildHeaderSpans("/a/bb/c")

	wantText := " ~ < >  /a/bb/c"
	if text != wantText {
		t.Fatalf("text = %q, want %q", text, wantText)
	}

	want := []headerSpan{
		{start: 1, end: 2, action: actionHome},
		{start: 3, end: 4, action: actionBack},
		{start: 5, end: 6, action: actionForward},
		{start: 8, end: 9, action: actionNavigate, target: "/"},
		{start: 9, end: 10, action: actionNavigate, target: "/a"},
		{start: 11, end: 13, action: actionNavigate, target: "/a/bb"},
		{start: 14, end: 15, action: actionNavigate, target: "/a/bb/c"},
	}

	if len(spans) != len(want) {
		t.Fatalf("got %d spans, want %d: %+v", len(spans), len(want), spans)
	}
	for i := range want {
		if spans[i] != want[i] {
			t.Errorf("span %d = %+v, want %+v", i, spans[i], want[i])
		}
	}

	// Every path span's slice of text must equal its own last path
	// component (or "/" for the root span) — this is what makes clicking
	// a name actually correspond to what's drawn under the cursor.
	runes := []rune(text)
	for _, s := range spans {
		if s.action != actionNavigate {
			continue
		}
		got := string(runes[s.start:s.end])
		want := s.target
		if s.target != "/" {
			parts := []rune(s.target)
			for i := len(parts) - 1; i >= 0; i-- {
				if parts[i] == '/' {
					want = string(parts[i+1:])
					break
				}
			}
		}
		if got != want {
			t.Errorf("span %+v covers text %q, want %q", s, got, want)
		}
	}
}

func TestBuildHeaderSpansRoot(t *testing.T) {
	text, spans := buildHeaderSpans("/")

	wantText := " ~ < >  /"
	if text != wantText {
		t.Fatalf("text = %q, want %q", text, wantText)
	}

	// 3 buttons + the root span.
	if len(spans) != 4 {
		t.Fatalf("got %d spans, want 4: %+v", len(spans), spans)
	}
	root := spans[len(spans)-1]
	if root != (headerSpan{start: 8, end: 9, action: actionNavigate, target: "/"}) {
		t.Errorf("root span = %+v, want the trailing '/' span", root)
	}
}

// fixtureDir creates a directory with a known set of entries used by the
// completion tests below.
func fixtureDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	for _, name := range []string{"apple.txt", "apricot.txt", "banana.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "app-data"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCompletions(t *testing.T) {
	dir := fixtureDir(t)

	var p Panel // absolute input needs no p.path; see TestCompletionsRelative
	got := p.completions(dir + "/ap")
	sort.Strings(got)

	want := []string{dir + "/app-data/", dir + "/apple.txt", dir + "/apricot.txt"}
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestCompletionsRelative pins the fix for completions resolving against
// the process's working directory instead of the directory on screen.
func TestCompletionsRelative(t *testing.T) {
	p := Panel{path: fixtureDir(t)}

	got := p.completions("ban")
	if len(got) != 1 || got[0] != "banana.txt" {
		t.Fatalf("got %v, want [banana.txt] resolved against the panel's own path", got)
	}
}

// TestCompletionsKeepTypedForm checks that completing doesn't rewrite the
// field into a resolved absolute path the user never typed.
func TestCompletionsKeepTypedForm(t *testing.T) {
	p := Panel{path: fixtureDir(t)}

	got := p.completions("./ban")
	if len(got) != 1 || got[0] != "./banana.txt" {
		t.Fatalf("got %v, want [./banana.txt] with the typed prefix preserved", got)
	}
}

func TestCompletionsNoMatch(t *testing.T) {
	p := Panel{path: fixtureDir(t)}

	if got := p.completions("zz"); len(got) != 0 {
		t.Errorf("got %v, want no matches", got)
	}
}

func TestLongestCommonPrefix(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{"none", nil, ""},
		{"single value completes fully", []string{"banana.txt"}, "banana.txt"},
		{"common prefix", []string{"apple.txt", "apricot.txt"}, "ap"},
		{"one is a prefix of the other", []string{"app", "apple"}, "app"},
		{"nothing in common", []string{"apple", "banana"}, ""},
		// Compared by rune, so a multi-byte character is never cut in half.
		{"multi-byte", []string{"äpfel", "äpril"}, "äp"},
	}

	for _, tt := range tests {
		if got := longestCommonPrefix(tt.values); got != tt.want {
			t.Errorf("%s: longestCommonPrefix(%q) = %q, want %q", tt.name, tt.values, got, tt.want)
		}
	}
}

func TestResolvePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory available: %v", err)
	}

	p := Panel{path: "/etc"}

	tests := []struct {
		input string
		want  string
	}{
		{"/usr/local", "/usr/local"},         // absolute stays put
		{"hosts", "/etc/hosts"},              // relative to the panel, not the cwd
		{"../var", "/var"},                   // cleaned, still panel-relative
		{"~", home},                          // bare tilde
		{"~/Documents", home + "/Documents"}, // tilde prefix
		{"~notauser", "/etc/~notauser"},      // only "~" and "~/" expand
	}

	for _, tt := range tests {
		if got := p.resolvePath(tt.input); got != tt.want {
			t.Errorf("resolvePath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCheckboxText(t *testing.T) {
	if got := checkboxText(false); got != "○" {
		t.Errorf("checkboxText(false) = %q, want %q", got, "○")
	}
	if got := checkboxText(true); got != "●" {
		t.Errorf("checkboxText(true) = %q, want %q", got, "●")
	}
}

// TestPanelLoadPopulatesTable checks that load() builds one table row per
// entry, in the same directories-first order ListDir already guarantees,
// each carrying the rowRef that the rest of the table logic (activateRow,
// toggleCheckbox, RowAt) depends on.
func TestPanelLoadPopulatesTable(t *testing.T) {
	dir := fixtureDir(t) // app-data/, apple.txt, apricot.txt, banana.txt

	p, err := NewPanel(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	// dir has a parent, so row 0 is "..".
	wantNames := []string{"..", "app-data", "apple.txt", "apricot.txt", "banana.txt"}
	wantDirs := []bool{true, true, false, false, false}

	if got := p.table.GetRowCount(); got != len(wantNames) {
		t.Fatalf("got %d rows, want %d", got, len(wantNames))
	}

	for row, wantName := range wantNames {
		ref, ok := p.rowRef(row)
		if !ok {
			t.Fatalf("row %d: no rowRef", row)
		}
		if ref.name != wantName {
			t.Errorf("row %d: name = %q, want %q", row, ref.name, wantName)
		}
		if ref.isDir != wantDirs[row] {
			t.Errorf("row %d (%s): isDir = %v, want %v", row, ref.name, ref.isDir, wantDirs[row])
		}
	}
}

func TestToggleCheckbox(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	// Row 0 is "..", which addRow marks not checkable.
	p.toggleCheckbox(0)
	if len(p.selected) != 0 {
		t.Errorf("toggling \"..\" should not select anything, got %v", p.selected)
	}
	if cell := p.table.GetCell(0, colCheckbox); cell.Text != " " {
		t.Errorf("\"..\" checkbox cell = %q, want blank", cell.Text)
	}

	// Row 1 (app-data) is a normal, checkable entry.
	ref, ok := p.rowRef(1)
	if !ok {
		t.Fatal("row 1: no rowRef")
	}

	p.toggleCheckbox(1)
	if !p.selected[ref.path] {
		t.Errorf("expected %q to be selected after toggling", ref.path)
	}
	if cell := p.table.GetCell(1, colCheckbox); cell.Text != "●" {
		t.Errorf("checkbox cell = %q, want ●", cell.Text)
	}

	p.toggleCheckbox(1)
	if p.selected[ref.path] {
		t.Errorf("expected %q to be unselected after toggling twice", ref.path)
	}
	if cell := p.table.GetCell(1, colCheckbox); cell.Text != "○" {
		t.Errorf("checkbox cell = %q, want ○", cell.Text)
	}
}

// TestPanelLoadResetsSelection pins the documented behavior that
// selection is scoped to the directory on screen: navigating away and
// loading a new directory must not carry old checkmarks forward.
func TestPanelLoadResetsSelection(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	p.toggleCheckbox(1)
	if len(p.selected) != 1 {
		t.Fatalf("setup: expected 1 selected entry, got %d", len(p.selected))
	}

	if err := p.load(dir); err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(p.selected) != 0 {
		t.Errorf("expected selection to be cleared after load, got %v", p.selected)
	}
}
