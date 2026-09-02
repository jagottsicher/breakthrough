package ui

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// buttonBarAction identifies one clickable region in the button bar (see
// buttonBarSpan/buildButtonBar) — the bottom bar's own equivalent of
// headerSpan/propertySpan, the same hand-rolled span-tracking pattern
// used everywhere else in this codebase for a line with several
// distinct click targets, rather than tview's own region/Highlight
// mechanism.
type buttonBarAction int

const (
	buttonActionProperties buttonBarAction = iota
	buttonActionEdit
	buttonActionLook
	buttonActionRename
	buttonActionToggleHidden
	buttonActionOptions
	buttonActionSearch
	buttonActionHelp
	buttonActionTrash
	buttonActionTrashbin
	buttonActionRestore
	buttonActionRemove
	buttonActionSed
	buttonActionDetails
)

// buttonBarSpan is one clickable region within the button bar's text —
// the same half-open [start,end) column-range idea as headerSpan, for a
// single-line display so no row is needed.
type buttonBarSpan struct {
	startCol, endCol int
	action           buttonBarAction
}

// newBottomBar builds the three rows below the panel: bashConsole (see
// newBashConsole, in bashconsole.go — bashLine, a multi-line shell
// command/script editor, plus bashHistoryView, its scrollable output
// transcript), buttonBar, a hand-built single line of quick-action
// buttons (see buildButtonBar), and statusBar, a purely informational
// line (see refreshStatusBar/buildStatusBar). NewRoot adds all three to
// mainLayout beneath the panel.
//
// buttonBar's initial text is built here (see buildButtonBar), but —
// unlike when this was first written — it's no longer fixed for the
// run of the program: refreshButtonBar rebuilds it on the same
// Panel.onLoad wiring statusBar already uses below, since which
// buttons even appear now depends on the panel's current directory
// (see buildButtonBar's own doc comment). statusBar starts blank;
// NewRoot's caller is expected to call refreshStatusBar once real data
// (the panel's own directory) is available, the same as it always has.
func (r *Root) newBottomBar() {
	r.newBashConsole()

	r.buttonBar = tview.NewTextView()
	r.buttonBar.SetDynamicColors(true)
	r.buttonBar.SetMouseCapture(r.captureButtonBarMouse)
	text, spans := r.buildButtonBar()
	r.buttonBarSpans = spans
	r.buttonBar.SetText(text)

	r.statusBar = tview.NewTextView()
	r.statusBar.SetDynamicColors(true)
	// Deliberately no SetMouseCapture: statusBar is purely informational
	// now, nothing in it is clickable — see buttonBar above for that.

	r.currentUser = currentUsername()
}

// currentUsername resolves the running process's own username, the same
// way fsops.Stat's Owner field would report it for a file this user
// owns — falls back to $USER if user.Current() itself fails for some
// reason (e.g. no matching /etc/passwd entry), rather than showing
// nothing at all.
func currentUsername() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

// refreshStatusBar rebuilds and redraws the (purely informational, no
// buttons — see buildButtonBar for those) status bar's text — called
// whenever anything it shows might have changed: the panel navigating
// (disk usage depends on the current directory — see Panel.onLoad,
// wired in NewRoot), and once a second from StartClock's ticker (the
// clock itself).
func (r *Root) refreshStatusBar() {
	r.statusBar.SetText(r.buildStatusBar())
}

// refreshButtonBar rebuilds and redraws the button bar's text — called
// from the same Panel.onLoad wiring refreshStatusBar uses (see NewRoot),
// since buildButtonBar's own output now depends on the panel's current
// directory (in/out of the trash — see inTrash) as well as its
// showHidden flag, both of which a navigation, toggleHidden's own
// reload, or a trash operation's reloadPanel can change.
func (r *Root) refreshButtonBar() {
	text, spans := r.buildButtonBar()
	r.buttonBarSpans = spans
	r.buttonBar.SetText(text)
}

