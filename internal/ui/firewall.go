package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/firewall"
)

// The Firewall screen ("jf"): a fourth full-screen, read-only catalog
// alongside Options/Toolbox/Mounts, showing this host's own actual
// firewall rules — whichever single backend (UFW, nftables, or iptables)
// actually governs traffic right now, detected and normalized by
// internal/firewall (see that package's own doc comment for why exactly
// one backend is read rather than all of them). Phase 1 is deliberately
// read-only: building a new rule or simulating "what happens to a request
// on port X from IP Y" without needing to learn any firewall-specific
// syntax is real, substantial follow-up work of its own, tracked
// separately in feature_ideas.txt rather than folded into this first,
// purely observational cut.
//
// Rules are grouped into an "Incoming"/"Outgoing" section per Direction —
// first match wins within each direction's own chain independently, so
// showing them as two separate sequences (rather than one flat list
// ordered however the backend happened to print them) is what actually
// answers "which rule applies when" at a glance. A rule already shadowed
// by an earlier, broader-or-equal one in the same section (see
// firewall.AnnotateShadows) is dimmed and annotated instead, since it can
// never actually fire.

const firewallPage = "firewall"

const (
	firewallColOrder = iota
	firewallColDirection
	firewallColAction
	firewallColProtocol
	firewallColPort
	firewallColSource
	firewallColDestination
	firewallColInterface
	firewallColNote
)

// firewallColumnWidth is each column's own padding floor — never a
// ceiling, the same reasoning mountsColumnWidth's own doc comment gives.
// Note has no floor: nothing follows it.
func firewallColumnWidth(col int) int {
	switch col {
	case firewallColOrder:
		return 4
	case firewallColDirection:
		return 5
	case firewallColAction:
		return 8
	case firewallColProtocol:
		return 6
	case firewallColPort:
		return 18
	case firewallColSource:
		return 18
	case firewallColDestination:
		return 18
	case firewallColInterface:
		return 10
	default:
		return 0
	}
}

// loadFirewallServices reads /etc/services for well-known port names (see
// firewall.LoadServiceLookup). A missing or unreadable /etc/services just
// means port numbers show without a name next to them — a cosmetic
// difference, never worth failing this whole screen's own read over, so
// this returns an empty, harmless lookup instead of an error.
var loadFirewallServices = func() firewall.ServiceLookup {
	lookup, err := firewall.LoadServiceLookup("/etc/services")
	if err != nil {
		return firewall.ServiceLookup{}
	}
	return lookup
}

// readFirewallSnapshot is a package-level swappable var (the same
// mockable-exec idiom internal/firewall's own detect.go already uses for
// its lower-level run* wrappers) so this screen's own render logic can be
// exercised without ever touching a real system's actual firewall state.
var readFirewallSnapshot = firewall.ReadSnapshot

// firewallHintEntries is this screen's own bottom hint bar (see
// buildListHint) — used both at construction and by applyTheme.
var firewallHintEntries = []listHintEntry{
	{"↑/↓", "move"},
	{"r", "refresh"},
	{"a", "add rule"},
	{"t", "simulate"},
	{"Esc", "close"},
}

// newFirewallScreen builds the whole screen once, at startup — the same
// build-once/repopulate-on-open shape newMountsScreen already establishes.
func (r *Root) newFirewallScreen() {
	r.firewallTable = tview.NewTable()
	r.firewallTable.SetBorders(false)
	r.firewallTable.SetBorderPadding(1, 0, 2, 1)
	r.firewallTable.SetSelectable(true, false)
	r.firewallTable.SetFixed(1, 0)
	r.firewallTable.SetInputCapture(r.captureFirewallKey)

	r.firewallTitleBar = newPlainTitleBar("Firewall")

	r.firewallHint = tview.NewTextView()
	r.firewallHint.SetWrap(false)
	r.firewallHint.SetDynamicColors(true)
	r.firewallHint.SetText(buildListHint(r.theme, firewallHintEntries))

	r.firewallLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.firewallTitleBar, 1, 0, false).
		AddItem(r.firewallTable, 0, 1, true).
		AddItem(r.firewallHint, 1, 0, false)
}

