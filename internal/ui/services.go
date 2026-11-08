package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/systemd"
)

// The Services screen: a full-screen, read-only view of every systemd
// service unit on this host — a fifth full-screen catalog alongside
// Options, the Toolbox, Mounts, and Firewall, the same "its own
// destination, not folded into the Toolbox's own command list" shape
// those three already establish, since this one shows live, structured
// data rather than a catalog of commands to run.
//
// The actual systemctl shelling-out and JSON parsing live in
// internal/systemd (see its own doc comment); this file only renders
// whatever []systemd.Unit it returns. Read-only: starting, stopping,
// enabling, or disabling a unit is deliberately out of scope for this
// first version (see feature_ideas.txt) — the same staged "read-only
// first, actions later behind their own confirmation" shape Firewall's
// own "Add rule" dialog went through.

const servicesPage = "services"

// servicesState holds the Services screen's widgets and last read
// result.
type servicesState struct {
	Layout    *tview.Flex
	TitleBar  *tview.TextView
	Hint      *tview.TextView
	HintSpans []listHintSpan
	Table     *tview.Table
	Scrollbar *verticalScrollbar
	Units     []systemd.Unit
	Err       error
}

// Column indices within services.Table — Name first (identity), then
// the three short, fixed-width systemd status fields, Description last
// since it's the one unbounded column (the same "short facts before the
// one unbounded column" ordering mountsColumnWidth's own doc comment
// already explains for Target/Bind/Persistent/Type before Source).
const (
	servicesColName = iota
	servicesColActive
	servicesColSub
	servicesColLoad
	servicesColDescription
)

// servicesColumnWidth is each column's own padding floor — see
// mountsColumnWidth's own doc comment for why this is a floor, not a
// truncating ceiling. Name's own floor is wide enough for most real
// unit names; a long one (or one systemd itself renders with a
// "\x.." escape, e.g. a scope unit — see systemd.Unit's own doc
// comment) simply pushes the columns after it rather than being cut
// off.
func servicesColumnWidth(col int) int {
	switch col {
	case servicesColName:
		return 40
	case servicesColActive:
		return 8
	case servicesColSub:
		return 10
	case servicesColLoad:
		return 11
	default:
		return 0
	}
}

// servicesHintEntries is this screen's own bottom hint bar (see
// buildListHint) — a function, not a var, per activityLogHintEntries's
// own doc comment.
func servicesHintEntries() []listHintEntry {
	return []listHintEntry{
		{keys: []listHintKey{
			{"↑", simulateKeyOnFocused(tcell.KeyUp)},
			{"↓", simulateKeyOnFocused(tcell.KeyDown)},
		}, label: "move"},
		hintKey("r", "refresh", func(r *Root) { r.reloadServices() }),
		hintKey("Esc", "close", func(r *Root) { r.closeServices() }),
	}
}

// newServicesScreen builds the whole screen once, at startup — the
// same build-once/repopulate-on-open shape newMountsScreen already
// establishes.
func (r *Root) newServicesScreen() {
	r.services.Table = tview.NewTable()
	r.services.Table.SetBorders(false)
	r.services.Table.SetBorderPadding(1, 0, 2, 1)
	r.services.Table.SetSelectable(true, false)
	r.services.Table.SetFixed(1, 0)
	r.services.Table.SetInputCapture(r.captureServicesKey)

	r.services.TitleBar = newPlainTitleBar("Services")
	r.services.TitleBar.SetMouseCapture(captureReloadCloseTitleBarMouse(r.services.TitleBar, r.reloadServices, r.closeServices))

	r.services.Hint = tview.NewTextView()
	r.services.Hint.SetWrap(false)
	r.services.Hint.SetDynamicColors(true)
	servicesHintText, hintSpans := buildListHint(r.theme, servicesHintEntries())
	r.services.Hint.SetText(servicesHintText)
	r.services.HintSpans = hintSpans
	r.services.Hint.SetMouseCapture(r.captureListHintMouse(r.services.Hint, &r.services.HintSpans))

	r.services.Scrollbar = newVerticalScrollbarForTable(r, r.services.Table)
	servicesTableRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.services.Table, 0, 1, true).
		AddItem(r.services.Scrollbar, 1, 0, false)
	servicesTableRow.SetMouseCapture(r.services.Scrollbar.captureMouse)

	r.services.Layout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.services.TitleBar, 1, 0, false).
		AddItem(servicesTableRow, 0, 1, true).
		AddItem(r.services.Hint, 1, 0, false)
}

// openServices shows the Services screen, freshly read every time —
// the same "nothing here worth remembering across opens" reasoning
// openMounts already gives, since the whole point is to reflect
// whatever is actually running right now.
func (r *Root) openServices() {
	r.reloadServices()
	r.showOverlay(servicesPage, r.services.Layout)
}

// closeServices hides the Services screen. Nothing to save — this is a
// pure read-only view.
func (r *Root) closeServices() {
	r.hideOverlay()
}

// reloadServices re-reads systemd's own unit list and re-renders —
// the screen's own initial population, and "r"'s own manual refresh,
// since a service's state can change (a crash, a restart, a manual
// stop) while this screen is open and nothing here otherwise notices
// on its own.
func (r *Root) reloadServices() {
	units, err := systemd.Read()
	r.services.Units = units
	r.services.Err = err
	r.renderServices()
}

// servicesSection is one group renderServices lists units under —
// Failed first, the same "conflicts/most-actionable first" ordering
// Git Breach's own Files box already uses for Conflicts, so a failed
// service never scrolls off below a long Active list on a busy host.
// other catches any Active value this package has never seen before
// (see systemd.Unit's own doc comment) — always rendered, never
// silently dropped, even if it never has any members on a real system.
type servicesSection struct {
	label  string
	active string
}

