package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// loadInitialSettings resolves breakthrough's on-disk settings and
// available color schemes at startup (see NewRoot) — a package-level var
// rather than a direct config.Load/LoadColorSchemes call, so tests can
// override it (see TestMain in bottombar_test.go) and stay isolated from
// whatever a real /etc/breakthrough or ~/.config/breakthrough on the
// machine running `go test` might actually contain — the same class of
// problem the bash-history tests solved via a forced HISTFILE (see
// historyFilePath).
// Also returns each setting's own origin (see config.LoadWithOrigins) —
// which tier the effective value actually came from — for the Options
// screen to show alongside each value, and to make its "reset" honest
// (see Root.resetSetting).
var loadInitialSettings = func() (config.Settings, map[string]config.Origin, []config.NamedTheme, []string) {
	settings, origins, warnings := config.LoadWithOrigins(config.SystemConfigFile(), config.UserConfigFile())
	schemes := config.LoadColorSchemes(config.SystemColorSchemeDir(), config.UserColorSchemeDir())
	return settings, origins, schemes, warnings
}

// userConfigFilePath is where a setting change gets persisted (see
// persistSetting) — a separate override point from loadInitialSettings
// itself, so a test that exercises persistence can isolate just the
// write without also having to fake every Load/LoadColorSchemes call.
var userConfigFilePath = config.UserConfigFile

// persistSetting saves one key to the user's config file (see
// config.SetKey/userConfigFilePath) — a color-scheme pick (see
// applyColorScheme) or a "Globals" toggle (see Root.toggleHidden/
// toggleSizeBytes/toggleMtimeUnix) — so breakthrough remembers it across
// restarts instead of always resetting to config.DefaultSettings' own
// built-in default. Best-effort: a failure to persist is reported (see
// showError) but doesn't undo whatever was already applied live — the
// new value is in effect either way, just not guaranteed to survive a
// restart if saving it failed.
func (r *Root) persistSetting(key, value string) {
	if r.suppressPersist {
		// Deliberately applying a value that came *from* disk (a reset,
		// or a reload after an external edit) — writing it straight back
		// would undo the very thing that produced it. See
		// applyWithoutPersisting.
		return
	}
	path := userConfigFilePath()
	if path == "" {
		return // no user config tier available (see config.UserDir's own doc comment) — nothing to persist to
	}
	if err := config.SetKey(path, key, value); err != nil {
		r.showError(fmt.Errorf("saving %s: %w", key, err))
		return
	}
	r.settingOrigins[key] = config.OriginUser
}

// applyWithoutPersisting runs fn with persistSetting suppressed.
//
// Exists for the two places that apply a value which already reflects
// what's on disk: resetting a setting (see resetOptions) and reloading
// after the config file was edited externally (see
// reloadSettingsFromDisk). Both have to put the value into effect
// through the setting's own normal apply path — that's where all the
// live consequences live, from reloading every tab to relabelling the
// context menu — but must not write it back out, which for a reset
// would re-create the exact key the reset just removed.
//
// Restores the previous value rather than clearing the flag outright,
// so nesting can't silently re-enable persistence half way through.
func (r *Root) applyWithoutPersisting(fn func()) {
	previous := r.suppressPersist
	r.suppressPersist = true
	defer func() { r.suppressPersist = previous }()
	fn()
}

// resetSetting is persistSetting's counterpart: it removes key from the
// user's own config file entirely (see config.UnsetKey) rather than
// writing a default value into it, so the effective value falls back
// through the tiers — to a system administrator's setting where one
// exists, and only to the built-in default where none does.
//
// Returns the value now in force, which the caller applies the same way
// it would apply any other change: this only touches what's stored, not
// what's currently running. Deliberately split that way — the "apply a
// value" half is already written once per setting (see
// optionSpec.apply), and reset has no business duplicating it.
func (r *Root) resetSetting(key string) string {
	path := userConfigFilePath()
	if path != "" {
		if err := config.UnsetKey(path, key); err != nil {
			r.showError(fmt.Errorf("resetting %s: %w", key, err))
			return r.effectiveSettingValue(key)
		}
	}

	// Re-read from disk rather than assuming the built-in default: the
	// whole point of removing the key is to find out what the tier below
	// actually has, which only a fresh merge can answer.
	_, origins, _, _ := loadInitialSettings()
	if origin, ok := origins[key]; ok {
		r.settingOrigins[key] = origin
	} else {
		r.settingOrigins[key] = config.OriginDefault
	}
	return r.effectiveSettingValue(key)
}

