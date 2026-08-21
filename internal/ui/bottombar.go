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
	statusActionRename
	statusActionToggleHidden
)

// statusBarSpan is one clickable region within the status bar's text —
// the same half-open [start,end) column-range idea as headerSpan, for a
// single-line display so no row is needed.
type statusBarSpan struct {
	startCol, endCol int
	action           statusBarAction
}

// newBottomBar builds the two rows below the panel: bashLine, a plain
// InputField for shell commands (see runShellCommand), and statusBar, a
// hand-built single line showing who/df/quick-action buttons/the clock
// (see refreshStatusBar). NewRoot adds both to mainLayout beneath the
// panel.
func (r *Root) newBottomBar() {
	r.bashLine = tview.NewInputField()
	r.bashLine.SetLabel("$ ")
	r.bashLine.SetFieldBackgroundColor(accentBackgroundColor)
	r.bashLine.SetBackgroundColor(accentBackgroundColor)
	r.bashLine.SetLabelColor(tcell.ColorWhite)
	r.bashLine.SetFieldTextColor(tcell.ColorWhite)
	r.bashLine.SetDoneFunc(func(key tcell.Key) {
		if key != tcell.KeyEnter {
			return
		}
		r.runShellCommand(r.bashLine.GetText())
	})
	r.bashLine.SetInputCapture(r.captureBashLineKey)

	// Inherit whatever real command history already exists (see
	// historyFilePath/loadBashHistory), the same way a real shell starts
	// a new session with Up already recalling what earlier sessions ran
	// — not just what's typed into this one.
	r.bashHistoryFile = historyFilePath()
	r.bashHistory = loadBashHistory(r.bashHistoryFile)
	r.bashHistoryIdx = len(r.bashHistory)

	r.statusBar = tview.NewTextView().SetTextColor(tcell.ColorWhite)
	r.statusBar.SetBackgroundColor(accentBackgroundColor)
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
// usage for the panel's current directory (see dfSummary), three quick-
// action buttons in nano's own "^X Label" style (instantly recognizable
// as "Ctrl+X does this" without needing a separate legend), and the
// clock.
func (r *Root) buildStatusBar() (text string, spans []statusBarSpan) {
	var b strings.Builder
	col := 0

	write := func(s string) {
		b.WriteString(s)
		col += len([]rune(s))
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
	write(dfSummary(r.panel.path))
	sep()
	button("^E Edit", statusActionEdit)
	write("  ")
	button("^R Rename", statusActionRename)
	write("  ")
	button("^G Hidden", statusActionToggleHidden)
	sep()
	write(clockText())

	return b.String(), spans
}

// dfSummary runs `df -h` on dir and returns its data line (skipping the
// header row), whitespace-collapsed to one line — deliberately not
// parsed into named fields: GNU (Linux) and BSD (macOS/FreeBSD) df don't
// agree on column layout, but both print exactly one header line plus
// one data line for a single given path, so showing that line as-is
// works the same way on every platform this project targets.
func dfSummary(dir string) string {
	out, err := exec.Command("df", "-h", dir).Output()
	if err != nil {
		return "df: unavailable"
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) < 2 {
		return "df: unavailable"
	}
	return strings.Join(strings.Fields(lines[len(lines)-1]), " ")
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
	case statusActionRename:
		r.renameCurrentEntry()
	case statusActionToggleHidden:
		r.toggleHidden()
	}
}

// editCurrentEntry is the Edit button/Ctrl+E's actual action: runs the
// configured editor (see editorCommand) on whichever entry the table's
// cursor is currently on. A no-op on the ".." row or an empty panel
// (Panel.CurrentRowPath's ok=false).
func (r *Root) editCurrentEntry() {
	_, path, ok := r.panel.CurrentRowPath()
	if !ok {
		return
	}
	r.runEditor(path)
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

// acceptsGlobalShortcut reports whether Ctrl+E/Ctrl+R/Ctrl+G (see
// EditShortcut/RenameShortcut/ToggleHiddenShortcut, wired up in
// cmd/breakthrough) should act right now: no overlay is open, and the
// bash command line doesn't have keyboard focus.
//
// Unlike RequestQuit/RequestCancel (Ctrl+Q/Ctrl+C), which are meant to
// work from literally anywhere, these three operate on "the currently
// selected file" or the hidden-files display — actions that only make
// sense while the panel itself is what's focused. Critically, this also
// keeps them out of the bash line's way: tview's plain InputField
// doesn't implement any readline-style keybindings of its own, but real
// bash/readline uses Ctrl+E for end-of-line and Ctrl+R for
// reverse-search — letting these global shortcuts fire while typing a
// command there would silently defeat muscle memory this line is
// explicitly meant to feel like bash.
func (r *Root) acceptsGlobalShortcut() bool {
	return r.activePage == "" && !r.bashLine.HasFocus()
}

// EditShortcut, RenameShortcut, and ToggleHiddenShortcut are Ctrl+E,
// Ctrl+R, and Ctrl+G's global actions (see cmd/breakthrough and
// acceptsGlobalShortcut for why they check first rather than acting
// unconditionally).
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

// userShell returns $SHELL, or "/bin/sh" if it isn't set — used by both
// runShellCommand and runEditor (see editorCommand's own doc comment for
// why the editor is run through the shell too) as the interpreter for
// whatever the user typed or configured.
func userShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/sh"
}

// historyFilePath returns where bash-style command history lives:
// $HISTFILE if set (an explicit override, honored regardless of what
// $SHELL actually is), otherwise "~/.bash_history" — bash's own
// hardcoded default. That default is used even if $SHELL isn't bash: a
// "bash Eingabezeile" inheriting history "wie in einer normalen bash
// Session" is specifically what was asked for, not whatever the current
// shell's own (possibly different, e.g. zsh's ~/.zsh_history) history
// file convention happens to be. Empty if the home directory can't be
// resolved.
func historyFilePath() string {
	if f := os.Getenv("HISTFILE"); f != "" {
		return f
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".bash_history")
}

// loadBashHistory reads path's existing command history, oldest first
// — one command per line, skipping bash's own optional "#<unix
// timestamp>" comment lines (written when HISTTIMEFORMAT is set) rather
// than mistaking them for commands. A missing or unreadable file isn't
// an error worth reporting: an empty history is exactly what a first
// run — or one where $HISTFILE genuinely doesn't exist yet — should
// start with.
func loadBashHistory(path string) []string {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var history []string
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || isHistoryTimestampComment(line) {
			continue
		}
		history = append(history, line)
	}
	return history
}

