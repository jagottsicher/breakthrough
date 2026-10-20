package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/rclone"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// errRcloneNotAvailable is openCloudMountCreateDialog's own "refuse to
// open at all" case when rclone itself isn't on PATH — nothing this
// dialog could do would ever succeed without it.
var errRcloneNotAvailable = fmt.Errorf("rclone is not installed — cloud mounts need the rclone binary on PATH")

// cloudMountDialogCreateNewOption is a synthetic first entry in the
// Remote dropdown, never a real rclone remote — selecting it opens
// cloudremotedialog.go's own "New cloud remote" dialog instead of
// syncing Target the way a real remote's own selection does (see
// cloudMountDialogSyncTargetToSelection). This is what makes "no
// remotes configured yet" no longer a dead end (see this dialog's own
// doc comment on why it used to refuse to open at all in that case).
const cloudMountDialogCreateNewOption = "+  Create new remote…"

// The "New cloud mount" dialog — reached via the "+ New" chooser (see
// openNewConnectionChooser) rather than its own key, same as the
// Connect dialog it sits beside. Picks an rclone remote (by name, from
// ListRemotes) and a local target directory; a synthetic first entry
// in the Remote dropdown (cloudMountDialogCreateNewOption) opens
// cloudremotedialog.go's own "New cloud remote" dialog for the four
// static-credential backends it supports, returning here with the new
// remote preselected on success. An OAuth remote (OneDrive, Dropbox,
// Google Drive) still has to be configured outside breakthrough first
// (`rclone config`/`rclone authorize`) — see cloudremotedialog.go's
// own doc comment on why that's out of scope for a non-interactive
// form.

const cloudMountDialogPage = "cloud-mount-dialog"

// newCloudMountDialogForm builds the (initially empty) form once, from
// NewRoot — mirrors newConnectForm's own shape exactly, just four
// fields instead of six, and a DropDown instead of a plain Host field
// since "which remote" is a closed choice (ListRemotes' own result),
// not free text.
func (r *Root) newCloudMountDialogForm() *tview.Form {
	f := tview.NewForm()
	f.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			r.hideOverlay()
			return nil
		}
		return event
	})

	r.cloudMountDialogRemoteField = tview.NewDropDown().SetLabel("Remote")
	// A real, reported gap: widening the dialog itself (64 -> 100) did
	// nothing for this field specifically — confirmed directly against
	// tview's own dropdown.go, not guessed: unlike InputField,
	// DropDown.SetFormAttributes never receives a fieldWidth from the
	// Form at all, and its own default (GetFieldWidth, width 0) sizes
	// the closed box to its widest *option* string, never to whatever
	// room the Form actually has. SetFieldWidth is the only way to
	// make it wider than that — 50, generous enough for a real-world
	// remote name without being as wide as Target's own full path.
	r.cloudMountDialogRemoteField.SetFieldWidth(50)
	r.cloudMountDialogRemoteField.SetSelectedFunc(func(text string, index int) {
		// Prefills Target from the newly selected remote's own name,
		// but only if Target still holds some *other* remote's own
		// default — an explicitly typed path must never be silently
		// overwritten just because the dropdown selection moved (see
		// cloudMountDialogDefaultTarget's own doc comment for the
		// exact rule this follows).
		r.cloudMountDialogSyncTargetToSelection(text)
		// Delete makes no sense for the "+ Create new remote…"
		// sentinel (nothing real to delete yet) — rebuild the button
		// row to match, the same "which buttons are in the Flex
		// changes per state" shape setConnectButtonsMode already
		// establishes for the Connect dialog's own two-vs-three button
		// modes.
		r.setCloudMountDialogButtonsMode(index != 0)
	})
	f.AddFormItem(r.cloudMountDialogRemoteField)

	r.cloudMountDialogTargetField = tview.NewInputField().SetLabel("Target")
	r.cloudMountDialogTargetField.SetPlaceholder("local directory this remote mounts onto")
	f.AddFormItem(r.cloudMountDialogTargetField)

	r.cloudMountDialogIdentifierField = tview.NewInputField().SetLabel("Identifier")
	r.cloudMountDialogIdentifierField.SetPlaceholder("optional, a-z/0-9/_/-/space, max 32 chars")
	r.cloudMountDialogIdentifierField.SetAcceptanceFunc(connectIdentifierAcceptanceFunc)
	r.cloudMountDialogIdentifierField.SetBlurFunc(func() {
		r.cloudMountDialogIdentifierField.SetText(normalizeConnectIdentifier(r.cloudMountDialogIdentifierField.GetText()))
	})
	f.AddFormItem(r.cloudMountDialogIdentifierField)

	r.cloudMountDialogTagsField = tview.NewInputField().SetLabel("Tags")
	r.cloudMountDialogTagsField.SetPlaceholder("optional, comma-separated, max 10")
	r.cloudMountDialogTagsField.SetAcceptanceFunc(connectTagsAcceptanceFunc)
	r.cloudMountDialogTagsField.SetBlurFunc(func() {
		r.cloudMountDialogTagsField.SetText(strings.Join(remotefs.ParseTags(r.cloudMountDialogTagsField.GetText()), ", "))
	})
	r.cloudMountDialogTagsField.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			r.app.SetFocus(r.cloudMountDialogCancelBtn)
			return nil
		case tcell.KeyEnter:
			// OK, not Mount — the same "OK is the default, Enter in any
			// field runs it" contract the Connect dialog's own README
			// entry already documents: OK only ever saves, same as
			// there, never dials/mounts on its own.
			r.runSaveCloudMount()
			return nil
		}
		return event
	})
	f.AddFormItem(r.cloudMountDialogTagsField)

	return f
}

