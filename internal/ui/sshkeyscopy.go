package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/sshkeys"
)

// The SSH Keys screen's own "Copy to server" form ("c", see sshkeys.go's
// own captureSSHKeysKey) — Stage 3 of feature_ideas.txt's own
// SSH-Key-Verwaltung entry: installing the currently selected key
// pair's own public half into a remote user's authorized_keys, the same
// end result `ssh-copy-id` produces, built explicitly instead (see
// internal/sshkeys/distribute.go's own doc comments) so this app
// controls its own three safety properties directly — never a
// duplicate, never a silently overwritten authorized_keys, never a
// permission mistake that locks sshd out of trusting the result.
//
// Host/Port/User can be typed by hand or taken from an already-open
// remote tab ("Pick tab…", the same "reuse an existing connection
// instead of asking a second time" shape openRsyncTabPicker already
// establishes for Rsync). The exact `ssh` command is shown for
// confirmation before it ever runs, through a real, attached terminal —
// whichever auth the target actually needs (agent, a passphrase, a
// password) needs one. Immediately afterwards, and only on success,
// this also runs a second, quick, non-interactive check
// (sshkeys.TestCommand) that the new key alone — not just an
// already-loaded agent identity — now actually gets in with no prompt
// at all, and reports that result plainly rather than just hoping.
const sshKeysCopyPage = "sshkeys-copy"

// openSSHKeysCopy requires a selected row with a public key to install
// (see sshkeys.KeyPair.HasPublic) — nothing to copy without one, the
// same reasoning openFirewallAddRule's own no-backend guard gives for
// refusing outright rather than opening a form that could only ever
// fail.
func (r *Root) openSSHKeysCopy() {
	row, _ := r.sshKeysTable.GetSelection()
	index := row - 1 // header row occupies row 0
	if index < 0 || index >= len(r.sshKeysPairs) {
		r.showError(errors.New("ssh keys: select a key pair first"))
		return
	}
	kp := r.sshKeysPairs[index]
	if !kp.HasPublic {
		r.showError(fmt.Errorf("ssh keys: %s has no public key file to copy", kp.Name))
		return
	}

	r.sshKeysCopyKeyName = kp.Name
	r.sshKeysCopyHostText = ""
	r.sshKeysCopyPortText = strconv.Itoa(sshkeys.DefaultPort)
	r.sshKeysCopyUserText = os.Getenv("USER")
	// Prefills from the active panel's own already-open connection, the
	// same "reuse what's already there" shape defaultRsyncSource's own
	// doc comment establishes for Rsync — never guessed when the active
	// panel is local, since there is nothing to reuse.
	if r.panel.remote != nil {
		r.sshKeysCopyHostText = r.panel.remoteConn.Host
		r.sshKeysCopyPortText = strconv.Itoa(r.panel.remoteConn.Port22IfZero())
		r.sshKeysCopyUserText = r.panel.remoteConn.User
	}
	r.setSSHKeysCopyStatus("", r.theme.Text)
	r.renderSSHKeysCopyForm()

	width, height := 62, 12
	_, _, screenWidth, screenHeight := r.GetRect()
	if width > screenWidth-4 {
		width = screenWidth - 4
	}
	if height > screenHeight-4 {
		height = screenHeight - 4
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	r.sshKeysCopyLayout.SetRect(x, y, width, height)
	r.pushOverlay(sshKeysCopyPage, r.sshKeysCopyLayout, nil)
}

// newSSHKeysCopyForm builds the (initially empty) "Copy to server"
// form — called once from NewRoot; renderSSHKeysCopyForm populates it
// fresh on every open, the same reasoning newFirewallAddRuleForm's own
// doc comment gives.
func (r *Root) newSSHKeysCopyForm() *tview.Form {
	f := tview.NewForm()
	f.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			r.cancelSSHKeysCopy()
			return nil
		}
		return event
	})
	return f
}

