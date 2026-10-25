package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/filterexpr"
	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/logview"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// newTestRootForLogAudit builds a Root whose active panel points at
// dir, and opens the Log Audit screen on it ("jL"'s own effect) —
// dir's own files are whatever the caller already wrote before calling
// this, the same "build the fixture, then open the screen" order
// newTestRootForMessages already uses.
func newTestRootForLogAudit(t *testing.T, dir string) *Root {
	t.Helper()
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openLogAudit()
	return r
}

func writeLogAuditFixture(t *testing.T, dir string) {
	t.Helper()
	appLog := `{"time":"2026-10-07T16:04:21Z","level":"error","msg":"database connection timeout","service":"app"}
{"time":"2026-10-07T16:04:23Z","level":"info","msg":"connection established","service":"app"}
`
	if err := os.WriteFile(filepath.Join(dir, "app.log"), []byte(appLog), 0o644); err != nil {
		t.Fatalf("WriteFile app.log: %v", err)
	}
	sysLog := "Oct  7 16:04:22 server kernel: eth0: link down\n"
	if err := os.WriteFile(filepath.Join(dir, "syslog"), []byte(sysLog), 0o644); err != nil {
		t.Fatalf("WriteFile syslog: %v", err)
	}
}

// TestOpenLogAuditDiscoversAndReadsRemoteFiles pins the user's own
// explicit request: "jL" used to always browse the local filesystem
// even while the active panel was already connected to a remote
// session — the connection was already up, so the whole point of
// pointing Log Audit at a directory on it was to read through its own
// log files, not this machine's. Verifies both halves end to end
// through the real currentLogAuditSource/remoteLogFileSource wiring:
// discovery (the family shows up at all) and actually reading the
// remote file's own content (the entry's own Level, not just its
// presence, proving the bytes really came from the fake remote client,
// not an empty/local fallback).
func TestOpenLogAuditDiscoversAndReadsRemoteFiles(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), t.TempDir())
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	remote := newTestFakeRemote("/remote")
	remote.content = map[string][]byte{}
	content := "2026-10-07 16:04:23 ERROR database connection timeout\n"
	remote.entries["/remote"] = []fsops.Entry{
		{Name: "app.log", Type: fsops.TypeFile, Size: int64(len(content)), ModTime: time.Now()},
	}
	remote.content["/remote/app.log"] = []byte(content)

	if err := r.panel.connectRemote(remote, remotefs.Connection{Host: "testhost"}); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}

	r.openLogAudit()
	if len(r.logAuditGroups) != 1 || r.logAuditGroups[0].Base != "app.log" {
		t.Fatalf("logAuditGroups = %+v, want one group for app.log — Discover must have gone through the remote client, not the local filesystem", r.logAuditGroups)
	}

	r.logAuditTable.Select(1, 0)
	r.openLogAuditViewer()

	if r.logAuditFiles != 1 {
		t.Fatalf("logAuditFiles = %d, want 1", r.logAuditFiles)
	}
	if len(r.logAuditAllEntries) != 1 {
		t.Fatalf("len(logAuditAllEntries) = %d, want 1", len(r.logAuditAllEntries))
	}
	if r.logAuditAllEntries[0].Level != logview.LevelError {
		t.Errorf("entries[0].Level = %v, want LevelError — the remote file's own real content should have been read and parsed", r.logAuditAllEntries[0].Level)
	}
}

// TestRenderLogAuditSelectionTitleNamesTheRemoteConnectionWhenRemote
// pins this project's own "a remote view must never describe local
// paths or metadata" rule applied in the other direction: once the
// selection screen genuinely reads through a remote connection, it
// must say so (and which one) rather than looking exactly like a
// local listing — the user has no other way to tell from this screen
// alone.
func TestRenderLogAuditSelectionTitleNamesTheRemoteConnectionWhenRemote(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	if got := r.logAuditTitleBar.GetText(true); strings.Contains(got, "testhost") {
		t.Errorf("local title = %q, want no connection name", got)
	}

	remote := newTestFakeRemote("/remote")
	if err := r.panel.connectRemote(remote, remotefs.Connection{Host: "testhost"}); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}
	r.openLogAudit()
	if got := r.logAuditTitleBar.GetText(true); !strings.Contains(got, "testhost") {
		t.Errorf("remote title = %q, want it to name the connection (testhost)", got)
	}
}

func TestOpenLogAuditDiscoversGroups(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)

	if r.activePage != logAuditSelectionPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, logAuditSelectionPage)
	}
	if len(r.logAuditGroups) != 2 {
		t.Fatalf("len(logAuditGroups) = %d, want 2 (app.log, syslog)", len(r.logAuditGroups))
	}
}

func TestOpenLogAuditViewerOpensGroupUnderCursor(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)

	r.logAuditTable.Select(1, 0) // row 1 = logAuditGroups[0], the app.log group (sorted before syslog)
	r.openLogAuditViewer()

	if r.activePage != logAuditViewerPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, logAuditViewerPage)
	}
	if r.logAuditFiles != 1 {
		t.Fatalf("logAuditFiles = %d, want 1 (just app.log)", r.logAuditFiles)
	}
	if len(r.logAuditAllEntries) != 2 {
		t.Fatalf("len(logAuditAllEntries) = %d, want 2", len(r.logAuditAllEntries))
	}
}