// styleCloudMountDialogFields applies the current theme to every
// field individually — styleInput/styleDropDown, not just the Form-
// level SetFieldBackgroundColor applyTheme's own cloudMountDialogForm
// block already sets. A real, reported gap: Form-level colors only
// ever reach a field without its own placeholder text (tview's own
// InputField renders a placeholder with a separate placeholderStyle,
// defaulting to tview's own global Styles.ContrastBackgroundColor,
// confirmed directly against inputfield.go, not guessed) — every
// field here has one, so without this they looked unstyled even
// though the Form itself was. Called from openCloudMountCreateDialog
// (every open) and applyTheme (a live color-scheme switch while
// already open).
func (r *Root) styleCloudMountDialogFields() {
	styleDropDown(r.cloudMountDialogRemoteField, r.theme)
	// true, not false — the same always-"focused"-looking background
	// every other static, single-purpose dialog in this app already
	// uses (Rename, Properties, chmod, batch rename, the Find dialog's
	// own searchEditField, see theme.go's own calls), rather than
	// InputBackground's own petrol/teal, which was never meant to be a
	// filled-field's own resting color — a real, reported mismatch once
	// this dialog actually had fields with text already in them.
	styleInput(r.cloudMountDialogTargetField, r.theme, true)
	styleInput(r.cloudMountDialogIdentifierField, r.theme, true)
	styleInput(r.cloudMountDialogTagsField, r.theme, true)
}

