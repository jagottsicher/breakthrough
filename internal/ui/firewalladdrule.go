// firewalladdrule.go implements the Firewall screen's own "Add rule"
// form ("a", see firewall.go's own captureFirewallKey) — Stufe 2b of
// feature_ideas.txt's own Firewall-Regel-Baukasten. Building a rule
// never hand-assembles firewall syntax itself: it fills in
// firewall.NewRuleSpec, asks firewall.AddRuleCommand for the exact
// backend command that would apply it, and shows that literal command
// in a confirmation dialog before ever running it — the same "irreversible
// action sichtbar machen und bestätigen" discipline Trash/Remove/Purge
// already follow (see openConfirm in trash.go).
//
// Actually running the command needs a real, attached terminal: ufw/
// iptables normally need root, and the deliberate choice here is to run
// them via "sudo" rather than assume breakthrough itself already runs
// privileged (see applyFirewallAddRule's own doc comment) — sudo's own
// interactive password prompt only works with a real terminal, the same
// reason runShellCommandFullScreen (bashconsole.go) suspends the whole
// TUI for the duration rather than capturing output non-interactively.
//
// A rule that could plausibly cut off an already-established SSH
// session (see firewall.NewRuleSpec.IsSSHRelevant) arms an automatic
// rollback once applied, but only when this breakthrough process is
// itself running over SSH (see firewall.RunningOverSSH) — see
// armFirewallRollback below.
package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/firewall"
)

const firewallAddRulePage = "firewall-add-rule"
const firewallRollbackPage = "firewall-rollback"

// firewallRollbackTimeout is how long a self-lockout rollback (see this
// file's own doc comment) waits for explicit confirmation before
// automatically reverting the rule it just applied — long enough to
// notice and act on a real terminal, short enough not to leave a live
// host needlessly exposed if breakthrough itself, not just the SSH
// session it was watching, is what's actually stuck. Not currently
// configurable: feature_ideas.txt's own Teilschritt 2b calls the exact
// duration an open product decision, and a fixed, generous default
// covers the common case without adding a setting for something this
// rarely tuned.
const firewallRollbackTimeout = 30 * time.Second

// firewallDirectionChoices/firewallActionChoices/firewallProtocolChoices
// are the "Add rule" form's own three dropdowns' choices, in display
// order — value/label pairs rather than iterating firewall.Direction/
// firewall.Action directly, so this form's own wording ("Incoming"/
// "Outgoing") can differ from Rule.Direction.String()'s terser table
// labels ("IN"/"OUT") without either side having to know about the
// other's own convention.
var firewallDirectionChoices = []struct {
	value firewall.Direction
	label string
}{
	{firewall.DirectionIn, "Incoming"},
	{firewall.DirectionOut, "Outgoing"},
}

var firewallActionChoices = []struct {
	value firewall.Action
	label string
}{
	{firewall.ActionAllow, "Allow"},
	{firewall.ActionDeny, "Deny"},
	{firewall.ActionReject, "Reject"},
}

var firewallProtocolChoices = []struct{ value, label string }{
	{"", "any"},
	{"tcp", "tcp"},
	{"udp", "udp"},
}

