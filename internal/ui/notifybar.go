// notifybar.go is internal/notify's own Stufe 2 (see feature_ideas.txt's
// "3a. Benachrichtigungen"): a passive, transient overlay across the
// panel's own bottom row whenever r.notify pushes a new Message — purely
// informational, never interactive. Deliberately not built on
// showOverlay/pushOverlay (see notifyBar's own doc comment on Root):
// those always end in Application.SetFocus and register on
// r.overlayStack/r.activePage, both wrong here — nothing in this bar is
// clickable, and showing or hiding it must never disturb whatever
// already had keyboard focus.
package ui

import (
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/notify"
)

const notifyToastPage = "notify-toast"

// notifyGlyph/notifyColor render one Message's own Level — the same
// glyph/color pattern Sessions' own Status column (✔/✘, green/red —
// see sessionsAttachedGlyph/sessionsStatusColor) and the Firewall
// screen's own Action column (see actionColor) already establish, not
// a fresh color language invented for this bar. notify.LevelInfo (the
// self-lockout rollback firing, currently the only trigger that isn't
// itself a plain success/failure) reuses the same "⚠"/WarningText
// pairing rsync.go's own remote-relay notice already uses.
func notifyGlyph(level notify.Level) string {
	switch level {
	case notify.LevelSuccess:
		return sessionsAttachedGlyph
	case notify.LevelError:
		return sessionsDetachedGlyph
	default:
		return "⚠"
	}
}

func notifyColor(level notify.Level, theme config.ResolvedTheme) tcell.Color {
	switch level {
	case notify.LevelSuccess:
		return theme.EntryExecutable
	case notify.LevelError:
		return theme.CriticalText
	default:
		return theme.WarningText
	}
}

// newNotifyBar builds the bar and adds it to Pages as its own page,
// resize=false (it positions itself explicitly on every push — see
// pushNotifyToast), initially hidden. Must run after r.panelHost exists
// (pushNotifyToast reads its rect) and after r.notify itself (NewRoot's
// own struct literal already guarantees both).
func (r *Root) newNotifyBar() {
	r.notifyBar = tview.NewTextView()
	r.notifyBar.SetDynamicColors(true)
	r.AddPage(notifyToastPage, r.notifyBar, false, false)
	r.notify.Subscribe(r.pushNotifyToast)
}

// pushNotifyToast is r.notify's own Subscribe callback — called
// synchronously by notify.Store.Push, always already on the UI
// goroutine: every real trigger today (finishRsyncJob, finishPasteJob,
// rollbackFirewallRule) only ever reaches Push from inside its own
// r.app.QueueUpdateDraw callback, so this never needs a second one of
// its own.
//
// Replaces whatever the bar was already showing outright rather than
// queuing — per the spec's own explicit "immer nur die jeweils
// neueste sichtbar, kein Stapel in der Anzeige selbst": the full
// history always still lives in r.notify's own ring buffer regardless,
// nothing here is lost, only no longer the freshest one on screen.
func (r *Root) pushNotifyToast(m notify.Message) {
	text := fmt.Sprintf("%s %s %s", m.Time.Format("15:04"), notifyGlyph(m.Level), m.Text)
	r.notifyBar.SetText(wrapColor(notifyColor(m.Level, r.theme), text))

	x, y, width, height := r.panelHost.GetRect()
	r.notifyBar.SetRect(x, y+height-1, width, 1)
	r.ShowPage(notifyToastPage)
	r.SendToFront(notifyToastPage)

	r.notifyGeneration++
	generation := r.notifyGeneration
	time.AfterFunc(errorAutoHideDelay, func() {
		r.app.QueueUpdateDraw(func() { r.autoHideNotifyToast(generation) })
	})
}

// autoHideNotifyToast is pushNotifyToast's own timer callback — split
// out so a test can call it directly instead of waiting on a real
// errorAutoHideDelay, the same shape autoHideError already has for
// showTransientError. generation guards the same race autoHideError's
// own doc comment describes: a second Message pushed before the first
// one's timer fires must not have that timer hide the newer, still-
// relevant one out from under it.
func (r *Root) autoHideNotifyToast(generation int) {
	if r.notifyGeneration == generation {
		r.HidePage(notifyToastPage)
	}
}
