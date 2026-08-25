package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/search"
)

func TestBuildHeaderSpans(t *testing.T) {
	text, spans := buildHeaderSpans("/a/bb/c")

	wantText := " ^ ~ < >  /a/bb/c"
	if text != wantText {
		t.Fatalf("text = %q, want %q", text, wantText)
	}

	want := []headerSpan{
		{start: 1, end: 2, action: actionStart},
		{start: 3, end: 4, action: actionHome},
		{start: 5, end: 6, action: actionBack},
		{start: 7, end: 8, action: actionForward},
		{start: 10, end: 11, action: actionNavigate, target: "/"},
		{start: 11, end: 12, action: actionNavigate, target: "/a"},
		{start: 13, end: 15, action: actionNavigate, target: "/a/bb"},
		{start: 16, end: 17, action: actionNavigate, target: "/a/bb/c"},
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

	wantText := " ^ ~ < >  /"
	if text != wantText {
		t.Fatalf("text = %q, want %q", text, wantText)
	}

	// 4 buttons + the root span.
	if len(spans) != 5 {
		t.Fatalf("got %d spans, want 5: %+v", len(spans), spans)
	}
	root := spans[len(spans)-1]
	if root != (headerSpan{start: 10, end: 11, action: actionNavigate, target: "/"}) {
		t.Errorf("root span = %+v, want the trailing '/' span", root)
	}
}

// TestBuildHeaderSpansAccountsForWideCharacters pins the fix for the
// user's own report: a directory name containing double-width (e.g.
// CJK) characters must advance the column count by its real terminal
// width, not its rune count, or every span after it (and the click
// target it maps to) drifts out of alignment with what's actually drawn
// on screen. "文档" is 2 runes but 4 terminal columns.
func TestBuildHeaderSpansAccountsForWideCharacters(t *testing.T) {
	text, spans := buildHeaderSpans("/文档/c")

	wantText := " ^ ~ < >  /文档/c"
	if text != wantText {
		t.Fatalf("text = %q, want %q", text, wantText)
	}

	want := []headerSpan{
		{start: 1, end: 2, action: actionStart},
		{start: 3, end: 4, action: actionHome},
		{start: 5, end: 6, action: actionBack},
		{start: 7, end: 8, action: actionForward},
		{start: 10, end: 11, action: actionNavigate, target: "/"},
		{start: 11, end: 15, action: actionNavigate, target: "/文档"},
		{start: 16, end: 17, action: actionNavigate, target: "/文档/c"},
	}
	if len(spans) != len(want) {
		t.Fatalf("got %d spans, want %d: %+v", len(spans), len(want), spans)
	}
	for i := range want {
		if spans[i] != want[i] {
			t.Errorf("span %d = %+v, want %+v", i, spans[i], want[i])
		}
	}
}

// TestStartButtonReturnsToLaunchDirectory pins the Start button's
// contract: no matter how far navigation has wandered since, it always
// returns to the directory the Panel was first opened at (history[0]),
// distinct from the OS home directory the Home button uses.
func TestStartButtonReturnsToLaunchDirectory(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	sub := filepath.Join(dir, "app-data")
	if err := p.navigate(sub); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if p.path != sub {
		t.Fatalf("setup: p.path = %q, want %q", p.path, sub)
	}

	p.runHeaderAction(headerSpan{action: actionStart})
	if p.path != dir {
		t.Errorf("after Start: p.path = %q, want launch directory %q", p.path, dir)
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

// TestDirCompletionsExcludesFiles pins dirCompletions' own first
// difference from completions — per the user's own explicit request:
// apple.txt/apricot.txt (files, matching "ap" too) are excluded
// outright, leaving only the one real directory that starts with it.
func TestDirCompletionsExcludesFiles(t *testing.T) {
	dir := fixtureDir(t) // apple.txt, apricot.txt, banana.txt, app-data/

	var p Panel
	got := p.dirCompletions(dir + "/ap")

	want := []string{dir + "/app-data/"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("got %v, want %v (files excluded)", got, want)
	}
}

// TestDirCompletionsIsCaseSensitive pins dirCompletions' own second
// difference from completions — per the user's own explicit request: a
// lowercase prefix matches only a same-case directory, not one
// differing purely in case.
func TestDirCompletionsIsCaseSensitive(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "Foo"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "foo"), 0755); err != nil {
		// A case-insensitive-but-preserving filesystem (macOS' own
		// default APFS mode, unlike Linux' usual ext4/xfs/btrfs) treats
		// "Foo" and "foo" as the very same entry — "Foo" already exists
		// by the time this one runs, so this isn't a real failure, just
		// this test's own premise (two directories differing only in
		// case coexisting) not holding on this filesystem.
		t.Skipf("can't create \"foo\" alongside \"Foo\" — case-insensitive filesystem? %v", err)
	}

	var p Panel
	got := p.dirCompletions(dir + "/F")

	want := []string{dir + "/Foo/"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("got %v, want %v (\"foo/\" differs only in case, shouldn't match \"F\")", got, want)
	}
}

// TestDirCompletionsIncludesDirSymlinks pins that a symlink resolving
// to a directory is treated the same as a real directory (Entry.IsDir
// already accounts for this — see its own doc comment), not excluded
// alongside plain files.
func TestDirCompletionsIncludesDirSymlinks(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "realdir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "realdir"), filepath.Join(dir, "linkdir")); err != nil {
		t.Fatal(err)
	}

	var p Panel
	got := p.dirCompletions(dir + "/link")

	want := []string{dir + "/linkdir/"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("got %v, want %v (a directory symlink should match like a real directory)", got, want)
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
		// Case-insensitive comparison (matching completions' own
		// case-insensitive matching — see this func's own doc comment):
		// these two only share "Download" case-insensitively, diverging
		// at the 9th character ('s' vs '-') — a case-sensitive compare
		// would have collapsed this all the way back to "" the moment it
		// hit the very first differing-case letter ('D' vs 'd'), a real
		// bug this pins the fix for.
		{"case-insensitive divergence", []string{"Downloads", "download-thing.sh"}, "Download"},
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

	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
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
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
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

func TestFilterHidden(t *testing.T) {
	entries := []fsops.Entry{
		{Name: ".hidden"},
		{Name: "visible.txt"},
		{Name: ".git", IsDir: true, Type: fsops.TypeDir},
		{Name: "normal", IsDir: true, Type: fsops.TypeDir},
	}

	got := filterHidden(entries)
	var names []string
	for _, e := range got {
		names = append(names, e.Name)
	}

	want := []string{"visible.txt", "normal"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("names[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestApplySortPreferenceByName(t *testing.T) {
	entries := []fsops.Entry{
		{Name: "Alpha", IsDir: true},
		{Name: "Omega", IsDir: true},
		{Name: "beta.txt"},
		{Name: "zeta.txt"},
	}

	applySortPreference(entries, sortByName, true)

	// Reversed within each group independently — directories still come
	// first, just Z-A instead of A-Z within that group, same for files.
	want := []string{"Omega", "Alpha", "zeta.txt", "beta.txt"}
	for i, w := range want {
		if entries[i].Name != w {
			t.Errorf("entries[%d].Name = %q, want %q", i, entries[i].Name, w)
		}
	}
}

func TestApplySortPreferenceEdgeCases(t *testing.T) {
	// All one group (no files at all) must not panic or misbehave at the
	// split boundary.
	allDirs := []fsops.Entry{{Name: "a", IsDir: true}, {Name: "b", IsDir: true}}
	applySortPreference(allDirs, sortByName, true)
	if allDirs[0].Name != "b" || allDirs[1].Name != "a" {
		t.Errorf("allDirs = %v, want [b a]", allDirs)
	}

	var empty []fsops.Entry
	applySortPreference(empty, sortByName, true) // must not panic
}

func TestApplySortPreferenceBySize(t *testing.T) {
	entries := []fsops.Entry{
		{Name: "big.txt", Size: 300},
		{Name: "small.txt", Size: 100},
		{Name: "medium.txt", Size: 200},
	}

	applySortPreference(entries, sortBySize, false)
	want := []string{"small.txt", "medium.txt", "big.txt"}
	for i, w := range want {
		if entries[i].Name != w {
			t.Errorf("ascending: entries[%d].Name = %q, want %q", i, entries[i].Name, w)
		}
	}

	applySortPreference(entries, sortBySize, true)
	wantDesc := []string{"big.txt", "medium.txt", "small.txt"}
	for i, w := range wantDesc {
		if entries[i].Name != w {
			t.Errorf("descending: entries[%d].Name = %q, want %q", i, entries[i].Name, w)
		}
	}
}

func TestApplySortPreferenceByModified(t *testing.T) {
	older := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	entries := []fsops.Entry{
		{Name: "b.txt", ModTime: newer},
		{Name: "a.txt", ModTime: older},
	}

	applySortPreference(entries, sortByModified, false)
	if entries[0].Name != "a.txt" || entries[1].Name != "b.txt" {
		t.Errorf("ascending by ModTime: got %v, %v, want a.txt, b.txt", entries[0].Name, entries[1].Name)
	}
}

// TestApplySortPreferenceTiesBreakByName pins that entries sharing the
// same size (or mtime) fall back to name order rather than an arbitrary
// one — same case-insensitive comparison ListDir itself already sorts
// by.
func TestApplySortPreferenceTiesBreakByName(t *testing.T) {
	entries := []fsops.Entry{
		{Name: "zeta.txt", Size: 100},
		{Name: "alpha.txt", Size: 100},
	}
	applySortPreference(entries, sortBySize, false)
	if entries[0].Name != "alpha.txt" || entries[1].Name != "zeta.txt" {
		t.Errorf("tied sizes should fall back to name order, got %v, %v", entries[0].Name, entries[1].Name)
	}
}

func TestFormatSizeCell(t *testing.T) {
	tests := []struct {
		size      int64
		bytesMode bool
		want      string
	}{
		{2184, false, humanSize(2184)},
		{2184, true, "2184"},
	}
	for _, tt := range tests {
		got := formatSizeCell(tt.size, tt.bytesMode)
		if len([]rune(got)) != sizeColumnWidth {
			t.Errorf("formatSizeCell(%d, %v) = %q, width %d, want %d", tt.size, tt.bytesMode, got, len([]rune(got)), sizeColumnWidth)
		}
		if strings.TrimSpace(got) != tt.want {
			t.Errorf("formatSizeCell(%d, %v) = %q, want (trimmed) %q", tt.size, tt.bytesMode, got, tt.want)
		}
	}
}

func TestFormatModTimeCell(t *testing.T) {
	when := time.Date(2026, time.August, 19, 9, 12, 3, 0, time.Local)

	formatted := formatModTimeCell(when, false)
	if len([]rune(formatted)) != modColumnWidth {
		t.Errorf("formatted width = %d, want %d", len([]rune(formatted)), modColumnWidth)
	}
	if strings.TrimSpace(formatted) != "2026-08-19 09:12:03" {
		t.Errorf("formatModTimeCell(formatted) = %q, want %q", formatted, "2026-08-19 09:12:03")
	}

	unix := formatModTimeCell(when, true)
	if len([]rune(unix)) != modColumnWidth {
		t.Errorf("unix width = %d, want %d", len([]rune(unix)), modColumnWidth)
	}
	if strings.TrimSpace(unix) != strconv.FormatInt(when.Unix(), 10) {
		t.Errorf("formatModTimeCell(unix) = %q, want %q", unix, strconv.FormatInt(when.Unix(), 10))
	}
}

// TestSetSortKeySwitchingColumnStartsAscending pins the "click a new
// column: always ascending" half of setSortKey's convention — only
// clicking the *already active* column reverses direction (covered by
// TestSetSortKeyReversesOrderAndPersistsAcrossNavigation).
func TestSetSortKeySwitchingColumnStartsAscending(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	p.setSortKey(sortBySize)
	if p.sortKey != sortBySize || p.sortDescending {
		t.Errorf("after switching to sortBySize: key=%v descending=%v, want sortBySize, false", p.sortKey, p.sortDescending)
	}

	p.setSortKey(sortBySize) // clicking the same column again reverses
	if !p.sortDescending {
		t.Error("clicking the already-active column should reverse direction")
	}

	p.setSortKey(sortByModified) // switching to a different column resets to ascending
	if p.sortKey != sortByModified || p.sortDescending {
		t.Errorf("after switching to sortByModified: key=%v descending=%v, want sortByModified, false", p.sortKey, p.sortDescending)
	}
}

// TestBuildColumnHeaderShowsArrowOnActiveColumnOnly pins that the sort
// arrow appears only next to the current sort key's own label.
func TestBuildColumnHeaderShowsArrowOnActiveColumnOnly(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	nameText := p.columnHeader.GetCell(0, colName).Text
	if !strings.Contains(nameText, "↑") {
		t.Errorf("Name header = %q, want it to carry the sort arrow (sortByName is the default)", nameText)
	}
	sizeText := p.columnHeader.GetCell(0, colSize).Text
	if strings.Contains(sizeText, "↑") || strings.Contains(sizeText, "↓") {
		t.Errorf("Size header = %q, should not carry an arrow while Name is the active sort", sizeText)
	}

	p.setSortKey(sortBySize)

	nameText = p.columnHeader.GetCell(0, colName).Text
	if strings.Contains(nameText, "↑") || strings.Contains(nameText, "↓") {
		t.Errorf("Name header = %q, should lose its arrow once Size becomes the active sort", nameText)
	}
	sizeText = p.columnHeader.GetCell(0, colSize).Text
	if !strings.Contains(sizeText, "↑") {
		t.Errorf("Size header = %q, want it to carry the arrow now that it's the active sort", sizeText)
	}
}

// TestColumnHeaderClickSortsBySize is a real click, dispatched through
// the column header's own MouseHandler the way an actual click arrives
// — the same rigor TestRenamePositionsOverRightClickedRow and the
// Properties multi-row click tests use, for exactly the same reason:
// this is new, real coordinate-dependent click routing, not something
// worth trusting to direct method calls alone.
func TestColumnHeaderClickSortsBySize(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte("aaaaaaaaaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "small.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(120, 24)
	p.columnHeader.SetRect(0, 0, 120, 1)
	p.columnHeader.Draw(screen)

	x := -1
	for tryX := 0; tryX < 120; tryX++ {
		if r, c := p.columnHeader.CellAt(tryX, 0); r == 0 && c == colSize {
			x = tryX
			break
		}
	}
	if x < 0 {
		t.Fatal("could not locate the Size header cell's screen position")
	}

	handler := p.columnHeader.MouseHandler()
	handler(tview.MouseLeftClick, tcell.NewEventMouse(x, 0, tcell.Button1, 0), func(tview.Primitive) {})

	if p.sortKey != sortBySize {
		t.Errorf("sortKey = %v after clicking the Size header, want sortBySize", p.sortKey)
	}
	if ref, _ := p.rowRef(1); ref.name != "small.txt" {
		t.Errorf("row 1 = %q, want small.txt (ascending by size)", ref.name)
	}
}

func TestAllSelected(t *testing.T) {
	dir := fixtureDir(t) // app-data/, apple.txt, apricot.txt, banana.txt
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	if p.allSelected() {
		t.Error("allSelected() = true, want false: nothing is checked yet")
	}

	p.selectAll()
	if !p.allSelected() {
		t.Error("allSelected() = false after selectAll(), want true")
	}

	p.toggleCheckbox(1) // unchecks one entry
	if p.allSelected() {
		t.Error("allSelected() = true after unchecking one entry, want false")
	}
}

// TestToggleSelectAllViaHeader pins the header checkbox's own action:
// selects everything if it isn't all selected yet, deselects everything
// if it already is — the same two actions Select all/Deselect all in the
// context menu already offer.
func TestToggleSelectAllViaHeader(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	p.toggleSelectAllViaHeader()
	if !p.allSelected() {
		t.Error("first click should have selected everything")
	}

	p.toggleSelectAllViaHeader()
	if len(p.selected) != 0 {
		t.Errorf("second click should have deselected everything, got %v", p.selected)
	}
}

// TestRefreshHeaderCheckboxStaysInSyncWithIndividualToggles pins that the
// column header's checkbox glyph updates from every selection path, not
// just toggleSelectAllViaHeader itself — setChecked is the one place all
// of them funnel through (see its own doc comment).
func TestRefreshHeaderCheckboxStaysInSyncWithIndividualToggles(t *testing.T) {
	dir := fixtureDir(t) // app-data/, apple.txt, apricot.txt, banana.txt: rows 1-4
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	headerText := func() string { return p.columnHeader.GetCell(0, colCheckbox).Text }

	if got := headerText(); got != checkboxText(false) {
		t.Fatalf("setup: header checkbox = %q, want unchecked", got)
	}

	for row := 1; row <= 4; row++ {
		p.toggleCheckbox(row)
	}
	if got := headerText(); got != checkboxText(true) {
		t.Errorf("header checkbox = %q after checking every row individually, want checked", got)
	}

	p.toggleCheckbox(2) // uncheck just one
	if got := headerText(); got != checkboxText(false) {
		t.Errorf("header checkbox = %q after unchecking one row, want unchecked again", got)
	}
}

// TestColumnHeaderCheckboxClickSelectsAll is the header checkbox's real-
// click counterpart to TestColumnHeaderClickSortsBySize — same rigor,
// same reason: real coordinate-dependent click routing.
func TestColumnHeaderCheckboxClickSelectsAll(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(120, 24)
	p.columnHeader.SetRect(0, 0, 120, 1)
	p.columnHeader.Draw(screen)

	x, _, _ := p.columnHeader.GetCell(0, colCheckbox).GetLastPosition()

	handler := p.columnHeader.MouseHandler()
	handler(tview.MouseLeftClick, tcell.NewEventMouse(x, 0, tcell.Button1, 0), func(tview.Primitive) {})

	if !p.allSelected() {
		t.Error("clicking the header checkbox should have selected every row")
	}
}

// TestColumnSeparatorsPresent pins that the "│" divider cells (see
// columnSeparator) are populated in both the column header and every
// data row, including "..", which has no real Entry behind it (unlike
// Size/Modified, there's nothing data-dependent about a separator to
// blank out for that row).
func TestColumnSeparatorsPresent(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	if got := p.columnHeader.GetCell(0, colSizeSep).Text; got != "│" {
		t.Errorf("header colSizeSep = %q, want %q", got, "│")
	}
	if got := p.columnHeader.GetCell(0, colModifiedSep).Text; got != "│" {
		t.Errorf("header colModifiedSep = %q, want %q", got, "│")
	}

	for row := 0; row < p.table.GetRowCount(); row++ {
		if got := p.table.GetCell(row, colSizeSep).Text; got != "│" {
			t.Errorf("row %d colSizeSep = %q, want %q", row, got, "│")
		}
		if got := p.table.GetCell(row, colModifiedSep).Text; got != "│" {
			t.Errorf("row %d colModifiedSep = %q, want %q", row, got, "│")
		}
	}
}

// TestLoadHidesDotfilesByDefault pins showHidden's default: dotfiles are
// filtered out of the listing entirely, not just skipped over.
func TestLoadShowsDotfilesByDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".hidden"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "visible.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	found := false
	for row := 0; row < p.table.GetRowCount(); row++ {
		if ref, ok := p.rowRef(row); ok && ref.name == ".hidden" {
			found = true
		}
	}
	if !found {
		t.Error(".hidden should be listed by default")
	}
}

// TestToggleHiddenOffHidesDotfiles pins the other half: turning
// showHidden off and reloading filters dotfiles back out.
func TestToggleHiddenOffHidesDotfiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".hidden"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	p.showHidden = false
	if err := p.load(dir); err != nil {
		t.Fatalf("load: %v", err)
	}

	for row := 0; row < p.table.GetRowCount(); row++ {
		if ref, ok := p.rowRef(row); ok && ref.name == ".hidden" {
			t.Error(".hidden should not be listed once showHidden is false")
		}
	}
}

// TestSelectAllExcludesHiddenFiles pins the explicit requirement that
// selection actions never touch a currently-hidden dotfile — automatic
// here since a hidden entry never gets a row in the first place.
func TestSelectAllExcludesHiddenFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".hidden"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "visible.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	// Dotfiles are shown by default now — explicitly hide them, since
	// that's the condition this test is actually about: selectAll must
	// never touch an entry that's currently filtered out.
	p.showHidden = false
	if err := p.load(dir); err != nil {
		t.Fatalf("load: %v", err)
	}

	p.selectAll()

	for path := range p.selected {
		if filepath.Base(path) == ".hidden" {
			t.Error("selectAll should not have selected a hidden entry")
		}
	}
	if len(p.selected) != 1 { // just visible.txt
		t.Errorf("selected = %d, want 1", len(p.selected))
	}
}

// TestSelectAllIncludesHiddenFilesWhenShown is the positive-case
// counterpart: once dotfiles are shown (the default), they're ordinary
// rows like any other and selectAll does include them.
func TestSelectAllIncludesHiddenFilesWhenShown(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".hidden"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "visible.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	p.selectAll()

	if len(p.selected) != 2 {
		t.Errorf("selected = %d, want 2 (both .hidden and visible.txt)", len(p.selected))
	}
}

// TestSetSortKeyReversesOrderAndPersistsAcrossNavigation exercises the
// column header's actual click action (setSortKey), and pins that the
// preference is session-scoped (survives navigating away and back), not
// reset on every load like the checkbox selection is.
func TestSetSortKeyReversesOrderAndPersistsAcrossNavigation(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "Alpha")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "Omega"), 0o755); err != nil {
		t.Fatal(err)
	}

	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	// Row 0 is "..", row 1 is the first directory ascending (Alpha).
	if ref, _ := p.rowRef(1); ref.name != "Alpha" {
		t.Fatalf("setup: row 1 = %q, want Alpha", ref.name)
	}

	p.setSortKey(sortByName) // already the active key: this reverses direction
	if ref, _ := p.rowRef(1); ref.name != "Omega" {
		t.Errorf("after sort toggle: row 1 = %q, want Omega", ref.name)
	}

	if err := p.navigate(sub); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := p.navigate(dir); err != nil {
		t.Fatalf("navigate back: %v", err)
	}
	if ref, _ := p.rowRef(1); ref.name != "Omega" {
		t.Errorf("after navigating away and back: row 1 = %q, want Omega (descending should have stuck)", ref.name)
	}
}

