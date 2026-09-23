// rsync.go is breakthrough's own dialog over internal/rsync's pure
// command-building core: picks sensible Source/Destination defaults
// from whatever's on screen right now, lets the user adjust every
// flag internal/rsync.Job understands, shows the exact command that
// would run as a live preview, and — once confirmed — either hands
// that command to a real rsync(1) process with the real terminal
// ("Run", the same way this app's own embedded bash line already runs
// anything that benefits from one directly attached), or starts it in
// the background ("Run in background" — see rsyncjob.go), keeping
// breakthrough itself fully usable, including a Copy/Cut/Paste running
// at the same time, while its own live --info=progress2 percentage
// shows in the status bar instead.
package ui

import (
	"fmt"
	"path"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
	"github.com/jagottsicher/breakthrough/internal/rsync"
)

// Flag labels for rsyncFlagsList — constants rather than repeated
// literals so a typo in one place can't silently desync the item text
// from the map key read back in currentRsyncJob, the same reasoning
// sedFlagOrder's own doc comment gives for its identical shape.
const (
	rsyncLabelCopyContents = "Copy the folder's contents in (not the folder itself)"
	rsyncLabelArchive      = "Archive mode — preserve permissions/times/symlinks (-a)"
	rsyncLabelCompress     = "Compress data in transit (-z)"
	rsyncLabelDelete       = "Delete extraneous files from destination (--delete)"
	rsyncLabelDryRun       = "Dry run — show what would happen, change nothing (-n)"
)

var rsyncFlagOrder = []string{rsyncLabelCopyContents, rsyncLabelArchive, rsyncLabelCompress, rsyncLabelDelete, rsyncLabelDryRun}

