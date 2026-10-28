package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/consigliere"
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

// TestClosingConsigliereWithAnActiveConversationAsksFirst pins the
// user's own explicit request: closing the sidebar discards the whole
// conversation outright (see showConsigliere's own "fresh session
// every time" behavior), so once a real exchange exists, every
// user-initiated close path must ask before actually losing it.
func TestClosingConsigliereWithAnActiveConversationAsksFirst(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.showConsigliere()
	root.consigliereMessages = []consigliere.Message{{Role: consigliere.RoleUser, Text: "hello"}}

	root.handleConsigliereInputDone(tcell.KeyEscape)

	if !root.consigliereVisible {
		t.Error("the sidebar should still be open, pending the confirmation")
	}
	if root.activePage != confirmPage {
		t.Errorf("activePage = %q, want the confirmation dialog open", root.activePage)
	}

	root.acceptConfirm()

	if root.consigliereVisible {
		t.Error("confirming should have closed the sidebar")
	}
}

// TestClosingConsigliereWithAnActiveConversationCancelLeavesItOpen is
// the other half: declining the confirmation must leave the
// conversation exactly as it was, not lose it anyway.
func TestClosingConsigliereWithAnActiveConversationCancelLeavesItOpen(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.showConsigliere()
	root.consigliereMessages = []consigliere.Message{{Role: consigliere.RoleUser, Text: "hello"}}

	root.handleConsigliereInputDone(tcell.KeyEscape)
	root.cancelConfirm()

	if !root.consigliereVisible {
		t.Error("cancelling the confirmation should have left the sidebar open")
	}
	if len(root.consigliereMessages) != 1 {
		t.Errorf("consigliereMessages = %v, want the conversation untouched", root.consigliereMessages)
	}
}

