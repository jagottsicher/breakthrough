package ui

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// dialFunc is remotefs.Dial, a package-level var so a test can fake a
// connection attempt without a real network round trip — the same
// reasoning sedPreviewFunc/searchRun already establish for anything a
// background goroutine calls that a test needs to observe or control.
var dialFunc = remotefs.Dial

// openConnectDialog shows the "Connect" form, prefilled from prefill —
// the zero Connection for a blank "New connection…" open (see
// connectionmenu.go), or a history entry's own Connection when
// reconnecting to it. Never touches connectPasswordField: a password
// is never persisted (see remotefs.Connection's own doc comment), so
// there's nothing to prefill it with even when reconnecting to an
// entry that originally needed one.
func (r *Root) openConnectDialog(prefill remotefs.Connection) {
	r.connectHostField.SetText(prefill.Host)
	if prefill.Port != 0 {
		r.connectPortField.SetText(strconv.Itoa(prefill.Port))
	} else {
		r.connectPortField.SetText("")
	}
	r.connectUserField.SetText(prefill.User)
	r.connectPasswordField.SetText("")
	r.setConnectStatus("", r.theme.Text)

	// height covers connectTitleBar's own row plus connectLayout's three
	// stacked pieces (connectForm's own 9, see newConnectLayout's own
	// doc comment on why; connectStatus's 1; connectButtons' 1) —
	// checked against a real render, not guessed; a shorter value
	// silently clipped the bottom rows (see openSedReplace's own
	// identical comment on its own dialog).
	width, height := 64, 12
	_, _, screenWidth, screenHeight := r.GetRect()
	if width > screenWidth-4 {
		width = screenWidth - 4
	}
	if height > screenHeight-4 {
		height = screenHeight - 4
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	r.connectLayout.SetRect(x, y, width, height)
	r.showOverlay(connectDialogPage, r.connectLayout)
}

// newConnectForm builds the (initially empty) "Connect" form once,
// from NewRoot — see resetSedForm's own doc comment on the equivalent
// choice there for why a fixed field set like this one doesn't need
// the same "rebuilt fresh on every open" treatment Sed Replace's own
// variable field set does.
func (r *Root) newConnectForm() *tview.Form {
	f := tview.NewForm()
	f.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			r.cancelConnect()
			return nil
		}
		return event
	})

	r.connectHostField = tview.NewInputField().SetLabel("Host")
	f.AddFormItem(r.connectHostField)
	r.connectPortField = tview.NewInputField().SetLabel("Port (default 22)")
	f.AddFormItem(r.connectPortField)
	r.connectUserField = tview.NewInputField().SetLabel("User")
	f.AddFormItem(r.connectUserField)
	// A short label, deliberately: tview.Form sizes the shared label
	// column to its widest field's own label, so a long explanatory one
	// here would squeeze every other field's input area too — the
	// explanation instead lives in the placeholder text, only visible
	// (as intended) once the field itself has real width to show it in.
	r.connectPasswordField = tview.NewInputField().SetLabel("Password")
	r.connectPasswordField.SetPlaceholder("only tried if key/agent auth doesn't apply")
	r.connectPasswordField.SetMaskCharacter('*')
	f.AddFormItem(r.connectPasswordField)

	// Tab/Enter on the form's own last field would otherwise just wrap
	// back to its first one instead of ever reaching connectCancelBtn/
	// connectConnectBtn: verified directly against tview's own form.go,
	// not guessed — Form.Focus unconditionally calls
	// item.SetFinishedFunc on every item whenever the form itself gains
	// focus, silently overwriting any SetDoneFunc set here beforehand,
	// so the only place left to actually intercept Tab is a
	// SetInputCapture, which runs before an item's own native handling
	// at all. Enter goes one step further and submits the form
	// outright, the same "last field, Enter submits" convenience a real
	// login form has — the same reasoning this is only wired on the
	// *last* field: every other field's own Tab/Enter already does the
	// right thing (move to the next field) via the form's own default
	// handling.
	r.connectPasswordField.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			r.app.SetFocus(r.connectCancelBtn)
			return nil
		case tcell.KeyEnter:
			r.runConnect()
			return nil
		}
		return event
	})

	return f
}

