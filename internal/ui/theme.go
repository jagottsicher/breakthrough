package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"

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
var loadInitialSettings = func() (config.Settings, []config.NamedTheme, []string) {
	settings, warnings := config.Load(config.SystemConfigFile(), config.UserConfigFile())
	schemes := config.LoadColorSchemes(config.SystemColorSchemeDir(), config.UserColorSchemeDir())
	return settings, schemes, warnings
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
	path := userConfigFilePath()
	if path == "" {
		return // no user config tier available (see config.UserDir's own doc comment) — nothing to persist to
	}
	if err := config.SetKey(path, key, value); err != nil {
		r.showError(fmt.Errorf("saving %s: %w", key, err))
	}
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
	r.menu.SetBackgroundColor(theme.AccentBackground)
	r.menu.SetMainTextColor(theme.Text)
	// SelectionBackground (the same turquoise/darkcyan the panel's own
	// current row already highlights with — see Panel.paintStaticChrome)
	// instead of tview.List's own uncustomized default (a plain white
	// background), per the user's own explicit request.
	r.menu.SetSelectedStyle(tcell.StyleDefault.
		Background(theme.SelectionBackground).
		Foreground(theme.Text))
	r.menuTitleBar.SetTextColor(theme.Text)

	r.rename.SetFieldBackgroundColor(theme.AccentBackground)
	r.rename.SetBackgroundColor(theme.AccentBackground)
	r.rename.SetLabelColor(theme.Text)
	r.rename.SetFieldTextColor(theme.Text)

	r.prompt.SetFieldBackgroundColor(theme.AccentBackground)
	r.prompt.SetBackgroundColor(theme.AccentBackground)
	r.prompt.SetLabelColor(theme.Text)
	r.prompt.SetFieldTextColor(theme.Text)

	r.quitConfirm.SetBackgroundColor(theme.AccentBackground)
	r.quitConfirm.SetMainTextColor(theme.Text)

	r.purgeConfirm.SetBackgroundColor(theme.AccentBackground)
	r.purgeConfirm.SetMainTextColor(theme.Text)

	r.picker.SetBackgroundColor(theme.AccentBackground)
	r.picker.SetMainTextColor(theme.Text)

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
	r.bashLine.SetBackgroundColor(theme.AccentBackground)
	r.bashLine.SetTextStyle(tcell.StyleDefault.Foreground(theme.Text).Background(theme.AccentBackground))
	if r.bashLine.HasFocus() {
		r.bashHint.SetBackgroundColor(theme.FocusedBackground)
	} else {
		r.bashHint.SetBackgroundColor(theme.EditableBackground)
	}
	r.bashHint.SetTextColor(theme.PlaceholderText) // a dimmer hint, not primary content — same role PlaceholderText already has elsewhere
	r.buttonBar.SetBackgroundColor(theme.AccentBackground)
	r.buttonBar.SetTextColor(theme.Text)
	r.statusBar.SetBackgroundColor(theme.AccentBackground)
	r.statusBar.SetTextColor(theme.Text)

	// AccentBackground: the shared, constant "normal panel background"
	// every panel floating over the main one now uses (toolWindow/
	// Details' own content areas included — see
	// toolwindow.go/detailssidebar.go), per the user's own explicit
	// request. propertiesTitleBar's own background is set via
	// updateOverlayTitleBarColors below instead, since it now depends on
	// whether Properties is the currently active overlay — see its own
	// doc comment.
	r.propertiesText.SetBackgroundColor(theme.AccentBackground)
	r.propertiesEditField.SetFieldBackgroundColor(theme.FocusedBackground)
	r.propertiesEditField.SetBackgroundColor(theme.FocusedBackground)
	r.propertiesEditField.SetFieldTextColor(theme.Text)
	r.propertiesButtons.SetBackgroundColor(theme.AccentBackground)
	r.propertiesTitleBar.SetTextColor(theme.Text)
	r.propertiesCancelBtn.SetBackgroundColor(theme.AccentBackground)
	r.propertiesCancelBtn.SetLabelColor(theme.Text)
	r.propertiesSaveBtn.SetBackgroundColor(theme.AccentBackground)
	r.propertiesSaveBtn.SetLabelColor(theme.Text)
	r.rerenderProperties() // repaints focusTag's own style tags with the new theme

	if r.optionsList != nil {
		r.optionsList.SetBackgroundColor(theme.AccentBackground)
		r.optionsList.SetMainTextColor(theme.Text)
	}

	if r.searchTop != nil {
		r.searchTop.SetBackgroundColor(theme.AccentBackground)
		r.searchLeft.SetBackgroundColor(theme.AccentBackground)
		r.searchRight.SetBackgroundColor(theme.AccentBackground)
		r.searchEditField.SetFieldBackgroundColor(theme.FocusedBackground)
		r.searchEditField.SetBackgroundColor(theme.FocusedBackground)
		r.searchEditField.SetFieldTextColor(theme.Text)
		r.searchButtons.SetBackgroundColor(theme.AccentBackground)
		r.searchCancelBtn.SetBackgroundColor(theme.AccentBackground)
		r.searchCancelBtn.SetLabelColor(theme.Text)
		r.searchSearchBtn.SetBackgroundColor(theme.AccentBackground)
		r.searchSearchBtn.SetLabelColor(theme.Text)
		r.rerenderSearchDialog() // repaints focusTag/dimTag's own style tags with the new theme
	}

	if r.chmodText != nil {
		r.chmodText.SetBackgroundColor(theme.AccentBackground)
		r.chmodEditField.SetFieldBackgroundColor(theme.FocusedBackground)
		r.chmodEditField.SetBackgroundColor(theme.FocusedBackground)
		r.chmodEditField.SetFieldTextColor(theme.Text)
		r.chmodButtons.SetBackgroundColor(theme.AccentBackground)
		r.chmodCancelBtn.SetBackgroundColor(theme.AccentBackground)
		r.chmodCancelBtn.SetLabelColor(theme.Text)
		r.chmodApplyBtn.SetBackgroundColor(theme.AccentBackground)
		r.chmodApplyBtn.SetLabelColor(theme.Text)
		r.rerenderChmodDialog() // repaints focusTag/dimTag's own style tags with the new theme
	}

	if r.dirPicker != nil {
		r.dirPicker.SetBackgroundColor(theme.AccentBackground)
		r.dirPickerHeader.SetBackgroundColor(theme.AccentBackground)
		r.dirPickerHeader.SetTextColor(theme.Text)
		r.dirPickerList.SetBackgroundColor(theme.AccentBackground)
		r.dirPickerList.SetMainTextColor(theme.Text)
		r.dirPickerSelectBtn.SetBackgroundColor(theme.AccentBackground)
		r.dirPickerSelectBtn.SetLabelColor(theme.Text)
		r.dirPickerCancelBtn.SetBackgroundColor(theme.AccentBackground)
		r.dirPickerCancelBtn.SetLabelColor(theme.Text)
	}

	if r.helpView != nil {
		r.helpView.SetBackgroundColor(theme.AccentBackground)
		r.helpView.SetTextColor(theme.Text)
	}

	if r.viewerView != nil {
		r.viewerView.SetBackgroundColor(theme.AccentBackground)
		r.viewerView.SetTextColor(theme.Text)
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

	r.sedForm.SetBackgroundColor(theme.AccentBackground)
	r.sedForm.SetLabelColor(theme.Text)
	r.sedForm.SetFieldBackgroundColor(theme.FocusedBackground)
	r.sedForm.SetFieldTextColor(theme.Text)
	r.sedFlagsList.SetBackgroundColor(theme.AccentBackground)
	r.sedFlagsList.SetMainTextColor(theme.Text)
	r.sedActions.SetBackgroundColor(theme.AccentBackground)
	r.sedActions.SetMainTextColor(theme.Text)

	r.sedPreviewStatus.SetBackgroundColor(theme.AccentBackground)
	r.sedPreviewStatus.SetTextColor(theme.Text)
	r.sedPreviewTable.SetBackgroundColor(theme.AccentBackground)
	r.sedPreviewActions.SetBackgroundColor(theme.AccentBackground)
	r.sedPreviewActions.SetMainTextColor(theme.Text)

	r.panel.applyTheme(theme)
}
