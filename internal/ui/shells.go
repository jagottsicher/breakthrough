// shells.go is feature_ideas.txt #11's Phase 3a: "Shells" — a separate,
// independent collection of embedded remote terminals (internal/termtab.
// Session + termTabView, Phases 1/2), deliberately NOT merged into
// r.tabs/r.panel (see that same section's own doc comment on why a true
// unification would mean turning r.panel from a concrete *Panel into an
// interface, touching most of this package, for a win Alt+S's own
// separate switcher gets without that risk).
//
// A Shell runs as a real background process the moment it's opened,
// independent of whether it's currently on screen — switching to file
// tabs and back (or to a different Shell) never pauses it, the entire
// point of this over the existing Suspend+exec "gs" SSH shell launcher
// (sshshell.go), which blocks the whole application for as long as it
// runs.
package ui

import (
	"fmt"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
	"github.com/jagottsicher/breakthrough/internal/termtab"
)

// shellTab bundles one open Shell's own session, view, and display
// label — a single struct rather than parallel slices on Root (one
// per field), the same "don't let three slices silently drift out of
// sync" reasoning behind every other bundled-fields-not-parallel-
// slices type in this app.
type shellTab struct {
	session *termtab.Session
	view    *termTabView
	// header is this Terminal's own one-line "connected to" display,
	// mounted above view whenever this Terminal is the one on screen
	// (see remountShellView) — per the user's own explicit request,
	// alongside keeping the panel's own header row visible too (same
	// function), rather than the terminal content alone filling
	// panelHost the way the first version of this did.
	header *tview.TextView
	conn   remotefs.Connection
	// exited is set once session.Done() fires on its own (see
	// watchShellExit) — the process is gone, but the tab itself stays
	// in r.shells and on screen exactly as it was, per the user's own
	// explicit request ("ich mag nicht, wenn da noch was zu sehen
	// ist, dass das einfach weg ist"): no automatic fallback to the
	// panel, no automatic removal from the strip/switcher. Only an
	// explicit close (closeShell) ever removes a tab — the identical
	// "nothing closes a tab but you" rule tabs.go's own tabs already
	// follow.
	exited bool
}

// label is this Terminal's own display name in the switcher and its
// own header line — the connection it's open to, exactly as the
// Connect/SSH-shell dropdowns already label the same information
// (remotefs.Connection.Label), with "(exited)" appended once its own
// process has ended on its own (see exited's own doc comment) so
// there's always a visible reason the screen stopped updating.
func (t *shellTab) label() string {
	if t.exited {
		return t.conn.Label() + "  (exited)"
	}
	return t.conn.Label()
}

// startShellSession is termtab.Start, indirected through a package
// var the same way runMultiplexCommand/mailImapSourceUnreadCount
// already are (see theme.go's loadInitialSettings for the oldest
// instance of the pattern) — the seam a test overrides to run a real
// "sh -c ..." command instead of actually dialing conn, exactly as
// internal/termtab's own StartOptions.Command is used directly in
// that package's own tests.
var startShellSession = termtab.Start

// openNewShellEmbedded starts a new embedded Shell against conn and
// switches to it immediately — the Shells-menu's own "+ New shell…"
// row and (once a connection is actually chosen there, see
// sshShellSubmit) the dialog it opens both end here.
//
// A failure to start (bad command, pty allocation failure — not "ssh
// itself couldn't connect", which Session's own background process
// reports once running, the same split launchSSHShell's own
// Succeeded/ConnectionFailed/LocalError already draws for the
// Suspend+exec launcher) surfaces as an ordinary error overlay: there
// is no Shell to watch or switch to if Start itself never returned
// one.
func (r *Root) openNewShellEmbedded(conn remotefs.Connection) {
	session, err := startShellSession(conn, termtab.StartOptions{})
	if err != nil {
		r.showError(fmt.Errorf("opening shell to %s: %w", conn.Label(), err))
		return
	}
	tab := &shellTab{session: session, view: newTermTabView(session), conn: conn}
	tab.header = newShellHeader(tab, r.theme)
	// Only ever actually fires once tab.view.exited is also set (see
	// watchShellExit) — a live session never reaches isTermLeaveKey's
	// own check at all (see termtabview.go's own doc comment).
	tab.view.onLeave = func() { r.ExitTerminalViewShortcut() }
	r.shells = append(r.shells, tab)
	r.watchShellExit(tab)
	r.switchToShell(len(r.shells) - 1)
}

// newShellHeader builds tab's own one-line "connected to" display —
// colored from theme at construction time (the same as every other
// dynamically-created, long-lived widget this app builds outside
// NewRoot — see styleShellHeaders' own doc comment for the
// applyTheme-side half of keeping it that way across a live scheme
// switch).
func newShellHeader(tab *shellTab, theme config.ResolvedTheme) *tview.TextView {
	header := tview.NewTextView()
	header.SetWrap(false)
	header.SetDynamicColors(false)
	styleShellHeader(header, theme)
	header.SetText(" " + tab.label() + " ")
	return header
}

