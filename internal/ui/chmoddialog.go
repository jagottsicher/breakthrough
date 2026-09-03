package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

const chmodPage = "chmod"

// chmodField identifies one editable/clickable region within the Chmod
// dialog (see chmodSpan/chmodBuilder) — the same idea as propertyField
// in properties.go, just for this dialog's own, much smaller field set.
type chmodField int

const (
	chmodFieldNone chmodField = iota

	// Permissions' own 9 individual rwx bits — mirrors
	// fieldPermOwnerRead etc. in properties.go exactly, one-for-one,
	// just toggling stagedChmodMode instead of Properties' own
	// stagedMode (see chmodPermFieldBit/chmodModeBitFields).
	chmodFieldModeOwnerRead
	chmodFieldModeOwnerWrite
	chmodFieldModeOwnerExec
	chmodFieldModeGroupRead
	chmodFieldModeGroupWrite
	chmodFieldModeGroupExec
	chmodFieldModeOtherRead
	chmodFieldModeOtherWrite
	chmodFieldModeOtherExec

	chmodFieldMode          // the equivalent octal value, applied directly to every target
	chmodFieldRecursiveDirs // toggle: also recurse into every subdirectory (fsops.ChmodDirsRecursive)

	// chmodFieldFilesEnable is the "also touch files inside" checkbox —
	// deliberately not itself called/labeled "recursive" (see its own
	// render site in renderChmodDialog): recursion isn't a separate
	// choice here, there never was a "just this folder's own files, not
	// nested ones" option, so the checkbox's own job is purely whether
	// files get touched at all. A plain, non-interactive "recursive"
	// word after its own value is what explains what checking it
	// actually does (every file inside, not just the folder's own
	// immediate children) — see renderChmodDialog. Files' own 9
	// individual rwx bits and octal value
	// (chmodFilesModeBitFields/chmodFieldFilesMode below) only render —
	// and so only exist as field stops at all — once this is checked;
	// see chmodFieldOrder/permissionBitsAndOctal.
	chmodFieldFilesEnable

	// Files' own mirror of Permissions' own bit block above, toggling
	// stagedChmodFilesMode instead (see chmodFilesModeBitFields).
	chmodFieldFilesOwnerRead
	chmodFieldFilesOwnerWrite
	chmodFieldFilesOwnerExec
	chmodFieldFilesGroupRead
	chmodFieldFilesGroupWrite
	chmodFieldFilesGroupExec
	chmodFieldFilesOtherRead
	chmodFieldFilesOtherWrite
	chmodFieldFilesOtherExec

	chmodFieldFilesMode // the equivalent octal value for files
)

// chmodLabelWidth is how wide every row's own leading label column is —
// "Directory:", the "○ Files:" checkbox+label combo, and the
// "Permissions" column heading above them all pad/align to this same
// column, so the actual permission values (bits + octal) all start in
// one straight vertical line regardless of which row they're on.
const chmodLabelWidth = 13

// chmodModeBitFields/chmodFilesModeBitFields are the 9 individual rwx
// bit fields for the Permissions and Files rows respectively, in
// owner/group/other × read/write/exec order — matching
// os.FileMode.Perm()'s own bit order, the same as properties.go's own
// bitFields local variable in permissionsField.
var chmodModeBitFields = [9]chmodField{
	chmodFieldModeOwnerRead, chmodFieldModeOwnerWrite, chmodFieldModeOwnerExec,
	chmodFieldModeGroupRead, chmodFieldModeGroupWrite, chmodFieldModeGroupExec,
	chmodFieldModeOtherRead, chmodFieldModeOtherWrite, chmodFieldModeOtherExec,
}

var chmodFilesModeBitFields = [9]chmodField{
	chmodFieldFilesOwnerRead, chmodFieldFilesOwnerWrite, chmodFieldFilesOwnerExec,
	chmodFieldFilesGroupRead, chmodFieldFilesGroupWrite, chmodFieldFilesGroupExec,
	chmodFieldFilesOtherRead, chmodFieldFilesOtherWrite, chmodFieldFilesOtherExec,
}

// chmodPermFieldBit maps each individual rwx bit field (Permissions' or
// Files' own set alike — the bit values themselves don't depend on
// which of the two a field belongs to) to the bit it toggles — the same
// 9-value table properties.go's own permFieldBit already has, just
// covering both of this dialog's own bit sets in one map since the
// values are identical either way.
var chmodPermFieldBit = map[chmodField]os.FileMode{
	chmodFieldModeOwnerRead:  0o400,
	chmodFieldModeOwnerWrite: 0o200,
	chmodFieldModeOwnerExec:  0o100,
	chmodFieldModeGroupRead:  0o040,
	chmodFieldModeGroupWrite: 0o020,
	chmodFieldModeGroupExec:  0o010,
	chmodFieldModeOtherRead:  0o004,
	chmodFieldModeOtherWrite: 0o002,
	chmodFieldModeOtherExec:  0o001,

	chmodFieldFilesOwnerRead:  0o400,
	chmodFieldFilesOwnerWrite: 0o200,
	chmodFieldFilesOwnerExec:  0o100,
	chmodFieldFilesGroupRead:  0o040,
	chmodFieldFilesGroupWrite: 0o020,
	chmodFieldFilesGroupExec:  0o010,
	chmodFieldFilesOtherRead:  0o004,
	chmodFieldFilesOtherWrite: 0o002,
	chmodFieldFilesOtherExec:  0o001,
}

