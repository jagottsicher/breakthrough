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
	if len(r.log.Groups) != 1 || r.log.Groups[0].Base != "app.log" {
		t.Fatalf("log.Groups = %+v, want one group for app.log — Discover must have gone through the remote client, not the local filesystem", r.log.Groups)
	}

	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()

	if r.log.Files != 1 {
		t.Fatalf("log.Files = %d, want 1", r.log.Files)
	}
	if len(r.log.AllEntries) != 1 {
		t.Fatalf("len(log.AllEntries) = %d, want 1", len(r.log.AllEntries))
	}
	if r.log.AllEntries[0].Level != logview.LevelError {
		t.Errorf("entries[0].Level = %v, want LevelError — the remote file's own real content should have been read and parsed", r.log.AllEntries[0].Level)
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
	if got := r.log.TitleBar.GetText(true); strings.Contains(got, "testhost") {
		t.Errorf("local title = %q, want no connection name", got)
	}

	remote := newTestFakeRemote("/remote")
	if err := r.panel.connectRemote(remote, remotefs.Connection{Host: "testhost"}); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}
	r.openLogAudit()
	if got := r.log.TitleBar.GetText(true); !strings.Contains(got, "testhost") {
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
	if len(r.log.Groups) != 2 {
		t.Fatalf("len(log.Groups) = %d, want 2 (app.log, syslog)", len(r.log.Groups))
	}
}

func TestOpenLogAuditViewerOpensGroupUnderCursor(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)

	r.log.Table.Select(1, 0) // row 1 = log.Groups[0], the app.log group (sorted before syslog)
	r.openLogAuditViewer()

	if r.activePage != logAuditViewerPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, logAuditViewerPage)
	}
	if r.log.Files != 1 {
		t.Fatalf("log.Files = %d, want 1 (just app.log)", r.log.Files)
	}
	if len(r.log.AllEntries) != 2 {
		t.Fatalf("len(log.AllEntries) = %d, want 2", len(r.log.AllEntries))
	}
}

// TestOpenLogAuditViewerFocusesTheList pins the user's own explicit
// request: opening the viewer should land ready to read/navigate
// entries right away, not typing into the keyword filter first.
func TestOpenLogAuditViewerFocusesTheList(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)

	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()

	if !r.log.ViewerTable.HasFocus() {
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

	r.log.Table.Select(1, 0)
	r.openLogAuditViewer() // focuses the table — see the test above

	if got := r.log.FocusIndicator.GetBackgroundColor(); got != r.theme.InputFocusedBackground {
		t.Errorf("focus indicator background = %v, want InputFocusedBackground %v while the list has focus", got, r.theme.InputFocusedBackground)
	}

	r.app.SetFocus(r.log.KeywordField)
	// PanelBackground, not SurfaceBackground: the viewer table itself
	// moved to PanelBackground per the user's own explicit request to
	// bring every full-screen catalog in line with the main screen it
	// fully covers (see log.ViewerTable's own comment) — this
	// indicator blends into the table it's a cue for while unfocused,
	// so it moved with it.
	if got := r.log.FocusIndicator.GetBackgroundColor(); got != r.theme.PanelBackground {
		t.Errorf("focus indicator background = %v, want PanelBackground %v once focus moves to a filter field", got, r.theme.PanelBackground)
	}
}

// TestLogAuditTimeHeaderClickTogglesSortOrder pins the "Time" header's
// own click handler — the same "click to sort, click again to
// reverse" convention the panel's own column headers already use (see
// sortArrow, panel.go) — per the user's own explicit request to read
// the list old->new or new->old. Opens newest-first by default (see
// openLogAuditViewer's own doc comment on log.NewestFirst, per a
// later, separate explicit request), so this test's own "before"
// state is newest-first, toggling to oldest-first — the opposite
// direction from when that default was still oldest-first.
func TestLogAuditTimeHeaderClickTogglesSortOrder(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir) // app.log: error @16:04:21, then info @16:04:23
	r := newTestRootForLogAudit(t, dir)
	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()

	firstEntry := func() logview.Entry {
		e, ok := r.logAuditEntryAt(1)
		if !ok {
			t.Fatal("row 1 has no entry reference")
		}
		return e
	}

	if !r.log.NewestFirst {
		t.Fatal("setup: opening the viewer should default to newest-first")
	}
	if got := firstEntry(); got.Level != logview.LevelInfo {
		t.Fatalf("before toggling, row 1 = %+v, want the newest entry (Info @16:04:23) — newest-first is the default", got)
	}

	header := r.log.ViewerTable.GetCell(0, logAuditColTime)
	if header.Clicked == nil {
		t.Fatal("Time header has no Clicked handler")
	}
	header.Clicked()
	if r.log.NewestFirst {
		t.Fatal("clicking the Time header should have cleared log.NewestFirst")
	}
	if got := firstEntry(); got.Level != logview.LevelError {
		t.Errorf("after toggling to oldest-first, row 1 = %+v, want the oldest entry (Error @16:04:21)", got)
	}
	// Re-fetched, not the same cell object header still points at:
	// renderLogAuditViewer (called from the Clicked handler itself)
	// replaces row 0's cell outright via SetCell, same as every other
	// row — header's own copy is now stale.
	if got := r.log.ViewerTable.GetCell(0, logAuditColTime).Text; !strings.Contains(got, "↑") {
		t.Errorf("Time header = %q, want a ↑ arrow once sorted oldest-first", got)
	}
}

func TestCloseLogAuditViewerReturnsToSelection(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)

	r.log.Table.Select(1, 0)
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

	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()

	r.log.ViewerTable.Select(1, 0)
	r.navigateLogAuditLevel(logview.LevelError)

	e, ok := r.logAuditEntryAt(func() int { row, _ := r.log.ViewerTable.GetSelection(); return row }())
	if !ok || e.Level != logview.LevelError {
		t.Fatalf("after navigateLogAuditLevel(Error), selected entry = %+v, ok=%v, want an Error row", e, ok)
	}
}

func TestRenderLogAuditViewerFiltersByKeyword(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)

	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()

	// SetText's own SetChangedFunc hook now dispatches the real scan
	// through scheduleLogAuditFilterRescan (see its own doc comment) —
	// tests never drain QueueUpdateDraw (this package's own established
	// convention), so the result is applied directly here instead,
	// exactly the already-computed-result shape applyLogAuditRotatedFile's
	// own tests already use.
	r.log.KeywordField.SetText("timeout")
	r.renderLogAuditViewer()
	if got := r.log.ViewerTable.GetRowCount(); got != 2 { // header + 1 match
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
	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()

	title := r.log.ViewerTitle.GetText(true)
	if !strings.Contains(title, fmt.Sprintf("showing %d/", logAuditMaxRenderedRows)) {
		t.Errorf("title = %q, want an explicit truncation notice", title)
	}
	if got := r.log.ViewerTable.GetRowCount(); got != logAuditMaxRenderedRows+1 { // +1 header
		t.Errorf("GetRowCount() = %d, want %d (header + cap)", got, logAuditMaxRenderedRows+1)
	}
}