// buildButtonBar renders the button bar's text: the quick-action
// buttons in nano's own "^X Label" style (instantly recognizable as
// "Ctrl+X does this" without needing a separate legend) — Help, Rename,
// Edit, Look, Properties, Find, Sed, toggle hidden files, Options,
// Trash, Trashbin/Restore, Remove, in that fixed order.
//
// Two of these aren't fixed labels any more (see refreshButtonBar for
// when this gets called again): the hidden-files toggle reads "Hide" or
// "Unhide" depending on r.panel.showHidden — the same "label names the
// action clicking it performs next" convention hiddenToggleLabel
// already uses for the context menu's own equivalent, just with this
// button's own shorter Hide/Unhide vocabulary instead of that item's
// fuller "Show/Hide hidden files" text. And the Trashbin slot itself
// swaps to Restore while r.inTrash() — browsing the trash and asking to
// "go to trash" again does nothing useful, but Restore does; see
// moveSelectionToTrash for Trash's own equivalent swap, which
// disappears from this bar entirely in the same state rather than
// swapping to anything, since there's nothing sensible to move an
// already-trashed item to.
func (r *Root) buildButtonBar() (text string, spans []buttonBarSpan) {
	type buttonSpec struct {
		label  string
		action buttonBarAction
	}

	hideUnhideLabel := "^G Unhide"
	if r.panel.showHidden {
		hideUnhideLabel = "^G Hide"
	}

	trashbinLabel, trashbinAction := "^B Trashbin", buttonActionTrashbin
	inTrash := r.inTrash()
	if inTrash {
		trashbinLabel, trashbinAction = "^B Restore", buttonActionRestore
	}

	buttons := []buttonSpec{
		{"F1 Help", buttonActionHelp},
		{"F2 Rename", buttonActionRename},
		{"^E Edit", buttonActionEdit},
		{"^L Look", buttonActionLook},
		{"^P Properties", buttonActionProperties},
		{"^D Details", buttonActionDetails},
		{"^F Find", buttonActionSearch},
		{"^S Sed", buttonActionSed},
		{hideUnhideLabel, buttonActionToggleHidden},
		{"^O Options", buttonActionOptions},
	}
	if !inTrash {
		buttons = append(buttons, buttonSpec{"^T Trash", buttonActionTrash})
	}
	buttons = append(buttons,
		buttonSpec{trashbinLabel, trashbinAction},
		buttonSpec{"^R Remove", buttonActionRemove},
	)

	// " │ " (U+2502, the same box-drawing vertical bar buildStatusBar's
	// own sep already uses one row below this) reads as a clearer
	// separator between buttons than a plain double space, and keeps the
	// two adjacent rows visually consistent with each other — a plain
	// ASCII "|" here, tried first, read as an inconsistency once both
	// were actually side by side. Per the user's own explicit request,
	// which also asked for a narrower bare "│" (no surrounding spaces)
	// once the row's own available width can't fit the wider version.
	// Not done: buildButtonBar only ever runs again on specific state
	// changes (toggling hidden files, entering/leaving the trash view —
	// see refreshButtonBar's own callers), never on a bare terminal
	// resize by itself, so a width check here would frequently judge
	// against whatever rect r.buttonBar happened to have the *last* time
	// some unrelated state change last rebuilt it — not the terminal's
	// current real size — and silently keep the wrong separator until
	// another such change happened to come along. Root.handleBeforeDraw
	// already solves exactly this for Properties/Details (see its own
	// doc comment); wiring buildButtonBar into it too, rather than
	// guessing a width here, is the real follow-up.
	const sep = " │ "

	var b strings.Builder
	col := 0

	// col advances by s's display width (tview.TaggedStringWidth), not a
	// plain rune count — a button label could in principle contain
	// double-width (e.g. CJK) characters some day, and a rune count
	// would misalign every buttonBarSpan after it (see buildHeaderSpans/
	// propertiesBuilder.text for the same fix elsewhere).
	write := func(s string) {
		b.WriteString(s)
		col += tview.TaggedStringWidth(s)
	}
	for i, bt := range buttons {
		if i > 0 {
			write(sep)
		}
		start := col
		write(bt.label)
		spans = append(spans, buttonBarSpan{startCol: start, endCol: col, action: bt.action})
	}

	return b.String(), spans
}

