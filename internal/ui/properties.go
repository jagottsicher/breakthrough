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

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/fsops"
)

const propertiesPage = "properties"

// propertyField identifies one editable region within the Properties
// overlay — see propertySpan and propertiesBuilder.
type propertyField int

const (
	fieldNone propertyField = iota
	fieldName
	fieldPermOwnerRead
	fieldPermOwnerWrite
	fieldPermOwnerExec
	fieldPermGroupRead
	fieldPermGroupWrite
	fieldPermGroupExec
	fieldPermOtherRead
	fieldPermOtherWrite
	fieldPermOtherExec
	fieldPermOctal
	fieldMtimeDate
	fieldMtimeTime
	fieldOwner
	fieldGroup
)

// propertyFieldOrder is every editable field, in the same top-to-bottom
// order renderProperties draws them — the Tab/Backtab navigation order
// (see movePropertiesFocus/setPropertiesFocus): Name, the 9 permission
// bits, the octal permission value (fieldPermOctal — an equivalent,
// direct way to set all 9 at once, per the user's own request, rather
// than only one bit at a time by clicking), Owner, Group, then the
// Modified date and time halves. Cancel and Save (see newPropertiesView)
// follow immediately after, as stops len(propertyFieldOrder) and
// len(propertyFieldOrder)+1 — not part of this slice themselves, since
// they're real tview.Button widgets with their own focus, not text spans
// within propertiesText.
var propertyFieldOrder = []propertyField{
	fieldName,
	fieldPermOwnerRead, fieldPermOwnerWrite, fieldPermOwnerExec,
	fieldPermGroupRead, fieldPermGroupWrite, fieldPermGroupExec,
	fieldPermOtherRead, fieldPermOtherWrite, fieldPermOtherExec,
	fieldPermOctal,
	fieldOwner, fieldGroup,
	fieldMtimeDate, fieldMtimeTime,
}

// propertyFieldIndex returns field's position in propertyFieldOrder —
// used both to seed propertiesFocusIndex when a field is clicked (see
// activatePropertyField) and, in reverse, by focusedPropertyField to
// render the right one highlighted.
func propertyFieldIndex(field propertyField) (int, bool) {
	for i, f := range propertyFieldOrder {
		if f == field {
			return i, true
		}
	}
	return 0, false
}

// permFieldBit maps each permission propertyField to the bit it toggles
// in a permission-only os.FileMode (0-0777) — owner/group/other times
// read/write/execute, matching os.FileMode.Perm()'s own bit order.
var permFieldBit = map[propertyField]os.FileMode{
	fieldPermOwnerRead:  0o400,
	fieldPermOwnerWrite: 0o200,
	fieldPermOwnerExec:  0o100,
	fieldPermGroupRead:  0o040,
	fieldPermGroupWrite: 0o020,
	fieldPermGroupExec:  0o010,
	fieldPermOtherRead:  0o004,
	fieldPermOtherWrite: 0o002,
	fieldPermOtherExec:  0o001,
}

// permFieldLetter maps each permission propertyField to the rwx letter
// that explicitly turns it on (see capturePropertiesKey) — 'r'/'w'/'x'
// depending on which of the 9 bits it is, regardless of owner/group/
// other. The matching letter sets the bit on; Delete or '-' sets it off;
// Space/Enter toggle whatever it currently is — three ways in, per the
// user's own request, alongside the plain click every bit already had.
var permFieldLetter = map[propertyField]byte{
	fieldPermOwnerRead: 'r', fieldPermGroupRead: 'r', fieldPermOtherRead: 'r',
	fieldPermOwnerWrite: 'w', fieldPermGroupWrite: 'w', fieldPermOtherWrite: 'w',
	fieldPermOwnerExec: 'x', fieldPermGroupExec: 'x', fieldPermOtherExec: 'x',
}

// propertySpan is one clickable region within the Properties overlay's
// text — the same half-open [start,end) column-range idea as headerSpan,
// with a row added since Properties, unlike the header, is multi-line.
type propertySpan struct {
	row              int
	startCol, endCol int
	field            propertyField
}

// propertiesBuilder assembles the Properties overlay's text field by
// field, tracking each editable field's row/column span as it goes (see
// propertySpan) — the same running-column-count idea buildHeaderSpans
// uses for the path bar, extended to multiple rows. Style tags (used for
// the slategray/aqua styling) are written via tag, which — unlike text —
// doesn't advance col, since tags are zero-width once tview renders them;
// getting this distinction wrong would silently throw off every span
// after the first highlighted one.
type propertiesBuilder struct {
	b     strings.Builder
	row   int
	col   int
	spans []propertySpan

	// theme drives focusTag's own colors — set once when pb is
	// constructed (see renderProperties), from whatever Root.theme
	// currently is.
	theme config.ResolvedTheme
}

func (pb *propertiesBuilder) tag(s string) {
	pb.b.WriteString(s)
}

// text advances col by s's display width (tview.TaggedStringWidth, the
// same measure buildHeaderSpans uses — see its own doc comment), not a
// plain rune count: a file name value can itself contain double-width
// (e.g. CJK) characters, and a rune count would leave that field's own
// span, and every span after it on the same row, misaligned with where
// the text is actually drawn — visible as the inline editor (see
// activateInlineTextField) or the owner/group picker (see
// propertyFieldPosition) landing next to the real text instead of on it.
func (pb *propertiesBuilder) text(s string) {
	pb.b.WriteString(s)
	pb.col += tview.TaggedStringWidth(s)
}

func (pb *propertiesBuilder) newline() {
	pb.b.WriteByte('\n')
	pb.row++
	pb.col = 0
}

// field writes one plain, non-editable "Label: value" line — same
// column layout as infoField, which this replaces as the properties
// overlay's line-builder (infoField itself is kept for hashLines, which
// has no spans to track).
func (pb *propertiesBuilder) field(label, value string) {
	pb.text(infoField(label, value))
}

// focusTag returns the style tag/reset pair a span for field should use:
// a brighter, bolder "this one has keyboard focus" style (pb.theme's own
// FocusedBackground) if it's the one propertiesFocusIndex currently
// points at (focused — see focusedPropertyField), otherwise the plain
// "this is editable" style (pb.theme's EditableBackground) every field
// already had. Neither ever touches the foreground color, only
// background/bold, so the reset tag never needs to restore anything
// beyond what it changed. Colors are rendered as "#rrggbb" (see
// colorTag), not a color name, so a themed value always round-trips
// exactly through tview's own tag parser rather than depending on it
// recognizing the same names tcell.GetColor does.
func (pb *propertiesBuilder) focusTag(field, focused propertyField) (tag, reset string) {
	if field == focused {
		return fmt.Sprintf("[:%s:b]", colorTag(pb.theme.FocusedBackground)), "[:-:-]"
	}
	return fmt.Sprintf("[:%s]", colorTag(pb.theme.EditableBackground)), "[:-]"
}

// editableField writes one "Label: value" line with value highlighted
// (plain slategray, or focusTag's brighter style while it's the
// one keyboard focus currently points at) and recorded as a propertySpan
// under field.
func (pb *propertiesBuilder) editableField(label, value string, field, focused propertyField) {
	pb.text(fmt.Sprintf("%-13s", label+":"))
	tag, reset := pb.focusTag(field, focused)
	pb.tag(tag)
	start := pb.col
	pb.text(value)
	end := pb.col
	pb.tag(reset)
	pb.spans = append(pb.spans, propertySpan{row: pb.row, startCol: start, endCol: end, field: field})
}

// permissionsField writes the "Permissions:" line: the one-character
// file-type prefix (not editable — changing what kind of thing a path is
// isn't a permission edit), then the 9 rwx characters, each its own
// propertySpan (for per-character click routing — see
// Root.togglePermBit) styled individually via focusTag so the one
// keyboard focus currently points at (if any) stands out from the rest,
// then the octal form — itself its own editable propertySpan
// (fieldPermOctal), so all 9 bits can be set at once by typing a value
// like "755" instead of only one at a time by clicking, per the user's
// own request.
func (pb *propertiesBuilder) permissionsField(mode os.FileMode, focused propertyField) {
	pb.text(fmt.Sprintf("%-13s", "Permissions:"))
	pb.text(string(permTypeChar(mode)))

	bitFields := [9]propertyField{
		fieldPermOwnerRead, fieldPermOwnerWrite, fieldPermOwnerExec,
		fieldPermGroupRead, fieldPermGroupWrite, fieldPermGroupExec,
		fieldPermOtherRead, fieldPermOtherWrite, fieldPermOtherExec,
	}
	const rwx = "rwxrwxrwx"

	for i, f := range bitFields {
		tag, reset := pb.focusTag(f, focused)
		pb.tag(tag)
		start := pb.col
		ch := byte('-')
		if mode&(1<<uint(9-1-i)) != 0 {
			ch = rwx[i]
		}
		pb.text(string(ch))
		pb.tag(reset)
		pb.spans = append(pb.spans, propertySpan{row: pb.row, startCol: start, endCol: pb.col, field: f})
	}

	pb.text(" (")
	octalTag, octalReset := pb.focusTag(fieldPermOctal, focused)
	pb.tag(octalTag)
	octalStart := pb.col
	pb.text(fmt.Sprintf("%04o", mode.Perm()))
	octalEnd := pb.col
	pb.tag(octalReset)
	pb.text(")")
	pb.spans = append(pb.spans, propertySpan{row: pb.row, startCol: octalStart, endCol: octalEnd, field: fieldPermOctal})
}