// newConnectButtons builds a real Cancel/Connect button pair,
// bottom-left/bottom-right — the same shape
// newChmodButtons/newSearchButtons/newPropertiesButtons/
// newDuplicateButtons all already establish for their own dialogs, per
// the user's own explicit, repeated request that every dialog's own
// action row match that established look rather than Sed Replace's
// own vertical two-item List (see newDuplicateButtons' own doc comment
// for the first time this exact correction was made).
func (r *Root) newConnectButtons() *tview.Flex {
	r.connectCancelBtn = tview.NewButton("Cancel").SetSelectedFunc(r.cancelConnect)
	r.connectConnectBtn = tview.NewButton("Connect").SetSelectedFunc(r.runConnect)
	r.connectCancelBtn.SetInputCapture(spaceAlsoActivates(r.cancelConnect))
	r.connectConnectBtn.SetInputCapture(spaceAlsoActivates(r.runConnect))

	// Tab/Backtab cycle between the two buttons and back into the form
	// (connectPasswordField's own forward Tab is the other half of this
	// — see newConnectForm's own doc comment); Escape always cancels,
	// the same as everywhere else in this dialog.
	r.connectCancelBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.connectConnectBtn)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.connectPasswordField)
		case tcell.KeyEscape:
			r.cancelConnect()
		}
	})
	r.connectConnectBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.connectHostField)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.connectCancelBtn)
		case tcell.KeyEscape:
			r.cancelConnect()
		}
	})

	// Equal proportion (0, 1) each, nothing else, so the two together
	// fill the whole row edge to edge, split exactly in half — the same
	// shape newChmodButtons/newDuplicateButtons already use.
	return tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.connectCancelBtn, 0, 1, false).
		AddItem(r.connectConnectBtn, 0, 1, false)
}

// newConnectLayout wraps connectTitleBar over the form, a one-line
// status area (blank until runConnect has something to say — an
// in-progress spinner, or an error), and the Cancel/Connect buttons —
// the same title-bar-over-content shape newSedLayout already
// establishes.
func (r *Root) newConnectLayout() *tview.Flex {
	r.connectTitleBar = newPlainTitleBar("Connect")
	r.connectStatus = tview.NewTextView().SetDynamicColors(true)
	// 9 rows for connectForm: tview.NewForm() reserves a 1-row border
	// padding top and bottom of its own (SetBorderPadding(1,1,1,1),
	// independent of whether a visible border line is drawn at all —
	// verified directly against tview's own form.go, not guessed, after
	// a real render silently clipped the fourth field otherwise) on top
	// of its own four one-row fields plus itemPadding's default 1-row
	// gap between each: 2 (padding) + 4 (fields) + 3 (gaps) = 9.
	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.connectForm, 9, 0, true).
		AddItem(r.connectStatus, 1, 0, false).
		AddItem(r.connectButtons, 1, 0, false)
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.connectTitleBar, 1, 0, false).
		AddItem(content, 0, 1, true)
}

func (r *Root) setConnectStatus(text string, color tcell.Color) {
	r.connectStatus.SetTextColor(color)
	r.connectStatus.SetText(text)
}

func (r *Root) cancelConnect() {
	r.cancelConnectAttempt()
	r.hideOverlay()
}

// cancelConnectAttempt stops an in-flight Dial, if any, without
// necessarily closing the dialog — split out from cancelConnect so
// runConnect can call just this half to replace an earlier, still-
// running attempt instead of piling a second one on top of it.
func (r *Root) cancelConnectAttempt() {
	if r.connectCancel != nil {
		r.connectCancel()
		r.connectCancel = nil
	}
}

