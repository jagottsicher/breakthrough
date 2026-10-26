package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// drawConsigliereLayout forces a real Flex layout pass (via a
// simulation screen) so consigliereHistory's own rect — and therefore
// consigliereScrollbar.maxOffset() — actually reflects the size just
// given to consigliereLayout: Flex only resolves a child's own rect
// during Draw, never synchronously when SetRect is called on the
// parent (see repositionConsigliereSidebar's own doc comment) — the
// same pattern drawCompareDiff already establishes in
// comparediff_scrollbar_test.go for the identical reason.
func drawConsigliereLayout(t *testing.T, r *Root, width, height int) {
	t.Helper()
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	t.Cleanup(screen.Fini)
	screen.SetSize(width, height)
	r.consigliereLayout.Draw(screen)
}

// TestChordGoAIConsigliereOpensTheSidebarAndFocusesInput pins "gC"
// (see keymap.go): opens the sidebar and hands keyboard focus straight
// to the question field, so typing can start immediately — unlike
// Details ("I"), which deliberately never touches focus (see
// showConsigliere's own doc comment on why this one does).
func TestChordGoAIConsigliereOpensTheSidebarAndFocusesInput(t *testing.T) {
	root := newPlainKeyRoot(t)

	root.HandlePlainKey(runeEvent('g'))
	root.HandlePlainKey(runeEvent('C'))

	if !root.consigliereVisible {
		t.Fatal("\"gC\" should have shown the AI Consigliere sidebar")
	}
	if !root.consigliereInput.HasFocus() {
		t.Error("opening the sidebar should have focused the question field")
	}
}

// TestEscapeInConsigliereInputClosesTheSidebar pins the real close
// path once the sidebar is open: acceptsPlainKeyCommand (see
// keymap.go) only reads "gC" as a chord while the panel table itself
// has focus, so once consigliereInput has it (immediately after
// opening — see above), Escape/the close glyph/a click outside are
// what actually close it, not a second "gC" keystroke (see
// toggleConsigliere's own doc comment).
func TestEscapeInConsigliereInputClosesTheSidebar(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.showConsigliere()

	root.handleConsigliereInputDone(tcell.KeyEscape)

	if root.consigliereVisible {
		t.Error("Escape should have closed the sidebar")
	}
}

// TestToggleConsigliereClosesAnAlreadyOpenSidebar pins
// toggleConsigliere's own "close" branch directly — the path a future
// button (per the spec) or ConsigliereShortcut would use, since a
// literal second "gC" keystroke can no longer reach it once the input
// field has focus (see the two tests above).
func TestToggleConsigliereClosesAnAlreadyOpenSidebar(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.showConsigliere()

	root.toggleConsigliere()

	if root.consigliereVisible {
		t.Error("toggleConsigliere should have closed the already-open sidebar")
	}
}

// TestConsigliereShortcutOpensThenClosesItself pins ConsigliereShortcut's
// own guard (see consigliere.go): unlike a hypothetical reuse of
// acceptsGlobalShortcut (which requires activePage == ""), this one
// must still fire while the sidebar itself is open, since it's not
// tracked via activePage at all (it's non-modal, the same as Details)
// — otherwise its own "close" half would be permanently unreachable.
func TestConsigliereShortcutOpensThenClosesItself(t *testing.T) {
	root := newPlainKeyRoot(t)

	root.ConsigliereShortcut()
	if !root.consigliereVisible {
		t.Fatal("expected the first call to show the sidebar")
	}

	root.ConsigliereShortcut()
	if root.consigliereVisible {
		t.Error("a second ConsigliereShortcut call should have closed the sidebar")
	}
}

// TestShowConsigliereStartsAFreshSession pins the spec's own
// "Schließen und erneutes Öffnen startet eine frische Konversation":
// leftover messages from a previous session must not still be there
// the next time the sidebar opens.
func TestShowConsigliereStartsAFreshSession(t *testing.T) {
	root := newPlainKeyRoot(t)

	root.showConsigliere()
	root.submitConsigliereMessage("erste Frage")
	if len(root.consigliereMessages) == 0 {
		t.Fatal("submitConsigliereMessage should have recorded the question")
	}
	root.hideConsigliere()

	root.showConsigliere()
	if len(root.consigliereMessages) != 0 {
		t.Errorf("consigliereMessages = %v, want an empty, fresh session", root.consigliereMessages)
	}
}

