package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
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
	toolboxFormPage  = "toolbox-form"
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

// toolboxFieldKind is one toolboxFormField's own shape — text (a plain
// InputField), choice (a DropDown among a fixed set of options, e.g.
// curl's Method), or toggle (a boolean, rendered as a row in
// toolboxFormToggles rather than a Form checkbox — see toolboxFormEntry's
// own doc comment for why).
type toolboxFieldKind int

const (
	toolboxFieldText toolboxFieldKind = iota
	toolboxFieldChoice
	toolboxFieldToggle
)

// toolboxFormField describes one field of a toolboxFormEntry. name is a
// stable key, never a positional index — renderToolboxForm/
// buildToolboxFormValues read a field's current value back out of
// toolboxFormWidgets/toolboxFormToggleState by this name, so inserting or
// reordering fields later can never silently shift which value lands in
// which build() argument the way a positional []string would.
type toolboxFormField struct {
	name  string // stable key, read back via toolboxFormValues
	label string

	kind toolboxFieldKind

	placeholder string // text only
	defaultText string // text and choice
	choices     []string
	defaultBool bool // toggle only
}

// toolboxFormValues is what a toolboxFormEntry's own build func reads:
// the current text (including a choice field's own selected option) and
// toggle state of every field, keyed by toolboxFormField.name.
type toolboxFormValues struct {
	Text  map[string]string
	Flags map[string]bool
}

// toolboxFormEntry builds an entry whose command genuinely needs more
// than the single free-text argument toolboxArgEntry offers — curl's own
// Method/Header/Follow redirects/Output, say. Opens toolboxForm (see
// openToolboxForm) instead of toolboxInput: a real multi-field Form for
// text/choice fields, plus a separate toolboxFormToggles List for any
// boolean ones (never Form.AddCheckbox — see newSedForm's own doc
// comment for the established reason: Form re-applies one uniform
// FocusedBackground to every item it owns, so a checkbox added through
// Form can never keep a look distinct from a real text field's).
//
// build turns the fields' current values into real argv, run on Run —
// see runToolboxForm, which renders the exact same slice both as the
// live preview line and as what actually executes, the same
// "one source of truth, command always shown before it runs" guarantee
// toolboxArgEntry/toolboxFixedEntry already give every other entry.
// build must itself skip any field whose value is empty/unset rather
// than emitting a blank token (e.g. an empty Header must never become
// "-H" followed by an empty string) — there is no generic way to enforce that here, since what
// "empty" means differs per flag (an optional value vs. a toggle that's
// simply off).
func toolboxFormEntry(label, help, name string, fields []toolboxFormField, build func(toolboxFormValues) []string) toolboxEntry {
	return toolboxEntry{
		label: label,
		help:  help,
		run: func(r *Root) {
			r.openToolboxForm(name, fields, build)
		},
	}
}

// toolboxCurlField* are curl's own toolboxFormField names — stable keys
// toolboxCurlEntry's own build func reads back by name (see
// toolboxFormField's own doc comment on why never by position).
const (
	toolboxCurlFieldURL    = "url"
	toolboxCurlFieldMethod = "method"
	toolboxCurlFieldHeader = "header"
	toolboxCurlFieldFollow = "follow"
	toolboxCurlFieldOutput = "output"
)