// TestOpenLogAuditViewerFocusesTheList pins the user's own explicit
// request: opening the viewer should land ready to read/navigate
// entries right away, not typing into the keyword filter first.
func TestOpenLogAuditViewerFocusesTheList(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)

	r.logAuditTable.Select(1, 0)
	r.openLogAuditViewer()

	if !r.logAuditViewerTable.HasFocus() {
		t.Error("opening the viewer should focus the list, not a filter field")
	}
}

// TestLogAuditFocusIndicatorReflectsTableFocus pins the user's own
// explicit request for a visible cue that the list currently has
// keyboard focus — repurposing the row between the timeline and the
// table's own header, previously just blank border padding, as a
// background-color indicator (no text of its own — an earlier "●
// List" label was dropped per the user's own later, explicit report
// that the color alone already says it).
func TestLogAuditFocusIndicatorReflectsTableFocus(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)

	r.logAuditTable.Select(1, 0)
	r.openLogAuditViewer() // focuses the table — see the test above

	if got := r.logAuditFocusIndicator.GetBackgroundColor(); got != r.theme.InputFocusedBackground {
		t.Errorf("focus indicator background = %v, want InputFocusedBackground %v while the list has focus", got, r.theme.InputFocusedBackground)
	}

	r.app.SetFocus(r.logAuditKeywordField)
	// PanelBackground, not SurfaceBackground: the viewer table itself
	// moved to PanelBackground per the user's own explicit request to
	// bring every full-screen catalog in line with the main screen it
	// fully covers (see logAuditViewerTable's own comment) — this
	// indicator blends into the table it's a cue for while unfocused,
	// so it moved with it.
	if got := r.logAuditFocusIndicator.GetBackgroundColor(); got != r.theme.PanelBackground {
		t.Errorf("focus indicator background = %v, want PanelBackground %v once focus moves to a filter field", got, r.theme.PanelBackground)
	}
}

// TestLogAuditTimeHeaderClickTogglesSortOrder pins the "Time" header's
// own click handler — the same "click to sort, click again to
// reverse" convention the panel's own column headers already use (see
// sortArrow, panel.go) — per the user's own explicit request to read
// the list old->new or new->old. Opens newest-first by default (see
// openLogAuditViewer's own doc comment on logAuditNewestFirst, per a
// later, separate explicit request), so this test's own "before"
// state is newest-first, toggling to oldest-first — the opposite
// direction from when that default was still oldest-first.
func TestLogAuditTimeHeaderClickTogglesSortOrder(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir) // app.log: error @16:04:21, then info @16:04:23
	r := newTestRootForLogAudit(t, dir)
	r.logAuditTable.Select(1, 0)
	r.openLogAuditViewer()

	firstEntry := func() logview.Entry {
		e, ok := r.logAuditEntryAt(1)
		if !ok {
			t.Fatal("row 1 has no entry reference")
		}
		return e
	}

	if !r.logAuditNewestFirst {
		t.Fatal("setup: opening the viewer should default to newest-first")
	}
	if got := firstEntry(); got.Level != logview.LevelInfo {
		t.Fatalf("before toggling, row 1 = %+v, want the newest entry (Info @16:04:23) — newest-first is the default", got)
	}

	header := r.logAuditViewerTable.GetCell(0, logAuditColTime)
	if header.Clicked == nil {
		t.Fatal("Time header has no Clicked handler")
	}
	header.Clicked()
	if r.logAuditNewestFirst {
		t.Fatal("clicking the Time header should have cleared logAuditNewestFirst")
	}
	if got := firstEntry(); got.Level != logview.LevelError {
		t.Errorf("after toggling to oldest-first, row 1 = %+v, want the oldest entry (Error @16:04:21)", got)
	}
	// Re-fetched, not the same cell object header still points at:
	// renderLogAuditViewer (called from the Clicked handler itself)
	// replaces row 0's cell outright via SetCell, same as every other
	// row — header's own copy is now stale.
	if got := r.logAuditViewerTable.GetCell(0, logAuditColTime).Text; !strings.Contains(got, "↑") {
		t.Errorf("Time header = %q, want a ↑ arrow once sorted oldest-first", got)
	}
}

func TestCloseLogAuditViewerReturnsToSelection(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)

	r.logAuditTable.Select(1, 0)
	r.openLogAuditViewer()
	r.closeLogAuditViewer()

	if r.activePage != logAuditSelectionPage {
		t.Fatalf("activePage = %q, want back to %q", r.activePage, logAuditSelectionPage)
	}
}

func TestNavigateLogAuditLevelFindsAndWrapsErrors(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)

	r.logAuditTable.Select(1, 0)
	r.openLogAuditViewer()

	r.logAuditViewerTable.Select(1, 0)
	r.navigateLogAuditLevel(logview.LevelError)

	e, ok := r.logAuditEntryAt(func() int { row, _ := r.logAuditViewerTable.GetSelection(); return row }())
	if !ok || e.Level != logview.LevelError {
		t.Fatalf("after navigateLogAuditLevel(Error), selected entry = %+v, ok=%v, want an Error row", e, ok)
	}
}

func TestRenderLogAuditViewerFiltersByKeyword(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)

	r.logAuditTable.Select(1, 0)
	r.openLogAuditViewer()

	r.logAuditKeywordField.SetText("timeout")
	if got := r.logAuditViewerTable.GetRowCount(); got != 2 { // header + 1 match
		t.Fatalf("GetRowCount() after filter = %d, want 2 (header + 1 match)", got)
	}
}

