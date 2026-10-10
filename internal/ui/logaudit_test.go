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
	if got := r.logAuditFocusIndicator.GetBackgroundColor(); got != r.theme.SurfaceBackground {
		t.Errorf("focus indicator background = %v, want SurfaceBackground %v once focus moves to a filter field", got, r.theme.SurfaceBackground)
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
// a real, confirmed tview gap (see restyleLogAuditFilterFields' own
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

func TestLogAuditMatchesKeyword(t *testing.T) {
	e := logview.Entry{Source: "nginx", Message: "connection timeout"}
	if !logAuditMatchesKeyword(e, "TIMEOUT") {
		t.Error("expected case-insensitive match against Message")
	}
	if !logAuditMatchesKeyword(e, "NGINX") {
		t.Error("expected case-insensitive match against Source")
	}
	if logAuditMatchesKeyword(e, "bogus") {
		t.Error("expected no match")
	}
	if !logAuditMatchesKeyword(e, "") {
		t.Error("expected blank keyword to match everything")
	}
}

func TestLogAuditHighlightWrapsEveryOccurrence(t *testing.T) {
	got := logAuditHighlight("a timeout then another timeout", "timeout", tcell.ColorRed)
	if count := strings.Count(got, "timeout"); count != 2 {
		t.Errorf("highlighted text = %q, want the raw word \"timeout\" still present twice, got %d", got, count)
	}
	if count := strings.Count(got, "[-:-:-]"); count != 2 {
		t.Errorf("highlighted text = %q, want 2 reset tags (one per occurrence)", got)
	}
}

func TestLogAuditHighlightEscapesLiteralBrackets(t *testing.T) {
	text := "tag[1] failed"
	got := logAuditHighlight(text, "", tcell.ColorRed)
	if want := tview.Escape(text); got != want {
		t.Errorf("logAuditHighlight(keyword=\"\") = %q, want tview.Escape's own output %q", got, want)
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

	if !logAuditEntryVisible(e, "", "", "", now) {
		t.Error("no filters at all should show everything")
	}
	if !logAuditEntryVisible(e, "timeout", "", "warn", now) {
		t.Error("matching keyword + matching minimum level should show the entry")
	}
	if logAuditEntryVisible(e, "timeout", "", "error", now) {
		t.Error("Warn entry must not pass a >= Error level filter")
	}
	if logAuditEntryVisible(e, "bogus", "", "", now) {
		t.Error("non-matching keyword should hide the entry regardless of level/time")
	}
	if !logAuditEntryVisible(e, "", "before 2026-10-07T17:00:00Z", "", now) {
		t.Error("entry's own Time is before the time filter's cutoff, should pass")
	}
	if logAuditEntryVisible(e, "", "after 2026-10-07T17:00:00Z", "", now) {
		t.Error("entry's own Time is before the time filter's cutoff, should be excluded by 'after'")
	}
}

func TestLogAuditEntryVisibleExcludesUnknownLevelFromLevelFilter(t *testing.T) {
	e := logview.Entry{Level: logview.LevelUnknown, Message: "no level recognized here"}
	if logAuditEntryVisible(e, "", "", "trace", time.Now()) {
		t.Error("an entry with no recognized level should not pass even a >= trace filter")
	}
	if !logAuditEntryVisible(e, "", "", "", time.Now()) {
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