// chmodPermFieldLetter maps each individual rwx bit field to the rwx
// letter that explicitly turns it on (see captureChmodKey) — the same
// role permFieldLetter has in properties.go, and the same three ways in
// per the user's own explicit request that Chmod's own permission entry
// work exactly like Properties' already does: the matching letter sets
// the bit on; Delete or '-' sets it off; Space/Enter toggle whatever it
// currently is; a plain click does too.
var chmodPermFieldLetter = map[chmodField]byte{
	chmodFieldModeOwnerRead: 'r', chmodFieldModeGroupRead: 'r', chmodFieldModeOtherRead: 'r',
	chmodFieldModeOwnerWrite: 'w', chmodFieldModeGroupWrite: 'w', chmodFieldModeOtherWrite: 'w',
	chmodFieldModeOwnerExec: 'x', chmodFieldModeGroupExec: 'x', chmodFieldModeOtherExec: 'x',

	chmodFieldFilesOwnerRead: 'r', chmodFieldFilesGroupRead: 'r', chmodFieldFilesOtherRead: 'r',
	chmodFieldFilesOwnerWrite: 'w', chmodFieldFilesGroupWrite: 'w', chmodFieldFilesOtherWrite: 'w',
	chmodFieldFilesOwnerExec: 'x', chmodFieldFilesGroupExec: 'x', chmodFieldFilesOtherExec: 'x',
}

// chmodBitTarget returns a pointer to whichever staged mode field's own
// bit field is being edited — stagedChmodMode for a Permissions-row
// field, stagedChmodFilesMode for a Files-row one — and whether field
// is a permission-bit field at all. The pointer lets toggleChmodBit/
// setChmodBit share one implementation for both rows instead of two
// near-identical copies differing only in which staged field they
// touch.
func (r *Root) chmodBitTarget(field chmodField) (target *os.FileMode, ok bool) {
	if _, isBit := chmodPermFieldBit[field]; !isBit {
		return nil, false
	}
	switch field {
	case chmodFieldFilesOwnerRead, chmodFieldFilesOwnerWrite, chmodFieldFilesOwnerExec,
		chmodFieldFilesGroupRead, chmodFieldFilesGroupWrite, chmodFieldFilesGroupExec,
		chmodFieldFilesOtherRead, chmodFieldFilesOtherWrite, chmodFieldFilesOtherExec:
		return &r.stagedChmodFilesMode, true
	default:
		return &r.stagedChmodMode, true
	}
}

// toggleChmodBit flips one permission bit — the click itself *is* the
// edit here, unlike the octal fields, which need actual typing. The
// same role togglePermBit has in properties.go.
func (r *Root) toggleChmodBit(field chmodField) {
	target, ok := r.chmodBitTarget(field)
	if !ok {
		return
	}
	*target ^= chmodPermFieldBit[field]
	r.rerenderChmodDialog()
}

// setChmodBit explicitly sets (on) or clears (!on) one permission bit —
// the "type the matching letter to turn it on, Delete or '-' to turn it
// off" alternative to toggleChmodBit's "flip whatever it currently is"
// (see captureChmodKey). The same role setPermBit has in properties.go.
func (r *Root) setChmodBit(field chmodField, on bool) {
	target, ok := r.chmodBitTarget(field)
	if !ok {
		return
	}
	if on {
		*target |= chmodPermFieldBit[field]
	} else {
		*target &^= chmodPermFieldBit[field]
	}
	r.rerenderChmodDialog()
}

// chmodFieldOrder returns every field stop for the dialog's current
// session, in the same top-to-bottom, left-to-right order
// renderChmodDialog draws them — the Tab/Backtab navigation order,
// mirroring currentFieldOrder's own shape in properties.go. Permissions'
// own 9 bits plus its own octal value are always present; the
// recursive-folders toggle and the Files-enable checkbox only exist
// once chmodAnyDir is true (see openChmod) — a target set made up
// entirely of plain files has nothing for either to apply to. Files'
// own 9 bits and octal value go one step further, only becoming field
// stops once stagedChmodFilesEnabled is actually checked — while it's
// off they render dimmed with no field of their own at all (see
// permissionBitsAndOctal/renderChmodDialog), so there is nothing here
// for Tab to stop on yet either.
func (r *Root) chmodFieldOrder() []chmodField {
	order := make([]chmodField, 0, len(chmodModeBitFields)+len(chmodFilesModeBitFields)+4)
	order = append(order, chmodModeBitFields[:]...)
	order = append(order, chmodFieldMode)
	if r.chmodAnyDir {
		order = append(order, chmodFieldRecursiveDirs)
		order = append(order, chmodFieldFilesEnable)
		if r.stagedChmodFilesEnabled {
			order = append(order, chmodFilesModeBitFields[:]...)
			order = append(order, chmodFieldFilesMode)
		}
	}
	return order
}

// chmodFieldIndex returns field's position in the dialog's current field
// order (see chmodFieldOrder) — the same role propertyFieldIndex has for
// Properties.
func (r *Root) chmodFieldIndex(field chmodField) (int, bool) {
	for i, f := range r.chmodFieldOrder() {
		if f == field {
			return i, true
		}
	}
	return 0, false
}

// isAutoEditChmodField reports whether landing keyboard focus on field
// should open its inline editor immediately, no separate Enter/Space
// needed first — the same convention isAutoEditField already has in
// properties.go: both of this dialog's octal values are plain text
// entry, where "tab to it" and "start typing" are the same action as
// far as the user's concerned. Every individual permission bit (a
// toggle, not text — see captureChmodKey's letter/Delete/Space
// handling) and both checkboxes are deliberately excluded, the same as
// every permission-bit/checkbox already is in Properties.
func isAutoEditChmodField(field chmodField) bool {
	return field == chmodFieldMode || field == chmodFieldFilesMode
}

// chmodSpan is one clickable/keyboard-focusable region within
// chmodText — the same row/column range idea as propertySpan.
type chmodSpan struct {
	row              int
	startCol, endCol int
	field            chmodField
}

