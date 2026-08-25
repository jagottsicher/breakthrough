package ui

import (
	"strconv"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// isolateInitialSettings overrides loadInitialSettings (see theme.go) to
// return settings/schemes/nil-warnings for the duration of t, restoring
// the previous override (TestMain's own fixed-default one) afterward —
// the same save/restore-via-t.Cleanup shape as isolateUserConfigFile,
// for tests that need NewRoot to see specific schemes rather than just
// the "default" one TestMain's override provides.
func isolateInitialSettings(t *testing.T, settings config.Settings, schemes []config.NamedTheme) {
	t.Helper()
	original := loadInitialSettings
	loadInitialSettings = func() (config.Settings, []config.NamedTheme, []string) {
		return settings, schemes, nil
	}
	t.Cleanup(func() { loadInitialSettings = original })
}

// solarizedTheme is a minimal non-default scheme used throughout this
// file — just distinct enough from config.DefaultTheme() (a different
// AccentBackground) to prove a scheme switch actually took effect,
// without needing every field filled in (Resolve fills the rest from
// DefaultTheme — see its own doc comment).
func solarizedTheme() config.Theme {
	return config.Theme{Name: "Solarized", AccentBackground: "#002b36"}
}

func TestOpenOptionsListsSchemesWithCurrentMarked(t *testing.T) {
	schemes := []config.NamedTheme{
		{Slug: "default", Theme: config.DefaultTheme()},
		{Slug: "solarized", Theme: solarizedTheme()},
	}
	isolateInitialSettings(t, config.Settings{ColorScheme: "solarized", Language: "en"}, schemes)

	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openOptions()
	if r.activePage != optionsPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, optionsPage)
	}
	if got, want := r.optionsList.GetItemCount(), len(schemes); got != want {
		t.Fatalf("optionsList has %d items, want %d", got, want)
	}

	current := r.optionsList.GetCurrentItem()
	label, _ := r.optionsList.GetItemText(current)
	if label != "Solarized (current)" {
		t.Errorf("current item label = %q, want %q", label, "Solarized (current)")
	}
}

func TestApplyColorSchemeAppliesLiveAndPersists(t *testing.T) {
	schemes := []config.NamedTheme{
		{Slug: "default", Theme: config.DefaultTheme()},
		{Slug: "solarized", Theme: solarizedTheme()},
	}
	isolateInitialSettings(t, config.DefaultSettings(), schemes)
	path := isolateUserConfigFile(t)

	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.applyColorScheme("solarized")

	wantTheme := solarizedTheme().Resolve()
	if r.theme.AccentBackground != wantTheme.AccentBackground {
		t.Errorf("r.theme.AccentBackground = %v, want %v (applied live)", r.theme.AccentBackground, wantTheme.AccentBackground)
	}
	if r.menu.GetBackgroundColor() != wantTheme.AccentBackground {
		t.Errorf("r.menu background = %v, want %v (applyTheme should have repainted it)", r.menu.GetBackgroundColor(), wantTheme.AccentBackground)
	}
	if r.settings.ColorScheme != "solarized" {
		t.Errorf("r.settings.ColorScheme = %q, want %q", r.settings.ColorScheme, "solarized")
	}

	values, _, err := config.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile(%q): %v", path, err)
	}
	if values["color_scheme"] != "solarized" {
		t.Errorf("persisted color_scheme = %q, want %q, file contents: %v", values["color_scheme"], "solarized", values)
	}
}

// TestApplyColorSchemeUnknownSlugFallsBackToDefault pins
// FindColorScheme's own fallback (see its doc comment) end to end
// through applyColorScheme — e.g. a config still referencing a scheme
// file that's since been deleted.
func TestApplyColorSchemeUnknownSlugFallsBackToDefault(t *testing.T) {
	isolateUserConfigFile(t)

	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.applyColorScheme("does-not-exist")

	want := config.DefaultTheme().Resolve()
	if r.theme != want {
		t.Errorf("r.theme = %+v, want DefaultTheme().Resolve() = %+v", r.theme, want)
	}
}

// TestNewRootRestoresGlobalsFromSettings pins the user's own request:
// breakthrough starts up with the "Globals" toggles (showHidden/
// sizeBytes/mtimeUnix) it last saved, not always config.DefaultSettings'
// own built-in default.
func TestNewRootRestoresGlobalsFromSettings(t *testing.T) {
	isolateInitialSettings(t, config.Settings{
		ColorScheme: "default",
		Language:    "en",
		ShowHidden:  false,
		SizeBytes:   true,
		MtimeUnix:   true,
	}, config.LoadColorSchemes("", ""))

	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	if r.panel.showHidden {
		t.Error("panel.showHidden = true, want false (restored from settings)")
	}
	if !r.panel.sizeBytes {
		t.Error("panel.sizeBytes = false, want true (restored from settings)")
	}
	if !r.panel.mtimeUnix {
		t.Error("panel.mtimeUnix = false, want true (restored from settings)")
	}
}