// TestRenderLogAuditViewerKeepsGoodEntriesWhenOneFileFails pins a real,
// independently-discovered bug directly on point for the user's own
// "nicht alle Logeinträge sichtbar" report: a parse error from just
// one rotation of a logrotate family (here, a corrupted .gz) used to
// blank out the whole merged view, hiding every entry already
// successfully read from the family's other, perfectly fine files.
//
// app.log.1.gz is a rotated file, read progressively in the
// background (see startLogAuditProgressiveLoad) — nothing here drains
// r.app.QueueUpdateDraw (see TestComputeHashesUpdatesPropertiesText's
// own identical reasoning in properties_test.go), so its own result is
// applied directly via applyLogAuditRotatedFile instead, the same
// already-computed-result shape the real background loader's own
// QueueUpdateDraw callback uses.
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
	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()

	// Synchronous part: only app.log (the current, unrotated file) has
	// been read so far.
	if r.log.ParseErr != nil {
		t.Fatalf("setup: log.ParseErr = %v, want nil before the rotated file has even been attempted", r.log.ParseErr)
	}
	if len(r.log.AllEntries) != 1 {
		t.Fatalf("len(log.AllEntries) = %d, want 1 (app.log's own entry) before the rotated file has been attempted", len(r.log.AllEntries))
	}

	group := r.log.GroupOpen
	if len(group.Files) != 2 {
		t.Fatalf("setup: want 2 files in the family, got %d", len(group.Files))
	}
	rotated := group.Files[1]
	entries, err := readLogAuditFile(r.currentLogAuditSource(), rotated)
	if err == nil {
		t.Fatal("setup: expected app.log.1.gz to fail to parse")
	}
	r.applyLogAuditRotatedFile(rotated, entries, false, err)

	if len(r.log.AllEntries) != 1 {
		t.Fatalf("len(log.AllEntries) = %d, want 1 (app.log's own entry, kept despite the rotated file's own error)", len(r.log.AllEntries))
	}
	if got := r.log.ViewerTable.GetRowCount(); got != 2 { // header + the one good entry
		t.Errorf("GetRowCount() = %d, want 2 (header + 1 entry) — the error must not blank the whole table", got)
	}
	if title := r.log.ViewerTitle.GetText(true); !strings.Contains(title, "1 file failed") {
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
	r.log.Table.Select(1, 0)
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

	clickPrimitive(r, r.log.KeywordField)
	r.Draw(screen)
	if got := fieldBG(r.log.KeywordField); got != r.theme.InputFocusedBackground {
		t.Fatalf("setup: Keyword bg = %v after its own click, want InputFocusedBackground %v", got, r.theme.InputFocusedBackground)
	}

	clickPrimitive(r, r.log.TimeField)
	r.Draw(screen)
	if got := fieldBG(r.log.KeywordField); got != r.theme.InputBackground {
		t.Errorf("Keyword bg = %v after clicking Time, want it back to InputBackground %v, not stuck highlighted", got, r.theme.InputBackground)
	}
	if got := fieldBG(r.log.TimeField); got != r.theme.InputFocusedBackground {
		t.Errorf("Time bg = %v, want InputFocusedBackground %v (it's the one just clicked)", got, r.theme.InputFocusedBackground)
	}

	clickPrimitive(r, r.log.LevelField)
	r.Draw(screen)
	if got := fieldBG(r.log.KeywordField); got != r.theme.InputBackground {
		t.Errorf("Keyword bg = %v after clicking Level, want InputBackground %v", got, r.theme.InputBackground)
	}
	if got := fieldBG(r.log.TimeField); got != r.theme.InputBackground {
		t.Errorf("Time bg = %v after clicking Level, want InputBackground %v — not stuck highlighted either", got, r.theme.InputBackground)
	}
	if got := fieldBG(r.log.LevelField); got != r.theme.InputFocusedBackground {
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
	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()

	for _, tc := range []struct {
		field *tview.InputField
		label string
	}{
		{r.log.KeywordField, "Filter: "},
		{r.log.TimeField, "Time: "},
		{r.log.LevelField, "Level >= "},
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

	sizeText := r.log.Table.GetCell(1, logAuditSelSize).Text
	modText := r.log.Table.GetCell(1, logAuditSelModified).Text
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
		return logAuditEntryVisible(e, parseLogAuditFilters(keyword, timeExpr, levelText, now))
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
	now := time.Now()
	if logAuditEntryVisible(e, parseLogAuditFilters("", "", "trace", now)) {
		t.Error("an entry with no recognized level should not pass even a >= trace filter")
	}
	if !logAuditEntryVisible(e, parseLogAuditFilters("", "", "", now)) {
		t.Error("with no level filter at all, the same entry should still show")
	}
}

func TestToggleLogAuditFollowStartsAndStopsTicker(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()

	if r.log.Following {
		t.Fatal("follow should start off")
	}
	r.toggleLogAuditFollow()
	if !r.log.Following || r.log.FollowCancel == nil {
		t.Fatal("toggle should start following")
	}
	r.toggleLogAuditFollow()
	if r.log.Following || r.log.FollowCancel != nil {
		t.Fatal("toggle again should stop following")
	}
}

func TestCloseLogAuditViewerStopsFollow(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()

	r.startLogAuditFollow()
	r.closeLogAuditViewer()

	if r.log.Following {
		t.Error("closing the viewer should stop a running follow")
	}
}

func TestOpenLogAuditDetailShowsRawOnlyWhenDifferentFromMessage(t *testing.T) {
	// app.log (JSON Lines) — Raw is the full JSON line, Message only
	// the extracted "msg" field, so the two differ and Raw: must show.
	jsonDir := t.TempDir()
	writeLogAuditFixture(t, jsonDir)
	r := newTestRootForLogAudit(t, jsonDir)
	r.log.Table.Select(1, 0) // app.log group, sorted before syslog
	r.openLogAuditViewer()
	r.openLogAuditDetail(1)
	if got := r.log.DetailView.GetText(true); !strings.Contains(got, "Raw:") {
		t.Errorf("detail view for a JSON entry = %q, want a Raw: section", got)
	}

	// A FormatPlain file — Raw and Message are identical (the whole
	// line, verbatim), so no redundant second copy should appear.
	plainDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(plainDir, "plain.log"), []byte("just some unstructured text\n"), 0o644); err != nil {
		t.Fatalf("WriteFile plain.log: %v", err)
	}
	r2 := newTestRootForLogAudit(t, plainDir)
	r2.log.Table.Select(1, 0)
	r2.openLogAuditViewer()
	r2.openLogAuditDetail(1)
	if got := r2.log.DetailView.GetText(true); strings.Contains(got, "Raw:") {
		t.Errorf("detail view for a FormatPlain entry = %q, want no redundant Raw: section", got)
	}
}

// TestClickLogAuditSelectionCellOpensTheViewer pins a real, reported
// bug: clicking a log family selected the row but a second click did
// nothing to it at all — the event fell through to whatever was
// drawn underneath instead (the file panel), since tview's own Table
// never runs a row's SetSelectedFunc on a plain click, only moves the
// selection there (the same reason clickSessionsCell exists). A click
// must open the viewer directly, the same single action Enter already
// is.
func TestClickLogAuditSelectionCellOpensTheViewer(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)

	cell := r.log.Table.GetCell(1, logAuditSelName)
	if cell.Clicked == nil {
		t.Fatal("row 1's own Name cell has no Clicked handler")
	}
	cell.Clicked()

	if r.activePage != logAuditViewerPage {
		t.Errorf("activePage = %q after clicking a log family, want the viewer open", r.activePage)
	}
}

// TestClickLogAuditViewerCellOpensTheDetail mirrors
// TestClickLogAuditSelectionCellOpensTheViewer for the viewer
// table's own rows: a click used to select a log line but never open
// it the way Enter does.
func TestClickLogAuditViewerCellOpensTheDetail(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()

	cell := r.log.ViewerTable.GetCell(1, logAuditColMessage)
	if cell.Clicked == nil {
		t.Fatal("row 1's own Message cell has no Clicked handler")
	}
	cell.Clicked()

	if r.activePage != logAuditDetailPage {
		t.Errorf("activePage = %q after clicking a log line, want the detail dialog open", r.activePage)
	}
}

// TestDoubleClickLogAuditSelectionRowStillOpensTheViewer pins a real,
// reported bug: a fast second click (within tview's own 500ms
// DoubleClickInterval) fires MouseLeftDoubleClick instead of a second
// MouseLeftClick, and Table.MouseHandler has no case for that action
// at all — it used to fall through to whatever was drawn underneath
// instead (the file panel) rather than simply reopening the row a
// plain click already opens. chainTableDoubleClick's own job is to
// catch that action and rerun the same cell's Clicked func.
func TestDoubleClickLogAuditSelectionRowStillOpensTheViewer(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	drawRoot(t, r, 100, 30)

	rectX, rectY, _, _ := r.log.Table.GetInnerRect()
	fallbackCalled := false
	fallback := func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		fallbackCalled = true
		return action, event
	}
	capture := chainTableDoubleClick(r.log.Table, fallback)

	capture(tview.MouseLeftDoubleClick, tcell.NewEventMouse(rectX, rectY+1, tcell.ButtonPrimary, 0))

	if r.activePage != logAuditViewerPage {
		t.Errorf("activePage = %q after a double-click on row 1, want the viewer open", r.activePage)
	}
	if fallbackCalled {
		t.Error("a double-click landing on a row with a Clicked handler should not have reached the fallback")
	}
}

