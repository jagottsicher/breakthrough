package ui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/apikey"
	"github.com/jagottsicher/breakthrough/internal/config"
)

// apiKeyStoreFilename is the encrypted store's own file, inside
// config.UserDir() — alongside connections.json, labels.json and
// every other per-user breakthrough state file, never under the
// system-wide tier (there is no sensible shared passphrase for
// several different logins on one machine to share a single AES key
// under).
const apiKeyStoreFilename = "apikeys.json"

// apiKeyStorePath is where NewRoot opens the encrypted store from —
// "" means no user config directory is available at all (see
// config.UserDir's own doc comment), the same contract
// userConfigFilePath already follows. A swappable var, like
// userConfigFilePath/notifyPersistPath/labelsPersistPath/
// operationsPersistPath, so a test never reads or writes a real
// ~/.config/breakthrough/apikeys.json on the machine running `go
// test` (see bottombar_test.go's own TestMain override) — a real,
// reported gap: a prior test run without this override left a real
// encrypted store sitting in a developer's actual home directory,
// with a passphrase no later test could ever reproduce.
var apiKeyStorePath = func() string {
	userDir := config.UserDir()
	if userDir == "" {
		return ""
	}
	return filepath.Join(userDir, apiKeyStoreFilename)
}

// apiKeyDialogStep is which half of the "store an encrypted API key"
// flow apiKeyDialogLayout is currently showing — see openAPIKeyDialog.
type apiKeyDialogStep int

const (
	// apiKeyDialogStepPassphrase asks for the passphrase that unlocks
	// (or, for a store with no entries at all yet, establishes) the
	// encrypted tier — see rebuildAPIKeyDialogFields's own doc comment
	// for why a brand-new store additionally shows a confirm field.
	apiKeyDialogStepPassphrase apiKeyDialogStep = iota
	// apiKeyDialogStepKey asks for the provider's own API key, once the
	// session is unlocked (either just now, or already from an earlier
	// row this same run — see openAPIKeyDialog).
	apiKeyDialogStepKey
)

// openAPIKeyDialog is the "API key (encrypted)" row's own secretAction
// — see optionCategories' AI Consigliere category. provider is
// whichever one is currently selected (consigliere_provider); this is
// never a per-provider pop-up list, there's exactly one row and it
// always means "the provider you've currently got selected above."
//
// Starts at apiKeyDialogStepKey directly, skipping the passphrase
// prompt entirely, if a passphrase was already unlocked earlier this
// same run (r.apiKeySession.IsUnlocked()) — the same "prompt once per
// session, not once per call" contract ssh-agent/gpg-agent already
// set, and apikey.Session.Unlock's own doc comment promises.
func (r *Root) openAPIKeyDialog(provider string) {
	if r.apiKeySession == nil {
		r.showError(errors.New("no user config directory available — the encrypted API key store needs one to live in"))
		return
	}
	r.apiKeyDialogProvider = provider
	r.setAPIKeyDialogStatus("", r.theme.Text)

	step := apiKeyDialogStepPassphrase
	if r.apiKeySession.IsUnlocked() {
		step = apiKeyDialogStepKey
	}
	r.rebuildAPIKeyDialogFields(step)

	width, height := 56, r.apiKeyDialogFormHeight(step)+4
	_, _, screenWidth, screenHeight := r.GetRect()
	if width > screenWidth-4 {
		width = screenWidth - 4
	}
	if height > screenHeight-4 {
		height = screenHeight - 4
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	r.apiKeyDialogLayout.SetRect(x, y, width, height)
	r.renderAPIKeyDialogTitleBar(width)
	// pushOverlay, not showOverlay: this opens from inside the Options
	// screen, which is itself an overlay (see openOptions) — showOverlay
	// would close it first, so Cancel/Escape on this dialog later had
	// nothing left on the stack to reveal and landed on the panel
	// instead of back on Options (a real, reported bug). The same
	// "layer on top, don't replace" shape openHelp/openOwnerGroupPicker
	// already use for an identical reason.
	r.pushOverlay(apiKeyDialogPage, r.apiKeyDialogLayout, nil)
}

// renderAPIKeyDialogTitleBar mirrors renderConnectTitleBar's own
// "label padded out to width, with a close glyph in the top-right
// corner" shape.
func (r *Root) renderAPIKeyDialogTitleBar(width int) {
	var label string
	switch r.apiKeyDialogStep {
	case apiKeyDialogStepKey:
		label = fmt.Sprintf(" API key: %s ", r.apiKeyDialogProvider)
	default:
		label = " Unlock encrypted API key store "
	}
	closeCol := toolWindowCloseButtonCol(0, width)
	padding := closeCol - tview.TaggedStringWidth(label)
	if padding < 0 {
		padding = 0
	}
	r.apiKeyDialogTitleBar.SetText(label + strings.Repeat(" ", padding) + string(toolWindowCloseGlyph) + " ")
}

func (r *Root) captureAPIKeyDialogTitleBarMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action != tview.MouseLeftDown {
		return action, event
	}
	x, y := event.Position()
	wx, wy, width, _ := r.apiKeyDialogLayout.GetRect()
	if y == wy && x == wx+toolWindowCloseButtonCol(0, width) {
		r.cancelAPIKeyDialog()
		return tview.MouseConsumed, nil
	}
	return action, event
}

