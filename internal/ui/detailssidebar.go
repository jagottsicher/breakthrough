package ui

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/viewer"
)

const detailsSidebarPage = "details-sidebar"

// detailsSidebarMinWidth is detailsSidebarSize's own floor for a
// terminal too narrow for a genuine one-third share to still be usable
// — same shape as helpMinWidth/helpMinHeight (see help.go). At least 26:
// infoFieldDateTime's own longer line ("Modified:" plus a 10-character
// date, 23 columns) needs 23 usable columns after the sidebar's own
// 1-column left/right border padding — see its own doc comment on why
// that specific line is guaranteed-length and worth guaranteeing space
// for, unlike most of this sidebar's other, unbounded-length fields.
const detailsSidebarMinWidth = 26

// newDetailsSidebarView builds the Details sidebar — a scrollable,
// read-only TextView, the same basic shape as Help/the Look pager, just
// positioned and shown/hidden its own way (see showDetailsSidebar/
// hideDetailsSidebar).
//
// Unlike every other overlay in this app (Properties, Search, Help, the
// Look pager, ...), this one is not modal: showing it never moves
// keyboard focus by itself, so the still-focused, still-fully-
// interactive panel underneath keeps working — the same way Midnight
// Commander's own "Info" panel mode works, rather than taking over the
// screen the way a dialog does. So it deliberately doesn't go through
// showOverlay/pushOverlay (which both hand keyboard focus to the widget
// they show) or get tracked in activePage/overlayStack —
// showDetailsSidebar/hideDetailsSidebar/toggleDetailsSidebar below are
// its own, separate, focus-preserving show/hide path, and
// refreshDetailsSidebar (wired to the panel table's own
// SetSelectionChangedFunc in NewRoot) is what keeps its content live
// instead of a one-shot snapshot the way Properties' own is.
//
// It CAN still take real focus, deliberately, once content grows longer
// than it has room for: Tab toggles focus between it and the panel (see
// ToggleDetailsFocusShortcut) so tview's own already-built-in TextView
// scrolling (arrow keys, PageUp/PageDown, Home/End, vi-style
// j/k/g/G/h/l, mouse wheel) works for free — cheaper and more capable
// than teaching this sidebar its own scroll keys from scratch, and
// scrolling while it has focus is exactly the same "browsing paused,
// reading instead" tradeoff a real MC info panel accepts too.
// SetFocusFunc/SetBlurFunc give a visual cue for whichever state it's
// currently in, the same FocusedBackground/AccentBackground contrast
// propertiesEditField already uses.
func (r *Root) newDetailsSidebarView() *tview.TextView {
	v := tview.NewTextView()
	v.SetDynamicColors(true)
	v.SetWrap(true)
	v.SetBorderPadding(0, 0, 1, 1)
	v.SetMouseCapture(r.captureDetailsSidebarMouse)
	v.SetFocusFunc(func() { v.SetBackgroundColor(r.theme.FocusedBackground) })
	v.SetBlurFunc(func() { v.SetBackgroundColor(r.theme.AccentBackground) })
	return v
}

// detailsSidebarSize sizes the sidebar against the whole screen (Root's
// own rect, like helpSize — see clampToScreen's own doc comment on why
// Help uses the same base instead of clampToPanel): at least a third of
// the screen's width, and — for now — its full height, top to bottom.
// Leaving two rows clear at the top and three at the bottom (to stay
// clear of the header and the button bar/status bar/bash console rows)
// is a deliberate follow-up, not done here yet.
func (r *Root) detailsSidebarSize() (width, height int) {
	_, _, screenWidth, screenHeight := r.GetRect()
	width = screenWidth / 3
	if width < detailsSidebarMinWidth {
		width = detailsSidebarMinWidth
	}
	return width, screenHeight
}

// repositionDetailsSidebar (re)computes the sidebar's own rect from
// detailsSidebarSize, flush against the right edge of the screen, and
// applies it — the size/position half of showDetailsSidebar, split out
// on its own so Root's own resize handling (see handleBeforeDraw) can
// re-run just this, without also reloading content or resetting scroll
// position the way showDetailsSidebar itself always does.
func (r *Root) repositionDetailsSidebar() {
	width, height := r.detailsSidebarSize()
	_, _, screenWidth, _ := r.GetRect()
	x, y, width, height := r.clampToScreen(screenWidth-width, 0, width, height)
	r.detailsSidebar.SetRect(x, y, width, height)
}