// mtimeField writes the "Modified:" line with the date and time halves
// as two independently highlighted, independently clickable spans (see
// fieldMtimeDate/fieldMtimeTime) — edited separately, per the user's own
// request, even though both stage into the same time.Time together. Each
// half is styled via focusTag, so whichever one (if either) keyboard
// focus currently points at stands out from the other.
func (pb *propertiesBuilder) mtimeField(t time.Time, focused propertyField) {
	pb.text(fmt.Sprintf("%-13s", "Modified:"))

	dateTag, dateReset := pb.focusTag(fieldMtimeDate, focused)
	pb.tag(dateTag)
	dateStart := pb.col
	pb.text(t.Format("2006-01-02"))
	dateEnd := pb.col
	pb.tag(dateReset)

	pb.text(" ")

	timeTag, timeReset := pb.focusTag(fieldMtimeTime, focused)
	pb.tag(timeTag)
	timeStart := pb.col
	pb.text(t.Format("15:04:05"))
	timeEnd := pb.col
	pb.tag(timeReset)

	pb.spans = append(pb.spans,
		propertySpan{row: pb.row, startCol: dateStart, endCol: dateEnd, field: fieldMtimeDate},
		propertySpan{row: pb.row, startCol: timeStart, endCol: timeEnd, field: fieldMtimeTime},
	)
}

// newPropertiesView creates the Properties overlay: a read-only text
// display (propertiesText) that individual fields' values are drawn on
// top of the highlighted spans of (see propertiesBuilder), a single
// reusable inline input (propertiesEditField, shown only while text-
// editing a field, repositioned over whichever one that is — the same
// "one shared field, repositioned per use" approach Root.rename/
// Root.prompt already use), and a Cancel/Save button row
// (propertiesButtons) visible from the moment Properties opens (see
// openProperties) — not gated behind any edit having started, per the
// user's own request. All three live in their own tview.Pages
// (r.properties) rather than Root's top-level one: unlike Root's
// overlays, which are mutually exclusive, propertiesText must stay
// visible under propertiesEditField/propertiesButtons, and tview.Pages
// happily shows multiple of its own pages at once as long as nothing
// tells it not to — Root's showOverlay/hideOverlay only enforce
// single-overlay-at-a-time as their own policy on top of that, not a
// limitation of Pages itself.
//
// Keyboard navigation between the fields inside propertiesText and the
// two buttons is hand-rolled (see capturePropertiesKey,
// setPropertiesFocus, movePropertiesFocus) rather than left to tview's
// own focus system, for the same reason every other click target in this
// overlay is tracked via propertySpan instead of tview's region/
// Highlight mechanism: propertiesText's fields aren't separate
// Primitives tview could cycle focus between on its own. The two buttons
// ARE real Primitives, though, and do get real keyboard focus once
// navigation reaches them (see propertiesCancelBtn/propertiesSaveBtn's
// SetExitFunc below) — their own InputHandler already does the right
// thing for Enter; SetInputCapture adds the same for Space, per the
// user's own request that either activate a focused button.
func (r *Root) newPropertiesView() *tview.Pages {
	r.propertiesText = tview.NewTextView()
	r.propertiesText.SetBorderPadding(0, 0, 1, 1)
	r.propertiesText.SetDynamicColors(true) // needed for focusTag's style tags
	r.propertiesText.SetInputCapture(r.capturePropertiesKey)
	r.propertiesText.SetMouseCapture(r.capturePropertiesMouse)

	// Colored via r.theme.FocusedBackground (see Root.applyTheme), not
	// r.theme.EditableBackground: this field is only ever shown for
	// whichever field is currently focused (see focusTag's own doc
	// comment), so it should always carry that look, not the plainer
	// "merely editable" one the span underneath no longer shows once
	// this covers it.
	r.propertiesEditField = tview.NewInputField()
	r.propertiesEditField.SetDoneFunc(r.finishPropertyEdit)

	r.propertiesButtons = r.newPropertiesButtons()

	pages := tview.NewPages()
	pages.AddPage("text", r.propertiesText, true, true)
	pages.AddPage("editfield", r.propertiesEditField, false, false)
	pages.AddPage("buttons", r.propertiesButtons, false, true)

	// Installed on pages itself, the shared ancestor of all three pages
	// above — see hashesMouseCapture's own doc comment for why that's
	// what makes a hash-section click keep working no matter which of
	// the three currently has focus. The keyboard half of this used to
	// be a bare 'h', installed here the same way for the same reason —
	// removed once Ctrl+K existed and covered it better: a global
	// Application-level shortcut (see cmd/breakthrough) already runs
	// before any of this widget-level focus routing even comes into it,
	// so it never needed a "shared ancestor" trick to keep working
	// regardless of which sub-widget had focus, unlike 'h' once did.
	pages.SetMouseCapture(r.hashesMouseCapture)

	return pages
}

// newPropertiesButtons builds the always-visible Cancel/Save row (see
// newPropertiesView) and stores the two buttons directly
// (propertiesCancelBtn/propertiesSaveBtn) — setPropertiesFocus needs to
// give either of them real keyboard focus individually, which the Flex
// container alone doesn't let it address.
func (r *Root) newPropertiesButtons() *tview.Flex {
	r.propertiesCancelBtn = tview.NewButton("Cancel").SetSelectedFunc(r.cancelPropertiesEdit)
	r.propertiesSaveBtn = tview.NewButton("Save").SetSelectedFunc(r.savePropertiesEdit)
	r.propertiesCancelBtn.SetInputCapture(spaceAlsoActivates(r.cancelPropertiesEdit))
	r.propertiesSaveBtn.SetInputCapture(spaceAlsoActivates(r.savePropertiesEdit))

	n := len(propertyFieldOrder)
	// Tab/Backtab leave the button that currently has focus (see
	// Button.InputHandler) rather than doing anything themselves — where
	// that lands is entirely up to SetExitFunc. Escape reaches the same
	// place from either button: cancelPropertiesEdit, matching Escape's
	// meaning everywhere else it's already used in this overlay (and in
	// every other overlay in this app).
	r.propertiesCancelBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.setPropertiesFocus(n + 1) // Save
		case tcell.KeyBacktab:
			r.setPropertiesFocus(n - 1) // last field
		case tcell.KeyEscape:
			r.cancelPropertiesEdit()
		}
	})
	r.propertiesSaveBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.setPropertiesFocus(0) // wrap to the first field
		case tcell.KeyBacktab:
			r.setPropertiesFocus(n) // Cancel
		case tcell.KeyEscape:
			r.cancelPropertiesEdit()
		}
	})
	// A plain mouse click also moves real tview focus to a button
	// (Button.MouseHandler's own MouseLeftDown case) without going
	// through setPropertiesFocus at all — SetFocusFunc is what keeps
	// propertiesFocusIndex (and so the rendered highlight) in sync with
	// that path too, not just the keyboard one.
	r.propertiesCancelBtn.SetFocusFunc(func() {
		r.propertiesFocusIndex = n
		r.rerenderProperties()
	})
	r.propertiesSaveBtn.SetFocusFunc(func() {
		r.propertiesFocusIndex = n + 1
		r.rerenderProperties()
	})

	flex := tview.NewFlex().SetDirection(tview.FlexColumn)
	flex.AddItem(r.propertiesCancelBtn, 0, 1, false)
	flex.AddItem(r.propertiesSaveBtn, 0, 1, false)
	return flex
}

// spaceAlsoActivates makes the space bar run action, alongside the Enter
// key handling Button.InputHandler already has built in for its own
// SetSelectedFunc — per the user's explicit request that either key
// activate a focused button.
func spaceAlsoActivates(action func()) func(event *tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyRune && event.Rune() == ' ' {
			action()
			return nil
		}
		return event
	}
}