// runConnect reads the form, validates just enough to dial (a host is
// required; a malformed port is rejected before ever touching the
// network), and attempts the connection in the background — see
// animateConnectProgress for the "in progress" status line and
// finishConnect for every one of its outcomes. r.panel is snapshotted
// immediately, not re-read once Dial returns: the user is free to
// switch tabs or close this one while a slow connection attempt is
// still in flight, and the result belongs to whichever tab was active
// when Connect was actually clicked (see finishConnect's own
// hasTab guard for the case that tab is gone by the time it matters).
func (r *Root) runConnect() {
	host := strings.TrimSpace(r.connectHostField.GetText())
	if host == "" {
		r.setConnectStatus("Host is required.", r.theme.CriticalText)
		return
	}
	port := 0
	if text := strings.TrimSpace(r.connectPortField.GetText()); text != "" {
		p, err := strconv.Atoi(text)
		if err != nil || p <= 0 || p > 65535 {
			r.setConnectStatus("Port must be a number from 1 to 65535.", r.theme.CriticalText)
			return
		}
		port = p
	}
	user := strings.TrimSpace(r.connectUserField.GetText())
	if user == "" {
		user = currentUsername()
	}
	var passwordPrompt remotefs.PasswordPrompt
	if password := r.connectPasswordField.GetText(); password != "" {
		passwordPrompt = func() (string, error) { return password, nil }
	}

	conn := remotefs.Connection{Host: host, Port: port, User: user}

	r.cancelConnectAttempt()
	ctx, cancel := context.WithCancel(context.Background())
	r.connectCancel = cancel
	r.connectAnimFrame = 0
	r.setConnectStatus(hashAnimationFrames[0]+" Connecting…", r.theme.Text)

	onPanic := func() { r.cancelConnectAttempt() }
	r.safeGo("connect progress animation", onPanic, func() { r.animateConnectProgress(ctx) })

	panel := r.panel
	r.safeGo("connect", onPanic, func() {
		client, err := dialFunc(ctx, remotefs.DialOptions{
			Connection:    conn,
			Auth:          remotefs.AuthOptions{AgentSocket: os.Getenv("SSH_AUTH_SOCK"), Password: passwordPrompt},
			HostKeyPrompt: r.askHostKeyTrust,
		})
		r.app.QueueUpdateDraw(func() {
			if ctx.Err() != nil {
				return
			}
			r.finishConnect(panel, conn, client, err)
		})
	})
}

// animateConnectProgress advances connectAnimFrame every
// hashAnimationInterval until ctx is done — the same ticker-driven "in
// progress" animation animateSedPreviewProgress/animateSearchProgress
// already use, reusing hashAnimationFrames rather than a second,
// separately defined set.
func (r *Root) animateConnectProgress(ctx context.Context) {
	ticker := time.NewTicker(hashAnimationInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			r.app.QueueUpdateDraw(func() {
				if ctx.Err() != nil {
					return
				}
				r.connectAnimFrame++
				frame := hashAnimationFrames[r.connectAnimFrame%len(hashAnimationFrames)]
				r.setConnectStatus(frame+" Connecting…", r.theme.Text)
			})
		case <-ctx.Done():
			return
		}
	}
}

// finishConnect applies dialFunc's own outcome: records it in the
// connection history either way (see remotefs.RecordAttempt), then
// either reports the error back into the still-open dialog (so the
// user can fix a field and retry without starting over) or attaches
// the new client to panel and closes the dialog.
//
// Split out from runConnect specifically so it's callable directly,
// without needing a real Application event loop to drain
// QueueUpdateDraw first — see showSedPreviewResult's own identical
// doc comment for the same reasoning.
func (r *Root) finishConnect(panel *Panel, conn remotefs.Connection, client remotefs.Client, err error) {
	r.cancelConnectAttempt()
	if err != nil {
		_ = remotefs.RecordAttempt(conn, true)
		r.activityLog.Error(activitylog.CategoryRemote, fmt.Sprintf("connect to %s: %v", conn.Label(), err))
		r.setConnectStatus(err.Error(), r.theme.CriticalText)
		return
	}
	// Recorded on conn itself, before RecordAttempt persists it to
	// history and connectRemote attaches it to panel — see
	// remotefs.AuthMethod's own doc comment for why this is the one
	// "how" worth keeping despite Connection otherwise only ever
	// describing "where": rsync.go's own refuseBackgroundPasswordAuth
	// reads exactly this back off the active panel's own remoteConn.
	conn.AuthMethod = client.AuthMethod()

	_ = remotefs.RecordAttempt(conn, false)
	r.activityLog.Action(activitylog.CategoryRemote, fmt.Sprintf("connected to %s", conn.Label()))

	if !r.hasTab(panel) {
		// The tab this connection was meant for closed while Dial was
		// still in flight — nothing left to attach it to.
		_ = client.Close()
		r.hideOverlay()
		return
	}
	r.hideOverlay()
	if err := panel.connectRemote(client, conn); err != nil {
		r.showError(err)
	}
}

// hasTab reports whether p is still one of r.tabs — see finishConnect's
// own doc comment for why this matters.
func (r *Root) hasTab(p *Panel) bool {
	for _, t := range r.tabs {
		if t == p {
			return true
		}
	}
	return false
}
