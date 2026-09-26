package ui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// The Toolbox screen: a full-screen, browsable catalog of real external
// networking and hardware tools — the "eventually every Toolbox entry"
// openToolCommand's own doc comment already promised (toolwindow.go),
// and Ping's own real entry point at last, in place of the no-longer-
// existing context-menu row it used to need before this screen existed.
//
// Every entry either runs immediately (toolboxFixedEntry — lsblk,
// lscpu, ...) or asks for one further argument first through
// toolboxInput, a small field floated on top of this screen the same
// way optionsInput floats on top of Options (toolboxArgEntry — ping,
// nmap, dig, ...). Either way, the command that actually runs is a real
// external process, streamed live into a toolWindow via
// openToolCommand — never reimplemented, matching every other external-
// tool integration this app already has (du/df/grep/sed/find/rsync).
//
// The catalog lives in toolboxCategories, not in the screen-rendering
// code below: this file knows how to browse and run *an* entry, never
// which ones exist.

const (
	toolboxPage      = "toolbox"
	toolboxInputPage = "toolbox-input"
)

// Column indices within toolboxTable.
const (
	toolboxColLabel = iota
	toolboxColHelp
)

// toolboxLabelWidth is the Name column's fixed width — wide enough for
// the longest entry label plus room to breathe, and fixed rather than
// self-sizing so the description column always starts in the same
// place, the same reasoning optionsLabelWidth's own doc comment gives.
const toolboxLabelWidth = 30

// toolboxEntry is one runnable command in the Toolbox catalog.
type toolboxEntry struct {
	label string // shown in the Name column, and used as the toolWindow's own title for a fixed entry
	help  string // shown in the Description column
	run   func(r *Root)
}

// toolboxCategory groups related entries under one header row.
type toolboxCategory struct {
	name    string
	entries []toolboxEntry
}

// toolboxFixedEntry builds an entry that needs no further input at
// all: the args are exactly what runs, every time.
func toolboxFixedEntry(label, help, name string, args ...string) toolboxEntry {
	return toolboxEntry{
		label: label,
		help:  help,
		run: func(r *Root) {
			title := name
			if len(args) > 0 {
				title = name + " " + strings.Join(args, " ")
			}
			r.openToolCommand(title, name, args)
		},
	}
}

// toolboxArgEntry builds an entry that first asks for one further
// argument — a host, a URL, a domain, or a handful of free-form
// flags/arguments — through the shared toolboxInput overlay (see
// toolboxPromptForArg). The typed text is split on whitespace and
// appended after fixedArgs, so "getent" (no fixed args) plus typed
// "hosts example.com" runs "getent hosts example.com", and "tail -f"
// (fixedArgs ["-f"]) plus typed "/var/log/syslog" runs exactly that.
func toolboxArgEntry(label, help, promptLabel, prefill, name string, fixedArgs ...string) toolboxEntry {
	return toolboxEntry{
		label: label,
		help:  help,
		run: func(r *Root) {
			r.toolboxPromptForArg(promptLabel, prefill, func(text string) {
				args := append(append([]string{}, fixedArgs...), strings.Fields(text)...)
				r.openToolCommand(name+" "+strings.Join(args, " "), name, args)
			})
		},
	}
}

// toolboxCategoriesNamed returns the catalog filtered down to the
// categories in names, preserving toolboxCategories' own order — used by
// the "jn"/"jh" screens (openNetworkTools/openHardwareTools) to show only
// one category at a time. No names at all (the empty call) means "no
// filter", i.e. the full catalog, the same list toolboxCategories itself
// returns.
func toolboxCategoriesNamed(names ...string) []toolboxCategory {
	if len(names) == 0 {
		return toolboxCategories()
	}
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	var out []toolboxCategory
	for _, cat := range toolboxCategories() {
		if want[cat.name] {
			out = append(out, cat)
		}
	}
	return out
}

