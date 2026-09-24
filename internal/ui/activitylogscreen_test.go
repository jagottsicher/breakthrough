package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
)

// activityLogFixture is three entries spanning both categories and
// every level this file's own tests need — Time strictly increasing so
// "newest first" (see readActivityLogEntries) has something real to
// pin against.
func activityLogFixture() []activitylog.Entry {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	return []activitylog.Entry{
		{Time: base, Level: activitylog.LevelActions, Category: activitylog.CategoryFileOps, Message: "copied a.txt to b.txt"},
		{Time: base.Add(time.Hour), Level: activitylog.LevelErrors, Category: activitylog.CategoryRsync, Message: "rsync failed: connection refused"},
		{Time: base.Add(2 * time.Hour), Level: activitylog.LevelActions, Category: activitylog.CategoryPermissions, Message: "chmod 644 report.pdf"},
	}
}

// TestFilterActivityLogEntriesKeyword pins the "full-text over the
// message" contract from feature_ideas.txt: a case-insensitive
// substring match against Message alone, nothing else.
func TestFilterActivityLogEntriesKeyword(t *testing.T) {
	entries := activityLogFixture()

	got := filterActivityLogEntries(entries, "RSYNC", "", time.Now())
	if len(got) != 1 || got[0].Message != "rsync failed: connection refused" {
		t.Errorf("filterActivityLogEntries(keyword=RSYNC) = %+v, want just the rsync entry", got)
	}

	if got := filterActivityLogEntries(entries, "", "", time.Now()); len(got) != len(entries) {
		t.Errorf("filterActivityLogEntries(keyword=\"\") = %d entries, want all %d", len(got), len(entries))
	}
}

// TestFilterActivityLogEntriesTimeRange pins that the time field reuses
// filterexpr.ParseMtime's own grammar verbatim — "after <moment>" here,
// the same expression the panel's own Modified-time filter already
// accepts.
func TestFilterActivityLogEntriesTimeRange(t *testing.T) {
	entries := activityLogFixture()
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)

	got := filterActivityLogEntries(entries, "", "after 2026-09-01T13:00:00Z", now)
	if len(got) != 1 || got[0].Message != "chmod 644 report.pdf" {
		t.Errorf("filterActivityLogEntries(after 13:00) = %+v, want just the last (2026-09-01T14:00Z) entry", got)
	}
}

// TestFilterActivityLogEntriesInvalidTimeExprIsIgnored mirrors
// filterByMtime's own graceful-degradation contract (see panel.go): a
// time expression that fails to parse simply isn't applied, rather than
// this screen showing an error state a bare keyword filter never has to.
func TestFilterActivityLogEntriesInvalidTimeExprIsIgnored(t *testing.T) {
	entries := activityLogFixture()
	got := filterActivityLogEntries(entries, "", "not a real expression", time.Now())
	if len(got) != len(entries) {
		t.Errorf("filterActivityLogEntries with an unparseable time expr = %d entries, want all %d (filter ignored)", len(got), len(entries))
	}
}

// TestFilterActivityLogEntriesCombinesBothFilters pins that keyword and
// time apply together (AND, not OR) — an entry has to satisfy both once
// both fields hold something.
func TestFilterActivityLogEntriesCombinesBothFilters(t *testing.T) {
	entries := activityLogFixture()
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)

	got := filterActivityLogEntries(entries, "chmod", "after 2026-09-01T13:00:00Z", now)
	if len(got) != 1 {
		t.Fatalf("combined filter = %d entries, want exactly 1", len(got))
	}

	got = filterActivityLogEntries(entries, "copied", "after 2026-09-01T13:00:00Z", now)
	if len(got) != 0 {
		t.Errorf("combined filter (keyword matches an entry the time filter excludes) = %d entries, want 0", len(got))
	}
}