// openFirewall shows the Firewall screen, freshly read every time — the
// whole point is to reflect this host's actual rules right now, the same
// reasoning openMounts' own doc comment gives.
func (r *Root) openFirewall() {
	r.reloadFirewall()
	r.showOverlay(firewallPage, r.firewallLayout)
}

// closeFirewall hides the Firewall screen. Nothing to save — a pure
// read-only view, same as Mounts.
func (r *Root) closeFirewall() {
	r.hideOverlay()
}

// reloadFirewall re-reads this host's own firewall rules and re-renders —
// the screen's own initial population, and "r"'s own manual refresh,
// since a rule can be added or removed by something else entirely (a
// systemd unit, another admin) while this screen is open.
func (r *Root) reloadFirewall() {
	snapshot, err := readFirewallSnapshot()
	r.firewallSnapshot = snapshot
	r.firewallErr = err
	r.firewallServices = loadFirewallServices()
	r.renderFirewall()
}

// renderFirewall fills the table: a bold header row, then one section per
// traffic Direction actually present, each headed by its own bold,
// non-selectable label row. A failed read, or no backend this package
// understands being active at all, shows in place of any rows rather than
// leaving the screen looking like an empty, successful "no rules" read —
// the same "report it, don't swallow it" principle renderMounts already
// follows.
func (r *Root) renderFirewall() {
	r.firewallTable.Clear()

	header := func(col int, text string) {
		r.firewallTable.SetCell(0, col,
			tview.NewTableCell(padRight(text, firewallColumnWidth(col))).
				SetTextColor(r.theme.Text).
				SetAttributes(tcell.AttrBold).
				SetSelectable(false))
	}
	header(firewallColOrder, "#")
	header(firewallColDirection, "Dir")
	header(firewallColAction, "Action")
	header(firewallColProtocol, "Proto")
	header(firewallColPort, "Port")
	header(firewallColSource, "Source")
	header(firewallColDestination, "Destination")
	header(firewallColInterface, "Interface")
	header(firewallColNote, "Note")

	r.firewallTitleBar.SetText(" " + firewallTitle(r.firewallSnapshot, r.firewallErr) + " ")

	// See showTablePlaceholder's own doc comment for why every early
	// return below goes through it rather than a bare SetCell — it's
	// the fix for a real, reported freeze, not just a message.
	if r.firewallErr != nil {
		showTablePlaceholder(r.firewallTable, r.firewallErr.Error(), r.theme.EntryError)
		return
	}
	if r.firewallSnapshot.Backend == firewall.BackendNone {
		showTablePlaceholder(r.firewallTable, "No supported firewall backend (ufw, nftables, or iptables) is active on this system.", r.theme.PlaceholderText)
		return
	}

	var in, out []firewall.Rule
	for _, rule := range r.firewallSnapshot.Rules {
		if rule.Direction == firewall.DirectionIn {
			in = append(in, rule)
		} else {
			out = append(out, rule)
		}
	}

	row := 1
	firstDataRow := 0
	if len(in) > 0 {
		label := row
		row = r.renderFirewallSection(row, "Incoming", in)
		firstDataRow = label + 1
	}
	if len(out) > 0 {
		label := row
		row = r.renderFirewallSection(row, "Outgoing", out)
		if firstDataRow == 0 {
			firstDataRow = label + 1
		}
	}
	if firstDataRow == 0 {
		showTablePlaceholder(r.firewallTable, "No rules configured.", r.theme.PlaceholderText)
		return
	}

	// Real, selectable rows exist again — see showTablePlaceholder's own
	// doc comment for why this isn't optional cosmetic tidiness.
	enableTableSelection(r.firewallTable)

	// Keep the cursor in range (and off the header/section-label rows)
	// after a refresh changed how many rows there are — the same
	// restraint renderMounts' own tail already shows.
	if cur, _ := r.firewallTable.GetSelection(); cur < firstDataRow || cur >= row {
		r.firewallTable.Select(firstDataRow, 0)
	}
}

