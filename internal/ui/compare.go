package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/compare"
	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/replace"
)

// The Compare feature ("C"): answers "are these two things the same,
// and if not, what's different" for two files or two directories.
// Two files get a compact overlay (this file) — much like Properties,
// since there's no list to browse, just one pair's own facts. Two
// directories get a full screen (comparetree.go), the same list-plus-
// status-plus-buttons shape Batch Rename's own preview already
// establishes. Both offer a real line-by-line answer on text content
// through the system's own diff(1) (see internal/compare's own doc
// comment for why that's an external tool here, not a reimplementation)
// and a byte-exact one through fsops.Hash — this file duplicates
// neither, it just wires the two screens to internal/compare and to
// those two.
//
// The actual comparison logic lives in internal/compare, not here —
// this file only ever renders what that package reports and reads back
// which of its two on-demand actions (hash, diff) the user asked for,
// the same fsops-vs-ui split this project keeps everywhere else.

const comparePage = "compare"

// comparePathTruncateWidth is how wide a path is allowed to get in
// the file-vs-file overlay's header before truncateMiddle ellipsizes
// its middle -- chosen to fit comfortably inside the overlay's own
// fixed width (see openCompareFile) after the "A: "/"B: " label, so a
// path never wraps onto a second line and silently breaks the
// overlay's own height budget the way an unbounded one did during
// development.
const comparePathTruncateWidth = 74

// compareHeaderHeight is the header's own fixed row count: two path
// lines, a blank, the A/B column header, Size, Modified (six content
// rows -- always exactly this many, never more, which is exactly why
// only SHA-256 ever appears there, see renderCompareFile's own doc
// comment: a raw hex digest is long enough that showing all five, per
// side, wrapped, blew this overlay's height budget wide open during
// development) plus SetBorderPadding's own top row -- checked against
// a real render, not guessed, the same lesson batchRenameFieldsHeight
// already records for the identical reason.
const compareHeaderHeight = 7

// newCompareScreens builds the file-vs-file overlay and the
// directory-vs-directory full screen — built once at startup, contents
// reset on each open, the same build-once/repopulate-on-open shape
// every other screen in this app already follows.
func (r *Root) newCompareScreens() {
	r.newCompareFileScreen()
	r.newCompareTreeScreen()
}

// newCompareFileScreen builds the file-vs-file overlay: a header
// (paths/size/modified side by side), a verdict line, and a button row
// (Compute hash / Show diff / Close).
func (r *Root) newCompareFileScreen() {
	r.compareHeader = tview.NewTextView().SetDynamicColors(true)
	r.compareHeader.SetWrap(true)
	r.compareHeader.SetBorderPadding(1, 0, 2, 1)

	r.compareVerdict = tview.NewTextView().SetDynamicColors(true)
	r.compareVerdict.SetWrap(true)
	r.compareVerdict.SetBorderPadding(0, 0, 2, 1)

	r.compareButtons = r.newCompareButtons()

	r.compareTitleBar = newPlainTitleBar("Compare")

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.compareHeader, compareHeaderHeight, 0, false).
		AddItem(r.compareVerdict, 3, 0, false).
		AddItem(r.compareButtons, 1, 0, false)

	r.compareLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.compareTitleBar, 1, 0, false).
		AddItem(body, 0, 1, true)
}

// newCompareButtons builds the file-vs-file overlay's own button row:
// Compute hash (a byte-exact answer via fsops.Hash — see
// computeCompareHash), Show diff (a line-by-line answer via diff(1) —
// see openCompareDiff, hidden entirely for a binary pair or when
// diff(1) isn't installed), Close.
func (r *Root) newCompareButtons() *tview.Flex {
	type buttonSpec struct {
		button **tview.Button
		label  string
		action func()
	}
	specs := []buttonSpec{
		{&r.compareHashBtn, "Compute hash", r.computeCompareHash},
		{&r.compareDiffBtn, "Show diff", r.openCompareDiff},
		{&r.compareCloseBtn, "Close", r.closeCompare},
	}
	row := tview.NewFlex().SetDirection(tview.FlexColumn)
	for _, spec := range specs {
		b := tview.NewButton(spec.label)
		b.SetSelectedFunc(spec.action)
		b.SetInputCapture(chainKeyCaptures(r.captureCompareKey, spaceAlsoActivates(spec.action)))
		*spec.button = b
		row.AddItem(b, 0, 1, false)
	}
	return row
}