// openFirewallAddRule is the Firewall screen's own "a": opens the "Add
// rule" form, seeded to a harmless default (Incoming/Allow/any
// protocol/any port/any source/any destination/any interface) fresh on
// every open, never sticky from a previous fill-in — an accidentally
// reused Deny-any-port from a previous, unrelated rule would be exactly
// the kind of quiet, surprising default this app's own "keine stillen
// Fehler" principle rules out.
//
// Refuses outright for nftables (see firewall.NFTAddRuleCommand's own
// doc comment for why its table/chain layout can never be safely
// guessed) and for no active backend at all, rather than let the user
// fill in a form that could never actually submit.
func (r *Root) openFirewallAddRule() {
	switch r.firewallSnapshot.Backend {
	case firewall.BackendNone:
		r.showError(fmt.Errorf("firewall: no supported backend (ufw, nftables, or iptables) is active on this system"))
		return
	case firewall.BackendNFTables:
		r.showError(fmt.Errorf("firewall: adding a rule isn't supported for nftables here — its table/chain layout is host-specific and can't be safely guessed; use nft directly"))
		return
	}

	r.firewallAddRuleDirection = firewall.DirectionIn
	r.firewallAddRuleAction = firewall.ActionAllow
	r.firewallAddRuleProtocol = ""
	r.firewallAddRulePortText = ""
	r.firewallAddRuleSourceText = ""
	r.firewallAddRuleDestText = ""
	r.firewallAddRuleIfaceText = ""
	r.setFirewallAddRuleStatus("", r.theme.Text)
	r.renderFirewallAddRuleForm()

	// height: title bar's own row (1) + the form's own 12 (six items,
	// five rows of itemPadding between them, two rows of the Form's own
	// top/bottom border padding — the same derivation newConnectLayout's
	// own doc comment spells out for its own, smaller four-item form) +
	// status (1) + buttons (1) — checked against a real render, not
	// guessed; a shorter value silently clips the bottom rows, the same
	// lesson openSedReplace's own doc comment records.
	width, height := 66, 15
	_, _, screenWidth, screenHeight := r.GetRect()
	if width > screenWidth-4 {
		width = screenWidth - 4
	}
	if height > screenHeight-4 {
		height = screenHeight - 4
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	r.firewallAddRuleLayout.SetRect(x, y, width, height)
	// pushOverlay, not showOverlay: this opens on top of the still-open
	// Firewall screen (see pushOverlay's own doc comment), the same
	// layering openOwnerGroupPicker uses over Properties.
	r.pushOverlay(firewallAddRulePage, r.firewallAddRuleLayout, nil)
}

// newFirewallAddRuleForm builds the (initially empty) "Add rule" form —
// called once from NewRoot; renderFirewallAddRuleForm populates it
// fresh on every open, the same reasoning newCompressForm's own doc
// comment gives for a form with more than one dropdown.
func (r *Root) newFirewallAddRuleForm() *tview.Form {
	f := tview.NewForm()
	f.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			r.cancelFirewallAddRule()
			return nil
		}
		return event
	})
	return f
}

// renderFirewallAddRuleForm (re)builds firewallAddRuleForm's own items:
// Direction/Action/Protocol dropdowns, then Port/Source/Destination/
// Interface plain text fields — each one just writes straight back into
// its own firewallAddRuleXxx mirror field on every change, the same
// "value mirror kept in sync live" shape resetDuplicateForm's own
// fields already use, so buildFirewallAddRuleSpec (run only once, on
// submit) never has to read the widgets directly.
//
// The last field (Interface) gets the same explicit Tab-to-buttons/
// Enter-submits wiring newConnectForm's own password field already
// establishes: tview.Form's own default Tab handling only ever cycles
// between its own items (verified there against tview's own form.go),
// never reaching a sibling button row built outside the Form itself.
func (r *Root) renderFirewallAddRuleForm() {
	r.firewallAddRuleForm.Clear(true)

	dirLabels := make([]string, len(firewallDirectionChoices))
	dirCurrent := 0
	for i, c := range firewallDirectionChoices {
		dirLabels[i] = c.label
		if c.value == r.firewallAddRuleDirection {
			dirCurrent = i
		}
	}
	dirField := tview.NewDropDown().SetLabel("Direction").SetOptions(dirLabels, func(_ string, index int) {
		if index < 0 || index >= len(firewallDirectionChoices) {
			return
		}
		r.firewallAddRuleDirection = firewallDirectionChoices[index].value
	})
	dirField.SetCurrentOption(dirCurrent)
	styleDropDown(dirField, r.theme)
	r.firewallAddRuleForm.AddFormItem(dirField)

	actionLabels := make([]string, len(firewallActionChoices))
	actionCurrent := 0
	for i, c := range firewallActionChoices {
		actionLabels[i] = c.label
		if c.value == r.firewallAddRuleAction {
			actionCurrent = i
		}
	}
	actionField := tview.NewDropDown().SetLabel("Action").SetOptions(actionLabels, func(_ string, index int) {
		if index < 0 || index >= len(firewallActionChoices) {
			return
		}
		r.firewallAddRuleAction = firewallActionChoices[index].value
	})
	actionField.SetCurrentOption(actionCurrent)
	styleDropDown(actionField, r.theme)
	r.firewallAddRuleForm.AddFormItem(actionField)

	protoLabels := make([]string, len(firewallProtocolChoices))
	protoCurrent := 0
	for i, c := range firewallProtocolChoices {
		protoLabels[i] = c.label
		if c.value == r.firewallAddRuleProtocol {
			protoCurrent = i
		}
	}
	protoField := tview.NewDropDown().SetLabel("Protocol").SetOptions(protoLabels, func(_ string, index int) {
		if index < 0 || index >= len(firewallProtocolChoices) {
			return
		}
		r.firewallAddRuleProtocol = firewallProtocolChoices[index].value
	})
	protoField.SetCurrentOption(protoCurrent)
	styleDropDown(protoField, r.theme)
	r.firewallAddRuleForm.AddFormItem(protoField)

	portField := tview.NewInputField().SetLabel("Port (blank = any, e.g. 22 or 6000-6063)").SetText(r.firewallAddRulePortText)
	portField.SetChangedFunc(func(v string) { r.firewallAddRulePortText = v })
	r.firewallAddRuleForm.AddFormItem(portField)

	sourceField := tview.NewInputField().SetLabel("Source (blank = any)").SetText(r.firewallAddRuleSourceText)
	sourceField.SetChangedFunc(func(v string) { r.firewallAddRuleSourceText = v })
	r.firewallAddRuleForm.AddFormItem(sourceField)

	destField := tview.NewInputField().SetLabel("Destination (blank = any)").SetText(r.firewallAddRuleDestText)
	destField.SetChangedFunc(func(v string) { r.firewallAddRuleDestText = v })
	r.firewallAddRuleForm.AddFormItem(destField)

	ifaceField := tview.NewInputField().SetLabel("Interface (blank = any)").SetText(r.firewallAddRuleIfaceText)
	ifaceField.SetChangedFunc(func(v string) { r.firewallAddRuleIfaceText = v })
	ifaceField.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			r.app.SetFocus(r.firewallAddRuleCancelBtn)
			return nil
		case tcell.KeyEnter:
			r.submitFirewallAddRule()
			return nil
		}
		return event
	})
	r.firewallAddRuleForm.AddFormItem(ifaceField)
}