// renderSSHKeysCopyForm (re)builds sshKeysCopyForm's own items: a
// read-only line naming which key pair is being copied, then Host,
// Port, and User — each one just writes straight back into its own
// sshKeysCopyXxx mirror field, the same "value mirror kept in sync
// live" shape resetDuplicateForm's own fields already use.
func (r *Root) renderSSHKeysCopyForm() {
	r.sshKeysCopyForm.Clear(true)

	r.sshKeysCopyForm.AddTextView("Key", r.sshKeysCopyKeyName+".pub", 0, 1, true, false)

	hostField := tview.NewInputField().SetLabel("Host").SetText(r.sshKeysCopyHostText)
	hostField.SetChangedFunc(func(v string) { r.sshKeysCopyHostText = v })
	r.sshKeysCopyForm.AddFormItem(hostField)

	portField := tview.NewInputField().SetLabel("Port").SetText(r.sshKeysCopyPortText)
	portField.SetChangedFunc(func(v string) { r.sshKeysCopyPortText = v })
	r.sshKeysCopyForm.AddFormItem(portField)

	userField := tview.NewInputField().SetLabel("User").SetText(r.sshKeysCopyUserText)
	userField.SetChangedFunc(func(v string) { r.sshKeysCopyUserText = v })
	userField.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			r.app.SetFocus(r.sshKeysCopyPickBtn)
			return nil
		case tcell.KeyEnter:
			r.submitSSHKeysCopy()
			return nil
		}
		return event
	})
	r.sshKeysCopyForm.AddFormItem(userField)
}

// newSSHKeysCopyPickButton builds sshKeysCopyPickBtn once, from
// NewRoot — opens openSSHKeysCopyTabPicker, the same "a real button,
// not a new keybinding" shape newRsyncPickButtons' own doc comment
// explains for Rsync's identical pair.
func (r *Root) newSSHKeysCopyPickButton() *tview.Button {
	pick := func() { r.openSSHKeysCopyTabPicker() }
	r.sshKeysCopyPickBtn = tview.NewButton("Pick tab…").SetSelectedFunc(pick)
	r.sshKeysCopyPickBtn.SetInputCapture(spaceAlsoActivates(pick))
	r.sshKeysCopyPickBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.sshKeysCopyCancelBtn)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.sshKeysCopyForm)
		case tcell.KeyEscape:
			r.cancelSSHKeysCopy()
		}
	})
	return r.sshKeysCopyPickBtn
}

// openSSHKeysCopyTabPicker lists every currently connected remote tab
// (r.panel.remote != nil elsewhere too — see defaultRsyncSource's own
// identical check) and fills Host/Port/User directly from whichever one
// is picked — separate fields, unlike openRsyncTabPicker's own combined
// "user@host:path" text, since sshkeys.InstallCommand/TestCommand both
// need Host/Port/User apart, not a single string to reparse.
func (r *Root) openSSHKeysCopyTabPicker() {
	var remoteTabs []*Panel
	for _, tab := range r.tabs {
		if tab.remote != nil {
			remoteTabs = append(remoteTabs, tab)
		}
	}
	if len(remoteTabs) == 0 {
		r.showTransientError(errors.New("ssh keys: no open remote tabs to pick from"))
		return
	}

	r.picker.Clear()
	for i, tab := range remoteTabs {
		conn := tab.remoteConn
		label := fmt.Sprintf("%2d  %s@%s:%d", i+1, conn.User, conn.Host, conn.Port22IfZero())
		r.picker.AddItem(label, "", 0, func() {
			r.hideOverlay()
			r.sshKeysCopyHostText = conn.Host
			r.sshKeysCopyPortText = strconv.Itoa(conn.Port22IfZero())
			r.sshKeysCopyUserText = conn.User
			r.renderSSHKeysCopyForm()
		})
	}
	r.picker.SetDoneFunc(func() { r.hideOverlay() })

	width, _ := listSize(r.picker)
	height := pickerHeight
	if len(remoteTabs) < height {
		height = len(remoteTabs)
	}
	x, y := r.centeredOnScreen(width, height)
	x, y, width, height = r.clampToPanel(x, y, width, height)
	r.picker.SetRect(x, y, width, height)
	r.picker.SetCurrentItem(0)

	r.pushOverlay(pickerPage, r.picker, nil)
}

// newSSHKeysCopyButtons builds sshKeysCopyForm's own action row once,
// from NewRoot — the same Cancel/action button pair
// newFirewallAddRuleButtons already establishes.
func (r *Root) newSSHKeysCopyButtons() *tview.Flex {
	r.sshKeysCopyCancelBtn = tview.NewButton("Cancel").SetSelectedFunc(r.cancelSSHKeysCopy)
	r.sshKeysCopyGoBtn = tview.NewButton("Copy key").SetSelectedFunc(r.submitSSHKeysCopy)
	r.sshKeysCopyCancelBtn.SetInputCapture(spaceAlsoActivates(r.cancelSSHKeysCopy))
	r.sshKeysCopyGoBtn.SetInputCapture(spaceAlsoActivates(r.submitSSHKeysCopy))

	r.sshKeysCopyCancelBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.sshKeysCopyGoBtn)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.sshKeysCopyPickBtn)
		case tcell.KeyEscape:
			r.cancelSSHKeysCopy()
		}
	})
	r.sshKeysCopyGoBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.sshKeysCopyForm)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.sshKeysCopyCancelBtn)
		case tcell.KeyEscape:
			r.cancelSSHKeysCopy()
		}
	})

	return tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.sshKeysCopyCancelBtn, 0, 1, false).
		AddItem(r.sshKeysCopyGoBtn, 0, 1, false)
}