// showDetailsSidebar positions the sidebar flush against the right edge
// of the screen (see repositionDetailsSidebar) and shows it, without
// touching keyboard focus — see newDetailsSidebarView's own doc comment
// on why that matters here specifically, and preserveFocusAcross's own
// doc comment for the real bug that turned out to take to actually
// guarantee it. SendToFront matters for the same reason it does in
// pushOverlay: ShowPage alone doesn't reorder Pages' own internal draw
// order, so without it the sidebar could end up drawn underneath — and
// fully hidden by — some other page registered after it in NewRoot.
//
// Always (re)loads its content for whatever the panel's cursor
// currently points at, even if that's the same entry it showed the last
// time it was open — unlike refreshDetailsSidebar's own selection-change
// hook, which deliberately skips reloading when the cursor hasn't
// actually moved, this is the one path that must never skip: it's also
// what populates the sidebar the very first time it's ever shown, when
// there's nothing yet to compare the new target against.
func (r *Root) showDetailsSidebar() {
	r.repositionDetailsSidebar()
	r.preserveFocusAcross(func() {
		r.ShowPage(detailsSidebarPage)
		r.SendToFront(detailsSidebarPage)
	})
	r.detailsSidebarVisible = true

	_, path, ok := r.panel.CurrentRowPath()
	if !ok {
		path = ""
	}
	r.loadDetailsTarget(path)
}

// preserveFocusAcross runs f (some combination of ShowPage/HidePage/
// SendToFront), then restores whatever keyboard focus was in place right
// before it ran — the fix for a real bug: tview's own Pages.ShowPage/
// SendToFront/HidePage all end by calling their own Pages.Focus()
// internally whenever Root itself already has focus (which it always
// does), and that hands real keyboard focus to whichever page is now
// *last* among the currently-visible ones (verified directly against
// tview's own pages.go, not guessed) — for detailsSidebarPage, that's
// itself, the moment SendToFront makes it the last one. Every other
// overlay in this app (Properties, Search, Help, ...) already wants
// exactly that — pushOverlay's own explicit Application.SetFocus call
// right after is actually redundant with it, just harmlessly so, since
// both ultimately target the same widget. Details is the first thing in
// this app that explicitly doesn't want it (see newDetailsSidebarView's
// own doc comment): the observed symptom was arrow keys silently no
// longer navigating the panel at all, for as long as the sidebar was
// shown — tview had quietly moved real focus onto detailsSidebar itself
// underneath, invisibly, with nothing in this codebase's own tests ever
// asserting Application.GetFocus() after showing it to catch that.
func (r *Root) preserveFocusAcross(f func()) {
	previousFocus := r.app.GetFocus()
	f()
	if previousFocus != nil {
		r.app.SetFocus(previousFocus)
	}
}

// hideDetailsSidebar closes the sidebar — showDetailsSidebar's own
// counterpart. Cancels a hash computation still in flight, per the
// original design intent: an expensive calculation running purely for
// this sidebar's own benefit has no reason to keep going once it's not
// even shown any more.
//
// Explicitly redirects focus to the panel's own table if the sidebar
// itself currently has it (see ToggleDetailsFocusShortcut) — a case
// preserveFocusAcross alone can't handle correctly: "restore whatever
// had focus right before" is exactly wrong here, since what had focus
// right before *is* the widget this is about to hide. Left to it alone,
// focus would end up parked on a now-invisible primitive, with nothing
// left able to receive it back.
func (r *Root) hideDetailsSidebar() {
	r.cancelDetailsHashComputation()
	hadFocus := r.detailsSidebar.HasFocus()
	r.preserveFocusAcross(func() { r.HidePage(detailsSidebarPage) })
	if hadFocus {
		r.app.SetFocus(r.panel.table)
	}
	r.detailsSidebarVisible = false
}

// toggleDetailsSidebar is the Details button's own action (see
// runButtonBarAction) — called directly and unguarded, the same "a
// click is always deliberate" reasoning every other button click
// already gets. ToggleDetailsSidebarShortcut (Ctrl+D) is this plus the
// same acceptsGlobalShortcut precondition every other keyboard shortcut
// checks — see its own doc comment for why that one specifically can't
// skip it the way this can.
func (r *Root) toggleDetailsSidebar() {
	if r.detailsSidebarVisible {
		r.hideDetailsSidebar()
	} else {
		r.showDetailsSidebar()
	}
}

// ToggleDetailsSidebarShortcut is Ctrl+D's own action — see
// cmd/breakthrough, which falls through to bashLine's own handling
// (returns the event, not nil) whenever acceptsGlobalShortcut reports
// false, the same as Ctrl+T/Ctrl+S/Ctrl+P/Ctrl+B: tview's own TextArea
// binds Ctrl+D to "delete forward" (the same as the physical Delete
// key), and losing that while typing a command would be a real, working
// feature silently broken, not just an unlikely readline convention no
// one would ever actually hit.
func (r *Root) ToggleDetailsSidebarShortcut() {
	if r.acceptsGlobalShortcut() {
		r.toggleDetailsSidebar()
	}
}