// styleShellHeader paints header's own colors — InputFocusedBackground/
// TextColor, the same close-glyph-title-bar pair sshShellTitleBar and
// the other dialog title bars in this app already use, since this
// reads the same way: a one-line label sitting above real content.
// Called once at construction (newShellHeader) and again from
// applyTheme's own per-shell loop (theme.go) on a live scheme switch.
func styleShellHeader(header *tview.TextView, theme config.ResolvedTheme) {
	header.SetBackgroundColor(theme.InputFocusedBackground)
	header.SetTextColor(theme.TextColor)
}

// refreshShellHeader re-renders tab's own header text — called once
// its own exited flag flips (see watchShellExit), so the "(exited)"
// marker label() now carries shows up live rather than only the next
// time this Terminal happens to be switched to.
func refreshShellHeader(tab *shellTab) {
	tab.header.SetText(" " + tab.label() + " ")
}

// openNewTerminalDialog opens the existing "Add new ssh connection"
// dialog with its submit overridden to land here (openNewShellEmbedded)
// instead of the default Suspend+exec ssh launcher — the one place both
// the terminal strip's own leading "+" (terminalstrip.go) and the
// switcher's own "+ New terminal…" row (terminalswitcher.go) end up, so
// neither has to duplicate the override-then-open sequence.
// ViewingTerminal reports whether a Terminal is currently on screen —
// cmd/breakthrough's own guard for Ctrl+C (and anything else that
// needs to stop treating a global shortcut as global the moment a
// Terminal has focus, letting the keystroke fall through to
// termTabView's own InputHandler instead).
func (r *Root) ViewingTerminal() bool {
	return r.viewingShells
}

// ExitTerminalViewShortcut is Ctrl+\ (see cmd/breakthrough's own global
// SetInputCapture) — the one way out of a Terminal when neither Alt+S
// nor the switcher's own Escape is reachable (a real iPad-keyboard
// report: Option+S, Alt+S's own equivalent there, never reached the
// app at all). Reserved exclusively while a Terminal is actually on
// screen; reports false otherwise so the global handler lets the
// keystroke fall through unconsumed rather than swallowing a
// combination that means nothing here.
//
// Closes any overlay stacked on top first (the terminal switcher
// itself, most likely) rather than only ever working with none open:
// the point of this key is "get me back to the files, whatever's
// currently in the way", not one more thing to navigate past first.
func (r *Root) ExitTerminalViewShortcut() bool {
	if !r.viewingShells {
		return false
	}
	r.closeAllOverlays()
	r.viewFiles()
	return true
}

func (r *Root) openNewTerminalDialog() {
	r.sshShellSubmit = r.openNewShellEmbedded
	r.openSSHShellDialog(remotefs.Connection{})
}

// watchShellExit marks tab exited once its own underlying process ends
// on its own (the remote shell was told to log out, the connection
// dropped, ...) — one background goroutine per open Shell, the same
// safeGo-wrapped shape every other background watcher in this app
// already uses. Deliberately does NOT remove tab or touch whatever's
// currently on screen (see shellTab.exited's own doc comment on Root
// for the user's own explicit request behind that): closeShell, never
// this, is the one path that actually drops a tab out of r.shells.
// Still runs via QueueUpdateDraw, not a bare field write, since this
// fires from a background goroutine and both the flag and the header
// text it updates are read from the UI thread.
func (r *Root) watchShellExit(tab *shellTab) {
	r.safeGo("shell exit watch", nil, func() {
		<-tab.session.Done()
		r.app.QueueUpdateDraw(func() {
			tab.exited = true
			tab.view.exited = true
			refreshShellHeader(tab)
		})
	})
}

// refreshTerminalStrips pushes the current Terminal count and active
// index into every tab's own terminal strip — the identical shape
// Root.refreshTabStrips already establishes for the tab strip, for the
// identical reason: a tab that isn't currently on screen still has to be
// correct the instant it's switched to.
func (r *Root) refreshTerminalStrips() {
	for _, p := range r.tabs {
		p.setTerminals(len(r.shells), r.activeShell)
	}
}

// shellIndex returns tab's own current position in r.shells, or -1 if
// it's no longer there at all.
func (r *Root) shellIndex(tab *shellTab) int {
	for i, t := range r.shells {
		if t == tab {
			return i
		}
	}
	return -1
}

// closeShell closes shell i's own session outright and removes it
// from r.shells — the switcher's own "✕" cell/key/Delete, and the
// only path that ever actually drops a tab (watchShellExit marks one
// exited on its own, but deliberately never removes it — see
// shellTab.exited's own doc comment). Session.Close() is documented
// safe to call more than once, so closing an already-exited tab here
// is harmless.
func (r *Root) closeShell(i int) {
	if i < 0 || i >= len(r.shells) {
		return
	}
	tab := r.shells[i]
	_ = tab.session.Close()
	r.removeShell(tab)
}