// buildStatusBar renders the status bar's text: the current user, disk
// and inode usage for the panel's current directory (see
// fsops.FetchDiskUsage), the running kernel release (see
// kernelVersionText), uptime and load average where available (see
// uptimeText/loadAverageText — Linux only, gracefully omitted
// elsewhere, the same "just show one less segment" degradation
// fsops.FetchDiskUsage itself already has), and the clock. No buttons
// here any more — see buildButtonBar.
func (r *Root) buildStatusBar() string {
	var b strings.Builder
	write := func(s string) { b.WriteString(s) }
	sep := func() { write(" │ ") }

	write(r.currentUser)
	sep()
	if u, ok := fsops.FetchDiskUsage(r.panel.path); ok {
		write(diskUsageText(u))
		sep()
		write(inodeUsageText(u))
		sep()
	}
	if k := kernelVersionText(); k != "" {
		write(k)
		sep()
	}
	if up, ok := uptimeText(); ok {
		write(up)
		sep()
	}
	if load, ok := loadAverageText(); ok {
		write(load)
		sep()
	}
	write(clockText())

	return b.String()
}

// kernelVersionText returns `uname -r`'s own output, trimmed — the same
// "shell out to a real system tool" approach fsops.FetchDiskUsage's own
// df already uses, rather than a syscall wrapper needing per-platform
// struct handling for what's ultimately just one string. Returns "" if
// uname isn't available (e.g. some minimal containers) — the status bar
// just shows one less segment then.
func kernelVersionText() string {
	out, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// uptimeText and loadAverageText read Linux's /proc/uptime and
// /proc/loadavg directly rather than shelling out to and parsing
// `uptime`'s own output, whose format differs enough between GNU/Linux
// and BSD/macOS (singular/plural "load average(s)", comma- vs.
// space-separated numbers, different time formats — verified by actually
// comparing sample output from both, not guessed) to make reliable
// cross-platform parsing more fragile than it's worth for a status line.
// Both simply return ok=false where /proc/... doesn't exist at all (e.g.
// macOS, most BSDs) — no build tag needed, os.ReadFile's own error
// already tells the two cases apart.
func uptimeText() (string, bool) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "", false
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return "", false
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return "", false
	}
	return "up " + formatUptime(time.Duration(seconds*float64(time.Second))), true
}

// formatUptime renders d as "NdHH:MM" once it's reached a full day, or
// just "HH:MM" before that — the same shape `uptime`'s own "N days,
// HH:MM" takes, just compact enough for a status line already showing
// several other segments.
func formatUptime(d time.Duration) string {
	totalMinutes := int(d.Minutes())
	days := totalMinutes / (24 * 60)
	hours := (totalMinutes / 60) % 24
	minutes := totalMinutes % 60
	if days > 0 {
		return fmt.Sprintf("%dd %02d:%02d", days, hours, minutes)
	}
	return fmt.Sprintf("%02d:%02d", hours, minutes)
}

func loadAverageText() (string, bool) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return "", false
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return "", false
	}
	return fmt.Sprintf("load %s %s %s", fields[0], fields[1], fields[2]), true
}

// diskUsageWarnColor is the color a usage percentage should stand out
// in — tcell.ColorRed at 90% or more, tcell.ColorOrange at 80% or
// more, tcell.ColorDefault (no warning, leave the surrounding text's
// own color alone) otherwise — the two thresholds the user asked for,
// shared by both the disk-space and the inode percentage.
func diskUsageWarnColor(percent int) tcell.Color {
	switch {
	case percent >= 90:
		return tcell.ColorRed
	case percent >= 80:
		return tcell.ColorOrange
	default:
		return tcell.ColorDefault
	}
}

