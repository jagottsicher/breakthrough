// Command breakthrough is the entry point of the breakthrough TUI file
// manager.
//
// Phase 0/1: a single panel showing the current directory, navigable with
// the arrow keys, Enter, and a browser-style path header (Home/Back/
// Forward, clickable breadcrumbs, click-to-edit), plus a right-click
// context menu (Info, Rename). A second panel and further actions land in
// later, vertically-sliced feature branches (see docs/whitepaper.md for
// the overall concept).
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
	start, err := os.Getwd()
	if err != nil {
		return err
	}

	app := tview.NewApplication().EnableMouse(true)

	root, err := ui.NewRoot(app, start)
	if err != nil {
		return err
	}

	// tcell puts the terminal in raw mode, so these arrive as regular key
	// events rather than signals. These are global captures (not tied to
	// any one primitive) so they work regardless of what currently has
	// focus.
	//
	// Ctrl+X and Ctrl+Q both just open a confirmation overlay rather than
	// stopping immediately, since a stray keypress shouldn't lose your
	// place without asking first — Ctrl+Q is a second, equally direct way
	// in (many terminal apps use it to quit) alongside Ctrl+X. Ctrl+C
	// deliberately does not quit at all — it backs out of whatever is
	// open, like Escape.
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyCtrlX, tcell.KeyCtrlQ:
			root.RequestQuit()
			return nil
		case tcell.KeyCtrlC:
			root.RequestCancel()
			return nil
		}
		return event
	})

	return app.SetRoot(root, true).Run()
}
