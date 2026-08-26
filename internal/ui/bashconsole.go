package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// bashConsoleInputRows is how many rows bashLine itself keeps once the
// console is expanded (see expandBashConsole) — one, per the user's own
// explicit request (the original 3 left too little of the expanded
// region for bashHistoryView's own share). A multi-line script in
// progress still scrolls within that one row's own width via TextArea's
// own default cursor handling; it just doesn't get a taller box to show
// more than the current line at once.
const bashConsoleInputRows = 1

// newBashConsole builds bashLine (a multi-line shell command/script
// editor — no "$ " label, full width, per the user's own explicit
// request) and bashHistoryView (its scrollable output transcript),
// wrapped together in bashConsole, a small nested Flex that
// expandBashConsole/collapseBashConsole resize as one unit. Called from
// newBottomBar.
func (r *Root) newBashConsole() {
	r.bashHistoryView = tview.NewTextView()
	r.bashHistoryView.SetWrap(true)
	r.bashHistoryView.SetScrollable(true)
	// Dynamic colors deliberately left off: bashHistoryView's own text
	// is arbitrary captured command output (see runShellCommandCaptured),
	// not this app's own — a literal "[" in it (regexp output, JSON,
	// anything) would otherwise risk being misread as a style tag the
	// same way an unescaped filename would (see nameHighlightTags's own
	// doc comment on that class of bug). Leaving tag parsing off entirely
	// sidesteps it rather than escaping every byte that flows through.
	r.bashHistoryView.SetChangedFunc(func() { r.app.Draw() })

	r.bashLine = tview.NewTextArea()
	r.bashLine.SetInputCapture(r.captureBashLineKey)
	r.bashLine.SetFinishedFunc(func(tcell.Key) {
		// Escape/Backtab (the only two keys TextArea itself calls this
		// for — see its own SetFinishedFunc doc comment): hand focus
		// back to the panel. collapseBashConsole runs from bashLine's
		// own SetBlurFunc below, not repeated here.
		r.app.SetFocus(r.panel.table)
	})
	r.bashLine.SetFocusFunc(r.expandBashConsole)
	r.bashLine.SetBlurFunc(r.collapseBashConsole)

	// Inherit whatever real command history already exists (see
	// historyFilePath/loadBashHistory), the same way a real shell starts
	// a new session with Ctrl+P already recalling what earlier sessions
	// ran — not just what's typed into this one.
	r.bashHistoryFile = historyFilePath()
	r.bashHistory = loadBashHistory(r.bashHistoryFile)
	r.bashHistoryIdx = len(r.bashHistory)

	r.bashConsole = tview.NewFlex().SetDirection(tview.FlexRow)
	r.bashConsole.AddItem(r.bashHistoryView, 0, 0, false) // hidden (0,0) until expanded
	r.bashConsole.AddItem(r.bashLine, 1, 0, true)
}

// expandBashConsole grows bashConsole upward — full width, already
// true, since it's stacked directly in mainLayout's own FlexRow rather
// than nested in a FlexColumn — up to just under half the terminal's
// current height, per the user's own explicit request: "als eine art
// bash overlay das nach oben wegscrollt". Wired as bashLine's own
// FocusFunc, so this runs the moment it's clicked into or otherwise
// gains focus. bashHistoryView's own scrollback (whatever was already
// captured — see runShellCommandCaptured) survives a collapse/expand
// cycle unchanged: it's resized, never torn down or cleared.
func (r *Root) expandBashConsole() {
	_, _, _, screenHeight := r.GetRect() // Root fills the whole screen
	target := screenHeight / 2
	if target < bashConsoleInputRows+1 {
		target = bashConsoleInputRows + 1 // always leave bashHistoryView at least one row
	}

	r.mainLayout.ResizeItem(r.bashConsole, target, 0)
	r.bashConsole.ResizeItem(r.bashHistoryView, 0, 1) // fills whatever's left of target
	r.bashConsole.ResizeItem(r.bashLine, bashConsoleInputRows, 0)
	r.bashHistoryView.ScrollToEnd()
}

