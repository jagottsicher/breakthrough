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

// confirmReset answers the shared confirmation dialog (see openConfirm)
// with its confirming choice — what a reset now goes through, so a test
// exercising one has to as well.
//
// Deliberately selects index 0 explicitly rather than pressing Enter on
// whatever is preselected: the dialog opens on "Cancel" (index 1) on
// purpose, and a helper that quietly relied on that ordering would stop
// confirming anything the moment it changed.
func confirmReset(t *testing.T, r *Root) {
	t.Helper()
	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the confirmation dialog %q", r.activePage, confirmPage)
	}
	r.confirmDialog.SetCurrentItem(0)
	r.confirmDialog.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})
}

// TestOptionCatalogMatchesSettingDocs is the drift guard between the
// Options screen's own catalogue and the config package's canonical key
// list: every setting offered here must be a key config actually
// recognizes, and every implemented key must be offered somewhere.
//
// Without this, adding a setting to config but forgetting the catalogue
// would leave it silently uneditable, and a typo in a catalogue key
// would silently write a key nothing reads.
// TestOpenOptionsFocusesCategoriesNotSettings pins the user's own
// explicit request: opening the screen ("oo") lands on "Appearance"
// selected in the left-hand category list — but keyboard focus stays
// there, rather than already jumping into its settings on the right the
// way it used to. Right (or Tab) is what actually moves you in.
func TestOpenOptionsFocusesCategoriesNotSettings(t *testing.T) {
	r, _ := newOptionsRoot(t)

	if !r.optionsCategories.HasFocus() {
		t.Error("opening Options should focus the category list, not the settings table")
	}
	if r.optionsTable.HasFocus() {
		t.Error("opening Options should not already have focus on the settings table")
	}
}

func TestOptionCatalogMatchesSettingDocs(t *testing.T) {
	offered := map[string]bool{}
	for _, cat := range optionCategories() {
		for _, opt := range cat.options {
			if opt.secretAction != nil {
				// Deliberately not config.SettingDocs-backed — see
				// optionSpec.secretAction's own doc comment on why a
				// credential never goes through persistSetting at all.
				if offered[opt.key] {
					t.Errorf("%q is offered in more than one category", opt.key)
				}
				offered[opt.key] = true
				continue
			}
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
			if opt.secretAction != nil {
				// Never persisted via config.Settings at all — see
				// optionSpec.secretAction's own doc comment.
				continue
			}
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

// TestConfirmEnableOptionAsksBeforeTurningOn pins Machiavelli Mode's
// own gate: activating the row while it's off must not apply anything
// yet, only open the scroll-to-acknowledge notice — accepting it (once
// scrolled to the end) is what actually flips the setting.
// TestCycleOptionChoiceSkipsThePastTypedEntrySentinel pins a real,
// reported regression: cycling used to open the free-text input the
// instant it reached the typed-entry sentinel (always the last
// choice — see appendConsigliereTypedEntryChoice), which meant
// clicking through Model/Effort fast could pop that dialog mid-click,
// right under a still-incoming burst of further clicks/keypresses — a
// real, reported freeze. Cycling now skips straight past it, wrapping
// the same way a real carousel does; reaching it is a separate,
// deliberate action (see TestOptionsTableKeyMOpensTypedEntryOnARowThatHasIt).
func TestCycleOptionChoiceSkipsThePastTypedEntrySentinel(t *testing.T) {
	r, _ := newOptionsRoot(t)
	selectOptionCategory(t, r, "AI Consigliere")

	row, ok := optionRowByKey(r, "consigliere_anthropic_model")
	if !ok {
		t.Fatal("no consigliere_anthropic_model row")
	}
	opt, _ := r.optionAtRow(row)
	choices := opt.choices(r)
	last := choices[len(choices)-1]
	if last.value != consigliereTypedEntrySentinel {
		t.Fatalf("last choice = %+v, want the typed-entry sentinel", last)
	}

	// A full lap (one activation per real choice, sentinel excluded)
	// must land back on "" without ever opening the free-text input
	// along the way.
	for i := 0; i < len(choices)-1; i++ {
		r.activateOptionRow(row)
		if r.activePage == optionsInputPage {
			t.Fatalf("cycling opened the free-text input after %d activation(s) — the sentinel should only ever be skipped past", i+1)
		}
	}
	if got := r.settings.ConsigliereAnthropicModel; got != "" {
		t.Errorf("ConsigliereAnthropicModel = %q after a full lap, want back at \"\" (the sentinel must never be applied as a real model ID either)", got)
	}
}

// TestOptionsTableKeyMOpensTypedEntryOnARowThatHasIt pins the
// deliberate, separate action for reaching the typed-entry sentinel —
// see TestCycleOptionChoiceSkipsThePastTypedEntrySentinel's own doc
// comment on why cycling itself no longer does.
func TestOptionsTableKeyMOpensTypedEntryOnARowThatHasIt(t *testing.T) {
	r, _ := newOptionsRoot(t)
	selectOptionCategory(t, r, "AI Consigliere")

	row, ok := optionRowByKey(r, "consigliere_anthropic_model")
	if !ok {
		t.Fatal("no consigliere_anthropic_model row")
	}
	r.optionsTable.Select(row, 0)

	r.optionsTable.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'm', tcell.ModNone), func(tview.Primitive) {})

	if r.activePage != optionsInputPage {
		t.Errorf("activePage = %q, want the free-text input open", r.activePage)
	}
}

// TestOptionsTableKeyMIsANoOpOnARowWithoutTypedEntry pins the other
// half: "m" does nothing (doesn't even consume the keypress) on a row
// that offers no free-text entry at all, e.g. an ordinary enum like
// color_scheme.
func TestOptionsTableKeyMIsANoOpOnARowWithoutTypedEntry(t *testing.T) {
	r, _ := newOptionsRoot(t)

	row, ok := optionRowByKey(r, "color_scheme")
	if !ok {
		t.Fatal("no color_scheme row")
	}
	r.optionsTable.Select(row, 0)
	before := r.activePage

	r.optionsTable.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'm', tcell.ModNone), func(tview.Primitive) {})

	if r.activePage != before {
		t.Errorf("activePage changed to %q after \"m\" on a row with no typed entry", r.activePage)
	}
}