// ToggleDetailsFocusShortcut is Tab's own action while either the
// panel's own table or the details sidebar itself currently has real
// keyboard focus — see cmd/breakthrough, which only consumes Tab when
// this returns true. Everywhere else (Properties' own fields, the
// header's path edit, the filter box, bash-line completion, ...), it
// reports false and Tab falls through completely untouched, exactly as
// it already does today — verified against tview's own Table/TextView
// source: neither installs a SetDoneFunc in this codebase, so a bare
// Tab while the table has focus was already a pure no-op before this
// existed, nothing here takes over a working feature.
//
// The sidebar only actually needs real focus once its own content
// outgrows the space it has — see newDetailsSidebarView's own doc
// comment on why tview's already-built-in TextView scrolling is worth
// that trade-off (browsing pauses while it's focused, since the panel
// isn't what's receiving arrow keys any more) rather than teaching this
// sidebar its own separate scroll keys.
func (r *Root) ToggleDetailsFocusShortcut() bool {
	switch {
	case r.detailsSidebarVisible && r.panel.table.HasFocus():
		r.app.SetFocus(r.detailsSidebar)
		return true
	case r.detailsSidebar.HasFocus():
		r.app.SetFocus(r.panel.table)
		return true
	default:
		return false
	}
}

// refreshDetailsSidebar reloads and re-renders the sidebar's content for
// whatever the panel's own cursor currently points at — wired to the
// panel table's own SetSelectionChangedFunc in NewRoot, so it runs
// after arrow-key navigation, a row click, or a directory reload
// repositioning the cursor. A cheap no-op whenever the sidebar isn't
// currently shown, so plain browsing costs nothing extra the rest of
// the time, and a further no-op when the cursor landed back on the
// exact entry it was already showing (e.g. a reload that leaves the
// selection where it was) — see loadDetailsTarget for the part that
// actually does the (re)loading.
func (r *Root) refreshDetailsSidebar() {
	if !r.detailsSidebarVisible {
		return
	}

	_, path, ok := r.panel.CurrentRowPath()
	if !ok {
		path = ""
	}
	if path == r.detailsTarget {
		return
	}
	r.loadDetailsTarget(path)
}

// loadDetailsTarget stats path (""=nothing meaningfully selected, e.g.
// the ".." row or an otherwise-empty listing) and, for a non-directory
// it successfully stats, tries loading a preview too — an image
// directly (see viewer.Load), or for a PDF, its own page count (see
// viewer.PDFPageCount) plus a rasterized first page where that's
// possible (see viewer.LoadPDFPage) — the shared data-loading half of
// refreshDetailsSidebar/showDetailsSidebar (see their own doc comments
// on why one skips a redundant reload and the other never does).
// Cancels and discards any hash computation and metadata state from
// whatever the previous target was — both are meaningless for a
// different file, the same reasoning loadPropertiesTarget already
// applies for Properties.
//
// PDFViewGraphic, not the default PDFViewAuto Look itself starts a PDF
// with, deliberately: a failure to rasterize should leave detailsImage
// nil so renderDetailsSidebar falls back to its own "Pages: N" line and
// a short install hint, not silently show extracted page text crammed
// into this sidebar's own much narrower width than Look ever has to
// account for.
func (r *Root) loadDetailsTarget(path string) {
	r.cancelDetailsHashComputation()
	r.detailsMetadataState = ""
	r.detailsTarget = path
	// Adopts Properties' own result immediately if it's already open on
	// this exact file and has one (see propertiesHashesFor's own doc
	// comment) — nil otherwise, same as the plain assignment this
	// replaced.
	r.detailsHashes = r.propertiesHashesFor(path)

	r.detailsStat = fsops.Info{}
	r.detailsStatErr = nil
	r.detailsImage = nil
	r.detailsPDFPageCount = 0
	if path != "" {
		r.detailsStat, r.detailsStatErr = fsops.Stat(path)
		if r.detailsStatErr == nil && !isDirish(r.detailsStat) {
			if result, err := viewer.Load(path, viewer.DefaultPreviewLimit); err == nil {
				switch result.Kind {
				case viewer.KindImage:
					r.detailsImage = &result
				case viewer.KindPDF:
					if count, err := viewer.PDFPageCount(path); err == nil {
						r.detailsPDFPageCount = count
					}
					if page, err := viewer.LoadPDFPage(path, 1, viewer.PDFViewGraphic); err == nil && page.Kind == viewer.KindImage {
						r.detailsImage = &page
					}
				}
			}
		}
	}
	r.renderDetailsSidebar()

	// A new target always starts showing from its own top — not
	// wherever the previous one happened to be scrolled to (see
	// ToggleDetailsFocusShortcut for how it gets scrolled at all).
	// Deliberately not in renderDetailsSidebar itself: that also reruns
	// on every hash-progress animation frame for the *same* target,
	// which must never yank the scroll position back to the top out
	// from under someone actually reading further down.
	r.detailsSidebar.ScrollToBeginning()
}