// openRsync is the "R" key's (see keymap.go) and the context menu's
// own action: builds Source/Destination defaults from whatever's
// currently on screen, resets every other field to Rsync's own sane
// starting point, and opens the dialog.
//
// Source defaults to the current selection's own single target if
// there's exactly one (matching selectedOrCurrentPaths' own "one
// specific thing" case), or the active panel's own current directory
// otherwise — several marked files/folders at once have no single
// obvious rsync source path between them, so this deliberately falls
// back to "sync the directory you're looking at" rather than guessing
// which one marked item was meant. Destination defaults to the split
// view's own other pane, when one is actually open (the one case
// where "the other side" is unambiguous — see Root.splitPartner) —
// left blank otherwise, since guessing a destination for a
// consequential, --delete-capable command is worse than asking.
func (r *Root) openRsync() {
	r.resetRsyncForm()

	// height is rsyncTitleBar's own row (1) plus newRsyncContentLayout's
	// own stacked rows (9 + 5 + 2 + 1 + 1 + 1 + 1 = 20), checked against
	// a real render (see this dialog's own live-tmux verification): a
	// shorter value silently clips the bottom rows, the same lesson
	// every other dialog in this app's own history already recorded
	// once, but a taller one leaves genuinely blank rows of its own
	// rect unfilled at the bottom instead — none of this dialog's own
	// Flex containers paint a background across space no child actually
	// occupies, so those leftover rows show whatever the panel
	// underneath last drew there rather than empty space.
	width, height := 86, 21
	_, _, screenWidth, screenHeight := r.GetRect() // Root fills the whole screen
	if width > screenWidth-4 {
		width = screenWidth - 4
	}
	if height > screenHeight-4 {
		height = screenHeight - 4
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	r.rsyncLayout.SetRect(x, y, width, height)
	r.showOverlay(rsyncPage, r.rsyncLayout)
}

// defaultRsyncSource is openRsync's own Source default — see its doc
// comment for the reasoning. Remote-aware: if the active panel is
// currently connected, the result is rendered as rsync's own
// "user@host:path" syntax instead of a bare local path, with that
// connection tracked alongside it (see rsyncFieldDefault) so its own
// port travels through to the real rsync invocation too, per the
// user's own explicit request that Rsync learn about a tab's already-
// established connection instead of asking Host/User/Port a second
// time by hand.
//
// Checks the checkbox selection first, then — with nothing marked —
// whichever row the table's cursor is actually on, the same
// selection-with-single-item-fallback shape selectedOrCurrentPaths
// already establishes for Move to Trash/Remove/Sed Replace. That
// second step is what a bare right-click needs: it moves the cursor to
// the clicked row but marks nothing, so reading only
// SelectedPathsInDisplayOrder here fell through to the panel's own
// current *directory* instead of the row actually clicked.
func (r *Root) defaultRsyncSource() rsyncFieldDefault {
	path := r.panel.path
	isFile := false
	switch paths := r.panel.SelectedPathsInDisplayOrder(); len(paths) {
	case 1:
		path = paths[0]
		if ref, ok := r.panel.rowRefForPath(path); ok {
			isFile = !ref.isDir
		}
	case 0:
		if _, rowPath, ok := r.panel.CurrentRowPath(); ok {
			path = rowPath
			if ref, ok := r.panel.rowRefForPath(path); ok {
				isFile = !ref.isDir
			}
		}
	}
	return rsyncFieldDefaultFor(r.panel, path, isFile)
}

// defaultRsyncDestination is openRsync's own Destination default —
// see its doc comment for the reasoning. Remote-aware the same way
// defaultRsyncSource is, against the split partner's own panel instead
// of r.panel. Always a directory (a tab's own current directory can
// never be a plain file), unlike Source.
func (r *Root) defaultRsyncDestination() rsyncFieldDefault {
	idx, ok := r.splitPartner()
	if !ok {
		return rsyncFieldDefault{}
	}
	partner := r.tabs[idx]
	return rsyncFieldDefaultFor(partner, partner.path, false)
}

// openRsyncTabPicker lists every open tab (see r.tabs) as a candidate
// Source/Destination for the Rsync dialog open behind it — the user's
// own explicit request for a real Tab-/Pane-Auswahl rather than typing
// (or trusting a one-shot default) by hand every time. Layers over the
// Rsync dialog the same way openOwnerGroupPicker already reuses r.picker
// for the unrelated owner/group case (see its own doc comment on the
// Root struct) — the two are never open at the same time, so nothing
// here has to coexist with that one's own state.
//
// Picking a tab writes exactly what defaultRsyncSource/
// defaultRsyncDestination would have defaulted field to had the dialog
// been opened from that tab in the first place (see rsyncFieldDefaultFor
// and *def, updated alongside field itself) — a remote tab's own
// connection (Host/Port/User) travels through to the real rsync
// invocation the same way a freshly-opened dialog's own default already
// does, not just its visible path text.
func (r *Root) openRsyncTabPicker(field *tview.InputField, def *rsyncFieldDefault) {
	if len(r.tabs) == 0 {
		return
	}

	r.picker.Clear()
	for i, tab := range r.tabs {
		picked := rsyncFieldDefaultFor(tab, tab.path, false)
		r.picker.AddItem(rsyncTabPickerLabel(i, picked.text), "", 0, func() {
			r.hideOverlay()
			*def = picked
			field.SetText(picked.text)
			r.renderRsyncPreview()
		})
	}
	r.picker.SetDoneFunc(func() { r.hideOverlay() })

	width, _ := listSize(r.picker)
	height := pickerHeight
	if len(r.tabs) < height {
		height = len(r.tabs)
	}
	x, y := r.centeredOnScreen(width, height)
	x, y, width, height = r.clampToPanel(x, y, width, height)
	r.picker.SetRect(x, y, width, height)
	r.picker.SetCurrentItem(0)

	r.pushOverlay(pickerPage, r.picker, nil)
}

// rsyncTabPickerLabel renders one row of openRsyncTabPicker — numbered
// the same way the tab switcher's own rows already are (see
// tabSwitcherRowLabel), but showing text exactly as it would land in
// the Rsync field it's for (a remote tab's own "user@host:path", not
// just its bare local-looking path — see rsyncFieldDefaultFor) rather
// than reusing that function directly, which only ever shows the bare
// path.
func rsyncTabPickerLabel(i int, text string) string {
	return fmt.Sprintf("%2d  %s", i+1, shortenPathLeft(text, tabSwitcherMaxPathWidth))
}

// rsyncFieldDefault records exactly what defaultRsyncSource/
// defaultRsyncDestination prefilled one field with, so currentRsyncJob
// can later tell "the field still reads exactly what was defaulted
// from a connected panel, so that connection's own Host/Port/User
// travel through to the real rsync invocation too" apart from "the
// user has since typed something else, treat it as plain rsync remote
// syntax (or a local path) with no way to know a non-default port from
// text alone" — the same limitation rsync.Endpoint's own Port field
// doc comment already gives for why its compact "host:path" form has
// no room to carry one, and exactly why a bare `ssh host` typed by
// hand doesn't magically know a non-standard port either.
type rsyncFieldDefault struct {
	text string               // the exact string put into the field
	conn *remotefs.Connection // nil if this default was a local path
	path string               // the bare path portion; meaningful only when conn != nil

	// isFile records whether this default resolved to a single plain
	// file rather than a directory (see defaultRsyncSource — always
	// false for defaultRsyncDestination, a tab's own current directory
	// is never a file). "Copy the folder's contents in" has no meaning
	// for a file the way it does for a directory, so
	// applyRsyncCopyContentsFlagToField and endpoint both special-case
	// this instead of mechanically appending "/" to a filename, which
	// would just be a nonexistent path rsync itself would refuse.
	isFile bool
}

// rsyncFieldDefaultFor builds one field's own default from panel: its
// current connection (if any) rendered as rsync's own "[user@]host:path"
// syntax via rsync.Endpoint.String — reused rather than reimplemented,
// so this can never drift from what Job.Command/Args actually produce
// — or path unchanged for a local panel.
func rsyncFieldDefaultFor(panel *Panel, path string, isFile bool) rsyncFieldDefault {
	if panel.remote == nil {
		return rsyncFieldDefault{text: path, isFile: isFile}
	}
	conn := panel.remoteConn
	text := rsync.Endpoint{Host: conn.Host, User: conn.User, Path: path}.String()
	return rsyncFieldDefault{text: text, conn: &conn, path: path, isFile: isFile}
}

// parentDirWithSlash returns d's own containing directory, rendered
// exactly the same way d.text itself is (a bare local path, or this
// connection's own "user@host:path" syntax), always ending in "/" —
// rsync's own "copy what's inside this directory" syntax. Only ever
// called when d.isFile: what applyRsyncCopyContentsFlagToField
// substitutes the Source field's own file default with while "Copy the
// folder's contents in" is checked, and what endpoint recognizes as
// still tracking d's own connection (see its own doc comment) rather
// than a real edit.
func (d rsyncFieldDefault) parentDirWithSlash() string {
	if d.conn == nil {
		return path.Dir(d.text) + "/"
	}
	return rsync.Endpoint{Host: d.conn.Host, User: d.conn.User, Path: path.Dir(d.path)}.String() + "/"
}

// endpoint reconstructs the real rsync.Endpoint fieldText should
// become right now — see rsyncFieldDefault's own doc comment for the
// exact-match reasoning.
//
// Tolerates fieldText carrying exactly one trailing "/" beyond d.text
// (unless d.text already ends in one itself, e.g. defaulted from the
// filesystem root — then an exact match is required, same as before):
// applyRsyncCopyContentsFlagToField adds or removes that same trailing
// "/" on the Source field as a purely cosmetic mirror of the "Copy the
// folder's contents in" toggle (see its own doc comment), not a real
// edit the user typed — losing a connection's own tracked port just
// because that toggle was flipped would be a real, surprising
// regression, not the harmless no-op it's meant to be.
//
// A file default (d.isFile) additionally tolerates fieldText reading
// exactly d.parentDirWithSlash() — the same toggle substitutes the
// whole field with that, not just an appended "/", since a plain file
// has no directory of its own to add one to.
func (d rsyncFieldDefault) endpoint(fieldText string) rsync.Endpoint {
	fieldText = strings.TrimSpace(fieldText)
	if d.conn == nil {
		return rsync.Endpoint{Path: fieldText}
	}
	if d.isFile && fieldText == d.parentDirWithSlash() {
		return rsync.Endpoint{Host: d.conn.Host, Port: d.conn.Port, User: d.conn.User, Path: path.Dir(d.path)}
	}
	compareText := fieldText
	if !strings.HasSuffix(d.text, "/") {
		compareText = strings.TrimSuffix(fieldText, "/")
	}
	if compareText == d.text {
		return rsync.Endpoint{Host: d.conn.Host, Port: d.conn.Port, User: d.conn.User, Path: d.path}
	}
	return rsync.Endpoint{Path: fieldText}
}

// newRsyncForm builds the (initially empty) Source/Destination/
// Excludes/Extra-flags form — called once from NewRoot; resetRsyncForm
// populates it fresh on every open, the same "Form doesn't lend itself
// to being reset in place" reasoning newSedForm/newDuplicateForm's own
// doc comments already give.
func (r *Root) newRsyncForm() *tview.Form {
	f := tview.NewForm()
	f.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			r.hideOverlay()
			return nil
		}
		return event
	})
	return f
}

