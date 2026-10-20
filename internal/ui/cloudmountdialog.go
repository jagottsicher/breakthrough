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
const cloudMountDialogCreateNewOption = " + Create new remote cloud location"

// cloudMountDialogUnconfiguredRemoteName is the RemoteName
// saveCloudMountEntry gives a MountEntry saved while
// cloudMountDialogCreateNewOption is still selected — a real, reported
// gap: unlike the Connect dialog's own free-text Host field, Remote
// here is a closed choice (ListRemotes' own result), so there was
// previously no way to save a placeholder entry — naming it and
// setting a Target — before a real rclone remote exists to back it,
// the same "prepared, not yet working" state SFTP's own untested Host
// already allows. Mounting one still fails, honestly, exactly the way
// it would for any other nonexistent rclone remote (rclone.DeleteRemote
// already treats this same no-such-remote case as a harmless no-op,
// see its own doc comment — not a real, reported error path, just
// nothing to find and remove).
//
// A short, valid directory-name slug on purpose, not the sentinel's
// own long label — this becomes an actual rclone.Mount argument and a
// MountDir path component (filepath.Join(base, name)), not just
// display text. "N/A" (the user's own first choice) doesn't work here
// for exactly that reason — filepath.Join would read its "/" as a
// path separator, nesting an "N" directory with an "A" inside instead
// of a single "N/A"-named one.
const cloudMountDialogUnconfiguredRemoteName = "uncfg"

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
	// No SetFieldWidth call here — unlike InputField, DropDown.
	// SetFormAttributes never receives a fieldWidth from the Form at
	// all (confirmed directly against tview's own dropdown.go, not
	// guessed), and its own default (GetFieldWidth, width 0) sizes the
	// closed box to its widest *option* string rather than to whatever
	// room the Form actually gives Target/Identifier/Tags. The actual
	// fix needs the dialog's own rect, which doesn't exist yet here —
	// openCloudMountCreateDialog sets it, every open (see its own doc
	// comment for the exact formula, which has to mirror Form.Draw's
	// own vertical-layout math to end at the same column).
	//
	// The real SetSelectedFunc/SetOptions callback is wired by
	// openCloudMountCreateDialog instead, every open — tview's own
	// SetOptions(texts, selected) *replaces* whatever SetSelectedFunc
	// already set (both write the same internal field, confirmed
	// against dropdown.go), so a callback set here would never survive
	// that first call anyway.
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
	// Per-field focused/unfocused coloring is not reachable here:
	// tview.Form.Draw calls every FormItem's own SetFormAttributes with
	// the Form's single fieldStyle on every single frame (confirmed
	// directly against form.go/inputfield.go/dropdown.go, not guessed),
	// which stomps whatever SetFieldBackgroundColor this call makes the
	// very next redraw — true for every Form-based dialog in this app
	// (Connect included, see theme.go's own identical
	// connectForm.SetFieldBackgroundColor(theme.InputFocusedBackground)
	// call), not something specific to this dialog. false still matters
	// for the parts Form.Draw doesn't touch — placeholder style, and
	// the label's own background (see styleInput's own doc comment).
	styleInput(r.cloudMountDialogTargetField, r.theme, false)
	styleInput(r.cloudMountDialogIdentifierField, r.theme, false)
	styleInput(r.cloudMountDialogTagsField, r.theme, false)
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