// effectiveSettingValue is the value key currently has on disk once both
// tiers are merged — what resetSetting hands back for the caller to
// apply. Falls back to the built-in default for an unrecognized key,
// which can't happen through the Options screen (it only ever names
// keys from config.SettingDocs) but is the safe answer regardless.
func (r *Root) effectiveSettingValue(key string) string {
	settings, _, _, _ := loadInitialSettings()
	if value, ok := settingValueByKey(settings, key); ok {
		return value
	}
	if doc, ok := config.FindSettingDoc(key); ok {
		return doc.Default
	}
	return ""
}

// colorTag renders c as the color specifier tview's own "[fg:bg:flags]"
// tag syntax expects (see propertiesBuilder.focusTag) — a "#rrggbb" hex
// value, so a themed color always round-trips exactly rather than
// depending on tview's own tag-color name table agreeing with
// tcell.GetColor's.
func colorTag(c tcell.Color) string {
	return fmt.Sprintf("#%06x", c.Hex())
}

// applyTheme switches every widget Root owns directly to theme, plus
// Properties (via a re-render — its own colors are baked into style tags
// propertiesBuilder.focusTag generates, not looked up live) and the
// panel (see Panel.applyTheme, which additionally reloads to repaint
// each row's own cell colors). Called once from NewRoot with whatever
// was loaded from disk (see loadInitialSettings), and again by
// applyColorScheme whenever the Options overlay picks a different
// scheme.
func (r *Root) applyTheme(theme config.ResolvedTheme) {
	r.theme = theme
	r.updateOverlayTitleBarColors() // propertiesTitleBar/menuTitleBar — see its own doc comment

	// AccentBackground: the shared, constant "normal panel background"
	// every panel now uses (see propertiesText's own comment below for
	// the full reasoning) — menuTitleBar's own background is set via
	// updateOverlayTitleBarColors below instead, since — per the user's
	// own explicit request — it now depends on whether the context menu
	// is the currently active overlay, the same as propertiesTitleBar.
	styleList(r.menu, theme)
	r.menuTitleBar.SetTextColor(theme.Text)

	// FocusedBackground, not AccentBackground: rename/prompt are always
	// the one thing accepting keystrokes for as long as they're shown at
	// all (both are modal, single-field overlays — see openRename/
	// openPrompt), the same "always the active input" reasoning
	// headerEdit's own comment gives above, per the user's own explicit
	// request that every input field in the app follow this same
	// convention consistently.
	styleInput(r.rename, theme, true)
	r.rename.SetLabelColor(theme.TextColor)

	styleInput(r.prompt, theme, true)
	r.prompt.SetLabelColor(theme.TextColor)

	// FocusedBackground, fixed rather than active/inactive-dependent the
	// way propertiesTitleBar/menuTitleBar are (see updateOverlayTitleBarColors):
	// quitConfirm/confirmDialog are single-layer, always-modal blocking
	// dialogs, never themselves the base a further overlay stacks on top
	// of the way Properties/Menu/Help can be — the same reasoning
	// optionsTitleBar's own fixed FocusedBackground already follows.
	styleList(r.quitConfirm, theme)
	r.quitConfirmTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.quitConfirmTitleBar.SetTextColor(theme.TextColor)

	styleList(r.confirmDialog, theme)
	r.confirmDialogTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.confirmDialogTitleBar.SetTextColor(theme.TextColor)

	// styleList(r.pasteConflictDialog, ...) and the title bar's own
	// coloring were both simply missing before — unlike every other List
	// in this app, this one was never themed at all. FocusedBackground,
	// fixed rather than active/inactive-dependent: this dialog is a
	// single-layer, always-modal overlay nothing else ever stacks on top
	// of, the same reasoning quitConfirm/confirmDialog's own (still
	// title-bar-less on this branch) styling already follows.
	styleList(r.pasteConflictDialog, theme)
	r.pasteConflictDialogTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.pasteConflictDialogTitleBar.SetTextColor(theme.TextColor)

	styleList(r.picker, theme)

	r.errorView.SetTextColor(theme.Text)
	r.errorView.SetBackgroundColor(theme.ErrorBackground)

	// AccentBackground: the "Bash Prompt Editor" (see
	// bashconsole.go/bashHintText) is one of the panels the user's own
	// explicit request named directly — its content area (bashLine) gets
	// the same shared, constant "normal panel background" every other
	// one has now. bashHint just below is its title bar equivalent,
	// shown only while bashLine is expanded/focused — re-derive its
	// color from bashLine's own current focus state (see
	// expandBashConsole/collapseBashConsole) rather than resetting to
	// one fixed color unconditionally, the same live-color-scheme-switch
	// hazard detailsTitleBar's own comment below documents.
	r.bashLine.SetBackgroundColor(theme.SurfaceBackground)
	r.bashLine.SetTextStyle(tcell.StyleDefault.Foreground(theme.TextColor).Background(theme.SurfaceBackground))
	if r.bashLine.HasFocus() {
		r.bashHint.SetBackgroundColor(theme.InputFocusedBackground)
	} else {
		r.bashHint.SetBackgroundColor(theme.InputBackground)
	}
	r.bashHint.SetTextColor(theme.MutedTextColor)
	r.buttonBar.SetBackgroundColor(theme.SurfaceBackground)
	r.buttonBar.SetTextColor(theme.TextColor)
	r.statusBar.SetBackgroundColor(theme.SurfaceBackground)
	r.statusBar.SetTextColor(theme.TextColor)

	// AccentBackground: the shared, constant "normal panel background"
	// every panel floating over the main one now uses (toolWindow/
	// Details' own content areas included — see
	// toolwindow.go/detailssidebar.go), per the user's own explicit
	// request. propertiesTitleBar's own background is set via
	// updateOverlayTitleBarColors below instead, since it now depends on
	// whether Properties is the currently active overlay — see its own
	// doc comment.
	r.propertiesText.SetBackgroundColor(theme.SurfaceBackground)
	styleInput(r.propertiesEditField, theme, true)
	r.propertiesButtons.SetBackgroundColor(theme.SurfaceBackground)
	r.propertiesTitleBar.SetTextColor(theme.TextColor)
	styleButton(r.propertiesCancelBtn, theme)
	styleButton(r.propertiesSaveBtn, theme)
	r.rerenderProperties() // repaints focusTag's own style tags with the new theme

	// The Options screen (see optionsscreen.go). Guarded because
	// applyTheme also runs from NewRoot, before newOptionsScreen has
	// built any of these.
	if r.optionsCategories != nil {
		styleList(r.optionsCategories, theme)

		r.optionsLayout.SetBackgroundColor(theme.SurfaceBackground)
		r.optionsButtons.SetBackgroundColor(theme.SurfaceBackground)

		r.optionsTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
		r.optionsTitleBar.SetTextColor(theme.TextColor)
		r.optionsHint.SetBackgroundColor(theme.InputBackground)
		r.optionsHint.SetTextColor(theme.MutedTextColor)

		r.optionsTable.SetBackgroundColor(theme.SurfaceBackground)

		r.optionsInfo.SetBackgroundColor(theme.InputBackground)
		r.optionsInfo.SetTextColor(theme.TextColor)

		styleInput(r.optionsInput, theme, true)
		r.optionsInput.SetLabelColor(theme.TextColor)

		for _, b := range r.optionsButtonList() {
			styleButton(b, theme)
		}

		// Last, and deliberately after styleList above: that sets one
		// fixed FocusedBackground selection color, which is right for
		// every other list in this app but would erase the two panes'
		// own focus-dependent highlight (see setOptionsPaneFocused) —
		// a real bug, caught by reading the drawn colors back off a
		// screen. Re-derived from each pane's actual focus, which is
		// trustworthy here: applyTheme is never called from inside a
		// blur callback, the one place HasFocus lies.
		r.setOptionsPaneFocused(r.optionsCategories, r.optionsCategories.HasFocus())
		r.setOptionsPaneFocused(r.optionsTable, r.optionsTable.HasFocus())

		// Re-render: the table's own cell colors are baked in per cell
		// (see renderOptions), not looked up live at draw time.
		r.renderOptions()
	}

	// The Batch Rename screen (see batchrename.go) — guarded the same
	// way the Options block above is (applyTheme also runs from
	// NewRoot, before newBatchRenameScreen has built any of these).
	if r.batchRenameStepsList != nil {
		styleList(r.batchRenameStepsList, theme)

		r.batchRenameLayout.SetBackgroundColor(theme.SurfaceBackground)
		r.batchRenameButtons.SetBackgroundColor(theme.SurfaceBackground)

		r.batchRenameTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
		r.batchRenameTitleBar.SetTextColor(theme.TextColor)
		r.batchRenameHint.SetBackgroundColor(theme.InputBackground)
		r.batchRenameHint.SetTextColor(theme.MutedTextColor)

		r.batchRenameFieldsTable.SetBackgroundColor(theme.SurfaceBackground)
		r.batchRenamePreviewTable.SetBackgroundColor(theme.SurfaceBackground)
		r.batchRenameStatus.SetBackgroundColor(theme.SurfaceBackground)
		r.batchRenameStatus.SetTextColor(theme.TextColor)

		styleInput(r.batchRenameInput, theme, true)
		r.batchRenameInput.SetLabelColor(theme.TextColor)

		for _, b := range r.batchRenameButtonList() {
			styleButton(b, theme)
		}

		// Last, and deliberately after styleList above — see
		// applyTheme's own identical comment on the Options screen's
		// two panes just above for why.
		r.setOptionsPaneFocused(r.batchRenameStepsList, r.batchRenameStepsList.HasFocus())
		r.setOptionsPaneFocused(r.batchRenameFieldsTable, r.batchRenameFieldsTable.HasFocus())

		// Re-render: both tables' own cell colors are baked in per cell
		// at render time, not looked up live at draw time.
		r.renderBatchRenameFields()
		r.renderBatchRenamePreview()
	}

	if r.searchTop != nil {
		r.searchTop.SetBackgroundColor(theme.SurfaceBackground)
		r.searchLeft.SetBackgroundColor(theme.SurfaceBackground)
		r.searchRight.SetBackgroundColor(theme.SurfaceBackground)
		styleInput(r.searchEditField, theme, true)
		r.searchButtons.SetBackgroundColor(theme.SurfaceBackground)
		styleButton(r.searchCancelBtn, theme)
		styleButton(r.searchSearchBtn, theme)
		// FocusedBackground, fixed — Search is a single-layer, always-modal
		// dialog nothing else ever stacks on top of, the same reasoning
		// confirmDialogTitleBar/chmodTitleBar/sedTitleBar already follow.
		r.searchTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
		r.searchTitleBar.SetTextColor(theme.TextColor)
		r.rerenderSearchDialog() // repaints focusTag/dimTag's own style tags with the new theme
	}

	if r.chmodText != nil {
		r.chmodText.SetBackgroundColor(theme.SurfaceBackground)
		styleInput(r.chmodEditField, theme, true)
		r.chmodButtons.SetBackgroundColor(theme.SurfaceBackground)
		// FocusedBackground, fixed — chmod is a single-layer, always-modal
		// dialog nothing else ever stacks on top of, the same reasoning
		// optionsTitleBar's own fixed FocusedBackground already follows
		// (see confirmDialogTitleBar/quitConfirmTitleBar just above for
		// the identical choice on the same grounds).
		r.chmodTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
		r.chmodTitleBar.SetTextColor(theme.TextColor)
		styleButton(r.chmodCancelBtn, theme)
		styleButton(r.chmodApplyBtn, theme)
		r.rerenderChmodDialog() // repaints focusTag/dimTag's own style tags with the new theme
	}

	if r.dirPicker != nil {
		r.dirPicker.SetBackgroundColor(theme.SurfaceBackground)
		r.dirPickerHeader.SetBackgroundColor(theme.SurfaceBackground)
		r.dirPickerHeader.SetTextColor(theme.TextColor)
		styleList(r.dirPickerList, theme)
		styleButton(r.dirPickerSelectBtn, theme)
		styleButton(r.dirPickerCancelBtn, theme)
	}

	if r.helpView != nil {
		r.helpView.SetBackgroundColor(theme.SurfaceBackground)
		r.helpView.SetTextColor(theme.TextColor)
	}
	if r.helpTitleBar != nil {
		// Background is set via updateOverlayTitleBarColors above
		// instead, since it depends on whether Help is currently the
		// active overlay, the same as propertiesTitleBar/menuTitleBar.
		r.helpTitleBar.SetTextColor(theme.Text)
	}

	if r.viewerView != nil {
		r.viewerView.SetBackgroundColor(theme.SurfaceBackground)
		r.viewerView.SetTextColor(theme.TextColor)
	}

	if r.detailsSidebar != nil {
		// Always AccentBackground regardless of focus (see
		// newDetailsSidebarView's own doc comment) — unlike
		// detailsTitleBar just below, this one never needs to check
		// current focus state at all: it doesn't have a second state to
		// preserve.
		r.detailsSidebar.SetBackgroundColor(theme.AccentBackground)
		r.detailsSidebar.SetTextColor(theme.Text)
	}
	if r.detailsTitleBar != nil {
		// Unlike detailsSidebar above, this one DOES have a focus-
		// dependent state (see newDetailsTitleBar's own doc comment) —
		// re-derive it from whichever theme.* color that state actually
		// maps to right now, rather than always resetting to the
		// unfocused look the way an unconditional EditableBackground
		// here would (a real, visible bug: switching color schemes while
		// Details has focus would otherwise show the wrong one until
		// the next blur/focus cycle).
		if r.detailsSidebar.HasFocus() {
			r.detailsTitleBar.SetBackgroundColor(theme.FocusedBackground)
		} else {
			r.detailsTitleBar.SetBackgroundColor(theme.EditableBackground)
		}
		r.detailsTitleBar.SetTextColor(theme.Text)
	}

	r.sedForm.SetBackgroundColor(theme.SurfaceBackground)
	r.sedForm.SetLabelColor(theme.TextColor)
	r.sedForm.SetFieldBackgroundColor(theme.InputFocusedBackground)
	r.sedForm.SetFieldTextColor(theme.TextColor)
	styleList(r.sedFlagsList, theme)
	styleList(r.sedActions, theme)
	// FocusedBackground, fixed — Sed Replace/Preview are single-layer,
	// always-modal dialogs nothing else ever stacks on top of, the same
	// reasoning confirmDialogTitleBar/quitConfirmTitleBar/chmodTitleBar
	// already follow.
	r.sedTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.sedTitleBar.SetTextColor(theme.TextColor)

	r.duplicateForm.SetBackgroundColor(theme.SurfaceBackground)
	r.duplicateForm.SetLabelColor(theme.TextColor)
	r.duplicateForm.SetFieldBackgroundColor(theme.InputFocusedBackground)
	r.duplicateForm.SetFieldTextColor(theme.TextColor)
	// See themeDuplicateDropDown's own doc comment (duplicate.go) for why
	// both dropdowns need this explicit call at all: SetFormAttributes
	// (what the two lines above actually drive, applied fresh every
	// Draw) only ever reaches DropDown's own fieldStyle, never its
	// separate focusedStyle, and never its open popup list's own colors
	// either — a color-scheme switch while Multiply happens to be open
	// would otherwise leave either dropdown showing stale colors, or the
	// popup showing tview's own stock palette, indefinitely.
	// duplicateDateTimeTypeField only exists while the Date/time
	// strategy is the one currently selected (see renderDuplicateForm).
	if r.duplicateStrategyField != nil {
		r.themeDuplicateDropDown(r.duplicateStrategyField)
	}
	if r.duplicateDateTimeTypeField != nil {
		r.themeDuplicateDropDown(r.duplicateDateTimeTypeField)
	}
	// The Date/time format field's own disabled ("Unix timestamp") look
	// is set directly in renderDuplicateDateTimeFields, not by Form's
	// own generic pass above (which would otherwise fight it back to
	// the ordinary editable field colors) — refreshed here too, for the
	// same reason the other two dropdowns are: a color-scheme switch
	// while it's showing must not leave it in the previous scheme's dim
	// color indefinitely.
	if r.duplicateDateTimeFormatField != nil && r.duplicateDateTimeFormatType == duplicateDateTimeTypeUnix {
		styleDisabledInput(r.duplicateDateTimeFormatField, theme)
	}
	r.duplicatePreviewView.SetBackgroundColor(theme.SurfaceBackground)
	r.duplicatePreviewView.SetTextColor(theme.TextColor)
	r.duplicateSpacer.SetBackgroundColor(theme.SurfaceBackground)
	r.duplicateButtons.SetBackgroundColor(theme.SurfaceBackground)
	styleButton(r.duplicateCancelBtn, theme)
	styleButton(r.duplicateApplyBtn, theme)
	// FocusedBackground, fixed — Multiply is a single-layer, always-modal
	// dialog nothing else ever stacks on top of, the same reasoning
	// confirmDialogTitleBar/chmodTitleBar/sedTitleBar already follow.
	r.duplicateTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.duplicateTitleBar.SetTextColor(theme.TextColor)

	r.sedPreviewStatus.SetBackgroundColor(theme.SurfaceBackground)
	r.sedPreviewStatus.SetTextColor(theme.TextColor)
	r.sedPreviewTable.SetBackgroundColor(theme.SurfaceBackground)
	r.sedPreviewTable.SetSelectedStyle(tcell.StyleDefault.
		Background(theme.SelectionBackground).
		Foreground(theme.TextColor))
	styleList(r.sedPreviewActions, theme)
	r.sedPreviewTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.sedPreviewTitleBar.SetTextColor(theme.TextColor)

	r.tabSwitcher.SetBackgroundColor(theme.SurfaceBackground)
	r.tabSwitcher.SetSelectedStyle(tcell.StyleDefault.
		Background(theme.SelectionBackground).
		Foreground(theme.TextColor))
	r.tabSwitcherTitleBar.SetBackgroundColor(theme.SurfaceBackground)
	r.tabSwitcherTitleBar.SetTextColor(theme.Text)

	// AccentBackground, the same as tabSwitcherTitleBar just above, not
	// the FocusedBackground every centered, always-modal dialog's own
	// title bar (confirmDialog/chmod/Sed Replace/Search, ...) uses: this
	// is a small dropdown anchored under a header button, closer in
	// spirit to the tab switcher than to a full dialog.
	r.filterMenuTitleBar.SetBackgroundColor(theme.SurfaceBackground)
	r.filterMenuTitleBar.SetTextColor(theme.Text)

	// Every tab, not just the visible one: a color scheme is as global as
	// a setting gets, and a background tab still holding the old palette
	// would repaint jarringly the moment it was switched to. See
	// Root.forEachTab's own doc comment.
	r.forEachTab(func(p *Panel) { p.applyTheme(theme) })
}

