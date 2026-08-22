// Command breakthrough is the entry point of the breakthrough TUI file
// manager — see internal/ui.Root for what it actually shows (see also
// docs/whitepaper.md for the overall concept and vision).
//
// The initial directory is the process's working directory unless a path
// is given on the command line, e.g. "breakthrough /var/log" — see
// startDir. That directory also becomes the header's Start button target.
// "breakthrough --version" (or "-v") prints version information instead
// of starting the TUI — see the version/commit/date/builtBy vars below.
package main

import (
	"fmt"
	"os"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/ui"
)

// version, commit, date, and builtBy are set via "go build -ldflags -X
// ..." by the release pipeline (see .goreleaser.yaml, which relies on
// exactly these four names existing here) — the defaults below are what
// a plain "go build", with no ldflags at all, reports instead.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "source"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("breakthrough %s (commit %s, built %s by %s)\n", version, commit, date, builtBy)
		return
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "breakthrough:", err)
		os.Exit(1)
	}
}

func run() error {
	start, err := startDir()
	if err != nil {
		return err
	}

	// EnablePaste turns on tcell's bracketed-paste support: without it, a
	// terminal paste arrives as a burst of ordinary key events, which for
	// a multi-line clipboard (or one containing a stray control
	// character) can misfire an InputField's own key handling instead of
	// just inserting the text. This is what makes pasting into any of
	// breakthrough's text fields — the new bash line (see
	// ui.Root.StartClock's neighbors in bottombar.go) most of all —
	// reliable.
	app := tview.NewApplication().EnableMouse(true).EnablePaste(true)

	root, err := ui.NewRoot(app, start)
	if err != nil {
		return err
	}

	// Only meaningful once Application.Run is about to start draining its
	// update queue — see StartClock's own doc comment for why NewRoot
	// itself doesn't do this.
	stopClock := root.StartClock()
	defer stopClock()

	// tcell puts the terminal in raw mode, so these arrive as regular key
	// events rather than signals. These are global captures (not tied to
	// any one primitive) so they work regardless of what currently has
	// focus.
	//
	// Ctrl+Q only opens a confirmation overlay rather than stopping
	// immediately, since a stray keypress shouldn't lose your place
	// without asking first. Ctrl+C deliberately does not quit at all —
	// it backs out of whatever is open, like Escape. Ctrl+X is
	// deliberately left unclaimed (available for a future binding),
	// having previously also quit.
	//
	// Ctrl+E/Ctrl+R/Ctrl+G/Ctrl+X (Edit/Rename/toggle hidden files/Settings
	// — see the bottom bar's own buttons) check their own preconditions
	// before acting (see Root.acceptsGlobalShortcut) rather than always
	// firing the way Ctrl+Q/Ctrl+C do: unlike those two, they'd otherwise
	// step on the bash line's own typing. Ctrl+H is deliberately not one
	// of them — it's indistinguishable from Backspace at the terminal
	// protocol level (both send the same 0x08 byte), so Ctrl+G was used
	// for "toggle hidden files" instead. Ctrl+X, previously left unclaimed
	// (it used to also quit, redundantly with Ctrl+Q, before that was
	// cleaned up), is now Settings.
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyCtrlQ:
			root.RequestQuit()
			return nil
		case tcell.KeyCtrlC:
			root.RequestCancel()
			return nil
		case tcell.KeyCtrlE:
			root.EditShortcut()
			return nil
		case tcell.KeyCtrlR:
			root.RenameShortcut()
			return nil
		case tcell.KeyCtrlG:
			root.ToggleHiddenShortcut()
			return nil
		case tcell.KeyCtrlX:
			root.SettingsShortcut()
			return nil
		}
		return event
	})

	return app.SetRoot(root, true).Run()
}

// startDir picks the directory breakthrough opens in: an explicit
// "breakthrough /some/path" argument if one was given, otherwise the
// process's current working directory. Whichever it resolves to also
// becomes the panel's "Start" button target (see ui.Panel's header) — an
// invalid or unreadable argument is left for Panel's own load to reject,
// rather than duplicating that validation here.
func startDir() (string, error) {
	if len(os.Args) > 1 {
		return os.Args[1], nil
	}
	return os.Getwd()
}