// TestEditOptionValueBlankCommitIsNoOpWithoutEmptyMeansDefault pins
// editOptionValue's own ordinary rule, unchanged: for a ordinary
// KindString row, clearing the field and pressing Enter leaves the
// previous value alone rather than applying "" — the behavior
// optionSpec.emptyMeansDefault's own doc comment explicitly opts out
// of for Model/Effort (see
// TestEditOptionValueBlankCommitAppliesWhenEmptyMeansDefault).
func TestEditOptionValueBlankCommitIsNoOpWithoutEmptyMeansDefault(t *testing.T) {
	r, _ := newOptionsRoot(t)
	applied := false
	opt := optionSpec{
		key:   "test",
		label: "Test",
		value: func(*Root) string { return "old-value" },
		apply: func(*Root, string) { applied = true },
	}

	r.editOptionValue(opt)
	r.optionsInput.SetText("")
	r.optionsInput.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if applied {
		t.Error("apply should not have been called for a blank commit without emptyMeansDefault")
	}
}

// TestEditOptionValueBlankCommitAppliesWhenEmptyMeansDefault pins the
// user's own explicit report: typing something into Model/Effort and
// then clearing the field again used to do nothing at all — no way
// back to "use the built-in default" short of "Reset category", which
// also discards every other AI Consigliere setting along with it.
func TestEditOptionValueBlankCommitAppliesWhenEmptyMeansDefault(t *testing.T) {
	r, _ := newOptionsRoot(t)
	var got string
	applied := false
	opt := optionSpec{
		key:               "test",
		label:             "Test",
		value:             func(*Root) string { return "old-value" },
		apply:             func(_ *Root, v string) { applied = true; got = v },
		emptyMeansDefault: true,
	}

	r.editOptionValue(opt)
	r.optionsInput.SetText("")
	r.optionsInput.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if !applied {
		t.Fatal("apply should have been called with \"\" for a blank commit with emptyMeansDefault")
	}
	if got != "" {
		t.Errorf("apply was called with %q, want \"\"", got)
	}
}

func TestConfirmEnableOptionAsksBeforeTurningOn(t *testing.T) {
	r, _ := newOptionsRoot(t)
	r.SetRect(0, 0, 100, 40)
	selectOptionCategory(t, r, "AI Consigliere")

	row, ok := optionRowByKey(r, "consigliere_hardcore_mode")
	if !ok {
		t.Fatal("no consigliere_hardcore_mode row")
	}

	r.activateOptionRow(row)

	if r.settings.ConsigliereHardcoreMode {
		t.Error("turning it on should not have applied yet, pending the notice")
	}
	if r.activePage != scrollAcknowledgePage {
		t.Errorf("activePage = %q, want the scroll-to-acknowledge notice open", r.activePage)
	}

	r.scrollAcknowledgeMessageView.ScrollTo(r.scrollAcknowledgeScrollbar.maxOffset(), 0) // ScrollToEnd only resolves on the next real Draw, which tests never trigger
	r.acceptScrollAcknowledgeNotice()

	if !r.settings.ConsigliereHardcoreMode {
		t.Error("confirming after scrolling to the end should have turned it on")
	}
}

// TestConfirmEnableOptionWithoutScrollingDoesNothing pins the actual
// gate the user asked for: confirming before reaching the end of the
// warning must not apply anything, unlike a plain openConfirm question
// that would.
func TestConfirmEnableOptionWithoutScrollingDoesNothing(t *testing.T) {
	r, _ := newOptionsRoot(t)
	r.SetRect(0, 0, 100, 40)
	selectOptionCategory(t, r, "AI Consigliere")

	row, ok := optionRowByKey(r, "consigliere_hardcore_mode")
	if !ok {
		t.Fatal("no consigliere_hardcore_mode row")
	}

	r.activateOptionRow(row)
	r.acceptScrollAcknowledgeNotice() // never scrolled

	if r.settings.ConsigliereHardcoreMode {
		t.Error("confirming without scrolling to the end should not have turned it on")
	}
	// The rejection's own explanation layers on top (showTransientErrorOverOverlay),
	// the same "don't close what's underneath" reasoning
	// validateOptionsInputPath's identical case already follows —
	// activePage is now the transient error, but the notice underneath
	// must still be open, not closed over.
	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want the rejection's own explanation layered on top", r.activePage)
	}
	assertPageVisible(t, r, scrollAcknowledgePage)
}

// TestConfirmEnableOptionCancelLeavesItOff is the other half: declining
// the notice must leave the setting untouched.
func TestConfirmEnableOptionCancelLeavesItOff(t *testing.T) {
	r, _ := newOptionsRoot(t)
	r.SetRect(0, 0, 100, 40)
	selectOptionCategory(t, r, "AI Consigliere")

	row, ok := optionRowByKey(r, "consigliere_hardcore_mode")
	if !ok {
		t.Fatal("no consigliere_hardcore_mode row")
	}

	r.activateOptionRow(row)
	r.cancelScrollAcknowledgeNotice()

	if r.settings.ConsigliereHardcoreMode {
		t.Error("cancelling the notice should have left it off")
	}
}