// toolboxCurlEntry is curl's own catalog entry — the first real consumer
// of toolboxFormEntry, replacing the single free-text argument it used
// before (just "curl <whatever the user typed>") with named fields: URL
// (required), Method (a choice; GET is curl's own default, so it's the
// only one that emits no "-X" at all — every other choice does), Header
// (optional text, "-H <value>" only if non-empty), Follow redirects (a
// toggle, "-L" only if checked), Output (optional text, "-o <value>" only
// if non-empty, otherwise curl's own default of printing the response to
// stdout). Deliberately only these — not auth, timeouts, or proxies —
// this sub-step's own explicit scope.
func toolboxCurlEntry() toolboxEntry {
	fields := []toolboxFormField{
		{name: toolboxCurlFieldURL, label: "URL", kind: toolboxFieldText,
			placeholder: "https://example.com"},
		{name: toolboxCurlFieldMethod, label: "Method", kind: toolboxFieldChoice,
			choices: []string{"GET", "POST", "PUT", "DELETE", "HEAD"}, defaultText: "GET"},
		{name: toolboxCurlFieldHeader, label: "Header", kind: toolboxFieldText,
			placeholder: `optional, e.g. "Content-Type: application/json"`},
		{name: toolboxCurlFieldFollow, label: "Follow redirects (-L)", kind: toolboxFieldToggle},
		{name: toolboxCurlFieldOutput, label: "Output", kind: toolboxFieldText,
			placeholder: "optional file path, default is stdout"},
	}
	build := func(v toolboxFormValues) []string {
		var args []string
		if method := v.Text[toolboxCurlFieldMethod]; method != "" && method != "GET" {
			args = append(args, "-X", method)
		}
		if header := v.Text[toolboxCurlFieldHeader]; header != "" {
			args = append(args, "-H", header)
		}
		if v.Flags[toolboxCurlFieldFollow] {
			args = append(args, "-L")
		}
		if output := v.Text[toolboxCurlFieldOutput]; output != "" {
			args = append(args, "-o", output)
		}
		// URL last, unlabeled, so it's always right before where curl's
		// own real URL argument would go in a hand-typed command line too
		// — omitted entirely while empty rather than appending a blank
		// trailing token, the same "no empty args" rule every other field
		// here already follows.
		if url := v.Text[toolboxCurlFieldURL]; url != "" {
			args = append(args, url)
		}
		return args
	}
	return toolboxFormEntry("curl", "Fetch a URL and show the response", "curl", fields, build)
}

// toolboxNmapField* are nmap's own toolboxFormField names — stable keys
// toolboxNmapEntry's own build func reads back by name (see
// toolboxFormField's own doc comment on why never by position).
const (
	toolboxNmapFieldTarget = "target"
	toolboxNmapFieldScan   = "scan"
	toolboxNmapFieldPorts  = "ports"
	toolboxNmapFieldTiming = "timing"
)

// toolboxNmapScanChoices/toolboxNmapTimingChoices are nmap's own Scan
// type/Timing template choices, "Default" meaning "no flag at all" in
// both — nmap's own default behavior (a plain SYN or Connect scan,
// whichever the running user's privileges allow; Normal timing) applies
// exactly as if this form didn't exist, rather than this entry silently
// pinning one of nmap's own defaults as if it were a deliberate choice.
var (
	toolboxNmapScanChoices   = []string{"Default", "SYN scan (-sS)", "Connect scan (-sT)", "UDP scan (-sU)", "Version detection (-sV)", "Ping scan, no port scan (-sn)"}
	toolboxNmapTimingChoices = []string{"Default", "Paranoid (-T0)", "Sneaky (-T1)", "Polite (-T2)", "Normal (-T3)", "Aggressive (-T4)", "Insane (-T5)"}
)

// toolboxNmapFlagFor pulls the real flag (e.g. "-sS", "-T4") out of a
// toolboxNmapScanChoices/toolboxNmapTimingChoices label — the label's
// own "(-flag)" suffix, parsed rather than kept as a second, separately
// maintained list that could drift out of sync with the choices shown.
// "Default" (or anything without the suffix) returns "", meaning no
// flag at all — see toolboxNmapScanChoices' own doc comment.
func toolboxNmapFlagFor(choice string) string {
	open := strings.LastIndex(choice, "(")
	shut := strings.LastIndex(choice, ")")
	if open < 0 || shut < open {
		return ""
	}
	return choice[open+1 : shut]
}