func (r *Root) compareButtonList() []*tview.Button {
	return []*tview.Button{r.compareHashBtn, r.compareDiffBtn, r.compareCloseBtn}
}

// captureCompareKey is the file-vs-file overlay's own shared key
// handling: Tab/Shift+Tab cycle the three buttons, Escape closes — the
// same centralized-on-every-widget pattern captureBatchRenameKey/
// captureOptionsKey already establish for their own screens.
func (r *Root) captureCompareKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyTab:
		r.cycleCompareFocus(1)
		return nil
	case tcell.KeyBacktab:
		r.cycleCompareFocus(-1)
		return nil
	case tcell.KeyEscape:
		r.closeCompare()
		return nil
	}
	return event
}

func (r *Root) cycleCompareFocus(delta int) {
	buttons := r.compareButtonList()
	for i, b := range buttons {
		if !b.HasFocus() {
			continue
		}
		next := (i + delta + len(buttons)) % len(buttons)
		r.app.SetFocus(buttons[next])
		return
	}
	r.app.SetFocus(buttons[0])
}

// compareTargets is where the two things to compare come from —
// deliberately reusing existing selection machinery rather than a
// picker of its own (see the package's own scope note in doc.go):
// exactly two entries marked (in on-screen order — see
// SelectedPathsInDisplayOrder's own doc comment for why a plain
// SelectedPaths, backed by a map, would silently hand back the wrong
// pair some of the time) work from a single panel; failing that, split
// view lets the cursor position in each pane stand in for "the two
// things", which needs nothing marked at all. Anything else — fewer
// than two marked and no split — has no well-defined second target, so
// ok comes back false rather than guessing.
func (r *Root) compareTargets() (a, b string, ok bool) {
	if paths := r.panel.SelectedPathsInDisplayOrder(); len(paths) == 2 {
		return paths[0], paths[1], true
	}
	if r.splitActive && r.splitPanesValid() {
		other := r.splitPanes[0]
		if other == r.activeTab {
			other = r.splitPanes[1]
		}
		_, pa, okA := r.panel.CurrentRowPath()
		_, pb, okB := r.tabs[other].CurrentRowPath()
		if okA && okB {
			return pa, pb, true
		}
	}
	return "", "", false
}

// errCompareNeedsTwoTargets is compareTargets' own "nothing well-defined
// to compare" case, worded as guidance rather than a bare complaint —
// the same "tell them what to do instead" convention this app's other
// guard-rail errors already follow (see errNotSupportedInArchive).
var errCompareNeedsTwoTargets = errors.New("compare: mark exactly two items, or open split view and put the cursor on one item in each pane")

// openCompare is "C": routes to the file overlay or the directory
// screen depending on what compareTargets found — or reports why it
// couldn't, rather than silently doing nothing.
func (r *Root) openCompare() {
	if r.panel.inArchiveView() {
		r.showError(errNotSupportedInArchive)
		return
	}
	a, b, ok := r.compareTargets()
	if !ok {
		r.showError(errCompareNeedsTwoTargets)
		return
	}
	infoA, err := os.Lstat(a)
	if err != nil {
		r.showError(err)
		return
	}
	infoB, err := os.Lstat(b)
	if err != nil {
		r.showError(err)
		return
	}
	switch {
	case infoA.IsDir() && infoB.IsDir():
		r.openCompareTree(a, b)
	case infoA.IsDir() || infoB.IsDir():
		r.showError(fmt.Errorf("compare: %s is a directory, %s is not — compare two files or two directories", pickDir(a, infoA, b), pickDir(b, infoB, a)))
	default:
		r.openCompareFile(a, b)
	}
}

// pickDir names whichever of a/b actually is the directory, for
// openCompare's own mismatch message — a plain helper rather than
// inlining the conditional twice at the call site.
func pickDir(path string, info os.FileInfo, other string) string {
	if info.IsDir() {
		return path
	}
	return other
}