// TestGlobalsTogglesPersist pins that each of the three "Globals"
// toggles saves its new value to the user's config file (see
// persistSetting), the same as a color-scheme pick already does (see
// TestApplyColorSchemeAppliesLiveAndPersists) — so it's still in effect
// next time breakthrough starts (see TestNewRootRestoresGlobalsFromSettings).
func TestGlobalsTogglesPersist(t *testing.T) {
	path := isolateUserConfigFile(t)

	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.toggleHidden()
	r.toggleSizeBytes()
	r.toggleMtimeUnix()

	values, _, err := config.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile(%q): %v", path, err)
	}
	want := map[string]string{
		"show_hidden": strconv.FormatBool(r.panel.showHidden),
		"size_bytes":  strconv.FormatBool(r.panel.sizeBytes),
		"mtime_unix":  strconv.FormatBool(r.panel.mtimeUnix),
	}
	for key, wantValue := range want {
		if values[key] != wantValue {
			t.Errorf("persisted %s = %q, want %q, file: %v", key, values[key], wantValue, values)
		}
	}
}

// TestApplyColorSchemeWithNoUserConfigTierDoesNotError pins
// applyColorScheme's own documented behavior when userConfigFilePath
// returns "" (see config.UserDir's doc comment on when that happens):
// the live switch still applies, just without persisting — no error is
// reported for having nothing to persist to.
func TestApplyColorSchemeWithNoUserConfigTierDoesNotError(t *testing.T) {
	original := userConfigFilePath
	userConfigFilePath = func() string { return "" }
	t.Cleanup(func() { userConfigFilePath = original })

	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.applyColorScheme("default")
	if r.activePage == errorPage {
		t.Errorf("applyColorScheme should not report an error just because there's no user config tier, got: %q", r.errorView.GetText(true))
	}
}

func TestOpenOptionsCancelRunsClosesOverlay(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openOptions()
	r.optionsList.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.activePage != "" {
		t.Errorf("activePage = %q after Escape, want closed", r.activePage)
	}
}

func TestOptionsListPickAppliesAndCloses(t *testing.T) {
	schemes := []config.NamedTheme{
		{Slug: "default", Theme: config.DefaultTheme()},
		{Slug: "solarized", Theme: solarizedTheme()},
	}
	isolateInitialSettings(t, config.DefaultSettings(), schemes)
	isolateUserConfigFile(t)

	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openOptions()
	// "solarized" is the second item — see isolateInitialSettings' fixed
	// schemes slice above (LoadColorSchemes-style slug ordering, sorted).
	r.optionsList.SetCurrentItem(1)
	r.optionsList.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.settings.ColorScheme != "solarized" {
		t.Errorf("ColorScheme = %q after picking it, want %q", r.settings.ColorScheme, "solarized")
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q after picking a scheme, want closed", r.activePage)
	}
}

// TestOptionsShortcutRespectsGuard is OptionsShortcut's own
// TestToggleHiddenShortcutRespectsGuard-style pin: Ctrl+O only opens
// Options while acceptsGlobalShortcut's guard passes.
func TestOptionsShortcutRespectsGuard(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.app.SetFocus(r.bashLine)
	r.OptionsShortcut()
	if r.activePage == optionsPage {
		t.Error("OptionsShortcut should no-op while the bash line has focus")
	}

	r.app.SetFocus(r.panel)
	r.OptionsShortcut()
	if r.activePage != optionsPage {
		t.Errorf("activePage = %q after OptionsShortcut with the guard passing, want %q", r.activePage, optionsPage)
	}
}

func TestColorTagFormatsHex(t *testing.T) {
	if got, want := colorTag(tcell.NewRGBColor(0x11, 0x22, 0x33)), "#112233"; got != want {
		t.Errorf("colorTag(#112233) = %q, want %q", got, want)
	}
}

func TestApplyThemeRepaintsPanel(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	custom := solarizedTheme().Resolve()
	r.applyTheme(custom)

	if r.panel.theme != custom {
		t.Errorf("r.panel.theme = %+v, want %+v — applyTheme should propagate to the panel", r.panel.theme, custom)
	}
	if r.panel.header.GetBackgroundColor() != custom.AccentBackground {
		t.Errorf("r.panel.header background = %v, want %v", r.panel.header.GetBackgroundColor(), custom.AccentBackground)
	}
}

// TestOptionsPersistDoesNotClobberOtherKeys pins that applyColorScheme's
// write (see config.SetKey) preserves a language key already present in
// the user config file — the same "preserve everything else" contract
// config.SetKey's own tests already pin at the config-package level,
// exercised here end to end through the UI action that actually calls it.
func TestOptionsPersistDoesNotClobberOtherKeys(t *testing.T) {
	path := isolateUserConfigFile(t)
	if err := config.SetKey(path, "language", "de"); err != nil {
		t.Fatalf("SetKey: %v", err)
	}

	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.applyColorScheme("default")

	values, _, err := config.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if values["language"] != "de" {
		t.Errorf("language = %q after a color-scheme save, want it preserved as %q, file: %v", values["language"], "de", values)
	}
	if values["color_scheme"] != "default" {
		t.Errorf("color_scheme = %q, want %q, file: %v", values["color_scheme"], "default", values)
	}
}