// toolboxNmapEntry is nmap's own catalog entry — Phase 3 sub-step (c):
// Target (required), Scan type and Timing template (both choices among
// nmap's own real flags, see toolboxNmapScanChoices/
// toolboxNmapTimingChoices), and Ports (optional text, "-p <value>"
// only if non-empty). Deliberately only these — not every nmap flag —
// the same curl precedent's own explicit scope (toolboxCurlEntry's own
// doc comment).
func toolboxNmapEntry() toolboxEntry {
	fields := []toolboxFormField{
		{name: toolboxNmapFieldTarget, label: "Target", kind: toolboxFieldText,
			placeholder: "a host, range, or CIDR"},
		{name: toolboxNmapFieldScan, label: "Scan type", kind: toolboxFieldChoice,
			choices: toolboxNmapScanChoices, defaultText: "Default"},
		{name: toolboxNmapFieldPorts, label: "Ports", kind: toolboxFieldText,
			placeholder: `optional, e.g. "22,80,443" or "1-1024"`},
		{name: toolboxNmapFieldTiming, label: "Timing template", kind: toolboxFieldChoice,
			choices: toolboxNmapTimingChoices, defaultText: "Default"},
	}
	build := func(v toolboxFormValues) []string {
		var args []string
		if flag := toolboxNmapFlagFor(v.Text[toolboxNmapFieldScan]); flag != "" {
			args = append(args, flag)
		}
		if ports := v.Text[toolboxNmapFieldPorts]; ports != "" {
			args = append(args, "-p", ports)
		}
		if flag := toolboxNmapFlagFor(v.Text[toolboxNmapFieldTiming]); flag != "" {
			args = append(args, flag)
		}
		// Target last, unlabeled — the same "required field last,
		// omitted entirely while empty rather than a blank trailing
		// token" rule toolboxCurlEntry's own URL field already follows.
		if target := v.Text[toolboxNmapFieldTarget]; target != "" {
			args = append(args, target)
		}
		return args
	}
	return toolboxFormEntry("Nmap scan", "Scan a host, range, or CIDR for open ports", "nmap", fields, build)
}

// toolboxDigField* are dig's own toolboxFormField names — stable keys
// toolboxDigEntry's own build func reads back by name (see
// toolboxFormField's own doc comment on why never by position).
const (
	toolboxDigFieldDomain = "domain"
	toolboxDigFieldType   = "type"
	toolboxDigFieldServer = "server"
)

// toolboxDigRecordTypeChoices is dig's own Record type choices —
// "Default" meaning "no type argument at all", i.e. dig's own default
// (an A record), the same "Default means no flag, nothing pinned" rule
// toolboxNmapEntry's own Scan type/Timing template already establish.
var toolboxDigRecordTypeChoices = []string{"Default", "A", "AAAA", "MX", "TXT", "NS", "CNAME", "SOA", "ANY"}

// toolboxDigEntry is dig's own catalog entry — Phase 3 sub-step (d):
// Domain (required), Record type (a choice among dig's own real record
// types, see toolboxDigRecordTypeChoices), and Server (optional text,
// prefixed with "@" and placed first, dig's own "@server name type"
// order). Deliberately only these — the same curl/nmap precedent's own
// explicit scope.
func toolboxDigEntry() toolboxEntry {
	fields := []toolboxFormField{
		{name: toolboxDigFieldDomain, label: "Domain", kind: toolboxFieldText,
			placeholder: "example.com"},
		{name: toolboxDigFieldType, label: "Record type", kind: toolboxFieldChoice,
			choices: toolboxDigRecordTypeChoices, defaultText: "Default"},
		{name: toolboxDigFieldServer, label: "Server", kind: toolboxFieldText,
			placeholder: "optional, e.g. 8.8.8.8 or 1.1.1.1"},
	}
	build := func(v toolboxFormValues) []string {
		var args []string
		// @server first, dig's own real "@server name type" order —
		// omitted entirely while empty, the same "no blank/pinned
		// tokens" rule every other field here already follows.
		if server := v.Text[toolboxDigFieldServer]; server != "" {
			args = append(args, "@"+server)
		}
		if domain := v.Text[toolboxDigFieldDomain]; domain != "" {
			args = append(args, domain)
		}
		if recordType := v.Text[toolboxDigFieldType]; recordType != "" && recordType != "Default" {
			args = append(args, recordType)
		}
		return args
	}
	return toolboxFormEntry("dig", "Resolve a hostname with full DNS record detail", "dig", fields, build)
}

// toolboxWgetField* are wget's own toolboxFormField names — stable keys
// toolboxWgetEntry's own build func reads back by name (see
// toolboxFormField's own doc comment on why never by position).
const (
	toolboxWgetFieldURL    = "url"
	toolboxWgetFieldOutput = "output"
	toolboxWgetFieldResume = "resume"
)

