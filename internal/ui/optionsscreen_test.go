package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// newOptionsRoot is the shared setup here: a Root with its own isolated
// user config file, with the Options screen open.
func newOptionsRoot(t *testing.T) (*Root, string) {
	t.Helper()
	configPath := isolateUserConfigFile(t)

	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openOptions()
	return r, configPath
}

// selectOptionCategory switches the screen to the named category, so a
// test can reach settings outside the default one.
func selectOptionCategory(t *testing.T, r *Root, name string) {
	t.Helper()
	for i, cat := range optionCategories() {
		if cat.name == name {
			r.optionsCategories.SetCurrentItem(i)
			return
		}
	}
	t.Fatalf("no option category named %q", name)
}

// TestOptionCatalogMatchesSettingDocs is the drift guard between the
// Options screen's own catalogue and the config package's canonical key
// list: every setting offered here must be a key config actually
// recognizes, and every implemented key must be offered somewhere.
//
// Without this, adding a setting to config but forgetting the catalogue
// would leave it silently uneditable, and a typo in a catalogue key
// would silently write a key nothing reads.
func TestOptionCatalogMatchesSettingDocs(t *testing.T) {
	offered := map[string]bool{}
	for _, cat := range optionCategories() {
		for _, opt := range cat.options {
			if _, ok := opt.doc(); !ok {
				t.Errorf("category %q offers %q, which config.SettingDocs doesn't recognize", cat.name, opt.key)
			}
			if offered[opt.key] {
				t.Errorf("%q is offered in more than one category", opt.key)
			}
			offered[opt.key] = true
		}
	}

	for _, doc := range config.SettingDocs() {
		if doc.Implemented && !offered[doc.Key] {
			t.Errorf("%q is implemented but no category offers it", doc.Key)
		}
		if !doc.Implemented && offered[doc.Key] {
			t.Errorf("%q is offered but marked unimplemented — a control that does nothing", doc.Key)
		}
	}
}

// TestSettingValueByKeyCoversEverySetting pins that the reset path can
// read back every setting it might be asked to reset — a key missing
// from settingValueByKey would make resetSetting fall through to the
// built-in default instead of the system tier's value, quietly
// defeating the whole two-tier fallback.
func TestSettingValueByKeyCoversEverySetting(t *testing.T) {
	settings := config.DefaultSettings()
	for _, cat := range optionCategories() {
		for _, opt := range cat.options {
			if _, ok := settingValueByKey(settings, opt.key); !ok {
				t.Errorf("settingValueByKey has no case for %q", opt.key)
			}
		}
	}
}

// TestToggleOptionAppliesLiveAndPersists pins the no-save-button
// contract for a boolean: activating the row flips it, the change is
// live immediately, and it's already written to the config file.
func TestToggleOptionAppliesLiveAndPersists(t *testing.T) {
	r, configPath := newOptionsRoot(t)

	row, ok := optionRowByKey(r, "show_hidden")
	if !ok {
		t.Fatal("no show_hidden row")
	}
	before := r.panel.showHidden

	r.activateOptionRow(row)

	if r.panel.showHidden == before {
		t.Error("the panel's own showHidden didn't change — the toggle wasn't applied live")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if !strings.Contains(string(data), "show_hidden =") {
		t.Errorf("config file doesn't contain the change:\n%s", data)
	}
}

// TestToggleOptionMarksTheOriginAsChanged pins the origin column
// following a change — the thing that makes reset's own behaviour
// visible.
func TestToggleOptionMarksTheOriginAsChanged(t *testing.T) {
	r, _ := newOptionsRoot(t)

	row, ok := optionRowByKey(r, "show_hidden")
	if !ok {
		t.Fatal("no show_hidden row")
	}
	if got := r.settingOriginLabel("show_hidden"); got != config.OriginDefault.String() {
		t.Fatalf("origin before the change = %q, want %q", got, config.OriginDefault.String())
	}

	r.activateOptionRow(row)

	if got, want := r.settingOriginLabel("show_hidden"), config.OriginUser.String(); got != want {
		t.Errorf("origin after the change = %q, want %q", got, want)
	}
	if got := r.optionsTable.GetCell(row, optionsColOrigin).Text; !strings.Contains(got, "changed by you") {
		t.Errorf("origin cell = %q, want it to say the value was changed", got)
	}
}

// TestResetRemovesTheKeyRatherThanWritingTheDefault is the core reset
// guarantee, and a real bug this caught: resetting used to remove the
// key and then immediately re-persist the resulting value through the
// setting's own apply, re-creating the exact key it had just deleted.
// The config file must come back with the key genuinely gone.
func TestResetRemovesTheKeyRatherThanWritingTheDefault(t *testing.T) {
	r, configPath := newOptionsRoot(t)

	row, ok := optionRowByKey(r, "show_hidden")
	if !ok {
		t.Fatal("no show_hidden row")
	}
	r.activateOptionRow(row) // change it, so there's something to reset
	if data, err := os.ReadFile(configPath); err != nil || !strings.Contains(string(data), "show_hidden =") {
		t.Fatalf("setup: expected show_hidden in the config file, got %q (err %v)", data, err)
	}

	r.resetCurrentOptionCategory()

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "show_hidden") {
			t.Errorf("show_hidden is still set after a reset: %q\n\nwhole file:\n%s", line, data)
		}
	}
}

