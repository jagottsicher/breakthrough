package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
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

// TestPushNotifyToastCoversTheStatusBarRow pins the bar's own real
// position: a layer directly over the status bar's own row — the
// screen's own bottom-most line — not the panel's own bottom row, per
// the user's own explicit correction (see notifybar.go's own doc
// comment).
func TestPushNotifyToastCoversTheStatusBarRow(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(80, 24)
	r.SetRect(0, 0, 80, 24)
	r.Draw(screen) // establish real rects (statusBar's included) before reading them below

	r.notify.Push(notify.LevelSuccess, notify.CategoryRsync, "rsync backup finished")

	wantX, wantY, wantWidth, _ := r.statusBar.GetRect()
	gotX, gotY, gotWidth, gotHeight := r.notifyBar.GetRect()
	if gotX != wantX || gotY != wantY || gotWidth != wantWidth || gotHeight != 1 {
		t.Errorf("notifyBar rect = (%d,%d,%d,%d), want (%d,%d,%d,1) — the status bar's own row",
			gotX, gotY, gotWidth, gotHeight, wantX, wantY, wantWidth)
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
