package ui

import (
	"strings"
	"testing"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/notify"
)

func notifyToastVisible(r *Root) bool {
	for _, name := range r.GetPageNames(true) {
		if name == notifyToastPage {
			return true
		}
	}
	return false
}

func TestPushNotifyToastShowsTheBarWithoutTakingFocus(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.notify.Push(notify.LevelSuccess, notify.CategoryRsync, "rsync backup finished")

	if !notifyToastVisible(r) {
		t.Error("notify toast page is not visible after a push")
	}
	if got := r.notifyBar.GetText(true); got == "" {
		t.Error("notifyBar text is empty after a push")
	}
	// Deliberately never focused — see notifybar.go's own doc comment on
	// why this bypasses showOverlay/pushOverlay entirely.
	if r.activePage != "" {
		t.Errorf("activePage = %q, want it untouched (no overlay-stack entry for a toast)", r.activePage)
	}
}

func TestPushNotifyToastReplacesRatherThanStacks(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.notify.Push(notify.LevelSuccess, notify.CategoryRsync, "first")
	r.notify.Push(notify.LevelError, notify.CategoryPaste, "second")

	got := r.notifyBar.GetText(true)
	if !strings.Contains(got, "second") {
		t.Errorf("notifyBar text = %q, want the newest message", got)
	}
	if strings.Contains(got, "first") {
		t.Errorf("notifyBar text = %q, want only the newest message, not both stacked", got)
	}
	// Both are still preserved in the ring buffer regardless — see
	// pushNotifyToast's own doc comment.
	if msgs := r.notify.Messages(); len(msgs) != 2 {
		t.Errorf("got %d messages in the ring buffer, want 2", len(msgs))
	}
}

func TestAutoHideNotifyToastHidesTheNoticeItWasArmedFor(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.notify.Push(notify.LevelSuccess, notify.CategoryRsync, "done")
	generation := r.notifyGeneration

	r.autoHideNotifyToast(generation)

	if notifyToastVisible(r) {
		t.Error("autoHideNotifyToast left the bar visible, want it hidden")
	}
}

// TestAutoHideNotifyToastLeavesANewerUnrelatedNoticeAlone guards the
// same race autoHideError's own doc comment describes: a stale timer
// must never hide a second, newer message that arrived before it fired.
func TestAutoHideNotifyToastLeavesANewerUnrelatedNoticeAlone(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.notify.Push(notify.LevelSuccess, notify.CategoryRsync, "first")
	staleGeneration := r.notifyGeneration
	r.notify.Push(notify.LevelError, notify.CategoryPaste, "second")

	r.autoHideNotifyToast(staleGeneration)

	if !notifyToastVisible(r) {
		t.Error("autoHideNotifyToast hid a newer, unrelated notice using a stale generation")
	}
}