// TestSubmitConsigliereMessageShowsTheQuestionImmediately pins
// submitConsigliereMessage's own synchronous half: the user's own
// question renders into the history right away, before the provider
// (here consigliere.Mock, see NewRoot) has answered — the same
// "assert only the synchronous half" limitation
// TestRunTestCloudRemoteShowsTestingImmediately already documents,
// since nothing here drains a live Application.Run() event loop for
// the QueueUpdateDraw hop the reply itself arrives through.
func TestSubmitConsigliereMessageShowsTheQuestionImmediately(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.showConsigliere()

	root.submitConsigliereMessage("seit 3 Tagen geht mein USB-Geraet nicht mehr")

	if got := root.consigliereHistory.GetText(true); got == "" {
		t.Fatal("history should already show the question")
	}
	if root.consigliereCancel == nil {
		t.Error("submitConsigliereMessage should have started the reply in the background")
	}
}

// TestTabInConsigliereInputMovesFocusToHistory pins the sidebar's own
// real keyboard path to the scrollable history: InputField's own
// SetDoneFunc claims Tab before it could ever reach CycleFocusShortcut
// (see handleConsigliereInputDone's own doc comment), so without this,
// there would be no way to reach arrow/PageUp/PageDown/vi-key
// scrolling at all once the input has real focus.
func TestTabInConsigliereInputMovesFocusToHistory(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.showConsigliere()

	root.handleConsigliereInputDone(tcell.KeyTab)

	if !root.consigliereHistory.HasFocus() {
		t.Error("Tab should have moved focus to consigliereHistory")
	}
}

// TestTabInConsigliereInputSkipsHistoryWhenNothingToScroll pins
// handleConsigliereInputDone's own other branch: on a generously sized
// Root (unlike newPlainKeyRoot's own tiny default — see
// TestCycleFocusShortcutSkipsConsigliereHistoryWhenNothingToScroll's
// own doc comment), consigliereSplashArt's short content fits without
// scrolling, so Tab must land back on the panel directly instead of a
// history view with nothing in it to scroll to.
func TestTabInConsigliereInputSkipsHistoryWhenNothingToScroll(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.SetRect(0, 0, 200, 100)
	root.showConsigliere()
	drawConsigliereLayout(t, root, 200, 100)

	root.handleConsigliereInputDone(tcell.KeyTab)

	if !root.panel.table.HasFocus() {
		t.Error("Tab should have moved focus straight back to the panel")
	}
}

// TestEscapeInConsigliereHistoryClosesTheSidebar pins
// captureConsigliereHistoryKey: Escape must still close the sidebar
// once focus has moved to the history (see the test above), even
// though that widget deliberately has no SetDoneFunc of its own (which
// would have swallowed Tab too — see captureConsigliereHistoryKey's
// own doc comment).
func TestEscapeInConsigliereHistoryClosesTheSidebar(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.showConsigliere()
	root.app.SetFocus(root.consigliereHistory)

	root.captureConsigliereHistoryKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))

	if root.consigliereVisible {
		t.Error("Escape should have closed the sidebar from the history view too")
	}
}

// TestConsigliereTitleBarReflectsHistoryFocusOnly pins the user's own
// explicit request: the title bar reads as active while
// consigliereHistory specifically has real keyboard focus, and only
// then — not while consigliereInput has it, even though that's the
// widget showConsigliere itself normally focuses first.
func TestConsigliereTitleBarReflectsHistoryFocusOnly(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.showConsigliere() // focuses consigliereInput

	theme := root.theme
	if got := root.consigliereTitleBar.GetBackgroundColor(); got != theme.InputBackground {
		t.Errorf("title bar background with the input focused = %v, want the unfocused InputBackground", got)
	}

	root.app.SetFocus(root.consigliereHistory)
	if got := root.consigliereTitleBar.GetBackgroundColor(); got != theme.InputFocusedBackground {
		t.Errorf("title bar background with the history focused = %v, want InputFocusedBackground", got)
	}

	root.app.SetFocus(root.consigliereInput)
	if got := root.consigliereTitleBar.GetBackgroundColor(); got != theme.InputBackground {
		t.Errorf("title bar background back on the input = %v, want InputBackground again", got)
	}
}

