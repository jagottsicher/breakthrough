package ui

import (
	"testing"

	"github.com/rivo/tview"
)

// resolveHostKeyConfirm is askHostKeyTrust's own directly-testable
// half — the same "split the QueueUpdateDraw-driven half from the
// synchronous half" shape showSedPreviewResult's own doc comment
// explains for the identical reason: askHostKeyTrust itself blocks on
// a channel behind a real Application.QueueUpdateDraw call, which
// nothing drains without a real, running event loop (see
// sedreplace_test.go's own isolateSedPreviewFunc doc comment) — a test
// calling it directly would simply hang.
func newTestRootWithHostKeyConfirmOpen(t *testing.T) *Root {
	t.Helper()
	dir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.hostKeyConfirmTitleBar.SetText(" Unknown host key for example.com — trust it? ")
	r.pushOverlay(hostKeyConfirmPage, r.hostKeyConfirmLayout, nil)
	return r
}

func TestResolveHostKeyConfirmAcceptSendsTrueAndClosesTheDialog(t *testing.T) {
	r := newTestRootWithHostKeyConfirmOpen(t)
	resp := make(chan bool, 1)
	r.hostKeyConfirmResponse = resp

	r.resolveHostKeyConfirm(true)

	select {
	case got := <-resp:
		if !got {
			t.Error("resolveHostKeyConfirm(true) sent false")
		}
	default:
		t.Fatal("resolveHostKeyConfirm(true) never sent anything on the response channel")
	}
	if r.activePage == hostKeyConfirmPage {
		t.Error("the host-key confirm dialog is still open after being answered")
	}
	if r.hostKeyConfirmResponse != nil {
		t.Error("hostKeyConfirmResponse was not cleared after being answered")
	}
}

func TestResolveHostKeyConfirmDeclineSendsFalse(t *testing.T) {
	r := newTestRootWithHostKeyConfirmOpen(t)
	resp := make(chan bool, 1)
	r.hostKeyConfirmResponse = resp

	r.resolveHostKeyConfirm(false)

	select {
	case got := <-resp:
		if got {
			t.Error("resolveHostKeyConfirm(false) sent true")
		}
	default:
		t.Fatal("resolveHostKeyConfirm(false) never sent anything on the response channel")
	}
}

// TestResolveHostKeyConfirmWithNoPendingPromptDoesNotPanic covers the
// (currently unreachable in practice, but still worth being safe
// against) case of a stray extra click after a decision has already
// been sent — SetCurrentItem/double SelectedFunc firing twice, say.
func TestResolveHostKeyConfirmWithNoPendingPromptDoesNotPanic(t *testing.T) {
	r := newTestRootWithHostKeyConfirmOpen(t)
	r.resolveHostKeyConfirm(true) // hostKeyConfirmResponse is nil throughout — must not panic
}