// writeActivityLogLines writes raw, pre-formatted lines straight into
// the real, isolated log file (see isolateActivityLogPaths) — simpler
// than going through a real Logger/Writer when the test only cares
// about what readActivityLogEntries makes of already-written lines.
func writeActivityLogLines(t *testing.T, systemDir string, entries ...activitylog.Entry) {
	t.Helper()
	if err := os.MkdirAll(systemDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	var b []byte
	for _, e := range entries {
		b = append(b, e.Format()...)
	}
	if err := os.WriteFile(filepath.Join(systemDir, "breakthrough.log"), b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// TestReadActivityLogEntriesNewestFirst pins the deliberate reversal:
// written oldest-line-first (how the real Writer appends), read back
// newest-first (see readActivityLogEntries's own doc comment on why).
func TestReadActivityLogEntriesNewestFirst(t *testing.T) {
	systemDir := isolateActivityLogPaths(t)
	fixture := activityLogFixture()
	writeActivityLogLines(t, systemDir, fixture...)

	got, err := readActivityLogEntries()
	if err != nil {
		t.Fatalf("readActivityLogEntries: %v", err)
	}
	if len(got) != len(fixture) {
		t.Fatalf("got %d entries, want %d", len(got), len(fixture))
	}
	for i, e := range got {
		want := fixture[len(fixture)-1-i]
		if !e.Time.Equal(want.Time) || e.Message != want.Message {
			t.Errorf("entry %d = %+v, want %+v (newest first)", i, e, want)
		}
	}
}

// TestReadActivityLogEntriesMissingFileIsNotAnError pins that a log
// that simply doesn't exist yet (logging just enabled, or never used)
// reads as an empty result, not a failure — see the function's own doc
// comment.
func TestReadActivityLogEntriesMissingFileIsNotAnError(t *testing.T) {
	isolateActivityLogPaths(t)

	got, err := readActivityLogEntries()
	if err != nil {
		t.Errorf("readActivityLogEntries on a missing file: err = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Errorf("readActivityLogEntries on a missing file = %d entries, want 0", len(got))
	}
}

// TestOpenActivityLogPopulatesTableFromRealFile is the end-to-end smoke
// test: a real log file, opened through openActivityLog, ends up
// rendered in the real table — not just readActivityLogEntries/
// filterActivityLogEntries individually.
func TestOpenActivityLogPopulatesTableFromRealFile(t *testing.T) {
	systemDir := isolateActivityLogPaths(t)
	writeActivityLogLines(t, systemDir, activityLogFixture()...)

	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openActivityLog()

	if r.activePage != activityLogPage {
		t.Fatalf("activePage = %q, want the Activity Log screen", r.activePage)
	}
	// Header row (0) + 3 entries.
	if got, want := r.activityLogTable.GetRowCount(), 4; got != want {
		t.Fatalf("table has %d rows, want %d (header + 3 entries)", got, want)
	}
	// Newest first (see readActivityLogEntries): row 1 is the fixture's
	// own last, latest-timestamped entry.
	if got, want := r.activityLogTable.GetCell(1, activityLogColMessage).Text, "chmod 644 report.pdf"; got != want {
		t.Errorf("row 1 Message = %q, want %q (newest entry first)", got, want)
	}
}

// TestRenderActivityLogShowsPlaceholderWhenEmpty pins the "report it,
// don't leave it looking like a successful empty read" contract for the
// genuinely-nothing-logged-yet case.
func TestRenderActivityLogShowsPlaceholderWhenEmpty(t *testing.T) {
	isolateActivityLogPaths(t)
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openActivityLog()

	if got := r.activityLogTable.GetRowCount(); got != 2 {
		t.Fatalf("table has %d rows, want 2 (header + placeholder)", got)
	}
	placeholder, _ := r.activityLogTable.GetCell(1, activityLogColTime).Text, ""
	if placeholder != "No activity logged yet." {
		t.Errorf("placeholder = %q, want %q", placeholder, "No activity logged yet.")
	}
}

// TestRenderActivityLogFiltersLiveAsYouType pins that typing into the
// keyword field re-renders immediately (SetChangedFunc), the same
// "narrows as you type" feel the panel's own filter dropdown already
// has — no separate "apply" step.
func TestRenderActivityLogFiltersLiveAsYouType(t *testing.T) {
	systemDir := isolateActivityLogPaths(t)
	writeActivityLogLines(t, systemDir, activityLogFixture()...)

	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openActivityLog()

	r.activityLogKeywordField.SetText("chmod")

	if got, want := r.activityLogTable.GetRowCount(), 2; got != want {
		t.Fatalf("table has %d rows after typing a keyword, want %d (header + 1 matching entry)", got, want)
	}
}

// TestCaptureActivityLogTableKeyEscapeCloses mirrors the Mounts/Firewall
// screens' own equivalent test.
func TestCaptureActivityLogTableKeyEscapeCloses(t *testing.T) {
	isolateActivityLogPaths(t)
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openActivityLog()

	if got := r.captureActivityLogTableKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)); got != nil {
		t.Error("captureActivityLogTableKey should consume Escape")
	}
	if r.activePage == activityLogPage {
		t.Error("Escape should have closed the Activity Log screen")
	}
}

// TestCaptureActivityLogTableKeyRReloads pins the manual-refresh key —
// a background job can still be logging while this screen sits open,
// the same reasoning Mounts/Firewall's own "r" already has.
func TestCaptureActivityLogTableKeyRReloads(t *testing.T) {
	systemDir := isolateActivityLogPaths(t)
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openActivityLog()
	if got, want := r.activityLogTable.GetRowCount(), 2; got != want {
		t.Fatalf("table has %d rows before logging anything, want %d (header + placeholder)", got, want)
	}

	writeActivityLogLines(t, systemDir, activityLogFixture()...)
	r.captureActivityLogTableKey(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModNone))

	if got, want := r.activityLogTable.GetRowCount(), 4; got != want {
		t.Errorf("table has %d rows after \"r\", want %d (header + 3 entries) — reload didn't pick up the new file content", got, want)
	}
}

// TestCaptureActivityLogTableKeyTabFocusesKeywordField pins the focus
// cycle's table-to-field leg — the fields' own activityLogFieldDone
// covers the other two legs.
func TestCaptureActivityLogTableKeyTabFocusesKeywordField(t *testing.T) {
	isolateActivityLogPaths(t)
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openActivityLog()

	r.captureActivityLogTableKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))

	if !r.activityLogKeywordField.HasFocus() {
		t.Error("Tab from the table should focus the keyword field")
	}
}

// TestActivityLogFieldDoneEscapeClosesFromEitherField pins that Escape
// closes the whole screen regardless of which of the two filter fields
// it was pressed in — not just from the table (see
// TestCaptureActivityLogTableKeyEscapeCloses).
func TestActivityLogFieldDoneEscapeClosesFromEitherField(t *testing.T) {
	isolateActivityLogPaths(t)
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openActivityLog()

	r.activityLogFieldDone(tcell.KeyEscape, r.activityLogTimeField, r.activityLogTable)

	if r.activePage == activityLogPage {
		t.Error("Escape from the keyword field should have closed the Activity Log screen")
	}
}