// toolboxCategories is the whole Toolbox catalog — every networking and
// hardware tool from the project's own longer-term notes, each wired
// directly to its real command line.
//
// Ping deliberately runs plain "ping <host>", no "-c N": it's meant to
// run until explicitly stopped, exactly the case a toolWindow is for
// (see openToolCommand's own doc comment) — the one entry here that
// predates this screen.
func toolboxCategories() []toolboxCategory {
	return []toolboxCategory{
		{name: "Networking", entries: []toolboxEntry{
			toolboxArgEntry("Ping", "Send ICMP echo requests to a host, until stopped", "Ping host:", "", "ping"),
			toolboxArgEntry("Nmap scan", "Scan a host, range, or CIDR for open ports", "Nmap target:", "", "nmap"),
			toolboxFixedEntry("IP addresses (ip)", "Show every network interface and its addresses", "ip", "addr"),
			toolboxFixedEntry("Routing table (route)", "Show the kernel's own routing table", "route", "-n"),
			toolboxFixedEntry("Sockets (ss)", "List open TCP/UDP sockets and the process on each", "ss", "-tulpn"),
			toolboxArgEntry("getent", `Query a system database (e.g. "hosts example.com")`, `getent (database key, e.g. "hosts example.com"):`, "", "getent"),
			toolboxArgEntry("wget", "Download a URL into the current directory", "wget URL:", "", "wget"),
			toolboxArgEntry("nslookup", "Resolve a hostname the old-school way", "nslookup host:", "", "nslookup"),
			toolboxArgEntry("dig", "Resolve a hostname with full DNS record detail", "dig domain:", "", "dig"),
			toolboxArgEntry("netcat (nc)", `Test a TCP/UDP connection, or listen on a port`, `nc arguments (e.g. "-zv host 22"):`, "", "nc"),
			toolboxArgEntry("curl", "Fetch a URL and show the response", `curl arguments (e.g. "-I https://example.com"):`, "", "curl"),
			toolboxArgEntry("Logviewer (tail -f)", "Follow a growing log file live", "Log file to follow:", "/var/log/syslog", "tail", "-f"),
		}},
		{name: "Hardware", entries: []toolboxEntry{
			toolboxFixedEntry("Block devices (lsblk)", "List disks, partitions, and their filesystems", "lsblk", "-f"),
			toolboxFixedEntry("USB devices (lsusb)", "List every USB device currently attached", "lsusb"),
			toolboxFixedEntry("CPU info (lscpu)", "Show CPU architecture, cores, and cache details", "lscpu"),
			toolboxFixedEntry("Memory devices (lsmem)", "Show installed memory ranges and their state", "lsmem"),
			toolboxFixedEntry("Kernel devices (lsdev)", "List devices known to the running kernel", "lsdev"),
			toolboxFixedEntry("Hardware summary (hwinfo)", "Detailed hardware inventory, one line each", "hwinfo", "--short"),
			toolboxFixedEntry("System overview (inxi)", "A single, readable, all-in-one system summary", "inxi", "-Fxz"),
			toolboxFixedEntry("SCSI devices (lsscsi)", "List SCSI/SATA disks and optical drives", "lsscsi"),
		}},
	}
}

// toolboxDisplayRow is one row of the Toolbox table: a real, runnable
// entry, a category header, or a blank spacer row between two
// categories — exactly one of blank/header is set for a non-entry row,
// mirroring optionDisplayRow's own shape in optionsscreen.go.
type toolboxDisplayRow struct {
	blank  bool
	header string
	entry  toolboxEntry
}

// isEntry reports whether this row is a real, selectable command rather
// than a header or a blank spacer.
func (dr toolboxDisplayRow) isEntry() bool {
	return !dr.blank && dr.header == ""
}

// toolboxDisplayRowsFor expands categories into the table's own row
// list: one header row per category, a blank spacer between two
// categories (never before the first one), then each category's own
// entries. The screen currently open (see r.toolboxRows) decides which
// categories that is — one single category, for "jn" or "jh".
func toolboxDisplayRowsFor(categories []toolboxCategory) []toolboxDisplayRow {
	var rows []toolboxDisplayRow
	for i, cat := range categories {
		if i > 0 {
			rows = append(rows, toolboxDisplayRow{blank: true})
		}
		rows = append(rows, toolboxDisplayRow{header: cat.name})
		for _, e := range cat.entries {
			rows = append(rows, toolboxDisplayRow{entry: e})
		}
	}
	return rows
}

// toolboxDisplayRows is toolboxDisplayRowsFor for the whole catalog —
// the one every test not concerned with category filtering exercises
// directly.
func toolboxDisplayRows() []toolboxDisplayRow {
	return toolboxDisplayRowsFor(toolboxCategories())
}

// toolboxEntryAtRowIn is the entry shown on one row of rows — false for a
// row out of range, or a header/blank row, neither of which has an entry
// of its own to run.
func toolboxEntryAtRowIn(rows []toolboxDisplayRow, row int) (toolboxEntry, bool) {
	if row < 0 || row >= len(rows) || !rows[row].isEntry() {
		return toolboxEntry{}, false
	}
	return rows[row].entry, true
}

// toolboxEntryAtRow is toolboxEntryAtRowIn against the whole catalog.
func toolboxEntryAtRow(row int) (toolboxEntry, bool) {
	return toolboxEntryAtRowIn(toolboxDisplayRows(), row)
}