// openCompareFile shows the file-vs-file overlay for a and b, freshly
// computed — no hash or diff yet, both are on-demand (see the button
// row): metadata alone is instant, either of those isn't.
func (r *Root) openCompareFile(a, b string) {
	r.cancelCompareHashComputation()
	r.compareA, r.compareB = a, b
	r.compareHashes = nil
	r.renderCompareFile()

	// height covers title bar (1) + the header's own fixed 6 rows (see
	// compareHeaderHeight) + the verdict line's own wrap allowance (2)
	// + the button row (1), plus a little slack -- checked against a
	// real render, not guessed, the same lesson openSedReplace/
	// openDuplicate's own doc comments already record.
	width, height := 84, 13
	_, _, screenWidth, screenHeight := r.GetRect()
	if width > screenWidth-4 {
		width = screenWidth - 4
	}
	if height > screenHeight-4 {
		height = screenHeight - 4
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	r.compareLayout.SetRect(x, y, width, height)
	r.showOverlayWithRestore(comparePage, r.compareLayout, func() { r.app.SetFocus(r.compareHashBtn) })
}

// closeCompare hides the overlay and stops any hash computation still
// in flight — the same cancel-on-close reasoning cancelHashComputation
// already documents for Properties.
func (r *Root) closeCompare() {
	r.cancelCompareHashComputation()
	r.hideOverlay()
}

// renderCompareFile fills the header and verdict line from
// compare.CompareFiles, plus whatever's in compareHashes if a hash was
// already computed this open (see computeCompareHash) — called after
// every state change (open, hash result, hash progress tick) the same
// way renderBatchRenameFields is.
func (r *Root) renderCompareFile() {
	fc, err := compare.CompareFiles(r.compareA, r.compareB)
	if err != nil {
		r.showError(err)
		r.closeCompare()
		return
	}

	label := colorTag(r.theme.MutedTextColor)
	text := fmt.Sprintf("[%s]A:[-] %s\n[%s]B:[-] %s\n\n", label,
		tview.Escape(truncateMiddle(r.compareA, comparePathTruncateWidth)), label,
		tview.Escape(truncateMiddle(r.compareB, comparePathTruncateWidth)))
	text += fmt.Sprintf("%-10s %-28s %-28s\n", "", "A", "B")
	text += fmt.Sprintf("%-10s %-28s %-28s\n", "Size", sizeWithBytes(fc.A.Size), sizeWithBytes(fc.B.Size))
	text += fmt.Sprintf("%-10s %-28s %-28s\n", "Modified", formatModTimeCell(fc.A.ModTime, r.settings.MtimeUnix), formatModTimeCell(fc.B.ModTime, r.settings.MtimeUnix))
	r.compareHeader.SetText(text)

	r.compareVerdict.SetText(compareVerdictText(r.theme, fc, r.compareHashes, r.compareA, r.compareB, r.compareHashRunning, r.compareHashAnim))

	binaryA, _ := replace.LooksBinaryFile(r.compareA)
	binaryB, _ := replace.LooksBinaryFile(r.compareB)
	r.compareDiffBtn.SetDisabled(!compare.Available() || binaryA || binaryB || fc.A.IsDir || fc.B.IsDir)
}

// compareVerdictText is the one-line summary shown under the header:
// a hash result (if computed) always wins as the most definite answer
// available — SHA-256 alone, not all five fsops.Hash computes: a raw
// digest is only worth displaying in full when someone actually wants
// to read or copy it (Properties/Details already do exactly that for
// one file at a time — see "h" there), not for a same/different
// verdict, which one digest settles as conclusively as five. Short of
// a hash, CompareFiles' own size+time heuristic (see its own doc
// comment on why that's the same "quick check" rsync uses) — worded
// so "same size, different time" reads as the genuinely open question
// it is, not a hidden "probably fine".
func compareVerdictText(theme config.ResolvedTheme, fc compare.FileCompare, hashes map[string]string, a, b string, running bool, animFrame int) string {
	if running {
		return fmt.Sprintf("[%s]%s Computing hash…[-]", colorTag(theme.MutedTextColor), hashAnimationFrames[animFrame%len(hashAnimationFrames)])
	}
	if hashes != nil {
		if hashes[a] == hashes[b] {
			return fmt.Sprintf("[%s]Identical — SHA-256 matches.[-]", colorTag(theme.EntryExecutable))
		}
		return fmt.Sprintf("[%s]Different — SHA-256 does not match, despite what size/time alone suggested.[-]", colorTag(theme.EntryError))
	}
	switch {
	case !fc.SizeEqual:
		return fmt.Sprintf("[%s]Different — sizes don't match.[-]", colorTag(theme.EntryError))
	case fc.ModTimeEqual:
		return fmt.Sprintf("[%s]Probably identical — size and modification time both match (compute a hash to be certain).[-]", colorTag(theme.EntryExecutable))
	default:
		return fmt.Sprintf("[%s]Uncertain — same size, different modification time. Compute a hash for a definite answer.[-]", colorTag(theme.WarningText))
	}
}

// computeCompareHash hashes both compareA and compareB on a background
// goroutine — the same context.WithCancel + safeGo + QueueUpdateDraw
// shape computeHashes (properties.go) already establishes, just for two
// files instead of one. A no-op if a computation is already running, or
// if the overlay isn't even open.
func (r *Root) computeCompareHash() {
	if r.activePage != comparePage || r.compareHashRunning {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.compareHashCancel = cancel
	r.compareHashRunning = true
	r.compareHashAnim = 0
	r.renderCompareFile()

	a, b := r.compareA, r.compareB
	onPanic := func() {
		r.cancelCompareHashComputation()
		r.renderCompareFile()
	}
	r.safeGo("Compare hash progress animation", onPanic, func() { r.animateCompareHash(ctx) })
	r.safeGo("Compare hash computation", onPanic, func() {
		hashA, errA := hashFile(ctx, a, nil)
		if ctx.Err() != nil {
			return
		}
		if errA != nil {
			r.app.QueueUpdateDraw(func() {
				r.cancelCompareHashComputation()
				r.showError(errA)
			})
			return
		}
		hashB, errB := hashFile(ctx, b, nil)
		if ctx.Err() != nil {
			return
		}
		r.app.QueueUpdateDraw(func() {
			if ctx.Err() != nil {
				return
			}
			r.cancelCompareHashComputation()
			if errB != nil {
				r.showError(errB)
				return
			}
			r.compareHashes = map[string]string{a: hashA.SHA256, b: hashB.SHA256}
			r.renderCompareFile()
		})
	})
}

// animateCompareHash advances compareHashAnim until ctx is done —
// mirrors animateHashProgress (properties.go) exactly, for the same
// reason: a moving glyph beats a frozen "Computing…" for however long
// two files actually take to hash.
func (r *Root) animateCompareHash(ctx context.Context) {
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
				r.compareHashAnim++
				r.renderCompareFile()
			})
		case <-ctx.Done():
			return
		}
	}
}