// TestConfirmEnableOptionTurningOffNeedsNoConfirmation pins the
// asymmetry: only switching it on is gated — switching back off is
// exactly as immediate as every other boolean option.
func TestConfirmEnableOptionTurningOffNeedsNoConfirmation(t *testing.T) {
	r, _ := newOptionsRoot(t)
	r.settings.ConsigliereHardcoreMode = true
	selectOptionCategory(t, r, "AI Consigliere")

	row, ok := optionRowByKey(r, "consigliere_hardcore_mode")
	if !ok {
		t.Fatal("no consigliere_hardcore_mode row")
	}

	r.activateOptionRow(row)

	if r.settings.ConsigliereHardcoreMode {
		t.Error("turning it off should have applied immediately, no notice")
	}
	if r.activePage == scrollAcknowledgePage {
		t.Error("no scroll-to-acknowledge notice should have opened for turning it off")
	}
}

// TestDefaultHintAppearsOnlyWhileTheValueDiffers pins the column that
// replaced the old origin one, and the exact complaint that prompted it:
// toggling a setting twice puts the default value back, and the hint has
// to disappear again rather than going on claiming the value was
// changed.
func TestDefaultHintAppearsOnlyWhileTheValueDiffers(t *testing.T) {
	r, _ := newOptionsRoot(t)

	row, ok := optionRowByKey(r, "show_hidden")
	if !ok {
		t.Fatal("no show_hidden row")
	}
	hint := func() string {
		return strings.TrimSpace(r.optionsTable.GetCell(row, optionsColDefault).Text)
	}

	if got := hint(); got != "" {
		t.Errorf("default hint at the default value = %q, want empty", got)
	}

	r.activateOptionRow(row)
	if got := hint(); !strings.HasPrefix(got, "default:") {
		t.Errorf("default hint after changing the value = %q, want it to name the default", got)
	}

	r.activateOptionRow(row) // back to the default value
	if got := hint(); got != "" {
		t.Errorf("default hint after toggling back = %q, want empty again — this is the whole point of the column", got)
	}
}