// collapseBashConsole is expandBashConsole's counterpart, wired as
// bashLine's own BlurFunc: back to a single row, bashHistoryView
// hidden. Runs every time focus leaves bashLine, including while a
// captured command is still streaming output in the background (see
// runShellCommandCaptured) — that keeps writing to bashHistoryView
// regardless of whether it's currently visible; clicking back into
// bashLine (re-expanding) shows whatever accumulated meanwhile,
// scrolled to the end.
func (r *Root) collapseBashConsole() {
	r.mainLayout.ResizeItem(r.bashConsole, 1, 0)
	r.bashConsole.ResizeItem(r.bashHistoryView, 0, 0)
	r.bashConsole.ResizeItem(r.bashLine, 1, 0)
}

// captureBashLineKey handles everything bashLine's own default TextArea
// behavior doesn't already cover correctly for a command line:
//
//   - Enter (no modifier): run the buffer now (see runBashCommand) —
//     not TextArea's own default of inserting a newline, which Alt+Enter
//     is left to do instead (see below), so this line keeps feeling like
//     a real shell for the common case of a single command.
//   - Alt+Enter: falls through to TextArea's own default handling
//     (returned unchanged) — inserts a literal newline, for composing a
//     multi-line script across several lines before running it.
//   - Ctrl+P / Ctrl+N: recall the previous/next history entry (see
//     bashHistoryUp/Down) — not Up/Down, which TextArea already uses to
//     move the cursor between lines, needed now that the buffer can
//     genuinely span more than one.
//   - PageUp / PageDown: scroll bashHistoryView (see scrollBashHistory)
//     instead of TextArea's own default of moving the cursor a page at a
//     time within the buffer — reading back through what's already run
//     matters more here than paging through a multi-line script that
//     rarely runs more than a screen's worth of lines anyway.
func (r *Root) captureBashLineKey(event *tcell.EventKey) *tcell.EventKey {
	switch {
	case event.Key() == tcell.KeyEnter && event.Modifiers()&tcell.ModAlt != 0:
		return event
	case event.Key() == tcell.KeyEnter:
		r.runBashCommand(r.bashLine.GetText())
		return nil
	case event.Key() == tcell.KeyCtrlP:
		r.bashHistoryUp()
		return nil
	case event.Key() == tcell.KeyCtrlN:
		r.bashHistoryDown()
		return nil
	case event.Key() == tcell.KeyPgUp:
		r.scrollBashHistory(-1)
		return nil
	case event.Key() == tcell.KeyPgDn:
		r.scrollBashHistory(1)
		return nil
	}
	return event
}

// scrollBashHistory moves bashHistoryView by one page (its own current
// height, at least 1) in dir's direction (-1 up, +1 down) — the same
// "one screenful" page size a real pager uses, without needing to
// (and without wanting to — see captureBashLineKey) move keyboard focus
// away from bashLine to do it. A negative starting offset (tview.
// TextView's own "never scrolled yet" sentinel, -1 — see its own
// lineOffset field) is treated as 0 first, or the very first PageDown
// would land one row short.
func (r *Root) scrollBashHistory(dir int) {
	_, _, _, height := r.bashHistoryView.GetInnerRect()
	if height < 1 {
		height = 1
	}
	row, col := r.bashHistoryView.GetScrollOffset()
	if row < 0 {
		row = 0
	}
	row += dir * height
	if row < 0 {
		row = 0
	}
	r.bashHistoryView.ScrollTo(row, col)
}

// bashHistoryUp recalls the previous (older) history entry, the same as
// pressing Ctrl+P in a real shell's readline: the first press remembers
// whatever was on the line so far (bashHistoryDraft), so pressing
// Ctrl+N enough times afterwards gets back to it rather than an empty
// line. Stops at the oldest entry rather than wrapping.
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
	r.bashLine.SetText(r.bashHistory[r.bashHistoryIdx], true)
}