// resetRsyncForm rebuilds rsyncForm's fields and rsyncFlagsList's
// toggles fresh for this open — Clear(true) first, the same reasoning
// resetSedForm's own doc comment gives (Form has no other way to
// remove everything a previous open left behind).
//
// Archive defaults on (rsync's own conventional "just copy it
// properly" default); Delete and Dry run default off — Delete
// specifically, since it's the one flag here that can permanently
// remove files at the destination, must never be silently pre-enabled.
// Copy contents defaults off too: "put a new folder named after the
// source inside the destination" matches how this app's own ordinary
// Copy/Paste already behaves, so it's the less surprising default
// between the two — see internal/rsync's own CopyContents doc comment
// for what the choice actually changes.
func (r *Root) resetRsyncForm() {
	r.rsyncForm.Clear(true)

	r.rsyncSourceDefault = r.defaultRsyncSource()
	r.rsyncSourceField = tview.NewInputField().SetLabel("Source")
	r.rsyncSourceField.SetText(r.rsyncSourceDefault.text)
	r.rsyncSourceField.SetChangedFunc(func(string) { r.renderRsyncPreview() })
	r.rsyncForm.AddFormItem(r.rsyncSourceField)

	r.rsyncDestinationDefault = r.defaultRsyncDestination()
	r.rsyncDestinationField = tview.NewInputField().SetLabel("Destination")
	r.rsyncDestinationField.SetText(r.rsyncDestinationDefault.text)
	r.rsyncDestinationField.SetChangedFunc(func(string) { r.renderRsyncPreview() })
	r.rsyncForm.AddFormItem(r.rsyncDestinationField)

	r.rsyncExcludesField = tview.NewInputField().SetLabel("Exclude (comma-separated patterns)")
	r.rsyncExcludesField.SetChangedFunc(func(string) { r.renderRsyncPreview() })
	r.rsyncForm.AddFormItem(r.rsyncExcludesField)

	r.rsyncExtraArgsField = tview.NewInputField().SetLabel("Extra flags")
	r.rsyncExtraArgsField.SetChangedFunc(func(string) { r.renderRsyncPreview() })
	r.rsyncForm.AddFormItem(r.rsyncExtraArgsField)

	// Tab off the form's own last field would otherwise just wrap back
	// to Source instead of ever reaching rsyncFlagsList — the same
	// "verified directly against tview's own Form.Focus, not guessed"
	// gap newConnectForm's own doc comment already found and fixed for
	// the Connect dialog: Form.Focus unconditionally overwrites any
	// SetDoneFunc with its own SetFinishedFunc whenever the form gains
	// focus, so SetInputCapture — which runs first — is the only place
	// left to intercept it.
	r.rsyncExtraArgsField.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyTab {
			r.app.SetFocus(r.rsyncFlagsList)
			return nil
		}
		return event
	})

	r.rsyncFlags = map[string]bool{
		rsyncLabelCopyContents: false,
		rsyncLabelArchive:      true,
		rsyncLabelCompress:     false,
		rsyncLabelDelete:       false,
		rsyncLabelDryRun:       false,
	}
	r.rsyncFlagsList.Clear()
	for _, label := range rsyncFlagOrder {
		label := label // capture for the closure below
		r.rsyncFlagsList.AddItem(rsyncFlagItemText(label, r.rsyncFlags[label]), "", 0, func() { r.toggleRsyncFlag(label) })
	}

	r.renderRsyncPreview()
}