// TestRenderLogAuditViewerNotesTruncationInTitle pins a real, user-
// reported point of confusion: logAuditMaxRenderedRows silently capped
// the table while the title bar's own counts still claimed every file
// was read in full, reading as a bug rather than a documented limit.
func TestRenderLogAuditViewerNotesTruncationInTitle(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 0; i < logAuditMaxRenderedRows+5; i++ {
		b.WriteString("just some unstructured text\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "big.log"), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("WriteFile big.log: %v", err)
	}
	r := newTestRootForLogAudit(t, dir)
	r.logAuditTable.Select(1, 0)
	r.openLogAuditViewer()

	title := r.logAuditViewerTitle.GetText(true)
	if !strings.Contains(title, fmt.Sprintf("showing first %d", logAuditMaxRenderedRows)) {
		t.Errorf("title = %q, want an explicit truncation notice", title)
	}
	if got := r.logAuditViewerTable.GetRowCount(); got != logAuditMaxRenderedRows+1 { // +1 header
		t.Errorf("GetRowCount() = %d, want %d (header + cap)", got, logAuditMaxRenderedRows+1)
	}
}

// TestRenderLogAuditViewerKeepsGoodEntriesWhenOneFileFails pins a real,
// independently-discovered bug directly on point for the user's own
// "nicht alle Logeinträge sichtbar" report: a parse error from just
// one rotation of a logrotate family (here, a corrupted .gz) used to
// blank out the whole merged view, hiding every entry readLogAuditGroup
// had already successfully read from the family's other, perfectly
// fine files.
func TestRenderLogAuditViewerKeepsGoodEntriesWhenOneFileFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.log"), []byte("just some unstructured text\n"), 0o644); err != nil {
		t.Fatalf("WriteFile app.log: %v", err)
	}
	// Not a valid gzip stream at all — logview.Open's gzip.NewReader
	// call fails on it, the same shape a truncated/corrupted rotated
	// file would hit in the wild.
	if err := os.WriteFile(filepath.Join(dir, "app.log.1.gz"), []byte("not actually gzip"), 0o644); err != nil {
		t.Fatalf("WriteFile app.log.1.gz: %v", err)
	}

	r := newTestRootForLogAudit(t, dir)
	r.logAuditTable.Select(1, 0)
	r.openLogAuditViewer()

	if r.logAuditParseErr == nil {
		t.Fatal("setup: expected app.log.1.gz to fail to parse")
	}
	if len(r.logAuditAllEntries) != 1 {
		t.Fatalf("len(logAuditAllEntries) = %d, want 1 (app.log's own entry, kept despite the other file's error)", len(r.logAuditAllEntries))
	}
	if got := r.logAuditViewerTable.GetRowCount(); got != 2 { // header + the one good entry
		t.Errorf("GetRowCount() = %d, want 2 (header + 1 entry) — the error must not blank the whole table", got)
	}
	if title := r.logAuditViewerTitle.GetText(true); !strings.Contains(title, "1 file failed") {
		t.Errorf("title = %q, want the error surfaced as a note, not silently dropped", title)
	}
}

// clickPrimitive simulates a real left-click on p — MouseLeftDown then
// MouseLeftUp at its own rect's center, through root's actual
// MouseHandler chain, the same path a real click takes (not a direct
// r.app.SetFocus(p) call, which would bypass the exact tview quirk
// TestRestyleLogAuditFilterFieldsFixesStuckFocusColorOnMouseClick
// below exists to pin).
func clickPrimitive(root *Root, p tview.Primitive) {
	x, y, w, h := p.GetRect()
	cx, cy := x+w/2, y+h/2
	handler := root.MouseHandler()
	handler(tview.MouseLeftDown, tcell.NewEventMouse(cx, cy, tcell.Button1, 0), func(p tview.Primitive) { root.app.SetFocus(p) })
	handler(tview.MouseLeftUp, tcell.NewEventMouse(cx, cy, tcell.ButtonNone, 0), func(p tview.Primitive) { root.app.SetFocus(p) })
}