// TestDefaultHintRendersInTheSameVocabularyAsTheValue pins that a
// boolean's default hint shows the radio glyph rather than the literal
// "true"/"false", so the reader isn't left translating between two
// notations to compare them.
func TestDefaultHintRendersInTheSameVocabularyAsTheValue(t *testing.T) {
	r, _ := newOptionsRoot(t)

	row, ok := optionRowByKey(r, "show_hidden")
	if !ok {
		t.Fatal("no show_hidden row")
	}
	r.activateOptionRow(row) // make it differ, so the hint shows at all

	got := strings.TrimSpace(r.optionsTable.GetCell(row, optionsColDefault).Text)
	if strings.Contains(got, "true") || strings.Contains(got, "false") {
		t.Errorf("default hint = %q, want the radio glyph rather than a boolean literal", got)
	}
	if !strings.Contains(got, checkboxText(true)) && !strings.Contains(got, checkboxText(false)) {
		t.Errorf("default hint = %q, want it to contain a radio glyph", got)
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
	confirmReset(t, r)

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
	confirmReset(t, r)

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
	confirmReset(t, r)

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

// TestOptionsSpaceActivatesTheSelectedRow pins the user's own explicit
// request that Space toggle a radio button too, not just Enter —
// matching every other toggle in this app.
func TestOptionsSpaceActivatesTheSelectedRow(t *testing.T) {
	r, _ := newOptionsRoot(t)

	row, ok := optionRowByKey(r, "show_hidden")
	if !ok {
		t.Fatal("no show_hidden row")
	}
	r.optionsTable.Select(row, 0)
	before := r.panel.showHidden

	r.optionsTable.InputHandler()(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone), func(tview.Primitive) {})

	if r.panel.showHidden == before {
		t.Error("Space didn't toggle the selected setting")
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

	r.hideOverlay() // same effect a real Escape keypress should have — see TestOptionsInfoRealEscapeKeypressCloses for that
	if r.activePage != optionsPage {
		t.Errorf("activePage = %q after closing the info window, want back on %q", r.activePage, optionsPage)
	}
}

// TestOptionsInfoRealEscapeKeypressCloses pins a real, reported gap
// TestOptionsInfoOpensAndCloses' own call to hideOverlay directly never
// could have caught: tview.TextView.InputHandler only calls its own
// done func on Escape/Enter/Tab/Backtab if one was actually set
// (verified directly against its source) — with none ever bound to
// r.optionsInfo, a real Escape keypress on the focused popup did
// nothing at all, leaving no keyboard way to close it (only the
// mouse-only captureOutsideClick). Drives the actual widget's own
// InputHandler, the same way a real keypress would reach it once it
// has focus, rather than calling hideOverlay directly.
func TestOptionsInfoRealEscapeKeypressCloses(t *testing.T) {
	r, _ := newOptionsRoot(t)

	r.optionsTable.InputHandler()(tcell.NewEventKey(tcell.KeyRune, '?', tcell.ModNone), func(tview.Primitive) {})
	if r.activePage != optionsInfoPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, optionsInfoPage)
	}

	r.optionsInfo.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.activePage != optionsPage {
		t.Errorf("activePage = %q after a real Escape keypress on the info popup, want back on %q", r.activePage, optionsPage)
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

// TestCopyMoveIsASubsectionOfBehaviorNotItsOwnCategory pins the user's
// own explicit request: the eight Copy & Move settings live grouped
// under Behavior, with a "Copy & Move" header row of their own, rather
// than costing their own top-level category in the left-hand list.
func TestCopyMoveIsASubsectionOfBehaviorNotItsOwnCategory(t *testing.T) {
	r, _ := newOptionsRoot(t)

	for _, cat := range optionCategories() {
		if cat.name == "Copy & Move" {
			t.Fatal("Copy & Move should no longer be its own top-level category")
		}
	}

	selectOptionCategory(t, r, "Behavior")

	headerRow := -1
	rows := optionCategoryDisplayRows(r, mustOptionCategory(t, "Behavior"))
	for i, dr := range rows {
		if dr.header == "Copy & Move" {
			headerRow = i
			break
		}
	}
	if headerRow == -1 {
		t.Fatal("no \"Copy & Move\" subsection header found under Behavior")
	}
	if got := strings.TrimSpace(r.optionsTable.GetCell(headerRow, optionsColLabel).Text); got != "Copy & Move" {
		t.Errorf("header row label = %q, want %q", got, "Copy & Move")
	}

	copyRow, ok := optionRowByKey(r, "copy_preserve_attributes")
	if !ok {
		t.Fatal("copy_preserve_attributes not found under Behavior")
	}
	if copyRow != headerRow+1 {
		t.Errorf("copy_preserve_attributes at row %d, want it directly after the header at row %d", copyRow, headerRow+1)
	}
}

// mustOptionCategory finds the named category or fails the test — a
// small helper for tests that need the category value itself (not just
// to switch the screen to it, see selectOptionCategory).
func mustOptionCategory(t *testing.T, name string) optionCategory {
	t.Helper()
	for _, cat := range optionCategories() {
		if cat.name == name {
			return cat
		}
	}
	t.Fatalf("no option category named %q", name)
	return optionCategory{}
}

// TestOptionCategoryDisplayRowsInsertsHeaderOnce pins
// optionCategoryDisplayRows' own contract directly: a header appears
// exactly once, right before the first option of its own section, not
// once per option in it.
// TestOptionCategoryDisplayRowsHidesInvisibleOption pins
// optionSpec.visible's own contract directly, independent of any real
// AI Consigliere row: an option whose visible func reports false for r
// simply isn't in the display rows at all, as if it weren't in
// cat.options in the first place; r == nil (the test-only, unfiltered
// path optionRowByKey no longer uses, but settingValueByKey's own
// contract still relies on cat.options itself staying untouched by
// this) must show every option regardless.
func TestOptionCategoryDisplayRowsHidesInvisibleOption(t *testing.T) {
	cat := optionCategory{
		name: "test",
		options: []optionSpec{
			{key: "a"},
			{key: "b", visible: func(r *Root) bool { return false }},
			{key: "c"},
		},
	}

	r := &Root{}
	var keys []string
	for _, dr := range optionCategoryDisplayRows(r, cat) {
		keys = append(keys, dr.opt.key)
	}
	if want := []string{"a", "c"}; len(keys) != len(want) || keys[0] != want[0] || keys[1] != want[1] {
		t.Errorf("keys = %v, want %v", keys, want)
	}

	var allKeys []string
	for _, dr := range optionCategoryDisplayRows(nil, cat) {
		allKeys = append(allKeys, dr.opt.key)
	}
	if want := []string{"a", "b", "c"}; len(allKeys) != len(want) {
		t.Errorf("unfiltered keys = %v, want %v", allKeys, want)
	}
}

// TestConsigliereProviderSpecificRowsAreHiddenForOtherProviders pins
// the user's own explicit complaints: "Ollama base URL" and "Custom
// base URL" must each only show up while Provider is set to the
// matching value, and the two API-key rows must be hidden entirely
// while Provider is "ollama" (which needs no key at all).
func TestConsigliereProviderSpecificRowsAreHiddenForOtherProviders(t *testing.T) {
	r, _ := newOptionsRoot(t)
	selectOptionCategory(t, r, "AI Consigliere")

	r.settings.ConsigliereProvider = "anthropic"
	if _, ok := optionRowByKey(r, "consigliere_ollama_base_url"); ok {
		t.Error("\"Ollama base URL\" should be hidden while Provider is \"anthropic\"")
	}
	if _, ok := optionRowByKey(r, "consigliere_custom_base_url"); ok {
		t.Error("\"Custom base URL\" should be hidden while Provider is \"anthropic\"")
	}
	if _, ok := optionRowByKey(r, "consigliere_anthropic_api_key_file"); !ok {
		t.Error("the plain-text API key row should be shown while Provider is \"anthropic\"")
	}
	if _, ok := optionRowByKey(r, "consigliere_api_key_secret"); !ok {
		t.Error("the encrypted API key row should be shown while Provider is \"anthropic\"")
	}

	r.settings.ConsigliereProvider = "ollama"
	if _, ok := optionRowByKey(r, "consigliere_ollama_base_url"); !ok {
		t.Error("\"Ollama base URL\" should be shown while Provider is \"ollama\"")
	}
	if _, ok := optionRowByKey(r, "consigliere_custom_base_url"); ok {
		t.Error("\"Custom base URL\" should stay hidden while Provider is \"ollama\"")
	}
	if _, ok := optionRowByKey(r, "consigliere_anthropic_api_key_file"); ok {
		t.Error("anthropic's own API key row should stay hidden while Provider is \"ollama\"")
	}
	if _, ok := optionRowByKey(r, "consigliere_api_key_secret"); ok {
		t.Error("the encrypted API key row should be hidden while Provider is \"ollama\"")
	}

	r.settings.ConsigliereProvider = "custom"
	if _, ok := optionRowByKey(r, "consigliere_ollama_base_url"); ok {
		t.Error("\"Ollama base URL\" should stay hidden while Provider is \"custom\"")
	}
	if _, ok := optionRowByKey(r, "consigliere_custom_base_url"); !ok {
		t.Error("\"Custom base URL\" should be shown while Provider is \"custom\"")
	}
	if _, ok := optionRowByKey(r, "consigliere_custom_api_key_file"); !ok {
		t.Error("the plain-text API key row should be shown while Provider is \"custom\"")
	}
	if _, ok := optionRowByKey(r, "consigliere_api_key_secret"); !ok {
		t.Error("the encrypted API key row should be shown while Provider is \"custom\"")
	}
}

func TestOptionCategoryDisplayRowsInsertsHeaderOnce(t *testing.T) {
	cat := optionCategory{
		name: "test",
		options: []optionSpec{
			{key: "a"},
			{key: "b", section: "Group"},
			{key: "c", section: "Group"},
			{key: "d"},
		},
	}

	rows := optionCategoryDisplayRows(nil, cat)

	var headers []string
	for _, dr := range rows {
		if dr.header != "" {
			headers = append(headers, dr.header)
		}
	}
	if len(headers) != 1 || headers[0] != "Group" {
		t.Fatalf("headers = %v, want exactly one \"Group\" header", headers)
	}
	if len(rows) != len(cat.options)+1 {
		t.Fatalf("got %d display rows, want %d (options) + 1 (header)", len(rows), len(cat.options)+1)
	}
	// The header must sit directly before "b", the first option of its
	// own section — not before "a" (which has no section at all) and
	// not repeated again before "c" (same section as "b").
	if rows[0].opt.key != "a" || rows[1].header != "Group" || rows[2].opt.key != "b" ||
		rows[3].opt.key != "c" || rows[4].opt.key != "d" {
		t.Fatalf("rows = %+v, want [a, header(Group), b, c, d]", rows)
	}
}

// TestOptionAtRowReturnsFalseForHeaderRow pins that a header row has no
// setting of its own to activate, explain, or click — optionAtRow (and
// therefore activateOptionRow/showOptionInfo, which both go through it)
// must report false rather than the option that happens to follow it.
func TestOptionAtRowReturnsFalseForHeaderRow(t *testing.T) {
	r, _ := newOptionsRoot(t)
	selectOptionCategory(t, r, "Behavior")

	copyRow, ok := optionRowByKey(r, "copy_preserve_attributes")
	if !ok {
		t.Fatal("copy_preserve_attributes not found under Behavior")
	}
	headerRow := copyRow - 1

	if _, ok := r.optionAtRow(headerRow); ok {
		t.Error("optionAtRow on the header row should return false, not the option right after it")
	}
	if _, ok := r.optionAtRow(copyRow); !ok {
		t.Error("optionAtRow on the row right after the header should still find copy_preserve_attributes")
	}
}

// TestOptionsArrowKeysSkipTheSubsectionHeader drives the real
// tview.Table InputHandler down through Behavior's own settings and
// pins that the cursor never lands on the "Copy & Move" header row —
// relying on tview's own documented behavior (verified directly against
// its source, not assumed) that arrow-key movement skips any cell
// marked NotSelectable, which renderOptions sets on every cell of a
// header row.
func TestOptionsArrowKeysSkipTheSubsectionHeader(t *testing.T) {
	r, _ := newOptionsRoot(t)
	selectOptionCategory(t, r, "Behavior")
	r.app.SetFocus(r.optionsTable)

	headerRow, ok := func() (int, bool) {
		for i, dr := range optionCategoryDisplayRows(r, mustOptionCategory(t, "Behavior")) {
			if dr.header == "Copy & Move" {
				return i, true
			}
		}
		return 0, false
	}()
	if !ok {
		t.Fatal("no \"Copy & Move\" header found under Behavior")
	}

	r.optionsTable.Select(0, 0)
	rowCount := r.optionsTable.GetRowCount()
	for i := 0; i < rowCount; i++ {
		r.optionsTable.InputHandler()(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone), func(tview.Primitive) {})
		row, _ := r.optionsTable.GetSelection()
		if row == headerRow {
			t.Fatalf("cursor landed on the header row (%d) after %d Down presses", headerRow, i+1)
		}
	}
}

// TestOptionCategoryDisplayRowsInsertsBlankRowBetweenSections pins the
// user's own explicit request: a blank spacer row separates two
// consecutive sections, but never appears before the very first
// section in the category — there's nothing above it there to separate
// from.
func TestOptionCategoryDisplayRowsInsertsBlankRowBetweenSections(t *testing.T) {
	cat := optionCategory{
		name: "test",
		options: []optionSpec{
			{key: "a", section: "First"},
			{key: "b", section: "First"},
			{key: "c", section: "Second"},
			{key: "d", section: "Third"},
		},
	}

	rows := optionCategoryDisplayRows(nil, cat)

	var got []string
	for _, dr := range rows {
		switch {
		case dr.blank:
			got = append(got, "blank")
		case dr.header != "":
			got = append(got, "header:"+dr.header)
		default:
			got = append(got, "opt:"+dr.opt.key)
		}
	}
	want := []string{
		"header:First", "opt:a", "opt:b",
		"blank", "header:Second", "opt:c",
		"blank", "header:Third", "opt:d",
	}
	if len(got) != len(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

// TestOptionAtRowReturnsFalseForBlankRow is
// TestOptionAtRowReturnsFalseForHeaderRow's own counterpart for a blank
// spacer row: it has no setting of its own either, and — since its own
// zero-value opt.key is "" — must never be confused with a genuine
// setting that happens to share that empty key.
func TestOptionAtRowReturnsFalseForBlankRow(t *testing.T) {
	r, _ := newOptionsRoot(t)
	selectOptionCategory(t, r, "Behavior")

	copyRow, ok := optionRowByKey(r, "copy_preserve_attributes")
	if !ok {
		t.Fatal("copy_preserve_attributes not found under Behavior")
	}
	blankRow := copyRow - 2 // header sits right before copyRow; the blank spacer, one further back

	rows := optionCategoryDisplayRows(r, mustOptionCategory(t, "Behavior"))
	if !rows[blankRow].blank {
		t.Fatalf("setup: row %d isn't the blank spacer row (got %+v)", blankRow, rows[blankRow])
	}
	if _, ok := r.optionAtRow(blankRow); ok {
		t.Error("optionAtRow on the blank spacer row should return false")
	}
}

// TestFirstSelectableOptionsRowSkipsALeadingHeader pins the fix this
// section/spacer feature needed once "General" gave Behavior's own
// first row a header of its own: the out-of-range recovery in
// renderOptions must land on the first real setting, not row 0 itself,
// once row 0 is a header rather than an option.
func TestFirstSelectableOptionsRowSkipsALeadingHeader(t *testing.T) {
	r, _ := newOptionsRoot(t)
	selectOptionCategory(t, r, "Behavior")

	got := r.firstSelectableOptionsRow()

	restoreTabsRow, ok := optionRowByKey(r, "restore_tabs")
	if !ok {
		t.Fatal("restore_tabs not found under Behavior")
	}
	if got != restoreTabsRow {
		t.Errorf("firstSelectableOptionsRow = %d, want %d (restore_tabs, right after the \"General\" header)", got, restoreTabsRow)
	}
	if opt, ok := r.optionAtRow(got); !ok || opt.key != "restore_tabs" {
		t.Errorf("row %d isn't a real, selectable option (optionAtRow ok=%v)", got, ok)
	}
}

// TestRenderOptionsRecoversFromOutOfRangeCursorOntoARealOption pins the
// exact scenario firstSelectableOptionsRow exists for: if the cursor
// somehow points past the end of the currently rendered table (the same
// "category switch shortened the list" case renderOptions already
// guards against), the recovery must land on a real, selectable option
// — not row 0, once row 0 is itself a header (Behavior's own "General").
func TestRenderOptionsRecoversFromOutOfRangeCursorOntoARealOption(t *testing.T) {
	r, _ := newOptionsRoot(t)
	selectOptionCategory(t, r, "Behavior")

	r.optionsTable.Select(9999, 0) // simulate a cursor left over from a longer category
	r.renderOptions()

	row, _ := r.optionsTable.GetSelection()
	opt, ok := r.optionAtRow(row)
	if !ok {
		t.Fatalf("recovered cursor landed on row %d, which isn't a real option", row)
	}
	if opt.key != "restore_tabs" {
		t.Errorf("recovered cursor = %q, want restore_tabs (the first real option under Behavior)", opt.key)
	}
}

// TestClickingACategoryDoesNotCloseTheScreen pins a real reported bug:
// the screen was registered as an overlay with only the settings table
// as its widget, so captureOutsideClick — which closes an overlay on any
// click outside the widget it was shown with — treated a click on the
// categories, the buttons or the title bar as "outside" and shut the
// whole thing.
func TestClickingACategoryDoesNotCloseTheScreen(t *testing.T) {
	r, _ := newOptionsRoot(t)

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(110, 30)
	r.SetRect(0, 0, 110, 30)
	r.Draw(screen)

	// A point inside the categories pane — which is emphatically part of
	// the Options screen, however "outside the focused widget" it is.
	x, y, _, _ := r.optionsCategories.GetRect()
	r.captureOutsideClick(tview.MouseLeftClick, tcell.NewEventMouse(x+1, y+1, tcell.Button1, 0))

	if r.activePage != optionsPage {
		t.Errorf("activePage = %q after clicking a category, want the screen still open", r.activePage)
	}
}

// TestClickingTheButtonRowDoesNotCloseTheScreen is the same bug from the
// other side of the layout — the buttons are as much part of the screen
// as the categories are.
func TestClickingTheButtonRowDoesNotCloseTheScreen(t *testing.T) {
	r, _ := newOptionsRoot(t)

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(110, 30)
	r.SetRect(0, 0, 110, 30)
	r.Draw(screen)

	x, y, _, _ := r.optionsResetAllBtn.GetRect()
	r.captureOutsideClick(tview.MouseLeftClick, tcell.NewEventMouse(x+1, y, tcell.Button1, 0))

	if r.activePage != optionsPage {
		t.Errorf("activePage = %q after clicking a button, want the screen still open", r.activePage)
	}
}

// TestOptionsFocusIsVisiblyDistinct pins the other half of that report:
// with no visual difference between the two panes, there was no telling
// which one the arrow keys were driving. The focused pane's selected row
// takes FocusedBackground, the other one EditableBackground — the same
// "petrol means keystrokes go here" convention the rest of the app uses.
//
// Read off a real drawn screen rather than from the widgets: tview
// exposes no getter for a selected style, and what actually reaches the
// terminal is the thing worth pinning anyway.
func TestOptionsFocusIsVisiblyDistinct(t *testing.T) {
	r, _ := newOptionsRoot(t)

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(110, 30)
	r.SetRect(0, 0, 110, 30)

	// backgroundAt returns the background color actually drawn at a
	// point, after a full redraw.
	backgroundAt := func(x, y int) tcell.Color {
		r.Draw(screen)
		_, style, _ := screen.Get(x, y)
		_, bg, _ := style.Decompose()
		return bg
	}

	// One draw first: tview lays a Flex's children out during Draw, so
	// before that every rect here is still the construction-time default
	// and any coordinate read from it points somewhere else entirely.
	r.Draw(screen)

	// The first row of each pane is the selected one on a fresh open.
	catX, catY, _, _ := r.optionsCategories.GetInnerRect()
	tableX, tableY, _, _ := r.optionsTable.GetInnerRect()

	r.app.SetFocus(r.optionsTable)
	if got, want := backgroundAt(tableX, tableY), r.theme.FocusedBackground; got != want {
		t.Errorf("focused settings row drew %v, want FocusedBackground %v", got, want)
	}
	if got, want := backgroundAt(catX, catY), r.theme.EditableBackground; got != want {
		t.Errorf("unfocused category row drew %v, want EditableBackground %v", got, want)
	}

	r.app.SetFocus(r.optionsCategories)
	if got, want := backgroundAt(catX, catY), r.theme.FocusedBackground; got != want {
		t.Errorf("focused category row drew %v, want FocusedBackground %v", got, want)
	}
	if got, want := backgroundAt(tableX, tableY), r.theme.EditableBackground; got != want {
		t.Errorf("unfocused settings row drew %v, want EditableBackground %v", got, want)
	}
}

// TestResetAsksBeforeDiscardingAnything pins the safeguard the user
// asked for: a reset opens the shared confirmation rather than acting
// straight away, and Cancel leaves everything exactly as it was.
func TestResetAsksBeforeDiscardingAnything(t *testing.T) {
	r, configPath := newOptionsRoot(t)

	row, ok := optionRowByKey(r, "show_hidden")
	if !ok {
		t.Fatal("no show_hidden row")
	}
	r.activateOptionRow(row)
	changed := r.panel.showHidden

	r.resetCurrentOptionCategory()

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want a confirmation before anything is discarded", r.activePage)
	}
	if r.panel.showHidden != changed {
		t.Error("the reset already took effect before being confirmed")
	}

	// Cancel — index 1, and also what the dialog opens on.
	r.confirmDialog.SetCurrentItem(1)
	r.confirmDialog.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.panel.showHidden != changed {
		t.Error("cancelling the confirmation still reset the value")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if !strings.Contains(string(data), "show_hidden =") {
		t.Errorf("cancelling still removed the key from the config file:\n%s", data)
	}
}

// TestResetConfirmationOpensOnCancel pins that a stray Enter can never
// trigger a reset — the dialog preselects Cancel, the same rule every
// other irreversible action in this app follows.
func TestResetConfirmationOpensOnCancel(t *testing.T) {
	r, _ := newOptionsRoot(t)

	r.resetAllOptions()

	if got := r.confirmDialog.GetCurrentItem(); got != 1 {
		t.Errorf("confirmation opened on item %d, want 1 (Cancel)", got)
	}
}

// TestResetConfirmationNamesTheAction pins that the confirming answer
// says what it will do rather than a bare "OK", and that the two resets
// are told apart — "Reset category" and "Reset all" ask genuinely
// different questions. The question itself now lives in
// confirmDialogTitleBar, not a list item — see openConfirm's own doc
// comment.
func TestResetConfirmationNamesTheAction(t *testing.T) {
	r, _ := newOptionsRoot(t)

	r.resetCurrentOptionCategory()
	categoryQuestion := r.confirmDialogTitleBar.GetText(true)
	categoryAnswer, _ := r.confirmDialog.GetItemText(0)
	r.cancelConfirm()

	r.resetAllOptions()
	allQuestion := r.confirmDialogTitleBar.GetText(true)
	allAnswer, _ := r.confirmDialog.GetItemText(0)
	r.cancelConfirm()

	if categoryQuestion == allQuestion {
		t.Errorf("both resets ask the same question %q — they do different things", categoryQuestion)
	}
	for _, answer := range []string{categoryAnswer, allAnswer} {
		if !strings.Contains(strings.ToLower(answer), "reset") {
			t.Errorf("confirming answer = %q, want it to name the action", answer)
		}
	}
}

// TestConfirmDialogWidthCoversLongQuestion pins a gap the header move
// itself opened up: confirmDialogLayout used to size itself purely off
// listSize(r.confirmDialog), which was fine while the question was
// itself a list item, but stopped covering it once the question moved
// into confirmDialogTitleBar instead (see openConfirm's own doc
// comment) — "Reset all N settings, in every category, to their
// defaults?" runs considerably longer than either "Cancel" or "Yes,
// reset everything", the two list items actually left.
func TestConfirmDialogWidthCoversLongQuestion(t *testing.T) {
	r, _ := newOptionsRoot(t)
	// A real terminal size, not the tiny 15x10 default an unsized test
	// Root otherwise has — openConfirm now clamps its own dialog to the
	// screen (see its own doc comment on the colored-stripes bug that
	// fixed), so this test needs a screen roomy enough that the
	// question genuinely fits without clamping, the same as any actual
	// terminal this dialog would ever appear on.
	r.SetRect(0, 0, 100, 40)

	r.resetAllOptions()

	headerWidth := tview.TaggedStringWidth(r.confirmDialogTitleBar.GetText(false))
	_, _, layoutWidth, _ := r.confirmDialogLayout.GetRect()
	if layoutWidth < headerWidth {
		t.Errorf("confirmDialogLayout width = %d, want at least %d to fit the question %q",
			layoutWidth, headerWidth, r.confirmDialogTitleBar.GetText(true))
	}
	r.cancelConfirm()
}

// TestConfirmDialogClampsAMessageWiderThanTheScreen pins a real,
// reported bug: a question longer than the screen itself used to
// leave the dialog's own x deeply negative and its width past the
// screen's edge entirely — tview rendered that as corrupted, flashing
// stripes rather than a readable (even if truncated) dialog.
func TestConfirmDialogClampsAMessageWiderThanTheScreen(t *testing.T) {
	r, _ := newOptionsRoot(t)
	r.SetRect(0, 0, 40, 20) // deliberately narrower than the message below

	r.openConfirm(strings.Repeat("word ", 40), "Yes", func() {})

	x, _, width, _ := r.confirmDialogLayout.GetRect()
	if x < 0 {
		t.Errorf("confirmDialogLayout x = %d, want it clamped to never go negative", x)
	}
	if width > 40 {
		t.Errorf("confirmDialogLayout width = %d, want it clamped to the 40-wide screen", width)
	}
}

// TestResetLeavesTheOptionsScreenOpen pins a real bug found by driving
// the screen live: the confirmation used to replace the whole overlay
// stack rather than layering on top of it, so confirming a reset closed
// the Options screen along with the dialog and dropped the user back in
// the file panel. Answering a question is no reason to close the thing
// that asked it.
func TestResetLeavesTheOptionsScreenOpen(t *testing.T) {
	r, _ := newOptionsRoot(t)

	row, ok := optionRowByKey(r, "show_hidden")
	if !ok {
		t.Fatal("no show_hidden row")
	}
	r.activateOptionRow(row)

	r.resetCurrentOptionCategory()
	confirmReset(t, r)

	if r.activePage != optionsPage {
		t.Errorf("activePage = %q after confirming a reset, want the Options screen still open", r.activePage)
	}
}

// TestCancellingAResetLeavesTheOptionsScreenOpen is the same guarantee
// for the other answer — backing out of a question must be even less
// destructive than answering it.
func TestCancellingAResetLeavesTheOptionsScreenOpen(t *testing.T) {
	r, _ := newOptionsRoot(t)

	r.resetAllOptions()
	r.cancelConfirm()

	if r.activePage != optionsPage {
		t.Errorf("activePage = %q after cancelling, want the Options screen still open", r.activePage)
	}
}

// TestLabelNameOptionsDefaultToTheirOwnColorName pins the fresh-install
// defaults — each one starting with the same hue and order as
// config.labelFallbackColors, against the generic "Label 1".."Label 9"
// an earlier revision used, which told a reader nothing about which
// color they'd actually be renaming.
func TestLabelNameOptionsDefaultToTheirOwnColorName(t *testing.T) {
	want := []string{
		"Red Alert", "Green Day", "Yellow Stone", "Blue Moon", "Orange Crush",
		"Purple Rain", "Teal Deer", "Pink Panther", "Lime Disease",
	}
	d := config.DefaultSettings()
	for id := 1; id <= config.MaxLabelID; id++ {
		if got := d.LabelName(id); got != want[id-1] {
			t.Errorf("DefaultSettings().LabelName(%d) = %q, want %q", id, got, want[id-1])
		}
	}
}

// TestLabelNameOptionsCarryTheirOwnLabelBackground checks that each of
// the nine "Label N name" options' own labelBackground resolves to
// exactly the color Panel.rowLabelBackground would also use for that
// same id — the one guarantee the whole feature rests on: the swatch
// behind the field you're renaming must be the same color the file list
// itself already shows for it, not an independently-picked one that
// could drift out of sync.
func TestLabelNameOptionsCarryTheirOwnLabelBackground(t *testing.T) {
	r, _ := newOptionsRoot(t)

	for id := 1; id <= config.MaxLabelID; id++ {
		opt := labelNameOptions()[id-1]
		if opt.labelBackground == nil {
			t.Fatalf("label %d option has no labelBackground", id)
		}
		want := r.theme.LabelBackground(id)
		if got := opt.labelBackground(r); got != want {
			t.Errorf("label %d option's labelBackground = %v, want %v (theme.LabelBackground(%d))", id, got, want, id)
		}
	}
}

// TestNonLabelOptionsHaveNoLabelBackground guards the other side: every
// option outside the "Label" category must leave labelBackground nil,
// so an ordinary row never silently gains a colored swatch it was never
// meant to have.
func TestNonLabelOptionsHaveNoLabelBackground(t *testing.T) {
	for _, cat := range optionCategories() {
		if cat.name == "Label" {
			continue
		}
		for _, opt := range cat.options {
			if opt.labelBackground != nil {
				t.Errorf("category %q option %q has a labelBackground set, want nil", cat.name, opt.key)
			}
		}
	}
}

// TestLabelCategoryRowsShowTheirOwnLabelColorBehindTheFieldName renders
// the "Label" category for real and reads back each row's own label
// cell Style — the end-to-end proof that renderOptions actually wires
// optionSpec.labelBackground through to the table, not just that the
// catalog entry carries the right color.
func TestLabelCategoryRowsShowTheirOwnLabelColorBehindTheFieldName(t *testing.T) {
	r, _ := newOptionsRoot(t)
	selectOptionCategory(t, r, "Label")
	r.renderOptions()

	for row := 0; row < config.MaxLabelID; row++ {
		id := row + 1
		want := r.theme.LabelBackground(id)
		cell := r.optionsTable.GetCell(row, optionsColLabel)
		_, got, _ := cell.Style.Decompose()
		if got != want {
			t.Errorf("row %d (label %d) cell background = %v, want %v", row, id, got, want)
		}
	}
}