// bashHistoryDown is bashHistoryUp's counterpart: recalls the next
// (newer) entry, or restores whatever was being typed before Ctrl+P was
// first pressed (bashHistoryDraft) once it moves past the newest one —
// a no-op if history navigation isn't in progress at all.
func (r *Root) bashHistoryDown() {
	if r.bashHistoryIdx >= len(r.bashHistory) {
		return
	}
	r.bashHistoryIdx++
	if r.bashHistoryIdx == len(r.bashHistory) {
		r.bashLine.SetText(r.bashHistoryDraft, true)
	} else {
		r.bashLine.SetText(r.bashHistory[r.bashHistoryIdx], true)
	}
}

// userShell returns $SHELL, or "/bin/sh" if it isn't set — used by
// runShellCommandFullScreen/runShellCommandCaptured and runEditor (see
// editorCommand's own doc comment for why the editor is run through the
// shell too) as the interpreter for whatever the user typed or
// configured.
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
// inherits it too, the same way runBashCommand's caller inherited
// whatever was already there (see loadBashHistory). Best-effort: called
// via "_ = appendBashHistory(...)" in runBashCommand — a failure here
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

// interactivePrograms is the set of command names that need a real
// terminal to work at all — they open /dev/tty directly, or otherwise
// assume full control of the screen (cursor positioning, raw mode, an
// alternate screen buffer), which capturing their output into a plain
// buffer would just show as garbled escape codes rather than the actual
// program (see needsRealTerminal/runShellCommandFullScreen).
//
// Not exhaustive — deliberately erring toward including a program here
// rather than not: capturing something that actually needs a real
// terminal produces a broken, confusing result, while sending a
// perfectly capturable command (needlessly) to the full-screen handoff
// only costs the same brief screen flip every command already used to
// cost before this. Well-behaved non-interactive tools (git log/diff,
// ls --color, ...) usually already detect a non-terminal stdout on
// their own and adjust (e.g. git disables its own pager, ls disables
// color) — this list only needs to cover what doesn't.
var interactivePrograms = map[string]bool{
	"vim": true, "vi": true, "nvim": true, "view": true,
	"nano": true, "pico": true, "emacs": true, "joe": true, "jed": true,
	"less": true, "more": true, "most": true, "man": true,
	"top": true, "htop": true, "btop": true, "atop": true, "iotop": true,
	"watch": true, "tmux": true, "screen": true,
	"ssh": true, "mosh": true, "telnet": true, "ftp": true, "sftp": true,
	"mysql": true, "psql": true, "sqlite3": true, "redis-cli": true,
	"python": true, "python2": true, "python3": true, "irb": true, "node": true,
	"gdb": true, "lldb": true,
	"sudo": true, "su": true, "passwd": true,
	"visudo": true, "crontab": true,
	// TUI file managers — breakthrough's own closest relatives, per
	// CLAUDE.md's own "inspired by Midnight Commander" framing. Missing
	// "mc" itself here was an embarrassing gap, per the user's own
	// direct report: it silently ran captured instead of full-screen.
	"mc": true, "ranger": true, "nnn": true, "lf": true, "vifm": true,
	// Other common full-screen terminal tools this app's own POSIX/
	// sysadmin-leaning audience (see CLAUDE.md) is likely to reach for
	// from here.
	"tig": true, "ncdu": true, "w3m": true, "lynx": true, "links": true,
	"alsamixer": true, "nmtui": true, "weechat": true, "irssi": true,
	"mutt": true, "neomutt": true,
}