// formatUsagePercent renders percent as "N%", wrapped in a foreground-
// only tview color tag (see colorTag — the same "#rrggbb", not a color
// name, so it round-trips exactly through tview's own tag parser) once
// diskUsageWarnColor says it should stand out — "[-]" resets just the
// foreground back to the status bar's own configured text color
// afterward, not a hardcoded one, so this still looks right under
// every color scheme (see Root.applyTheme).
func formatUsagePercent(percent int) string {
	color := diskUsageWarnColor(percent)
	if color == tcell.ColorDefault {
		return fmt.Sprintf("%d%%", percent)
	}
	return fmt.Sprintf("[%s]%d%%[-]", colorTag(color), percent)
}

// diskUsageText and inodeUsageText render one labeled "Label X used, Y
// free (Z%)" status-bar segment each — explicit "used"/"free" labels
// (not just two bare numbers) precisely because the user reported the
// previous, unlabeled df dump as unreadable ("man weiß gar nicht was
// die heißen sollen"), and explicit used *and* free numbers for
// inodes specifically, per the user's own request, rather than just a
// percentage.
func diskUsageText(u fsops.DiskUsage) string {
	return fmt.Sprintf("Disk %s used, %s free (%s)", humanSize(u.UsedBytes), humanSize(u.AvailBytes), formatUsagePercent(u.UsePercent))
}

func inodeUsageText(u fsops.DiskUsage) string {
	return fmt.Sprintf("Inodes %s used, %s free (%s)", humanCount(u.UsedInodes), humanCount(u.AvailInodes), formatUsagePercent(u.InodePercent))
}

// humanCount renders n the same way humanSize renders a byte count
// (1024-based grouping, one decimal above the smallest unit) but
// without humanSize's own "B" suffix — appropriate for a plain count
// (inodes) rather than a size in bytes, which "512B" would misleadingly
// suggest this was.
func humanCount(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", float64(n)/float64(div), "KMGTPE"[exp])
}

// clockText renders the current date, time, and the local timezone's
// abbreviation (e.g. "CEST", "UTC") — whatever the OS's own timezone
// database reports, via Go's "MST" format verb.
func clockText() string {
	return time.Now().Format("2006-01-02 15:04:05 MST")
}

// buttonBarActionAt returns the buttonBarAction at screen position
// (x, y), if any — the column lookup captureButtonBarMouse itself needs
// to actually run one, factored out so captureOutsideClick can also ask
// "what would a click here do" without running it (see its own
// buttonActionDetails carve-out, letting a Details click through while
// Properties is open instead of treating it as a click outside the
// overlay).
func (r *Root) buttonBarActionAt(x, y int) (buttonBarAction, bool) {
	if !r.buttonBar.InRect(x, y) {
		return 0, false
	}
	rectX, _, _, _ := r.buttonBar.GetInnerRect()
	col := x - rectX
	for _, s := range r.buttonBarSpans {
		if col >= s.startCol && col < s.endCol {
			return s.action, true
		}
	}
	return 0, false
}

// captureButtonBarMouse routes a click on one of the button bar's
// buttons (see buildButtonBar/buttonBarSpan) to its action. A click
// elsewhere on the row (the gaps between buttons, or empty space) just
// does nothing.
func (r *Root) captureButtonBarMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action != tview.MouseLeftClick || !r.buttonBar.InRect(event.Position()) {
		return action, event
	}

	x, y := event.Position()
	if bAction, ok := r.buttonBarActionAt(x, y); ok {
		r.runButtonBarAction(bAction)
	}
	return tview.MouseConsumed, nil
}