// TestDoubleClickLogAuditViewerRowStillOpensTheDetail mirrors
// TestDoubleClickLogAuditSelectionRowStillOpensTheViewer for the
// viewer table's own rows.
func TestDoubleClickLogAuditViewerRowStillOpensTheDetail(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	drawRoot(t, r, 100, 30)
	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()
	drawRoot(t, r, 100, 30)

	rectX, rectY, _, _ := r.log.ViewerTable.GetInnerRect()
	fallbackCalled := false
	fallback := func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		fallbackCalled = true
		return action, event
	}
	capture := chainTableDoubleClick(r.log.ViewerTable, fallback)

	capture(tview.MouseLeftDoubleClick, tcell.NewEventMouse(rectX, rectY+1, tcell.ButtonPrimary, 0))

	if r.activePage != logAuditDetailPage {
		t.Errorf("activePage = %q after a double-click on row 1, want the detail dialog open", r.activePage)
	}
	if fallbackCalled {
		t.Error("a double-click landing on a row with a Clicked handler should not have reached the fallback")
	}
}

// TestLogAuditTimeFieldDoesNotFilterLiveButDoesOnEnterOrLeave pins the
// user's own explicit report: re-rendering the table and re-bucketing
// the timeline on every single keystroke made typing into the Time
// field noticeably sluggish, unlike Keyword/Level (cheap per
// character) — a date/time expression only means anything once
// finished typing anyway, so it should apply on Enter, Tab/Backtab, or
// whenever focus moves on, never live.
func TestLogAuditTimeFieldDoesNotFilterLiveButDoesOnEnterOrLeave(t *testing.T) {
	r := openLogAuditTimelineViewer(t, 200, 50)
	allRows := r.log.ViewerTable.GetRowCount()
	if allRows <= 1 {
		t.Fatalf("setup: table has %d rows, want more than just the header", allRows)
	}

	// A filter that excludes every one of the fixture's 20 entries
	// (all in October 2026) — typing it should change nothing yet.
	r.log.TimeField.SetText("after 2030-01-01")
	if got := r.log.ViewerTable.GetRowCount(); got != allRows {
		t.Errorf("row count = %d after merely setting the Time field's text, want unchanged %d (no live filtering)", got, allRows)
	}

	r.log.TimeField.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})
	// 2, not 1: header row + the table's own "No entries match the
	// current filter." row, not just a bare header.
	if got := r.log.ViewerTable.GetRowCount(); got != 2 {
		t.Errorf("row count = %d after Enter, want 2 (header + the no-match row — the filter excludes everything)", got)
	}
}

// TestLogAuditTimeFieldAppliesOnLeavingViaMouseClick pins the other
// half: tview never reliably forwards Blur from an InputField's own
// embedded TextArea on a mouse-driven focus change (the same
// documented gap restyleLogAuditFocusTargets's own doc comment covers
// for styling), so applying the Time filter can't wait for the Time
// field's own Blur — it has to come from whichever *other* target's
// Focus actually fires next, which this drives through the real
// MouseHandler chain (clickPrimitive), not a direct SetFocus call,
// specifically so it exercises that real gap rather than bypassing it.
func TestLogAuditTimeFieldAppliesOnLeavingViaMouseClick(t *testing.T) {
	r := openLogAuditTimelineViewer(t, 200, 50)
	allRows := r.log.ViewerTable.GetRowCount()

	clickPrimitive(r, r.log.TimeField)
	r.log.TimeField.SetText("after 2030-01-01")
	if got := r.log.ViewerTable.GetRowCount(); got != allRows {
		t.Fatalf("setup: row count = %d after setting the Time field's text, want unchanged %d", got, allRows)
	}

	clickPrimitive(r, r.log.KeywordField)
	// 2, not 1: header row + the table's own "No entries match the
	// current filter." row, not just a bare header.
	if got := r.log.ViewerTable.GetRowCount(); got != 2 {
		t.Errorf("row count = %d after clicking away to Keyword, want 2 (header + no-match row — the Time filter should now be applied)", got)
	}
}

// writeLogAuditTimelineFixture writes 20 JSON-Lines entries, one per
// second, all on the same calendar day — predictable enough to drive
// the interactive timeline without hand-computing bucket boundaries:
// tests read back whatever log.TimelineBucketCount/MinT/MaxT the
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
// what gives log.TimelineView a real, non-zero rect — see
// logAuditTimelineUnlaidOutWidth's own doc comment), and fails the
// test outright if that didn't leave at least 2 buckets: every test
// using this helper needs room to select a sub-range, not just one
// single all-or-nothing bucket.
func openLogAuditTimelineViewer(t *testing.T, width, height int) *Root {
	t.Helper()
	dir := t.TempDir()
	writeLogAuditTimelineFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()
	drawRoot(t, r, width, height)
	if r.log.TimelineBucketCount < 2 {
		t.Fatalf("setup: bucketCount = %d at %dx%d, want at least 2 — widen the test's own draw size", r.log.TimelineBucketCount, width, height)
	}
	return r
}

func TestLogAuditTimelineArrowKeysMoveAndCollapseSelection(t *testing.T) {
	r := openLogAuditTimelineViewer(t, 200, 50)
	r.app.SetFocus(r.log.TimelineView)
	if !r.log.TimelineFocused {
		t.Fatal("setup: focusing the timeline should set log.TimelineFocused")
	}
	if r.log.TimelineAnchor != 0 || r.log.TimelineCursor != 0 {
		t.Fatalf("setup: anchor,cursor = %d,%d, want 0,0 on a fresh render", r.log.TimelineAnchor, r.log.TimelineCursor)
	}

	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	if r.log.TimelineAnchor != 2 || r.log.TimelineCursor != 2 {
		t.Errorf("after two plain Rights: anchor,cursor = %d,%d, want 2,2 (collapsed)", r.log.TimelineAnchor, r.log.TimelineCursor)
	}

	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone))
	if r.log.TimelineAnchor != 1 || r.log.TimelineCursor != 1 {
		t.Errorf("after a plain Left: anchor,cursor = %d,%d, want 1,1", r.log.TimelineAnchor, r.log.TimelineCursor)
	}
}

func TestLogAuditTimelineCursorClampsAtBothEnds(t *testing.T) {
	r := openLogAuditTimelineViewer(t, 200, 50)
	r.app.SetFocus(r.log.TimelineView)

	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone))
	if r.log.TimelineCursor != 0 {
		t.Errorf("Left at the very first bucket: cursor = %d, want clamped to 0", r.log.TimelineCursor)
	}

	last := r.log.TimelineBucketCount - 1
	for i := 0; i < r.log.TimelineBucketCount+3; i++ {
		r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	}
	if r.log.TimelineCursor != last {
		t.Errorf("Right past the last bucket repeatedly: cursor = %d, want clamped to %d", r.log.TimelineCursor, last)
	}
}

func TestLogAuditTimelineShiftArrowExtendsFromTheFixedAnchor(t *testing.T) {
	r := openLogAuditTimelineViewer(t, 200, 50)
	r.app.SetFocus(r.log.TimelineView)

	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	if r.log.TimelineCursor != 2 {
		t.Fatalf("setup: cursor = %d, want 2", r.log.TimelineCursor)
	}

	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModShift))
	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModShift))
	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModShift))
	if r.log.TimelineAnchor != 2 {
		t.Errorf("anchor = %d after three Shift+Right, want 2 (unchanged — it must stay at the pre-Shift cursor position)", r.log.TimelineAnchor)
	}
	if r.log.TimelineCursor != 5 {
		t.Errorf("cursor = %d after three Shift+Right from 2, want 5", r.log.TimelineCursor)
	}

	// A plain arrow (Shift released) must collapse the just-built range.
	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	if r.log.TimelineAnchor != 6 || r.log.TimelineCursor != 6 {
		t.Errorf("anchor,cursor = %d,%d after a plain Right, want 6,6 (collapsed)", r.log.TimelineAnchor, r.log.TimelineCursor)
	}
}

