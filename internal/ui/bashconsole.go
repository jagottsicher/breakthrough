package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"golang.org/x/term"
)

// bashHintText is bashHint's own fixed content — a single-row, always-
// the-same legend spelling out bashLine's keybindings, since none of
// them are otherwise discoverable from the collapsed line alone (per
// the user's own report: "man weiß nicht worum es geht").
const bashHintText = "Enter Run (full screen)   Alt+Enter Newline   ↑/↓ or ^P/^N History   Esc Close"

// newBashConsole builds bashLine, a multi-line shell command/script
// editor (no "$ " label, full width, per the user's own explicit
// request) that grows upward on focus (see expandBashConsole/
// collapseBashConsole) so a longer, multi-line script (composed via
// Alt+Enter — see captureBashLineKey) stays visible while it's being
// written, plus bashHint, a plain, read-only legend line shown above it
// while expanded (see bashHintText) — both wrapped together in
// bashConsole, a small nested Flex expandBashConsole/collapseBashConsole
// resize as one unit. Called from newBottomBar.
//
// Every command runs the same way: full-screen, via a real terminal
// (see runShellCommandFullScreen) — the same as Midnight Commander's
// own command line handles every command, and the same as this app did
// before an earlier attempt at also capturing non-interactive commands'
// output inline here. That attempt needed a list of which programs
// need a real terminal to work at all (vim, less, top, mc, ...) to
// decide when *not* to capture — no such list can ever be complete (the
// user's own report: "wir können nicht eine komplette Liste aller
// Programme haben"), and getting it wrong for a program that assumes
// real terminal control produces a broken, garbled result, not just a
// cosmetic one. Always going full-screen sidesteps the classification
// problem entirely, the same way MC's own design does.
func (r *Root) newBashConsole() {
	r.bashHint = tview.NewTextView()
	r.bashHint.SetText(bashHintText)

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
	r.bashConsole.AddItem(r.bashHint, 0, 0, false) // hidden (0,0) until expanded
	r.bashConsole.AddItem(r.bashLine, 1, 0, true)
}

// expandBashConsole grows bashConsole upward — full width, already
// true, since it's stacked directly in mainLayout's own FlexRow rather
// than nested in a FlexColumn — up to just under half the terminal's
// current height: one row for bashHint's own legend, the rest for
// bashLine, so a multi-line script being composed (see
// captureBashLineKey's own Alt+Enter case) stays visible as it grows,
// rather than scrolling out of a single row. Wired as bashLine's own
// FocusFunc, so this runs the moment it's clicked into or otherwise
// gains focus.
func (r *Root) expandBashConsole() {
	_, _, _, screenHeight := r.GetRect() // Root fills the whole screen
	target := screenHeight / 2
	if target < 2 {
		target = 2 // bashHint's own row, plus at least one for bashLine
	}
	r.mainLayout.ResizeItem(r.bashConsole, target, 0)
	r.bashConsole.ResizeItem(r.bashHint, 1, 0)
	r.bashConsole.ResizeItem(r.bashLine, 0, 1) // fills whatever's left of target
}

