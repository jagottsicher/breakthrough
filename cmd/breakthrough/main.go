// Command breakthrough is the entry point of the breakthrough TUI file
// manager.
//
// Phase 0/1: a single panel showing the current directory, navigable with
// the arrow keys, Enter, and a browser-style path header (Start/Home/Back/
// Forward, clickable breadcrumbs, click-to-edit), plus a right-click
// context menu (Info, Rename). A second panel and further actions land in
// later, vertically-sliced feature branches (see docs/whitepaper.md for
// the overall concept).
//
// The initial directory is the process's working directory unless a path
// is given on the command line, e.g. "breakthrough /var/log" — see
// startDir. That directory also becomes the header's Start button target.
package main

import (
	"fmt"
	"os"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/ui"
)

func main() {
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
	// Ctrl+E/Ctrl+R/Ctrl+G (Edit/Rename/toggle hidden files — see the
	// bottom bar's own buttons) check their own preconditions before
	// acting (see Root.acceptsGlobalShortcut) rather than always firing
	// the way Ctrl+Q/Ctrl+C do: unlike those two, they'd otherwise step
	// on the bash line's own typing. Ctrl+H is deliberately not one of
	// them — it's indistinguishable from Backspace at the terminal
	// protocol level (both send the same 0x08 byte), so Ctrl+G was used
	// for "toggle hidden files" instead.
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