// openProperties is the context menu's "Properties" action (still called
// "Info" internally in a few identifiers pending a fuller cleanup — the
// user-visible label changed first): it shows and, per this field's own
// rules, lets the user edit what breakthrough knows about the target —
// gathered natively via fsops.Stat rather than by shelling out to and
// parsing ls (see the Phase 1 design discussion for why generic
// command-output parsing doesn't scale here).
//
// Edits are staged, not applied live: stagedName/stagedMode/stagedMtime
// start out equal to what fsops.Stat just found, and only change as
// fields are edited; nothing touches the real file until Save (see
// savePropertiesEdit). See markPropertiesDirty for why even clicking a
// field once — not just an actual change — locks the overlay into
// "Cancel or Save to leave" mode.
func (r *Root) openProperties() {
	if err := r.loadPropertiesTarget(); err != nil {
		r.hideOverlay() // close the context menu before reporting
		r.showError(err)
		return
	}

	x, y, _, _ := r.menu.GetRect()
	r.resizeProperties(x, y)

	r.showOverlayWithRestore(propertiesPage, r.properties, r.restoreProperties)
}

// propertiesCurrentEntry is the Properties button/Ctrl+P's actual
// action — the keyboard/status-bar equivalent of the context menu's
// "Properties" (see openProperties above), targeting whichever entry
// the table's cursor is currently on instead of a right-clicked one.
// r.menu never having been shown yet (no right-click this session) just
// means openProperties' own GetRect() reads as its zero value —
// resizeProperties' clampToPanel call still turns that into a valid,
// on-screen position, the same as it would any other out-of-range x/y.
func (r *Root) propertiesCurrentEntry() {
	row, path, ok := r.panel.CurrentRowPath()
	if !ok {
		return
	}
	r.target = path
	r.targetRow = row
	r.openProperties()
}

// PropertiesShortcut is Ctrl+P's global action — see cmd/breakthrough
// and acceptsGlobalShortcut for why it checks its own precondition
// first, the same as Ctrl+E/F2/Ctrl+G/Ctrl+O/Ctrl+F/Ctrl+R. Unlike
// those six, Ctrl+P also needs cmd/breakthrough's own dispatch-level
// AcceptsGlobalShortcut check before it's even called, since bashLine's
// own captureBashLineKey binds Ctrl+P to command-history recall.
func (r *Root) PropertiesShortcut() {
	if r.acceptsGlobalShortcut() {
		r.propertiesCurrentEntry()
	}
}

// loadPropertiesTarget does openProperties' own state-population half
// (stat r.target, stage every editable field, render the text), kept
// separate from its own positioning/overlay logic below.
func (r *Root) loadPropertiesTarget() error {
	info, err := fsops.Stat(r.target)
	if err != nil {
		return err
	}

	r.cancelHashComputation() // a previous target's own hash computation, if still running, is now stale
	r.propertiesTarget = r.target
	r.propertiesStat = info
	// Adopts Details' own result immediately if it already has one for
	// this exact file (see detailsHashesFor's own doc comment) — nil
	// otherwise, same as always. Per the user's own explicit request:
	// opening Properties on a file Details already hashed shouldn't
	// show a fresh "press Ctrl+K" hint (or need recomputing) just
	// because Properties itself is only now opening on it.
	r.propertiesHashes = r.detailsHashesFor(r.propertiesTarget)
	r.propertiesDirty = false
	r.propertiesFocusIndex = -1 // nothing focused yet — see setPropertiesFocus
	r.stagedName = info.Name
	r.stagedMode = info.Mode.Perm()
	r.stagedMtime = info.ModTime
	r.stagedOwner = info.Owner
	r.stagedGroup = info.Group

	r.renderProperties()
	r.properties.HidePage("editfield")
	return nil
}

// renderProperties rebuilds the Properties overlay's text from
// propertiesStat (the read-only fields) and the staged values (Name,
// Permissions, Modified — see openProperties), plus a hash section for
// anything that isn't a directory (or resolves to one — see isDirish): a
// hint to compute them until propertiesHashes is set, then the digests
// themselves. Called after every edit (a permission toggle, finishing a
// text field, computing hashes, a focus change) to keep the display,
// propertySpans, and the focus highlight all in sync with current state.
func (r *Root) renderProperties() {
	pb := &propertiesBuilder{theme: r.theme}
	focused := r.focusedPropertyField()

	pb.editableField("Name", r.stagedName, fieldName, focused)
	pb.newline()
	pb.field("Type", classifyKind(r.propertiesStat))
	pb.newline()
	pb.permissionsField(r.stagedMode, focused)
	if r.propertiesStat.Nlink > 1 && !r.propertiesStat.IsDir {
		// Not shown for directories: every directory has Nlink >= 2
		// trivially (its own "." entry, plus each subdirectory's ".."),
		// which isn't the "this content also exists under another name"
		// signal that's actually worth flagging.
		pb.newline()
		pb.field("Links", fmt.Sprintf("%d (shared with other names)", r.propertiesStat.Nlink))
	}
	pb.newline()
	pb.editableField("Owner", r.stagedOwner, fieldOwner, focused)
	pb.newline()
	pb.editableField("Group", r.stagedGroup, fieldGroup, focused)
	pb.newline()
	pb.field("Size", sizeWithBytes(r.propertiesStat.Size))
	pb.newline()
	pb.mtimeField(r.stagedMtime, focused)
	pb.newline()
	pb.field("Path", r.propertiesStat.Path)
	if r.propertiesStat.IsSymlink && r.propertiesStat.LinkTarget != "" {
		pb.newline()
		pb.field("Link target", r.propertiesStat.LinkTarget)
	}
	if r.propertiesStat.MountPoint {
		pb.newline()
		pb.field("Mount point", "yes")
	}
	if r.propertiesStat.IsSymlink {
		if chain := fsops.ResolveChain(r.propertiesTarget); len(chain.Hops) > 1 {
			// Only shown once there's an actual chain (more than one
			// hop) — a simple single-target symlink is already fully
			// described by "Link target" and "Type" above.
			pb.newline()
			pb.field("Chain", formatChain(chain))
		}
	}

	text := pb.b.String()
	if !isDirish(r.propertiesStat) {
		r.hashSectionRow = pb.row + 2 // +1 past the fields' own last line, +1 for the blank separator
		switch {
		case r.hashInProgress:
			text += "\n\n" + hashAnimationFrames[r.hashAnimFrame%len(hashAnimationFrames)] + " Computing hashes" + hashProgressSuffix(r.hashBytesRead.Load(), r.propertiesStat.Size)
		default:
			text += "\n\n" + hashLines(r.propertiesHashes, "Press Ctrl+K or click here to compute SHA-256 / SHA-1 / MD5 / SHA-512 / BLAKE2b-512", propertiesHashFieldWidth)
		}
	}

	r.propertySpans = pb.spans
	r.propertiesText.SetText(text)
}

// resizeProperties recomputes the overlay's rect from its current text
// (see renderProperties), keeping (x, y) as given but not necessarily as
// where it last was — openProperties passes the context menu's own
// position (first open), everything else passes wherever the overlay
// currently sits (a resize after an edit, not a reposition). One line is
// reserved for the Cancel/Save row, which is visible for as long as
// Properties itself is (see newPropertiesView) — leaving that row out of
// the reserved height would leave it with nothing of its own to sit on,
// overlapping propertiesText's own last line instead.
func (r *Root) resizeProperties(x, y int) {
	width, height := textSize(r.propertiesText.GetText(true))
	height++ // reserved button row
	x, y, width, height = r.clampToPanel(x, y, width, height)

	r.properties.SetRect(x, y, width, height)
	r.propertiesButtons.SetRect(x, y+height-1, width, 1)
}

// rerenderProperties re-runs renderProperties/resizeProperties in place
// — the common tail of every edit action (permission toggle, finishing a
// text field, computing hashes).
func (r *Root) rerenderProperties() {
	x, y, _, _ := r.properties.GetRect()
	r.renderProperties()
	r.resizeProperties(x, y)
}

// markPropertiesDirty is what the very first interaction with any field
// does — a click, not necessarily a completed change. It's what (see
// captureOutsideClick) switches an outside click from "close the
// overlay" to "do nothing": once something's been touched, Cancel or
// Save is the only way out, so a stray click can't silently discard or
// lose track of an in-progress edit.
func (r *Root) markPropertiesDirty() {
	r.propertiesDirty = true
}

// cancelPropertiesEdit is the Cancel button: closes the overlay without
// applying anything — safe to just hide, since nothing staged was ever
// written to the real file (see savePropertiesEdit, the only place any
// of this actually touches disk).
func (r *Root) cancelPropertiesEdit() {
	r.cancelHashComputation()
	r.hideOverlay()
}