// detailsImageBoxSize is the box renderDetailsSidebar scales an image
// preview to fit within: the sidebar's own inner width, and at most a
// third of its inner height — per the user's own explicit request, so a
// tall/portrait image can never grow to dominate the whole sidebar the
// way an unconstrained preview would. ScaleForTerminal always preserves
// the image's own aspect ratio within that box (never stretching or
// cropping — see its own doc comment), so a wide/landscape image simply
// ends up shorter than this — renderDetailsSidebar itself only ever
// passes the scaled image's own real row count to renderImageHalfBlocks,
// not this function's own maximum, specifically so that "shorter" isn't
// then padded back out to the full reserved height with blank lines
// (real, observed bug: a strangely large gap between a mini preview and
// whatever came right after it, for exactly this reason).
func (r *Root) detailsImageBoxSize() (width, height int) {
	_, _, innerWidth, innerHeight := r.detailsSidebar.GetInnerRect()
	height = innerHeight / 3
	if height < 1 {
		height = 1
	}
	return innerWidth, height
}

// detailsFullscreenHint is the preview section's own click-zone hint
// (image or rasterized PDF page alike) — reuses Ctrl+L, this app's own
// existing "Look" shortcut, rather than inventing a second one: Look
// already opens fullscreen for exactly these two Kinds (see
// showBuiltinLook), and it already works from here unmodified (it
// reads the panel's own current selection itself, the same as this
// sidebar's own target — see openLook), so nothing new was needed
// beyond a click zone routing to it and a line saying so.
//
// Deliberately short (18 characters) rather than spelling out "or click
// here" the way the hash/metadata hints do: a real, observed bug once
// found the longer wording wrapped at this sidebar's own minimum width
// (see detailsSidebarMinWidth), silently mis-numbering every row after
// it — the exact same class of bug already fixed once for hashes and
// once for Modified. This one's own click-zone is also the whole
// preview image itself (see renderDetailsSidebar/
// captureDetailsSidebarMouse), a much bigger and more discoverable
// target than the text alone, so losing the explicit "click here"
// wording costs less here than it would in the other two hints.
const detailsFullscreenHint = "Ctrl+L: fullscreen"

// detailsMetadataHint is the metadata section's own placeholder, shown
// until fetchDetailsMetadata actually runs for the current target — the
// same "hint until triggered" shape hashLines already uses for hashes,
// just naming Ctrl+N instead of Ctrl+K.
//
// Deliberately short, for the same reason detailsFullscreenHint is (see
// its own doc comment): unlike the hash hint, which sits last with
// nothing after it to mis-number, this one is always followed by the
// stat block (and often the hash section too) — a real, observed bug
// once found the original, longer wording of this exact string wrapped
// at the sidebar's own minimum width, silently pushing both of those
// down by a row this function's own click-zone bookkeeping never knew
// to account for.
const detailsMetadataHint = "Ctrl+N: load metadata"

// detailsMetadataStubMessage is what fetchDetailsMetadata currently
// shows once triggered — see its own doc comment on why that's a stub
// rather than real EXIF/format-specific metadata today. Short for the
// same reason detailsMetadataHint is: this replaces that hint in the
// exact same row-tracked position, so it's just as capable of
// mis-numbering everything after it if it wrapped.
const detailsMetadataStubMessage = "(not implemented yet)"

// fetchDetailsMetadata is Ctrl+N/the metadata hint's click zone's own
// action — currently a stub: real EXIF/format-specific metadata
// extraction is deliberate follow-up work, once a metadata library has
// actually been picked, not something this pass builds. What already
// works end-to-end today is everything around it — the hint text, its
// click zone, and this keyboard shortcut all visibly do something when
// triggered, just not the real data yet.
func (r *Root) fetchDetailsMetadata() {
	if r.detailsImage == nil {
		return
	}
	r.detailsMetadataState = detailsMetadataStubMessage
	r.renderDetailsSidebar()
}

// FetchMetadataShortcut is Ctrl+N's own action — see cmd/breakthrough.
// Properties has no metadata section of its own to defer to (unlike
// hashes — see ComputeHashesShortcut), so this only ever targets
// Details.
func (r *Root) FetchMetadataShortcut() {
	if r.detailsSidebarVisible {
		r.fetchDetailsMetadata()
	}
}