// chmodBuilder assembles the Chmod dialog's text field by field,
// tracking each one's row/column span as it goes — propertiesBuilder's
// own shape, scaled down for this dialog's much smaller field set.
type chmodBuilder struct {
	root  *Root
	b     strings.Builder
	row   int
	col   int
	spans []chmodSpan
}

func (cb *chmodBuilder) tag(s string) { cb.b.WriteString(s) }

// text advances col by s's display width, not a plain rune count — see
// propertiesBuilder.text's own doc comment on why that distinction
// matters for span accuracy (immaterial here today, since nothing this
// dialog ever draws is anything but single-width ASCII, but kept
// consistent with every other builder in this codebase rather than
// quietly relying on that never changing).
func (cb *chmodBuilder) text(s string) {
	cb.b.WriteString(s)
	cb.col += tview.TaggedStringWidth(s)
}

func (cb *chmodBuilder) newline() {
	cb.b.WriteByte('\n')
	cb.row++
	cb.col = 0
}

// padTo appends spaces until col reaches at least target — the same
// effect fmt.Sprintf("%-Ns", ...) has for a plain ASCII label, but
// based on the builder's own tracked display width (see text) rather
// than a byte count, so it stays correct even once a row's own leading
// text isn't plain ASCII alone — Files' own checkbox-glyph-plus-label
// combo (see renderChmodDialog).
func (cb *chmodBuilder) padTo(target int) {
	for cb.col < target {
		cb.text(" ")
	}
}

// focusTag mirrors propertiesBuilder's own focusTag exactly — same
// colors, same brighter/bold style for whichever field currently has
// keyboard focus.
func (cb *chmodBuilder) focusTag(field chmodField) (tag, reset string) {
	if field == cb.root.focusedChmodField() {
		return fmt.Sprintf("[:%s:b]", colorTag(cb.root.theme.FocusedBackground)), "[:-:-]"
	}
	return fmt.Sprintf("[:%s]", colorTag(cb.root.theme.EditableBackground)), "[:-]"
}

// octalValue writes mode as a highlighted, clickable/editable 4-digit
// octal span (see fsops.ParseMode) — factored out from any particular
// label since this dialog's two mode fields (Permissions/Files) don't
// share one uniform "Label: value" layout the way Properties' fields
// do; used both on its own and as permissionBitsAndOctal's own tail
// (the "(0644)" half of "rwxrwxrwx (0644)"). dimmed forces the same
// gray "not applicable right now" style search.go's own dimTag already
// uses there (Ignore dirs' own value field while its enable checkbox is
// off) — Files' own value while stagedChmodFilesEnabled is false.
func (cb *chmodBuilder) octalValue(mode os.FileMode, field chmodField, dimmed bool) {
	if dimmed {
		cb.tag(dimTag)
	} else {
		tag, _ := cb.focusTag(field)
		cb.tag(tag)
	}
	start := cb.col
	cb.text(fmt.Sprintf("%04o", mode))
	end := cb.col
	cb.tag("[-:-:-]")
	cb.spans = append(cb.spans, chmodSpan{row: cb.row, startCol: start, endCol: end, field: field})
}

// permissionBitsAndOctal writes "rwxrwxrwx (0644)" — nine individually
// toggleable rwx characters (bitFields, one of chmodModeBitFields/
// chmodFilesModeBitFields) plus the equivalent octal value in
// parentheses (see octalValue), itself its own editable span
// (octalField) — two entirely equivalent ways to set the same 9 bits,
// always shown together, per the user's own explicit request that
// Chmod's own permission entry work exactly like Properties'
// permissionsField already does, identically, every time. Deliberately
// omits Properties' own leading, non-editable file-type character: a
// chmod-only os.FileMode never carries type bits (ParseMode only ever
// accepts 0-0777), and this dialog's own targets can freely mix files
// and directories in the first place, so there's no one "type" a
// leading character could meaningfully show here.
//
// dimmed is Files' own row while its own enable checkbox
// (chmodFieldFilesEnable) is off: styled gray throughout, and — unlike
// every other dimmed field in this codebase — deliberately registering
// no per-character spans of its own at all. There is nothing here yet
// for an individual bit or the octal value to mean independently of one
// another while files aren't even being touched, so renderChmodDialog
// itself wraps the *entire* dimmed row (checkbox, label, and this whole
// call's own output together) in one single span instead, per the
// user's own explicit request that a click anywhere on that row — not
// just precisely on the checkbox glyph — is enough to turn it on.
func (cb *chmodBuilder) permissionBitsAndOctal(mode os.FileMode, bitFields [9]chmodField, octalField chmodField, dimmed bool) {
	const rwx = "rwxrwxrwx"
	for i, f := range bitFields {
		if dimmed {
			cb.tag(dimTag)
		} else {
			tag, _ := cb.focusTag(f)
			cb.tag(tag)
		}
		start := cb.col
		ch := byte('-')
		if mode&(1<<uint(9-1-i)) != 0 {
			ch = rwx[i]
		}
		cb.text(string(ch))
		cb.tag("[-:-:-]")
		if !dimmed {
			cb.spans = append(cb.spans, chmodSpan{row: cb.row, startCol: start, endCol: cb.col, field: f})
		}
	}

	cb.text(" (")
	if dimmed {
		cb.tag(dimTag)
		cb.text(fmt.Sprintf("%04o", mode))
		cb.tag("[-:-:-]")
	} else {
		cb.octalValue(mode, octalField, false)
	}
	cb.text(")")
}

// toggle writes one "○/● label" checkbox, per checkboxText's own ○/●
// convention (see panel.go) — deliberately never wrapped in focusTag,
// unlike every other field in this dialog, even while focused: per the
// user's own explicit request for Properties' matching recursive-apply
// toggles, this is a plain clickbox, not a value being read or typed
// into, so it doesn't need the "this one has focus" background tint.
func (cb *chmodBuilder) toggle(checked bool, label string, field chmodField) {
	start := cb.col
	cb.text(checkboxText(checked) + " " + label)
	end := cb.col
	cb.spans = append(cb.spans, chmodSpan{row: cb.row, startCol: start, endCol: end, field: field})
}