// closeAllShells closes every open Shell's own session outright —
// confirmQuit's own cleanup, the same reason the old Suspend+exec "gs"
// launcher never needed one: that one real `ssh` process always had
// already exited, synchronously, before launchSSHShell itself ever
// returned, while a Shell keeps running in the background for as
// long as it's open (see openNewShellEmbedded's own doc comment) —
// without this, quitting the app would leave every still-open Shell's
// own child process (and, for an actual ssh connection, the remote
// side of it) running orphaned.
func (r *Root) closeAllShells() {
	for _, tab := range r.shells {
		_ = tab.session.Close()
	}
}

// removeShell drops tab out of r.shells — closeShell's own bookkeeping,
// the only caller (see its own doc comment on why watchShellExit
// doesn't call this) — and fixes up r.activeShell/viewingShells so
// neither one still refers to a Shell that no longer exists.
func (r *Root) removeShell(tab *shellTab) {
	i := r.shellIndex(tab)
	if i < 0 {
		return // already removed — see shellIndex's own doc comment
	}
	wasActive := r.viewingShells && r.activeShell == i
	r.shells = append(r.shells[:i], r.shells[i+1:]...)

	switch {
	case len(r.shells) == 0:
		r.activeShell = -1
		if r.viewingShells {
			r.viewFiles()
		}
	case wasActive:
		next := i
		if next >= len(r.shells) {
			next = len(r.shells) - 1
		}
		r.switchToShell(next)
	case r.activeShell > i:
		r.activeShell--
	}
	r.refreshTerminalStrips()
}

// switchToShell makes shell i the visible one, entering "viewing
// shells" mode if not already in it — the Shells menu's own Enter/
// click on a shell row, and a plain digit typed while that menu has
// focus (see captureShellsMenuKey). Also refreshes every panel's own
// terminal strip, since this is the one place r.activeShell actually
// moves (removeShell's own index-only-adjustment branch is the lone
// exception, and calls refreshTerminalStrips itself).
func (r *Root) switchToShell(i int) {
	if i < 0 || i >= len(r.shells) {
		return
	}
	r.activeShell = i
	r.remountShellView()
	r.app.SetFocus(r.shells[i].view)
	r.refreshTerminalStrips()
}

// viewFiles leaves "viewing shells" mode, returning to whichever
// file-tab/panel layout remountPanels would otherwise show — the
// Shells menu's own "Files" row. A no-op while already showing files;
// remountPanels itself is also where this same transition happens as
// a side effect of switching file tabs or entering/leaving split view
// while a Shell was showing (see its own doc comment) — this function
// is only needed for the one remaining path, leaving shell view
// without picking a file tab at the same time.
func (r *Root) viewFiles() {
	if !r.viewingShells {
		return
	}
	r.remountPanels()
	r.app.SetFocus(r.panel.table)
}

// remountShellView puts the active shell's own view into panelHost in
// place of whatever remountPanels would otherwise show there, stacked
// below r.panel's own header row (path bar, filter, tab/terminal
// strips) and this Terminal's own one-line "connected to" header (see
// shellTab.header) — per the user's own explicit request that the top
// row not simply disappear while a Terminal is on screen, the way the
// first version of this did. r.panel's header row is reused directly
// rather than rebuilt: it's already a standalone *tview.Flex field
// (see NewPanel), and r.panel itself (table included) isn't mounted
// anywhere else while this is true, so there's no conflict over who
// draws it. bashConsole's own row still collapses to zero height — a
// live remote shell has no bash-prompt-editor concept of its own (the
// button bar and status bar both stay, per the user's own explicit
// request: the button bar's own keyboard shortcuts mostly won't reach
// it while a Shell has focus, but it costs nothing to leave on
// screen, and the status bar's clock/disk-usage/etc. remain exactly
// as informational here as anywhere else). remountPanels (tabs.go/
// split.go) is the one place that reverses this — see its own doc
// comment — so every existing path that already calls it (switching
// file tabs, closing one, entering/leaving split view) correctly
// leaves shell view as a side effect too, without each of those
// needing to know Shells exist at all.
func (r *Root) remountShellView() {
	r.viewingShells = true
	r.panelHost.Clear()
	r.panelHost.SetDirection(tview.FlexRow)
	tab := r.shells[r.activeShell]
	r.panelHost.AddItem(r.panel.headerRow, 1, 0, false)
	r.panelHost.AddItem(tab.header, 1, 0, false)
	r.panelHost.AddItem(tab.view, 0, 1, true)
	r.mainLayout.ResizeItem(r.bashConsole, 0, 0)
}