func TestLogAuditTimelineShiftArrowSelectsLeftwardJustAsWell(t *testing.T) {
	r := openLogAuditTimelineViewer(t, 200, 50)
	r.app.SetFocus(r.log.TimelineView)
	for i := 0; i < 5; i++ {
		r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	}
	if r.log.TimelineCursor != 5 {
		t.Fatalf("setup: cursor = %d, want 5", r.log.TimelineCursor)
	}

	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModShift))
	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModShift))
	if r.log.TimelineAnchor != 5 || r.log.TimelineCursor != 3 {
		t.Fatalf("anchor,cursor = %d,%d after two Shift+Left from 5, want 5,3", r.log.TimelineAnchor, r.log.TimelineCursor)
	}

	start := min(r.log.TimelineAnchor, r.log.TimelineCursor)
	end := max(r.log.TimelineAnchor, r.log.TimelineCursor)
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
	r.app.SetFocus(r.log.TimelineView)
	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModShift))

	wantFrom, _ := logAuditBucketTimeRange(r.log.TimelineMinT, r.log.TimelineMaxT, r.log.TimelineBucketCount, 1)
	_, wantTo := logAuditBucketTimeRange(r.log.TimelineMinT, r.log.TimelineMaxT, r.log.TimelineBucketCount, 2)

	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	if !r.log.TimeField.HasFocus() {
		t.Error("committing should move focus to the Time field")
	}
	if r.log.TimelineFocused {
		t.Error("committing should clear log.TimelineFocused (focus moved away)")
	}
	got := r.log.TimeField.GetText()
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

	// Per the user's own explicit report: having to press Enter a
	// second time after an already-deliberate selection was pure
	// friction. The commit above narrowed the filter to just buckets
	// 1-2 out of the fixture's 20 one-second-apart entries — the table
	// must already reflect that right now, not only after some further
	// Enter.
	if got := r.log.ViewerTable.GetRowCount(); got >= 21 {
		t.Errorf("row count = %d right after committing, want it already narrowed (fixture has 20 entries + header = 21 unfiltered)", got)
	}
}

func TestLogAuditTimelineSpaceAlsoCommits(t *testing.T) {
	r := openLogAuditTimelineViewer(t, 200, 50)
	r.app.SetFocus(r.log.TimelineView)
	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone))
	if r.log.TimeField.GetText() == "" {
		t.Error("Space should commit the current (single-bucket) selection, same as Enter")
	}
}

func TestLogAuditTimelineTabOrderBetweenLevelFieldAndTable(t *testing.T) {
	r := openLogAuditTimelineViewer(t, 200, 50)

	r.app.SetFocus(r.log.LevelField)
	r.log.LevelField.InputHandler()(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), func(tview.Primitive) {})
	if !r.log.TimelineView.HasFocus() {
		t.Error("Tab from the Level field should land on the Timeline, not the table directly")
	}

	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	if !r.log.ViewerTable.HasFocus() {
		t.Error("Tab from the Timeline should land on the table")
	}

	r.captureLogAuditViewerTableKey(tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone))
	if !r.log.TimelineView.HasFocus() {
		t.Error("Backtab from the table should land back on the Timeline")
	}

	r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone))
	if !r.log.LevelField.HasFocus() {
		t.Error("Backtab from the Timeline should land on the Level field")
	}
}