var servicesSections = []servicesSection{
	{"Failed", "failed"},
	{"Active", "active"},
	{"Inactive", "inactive"},
}

// renderServices fills the table: a bold header row, then one group
// per servicesSections, each a non-selectable "<Label> (N)" row
// followed by its own units — the same per-section header shape
// renderGitBreachFiles already establishes for Conflicts/Staged/
// Unstaged/Untracked. A failed read (systemctl missing — expected on
// macOS/BSD, since systemd doesn't exist there — or erroring outright)
// shows in place of any rows rather than silently leaving the screen
// empty, the same "report it, don't swallow it" principle every other
// action in this app already follows.
func (r *Root) renderServices() {
	r.services.Table.Clear()

	renderReloadCloseTitleBar(r.services.TitleBar, " Services ", r.lastScreenWidth)

	header := func(col int, text string) {
		r.services.Table.SetCell(0, col,
			tview.NewTableCell(padRight(text, servicesColumnWidth(col))).
				SetTextColor(r.theme.Text).
				SetAttributes(tcell.AttrBold).
				SetSelectable(false))
	}
	header(servicesColName, "Name")
	header(servicesColActive, "Active")
	header(servicesColSub, "Sub")
	header(servicesColLoad, "Load")
	header(servicesColDescription, "Description")

	if r.services.Err != nil {
		showTablePlaceholder(r.services.Table, r.services.Err.Error(), r.theme.EntryError)
		return
	}

	enableTableSelection(r.services.Table)

	sections := servicesSections
	seen := map[string]bool{}
	for _, s := range sections {
		seen[s.active] = true
	}
	// "Other" catches every unit whose own Active value isn't one of
	// the three known ones above — see servicesSection's own doc
	// comment on why this exists at all even though it's empty on
	// every real system this was tested against.
	sections = append(sections, servicesSection{"Other", ""})

	row := 1
	for _, sec := range sections {
		var units []systemd.Unit
		for _, u := range r.services.Units {
			matches := u.Active == sec.active
			if sec.label == "Other" {
				matches = !seen[u.Active]
			}
			if matches {
				units = append(units, u)
			}
		}
		if len(units) == 0 {
			continue
		}

		r.services.Table.SetCell(row, servicesColName,
			tview.NewTableCell(fmt.Sprintf("%s (%d)", sec.label, len(units))).
				SetTextColor(r.theme.Text).
				SetAttributes(tcell.AttrBold).
				SetSelectable(false))
		row++

		for _, u := range units {
			nameColor := r.theme.Text
			if u.Active == "failed" {
				nameColor = r.theme.EntryError
			}
			r.services.Table.SetCell(row, servicesColName,
				tview.NewTableCell(padRight(u.Name, servicesColumnWidth(servicesColName))).
					SetTextColor(nameColor).SetSelectable(true))
			r.services.Table.SetCell(row, servicesColActive,
				tview.NewTableCell(padRight(u.Active, servicesColumnWidth(servicesColActive))).
					SetTextColor(nameColor).SetSelectable(true))
			r.services.Table.SetCell(row, servicesColSub,
				tview.NewTableCell(padRight(u.Sub, servicesColumnWidth(servicesColSub))).
					SetTextColor(r.theme.PlaceholderText).SetSelectable(true))
			r.services.Table.SetCell(row, servicesColLoad,
				tview.NewTableCell(padRight(u.Load, servicesColumnWidth(servicesColLoad))).
					SetTextColor(r.theme.PlaceholderText).SetSelectable(true))
			r.services.Table.SetCell(row, servicesColDescription,
				tview.NewTableCell(u.Description).
					SetTextColor(r.theme.PlaceholderText).SetSelectable(true))
			row++
		}
	}

	if row == 1 {
		showTablePlaceholder(r.services.Table, "No service units reported.", r.theme.PlaceholderText)
		return
	}
	if sel, _ := r.services.Table.GetSelection(); sel < 1 || sel >= row {
		r.services.Table.Select(1, 0)
	}
}

// captureServicesKey is the Services screen's own key handling: Escape
// closes it, "r" re-reads the live unit list — the same shape
// captureMountsKey already establishes.
func (r *Root) captureServicesKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeServices()
		return nil
	}
	if r.consigliere.Visible {
		if event.Key() == tcell.KeyTab {
			r.app.SetFocus(r.consigliere.Input)
			return nil
		}
		if event.Key() == tcell.KeyBacktab {
			if r.consigliere.Scrollbar.maxOffset() > 0 {
				r.app.SetFocus(r.consigliere.History)
			} else {
				r.app.SetFocus(r.consigliere.Input)
			}
			return nil
		}
	}
	if event.Key() == tcell.KeyRune && event.Rune() == 'r' {
		r.reloadServices()
		return nil
	}
	if event.Key() == tcell.KeyRune && event.Rune() == 'g' {
		r.startConsigliereChord(nil)
		return nil
	}
	return event
}

// applyServicesTheme themes the Services screen — split out of
// applyTheme (see its own doc comment). Guarded the same way
// applyMountsTheme is.
func (r *Root) applyServicesTheme(theme config.ResolvedTheme) {
	if r.services.Table == nil {
		return
	}
	r.services.Layout.SetBackgroundColor(theme.PanelBackground)
	r.services.Table.SetBackgroundColor(theme.PanelBackground)
	r.services.Table.SetSelectedStyle(tcell.StyleDefault.
		Background(theme.SelectionBackground).
		Foreground(theme.TextColor))

	r.services.TitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.services.TitleBar.SetTextColor(theme.TextColor)
	r.services.Hint.SetBackgroundColor(theme.SurfaceBackground)
	r.services.Hint.SetTextColor(theme.TextColor)
}