// rsyncFlagItemText renders one rsyncFlagsList row — the same filled/
// outline-circle glyph (see checkboxText) sedFlagItemText already uses
// for its own identical shape, so a boolean toggle looks the same
// everywhere in this app regardless of which dialog it's in.
func rsyncFlagItemText(label string, checked bool) string {
	return fmt.Sprintf("%s  %s", checkboxText(checked), label)
}

// toggleRsyncFlag flips one flag's state, re-renders just that row,
// and refreshes the live preview — the same "selectedFunc flips
// state, then relabels" shape toggleSedFlag already uses, plus the
// preview refresh Sed Replace's own flags have no equivalent need for
// (that dialog only ever previews on demand, via its own "Preview"
// button, never live on every change).
func (r *Root) toggleRsyncFlag(label string) {
	r.rsyncFlags[label] = !r.rsyncFlags[label]
	for i, l := range rsyncFlagOrder {
		if l == label {
			r.rsyncFlagsList.SetItemText(i, rsyncFlagItemText(label, r.rsyncFlags[label]), "")
			break
		}
	}
	if label == rsyncLabelCopyContents {
		r.applyRsyncCopyContentsFlagToField(r.rsyncFlags[label])
	}
	r.renderRsyncPreview()
}

// applyRsyncCopyContentsFlagToField keeps the Source field's own
// visible text in sync with "Copy the folder's contents in" — per the
// user's own explicit request: the trailing "/" this toggle means was
// already reflected in the live preview line below (via
// currentRsyncJob/sourceArg), but the field itself stayed exactly as
// typed or defaulted, showing something that silently disagreed with
// what the preview said was actually about to run. on appends a
// trailing "/" if the field doesn't already end with one; off strips
// exactly one. A no-op on an empty field — there's no path yet to add
// or remove a slash from. See rsyncFieldDefault.endpoint's own doc
// comment for why this cosmetic edit doesn't lose a connection's own
// tracked port the way a real edit deliberately would.
//
// A file default (rsyncSourceDefault.isFile) is special-cased instead:
// "contents" has no meaning for a plain file, so checking the box
// while the field still reads exactly that file's own default path
// substitutes its parent directory (with the trailing "/") instead of
// mechanically appending one to a filename — which would just describe
// a directory that doesn't exist, and which rsync would refuse outright
// rather than silently do something sensible with. Unchecking it again
// while the field still reads exactly that substituted directory
// restores the original file path exactly, rather than merely
// stripping the slash, which would leave the directory selected
// instead of going back to the single file the dialog actually opened
// on. Either substitution only ever fires while the field still reads
// exactly what it was substituted from — the same "only a cosmetic
// no-op, never overriding something the user actually typed" guarantee
// the plain slash-toggle below already gives.
func (r *Root) applyRsyncCopyContentsFlagToField(on bool) {
	text := r.rsyncSourceField.GetText()
	if text == "" {
		return
	}
	if d := r.rsyncSourceDefault; d.isFile {
		switch {
		case on && text == d.text:
			r.rsyncSourceField.SetText(d.parentDirWithSlash())
			return
		case !on && text == d.parentDirWithSlash():
			r.rsyncSourceField.SetText(d.text)
			return
		}
	}
	switch {
	case on && !strings.HasSuffix(text, "/"):
		r.rsyncSourceField.SetText(text + "/")
	case !on && strings.HasSuffix(text, "/"):
		r.rsyncSourceField.SetText(strings.TrimSuffix(text, "/"))
	}
}