// toolboxWgetEntry is wget's own catalog entry — Phase 3 sub-step (e):
// URL (required), Output path (optional text, "-O <value>" only if
// non-empty, otherwise wget's own default of saving under the URL's own
// filename in the current directory), Resume (a toggle, "-c" only if
// checked). Deliberately only these — the same curl/nmap/dig
// precedent's own explicit scope.
func toolboxWgetEntry() toolboxEntry {
	fields := []toolboxFormField{
		{name: toolboxWgetFieldURL, label: "URL", kind: toolboxFieldText,
			placeholder: "https://example.com/file.tar.gz"},
		{name: toolboxWgetFieldOutput, label: "Output path", kind: toolboxFieldText,
			placeholder: "optional, default is the URL's own filename"},
		{name: toolboxWgetFieldResume, label: "Resume a partial download (-c)", kind: toolboxFieldToggle},
	}
	build := func(v toolboxFormValues) []string {
		var args []string
		if v.Flags[toolboxWgetFieldResume] {
			args = append(args, "-c")
		}
		if output := v.Text[toolboxWgetFieldOutput]; output != "" {
			args = append(args, "-O", output)
		}
		// URL last, unlabeled — the same "required field last, omitted
		// entirely while empty" rule toolboxCurlEntry's own URL field
		// already follows.
		if url := v.Text[toolboxWgetFieldURL]; url != "" {
			args = append(args, url)
		}
		return args
	}
	return toolboxFormEntry("wget", "Download a URL into the current directory", "wget", fields, build)
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
			toolboxNmapEntry(),
			toolboxFixedEntry("IP addresses (ip)", "Show every network interface and its addresses", "ip", "addr"),
			toolboxFixedEntry("Routing table (route)", "Show the kernel's own routing table", "route", "-n"),
			toolboxFixedEntry("Sockets (ss)", "List open TCP/UDP sockets and the process on each", "ss", "-tulpn"),
			toolboxArgEntry("getent", `Query a system database (e.g. "hosts example.com")`, `getent (database key, e.g. "hosts example.com"):`, "", "getent"),
			toolboxWgetEntry(),
			toolboxArgEntry("nslookup", "Resolve a hostname the old-school way", "nslookup host:", "", "nslookup"),
			toolboxDigEntry(),
			toolboxArgEntry("netcat (nc)", `Test a TCP/UDP connection, or listen on a port`, `nc arguments (e.g. "-zv host 22"):`, "", "nc"),
			toolboxCurlEntry(),
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
// list: one header row per category (skipped entirely when categories
// holds just the one — "jn"/"jh" already name it in the title bar
// itself, so repeating "Networking"/"Hardware" right below it as a
// second, redundant heading added nothing), a blank spacer between two
// categories (never before the first one), then each category's own
// entries. The screen currently open (see r.toolboxRows) decides which
// categories that is.
func toolboxDisplayRowsFor(categories []toolboxCategory) []toolboxDisplayRow {
	var rows []toolboxDisplayRow
	for i, cat := range categories {
		if i > 0 {
			rows = append(rows, toolboxDisplayRow{blank: true})
		}
		if len(categories) > 1 {
			rows = append(rows, toolboxDisplayRow{header: cat.name})
		}
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
// buildListHint) — a function, not a var, per activityLogHintEntries'
// own doc comment.
func toolboxHintEntries() []listHintEntry {
	return []listHintEntry{
		{keys: []listHintKey{
			{"↑", simulateKeyOnFocused(tcell.KeyUp)},
			{"↓", simulateKeyOnFocused(tcell.KeyDown)},
		}, label: "move"},
		hintKey("Enter", "run", func(r *Root) {
			row, _ := r.toolboxTable.GetSelection()
			r.activateToolboxRow(row)
		}),
		hintKey("Esc", "close", func(r *Root) { r.closeToolbox() }),
	}
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
	// Close only, no reload — Toolbox is a static catalog of commands
	// (see openToolboxScreen's own doc comment); it never goes stale
	// while open the way Mounts/Firewall's own live host state can, so
	// there's nothing for a reload button to do.
	r.toolboxTitleBar.SetMouseCapture(captureCloseTitleBarMouse(r.toolboxTitleBar, r.closeToolbox))

	r.toolboxHint = tview.NewTextView()
	r.toolboxHint.SetWrap(false)
	r.toolboxHint.SetDynamicColors(true)
	toolboxHintText, toolboxHintSpans := buildListHint(r.theme, toolboxHintEntries())
	r.toolboxHint.SetText(toolboxHintText)
	r.toolboxHintSpans = toolboxHintSpans
	r.toolboxHint.SetMouseCapture(r.captureListHintMouse(r.toolboxHint, &r.toolboxHintSpans))

	r.toolboxScrollbar = newVerticalScrollbarForTable(r, r.toolboxTable)
	toolboxTableRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.toolboxTable, 0, 1, true).
		AddItem(r.toolboxScrollbar, 1, 0, false)
	toolboxTableRow.SetMouseCapture(r.toolboxScrollbar.captureMouse)

	r.toolboxLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.toolboxTitleBar, 1, 0, false).
		AddItem(toolboxTableRow, 0, 1, true).
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
	renderCloseTitleBar(r.toolboxTitleBar, " "+title+" ", r.lastScreenWidth)
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

// newToolboxFormScreen builds toolboxForm and friends once, at startup —
// the same build-once/repopulate-on-open split newToolboxScreen itself
// already establishes for the plain catalog screen. toolboxFormLayout
// itself is left empty here; renderToolboxForm rebuilds its rows fresh
// on every openToolboxForm call, since how many rows it needs (whether
// this entry has any toggle fields at all, and how many text/choice
// fields) differs per entry.
func (r *Root) newToolboxFormScreen() {
	r.toolboxForm = tview.NewForm()
	r.toolboxForm.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			r.hideOverlay()
			return nil
		}
		return event
	})

	// A real tview.List, not Form.AddCheckbox — see toolboxFormField's
	// own doc comment for why: Form re-applies one uniform
	// FocusedBackground to every item it owns, so a checkbox added
	// through it could never keep a look distinct from a real text
	// field's (the same established reason newSedForm's own doc comment
	// gives for sedFlagsList).
	r.toolboxFormToggles = tview.NewList().ShowSecondaryText(false)
	r.toolboxFormToggles.SetHighlightFullLine(true)
	r.toolboxFormToggles.SetDoneFunc(r.hideOverlay) // Escape

	r.toolboxFormPreview = tview.NewTextView()
	r.toolboxFormPreview.SetWrap(false)

	// Real Cancel/Run buttons in a Flex row, not a List — the same
	// established look newConnectButtons/newCompressButtons/
	// newDuplicateButtons all already give their own dialogs' action
	// rows (see newConnectButtons' own doc comment).
	r.toolboxFormCancelBtn = tview.NewButton("Cancel").SetSelectedFunc(r.hideOverlay)
	r.toolboxFormRunBtn = tview.NewButton("Run").SetSelectedFunc(r.runToolboxForm)
	r.toolboxFormCancelBtn.SetInputCapture(spaceAlsoActivates(r.hideOverlay))
	r.toolboxFormRunBtn.SetInputCapture(spaceAlsoActivates(r.runToolboxForm))
	r.toolboxFormButtons = tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.toolboxFormCancelBtn, 0, 1, false).
		AddItem(r.toolboxFormRunBtn, 0, 1, false)

	r.toolboxFormTitleBar = newPlainTitleBar("Toolbox")
	r.toolboxFormLayout = tview.NewFlex().SetDirection(tview.FlexRow)
}