// TestRestyleLogAuditFilterFieldsFixesStuckFocusColorOnMouseClick pins
// a real, confirmed tview gap (see restyleLogAuditFocusTargets' own
// doc comment, logauditscreen.go, for the full mechanism): tview's
// InputField forwards its embedded TextArea's Focus event back out to
// the InputField's own FocusFunc, but never forwards Blur — so a mouse
// click (unlike Tab, which focuses the *InputField directly) leaves
// the previously-focused field's own background stuck at
// InputFocusedBackground forever, since the real tview.Primitive that
// actually loses focus on the next click is the TextArea, not the
// InputField whose BlurFunc we registered. The user's own explicit,
// live report: "ich kann auch alle zu Fokusfarbe machen" — clicking
// through all three fields left every one of them stuck highlighted.
func TestRestyleLogAuditFilterFieldsFixesStuckFocusColorOnMouseClick(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.logAuditTable.Select(1, 0)
	r.openLogAuditViewer()

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(200, 50)
	r.SetRect(0, 0, 200, 50)
	r.Draw(screen)

	fieldBG := func(f *tview.InputField) tcell.Color {
		_, bg, _ := f.GetFieldStyle().Decompose()
		return bg
	}

	clickPrimitive(r, r.logAuditKeywordField)
	r.Draw(screen)
	if got := fieldBG(r.logAuditKeywordField); got != r.theme.InputFocusedBackground {
		t.Fatalf("setup: Keyword bg = %v after its own click, want InputFocusedBackground %v", got, r.theme.InputFocusedBackground)
	}

	clickPrimitive(r, r.logAuditTimeField)
	r.Draw(screen)
	if got := fieldBG(r.logAuditKeywordField); got != r.theme.InputBackground {
		t.Errorf("Keyword bg = %v after clicking Time, want it back to InputBackground %v, not stuck highlighted", got, r.theme.InputBackground)
	}
	if got := fieldBG(r.logAuditTimeField); got != r.theme.InputFocusedBackground {
		t.Errorf("Time bg = %v, want InputFocusedBackground %v (it's the one just clicked)", got, r.theme.InputFocusedBackground)
	}

	clickPrimitive(r, r.logAuditLevelField)
	r.Draw(screen)
	if got := fieldBG(r.logAuditKeywordField); got != r.theme.InputBackground {
		t.Errorf("Keyword bg = %v after clicking Level, want InputBackground %v", got, r.theme.InputBackground)
	}
	if got := fieldBG(r.logAuditTimeField); got != r.theme.InputBackground {
		t.Errorf("Time bg = %v after clicking Level, want InputBackground %v — not stuck highlighted either", got, r.theme.InputBackground)
	}
	if got := fieldBG(r.logAuditLevelField); got != r.theme.InputFocusedBackground {
		t.Errorf("Level bg = %v, want InputFocusedBackground %v (it's the one just clicked)", got, r.theme.InputFocusedBackground)
	}
}

// TestLogAuditFilterLabelsAreShortWithExamplesAsPlaceholders pins the
// user's own explicit request: the Time/Level fields' own "(e.g. ...)"
// examples used to be baked into the label text itself, shown
// permanently; they now live in each field's own placeholder instead,
// visible only while it's actually empty, with the labels themselves
// shortened to match.
func TestLogAuditFilterLabelsAreShortWithExamplesAsPlaceholders(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.logAuditTable.Select(1, 0)
	r.openLogAuditViewer()

	for _, tc := range []struct {
		field *tview.InputField
		label string
	}{
		{r.logAuditKeywordField, "Filter: "},
		{r.logAuditTimeField, "Time: "},
		{r.logAuditLevelField, "Level >= "},
	} {
		if got := tc.field.GetLabel(); got != tc.label {
			t.Errorf("label = %q, want %q", got, tc.label)
		}
		if strings.Contains(tc.field.GetLabel(), "e.g.") {
			t.Errorf("label = %q, want the example moved to the field's own placeholder, not left in the label", tc.field.GetLabel())
		}
	}
}

func TestLogAuditGroupInfo(t *testing.T) {
	groups := []logview.FileGroup{
		{Base: "access.log", Files: []logview.CandidateFile{
			{Path: "access.log", Compressed: false, Supported: true},
			{Path: "access.log.1.gz", Compressed: true, Supported: true},
			// A hypothetical, currently-impossible extension Open
			// doesn't recognize at all — every real one Discover can
			// produce today (gz/xz/zst/bz2) is always Supported (see
			// logAuditGroupInfo's own doc comment).
			{Path: "access.log.2.foo", Compressed: true, Supported: false},
		}},
	}
	got := logAuditGroupInfo(groups[0])
	if got != "3 files (2 compressed) — 1 unsupported compression" {
		t.Errorf("logAuditGroupInfo = %q", got)
	}
}

// TestRenderLogAuditSelectionShowsSizeAndModified pins the user's own
// explicit request to see the family's own newest file's size/mtime
// in the selection list without opening the group first.
func TestRenderLogAuditSelectionShowsSizeAndModified(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir) // app.log has real content, non-zero size/mtime
	r := newTestRootForLogAudit(t, dir)

	sizeText := r.logAuditTable.GetCell(1, logAuditSelSize).Text
	modText := r.logAuditTable.GetCell(1, logAuditSelModified).Text
	if sizeText == "" {
		t.Error("Size column is empty, want the newest file's humanized size")
	}
	if modText == "" {
		t.Error("Modified column is empty, want the newest file's mtime")
	}
}

func TestLogAuditKeywordQueryMatchesSourceAndMessage(t *testing.T) {
	e := logview.Entry{Source: "nginx", Message: "connection timeout"}

	mustMatch := func(keyword string, want bool) {
		t.Helper()
		query, hasQuery := logAuditKeywordQuery(keyword)
		if got := logAuditMatchesQuery(e, query, hasQuery); got != want {
			t.Errorf("logAuditMatchesQuery(keyword=%q) = %v, want %v", keyword, got, want)
		}
	}
	mustMatch("TIMEOUT", true) // case-insensitive match against Message
	mustMatch("NGINX", true)   // case-insensitive match against Source
	mustMatch("bogus", false)
	mustMatch("", true) // blank keyword matches everything

	// The feature the user asked for: "keyword1 AND (keyword2 OR
	// keyword3)" — this entry's own Source/Message hold "nginx" and
	// "connection timeout", so a query requiring "nginx" AND ("timeout"
	// OR "bogus") must still match via the OR's first alternative.
	mustMatch("nginx AND (timeout OR bogus)", true)
	mustMatch("nginx AND (bogus OR evenmorebogus)", false)
}

