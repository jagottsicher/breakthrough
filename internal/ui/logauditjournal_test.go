package ui

import (
	"strings"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/logview"
)

func requireJournalctlForUI(t *testing.T) {
	t.Helper()
	if !logview.JournalAvailable() {
		t.Skip("journalctl not installed")
	}
}

func TestLogAuditSelectionShowsJournalRowWhenAvailable(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.log.JournalAvailableFn = func() bool { return true }
	r.reloadLogAuditDiscovery()

	if !r.log.JournalAvailable {
		t.Fatal("setup: JournalAvailable = false, want true")
	}
	if got := r.log.Table.GetCell(1, logAuditSelName).Text; !strings.Contains(got, "systemd-journal") {
		t.Errorf("row 1 Name = %q, want it to mention systemd-journal", got)
	}
	// 1 header row + 1 journal row + len(Groups) file-group rows.
	wantRows := len(r.log.Groups) + 2
	if got := r.log.Table.GetRowCount(); got != wantRows {
		t.Errorf("Table has %d rows, want %d", got, wantRows)
	}
}

func TestLogAuditSelectionHidesJournalRowWhenUnavailable(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir) // JournalAvailableFn already false, see newTestRootForLogAudit

	if r.log.JournalAvailable {
		t.Fatal("setup: JournalAvailable = true, want false")
	}
	if got := r.log.Table.GetCell(1, logAuditSelName).Text; strings.Contains(got, "systemd-journal") {
		t.Errorf("row 1 Name = %q, must not be the journal row when it's unavailable", got)
	}
}

func TestLogAuditGroupAtAccountsForJournalRowOffset(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.log.JournalAvailableFn = func() bool { return true }
	r.reloadLogAuditDiscovery()

	if _, ok := r.logAuditGroupAt(1); ok {
		t.Error("logAuditGroupAt(1) found a file group at the journal row")
	}
	if _, ok := r.logAuditGroupAt(2); !ok {
		t.Error("logAuditGroupAt(2) found nothing — want the first real file group, offset past the journal row")
	}
}

func TestOpenLogAuditViewerOnJournalRowSetsJournalModeAndOpensViewer(t *testing.T) {
	requireJournalctlForUI(t)
	dir := t.TempDir()
	r := newTestRootForLogAudit(t, dir)
	r.log.JournalAvailableFn = func() bool { return true }
	r.reloadLogAuditDiscovery()

	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()

	if !r.log.JournalMode {
		t.Error("JournalMode = false after opening the journal row, want true")
	}
	if r.activePage != logAuditViewerPage {
		t.Errorf("activePage = %q, want %q", r.activePage, logAuditViewerPage)
	}
}

// TestOpenLogAuditViewerOnFileRowClearsJournalMode pins the other half
// of the toggle: switching from the journal row back to an ordinary
// file group (openLogAuditViewer's own branch, logaudit.go) must not
// leave a stale JournalMode behind for "r"/follow to misread.
func TestOpenLogAuditViewerOnFileRowClearsJournalMode(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.log.JournalMode = true // simulate having just come from the journal row

	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()

	if r.log.JournalMode {
		t.Error("JournalMode stayed true after opening an ordinary file group")
	}
}

func TestReopenLogAuditViewerUsesJournalRefreshWhenInJournalMode(t *testing.T) {
	requireJournalctlForUI(t)
	dir := t.TempDir()
	r := newTestRootForLogAudit(t, dir)
	r.log.JournalAvailableFn = func() bool { return true }
	r.reloadLogAuditDiscovery()

	r.log.Table.Select(1, 0)
	r.openLogAuditViewer()
	if !r.log.JournalMode {
		t.Fatal("setup: JournalMode = false after opening the journal row")
	}

	r.log.CurrentEntries = nil // force a visible change reopenLogAuditViewer must undo
	r.reopenLogAuditViewer()

	if r.log.CurrentFiles != 1 {
		t.Errorf("CurrentFiles = %d after reopening in journal mode, want 1", r.log.CurrentFiles)
	}
}