// savePropertiesEdit is the Save button: applies whichever of
// Name/Permissions/Modified/Owner/Group actually changed, in that order,
// stopping at the first failure rather than trying the rest — a failure
// partway through leaves whatever already succeeded applied and whatever
// didn't get to run undone; reopening Properties and redoing the
// remaining edits is what recovering from that looks like for now, not
// an automatic retry or rollback.
func (r *Root) savePropertiesEdit() {
	r.cancelHashComputation()
	target := r.propertiesTarget
	var firstErr error

	if r.stagedName != r.propertiesStat.Name {
		newPath, err := fsops.Rename(target, r.stagedName)
		if err != nil {
			firstErr = err
		} else {
			target = newPath
		}
	}
	if firstErr == nil && r.stagedMode != r.propertiesStat.Mode.Perm() {
		if err := fsops.Chmod(target, r.stagedMode); err != nil {
			firstErr = err
		}
	}
	// Compared at whole-second precision, not with Equal directly: the
	// Modified date/time fields (see parseDate/parseTime) only ever
	// show and edit whole seconds, zeroing out whatever sub-second
	// precision propertiesStat.ModTime originally had the moment either
	// half is committed — including by just tabbing through it without
	// actually typing a new value. Comparing at full precision would
	// treat that precision loss alone as a real change and write it,
	// even though nothing the user could see or intentionally edit
	// here actually differs.
	if firstErr == nil && !r.stagedMtime.Truncate(time.Second).Equal(r.propertiesStat.ModTime.Truncate(time.Second)) {
		if err := fsops.SetModTime(target, r.stagedMtime); err != nil {
			firstErr = err
		}
	}
	if firstErr == nil && (r.stagedOwner != r.propertiesStat.Owner || r.stagedGroup != r.propertiesStat.Group) {
		// -1 for whichever half didn't change, matching os.Chown's own
		// "leave this one unchanged" convention (see fsops.ParseOwnerGroup,
		// which the standalone chown action's text fallback already relies
		// on the same way).
		uid, gid := -1, -1
		if r.stagedOwner != r.propertiesStat.Owner {
			if id, err := fsops.ResolveUID(r.stagedOwner); err != nil {
				firstErr = err
			} else {
				uid = id
			}
		}
		if firstErr == nil && r.stagedGroup != r.propertiesStat.Group {
			if id, err := fsops.ResolveGID(r.stagedGroup); err != nil {
				firstErr = err
			} else {
				gid = id
			}
		}
		if firstErr == nil {
			if err := fsops.Chown(target, uid, gid); err != nil {
				firstErr = err
			}
		}
	}

	// Per the user's own explicit request: Details, if it's showing the
	// very same file Properties was just editing, reflects whatever
	// actually landed on disk immediately — not stale info until the
	// user happens to navigate away and back. Needed regardless of
	// firstErr: even a save that failed partway through (see this
	// function's own doc comment on why that isn't rolled back) may
	// still have really renamed/rechmod'd/etc. the file before hitting
	// whatever failed next, and target already reflects that. Checked
	// against propertiesTarget, not the possibly-just-renamed target
	// itself: that's the file Details would have been showing
	// beforehand, under its original, pre-edit path.
	//
	// Deliberately not left to the panel's own reload just below to
	// trigger this on its own: p.load only repositions the table's
	// selection (see focusRow, which is what actually fires
	// SetSelectionChangedFunc) when moving to a genuinely different
	// directory — a same-directory reload, exactly this case, rebuilds
	// every row's data in place without reselecting anything, so
	// nothing would tell Details a rename even happened.
	if r.detailsSidebarVisible && r.detailsTarget == r.propertiesTarget {
		r.loadDetailsTarget(target)
	}

	r.hideOverlay()
	r.showError(r.panel.load(r.panel.path))
	if firstErr != nil {
		r.showError(firstErr)
	}
}

// togglePermBit flips one permission bit in stagedMode (see
// permFieldBit), marks the overlay dirty, and re-renders — the click *is*
// the edit here, unlike Name/Modified, which need actual typing. Marks
// dirty itself rather than relying on activatePropertyField (its only
// real caller) to have done it first, so it stays correct on its own —
// including for a test calling it directly, bypassing the click path
// entirely.
func (r *Root) togglePermBit(field propertyField) {
	bit, ok := permFieldBit[field]
	if !ok {
		return
	}
	r.stagedMode ^= bit
	r.markPropertiesDirty()
	r.rerenderProperties()
}

// setPermBit explicitly sets (on) or clears (!on) one permission bit in
// stagedMode — the "type the matching letter to turn it on, Delete or
// '-' to turn it off" alternative to togglePermBit's "flip whatever it
// currently is" (see capturePropertiesKey), per the user's own request.
func (r *Root) setPermBit(field propertyField, on bool) {
	bit, ok := permFieldBit[field]
	if !ok {
		return
	}
	if on {
		r.stagedMode |= bit
	} else {
		r.stagedMode &^= bit
	}
	r.markPropertiesDirty()
	r.rerenderProperties()
}

// activatePropertyField is what clicking any propertySpan does — and,
// via activateFocusedPropertyStop, what pressing Enter on one via
// keyboard navigation does too: a permission bit toggles immediately
// (see togglePermBit); Name/Modified instead position and show the
// shared inline edit field over that exact span, pre-filled with the
// current staged value. Either way, this is the first interaction with a
// field, so it also marks the overlay dirty (see markPropertiesDirty) —
// even a permission click that gets toggled right back counts, matching
// "as soon as you click one to edit" as literally as it says — and moves
// propertiesFocusIndex to this field, so Tab afterwards continues
// naturally from wherever was just clicked rather than wherever keyboard
// navigation last left off.
func (r *Root) activatePropertyField(span propertySpan) {
	r.markPropertiesDirty()
	if idx, ok := propertyFieldIndex(span.field); ok {
		r.propertiesFocusIndex = idx
	}
	r.rerenderProperties()

	if _, ok := permFieldBit[span.field]; ok {
		r.togglePermBit(span.field)
		return
	}

	switch span.field {
	case fieldOwner:
		r.openOwnerGroupPicker(pickUser, r.propertiesStat.UID, r.propertyFieldPosition(span), func(name string, _ int) {
			r.stagedOwner = name
			r.resumeProperties()
		}, r.resumeProperties, func() {
			r.activateInlineTextField(span, r.stagedOwner, 24)
		})
		return
	case fieldGroup:
		r.openOwnerGroupPicker(pickGroup, r.propertiesStat.GID, r.propertyFieldPosition(span), func(name string, _ int) {
			r.stagedGroup = name
			r.resumeProperties()
		}, r.resumeProperties, func() {
			r.activateInlineTextField(span, r.stagedGroup, 24)
		})
		return
	}

	var prefill string
	var minWidth int
	switch span.field {
	case fieldName:
		prefill = r.stagedName
		minWidth = 24 // room to type a longer name than the current one
	case fieldPermOctal:
		// 4 digits, matching exactly what's on screen (permissionsField's
		// own "%04o") — typing "644" still works fine too, ParseMode
		// doesn't care about a leading zero either way, but the field you
		// clicked shouldn't visibly change digit count out from under you
		// the moment you start editing it.
		prefill = fmt.Sprintf("%04o", r.stagedMode.Perm())
		minWidth = 4
	case fieldMtimeDate:
		prefill = r.stagedMtime.Format("2006-01-02")
		minWidth = 10
	case fieldMtimeTime:
		prefill = r.stagedMtime.Format("15:04:05")
		minWidth = 8
	default:
		return
	}
	r.activateInlineTextField(span, prefill, minWidth)
}

// resumeProperties re-renders the Properties overlay once the owner/group
// picker (a separate overlay layered on top of it — see
// openOwnerGroupPicker/pushOverlay) has closed, whether that was a pick
// or a cancel, so a picked name shows up immediately. Properties itself
// was never hidden while the picker was up — closing just the picker
// (hideOverlay's own pop) already restored its focus (see
// restoreProperties, its frame's own restore callback) before this even
// runs — so there's nothing left to do here beyond reflecting whatever
// changed. propertiesDirty and every staged value survive the round trip
// untouched regardless: they're plain Root fields, not tied to whether
// the picker happened to be open.
func (r *Root) resumeProperties() {
	r.rerenderProperties()
}