func TestLogAuditTimelineMouseClickSelectsSingleBucket(t *testing.T) {
	r := openLogAuditTimelineViewer(t, 200, 50)
	viewX, viewY, _, _ := r.log.TimelineView.GetInnerRect()
	clickCol := viewX + r.log.TimelineBarCol + 3

	r.captureLogAuditTimelineMouse(tview.MouseLeftDown, tcell.NewEventMouse(clickCol, viewY, tcell.Button1, 0))
	if !r.log.TimelineView.HasFocus() {
		t.Error("clicking a bucket should give the timeline keyboard focus")
	}
	if r.log.TimelineAnchor != 3 || r.log.TimelineCursor != 3 {
		t.Errorf("anchor,cursor = %d,%d after clicking bucket 3, want 3,3", r.log.TimelineAnchor, r.log.TimelineCursor)
	}

	r.captureLogAuditTimelineMouse(tview.MouseLeftUp, tcell.NewEventMouse(clickCol, viewY, tcell.ButtonNone, 0))
	if r.log.TimelineDragging {
		t.Error("MouseLeftUp should end the drag")
	}
	if r.log.TimeField.GetText() == "" {
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
	viewX, viewY, _, _ := r.log.TimelineView.GetInnerRect()
	col := func(bucket int) int { return viewX + r.log.TimelineBarCol + bucket }

	r.captureLogAuditTimelineMouse(tview.MouseLeftDown, tcell.NewEventMouse(col(6), viewY, tcell.Button1, 0))
	r.captureLogAuditTimelineMouse(tview.MouseMove, tcell.NewEventMouse(col(2), viewY, tcell.Button1, 0))
	if !r.log.TimelineDragging {
		t.Fatal("setup: should still be dragging")
	}
	start := min(r.log.TimelineAnchor, r.log.TimelineCursor)
	end := max(r.log.TimelineAnchor, r.log.TimelineCursor)
	if start != 2 || end != 6 {
		t.Fatalf("mid-drag selection = [%d,%d], want [2,6] (dragged from 6 to 2)", start, end)
	}

	r.captureLogAuditTimelineMouse(tview.MouseLeftUp, tcell.NewEventMouse(col(2), viewY, tcell.ButtonNone, 0))
	if r.log.TimelineDragging {
		t.Error("MouseLeftUp should end the drag")
	}
	if r.log.TimeField.GetText() == "" {
		t.Error("releasing after a leftward drag should still commit a selection")
	}
}

func TestLogAuditTimelineMouseMoveStrayReleaseStillCommits(t *testing.T) {
	r := openLogAuditTimelineViewer(t, 200, 50)
	viewX, viewY, _, _ := r.log.TimelineView.GetInnerRect()
	col := func(bucket int) int { return viewX + r.log.TimelineBarCol + bucket }

	r.captureLogAuditTimelineMouse(tview.MouseLeftDown, tcell.NewEventMouse(col(1), viewY, tcell.Button1, 0))
	r.captureLogAuditTimelineMouse(tview.MouseMove, tcell.NewEventMouse(col(4), viewY, tcell.ButtonNone, 0))

	if r.log.TimelineDragging {
		t.Error("a MouseMove with no button held should end the drag, same as a real MouseLeftUp")
	}
	if r.log.TimeField.GetText() == "" {
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
	before := r.log.TimelineView.GetText(true)
	beforeCount := r.log.TimelineBucketCount

	r.app.SetFocus(r.log.TimelineView)

	moreEntries := append([]logview.Entry{}, r.log.AllEntries...)
	moreEntries = append(moreEntries, logview.Entry{Time: r.log.TimelineMaxT.Add(time.Hour), Message: "later"})
	r.renderLogAuditTimeline(moreEntries)

	if got := r.log.TimelineView.GetText(true); got != before {
		t.Errorf("timeline text changed while focused, want it frozen: before=%q after=%q", before, got)
	}
	if r.log.TimelineBucketCount != beforeCount {
		t.Errorf("bucketCount changed while focused: %d -> %d, want unchanged (frozen)", beforeCount, r.log.TimelineBucketCount)
	}

	r.app.SetFocus(r.log.ViewerTable)
	r.renderLogAuditTimeline(moreEntries)
	if got := r.log.TimelineView.GetText(true); got == before {
		t.Error("after focus leaves, the timeline should resume reflecting new entries, not stay frozen forever")
	}
}

// TestGCReachesAIConsigliereFromEveryLogAuditCapture pins "gC" reaching
// the AI Consigliere from inside Log Audit — see startConsigliereChord's
// own doc comment in consigliere.go. acceptsPlainKeyCommand (see
// keymap.go) never passes while Log Audit is the active full-screen
// overlay, so "g" alone could never have started the chord without
// each of these InputCapture functions explicitly doing it; this
// exercises that for all four, including the one real trade-off
// (captureLogAuditDetailKey's own case, which gives up tview's
// built-in vi-style "g" = scroll-to-top on that specific TextView —
// see its own doc comment).
func TestGCReachesAIConsigliereFromEveryLogAuditCapture(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)

	for _, tc := range []struct {
		name  string
		setup func(r *Root) func(*tcell.EventKey) *tcell.EventKey
	}{
		{"Selection", func(r *Root) func(*tcell.EventKey) *tcell.EventKey {
			return r.captureLogAuditSelectionTableKey
		}},
		{"Viewer", func(r *Root) func(*tcell.EventKey) *tcell.EventKey {
			r.log.Table.Select(1, 0)
			r.openLogAuditViewer()
			return r.captureLogAuditViewerTableKey
		}},
		{"Timeline", func(r *Root) func(*tcell.EventKey) *tcell.EventKey {
			r.log.Table.Select(1, 0)
			r.openLogAuditViewer()
			return r.captureLogAuditTimelineKey
		}},
		{"Detail", func(r *Root) func(*tcell.EventKey) *tcell.EventKey {
			r.log.Table.Select(1, 0)
			r.openLogAuditViewer()
			r.openLogAuditDetail(1)
			return r.captureLogAuditDetailKey
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRootForLogAudit(t, dir)
			capture := tc.setup(r)

			if got := capture(tcell.NewEventKey(tcell.KeyRune, 'g', tcell.ModNone)); got != nil {
				t.Fatalf("capture('g') = %v, want nil (consumed, chord started)", got)
			}
			if r.pendingChord != 'g' {
				t.Fatalf("pendingChord = %q, want 'g'", string(r.pendingChord))
			}

			if !r.HandlePlainKey(tcell.NewEventKey(tcell.KeyRune, 'C', tcell.ModNone)) {
				t.Fatal("the resolving 'C' should have been consumed")
			}
			if !r.consigliere.Visible {
				t.Error("\"gC\" should have opened the AI Consigliere sidebar")
			}
		})
	}
}

// TestGCInLogAuditDoesNotReachOtherGotoMembers pins the actual bug
// pendingChordFamily (see its own doc comment in root.go) exists to
// fix: before it, resolveChord re-derived whatever family was pending
// by its prefix rune alone, so starting the chord from inside Log
// Audit on a bare 'g' would have resolved against the real "goto"
// family (chordFamilies' own 'g' entry) — making gg/gh/gu/gb/... all
// reachable here too, silently acting on the hidden file panel
// underneath. Only 'C' may resolve; anything else must report "not a
// command", not run some other goto member.
func TestGCInLogAuditDoesNotReachOtherGotoMembers(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	beforePath := r.panel.path

	r.captureLogAuditSelectionTableKey(tcell.NewEventKey(tcell.KeyRune, 'g', tcell.ModNone))
	if !r.HandlePlainKey(tcell.NewEventKey(tcell.KeyRune, 'b', tcell.ModNone)) {
		t.Fatal("the resolving 'b' should still have been consumed (even though it's unbound here)")
	}

	if r.consigliere.Visible {
		t.Error("'b' is not 'C' — this must not have opened the AI Consigliere")
	}
	// The real goto family's own "gb" navigates the panel straight to
	// the Trash directory (openTrash) — no overlay of its own, so the
	// panel's own path is the one reliable signal that it never ran.
	// (The expected "not a command" transient error this resolves to
	// instead is itself a real, if short-lived, overlay — asserting
	// activePage stayed completely unchanged would be a false
	// negative, not a stronger check.)
	if r.panel.path != beforePath {
		t.Errorf("panel.path changed to %q, want unchanged %q — no goto-family member should have touched the hidden panel", r.panel.path, beforePath)
	}
}

// TestGCUnknownSecondKeyInLogAuditDoesNotCloseTheScreen is the exact
// scenario the user reported: pressing "g" then some other key inside
// Log Audit (not just the file panel) must show the "not a command"
// notice without that notice itself kicking them back out to the file
// panel — see showTransientErrorOverOverlay's own doc comment for the
// general fix. Checked on both the selection screen and the viewer,
// since both wire "gC" in independently (see
// captureLogAuditSelectionTableKey/captureLogAuditViewerTableKey).
func TestGCUnknownSecondKeyInLogAuditDoesNotCloseTheScreen(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)

	for _, tc := range []struct {
		name  string
		setup func(r *Root) (capture func(*tcell.EventKey) *tcell.EventKey, wantPage string)
	}{
		{"Selection", func(r *Root) (func(*tcell.EventKey) *tcell.EventKey, string) {
			return r.captureLogAuditSelectionTableKey, logAuditSelectionPage
		}},
		{"Viewer", func(r *Root) (func(*tcell.EventKey) *tcell.EventKey, string) {
			r.log.Table.Select(1, 0)
			r.openLogAuditViewer()
			return r.captureLogAuditViewerTableKey, logAuditViewerPage
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRootForLogAudit(t, dir)
			capture, wantPage := tc.setup(r)

			capture(tcell.NewEventKey(tcell.KeyRune, 'g', tcell.ModNone))
			r.HandlePlainKey(tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone)) // not 'C'

			if r.activePage != errorPage {
				t.Fatalf("activePage = %q, want the notice layered on top as %q", r.activePage, errorPage)
			}

			r.hideOverlay() // dismiss the notice

			if r.activePage != wantPage {
				t.Errorf("activePage after dismissing the notice = %q, want %q restored", r.activePage, wantPage)
			}
		})
	}
}

// TestTabBacktabReachConsigliereFromLogAuditSelection pins the entry
// point captureLogAuditSelectionTableKey now owns: Tab/Backtab from
// the selection table reach the AI Consigliere sidebar once it's
// open — the only other stop on this screen (see
// startConsigliereChord's own doc comment in consigliere.go).
func TestTabBacktabReachConsigliereFromLogAuditSelection(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.showConsigliere()
	r.app.SetFocus(r.log.Table)

	if got := r.captureLogAuditSelectionTableKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)); got != nil {
		t.Fatalf("capture(Tab) = %v, want nil (consumed)", got)
	}
	if !r.consigliere.Input.HasFocus() {
		t.Error("Tab should have reached consigliere.Input")
	}
}

// TestTabFromLogAuditViewerTableReachesConsigliere and
// TestBacktabFromLogAuditKeywordReachesConsigliere pin the two edges
// patched into the viewer's own fixed field chain (Keyword -> Time ->
// Level -> Timeline -> Table -> wraps to Keyword): Table's own Tab,
// and Keyword's own Backtab, now reach the sidebar instead of
// wrapping within the chain, once it's open.
func TestTabFromLogAuditViewerTableReachesConsigliere(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()
	r.showConsigliere()
	r.app.SetFocus(r.log.ViewerTable)

	if got := r.captureLogAuditViewerTableKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)); got != nil {
		t.Fatalf("capture(Tab) = %v, want nil (consumed)", got)
	}
	if !r.consigliere.Input.HasFocus() {
		t.Error("Tab from the table should have reached consigliere.Input, not wrapped to Keyword")
	}
}

func TestBacktabFromLogAuditKeywordReachesConsigliere(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()
	r.showConsigliere()

	r.app.SetFocus(r.log.KeywordField)
	// Drives the field's own SetDoneFunc through its real
	// InputHandler, the same pattern this package's other field-chain
	// tests already use (tview.InputField exposes no direct way to
	// call a DoneFunc set on it).
	r.log.KeywordField.InputHandler()(tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone), func(tview.Primitive) {})

	// Lands on consigliere.History, not consigliere.Input, whenever
	// there's something to scroll to (the default, tiny Root rect this
	// test runs on makes consigliereSplashArt overflow it — same
	// precondition TestCycleFocusShortcutLoopsPanelInputHistoryPanel's
	// own doc comment already documents) — either one still proves
	// the backward edge reached consigliere instead of wrapping to
	// the table.
	if !r.consigliere.Input.HasFocus() && !r.consigliere.History.HasFocus() {
		t.Error("Backtab from Keyword should have reached consigliere, not wrapped to the table")
	}
}