// renderChmodDialog rebuilds the Chmod dialog's text from scratch —
// cheap enough to just call after every state change (a toggle
// flipped, an edit committed, a focus change) rather than patching
// anything incrementally, the same "always rebuild" approach
// renderProperties/rerenderSearchDialog already take.
//
// The first row's own permission value always shows; the
// recursive-folders toggle and the whole Files row only appear once
// chmodAnyDir is true (see openChmod) — a selection made up entirely of
// plain files has nothing for either to apply to. That first row's own
// label switches between "Permissions:" (a plain-file-only selection —
// there's no separate Files row to disambiguate it from, so the generic
// word already says everything there is to say) and "Directory:" (once
// a directory is involved — the more specific word, plus the shared
// "Permissions" column heading above both rows, is what distinguishes
// this row's own value from Files' own, separate one right below it),
// per the user's own explicit request. "recursive" remains the folders
// toggle's own label — also every subfolder inside, not just the
// folder(s) selected directly — but Files' own checkbox reads "Files:"
// instead: whether files get touched at all, not a recursion depth
// choice (there never was a non-recursive "just this one folder's own
// files" option to distinguish it from — see chmodFieldFilesEnable's
// own doc comment). A plain "recursive" word, with no checkbox or span
// of its own, follows Files' own value anyway — per the user's own
// explicit request for a hint that checking it reaches every file
// inside, not just the folder's own immediate children, without a
// second clickable control implying it's a separate choice.
func (r *Root) renderChmodDialog() {
	cb := &chmodBuilder{root: r}
	cb.newline() // blank margin row — no border of its own, matching Properties/Search

	firstRowLabel := "Permissions:"
	if r.chmodAnyDir {
		cb.padTo(chmodLabelWidth)
		cb.text("Permissions")
		cb.newline()
		firstRowLabel = "Directory:"
	}

	cb.text(fmt.Sprintf("%-*s", chmodLabelWidth, firstRowLabel))
	cb.permissionBitsAndOctal(r.stagedChmodMode, chmodModeBitFields, chmodFieldMode, false)
	if r.chmodAnyDir {
		cb.text("   ")
		cb.toggle(r.stagedChmodRecursiveDirs, "recursive", chmodFieldRecursiveDirs)
	}
	cb.newline()

	if r.chmodAnyDir {
		cb.newline()
		// The "○ Files:" checkbox+label combo is its own single span,
		// always chmodFieldFilesEnable regardless of state — clicking it
		// (or Enter/Space while it has keyboard focus) always toggles
		// stagedChmodFilesEnabled, on or off either way (see
		// activateChmodField's own chmodFieldFilesEnable case).
		rowStart := cb.col
		cb.text(checkboxText(r.stagedChmodFilesEnabled) + " Files:")
		labelEnd := cb.col
		cb.padTo(chmodLabelWidth)
		cb.permissionBitsAndOctal(r.stagedChmodFilesMode, chmodFilesModeBitFields, chmodFieldFilesMode, !r.stagedChmodFilesEnabled)

		// "recursive" itself is never a span of its own (see
		// chmodFieldFilesEnable's own doc comment) — while disabled it
		// simply falls inside the one whole-row span appended below, the
		// same as every other dimmed character on this row.
		cb.text("   ")
		if !r.stagedChmodFilesEnabled {
			cb.tag(dimTag)
		}
		cb.text("recursive")
		if !r.stagedChmodFilesEnabled {
			cb.tag("[-:-:-]")
		}

		if r.stagedChmodFilesEnabled {
			// Enabled: the checkbox+label keeps its own narrow span (to
			// turn it back off again); every individual bit and the
			// octal value already registered their own real spans
			// inside permissionBitsAndOctal above, the same as
			// Permissions'/Directory's own row.
			cb.spans = append(cb.spans, chmodSpan{row: cb.row, startCol: rowStart, endCol: labelEnd, field: chmodFieldFilesEnable})
		} else {
			// Disabled: permissionBitsAndOctal registered no spans of its
			// own at all (see its own doc comment) — one single span
			// covering the *entire* row (checkbox, label, and the whole
			// dimmed permission display together) stands in for all of
			// them, so a click anywhere on this row turns it on.
			cb.spans = append(cb.spans, chmodSpan{row: cb.row, startCol: rowStart, endCol: cb.col, field: chmodFieldFilesEnable})
		}
		cb.newline()
	}

	cb.newline()
	cb.newline()
	cb.text(chmodTargetsSummary(r.chmodTargets))

	r.chmodSpans = cb.spans
	r.chmodText.SetText(cb.b.String())
}

// chmodTargetsSummary renders what the dialog is about to apply to —
// the single path itself for one target (matching what the old
// single-target prompt implicitly showed via its own centered position
// over that one row), or a plain count once more than one is selected,
// since a full multi-path listing could easily run off the bottom of
// the screen.
func chmodTargetsSummary(targets []string) string {
	if len(targets) == 1 {
		return targets[0]
	}
	return fmt.Sprintf("%d items selected", len(targets))
}

// resizeChmodDialog sizes and positions the dialog from its own current
// text (see renderChmodDialog), clamped to the panel like every other
// overlay — the same shape resizeProperties already has, including the
// one extra reserved row for the always-visible Cancel/Apply button
// row.
func (r *Root) resizeChmodDialog(x, y int) {
	width, height := textSize(r.chmodText.GetText(true))
	height++ // reserved button row
	x, y, width, height = r.clampToPanel(x, y, width, height)

	r.chmodPages.SetRect(x, y, width, height)
	r.chmodButtons.SetRect(x, y+height-1, width, 1)
}