// infoFieldDateTime is wideInfoField's own shape (label on line one,
// an aligned second line under it), but splitting at the natural
// boundary between date and time instead of a value's own midpoint — a
// real, observed bug once found the plain single-line
// infoField("Modified", ...) this replaced wrapping by exactly one
// column on the sidebar's own minimum width (see detailsSidebarMinWidth):
// unlike a hash digest, whose length only ever changes with the
// algorithm (a handful of fixed possibilities — see wideInfoField's own
// doc comment), "YYYY-MM-DD HH:MM:SS" is a known, fixed 19 characters
// for every file, every time, so this sidebar can — and should —
// guarantee it never wraps, the same way it already guarantees that for
// hashes.
func infoFieldDateTime(label string, t time.Time) string {
	return fmt.Sprintf("%-13s%s\n%13s%s", label+":", t.Format("2006-01-02"), "", t.Format("15:04:05"))
}

// detailsStatLines formats stat, mirroring renderProperties' own field
// set and formatting (see fsops.Stat/Info) but as a single flat block of
// plain, read-only lines — no editable spans, no focus tracking: this
// sidebar never takes keyboard focus at all (see newDetailsSidebarView's
// own doc comment), so there's nothing here that could ever be
// "focused" the way Properties' own Name/Permissions/Owner/Group/
// Modified fields are.
func detailsStatLines(stat fsops.Info, target string) string {
	lines := []string{
		infoField("Type", classifyKind(stat)),
		infoField("Permissions", formatPermissions(stat.Mode)),
	}
	if stat.Nlink > 1 && !stat.IsDir {
		// Not shown for directories — see renderProperties' own doc
		// comment on why Nlink >= 2 is trivially true for every one of
		// those already.
		lines = append(lines, infoField("Links", fmt.Sprintf("%d (shared with other names)", stat.Nlink)))
	}
	lines = append(lines,
		infoField("Owner", stat.Owner),
		infoField("Group", stat.Group),
		infoField("Size", sizeWithBytes(stat.Size)),
		infoFieldDateTime("Modified", stat.ModTime),
		infoField("Path", stat.Path),
	)
	if stat.IsSymlink && stat.LinkTarget != "" {
		lines = append(lines, infoField("Link target", stat.LinkTarget))
	}
	if stat.MountPoint {
		lines = append(lines, infoField("Mount point", "yes"))
	}
	if stat.IsSymlink {
		if chain := fsops.ResolveChain(target); len(chain.Hops) > 1 {
			// Only shown once there's an actual chain (more than one
			// hop) — see renderProperties' own identical condition.
			lines = append(lines, infoField("Chain", formatChain(chain)))
		}
	}
	return strings.Join(lines, "\n")
}

// formatPermissions renders mode the same way Properties' own
// permissionsField does (type char, then rwxrwxrwx with '-' for unset
// bits, then the octal form) — duplicated rather than shared because
// permissionsField's version is inseparable from its own per-bit
// propertySpan/focus-tag tracking, none of which applies to a read-only
// display with nothing to click or focus.
func formatPermissions(mode os.FileMode) string {
	const rwx = "rwxrwxrwx"
	b := make([]byte, 10)
	b[0] = permTypeChar(mode)
	for i := 0; i < 9; i++ {
		b[i+1] = '-'
		if mode&(1<<uint(9-1-i)) != 0 {
			b[i+1] = rwx[i]
		}
	}
	return fmt.Sprintf("%s (%04o)", b, mode.Perm())
}

