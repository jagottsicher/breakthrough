package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// newPrefixRoot is a Root on a real throwaway directory, ready for
// prefix-mode tests.
func newPrefixRoot(t *testing.T) *Root {
	t.Helper()
	isolateUserConfigFile(t)
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	return r
}

// pressPrefixVerb runs the whole gesture: Ctrl+_ then one rune.
func pressPrefixVerb(r *Root, verb rune) {
	r.PrefixShortcut()
	r.HandlePrefixKey(tcell.NewEventKey(tcell.KeyRune, verb, tcell.ModNone))
}

func TestPrefixStartsAndEndsCleanly(t *testing.T) {
	r := newPrefixRoot(t)

	r.PrefixShortcut()
	if !r.PrefixActive() {
		t.Fatal("prefix should be active after Ctrl+_")
	}
	r.HandlePrefixKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if r.PrefixActive() {
		t.Error("Escape should leave prefix mode")
	}
}

func TestPrefixKeyPressedTwiceCancels(t *testing.T) {
	r := newPrefixRoot(t)

	r.PrefixShortcut()
	// The second press arrives as an ordinary key first, and prefix mode
	// consumes it — which is what cancels. Either path must leave the
	// mode; neither may leave it stuck on.
	r.HandlePrefixKey(tcell.NewEventKey(tcell.KeyCtrlUnderscore, 0, tcell.ModNone))
	if r.PrefixActive() {
		t.Error("a second Ctrl+_ should leave prefix mode, not stay in it")
	}
}

func TestPrefixConsumesEveryKeyWhileActive(t *testing.T) {
	r := newPrefixRoot(t)
	r.PrefixShortcut()

	// A letter that is *also* a global shortcut elsewhere: prefix mode
	// has to swallow it, or one keypress would fire two things.
	if !r.HandlePrefixKey(tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone)) {
		t.Error("prefix mode should consume the verb key")
	}
	// And once the mode is over, it must stop consuming.
	if r.HandlePrefixKey(tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone)) {
		t.Error("keys must fall through again once prefix mode has ended")
	}
}

func TestPrefixVerbTogglesSplit(t *testing.T) {
	r := newPrefixRoot(t)

	pressPrefixVerb(r, 's')

	if !r.splitActive {
		t.Error("^_ s should turn split view on")
	}
	pressPrefixVerb(r, 's')
	if r.splitActive {
		t.Error("^_ s again should turn it off")
	}
}

func TestPrefixVerbFlipsOrientation(t *testing.T) {
	r := newPrefixRoot(t)
	before := r.settings.SplitStacked

	pressPrefixVerb(r, 'o')

	if r.settings.SplitStacked == before {
		t.Error("^_ o should flip the split orientation")
	}
}

func TestPrefixVerbOpensTabSwitcher(t *testing.T) {
	r := newPrefixRoot(t)

	pressPrefixVerb(r, 't')

	if r.activePage != tabSwitcherPage {
		t.Errorf("activePage = %q, want the tab switcher %q", r.activePage, tabSwitcherPage)
	}
}

func TestPrefixVerbOpensANewTab(t *testing.T) {
	r := newPrefixRoot(t)

	pressPrefixVerb(r, 'n')

	if r.tabCount() != 2 {
		t.Errorf("tab count = %d, want 2 after ^_ n", r.tabCount())
	}
}

func TestPrefixDigitJumpsToATab(t *testing.T) {
	r := newPrefixRoot(t)
	r.newTab(t.TempDir()) // tab 2, now active
	if r.activeTab != 1 {
		t.Fatalf("setup: activeTab = %d, want 1", r.activeTab)
	}

	pressPrefixVerb(r, '1')

	if r.activeTab != 0 {
		t.Errorf("activeTab = %d, want ^_ 1 to jump to the first tab", r.activeTab)
	}
}

// TestPrefixDigitZeroIsTheTenthTab pins the same "…0 means ten"
// convention Ctrl+0 and the tab strip's own numbering already use.
func TestPrefixDigitZeroIsTheTenthTab(t *testing.T) {
	r := newPrefixRoot(t)
	for i := 0; i < 9; i++ {
		r.newTab(t.TempDir())
	}
	if r.tabCount() != 10 {
		t.Fatalf("setup: %d tabs, want 10", r.tabCount())
	}
	r.switchToTab(0)

	pressPrefixVerb(r, '0')

	if r.activeTab != 9 {
		t.Errorf("activeTab = %d, want ^_ 0 to reach the tenth tab (index 9)", r.activeTab)
	}
}

