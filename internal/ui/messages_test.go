package ui

import (
	"strings"
	"testing"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/notify"
)

// newTestRootForMessages builds a Root with n fixture Messages already
// pushed into r.notify and the Messages screen open — n pushes, each
// with distinct text ("msg 0", "msg 1", ...) so a test can tell rows
// apart after reloadMessages' own newest-first reversal.
func newTestRootForMessages(t *testing.T, n int) *Root {
	t.Helper()
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	for i := 0; i < n; i++ {
		r.notify.Push(notify.LevelSuccess, notify.CategoryRsync, "msg "+string(rune('0'+i)))
	}
	r.openMessages()
	return r
}

func TestOpenMessagesListsNewestFirst(t *testing.T) {
	r := newTestRootForMessages(t, 3)

	if len(r.messagesList) != 3 {
		t.Fatalf("got %d messages, want 3", len(r.messagesList))
	}
	if r.messagesList[0].Text != "msg 2" || r.messagesList[2].Text != "msg 0" {
		t.Errorf("got order %q, %q, %q, want newest (msg 2) first",
			r.messagesList[0].Text, r.messagesList[1].Text, r.messagesList[2].Text)
	}
}

func TestActivateMessagesCellOnSelectColumnTogglesCheckbox(t *testing.T) {
	r := newTestRootForMessages(t, 1)
	id := r.messagesList[0].ID

	r.activateMessagesCell(1, messagesColSelect)
	if !r.messagesSelected[id] {
		t.Error("Select column activation did not check the row")
	}
	r.activateMessagesCell(1, messagesColSelect)
	if r.messagesSelected[id] {
		t.Error("a second activation did not uncheck the row")
	}
}

func TestActivateMessagesCellOnNewColumnTogglesRead(t *testing.T) {
	r := newTestRootForMessages(t, 1)
	id := r.messagesList[0].ID

	r.activateMessagesCell(1, messagesColNew)

	msgs := r.notify.Messages()
	if !msgs[0].Read {
		t.Error("New column activation did not mark the message read")
	}
	if id != msgs[0].ID {
		t.Fatal("test precondition broken: unexpected message ID")
	}
}

func TestActivateMessagesCellOnMessageColumnOpensDetail(t *testing.T) {
	r := newTestRootForMessages(t, 1)

	r.activateMessagesCell(1, messagesColMessage)

	if r.activePage != messagesDetailPage {
		t.Errorf("activePage = %q, want the detail modal", r.activePage)
	}
	if got := r.messagesDetailView.GetText(true); !strings.Contains(got, "msg 0") {
		t.Errorf("detail view text = %q, want it to contain the full message", got)
	}
}

func TestActivateMessagesCellOnCloseColumnDeletesTheRow(t *testing.T) {
	r := newTestRootForMessages(t, 2)

	r.activateMessagesCell(1, messagesColClose) // row 1 is "msg 1" (newest first)

	if len(r.messagesList) != 1 {
		t.Fatalf("got %d messages left, want 1", len(r.messagesList))
	}
	if r.messagesList[0].Text != "msg 0" {
		t.Errorf("got %q left, want \"msg 0\"", r.messagesList[0].Text)
	}
}

func TestDeleteCurrentMessageRowIgnoresSelection(t *testing.T) {
	r := newTestRootForMessages(t, 2)
	// Select the OTHER row, then delete via "x" on the currently
	// focused one — per messagesHintEntries' own doc comment, this must
	// only ever touch the focused row, never the Auswahl.
	r.messagesTable.Select(2, messagesColSelect)
	r.toggleMessageCheckboxRow(2)
	r.messagesTable.Select(1, messagesColSelect)

	r.deleteCurrentMessageRow()

	if len(r.messagesList) != 1 {
		t.Fatalf("got %d messages left, want 1", len(r.messagesList))
	}
	if r.messagesList[0].Text != "msg 0" {
		t.Errorf("deleteCurrentMessageRow deleted the wrong row: got %q left", r.messagesList[0].Text)
	}
}

func TestToggleSelectAllMessagesSelectsThenClearsEverything(t *testing.T) {
	r := newTestRootForMessages(t, 3)

	r.toggleSelectAllMessages()
	if len(r.messagesSelected) != 3 {
		t.Fatalf("got %d selected, want 3 after select-all", len(r.messagesSelected))
	}

	r.toggleSelectAllMessages()
	if len(r.messagesSelected) != 0 {
		t.Fatalf("got %d selected, want 0 after the second toggle", len(r.messagesSelected))
	}
}