// renderDetailsSidebar rebuilds the sidebar's text from whatever
// loadDetailsTarget last populated (detailsStat/detailsImage/
// detailsPDFPageCount/detailsHashes/detailsMetadataState), in the order
// the user explicitly asked for: a preview (an image, or a rasterized
// PDF page — see detailsImageBoxSize) with its own Format/Dimensions or
// Type/Pages line and a fullscreen hint (see detailsFullscreenHint),
// then — images only — a metadata hint/result (see detailsMetadataHint),
// then the stat block (see detailsStatLines), and finally the hash
// section (see hashLines) — omitted entirely for a directory, or a
// symlink resolving to one, the same as Properties' own isDirish check.
//
// detailsPreviewRowStart/End, detailsMetaRowStart/End, and
// detailsHashRowStart are (re)computed here, alongside the text they
// describe, so captureDetailsSidebarMouse's own click routing always
// matches whatever is actually on screen right now — the same
// "compute once, right where the content itself is
// built" approach hashSectionRow already takes in renderProperties,
// rather than trying to reverse-engineer row numbers separately
// afterward.
func (r *Root) renderDetailsSidebar() {
	r.detailsPreviewRowStart, r.detailsPreviewRowEnd = -1, -1
	r.detailsMetaRowStart, r.detailsMetaRowEnd = -1, -1
	r.detailsHashRowStart = -1

	if r.detailsTarget == "" {
		r.detailsSidebar.SetText("(nothing selected)")
		return
	}
	if r.detailsStatErr != nil {
		r.detailsSidebar.SetText(fmt.Sprintf("(couldn't read this entry: %v)", r.detailsStatErr))
		return
	}

	var b strings.Builder
	row := 0
	// writeSection appends s as its own paragraph (separated from
	// whatever came before by a blank line, the same visual grouping
	// renderProperties' own hash section already gets), and returns the
	// row range s itself now occupies within the final text — the only
	// numbers captureDetailsSidebarMouse actually needs to route a click
	// correctly.
	writeSection := func(s string) (startRow, endRow int) {
		if b.Len() > 0 {
			b.WriteString("\n\n")
			row += 2
		}
		startRow = row
		b.WriteString(s)
		endRow = row + strings.Count(s, "\n")
		row = endRow
		return
	}

	isPDF := r.detailsPDFPageCount > 0

	switch {
	case r.detailsImage != nil:
		boxW, maxBoxH := r.detailsImageBoxSize()
		scaled := viewer.ScaleForTerminal(r.detailsImage.Image, boxW, maxBoxH)

		// The scaled image's own real row count, not maxBoxH — passing
		// the full reserved maximum here would have renderImageHalfBlocks
		// pad a shorter image back out with blank lines to fill it (its
		// own vertical-centering logic, meant for a box it's actually
		// supposed to fill) — see detailsImageBoxSize's own doc comment
		// on the real, observed gap this caused.
		actualRows := (scaled.Bounds().Dy() + 1) / 2
		if actualRows < 1 {
			actualRows = 1
		}
		// TrimSuffix: renderImageHalfBlocks terminates every row with its
		// own "\n", including the last one — never a problem for its only
		// other caller (Look's own showBuiltinLook), which never
		// concatenates anything else after an image in the same text, so
		// nothing was ever there to expose it. writeSection assumes a
		// section has no trailing newline of its own (true of every other
		// section here, all built via strings.Join without one) — without
		// this trim, that extra "\n" combines with writeSection's own
		// "\n\n" separator into two blank lines instead of one, a real,
		// observed gap, and silently overcounts this section's own row
		// range by one to boot.
		preview := strings.TrimSuffix(renderImageHalfBlocks(scaled, boxW, actualRows), "\n")
		previewStart, _ := writeSection(preview)

		var infoLines []string
		if isPDF {
			infoLines = []string{
				infoField("Type", "PDF"),
				infoField("Pages", strconv.Itoa(r.detailsPDFPageCount)),
			}
		} else {
			bounds := r.detailsImage.Image.Bounds()
			infoLines = []string{
				infoField("Format", strings.ToUpper(r.detailsImage.ImageFormat)),
				infoField("Dimensions", fmt.Sprintf("%d × %d px", bounds.Dx(), bounds.Dy())),
			}
		}
		infoLines = append(infoLines, detailsFullscreenHint)
		_, previewEnd := writeSection(strings.Join(infoLines, "\n"))
		r.detailsPreviewRowStart, r.detailsPreviewRowEnd = previewStart, previewEnd

		if !isPDF {
			// EXIF-style metadata is an image-specific concept (see
			// fetchDetailsMetadata's own doc comment) — a PDF has its own,
			// different kind of metadata (Title/Author/Creator, ...), not
			// built here yet, so no hint for it is shown on one.
			meta := r.detailsMetadataState
			if meta == "" {
				meta = detailsMetadataHint
			}
			r.detailsMetaRowStart, r.detailsMetaRowEnd = writeSection(meta)
		}

	case isPDF:
		// PDFPageCount succeeded but rasterizing page 1 didn't (most
		// likely: pdftoppm/poppler-utils isn't installed — see
		// loadDetailsTarget) — still worth naming the page count, with a
		// short note on how to get an actual preview, rather than
		// silently showing nothing at all where the image branch above
		// would have been.
		writeSection(strings.Join([]string{
			infoField("Type", "PDF"),
			infoField("Pages", strconv.Itoa(r.detailsPDFPageCount)),
			"(install poppler-utils for a page preview)",
		}, "\n"))
	}

	writeSection(detailsStatLines(r.detailsStat, r.detailsTarget))

	if !isDirish(r.detailsStat) {
		var hashText string
		switch {
		case r.detailsHashInProgress:
			hashText = hashAnimationFrames[r.detailsHashAnimFrame%len(hashAnimationFrames)] + " Computing hashes" + hashProgressSuffix(r.detailsHashBytesRead.Load(), r.detailsStat.Size)
		default:
			// The sidebar's own actual current width, not
			// propertiesHashFieldWidth's fixed constant (see its own doc
			// comment) — a real, observed bug once used that constant's
			// 64-character halves here too, and each one wrapped again
			// inside this sidebar's own much narrower box.
			_, _, innerWidth, _ := r.detailsSidebar.GetInnerRect()
			hashText = hashLines(r.detailsHashes, "Press Ctrl+K or click here to compute SHA-256 / SHA-1 / MD5 / SHA-512 / BLAKE2b-512", innerWidth)
		}
		r.detailsHashRowStart, _ = writeSection(hashText)
	}

	r.detailsSidebar.SetText(b.String())
}