// newAPIKeyDialogForm builds the (initially empty) form — its actual
// fields depend on apiKeyDialogStep, so rebuildAPIKeyDialogFields
// fills them in, the same "fixed shell, variable fields" split
// newCloudRemoteDialogForm/rebuildCloudRemoteDialogFields already
// establish for an identical problem (there, which backend is
// selected; here, which step this is).
func (r *Root) newAPIKeyDialogForm() *tview.Form {
	f := tview.NewForm()
	f.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			r.cancelAPIKeyDialog()
			return nil
		}
		return event
	})
	return f
}

func (r *Root) styleAPIKeyDialogForm() {
	r.apiKeyDialogForm.SetBackgroundColor(r.theme.SurfaceBackground)
	r.apiKeyDialogForm.SetLabelColor(r.theme.TextColor)
	r.apiKeyDialogForm.SetFieldBackgroundColor(r.theme.InputFocusedBackground)
	r.apiKeyDialogForm.SetFieldTextColor(r.theme.TextColor)
}

// rebuildAPIKeyDialogFields rebuilds apiKeyDialogForm from scratch for
// step — tview.Form has no clean way to add/remove fields in place
// (see rebuildCloudRemoteDialogFields's own identical reasoning).
//
// apiKeyDialogStepPassphrase shows one masked Passphrase field, plus a
// second masked "Confirm passphrase" field only when the store has no
// entries at all yet (r.apiKeySession.HasAnyEntry() == false): a typo
// in a brand-new passphrase would otherwise silently encrypt the key
// under a passphrase the user can never reproduce, and nothing in
// internal/apikey can detect that after the fact (Session.Unlock
// accepts any passphrase against an empty store, by design — see its
// own doc comment). An existing store's passphrase is instead verified
// against its own ciphertext (see Session.Unlock), so a second
// confirm field there would just be redundant typing.
//
// apiKeyDialogStepKey shows one masked "API key" field — the
// passphrase itself is never asked for again this process (see
// openAPIKeyDialog).
func (r *Root) rebuildAPIKeyDialogFields(step apiKeyDialogStep) {
	r.apiKeyDialogStep = step
	r.apiKeyDialogForm.Clear(true)
	r.styleAPIKeyDialogForm()

	var last *tview.InputField
	switch step {
	case apiKeyDialogStepKey:
		r.apiKeyDialogKeyField = tview.NewInputField().SetLabel("API key")
		r.apiKeyDialogKeyField.SetMaskCharacter('*')
		if r.apiKeySession.HasProvider(r.apiKeyDialogProvider) {
			r.apiKeyDialogKeyField.SetPlaceholder("already stored — type to replace")
		} else {
			r.apiKeyDialogKeyField.SetPlaceholder("required")
		}
		r.apiKeyDialogForm.AddFormItem(r.apiKeyDialogKeyField)
		last = r.apiKeyDialogKeyField
		r.apiKeyDialogConfirmField = nil
	default:
		r.apiKeyDialogPassphraseField = tview.NewInputField().SetLabel("Passphrase")
		r.apiKeyDialogPassphraseField.SetMaskCharacter('*')
		r.apiKeyDialogForm.AddFormItem(r.apiKeyDialogPassphraseField)
		last = r.apiKeyDialogPassphraseField

		if r.apiKeySession.HasAnyEntry() {
			r.apiKeyDialogConfirmField = nil
		} else {
			r.apiKeyDialogConfirmField = tview.NewInputField().SetLabel("Confirm passphrase")
			r.apiKeyDialogConfirmField.SetMaskCharacter('*')
			r.apiKeyDialogForm.AddFormItem(r.apiKeyDialogConfirmField)
			last = r.apiKeyDialogConfirmField
		}
	}

	// Last field's own Tab/Enter would otherwise wrap back to the
	// first one instead of ever reaching apiKeyDialogCancelBtn/
	// apiKeyDialogOKBtn — the same gap newConnectForm's own doc
	// comment already found and fixed there.
	last.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			r.app.SetFocus(r.apiKeyDialogCancelBtn)
			return nil
		case tcell.KeyEnter:
			r.runAPIKeyDialogOK()
			return nil
		}
		return event
	})

	r.resizeAPIKeyDialog()
}