// restoreProperties re-applies whichever concrete Properties sub-widget
// should hold real keyboard focus for propertiesFocusIndex's current
// value. Properties keeps several sub-widgets simultaneously visible
// (see newPropertiesView), so the overlay stack's own generic "just
// refocus whatever widget this frame was pushed with" isn't precise
// enough on its own; this is what showOverlayWithRestore is given as
// Properties' own frame's restore callback (see openProperties), run
// whenever Properties becomes the topmost overlay again after the
// owner/group picker — currently the only thing that ever layers on top
// of it — closes.
func (r *Root) restoreProperties() {
	r.setPropertiesFocus(r.propertiesFocusIndex)
}

// focusedPropertyField returns which field, if any, propertiesFocusIndex
// currently points at (see setPropertiesFocus) — fieldNone once the
// index has moved past the last field stop onto Cancel/Save, or before
// Tab has ever been pressed (index -1, Properties' state right after
// opening — see openProperties).
func (r *Root) focusedPropertyField() propertyField {
	if r.propertiesFocusIndex < 0 || r.propertiesFocusIndex >= len(propertyFieldOrder) {
		return fieldNone
	}
	return propertyFieldOrder[r.propertiesFocusIndex]
}

// propertySpanForField returns the current propertySpan for field, if
// rendered — the keyboard-navigation counterpart to a mouse click
// already having a propertySpan in hand (see
// activateFocusedPropertyStop, capturePropertiesMouse's own click path).
func (r *Root) propertySpanForField(field propertyField) (propertySpan, bool) {
	for _, s := range r.propertySpans {
		if s.field == field {
			return s, true
		}
	}
	return propertySpan{}, false
}

// isAutoEditField reports whether landing keyboard focus on field (see
// setPropertiesFocus) should open its inline editor immediately, with no
// separate Enter/Space needed first — Name, the octal permission value,
// and the Modified date/time halves: plain text entry, where "select the
// field" and "start typing" are the same action as far as the user's
// concerned, per their own request. The individual permission bits (a
// toggle, not text — see capturePropertiesKey's letter/Delete/Space
// handling) and Owner/Group (which open the heavier picker overlay, not
// appropriate to trigger just by tabbing past on the way to some other
// field) are deliberately excluded — those still wait for an explicit
// key or click.
func isAutoEditField(field propertyField) bool {
	switch field {
	case fieldName, fieldPermOctal, fieldMtimeDate, fieldMtimeTime:
		return true
	default:
		return false
	}
}

// setPropertiesFocus moves keyboard-navigation focus to stop idx — a
// field span (0..len(propertyFieldOrder)-1), the Cancel button
// (len(propertyFieldOrder)), or the Save button
// (len(propertyFieldOrder)+1) — re-rendering so the highlighted field
// (if any) matches, and giving the concrete widget that stop actually
// lives on real keyboard focus: propertiesText for a field (see
// capturePropertiesKey's Tab/Backtab/Enter handling), or the button
// itself, whose own InputHandler and SetExitFunc (see
// newPropertiesButtons) take over navigation from there. Landing on a
// field that opens automatically (see isAutoEditField) immediately does
// so, folding "select the field" and "start editing it" into the single
// Tab press that got here. Also serves as the owner/group picker's own
// restore callback (see showOverlayWithRestore/restoreProperties):
// reapplying the current index is exactly what re-entering Properties
// after the picker closes needs, regardless of why setPropertiesFocus is
// running.
func (r *Root) setPropertiesFocus(idx int) {
	r.propertiesFocusIndex = idx
	r.rerenderProperties()

	n := len(propertyFieldOrder)
	switch idx {
	case n:
		r.app.SetFocus(r.propertiesCancelBtn)
	case n + 1:
		r.app.SetFocus(r.propertiesSaveBtn)
	default:
		r.app.SetFocus(r.propertiesText)
		if idx >= 0 && idx < n && isAutoEditField(propertyFieldOrder[idx]) {
			r.activateFocusedPropertyStop()
		}
	}
}

// movePropertiesFocus advances propertiesFocusIndex by delta (+1 for
// Tab, -1 for Backtab) among the field stops — only ever called while a
// field stop (or nothing, idx < 0) currently holds focus, via
// capturePropertiesKey; Cancel/Save, once reached, hand navigation off
// to the buttons' own SetExitFunc (see newPropertiesButtons), which
// calls setPropertiesFocus directly the same way once Tab/Backtab leaves
// them.
func (r *Root) movePropertiesFocus(delta int) {
	n := len(propertyFieldOrder)
	idx := r.propertiesFocusIndex + delta
	switch {
	case idx < 0:
		idx = n + 1 // wrap to Save
	case idx >= n:
		idx = n // Cancel
	}
	r.setPropertiesFocus(idx)
}

// activateFocusedPropertyStop is Enter's own action in
// capturePropertiesKey while a field stop has focus — the keyboard
// counterpart to clicking that same field (see activatePropertyField).
// A no-op if nothing is focused yet (idx < 0: Tab hasn't been pressed
// since Properties opened) or the field somehow isn't currently rendered
// (shouldn't happen — propertyFieldOrder and what renderProperties
// actually draws are meant to always agree).
func (r *Root) activateFocusedPropertyStop() {
	if r.propertiesFocusIndex < 0 || r.propertiesFocusIndex >= len(propertyFieldOrder) {
		return
	}
	field := propertyFieldOrder[r.propertiesFocusIndex]
	if span, ok := r.propertySpanForField(field); ok {
		r.activatePropertyField(span)
	}
}

// activateInlineTextField positions and shows the shared inline edit
// field over span, pre-filled with prefill, at least minWidth wide — the
// common tail for Name/Modified date/time, and the owner/group picker's
// own text fallback when fsops.ListUsers/ListGroups isn't available.
func (r *Root) activateInlineTextField(span propertySpan, prefill string, minWidth int) {
	r.propertiesEditTarget = span.field
	r.propertiesEditField.SetText(prefill)

	rectX, rectY, _, _ := r.propertiesText.GetInnerRect()
	width := span.endCol - span.startCol
	if width < minWidth {
		width = minWidth
	}
	r.propertiesEditField.SetRect(rectX+span.startCol, rectY+span.row, width, 1)

	r.properties.ShowPage("editfield")
	r.app.SetFocus(r.propertiesEditField)
}

// finishPropertyEdit handles Enter, Tab, and Backtab (commit) and
// Escape (discard just this field's in-progress text) in the shared
// inline edit field.
//
// Tab/Backtab commit the same as Enter — not discard, the way they used
// to (and Escape still does) — and then continue the outer field
// navigation Tab/Backtab was already asking for (see
// movePropertiesFocus): "type a value, then keep tabbing through the
// rest of the fields" only works if leaving a field via Tab keeps what
// was just typed. Before this fix, tabbing out of a field discarded it
// exactly like Escape — from the user's own perspective, the value they
// just edited appeared to silently reset the moment the field lost
// focus.
//
// Invalid date/time input is silently discarded rather than surfaced as
// an error: Root's error overlay and Properties are both single-slot
// overlays (see Root.activePage), so opening one here would replace
// Properties instead of layering over it, losing every other field's
// staged edits along with it. Leaving the field's own text on screen
// with no other feedback isn't great, but it beats that.
func (r *Root) finishPropertyEdit(key tcell.Key) {
	text := r.propertiesEditField.GetText()
	target := r.propertiesEditTarget
	r.properties.HidePage("editfield")

	if key == tcell.KeyEnter || key == tcell.KeyTab || key == tcell.KeyBacktab {
		r.applyPropertyEditText(target, text)
	}

	switch key {
	case tcell.KeyTab:
		r.movePropertiesFocus(1)
	case tcell.KeyBacktab:
		r.movePropertiesFocus(-1)
	default: // Enter or Escape: conclude editing, stay on the same field
		r.refocusPropertiesField()
	}
}

// refocusPropertiesField re-renders and returns real keyboard focus to
// propertiesText for whatever propertiesFocusIndex currently is, without
// re-triggering isAutoEditField's auto-open the way setPropertiesFocus
// would — finishPropertyEdit's own Enter/Escape case (see above) needs
// exactly this: the field just edited stays focused/highlighted, but
// its own Return is what's supposed to *conclude* that one entry, per
// the user's own request — a fresh Tab arrival auto-opening a field is
// one thing; immediately reopening the very editor Enter just closed,
// on the same keystroke, would be another.
func (r *Root) refocusPropertiesField() {
	r.rerenderProperties()
	r.app.SetFocus(r.propertiesText)
}