// newSSHKeysCopyLayout wraps sshKeysCopyTitleBar over the form, the
// Pick tab… button, a one-line status area, and the Cancel/Copy key
// buttons — the same shape newFirewallAddRuleLayout already
// establishes, with the Pick button as its own row in between, the
// same position newRsyncPickButtons' own row occupies for Rsync.
func (r *Root) newSSHKeysCopyLayout() *tview.Flex {
	r.sshKeysCopyTitleBar = newPlainTitleBar("Copy key to server")
	r.sshKeysCopyStatus = tview.NewTextView().SetDynamicColors(true)
	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.sshKeysCopyForm, 8, 0, true).
		AddItem(r.sshKeysCopyPickBtn, 1, 0, false).
		AddItem(r.sshKeysCopyStatus, 1, 0, false).
		AddItem(r.sshKeysCopyButtons, 1, 0, false)
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.sshKeysCopyTitleBar, 1, 0, false).
		AddItem(content, 0, 1, true)
}

func (r *Root) setSSHKeysCopyStatus(text string, color tcell.Color) {
	r.sshKeysCopyStatus.SetTextColor(color)
	r.sshKeysCopyStatus.SetText(text)
}

func (r *Root) cancelSSHKeysCopy() {
	r.hideOverlay()
}

// submitSSHKeysCopy is the form's own "Copy key" action (button, or
// Enter on the User field): reads the selected key pair's own exact
// public-key line (never reconstructed — see
// sshkeys.ReadPublicKeyLine's own doc comment), builds the exact `ssh`
// command that would install it, and asks for explicit confirmation
// showing that literal command — never running anything before the
// user has seen and accepted exactly what will run. Any failure is
// reported in place, in sshKeysCopyStatus, without closing the form.
func (r *Root) submitSSHKeysCopy() {
	port, err := strconv.Atoi(strings.TrimSpace(r.sshKeysCopyPortText))
	if err != nil || port <= 0 {
		r.setSSHKeysCopyStatus(fmt.Sprintf("invalid port %q", r.sshKeysCopyPortText), r.theme.CriticalText)
		return
	}
	host := strings.TrimSpace(r.sshKeysCopyHostText)
	user := strings.TrimSpace(r.sshKeysCopyUserText)

	dir := sshkeys.DefaultDir()
	if dir == "" {
		r.setSSHKeysCopyStatus("could not determine the current user's home directory", r.theme.CriticalText)
		return
	}
	pubKeyLine, err := sshkeys.ReadPublicKeyLine(dir, r.sshKeysCopyKeyName)
	if err != nil {
		r.setSSHKeysCopyStatus(err.Error(), r.theme.CriticalText)
		return
	}

	command, err := sshkeys.InstallCommand(host, port, user, pubKeyLine)
	if err != nil {
		r.setSSHKeysCopyStatus(err.Error(), r.theme.CriticalText)
		return
	}
	privateKeyPath := ""
	if info, statErr := os.Stat(dir); statErr == nil && info.IsDir() {
		privateKeyPath = dir + "/" + r.sshKeysCopyKeyName
	}

	message := fmt.Sprintf("Run this exact command now? %s", command)
	r.openConfirm(message, "Yes, copy key", func() {
		r.applySSHKeysCopy(host, port, user, privateKeyPath, command)
	})
}

