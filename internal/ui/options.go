package ui

import (
	"github.com/jagottsicher/breakthrough/internal/config"
)

// optionsPage is the Options screen's own Pages key — the screen itself
// lives in optionsscreen.go, which replaced the bare color-scheme list
// this file used to hold.
const optionsPage = "options"

// applyColorScheme switches to the color scheme named slug (a
// config.NamedTheme.Slug from r.colorSchemes — see the Options screen's
// own scheme picker): applies it live (see Root.applyTheme) and persists
// the choice to the user's own config file (see config.SetKey/
// userConfigFilePath) so it's still active the next time breakthrough
// starts. A failure to persist is reported (see Root.showError) but
// doesn't undo the already-applied live switch — the picked scheme is on
// screen either way, just not guaranteed to survive a restart if saving
// it failed.
//
// The persisting counterpart to applyThemeOnly, which is the same live
// switch *without* writing anything — used for the picker's own
// preview-as-you-browse and for repainting after a scheme file was
// edited (see optionsscreen.go).
func (r *Root) applyColorScheme(slug string) {
	theme := config.FindColorScheme(r.colorSchemes, slug).Resolve()
	r.applyTheme(theme)
	r.settings.ColorScheme = slug
	r.persistSetting("color_scheme", slug)
}