// applyPropertyEditText stages text as target's new value — the commit
// logic finishPropertyEdit's Enter/Tab/Backtab cases share.
func (r *Root) applyPropertyEditText(target propertyField, text string) {
	switch target {
	case fieldName:
		if text != "" {
			r.stagedName = text
		}
	case fieldPermOctal:
		// Same parser the standalone chmod menu action's prompt already
		// uses — accepts "755"-style octal, rejects setuid/setgid/sticky
		// and anything out of range. Invalid input is silently discarded,
		// the same convention as an invalid date/time (see this method's
		// own doc comment) — a value already checked by ParseMode itself,
		// so unlike Owner/Group there's no separate Save-time validation
		// step for this one.
		if mode, err := fsops.ParseMode(text); err == nil {
			r.stagedMode = mode
		}
	case fieldMtimeDate:
		if t, err := parseDate(text, r.stagedMtime); err == nil {
			r.stagedMtime = t
		}
	case fieldMtimeTime:
		if t, err := parseTime(text, r.stagedMtime); err == nil {
			r.stagedMtime = t
		}
	case fieldOwner:
		// Not resolved here — just staged as typed. Owner/Group only
		// reach this text field at all as the picker's fallback (see
		// activatePropertyField), so there's no fsops.ListUsers result to
		// validate against locally; resolving via fsops.ResolveUID and
		// reporting a real error if that fails waits for Save, the same
		// as any other invalid staged value.
		if text != "" {
			r.stagedOwner = text
		}
	case fieldGroup:
		if text != "" {
			r.stagedGroup = text
		}
	}
}

// parseDate parses s as YYYY-MM-DD, accepting 1- or 2-digit month/day
// (e.g. "2026-8-5" as well as "2026-08-05") — the leading-zero padding
// the user asked for happens on the way back out (formatted with the
// strict 2006-01-02 layout once parsed and applied — see
// propertiesBuilder.mtimeField), not by rejecting shorthand going in. The
// time-of-day comes from base, unchanged; only the date half is being
// edited here. A calendar date that doesn't exist (e.g. Feb 30) is
// rejected: time.Date normalizes out-of-range components instead of
// erroring, so the constructed time is read back and compared against
// what was asked for to catch that.
func parseDate(s string, base time.Time) (time.Time, error) {
	parts := strings.Split(strings.TrimSpace(s), "-")
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("date must be YYYY-MM-DD, got %q", s)
	}
	y, err1 := strconv.Atoi(parts[0])
	mo, err2 := strconv.Atoi(parts[1])
	d, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return time.Time{}, fmt.Errorf("date must be YYYY-MM-DD, got %q", s)
	}

	t := time.Date(y, time.Month(mo), d, base.Hour(), base.Minute(), base.Second(), 0, time.Local)
	if t.Year() != y || int(t.Month()) != mo || t.Day() != d {
		return time.Time{}, fmt.Errorf("%q is not a valid date", s)
	}
	return t, nil
}

// parseTime is parseDate's counterpart for HH:MM:SS — see its doc
// comment for the shorthand/validation/padding rules, which are the
// same. The date comes from base, unchanged. Sub-second precision is
// never part of the input, so the result always has zero nanoseconds —
// milliseconds/nanoseconds "set to 0", per the user's own spec, falls
// out of this for free rather than needing to be handled separately.
func parseTime(s string, base time.Time) (time.Time, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("time must be HH:MM:SS, got %q", s)
	}
	h, err1 := strconv.Atoi(parts[0])
	mi, err2 := strconv.Atoi(parts[1])
	sec, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return time.Time{}, fmt.Errorf("time must be HH:MM:SS, got %q", s)
	}

	t := time.Date(base.Year(), base.Month(), base.Day(), h, mi, sec, 0, time.Local)
	if t.Hour() != h || t.Minute() != mi || t.Second() != sec {
		return time.Time{}, fmt.Errorf("%q is not a valid time", s)
	}
	return t, nil
}

// isDirish reports whether info should be treated as a directory for the
// hash button's purposes (see renderProperties/computeHashes/
// capturePropertiesMouse). info.IsDir alone isn't enough: it's
// Lstat-based, so it's always false for a symlink even when the symlink
// resolves to a directory — hashing that would just fail once
// fsops.Hash follows it there anyway, so there's no point offering the
// button in the first place.
func isDirish(info fsops.Info) bool {
	return info.IsDir || (info.IsSymlink && info.LinkIsDir)
}

// formatChain renders a multi-hop symlink chain as e.g.
// "/a -> /b -> /c (file)" (or "(directory)"/"(broken)"/
// "(cycle detected)"/"(too many hops)" instead) — see ResolveChain.
func formatChain(chain fsops.LinkChain) string {
	suffix := " (file)"
	switch {
	case chain.Broken:
		suffix = " (broken)"
	case chain.Cyclic:
		suffix = " (cycle detected)"
	case chain.TooDeep:
		suffix = " (too many hops)"
	case chain.FinalIsDir:
		suffix = " (directory)"
	}
	return strings.Join(chain.Hops, " -> ") + suffix
}

// hashAnimationFrames is computeHashes' own "in progress" animation
// (see hashInProgress/renderProperties) — a dot growing into a filled
// circle, then dissolving into a larger hollow one — shown in the hash
// section while a real hash computation (which can take a few seconds
// on a large file) is still running, per the user's own request,
// rather than the hint/result line just sitting frozen with nothing
// suggesting anything is happening at all. Single-width glyphs
// throughout (see checkboxText's own ○/● for the same "this app
// already commits to UTF-8, but stays inside safely single-width
// symbols" choice), not double-width ones a CJK-unaware terminal or
// this app's own earlier column math could misjudge (see
// buildHeaderSpans' own doc comment on that class of bug elsewhere).
var hashAnimationFrames = []string{"·", "•", "●", "○", "◯"}

// hashAnimationInterval is how often computeHashes' own ticker (see
// animateHashProgress) advances hashAnimFrame — fast enough to read as
// animated, slow enough not to flicker or waste redraws on something
// purely decorative.
const hashAnimationInterval = 150 * time.Millisecond

// hashFile is fsops.Hash — a package-level var, not called directly,
// so a test can substitute a fast, deterministic fake (see
// properties_test.go) instead of hashing a real file and racing a real
// background ticker, the same override-var pattern this codebase
// already uses for searchRun/loadInitialSettings.
var hashFile = fsops.Hash

// hashProgressSuffix formats the "… NN%" fragment renderProperties
// appends to the in-progress hash line, or "…" alone if total isn't
// known/positive (an empty file, or a filesystem where Size came back
// 0) — a percentage would be meaningless there. read is clamped to
// total so a file that grows while being hashed (see Hash's own doc
// comment on that edge case) can never show more than 100%.
func hashProgressSuffix(read, total int64) string {
	if total <= 0 {
		return "…"
	}
	if read > total {
		read = total
	}
	return fmt.Sprintf("… %d%%", read*100/total)
}

// computeHashes is the Properties overlay's hash action (see hashLines
// and capturePropertiesKey/capturePropertiesMouse, its two triggers):
// hashes the entry Properties is currently showing via hashFile, on a
// background goroutine — paired with animateHashProgress's own ticker,
// so the hash section shows a moving "in progress" animation for
// however long that takes, rather than the UI just freezing or the
// hint line sitting there with no feedback — and re-renders with the
// results once it's done. A no-op if Properties isn't the open
// overlay, its target is a directory or resolves to one via isDirish
// (hashing isn't offered for those — see fsops.Hash's own doc comment
// on why), or a computation is already running (see hashInProgress) —
// pressing h or clicking again mid-computation doesn't restart it.
func (r *Root) computeHashes() {
	if r.activePage != propertiesPage || isDirish(r.propertiesStat) || r.hashInProgress {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.hashCancel = cancel
	r.hashInProgress = true
	r.hashAnimFrame = 0
	r.hashBytesRead.Store(0)
	r.rerenderProperties()

	target := r.propertiesTarget
	go r.animateHashProgress(ctx)
	go func() {
		// hashFile itself stops reading — and stops calling
		// r.hashBytesRead.Store — as soon as ctx is cancelled (see
		// fsops.Hash's own doc comment); this check only covers the
		// narrow gap between that happening and this goroutine actually
		// getting to run again, not a possibly-still-running old
		// computation. Without hashFile's own cancellation (a real bug,
		// fixed there — see progressReader.Read), a cancelled call kept
		// reading (and reporting into the same r.hashBytesRead a *new*
		// call might already have reset to 0) for as long as its own
		// read loop still happened to take, the two visibly racing over
		// what the hash section showed.
		hashes, err := hashFile(ctx, target, r.hashBytesRead.Store)
		if ctx.Err() != nil {
			return // superseded before we even got to report anything — see cancelHashComputation
		}
		r.app.QueueUpdateDraw(func() {
			if ctx.Err() != nil {
				return
			}
			r.cancelHashComputation()
			if err != nil {
				r.showError(err)
				return
			}
			// Updates propertiesHashes/re-renders itself, then mirrors
			// into Details too if that's showing the same target — see
			// propagateHashResult's own doc comment (detailssidebar.go).
			r.propagateHashResult(target, hashes)
		})
	}()
}

// animateHashProgress advances hashAnimFrame every hashAnimationInterval
// until ctx is done (see computeHashes/cancelHashComputation) — its own
// background goroutine, paired with (but independent of) computeHashes'
// own hashFile call, so the animation keeps moving smoothly regardless
// of how long the actual hashing takes.
func (r *Root) animateHashProgress(ctx context.Context) {
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
				r.hashAnimFrame++
				r.rerenderProperties()
			})
		case <-ctx.Done():
			return
		}
	}
}