// TestClosingConsigliereWithNoMessagesSkipsTheConfirmation pins the
// other side of the same fix: with nothing asked yet, there is nothing
// to lose, so closing stays immediate rather than adding friction to
// the "opened by mistake" case.
func TestClosingConsigliereWithNoMessagesSkipsTheConfirmation(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.showConsigliere()

	root.handleConsigliereInputDone(tcell.KeyEscape)

	if root.consigliereVisible {
		t.Error("closing with no messages yet should not have asked for confirmation")
	}
	if root.activePage == confirmPage {
		t.Error("no confirmation dialog should have opened")
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
// (buildConsigliereProvider's own real Provider, which errors out
// immediately here since TestMain blanks the env-var key tier and
// nothing unlocks the encrypted one) has answered — the same
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

// TestBuildConsigliereProviderForGeminiUsesGeminiDefaults pins the
// provider-specific wiring buildConsigliereProvider adds for Gemini —
// same OpenAICompat shape OpenAI/DeepSeek already use, pointed at
// Gemini's own OpenAI-compatible endpoint and default model.
func TestBuildConsigliereProviderForGeminiUsesGeminiDefaults(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.settings.ConsigliereProvider = "gemini"

	got, ok := root.buildConsigliereProvider().(consigliere.OpenAICompat)
	if !ok {
		t.Fatalf("buildConsigliereProvider() = %T, want consigliere.OpenAICompat", root.buildConsigliereProvider())
	}
	if got.BaseURL != consigliere.GeminiDefaultBaseURL {
		t.Errorf("BaseURL = %q, want %q", got.BaseURL, consigliere.GeminiDefaultBaseURL)
	}
	if got.Model != consigliere.GeminiDefaultModel {
		t.Errorf("Model = %q, want %q", got.Model, consigliere.GeminiDefaultModel)
	}
}

// TestBuildConsigliereProviderForOllamaNeedsNoRealKey pins the one
// deliberate exception to every other provider here: Ollama's own
// KeyFunc must succeed with a placeholder, never consulting
// internal/apikey (and therefore never erroring with
// errConsigliereNoAPIKey) even though nothing at all is configured in
// Options for it.
func TestBuildConsigliereProviderForOllamaNeedsNoRealKey(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.settings.ConsigliereProvider = "ollama"

	got, ok := root.buildConsigliereProvider().(consigliere.OpenAICompat)
	if !ok {
		t.Fatalf("buildConsigliereProvider() = %T, want consigliere.OpenAICompat", root.buildConsigliereProvider())
	}
	if got.BaseURL != consigliere.OllamaDefaultBaseURL {
		t.Errorf("BaseURL = %q, want %q", got.BaseURL, consigliere.OllamaDefaultBaseURL)
	}
	key, err := got.APIKey()
	if err != nil {
		t.Fatalf("APIKey() = _, %v, want no error even with nothing configured", err)
	}
	if key == "" {
		t.Error("APIKey() returned an empty string, want the documented placeholder")
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

// TestConsigliereTitleBarShowsMachiavelliModeRegardlessOfFocus pins
// the unmissable reminder: while Machiavelli Mode is on, the title
// bar's own warning color and label override the ordinary
// focused/unfocused look entirely, rather than only showing on one of
// the two states.
func TestConsigliereTitleBarShowsMachiavelliModeRegardlessOfFocus(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.settings.ConsigliereHardcoreMode = true
	root.showConsigliere()

	theme := root.theme
	if got := root.consigliereTitleBar.GetBackgroundColor(); got != theme.ErrorBackground {
		t.Errorf("title bar background with the input focused = %v, want ErrorBackground", got)
	}
	if !strings.Contains(root.consigliereTitleBar.GetText(false), "MACHIAVELLI") {
		t.Errorf("title bar text = %q, want it to mention Machiavelli Mode", root.consigliereTitleBar.GetText(false))
	}

	root.app.SetFocus(root.consigliereHistory)
	if got := root.consigliereTitleBar.GetBackgroundColor(); got != theme.ErrorBackground {
		t.Errorf("title bar background with the history focused = %v, want ErrorBackground still", got)
	}
}

// TestShowConsigliereLeavesAnOpenDetailsSidebarOpen and
// TestShowDetailsSidebarLeavesAnOpenConsigliereOpen pin the user's own
// explicit request: the two no longer need to be mutually exclusive —
// Details can simply stay open behind Consigliere (and vice versa),
// rather than one silently closing the other.
func TestShowConsigliereLeavesAnOpenDetailsSidebarOpen(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.showDetailsSidebar()

	root.showConsigliere()

	if !root.detailsSidebarVisible {
		t.Error("showConsigliere should not have closed the open Details sidebar")
	}
}

func TestShowDetailsSidebarLeavesAnOpenConsigliereOpen(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.showConsigliere()

	root.showDetailsSidebar()

	if !root.consigliereVisible {
		t.Error("showDetailsSidebar should not have closed the open Consigliere sidebar")
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
// full-screen modal (Help stands in for Log Audit/Git Breach here),
// closing it must hand focus back to that modal's own widget, not the
// panel underneath it, which isn't even visible.
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

// TestOpeningAnotherOverlayLeavesConsigliereOpen and
// TestClosingTheHostOverlayLeavesConsigliereOpen pin the user's own
// explicit reversal of an earlier decision: the sidebar used to close
// itself the moment the page it was opened from stopped being active
// (e.g. drilling from Log Audit's own selection screen into a
// specific file's viewer) — per the user's own explicit report, that
// made perfectly ordinary within-task navigation lose the sidebar for
// no reason, so it now simply stays open across any host page change,
// closing only via Escape/the close glyph/a click outside, same as
// ever. Help stands in for any pushOverlay/hideOverlay caller.
func TestOpeningAnotherOverlayLeavesConsigliereOpen(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.showConsigliere()

	root.openHelp()

	if !root.consigliereVisible {
		t.Error("opening another overlay should not have closed the already-open Consigliere sidebar")
	}
}

func TestClosingTheHostOverlayLeavesConsigliereOpen(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.openHelp()
	root.showConsigliere()

	root.hideOverlay() // closes Help

	if !root.consigliereVisible {
		t.Error("closing the host overlay should not have closed Consigliere too")
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

// TestClickOutsideConsigliereWithNothingElseOpenLeavesItOpen pins a
// real, reported bug fix: unlike every modal overlay's own "click
// outside closes it" convention (captureOutsideClick), the sidebar is
// deliberately non-modal and must stay open through a click on the
// panel underneath — only its own close glyph or Escape should close
// it (see captureOutsideClick's own doc comment).
func TestClickOutsideConsigliereWithNothingElseOpenLeavesItOpen(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.SetRect(0, 0, 100, 40)
	root.showConsigliere()

	root.captureOutsideClick(tview.MouseLeftClick, tcellMouseEventAt(0, 0))

	if !root.consigliereVisible {
		t.Error("a click outside the sidebar, with nothing else open, should not have closed it")
	}
}

// TestCycleFocusShortcutDoesNotMatchConsigliereInsideAModal pins step
// one of the real fix for a reported bug: once some full-screen modal
// (Help stands in for Log Audit/Git Breach) is the active page, the
// global focus cycle must no longer treat consigliereInput as one of
// its own stops at all — matching it there, before the host screen's
// own local Tab handling ever ran, is exactly what used to strand the
// keyboard on the hidden file panel.
func TestCycleFocusShortcutDoesNotMatchConsigliereInsideAModal(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.openHelp()
	root.showConsigliere()
	if !root.consigliereInput.HasFocus() {
		t.Fatal("test precondition: expected consigliereInput to have focus")
	}

	if root.CycleFocusShortcut() {
		t.Error("CycleFocusShortcut should report false — consigliereInput must not be one of its own stops while a modal is open")
	}
	if !root.consigliereInput.HasFocus() {
		t.Error("focus should not have moved at all")
	}
}

// TestTabInConsigliereInputExitsToTheHostNotTheHiddenPanel pins the
// real fix: exiting consigliere's own loop (via consigliereExitFocus)
// must land on whatever had focus on the host screen when the sidebar
// was opened, not a hardcoded r.panel.table — which, with a
// full-screen modal open, isn't even visible. Help stands in for Log
// Audit/Git Breach; the mechanism is host-agnostic.
func TestTabInConsigliereInputExitsToTheHostNotTheHiddenPanel(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.SetRect(0, 0, 200, 100)
	root.openHelp()
	before := root.app.GetFocus()
	root.showConsigliere()
	drawConsigliereLayout(t, root, 200, 100) // makes the splash art fit without scrolling

	root.handleConsigliereInputDone(tcell.KeyTab)

	if got := root.app.GetFocus(); got != before {
		t.Errorf("focus after Tab = %v, want %v (Help's own widget, not the panel)", got, before)
	}
}

// TestBacktabInConsigliereInputExitsToTheHost pins Backtab's own,
// unconditional case: backward out of the very first stop in
// consigliere's own loop always exits immediately, regardless of
// whether there's anything to scroll to.
func TestBacktabInConsigliereInputExitsToTheHost(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.openHelp()
	before := root.app.GetFocus()
	root.showConsigliere()

	root.handleConsigliereInputDone(tcell.KeyBacktab)

	if got := root.app.GetFocus(); got != before {
		t.Errorf("focus after Backtab = %v, want %v", got, before)
	}
}

// TestTabInConsigliereHistoryExitsToTheHost and
// TestBacktabInConsigliereHistoryReturnsToInput pin
// captureConsigliereHistoryKey's own, newly-added Tab/Backtab cases —
// only ever reached once a full-screen modal has excluded consigliere
// from the global focus cycle (see focusCycleStops' own doc comment),
// since that cycle still claims both directly otherwise.
func TestTabInConsigliereHistoryExitsToTheHost(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.openHelp()
	before := root.app.GetFocus()
	root.showConsigliere()
	root.app.SetFocus(root.consigliereHistory)

	root.captureConsigliereHistoryKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))

	if got := root.app.GetFocus(); got != before {
		t.Errorf("focus after Tab = %v, want %v", got, before)
	}
}

func TestBacktabInConsigliereHistoryReturnsToInput(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.openHelp()
	root.showConsigliere()
	root.app.SetFocus(root.consigliereHistory)

	root.captureConsigliereHistoryKey(tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone))

	if !root.consigliereInput.HasFocus() {
		t.Error("Backtab from the history should have returned focus to the input field")
	}
}

// TestConsigliereSizeIsAThirdOfScreenWidthAndHalfThatTall pins the
// user's own explicit request: width is a third of the whole screen,
// height is half that many characters — the ratio that reads as
// visually square given a terminal's own roughly-2:1 character cell,
// not a literal width==height count.
func TestConsigliereSizeIsAThirdOfScreenWidthAndHalfThatTall(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.SetRect(0, 0, 120, 40)

	width, height := root.consigliereSize()

	if want := 120 / 3; width != want {
		t.Errorf("width = %d, want %d (a third of the screen)", width, want)
	}
	if want := width / 2; height != want {
		t.Errorf("height = %d, want %d (half the width)", height, want)
	}
}

// TestConsigliereSizeSameOverTheBarePanelAndAModal pins that this
// geometry — unlike the old fill-the-column shape it replaced — no
// longer depends on whether the sidebar is docked over the bare file
// panel or a full-screen modal (Help stands in for Log Audit/Git
// Breach here).
func TestConsigliereSizeSameOverTheBarePanelAndAModal(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.SetRect(0, 0, 120, 40)

	barePanelWidth, barePanelHeight := root.consigliereSize()

	root.openHelp()
	modalWidth, modalHeight := root.consigliereSize()

	if modalWidth != barePanelWidth || modalHeight != barePanelHeight {
		t.Errorf("size over a modal = (%d, %d), want the same (%d, %d) as over the bare panel", modalWidth, modalHeight, barePanelWidth, barePanelHeight)
	}
}

// TestConsigliereLayoutDockedRightAndCenteredVertically pins
// repositionConsigliereSidebar's own placement: flush against the
// right edge, vertically centered on the whole screen.
func TestConsigliereLayoutDockedRightAndCenteredVertically(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.SetRect(0, 0, 120, 40)

	root.showConsigliere()

	width, height := root.consigliereSize()
	x, y, gotWidth, gotHeight := root.consigliereLayout.GetRect()

	if gotWidth != width || gotHeight != height {
		t.Errorf("consigliereLayout size = (%d, %d), want (%d, %d) from consigliereSize", gotWidth, gotHeight, width, height)
	}
	if wantX := 120 - width; x != wantX {
		t.Errorf("consigliereLayout x = %d, want %d (flush right)", x, wantX)
	}
	if wantY := (40 - height) / 2; y != wantY {
		t.Errorf("consigliereLayout y = %d, want %d (vertically centered on the whole screen)", y, wantY)
	}
}

// TestConsigliereTitleBarCloseGlyphClickClosesIt pins
// dragConsigliereMouseCapture's own close-glyph case — MouseLeftDown,
// not MouseLeftClick, since drag-start detection now needs to see the
// press itself (see its own doc comment).
func TestConsigliereTitleBarCloseGlyphClickClosesIt(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.SetRect(0, 0, 100, 40)
	root.showConsigliere()
	// Forces a real Flex layout pass so consigliereTitleBar's own
	// rect actually matches consigliereLayout's — see
	// drawConsigliereLayout's own doc comment.
	drawConsigliereLayout(t, root, 100, 40)

	x, y, width, _ := root.consigliereTitleBar.GetRect()
	closeX := x + toolWindowCloseButtonCol(0, width)
	captured, _ := root.captureConsigliereTitleBarMouse(tview.MouseLeftDown, tcell.NewEventMouse(closeX, y, tcell.ButtonNone, 0))

	if captured != tview.MouseConsumed {
		t.Error("clicking the close glyph should consume the click")
	}
	if root.consigliereVisible {
		t.Error("clicking the close glyph should have closed the sidebar")
	}
}

// TestDraggingConsigliereTitleBarMovesIt pins the user's own explicit
// request: press and drag anywhere else on the title bar moves the
// whole sidebar.
func TestDraggingConsigliereTitleBarMovesIt(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.SetRect(0, 0, 100, 40)
	root.showConsigliere()
	drawConsigliereLayout(t, root, 100, 40)

	startX, startY, width, height := root.consigliereLayout.GetRect()
	titleY := startY // the title bar is the layout's own first row
	grabX := startX + 2

	root.captureConsigliereTitleBarMouse(tview.MouseLeftDown, tcell.NewEventMouse(grabX, titleY, tcell.Button1, 0))
	if !root.consigliereDragging {
		t.Fatal("pressing on the title bar (away from the close glyph) should have started a drag")
	}

	root.dragConsigliereMouseCapture(tview.MouseMove, tcell.NewEventMouse(grabX-5, titleY-3, tcell.Button1, 0))
	newX, newY, newWidth, newHeight := root.consigliereLayout.GetRect()
	if newX == startX && newY == startY {
		t.Error("dragging should have moved the sidebar")
	}
	if newWidth != width || newHeight != height {
		t.Errorf("drag changed size: %dx%d, want unchanged %dx%d (no resizing)", newWidth, newHeight, width, height)
	}

	root.dragConsigliereMouseCapture(tview.MouseLeftUp, tcell.NewEventMouse(grabX-5, titleY-3, tcell.ButtonNone, 0))
	if root.consigliereDragging {
		t.Error("releasing the mouse should have ended the drag")
	}
	if !root.consigliereManuallyPositioned {
		t.Error("a completed drag should have set consigliereManuallyPositioned")
	}
}

// TestConsigliereManuallyPositionedSurvivesResize pins
// repositionConsigliereSidebar's own doc comment: once dragged, a
// live terminal resize must not silently snap the sidebar back to its
// docked position.
func TestConsigliereManuallyPositionedSurvivesResize(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.SetRect(0, 0, 100, 40)
	root.showConsigliere()
	root.moveConsigliere(10, 5)
	x, y, _, _ := root.consigliereLayout.GetRect()

	root.SetRect(0, 0, 120, 45) // simulates a live terminal resize
	root.repositionConsigliereSidebar()

	gotX, gotY, _, _ := root.consigliereLayout.GetRect()
	if gotX != x || gotY != y {
		t.Errorf("position after resize = (%d, %d), want unchanged (%d, %d)", gotX, gotY, x, y)
	}
}

// TestConsigliereManuallyPositionedResetsOnFreshOpen pins the other
// half: closing and reopening starts a fresh session (per the spec's
// own "frische Konversation" rule), including position — a drag must
// not outlive that.
func TestConsigliereManuallyPositionedResetsOnFreshOpen(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.SetRect(0, 0, 100, 40)
	root.showConsigliere()
	root.moveConsigliere(10, 5)
	root.hideConsigliere()

	root.showConsigliere()

	if root.consigliereManuallyPositioned {
		t.Error("a fresh session should not inherit the previous one's manual position")
	}
	width, height := root.consigliereSize()
	wantX, wantY := 100-width, (40-height)/2
	gotX, gotY, _, _ := root.consigliereLayout.GetRect()
	if gotX != wantX || gotY != wantY {
		t.Errorf("fresh session should be docked flush right, vertically centered — got (%d, %d), want (%d, %d)", gotX, gotY, wantX, wantY)
	}
}

// TestPushOverlayKeepsConsigliereOnTopOfANewlyOpenedPage pins the real
// bug the user reported: navigating within a related task (e.g. Log
// Audit's own selection screen into a specific file's viewer, both
// pushOverlay calls) while the sidebar is open used to visually bury
// it — tview.Pages draws in SendToFront order, not Z-index, so
// pushing a new full-screen page redrew it on top of
// consigliereLayout, even though the sidebar was never actually
// closed (see TestOpeningAnotherOverlayLeavesConsigliereOpen). Help
// stands in for any pushOverlay caller.
func TestPushOverlayKeepsConsigliereOnTopOfANewlyOpenedPage(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.showConsigliere()

	root.openHelp() // pushOverlay(helpPage, ...)

	// GetPageNames(true) orders front to back (see its own doc comment
	// in tview's pages.go) — index 0 is topmost.
	names := root.GetPageNames(true)
	if len(names) == 0 || names[0] != consiglierePage {
		t.Errorf("visible page draw order = %v, want %q drawn frontmost (index 0)", names, consiglierePage)
	}
}

// TestShowConsigliereForcesPassphraseWhenKeyIsLockedAndStored pins the
// user's own explicit request: if a question can only be answered via
// the encrypted tier, and that tier is still locked, the sidebar must
// demand the passphrase right at "gC" — masked input, Enter tries to
// unlock rather than submitting a question — instead of only
// surfacing this once a question already failed
// (errConsigliereLocked).
func TestShowConsigliereForcesPassphraseWhenKeyIsLockedAndStored(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.apiKeySession = testAPIKeySession(t)
	if err := root.apiKeySession.Unlock("correct-passphrase"); err != nil {
		t.Fatalf("seeding Unlock: %v", err)
	}
	if err := root.apiKeySession.Set("anthropic", "sk-ant-seed-value"); err != nil {
		t.Fatalf("seeding Set: %v", err)
	}
	root.apiKeySession.Lock() // a fresh run starts locked again

	root.showConsigliere()

	if !root.consigliereAwaitingPassphrase {
		t.Fatal("showConsigliere should have started in passphrase mode")
	}
}

// TestShowConsigliereSkipsPassphraseWhenEnvVarAlreadyProvidesAKey pins
// the other half: a higher-priority tier already supplying a key
// means there's nothing to unlock, even if the encrypted tier
// separately has a locked entry sitting unused.
func TestShowConsigliereSkipsPassphraseWhenEnvVarAlreadyProvidesAKey(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.apiKeySession = testAPIKeySession(t)
	if err := root.apiKeySession.Unlock("correct-passphrase"); err != nil {
		t.Fatalf("seeding Unlock: %v", err)
	}
	if err := root.apiKeySession.Set("anthropic", "sk-ant-seed-value"); err != nil {
		t.Fatalf("seeding Set: %v", err)
	}
	root.apiKeySession.Lock()
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-from-env")

	root.showConsigliere()

	if root.consigliereAwaitingPassphrase {
		t.Error("showConsigliere should not demand a passphrase when the env var already supplies a key")
	}
}

// TestSubmitConsigliereUnlockWrongPassphraseStaysLocked pins Enter's
// own rejection path: a wrong passphrase clears the field and shows a
// distinct placeholder (see apikey.ErrWrongPassphrase's own doc
// comment on why that distinction matters), but stays in passphrase
// mode rather than quietly falling through to treating the next
// keystroke as a real question.
func TestSubmitConsigliereUnlockWrongPassphraseStaysLocked(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.apiKeySession = testAPIKeySession(t)
	if err := root.apiKeySession.Unlock("correct-passphrase"); err != nil {
		t.Fatalf("seeding Unlock: %v", err)
	}
	if err := root.apiKeySession.Set("anthropic", "sk-ant-seed-value"); err != nil {
		t.Fatalf("seeding Set: %v", err)
	}
	root.apiKeySession.Lock()
	root.showConsigliere()

	root.consigliereInput.SetText("wrong-passphrase")
	root.submitConsigliereUnlock()

	if !root.consigliereAwaitingPassphrase {
		t.Error("a wrong passphrase should not leave passphrase mode")
	}
	if root.consigliereInput.GetText() != "" {
		t.Errorf("input text = %q, want cleared after a rejected attempt", root.consigliereInput.GetText())
	}
	if root.apiKeySession.IsUnlocked() {
		t.Error("session should still be locked after a wrong passphrase")
	}
}

// TestSubmitConsigliereUnlockCorrectPassphraseUnlocks pins the success
// path: the sidebar leaves passphrase mode, unmasks the input, and
// restores its normal placeholder, ready for a real question.
func TestSubmitConsigliereUnlockCorrectPassphraseUnlocks(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.apiKeySession = testAPIKeySession(t)
	if err := root.apiKeySession.Unlock("correct-passphrase"); err != nil {
		t.Fatalf("seeding Unlock: %v", err)
	}
	if err := root.apiKeySession.Set("anthropic", "sk-ant-seed-value"); err != nil {
		t.Fatalf("seeding Set: %v", err)
	}
	root.apiKeySession.Lock()
	root.showConsigliere()

	root.consigliereInput.SetText("correct-passphrase")
	root.submitConsigliereUnlock()

	if root.consigliereAwaitingPassphrase {
		t.Error("a correct passphrase should leave passphrase mode")
	}
	if !root.apiKeySession.IsUnlocked() {
		t.Error("session should be unlocked")
	}
}

// TestConsigliereAPIKeyStatusDistinguishesLockedFromNotSet pins the
// user's own explicit request: a stored-but-locked entry must never
// render as "not set" (which would wrongly suggest nothing was ever
// configured), and an unlocked one shows a non-reversible last-4-
// characters hint rather than either the full key or nothing at all.
// TestConsigliereAPIKeyStatusNotRequiredForOllama pins the status
// row's own wording for the one provider that needs no key at all —
// "not set" would wrongly read as something still to configure.
func TestConsigliereAPIKeyStatusNotRequiredForOllama(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.settings.ConsigliereProvider = "ollama"

	if got := root.consigliereAPIKeyStatus(); got != "not required (local server)" {
		t.Errorf("status for ollama = %q, want %q", got, "not required (local server)")
	}
}

func TestConsigliereAPIKeyStatusDistinguishesLockedFromNotSet(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.apiKeySession = testAPIKeySession(t)

	if got := root.consigliereAPIKeyStatus(); got != "not set" {
		t.Errorf("status with nothing stored = %q, want %q", got, "not set")
	}

	if err := root.apiKeySession.Unlock("correct-passphrase"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if err := root.apiKeySession.Set("anthropic", "sk-ant-abcd1234"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	root.apiKeySession.Lock()

	if got := root.consigliereAPIKeyStatus(); !strings.Contains(got, "locked") {
		t.Errorf("status with a locked, stored entry = %q, want it to mention \"locked\", not read as unset", got)
	}

	if err := root.apiKeySession.Unlock("correct-passphrase"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got := root.consigliereAPIKeyStatus(); !strings.Contains(got, "1234") {
		t.Errorf("status while unlocked = %q, want it to hint the stored key's last 4 characters", got)
	}
}