// newRsyncFlagsList builds rsyncFlagsList once, from NewRoot — see
// newRsyncForm's own package doc comment for why the five flag
// toggles live here instead of as Form checkboxes. Repopulated fresh
// on every open (see resetRsyncForm), the same as rsyncForm's own
// fields.
func (r *Root) newRsyncFlagsList() *tview.List {
	l := tview.NewList().ShowSecondaryText(false)
	l.SetHighlightFullLine(true)
	l.SetDoneFunc(r.hideOverlay) // Escape
	// Tab/Backtab continue the same chain rsyncExtraArgsField's own
	// SetInputCapture starts — a List has no SetExitFunc the way
	// Button does, so this is the only place to intercept them; Escape
	// itself is deliberately left alone here (passed straight through
	// via the final "return event"), reaching the List's own native
	// handling and, through it, SetDoneFunc above.
	l.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			r.app.SetFocus(r.rsyncPickSourceBtn)
			return nil
		case tcell.KeyBacktab:
			r.app.SetFocus(r.rsyncExtraArgsField)
			return nil
		}
		return event
	})
	return l
}

func (r *Root) newRsyncPreviewView() *tview.TextView {
	v := tview.NewTextView()
	v.SetBorderPadding(0, 0, 1, 0)
	v.SetDynamicColors(true)
	return v
}

// newRsyncHintView builds rsyncHintView once — see its own doc comment
// on the Root struct for what it's for and why it's always present,
// just blank when there's nothing to say.
func (r *Root) newRsyncHintView() *tview.TextView {
	v := tview.NewTextView()
	v.SetBorderPadding(0, 0, 1, 0)
	v.SetDynamicColors(true)
	return v
}

