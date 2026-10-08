// logaudit.go is the Log Audit screen's own glue between
// internal/logview (Discover/Open/ParseAll/Merge — the real, UI-free
// business logic) and the two overlay layers logauditscreen.go builds:
// turning the active panel's own current directory into the screen's
// state, and turning the file group under the cursor into a merged
// Entry stream for the viewer to show.
package ui

import (
	"os"
	"path/filepath"
	"time"

	"github.com/jagottsicher/breakthrough/internal/logview"
)

// openLogAudit is "jL" — unlike every other full-screen catalog chord
// (jm/jn/jf/jh/jl/js/jk), this one is scoped to wherever the active
// panel currently is, not a fixed destination, per the user's own
// explicit request: "ich möchte den logviewer... in einem beliebigen
// verzeichnis via button starten." Always re-discovers fresh, the same
// "reflect the real, current state" reasoning openMounts/
// openActivityLog already establish — a directory's own log files can
// change between two "jL" presses just as easily as its mount table
// or activity log can.
func (r *Root) openLogAudit() {
	r.logAuditDir = r.panel.path
	r.reloadLogAuditDiscovery()
	r.showOverlay(logAuditSelectionPage, r.logAuditSelectionLayout)
	r.app.SetFocus(r.logAuditTable)
}

// reloadLogAuditDiscovery re-runs Discover against logAuditDir — the
// same "reflect the real, current state" contract reloadActivityLog
// already has for "r".
func (r *Root) reloadLogAuditDiscovery() {
	groups, err := logview.Discover(r.logAuditDir)
	r.logAuditGroups = groups
	r.logAuditDiscoverErr = err
	r.renderLogAuditSelection()
}

func (r *Root) closeLogAuditSelection() {
	r.hideOverlay()
}

// logAuditGroupAt returns logAuditGroups[row-1] — row is a table row
// (1-based, header at 0), the same offset every other screen's own
// table-row-to-data mapping uses (see e.g. renderActivityLog).
func (r *Root) logAuditGroupAt(row int) (logview.FileGroup, bool) {
	idx := row - 1
	if idx < 0 || idx >= len(r.logAuditGroups) {
		return logview.FileGroup{}, false
	}
	return r.logAuditGroups[idx], true
}

// openLogAuditViewer reads and merges every file in the group under
// the cursor, then pushes the viewer overlay on top of the selection
// screen — pushOverlay, not showOverlay, so Escape from the viewer
// reveals the selection screen again rather than jumping straight back
// to the panel (see root.go's own doc comment on this pair of
// layers). No checkbox/multi-select here, per the user's own explicit
// request: you're already choosing one family by moving the cursor to
// it, so marking it again first would be a second, redundant step —
// unlike Copy/Cut's own checkbox-or-cursor convention, there's no
// "act on several, independently of where the cursor ends up"
// use case this screen actually needs. The group read is remembered
// (logAuditGroupOpen) so "r" can re-read it without going back through
// the selection screen.
func (r *Root) openLogAuditViewer() {
	row, _ := r.logAuditTable.GetSelection()
	group, ok := r.logAuditGroupAt(row)
	if !ok {
		return
	}
	r.logAuditGroupOpen = group
	r.refreshLogAuditViewerData()
	r.logAuditKeywordField.SetText("")
	r.renderLogAuditViewer()
	r.pushOverlay(logAuditViewerPage, r.logAuditViewerLayout, nil)
	r.app.SetFocus(r.logAuditKeywordField)
}

// refreshLogAuditViewerData re-reads and re-merges logAuditGroupOpen's
// own files — the actual read/parse/merge work, factored out of
// openLogAuditViewer so reopenLogAuditViewer's own manual refresh
// ("r") can redo it in place, without pushing a second viewer overlay
// on top of the one already open.
func (r *Root) refreshLogAuditViewerData() {
	var allEntries [][]logview.Entry
	files, skipped := 0, 0
	var lastErr error
	for _, f := range r.logAuditGroupOpen.Files {
		if !f.Supported {
			skipped++
			continue
		}
		entries, err := readLogAuditFile(f)
		if err != nil {
			lastErr = err
			continue
		}
		files++
		allEntries = append(allEntries, entries)
	}

	r.logAuditAllEntries = logview.Merge(allEntries...)
	r.logAuditFiles = files
	r.logAuditSkipped = skipped
	r.logAuditParseErr = lastErr
}

// readLogAuditFile opens, decompresses, and parses one file — fallback
// time for a timestamp-less line (RFC 3164's own missing year, or a
// FormatPlain line with no timestamp at all) is the file's own mtime,
// a far better guess at "roughly when this happened" than the zero
// time every such line would otherwise collapse onto in Merge.
func readLogAuditFile(f logview.CandidateFile) ([]logview.Entry, error) {
	fallback := time.Now()
	if st, err := os.Stat(f.Path); err == nil {
		fallback = st.ModTime()
	}

	r, err := logview.Open(f)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()

	entries, _, err := logview.ParseAll(r, filepath.Base(f.Path), fallback)
	return entries, err
}

// reopenLogAuditViewer re-reads and re-merges the same groups the
// viewer is already showing — "r"'s own manual refresh, the same
// "re-run the real read, don't just redraw" contract reloadActivityLog
// already has.
func (r *Root) reopenLogAuditViewer() {
	r.refreshLogAuditViewerData()
	r.renderLogAuditViewer()
}

func (r *Root) closeLogAuditViewer() {
	r.hideOverlay()
}