func TestTypeGlyph(t *testing.T) {
	tests := []struct {
		name string
		ref  rowRef
		want byte
	}{
		{"plain file", rowRef{entryType: fsops.TypeFile}, ' '},
		{"executable file", rowRef{entryType: fsops.TypeFile, mode: 0o755}, '*'},
		{"executable bit for group only still counts", rowRef{entryType: fsops.TypeFile, mode: 0o650}, '*'},
		{"directory", rowRef{entryType: fsops.TypeDir}, '/'},
		{"symlink to directory", rowRef{entryType: fsops.TypeSymlinkDir}, '~'},
		{"symlink to file", rowRef{entryType: fsops.TypeSymlinkFile}, '@'},
		{"broken symlink", rowRef{entryType: fsops.TypeSymlinkBroken}, '!'},
		{"socket", rowRef{entryType: fsops.TypeSocket}, '='},
		{"FIFO", rowRef{entryType: fsops.TypeFIFO}, '|'},
		{"character device", rowRef{entryType: fsops.TypeCharDevice}, '-'},
		{"block device", rowRef{entryType: fsops.TypeBlockDevice}, '+'},
	}

	for _, tt := range tests {
		if got := typeGlyph(tt.ref); got != tt.want {
			t.Errorf("%s: typeGlyph() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestEntryColor(t *testing.T) {
	p := Panel{theme: config.DefaultTheme().Resolve()}

	if got := p.entryColor(rowRef{entryType: fsops.TypeSymlinkBroken}); got != p.theme.EntryError {
		t.Errorf("broken symlink color = %v, want %v (EntryError)", got, p.theme.EntryError)
	}
	if got := p.entryColor(rowRef{entryType: fsops.TypeFile, mode: 0o755}); got != p.theme.EntryExecutable {
		t.Errorf("executable file color = %v, want %v (EntryExecutable)", got, p.theme.EntryExecutable)
	}
	if got := p.entryColor(rowRef{entryType: fsops.TypeFile, mode: 0o644}); got != p.theme.EntryNormal {
		t.Errorf("non-executable file color = %v, want %v (EntryNormal)", got, p.theme.EntryNormal)
	}
	if got := p.entryColor(rowRef{entryType: fsops.TypeDir}); got != p.theme.EntryNormal {
		t.Errorf("directory color = %v, want %v (EntryNormal)", got, p.theme.EntryNormal)
	}
}

func TestModifierGlyph(t *testing.T) {
	tests := []struct {
		name string
		ref  rowRef
		want byte
	}{
		{"plain file", rowRef{entryType: fsops.TypeFile}, ' '},
		{"hardlinked file", rowRef{entryType: fsops.TypeFile, nlink: 2}, '&'},
		{"single-link file with nlink=1", rowRef{entryType: fsops.TypeFile, nlink: 1}, ' '},
		{"mounted directory", rowRef{entryType: fsops.TypeDir, mountPoint: true}, '>'},
		{"mounted directory symlink", rowRef{entryType: fsops.TypeSymlinkDir, mountPoint: true}, '>'},
		{"ordinary directory", rowRef{entryType: fsops.TypeDir}, ' '},
		{"mountPoint flag ignored for a file (shouldn't happen, but stay defensive)", rowRef{entryType: fsops.TypeFile, mountPoint: true}, ' '},
	}

	for _, tt := range tests {
		if got := modifierGlyph(tt.ref); got != tt.want {
			t.Errorf("%s: modifierGlyph() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestAddRowRendersTypeAndModifierColumns is an end-to-end check, through
// a real ListDir + load, that the type and modifier glyphs land in their
// own separate table cells rather than being baked into the name text.
func TestAddRowRendersTypeAndModifierColumns(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link-to-file")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(target, filepath.Join(dir, "hardlinked.txt")); err != nil {
		t.Skipf("hard links not supported here: %v", err)
	}

	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	byName := make(map[string]int) // name -> row
	for row := 0; row < p.table.GetRowCount(); row++ {
		if ref, ok := p.rowRef(row); ok {
			byName[ref.name] = row
		}
	}

	linkRow, ok := byName["link-to-file"]
	if !ok {
		t.Fatal("link-to-file row not found")
	}
	if got := p.table.GetCell(linkRow, colType).Text; got != "@" {
		t.Errorf("link-to-file type cell = %q, want %q", got, "@")
	}
	if got := p.table.GetCell(linkRow, colName).Text; got != "link-to-file -> "+target {
		t.Errorf("link-to-file name cell = %q, want the inline arrow-target form", got)
	}

	hardlinkRow, ok := byName["hardlinked.txt"]
	if !ok {
		t.Fatal("hardlinked.txt row not found")
	}
	if got := p.table.GetCell(hardlinkRow, colModifier).Text; got != "&" {
		t.Errorf("hardlinked.txt modifier cell = %q, want %q", got, "&")
	}
	if got := p.table.GetCell(hardlinkRow, colType).Text; got != " " {
		t.Errorf("hardlinked.txt type cell = %q, want blank (a plain, non-executable file)", got)
	}
}

// TestActivateRowNavigatesIntoDirectorySymlink pins a behavior change
// that comes for free now that ListDir resolves symlinks to classify
// them (see fsops.Entry.IsDir's doc comment): Enter/click on a directory
// symlink navigates into it, the same as a real directory — it used to
// be treated as an inert file.
func TestActivateRowNavigatesIntoDirectorySymlink(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "target-dir")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "inside.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link-to-dir")
	if err := os.Symlink(targetDir, link); err != nil {
		t.Fatal(err)
	}

	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	row := -1
	for i := 0; i < p.table.GetRowCount(); i++ {
		if ref, ok := p.rowRef(i); ok && ref.name == "link-to-dir" {
			row = i
		}
	}
	if row < 0 {
		t.Fatal("link-to-dir row not found")
	}

	p.activateRow(row)

	if p.path != link {
		t.Errorf("p.path = %q after activating a directory symlink, want %q", p.path, link)
	}
	if _, ok := p.rowRef(1); !ok {
		t.Fatal("expected the symlinked directory's own contents to be listed")
	}
}

// TestSelectAllDeselectAll pins the context menu's "Select all"/
// "Deselect all": every checkable row ends up checked or unchecked, and
// the ".." row (not checkable) is left alone either way.
func TestSelectAllDeselectAll(t *testing.T) {
	dir := fixtureDir(t) // app-data/, apple.txt, apricot.txt, banana.txt
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	p.selectAll()
	if len(p.selected) != 4 { // everything except ".."
		t.Fatalf("selectAll: %d selected, want 4", len(p.selected))
	}
	if cell := p.table.GetCell(0, colCheckbox); cell.Text != " " {
		t.Errorf("\"..\" checkbox cell = %q after selectAll, want blank", cell.Text)
	}

	p.deselectAll()
	if len(p.selected) != 0 {
		t.Errorf("deselectAll: %d still selected, want 0", len(p.selected))
	}
}

// TestSelectByPattern pins the context menu's "Select +"/"Select -":
// glob-matching entries get checked or unchecked, matching filepath.Match
// semantics; an unmatched pattern is not an error, a malformed one is.
func TestSelectByPattern(t *testing.T) {
	dir := fixtureDir(t) // app-data/, apple.txt, apricot.txt, banana.txt
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	matched, err := p.selectByPattern("ap*", true)
	if err != nil {
		t.Fatalf("selectByPattern: %v", err)
	}
	if matched != 3 { // app-data, apple.txt, apricot.txt
		t.Errorf("matched = %d, want 3", matched)
	}
	if len(p.selected) != 3 {
		t.Errorf("selected = %d, want 3", len(p.selected))
	}

	matched, err = p.selectByPattern("apricot.txt", false)
	if err != nil {
		t.Fatalf("selectByPattern: %v", err)
	}
	if matched != 1 || len(p.selected) != 2 {
		t.Errorf("after unmarking apricot.txt: matched=%d selected=%d, want 1, 2", matched, len(p.selected))
	}

	if matched, err := p.selectByPattern("nothing-matches-this", true); err != nil || matched != 0 {
		t.Errorf("no match: matched=%d err=%v, want 0, nil", matched, err)
	}

	if _, err := p.selectByPattern("[", true); err == nil {
		t.Error("a malformed pattern should return an error")
	}
}

// TestSelectedPaths pins that SelectedPaths reflects exactly the checked
// rows, absolute paths, in whatever order the underlying map yields.
func TestSelectedPaths(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	if got := p.SelectedPaths(); len(got) != 0 {
		t.Fatalf("setup: SelectedPaths = %v, want empty", got)
	}

	p.toggleCheckbox(1)
	p.toggleCheckbox(2)
	ref1, _ := p.rowRef(1)
	ref2, _ := p.rowRef(2)

	got := p.SelectedPaths()
	want := map[string]bool{ref1.path: true, ref2.path: true}
	if len(got) != 2 {
		t.Fatalf("SelectedPaths = %v, want 2 entries", got)
	}
	for _, path := range got {
		if !want[path] {
			t.Errorf("SelectedPaths returned unexpected path %q", path)
		}
	}
}

// TestPanelLoadResetsSelection pins the documented behavior that
// selection is scoped to the directory on screen: navigating away and
// loading a new directory must not carry old checkmarks forward.
func TestPanelLoadResetsSelection(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
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

// TestNameCellRectExcludesCheckboxColumn pins the geometry
// Root.openRename relies on: the name cell's on-screen rect must start
// at or after the checkbox column ends, so positioning the rename field
// over exactly that rect leaves the checkbox visible instead of covering
// the whole row.
func TestNameCellRectExcludesCheckboxColumn(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(80, 24)
	p.SetRect(0, 0, 80, 24)
	p.Draw(screen)

	const row = 1
	checkboxX, _, checkboxWidth := p.table.GetCell(row, colCheckbox).GetLastPosition()
	nameX, _, nameWidth, ok := p.nameCellRect(row)
	if !ok {
		t.Fatalf("nameCellRect(%d) failed", row)
	}

	if nameX < checkboxX+checkboxWidth {
		t.Errorf("name cell x=%d overlaps the checkbox column (x=%d, width=%d)", nameX, checkboxX, checkboxWidth)
	}
	if nameWidth <= 0 {
		t.Error("name cell width should be positive")
	}
}

// TestLoadResetsCursorToTopOnNewDirectory pins the fix for the user's
// own report: entering a directory (as opposed to refreshing the one
// already on screen) always starts at the top row, not wherever the
// table's own selection happened to be left from a previous, unrelated
// listing — Table.Clear doesn't touch that itself, so without this fix
// it stayed put at whatever row index a completely different directory
// last had it on.
func TestLoadResetsCursorToTopOnNewDirectory(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.panel.focusRow(3) // move away from the top, simulating having browsed around first

	sub := filepath.Join(dir, "app-data")
	if err := r.panel.navigate(sub); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	row, _ := r.panel.table.GetSelection()
	if row != 0 {
		t.Errorf("selected row after navigating to a new directory = %d, want 0 (the top)", row)
	}
}

// TestLoadPreservesCursorOnSameDirectoryRefresh is
// TestLoadResetsCursorToTopOnNewDirectory's counterpart: reloading the
// directory already on screen (e.g. toggling hidden files) does not
// reset the cursor — only an actual move to a different directory does.
func TestLoadPreservesCursorOnSameDirectoryRefresh(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.panel.focusRow(2)
	if err := r.panel.load(r.panel.path); err != nil {
		t.Fatalf("load (refresh): %v", err)
	}

	row, _ := r.panel.table.GetSelection()
	if row != 2 {
		t.Errorf("selected row after a same-directory refresh = %d, want unchanged 2", row)
	}
}

// TestShowSearchResultsClearsTableAndEntersSearchMode pins
// showSearchResults' own basic contract: an empty table, searchMode
// true, and — critically — p.path itself left completely untouched
// (see its own doc comment on why every other Panel method depends on
// that).
func TestShowSearchResultsClearsTableAndEntersSearchMode(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	realPath := p.path

	p.showSearchResults()

	if !p.searchMode {
		t.Error("searchMode = false after showSearchResults, want true")
	}
	if got := p.table.GetRowCount(); got != 0 {
		t.Errorf("table has %d rows right after showSearchResults, want 0", got)
	}
	if p.path != realPath {
		t.Errorf("p.path = %q after showSearchResults, want unchanged %q", p.path, realPath)
	}
	if p.searchRestorePath != realPath {
		t.Errorf("searchRestorePath = %q, want %q", p.searchRestorePath, realPath)
	}
}

// TestAppendSearchResultClassifiesAndUsesFullPathAsName pins the two
// things appendSearchResult adds on top of a bare path string: real
// fsops classification (a symlink here, to prove it's not just
// defaulting to TypeFile) via DescribeEntry, and Name overwritten to
// the result's own full path rather than DescribeEntry's usual
// basename — see both functions' own doc comments.
func TestAppendSearchResultClassifiesAndUsesFullPathAsName(t *testing.T) {
	dir := fixtureDir(t)
	target := filepath.Join(dir, "apple.txt")
	link := filepath.Join(dir, "link-to-apple")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	p.showSearchResults()
	p.appendSearchResult(search.Result{Path: link})

	ref, ok := p.rowRef(0)
	if !ok {
		t.Fatal("no rowRef for the one appended result")
	}
	if ref.name != link {
		t.Errorf("name = %q, want the full path %q", ref.name, link)
	}
	if ref.path != link {
		t.Errorf("path = %q, want %q", ref.path, link)
	}
	if ref.entryType != fsops.TypeSymlinkFile {
		t.Errorf("entryType = %v, want TypeSymlinkFile — DescribeEntry should have classified this as a real symlink", ref.entryType)
	}
	if ref.linkTarget != target {
		t.Errorf("linkTarget = %q, want %q", ref.linkTarget, target)
	}
}

// TestAppendSearchResultSortsGroupedByFullPath pins the user's own
// stated reason for preferring full-path-as-name over a bare basename:
// results scattered across different directories sort coherently
// (directories-first, then alphabetically by full path) rather than
// being interleaved by basename alone.
func TestAppendSearchResultSortsGroupedByFullPath(t *testing.T) {
	dir := fixtureDir(t)
	sub := filepath.Join(dir, "app-data") // already exists, see fixtureDir
	for _, name := range []string{"zzz.txt", "aaa.txt"} {
		if err := os.WriteFile(filepath.Join(sub, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	subDir := filepath.Join(dir, "app-data", "nested")
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	p.showSearchResults()
	// Deliberately appended out of any sorted order.
	for _, path := range []string{
		filepath.Join(sub, "zzz.txt"),
		subDir,
		filepath.Join(sub, "aaa.txt"),
	} {
		p.appendSearchResult(search.Result{Path: path})
	}

	var gotOrder []string
	for row := 0; row < p.table.GetRowCount(); row++ {
		ref, _ := p.rowRef(row)
		gotOrder = append(gotOrder, ref.path)
	}
	wantOrder := []string{
		subDir, // the one directory result — directories always group first, same as a real listing
		filepath.Join(sub, "aaa.txt"),
		filepath.Join(sub, "zzz.txt"),
	}
	if len(gotOrder) != len(wantOrder) {
		t.Fatalf("got %d rows, want %d: %v", len(gotOrder), len(wantOrder), gotOrder)
	}
	for i := range wantOrder {
		if gotOrder[i] != wantOrder[i] {
			t.Errorf("row %d = %q, want %q (full order: got %v, want %v)", i, gotOrder[i], wantOrder[i], gotOrder, wantOrder)
		}
	}
}

// TestAppendSearchResultShowsLineAndTextForContentMatch pins a content
// (grep) match's own display text — "path:line: text" — while its
// underlying rowRef.path stays the real, bare path throughout: only
// display should ever carry the line/text suffix (see
// searchResultEntry's own doc comment), never what activateRow or the
// context menu actually acts on.
func TestAppendSearchResultShowsLineAndTextForContentMatch(t *testing.T) {
	dir := fixtureDir(t)
	target := filepath.Join(dir, "apple.txt")

	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	p.showSearchResults()
	p.appendSearchResult(search.Result{Path: target, Line: 42, Text: "needle found here"})

	ref, ok := p.rowRef(0)
	if !ok {
		t.Fatal("no rowRef for the one appended result")
	}
	if ref.path != target {
		t.Errorf("path = %q, want the bare real path %q", ref.path, target)
	}
	wantName := target + ":42: needle found here"
	if ref.name != wantName {
		t.Errorf("name = %q, want %q", ref.name, wantName)
	}
}

// TestAppendSearchResultAllowsSamePathTwiceForMultipleContentMatches
// pins the reason searchResultEntry carries display separately from
// Entry.Name at all: a content search without "First hit" checked can
// legitimately report the same file more than once, once per matching
// line — both must show up as their own row, each with its own
// distinct line/text, not collapse into one or silently overwrite each
// other.
func TestAppendSearchResultAllowsSamePathTwiceForMultipleContentMatches(t *testing.T) {
	dir := fixtureDir(t)
	target := filepath.Join(dir, "apple.txt")

	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	p.showSearchResults()
	p.appendSearchResult(search.Result{Path: target, Line: 3, Text: "first hit"})
	p.appendSearchResult(search.Result{Path: target, Line: 9, Text: "second hit"})

	if got := p.table.GetRowCount(); got != 2 {
		t.Fatalf("got %d rows for two matches in the same file, want 2", got)
	}
	var gotNames []string
	for row := 0; row < 2; row++ {
		ref, ok := p.rowRef(row)
		if !ok {
			t.Fatalf("no rowRef for row %d", row)
		}
		if ref.path != target {
			t.Errorf("row %d: path = %q, want %q", row, ref.path, target)
		}
		gotNames = append(gotNames, ref.name)
	}
	wantNames := []string{target + ":3: first hit", target + ":9: second hit"}
	for _, want := range wantNames {
		found := false
		for _, got := range gotNames {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("gotNames = %v, missing %q", gotNames, want)
		}
	}
}

// TestSetSearchStatusPaintsHeaderWithNoClickableSpans pins that the
// header, while showing search status text, has nothing left in
// headerSpans — see captureHeaderMouse's own searchMode guard, which
// depends on this to avoid falling through to openEdit.
func TestSetSearchStatusPaintsHeaderWithNoClickableSpans(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	p.showSearchResults()

	p.setSearchStatus("⠋ searching…")

	if got := p.header.GetText(true); got != "⠋ searching…" {
		t.Errorf("header text = %q, want %q", got, "⠋ searching…")
	}
	if len(p.headerSpans) != 0 {
		t.Errorf("headerSpans = %v after setSearchStatus, want empty", p.headerSpans)
	}
}

// TestSetSortKeyInSearchModeReRendersWithoutTouchingRealDirectory pins
// setSortKey's own searchMode branch: it re-sorts and redraws
// searchEntries in place (renderSearchEntries), never calling load()
// (which would fail here anyway, and would incorrectly reset
// p.path/cursor if it somehow didn't) — the result count before and
// after is the same, and p.path (never touched by search mode to begin
// with) stays whatever the real directory already was.
func TestSetSortKeyInSearchModeReRendersWithoutTouchingRealDirectory(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	realPath := p.path
	p.showSearchResults()
	p.appendSearchResult(search.Result{Path: filepath.Join(dir, "apple.txt")})
	p.appendSearchResult(search.Result{Path: filepath.Join(dir, "banana.txt")})

	p.setSortKey(sortBySize) // any key different from the default is enough to exercise the branch

	if !p.searchMode {
		t.Error("searchMode = false after a sort-key change, want still true")
	}
	if p.path != realPath {
		t.Errorf("p.path = %q after a sort-key change in search mode, want unchanged %q", p.path, realPath)
	}
	if got := p.table.GetRowCount(); got != 2 {
		t.Errorf("got %d rows after re-sorting, want still 2", got)
	}
}

// TestRenderSearchEntriesPreservesCheckedStateAcrossSort pins the fix
// renderSearchEntries needed on top of addRow's own usual behavior:
// addRow always draws a fresh checkbox unchecked (correct for load(),
// which resets p.selected right before calling it — see load's own
// doc comment), so a search-mode re-render (triggered by a new result
// streaming in, or a sort-key change) must explicitly restore each
// row's own checked glyph from p.selected afterward, or a previously
// checked result would silently render as unchecked again.
func TestRenderSearchEntriesPreservesCheckedStateAcrossSort(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	p.showSearchResults()
	target := filepath.Join(dir, "apple.txt")
	p.appendSearchResult(search.Result{Path: target})
	p.appendSearchResult(search.Result{Path: filepath.Join(dir, "banana.txt")})

	var row int
	for i := 0; i < p.table.GetRowCount(); i++ {
		if ref, ok := p.rowRef(i); ok && ref.path == target {
			row = i
			break
		}
	}
	p.setChecked(row, true)

	p.setSortKey(sortBySize) // forces a full renderSearchEntries re-render

	if !p.selected[target] {
		t.Fatalf("p.selected lost %q across a sort-key change", target)
	}
	var newRow int
	for i := 0; i < p.table.GetRowCount(); i++ {
		if ref, ok := p.rowRef(i); ok && ref.path == target {
			newRow = i
			break
		}
	}
	if cell := p.table.GetCell(newRow, colCheckbox); cell.Text != checkboxText(true) {
		t.Errorf("checkbox glyph for %q = %q after re-sorting, want %q (still checked)", target, cell.Text, checkboxText(true))
	}
}

// TestExitSearchResultsRestoresRealDirectoryAndCursor pins
// exitSearchResults' own full round trip: leaving search mode brings
// back exactly the real directory and cursor row the panel was showing
// right before showSearchResults switched it over, per the user's own
// explicit request that this look like nothing ever happened.
func TestExitSearchResultsRestoresRealDirectoryAndCursor(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	p.focusRow(2)
	realRowCount := p.table.GetRowCount()

	p.showSearchResults()
	p.appendSearchResult(search.Result{Path: filepath.Join(dir, "apple.txt")})

	if err := p.exitSearchResults(); err != nil {
		t.Fatalf("exitSearchResults: %v", err)
	}

	if p.searchMode {
		t.Error("searchMode still true after exitSearchResults")
	}
	if got := p.table.GetRowCount(); got != realRowCount {
		t.Errorf("got %d rows after exiting search results, want the real directory's own %d back", got, realRowCount)
	}
	row, _ := p.table.GetSelection()
	if row != 2 {
		t.Errorf("selected row after exitSearchResults = %d, want restored 2", row)
	}
}

// TestActivateRowInSearchModeJumpsToResultInstead pins activateRow's
// own searchMode branch: unlike a real directory row (where only a
// directory does anything, and it's "cd into it"), any result — file
// or directory alike — leaves search mode and jumps straight to the
// result's real location, the same "Go to file/folder" meaning
// left-click on a result has always had.
func TestActivateRowInSearchModeJumpsToResultInstead(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	p.showSearchResults()
	target := filepath.Join(dir, "apple.txt") // a file — activateRow on a real row would ignore this entirely
	p.appendSearchResult(search.Result{Path: target})

	p.activateRow(0)

	if p.searchMode {
		t.Error("searchMode still true after activating a search result")
	}
	if p.path != dir {
		t.Errorf("p.path = %q after activating the result, want its own real directory %q", p.path, dir)
	}
	row, _ := p.table.GetSelection()
	ref, ok := p.rowRef(row)
	if !ok || ref.path != target {
		t.Errorf("selected row after activating the result = %+v, want it focused on %q", ref, target)
	}
}

// TestFilterFieldInertWhileSearchMode pins the deliberate scope
// boundary documented on filterField/filterRegexBtn's own guards:
// neither touches filterText/filterRegex, and neither reloads
// anything, while search results are showing — the search dialog
// already has its own Filename/Content fields for that need.
func TestFilterFieldInertWhileSearchMode(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	p.showSearchResults()
	p.appendSearchResult(search.Result{Path: filepath.Join(dir, "apple.txt")})
	rowsBefore := p.table.GetRowCount()

	// SetText alone doesn't fire SetChangedFunc in tview unless the
	// text actually differs from before — it does here, since
	// filterField starts blank.
	p.filterField.SetText("apple")

	if p.filterText != "" {
		t.Errorf("filterText = %q after typing while searchMode, want untouched %q", p.filterText, "")
	}
	if got := p.table.GetRowCount(); got != rowsBefore {
		t.Errorf("got %d rows after typing in the filter field while searchMode, want unchanged %d (no reload)", got, rowsBefore)
	}
}

// TestCaptureTableKeyEscapeCallsOnSearchEscapeOnlyInSearchMode pins
// where "Esc: back to search" is actually wired now that results live
// in the panel itself rather than a separate overlay — see
// onSearchEscape's own doc comment.
func TestCaptureTableKeyEscapeCallsOnSearchEscapeOnlyInSearchMode(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	called := false
	p.onSearchEscape = func() { called = true }

	p.captureTableKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if called {
		t.Error("onSearchEscape ran while showing a real directory, want only in search mode")
	}

	p.showSearchResults()
	p.captureTableKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if !called {
		t.Error("onSearchEscape did not run for Escape while searchMode")
	}
}

// TestActivateRowOnContentMatchOpensEditorWithoutLeavingSearchMode pins
// the user's own explicit request: activating a content-search result
// (Line > 0 — see rowRef.searchLine) calls onOpenSearchResult with its
// real path and matched line instead of just jumping to it, and —
// deliberately, unlike a filename match — leaves search mode showing
// afterward, so the next match is still right there to try.
func TestActivateRowOnContentMatchOpensEditorWithoutLeavingSearchMode(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	var gotPath string
	var gotLine int
	p.onOpenSearchResult = func(path string, line int) {
		gotPath, gotLine = path, line
	}
	p.showSearchResults()
	target := filepath.Join(dir, "apple.txt")
	p.appendSearchResult(search.Result{Path: target, Line: 9, Text: "needle"})

	p.activateRow(0)

	if gotPath != target || gotLine != 9 {
		t.Errorf("onOpenSearchResult got (%q, %d), want (%q, %d)", gotPath, gotLine, target, 9)
	}
	if !p.searchMode {
		t.Error("searchMode = false after opening a content match, want still true")
	}
}

// TestActivateRowOnContentMatchFallsBackWithoutOpenCallback pins the
// same nil-safety onSearchEscape already has: with onOpenSearchResult
// left nil (e.g. a Panel built without Root wiring it — see its own
// doc comment), a content match still falls back to the ordinary
// jump-to-result behavior rather than silently doing nothing.
func TestActivateRowOnContentMatchFallsBackWithoutOpenCallback(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	p.showSearchResults()
	target := filepath.Join(dir, "apple.txt")
	p.appendSearchResult(search.Result{Path: target, Line: 9, Text: "needle"})

	p.activateRow(0)

	if p.searchMode {
		t.Error("searchMode still true after activating a content match with no onOpenSearchResult, want the fallback jump to have left it")
	}
	if p.path != dir {
		t.Errorf("p.path = %q, want the fallback jump to have landed on %q", p.path, dir)
	}
}

// TestActivateRowOnFilenameMatchIgnoresOpenCallback pins that
// onOpenSearchResult is only ever consulted for a content match
// (searchLine > 0) — a filename match (Line == 0) always jumps to its
// real location instead, even with a callback set, since there's no
// specific line to open it at in the first place.
func TestActivateRowOnFilenameMatchIgnoresOpenCallback(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	called := false
	p.onOpenSearchResult = func(string, int) { called = true }
	p.showSearchResults()
	target := filepath.Join(dir, "apple.txt")
	p.appendSearchResult(search.Result{Path: target}) // Line == 0: a filename match

	p.activateRow(0)

	if called {
		t.Error("onOpenSearchResult ran for a filename match, want it only ever consulted for a content match")
	}
	if p.searchMode {
		t.Error("searchMode still true after activating a filename match, want the jump to have left it")
	}
}