// newRsyncPickButtons builds rsyncPickSourceBtn/rsyncPickDestinationBtn
// once, from NewRoot — each opens openRsyncTabPicker for its own field.
// A real button pair, not a new keybinding: see the Root struct's own
// doc comment on rsyncPickSourceBtn for why.
func (r *Root) newRsyncPickButtons() *tview.Flex {
	pickSource := func() { r.openRsyncTabPicker(r.rsyncSourceField, &r.rsyncSourceDefault) }
	pickDestination := func() { r.openRsyncTabPicker(r.rsyncDestinationField, &r.rsyncDestinationDefault) }

	r.rsyncPickSourceBtn = tview.NewButton("Pick source tab…").SetSelectedFunc(pickSource)
	r.rsyncPickDestinationBtn = tview.NewButton("Pick destination tab…").SetSelectedFunc(pickDestination)
	r.rsyncPickSourceBtn.SetInputCapture(spaceAlsoActivates(pickSource))
	r.rsyncPickDestinationBtn.SetInputCapture(spaceAlsoActivates(pickDestination))

	// Tab/Backtab continue the chain rsyncFlagsList's own
	// SetInputCapture starts and rsyncCancelBtn's own SetExitFunc picks
	// back up — see newRsyncButtons' own doc comment for the whole
	// cycle.
	r.rsyncPickSourceBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.rsyncPickDestinationBtn)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.rsyncFlagsList)
		case tcell.KeyEscape:
			r.hideOverlay()
		}
	})
	r.rsyncPickDestinationBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.rsyncCancelBtn)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.rsyncPickSourceBtn)
		case tcell.KeyEscape:
			r.hideOverlay()
		}
	})

	return tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.rsyncPickSourceBtn, 0, 1, false).
		AddItem(r.rsyncPickDestinationBtn, 0, 1, false)
}

// newRsyncButtons builds rsyncForm's own action row once, from
// NewRoot — a real Cancel/Run button pair, the same established shape
// newDuplicateButtons/newChmodButtons/newSearchButtons already use for
// theirs (see duplicateButtons' own doc comment on root.go for why
// this, not a vertical List).
func (r *Root) newRsyncButtons() *tview.Flex {
	r.rsyncCancelBtn = tview.NewButton("Cancel").SetSelectedFunc(r.hideOverlay)
	r.rsyncRunBtn = tview.NewButton("Run").SetSelectedFunc(r.runRsync)
	r.rsyncRunBackgroundBtn = tview.NewButton("Run in background").SetSelectedFunc(r.runRsyncBackground)
	r.rsyncCancelBtn.SetInputCapture(spaceAlsoActivates(r.hideOverlay))
	r.rsyncRunBtn.SetInputCapture(spaceAlsoActivates(r.runRsync))
	r.rsyncRunBackgroundBtn.SetInputCapture(spaceAlsoActivates(r.runRsyncBackground))

	// Tab/Backtab close the chain rsyncExtraArgsField/rsyncFlagsList/
	// rsyncPickButtons' own SetInputCapture/SetExitFunc calls start —
	// the same shape newConnectButtons' own doc comment explains in
	// full: cycles Pick destination tab… -> Cancel -> Run -> Run in
	// background -> back to Source, and the reverse.
	r.rsyncCancelBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.rsyncRunBtn)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.rsyncPickDestinationBtn)
		case tcell.KeyEscape:
			r.hideOverlay()
		}
	})
	r.rsyncRunBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.rsyncRunBackgroundBtn)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.rsyncCancelBtn)
		case tcell.KeyEscape:
			r.hideOverlay()
		}
	})
	r.rsyncRunBackgroundBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.rsyncSourceField)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.rsyncRunBtn)
		case tcell.KeyEscape:
			r.hideOverlay()
		}
	})

	return tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.rsyncCancelBtn, 0, 1, false).
		AddItem(r.rsyncRunBtn, 0, 1, false).
		AddItem(r.rsyncRunBackgroundBtn, 0, 1, false)
}

// newRsyncLayout wraps rsyncTitleBar (" Rsync ") above
// rsyncContentLayout — the same widget/layout split
// menu/menuTitleBar/menuLayout already established, mirrored by every
// other dialog in this app.
func (r *Root) newRsyncLayout() *tview.Flex {
	r.rsyncTitleBar = newPlainTitleBar("Rsync")
	r.rsyncContentLayout = r.newRsyncContentLayout()
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.rsyncTitleBar, 1, 0, false).
		AddItem(r.rsyncContentLayout, 0, 1, true)
}