// apiKeyDialogFormHeight is tview.Form's own 2-row top/bottom border
// padding (SetBorderPadding(1,1,1,1), present even without a visible
// border — see newConnectLayout's own doc comment on this exact gap)
// plus one row per field in step, no gap rows needed since there's at
// most two fields and Form's own itemPadding already accounts for
// that between them — verified against a real render, not guessed,
// the same way every other dialog's own fixed height already was.
func (r *Root) apiKeyDialogFormHeight(step apiKeyDialogStep) int {
	fields := 1
	if step == apiKeyDialogStepPassphrase && r.apiKeySession != nil && !r.apiKeySession.HasAnyEntry() {
		fields = 2
	}
	return 2 + fields + (fields - 1)
}

// resizeAPIKeyDialog re-applies apiKeyDialogContent's own form height
// after rebuildAPIKeyDialogFields changes the field count, and
// repositions the whole dialog centered on screen — the same "field
// count changed, re-measure and re-center" step
// resizeCloudRemoteDialog already performs for an identical reason.
func (r *Root) resizeAPIKeyDialog() {
	r.apiKeyDialogContent.ResizeItem(r.apiKeyDialogForm, r.apiKeyDialogFormHeight(r.apiKeyDialogStep), 0)

	width, height := 56, r.apiKeyDialogFormHeight(r.apiKeyDialogStep)+4
	_, _, screenWidth, screenHeight := r.GetRect()
	if width > screenWidth-4 {
		width = screenWidth - 4
	}
	if height > screenHeight-4 {
		height = screenHeight - 4
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	r.apiKeyDialogLayout.SetRect(x, y, width, height)
	r.renderAPIKeyDialogTitleBar(width)
	r.app.SetFocus(r.apiKeyDialogForm)
}

// newAPIKeyDialogButtons mirrors setConnectButtonsMode's own
// connectDialogModeEdit (Cancel/OK, no third button) shape exactly —
// this dialog never has a third action.
func (r *Root) newAPIKeyDialogButtons() *tview.Flex {
	r.apiKeyDialogCancelBtn = tview.NewButton("Cancel").SetSelectedFunc(r.cancelAPIKeyDialog)
	r.apiKeyDialogOKBtn = tview.NewButton("OK").SetSelectedFunc(r.runAPIKeyDialogOK)
	r.apiKeyDialogCancelBtn.SetInputCapture(spaceAlsoActivates(r.cancelAPIKeyDialog))
	r.apiKeyDialogOKBtn.SetInputCapture(spaceAlsoActivates(r.runAPIKeyDialogOK))

	r.apiKeyDialogCancelBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.apiKeyDialogOKBtn)
		case tcell.KeyBacktab:
			r.apiKeyDialogForm.SetFocus(0)
			r.app.SetFocus(r.apiKeyDialogForm)
		case tcell.KeyEscape:
			r.cancelAPIKeyDialog()
		}
	})
	r.apiKeyDialogOKBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.apiKeyDialogForm.SetFocus(0)
			r.app.SetFocus(r.apiKeyDialogForm)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.apiKeyDialogCancelBtn)
		case tcell.KeyEscape:
			r.cancelAPIKeyDialog()
		}
	})

	return tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.apiKeyDialogCancelBtn, 0, 1, false).
		AddItem(r.apiKeyDialogOKBtn, 0, 1, false)
}

// newAPIKeyDialogLayout mirrors newConnectLayout's own title-bar-over-
// content shape.
func (r *Root) newAPIKeyDialogLayout() *tview.Flex {
	r.apiKeyDialogTitleBar = newPlainTitleBar("")
	r.apiKeyDialogTitleBar.SetMouseCapture(r.captureAPIKeyDialogTitleBarMouse)
	r.apiKeyDialogStatus = tview.NewTextView().SetDynamicColors(true)
	r.apiKeyDialogContent = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.apiKeyDialogForm, r.apiKeyDialogFormHeight(apiKeyDialogStepPassphrase), 0, true).
		AddItem(r.apiKeyDialogStatus, 1, 0, false).
		AddItem(r.apiKeyDialogButtons, 1, 0, false)
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.apiKeyDialogTitleBar, 1, 0, false).
		AddItem(r.apiKeyDialogContent, 0, 1, true)
}

func (r *Root) setAPIKeyDialogStatus(text string, color tcell.Color) {
	r.apiKeyDialogStatus.SetTextColor(color)
	r.apiKeyDialogStatus.SetText(text)
}