// TestResetRestoresTheDefaultLive pins the other half: the reset value
// isn't only removed from the file, it's actually back in effect on
// screen — that's what "no save button" has to mean for a reset too.
func TestResetRestoresTheDefaultLive(t *testing.T) {
	r, _ := newOptionsRoot(t)

	row, ok := optionRowByKey(r, "show_hidden")
	if !ok {
		t.Fatal("no show_hidden row")
	}
	original := r.panel.showHidden
	r.activateOptionRow(row)
	if r.panel.showHidden == original {
		t.Fatal("setup: the toggle didn't change anything")
	}

	r.resetCurrentOptionCategory()

	if r.panel.showHidden != original {
		t.Errorf("showHidden = %v after reset, want the default %v back in effect", r.panel.showHidden, original)
	}
	if got, want := r.settingOriginLabel("show_hidden"), config.OriginDefault.String(); got != want {
		t.Errorf("origin after reset = %q, want %q", got, want)
	}
}

// TestResetAllCoversEveryCategory pins "Reset all" reaching settings
// outside the category currently on screen — the difference between it
// and "Reset category".
func TestResetAllCoversEveryCategory(t *testing.T) {
	r, configPath := newOptionsRoot(t)

	// Change something in Appearance...
	row, ok := optionRowByKey(r, "show_hidden")
	if !ok {
		t.Fatal("no show_hidden row")
	}
	r.activateOptionRow(row)

	// ...and something in Trash.
	selectOptionCategory(t, r, "Trash")
	trashRow, ok := optionRowByKey(r, "trash_persistent")
	if !ok {
		t.Fatal("no trash_persistent row")
	}
	r.activateOptionRow(trashRow)

	r.resetAllOptions()

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	for _, key := range []string{"show_hidden", "trash_persistent"} {
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "#") && strings.HasPrefix(trimmed, key) {
				t.Errorf("%s survived Reset all: %q", key, line)
			}
		}
	}
}

// TestOptionsPickerEscapeKeepsTheOriginalScheme pins that browsing the
// scheme list and backing out leaves the original in force — the live
// preview must not turn "I looked at it" into "I chose it".
func TestOptionsPickerEscapeKeepsTheOriginalScheme(t *testing.T) {
	schemes := []config.NamedTheme{
		{Slug: "default", Theme: config.DefaultTheme()},
		{Slug: "solarized", Theme: solarizedTheme()},
	}
	isolateInitialSettings(t, config.DefaultSettings(), schemes)
	isolateUserConfigFile(t)

	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openOptions()
	original := r.settings.ColorScheme

	row, ok := optionRowByKey(r, "color_scheme")
	if !ok {
		t.Fatal("no color_scheme row")
	}
	r.activateOptionRow(row)
	r.optionsPicker.SetCurrentItem(1) // preview the other scheme
	r.optionsPicker.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.settings.ColorScheme != original {
		t.Errorf("ColorScheme = %q after backing out, want the original %q", r.settings.ColorScheme, original)
	}
	if r.activePage != optionsPage {
		t.Errorf("activePage = %q, want back on the Options screen", r.activePage)
	}
}