// newCloudMountDialogButtons mirrors newConnectButtons' own shape:
// build all four button widgets once, here, and let
// setCloudMountDialogButtonsMode decide which of them are actually in
// the Flex (and therefore where Tab/Backtab lead) per mode — never
// rewired again from here. OK only ever saves the MountEntry — never
// mounts it — so several cloud mounts can be prepared ahead of
// actually needing them, the same reason SaveConnection exists
// independently of RecordAttempt for SFTP (see SaveConnection's own
// doc comment). Mount saves and then actually mounts it, immediately.
//
// Delete is the one new button, per a real, reported gap: the merged
// list's own "✕" (removeCloudMount, see cloudmountmenu.go) can only
// ever remove a remote that already has a MountEntry — a remote just
// created (via "+ Create new remote…", see
// cloudMountDialogCreateNewOption) but never actually turned into one
// yet has no row there to act on at all, and Cancel alone leaves it
// sitting in rclone's own config forever with no way back to delete
// it from this app. Delete operates on whichever remote the dropdown
// currently has selected, whether or not it has a MountEntry yet (see
// runDeleteSelectedCloudRemote) — but makes no sense at all for the
// synthetic cloudMountDialogCreateNewOption entry, so
// setCloudMountDialogButtonsMode leaves it out of the Flex entirely
// while that's selected, rather than just disabling it (a real,
// reported complaint: it used to always be there, even then).
func (r *Root) newCloudMountDialogButtons() *tview.Flex {
	r.cloudMountDialogCancelBtn = tview.NewButton("Cancel").SetSelectedFunc(r.hideOverlay)
	r.cloudMountDialogOKBtn = tview.NewButton("OK").SetSelectedFunc(r.runSaveCloudMount)
	r.cloudMountDialogMountBtn = tview.NewButton("Mount").SetSelectedFunc(r.runSaveAndMountCloudMount)
	r.cloudMountDialogDeleteBtn = tview.NewButton("Delete").SetSelectedFunc(r.runDeleteSelectedCloudRemote)
	r.cloudMountDialogCancelBtn.SetInputCapture(spaceAlsoActivates(r.hideOverlay))
	r.cloudMountDialogOKBtn.SetInputCapture(spaceAlsoActivates(r.runSaveCloudMount))
	r.cloudMountDialogMountBtn.SetInputCapture(spaceAlsoActivates(r.runSaveAndMountCloudMount))
	r.cloudMountDialogDeleteBtn.SetInputCapture(spaceAlsoActivates(r.runDeleteSelectedCloudRemote))

	r.cloudMountDialogButtons = tview.NewFlex().SetDirection(tview.FlexColumn)
	r.setCloudMountDialogButtonsMode(false)
	return r.cloudMountDialogButtons
}

// setCloudMountDialogButtonsMode rebuilds cloudMountDialogButtons' own
// contents and Tab/Backtab wiring for showDelete — called from
// openCloudMountCreateDialog on every open and from the Remote
// dropdown's own SetSelectedFunc on every selection change, not just
// once from newCloudMountDialogButtons: the same four button widgets
// are shared across every open, so both which of them are actually in
// the Flex *and* where their own Tab/Backtab lead have to be set
// together here, every time — the exact shape setConnectButtonsMode
// already establishes in connectdialog.go for the same reason.
//
// Equal proportion (0, 1) each, nothing else, so however many buttons
// are present this time fill the whole row edge to edge, split evenly.
func (r *Root) setCloudMountDialogButtonsMode(showDelete bool) {
	r.cloudMountDialogButtons.Clear()
	r.cloudMountDialogButtons.AddItem(r.cloudMountDialogCancelBtn, 0, 1, false)
	r.cloudMountDialogButtons.AddItem(r.cloudMountDialogOKBtn, 0, 1, false)
	r.cloudMountDialogButtons.AddItem(r.cloudMountDialogMountBtn, 0, 1, false)

	// Escape always cancels, the same as everywhere else in this
	// dialog, regardless of mode.
	r.cloudMountDialogCancelBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.cloudMountDialogOKBtn)
		case tcell.KeyBacktab:
			if showDelete {
				r.app.SetFocus(r.cloudMountDialogDeleteBtn)
			} else {
				r.app.SetFocus(r.cloudMountDialogMountBtn)
			}
		case tcell.KeyEscape:
			r.hideOverlay()
		}
	})
	r.cloudMountDialogOKBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.cloudMountDialogMountBtn)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.cloudMountDialogCancelBtn)
		case tcell.KeyEscape:
			r.hideOverlay()
		}
	})

	if !showDelete {
		// No Delete button at all — nothing real to delete yet for the
		// synthetic cloudMountDialogCreateNewOption entry. Mount's own
		// Tab wraps straight back to the form, the same "last real
		// stop, wrap to the first field" shape the showDelete case
		// below gives Delete instead.
		r.cloudMountDialogMountBtn.SetExitFunc(func(key tcell.Key) {
			switch key {
			case tcell.KeyTab:
				r.cloudMountDialogForm.SetFocus(0)
				r.app.SetFocus(r.cloudMountDialogForm)
			case tcell.KeyBacktab:
				r.app.SetFocus(r.cloudMountDialogOKBtn)
			case tcell.KeyEscape:
				r.hideOverlay()
			}
		})
		return
	}

	r.cloudMountDialogButtons.AddItem(r.cloudMountDialogDeleteBtn, 0, 1, false)
	r.cloudMountDialogMountBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.cloudMountDialogDeleteBtn)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.cloudMountDialogOKBtn)
		case tcell.KeyEscape:
			r.hideOverlay()
		}
	})
	r.cloudMountDialogDeleteBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			// r.cloudMountDialogForm.SetFocus(0), not
			// r.app.SetFocus(r.cloudMountDialogRemoteField) — see
			// connectConnectBtn's own identical wrap in connectdialog.go
			// for the real, user-reported bug (Form.Focus overwrites
			// SetDoneFunc, so naming a field directly here would leave
			// the form's own internal focusedElement counter stale)
			// this works around.
			r.cloudMountDialogForm.SetFocus(0)
			r.app.SetFocus(r.cloudMountDialogForm)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.cloudMountDialogMountBtn)
		case tcell.KeyEscape:
			r.hideOverlay()
		}
	})
}