// TestTabBacktabReachConsigliereFromLogAuditDetail pins the entry
// point captureLogAuditDetailKey now owns — Tab/Backtab are not a
// tview.TextView built-in binding to begin with (only 'g'/'G'/'j'/'k'/
// 'h'/'l' are — see textview.go), so neither was ever claimed before.
func TestTabBacktabReachConsigliereFromLogAuditDetail(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()
	r.openLogAuditDetail(1)
	r.showConsigliere()
	r.app.SetFocus(r.log.DetailView)

	if got := r.captureLogAuditDetailKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)); got != nil {
		t.Fatalf("capture(Tab) = %v, want nil (consumed)", got)
	}
	if !r.consigliere.Input.HasFocus() {
		t.Error("Tab should have reached consigliere.Input")
	}
}

// TestLogAuditDetailDragCanMoveUpAndLeft pins the user's own explicit
// request that the Log entry detail dialog become movable — mirrors
// TestSearchDragCanMoveUpAndLeft (search_test.go): dragging up/left
// must work exactly like dragging down/right, not just one direction
// (see dragLogAuditDetailMouseCapture's own doc comment on why
// captureOutsideClick needs its own log.DetailDragging check for
// this).
func TestLogAuditDetailDragCanMoveUpAndLeft(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	drawRoot(t, r, 300, 40)
	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()
	r.openLogAuditDetail(1)

	r.moveLogAuditDetail(150, 20)
	startX, startY, width, height := r.log.DetailLayout.GetRect()

	r.dragLogAuditDetailMouseCapture(tview.MouseLeftDown, tcell.NewEventMouse(startX+2, startY, tcell.ButtonPrimary, 0))
	if !r.log.DetailDragging {
		t.Fatal("setup: pressing the title bar should have started a drag")
	}

	newX, newY := startX-5, startY-3
	r.captureOutsideClick(tview.MouseMove, tcell.NewEventMouse(newX+2, newY, tcell.ButtonPrimary, 0))
	r.captureOutsideClick(tview.MouseLeftUp, tcell.NewEventMouse(newX+2, newY, tcell.ButtonNone, 0))

	gotX, gotY, gotWidth, gotHeight := r.log.DetailLayout.GetRect()
	if gotX != newX || gotY != newY {
		t.Errorf("rect = (%d,%d), want (%d,%d) — dragging up/left should move the dialog exactly like dragging down/right does", gotX, gotY, newX, newY)
	}
	if gotWidth != width || gotHeight != height {
		t.Errorf("size changed during a pure move: (%d,%d) -> (%d,%d), want unchanged", width, height, gotWidth, gotHeight)
	}
	if r.log.DetailDragging {
		t.Error("log.DetailDragging should be false after MouseLeftUp")
	}
}

// TestLogAuditDetailAltArrowMovesTheDialog pins the keyboard half of
// the same request: Alt+Arrow moves the dialog one cell at a time,
// the same convention toolWindow's own InputHandler already
// establishes (toolwindow.go).
func TestLogAuditDetailAltArrowMovesTheDialog(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	drawRoot(t, r, 300, 40)
	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()
	r.openLogAuditDetail(1)

	r.moveLogAuditDetail(150, 20)
	startX, startY, _, _ := r.log.DetailLayout.GetRect()

	if got := r.captureLogAuditDetailKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModAlt)); got != nil {
		t.Fatalf("capture(Alt+Up) = %v, want nil (consumed)", got)
	}
	if got := r.captureLogAuditDetailKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModAlt)); got != nil {
		t.Fatalf("capture(Alt+Left) = %v, want nil (consumed)", got)
	}

	gotX, gotY, _, _ := r.log.DetailLayout.GetRect()
	if gotX != startX-1 || gotY != startY-1 {
		t.Errorf("rect = (%d,%d), want (%d,%d) after Alt+Up then Alt+Left", gotX, gotY, startX-1, startY-1)
	}
}

// TestLogAuditTimeFieldLabelTurnsRedOnAnUnparseableExpression pins a
// real, reported bug: "between 2026-10-26 00:00:01 and 2026-10-26
// 29:59:59" (29 is not a valid hour) used to be accepted with no
// visible sign anything was wrong — logAuditEntryVisible silently
// skips the time filter entirely on a parse error, matching every
// entry instead of narrowing them. The label's own color is now that
// sign.
func TestLogAuditTimeFieldLabelTurnsRedOnAnUnparseableExpression(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.openLogAuditViewer()

	r.log.TimeField.SetText("between 2026-10-26 00:00:01 and 2026-10-26 29:59:59")

	got, _, _ := r.log.TimeField.GetLabelStyle().Decompose()
	if want := tcell.ColorRed; got != want {
		t.Errorf("label color = %v, want %v for an unparseable expression", got, want)
	}
}

// TestLogAuditTimeFieldLabelTurnsGreenOnAParseableExpression is the
// other half: a real, recognized expression (or an empty field) gets
// a bright, attention-grabbing green instead — deliberately not this
// app's own themed EntryExecutable, which is muted by design (see its
// own doc comment) — per the user's own explicit request.
func TestLogAuditTimeFieldLabelTurnsGreenOnAParseableExpression(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.openLogAuditViewer()

	r.log.TimeField.SetText("last 7 days")
	got, _, _ := r.log.TimeField.GetLabelStyle().Decompose()
	if want := tcell.ColorLime; got != want {
		t.Errorf("label color = %v, want %v for a parseable expression", got, want)
	}

	r.log.TimeField.SetText("")
	got, _, _ = r.log.TimeField.GetLabelStyle().Decompose()
	if want := tcell.ColorLime; got != want {
		t.Errorf("label color = %v, want %v for an empty field", got, want)
	}
}

// TestLogAuditKeywordFieldLabelTurnsRedOnAnUnparseableExpression pins
// the same silent-degradation gap logAuditKeywordQuery's own doc
// comment documents, extended to this field per the user's own
// explicit follow-up request: an unbalanced-parens typo used to be
// accepted with no visible sign anything was wrong.
func TestLogAuditKeywordFieldLabelTurnsRedOnAnUnparseableExpression(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.openLogAuditViewer()

	r.log.KeywordField.SetText("foo AND (bar")

	got, _, _ := r.log.KeywordField.GetLabelStyle().Decompose()
	if want := tcell.ColorRed; got != want {
		t.Errorf("label color = %v, want %v for an unparseable expression", got, want)
	}
}

// TestLogAuditKeywordFieldLabelTurnsGreenOnAParseableExpression is the
// other half: a real, recognized expression (or an empty field) gets
// the bright attention-grabbing green instead.
func TestLogAuditKeywordFieldLabelTurnsGreenOnAParseableExpression(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.openLogAuditViewer()

	r.log.KeywordField.SetText("foo AND (bar OR baz)")
	got, _, _ := r.log.KeywordField.GetLabelStyle().Decompose()
	if want := tcell.ColorLime; got != want {
		t.Errorf("label color = %v, want %v for a parseable expression", got, want)
	}

	r.log.KeywordField.SetText("")
	got, _, _ = r.log.KeywordField.GetLabelStyle().Decompose()
	if want := tcell.ColorLime; got != want {
		t.Errorf("label color = %v, want %v for an empty field", got, want)
	}
}

// TestLogAuditLevelFieldLabelTurnsRedOnAnUnrecognizedLevel pins the
// same silent-degradation gap logAuditMinLevel's own doc comment
// documents, extended to this field per the user's own explicit
// follow-up request: a typo'd level name used to be accepted with no
// visible sign anything was wrong.
func TestLogAuditLevelFieldLabelTurnsRedOnAnUnrecognizedLevel(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.openLogAuditViewer()

	r.log.LevelField.SetText("wrn")

	got, _, _ := r.log.LevelField.GetLabelStyle().Decompose()
	if want := tcell.ColorRed; got != want {
		t.Errorf("label color = %v, want %v for an unrecognized level", got, want)
	}
}