// runButtonBarAction is what a button click runs. Called directly,
// unguarded, unlike its keyboard-shortcut equivalent (see
// acceptsGlobalShortcut): a click is always a deliberate, explicit
// action on whatever it landed on, with none of the "is something else
// currently typing" ambiguity a global keystroke has to rule out first.
func (r *Root) runButtonBarAction(action buttonBarAction) {
	switch action {
	case buttonActionProperties:
		r.propertiesCurrentEntry()
	case buttonActionEdit:
		r.editCurrentEntry()
	case buttonActionLook:
		r.lookCurrentEntry()
	case buttonActionRename:
		r.renameCurrentEntry()
	case buttonActionToggleHidden:
		r.toggleHidden()
	case buttonActionOptions:
		r.openOptions()
	case buttonActionSearch:
		r.openSearch()
	case buttonActionHelp:
		r.openHelp()
	case buttonActionTrash:
		r.moveSelectionToTrash()
	case buttonActionTrashbin:
		r.openTrash()
	case buttonActionRestore:
		r.restoreSelectionFromTrash()
	case buttonActionRemove:
		r.openRemoveConfirm()
	case buttonActionSed:
		r.openSedReplace()
	case buttonActionDetails:
		r.toggleDetailsSidebar()
	}
}

// editCurrentEntry is the Edit button/Ctrl+E's actual action, also
// reused directly as the context menu's own "Edit" item (see NewRoot):
// a right-click already moves the table's cursor to the clicked row
// before the menu opens (see captureMouse's MouseRightClick case), so
// reading it here targets the same entry either way. Runs the
// configured editor (see editorCommand) on whichever entry the table's
// cursor is currently on. A no-op on the ".." row or an empty panel
// (Panel.CurrentRowPath's ok=false).
func (r *Root) editCurrentEntry() {
	_, path, ok := r.panel.CurrentRowPath()
	if !ok {
		return
	}
	r.runEditor(path, 0)
}

// renameCurrentEntry is the Rename button/F2's actual action — the
// keyboard/status-bar equivalent of the context menu's "Rename" (see
// Root.openRename), targeting whichever entry the table's cursor is
// currently on instead of a right-clicked one.
func (r *Root) renameCurrentEntry() {
	row, path, ok := r.panel.CurrentRowPath()
	if !ok {
		return
	}
	r.target = path
	r.targetRow = row
	r.openRename()
}

// renameRow is the click-pause-click rename gesture's own action (see
// Panel.onRenameGesture/handleNameClick) — renameCurrentEntry's own
// shape, for a row given directly rather than read from the panel's
// current cursor, since the gesture already knows exactly which row it
// fired on. Excludes ".." (rowRef.checkable is false for it) the same
// way CurrentRowPath already does for the keyboard path — not a real
// rename target either way.
func (r *Root) renameRow(row int) {
	ref, ok := r.panel.rowRef(row)
	if !ok || !ref.checkable {
		return
	}
	r.target = ref.path
	r.targetRow = row
	r.openRename()
}

// acceptsGlobalShortcut reports whether Ctrl+E/Ctrl+L/F2/Ctrl+G/
// Ctrl+O/Ctrl+F/Ctrl+R (see EditShortcut/LookShortcut/RenameShortcut/
// ToggleHiddenShortcut/OptionsShortcut/SearchShortcut/PurgeShortcut,
// wired up in cmd/breakthrough) should act right now: no overlay is
// open, and the bash command line doesn't have keyboard focus.
//
// Unlike RequestQuit/RequestCancel (Ctrl+Q/Ctrl+C), which are meant to
// work from literally anywhere, these seven operate on "the currently
// selected file", the hidden-files display, or open an overlay of their
// own — actions that only make sense while the panel itself is what's
// focused, or (Options, Search) that would otherwise layer confusingly
// on top of whatever's already open. Critically, this also keeps them
// out of the bash line's way: tview's TextArea already implements
// several readline-style keybindings of its own (Ctrl+A/Home,
// Ctrl+E/End, Ctrl+B/PgUp, Ctrl+F/PgDn) — since these seven are captured
// globally, at the Application level (see cmd/breakthrough), they'd
// reach and consume the keystroke before bashLine's own InputCapture or
// TextArea's own default handling ever saw it, silently defeating both
// that and the muscle memory this line is explicitly meant to feel like
// bash — hence checking this first and no-op'ing instead.
func (r *Root) acceptsGlobalShortcut() bool {
	return r.activePage == "" && !r.bashLine.HasFocus()
}