// needsRealTerminal reports whether command contains any word matching
// interactivePrograms — checked against every whitespace/shell-
// metacharacter-separated token, not just the first, so "cat file |
// less" and "sudo vim /etc/hosts" are both caught, not just a bare
// "less"/"sudo" typed on its own.
func needsRealTerminal(command string) bool {
	tokens := strings.FieldsFunc(command, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || strings.ContainsRune("|;&()<>", r)
	})
	for _, tok := range tokens {
		if interactivePrograms[tok] {
			return true
		}
	}
	return false
}

// runBashCommand is bashLine's own Enter action (see captureBashLineKey):
// records command in history, special-cases "cd" the same way Midnight
// Commander's own command line does (see parseCdCommand/changeDirectory
// — a "cd" run in a child shell/process only ever changes that child's
// own working directory, with no way to affect the panel once it exits),
// and otherwise runs it either full-screen (see
// runShellCommandFullScreen) or captured into bashHistoryView (see
// runShellCommandCaptured), whichever needsRealTerminal says it needs.
//
// command is recorded in bashHistory before it runs, unconditionally
// (not only once it succeeds) — the same as a real shell, which
// remembers what you typed regardless of the exit code.
func (r *Root) runBashCommand(command string) {
	if strings.TrimSpace(command) == "" {
		return
	}

	r.bashHistory = append(r.bashHistory, command)
	r.bashHistoryIdx = len(r.bashHistory)
	r.bashHistoryDraft = ""
	_ = appendBashHistory(r.bashHistoryFile, command) // best-effort — see its own doc comment

	if target, ok := parseCdCommand(command); ok {
		r.bashLine.SetText("", true)
		r.showError(r.changeDirectory(target))
		return
	}

	if needsRealTerminal(command) {
		r.runShellCommandFullScreen(command)
		return
	}
	r.runShellCommandCaptured(command)
}

// runShellCommandFullScreen suspends the TUI (see
// tview.Application.Suspend) and runs command through userShell, with
// the real terminal handed over for the duration and the panel's
// current directory as its working directory — full interactivity,
// including interactive programs like vim or less, exactly like
// Midnight Commander's own command line (and exactly what every command
// did before this — see runBashCommand/needsRealTerminal for when this
// is still used instead of a captured run). The panel reloads once the
// command exits, in case it changed anything in the directory currently
// on screen.
func (r *Root) runShellCommandFullScreen(command string) {
	var runErr error
	r.app.Suspend(func() {
		cmd := exec.Command(userShell(), "-c", command)
		cmd.Dir = r.panel.path
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		runErr = cmd.Run()
	})

	r.bashLine.SetText("", true)
	if runErr != nil {
		r.showError(fmt.Errorf("%s: %w", command, runErr))
		return
	}
	r.showError(r.panel.load(r.panel.path))
}