// TestLogAuditLevelFieldLabelTurnsGreenOnARecognizedLevel is the other
// half: a real, recognized level name (or an empty field) gets the
// bright attention-grabbing green instead.
func TestLogAuditLevelFieldLabelTurnsGreenOnARecognizedLevel(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.openLogAuditViewer()

	r.log.LevelField.SetText("warn")
	got, _, _ := r.log.LevelField.GetLabelStyle().Decompose()
	if want := tcell.ColorLime; got != want {
		t.Errorf("label color = %v, want %v for a recognized level", got, want)
	}

	r.log.LevelField.SetText("")
	got, _, _ = r.log.LevelField.GetLabelStyle().Decompose()
	if want := tcell.ColorLime; got != want {
		t.Errorf("label color = %v, want %v for an empty field", got, want)
	}
}

// TestFullTabLapFromLogAuditViewerVisitsEveryFieldAndConsigliere is the
// exact scenario the user reported: opening the AI Consigliere from
// the viewer's own table used to collapse the whole ring down to just
// input <-> history <-> table, permanently orphaning Keyword/Time/
// Level/Timeline — a real, reported bug in how the sidebar was first
// spliced into an existing host ring. Drives a full lap and asserts
// every stop is actually visited, not just the one hop the earlier,
// narrower tests already covered.
func TestFullTabLapFromLogAuditViewerVisitsEveryFieldAndConsigliere(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()
	r.app.SetFocus(r.log.ViewerTable)
	// The real "gC" entry path, not a direct showConsigliere() call —
	// that's what actually sets consigliere.ForwardExit (see
	// startConsigliereChord's own doc comment), the whole thing this
	// test exists to pin.
	r.captureLogAuditViewerTableKey(tcell.NewEventKey(tcell.KeyRune, 'g', tcell.ModNone))
	r.HandlePlainKey(tcell.NewEventKey(tcell.KeyRune, 'C', tcell.ModNone))

	tabFrom := func(current tview.Primitive) tview.Primitive {
		switch current {
		case r.log.ViewerTable:
			r.captureLogAuditViewerTableKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
		case r.log.KeywordField, r.log.TimeField, r.log.LevelField:
			current.(*tview.InputField).InputHandler()(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), func(tview.Primitive) {})
		case r.log.TimelineView:
			r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
		case r.consigliere.Input:
			r.handleConsigliereInputDone(tcell.KeyTab)
		case r.consigliere.History:
			r.captureConsigliereHistoryKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
		default:
			t.Fatalf("tabFrom: unhandled current focus %v", current)
		}
		return r.app.GetFocus()
	}

	var visited []tview.Primitive
	var current tview.Primitive = r.consigliere.Input // where "gC" just left focus
	for i := 0; i < 8; i++ {
		current = tabFrom(current)
		visited = append(visited, current)
		if current == r.consigliere.Input && i > 0 {
			break // back to the start — one full lap done
		}
	}

	want := map[string]tview.Primitive{
		"consigliere.Input": r.consigliere.Input,
		"Keyword":           r.log.KeywordField,
		"Time":              r.log.TimeField,
		"Level":             r.log.LevelField,
		"Timeline":          r.log.TimelineView,
		"Table":             r.log.ViewerTable,
	}
	for name, stop := range want {
		found := false
		for _, v := range visited {
			if v == stop {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s was never reached in one full Tab lap: %v", name, visited)
		}
	}
}

// TestConsigliereOpenedFromSelectionThenDrilledIntoViewerReJoinsTheViewerRing
// is the exact scenario the user reported: open the AI Consigliere
// from Log Audit's own selection screen (gC while r.log.Table has
// focus — captures consigliere.ReturnFocus pointing at it), then drill
// into a specific file's viewer (still open, per the user's own
// earlier explicit request that it survive this) — the selection
// screen's own table is now covered, not destroyed, so without
// consigliere.ExitPage's own staleness check, Tab/Backtab out of the
// sidebar kept landing on it anyway: a real, reported dead end ("ich
// komme aus dem Consig nicht mehr raus"). Tab from consigliere.Input
// must re-join the *viewer's* own ring (Keyword/Time/Level/Timeline/
// Table), never the hidden selection table.
func TestConsigliereOpenedFromSelectionThenDrilledIntoViewerReJoinsTheViewerRing(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.captureLogAuditSelectionTableKey(tcell.NewEventKey(tcell.KeyRune, 'g', tcell.ModNone))
	r.HandlePlainKey(tcell.NewEventKey(tcell.KeyRune, 'C', tcell.ModNone))
	if !r.consigliere.Visible {
		t.Fatal("setup: \"gC\" from the selection screen should have opened the sidebar")
	}

	r.log.Table.Select(1, 0)
	r.openLogAuditViewer() // still open while this runs, per the user's own explicit request

	tabFrom := func(current tview.Primitive) tview.Primitive {
		switch current {
		case r.log.ViewerTable:
			r.captureLogAuditViewerTableKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
		case r.log.KeywordField, r.log.TimeField, r.log.LevelField:
			current.(*tview.InputField).InputHandler()(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), func(tview.Primitive) {})
		case r.log.TimelineView:
			r.captureLogAuditTimelineKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
		case r.consigliere.Input:
			r.handleConsigliereInputDone(tcell.KeyTab)
		case r.consigliere.History:
			r.captureConsigliereHistoryKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
		default:
			t.Fatalf("tabFrom: unhandled current focus %v", current)
		}
		return r.app.GetFocus()
	}

	var visited []tview.Primitive
	var current tview.Primitive = r.consigliere.Input
	for i := 0; i < 8; i++ {
		current = tabFrom(current)
		visited = append(visited, current)
		if current == r.log.Table {
			t.Fatalf("Tab landed on the hidden selection table (covered by the viewer, not destroyed): %v", visited)
		}
		if current == r.consigliere.Input && i > 0 {
			break
		}
	}

	want := map[string]tview.Primitive{
		"Keyword":  r.log.KeywordField,
		"Time":     r.log.TimeField,
		"Level":    r.log.LevelField,
		"Timeline": r.log.TimelineView,
		"Table":    r.log.ViewerTable,
	}
	for name, stop := range want {
		found := false
		for _, v := range visited {
			if v == stop {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s was never reached in one full Tab lap: %v", name, visited)
		}
	}
}

// TestEscapeClosesConsigliereEvenAfterDrillingToADifferentPage pins
// the one other place the old "same page the whole time" assumption
// could still be hiding, per the advisor's own explicit caution:
// hideConsigliere must still work correctly from
// consigliere.History/consigliere.Input after the host page has moved
// on, not just the Tab/Backtab paths the rest of this test covers.
func TestEscapeClosesConsigliereEvenAfterDrillingToADifferentPage(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.captureLogAuditSelectionTableKey(tcell.NewEventKey(tcell.KeyRune, 'g', tcell.ModNone))
	r.HandlePlainKey(tcell.NewEventKey(tcell.KeyRune, 'C', tcell.ModNone))

	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()

	r.handleConsigliereInputDone(tcell.KeyEscape)

	if r.consigliere.Visible {
		t.Error("Escape should have closed the sidebar even after drilling into the viewer")
	}
	if r.activePage != logAuditViewerPage {
		t.Errorf("activePage = %q, want %q — closing the sidebar must not touch the still-open viewer", r.activePage, logAuditViewerPage)
	}
}

// BenchmarkLogAuditVisibleEntries measures logAuditVisibleEntries
// against a synthetic ~75k-entry family with Time populated the same
// way the real, user-reported slow case had it (a wide "between ...
// and ..." range covering the whole family) — the exact shape that
// used to re-run filterexpr.ParseMtime, up to eleven date/time layout
// attempts, once per entry instead of once per render (see
// parseLogAuditFilters' own doc comment). Run with
// `go test -bench LogAuditVisibleEntries -benchtime=10x ./internal/ui`
// to compare against a pre-fix checkout.
func BenchmarkLogAuditVisibleEntries(b *testing.B) {
	const n = 75000
	start := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	entries := make([]logview.Entry, n)
	for i := range entries {
		entries[i] = logview.Entry{
			Time:    start.Add(time.Duration(i) * time.Minute),
			Level:   logview.LevelInfo,
			Source:  "benchd",
			Message: "connection established",
		}
	}

	r := &Root{}
	r.log.AllEntries = entries
	r.log.KeywordField = tview.NewInputField()
	r.log.KeywordField.SetText("connection")
	r.log.TimeField = tview.NewInputField()
	r.log.TimeField.SetText("between 2026-09-18 00:00:00 and 2026-11-02 00:00:00")
	r.log.LevelField = tview.NewInputField()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.logAuditVisibleEntries()
	}
}

