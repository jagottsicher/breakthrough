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
)

// statusBarAction identifies one clickable region in the status bar (see
// statusBarSpan/buildStatusBar) — the bottom bar's own equivalent of
// headerSpan/propertySpan, the same hand-rolled span-tracking pattern
// used everywhere else in this codebase for a line with several
// distinct click targets, rather than tview's own region/Highlight
// mechanism.
type statusBarAction int

const (
	statusActionEdit statusBarAction = iota
	statusActionLook
	statusActionRename
	statusActionToggleHidden
	statusActionOptions
	statusActionSearch
	statusActionHelp
	statusActionTrash
	statusActionRemove
)

// statusBarSpan is one clickable region within the status bar's text —
// the same half-open [start,end) column-range idea as headerSpan, for a
// single-line display so no row is needed.
type statusBarSpan struct {
	startCol, endCol int
	action           statusBarAction
}

// newBottomBar builds the two rows below the panel: bashConsole (see
// newBashConsole, in bashconsole.go — bashLine, a multi-line shell
// command/script editor, plus bashHistoryView, its scrollable output
// transcript), and statusBar, a hand-built single line showing who/df/
// quick-action buttons/the clock (see refreshStatusBar). NewRoot adds
// both to mainLayout beneath the panel.
func (r *Root) newBottomBar() {
	r.newBashConsole()

	r.statusBar = tview.NewTextView()
	r.statusBar.SetDynamicColors(true)
	r.statusBar.SetMouseCapture(r.captureStatusBarMouse)

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

// refreshStatusBar rebuilds and redraws the status bar's text — called
// whenever anything it shows might have changed: the panel navigating
// (df depends on the current directory — see Panel.onLoad, wired in
// NewRoot), and once a second from StartClock's ticker (the clock
// itself).
func (r *Root) refreshStatusBar() {
	text, spans := r.buildStatusBar()
	r.statusBarSpans = spans
	r.statusBar.SetText(text)
}

// buildStatusBar renders the status bar's text: the current user, disk
// and inode usage for the panel's current directory (see
// fetchDiskUsage), the quick-action buttons (Edit/Look/Rename/Hidden/
// Find/Options/Help/Trash/Remove) in nano's own "^X Label" style
// (instantly recognizable as "Ctrl+X does this" without needing a
// separate legend), and the clock.
func (r *Root) buildStatusBar() (text string, spans []statusBarSpan) {
	var b strings.Builder
	col := 0

	// col advances by s's display width (tview.TaggedStringWidth), not a
	// plain rune count — dfSummary's mount point or the current username
	// could in principle contain double-width (e.g. CJK) characters, and
	// a rune count would misalign every statusBarSpan after it (see
	// buildHeaderSpans/propertiesBuilder.text for the same fix elsewhere).
	write := func(s string) {
		b.WriteString(s)
		col += tview.TaggedStringWidth(s)
	}
	button := func(label string, action statusBarAction) {
		start := col
		write(label)
		spans = append(spans, statusBarSpan{startCol: start, endCol: col, action: action})
	}
	sep := func() {
		write(" │ ")
	}

	write(r.currentUser)
	sep()
	if u, ok := fetchDiskUsage(r.panel.path); ok {
		write(diskUsageText(u))
		sep()
		write(inodeUsageText(u))
	} else {
		write("disk usage unavailable")
	}
	sep()
	button("^E Edit", statusActionEdit)
	write("  ")
	button("^L Look", statusActionLook)
	write("  ")
	button("^R Rename", statusActionRename)
	write("  ")
	button("^G Hidden", statusActionToggleHidden)
	write("  ")
	button("^F Find", statusActionSearch)
	write("  ")
	button("^O Options", statusActionOptions)
	write("  ")
	button("F1 Help", statusActionHelp)
	write("  ")
	button("^T Trash", statusActionTrash)
	write("  ")
	button("^P Remove", statusActionRemove)
	sep()
	write(clockText())

	return b.String(), spans
}

// diskUsage is one filesystem's block and inode usage for the status
// bar's own display (see fetchDiskUsage/diskUsageText/inodeUsageText)
// — the labeled, color-coded replacement for what used to be dfSummary,
// an entirely unlabeled raw df line the user themselves reported as
// unreadable ("man weiß gar nicht was die heißen sollen").
type diskUsage struct {
	usedBytes, availBytes    int64
	usedInodes, availInodes  int64
	usePercent, inodePercent int
}

// fetchDiskUsage runs `df -k` (block usage) and `df -i` (inode usage)
// on dir and parses each one's own data line into a diskUsage.
//
// Deliberately not `df -h`: -h's own human-readable formatting is
// locale-dependent (this app's own README/CLAUDE.md target audience
// includes non-English locales — a German one, for instance, renders
// "1.7G" as "1,7G", a comma this app would then have no reliable way
// to tell apart from a field separator when parsing it back out). Also
// deliberately not `df -P`, which on GNU df guarantees a single,
// portably-parseable data line — but means something else entirely on
// BSD df (512-byte blocks, not "portable output format" — verified
// against the FreeBSD/macOS df(1) man pages, not guessed: using it
// cross-platform for parseability, the way a straight port of GNU df's
// own convention would, is actually wrong here). Requesting raw block/
// inode counts via -k/-i and formatting them with this app's own
// humanSize/humanCount instead sidesteps both problems, and gets
// exact usedBytes/availBytes/usedInodes/availInodes for free rather
// than needing to reverse a rounded, unit-suffixed string.
func fetchDiskUsage(dir string) (diskUsage, bool) {
	blockLine, ok := dfLastLine("df", "-k", dir)
	if !ok {
		return diskUsage{}, false
	}
	inodeLine, ok := dfLastLine("df", "-i", dir)
	if !ok {
		return diskUsage{}, false
	}

	usedBlocks, availBlocks, usePercent, ok := parseDfDataLine(blockLine)
	if !ok {
		return diskUsage{}, false
	}
	usedInodes, availInodes, inodePercent, ok := parseDfDataLine(inodeLine)
	if !ok {
		return diskUsage{}, false
	}

	return diskUsage{
		usedBytes:    usedBlocks * 1024,
		availBytes:   availBlocks * 1024,
		usedInodes:   usedInodes,
		availInodes:  availInodes,
		usePercent:   usePercent,
		inodePercent: inodePercent,
	}, true
}

// dfLastLine runs name(args...) (df, with whatever flags the caller
// chose) and returns its own data line — the last line of output,
// skipping the header row df always prints first. A single given path
// always produces exactly one data line, on every platform this
// project targets.
func dfLastLine(name string, args ...string) (string, bool) {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return "", false
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) < 2 {
		return "", false
	}
	return lines[len(lines)-1], true
}

