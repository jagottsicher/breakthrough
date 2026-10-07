package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestToggleLogAuditGroupAndOpenViewer(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)

	r.toggleLogAuditGroup(1) // row 1 = logAuditGroups[0], the app.log group (sorted before syslog)
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

func TestOpenLogAuditViewerFallsBackToCursorRowWhenNothingChecked(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)

	r.logAuditTable.Select(1, 0) // app.log's own row, nothing checked
	r.openLogAuditViewer()

	if r.logAuditFiles != 1 {
		t.Fatalf("logAuditFiles = %d, want 1 (cursor-row fallback)", r.logAuditFiles)
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

	r.toggleLogAuditGroup(1)
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

	r.toggleLogAuditGroup(1)
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
			{Path: "access.log.2.xz", Compressed: true, Supported: false},
		}},
	}
	got := logAuditGroupInfo(groups[0])
	if got != "3 files (2 compressed) — 1 not yet supported" {
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