// rerenderChmodDialog re-runs renderChmodDialog/resizeChmodDialog in
// place, keeping the dialog's current on-screen position — the common
// tail of every edit action, the same shape rerenderProperties already
// has.
func (r *Root) rerenderChmodDialog() {
	x, y, _, _ := r.chmodPages.GetRect()
	r.renderChmodDialog()
	r.resizeChmodDialog(x, y)
}

// newChmodDialog builds the Chmod overlay: a read-only text display
// (chmodText) that individual fields' values are drawn on top of (see
// chmodBuilder), a single reusable inline input (chmodEditField, shown
// only while text-editing a field, repositioned over whichever one that
// is — the same "one shared field, repositioned per use" approach
// Properties/Search already use), and an always-visible Cancel/Apply
// button row (chmodButtons) — the same three-page shape newPropertiesView
// already has, for the same reasons (see its own doc comment):
// chmodText must stay visible underneath chmodEditField/chmodButtons,
// and keyboard navigation between the two checkboxes/two text fields is
// hand-rolled (see captureChmodKey/setChmodFocus/moveChmodFocus) rather
// than left to tview's own focus system, since none of chmodText's own
// fields are separate Primitives tview could cycle focus between on its
// own.
func (r *Root) newChmodDialog() *tview.Pages {
	r.chmodText = tview.NewTextView()
	r.chmodText.SetBorderPadding(0, 0, 1, 1)
	r.chmodText.SetDynamicColors(true) // needed for focusTag/dimTag's own style tags
	r.chmodText.SetInputCapture(r.captureChmodKey)
	r.chmodText.SetMouseCapture(r.captureChmodMouse)

	// Colored via r.theme.FocusedBackground (see applyTheme), same
	// reasoning as propertiesEditField: this field is only ever shown for
	// whichever field currently has focus.
	r.chmodEditField = tview.NewInputField()
	r.chmodEditField.SetDoneFunc(r.finishChmodEdit)
	// Set once, unconditionally, unlike Properties' own equivalent
	// (activateInlineTextField): this dialog's shared edit field is
	// always an octal value (chmodFieldMode/chmodFieldFilesMode, the
	// only two fields that ever activate it — see activateChmodField),
	// never a free-text one, so there's no other case here needing this
	// cleared again. A real InputField.SetMaxLength doesn't exist in
	// this tview version, verified directly against its own
	// inputfield.go — rejecting anything past the 4th character here is
	// the actual mechanism, per the user's own explicit request.
	r.chmodEditField.SetAcceptanceFunc(func(textToCheck string, lastChar rune) bool {
		return len(textToCheck) <= 4
	})

	r.chmodButtons = r.newChmodButtons()

	pages := tview.NewPages()
	pages.AddPage("text", r.chmodText, true, true)
	pages.AddPage("editfield", r.chmodEditField, false, false)
	pages.AddPage("buttons", r.chmodButtons, false, true)
	return pages
}

// newChmodButtons builds the always-visible Cancel/Apply row — the same
// shape newPropertiesButtons already has (see its own doc comment for
// why n is read fresh inside each closure rather than captured once).
func (r *Root) newChmodButtons() *tview.Flex {
	r.chmodCancelBtn = tview.NewButton("Cancel").SetSelectedFunc(r.cancelChmodDialog)
	r.chmodApplyBtn = tview.NewButton("Apply").SetSelectedFunc(r.applyChmodDialog)
	r.chmodCancelBtn.SetInputCapture(spaceAlsoActivates(r.cancelChmodDialog))
	r.chmodApplyBtn.SetInputCapture(spaceAlsoActivates(r.applyChmodDialog))

	r.chmodCancelBtn.SetExitFunc(func(key tcell.Key) {
		n := len(r.chmodFieldOrder())
		switch key {
		case tcell.KeyTab:
			r.setChmodFocus(n + 1) // Apply
		case tcell.KeyBacktab:
			r.setChmodFocus(n - 1) // last field
		case tcell.KeyEscape:
			r.cancelChmodDialog()
		}
	})
	r.chmodApplyBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.setChmodFocus(0) // wrap to the first field
		case tcell.KeyBacktab:
			r.setChmodFocus(len(r.chmodFieldOrder())) // Cancel
		case tcell.KeyEscape:
			r.cancelChmodDialog()
		}
	})
	r.chmodCancelBtn.SetFocusFunc(func() {
		r.chmodFocusedIdx = len(r.chmodFieldOrder())
		r.rerenderChmodDialog()
	})
	r.chmodApplyBtn.SetFocusFunc(func() {
		r.chmodFocusedIdx = len(r.chmodFieldOrder()) + 1
		r.rerenderChmodDialog()
	})

	return tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.chmodCancelBtn, 0, 1, false).
		AddItem(r.chmodApplyBtn, 0, 1, false)
}

// focusedChmodField returns which field, if any, chmodFocusedIdx
// currently points at — fieldNone once the index has moved past the
// last field stop onto Cancel/Apply, or before Tab has ever been
// pressed. The same role focusedPropertyField has for Properties.
func (r *Root) focusedChmodField() chmodField {
	order := r.chmodFieldOrder()
	if r.chmodFocusedIdx < 0 || r.chmodFocusedIdx >= len(order) {
		return chmodFieldNone
	}
	return order[r.chmodFocusedIdx]
}

// chmodSpanForField returns the current chmodSpan for field, if
// rendered — the keyboard-navigation counterpart to a mouse click
// already having a span in hand, the same role propertySpanForField has.
func (r *Root) chmodSpanForField(field chmodField) (chmodSpan, bool) {
	for _, s := range r.chmodSpans {
		if s.field == field {
			return s, true
		}
	}
	return chmodSpan{}, false
}

