package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/batchrename"
)

// Presets on the Batch Rename screen: the whole current pipeline saved
// under a name, and brought back by picking that name from a list — so
// a pipeline built once for a recurring job (camera imports, log
// rotations, a client's naming scheme) is a two-keystroke affair the
// next time, rather than something re-entered from memory. Stored one
// JSON file per preset (see batchrename/preset.go) under the user's
// own config directory, where color schemes already live.

// openBatchRenameSavePreset is the "Save preset..." button: asks for a
// name in the same one-line input the fields use, then writes the
// current rules under it — asking first if a preset of that name
// already exists, since saving over one is also how it's updated and
// shouldn't happen by a slip of the fingers.
func (r *Root) openBatchRenameSavePreset() {
	r.batchRenameInput.SetLabel(" Preset name: ")
	r.batchRenameInput.SetText("")
	r.batchRenameInput.SetAcceptanceFunc(nil)
	r.batchRenameInput.SetDoneFunc(func(key tcell.Key) {
		name := strings.TrimSpace(r.batchRenameInput.GetText())
		r.hideOverlay()
		if key != tcell.KeyEnter || name == "" {
			return
		}
		if err := batchrename.ValidatePresetName(name); err != nil {
			r.showError(fmt.Errorf("save preset: %w", err))
			return
		}
		if batchrename.PresetExists(r.batchRenamePresetDir, name) {
			r.openConfirm(fmt.Sprintf("Replace the existing preset %q?", name), "Yes, replace", func() {
				r.saveBatchRenamePreset(name)
			})
			return
		}
		r.saveBatchRenamePreset(name)
	})

	width, height := 56, 1
	x, y := r.centeredOnScreen(width, height)
	x, y, width, height = r.clampToScreen(x, y, width, height)
	r.batchRenameInput.SetRect(x, y, width, height)
	r.pushOverlay(batchRenameInputPage, r.batchRenameInput, nil)
}

// saveBatchRenamePreset writes the current rules as name, reporting a
// failure through the usual error overlay and success through the
// status line — there's nothing else on screen that changes when a
// preset is saved, so without that the button would seem to do nothing.
func (r *Root) saveBatchRenamePreset(name string) {
	if r.batchRenamePresetDir == "" {
		r.showError(fmt.Errorf("save preset: no user config directory available"))
		return
	}
	if err := batchrename.SavePreset(r.batchRenamePresetDir, name, r.batchRenameRules); err != nil {
		r.showError(fmt.Errorf("save preset: %w", err))
		return
	}
	r.batchRenameStatus.SetText(fmt.Sprintf(" Preset %q saved.", name))
}

// openBatchRenamePresetPicker is the "Load preset..." button: lists
// every saved preset; Enter loads one into the screen (replacing the
// current rules, live preview and all), d deletes one after asking.
// A preset file that can't be read is reported but doesn't stop the
// rest from being listed (see batchrename.LoadPresets).
func (r *Root) openBatchRenamePresetPicker() {
	presets, err := batchrename.LoadPresets(r.batchRenamePresetDir)
	if err != nil {
		r.showError(fmt.Errorf("load presets: %w", err))
	}

	r.batchRenamePresetList.Clear()
	if len(presets) == 0 {
		r.batchRenamePresetList.AddItem("(no presets saved yet)", "", 0, func() { r.hideOverlay() })
	}
	for _, p := range presets {
		preset := p // captured per item, not the shared loop variable
		r.batchRenamePresetList.AddItem(preset.Name, "", 0, func() {
			r.hideOverlay()
			r.applyBatchRenamePreset(preset)
		})
	}

	width, height := listSize(r.batchRenamePresetList)
	if titleWidth := tview.TaggedStringWidth(r.batchRenamePresetTitleBar.GetText(false)); titleWidth > width {
		width = titleWidth
	}
	height++ // the title bar row
	x, y := r.centeredOnScreen(width, height)
	x, y, width, height = r.clampToScreen(x, y, width, height)
	r.batchRenamePresetLayout.SetRect(x, y, width, height)
	// The list's own rect is set to the same area too — captureOutsideClick
	// reads r.activeWidget.GetRect(), and the list is what has focus (see
	// openConfirm's identical note on its own dialog).
	r.batchRenamePresetList.SetRect(x, y, width, height)
	r.batchRenamePresetList.SetCurrentItem(0)
	r.batchRenamePresetList.SetOffset(0, 0)

	r.pushOverlay(batchRenamePresetPage, r.batchRenamePresetList, nil)
}

// applyBatchRenamePreset makes preset the screen's current rules and
// re-renders everything that depends on them — the fields, the step
// marks, the preview.
func (r *Root) applyBatchRenamePreset(preset batchrename.Preset) {
	r.batchRenameRules = preset.Rules
	r.renderBatchRenameFields()
	r.renderBatchRenamePreview()
	r.batchRenameStatus.SetText(r.batchRenameStatus.GetText(false) + fmt.Sprintf(" · preset %q", preset.Name))
}

// captureBatchRenamePresetKey is the picker's own extra key: d (or
// Delete) removes the highlighted preset, after asking — a preset is
// cheap to recreate, but not so cheap that a stray keystroke should
// take it. Escape is the List's own DoneFunc (see newBatchRenameScreen).
func (r *Root) captureBatchRenamePresetKey(event *tcell.EventKey) *tcell.EventKey {
	isDelete := event.Key() == tcell.KeyDelete || (event.Key() == tcell.KeyRune && event.Rune() == 'd')
	if !isDelete {
		return event
	}
	index := r.batchRenamePresetList.GetCurrentItem()
	name, _ := r.batchRenamePresetList.GetItemText(index)
	if !batchrename.PresetExists(r.batchRenamePresetDir, name) {
		return nil // the "(no presets saved yet)" placeholder, or already gone
	}
	r.openConfirm(fmt.Sprintf("Delete the preset %q?", name), "Yes, delete", func() {
		if err := batchrename.DeletePreset(r.batchRenamePresetDir, name); err != nil {
			r.showError(fmt.Errorf("delete preset: %w", err))
		}
		// Rebuild the picker in place so the list reflects the deletion —
		// it's still open underneath the confirmation that just closed.
		r.hideOverlay()
		r.openBatchRenamePresetPicker()
	})
	return nil
}