// TestOptionsInfoOpensAndCloses pins the info window's own lifecycle:
// "?" opens it over the Options screen, Escape returns to the screen
// rather than closing everything.
func TestOptionsInfoOpensAndCloses(t *testing.T) {
	r, _ := newOptionsRoot(t)

	r.optionsTable.InputHandler()(tcell.NewEventKey(tcell.KeyRune, '?', tcell.ModNone), func(tview.Primitive) {})
	if r.activePage != optionsInfoPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, optionsInfoPage)
	}
	if got := r.optionsInfo.GetText(true); !strings.Contains(got, "Config key:") {
		t.Errorf("info text = %q, want it to name the config key", got)
	}

	r.hideOverlay() // Escape's own action
	if r.activePage != optionsPage {
		t.Errorf("activePage = %q after closing the info window, want back on %q", r.activePage, optionsPage)
	}
}

// TestOptionsTabCyclesFocus pins the focus ring: Tab walks from the
// categories through the table and every button, then wraps.
func TestOptionsTabCyclesFocus(t *testing.T) {
	r, _ := newOptionsRoot(t)
	ring := r.optionsFocusRing()

	r.app.SetFocus(ring[0])
	for i := 1; i <= len(ring); i++ {
		if !r.cycleOptionsFocus(1) {
			t.Fatalf("step %d: cycleOptionsFocus found nothing focused", i)
		}
		want := ring[i%len(ring)]
		if !want.HasFocus() {
			t.Fatalf("step %d: focus didn't land on ring position %d", i, i%len(ring))
		}
	}
	if !ring[0].HasFocus() {
		t.Error("Tab didn't wrap back to the first stop")
	}
}

// TestOptionsIntegerEditCommitsOnEnter pins the one deliberate exception
// to immediate application: a typed value takes effect on Enter, not on
// every keystroke (which would briefly apply "3" while typing "30").
func TestOptionsIntegerEditCommitsOnEnter(t *testing.T) {
	r, _ := newOptionsRoot(t)
	selectOptionCategory(t, r, "Trash")

	row, ok := optionRowByKey(r, "trash_max_age_days")
	if !ok {
		t.Fatal("no trash_max_age_days row")
	}
	r.activateOptionRow(row)
	if r.activePage != optionsInputPage {
		t.Fatalf("activePage = %q, want the input dialog %q", r.activePage, optionsInputPage)
	}

	r.optionsInput.SetText("45")
	r.optionsInput.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.settings.TrashMaxAgeDays != 45 {
		t.Errorf("TrashMaxAgeDays = %d, want 45", r.settings.TrashMaxAgeDays)
	}
	if r.activePage != optionsPage {
		t.Errorf("activePage = %q, want back on the Options screen", r.activePage)
	}
}

// TestOptionsIntegerEditEscapeDiscards is that exception's other half:
// backing out of the input leaves the value alone.
func TestOptionsIntegerEditEscapeDiscards(t *testing.T) {
	r, _ := newOptionsRoot(t)
	selectOptionCategory(t, r, "Trash")

	row, ok := optionRowByKey(r, "trash_max_age_days")
	if !ok {
		t.Fatal("no trash_max_age_days row")
	}
	original := r.settings.TrashMaxAgeDays

	r.activateOptionRow(row)
	r.optionsInput.SetText("999")
	r.optionsInput.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.settings.TrashMaxAgeDays != original {
		t.Errorf("TrashMaxAgeDays = %d after Escape, want the original %d", r.settings.TrashMaxAgeDays, original)
	}
}

// TestUnimplementedSettingIsNotOffered pins that "language" — parsed but
// acted on nowhere — has no control on the screen, so nothing appears to
// work that doesn't.
func TestUnimplementedSettingIsNotOffered(t *testing.T) {
	for _, cat := range optionCategories() {
		for _, opt := range cat.options {
			if opt.key == "language" {
				t.Errorf("category %q offers \"language\", which nothing reads", cat.name)
			}
		}
	}
}