// toolboxFormRowCounts splits fields into how many text/choice form rows
// and how many toggle rows an open needs — shared between
// renderToolboxForm (actual Flex row sizes) and openToolboxForm (the
// overlay's own total rect height), so the two can never drift apart
// from each other the way two separately hand-counted literals could.
func toolboxFormRowCounts(fields []toolboxFormField) (textOrChoice, toggles int) {
	for _, f := range fields {
		if f.kind == toolboxFieldToggle {
			toggles++
		} else {
			textOrChoice++
		}
	}
	return textOrChoice, toggles
}

// openToolboxForm is toolboxFormEntry's own run: rebuilds toolboxForm/
// toolboxFormToggles for fields (see renderToolboxForm), remembers name
// and build for runToolboxForm/updateToolboxFormPreview, and floats the
// whole thing over whatever's already open — pushOverlay, not
// showOverlay: the identical reasoning toolboxPromptForArg's own doc
// comment already gives for why closing the Toolbox screen underneath
// (showOverlay's own closeAllOverlays) would be wrong here too.
func (r *Root) openToolboxForm(name string, fields []toolboxFormField, build func(toolboxFormValues) []string) {
	r.toolboxFormName = name
	r.toolboxFormFields = fields
	r.toolboxFormBuild = build
	r.renderToolboxForm()

	// form rows: 2N+1 for N text/choice fields (N rows + (N-1) rows of
	// itemPadding between them + 2 rows of the Form's own top/bottom
	// border padding) — the same derivation newCompressLayout's own doc
	// comment spells out for its own, smaller form. Toggle rows: one
	// each, no padding — a plain List, the same as sedFlagsList's own 5
	// rows for 5 flags. Plus titlebar/preview/buttons, one row each.
	textOrChoice, toggles := toolboxFormRowCounts(fields)
	height := 1 + (2*textOrChoice + 1) + toggles + 1 + 1
	width := 70
	_, _, screenWidth, screenHeight := r.GetRect()
	if width > screenWidth-4 {
		width = screenWidth - 4
	}
	if height > screenHeight-4 {
		height = screenHeight - 4
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	r.toolboxFormLayout.SetRect(x, y, width, height)
	r.pushOverlay(toolboxFormPage, r.toolboxFormLayout, nil)
}

// toolboxFormToggleItemText renders one toolboxFormToggles row — the
// same outline/filled circle sedFlagItemText already uses for Sed
// Replace's own flags, so a boolean toggle looks the same everywhere in
// this app regardless of which widget happens to implement it.
func toolboxFormToggleItemText(label string, checked bool) string {
	return fmt.Sprintf("%s  %s", checkboxText(checked), label)
}

// renderToolboxForm (re)builds toolboxForm's fields and
// toolboxFormToggles' rows fresh for r.toolboxFormFields, and rewires
// the whole Tab/Backtab chain across all four widgets (form, toggles,
// Cancel, Run) — called by openToolboxForm on every single open, the
// same "variable field set, nothing to keep from a previous open" shape
// resetSedForm already establishes for Sed Replace.
func (r *Root) renderToolboxForm() {
	r.toolboxForm.Clear(true)
	r.toolboxFormToggles.Clear()
	r.toolboxFormWidgets = make(map[string]tview.Primitive, len(r.toolboxFormFields))
	r.toolboxFormToggleState = make(map[string]bool)

	renderCloseTitleBar(r.toolboxFormTitleBar, " "+r.toolboxFormName+" ", r.lastScreenWidth)

	var toggleFields []toolboxFormField
	var lastField tview.Primitive
	for _, field := range r.toolboxFormFields {
		field := field // capture for the closures below
		switch field.kind {
		case toolboxFieldToggle:
			toggleFields = append(toggleFields, field)
			continue
		case toolboxFieldChoice:
			dd := tview.NewDropDown().SetLabel(field.label).SetOptions(field.choices, func(string, int) {
				r.updateToolboxFormPreview()
			})
			index := 0
			for i, c := range field.choices {
				if c == field.defaultText {
					index = i
				}
			}
			dd.SetCurrentOption(index)
			styleDropDown(dd, r.theme)
			r.toolboxForm.AddFormItem(dd)
			r.toolboxFormWidgets[field.name] = dd
			lastField = dd
		default: // toolboxFieldText
			input := tview.NewInputField().SetLabel(field.label)
			if field.placeholder != "" {
				input.SetPlaceholder(field.placeholder)
			}
			if field.defaultText != "" {
				input.SetText(field.defaultText)
			}
			input.SetChangedFunc(func(string) { r.updateToolboxFormPreview() })
			r.toolboxForm.AddFormItem(input)
			r.toolboxFormWidgets[field.name] = input
			lastField = input
		}
	}

	hasToggles := len(toggleFields) > 0
	for _, field := range toggleFields {
		field := field
		r.toolboxFormToggleState[field.name] = field.defaultBool
		r.toolboxFormToggles.AddItem(toolboxFormToggleItemText(field.label, field.defaultBool), "", 0, func() {
			r.toolboxFormToggleState[field.name] = !r.toolboxFormToggleState[field.name]
			r.renderToolboxFormToggleRow(field.name, field.label)
			r.updateToolboxFormPreview()
		})
	}

	// Tab on the form's own last field would otherwise just wrap back to
	// its own first field — see newConnectForm's own doc comment for the
	// identical, established gap this works around: a SetInputCapture on
	// the last field, not a SetDoneFunc, since Form.Focus unconditionally
	// overwrites the latter on every field whenever the form gains focus.
	// Goes to toolboxFormToggles if this entry has any boolean fields,
	// otherwise straight to Cancel.
	nextAfterForm := tview.Primitive(r.toolboxFormCancelBtn)
	if hasToggles {
		nextAfterForm = r.toolboxFormToggles
	}
	captureTabTo := func(target tview.Primitive) func(event *tcell.EventKey) *tcell.EventKey {
		return func(event *tcell.EventKey) *tcell.EventKey {
			if event.Key() == tcell.KeyTab {
				r.app.SetFocus(target)
				return nil
			}
			return event
		}
	}
	switch w := lastField.(type) {
	case *tview.InputField:
		w.SetInputCapture(captureTabTo(nextAfterForm))
	case *tview.DropDown:
		w.SetInputCapture(captureTabTo(nextAfterForm))
	}

	// Tab/Backtab continue the chain the form's own last field starts
	// above and toolboxFormCancelBtn's own SetExitFunc picks back up
	// below — a List has no SetExitFunc the way Button does, so this is
	// the only place to intercept them (the identical shape
	// sedFlagsList's own doc comment already establishes).
	r.toolboxFormToggles.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			r.app.SetFocus(r.toolboxFormCancelBtn)
			return nil
		case tcell.KeyBacktab:
			if lastField != nil {
				r.app.SetFocus(lastField)
			}
			return nil
		}
		return event
	})

	r.toolboxFormCancelBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.toolboxFormRunBtn)
		case tcell.KeyBacktab:
			if hasToggles {
				r.app.SetFocus(r.toolboxFormToggles)
			} else if lastField != nil {
				r.app.SetFocus(lastField)
			}
		case tcell.KeyEscape:
			r.hideOverlay()
		}
	})
	// Tab wraps back to the form's own first field through
	// toolboxForm.SetFocus(0) then r.app.SetFocus(r.toolboxForm), not a
	// direct named-field jump — the identical, real, previously-fixed
	// bug connectConnectBtn's own doc comment (connectdialog.go) found
	// first: a direct jump leaves the Form's own internal focusedElement
	// counter stale, so the next Backtab from that field lands wrong.
	r.toolboxFormRunBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.toolboxForm.SetFocus(0)
			r.app.SetFocus(r.toolboxForm)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.toolboxFormCancelBtn)
		case tcell.KeyEscape:
			r.hideOverlay()
		}
	})

	textOrChoice, toggleCount := toolboxFormRowCounts(r.toolboxFormFields)
	r.toolboxFormLayout.Clear()
	r.toolboxFormLayout.AddItem(r.toolboxFormTitleBar, 1, 0, false)
	r.toolboxFormLayout.AddItem(r.toolboxForm, 2*textOrChoice+1, 0, true)
	if hasToggles {
		r.toolboxFormLayout.AddItem(r.toolboxFormToggles, toggleCount, 0, false)
	}
	r.toolboxFormLayout.AddItem(r.toolboxFormPreview, 1, 0, false)
	r.toolboxFormLayout.AddItem(r.toolboxFormButtons, 1, 0, false)

	r.updateToolboxFormPreview()
}