// BenchmarkRenderLogAuditViewer measures the full per-keystroke path —
// not just logAuditVisibleEntries (see BenchmarkLogAuditVisibleEntries
// just above), but also renderLogAuditTimeline's own bucketing over
// every matching entry (uncapped — logAuditMaxRenderedRows only caps
// the table, not the timeline or the title bar's own counts) and the
// up-to-2000-row table rebuild itself, including logAuditHighlight's
// per-row keyword search. Uses a real *Root (newTestRootForLogAudit's
// own construction, via a minimal on-disk fixture) with AllEntries
// swapped out for the large synthetic set, so every widget
// renderLogAuditViewer touches is real, not nil. Run with
// `go test -bench RenderLogAuditViewer -benchtime=10x ./internal/ui`.
func BenchmarkRenderLogAuditViewer(b *testing.B) {
	const n = 75000
	start := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	entries := make([]logview.Entry, n)
	for i := range entries {
		entries[i] = logview.Entry{
			Time:    start.Add(time.Duration(i) * time.Minute),
			Level:   logview.LevelInfo,
			Source:  "benchd",
			Message: "connection established",
		}
	}

	dir := b.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.log"), []byte("placeholder\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		b.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 200, 60)
	r.openLogAudit()
	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()

	r.log.AllEntries = entries
	r.log.KeywordField.SetText("connection")
	r.log.TimeField.SetText("between 2026-09-18 00:00:00 and 2026-11-02 00:00:00")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.renderLogAuditViewer()
	}
}

// BenchmarkApplyLogAuditRotatedFile measures startLogAuditProgressiveLoad's
// own hot path — applyLogAuditRotatedFile called once per rotated file,
// with an active Keyword filter (the real-world "already searching while
// still loading" report). Run with
// `go test -bench ApplyLogAuditRotatedFile -benchtime=5x ./internal/ui`.
func BenchmarkApplyLogAuditRotatedFile(b *testing.B) {
	const files = 40
	const perFile = 30000 // 40*30000 = 1.2M, scaled down from the reported 4.4M for benchmark runtime
	start := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)

	dir := b.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.log"), []byte("placeholder\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		b.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 200, 60)
	r.openLogAudit()
	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()
	r.log.KeywordField.SetText("connection")
	r.log.RotatedCache = map[string][]logview.Entry{}

	batches := make([][]logview.Entry, files)
	for i := range batches {
		entries := make([]logview.Entry, perFile)
		for j := range entries {
			entries[j] = logview.Entry{
				Time:    start.Add(time.Duration(i*perFile+j) * time.Second),
				Level:   logview.LevelInfo,
				Source:  "benchd",
				Message: "connection established",
			}
		}
		batches[i] = entries
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		r.log.RotatedEntries = nil
		r.log.RotatedFiles = 0
		r.log.RotatedSkipped = 0
		r.log.RotatedErr = nil
		r.log.RotatedErrCount = 0
		r.log.RotatedWarnCount = 0
		r.log.LastCombineAt = time.Time{}
		r.log.CombineDirty = false
		b.StartTimer()
		for fi, entries := range batches {
			r.applyLogAuditRotatedFile(logview.CandidateFile{Path: fmt.Sprintf("app.log.%d", fi+1)}, entries, false, nil)
		}
		r.finishLogAuditProgressiveLoad()
	}
}

// controlledLogAuditScanFn wires r.log.ScanFn to a hook that never
// calls done on its own — it captures the call instead, so a test can
// dispatch scheduleLogAuditFilterRescan and then decide when (and
// with what result) to let it "land", exactly mirroring what the real
// implementation (defaultLogAuditScanFn) would eventually do via
// QueueUpdateDraw, just without a real goroutine or Application.Run
// to drain it — the same shape controlledOnFetchEntries already
// establishes for remote navigation (panelload_test.go).
func controlledLogAuditScanFn(r *Root) (fire func(shown []logview.Entry)) {
	var done func([]logview.Entry)
	r.log.ScanFn = func(entries []logview.Entry, filters logAuditParsedFilters, d func([]logview.Entry)) {
		done = d
	}
	return func(shown []logview.Entry) {
		if done == nil {
			panic("fire called before ScanFn was ever invoked")
		}
		done(shown)
	}
}

// TestScheduleLogAuditFilterRescanAppliesTheResultOnceItLands pins the
// basic shape: the table keeps showing whatever it already had until
// the background scan's own result lands, then renderLogAuditViewerFor
// applies it.
func TestScheduleLogAuditFilterRescanAppliesTheResultOnceItLands(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()
	fire := controlledLogAuditScanFn(r)

	r.log.KeywordField.SetText("timeout")
	if got := r.log.ViewerTable.GetRowCount(); got == 2 {
		t.Fatal("table already shows the filtered result before the scan landed")
	}

	entries := r.log.AllEntries
	var want []logview.Entry
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Message), "timeout") {
			want = append(want, e)
		}
	}
	fire(want)

	if got := r.log.ViewerTable.GetRowCount(); got != 2 { // header + 1 match
		t.Errorf("GetRowCount() after the scan landed = %d, want 2 (header + 1 match)", got)
	}
}

// TestScheduleLogAuditFilterRescanCoalescesRapidTyping pins the
// no-concurrent-scans fix directly: a second keystroke while the first
// one's own scan is still in flight must NOT dispatch a second,
// overlapping scan (the real cost behind "stalls... once a keyword
// search is already running" on a multi-million-entry family) — it
// coalesces into FilterScanPending instead, and runLogAuditFilterScan
// re-dispatches exactly once, against the field's current text, right
// after the in-flight one completes. Also re-pins the same
// "a background answer for a target that's no longer current gets
// thrown away" rule applyPanelDiskUsage/applyLogAuditRotatedFile's own
// stale-result guards already establish: the superseded first scan's
// own eventual result must never reach the table.
func TestScheduleLogAuditFilterRescanCoalescesRapidTyping(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()

	var firstDone func([]logview.Entry)
	r.log.ScanFn = func(entries []logview.Entry, filters logAuditParsedFilters, done func([]logview.Entry)) {
		firstDone = done
	}
	r.log.KeywordField.SetText("t")
	if firstDone == nil {
		t.Fatal("setup: first scan never dispatched")
	}

	var coalescedDone func([]logview.Entry)
	r.log.ScanFn = func(entries []logview.Entry, filters logAuditParsedFilters, done func([]logview.Entry)) {
		coalescedDone = done
	}
	r.log.KeywordField.SetText("ti")
	if coalescedDone != nil {
		t.Fatal("second keystroke dispatched its own scan while the first was still in flight — should have coalesced instead")
	}
	if !r.log.FilterScanPending {
		t.Fatal("setup: FilterScanPending not set after a keystroke arrived while a scan was in flight")
	}

	firstDone([]logview.Entry{{Message: "stale result from the first keystroke"}}) // the stale, superseded one

	if coalescedDone == nil {
		t.Fatal("the coalesced rescan never dispatched once the in-flight scan completed")
	}
	if got := r.log.ViewerTable.GetCell(1, 0).Text; strings.Contains(got, "stale result") {
		t.Errorf("table cell = %q, the stale first scan's own result must not have been applied", got)
	}

	coalescedDone(nil)                                    // the coalesced scan's own (empty, for simplicity) result for "ti"
	if got := r.log.ViewerTable.GetRowCount(); got != 2 { // header + placeholder (showTablePlaceholder sets row 1, header stays at row 0)
		t.Errorf("GetRowCount() after the coalesced result landed = %d, want 2 (header + placeholder)", got)
	}
}
