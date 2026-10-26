package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

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
// Tab now walks through once the sidebar is open: panel ->
// consigliereInput -> consigliereHistory -> back to panel.
func TestCycleFocusShortcutLoopsPanelInputHistoryPanel(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.showConsigliere()
	root.app.SetFocus(root.panel.table)

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