// renderToolboxFormToggleRow repaints one toolboxFormToggles row after
// toggleSedFlag's own identical shape (toggleSedFlag, sedreplace.go) —
// re-renders just the one row that changed rather than the whole list.
func (r *Root) renderToolboxFormToggleRow(name, label string) {
	for i, field := range r.toolboxFormFields {
		if field.kind == toolboxFieldToggle && field.name == name {
			toggleIndex := 0
			for _, f := range r.toolboxFormFields[:i] {
				if f.kind == toolboxFieldToggle {
					toggleIndex++
				}
			}
			r.toolboxFormToggles.SetItemText(toggleIndex, toolboxFormToggleItemText(label, r.toolboxFormToggleState[name]), "")
			return
		}
	}
}

// buildToolboxFormValues reads toolboxForm/toolboxFormToggles' current
// state back into a toolboxFormValues, keyed by each field's own stable
// name (see toolboxFormField's own doc comment on why never by
// position) — what r.toolboxFormBuild actually turns into real argv.
func (r *Root) buildToolboxFormValues() toolboxFormValues {
	values := toolboxFormValues{
		Text:  make(map[string]string, len(r.toolboxFormWidgets)),
		Flags: make(map[string]bool, len(r.toolboxFormToggleState)),
	}
	for name, widget := range r.toolboxFormWidgets {
		switch w := widget.(type) {
		case *tview.InputField:
			values.Text[name] = strings.TrimSpace(w.GetText())
		case *tview.DropDown:
			_, text := w.GetCurrentOption()
			values.Text[name] = text
		}
	}
	for name, checked := range r.toolboxFormToggleState {
		values.Flags[name] = checked
	}
	return values
}