// collapseBashConsole is expandBashConsole's counterpart, wired as
// bashLine's own BlurFunc: back to a single row, bashHint hidden. Runs
// every time focus leaves bashLine — Escape, or a click elsewhere.
func (r *Root) collapseBashConsole() {
	r.mainLayout.ResizeItem(r.bashConsole, 1, 0)
	r.bashConsole.ResizeItem(r.bashHint, 0, 0)
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
//   - Up / Down at the first/last line of the buffer: recall the
//     previous/next history entry (see bashHistoryUp/Down), the same as
//     a real shell's own readline does once there's nowhere left to
//     move the cursor. For the common case — a single-line buffer, the
//     only line is always both the first and the last — this makes
//     plain Up/Down work exactly like a real shell's history recall.
//     Anywhere else in a multi-line buffer, falls through unchanged to
//     TextArea's own default handling instead, moving the cursor
//     between lines as usual.
//   - Ctrl+P / Ctrl+N: recall the previous/next history entry
//     unconditionally, regardless of cursor position — the explicit,
//     always-available form of the above.
func (r *Root) captureBashLineKey(event *tcell.EventKey) *tcell.EventKey {
	switch {
	case event.Key() == tcell.KeyEnter && event.Modifiers()&tcell.ModAlt != 0:
		return event
	case event.Key() == tcell.KeyEnter:
		r.runBashCommand(r.bashLine.GetText())
		return nil
	case event.Key() == tcell.KeyUp && r.bashLineAtFirstLine():
		r.bashHistoryUp()
		return nil
	case event.Key() == tcell.KeyDown && r.bashLineAtLastLine():
		r.bashHistoryDown()
		return nil
	case event.Key() == tcell.KeyCtrlP:
		r.bashHistoryUp()
		return nil
	case event.Key() == tcell.KeyCtrlN:
		r.bashHistoryDown()
		return nil
	}
	return event
}

// bashLineAtFirstLine and bashLineAtLastLine report whether bashLine's
// own cursor currently sits on the first/last line of its (possibly
// multi-line — see captureBashLineKey's own Alt+Enter case) buffer —
// the boundary at which Up/Down should recall history instead of
// TextArea's own default of moving the cursor to a line that doesn't
// exist. For a single-line buffer, the only line is always both.
func (r *Root) bashLineAtFirstLine() bool {
	row, _, _, _ := r.bashLine.GetCursor()
	return row == 0
}

func (r *Root) bashLineAtLastLine() bool {
	row, _, _, _ := r.bashLine.GetCursor()
	return row == strings.Count(r.bashLine.GetText(), "\n")
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

// userShell returns $SHELL, or "/bin/sh" if it isn't set — used by both
// runShellCommandFullScreen and runEditor (see editorCommand's own doc
// comment for why the editor is run through the shell too) as the
// interpreter for whatever the user typed or configured.
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

// runBashCommand is bashLine's own Enter action (see captureBashLineKey):
// records command in history, special-cases "cd" the same way Midnight
// Commander's own command line does (see parseCdCommand/changeDirectory
// — a "cd" run in a child shell/process only ever changes that child's
// own working directory, with no way to affect the panel once it exits),
// and otherwise always runs it full-screen (see runShellCommandFullScreen
// — see newBashConsole's own doc comment on why this doesn't try to
// distinguish "needs a real terminal" from "doesn't" any more).
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

	r.runShellCommandFullScreen(command)
}

// runShellCommandFullScreen suspends the TUI (see
// tview.Application.Suspend) and runs command through userShell, with
// the real terminal handed over for the duration and the panel's
// current directory as its working directory — full interactivity,
// including interactive programs like vim, less, or mc, exactly like
// Midnight Commander's own command line.
//
// Echoes "$ command" before running it and waits for Escape once it's
// done (see waitForEscape) — both per the user's own report that a
// short command's own output otherwise flashes by and is gone the
// moment breakthrough's own screen redraws over it the instant Suspend
// returns, with no indication of what even ran. bashLine ends up
// collapsed either way (SetFocus moves focus off it, triggering
// collapseBashConsole via its own BlurFunc — see newBashConsole) once
// back, per the user's own explicit "soll man zurückkehren auf
// breakthrough mit geschlossenem bash Feld".
//
// The panel reloads once the command exits, in case it changed anything
// in the directory currently on screen.
func (r *Root) runShellCommandFullScreen(command string) {
	var runErr error
	r.app.Suspend(func() {
		fmt.Printf("$ %s\n", command)
		cmd := exec.Command(userShell(), "-c", command)
		cmd.Dir = r.panel.path
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		runErr = cmd.Run()

		fmt.Print("\n[Press Esc to return to breakthrough]")
		waitForEscape()
	})

	r.bashLine.SetText("", true)
	r.app.SetFocus(r.panel.table)
	if runErr != nil {
		r.showError(fmt.Errorf("%s: %w", command, runErr))
		return
	}
	r.showError(r.panel.load(r.panel.path))
}

// waitForEscape blocks until Escape is pressed on the real terminal —
// called from inside app.Suspend (see runShellCommandFullScreen), after
// a command has finished and its own output is what's currently on
// screen, so there's a real chance to read it before breakthrough's own
// UI redraws over it. Puts stdin into raw mode for the duration (see
// golang.org/x/term) so a single keypress is enough — the terminal is
// back in its normal, line-buffered mode at this point, the same "hand
// a real terminal to the child process" state Suspend itself creates
// for the command that just ran.
//
// Best-effort: if stdin isn't a real terminal at all (e.g. under `go
// test`, or redirected) or raw mode can't be entered, returns
// immediately rather than risk hanging on a keypress that may never
// cleanly arrive — the same reasoning appendBashHistory's own doc
// comment gives for its own best-effort failures.
func waitForEscape() {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return
	}
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return
	}
	defer func() { _ = term.Restore(fd, oldState) }()

	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			return
		}
		if buf[0] == 0x1b { // Esc
			return
		}
	}
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
