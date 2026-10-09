// logaudit.go is the Log Audit screen's own glue between
// internal/logview (Discover/Open/ParseAll/Merge — the real, UI-free
// business logic) and the two overlay layers logauditscreen.go builds:
// turning the active panel's own current directory into the screen's
// state, and turning the file group under the cursor into a merged
// Entry stream for the viewer to show.
package ui

import (
	"context"
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
	r.logAuditTimeField.SetText("")
	r.logAuditLevelField.SetText("")
	// Newest-first, not the Merge's own oldest-first order — per the
	// user's own explicit request, the same "most recent at the top"
	// default every other reasonable default for triaging a log
	// actually wants. Still just the opening default: the Time column
	// header (logauditscreen.go) toggles it for the rest of the session.
	r.logAuditNewestFirst = true
	r.renderLogAuditViewer()
	r.pushOverlay(logAuditViewerPage, r.logAuditViewerLayout, nil)
	// The list, not logAuditKeywordField — per the user's own explicit
	// request: opening the viewer should land you ready to read/navigate
	// entries right away, not typing into a filter first (see
	// renderLogAuditFocusIndicator, logauditscreen.go, for the visible
	// cue this now gives).
	r.app.SetFocus(r.logAuditViewerTable)
}

// refreshLogAuditViewerData re-reads and re-merges logAuditGroupOpen's
// own files and stores the result directly on r — used for every
// *synchronous* read (the initial open, and "r"'s own manual refresh):
// a one-off, user-triggered action blocking briefly is the same
// tradeoff reloadActivityLog's own synchronous file read already
// makes elsewhere in this package. startLogAuditFollow does NOT use
// this directly — see readLogAuditGroup's own doc comment for why a
// *repeating* read needs the heavier I/O kept off the UI goroutine
// instead.
func (r *Root) refreshLogAuditViewerData() {
	entries, files, skipped, err := readLogAuditGroup(r.logAuditGroupOpen)
	r.logAuditAllEntries = entries
	r.logAuditFiles = files
	r.logAuditSkipped = skipped
	r.logAuditParseErr = err
}

