package ui

import (
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
)

const optionsPage = "options"

// newOptionsList builds the Options overlay: a scrollable list of
// every available color scheme (see config.LoadColorSchemes), the
// currently active one marked — currently the overlay's only content;
// a fuller settings panel is a later addition, not this one. Repopulated
// fresh each time openOptions runs, the same "throwaway, rebuilt on
// open" approach the owner/group picker (r.picker) already uses, rather
// than kept incrementally in sync.
func (r *Root) newOptionsList() *tview.List {
	list := tview.NewList().ShowSecondaryText(false)
	list.SetHighlightFullLine(true)
	list.SetBorderPadding(0, 0, 1, 1)
	list.SetDoneFunc(r.closeOptions) // Escape
	return list
}

// openOptions shows the Options overlay, centered on screen, the
// active scheme's entry pre-selected. r.colorSchemes was loaded once at
// startup (see NewRoot/loadInitialSettings) and isn't re-scanned here —
// a scheme file added to disk after breakthrough started won't appear
// until it's restarted, the simplest correct behavior for now (a "reload
// schemes" action can be added later if that turns out to matter).
func (r *Root) openOptions() {
	r.optionsList.Clear()

	currentIdx := 0
	for i, s := range r.colorSchemes {
		slug := s.Slug // captured per-iteration, not the shared loop variable
		label := s.Theme.Name
		if slug == r.settings.ColorScheme {
			label += " (current)"
			currentIdx = i
		}
		r.optionsList.AddItem(label, "", 0, func() {
			r.applyColorScheme(slug)
			r.closeOptions()
		})
	}

	width, height := listSize(r.optionsList)
	x, y := r.centeredOnScreen(width, height)
	x, y, width, height = r.clampToPanel(x, y, width, height)
	r.optionsList.SetRect(x, y, width, height)
	r.optionsList.SetCurrentItem(currentIdx)

	r.showOverlay(optionsPage, r.optionsList)
}

// closeOptions hides the Options overlay without changing anything
// further — Escape, or picking a scheme (see openOptions, which also
// calls this once the pick itself has been applied).
func (r *Root) closeOptions() {
	r.hideOverlay()
}

// applyColorScheme switches to the color scheme named slug (a
// config.NamedTheme.Slug from r.colorSchemes — see openOptions): applies
// it live (see Root.applyTheme) and persists the choice to the user's own
// config file (see config.SetKey/userConfigFilePath) so it's still active
// the next time breakthrough starts. A failure to persist is reported
// (see Root.showError) but doesn't undo the already-applied live switch
// — the picked scheme is on screen either way, just not guaranteed to
// survive a restart if saving it failed.
func (r *Root) applyColorScheme(slug string) {
	theme := config.FindColorScheme(r.colorSchemes, slug).Resolve()
	r.applyTheme(theme)
	r.settings.ColorScheme = slug
	r.persistSetting("color_scheme", slug)
}