func TestLogAuditHighlightWrapsEveryOccurrence(t *testing.T) {
	got := logAuditHighlight("a timeout then another timeout", []string{"timeout"}, tcell.ColorRed)
	if count := strings.Count(got, "timeout"); count != 2 {
		t.Errorf("highlighted text = %q, want the raw word \"timeout\" still present twice, got %d", got, count)
	}
	if count := strings.Count(got, "[-:-:-]"); count != 2 {
		t.Errorf("highlighted text = %q, want 2 reset tags (one per occurrence)", got)
	}
}

func TestLogAuditHighlightMultipleTerms(t *testing.T) {
	got := logAuditHighlight("foo and bar both appear", []string{"foo", "bar"}, tcell.ColorRed)
	if count := strings.Count(got, "[-:-:-]"); count != 2 {
		t.Errorf("highlighted text = %q, want 2 reset tags (one per term occurrence)", got)
	}
	if !strings.Contains(got, "foo") || !strings.Contains(got, "bar") {
		t.Errorf("highlighted text = %q, want both raw terms still present", got)
	}
}

func TestLogAuditHighlightEscapesLiteralBrackets(t *testing.T) {
	text := "tag[1] failed"
	got := logAuditHighlight(text, nil, tcell.ColorRed)
	if want := tview.Escape(text); got != want {
		t.Errorf("logAuditHighlight(terms=nil) = %q, want tview.Escape's own output %q", got, want)
	}
}

func TestLogAuditMinLevel(t *testing.T) {
	if _, ok := logAuditMinLevel(""); ok {
		t.Error("blank level text should not parse")
	}
	if _, ok := logAuditMinLevel("not-a-level"); ok {
		t.Error("unparseable level text should not parse")
	}
	level, ok := logAuditMinLevel("  Warn  ")
	if !ok || level != logview.LevelWarn {
		t.Errorf("logAuditMinLevel(\"  Warn  \") = %v, %v, want LevelWarn, true", level, ok)
	}
}

func TestLogAuditEntryVisibleCombinesAllThreeFilters(t *testing.T) {
	now := time.Date(2026, 10, 7, 18, 0, 0, 0, time.UTC)
	e := logview.Entry{
		Time:    time.Date(2026, 10, 7, 16, 4, 21, 0, time.UTC),
		Level:   logview.LevelWarn,
		Source:  "app",
		Message: "database connection timeout",
	}
	visible := func(keyword, timeExpr, levelText string) bool {
		t.Helper()
		query, hasQuery := logAuditKeywordQuery(keyword)
		return logAuditEntryVisible(e, query, hasQuery, timeExpr, levelText, now)
	}

	if !visible("", "", "") {
		t.Error("no filters at all should show everything")
	}
	if !visible("timeout", "", "warn") {
		t.Error("matching keyword + matching minimum level should show the entry")
	}
	if visible("timeout", "", "error") {
		t.Error("Warn entry must not pass a >= Error level filter")
	}
	if visible("bogus", "", "") {
		t.Error("non-matching keyword should hide the entry regardless of level/time")
	}
	if !visible("", "before 2026-10-07T17:00:00Z", "") {
		t.Error("entry's own Time is before the time filter's cutoff, should pass")
	}
	if visible("", "after 2026-10-07T17:00:00Z", "") {
		t.Error("entry's own Time is before the time filter's cutoff, should be excluded by 'after'")
	}
}

func TestLogAuditEntryVisibleExcludesUnknownLevelFromLevelFilter(t *testing.T) {
	e := logview.Entry{Level: logview.LevelUnknown, Message: "no level recognized here"}
	noQuery, hasNoQuery := logAuditKeywordQuery("")
	if logAuditEntryVisible(e, noQuery, hasNoQuery, "", "trace", time.Now()) {
		t.Error("an entry with no recognized level should not pass even a >= trace filter")
	}
	if !logAuditEntryVisible(e, noQuery, hasNoQuery, "", "", time.Now()) {
		t.Error("with no level filter at all, the same entry should still show")
	}
}

func TestToggleLogAuditFollowStartsAndStopsTicker(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.logAuditTable.Select(1, 0)
	r.openLogAuditViewer()

	if r.logAuditFollowing {
		t.Fatal("follow should start off")
	}
	r.toggleLogAuditFollow()
	if !r.logAuditFollowing || r.logAuditFollowCancel == nil {
		t.Fatal("toggle should start following")
	}
	r.toggleLogAuditFollow()
	if r.logAuditFollowing || r.logAuditFollowCancel != nil {
		t.Fatal("toggle again should stop following")
	}
}

func TestCloseLogAuditViewerStopsFollow(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.logAuditTable.Select(1, 0)
	r.openLogAuditViewer()

	r.startLogAuditFollow()
	r.closeLogAuditViewer()

	if r.logAuditFollowing {
		t.Error("closing the viewer should stop a running follow")
	}
}