// newRsyncContentLayout stacks rsyncForm, rsyncFlagsList,
// rsyncPreviewView (its own always-visible row, never a Form item —
// see this file's own doc comment on root.go for the real bug that
// shape rules out), rsyncHintView, rsyncSpacer, and rsyncButtons.
//
// 6 rows for rsyncForm: four fields (height 1 each) plus itemPadding
// (1 row between each pair, i.e. 3) plus 2 rows of the Form's own top/
// bottom border padding — 4 + 3 + 2 - 3 = ... verified directly
// against a real render (see this dialog's own live-tmux
// verification) rather than derived from the formula alone, the same
// "check, don't just compute" discipline every dialog height in this
// app's own history already had to learn the hard way at least once.
//
// rsyncHintView is a fixed extra row, always reserved, rather than a
// row that only appears when there's actually a remote→remote hint to
// show: a dialog whose own total height silently changes depending on
// what Source/Destination happen to resolve to right now is a worse
// surprise than one blank row most of the time — openRsync's own fixed
// height already has enough slack below the sum of these fixed rows to
// absorb it (checked directly, not assumed).
func (r *Root) newRsyncContentLayout() *tview.Flex {
	layout := tview.NewFlex().SetDirection(tview.FlexRow)
	layout.AddItem(r.rsyncForm, 9, 0, true)
	layout.AddItem(r.rsyncFlagsList, 5, 0, false)
	layout.AddItem(r.rsyncPreviewView, 2, 0, false)
	layout.AddItem(r.rsyncHintView, 1, 0, false)
	layout.AddItem(r.rsyncPickButtons, 1, 0, false)
	layout.AddItem(r.rsyncSpacer, 1, 0, false)
	layout.AddItem(r.rsyncButtons, 1, 0, false)
	return layout
}

// currentRsyncJob builds an rsync.Job from the dialog's own current
// field/flag state — never from anywhere else, so the live preview
// and the job actually run on "Run" can never disagree about what's
// about to happen.
//
// Source/Destination go through rsyncSourceDefault/
// rsyncDestinationDefault's own endpoint method: still exactly what
// was defaulted from a connected panel, so its Host/Port/User travel
// through too, or plain rsync remote syntax (or a local path) for
// anything the user has since typed by hand — see rsyncFieldDefault's
// own doc comment.
func (r *Root) currentRsyncJob() rsync.Job {
	excludes := splitRsyncExcludes(r.rsyncExcludesField.GetText())
	return rsync.Job{
		Source:       r.rsyncSourceDefault.endpoint(r.rsyncSourceField.GetText()),
		Destination:  r.rsyncDestinationDefault.endpoint(r.rsyncDestinationField.GetText()),
		CopyContents: r.rsyncFlags[rsyncLabelCopyContents],
		Archive:      r.rsyncFlags[rsyncLabelArchive],
		Compress:     r.rsyncFlags[rsyncLabelCompress],
		Delete:       r.rsyncFlags[rsyncLabelDelete],
		DryRun:       r.rsyncFlags[rsyncLabelDryRun],
		Progress:     true,
		Excludes:     excludes,
		ExtraArgs:    strings.TrimSpace(r.rsyncExtraArgsField.GetText()),
	}
}

// splitRsyncExcludes turns the Excludes field's own comma-separated
// text into individual patterns — trimmed, and with any empty entry
// (a trailing comma, or the field simply being blank) left out rather
// than turned into a real, meaningless "--exclude=" flag.
func splitRsyncExcludes(text string) []string {
	var patterns []string
	for _, p := range strings.Split(text, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			patterns = append(patterns, p)
		}
	}
	return patterns
}

// renderRsyncPreview updates rsyncPreviewView with the exact command
// currentRsyncJob would run right now — recomputed on every keystroke
// and every flag toggle, so a change is always reflected before "Run"
// is ever pressed, the same "never run something consequential
// without showing what it is first" principle this project already
// applies to the remote-archive confirm dialog. Also refreshes
// rsyncHintView (see its own doc comment).
//
// --delete is called out in the theme's own warning color when it's
// on: the one flag here that turns a sync into "also remove whatever
// isn't in Source anymore", worth a visibly different look rather
// than blending into the rest of the command.
func (r *Root) renderRsyncPreview() {
	if r.rsyncPreviewView == nil {
		return
	}
	job := r.currentRsyncJob()
	command := job.Command()
	if job.Delete {
		command = strings.Replace(command, "'--delete'", "["+colorTag(r.theme.WarningText)+"]'--delete'[-]", 1)
	}
	r.rsyncPreviewView.SetText(command)
	r.rsyncHintView.SetText(rsyncRelayHint(job, r.theme))
}

