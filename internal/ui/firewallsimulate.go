// firewallsimulate.go implements the Firewall screen's own "Simulate"
// form ("t", see firewall.go's own captureFirewallKey) — Stufe 3 of
// feature_ideas.txt's own Firewall-Regel-Baukasten: for a hypothetical
// request (Direction/Protocol/Port/Source/Destination/Interface), shows
// which real rule — if any — would actually decide it, and with what
// result. Purely an evaluation over the rules already read for the
// Firewall screen itself (see firewall.Simulate): no packet is ever
// sent, and no backend command is ever built or run, unlike the "Add
// rule" form's own submit action.
//
// Because this never touches a backend command, it isn't restricted the
// way "Add rule" is: nftables' own rules are already normalized into
// the exact same Rule list UFW/iptables produce (see ReadSnapshot), so
// Simulate works identically regardless of which single backend is
// actually active — including nftables, and even the trivial "no rule
// at all" case, both of which "Add rule" refuses outright for reasons
// that don't apply here.
package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/firewall"
)

const firewallSimulatePage = "firewall-simulate"

// openFirewallSimulate is the Firewall screen's own "t": opens the
// "Simulate" form, seeded to a harmless, fully generic default (Incoming/
// any protocol/any port/any source/any destination/any interface) fresh
// on every open — the same "never sticky from a previous fill-in"
// reasoning openFirewallAddRule's own doc comment gives.
func (r *Root) openFirewallSimulate() {
	r.firewallSimulateDirection = firewall.DirectionIn
	r.firewallSimulateProtocol = ""
	r.firewallSimulatePortText = ""
	r.firewallSimulateSourceText = ""
	r.firewallSimulateDestText = ""
	r.firewallSimulateIfaceText = ""
	r.firewallSimulateResult.SetText("")
	r.renderFirewallSimulateForm()

	// height: title bar (1) + the form's own 13 (tview.Form's own vertical
	// layout: two rows of top/bottom border padding, plus each of the
	// six items' own single row and the itemPadding row after it —
	// verified directly against tview's own form.go Draw, and against a
	// real render: an earlier, smaller value here silently clipped the
	// Interface field off the bottom, exactly the kind of thing that
	// stays invisible without an actual live check) + result (3, enough
	// for a matched rule's own multi-line summary without truncating) +
	// buttons (1).
	width, height := 66, 18
	_, _, screenWidth, screenHeight := r.GetRect()
	if width > screenWidth-4 {
		width = screenWidth - 4
	}
	if height > screenHeight-4 {
		height = screenHeight - 4
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	r.firewallSimulateLayout.SetRect(x, y, width, height)
	// pushOverlay, not showOverlay: opens on top of the still-open
	// Firewall screen, the same layering openFirewallAddRule already
	// uses.
	r.pushOverlay(firewallSimulatePage, r.firewallSimulateLayout, nil)
}

// newFirewallSimulateForm builds the (initially empty) "Simulate" form —
// called once from NewRoot; renderFirewallSimulateForm populates it
// fresh on every open, the same reasoning newFirewallAddRuleForm's own
// doc comment gives.
func (r *Root) newFirewallSimulateForm() *tview.Form {
	f := tview.NewForm()
	f.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			r.closeFirewallSimulate()
			return nil
		}
		return event
	})
	return f
}