// newFirewallAddRuleButtons builds firewallAddRuleForm's own action row
// once, from NewRoot — the same Cancel/action button pair
// newConnectButtons already establishes, including its own Tab/Backtab
// cycling between the two buttons and back into the form.
func (r *Root) newFirewallAddRuleButtons() *tview.Flex {
	r.firewallAddRuleCancelBtn = tview.NewButton("Cancel").SetSelectedFunc(r.cancelFirewallAddRule)
	r.firewallAddRuleAddBtn = tview.NewButton("Add rule").SetSelectedFunc(r.submitFirewallAddRule)
	r.firewallAddRuleCancelBtn.SetInputCapture(spaceAlsoActivates(r.cancelFirewallAddRule))
	r.firewallAddRuleAddBtn.SetInputCapture(spaceAlsoActivates(r.submitFirewallAddRule))

	r.firewallAddRuleCancelBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.firewallAddRuleAddBtn)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.firewallAddRuleForm)
		case tcell.KeyEscape:
			r.cancelFirewallAddRule()
		}
	})
	r.firewallAddRuleAddBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.firewallAddRuleForm)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.firewallAddRuleCancelBtn)
		case tcell.KeyEscape:
			r.cancelFirewallAddRule()
		}
	})

	return tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.firewallAddRuleCancelBtn, 0, 1, false).
		AddItem(r.firewallAddRuleAddBtn, 0, 1, false)
}

// newFirewallAddRuleLayout wraps firewallAddRuleTitleBar over the form,
// a one-line status area (blank until submitFirewallAddRule has
// something to say — a validation error), and the Cancel/Add rule
// buttons — the same title-bar-over-content-over-status-over-buttons
// shape newConnectLayout already establishes.
func (r *Root) newFirewallAddRuleLayout() *tview.Flex {
	r.firewallAddRuleTitleBar = newPlainTitleBar("Add rule")
	r.firewallAddRuleStatus = tview.NewTextView().SetDynamicColors(true)
	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.firewallAddRuleForm, 12, 0, true).
		AddItem(r.firewallAddRuleStatus, 1, 0, false).
		AddItem(r.firewallAddRuleButtons, 1, 0, false)
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.firewallAddRuleTitleBar, 1, 0, false).
		AddItem(content, 0, 1, true)
}

func (r *Root) setFirewallAddRuleStatus(text string, color tcell.Color) {
	r.firewallAddRuleStatus.SetTextColor(color)
	r.firewallAddRuleStatus.SetText(text)
}

