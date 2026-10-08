package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/logview"
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
