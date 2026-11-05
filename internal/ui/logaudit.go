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

	"github.com/jagottsicher/breakthrough/internal/logdiskcache"
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
	r.log.RotatedErrCount = 0
	r.log.RotatedWarnCount = 0
	r.log.LastCombineAt = time.Time{}
	r.log.CombineDirty = false
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
	r.log.CurrentErrCount, r.log.CurrentWarnCount = countLogAuditErrWarn(r.log.CurrentEntries)
	r.recomputeLogAuditCombined()
}

// countLogAuditErrWarn tallies entries' Error+Fatal and Warn levels —
// the per-batch half of ErrCount/WarnCount's own doc comment:
// refreshLogAuditCurrentFile and applyLogAuditRotatedFile each call
// this once, on just the entries they themselves just read, and add
// the result to a running total, instead of recomputeLogAuditCombined
// walking the full, ever-growing AllEntries on every single rotated
// file applied during a progressive load — see
// BenchmarkApplyLogAuditRotatedFile for the real-world cost that used
// to be (confirmed ~11.5s at 1.2M/40 files before this change).
func countLogAuditErrWarn(entries []logview.Entry) (errCount, warnCount int) {
	for _, e := range entries {
		switch e.Level {
		case logview.LevelError, logview.LevelFatal:
			errCount++
		case logview.LevelWarn:
			warnCount++
		}
	}
	return errCount, warnCount
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
	r.log.ErrCount = r.log.CurrentErrCount + r.log.RotatedErrCount
	r.log.WarnCount = r.log.CurrentWarnCount + r.log.RotatedWarnCount
	r.log.Files = r.log.CurrentFiles + r.log.RotatedFiles
	r.log.Skipped = r.log.CurrentSkipped + r.log.RotatedSkipped
	r.log.ParseErr = r.log.RotatedErr
	if r.log.ParseErr == nil {
		r.log.ParseErr = r.log.CurrentErr
	}
}

// logAuditCacheKey identifies f for log.RotatedCache — identity (see
// currentLogAuditSourceIdentity), path, size, and modtime together, the
// latter three the same CandidateFile.Size/ModTime Discover already
// populated from a single ReadDir/ListDir call, not a second stat. A
// rotated file never changes once written, so size+modtime alone
// already tell "the same file" apart from "a different file that
// happens to reuse the same path". identity is what tells two
// different machines apart: without it, two hosts provisioned from the
// same image (or simply reusing the same path, e.g. both rotating
// /var/log/nginx/access.log.1 to the same size at the same cron tick)
// would otherwise produce the exact same key and silently serve one
// host's entries for the other's family — a real case this app's own
// remote-vs-local separation rule (see CLAUDE.md) requires guarding
// against, not "belt and suspenders".
func logAuditCacheKey(identity string, f logview.CandidateFile) string {
	return identity + "|" + logAuditFileKey(f)
}

// logAuditFileKey is logAuditCacheKey's own key without the identity
// prefix — what logdiskcache.Cache's Get/Put take as their own "key"
// argument, since that type already partitions by identity into its
// own directory per connection (see internal/logdiskcache's own doc
// comment); embedding identity a second time into that key would be
// redundant, not wrong, just pointless hashing of the same string
// twice.
func logAuditFileKey(f logview.CandidateFile) string {
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
		r.log.RotatedCache = logview.NewRotatedCache()
	}
	r.log.TotalFiles = len(group.Files)

	// identity is captured once, right here, and closed over by every
	// use below (the pre-pass immediately, the background goroutine's
	// own closure later) rather than re-read from r.panel at Put time —
	// r.panel can reconnect to a different host while this same load is
	// still progressing, and a cache key built from whatever r.panel
	// says *then* would silently file one host's entries under
	// another's identity. See currentLogAuditSourceIdentity's own doc
	// comment for why this matters at all.
	identity := r.currentLogAuditSourceIdentity()

	var pending []logview.CandidateFile
	for _, f := range group.Files[1:] {
		switch {
		case !f.Supported:
			r.applyLogAuditRotatedFile(identity, f, nil, true, nil)
		default:
			if cached, ok := r.log.RotatedCache.Get(logAuditCacheKey(identity, f)); ok {
				r.applyLogAuditRotatedFile(identity, f, cached, false, nil)
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
	// diskCache is resolved once, right here, alongside identity above
	// — same reasoning: r.panel.isRemote() could answer differently by
	// the time a later QueueUpdateDraw callback ran if read again then,
	// and this cache is remote-only by design (see
	// internal/logdiskcache's own doc comment: a 2026-11-04 live
	// measurement found no win re-reading an already-local file, only
	// "no network round trip" pays for the complexity here).
	var diskCache *logdiskcache.Cache
	if r.panel.isRemote() && r.settings.LogAuditDiskCacheEnabled {
		diskCache = r.logDiskCache()
	}
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
			entries, ferr := readLogAuditRotatedFileWithDiskCache(ctx, diskCache, identity, source, f, r.settings.LogAuditDiskCacheSize)
			if ctx.Err() != nil {
				return
			}
			r.app.QueueUpdateDraw(func() {
				if ctx.Err() != nil {
					return
				}
				r.applyLogAuditRotatedFile(identity, f, entries, false, ferr)
			})
		}
		if diskCache != nil {
			// Once per load, not once per file — a 40-rotation family
			// would otherwise mean 40 full manifest rewrites on the
			// very path this cache exists to make fast (see
			// logdiskcache.Cache.Flush's own doc comment).
			_ = diskCache.Flush()
		}
		r.app.QueueUpdateDraw(func() {
			if ctx.Err() != nil {
				return
			}
			r.finishLogAuditProgressiveLoad()
		})
	})
}