// updateToolboxFormPreview repaints toolboxFormPreview with exactly the
// argv r.toolboxFormBuild currently produces — the same slice
// runToolboxForm goes on to actually execute, so the command shown here
// is never anything other than the real one about to run (see
// toolboxFormEntry's own doc comment).
func (r *Root) updateToolboxFormPreview() {
	args := r.toolboxFormBuild(r.buildToolboxFormValues())
	line := r.toolboxFormName
	if len(args) > 0 {
		line = r.toolboxFormName + " " + strings.Join(args, " ")
	}
	r.toolboxFormPreview.SetText(line)
}

// runToolboxForm is toolboxFormButtons' own "Run": builds the exact same
// argv updateToolboxFormPreview just showed and hands it to
// openToolCommand, the same real-process, never-reimplemented path
// every other Toolbox entry already uses.
func (r *Root) runToolboxForm() {
	args := r.toolboxFormBuild(r.buildToolboxFormValues())
	title := r.toolboxFormName
	if len(args) > 0 {
		title = r.toolboxFormName + " " + strings.Join(args, " ")
	}
	r.hideOverlay()
	r.openToolCommand(title, r.toolboxFormName, args)
}

// applyToolboxTheme themes the Toolbox screen — split out of applyTheme
// (see its own doc comment). Guarded the same way applyOptionsTheme is.
func (r *Root) applyToolboxTheme(theme config.ResolvedTheme) {
	if r.toolboxTable == nil {
		return
	}
	// PanelBackground, not SurfaceBackground — see mountsLayout's own
	// identical comment (mounts.go) for the full reasoning.
	r.toolboxLayout.SetBackgroundColor(theme.PanelBackground)
	r.toolboxTable.SetBackgroundColor(theme.PanelBackground)
	// Without this, tview's own default selected-cell style just
	// reverses each cell's own foreground into a background instead of
	// using one consistent selection bar — the same fix already applied
	// to sshKeysTable/connectionMenuTable/sedPreviewTable/tabSwitcher/
	// gitBreach's tables, for the identical reason.
	r.toolboxTable.SetSelectedStyle(tcell.StyleDefault.
		Background(theme.SelectionBackground).
		Foreground(theme.TextColor))

	// FocusedBackground, fixed — the Toolbox screen has exactly one
	// focusable widget (its own table), never itself the base a
	// further overlay stacks on top of in a way that should dim it,
	// the same reasoning optionsTitleBar's own fixed
	// FocusedBackground already follows.
	r.toolboxTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.toolboxTitleBar.SetTextColor(theme.TextColor)
	// SurfaceBackground/TextColor, matching the main screen's own
	// buttonBar/statusBar — see mountsHint's own identical comment.
	r.toolboxHint.SetBackgroundColor(theme.SurfaceBackground)
	r.toolboxHint.SetTextColor(theme.TextColor)
	toolboxHintText, toolboxHintSpans := buildListHint(theme, toolboxHintEntries())
	r.toolboxHint.SetText(toolboxHintText)
	r.toolboxHintSpans = toolboxHintSpans

	styleInput(r.toolboxInput, theme, true)
	r.toolboxInput.SetLabelColor(theme.TextColor)

	// toolboxForm and friends (see toolboxFormField's own doc comment in
	// this file) — every new widget that overlay introduced needs its
	// own explicit line here, the same rule sshShellForm's own block
	// above spells out: nothing gets themed automatically just by
	// existing.
	if r.toolboxForm != nil {
		r.toolboxForm.SetBackgroundColor(theme.SurfaceBackground)
		r.toolboxForm.SetLabelColor(theme.TextColor)
		r.toolboxForm.SetFieldBackgroundColor(theme.InputFocusedBackground)
		r.toolboxForm.SetFieldTextColor(theme.TextColor)
		styleList(r.toolboxFormToggles, theme)
		styleButton(r.toolboxFormCancelBtn, theme)
		styleButton(r.toolboxFormRunBtn, theme)
		r.toolboxFormPreview.SetBackgroundColor(theme.SurfaceBackground)
		r.toolboxFormPreview.SetTextColor(theme.TextColor)
		r.toolboxFormTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
		r.toolboxFormTitleBar.SetTextColor(theme.TextColor)
	}

	r.renderToolbox() // cell colors are baked in per cell, not looked up live at draw time
}