func (r *Root) cancelFirewallAddRule() {
	r.hideOverlay()
}

// buildFirewallAddRuleSpec reads the form's own current value mirrors
// into a firewall.NewRuleSpec — the one place the free-text Port field
// is parsed into PortFrom/PortTo (see parseFirewallPortField), so
// submitFirewallAddRule itself only ever deals with a real
// NewRuleSpec, never raw form text.
func (r *Root) buildFirewallAddRuleSpec() (firewall.NewRuleSpec, error) {
	from, to, err := parseFirewallPortField(r.firewallAddRulePortText)
	if err != nil {
		return firewall.NewRuleSpec{}, err
	}
	return firewall.NewRuleSpec{
		Direction:   r.firewallAddRuleDirection,
		Action:      r.firewallAddRuleAction,
		Protocol:    r.firewallAddRuleProtocol,
		PortFrom:    from,
		PortTo:      to,
		Source:      strings.TrimSpace(r.firewallAddRuleSourceText),
		Destination: strings.TrimSpace(r.firewallAddRuleDestText),
		Interface:   strings.TrimSpace(r.firewallAddRuleIfaceText),
	}, nil
}

// parseFirewallPortField parses the Port field's own free text: blank
// for "any port" (both zero, NewRuleSpec's own sentinel — see
// NewRuleSpec.HasAnyPort), a single number for one exact port, or
// "N-M" for a range — the same "-" separator the form's own placeholder
// text and UFWAddRuleCommand's own rendering both already use, so what
// the user types and what a resulting rule shows back both agree.
func parseFirewallPortField(text string) (from, to int, err error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, 0, nil
	}
	if before, after, ok := strings.Cut(text, "-"); ok {
		from, err = strconv.Atoi(strings.TrimSpace(before))
		if err != nil {
			return 0, 0, fmt.Errorf("port range %q: %w", text, err)
		}
		to, err = strconv.Atoi(strings.TrimSpace(after))
		if err != nil {
			return 0, 0, fmt.Errorf("port range %q: %w", text, err)
		}
		return from, to, nil
	}
	port, err := strconv.Atoi(text)
	if err != nil {
		return 0, 0, fmt.Errorf("port %q: %w", text, err)
	}
	return port, port, nil
}

// submitFirewallAddRule is the form's own "Add rule" action (button,
// or Enter on the last field — see renderFirewallAddRuleForm):
// validates the form into a NewRuleSpec, builds the exact backend
// command that would apply it (see firewall.AddRuleCommand), and asks
// for explicit confirmation showing that literal command — never
// running anything before the user has seen and accepted exactly what
// will run. A validation failure (a malformed port, or a port with no
// protocol on iptables — see IPTablesAddRuleCommand's own doc comment)
// is reported in place, in firewallAddRuleStatus, without closing the
// form, so a typo can be fixed and resubmitted immediately.
func (r *Root) submitFirewallAddRule() {
	spec, err := r.buildFirewallAddRuleSpec()
	if err != nil {
		r.setFirewallAddRuleStatus(err.Error(), r.theme.CriticalText)
		return
	}
	if err := spec.Validate(); err != nil {
		r.setFirewallAddRuleStatus(err.Error(), r.theme.CriticalText)
		return
	}
	backend := r.firewallSnapshot.Backend
	command, err := firewall.AddRuleCommand(backend, spec)
	if err != nil {
		r.setFirewallAddRuleStatus(err.Error(), r.theme.CriticalText)
		return
	}

	message := fmt.Sprintf("Run this exact command now? sudo %s", command)
	r.openConfirm(message, "Yes, add rule", func() {
		r.applyFirewallAddRule(backend, spec, command)
	})
}