// TestEveryOptionHasHelpText pins that the info button always has
// something worth showing — an empty explanation would make the button
// itself a dead end.
func TestEveryOptionHasHelpText(t *testing.T) {
	for _, cat := range optionCategories() {
		for _, opt := range cat.options {
			if strings.TrimSpace(opt.help) == "" {
				t.Errorf("%q (category %q) has no help text", opt.key, cat.name)
			}
			if strings.TrimSpace(opt.label) == "" {
				t.Errorf("%q (category %q) has no label", opt.key, cat.name)
			}
		}
	}
}

// TestBooleanOptionsRenderAsRadioGlyphs pins the user's own explicit
// request that a yes/no setting look exactly like every other boolean
// in this app — the filled/outline circle checkboxText already produces
// for the panel's checkbox column and the sed dialog's flags — rather
// than the words Yes/No it first shipped with.
func TestBooleanOptionsRenderAsRadioGlyphs(t *testing.T) {
	r, _ := newOptionsRoot(t)

	row, ok := optionRowByKey(r, "show_hidden")
	if !ok {
		t.Fatal("no show_hidden row")
	}

	before := strings.TrimSpace(r.optionsTable.GetCell(row, optionsColValue).Text)
	if want := checkboxText(r.panel.showHidden); before != want {
		t.Errorf("value cell = %q, want the radio glyph %q", before, want)
	}
	if before == "Yes" || before == "No" {
		t.Errorf("value cell = %q, want a glyph rather than a word", before)
	}

	r.activateOptionRow(row)

	after := strings.TrimSpace(r.optionsTable.GetCell(row, optionsColValue).Text)
	if after == before {
		t.Errorf("the glyph didn't change on toggle (still %q)", after)
	}
	if want := checkboxText(r.panel.showHidden); after != want {
		t.Errorf("value cell after toggle = %q, want %q", after, want)
	}
}

// TestOptionsArrowKeysMoveBetweenPanes pins the cursor-key navigation
// the user asked for: Right from the categories into the settings, Left
// back again — the two panes being physically side by side is what
// makes arrow keys the obvious way between them.
func TestOptionsArrowKeysMoveBetweenPanes(t *testing.T) {
	r, _ := newOptionsRoot(t)

	r.app.SetFocus(r.optionsCategories)
	r.optionsCategories.InputHandler()(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone), func(tview.Primitive) {})
	if !r.optionsTable.HasFocus() {
		t.Error("Right from the categories should focus the settings table")
	}

	r.optionsTable.InputHandler()(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone), func(tview.Primitive) {})
	if !r.optionsCategories.HasFocus() {
		t.Error("Left from the settings table should focus the categories")
	}
}

// TestOptionsLeftOnTheCategoriesIsInert pins the deliberate non-wrap:
// there is nothing to the left of the leftmost pane, and an arrow key
// that teleported to the far side of the screen would read as a glitch.
// Tab is the one that wraps.
func TestOptionsLeftOnTheCategoriesIsInert(t *testing.T) {
	r, _ := newOptionsRoot(t)

	r.app.SetFocus(r.optionsCategories)
	r.optionsCategories.InputHandler()(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone), func(tview.Primitive) {})

	if !r.optionsCategories.HasFocus() {
		t.Error("Left on the leftmost pane moved focus somewhere, want it left alone")
	}
}

// TestOptionsCategoryChangeSwapsTheSettingsShown pins that moving the
// category cursor — with the arrow keys, which is now a real path to it
// — re-renders the right-hand pane rather than needing Enter.
func TestOptionsCategoryChangeSwapsTheSettingsShown(t *testing.T) {
	r, _ := newOptionsRoot(t)

	if _, ok := optionRowByKey(r, "show_hidden"); !ok {
		t.Fatal("setup: expected the Appearance category first")
	}

	selectOptionCategory(t, r, "Trash")

	if _, ok := optionRowByKey(r, "show_hidden"); ok {
		t.Error("Appearance's own settings are still shown after switching category")
	}
	if _, ok := optionRowByKey(r, "trash_persistent"); !ok {
		t.Error("the Trash category's settings aren't shown after switching to it")
	}
}