func TestOpenLogAuditDetailShowsRawOnlyWhenDifferentFromMessage(t *testing.T) {
	// app.log (JSON Lines) — Raw is the full JSON line, Message only
	// the extracted "msg" field, so the two differ and Raw: must show.
	jsonDir := t.TempDir()
	writeLogAuditFixture(t, jsonDir)
	r := newTestRootForLogAudit(t, jsonDir)
	r.logAuditTable.Select(1, 0) // app.log group, sorted before syslog
	r.openLogAuditViewer()
	r.openLogAuditDetail(1)
	if got := r.logAuditDetailView.GetText(true); !strings.Contains(got, "Raw:") {
		t.Errorf("detail view for a JSON entry = %q, want a Raw: section", got)
	}

	// A FormatPlain file — Raw and Message are identical (the whole
	// line, verbatim), so no redundant second copy should appear.
	plainDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(plainDir, "plain.log"), []byte("just some unstructured text\n"), 0o644); err != nil {
		t.Fatalf("WriteFile plain.log: %v", err)
	}
	r2 := newTestRootForLogAudit(t, plainDir)
	r2.logAuditTable.Select(1, 0)
	r2.openLogAuditViewer()
	r2.openLogAuditDetail(1)
	if got := r2.logAuditDetailView.GetText(true); strings.Contains(got, "Raw:") {
		t.Errorf("detail view for a FormatPlain entry = %q, want no redundant Raw: section", got)
	}
}

// writeLogAuditTimelineFixture writes 20 JSON-Lines entries, one per
// second, all on the same calendar day — predictable enough to drive
// the interactive timeline without hand-computing bucket boundaries:
// tests read back whatever logAuditTimelineBucketCount/MinT/MaxT the
// real render actually produced (see openLogAuditTimelineViewer) and
// build their own expectations from that, the same "don't predict the
// rendering pipeline's width math by hand" approach logAuditBucketTimeRange's
// own unit tests already take one level down.
func writeLogAuditTimelineFixture(t *testing.T, dir string) {
	t.Helper()
	var b strings.Builder
	base := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&b, `{"time":%q,"level":"info","msg":"entry %d","service":"app"}`+"\n",
			base.Add(time.Duration(i)*time.Second).Format(time.RFC3339), i)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.log"), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("WriteFile app.log: %v", err)
	}
}

// openLogAuditTimelineViewer opens the viewer on writeLogAuditTimelineFixture's
// own single group, draws it at width x height (a real Draw pass is
// what gives logAuditTimelineView a real, non-zero rect — see
// logAuditTimelineUnlaidOutWidth's own doc comment), and fails the
// test outright if that didn't leave at least 2 buckets: every test
// using this helper needs room to select a sub-range, not just one
// single all-or-nothing bucket.
func openLogAuditTimelineViewer(t *testing.T, width, height int) *Root {
	t.Helper()
	dir := t.TempDir()
	writeLogAuditTimelineFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.logAuditTable.Select(1, 0)
	r.openLogAuditViewer()
	drawRoot(t, r, width, height)
	if r.logAuditTimelineBucketCount < 2 {
		t.Fatalf("setup: bucketCount = %d at %dx%d, want at least 2 — widen the test's own draw size", r.logAuditTimelineBucketCount, width, height)
	}
	return r
}

func TestLogAuditTimelineArrowKeysMoveAndCollapseSelection(t *testing.T) {
	r := openLogAuditTimelineViewer(t, 200, 50)
	r.app.SetFocus(r.logAuditTimelineView)
	if !r.logAuditTimelineFocused {
		t.Fatal("setup: focusing the timeline should set logAuditTimelineFocused")
	}
	if r.logAuditTimelineAnchor != 0 || r.logAuditTimelineCursor != 0 {
		t.Fatalf("setup: anchor,cursor = %d,%d, want 0,0 on a fresh render", r.logAuditTimelineAnchor, r.logAuditTimelineCursor)
	}

	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	if r.logAuditTimelineAnchor != 2 || r.logAuditTimelineCursor != 2 {
		t.Errorf("after two plain Rights: anchor,cursor = %d,%d, want 2,2 (collapsed)", r.logAuditTimelineAnchor, r.logAuditTimelineCursor)
	}

	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone))
	if r.logAuditTimelineAnchor != 1 || r.logAuditTimelineCursor != 1 {
		t.Errorf("after a plain Left: anchor,cursor = %d,%d, want 1,1", r.logAuditTimelineAnchor, r.logAuditTimelineCursor)
	}
}

func TestLogAuditTimelineCursorClampsAtBothEnds(t *testing.T) {
	r := openLogAuditTimelineViewer(t, 200, 50)
	r.app.SetFocus(r.logAuditTimelineView)

	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone))
	if r.logAuditTimelineCursor != 0 {
		t.Errorf("Left at the very first bucket: cursor = %d, want clamped to 0", r.logAuditTimelineCursor)
	}

	last := r.logAuditTimelineBucketCount - 1
	for i := 0; i < r.logAuditTimelineBucketCount+3; i++ {
		r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	}
	if r.logAuditTimelineCursor != last {
		t.Errorf("Right past the last bucket repeatedly: cursor = %d, want clamped to %d", r.logAuditTimelineCursor, last)
	}
}