// isHistoryTimestampComment reports whether line is one of bash's own
// "#<unix timestamp>" history-file comment lines (see loadBashHistory).
func isHistoryTimestampComment(line string) bool {
	rest, ok := strings.CutPrefix(line, "#")
	if !ok {
		return false
	}
	_, err := strconv.ParseInt(rest, 10, 64)
	return err == nil
}

// appendBashHistory appends command to path as bash itself would — one
// line — so a later real bash session (or another breakthrough one)
// inherits it too, the same way runShellCommand's caller inherited
// whatever was already there (see loadBashHistory). Best-effort: called
// via "_ = appendBashHistory(...)" in runShellCommand — a failure here
// (a missing home directory, a permissions problem) shouldn't stop the
// command that was just run from having run, or get reported as if it
// were that command's own failure.
func appendBashHistory(path, command string) (err error) {
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		// Only overrides err if the write itself succeeded — a failure
		// there is the more relevant one to report.
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	_, err = fmt.Fprintln(f, command)
	return err
}

// runShellCommand is the bash line's Enter action: suspends the TUI (see
// tview.Application.Suspend) and runs command through userShell, with
// the real terminal handed over for the duration and the panel's
// current directory as its working directory — full interactivity,
// including interactive programs like vim or less, exactly like
// Midnight Commander's own command line. The panel reloads once the
// command exits, in case it changed anything in the directory currently
// on screen.
//
// "cd", "cd <path>", and "cd -" are special-cased instead (see
// parseCdCommand/Root.changeDirectory) — the same as Midnight
// Commander's own command line does it, and for the same reason: a "cd"
// run inside the subshell above only ever changes that child process's
// own working directory, which has no way to affect the panel once the
// child exits. No subshell, and no Suspend, is involved for these —
// updating which directory the panel shows is instant, the same as
// clicking a breadcrumb already is.
//
// command is recorded in bashHistory before it runs, unconditionally
// (not only once it succeeds) — the same as a real shell, which
// remembers what you typed regardless of the exit code.
func (r *Root) runShellCommand(command string) {
	if strings.TrimSpace(command) == "" {
		return
	}

	r.bashHistory = append(r.bashHistory, command)
	r.bashHistoryIdx = len(r.bashHistory)
	r.bashHistoryDraft = ""
	_ = appendBashHistory(r.bashHistoryFile, command) // best-effort — see its own doc comment

	if target, ok := parseCdCommand(command); ok {
		r.bashLine.SetText("")
		r.showError(r.changeDirectory(target))
		return
	}

	var runErr error
	r.app.Suspend(func() {
		cmd := exec.Command(userShell(), "-c", command)
		cmd.Dir = r.panel.path
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		runErr = cmd.Run()
	})

	r.bashLine.SetText("")
	if runErr != nil {
		r.showError(fmt.Errorf("%s: %w", command, runErr))
		return
	}
	r.showError(r.panel.load(r.panel.path))
}