// TestShowConsigliereHidesAnOpenDetailsSidebar and
// TestShowDetailsSidebarHidesAnOpenConsigliere pin the mutual
// exclusion between the two sidebars: both dock to the exact same
// rect (see consigliereSize/detailsSidebarSize), so showing one while
// the other is already open must close the other first rather than
// silently stacking them.
func TestShowConsigliereHidesAnOpenDetailsSidebar(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.showDetailsSidebar()

	root.showConsigliere()

	if root.detailsSidebarVisible {
		t.Error("showConsigliere should have closed the open Details sidebar")
	}
}

func TestShowDetailsSidebarHidesAnOpenConsigliere(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.showConsigliere()

	root.showDetailsSidebar()

	if root.consigliereVisible {
		t.Error("showDetailsSidebar should have closed the open Consigliere sidebar")
	}
}

// TestCycleFocusShortcutReachesConsigliereInputFromThePanel pins the
// real keyboard path back into the question field once focus has left
// it some other way (a mouse click back into the panel, or tabbing on
// into consigliereHistory) — the user's own explicit question ("wie
// kommt man per Tastatur dann wieder ins Eingabefeld?"). Closing and
// reopening the sidebar would also reach it, but at the cost of
// wiping the session (see showConsigliere's own "fresh session every
// time" contract) — Tab must not require that.
func TestCycleFocusShortcutReachesConsigliereInputFromThePanel(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.showConsigliere()
	root.app.SetFocus(root.panel.table) // simulates a mouse click back into the panel

	if !root.CycleFocusShortcut() {
		t.Fatal("Tab from the panel, with the sidebar open, should report true")
	}
	if !root.consigliereInput.HasFocus() {
		t.Error("Tab from the panel should have reached consigliereInput")
	}
}

// TestCycleFocusShortcutLoopsPanelInputHistoryPanel pins the full loop
// Tab walks through once the sidebar is open AND its history actually
// needs scrolling: panel -> consigliereInput -> consigliereHistory ->
// back to panel. Relies on newPlainKeyRoot's own default, uninitialized
// Root rect (tview.Box's own 15x10 default — see showError's own doc
// comment on the same default elsewhere) being far too small to fit
// consigliereSplashArt's own ~11 lines, i.e. consigliereScrollbar.
// maxOffset() > 0 — see TestCycleFocusShortcutSkipsConsigliereHistoryWhenNothingToScroll
// for the opposite case on a generously sized Root instead.
func TestCycleFocusShortcutLoopsPanelInputHistoryPanel(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.showConsigliere()
	root.app.SetFocus(root.panel.table)

	if root.consigliereScrollbar.maxOffset() <= 0 {
		t.Fatal("test precondition: expected the default tiny Root rect to make scrolling necessary")
	}

	root.CycleFocusShortcut()
	if !root.consigliereInput.HasFocus() {
		t.Fatal("first Tab should have reached consigliereInput")
	}

	root.CycleFocusShortcut()
	if !root.consigliereHistory.HasFocus() {
		t.Fatal("second Tab should have reached consigliereHistory")
	}

	root.CycleFocusShortcut()
	if !root.panel.table.HasFocus() {
		t.Error("third Tab should have wrapped back around to the panel")
	}
}

// TestCycleFocusShortcutSkipsConsigliereHistoryWhenNothingToScroll pins
// the user's own explicit request: consigliereHistory must not be a
// Tab stop at all while there's nothing in it to scroll to (a
// generously sized Root here, unlike newPlainKeyRoot's own tiny
// default, is what makes consigliereSplashArt's own short content fit
// without scrolling) — Tab from consigliereInput wraps straight back
// to the panel instead of landing on a dead end.
func TestCycleFocusShortcutSkipsConsigliereHistoryWhenNothingToScroll(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.SetRect(0, 0, 200, 100)
	root.showConsigliere()
	drawConsigliereLayout(t, root, 200, 100)
	root.app.SetFocus(root.panel.table)

	if root.consigliereScrollbar.maxOffset() > 0 {
		t.Fatal("test precondition: expected the generous Root rect to make scrolling unnecessary")
	}

	root.CycleFocusShortcut()
	if !root.consigliereInput.HasFocus() {
		t.Fatal("first Tab should still have reached consigliereInput")
	}

	root.CycleFocusShortcut()
	if !root.panel.table.HasFocus() {
		t.Error("second Tab should have wrapped straight back to the panel, skipping consigliereHistory")
	}
}