// cancelHashComputation stops whatever computeHashes call is currently
// in flight, if any (see hashCancel) — called once its own result
// arrives (see computeHashes), when Properties closes via Cancel/Save
// (see cancelPropertiesEdit/savePropertiesEdit), and when it's reopened
// for a — possibly different — target (see openProperties), so a stale
// animation frame or hash result can never land on the wrong file's
// display, or keep animating after the user has moved on.
func (r *Root) cancelHashComputation() {
	if r.hashCancel != nil {
		r.hashCancel()
		r.hashCancel = nil
	}
	r.hashInProgress = false
}

// hashesMouseCapture makes a click on the hash section compute hashes
// (see computeHashes) unconditionally, regardless of which of
// Properties' several sub-widgets currently has real mouse focus —
// installed directly on r.properties (see newPropertiesView), the
// shared ancestor of propertiesText/propertiesEditField/
// propertiesButtons, so it runs before whichever of those three would
// otherwise claim the event on its own (a Box-based primitive's own
// SetMouseCapture always runs before it delegates to whatever currently
// has focus underneath it — see Box.WrapMouseHandler). Without this,
// landing keyboard focus on an auto-editing field (see isAutoEditField —
// Name, the octal permission value, either half of Modified) via Tab
// hands real focus to propertiesEditField instead of propertiesText, and
// a hash-section click (previously handled in capturePropertiesMouse)
// would never run — per the user's own explicit report and request that
// it work "jederzeit", not just before the first field is touched.
//
// The keyboard equivalent of this used to be a bare 'h', handled the
// same way, right below this function — see newPropertiesView's own
// doc comment on why Ctrl+K replaced it outright instead of needing the
// same "shared ancestor" treatment.
func (r *Root) hashesMouseCapture(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action != tview.MouseLeftClick || isDirish(r.propertiesStat) {
		return action, event
	}
	x, y := event.Position()

	// Checked first, and independently of the row math below: tview.Pages
	// gives every visible page the SAME rect as the Pages itself (see
	// newPropertiesView's own doc comment on why propertiesText,
	// propertiesEditField, and propertiesButtons all share r.properties'
	// rect) — so propertiesText.InRect is true for the Cancel/Save row
	// too, not just its own content lines. Without this check, a real
	// regression: hashSectionRow can, once the overlay is short enough,
	// numerically coincide with the button row's own screen row, and a
	// click meant for Cancel or Save gets swallowed into computeHashes
	// instead of ever reaching the button underneath it.
	if primitiveContains(r.propertiesButtons, x, y) {
		return action, event
	}
	if !r.propertiesText.InRect(x, y) {
		return action, event
	}

	_, rectY, _, _ := r.propertiesText.GetInnerRect()
	if y-rectY < r.hashSectionRow {
		return action, event
	}
	r.computeHashes()
	return tview.MouseConsumed, nil
}

// capturePropertiesKey is Properties' own keyboard-navigation dispatch,
// installed on propertiesText (see newPropertiesView) — only relevant
// while propertiesText itself has real keyboard focus, i.e. while a
// field stop (or nothing, idx < 0) currently has focus; once Tab/Backtab
// hands focus to a button, that button's own InputHandler and
// SetExitFunc (see newPropertiesButtons) take over instead, and once
// focus lands on a text field it opens immediately (see
// isAutoEditField/setPropertiesFocus), handing focus to
// propertiesEditField and its own SetDoneFunc instead.
//
// Tab/Backtab move propertiesFocusIndex (movePropertiesFocus); Enter
// activates whatever's currently focused (activateFocusedPropertyStop);
// Escape always cancels, matching what it already means everywhere else
// in this app. All four are fully consumed here rather than left to
// fall through to TextView's own default handling of them — plain
// TextView calls its DoneFunc (closing the overlay outright, discarding
// every staged edit with no way back) for all four, which is exactly the
// bug this replaces: Tab or Enter after finishing an edit used to close
// Properties instead of moving on, silently losing whatever had just
// been typed.
//
// While a permission bit has focus, three more keys act on it directly
// rather than needing Enter first, per the user's own request: Space
// toggles it, same as Enter; Delete or '-' explicitly clears it; the
// matching letter (r/w/x — see permFieldLetter) explicitly sets it.
func (r *Root) capturePropertiesKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyTab:
		r.movePropertiesFocus(1)
		return nil
	case tcell.KeyBacktab:
		r.movePropertiesFocus(-1)
		return nil
	case tcell.KeyEnter:
		r.activateFocusedPropertyStop()
		return nil
	case tcell.KeyEscape:
		r.cancelPropertiesEdit()
		return nil
	case tcell.KeyDelete:
		field := r.focusedPropertyField()
		if _, ok := permFieldBit[field]; ok {
			r.setPermBit(field, false)
			return nil
		}
	case tcell.KeyRune:
		field := r.focusedPropertyField()
		if _, ok := permFieldBit[field]; ok {
			switch {
			case event.Rune() == ' ':
				r.activateFocusedPropertyStop() // toggle, same as Enter
				return nil
			case event.Rune() == '-':
				r.setPermBit(field, false)
				return nil
			case rune(permFieldLetter[field]) == event.Rune():
				r.setPermBit(field, true)
				return nil
			}
		} else if event.Rune() == ' ' {
			r.activateFocusedPropertyStop() // toggle target aside, Space is Enter's equivalent everywhere else too (e.g. Owner/Group's picker)
			return nil
		}
	}
	return event
}

// capturePropertiesMouse routes a click within the Properties overlay's
// read-only text: a propertySpan (Name, a permission bit, or half of
// Modified) activates that field (see activatePropertyField). A hash-
// section click is handled a level up now (see hashesMouseCapture on
// r.properties itself, for the same reason capturePropertiesKey no
// longer handles 'h' — see its own doc comment), so by the time a click
// reaches here it's already past that check. A click that misses
// everything just does nothing — unlike Panel's header, there's no
// default TextView behavior here worth pre-empting.
func (r *Root) capturePropertiesMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action != tview.MouseLeftClick || !r.propertiesText.InRect(event.Position()) {
		return action, event
	}

	x, y := event.Position()
	rectX, rectY, _, _ := r.propertiesText.GetInnerRect()
	row, col := y-rectY, x-rectX

	if span, ok := r.propertySpanAt(row, col); ok {
		r.activatePropertyField(span)
		return tview.MouseConsumed, nil
	}
	return action, event
}

// propertySpanAt returns the propertySpan covering (row, col), if any.
func (r *Root) propertySpanAt(row, col int) (propertySpan, bool) {
	for _, s := range r.propertySpans {
		if s.row == row && col >= s.startCol && col < s.endCol {
			return s, true
		}
	}
	return propertySpan{}, false
}

// infoField renders one "Label: value" line in the Properties overlay's
// fixed column layout — shared by hashLines (which has no span to track,
// unlike propertiesBuilder.field/editableField) so both stay visually
// aligned.
func infoField(label, value string) string {
	return fmt.Sprintf("%-13s%s", label+":", value)
}

// wideInfoField is infoField's own two-line variant for a value too
// long to sit safely on one line at any realistic terminal width — a
// 128-character SHA-512/BLAKE2b-512 hex digest, e.g., plus the 13-column
// label prefix, needs 141 columns, wider than most real terminals.
// Splitting it into two fixed 64-character halves (indented to align
// under infoField's own value column, matching a SHA-256 digest's own
// length either half) keeps every line short enough to never actually
// need tview's own word-wrap — deliberately, not just hopefully: relying
// on the terminal happening to be wide enough was a real, observed bug
// here (see textSize's own doc comment on why it can't safely guess at
// wrapping tview itself might still do at render time — this sidesteps
// that question entirely rather than trying to answer it exactly).
func wideInfoField(label, value string) string {
	mid := len(value) / 2
	return fmt.Sprintf("%-13s%s\n%13s%s", label+":", value[:mid], "", value[mid:])
}