// readLogAuditRotatedFileWithDiskCache tries diskCache first — a nil
// diskCache (disk caching disabled, unavailable, or this load is local
// rather than remote, see startLogAuditProgressiveLoad's own doc
// comment) falls straight through to the real read, same as before
// this cache existed. A successful real read is written back to
// diskCache so this family's next open, on this machine, is a disk
// hit instead of another network round trip; a disk hit itself is
// never written back (it's already there). Factored out from the
// background goroutine's own loop specifically so it's unit-testable
// without a real tview.Application or goroutine — see its own test
// for the shape that exercises.
func readLogAuditRotatedFileWithDiskCache(ctx context.Context, diskCache *logdiskcache.Cache, identity string, source logview.FileSource, f logview.CandidateFile, maxDiskBytes int64) ([]logview.Entry, error) {
	if diskCache != nil {
		if cached, ok := diskCache.Get(ctx, identity, logAuditFileKey(f)); ok {
			return cached, nil
		}
	}
	entries, err := readLogAuditFile(source, f)
	if err == nil && diskCache != nil {
		_ = diskCache.Put(identity, logAuditFileKey(f), entries, maxDiskBytes)
	}
	return entries, err
}

// logAuditCombineThrottleInterval bounds how often applyLogAuditRotatedFile
// actually recombines and re-renders during a progressive load — see
// that function's own doc comment for why a per-file recombine doesn't
// scale. Matches hashAnimationInterval's own cadence (properties.go):
// fast enough that the viewer still visibly fills in as files land,
// far fewer than once per file on a family with hundreds of rotations.
const logAuditCombineThrottleInterval = 150 * time.Millisecond

// applyLogAuditRotatedFile merges one rotated file's own already-read
// result into log.RotatedEntries/Files/Skipped/Err — called from
// startLogAuditProgressiveLoad's own QueueUpdateDraw callback (the real
// file I/O already happened off the UI goroutine by then, the same
// shape tickLogAuditFollow already establishes for readLogAuditGroup),
// and directly, with no goroutine at all, from a test exercising one
// already-known result. identity is always whatever
// startLogAuditProgressiveLoad captured at the start of the load that
// produced f/entries, passed through the closure rather than re-read
// from r.panel here — see that function's own doc comment for why
// re-reading it here would be wrong.
//
// The actual recombine (logview.Merge's own full stable sort, run
// twice — once for RotatedEntries, again inside recomputeLogAuditCombined
// for AllEntries) and re-render (a full filtered scan) are the
// expensive part at scale — confirmed by BenchmarkApplyLogAuditRotatedFile
// at ~11.5s for just 1.2M entries across 40 files when run unconditionally
// per file, the real-world "stalls before the last file is even loaded,
// once a keyword search is already running" report on a 4.4M-entry
// family. New entries are appended raw here — logview.Merge's own stable
// sort-by-Time with no dedupe means Merge(Merge(a,b),c) and Merge(a,b,c)
// produce identical output, so deferring the sort to the next
// recomputeLogAuditCombined call (throttled below, or finally forced by
// finishLogAuditProgressiveLoad) changes nothing about the eventual
// order, only how often that full O(n log n) pass actually runs.
func (r *Root) applyLogAuditRotatedFile(identity string, f logview.CandidateFile, entries []logview.Entry, skip bool, err error) {
	switch {
	case skip:
		r.log.RotatedSkipped++
	case err != nil:
		r.log.RotatedErr = err
	default:
		r.log.RotatedFiles++
		fileErrCount, fileWarnCount := countLogAuditErrWarn(entries)
		r.log.RotatedErrCount += fileErrCount
		r.log.RotatedWarnCount += fileWarnCount
		r.log.RotatedEntries = append(r.log.RotatedEntries, entries...)
		r.log.RotatedCache.Put(logAuditCacheKey(identity, f), entries, r.settings.LogAuditCacheSize)
	}
	r.log.CombineDirty = true
	if !r.log.LastCombineAt.IsZero() && time.Since(r.log.LastCombineAt) < logAuditCombineThrottleInterval {
		return // throttled — the next tick past the interval, or finishLogAuditProgressiveLoad, will catch this up
	}
	r.recomputeLogAuditCombined()
	r.renderLogAuditViewer()
	r.log.LastCombineAt = time.Now()
	r.log.CombineDirty = false
}

// finishLogAuditProgressiveLoad ends a completed background load —
// every pending file has already been applied by the time this runs
// (see startLogAuditProgressiveLoad's own final QueueUpdateDraw call),
// but applyLogAuditRotatedFile's own throttle (see its doc comment) may
// have left the last batch or two un-recombined; flush that here so the
// viewer's final state always reflects every file, not just whichever
// one last happened to land past the throttle interval.
func (r *Root) finishLogAuditProgressiveLoad() {
	if r.log.CombineDirty {
		r.recomputeLogAuditCombined()
		r.log.LastCombineAt = time.Now()
		r.log.CombineDirty = false
	}
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