// firewallTitle renders the title bar's own live summary: which backend
// is active and how many rules it reported, or that none is.
func firewallTitle(snap firewall.Snapshot, err error) string {
	if err != nil {
		return "Firewall — read failed"
	}
	if snap.Backend == firewall.BackendNone {
		return "Firewall — no active backend"
	}
	return fmt.Sprintf("Firewall — %s · %d rule(s)", snap.Backend, len(snap.Rules))
}

// renderFirewallSection writes one direction's own bold section-label row
// followed by one row per rule, starting at startRow, and returns the row
// index just past the last one written.
func (r *Root) renderFirewallSection(startRow int, label string, rules []firewall.Rule) int {
	row := startRow
	r.firewallTable.SetCell(row, firewallColOrder,
		tview.NewTableCell(label).
			SetTextColor(r.theme.Text).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false))
	row++

	for _, rule := range rules {
		r.renderFirewallRow(row, rule)
		row++
	}
	return row
}

// renderFirewallRow fills one rule's own row. A rule already shadowed by
// an earlier, broader-or-equal one (see firewall.AnnotateShadows) is
// rendered entirely in the muted color and carries a "shadowed by #N"
// note — it can never actually fire, and looking exactly as prominent as
// a rule that does would be actively misleading, not just uninformative.
func (r *Root) renderFirewallRow(row int, rule firewall.Rule) {
	textColor := r.theme.Text
	actionColor := r.actionColor(rule.Action)
	note := ""
	if rule.ShadowedByOrder != 0 {
		textColor = r.theme.MutedTextColor
		actionColor = r.theme.MutedTextColor
		note = fmt.Sprintf("shadowed by #%d (%s)", rule.ShadowedByOrder, rule.ShadowedByAction)
	}

	cell := func(col int, text string, color tcell.Color) {
		r.firewallTable.SetCell(row, col,
			tview.NewTableCell(padRight(text, firewallColumnWidth(col))).
				SetTextColor(color).SetSelectable(true))
	}
	cell(firewallColOrder, fmt.Sprintf("%d", rule.Order), textColor)
	cell(firewallColDirection, rule.Direction.String(), textColor)
	cell(firewallColAction, rule.Action.String(), actionColor)
	protocol := rule.Protocol
	if protocol == "" {
		protocol = "any"
	}
	cell(firewallColProtocol, protocol, textColor)
	cell(firewallColPort, rule.PortLabel(r.firewallServices), textColor)
	cell(firewallColSource, rule.SourceLabel(), textColor)
	cell(firewallColDestination, rule.DestinationLabel(), textColor)
	cell(firewallColInterface, rule.InterfaceLabel(), textColor)
	cell(firewallColNote, note, r.theme.WarningText)
}

// actionColor picks ALLOW's own green, DENY/REJECT's own red — the same
// "stop sign" convention a firewall's own action already carries
// everywhere else, not a color this screen invented.
func (r *Root) actionColor(action firewall.Action) tcell.Color {
	if action == firewall.ActionAllow {
		return r.theme.EntryExecutable
	}
	return r.theme.CriticalText
}

// captureFirewallKey is the Firewall screen's own key handling: Escape
// closes it, "r" re-reads this host's actual rules — the same two keys
// captureMountsKey already handles, for the same reasons — "a" opens
// the "Add rule" form (see openFirewallAddRule and feature_ideas.txt's
// own Firewall-Regel-Baukasten Stufe 2b), and "t" opens the "Simulate"
// form (see openFirewallSimulate and Stufe 3).
func (r *Root) captureFirewallKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeFirewall()
		return nil
	}
	if event.Key() == tcell.KeyRune && event.Rune() == 'r' {
		r.reloadFirewall()
		return nil
	}
	if event.Key() == tcell.KeyRune && event.Rune() == 'a' {
		r.openFirewallAddRule()
		return nil
	}
	if event.Key() == tcell.KeyRune && event.Rune() == 't' {
		r.openFirewallSimulate()
		return nil
	}
	return event
}