// computeDetailsHashes is Ctrl+K/the hash section's own click zone's
// action while Details (not Properties) is the relevant target — see
// ComputeHashesShortcut's own doc comment on how that's decided.
// Mirrors Properties' own computeHashes exactly (see its doc comment
// for the full reasoning on the background goroutine/animation/
// cancellation shape) — duplicated rather than shared because the two
// track independent state (detailsHashes vs propertiesHashes) for what
// can, deliberately, be two different displays of the very same file at
// once (Details isn't modal — see newDetailsSidebarView's own doc
// comment — so it can stay open behind Properties).
func (r *Root) computeDetailsHashes() {
	if r.detailsTarget == "" || isDirish(r.detailsStat) || r.detailsHashInProgress {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.detailsHashCancel = cancel
	r.detailsHashInProgress = true
	r.detailsHashAnimFrame = 0
	r.detailsHashBytesRead.Store(0)
	r.renderDetailsSidebar()

	target := r.detailsTarget
	go r.animateDetailsHashProgress(ctx)
	go func() {
		hashes, err := hashFile(ctx, target, r.detailsHashBytesRead.Store)
		if ctx.Err() != nil {
			return // superseded before we even got to report anything — see cancelDetailsHashComputation
		}
		r.app.QueueUpdateDraw(func() {
			if ctx.Err() != nil {
				return
			}
			r.cancelDetailsHashComputation()
			if err != nil {
				r.showError(err)
				return
			}
			// Updates detailsHashes/renders itself, then mirrors into
			// Properties too if that's showing the same target — see
			// propagateHashResult's own doc comment.
			r.propagateHashResult(target, hashes)
		})
	}()
}

// propagateHashResult is the one place computeHashes/computeDetailsHashes
// both hand their finished digest to, updating whichever of
// Properties/Details is currently showing target — including whichever
// one actually ran the computation, not just "the other" one: its own
// condition below is just as true for that side, so this is the sole
// place either one's own display actually gets updated, not a
// bolt-on for a second reader. Per the user's own explicit request:
// since Details isn't modal (see newDetailsSidebarView's own doc
// comment), both can be open on the very same file at once, and a hash
// computed via either one — the keyboard shortcut, or either one's own
// click zone (see ComputeHashesShortcut/captureDetailsSidebarMouse) —
// is worth showing in both, not just wherever it happened to be
// triggered.
//
// Deliberately only mirrors the finished result, not the in-progress
// animation while one side is still computing — the other briefly still
// shows its own "not computed yet" hint until this runs, rather than
// needing the two independent progress states to stay synchronized
// while a computation is still in flight.
func (r *Root) propagateHashResult(target string, hashes fsops.Hashes) {
	if r.activePage == propertiesPage && r.propertiesTarget == target {
		r.propertiesHashes = &hashes
		r.rerenderProperties()
	}
	if r.detailsSidebarVisible && r.detailsTarget == target {
		r.detailsHashes = &hashes
		r.renderDetailsSidebar()
	}
}

// detailsHashesFor returns Details' own already-computed result if it's
// currently showing target, or nil — propagateHashResult's own
// "adopt on open" counterpart, checked by loadPropertiesTarget: opening
// Properties on a file Details already hashed shows that result right
// away instead of a fresh "press Ctrl+K" hint, per the user's own
// explicit request. Unlike propagateHashResult, this only ever needs
// one direction here — loadPropertiesTarget is the one doing the
// asking, so there's no symmetric "does Properties already have it"
// question to also answer in the same call (see propertiesHashesFor for
// that, checked the other way around by loadDetailsTarget).
func (r *Root) detailsHashesFor(target string) *fsops.Hashes {
	if r.detailsSidebarVisible && r.detailsTarget == target {
		return r.detailsHashes
	}
	return nil
}

// propertiesHashesFor is detailsHashesFor's own mirror image, checked
// by loadDetailsTarget: Details loading a target Properties already has
// open and hashed (always the same file whenever both are showing
// something at once — see propagateHashResult's own doc comment on why
// that's guaranteed, not just usually true) adopts that result
// immediately too, rather than needing its own separate computation.
func (r *Root) propertiesHashesFor(target string) *fsops.Hashes {
	if r.activePage == propertiesPage && r.propertiesTarget == target {
		return r.propertiesHashes
	}
	return nil
}

// animateDetailsHashProgress mirrors Properties' own
// animateHashProgress — see its doc comment.
func (r *Root) animateDetailsHashProgress(ctx context.Context) {
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
				r.detailsHashAnimFrame++
				r.renderDetailsSidebar()
			})
		case <-ctx.Done():
			return
		}
	}
}