func TestPrefixUnknownVerbReportsItself(t *testing.T) {
	r := newPrefixRoot(t)

	pressPrefixVerb(r, 'ß')

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want a notice on %q", r.activePage, errorPage)
	}
	got := strings.Join(strings.Fields(r.errorView.GetText(true)), " ")
	if !strings.Contains(got, "not a prefix command") {
		t.Errorf("notice = %q, want it to say the key isn't a prefix command", got)
	}
}

// TestPrefixIgnoresNonRuneKeys pins that an arrow key or F-key in
// prefix mode cancels rather than being guessed at as a verb.
func TestPrefixIgnoresNonRuneKeys(t *testing.T) {
	r := newPrefixRoot(t)
	r.PrefixShortcut()

	r.HandlePrefixKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))

	if r.PrefixActive() {
		t.Error("a non-printing key should leave prefix mode")
	}
	if r.activePage == errorPage {
		t.Error("...and should do it quietly, without a notice")
	}
}

func TestPrefixRefusesWhileAnOverlayIsOpen(t *testing.T) {
	r := newPrefixRoot(t)
	r.openHelp()

	r.PrefixShortcut()

	if r.PrefixActive() {
		t.Error("prefix mode should stand down while an overlay is open")
	}
}

// TestPrefixHintBarReplacesTheButtonsAndComesBack is the discoverability
// half of the feature: without the legend, a prefix tree is a
// memorization exercise (see keyprefix.go's own doc comment).
func TestPrefixHintBarReplacesTheButtonsAndComesBack(t *testing.T) {
	r := newPrefixRoot(t)
	ordinary := r.buttonBar.GetText(true)

	r.PrefixShortcut()
	hint := r.buttonBar.GetText(true)
	if hint == ordinary {
		t.Fatal("the button bar should become the verb legend in prefix mode")
	}
	for _, want := range []string{"s Split", "t Tabs", "Esc cancel"} {
		if !strings.Contains(hint, want) {
			t.Errorf("legend = %q, want it to list %q", hint, want)
		}
	}

	r.HandlePrefixKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if got := r.buttonBar.GetText(true); got != ordinary {
		t.Errorf("button bar = %q, want the ordinary buttons back (%q)", got, ordinary)
	}
}

// TestPrefixHintBarClearsClickTargets pins that a click on the legend
// can't trigger whatever button used to occupy those columns.
func TestPrefixHintBarClearsClickTargets(t *testing.T) {
	r := newPrefixRoot(t)
	if len(r.buttonBarSpans) == 0 {
		t.Fatal("setup: expected the ordinary button bar to have click spans")
	}

	r.PrefixShortcut()

	if len(r.buttonBarSpans) != 0 {
		t.Errorf("got %d click spans in prefix mode, want none", len(r.buttonBarSpans))
	}
}

// TestPrefixTreeCoversEveryFunctionKey is the promise the whole feature
// rests on: a MacBook user who never presses an F-key must still reach
// everything the F-keys reach.
func TestPrefixTreeCoversEveryFunctionKey(t *testing.T) {
	// F3 (mouse) and F5/F6 (split/orientation) plus F1/F2/F4.
	for _, verb := range []rune{'?', 'r', 'm', 't', 's', 'o'} {
		if _, ok := prefixVerbFor(verb); !ok {
			t.Errorf("no prefix verb %q — an F-key would have no keyboard alternative", string(verb))
		}
	}
}

// TestPrefixVerbsAreUnique guards the one mistake this table shape
// invites: two entries claiming the same letter, where the first
// silently wins.
func TestPrefixVerbsAreUnique(t *testing.T) {
	seen := map[rune]string{}
	for _, group := range prefixTree() {
		for _, verb := range group.verbs {
			if other, dup := seen[verb.key]; dup {
				t.Errorf("verb %q is claimed by both %q and %q", string(verb.key), other, verb.label)
			}
			seen[verb.key] = verb.label
		}
	}
	// Digits are handled before the tree is consulted, so a verb bound
	// to one would be unreachable (see handlePrefixKey).
	for key := range seen {
		if key >= '0' && key <= '9' {
			t.Errorf("verb %q shadows the tab-number shortcut and can never fire", string(key))
		}
	}
}