// renderFirewallSimulateForm (re)builds firewallSimulateForm's own
// items: a Direction/Protocol dropdown pair, then Port/Source/
// Destination/Interface plain text fields — the same "value mirror kept
// in sync live" shape renderFirewallAddRuleForm's own identical fields
// already establish, minus Action (see this file's own doc comment for
// why a hypothetical request has none of its own).
func (r *Root) renderFirewallSimulateForm() {
	r.firewallSimulateForm.Clear(true)

	dirLabels := make([]string, len(firewallDirectionChoices))
	dirCurrent := 0
	for i, c := range firewallDirectionChoices {
		dirLabels[i] = c.label
		if c.value == r.firewallSimulateDirection {
			dirCurrent = i
		}
	}
	dirField := tview.NewDropDown().SetLabel("Direction").SetOptions(dirLabels, func(_ string, index int) {
		if index < 0 || index >= len(firewallDirectionChoices) {
			return
		}
		r.firewallSimulateDirection = firewallDirectionChoices[index].value
	})
	dirField.SetCurrentOption(dirCurrent)
	styleDropDown(dirField, r.theme)
	r.firewallSimulateForm.AddFormItem(dirField)

	protoLabels := make([]string, len(firewallProtocolChoices))
	protoCurrent := 0
	for i, c := range firewallProtocolChoices {
		protoLabels[i] = c.label
		if c.value == r.firewallSimulateProtocol {
			protoCurrent = i
		}
	}
	protoField := tview.NewDropDown().SetLabel("Protocol").SetOptions(protoLabels, func(_ string, index int) {
		if index < 0 || index >= len(firewallProtocolChoices) {
			return
		}
		r.firewallSimulateProtocol = firewallProtocolChoices[index].value
	})
	protoField.SetCurrentOption(protoCurrent)
	styleDropDown(protoField, r.theme)
	r.firewallSimulateForm.AddFormItem(protoField)

	portField := tview.NewInputField().SetLabel("Port (blank = any, e.g. 22 or 6000-6063)").SetText(r.firewallSimulatePortText)
	portField.SetChangedFunc(func(v string) { r.firewallSimulatePortText = v })
	r.firewallSimulateForm.AddFormItem(portField)

	sourceField := tview.NewInputField().SetLabel("Source (blank = any)").SetText(r.firewallSimulateSourceText)
	sourceField.SetChangedFunc(func(v string) { r.firewallSimulateSourceText = v })
	r.firewallSimulateForm.AddFormItem(sourceField)

	destField := tview.NewInputField().SetLabel("Destination (blank = any)").SetText(r.firewallSimulateDestText)
	destField.SetChangedFunc(func(v string) { r.firewallSimulateDestText = v })
	r.firewallSimulateForm.AddFormItem(destField)

	ifaceField := tview.NewInputField().SetLabel("Interface (blank = any)").SetText(r.firewallSimulateIfaceText)
	ifaceField.SetChangedFunc(func(v string) { r.firewallSimulateIfaceText = v })
	ifaceField.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			r.app.SetFocus(r.firewallSimulateCloseBtn)
			return nil
		case tcell.KeyEnter:
			r.runFirewallSimulate()
			return nil
		}
		return event
	})
	r.firewallSimulateForm.AddFormItem(ifaceField)
}

// newFirewallSimulateButtons builds firewallSimulateForm's own action
// row once, from NewRoot — a "Close"/"Run" pair, the same Tab/Backtab
// cycling shape newFirewallAddRuleButtons already establishes for its
// own Cancel/Add rule pair.
func (r *Root) newFirewallSimulateButtons() *tview.Flex {
	r.firewallSimulateCloseBtn = tview.NewButton("Close").SetSelectedFunc(r.closeFirewallSimulate)
	r.firewallSimulateRunBtn = tview.NewButton("Run").SetSelectedFunc(r.runFirewallSimulate)
	r.firewallSimulateCloseBtn.SetInputCapture(spaceAlsoActivates(r.closeFirewallSimulate))
	r.firewallSimulateRunBtn.SetInputCapture(spaceAlsoActivates(r.runFirewallSimulate))

	r.firewallSimulateCloseBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.firewallSimulateRunBtn)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.firewallSimulateForm)
		case tcell.KeyEscape:
			r.closeFirewallSimulate()
		}
	})
	r.firewallSimulateRunBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.firewallSimulateForm)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.firewallSimulateCloseBtn)
		case tcell.KeyEscape:
			r.closeFirewallSimulate()
		}
	})

	return tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.firewallSimulateCloseBtn, 0, 1, false).
		AddItem(r.firewallSimulateRunBtn, 0, 1, false)
}