// parseCdCommand reports whether command is exactly a "cd" invocation —
// "cd", "cd <path>", or "cd -" — and if so, whatever followed it ("" for
// a bare "cd"). Deliberately narrow: a compound command like
// "cd /foo && ls" or "cd /foo; ls" is left alone and runs in the
// subshell as usual, rather than this trying to parse general shell
// syntax to decide how much of it is "the cd part" — recognizing plain,
// standalone "cd" is what Midnight Commander's own command line does
// too, and covers what "cd" is actually used for from a line like this.
func parseCdCommand(command string) (target string, ok bool) {
	fields := strings.Fields(command)
	if len(fields) == 0 || fields[0] != "cd" || len(fields) > 2 {
		return "", false
	}
	if len(fields) == 1 {
		return "", true
	}
	return fields[1], true
}

// changeDirectory is what the bash line's own "cd" (see parseCdCommand)
// runs instead of spawning a subshell for it: target is whatever
// followed "cd" — "" for a bare "cd" (home, the same as a real shell),
// "-" for the panel's previous directory (see Panel.previousPath), or
// otherwise resolved exactly the way typing it into the path header's
// own edit field would be (Panel.resolvePath: "~" expansion, relative
// paths resolved against the panel's current directory).
func (r *Root) changeDirectory(target string) error {
	switch target {
	case "":
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		return r.panel.navigate(home)
	case "-":
		prev, ok := r.panel.previousPath()
		if !ok {
			return fmt.Errorf("cd: no previous directory")
		}
		return r.panel.navigate(prev)
	default:
		return r.panel.navigate(r.panel.resolvePath(target))
	}
}

// editorCommand returns the editor to run for Edit — $VISUAL first (the
// POSIX convention for a screen-oriented editor, which is exactly this
// context: a full-screen program taking over the whole terminal), then
// $EDITOR, then "vi" as a last resort (POSIX-guaranteed to exist) —
// matching Midnight Commander's own external-editor convention.
func editorCommand() string {
	if v := os.Getenv("VISUAL"); v != "" {
		return v
	}
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	return "vi"
}

// runEditor suspends the TUI (see runShellCommand's own doc comment for
// why) and runs editorCommand on path. Run through the shell (via "$1",
// not a literal exec argument) rather than exec'd directly: $VISUAL/
// $EDITOR can legitimately be more than one word (e.g. "emacsclient
// -t"), and only the shell can be trusted to split that the way the
// user intended while still passing path through exactly as typed,
// spaces and all.
func (r *Root) runEditor(path string) {
	var runErr error
	r.app.Suspend(func() {
		script := editorCommand() + ` "$1"`
		cmd := exec.Command(userShell(), "-c", script, "sh", path)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		runErr = cmd.Run()
	})

	if runErr != nil {
		r.showError(fmt.Errorf("edit %s: %w", path, runErr))
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

// captureBashLineKey adds history navigation (see bashHistoryUp/Down) to
// the bash line — the one thing tview's plain InputField doesn't already
// give it for free, unlike a real shell's own readline.
func (r *Root) captureBashLineKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyUp:
		r.bashHistoryUp()
		return nil
	case tcell.KeyDown:
		r.bashHistoryDown()
		return nil
	}
	return event
}

// bashHistoryUp recalls the previous (older) history entry, the same as
// pressing Up in a real shell: the first press remembers whatever was
// on the line so far (bashHistoryDraft), so pressing Down enough times
// afterwards gets back to it rather than an empty line. Stops at the
// oldest entry rather than wrapping.
func (r *Root) bashHistoryUp() {
	if len(r.bashHistory) == 0 {
		return
	}
	if r.bashHistoryIdx == len(r.bashHistory) {
		r.bashHistoryDraft = r.bashLine.GetText()
	}
	if r.bashHistoryIdx > 0 {
		r.bashHistoryIdx--
	}
	r.bashLine.SetText(r.bashHistory[r.bashHistoryIdx])
}

// bashHistoryDown is bashHistoryUp's counterpart: recalls the next
// (newer) entry, or restores whatever was being typed before Up was
// first pressed (bashHistoryDraft) once it moves past the newest one —
// a no-op if history navigation isn't in progress at all.
func (r *Root) bashHistoryDown() {
	if r.bashHistoryIdx >= len(r.bashHistory) {
		return
	}
	r.bashHistoryIdx++
	if r.bashHistoryIdx == len(r.bashHistory) {
		r.bashLine.SetText(r.bashHistoryDraft)
	} else {
		r.bashLine.SetText(r.bashHistory[r.bashHistoryIdx])
	}
}