// cancelDetailsHashComputation mirrors Properties' own
// cancelHashComputation — see its doc comment. Called once a result
// arrives, when the target changes (see loadDetailsTarget), and when
// the sidebar is hidden (see hideDetailsSidebar).
func (r *Root) cancelDetailsHashComputation() {
	if r.detailsHashCancel != nil {
		r.detailsHashCancel()
		r.detailsHashCancel = nil
	}
	r.detailsHashInProgress = false
}

// ComputeHashesShortcut is Ctrl+K's own action — see cmd/breakthrough.
// Also Properties' *only* way to trigger this now (see
// hashesMouseCapture's own doc comment on why the bare 'h' it used to
// have besides its own click zone is gone). Targets whichever of
// Properties/Details is actually the relevant one right now, decided by
// real keyboard focus: Properties, being modal, always holds it while
// open, so Ctrl+K then reuses its own existing computeHashes instead of
// reaching past it to a Details sidebar sitting, unfocused, behind it —
// per the user's own explicit request for what should happen when both
// are open at once. Otherwise, if Details is currently shown, targets
// its own hash section (see computeDetailsHashes) instead. A no-op if
// neither applies.
func (r *Root) ComputeHashesShortcut() {
	switch {
	case r.activePage == propertiesPage:
		r.computeHashes()
	case r.detailsSidebarVisible:
		r.computeDetailsHashes()
	}
}

// captureDetailsSidebarMouse handles every mouse action landing within
// the sidebar's own current rect — everything else falls through to
// whatever is still there in the panel underneath, which shares that
// same screen space while the sidebar floats over it, so a click or
// scroll meant for it doesn't leak through by accident.
//
// MouseScrollUp/MouseScrollDown and MouseLeftDown are deliberately let
// through to the TextView's own default MouseHandler instead of being
// swallowed here: MouseLeftDown is what tview's own Box.MouseHandler
// uses to actually call Application.SetFocus (see its doc comment in
// box.go) — clicking anywhere in the sidebar, not just Tab (see
// ToggleDetailsFocusShortcut), is a second, more discoverable way to
// give it real focus — and MouseScrollUp/Down is genuinely native
// TextView behavior, working immediately regardless of focus (unlike
// keyboard scrolling, which does need it). A MouseLeftClick landing on
// a specific click zone (see renderDetailsSidebar for how
// detailsPreviewRowStart/End, detailsMetaRowStart/End, and
// detailsHashRowStart get set — the same row-range approach Properties'
// own hashesMouseCapture already uses) runs that action instead of
// falling through; a click anywhere else in the sidebar also falls
// through, to whatever tview's own default handling does with it
// (nothing harmful — this TextView uses none of the
// region/highlighting features that's actually for).
func (r *Root) captureDetailsSidebarMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	x, y := event.Position()
	if !primitiveContains(r.detailsSidebar, x, y) {
		return action, event
	}

	switch action {
	case tview.MouseScrollUp, tview.MouseScrollDown, tview.MouseLeftDown:
		return action, event

	case tview.MouseLeftClick:
		_, rectY, _, _ := r.detailsSidebar.GetInnerRect()
		row := y - rectY
		switch {
		case r.detailsPreviewRowStart >= 0 && row >= r.detailsPreviewRowStart && row <= r.detailsPreviewRowEnd:
			r.lookCurrentEntry()
			return tview.MouseConsumed, nil
		case r.detailsMetaRowStart >= 0 && row >= r.detailsMetaRowStart && row <= r.detailsMetaRowEnd:
			r.fetchDetailsMetadata()
			return tview.MouseConsumed, nil
		case r.detailsHashRowStart >= 0 && row >= r.detailsHashRowStart:
			// Through the same dispatcher Ctrl+K uses, not
			// computeDetailsHashes directly — so a click here defers to
			// Properties too when that's the one currently open (see
			// ComputeHashesShortcut's own doc comment), the same as
			// pressing the key would; clicking shouldn't be a second way
			// to bypass that rule.
			r.ComputeHashesShortcut()
			return tview.MouseConsumed, nil
		}
		return action, event

	default:
		return tview.MouseConsumed, nil
	}
}