// cancelCompareHashComputation stops whatever computeCompareHash call
// is in flight, if any — mirrors cancelHashComputation.
func (r *Root) cancelCompareHashComputation() {
	if r.compareHashCancel != nil {
		r.compareHashCancel()
		r.compareHashCancel = nil
	}
	r.compareHashRunning = false
}

// openCompareDiff is "Show diff": a real line-by-line answer for the
// currently-open file pair, via the system's own diff(1) (see
// compare.UnifiedDiff) — shown through the existing Look pager rather
// than a screen of its own. Not corner-cutting: Look's pager already
// does everything a diff view needs (scrolling, Page Up/Down, syntax
// coloring — chroma's own diff lexer already colors +/- lines exactly
// the way a patch file would, via the same viewer.Highlight/renderSyntax
// pipeline "l" uses on a real file, matched here by naming the temp
// file "*.diff" so that lexer is the one actually picked), so building
// a second pager would only ever be a worse copy of the first one.
//
// The temp file exists only for the synchronous instant showBuiltinLook
// needs to read it — removed immediately after, since by then its
// content is already copied into the pager's own TextView.
func (r *Root) openCompareDiff() {
	if !compare.Available() {
		r.showError(errors.New("compare: diff(1) not found on $PATH"))
		return
	}
	output, identical, err := compare.UnifiedDiff(r.compareA, r.compareB)
	if err != nil {
		r.showError(err)
		return
	}
	if identical {
		r.showError(fmt.Errorf("compare: %s and %s have no differences", filepath.Base(r.compareA), filepath.Base(r.compareB)))
		return
	}

	tmp, err := os.CreateTemp("", "breakthrough-compare-*.diff")
	if err != nil {
		r.showError(fmt.Errorf("compare: %w", err))
		return
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(output); err != nil {
		_ = tmp.Close()
		r.showError(fmt.Errorf("compare: %w", err))
		return
	}
	if err := tmp.Close(); err != nil {
		r.showError(fmt.Errorf("compare: %w", err))
		return
	}

	r.showBuiltinLook(tmp.Name())
}