// applySSHKeysCopy is submitSSHKeysCopy's own confirmed action: closes
// the "Copy to server" form (openConfirm's own acceptConfirm already
// closed the confirmation dialog itself — see applyFirewallAddRule's
// own identical comment), runs the install command full-screen via a
// real terminal, and — only if that succeeded — immediately follows up
// with a quick, non-interactive check (sshkeys.TestCommand) that the
// new key alone now actually gets in with no prompt at all, reporting
// that result plainly (openConfirm, reused here purely as a one-button
// acknowledgement — no second, separate "info toast" component exists
// in this app for a positive, non-error outcome) rather than just
// hoping the install succeeding also means it works.
func (r *Root) applySSHKeysCopy(host string, port int, user, privateKeyPath, installCommand string) {
	r.hideOverlay() // the "Copy to server" form itself — the SSH Keys screen underneath stays open

	runErr := r.runSSHKeysCommandFullScreen(installCommand)
	if runErr != nil {
		r.activityLog.Error(activitylog.CategorySSHKeys, fmt.Sprintf("copy key to %s@%s: %v", user, host, runErr))
		r.showError(fmt.Errorf("ssh: %w", runErr))
		return
	}
	r.activityLog.Action(activitylog.CategorySSHKeys, fmt.Sprintf("copied key to %s@%s:~/.ssh/authorized_keys", user, host))

	if privateKeyPath == "" {
		return
	}
	testCommand, err := sshkeys.TestCommand(host, port, user, privateKeyPath)
	if err != nil {
		return
	}
	if testErr := runSSHKeysTestCommand(testCommand); testErr != nil {
		r.activityLog.Action(activitylog.CategorySSHKeys, fmt.Sprintf("passwordless access to %s@%s not yet working: %v", user, host, testErr))
		r.openConfirm(fmt.Sprintf("Key installed, but passwordless access to %s@%s does not work yet (%v). Unlock/load the private key, or check the server's own sshd config.", user, host, testErr), "OK", func() {})
		return
	}
	r.activityLog.Action(activitylog.CategorySSHKeys, fmt.Sprintf("confirmed passwordless access to %s@%s", user, host))
	r.openConfirm(fmt.Sprintf("Passwordless access to %s@%s confirmed working.", user, host), "OK", func() {})
}

// sshKeysTestTimeout bounds runSSHKeysTestCommand's own real ssh
// process — a genuinely unreachable host must fail promptly with a
// clear "not working yet" rather than leaving this screen looking
// stuck; -o ConnectTimeout=10 inside the command itself already covers
// the network-level case, this is the outer floor for everything else
// (a slow-to-answer, but reachable, dead-end).
const sshKeysTestTimeout = 15 * time.Second

// runSSHKeysTestCommand runs command (see sshkeys.TestCommand) non-
// interactively — unlike runSSHKeysCommandFullScreen, this never
// suspends the TUI or attaches a real terminal: -o BatchMode=yes
// guarantees ssh itself never blocks on a prompt, so there is nothing
// for a human to answer here at all, only a quick pass/fail to capture.
var runSSHKeysTestCommand = func(command string) error {
	ctx, cancel := context.WithTimeout(context.Background(), sshKeysTestTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, userShell(), fullScreenShellArgs(command)...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
			return errors.New(trimmed)
		}
		return err
	}
	return nil
}

// runSSHKeysCommandFullScreen suspends the TUI and runs command through
// the real terminal — the same Suspend/echo/wait-for-Escape shape
// runFirewallCommandFullScreen already establishes, minus the "sudo "
// prefix: copying a key to a remote server never needs local root.
// Shared with sshkeysgenerate.go's own identically-shaped need.
func (r *Root) runSSHKeysCommandFullScreen(command string) error {
	var runErr error
	r.suspend(func() {
		fmt.Printf("$ %s\n", command)
		cmd := exec.Command(userShell(), fullScreenShellArgs(command)...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		runErr = cmd.Run()
		fmt.Print("\n[Press Esc to return to breakthrough]\n")
		waitForEscape()
	})
	return runErr
}

// applySSHKeysCopyTheme themes the "Copy to server" form — split out of
// applySSHKeysTheme, the same reasoning applySSHKeysGenerateTheme's own
// doc comment gives.
func (r *Root) applySSHKeysCopyTheme(theme config.ResolvedTheme) {
	r.sshKeysCopyForm.SetBackgroundColor(theme.SurfaceBackground)
	r.sshKeysCopyForm.SetLabelColor(theme.TextColor)
	r.sshKeysCopyForm.SetFieldBackgroundColor(theme.InputFocusedBackground)
	r.sshKeysCopyForm.SetFieldTextColor(theme.TextColor)
	r.sshKeysCopyStatus.SetBackgroundColor(theme.SurfaceBackground)
	r.sshKeysCopyButtons.SetBackgroundColor(theme.SurfaceBackground)
	styleButton(r.sshKeysCopyPickBtn, theme)
	styleButton(r.sshKeysCopyCancelBtn, theme)
	styleButton(r.sshKeysCopyGoBtn, theme)
	r.sshKeysCopyTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.sshKeysCopyTitleBar.SetTextColor(theme.TextColor)
}