// styleButton applies this app's own button look — ButtonBackground
// while it doesn't have real keyboard focus, FocusedBackground while it
// does, white text either way — to a real tview.Button (Cancel/Save/
// Apply/Select/Find, filterRegexBtn, ...). A package-level function
// taking theme explicitly, rather than a Root method, since
// Panel.paintStaticChrome (filterRegexBtn's own repaint site) has no
// Root to call through — only its own p.theme.
//
// Per the user's own explicit request for a lighter turquoise than
// FocusedBackground here — which surfaced a real, previously-unnoticed
// bug in the process: a plain SetBackgroundColor/SetLabelColor call
// (every button here used before this) has actually never done
// anything, for any button, ever — verified directly against tview's
// own button.go, not guessed: Button.Draw recomputes its own displayed
// background/foreground from its internal style/activatedStyle fields
// on every single Draw, discarding whatever SetBackgroundColor set
// moments earlier. SetStyle/SetActivatedStyle are the ones that
// actually reach the screen.
func styleButton(b *tview.Button, theme config.ResolvedTheme) {
	b.SetStyle(tcell.StyleDefault.Background(theme.ButtonBackground).Foreground(theme.Text))
	b.SetActivatedStyle(tcell.StyleDefault.Background(theme.ButtonFocusedBackground).Foreground(theme.Text))
}

