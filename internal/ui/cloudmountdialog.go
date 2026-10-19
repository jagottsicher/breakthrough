package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/rclone"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// errRcloneNotAvailable/errNoEligibleCloudRemotes are
// openCloudMountCreateDialog's own two "refuse to open at all" cases —
// see its own doc comment for why each one is a clear error instead
// of an unusable empty dialog.
var (
	errRcloneNotAvailable     = fmt.Errorf("rclone is not installed — cloud mounts need the rclone binary on PATH")
	errNoEligibleCloudRemotes = fmt.Errorf("no eligible cloud remotes: configure one with rclone first, or every configured remote is already a cloud mount")
)

// The "New cloud mount" dialog — reached via the "+ New" chooser (see
// openNewConnectionChooser) rather than its own key, same as the
// Connect dialog it sits beside. Deliberately narrower than that one:
// this only ever picks an rclone remote that already exists (by name,
// from ListRemotes) and a local target directory — it never drives
// `rclone config create` itself. Configuring a brand-new rclone remote
// with its own provider-specific fields (S3 keys, WebDAV URL, ...) is
// a separately scoped follow-up (see feature_ideas.txt section 12b);
// until then, a remote has to already exist in rclone's own config
// (by hand, or via `rclone config create`/`rclone authorize` run
// outside breakthrough) before it can be picked here.

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
	r.cloudMountDialogRemoteField.SetSelectedFunc(func(text string, index int) {
		// Prefills Target from the newly selected remote's own name,
		// but only if Target still holds some *other* remote's own
		// default — an explicitly typed path must never be silently
		// overwritten just because the dropdown selection moved (see
		// cloudMountDialogDefaultTarget's own doc comment for the
		// exact rule this follows).
		r.cloudMountDialogSyncTargetToSelection(text)
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
			r.runAddCloudMount()
			return nil
		}
		return event
	})
	f.AddFormItem(r.cloudMountDialogTagsField)

	return f
}

// newCloudMountDialogButtons mirrors newDuplicateButtons' own
// two-button Cancel/Add shape exactly.
func (r *Root) newCloudMountDialogButtons() *tview.Flex {
	r.cloudMountDialogCancelBtn = tview.NewButton("Cancel").SetSelectedFunc(r.hideOverlay)
	r.cloudMountDialogAddBtn = tview.NewButton("Add").SetSelectedFunc(r.runAddCloudMount)
	r.cloudMountDialogCancelBtn.SetInputCapture(spaceAlsoActivates(r.hideOverlay))
	r.cloudMountDialogAddBtn.SetInputCapture(spaceAlsoActivates(r.runAddCloudMount))

	r.cloudMountDialogCancelBtn.SetExitFunc(func(key tcell.Key) {
		if key == tcell.KeyBacktab {
			r.app.SetFocus(r.cloudMountDialogAddBtn)
		} else {
			r.cloudMountDialogForm.SetFocus(0)
			r.app.SetFocus(r.cloudMountDialogForm)
		}
	})
	r.cloudMountDialogAddBtn.SetExitFunc(func(key tcell.Key) {
		if key == tcell.KeyBacktab {
			r.cloudMountDialogForm.SetFocus(0)
			r.app.SetFocus(r.cloudMountDialogForm)
		} else {
			r.app.SetFocus(r.cloudMountDialogCancelBtn)
		}
	})

	return tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.cloudMountDialogCancelBtn, 0, 1, false).
		AddItem(r.cloudMountDialogAddBtn, 0, 1, false)
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

// openCloudMountCreateDialog shows the dialog, populated with every
// remote ListRemotes reports that isn't already a configured
// MountEntry's own RemoteName — picking a remote already in use here
// would otherwise let a second MountEntry silently alias the first
// one's own remote, onto a different Target, which is confusing
// (which one actually gets written to?) without being outright
// invalid the way AddMountEntry's own duplicate-Target refusal is.
//
// Refuses to open at all — a clear error instead — when rclone isn't
// available, or when there is nothing eligible to pick: an empty
// dropdown would be a dead end, not a usable dialog, and a caller is
// in a far better position to understand "no remotes" / "already all
// in use" from one sentence than from guessing why a form showing
// nothing won't let them type a selection.
func (r *Root) openCloudMountCreateDialog() {
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
	if len(names) == 0 {
		r.showError(errNoEligibleCloudRemotes)
		return
	}

	r.cloudMountDialogRemotes = names
	r.cloudMountDialogRemoteField.SetOptions(names, func(text string, index int) {
		if index < 0 || index >= len(names) {
			return
		}
		r.cloudMountDialogSyncTargetToSelection(names[index])
	})
	r.cloudMountDialogRemoteField.SetCurrentOption(0)
	r.cloudMountDialogTargetField.SetText(r.cloudMountDialogDefaultTarget(names[0]))
	r.cloudMountDialogIdentifierField.SetText("")
	r.cloudMountDialogTagsField.SetText("")

	width, height := 64, 12
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
func (r *Root) runAddCloudMount() {
	_, remoteName := r.cloudMountDialogRemoteField.GetCurrentOption()
	if remoteName == "" {
		return
	}
	target := strings.TrimSpace(r.cloudMountDialogTargetField.GetText())
	if target == "" {
		return
	}
	identifier := normalizeConnectIdentifier(r.cloudMountDialogIdentifierField.GetText())
	tags := remotefs.ParseTags(r.cloudMountDialogTagsField.GetText())

	if err := rclone.EnsureMountDir(target); err != nil {
		r.showError(err)
		return
	}
	entry := rclone.MountEntry{RemoteName: remoteName, Target: target, Identifier: identifier, Tags: tags}
	if err := rclone.AddMountEntry(entry); err != nil {
		r.showError(err)
		return
	}
	r.hideOverlay()
}
