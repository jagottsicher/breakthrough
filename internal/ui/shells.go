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
	conn    remotefs.Connection
}

// label is this shell's own display name in the Shells menu — the
// connection it's open to, exactly as the Connect/SSH-shell dropdowns
// already label the same information (remotefs.Connection.Label).
func (t *shellTab) label() string {
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
	r.shells = append(r.shells, tab)
	r.watchShellExit(tab)
	r.switchToShell(len(r.shells) - 1)
}

// watchShellExit removes tab once its own underlying process exits on
// its own (the remote shell was told to log out, the connection
// dropped, ...) — one background goroutine per open Shell, the same
// safeGo-wrapped shape every other background watcher in this app
// already uses. Nothing to clean up if the Shell was already removed
// some other way (closeShell closes the session itself, which this
// goroutine's own Done() channel then also reports — removeShell's
// own shellIndex lookup is a no-op the second time around).
func (r *Root) watchShellExit(tab *shellTab) {
	r.safeGo("shell exit watch", nil, func() {
		<-tab.session.Done()
		r.app.QueueUpdateDraw(func() {
			r.removeShell(tab)
		})
	})
}

// shellIndex returns tab's own current position in r.shells, or -1 if
// it's no longer there at all (already removed — see watchShellExit's
// own doc comment on why that's an expected, harmless race, not a
// bug).
func (r *Root) shellIndex(tab *shellTab) int {
	for i, t := range r.shells {
		if t == tab {
			return i
		}
	}
	return -1
}

// closeShell closes shell i's own session outright (the Shells menu's
// own "✕" cell/key) and removes it from r.shells immediately — not
// left for watchShellExit's own Done()-triggered removeShell call to
// catch up with asynchronously, since that would leave the menu
// showing a row for a Shell the user just told it to close until that
// round trip through the UI thread completes. watchShellExit's own
// call to removeShell afterwards is then simply a no-op (see
// shellIndex's own doc comment) — a Shell that exits on its own,
// never having gone through this function, still only ever gets
// removed that one way.
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

// removeShell drops tab out of r.shells once its own process has
// actually exited (see watchShellExit/closeShell, the only two
// callers) and fixes up r.activeShell/viewingShells so neither one
// still refers to a Shell that no longer exists.
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
}

// switchToShell makes shell i the visible one, entering "viewing
// shells" mode if not already in it — the Shells menu's own Enter/
// click on a shell row, and a plain digit typed while that menu has
// focus (see captureShellsMenuKey).
func (r *Root) switchToShell(i int) {
	if i < 0 || i >= len(r.shells) {
		return
	}
	r.activeShell = i
	r.remountShellView()
	r.app.SetFocus(r.shells[i].view)
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
// place of whatever remountPanels would otherwise show there, and
// collapses bashConsole's own row to zero height — a live remote
// shell has no bash-prompt-editor concept of its own (the button bar
// and status bar both stay, per the user's own explicit request: the
// button bar's own keyboard shortcuts mostly won't reach it while a
// Shell has focus, but it costs nothing to leave on screen, and the
// status bar's clock/disk-usage/etc. remain exactly as informational
// here as anywhere else). remountPanels (tabs.go/split.go) is the one
// place that reverses this — see its own doc comment — so every
// existing path that already calls it (switching file tabs, closing
// one, entering/leaving split view) correctly leaves shell view as a
// side effect too, without each of those needing to know Shells exist
// at all.
func (r *Root) remountShellView() {
	r.viewingShells = true
	r.panelHost.Clear()
	r.panelHost.SetDirection(tview.FlexColumn)
	r.panelHost.AddItem(r.shells[r.activeShell].view, 0, 1, true)
	r.mainLayout.ResizeItem(r.bashConsole, 0, 0)
}