func TestDeleteMessagesSelectionFallsBackToCurrentRow(t *testing.T) {
	r := newTestRootForMessages(t, 2)
	r.messagesTable.Select(1, messagesColSelect) // nothing checked

	r.deleteMessagesSelection()

	if len(r.messagesList) != 1 {
		t.Fatalf("got %d messages left, want 1 (fallback to the current row)", len(r.messagesList))
	}
}

func TestDeleteMessagesSelectionDeletesEveryChecked(t *testing.T) {
	r := newTestRootForMessages(t, 3)
	r.toggleMessageCheckboxRow(1)
	r.toggleMessageCheckboxRow(2)

	r.deleteMessagesSelection()

	if len(r.messagesList) != 1 {
		t.Fatalf("got %d messages left, want 1", len(r.messagesList))
	}
	if r.messagesList[0].Text != "msg 0" {
		t.Errorf("got %q left, want \"msg 0\"", r.messagesList[0].Text)
	}
}

func TestToggleReadForMessagesSelectionFlipsEveryChecked(t *testing.T) {
	r := newTestRootForMessages(t, 2)
	r.toggleMessageCheckboxRow(1)
	r.toggleMessageCheckboxRow(2)

	r.toggleReadForMessagesSelection()

	for _, m := range r.notify.Messages() {
		if !m.Read {
			t.Errorf("message %q still unread after the bulk toggle", m.Text)
		}
	}
}

// TestAdvanceMessagesShiftSelectSpansARange pins the Shift+Down range-
// select mechanic (Panel's own advanceShiftSelect, scoped here — see
// applyMessagesDragDelta's own doc comment): starting on row 1 and
// Shift+Down-ing to row 3 must check all three rows, the anchor
// included.
func TestAdvanceMessagesShiftSelectSpansARange(t *testing.T) {
	r := newTestRootForMessages(t, 3)
	r.messagesTable.Select(1, messagesColSelect)

	r.advanceMessagesShiftSelect(2)
	r.advanceMessagesShiftSelect(3)

	if len(r.messagesSelected) != 3 {
		t.Fatalf("got %d selected, want all 3 rows spanned", len(r.messagesSelected))
	}
}

func TestApplyMessagesDwellMarksReadOnlyForTheArmedGeneration(t *testing.T) {
	r := newTestRootForMessages(t, 1)
	id := r.messagesList[0].ID

	r.armMessagesDwell(1)
	generation := r.messagesDwellGeneration

	// A later selection change (e.g. the cursor moving on) bumps the
	// generation — this stale callback must not mark anything read.
	r.messagesDwellGeneration++
	r.applyMessagesDwell(id, generation)
	if msgs := r.notify.Messages(); msgs[0].Read {
		t.Error("a stale dwell generation marked the message read")
	}

	r.applyMessagesDwell(id, r.messagesDwellGeneration)
	if msgs := r.notify.Messages(); !msgs[0].Read {
		t.Error("applyMessagesDwell with the current generation did not mark the message read")
	}
}

// TestColumnMoveWithinSameRowDoesNotRearmDwell pins a real, reported
// bug: reaching the Neu column via Left/Right to mark an already-read
// message unread by hand, then leaving the cursor sitting there, used
// to have the dwell timer silently mark it read again a moment later —
// because tview's own SetSelectionChangedFunc fires for a bare column
// move within the same row too, and the wrapping in newMessagesScreen
// used to re-arm on every call, not just a real row change.
func TestColumnMoveWithinSameRowDoesNotRearmDwell(t *testing.T) {
	r := newTestRootForMessages(t, 1)
	r.notify.SetRead(r.messagesList[0].ID, true)
	r.messagesTable.Select(1, messagesColMessage) // lands on the row, arms (then skips: already read)
	generationAfterFirstLanding := r.messagesDwellGeneration

	r.messagesTable.Select(1, messagesColNew) // column-only move, same row

	if r.messagesDwellGeneration != generationAfterFirstLanding {
		t.Errorf("a column-only move within the same row re-armed the dwell timer (generation %d -> %d)",
			generationAfterFirstLanding, r.messagesDwellGeneration)
	}
}

func TestCloseMessagesHidesTheOverlay(t *testing.T) {
	r := newTestRootForMessages(t, 1)

	r.closeMessages()

	if r.activePage == messagesPage {
		t.Error("closeMessages left the screen open")
	}
}

func TestOpenMessagesShowsAPlaceholderWhenEmpty(t *testing.T) {
	r := newTestRootForMessages(t, 0)

	if len(r.messagesList) != 0 {
		t.Fatalf("got %d messages, want 0", len(r.messagesList))
	}
	// showTablePlaceholder's own row lands at row 1 — just confirm the
	// table has content and didn't panic on an empty list.
	if r.messagesTable.GetRowCount() == 0 {
		t.Error("messagesTable has no rows at all, want at least the placeholder")
	}
}