// firstSelectableToolboxRowIn is the first row in rows that is a real
// entry rather than a category's own leading header.
func firstSelectableToolboxRowIn(rows []toolboxDisplayRow) int {
	for row, dr := range rows {
		if dr.isEntry() {
			return row
		}
	}
	return 0
}

// firstSelectableToolboxRow is firstSelectableToolboxRowIn against the
// whole catalog.
func firstSelectableToolboxRow() int {
	return firstSelectableToolboxRowIn(toolboxDisplayRows())
}

// toolboxHintEntries is this screen's own bottom hint bar (see
// buildListHint) — used both at construction and by applyTheme.
var toolboxHintEntries = []listHintEntry{
	{"↑/↓", "move"},
	{"Enter", "run"},
	{"Esc", "close"},
}

// newToolboxScreen builds the whole screen once, at startup — the same
// build-once/repopulate-on-open shape newOptionsScreen already
// establishes. Only the table's own contents are rebuilt per open (see
// renderToolbox); the widgets themselves live for the application's
// lifetime.
func (r *Root) newToolboxScreen() {
	r.toolboxTable = tview.NewTable()
	r.toolboxTable.SetBorders(false)
	r.toolboxTable.SetBorderPadding(1, 0, 2, 1)
	r.toolboxTable.SetSelectable(true, false) // whole rows: one tool per row
	r.toolboxTable.SetSelectedFunc(func(row, _ int) { r.activateToolboxRow(row) })
	r.toolboxTable.SetMouseCapture(r.captureToolboxTableMouse)
	r.toolboxTable.SetInputCapture(r.captureToolboxKey)

	r.toolboxTitleBar = tview.NewTextView()
	r.toolboxTitleBar.SetWrap(false)
	r.toolboxTitleBar.SetText(" Toolbox ")

	r.toolboxHint = tview.NewTextView()
	r.toolboxHint.SetWrap(false)
	r.toolboxHint.SetDynamicColors(true)
	r.toolboxHint.SetText(buildListHint(r.theme, toolboxHintEntries))

	r.toolboxLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.toolboxTitleBar, 1, 0, false).
		AddItem(r.toolboxTable, 0, 1, true).
		AddItem(r.toolboxHint, 1, 0, false)

	// The one small floating input this screen layers on top of itself
	// (see toolboxPromptForArg) — the same "one shared, repopulated
	// field" pattern r.prompt/r.optionsInput already use elsewhere.
	r.toolboxInput = tview.NewInputField()
}

// openToolboxScreen shows the Toolbox screen filled with categories and
// titled title — the one shared mechanism behind "jn" (openNetworkTools,
// Networking only) and "jh" (openHardwareTools, Hardware only): a
// single table/rendering implementation, parameterized by which
// categories it currently shows, rather than two near-identical
// screens. Rebuilt fresh every time — there is no per-session state
// here worth remembering across opens the way Options remembers its
// last category.
func (r *Root) openToolboxScreen(title string, categories []toolboxCategory) {
	r.toolboxRows = toolboxDisplayRowsFor(categories)
	r.toolboxTitleBar.SetText(" " + title + " ")
	r.renderToolbox()
	r.showOverlay(toolboxPage, r.toolboxLayout)
}

// openNetworkTools shows only the catalog's "Networking" category.
func (r *Root) openNetworkTools() {
	r.openToolboxScreen("Network Tools", toolboxCategoriesNamed("Networking"))
}

// openHardwareTools is openNetworkTools' own counterpart for the
// catalog's "Hardware" category.
func (r *Root) openHardwareTools() {
	r.openToolboxScreen("Hardware Tools", toolboxCategoriesNamed("Hardware"))
}

// closeToolbox hides the Toolbox screen. Nothing to save — every entry
// either already ran (into its own independent toolWindow) or wasn't
// activated at all.
func (r *Root) closeToolbox() {
	r.hideOverlay()
}