// applyFirewallAddRule is submitFirewallAddRule's own confirmed action:
// closes the "Add rule" form (openConfirm's own acceptConfirm already
// closed the confirmation dialog itself before calling this — see its
// own doc comment), then runs "sudo <command>" full-screen, via a real
// terminal (see runFirewallCommandFullScreen), exactly like
// runShellCommandFullScreen already does for the bash line — sudo's
// own interactive password prompt needs one.
//
// The "sudo " prefix is added here, not baked into
// firewall.AddRuleCommand's own return value: every existing read path
// in internal/firewall (ufw status, iptables-save, nft list ruleset)
// already assumes breakthrough itself runs privileged rather than
// prepending sudo itself, but a whole TUI file manager run as root just
// to use this one screen is an unreasonable ask of an otherwise
// unprivileged, everyday session — asking for a password only for the
// one actually privileged action, the moment it's actually needed, is
// the same principle sudo itself exists for.
//
// Reloads the Firewall screen's own snapshot afterwards either way, so
// a real failure (a typo'd interface name, a conflicting existing rule)
// still shows today's actual rules, not a stale pre-attempt read.
func (r *Root) applyFirewallAddRule(backend firewall.Backend, spec firewall.NewRuleSpec, command string) {
	r.hideOverlay() // the "Add rule" form itself — the Firewall screen underneath stays open

	runErr := r.runFirewallCommandFullScreen(command)
	r.reloadFirewall()

	if runErr != nil {
		r.activityLog.Error(activitylog.CategoryFirewall, fmt.Sprintf("add rule %q: %v", command, runErr))
		r.showError(fmt.Errorf("firewall: %s: %w", command, runErr))
		return
	}
	r.activityLog.Action(activitylog.CategoryFirewall, fmt.Sprintf("added rule: %s", command))

	if spec.Action != firewall.ActionAllow && spec.IsSSHRelevant() && firewall.RunningOverSSH() {
		r.armFirewallRollback(backend, spec)
	}
}

// runFirewallCommandFullScreen suspends the TUI and runs "sudo
// <command>" through the real terminal — the same Suspend/echo/wait-
// for-Escape shape runShellCommandFullScreen (bashconsole.go)
// establishes for the bash line, just standalone: this isn't reached
// from bashLine, so there's no history, "cd" special-case, or panel
// directory to thread through here.
func (r *Root) runFirewallCommandFullScreen(command string) error {
	full := "sudo " + command
	var runErr error
	r.app.Suspend(func() {
		fmt.Printf("$ %s\n", full)
		cmd := exec.Command(userShell(), fullScreenShellArgs(full)...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		runErr = cmd.Run()
		fmt.Print("\n[Press Esc to return to breakthrough]\n")
		waitForEscape()
	})
	return runErr
}

// armFirewallRollback starts the self-lockout rollback (see this
// file's own doc comment): a real timer that reverts spec via
// rollbackFirewallRule once firewallRollbackTimeout passes, plus a
// countdown overlay offering "Keep this rule" to cancel it early.
// Called only once per applied rule — a second SSH-relevant rule
// applied while one rollback is already armed replaces it outright
// (see its own r.firewallRollbackTimer.Stop() below) rather than
// stacking two independent rollbacks that could race each other's own
// DeleteRuleCommand.
func (r *Root) armFirewallRollback(backend firewall.Backend, spec firewall.NewRuleSpec) {
	if r.firewallRollbackTimer != nil {
		r.firewallRollbackTimer.Stop()
	}
	if r.firewallRollbackCancel != nil {
		r.firewallRollbackCancel()
	}

	r.firewallRollbackBackend = backend
	r.firewallRollbackSpec = spec
	r.firewallRollbackDeadline = time.Now().Add(firewallRollbackTimeout)
	r.firewallRollbackTimer = time.AfterFunc(firewallRollbackTimeout, func() {
		r.app.QueueUpdateDraw(func() {
			r.rollbackFirewallRule()
		})
	})

	ctx, cancel := context.WithCancel(context.Background())
	r.firewallRollbackCancel = cancel
	r.updateFirewallRollbackText()
	onPanic := func() { cancel() }
	r.safeGo("firewall rollback countdown", onPanic, func() { r.animateFirewallRollbackCountdown(ctx) })

	width, height := 60, 5
	_, _, screenWidth, screenHeight := r.GetRect()
	if width > screenWidth-4 {
		width = screenWidth - 4
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	r.firewallRollbackLayout.SetRect(x, y, width, height)
	r.pushOverlay(firewallRollbackPage, r.firewallRollbackLayout, nil)
}

// animateFirewallRollbackCountdown refreshes firewallRollbackText once
// a second until ctx is cancelled (by keepFirewallRule or
// rollbackFirewallRule, whichever runs first) — the same ticker-driven
// live-update shape animateConnectProgress already establishes for its
// own "Connecting…" animation.
func (r *Root) animateFirewallRollbackCountdown(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
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
				r.updateFirewallRollbackText()
			})
		case <-ctx.Done():
			return
		}
	}
}