// newCloudMountDialogLayout mirrors newConnectLayout's own shape:
// title bar, then form + buttons stacked. 9 rows for the form: 2
// (tview.Form's own top/bottom border padding) + 4 fields + 3 gaps
// between them.
func (r *Root) newCloudMountDialogLayout() *tview.Flex {
	r.cloudMountDialogTitleBar = newPlainTitleBar("New cloud mount")
	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.cloudMountDialogForm, 9, 0, true).
		AddItem(r.cloudMountDialogButtons, 1, 0, false)
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.cloudMountDialogTitleBar, 1, 0, false).
		AddItem(content, 0, 1, true)
}

// cloudMountDialogDefaultTarget is the Target field's own prefill for
// remoteName: MountBase/remoteName — the exact path toggleCloudMount
// itself would compute if this entry were mounted right after being
// added with this field untouched.
func (r *Root) cloudMountDialogDefaultTarget(remoteName string) string {
	return rclone.MountDir(rclone.MountBase(r.settings), remoteName)
}

// cloudMountDialogSyncTargetToSelection updates Target to the newly
// selected remote's own default path — but only while Target still
// holds *some* remote's own unmodified default (i.e. it matches
// cloudMountDialogDefaultTarget for one of the dropdown's own
// options), never once the user has typed anything else into it. A
// field that silently overwrites what you just typed the moment you
// touch an unrelated control is exactly the kind of surprise this
// project's own "no silent surprises" principle exists to prevent.
func (r *Root) cloudMountDialogSyncTargetToSelection(selectedRemote string) {
	current := r.cloudMountDialogTargetField.GetText()
	for _, name := range r.cloudMountDialogRemotes {
		if current == r.cloudMountDialogDefaultTarget(name) {
			r.cloudMountDialogTargetField.SetText(r.cloudMountDialogDefaultTarget(selectedRemote))
			return
		}
	}
	if current == "" {
		r.cloudMountDialogTargetField.SetText(r.cloudMountDialogDefaultTarget(selectedRemote))
	}
}