// renderToolbox fills the table with the whole catalog: a bold, dimmed
// header per category, a blank spacer between two of them, and the Name
// plus Description columns for every real entry.
//
// The cursor is only ever reset to the first real entry when it isn't
// already sitting on one — landing there the first time this ever
// renders (before anything has been selected at all), but left alone on
// every later call (a live color-scheme switch while this screen is
// open), the same "don't reset a in-range, already-correct choice"
// restraint renderOptions' own tail shows.
func (r *Root) renderToolbox() {
	r.toolboxTable.Clear()

	for row, dr := range r.toolboxRows {
		switch {
		case dr.blank:
			r.toolboxTable.SetCell(row, toolboxColLabel, tview.NewTableCell("").SetSelectable(false))
			r.toolboxTable.SetCell(row, toolboxColHelp, tview.NewTableCell("").SetSelectable(false))
		case dr.header != "":
			r.toolboxTable.SetCell(row, toolboxColLabel,
				tview.NewTableCell(padRight(dr.header, toolboxLabelWidth)).
					SetTextColor(r.theme.PlaceholderText).
					SetAttributes(tcell.AttrBold).
					SetSelectable(false))
			r.toolboxTable.SetCell(row, toolboxColHelp, tview.NewTableCell("").SetSelectable(false))
		default:
			r.toolboxTable.SetCell(row, toolboxColLabel,
				tview.NewTableCell(padRight(dr.entry.label, toolboxLabelWidth)).
					SetTextColor(r.theme.Text).
					SetSelectable(true))
			r.toolboxTable.SetCell(row, toolboxColHelp,
				tview.NewTableCell(dr.entry.help).
					SetTextColor(r.theme.PlaceholderText).
					SetSelectable(true))
		}
	}

	if row, _ := r.toolboxTable.GetSelection(); !isToolboxEntryRowIn(r.toolboxRows, row) {
		r.toolboxTable.Select(firstSelectableToolboxRowIn(r.toolboxRows), 0)
	}
}

// isToolboxEntryRowIn reports whether row refers to a real, runnable
// entry within rows — used instead of a plain bounds check (see
// renderToolbox) since a category's own leading row is always a header,
// never a valid cursor position on its own.
func isToolboxEntryRowIn(rows []toolboxDisplayRow, row int) bool {
	_, ok := toolboxEntryAtRowIn(rows, row)
	return ok
}

// isToolboxEntryRow is isToolboxEntryRowIn against the whole catalog.
func isToolboxEntryRow(row int) bool {
	return isToolboxEntryRowIn(toolboxDisplayRows(), row)
}

// activateToolboxRow is Enter (or a click) on an entry: run it.
func (r *Root) activateToolboxRow(row int) {
	if entry, ok := toolboxEntryAtRowIn(r.toolboxRows, row); ok {
		entry.run(r)
	}
}

// captureToolboxKey is the Toolbox screen's own key handling: Escape
// closes it. Enter already reaches activateToolboxRow through the
// table's own SetSelectedFunc (see newToolboxScreen), the same as every
// other selectable table/list in this app.
func (r *Root) captureToolboxKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeToolbox()
		return nil
	}
	return event
}

// captureToolboxTableMouse routes a click on a real entry's row to
// running it, the same "select then activate" shape
// captureOptionsTableMouse already establishes.
func (r *Root) captureToolboxTableMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action != tview.MouseLeftClick {
		return action, event
	}
	x, y := event.Position()
	if !r.toolboxTable.InRect(x, y) {
		return action, event
	}

	row, _ := r.toolboxTable.CellAt(x, y)
	if !isToolboxEntryRowIn(r.toolboxRows, row) {
		return action, event
	}
	r.toolboxTable.Select(row, 0)
	r.activateToolboxRow(row)
	return tview.MouseConsumed, nil
}

// toolboxPromptForArg is the Toolbox screen's own small floating input
// overlay for an entry that needs one further argument (see
// toolboxArgEntry) — pushed on top of the Toolbox screen rather than
// replacing it (see pushOverlay), the same "small input floats over the
// full-screen list it belongs to" shape editOptionValue already
// establishes for Options' own optionsInput.
//
// Reusing the generic r.prompt/openPrompt here instead would have
// closed the Toolbox screen underneath it first (see openPrompt's own
// use of showOverlay, which calls closeAllOverlays) — wrong for a
// screen everything here is meant to return to once the command has
// started running in its own independent tool window.
func (r *Root) toolboxPromptForArg(label, prefill string, onSubmit func(text string)) {
	r.toolboxInput.SetLabel(" " + label + " ")
	r.toolboxInput.SetText(prefill)
	r.toolboxInput.SetAcceptanceFunc(nil)
	r.toolboxInput.SetDoneFunc(func(key tcell.Key) {
		text := strings.TrimSpace(r.toolboxInput.GetText())
		r.hideOverlay()
		if key != tcell.KeyEnter || text == "" {
			return
		}
		onSubmit(text)
	})

	width, height := 64, 1
	x, y := r.centeredOnScreen(width, height)
	x, y, width, height = r.clampToScreen(x, y, width, height)
	r.toolboxInput.SetRect(x, y, width, height)
	r.pushOverlay(toolboxInputPage, r.toolboxInput, nil)
}
