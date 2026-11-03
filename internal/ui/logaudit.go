// logaudit.go is the Log Audit screen's own glue between
// internal/logview (Discover/Open/ParseAll/Merge — the real, UI-free
// business logic) and the two overlay layers logauditscreen.go builds:
// turning the active panel's own current directory into the screen's
// state, and turning the file group under the cursor into a merged
// Entry stream for the viewer to show.
package ui

import (
	"context"
	"fmt"
	"path"
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
	r.log.Dir = r.panel.path
	r.reloadLogAuditDiscovery()
	r.showOverlay(logAuditSelectionPage, r.log.SelectionLayout)
	r.app.SetFocus(r.log.Table)
}

// reloadLogAuditDiscovery re-runs Discover against log.Dir — the
// same "reflect the real, current state" contract reloadActivityLog
// already has for "r".
func (r *Root) reloadLogAuditDiscovery() {
	groups, err := logview.Discover(r.currentLogAuditSource(), r.log.Dir)
	r.log.Groups = groups
	r.log.DiscoverErr = err
	r.renderLogAuditSelection()
}

func (r *Root) closeLogAuditSelection() {
	r.hideOverlay()
}

// logAuditGroupAt returns log.Groups[row-1] — row is a table row
// (1-based, header at 0), the same offset every other screen's own
// table-row-to-data mapping uses (see e.g. renderActivityLog).
func (r *Root) logAuditGroupAt(row int) (logview.FileGroup, bool) {
	idx := row - 1
	if idx < 0 || idx >= len(r.log.Groups) {
		return logview.FileGroup{}, false
	}
	return r.log.Groups[idx], true
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
// (log.GroupOpen) so "r" can re-read it without going back through
// the selection screen.
func (r *Root) openLogAuditViewer() {
	row, _ := r.log.Table.GetSelection()
	group, ok := r.logAuditGroupAt(row)
	if !ok {
		return
	}
	// A different group than whatever progressive load might still be
	// running for (or the same one, reopened) — cancel first, the same
	// "cancel before starting the next thing" rule stopLogAuditFollow
	// already follows, so a stale background loader never keeps
	// merging an old open's own rotated files into what's now showing.
	r.stopLogAuditProgressiveLoad()
	r.log.GroupOpen = group
	r.log.RotatedEntries = nil
	r.log.RotatedFiles = 0
	r.log.RotatedSkipped = 0
	r.log.RotatedErr = nil
	r.refreshLogAuditCurrentFile()
	r.log.KeywordField.SetText("")
	r.log.TimeField.SetText("")
	r.log.LevelField.SetText("")
	// Newest-first, not the Merge's own oldest-first order — per the
	// user's own explicit request, the same "most recent at the top"
	// default every other reasonable default for triaging a log
	// actually wants. Still just the opening default: the Time column
	// header (logauditscreen.go) toggles it for the rest of the session.
	r.log.NewestFirst = true
	r.renderLogAuditViewer()
	r.pushOverlay(logAuditViewerPage, r.log.ViewerLayout, nil)
	// The list, not log.KeywordField — per the user's own explicit
	// request: opening the viewer should land you ready to read/navigate
	// entries right away, not typing into a filter first (see
	// renderLogAuditFocusIndicator, logauditscreen.go, for the visible
	// cue this now gives).
	r.app.SetFocus(r.log.ViewerTable)
	// Only the current file has been read synchronously above — the
	// family's own rotated history (if any) streams in behind it, see
	// startLogAuditProgressiveLoad's own doc comment for why: a real
	// family pulled whole over SFTP used to block this entire open on
	// every rotation's own network round trip and decompression before
	// showing anything at all.
	r.startLogAuditProgressiveLoad(group)
}

// refreshLogAuditCurrentFile re-reads only log.GroupOpen's own
// current (first, unrotated) file and recombines it with whatever
// log.RotatedEntries already holds (see recomputeLogAuditCombined)
// — "r"'s own manual refresh, and the viewer's own first, fast paint
// on open. Never touches the rotated files themselves: once logrotate
// has renamed a file away from the family's own bare name, it never
// changes again, so there is nothing there for a refresh to find — see
// startLogAuditProgressiveLoad's own doc comment for why those are
// read once, progressively, and cached for the rest of the session
// instead. A one-off, user-triggered blocking read for just this one
// file is the same tradeoff reloadActivityLog's own synchronous read
// already makes elsewhere in this package.
func (r *Root) refreshLogAuditCurrentFile() {
	group := r.log.GroupOpen
	source := r.currentLogAuditSource()

	r.log.CurrentEntries = nil
	r.log.CurrentFiles = 0
	r.log.CurrentSkipped = 0
	r.log.CurrentErr = nil

	if len(group.Files) > 0 {
		f := group.Files[0]
		switch {
		case !f.Supported:
			r.log.CurrentSkipped = 1
		default:
			entries, err := readLogAuditFile(source, f)
			if err != nil {
				r.log.CurrentErr = err
			} else {
				r.log.CurrentFiles = 1
				r.log.CurrentEntries = entries
			}
		}
	}
	r.recomputeLogAuditCombined()
}

// recomputeLogAuditCombined merges log.CurrentEntries/Files/
// Skipped/Err and log.RotatedEntries/Files/Skipped/Err into
// log.AllEntries/Files/Skipped/ParseErr — the fields every other
// reader (filtering, the title bar, Machiavelli Mode's own context
// snapshot, ...) already uses, unaware this split exists at all. Err
// prefers the rotated side, matching the original single-loop
// readLogAuditGroup's own "last iterated wins" semantics: Files[0]
// (current) always came first there, so a later, rotated file's own
// error was always what actually ended up reported whenever both
// failed.
func (r *Root) recomputeLogAuditCombined() {
	r.log.AllEntries = logview.Merge(r.log.CurrentEntries, r.log.RotatedEntries)
	r.log.ErrCount, r.log.WarnCount = 0, 0
	for _, e := range r.log.AllEntries {
		switch e.Level {
		case logview.LevelError, logview.LevelFatal:
			r.log.ErrCount++
		case logview.LevelWarn:
			r.log.WarnCount++
		}
	}
	r.log.Files = r.log.CurrentFiles + r.log.RotatedFiles
	r.log.Skipped = r.log.CurrentSkipped + r.log.RotatedSkipped
	r.log.ParseErr = r.log.RotatedErr
	if r.log.ParseErr == nil {
		r.log.ParseErr = r.log.CurrentErr
	}
}

// logAuditCacheKey identifies f for log.RotatedCache — path, size,
// and modtime together, the same CandidateFile.Size/ModTime Discover
// already populated from a single ReadDir/ListDir call, not a second
// stat. A rotated file never changes once written, so in practice this
// only ever needs to tell "the same file" apart from "a different file
// that happens to reuse the same path" (e.g. a differently-configured
// remote reused between two sessions) — belt and suspenders, not a
// real expected case.
func logAuditCacheKey(f logview.CandidateFile) string {
	return fmt.Sprintf("%s|%d|%d", f.Path, f.Size, f.ModTime.UnixNano())
}

// startLogAuditProgressiveLoad reads group's own rotated files
// (Files[1:] — Files[0], the current file, is read separately and
// synchronously, see refreshLogAuditCurrentFile) one at a time in the
// background, merging each into log.RotatedEntries and
// re-rendering as it goes — the table and timeline are usable the
// instant the current file alone has been read; older history fills
// in behind it rather than blocking that first paint, over a local
// directory or an SFTP connection alike.
//
// Every file whose own logAuditCacheKey is already in
// log.RotatedCache is applied immediately, right here, on the UI
// goroutine — no I/O, no goroutine needed for those. The background
// goroutine below only ever runs for files that still need a real
// read, and never touches log.RotatedCache itself (see that
// field's own doc comment on why: only this pre-pass and
// applyLogAuditRotatedFile's own QueueUpdateDraw callback, both
// UI-goroutine-only, are allowed to).
func (r *Root) startLogAuditProgressiveLoad(group logview.FileGroup) {
	if len(group.Files) <= 1 {
		return
	}
	if r.log.RotatedCache == nil {
		r.log.RotatedCache = map[string][]logview.Entry{}
	}
	r.log.TotalFiles = len(group.Files)

	var pending []logview.CandidateFile
	for _, f := range group.Files[1:] {
		switch {
		case !f.Supported:
			r.applyLogAuditRotatedFile(f, nil, true, nil)
		default:
			if cached, ok := r.log.RotatedCache[logAuditCacheKey(f)]; ok {
				r.applyLogAuditRotatedFile(f, cached, false, nil)
				continue
			}
			pending = append(pending, f)
		}
	}
	if len(pending) == 0 {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.log.LoadCancel = cancel
	r.log.LoadingMore = true
	r.log.LoadAnimFrame = 0
	r.renderLogAuditViewer() // shows the loading segment right away, before the first file actually finishes

	source := r.currentLogAuditSource()
	onPanic := func() { r.stopLogAuditProgressiveLoad() }
	// Both wrapped in safeGo (see its own doc comment), the same
	// animation-plus-work pairing computeHashes/animateHashProgress
	// already establish in properties.go.
	r.safeGo("log audit progressive load animation", onPanic, func() { r.animateLogAuditLoad(ctx) })
	r.safeGo("log audit progressive load", onPanic, func() {
		for _, f := range pending {
			if ctx.Err() != nil {
				return
			}
			entries, ferr := readLogAuditFile(source, f)
			if ctx.Err() != nil {
				return
			}
			r.app.QueueUpdateDraw(func() {
				if ctx.Err() != nil {
					return
				}
				r.applyLogAuditRotatedFile(f, entries, false, ferr)
			})
		}
		r.app.QueueUpdateDraw(func() {
			if ctx.Err() != nil {
				return
			}
			r.finishLogAuditProgressiveLoad()
		})
	})
}

// applyLogAuditRotatedFile merges one rotated file's own already-read
// result into log.RotatedEntries/Files/Skipped/Err, recombines, and
// re-renders — called from startLogAuditProgressiveLoad's own
// QueueUpdateDraw callback (the real file I/O already happened off the
// UI goroutine by then, the same shape tickLogAuditFollow already
// establishes for readLogAuditGroup), and directly, with no goroutine
// at all, from a test exercising one already-known result.
func (r *Root) applyLogAuditRotatedFile(f logview.CandidateFile, entries []logview.Entry, skip bool, err error) {
	switch {
	case skip:
		r.log.RotatedSkipped++
	case err != nil:
		r.log.RotatedErr = err
	default:
		r.log.RotatedFiles++
		r.log.RotatedEntries = logview.Merge(r.log.RotatedEntries, entries)
		r.log.RotatedCache[logAuditCacheKey(f)] = entries
	}
	r.recomputeLogAuditCombined()
	r.renderLogAuditViewer()
}

// finishLogAuditProgressiveLoad ends a completed background load —
// every pending file has already been applied by the time this runs
// (see startLogAuditProgressiveLoad's own final QueueUpdateDraw call).
func (r *Root) finishLogAuditProgressiveLoad() {
	r.stopLogAuditProgressiveLoad()
	r.renderLogAuditViewer()
}

// stopLogAuditProgressiveLoad cancels any in-flight background load
// and its animation, however it ended (toggled off by a new open, the
// viewer closing, a background panic, or a normal finish) — safe to
// call even when nothing is running, the same shape stopLogAuditFollow
// already establishes for the identical reason.
func (r *Root) stopLogAuditProgressiveLoad() {
	if r.log.LoadCancel != nil {
		r.log.LoadCancel()
		r.log.LoadCancel = nil
	}
	r.log.LoadingMore = false
	r.log.ViewerTitleTemplate = ""
}

// animateLogAuditLoad advances log.LoadAnimFrame every
// hashAnimationInterval until ctx is done — paired with (but
// independent of) startLogAuditProgressiveLoad's own per-file reads,
// the same animateHashProgress shape (properties.go). Repaints only
// the title bar (see tickLogAuditLoadAnimation), never the full table:
// a full renderLogAuditViewer rescans every entry and rebuilds up to
// logAuditMaxRenderedRows TableCells — exactly the per-keystroke cost
// its own doc comment exists to avoid, which a several-times-a-second
// animation tick would otherwise reintroduce for the whole duration of
// a large or slow (SFTP) background load.
func (r *Root) animateLogAuditLoad(ctx context.Context) {
	ticker := time.NewTicker(hashAnimationInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			r.app.QueueUpdateDraw(func() {
				if ctx.Err() != nil {
					return
				}
				r.tickLogAuditLoadAnimation()
			})
		case <-ctx.Done():
			return
		}
	}
}

// readLogAuditGroup does the actual read/parse/merge work for group,
// touching no tview widget and no Root field of its own — safe to
// call from a background goroutine (see startLogAuditFollow), unlike
// refreshLogAuditCurrentFile's own direct field writes. A real log file
// can take hundreds of milliseconds to read and parse (300k lines,
// ~700ms, measured) — running that on the UI goroutine every single
// follow tick would stutter the whole app repeatedly, not just once,
// the way a manual "r" press's own one-off blocking read does not.
func readLogAuditGroup(source logview.FileSource, group logview.FileGroup) (entries []logview.Entry, files, skipped int, err error) {
	var allEntries [][]logview.Entry
	for _, f := range group.Files {
		if !f.Supported {
			skipped++
			continue
		}
		fileEntries, ferr := readLogAuditFile(source, f)
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
func readLogAuditFile(source logview.FileSource, f logview.CandidateFile) ([]logview.Entry, error) {
	// f.ModTime, not a fresh os.Stat — Discover already populated it
	// from the exact same directory listing this file came from (see
	// CandidateFile's own doc comment), which works identically for a
	// remote connection too; a direct os.Stat call here never could.
	fallback := f.ModTime
	if fallback.IsZero() {
		fallback = time.Now()
	}

	r, err := logview.Open(source, f)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()

	entries, _, err := logview.ParseAll(r, path.Base(f.Path), fallback)
	return entries, err
}

// reopenLogAuditViewer re-reads and re-merges the same groups the
// viewer is already showing — "r"'s own manual refresh, the same
// "re-run the real read, don't just redraw" contract reloadActivityLog
// already has.
func (r *Root) reopenLogAuditViewer() {
	r.refreshLogAuditCurrentFile()
	r.renderLogAuditViewer()
}

// closeLogAuditViewer always stops a running follow and any in-flight
// progressive load first — leaving either behind after the overlay
// closed would keep re-reading files and calling tview methods against
// a screen nobody can see, the same "cancel background work before it
// outlives the thing it's updating" rule every other ticker-driven
// overlay in this package already follows (see cancelChord/
// animateChordCountdown for the same shape).
func (r *Root) closeLogAuditViewer() {
	r.stopLogAuditFollow()
	r.stopLogAuditProgressiveLoad()
	r.hideOverlay()
}

// logAuditFollowInterval is how often a running follow re-reads
// log.GroupOpen's own files — frequent enough to feel live for an
// admin watching an active service, infrequent enough that it isn't
// re-parsing a growing file every fraction of a second. Phase 2 reads
// every file fresh each tick (see tickLogAuditFollow) rather
// than tailing byte offsets — the simpler, correct-by-construction
// choice for a first version; worth revisiting only if a real,
// large log file makes 2s full re-reads feel sluggish.
const logAuditFollowInterval = 2 * time.Second

// toggleLogAuditFollow is "f" in the viewer — starts or stops a
// running follow.
func (r *Root) toggleLogAuditFollow() {
	if r.log.Following {
		r.stopLogAuditFollow()
		return
	}
	r.startLogAuditFollow()
}

// startLogAuditFollow begins re-reading log.GroupOpen's own files
// every logAuditFollowInterval, the "tail -f, but for the merged
// audit view" the user asked for explicitly — a no-op if already
// running.
func (r *Root) startLogAuditFollow() {
	if r.log.Following {
		return
	}
	// readLogAuditGroup re-reads the whole family, including every
	// rotated file, on every tick — making startLogAuditProgressiveLoad
	// redundant from here on, and worse than redundant if left running
	// alongside it: both write log.AllEntries/Files/Skipped/ParseErr
	// (tickLogAuditFollow directly, applyLogAuditRotatedFile via
	// recomputeLogAuditCombined), so a follow tick landing between
	// two rotated files still streaming in would visibly shrink the
	// view back down until the loader caught back up.
	r.stopLogAuditProgressiveLoad()
	r.log.Following = true

	ctx, cancel := context.WithCancel(context.Background())
	r.log.FollowCancel = cancel

	// safeGo, the same as every other background ticker in this
	// package (see startChord/animateChordCountdown): a panic here
	// ends the follow cleanly instead of taking the whole process
	// down silently. group is captured once, by value, here — not
	// read from r.log.GroupOpen inside the ticker loop itself —
	// so this goroutine never touches a Root field concurrently with
	// the UI goroutine; see readLogAuditGroup's own doc comment for
	// why the read work itself has to happen out here, off the UI
	// goroutine, with only the already-computed result (not the I/O)
	// handed to QueueUpdateDraw.
	group := r.log.GroupOpen
	source := r.currentLogAuditSource()
	r.safeGo("log audit follow", func() { r.stopLogAuditFollow() }, func() {
		ticker := time.NewTicker(logAuditFollowInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if ctx.Err() != nil {
					return
				}
				entries, files, skipped, err := readLogAuditGroup(source, group)
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
	if r.log.FollowCancel != nil {
		r.log.FollowCancel()
		r.log.FollowCancel = nil
	}
	r.log.Following = false
}

// tickLogAuditFollow applies one follow tick's own already-read result
// (see readLogAuditGroup/startLogAuditFollow — the actual file I/O
// already happened off the UI goroutine by the time this runs) and
// re-renders — stayedAtTail captures, before applying it, whether the
// cursor was already on the row new entries actually arrive at (the
// only case Select should move it: an admin watching the tail end
// should keep seeing the tail end once new lines arrive, but one who
// scrolled away to read an older entry must not be yanked back to
// it). Which row that is depends on log.NewestFirst — row 1 (new
// entries appear at the top) when true, the last row (they appear at
// the bottom, same as a plain `tail -f`) when false, the default.
func (r *Root) tickLogAuditFollow(entries []logview.Entry, files, skipped int, err error) {
	rowCount := r.log.ViewerTable.GetRowCount()
	cur, _ := r.log.ViewerTable.GetSelection()
	tailRow := rowCount - 1
	if r.log.NewestFirst {
		tailRow = 1
	}
	stayedAtTail := rowCount > 1 && cur == tailRow

	r.log.AllEntries = entries
	r.log.Files = files
	r.log.Skipped = skipped
	r.log.ParseErr = err
	r.renderLogAuditViewer()

	if stayedAtTail {
		if newCount := r.log.ViewerTable.GetRowCount(); newCount > 1 {
			newTailRow := newCount - 1
			if r.log.NewestFirst {
				newTailRow = 1
			}
			r.log.ViewerTable.Select(newTailRow, 0)
		}
	}
}