// wrapInfoField is wideInfoField's own shape (label on the first line,
// every following one indented to align under the first line's own
// value column), generalized to as many lines as it takes to keep every
// one of them at most maxWidth columns wide — wideInfoField's fixed
// exactly-half split only ever produces lines short enough for
// Properties' own much wider ~141-column budget (see its own doc
// comment); reused as-is for a narrower context, a single 64-character
// half can still itself be too wide to fit, and would silently wrap
// again at render time — a real, observed bug the first time this
// sidebar's own hash section tried exactly that. Value is split on raw
// byte offsets, not runes: every real caller passes a hex digest
// (ASCII-only), so a byte index is always also a valid rune boundary.
func wrapInfoField(label, value string, maxWidth int) string {
	labelCol := fmt.Sprintf("%-13s", label+":")
	indent := strings.Repeat(" ", len(labelCol))
	chunkWidth := maxWidth - len(labelCol)
	if chunkWidth < 1 {
		chunkWidth = 1 // a maxWidth this narrow can't avoid wrapping somewhere — better one char per line than an infinite loop
	}

	var lines []string
	for i := 0; i < len(value); i += chunkWidth {
		end := min(i+chunkWidth, len(value))
		prefix := indent
		if i == 0 {
			prefix = labelCol
		}
		lines = append(lines, prefix+value[i:end])
	}
	if len(lines) == 0 {
		lines = []string{labelCol}
	}
	return strings.Join(lines, "\n")
}

// classifyKind renders the Type field with more detail than a plain
// file/directory/symlink split: a symlink additionally says what it
// resolves to (or that it's broken — info.LinkBroken), and the rarer
// special files (sockets, FIFOs, devices) get their own label instead of
// falling back to "file".
func classifyKind(info fsops.Info) string {
	switch {
	case info.IsSymlink && info.LinkBroken:
		return "broken symlink"
	case info.IsSymlink && info.LinkIsDir:
		return "symlink to directory"
	case info.IsSymlink:
		return "symlink to file"
	case info.IsDir:
		return "directory"
	case info.Mode&os.ModeSocket != 0:
		return "socket"
	case info.Mode&os.ModeNamedPipe != 0:
		return "FIFO"
	case info.Mode&os.ModeDevice != 0 && info.Mode&os.ModeCharDevice != 0:
		return "character device"
	case info.Mode&os.ModeDevice != 0:
		return "block device"
	default:
		return "file"
	}
}

// propertiesHashFieldWidth is the width hashLines' own Properties call
// site passes: sized so wrapInfoField's per-line chunk comes out to 64
// characters — exactly half of a 128-character SHA-512/BLAKE2b-512
// digest, i.e. the same two-line split wideInfoField itself would
// produce, for the width Properties' own overlay actually has (see
// clampToScreen/clampToPanel) — chosen once, empirically, rather than
// derived from Properties' real (variable, terminal-dependent) width,
// since a couple of columns of slack here costs nothing.
const propertiesHashFieldWidth = 77

// hashLines renders a hash section: hint until hashes is non-nil, then
// the five digests themselves, in the user's own requested order —
// SHA-256, SHA-1, MD5 (the three the standard library already covered),
// then SHA-512 and Blake2 (BLAKE2b-512, see fsops.Hashes' own doc
// comment on where that one actually comes from) — the two of these
// long enough to need wrapInfoField instead of infoField's own single
// line (see its own doc comment).
//
// hint and width are caller-supplied, not hardcoded, because the two
// callers have very different amounts of width to lay the two long
// digests out in (see propertiesHashFieldWidth vs Details' own actual
// sidebar width) — a shared hardcoded width would be wrong for one of
// them, and once was: this sidebar's own hash section initially reused
// wideInfoField's fixed 64-character halves unconditionally, and each
// one wrapped again inside Details' own much narrower box. hint is
// theirs to supply too, even though both currently say the same thing
// (Ctrl+K, since that now triggers this in Properties as well — see
// hashesMouseCapture's own doc comment on the bare 'h' it replaced) —
// keeping it a parameter rather than folding the wording in here still
// means neither caller has to change if that ever stops being true for
// one of them.
func hashLines(hashes *fsops.Hashes, hint string, width int) string {
	if hashes == nil {
		return hint
	}
	// wrapInfoField for all five, not just the two long ones — plain
	// infoField for SHA-256/SHA-1/MD5 fits Properties' own ~141-column
	// budget just fine, but a real, observed bug found it didn't fit
	// Details' own much narrower sidebar: those three lines wrapped
	// anyway, just via tview's own uncounted auto-wrap instead of a real
	// newline this function's own caller could see and budget rows for
	// (see renderDetailsSidebar's own writeSection) — silently
	// mis-numbering everything below the hash section, the exact same
	// bug class SHA-512/BLAKE2b-512 already had fixed once. wrapInfoField
	// produces identical single-line output to infoField whenever a
	// value already fits within width (see its own doc comment), so this
	// changes nothing for Properties' own wider case.
	return strings.Join([]string{
		wrapInfoField("SHA-256", hashes.SHA256, width),
		wrapInfoField("SHA-1", hashes.SHA1, width),
		wrapInfoField("MD5", hashes.MD5, width),
		wrapInfoField("SHA-512", hashes.SHA512, width),
		wrapInfoField("BLAKE2b-512", hashes.Blake2, width),
	}, "\n")
}

// permTypeChar is permString's own file-type character, split out so
// propertiesBuilder.permissionsField can use it without also getting
// the 9 rwx characters permString bakes in (it builds and tracks those
// itself, per-character, for click routing).
func permTypeChar(mode os.FileMode) byte {
	switch {
	case mode&os.ModeDir != 0:
		return 'd'
	case mode&os.ModeSymlink != 0:
		return 'l'
	case mode&os.ModeNamedPipe != 0:
		return 'p'
	case mode&os.ModeSocket != 0:
		return 's'
	case mode&os.ModeDevice != 0 && mode&os.ModeCharDevice != 0:
		return 'c'
	case mode&os.ModeDevice != 0:
		return 'b'
	default:
		return '-'
	}
}

// permString renders mode roughly the way `ls -l` does: a one-character
// file type (see permTypeChar) followed by the nine rwx permission
// characters. Unlike ls, it doesn't yet render setuid/setgid/sticky as
// the s/S/t/T variants in the execute-bit position — a known
// simplification.
func permString(mode os.FileMode) string {
	const rwx = "rwxrwxrwx"
	perm := mode.Perm()
	buf := make([]byte, 0, 10)
	buf = append(buf, permTypeChar(mode))
	for i, c := range rwx {
		if perm&(1<<uint(9-1-i)) != 0 {
			buf = append(buf, byte(c))
		} else {
			buf = append(buf, '-')
		}
	}
	return string(buf)
}

// humanSize renders size the way `ls -h` does: 1024-based, one decimal
// once it's above the smallest unit (e.g. "4.0K", "1.2M").
func humanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%dB", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", float64(size)/float64(div), "KMGTPE"[exp])
}

// sizeWithBytes renders size as its human-readable form (see humanSize)
// followed by the exact byte count, e.g. "2.1K (2184 bytes)" — the
// shorthand's rounding hides the kind of precision that matters when
// comparing two similarly-sized files. Below 1024 bytes, humanSize is
// already exact (e.g. "512B"), so there's nothing to add.
func sizeWithBytes(size int64) string {
	human := humanSize(size)
	if size < 1024 {
		return human
	}
	return fmt.Sprintf("%s (%d bytes)", human, size)
}

// textSize returns the width (the longest line, plus 1-char left/right
// padding — matching listSize) and height (a plain newline count) of a
// block of text, for sizing a no-border overlay to fit it exactly.
// Style tags (see propertiesBuilder) would throw this off if counted,
// but GetText(true) — every caller's source for the text passed in
// here — already strips them.
//
// Deliberately does not try to account for tview's own word-wrap
// (clampToPanel can still shrink the width this reports if the
// terminal itself is narrow) — a real, observed bug once did exactly
// that: a line too wide to fit wrapped at render time into a second
// row this had no way to know to budget for, silently pushing whatever
// came after it (BLAKE2b-512's own line, once SHA-512 grew this text
// past every terminal's width) below the overlay's own bottom edge,
// invisible even though it was still really there. The actual fix
// isn't here — it's the caller's own responsibility to never hand this
// a line long enough to need wrapping in the first place (see
// wideInfoField), which is simpler and more predictable than this
// function trying to reverse-engineer wherever tview would decide to
// break a line at whatever width it ends up actually getting.
func textSize(text string) (width, height int) {
	lines := strings.Split(text, "\n")
	height = len(lines)
	for _, l := range lines {
		if w := len([]rune(l)); w > width {
			width = w
		}
	}
	return width + 2, height
}