// TestHideConsigliereCancelsAPendingReply pins hideConsigliere's own
// cleanup: a reply still in flight when the sidebar closes must be
// cancelled, not left to land a QueueUpdateDraw into a session the
// user already left.
func TestHideConsigliereCancelsAPendingReply(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.showConsigliere()
	root.submitConsigliereMessage("Frage")
	if root.consigliereCancel == nil {
		t.Fatal("expected a pending cancel func after submitting")
	}

	root.hideConsigliere()

	if root.consigliereCancel != nil {
		t.Error("hideConsigliere should have cleared consigliereCancel")
	}
}

// TestHideConsigliereRestoresFocusToWhatHadItBefore pins the real
// bug a hardcoded r.panel.table fallback had: opened from inside a
// full-screen modal (Help stands in for Log Audit/Git Breach here —
// the mechanism is the same, see closeConsigliereIfHostPageChanged's
// own doc comment), closing it must hand focus back to that modal's
// own widget, not the panel underneath it, which isn't even visible.
func TestHideConsigliereRestoresFocusToWhatHadItBefore(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.openHelp()
	before := root.app.GetFocus()

	root.showConsigliere()
	root.hideConsigliere()

	if got := root.app.GetFocus(); got != before {
		t.Errorf("focus after hideConsigliere = %v, want %v (Help's own widget, not the panel)", got, before)
	}
}

// TestOpeningAnotherOverlayClosesConsigliere pins
// closeConsigliereIfHostPageChanged's own pushOverlay-side hook: per
// the user's own explicit request, the sidebar is about whatever view
// was open when it was asked for — opening a different one (Help,
// standing in for any pushOverlay caller) closes it.
func TestOpeningAnotherOverlayClosesConsigliere(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.showConsigliere()

	root.openHelp()

	if root.consigliereVisible {
		t.Error("opening another overlay should have closed the already-open Consigliere sidebar")
	}
}

// TestClosingTheHostOverlayClosesConsigliere pins
// closeConsigliereIfHostPageChanged's own hideOverlay-side hook: the
// sidebar must not survive its own host view closing out from under
// it.
func TestClosingTheHostOverlayClosesConsigliere(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.openHelp()
	root.showConsigliere()

	root.hideOverlay() // closes Help

	if root.consigliereVisible {
		t.Error("closing the host overlay should have closed Consigliere too")
	}
}

// TestClickOnConsigliereDoesNotCloseTheHostOverlay pins the real,
// confirmed bug in captureOutsideClick: the sidebar is non-modal, so
// neither overlayStack nor r.activeWidget ever knew about its own
// rect — a click landing on it, while some other modal is the active
// overlay, used to read as "outside that overlay" and close it
// instead.
func TestClickOnConsigliereDoesNotCloseTheHostOverlay(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.SetRect(0, 0, 100, 40)
	root.openHelp()
	root.showConsigliere()

	x, y, _, _ := root.consigliereLayout.GetRect()
	root.captureOutsideClick(tview.MouseLeftClick, tcellMouseEventAt(x, y))

	if root.activePage != helpPage {
		t.Errorf("activePage = %q, want helpPage still open", root.activePage)
	}
	if !root.consigliereVisible {
		t.Error("the sidebar itself should still be visible")
	}
}

// TestClickOutsideConsigliereWithNothingElseOpenClosesIt pins the
// other half of the same fix: with nothing else open at all, a click
// outside the sidebar must still close it — the standing claim this
// package's own doc comments already made ("a click outside" closes
// it), which activePage == "" used to short-circuit before ever
// reaching Consigliere-specific handling.
func TestClickOutsideConsigliereWithNothingElseOpenClosesIt(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.SetRect(0, 0, 100, 40)
	root.showConsigliere()

	root.captureOutsideClick(tview.MouseLeftClick, tcellMouseEventAt(0, 0))

	if root.consigliereVisible {
		t.Error("a click outside the sidebar, with nothing else open, should have closed it")
	}
}