// cloudMountDialogUniqueUnconfiguredTarget is the sentinel's own
// default-Target prefill, made unique — a real, reported gap: every
// placeholder entry (see cloudMountDialogUnconfiguredRemoteName's own
// doc comment) shares that exact same RemoteName, so
// cloudMountDialogDefaultTarget alone always computed the exact same
// path for every one of them, tripping AddMountEntry's own duplicate-
// Target refusal the moment a second placeholder was saved without
// first hand-editing Target. Unlike a real remote's own default
// (already unique — distinct remote names never collide, and
// openCloudMountCreateDialog's own inUse filter keeps an already-used
// real remote out of the dropdown entirely), nothing else makes this
// one unique on its own, so the suffix is added here instead.
func (r *Root) cloudMountDialogUniqueUnconfiguredTarget() string {
	base := r.cloudMountDialogDefaultTarget(cloudMountDialogUnconfiguredRemoteName)
	configured, err := rclone.LoadMountEntries()
	if err != nil {
		return base
	}
	used := make(map[string]bool, len(configured))
	for _, e := range configured {
		used[e.Target] = true
	}
	if !used[base] {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !used[candidate] {
			return candidate
		}
	}
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
func (r *Root) openCloudMountCreateDialog(preselect string, editing *rclone.MountEntry) {
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
		// editing's own entry is excluded from "in use" — it's using
		// its own remote already, which must not make that remote
		// vanish from its own Remote dropdown.
		if editing != nil && e.Target == editing.Target {
			continue
		}
		inUse[e.RemoteName] = true
	}

	var names []string
	for _, rem := range remotes {
		if !inUse[rem.Name] {
			names = append(names, rem.Name)
		}
	}
	r.cloudMountDialogRemotes = names

	if editing != nil {
		r.cloudMountDialogEditTarget = editing.Target
		if preselect == "" {
			preselect = editing.RemoteName
		}
	} else {
		r.cloudMountDialogEditTarget = ""
	}

	// 100, not 64 — Target's own value is a real local filesystem
	// path (the default alone, ~/.local/share/breakthrough/mounts/
	// <user>/<remote>, already runs well past 64 columns once Target's
	// own label and Form's padding are counted), a real, reported
	// complaint. Same reasoning as the "New cloud remote" dialog's own
	// 90 (see resizeCloudRemoteDialog's doc comment) — this one's
	// longest value, not longest label, is what was actually cramped.
	//
	// Computed before the Remote dropdown is touched below — its own
	// field width needs this dialog's final, clamped width to land on
	// the same column Target/Identifier/Tags do.
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

	// A real, reported complaint: Remote's own closed box fell well
	// short of where Target/Identifier/Tags end, and the open
	// dropdown's own rows didn't match its closed box either. Both
	// trace back to tview.Form.Draw's own vertical-layout formula
	// (confirmed against form.go, not guessed): every item gets the
	// same itemWidth (the Form's inner width) and the same labelWidth
	// (the widest label, +1), so an InputField's own rendered field
	// area is always itemWidth-labelWidth — reproduced here by hand
	// because DropDown never receives that itemWidth from Form at all
	// (see this field's own comment in newCloudMountDialogForm).
	//
	// 2, not some other constant — tview.NewForm's own
	// SetBorderPadding(1, 1, 1, 1), confirmed against form.go.
	innerWidth := width - 2
	maxLabelWidth := 0
	for _, label := range []string{"Remote", "Target", "Identifier", "Tags"} {
		if w := tview.TaggedStringWidth(label); w > maxLabelWidth {
			maxLabelWidth = w
		}
	}
	maxLabelWidth++
	fieldWidth := innerWidth - maxLabelWidth
	if fieldWidth < 10 {
		fieldWidth = 10
	}
	r.cloudMountDialogRemoteField.SetFieldWidth(fieldWidth)

	// Right-padded to fieldWidth, not the bare text — the open
	// dropdown's own popup list is sized from the options' own string
	// length alone (DropDown.Draw's own maxWidth, confirmed against
	// dropdown.go: it never reads fieldWidth at all), the only lever
	// available to make it match the closed box's width instead of
	// whatever the longest real remote name happens to be. Safe to pad
	// every option this way because the callback below — and both
	// read sites in saveCloudMountEntry/runDeleteSelectedCloudRemote —
	// resolve the selected remote by index into r.cloudMountDialogRemotes,
	// never by comparing the dropdown's own (now padded) text.
	rawOptions := append([]string{cloudMountDialogCreateNewOption}, names...)
	options := make([]string, len(rawOptions))
	for i, opt := range rawOptions {
		if w := tview.TaggedStringWidth(opt); w < fieldWidth {
			opt += strings.Repeat(" ", fieldWidth-w)
		}
		options[i] = opt
	}
	r.cloudMountDialogRemoteField.SetOptions(options, func(text string, index int) {
		// Delete makes no sense for the "+ Create new remote…"
		// sentinel (nothing real to delete yet) — rebuild the button
		// row to match, the same "which buttons are in the Flex
		// changes per state" shape setConnectButtonsMode already
		// establishes for the Connect dialog's own two-vs-three button
		// modes. Has to live here, not in newCloudMountDialogForm's own
		// SetSelectedFunc: tview's own SetOptions(texts, selected)
		// replaces whatever SetSelectedFunc already set — both write
		// the same internal field (confirmed against dropdown.go) — so
		// a callback set there would never survive this call anyway.
		r.setCloudMountDialogButtonsMode(index != 0)
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
	switch {
	case editing != nil:
		// The entry's own real values, not the computed default — an
		// edit should show exactly what's already saved, including a
		// Target that was never the default to begin with.
		r.cloudMountDialogTargetField.SetText(editing.Target)
		r.cloudMountDialogIdentifierField.SetText(editing.Identifier)
		r.cloudMountDialogTagsField.SetText(strings.Join(editing.Tags, ", "))
	case selectedIndex > 0:
		r.cloudMountDialogTargetField.SetText(r.cloudMountDialogDefaultTarget(names[selectedIndex-1]))
		r.cloudMountDialogIdentifierField.SetText("")
		r.cloudMountDialogTagsField.SetText("")
	default:
		// cloudMountDialogUniqueUnconfiguredTarget's own default, not
		// "" — leaving Target blank here would make OK silently do
		// nothing again the moment this sentinel is selected
		// (saveCloudMountEntry still refuses an empty Target), the
		// exact dead end this whole placeholder path exists to remove.
		// The unique variant, not the bare default — a real, reported
		// gap: every placeholder shares the same RemoteName, so the
		// bare default always computed the exact same path, tripping
		// AddMountEntry's own duplicate-Target refusal the moment a
		// second placeholder was saved without first hand-editing
		// Target (see cloudMountDialogUniqueUnconfiguredTarget's own
		// doc comment).
		r.cloudMountDialogTargetField.SetText(r.cloudMountDialogUniqueUnconfiguredTarget())
		r.cloudMountDialogIdentifierField.SetText("")
		r.cloudMountDialogTagsField.SetText("")
	}
	r.setCloudMountDialogButtonsMode(selectedIndex != 0)

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
	// Resolved by index into r.cloudMountDialogRemotes, not by reading
	// the dropdown's own current option text back — that text is now
	// right-padded to the field's own width (see
	// openCloudMountCreateDialog's own doc comment on why), which
	// would otherwise end up as a trailing-whitespace-corrupted
	// "remote name" here. index < 1 covers both "nothing selected"
	// (-1) and the "+ Create new remote…" sentinel (0) — saves a
	// placeholder entry instead of refusing outright in that case, see
	// cloudMountDialogUnconfiguredRemoteName's own doc comment on why.
	// Mounting it still fails, honestly, the same as any other
	// MountEntry whose own RemoteName doesn't resolve to a real rclone
	// remote (toggleCloudMount's own real Mount call is the one place
	// that actually finds out, same as it always was).
	index, _ := r.cloudMountDialogRemoteField.GetCurrentOption()
	var remoteName string
	if index < 1 || index-1 >= len(r.cloudMountDialogRemotes) {
		remoteName = cloudMountDialogUnconfiguredRemoteName
	} else {
		remoteName = r.cloudMountDialogRemotes[index-1]
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
	if r.cloudMountDialogEditTarget != "" {
		// Removed before adding, not updated in place — AddMountEntry's
		// own duplicate-Target refusal (see internal/rclone/config.go)
		// would otherwise block saving an edit that leaves Target
		// unchanged (Identifier/Tags-only edits are the common case).
		// This same remove-then-add also covers the Target-did-change
		// case uniformly, with no separate code path needed for it.
		if err := rclone.RemoveMountEntry(r.cloudMountDialogEditTarget); err != nil {
			r.showError(err)
			return rclone.MountEntry{}, false
		}
	}
	if err := rclone.AddMountEntry(entry); err != nil {
		r.showError(err)
		return rclone.MountEntry{}, false
	}
	r.cloudMountDialogEditTarget = ""
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
	// Resolved by index — see saveCloudMountEntry's own identical
	// comment on why (the dropdown's own text is now right-padded).
	index, _ := r.cloudMountDialogRemoteField.GetCurrentOption()
	if index < 1 || index-1 >= len(r.cloudMountDialogRemotes) {
		return
	}
	remoteName := r.cloudMountDialogRemotes[index-1]

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
			r.openCloudMountCreateDialog("", nil)
		},
	)
}