// setChmodFocus moves keyboard-navigation focus to stop idx — a field
// span (0..len(chmodFieldOrder())-1), the Cancel button
// (len(chmodFieldOrder())), or the Apply button
// (len(chmodFieldOrder())+1) — the same shape setPropertiesFocus
// already has, including auto-opening a field that's plain text entry
// (see isAutoEditChmodField) the moment focus lands on it.
func (r *Root) setChmodFocus(idx int) {
	r.chmodFocusedIdx = idx
	r.rerenderChmodDialog()

	order := r.chmodFieldOrder()
	n := len(order)
	switch idx {
	case n:
		r.app.SetFocus(r.chmodCancelBtn)
	case n + 1:
		r.app.SetFocus(r.chmodApplyBtn)
	default:
		r.app.SetFocus(r.chmodText)
		if idx >= 0 && idx < n && isAutoEditChmodField(order[idx]) {
			r.activateFocusedChmodStop()
		}
	}
}

// moveChmodFocus advances chmodFocusedIdx by delta (+1 for Tab, -1 for
// Backtab) among the field stops, wrapping past either end — the same
// shape movePropertiesFocus already has.
func (r *Root) moveChmodFocus(delta int) {
	n := len(r.chmodFieldOrder())
	idx := r.chmodFocusedIdx + delta
	switch {
	case idx < 0:
		idx = n + 1 // wrap to Apply
	case idx >= n:
		idx = n // Cancel
	}
	r.setChmodFocus(idx)
}

// activateFocusedChmodStop is Enter/Space's own action while a field
// stop has focus — the keyboard counterpart to clicking that same field
// (see activateChmodField). A no-op if nothing is focused yet, or the
// field somehow isn't currently rendered — shouldn't happen,
// chmodFieldOrder and what renderChmodDialog actually draws are meant
// to always agree, the same invariant activateFocusedPropertyStop's own
// doc comment already documents for Properties.
func (r *Root) activateFocusedChmodStop() {
	order := r.chmodFieldOrder()
	if r.chmodFocusedIdx < 0 || r.chmodFocusedIdx >= len(order) {
		return
	}
	field := order[r.chmodFocusedIdx]
	if span, ok := r.chmodSpanForField(field); ok {
		r.activateChmodField(span)
	}
}

// activateChmodField is what clicking any chmodSpan does — and, via
// activateFocusedChmodStop, what pressing Enter/Space on one via
// keyboard navigation does too: a permission bit or checkbox toggles
// immediately; either octal value instead positions and shows the
// shared inline edit field over that exact span, pre-filled with its
// current staged value — the same three-way shape activatePropertyField
// already has (a permission-bit check first, then a second switch for
// anything else that's a complete action on its own, then a third for
// anything that opens the shared text editor instead).
func (r *Root) activateChmodField(span chmodSpan) {
	if idx, ok := r.chmodFieldIndex(span.field); ok {
		r.chmodFocusedIdx = idx
	}
	r.rerenderChmodDialog()

	if _, ok := chmodPermFieldBit[span.field]; ok {
		r.toggleChmodBit(span.field)
		return
	}

	switch span.field {
	case chmodFieldRecursiveDirs:
		r.stagedChmodRecursiveDirs = !r.stagedChmodRecursiveDirs
		r.rerenderChmodDialog()
		return
	case chmodFieldFilesEnable:
		r.stagedChmodFilesEnabled = !r.stagedChmodFilesEnabled
		r.rerenderChmodDialog()
		return
	}

	switch span.field {
	case chmodFieldMode, chmodFieldFilesMode:
	default:
		return
	}
	// Blank, not the current value — per the user's own explicit
	// request: clicking in or Tab-focusing this field should let you
	// immediately type a fresh 4-digit value over it, not require
	// deleting the old one first. Leaving without typing anything
	// discards the (empty) edit exactly like any other invalid input
	// already does (see applyChmodEditText), leaving the staged mode
	// untouched.
	r.activateChmodTextField(span, "")
}

// activateChmodTextField positions and shows the shared inline edit
// field over span, pre-filled with prefill — the same shape
// activateInlineTextField already has for Properties, minus the
// minWidth parameter: both of this dialog's own text fields are a fixed
// 4-digit octal value, so there's no case here needing a wider minimum
// the way Name/Owner/Group's own free-text values do there.
func (r *Root) activateChmodTextField(span chmodSpan, prefill string) {
	r.chmodEditTarget = span.field
	r.chmodEditField.SetText(prefill)

	rectX, rectY, _, _ := r.chmodText.GetInnerRect()
	width := span.endCol - span.startCol
	if width < 4 {
		width = 4
	}
	r.chmodEditField.SetRect(rectX+span.startCol, rectY+span.row, width, 1)

	r.chmodPages.ShowPage("editfield")
	r.app.SetFocus(r.chmodEditField)
}

// finishChmodEdit handles Enter, Tab, and Backtab (commit) and Escape
// (discard just this field's in-progress text) in the shared inline
// edit field — the same shape finishPropertyEdit already has, including
// Tab/Backtab committing (not discarding) before continuing the outer
// field navigation they were already asking for.
func (r *Root) finishChmodEdit(key tcell.Key) {
	text := r.chmodEditField.GetText()
	target := r.chmodEditTarget
	r.chmodPages.HidePage("editfield")

	if key == tcell.KeyEnter || key == tcell.KeyTab || key == tcell.KeyBacktab {
		r.applyChmodEditText(target, text)
	}

	switch key {
	case tcell.KeyTab:
		r.moveChmodFocus(1)
	case tcell.KeyBacktab:
		r.moveChmodFocus(-1)
	default: // Enter or Escape: conclude editing, stay on the same field
		r.refocusChmodField()
	}
}