func TestLogAuditTimelineShiftArrowExtendsFromTheFixedAnchor(t *testing.T) {
	r := openLogAuditTimelineViewer(t, 200, 50)
	r.app.SetFocus(r.logAuditTimelineView)

	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	if r.logAuditTimelineCursor != 2 {
		t.Fatalf("setup: cursor = %d, want 2", r.logAuditTimelineCursor)
	}

	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModShift))
	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModShift))
	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModShift))
	if r.logAuditTimelineAnchor != 2 {
		t.Errorf("anchor = %d after three Shift+Right, want 2 (unchanged — it must stay at the pre-Shift cursor position)", r.logAuditTimelineAnchor)
	}
	if r.logAuditTimelineCursor != 5 {
		t.Errorf("cursor = %d after three Shift+Right from 2, want 5", r.logAuditTimelineCursor)
	}

	// A plain arrow (Shift released) must collapse the just-built range.
	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	if r.logAuditTimelineAnchor != 6 || r.logAuditTimelineCursor != 6 {
		t.Errorf("anchor,cursor = %d,%d after a plain Right, want 6,6 (collapsed)", r.logAuditTimelineAnchor, r.logAuditTimelineCursor)
	}
}

func TestLogAuditTimelineShiftArrowSelectsLeftwardJustAsWell(t *testing.T) {
	r := openLogAuditTimelineViewer(t, 200, 50)
	r.app.SetFocus(r.logAuditTimelineView)
	for i := 0; i < 5; i++ {
		r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	}
	if r.logAuditTimelineCursor != 5 {
		t.Fatalf("setup: cursor = %d, want 5", r.logAuditTimelineCursor)
	}

	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModShift))
	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModShift))
	if r.logAuditTimelineAnchor != 5 || r.logAuditTimelineCursor != 3 {
		t.Fatalf("anchor,cursor = %d,%d after two Shift+Left from 5, want 5,3", r.logAuditTimelineAnchor, r.logAuditTimelineCursor)
	}

	start := min(r.logAuditTimelineAnchor, r.logAuditTimelineCursor)
	end := max(r.logAuditTimelineAnchor, r.logAuditTimelineCursor)
	if start != 3 || end != 5 {
		t.Errorf("selection range = [%d,%d], want [3,5] regardless of having been built leftward", start, end)
	}
}

// TestLogAuditTimelineEnterCommitsSelectionToTimeField pins the actual
// product behavior: committing writes "between <from> and <to>" into
// the Time field using filterexpr.ParseMtime's own accepted grammar,
// and moves focus there — see commitLogAuditTimelineSelection's own
// doc comment on why focus has to move *before* SetText, not after
// (lifting the freeze-while-focused guard before the resulting
// re-bucket needs to run).
func TestLogAuditTimelineEnterCommitsSelectionToTimeField(t *testing.T) {
	r := openLogAuditTimelineViewer(t, 200, 50)
	r.app.SetFocus(r.logAuditTimelineView)
	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModShift))

	wantFrom, _ := logAuditBucketTimeRange(r.logAuditTimelineMinT, r.logAuditTimelineMaxT, r.logAuditTimelineBucketCount, 1)
	_, wantTo := logAuditBucketTimeRange(r.logAuditTimelineMinT, r.logAuditTimelineMaxT, r.logAuditTimelineBucketCount, 2)

	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	if !r.logAuditTimeField.HasFocus() {
		t.Error("committing should move focus to the Time field")
	}
	if r.logAuditTimelineFocused {
		t.Error("committing should clear logAuditTimelineFocused (focus moved away)")
	}
	got := r.logAuditTimeField.GetText()
	wantPrefix := "between " + wantFrom.Truncate(time.Second).Format("2006-01-02 15:04:05")
	if !strings.HasPrefix(got, wantPrefix) {
		t.Errorf("Time field = %q, want it to start with %q", got, wantPrefix)
	}
	if !strings.Contains(got, wantTo.Format("2006-01-02 15:04")) {
		t.Errorf("Time field = %q, want it to mention the selection's own end %v", got, wantTo)
	}

	if _, err := filterexpr.ParseMtime(got, time.Now()); err != nil {
		t.Errorf("committed text %q does not parse as a valid mtime expression: %v", got, err)
	}
}

func TestLogAuditTimelineSpaceAlsoCommits(t *testing.T) {
	r := openLogAuditTimelineViewer(t, 200, 50)
	r.app.SetFocus(r.logAuditTimelineView)
	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone))
	if r.logAuditTimeField.GetText() == "" {
		t.Error("Space should commit the current (single-bucket) selection, same as Enter")
	}
}

func TestLogAuditTimelineTabOrderBetweenLevelFieldAndTable(t *testing.T) {
	r := openLogAuditTimelineViewer(t, 200, 50)

	r.app.SetFocus(r.logAuditLevelField)
	r.logAuditLevelField.InputHandler()(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), func(tview.Primitive) {})
	if !r.logAuditTimelineView.HasFocus() {
		t.Error("Tab from the Level field should land on the Timeline, not the table directly")
	}

	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	if !r.logAuditViewerTable.HasFocus() {
		t.Error("Tab from the Timeline should land on the table")
	}

	r.captureLogAuditViewerTableKey(tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone))
	if !r.logAuditTimelineView.HasFocus() {
		t.Error("Backtab from the table should land back on the Timeline")
	}

	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone))
	if !r.logAuditLevelField.HasFocus() {
		t.Error("Backtab from the Timeline should land on the Level field")
	}
}