// styleList applies this app's own list look — AccentBackground overall,
// white text, FocusedBackground on whichever item is currently selected
// — to a real tview.List (the context menu, quit/purge confirmation,
// the owner/group and directory pickers, Options, Sed Replace's own
// flag/action lists, ...). Per the user's own explicit request to apply
// the context menu's own selection fix (see its own PR) everywhere a
// list exists: every one of these was still showing tview.List's own
// entirely uncustomized selected-item look (a plain white background)
// — the same real, previously-unnoticed gap styleButton's own doc
// comment documents for buttons, just for List instead of Button.
func styleList(l *tview.List, theme config.ResolvedTheme) {
	l.SetBackgroundColor(theme.SurfaceBackground)
	l.SetMainTextColor(theme.Text)
	l.SetSelectedStyle(tcell.StyleDefault.Background(theme.SelectionBackground).Foreground(theme.Text))
}

// styleInput applies the shared normal/focused input contract. tview keeps
// placeholder styling separate from the field style, so all three are set
// here rather than being left to each dialog to interpret independently.
func styleInput(field *tview.InputField, theme config.ResolvedTheme, focused bool) {
	background := theme.InputBackground
	if focused {
		background = theme.InputFocusedBackground
	}
	field.SetFieldBackgroundColor(background)
	field.SetBackgroundColor(background)
	field.SetFieldTextColor(theme.TextColor)
	field.SetPlaceholderStyle(tcell.StyleDefault.Background(background).Foreground(theme.MutedTextColor))
}

