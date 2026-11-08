// logauditjournal.go wires logview.ReadJournal into the Log Holepunch
// screen as a fourth, non-file source alongside whatever Discover
// found in log.Dir — see logview.ReadJournal's own doc comment for why
// this can't be another logview.FileSource: there is no path, size, or
// mtime for journald's own binary store to report, and Discover's
// logrotate-family grouping has nothing to group here. Kept in its own
// file, separate from logaudit.go's everyday file-group glue, the same
// "a genuinely different concern gets its own file" convention this
// package already follows elsewhere (e.g. logauditdiskcachemove.go).
//
// Local only, deliberately: logAuditJournalRowOffset (and therefore
// the whole row) only exists when reloadLogAuditDiscovery's own
// log.JournalAvailable computation found the panel not remote — see
// that function's own doc comment. Without this, a remote-connected
// panel could open a row that silently reads *this* machine's own
// journal while the rest of the screen claims to be showing the remote
// host's logs; there is no command-execution channel over
// remotefs.Client to run journalctl against a remote session at all,
// the same "Local only" constraint Git Breach already documents for
// itself.
package ui

import (
	"context"
	"time"

	"github.com/jagottsicher/breakthrough/internal/logview"
)

// logAuditJournalWindowLines is the fixed window ReadJournal reads —
// shown in the selection row's own label (renderLogAuditSelection,
// logauditscreen.go) so a short result never reads as "nothing
// happened", the same reasoning Git Breach's own active-filter header
// line follows. A user-adjustable window, unit ("-u") and boot ("-b")
// selection are deliberately out of scope for this first version (see
// feature_ideas.txt).
const logAuditJournalWindowLines = 2000

// logAuditJournalRowOffset is how many extra rows renderLogAuditSelection
// puts above log.Groups' own rows — 1 while the systemd-journal row is
// offered, 0 otherwise. logAuditGroupAt (logaudit.go) and
// renderLogAuditSelection (logauditscreen.go) both read this so neither
// can drift out of sync about which row is which.
func (r *Root) logAuditJournalRowOffset() int {
	if r.log.JournalAvailable {
		return 1
	}
	return 0
}

// openLogAuditJournalViewer is the systemd-journal row's own Enter/
// click action (dispatched from openLogAuditViewer, logaudit.go) — the
// journal counterpart to it, reusing the exact same viewer overlay and
// rendering: renderLogAuditViewer neither knows nor needs to know its
// entries came from journalctl rather than a file. No GroupOpen, no
// rotated files, no disk cache — a journal has none of the three (see
// logview.ReadJournal's own doc comment); log.JournalMode is the only
// thing distinguishing this open from openLogAuditViewer's, for "r"
// (reopenLogAuditViewer) and follow (startLogAuditFollow) to branch on
// afterward.
func (r *Root) openLogAuditJournalViewer() {
	r.stopLogAuditProgressiveLoad()
	r.log.JournalMode = true
	r.log.GroupOpen = logview.FileGroup{}
	r.log.RotatedEntries = nil
	r.log.PendingRotatedEntries = nil
	r.log.RotatedFiles = 0
	r.log.RotatedSkipped = 0
	r.log.RotatedExcluded = 0
	r.log.RotatedErr = nil
	r.log.RotatedErrCount = 0
	r.log.RotatedWarnCount = 0
	r.log.LastCombineAt = time.Time{}
	r.log.CombineDirty = false
	r.log.KeywordField.SetText("")
	r.log.TimeField.SetText("")
	r.log.LevelField.SetText("")
	r.log.LastFocusedFilterField = nil
	r.log.NewestFirst = true
	r.refreshLogAuditJournal()
	r.renderLogAuditViewer()
	r.pushOverlay(logAuditViewerPage, r.log.ViewerLayout, nil)
	r.app.SetFocus(r.log.ViewerTable)
}

// refreshLogAuditJournal re-runs logview.ReadJournal and recombines the
// result the same way refreshLogAuditCurrentFile does for a file's own
// current, unrotated content (logaudit.go) — "r" (via
// reopenLogAuditViewer) and openLogAuditJournalViewer both call this;
// log.RotatedEntries stays permanently empty for a journal either way,
// so recomputeLogAuditCombined's merge is really just a pass-through
// here.
func (r *Root) refreshLogAuditJournal() {
	entries, err := logview.ReadJournal(context.Background(), logview.JournalOptions{Lines: logAuditJournalWindowLines})
	r.log.CurrentEntries = nil
	r.log.CurrentFiles = 0
	r.log.CurrentSkipped = 0
	r.log.CurrentErr = err
	if err == nil {
		logview.SortByTime(entries)
		r.log.CurrentEntries = entries
		r.log.CurrentFiles = 1
	}
	r.log.CurrentErrCount, r.log.CurrentWarnCount = countLogAuditErrWarn(r.log.CurrentEntries)
	r.recomputeLogAuditCombined()
}