// AcceptsGlobalShortcut is acceptsGlobalShortcut, exported for
// cmd/breakthrough: Ctrl+P (see PropertiesShortcut), Ctrl+T/Entf (see
// TrashShortcut in trash.go), Ctrl+B (see TrashbinShortcut), and Ctrl+S
// (see SedReplaceShortcut) need to decide, before even calling their
// own Shortcut method, whether to consume the key at all — unlike the
// seven above, which always return
// nil regardless (an accepted, minor imperfection for keys TextArea
// might bind natively), each of these collides with a real, explicit
// feature of this same codebase: bashLine's own captureBashLineKey
// binds Ctrl+P to command-history recall, for instance. Consuming one
// of them unconditionally at the Application level would silently break
// that native behavior every time the bash line has focus, not just
// fail to fire the intended action — so cmd/breakthrough falls through
// to bashLine's own handling (returns the event, not nil) whenever this
// reports false, rather than swallowing it either way.
func (r *Root) AcceptsGlobalShortcut() bool {
	return r.acceptsGlobalShortcut()
}

// BashLineHasFocus is acceptsGlobalShortcut's own bashLine.HasFocus()
// half, exported on its own for cmd/breakthrough: Ctrl+K
// (ComputeHashesShortcut) and Ctrl+N (FetchMetadataShortcut) both bind
// keys tview's own TextArea gives a real, native meaning (delete-to-
// end-of-line, history-recall-adjacent movement — see the doc comment
// above), so like every AcceptsGlobalShortcut-gated shortcut, they must
// fall through instead of consuming the key while bashLine has focus.
// Unlike those, though, they deliberately do NOT also require
// activePage == "" — both need to keep firing while Properties
// specifically is open (that's exactly the case ComputeHashesShortcut
// itself has to tell apart — see its own doc comment), which
// AcceptsGlobalShortcut's coarser, combined check would otherwise block
// outright.
func (r *Root) BashLineHasFocus() bool {
	return r.bashLine.HasFocus()
}

// EditShortcut, RenameShortcut, ToggleHiddenShortcut, OptionsShortcut,
// and SearchShortcut are Ctrl+E, F2, Ctrl+G, Ctrl+O, and Ctrl+F's
// global actions (see cmd/breakthrough and acceptsGlobalShortcut for why
// they check first rather than acting unconditionally). LookShortcut
// (Ctrl+L) and PurgeShortcut (Ctrl+R, Remove) are the same shape,
// defined alongside the rest of Look/Trash in viewer.go/trash.go instead
// of here.
func (r *Root) EditShortcut() {
	if r.acceptsGlobalShortcut() {
		r.editCurrentEntry()
	}
}

func (r *Root) RenameShortcut() {
	if r.acceptsGlobalShortcut() {
		r.renameCurrentEntry()
	}
}

func (r *Root) ToggleHiddenShortcut() {
	if r.acceptsGlobalShortcut() {
		r.toggleHidden()
	}
}

func (r *Root) OptionsShortcut() {
	if r.acceptsGlobalShortcut() {
		r.openOptions()
	}
}

func (r *Root) SearchShortcut() {
	if r.acceptsGlobalShortcut() {
		r.openSearch()
	}
}

// editorCommand returns the editor to run for Edit — $VISUAL first (the
// POSIX convention for a screen-oriented editor, which is exactly this
// context: a full-screen program taking over the whole terminal), then
// $EDITOR, then Debian/Ubuntu's own select-editor(1) preference (see
// selectedEditor — a real system-level "which editor should command-line
// tools use" mechanism, unrelated to any particular tool, not this
// project's own convention), then "vi" as a last resort (POSIX-
// guaranteed to exist). This is select-editor(1)'s own documented
// precedence exactly: "Die Variable SELECTED_EDITOR wird durch die
// Umgebungsvariablen VISUAL und EDITOR außer Kraft gesetzt" (verified
// against the installed man page, not guessed).
func editorCommand() string {
	if v := os.Getenv("VISUAL"); v != "" {
		return v
	}
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	if s := selectedEditor(); s != "" {
		return s
	}
	return "vi"
}