// refocusChmodField re-renders and returns real keyboard focus to
// chmodText for whatever chmodFocusedIdx currently is, without
// re-triggering isAutoEditChmodField's auto-open — the same role
// refocusPropertiesField has for Properties, and for the same reason:
// Enter concluding an edit shouldn't immediately reopen the very editor
// it just closed.
func (r *Root) refocusChmodField() {
	r.rerenderChmodDialog()
	r.app.SetFocus(r.chmodText)
}

// applyChmodEditText stages text as target's new value — the commit
// logic finishChmodEdit's Enter/Tab/Backtab cases share. Invalid input
// is silently discarded, the same convention applyPropertyEditText's
// own fieldPermOctal case already documents: fsops.ParseMode is the one
// validation step either field needs, so there's nothing further to
// check at Apply time either.
func (r *Root) applyChmodEditText(target chmodField, text string) {
	switch target {
	case chmodFieldMode:
		if mode, err := fsops.ParseMode(text); err == nil {
			r.stagedChmodMode = mode
		}
	case chmodFieldFilesMode:
		if mode, err := fsops.ParseMode(text); err == nil {
			r.stagedChmodFilesMode = mode
		}
	}
}

// captureChmodKey is the Chmod dialog's own keyboard capture, installed
// on chmodText — Tab/Backtab move focus among the field
// stops/checkboxes, Enter/Space activate whichever one currently has
// it, Escape cancels — the same shape capturePropertiesKey already has,
// including its own permission-bit letter/Delete handling now that this
// dialog has individual rwx bits of its own too (see
// chmodPermFieldBit/chmodPermFieldLetter): while one has focus, r/w/x
// sets it directly, Delete or '-' clears it, matching Properties'
// identical three keys alongside the plain click/Space/Enter every
// field already has.
func (r *Root) captureChmodKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyTab:
		r.moveChmodFocus(1)
		return nil
	case tcell.KeyBacktab:
		r.moveChmodFocus(-1)
		return nil
	case tcell.KeyEnter:
		r.activateFocusedChmodStop()
		return nil
	case tcell.KeyEscape:
		r.cancelChmodDialog()
		return nil
	case tcell.KeyDelete:
		field := r.focusedChmodField()
		if _, ok := chmodPermFieldBit[field]; ok {
			r.setChmodBit(field, false)
			return nil
		}
	case tcell.KeyRune:
		field := r.focusedChmodField()
		if _, ok := chmodPermFieldBit[field]; ok {
			switch {
			case event.Rune() == ' ':
				r.activateFocusedChmodStop() // toggle, same as Enter
				return nil
			case event.Rune() == '-':
				r.setChmodBit(field, false)
				return nil
			case rune(chmodPermFieldLetter[field]) == event.Rune():
				r.setChmodBit(field, true)
				return nil
			}
		} else if event.Rune() == ' ' {
			r.activateFocusedChmodStop()
			return nil
		}
	}
	return event
}

// captureChmodMouse routes a click within the Chmod dialog's read-only
// text to whichever chmodSpan (if any) it landed on — the same shape
// capturePropertiesMouse already has.
func (r *Root) captureChmodMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action != tview.MouseLeftClick || !r.chmodText.InRect(event.Position()) {
		return action, event
	}

	x, y := event.Position()
	rectX, rectY, _, _ := r.chmodText.GetInnerRect()
	row, col := y-rectY, x-rectX

	if span, ok := r.chmodSpanAt(row, col); ok {
		r.activateChmodField(span)
		return tview.MouseConsumed, nil
	}
	return action, event
}

// chmodSpanAt returns the chmodSpan covering (row, col), if any — the
// same role propertySpanAt has for Properties.
func (r *Root) chmodSpanAt(row, col int) (chmodSpan, bool) {
	for _, s := range r.chmodSpans {
		if s.row == row && col >= s.startCol && col < s.endCol {
			return s, true
		}
	}
	return chmodSpan{}, false
}

// openChmod is the context menu's "chmod": opens a full dialog (see
// newChmodDialog) instead of the single-line prompt this used to be,
// per the user's own explicit request. Multi-select capable via
// selectedOrCurrentPaths — the same fallback Trash/Remove/Sed Replace
// already use — rather than only ever the single right-clicked target
// the old prompt-based version was limited to.
//
// chmodAnyDir (see chmodFieldOrder/renderChmodDialog) decides whether
// the recursive-folders toggle and the whole Files section even appear:
// only once at least one of the selected paths is a directory (or
// resolves to one — see isDirish) is there anything for either to
// actually recurse into.
//
// Permissions starts prefilled from the first target's real current
// mode (unlike the old prompt, which always started blank) — with
// multiple, possibly differently-moded targets selected, there's no
// single "the" current value, so the first one is simply the least
// arbitrary choice, and typing over it costs no more than the old blank
// field did. Files defaults to that same value with every execute bit
// cleared (mode &^ 0o111) — the traditional dir/file relationship
// (755/644, 750/640, 700/600), and exactly the relationship the user's
// own example (755 dirs / 644 files) already assumes.
//
// HidePage("editfield") up front is a real bug fix, not defensive
// boilerplate copied from openProperties for its own sake: clicking
// Cancel/Apply with the *mouse* while a value's shared inline editor is
// still open reaches chmodCancelBtn/chmodApplyBtn's own MouseHandler
// directly — a different Primitive than chmodEditField, on a different
// part of the screen — without ever routing through finishChmodEdit,
// which is the only other place "editfield" gets hidden. Left showing,
// the *next* openChmod (even on a single plain file, with chmodAnyDir
// now false and no Files row rendered at all) still had it floating at
// wherever it was last positioned — a real, reported symptom: a second,
// stray octal field, in the wrong place, appearing "for no reason" even
// for a target with nothing to have generated it. Hiding it here
// unconditionally, on every open, means this dialog never carries
// leftover state from whatever the *previous* session was doing,
// regardless of how that session ended.
func (r *Root) openChmod() {
	targets := r.selectedOrCurrentPaths()
	if len(targets) == 0 {
		return
	}
	r.chmodPages.HidePage("editfield")

	mode := chmodDefaultMode
	if info, err := fsops.Stat(targets[0]); err == nil {
		mode = info.Mode.Perm()
	}

	r.chmodTargets = targets
	r.chmodAnyDir = false
	for _, t := range targets {
		if info, err := fsops.Stat(t); err == nil && isDirish(info) {
			r.chmodAnyDir = true
			break
		}
	}

	r.stagedChmodMode = mode
	r.stagedChmodRecursiveDirs = false
	r.stagedChmodFilesEnabled = false
	r.stagedChmodFilesMode = mode &^ 0o111
	r.chmodFocusedIdx = -1

	r.renderChmodDialog()
	width, height := textSize(r.chmodText.GetText(true))
	height++ // reserved button row
	x, y := r.centeredOnScreen(width, height)
	r.resizeChmodDialog(x, y)

	// showOverlayWithRestore + restoreChmodDialog, not a plain
	// showOverlay + setChmodFocus(0): the same reason openProperties
	// leaves propertiesFocusIndex at -1 rather than auto-focusing (and
	// so auto-editing) Name immediately on open. chmodPages.SetRect
	// just above does NOT cascade to chmodText's own rect synchronously
	// — tview.Pages only resizes a resize=true child's rect inside its
	// own Draw(), which hasn't run yet at this point in the call, still
	// well before the app's event loop ever gets back control — so
	// auto-editing a field here would position the shared inline editor
	// against chmodText's stale, pre-resize rect (wherever it was left
	// from whatever the *previous* session was doing), not this one's
	// real, freshly computed position. Leaving nothing focused yet, the
	// same as Properties does, defers the first real text-field
	// activation to an actual subsequent Tab keypress — by then at
	// least one real Draw() has already run, and chmodText's rect is
	// correct.
	r.showOverlayWithRestore(chmodPage, r.chmodPages, r.restoreChmodDialog)
}