// parseDfDataLine extracts Used, Available, and Capacity (Use%/IUse%)
// from one df data line, indexed from the END of its whitespace-
// separated fields — Mounted-on last, Capacity/Use% just before it,
// Available before that, Used before that — rather than from the
// start. That's what makes this robust to a wrapped Filesystem name (a
// real, if rare, BSD df quirk for a very long device name, splitting
// it onto its own line and shifting how many fields precede the data
// that actually matters here) without needing to detect the wrap
// itself. Not robust to a mount point that itself contains a space
// (e.g. "/Volumes/My Drive") — an accepted, rare limitation, the same
// class dfSummary's own predecessor already accepted before this.
func parseDfDataLine(line string) (used, avail int64, percent int, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return 0, 0, 0, false
	}
	percent, err := strconv.Atoi(strings.TrimSuffix(fields[len(fields)-2], "%"))
	if err != nil {
		return 0, 0, 0, false
	}
	avail, err = strconv.ParseInt(fields[len(fields)-3], 10, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	used, err = strconv.ParseInt(fields[len(fields)-4], 10, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	return used, avail, percent, true
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
func diskUsageText(u diskUsage) string {
	return fmt.Sprintf("Disk %s used, %s free (%s)", humanSize(u.usedBytes), humanSize(u.availBytes), formatUsagePercent(u.usePercent))
}

func inodeUsageText(u diskUsage) string {
	return fmt.Sprintf("Inodes %s used, %s free (%s)", humanCount(u.usedInodes), humanCount(u.availInodes), formatUsagePercent(u.inodePercent))
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

// captureStatusBarMouse routes a click on one of the status bar's three
// buttons (see buildStatusBar/statusBarSpan) to its action. A click
// elsewhere on the row (the user/df/clock text, or empty space) just
// does nothing — those aren't actionable, only informational.
func (r *Root) captureStatusBarMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action != tview.MouseLeftClick || !r.statusBar.InRect(event.Position()) {
		return action, event
	}

	x, _ := event.Position()
	rectX, _, _, _ := r.statusBar.GetInnerRect()
	col := x - rectX

	for _, s := range r.statusBarSpans {
		if col >= s.startCol && col < s.endCol {
			r.runStatusBarAction(s.action)
			break
		}
	}
	return tview.MouseConsumed, nil
}

// runStatusBarAction is what a button click runs. Called directly,
// unguarded, unlike its keyboard-shortcut equivalent (see
// acceptsGlobalShortcut): a click is always a deliberate, explicit
// action on whatever it landed on, with none of the "is something else
// currently typing" ambiguity a global keystroke has to rule out first.
func (r *Root) runStatusBarAction(action statusBarAction) {
	switch action {
	case statusActionEdit:
		r.editCurrentEntry()
	case statusActionLook:
		r.lookCurrentEntry()
	case statusActionRename:
		r.renameCurrentEntry()
	case statusActionToggleHidden:
		r.toggleHidden()
	case statusActionOptions:
		r.openOptions()
	case statusActionSearch:
		r.openSearch()
	case statusActionHelp:
		r.openHelp()
	case statusActionTrash:
		r.moveSelectionToTrash()
	case statusActionRemove:
		r.openRemoveConfirm()
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

// renameCurrentEntry is the Rename button/Ctrl+R's actual action — the
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

// acceptsGlobalShortcut reports whether Ctrl+E/Ctrl+L/Ctrl+R/Ctrl+G/
// Ctrl+O/Ctrl+F (see EditShortcut/LookShortcut/RenameShortcut/
// ToggleHiddenShortcut/OptionsShortcut/SearchShortcut, wired up in
// cmd/breakthrough) should act right now: no overlay is open, and the
// bash command line doesn't have keyboard focus.
//
// Unlike RequestQuit/RequestCancel (Ctrl+Q/Ctrl+C), which are meant to
// work from literally anywhere, these six operate on "the currently
// selected file", the hidden-files display, or open an overlay of their
// own — actions that only make sense while the panel itself is what's
// focused, or (Options, Search) that would otherwise layer confusingly
// on top of whatever's already open. Critically, this also keeps them
// out of the bash line's way: tview's TextArea already implements
// several readline-style keybindings of its own (Ctrl+A/Home,
// Ctrl+E/End, Ctrl+B/PgUp, Ctrl+F/PgDn) — since these six are captured
// globally, at the Application level (see cmd/breakthrough), they'd
// reach and consume the keystroke before bashLine's own InputCapture or
// TextArea's own default handling ever saw it, silently defeating both
// that and the muscle memory this line is explicitly meant to feel like
// bash — hence checking this first and no-op'ing instead.
func (r *Root) acceptsGlobalShortcut() bool {
	return r.activePage == "" && !r.bashLine.HasFocus()
}

// EditShortcut, RenameShortcut, ToggleHiddenShortcut, OptionsShortcut,
// and SearchShortcut are Ctrl+E, Ctrl+R, Ctrl+G, Ctrl+O, and Ctrl+F's
// global actions (see cmd/breakthrough and acceptsGlobalShortcut for why
// they check first rather than acting unconditionally). LookShortcut
// (Ctrl+L) is the same shape, defined alongside the rest of Look in
// viewer.go instead of here.
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
// app's own POSIX-focused audience (see CLAUDE.md's own target
// platforms) already understand it; there's no attempt at a per-editor
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