// selectedEditor reads ~/.selected_editor — select-editor(1)'s own
// preference file (part of Debian/Ubuntu's sensible-utils package,
// installed independently of any particular editor or file manager;
// crontab -e and many other tools already honor it the same way this
// does) — and returns its SELECTED_EDITOR value, or "" if the file
// doesn't exist, can't be read, or doesn't contain a recognizable
// assignment. select-editor's own real, observed output (see its own
// source, and a live example file) is exactly:
//
//	# Generated by /usr/bin/select-editor
//	SELECTED_EDITOR="/usr/bin/vim.basic"
//
// This mechanism is Debian/Ubuntu-specific — simply absent on macOS and
// FreeBSD, which this app also targets — so a missing file is never an
// error, the same as an unset environment variable.
func selectedEditor() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".selected_editor"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "SELECTED_EDITOR=")
		if !ok {
			continue
		}
		return strings.Trim(rest, `"`)
	}
	return ""
}

// runEditor suspends the TUI (see runShellCommand's own doc comment for
// why) and runs editorCommand on path, at line if it's > 0 (see
// Panel.activateRow's own searchMode branch — a content-search match's
// own line number, 0 for every other caller, including
// editCurrentEntry). Run through the shell (via "$@", not a literal
// exec argument) rather than exec'd directly: $VISUAL/$EDITOR can
// legitimately be more than one word (e.g. "emacsclient -t"), and only
// the shell can be trusted to split that the way the user intended
// while still passing each of its own remaining arguments through
// exactly as given, spaces and all.
//
// A line is passed as a leading "+N" argument, vi/vim/nvim/nano/
// emacs' own shared convention for "open already positioned at line
// N" — the overwhelming majority of terminal $EDITOR values in this
// app's own POSIX-focused audience already understand it; there's no
// attempt at a per-editor
// lookup table for anything fancier (e.g. VS Code's own "-g file:N")
// — an editor that doesn't recognize "+N" is no worse off than not
// jumping to a line at all, just a leading argument it happens to
// ignore or, at worst, visibly complain about once, on-screen, exactly
// where the user would see and understand why.
//
// Skips its own usual post-edit reload if search results are currently
// showing (see Panel.searchMode): r.panel.path stays whatever real
// directory was current before the search that produced them ran (see
// Panel.showSearchResults' own doc comment), completely unrelated to
// path here, so reloading it would be both useless (refreshing a
// directory the file being edited isn't even in) and would silently
// discard the results themselves (Panel.load always exits search mode
// — see its own doc comment) the moment the editor closes — the
// opposite of the "stay in the results, jump straight back into the
// editor for the next match" flow this exists for. Editing a real row
// still refreshes the real directory afterward, unchanged.
func (r *Root) runEditor(path string, line int) {
	var runErr error
	r.app.Suspend(func() {
		script := editorCommand() + ` "$@"`
		args := []string{"-c", script, "sh"}
		if line > 0 {
			args = append(args, fmt.Sprintf("+%d", line))
		}
		args = append(args, path)
		cmd := exec.Command(userShell(), args...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		runErr = cmd.Run()
	})

	if runErr != nil {
		r.showError(fmt.Errorf("edit %s: %w", path, runErr))
		return
	}
	if r.panel.searchMode {
		return
	}
	r.showError(r.panel.load(r.panel.path))
}

// StartClock begins refreshing the status bar's clock display once a
// second, via a background goroutine and Application.QueueUpdateDraw.
// Deliberately not started automatically by NewRoot: many tests
// construct a Root directly without ever calling Application.Run, and
// QueueUpdateDraw blocks forever if nothing's actually running the event
// loop to drain it — cmd/breakthrough calls this itself, once, right
// before Run. Returns a function that stops the ticker.
func (r *Root) StartClock() (stop func()) {
	ticker := time.NewTicker(time.Second)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				r.app.QueueUpdateDraw(func() {
					r.refreshStatusBar()
				})
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()
	return func() { close(done) }
}