// newFirewallSimulateLayout wraps firewallSimulateTitleBar over the
// form, a multi-line result area (blank until "Run" has something to
// say), and the Close/Run buttons — the same title-bar-over-content-
// over-status-over-buttons shape newFirewallAddRuleLayout already
// establishes, with a taller result area in place of Add-rule's own
// one-line validation status, since a matched rule's own summary needs
// more than one line.
func (r *Root) newFirewallSimulateLayout() *tview.Flex {
	r.firewallSimulateTitleBar = newPlainTitleBar("Simulate")
	r.firewallSimulateResult = tview.NewTextView().SetDynamicColors(true).SetWrap(true)
	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.firewallSimulateForm, 13, 0, true).
		AddItem(r.firewallSimulateResult, 3, 0, false).
		AddItem(r.firewallSimulateButtons, 1, 0, false)
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.firewallSimulateTitleBar, 1, 0, false).
		AddItem(content, 0, 1, true)
}

func (r *Root) closeFirewallSimulate() {
	r.hideOverlay()
}

// buildFirewallSimulateSpec reads the form's own current value mirrors
// into a firewall.NewRuleSpec — the same shape and the same free-text
// Port parsing (parseFirewallPortField) buildFirewallAddRuleSpec already
// uses; its own Action field is left at its zero value (ActionAllow)
// since firewall.Simulate ignores it entirely.
func (r *Root) buildFirewallSimulateSpec() (firewall.NewRuleSpec, error) {
	from, to, err := parseFirewallPortField(r.firewallSimulatePortText)
	if err != nil {
		return firewall.NewRuleSpec{}, err
	}
	return firewall.NewRuleSpec{
		Direction:   r.firewallSimulateDirection,
		Protocol:    r.firewallSimulateProtocol,
		PortFrom:    from,
		PortTo:      to,
		Source:      strings.TrimSpace(r.firewallSimulateSourceText),
		Destination: strings.TrimSpace(r.firewallSimulateDestText),
		Interface:   strings.TrimSpace(r.firewallSimulateIfaceText),
	}, nil
}

// runFirewallSimulate is the form's own "Run" action (button, or Enter
// on the last field — see renderFirewallSimulateForm): validates the
// form into a NewRuleSpec, then reports which rule from the Firewall
// screen's own already-read snapshot (r.firewallSnapshot) — never a
// fresh read of its own, so a simulated request is always judged
// against exactly the rules currently on screen — actually decides it.
// A malformed port is reported in place, the same as
// submitFirewallAddRule's own validation failure, without closing the
// form.
func (r *Root) runFirewallSimulate() {
	spec, err := r.buildFirewallSimulateSpec()
	if err != nil {
		r.firewallSimulateResult.SetTextColor(r.theme.CriticalText)
		r.firewallSimulateResult.SetText(err.Error())
		return
	}
	if err := spec.Validate(); err != nil {
		r.firewallSimulateResult.SetTextColor(r.theme.CriticalText)
		r.firewallSimulateResult.SetText(err.Error())
		return
	}

	rule, ok := firewall.Simulate(r.firewallSnapshot.Rules, spec)
	if !ok {
		r.firewallSimulateResult.SetTextColor(r.theme.PlaceholderText)
		if r.firewallSnapshot.Backend == firewall.BackendNone {
			r.firewallSimulateResult.SetText("No active firewall backend, so no rule decides this request.")
		} else {
			r.firewallSimulateResult.SetText(fmt.Sprintf(
				"No rule matches — falls through to %s's own default policy (not read by this app).",
				r.firewallSnapshot.Backend))
		}
		return
	}

	r.firewallSimulateResult.SetTextColor(r.actionColor(rule.Action))
	r.firewallSimulateResult.SetText(fmt.Sprintf(
		"Rule #%d: %s %s — proto %s, port %s, source %s\n(%s, chain %s)",
		rule.Order, rule.Action, rule.Direction,
		orAnyProtocol(rule.Protocol), rule.PortLabel(r.firewallServices), rule.SourceLabel(),
		rule.Backend, rule.Chain))
}

// orAnyProtocol renders "any" for a Rule's own unrestricted protocol —
// the same "never a blank cell" reasoning Rule.SourceLabel/
// DestinationLabel/InterfaceLabel already give for their own "" case,
// repeated here rather than exported from internal/firewall solely for
// this one call site.
func orAnyProtocol(protocol string) string {
	if protocol == "" {
		return "any"
	}
	return protocol
}