// readLogAuditGroup does the actual read/parse/merge work for group,
// touching no tview widget and no Root field of its own — safe to
// call from a background goroutine (see startLogAuditFollow), unlike
// refreshLogAuditViewerData's own direct field writes. A real log file
// can take hundreds of milliseconds to read and parse (300k lines,
// ~700ms, measured) — running that on the UI goroutine every single
// follow tick would stutter the whole app repeatedly, not just once,
// the way a manual "r" press's own one-off blocking read does not.
func readLogAuditGroup(group logview.FileGroup) (entries []logview.Entry, files, skipped int, err error) {
	var allEntries [][]logview.Entry
	for _, f := range group.Files {
		if !f.Supported {
			skipped++
			continue
		}
		fileEntries, ferr := readLogAuditFile(f)
		if ferr != nil {
			err = ferr
			continue
		}
		files++
		allEntries = append(allEntries, fileEntries)
	}
	entries = logview.Merge(allEntries...)
	return entries, files, skipped, err
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

// closeLogAuditViewer always stops a running follow first — leaving a
// ticker behind after the overlay closed would keep re-reading files
// and calling tview methods against a screen nobody can see, the same
// "cancel background work before it outlives the thing it's updating"
// rule every other ticker-driven overlay in this package already
// follows (see cancelChord/animateChordCountdown for the same shape).
func (r *Root) closeLogAuditViewer() {
	r.stopLogAuditFollow()
	r.hideOverlay()
}

// logAuditFollowInterval is how often a running follow re-reads
// logAuditGroupOpen's own files — frequent enough to feel live for an
// admin watching an active service, infrequent enough that it isn't
// re-parsing a growing file every fraction of a second. Phase 2 reads
// every file fresh each tick (see refreshLogAuditViewerData) rather
// than tailing byte offsets — the simpler, correct-by-construction
// choice for a first version; worth revisiting only if a real,
// large log file makes 2s full re-reads feel sluggish.
const logAuditFollowInterval = 2 * time.Second

// toggleLogAuditFollow is "f" in the viewer — starts or stops a
// running follow.
func (r *Root) toggleLogAuditFollow() {
	if r.logAuditFollowing {
		r.stopLogAuditFollow()
		return
	}
	r.startLogAuditFollow()
}

// startLogAuditFollow begins re-reading logAuditGroupOpen's own files
// every logAuditFollowInterval, the "tail -f, but for the merged
// audit view" the user asked for explicitly — a no-op if already
// running.
func (r *Root) startLogAuditFollow() {
	if r.logAuditFollowing {
		return
	}
	r.logAuditFollowing = true

	ctx, cancel := context.WithCancel(context.Background())
	r.logAuditFollowCancel = cancel

	// safeGo, the same as every other background ticker in this
	// package (see startChord/animateChordCountdown): a panic here
	// ends the follow cleanly instead of taking the whole process
	// down silently. group is captured once, by value, here — not
	// read from r.logAuditGroupOpen inside the ticker loop itself —
	// so this goroutine never touches a Root field concurrently with
	// the UI goroutine; see readLogAuditGroup's own doc comment for
	// why the read work itself has to happen out here, off the UI
	// goroutine, with only the already-computed result (not the I/O)
	// handed to QueueUpdateDraw.
	group := r.logAuditGroupOpen
	r.safeGo("log audit follow", func() { r.stopLogAuditFollow() }, func() {
		ticker := time.NewTicker(logAuditFollowInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if ctx.Err() != nil {
					return
				}
				entries, files, skipped, err := readLogAuditGroup(group)
				if ctx.Err() != nil {
					return
				}
				r.app.QueueUpdateDraw(func() {
					if ctx.Err() != nil {
						return
					}
					r.tickLogAuditFollow(entries, files, skipped, err)
				})
			case <-ctx.Done():
				return
			}
		}
	})
}

// stopLogAuditFollow ends a running follow, however it ended (toggled
// off, the viewer closed, or a background panic) — safe to call even
// when nothing is running.
func (r *Root) stopLogAuditFollow() {
	if r.logAuditFollowCancel != nil {
		r.logAuditFollowCancel()
		r.logAuditFollowCancel = nil
	}
	r.logAuditFollowing = false
}

// tickLogAuditFollow applies one follow tick's own already-read result
// (see readLogAuditGroup/startLogAuditFollow — the actual file I/O
// already happened off the UI goroutine by the time this runs) and
// re-renders — stayedAtTail captures, before applying it, whether the
// cursor was already on the row new entries actually arrive at (the
// only case Select should move it: an admin watching the tail end
// should keep seeing the tail end once new lines arrive, but one who
// scrolled away to read an older entry must not be yanked back to
// it). Which row that is depends on logAuditNewestFirst — row 1 (new
// entries appear at the top) when true, the last row (they appear at
// the bottom, same as a plain `tail -f`) when false, the default.
func (r *Root) tickLogAuditFollow(entries []logview.Entry, files, skipped int, err error) {
	rowCount := r.logAuditViewerTable.GetRowCount()
	cur, _ := r.logAuditViewerTable.GetSelection()
	tailRow := rowCount - 1
	if r.logAuditNewestFirst {
		tailRow = 1
	}
	stayedAtTail := rowCount > 1 && cur == tailRow

	r.logAuditAllEntries = entries
	r.logAuditFiles = files
	r.logAuditSkipped = skipped
	r.logAuditParseErr = err
	r.renderLogAuditViewer()

	if stayedAtTail {
		if newCount := r.logAuditViewerTable.GetRowCount(); newCount > 1 {
			newTailRow := newCount - 1
			if r.logAuditNewestFirst {
				newTailRow = 1
			}
			r.logAuditViewerTable.Select(newTailRow, 0)
		}
	}
}