// openCloudMountCreateDialog shows the dialog, populated with
// cloudMountDialogCreateNewOption first, then every remote ListRemotes
// reports that isn't already a configured MountEntry's own
// RemoteName — picking a remote already in use here would otherwise
// let a second MountEntry silently alias the first one's own remote,
// onto a different Target, which is confusing (which one actually
// gets written to?) without being outright invalid the way
// AddMountEntry's own duplicate-Target refusal is.
//
// preselect, if non-empty, selects that remote by name instead of the
// first real one — used when returning here straight from a
// successful runCreateCloudRemote, so the remote just created is
// already the one about to be mounted rather than making the user
// find it again in the dropdown themselves.
//
// Refuses to open at all — a clear error instead — only when rclone
// itself isn't available: nothing here could ever succeed without it,
// including the "create new remote" entry. An empty real-remote list
// is no longer that kind of dead end (see cloudMountDialogCreateNewOption's
// own doc comment on why).
func (r *Root) openCloudMountCreateDialog(preselect string) {
	if !rclone.Available() {
		r.showError(errRcloneNotAvailable)
		return
	}
	remotes, err := rclone.ListRemotes()
	if err != nil {
		r.showError(err)
		return
	}
	configured, err := rclone.LoadMountEntries()
	if err != nil {
		r.showError(err)
		return
	}
	inUse := make(map[string]bool, len(configured))
	for _, e := range configured {
		inUse[e.RemoteName] = true
	}

	var names []string
	for _, rem := range remotes {
		if !inUse[rem.Name] {
			names = append(names, rem.Name)
		}
	}
	r.cloudMountDialogRemotes = names

	options := append([]string{cloudMountDialogCreateNewOption}, names...)
	r.cloudMountDialogRemoteField.SetOptions(options, func(text string, index int) {
		if index == 0 {
			// Deliberately no hideOverlay here — openCloudRemoteDialog
			// itself uses pushOverlay, stacking on top of this dialog
			// rather than replacing it, precisely so Escape/Cancel in
			// that dialog pops back to this one instead of closing
			// everything (a real, reported dead end this used to be:
			// hideOverlay-then-showOverlay left nothing underneath).
			r.openCloudRemoteDialog()
			return
		}
		if index < 1 || index-1 >= len(names) {
			return
		}
		r.cloudMountDialogSyncTargetToSelection(names[index-1])
	})

	selectedIndex := 0
	if preselect != "" {
		for i, name := range names {
			if name == preselect {
				selectedIndex = i + 1
				break
			}
		}
	} else if len(names) > 0 {
		selectedIndex = 1
	}
	r.cloudMountDialogRemoteField.SetCurrentOption(selectedIndex)
	if selectedIndex > 0 {
		r.cloudMountDialogTargetField.SetText(r.cloudMountDialogDefaultTarget(names[selectedIndex-1]))
	} else {
		r.cloudMountDialogTargetField.SetText("")
	}
	r.cloudMountDialogIdentifierField.SetText("")
	r.cloudMountDialogTagsField.SetText("")
	r.setCloudMountDialogButtonsMode(selectedIndex != 0)

	// 100, not 64 — Target's own value is a real local filesystem
	// path (the default alone, ~/.local/share/breakthrough/mounts/
	// <user>/<remote>, already runs well past 64 columns once Target's
	// own label and Form's padding are counted), a real, reported
	// complaint. Same reasoning as the "New cloud remote" dialog's own
	// 90 (see resizeCloudRemoteDialog's doc comment) — this one's
	// longest value, not longest label, is what was actually cramped.
	width, height := 100, 12
	_, _, screenWidth, screenHeight := r.GetRect()
	if width > screenWidth-4 {
		width = screenWidth - 4
	}
	if height > screenHeight-4 {
		height = screenHeight - 4
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	r.cloudMountDialogLayout.SetRect(x, y, width, height)
	r.styleCloudMountDialogFields()
	r.showOverlay(cloudMountDialogPage, r.cloudMountDialogLayout)
}

// runAddCloudMount validates the current form, creates the target
// directory (EnsureMountDir — see its own doc comment for exactly what
// "already exists" is and isn't accepted), and persists the new
// MountEntry — the Connect dialog's own OK button, for this entity
// instead of remotefs.Connection. Deliberately does not mount it
// immediately: adding the entry and actually mounting it are two
// separate, deliberate actions here, the same split SaveConnection/
// RecordAttempt already keep for SFTP (see SaveConnection's own doc
// comment on why), and toggleCloudMount is right there in the merged
// list the moment this dialog closes.
func (r *Root) saveCloudMountEntry() (rclone.MountEntry, bool) {
	_, remoteName := r.cloudMountDialogRemoteField.GetCurrentOption()
	if remoteName == "" || remoteName == cloudMountDialogCreateNewOption {
		return rclone.MountEntry{}, false
	}
	target := strings.TrimSpace(r.cloudMountDialogTargetField.GetText())
	if target == "" {
		return rclone.MountEntry{}, false
	}
	identifier := normalizeConnectIdentifier(r.cloudMountDialogIdentifierField.GetText())
	tags := remotefs.ParseTags(r.cloudMountDialogTagsField.GetText())

	if err := rclone.EnsureMountDir(target); err != nil {
		r.showError(err)
		return rclone.MountEntry{}, false
	}
	entry := rclone.MountEntry{RemoteName: remoteName, Target: target, Identifier: identifier, Tags: tags}
	if err := rclone.AddMountEntry(entry); err != nil {
		r.showError(err)
		return rclone.MountEntry{}, false
	}
	return entry, true
}

// runSaveCloudMount is the "OK" button's own action — saves the
// MountEntry without mounting it, the Connect dialog's own OK for
// this entity instead of remotefs.Connection (see SaveConnection's own
// doc comment on why "prepared, not yet attempted" is a real,
// deliberate state worth having).
func (r *Root) runSaveCloudMount() {
	if _, ok := r.saveCloudMountEntry(); ok {
		r.hideOverlay()
	}
}

// runSaveAndMountCloudMount is the "Mount" button's own action —
// saves the MountEntry exactly like OK does, then immediately mounts
// it. The dialog closes right away, same as clicking a history row's
// own label dials immediately for SFTP; toggleCloudMount itself
// already runs the real Mount call via safeGo, so this never blocks
// the UI thread either.
func (r *Root) runSaveAndMountCloudMount() {
	entry, ok := r.saveCloudMountEntry()
	if !ok {
		return
	}
	r.hideOverlay()
	r.toggleCloudMount(entry)
}

// runDeleteSelectedCloudRemote is the "Delete" button's own action —
// permanently removes whichever remote the Remote dropdown currently
// has selected. A no-op for the synthetic
// cloudMountDialogCreateNewOption entry — there is no real remote
// there to delete.
//
// This exists specifically for the remote that has no MountEntry yet:
// one just created via "+ Create new remote…" (or saved leniently via
// that dialog's own OK, missing required fields and all — see
// runSaveCloudRemoteLenient) that the user now wants to undo without
// first having to give it a Target and save a MountEntry just to then
// remove that too. The mountEntry lookup/mounted-check below is
// defensive, not reachable through this dialog's own normal
// navigation: openCloudMountCreateDialog already excludes any remote
// that already has a MountEntry from the Remote dropdown entirely
// (see its own doc comment on why), so by the time this runs, the
// selected remote in practice never has one — that case is already
// handled by removeCloudMount's own "✕" in the merged list.
func (r *Root) runDeleteSelectedCloudRemote() {
	_, remoteName := r.cloudMountDialogRemoteField.GetCurrentOption()
	if remoteName == "" || remoteName == cloudMountDialogCreateNewOption {
		return
	}

	entries, err := rclone.LoadMountEntries()
	if err != nil {
		r.showError(err)
		return
	}
	var mountEntry *rclone.MountEntry
	for i := range entries {
		if entries[i].RemoteName == remoteName {
			mountEntry = &entries[i]
			break
		}
	}
	if mountEntry != nil {
		mounted, err := rclone.IsMounted(mountEntry.Target)
		if err != nil {
			r.showError(err)
			return
		}
		if mounted {
			r.showError(fmt.Errorf("unmount %q before removing it", remoteName))
			return
		}
	}

	r.openConfirm(
		fmt.Sprintf("Remove %q and delete its cloud credentials permanently?", remoteName),
		"Yes, delete permanently",
		func() {
			r.deleteCloudRemoteByName(remoteName, mountEntry)
			// Not hideOverlay — acceptConfirm itself already closed the
			// confirm overlay before running this closure (see its own
			// doc comment in trash.go); calling it again here closed this
			// dialog too, a real, reported bug. Refresh the dropdown
			// instead, so the just-deleted remote disappears from it
			// while this dialog stays open — the same "stay open, just
			// update" shape removeCloudMount's own confirm has for the
			// merged list it acts on instead.
			r.openCloudMountCreateDialog("")
		},
	)
}