// restoreChmodDialog re-applies whichever concrete Chmod dialog
// sub-widget should hold real keyboard focus for chmodFocusedIdx's
// current value — openChmod's own restore callback (see
// showOverlayWithRestore), the same role restoreProperties has for
// Properties. chmodFocusedIdx is always -1 by the time this first runs
// (see openChmod), which setChmodFocus itself already treats as
// "nothing focused yet, but chmodText should still hold real keyboard
// focus" without auto-opening anything.
func (r *Root) restoreChmodDialog() {
	r.setChmodFocus(r.chmodFocusedIdx)
}

// chmodDefaultMode is openChmod's own fallback Permissions value if
// fsops.Stat on the first target fails for some reason (a race with the
// file being removed between the panel listing it and the dialog
// opening, say) — matching chmod(1)'s own everyday default for a new
// directory, since the dialog can't offer a sensible "current value"
// otherwise.
const chmodDefaultMode os.FileMode = 0o755

// cancelChmodDialog is the Cancel button: closes the overlay without
// applying anything — safe to just hide, since nothing staged was ever
// written to any real file (see applyChmodDialog, the only place any of
// this actually touches disk).
func (r *Root) cancelChmodDialog() {
	r.hideOverlay()
}

// applyChmodDialog is the Apply button: runs the dialog's own staged
// settings against every target in chmodTargets (see
// applyChmodToTarget), continuing through the rest even if one fails —
// the same "collect only the first error, keep going" convention
// pasteInto's own multi-target loop already uses, rather than
// abandoning the whole batch over one bad target. Refreshes Details for
// each target regardless of its own outcome (see
// refreshDetailsIfShowing's own doc comment on why that's safe even
// after a partial failure) — a no-op for every target but whichever
// one, if any, Details actually happens to be showing.
func (r *Root) applyChmodDialog() {
	var firstErr error
	for _, target := range r.chmodTargets {
		if err := r.applyChmodToTarget(target); err != nil && firstErr == nil {
			firstErr = err
		}
		r.refreshDetailsIfShowing(target, target)
	}

	r.hideOverlay()
	r.showError(r.panel.load(r.panel.path))
	if firstErr != nil {
		r.showError(firstErr)
	}
}

// applyChmodToTarget runs the dialog's own staged settings against one
// target. Permissions' own value always applies to the target itself —
// a plain fsops.Chmod, or fsops.ChmodDirsRecursive once
// stagedChmodRecursiveDirs is on AND target actually is a directory
// (recursion has nothing to walk into otherwise, so a checkbox that
// simply doesn't apply to this particular target is silently ignored
// rather than treated as an error). stagedChmodFilesEnabled
// additionally, independently runs fsops.ChmodFilesRecursive with its
// own separate value — independently, because the user's own explicit
// request was for the two to be combinable in a single pass ("allen
// Unterordnern 755 und allen enthaltenen Dateien 644"), not an
// either/or choice.
//
// Stops at the first failure for this one target, matching
// savePropertiesEdit's own "whatever already succeeded stays applied,
// no automatic rollback" convention — but that failure doesn't stop
// applyChmodDialog's own loop over the *other* targets (see its own doc
// comment).
func (r *Root) applyChmodToTarget(target string) error {
	info, err := fsops.Stat(target)
	if err != nil {
		return err
	}

	if isDirish(info) && r.stagedChmodRecursiveDirs {
		if err := fsops.ChmodDirsRecursive(target, r.stagedChmodMode); err != nil {
			return err
		}
	} else if err := fsops.Chmod(target, r.stagedChmodMode); err != nil {
		return err
	}

	if isDirish(info) && r.stagedChmodFilesEnabled {
		if err := fsops.ChmodFilesRecursive(target, r.stagedChmodFilesMode); err != nil {
			return err
		}
	}

	return nil
}