// updateFirewallRollbackText renders the countdown overlay's own
// remaining-seconds text — never negative: the timer's own AfterFunc
// callback is what actually fires the rollback once the deadline
// passes, this just reflects however much time is left until then.
func (r *Root) updateFirewallRollbackText() {
	remaining := time.Until(r.firewallRollbackDeadline).Round(time.Second)
	if remaining < 0 {
		remaining = 0
	}
	r.firewallRollbackText.SetText(fmt.Sprintf(
		"SSH-relevant rule applied.\nReverts automatically in %s unless kept.", remaining))
}

// newFirewallRollbackLayout builds the self-lockout countdown overlay
// once, from NewRoot: a title bar, the live countdown text, and a
// single "Keep this rule" button — Escape only hides this overlay (see
// this file's own doc comment on why the timer itself keeps running
// regardless).
func (r *Root) newFirewallRollbackLayout() *tview.Flex {
	r.firewallRollbackTitleBar = newPlainTitleBar("Self-lockout protection")
	r.firewallRollbackText = tview.NewTextView()
	r.firewallRollbackText.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			r.hideOverlay()
			return nil
		}
		return event
	})
	buttons := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(tview.NewBox(), 0, 1, false).
		AddItem(r.firewallRollbackKeepBtn, 20, 0, false).
		AddItem(tview.NewBox(), 0, 1, false)
	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.firewallRollbackText, 2, 0, true).
		AddItem(buttons, 1, 0, false)
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.firewallRollbackTitleBar, 1, 0, false).
		AddItem(content, 0, 1, true)
}

// keepFirewallRule is "Keep this rule": stops the armed rollback dead
// (both the real time.AfterFunc and the countdown's own ticker
// goroutine) and closes the overlay — the rule stays exactly as
// applied, no DeleteRuleCommand ever runs.
func (r *Root) keepFirewallRule() {
	if r.firewallRollbackTimer != nil {
		r.firewallRollbackTimer.Stop()
		r.firewallRollbackTimer = nil
	}
	if r.firewallRollbackCancel != nil {
		r.firewallRollbackCancel()
		r.firewallRollbackCancel = nil
	}
	r.activityLog.Action(activitylog.CategoryFirewall, "kept SSH-relevant rule (self-lockout rollback cancelled)")
	r.hideOverlay()
}

// rollbackFirewallRule is the armed timer's own automatic action once
// firewallRollbackTimeout passes unconfirmed: builds and runs the exact
// DeleteRuleCommand that reverses the rule armFirewallRollback recorded
// (see firewall.DeleteRuleCommand), the same full-screen/sudo execution
// applyFirewallAddRule's own addition used to apply it in the first
// place, then reports the outcome — a failed rollback is exactly the
// one case this app must never swallow silently: whoever is watching
// needs to know the rule is still in place.
func (r *Root) rollbackFirewallRule() {
	if r.firewallRollbackCancel != nil {
		r.firewallRollbackCancel()
		r.firewallRollbackCancel = nil
	}
	r.firewallRollbackTimer = nil
	if r.activePage == firewallRollbackPage {
		r.hideOverlay()
	}

	backend, spec := r.firewallRollbackBackend, r.firewallRollbackSpec
	command, err := firewall.DeleteRuleCommand(backend, spec)
	if err != nil {
		r.activityLog.Error(activitylog.CategoryFirewall, fmt.Sprintf("self-lockout rollback: %v", err))
		r.showError(fmt.Errorf("firewall: self-lockout rollback: %w", err))
		return
	}

	runErr := r.runFirewallCommandFullScreen(command)
	r.reloadFirewall()
	if runErr != nil {
		r.activityLog.Error(activitylog.CategoryFirewall, fmt.Sprintf("self-lockout rollback %q: %v", command, runErr))
		r.showError(fmt.Errorf("firewall: self-lockout rollback failed — the rule is still in place: %s: %w", command, runErr))
		return
	}
	r.activityLog.Action(activitylog.CategoryFirewall, fmt.Sprintf("self-lockout rollback: %s", command))
	r.showError(fmt.Errorf("firewall: the SSH-relevant rule wasn't confirmed in time and has been reverted"))
}
