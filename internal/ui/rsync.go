// rsync.go is breakthrough's own dialog over internal/rsync's pure
// command-building core: picks sensible Source/Destination defaults
// from whatever's on screen right now, lets the user adjust every
// flag internal/rsync.Job understands, shows the exact command that
// would run as a live preview, and — once confirmed — hands that
// command to a real rsync(1) process with the real terminal, the same
// way this app's own embedded bash line already runs anything else
// that isn't safe or useful to run silently in the background.
package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

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

	// height fits rsyncTitleBar's own row plus rsyncContentLayout's
	// stacked widgets (rsyncForm's four fields, rsyncFlagsList's five
	// toggles, rsyncPreviewView's own row, rsyncSpacer, rsyncButtons) —
	// checked against a real render (see this dialog's own live-tmux
	// verification), not guessed; a shorter value silently clips the
	// bottom rows, the same lesson every other dialog in this app's
	// own history already recorded once.
	width, height := 86, 24
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

// defaultRsyncSource is openRsync's own Source default — see its
// doc comment for the reasoning.
func (r *Root) defaultRsyncSource() string {
	paths := r.panel.SelectedPathsInDisplayOrder()
	if len(paths) == 1 {
		return paths[0]
	}
	return r.panel.path
}

// defaultRsyncDestination is openRsync's own Destination default —
// see its doc comment for the reasoning.
func (r *Root) defaultRsyncDestination() string {
	idx, ok := r.splitPartner()
	if !ok {
		return ""
	}
	return r.tabs[idx].path
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

	r.rsyncSourceField = tview.NewInputField().SetLabel("Source")
	r.rsyncSourceField.SetText(r.defaultRsyncSource())
	r.rsyncSourceField.SetChangedFunc(func(string) { r.renderRsyncPreview() })
	r.rsyncForm.AddFormItem(r.rsyncSourceField)

	r.rsyncDestinationField = tview.NewInputField().SetLabel("Destination")
	r.rsyncDestinationField.SetText(r.defaultRsyncDestination())
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
	r.renderRsyncPreview()
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
			r.app.SetFocus(r.rsyncCancelBtn)
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

// newRsyncButtons builds rsyncForm's own action row once, from
// NewRoot — a real Cancel/Run button pair, the same established shape
// newDuplicateButtons/newChmodButtons/newSearchButtons already use for
// theirs (see duplicateButtons' own doc comment on root.go for why
// this, not a vertical List).
func (r *Root) newRsyncButtons() *tview.Flex {
	r.rsyncCancelBtn = tview.NewButton("Cancel").SetSelectedFunc(r.hideOverlay)
	r.rsyncRunBtn = tview.NewButton("Run").SetSelectedFunc(r.runRsync)
	r.rsyncCancelBtn.SetInputCapture(spaceAlsoActivates(r.hideOverlay))
	r.rsyncRunBtn.SetInputCapture(spaceAlsoActivates(r.runRsync))

	// Tab/Backtab close the chain rsyncExtraArgsField/rsyncFlagsList's
	// own SetInputCapture calls start — the same shape
	// newConnectButtons' own doc comment explains in full: cycles
	// Cancel -> Run -> back to Source, and the reverse.
	r.rsyncCancelBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.rsyncRunBtn)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.rsyncFlagsList)
		case tcell.KeyEscape:
			r.hideOverlay()
		}
	})
	r.rsyncRunBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.rsyncSourceField)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.rsyncCancelBtn)
		case tcell.KeyEscape:
			r.hideOverlay()
		}
	})

	return tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.rsyncCancelBtn, 0, 1, false).
		AddItem(r.rsyncRunBtn, 0, 1, false)
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
// shape rules out), rsyncSpacer, and rsyncButtons.
//
// 6 rows for rsyncForm: four fields (height 1 each) plus itemPadding
// (1 row between each pair, i.e. 3) plus 2 rows of the Form's own top/
// bottom border padding — 4 + 3 + 2 - 3 = ... verified directly
// against a real render (see this dialog's own live-tmux
// verification) rather than derived from the formula alone, the same
// "check, don't just compute" discipline every dialog height in this
// app's own history already had to learn the hard way at least once.
func (r *Root) newRsyncContentLayout() *tview.Flex {
	layout := tview.NewFlex().SetDirection(tview.FlexRow)
	layout.AddItem(r.rsyncForm, 9, 0, true)
	layout.AddItem(r.rsyncFlagsList, 5, 0, false)
	layout.AddItem(r.rsyncPreviewView, 2, 0, false)
	layout.AddItem(r.rsyncSpacer, 1, 0, false)
	layout.AddItem(r.rsyncButtons, 1, 0, false)
	return layout
}

// currentRsyncJob builds an rsync.Job from the dialog's own current
// field/flag state — never from anywhere else, so the live preview
// and the job actually run on "Run" can never disagree about what's
// about to happen.
//
// Source/Destination are parsed as plain local paths for now — a
// typed "user@host:path" is passed straight through as Endpoint.Path
// on a local (Host == "") Endpoint, which happens to still produce the
// exact same argument rsync itself would expect, since Endpoint.String
// only ever adds its own "user@host:" prefix when Host is actually
// set. A future round can offer picking a currently-connected remote
// tab directly instead of typing its address by hand.
func (r *Root) currentRsyncJob() rsync.Job {
	excludes := splitRsyncExcludes(r.rsyncExcludesField.GetText())
	return rsync.Job{
		Source:       rsync.Endpoint{Path: strings.TrimSpace(r.rsyncSourceField.GetText())},
		Destination:  rsync.Endpoint{Path: strings.TrimSpace(r.rsyncDestinationField.GetText())},
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
// applies to the remote-archive confirm dialog.
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
	r.runShellCommandFullScreen(job.Command())
}