func TestLogAuditTimelineMouseClickSelectsSingleBucket(t *testing.T) {
	r := openLogAuditTimelineViewer(t, 200, 50)
	viewX, viewY, _, _ := r.logAuditTimelineView.GetInnerRect()
	clickCol := viewX + r.logAuditTimelineBarCol + 3

	r.captureLogAuditTimelineMouse(tview.MouseLeftDown, tcell.NewEventMouse(clickCol, viewY, tcell.Button1, 0))
	if !r.logAuditTimelineView.HasFocus() {
		t.Error("clicking a bucket should give the timeline keyboard focus")
	}
	if r.logAuditTimelineAnchor != 3 || r.logAuditTimelineCursor != 3 {
		t.Errorf("anchor,cursor = %d,%d after clicking bucket 3, want 3,3", r.logAuditTimelineAnchor, r.logAuditTimelineCursor)
	}

	r.captureLogAuditTimelineMouse(tview.MouseLeftUp, tcell.NewEventMouse(clickCol, viewY, tcell.ButtonNone, 0))
	if r.logAuditTimelineDragging {
		t.Error("MouseLeftUp should end the drag")
	}
	if r.logAuditTimeField.GetText() == "" {
		t.Error("releasing on the same bucket should still commit a (single-bucket) selection")
	}
}

// TestLogAuditTimelineMouseDragSelectsRangeRegardlessOfDirection pins
// the user's own explicit requirement: dragging leftward must select
// exactly the same way dragging rightward does — always from the
// leftmost to the rightmost bucket actually dragged over, independent
// of which edge the drag started from.
func TestLogAuditTimelineMouseDragSelectsRangeRegardlessOfDirection(t *testing.T) {
	r := openLogAuditTimelineViewer(t, 200, 50)
	viewX, viewY, _, _ := r.logAuditTimelineView.GetInnerRect()
	col := func(bucket int) int { return viewX + r.logAuditTimelineBarCol + bucket }

	r.captureLogAuditTimelineMouse(tview.MouseLeftDown, tcell.NewEventMouse(col(6), viewY, tcell.Button1, 0))
	r.captureLogAuditTimelineMouse(tview.MouseMove, tcell.NewEventMouse(col(2), viewY, tcell.Button1, 0))
	if !r.logAuditTimelineDragging {
		t.Fatal("setup: should still be dragging")
	}
	start := min(r.logAuditTimelineAnchor, r.logAuditTimelineCursor)
	end := max(r.logAuditTimelineAnchor, r.logAuditTimelineCursor)
	if start != 2 || end != 6 {
		t.Fatalf("mid-drag selection = [%d,%d], want [2,6] (dragged from 6 to 2)", start, end)
	}

	r.captureLogAuditTimelineMouse(tview.MouseLeftUp, tcell.NewEventMouse(col(2), viewY, tcell.ButtonNone, 0))
	if r.logAuditTimelineDragging {
		t.Error("MouseLeftUp should end the drag")
	}
	if r.logAuditTimeField.GetText() == "" {
		t.Error("releasing after a leftward drag should still commit a selection")
	}
}

func TestLogAuditTimelineMouseMoveStrayReleaseStillCommits(t *testing.T) {
	r := openLogAuditTimelineViewer(t, 200, 50)
	viewX, viewY, _, _ := r.logAuditTimelineView.GetInnerRect()
	col := func(bucket int) int { return viewX + r.logAuditTimelineBarCol + bucket }

	r.captureLogAuditTimelineMouse(tview.MouseLeftDown, tcell.NewEventMouse(col(1), viewY, tcell.Button1, 0))
	r.captureLogAuditTimelineMouse(tview.MouseMove, tcell.NewEventMouse(col(4), viewY, tcell.ButtonNone, 0))

	if r.logAuditTimelineDragging {
		t.Error("a MouseMove with no button held should end the drag, same as a real MouseLeftUp")
	}
	if r.logAuditTimeField.GetText() == "" {
		t.Error("a stray button-released move should still commit, not silently drop the selection")
	}
}

// TestLogAuditTimelineFreezesWhileFocused pins the follow-mode
// decision: renderLogAuditTimeline must not re-bucket (and so must not
// change what's displayed) while the timeline holds focus — a
// follow-mode tick is the only realistic way this function gets called
// in that state (see its own doc comment) — even though the table
// (not exercised here) keeps updating independently via the rest of
// renderLogAuditViewer.
func TestLogAuditTimelineFreezesWhileFocused(t *testing.T) {
	r := openLogAuditTimelineViewer(t, 200, 50)
	before := r.logAuditTimelineView.GetText(true)
	beforeCount := r.logAuditTimelineBucketCount

	r.app.SetFocus(r.logAuditTimelineView)

	moreEntries := append([]logview.Entry{}, r.logAuditAllEntries...)
	moreEntries = append(moreEntries, logview.Entry{Time: r.logAuditTimelineMaxT.Add(time.Hour), Message: "later"})
	r.renderLogAuditTimeline(moreEntries)

	if got := r.logAuditTimelineView.GetText(true); got != before {
		t.Errorf("timeline text changed while focused, want it frozen: before=%q after=%q", before, got)
	}
	if r.logAuditTimelineBucketCount != beforeCount {
		t.Errorf("bucketCount changed while focused: %d -> %d, want unchanged (frozen)", beforeCount, r.logAuditTimelineBucketCount)
	}

	r.app.SetFocus(r.logAuditViewerTable)
	r.renderLogAuditTimeline(moreEntries)
	if got := r.logAuditTimelineView.GetText(true); got == before {
		t.Error("after focus leaves, the timeline should resume reflecting new entries, not stay frozen forever")
	}
}