// styleDisabledInput gives disabled fields a distinct, readable state rather
// than treating them as ordinary inputs with a dimmed foreground only.
func styleDisabledInput(field *tview.InputField, theme config.ResolvedTheme) {
	field.SetFieldBackgroundColor(theme.InputDisabledBackground)
	field.SetBackgroundColor(theme.InputDisabledBackground)
	field.SetFieldTextColor(theme.MutedTextColor)
	field.SetPlaceholderStyle(tcell.StyleDefault.Background(theme.InputDisabledBackground).Foreground(theme.MutedTextColor))
}

// styleDropDown keeps the closed field and its popup list on the same palette
// as every other input and list in the application.
func styleDropDown(dropdown *tview.DropDown, theme config.ResolvedTheme) {
	normal := tcell.StyleDefault.Background(theme.InputBackground).Foreground(theme.TextColor)
	focused := tcell.StyleDefault.Background(theme.InputFocusedBackground).Foreground(theme.TextColor)
	dropdown.SetFieldStyle(normal)
	dropdown.SetFocusedStyle(focused)
	dropdown.SetListStyles(
		tcell.StyleDefault.Background(theme.PopupBackground).Foreground(theme.TextColor),
		tcell.StyleDefault.Background(theme.SelectionBackground).Foreground(theme.TextColor),
	)
}