// rsyncRelayHint is a one-line, warning-colored notice shown only when
// both Source and Destination resolve to a remote host — "" (nothing
// shown) for every other combination. rsync -e ssh has no server-to-
// server transfer mode of its own: every byte still flows through
// this machine over two separate ssh connections, never directly
// between the two remote hosts, exactly the "surprisingly slow over a
// weak local link" trap the user's own notes on this feature call out
// — worth saying up front, not discovered only once a sync already
// feels unexpectedly slow.
func rsyncRelayHint(job rsync.Job, theme config.ResolvedTheme) string {
	if !rsyncEndpointIsRemote(job.Source) || !rsyncEndpointIsRemote(job.Destination) {
		return ""
	}
	return fmt.Sprintf("[%s]⚠ remote→remote is relayed through this machine, not directly between hosts[-]", colorTag(theme.WarningText))
}

// rsyncEndpointIsRemote reports whether e names a remote rsync
// endpoint — either because Host is already set (a live connection
// this project itself resolved it from, see rsyncFieldDefault), or
// because its own Path looks like rsync's own "[user@]host:path"
// remote syntax on its own merits. That second case matters on its
// own: typing a remote address straight in — syncing to a third host
// never connected to via the Connect dialog at all — is a genuinely
// common way to use Rsync, and the relay warning below should still
// catch it. The heuristic is rsync(1)'s own real disambiguation rule
// between a remote spec and a local path containing a literal ":"
// somewhere inside it, not a guess: a colon before the first "/" names
// a host, one that doesn't is just part of an ordinary local path.
func rsyncEndpointIsRemote(e rsync.Endpoint) bool {
	if e.IsRemote() {
		return true
	}
	colon := strings.IndexByte(e.Path, ':')
	if colon < 0 {
		return false
	}
	slash := strings.IndexByte(e.Path, '/')
	return slash < 0 || colon < slash
}

// runRsync is the "Run" button's action: hands currentRsyncJob's own
// Command() to a real rsync process with the real terminal (see
// runShellCommandFullScreen), the same way this app's own embedded
// bash line already runs anything that benefits from a live, directly
// attached terminal rather than a background task — rsync's own
// --info=progress2 output relies on carriage-return line overwriting
// that only a real terminal renders correctly, not something this
// app's own tview-based overlays could easily reproduce.
//
// Source/Destination are required — an empty one is refused outright
// rather than handed to rsync itself to fail on, since rsync given an
// empty destination argument does something far more surprising than
// a plain "missing argument" error (POSIX shell parameter rules mean
// an empty, unquoted field simply vanishes from the argument list
// entirely, silently shifting every argument after it — confirmed
// directly, not assumed).
func (r *Root) runRsync() {
	job := r.currentRsyncJob()
	if job.Source.Path == "" || job.Destination.Path == "" {
		r.showError(fmt.Errorf("rsync: both Source and Destination are required"))
		return
	}
	r.hideOverlay()
	r.runShellCommandFullScreen(job.Command(), activitylog.CategoryRsync)
}

// runRsyncBackground is the "Run in background" button's own action —
// the same Source/Destination validation runRsync already does, then
// startRsyncBackground instead of suspending the terminal (see
// rsyncjob.go's own package doc comment for the full trade-off: no
// directly-attached terminal for whatever isn't already covered by
// --info=progress2's own percentage, but Copy/Cut/Paste — and browsing
// itself — keep working the whole time, and its own live progress shows
// in the status bar exactly the way a Paste's already does).
func (r *Root) runRsyncBackground() {
	job := r.currentRsyncJob()
	if job.Source.Path == "" || job.Destination.Path == "" {
		r.showError(fmt.Errorf("rsync: both Source and Destination are required"))
		return
	}
	r.hideOverlay()
	r.startRsyncBackground(job, rsyncEndpointBase(job.Source)+" → "+rsyncEndpointBase(job.Destination))
}

// rsyncEndpointBase is startRsyncBackground's own compact label for one
// endpoint — just enough to tell two backgrounded runs apart in the
// status bar without eating the whole line the way a full path would:
// the trailing path component, prefixed with the host for a remote
// endpoint (its path alone could name the same last component on two
// completely different machines).
func rsyncEndpointBase(e rsync.Endpoint) string {
	base := path.Base(e.Path)
	if !e.IsRemote() {
		return base
	}
	return e.Host + ":" + base
}