// cancelAPIKeyDialog closes the dialog, clearing whatever was typed —
// a cancelled passphrase or key never lingers in a hidden widget's own
// text buffer once the dialog is gone.
//
// Always re-renders the Options table on the way out, even though
// nothing may have actually been set: cancelling right after a
// successful Unlock (no new key typed, just confirming an existing
// one) still changed something real — the session is now unlocked —
// and the "API key (encrypted)" row's own status text
// (consigliereAPIKeyStatus) depends on that, so leaving the table
// unrendered would keep showing "not set" until some unrelated
// action happened to redraw it (a real, reported bug).
func (r *Root) cancelAPIKeyDialog() {
	if r.apiKeyDialogPassphraseField != nil {
		r.apiKeyDialogPassphraseField.SetText("")
	}
	if r.apiKeyDialogConfirmField != nil {
		r.apiKeyDialogConfirmField.SetText("")
	}
	if r.apiKeyDialogKeyField != nil {
		r.apiKeyDialogKeyField.SetText("")
	}
	r.hideOverlay()
	r.renderOptions()
	// Cancelling right after a successful Unlock (see this func's own
	// doc comment above) can also have just made an existing stored
	// key usable for the first time this run — Model/Effort's own
	// choices would otherwise stay stuck on just "(default: ...)"
	// until this category is left and re-entered.
	r.refreshConsigliereModelCacheIfSelected()
}

// runAPIKeyDialogOK is the OK button's own action (and Enter in the
// last field's — see rebuildAPIKeyDialogFields): dispatches on
// apiKeyDialogStep, the same per-mode dispatch
// setConnectButtonsMode's own doc comment documents for an identical
// "one dialog, several steps" shape.
func (r *Root) runAPIKeyDialogOK() {
	switch r.apiKeyDialogStep {
	case apiKeyDialogStepPassphrase:
		r.runAPIKeyDialogUnlock()
	case apiKeyDialogStepKey:
		r.runAPIKeyDialogSetKey()
	}
}

// runAPIKeyDialogUnlock validates and submits the passphrase step: a
// blank passphrase or a confirm mismatch is caught here, before ever
// touching apikey.Session, as is Unlock's own ErrWrongPassphrase —
// rendered distinctly from any other failure (see apikey.ErrWrongPassphrase's
// own doc comment on why that distinction matters to the caller).
func (r *Root) runAPIKeyDialogUnlock() {
	passphrase := r.apiKeyDialogPassphraseField.GetText()
	if passphrase == "" {
		r.setAPIKeyDialogStatus("Passphrase is required.", r.theme.CriticalText)
		return
	}
	if r.apiKeyDialogConfirmField != nil && r.apiKeyDialogConfirmField.GetText() != passphrase {
		r.setAPIKeyDialogStatus("Passphrases don't match.", r.theme.CriticalText)
		return
	}

	if err := r.apiKeySession.Unlock(passphrase); err != nil {
		if errors.Is(err, apikey.ErrWrongPassphrase) {
			r.setAPIKeyDialogStatus("Wrong passphrase.", r.theme.CriticalText)
		} else {
			r.setAPIKeyDialogStatus(err.Error(), r.theme.CriticalText)
		}
		return
	}

	r.setAPIKeyDialogStatus("", r.theme.Text)
	r.rebuildAPIKeyDialogFields(apiKeyDialogStepKey)
}

// runAPIKeyDialogSetKey validates and submits the key step: encrypts
// and persists it via apiKeySession.Set, logs that it happened
// (never the key itself — see activitylog.CategoryConsigliere's own
// doc comment), and closes the dialog.
func (r *Root) runAPIKeyDialogSetKey() {
	value := strings.TrimSpace(r.apiKeyDialogKeyField.GetText())
	if value == "" {
		r.setAPIKeyDialogStatus("API key is required.", r.theme.CriticalText)
		return
	}

	if err := r.apiKeySession.Set(r.apiKeyDialogProvider, value); err != nil {
		r.setAPIKeyDialogStatus(err.Error(), r.theme.CriticalText)
		return
	}

	r.activityLog.Action(activitylog.CategoryConsigliere, fmt.Sprintf("stored API key for %s", r.apiKeyDialogProvider))
	r.apiKeyDialogKeyField.SetText("")
	r.hideOverlay()
	r.renderOptions()
	// A key may have just become available for the first time —
	// Model/Effort's own choices would otherwise stay stuck on just
	// "(default: ...)" until this category is left and re-entered
	// (see refreshConsigliereModelCacheIfSelected's own doc comment on
	// why the fetch itself never runs from here directly).
	r.refreshConsigliereModelCacheIfSelected()
}