// runShellCommandCaptured runs command with its stdout/stderr streamed
// directly into bashHistoryView (see TextView.Write's own doc comment:
// safe to call from another goroutine, which is exactly what this does
// — SetChangedFunc, wired in newBashConsole, redraws as output arrives)
// instead of handing over the real terminal — breakthrough stays fully
// in control of the screen throughout, per the user's own explicit
// request to see a command's own output "innerhalb dieses Fensters",
// scrollable, rather than a brief full-screen flash. Asynchronous: the
// command runs in its own goroutine so the UI stays responsive (and so
// a non-terminating command, e.g. "tail -f", doesn't hang the whole
// application — see interruptBashCommand for how to stop one early).
//
// bashRunningCmd tracks the in-flight process for that interrupt, and
// also guards against a second captured command starting while one is
// already running: silently ignored here rather than started, so a
// second Enter while one is in flight (e.g. runBashCommand invoked
// directly, as some tests do, or just typing ahead) can't overwrite
// bashRunningCmd out from under the first command's own still-pending
// finishCapturedCommand. Deliberately NOT enforced via
// bashLine.SetDisabled: TextArea.SetDisabled unconditionally re-fires
// its own FinishedFunc (see tview's own SetDisabled — "if t.finished !=
// nil { t.finished(-1) }"), which newBashConsole wires to hand focus
// back to the panel — meaning disabling bashLine here would itself
// trigger collapseBashConsole (via bashLine's own BlurFunc) the instant
// a captured command starts, closing the console the user just opened
// to watch it run. bashLine stays fully interactive throughout; this
// guard alone is what actually matters for correctness.
func (r *Root) runShellCommandCaptured(command string) {
	if r.bashRunningCmd != nil {
		return
	}

	_, _ = fmt.Fprintf(r.bashHistoryView, "$ %s\n", command) // TextView.Write on an in-memory buffer: never meaningfully fails
	r.bashHistoryView.ScrollToEnd()

	cmd := exec.Command(userShell(), "-c", command)
	cmd.Dir = r.panel.path
	cmd.Stdout = r.bashHistoryView
	cmd.Stderr = r.bashHistoryView
	if err := cmd.Start(); err != nil {
		_, _ = fmt.Fprintf(r.bashHistoryView, "%s: %v\n", command, err) // see the "$ " echo just above on why this is never checked
		r.bashLine.SetText("", true)
		return
	}

	r.bashRunningCmd = cmd
	r.bashLine.SetText("", true)

	go func() {
		err := cmd.Wait()
		r.app.QueueUpdateDraw(func() {
			r.finishCapturedCommand(command, err)
		})
	}()
}

// finishCapturedCommand is runShellCommandCaptured's own completion
// handler — split out on its own specifically so it's callable directly
// (see bashconsole_test.go), without needing a real Application event
// loop behind QueueUpdateDraw to reach it. Clears bashRunningCmd
// (interruptBashCommand's own guard against a stale reference, and
// runShellCommandCaptured's own guard against a second command starting
// too early).
//
// cd's own effect, if the captured command included one (e.g. "cd
// /foo && ls" — parseCdCommand only recognizes a *bare* cd, see
// runBashCommand, so a compound command like this really did run in
// its own child shell, same as before), can't be reflected in the
// panel for the same "cd only changes the child's own directory"
// reason runBashCommand's own doc comment gives — reloading here would
// be a silent no-op for that case anyway, so this deliberately doesn't
// try, unlike runShellCommandFullScreen, which only ever reaches this
// kind of cleanup for its own (real, TTY-requiring) commands.
func (r *Root) finishCapturedCommand(command string, runErr error) {
	r.bashRunningCmd = nil
	if runErr != nil {
		_, _ = fmt.Fprintf(r.bashHistoryView, "[%s exited: %v]\n", command, runErr) // see runShellCommandCaptured's own "$ " echo on why this is never checked
	}
}

// interruptBashCommand sends SIGINT to the currently running captured
// command (see runShellCommandCaptured/bashRunningCmd), the same signal
// a real shell's own Ctrl+C sends its foreground job — called from
// RequestCancel, ahead of its usual "back out of whatever's open"
// behavior. Reports true if a command was actually running (and so was
// interrupted), false otherwise — RequestCancel falls through to its
// normal behavior in that case.
func (r *Root) interruptBashCommand() bool {
	if r.bashRunningCmd == nil || r.bashRunningCmd.Process == nil {
		return false
	}
	_ = r.bashRunningCmd.Process.Signal(syscall.SIGINT) // best-effort — a process that already exited is not an error worth reporting
	return true
}

// parseCdCommand reports whether command is exactly a "cd" invocation —
// "cd", "cd <path>", or "cd -" — and if so, whatever followed it ("" for
// a bare "cd"). Deliberately narrow: a compound command like
// "cd /foo && ls" or "cd /foo; ls" is left alone and runs as usual,
// rather than this trying to parse general shell syntax to decide how
// much of it is "the cd part" — recognizing plain, standalone "cd" is
// what Midnight Commander's own command line does too, and covers what
// "cd" is actually used for from a line like this.
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
