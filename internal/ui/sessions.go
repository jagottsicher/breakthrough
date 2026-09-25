// sessions.go is the Sessions screen ("js", see keymap.go) — a
// full-screen catalog listing this host's own local GNU screen and
// tmux terminal-multiplexer sessions (see internal/multiplex), styled
// after the Tab switcher/Connection menu's own per-row action-cell
// table (tabswitcher.go/connectionmenu.go) rather than Mounts/
// Firewall's own plain read-only rows: each session carries three
// independent actions of its own (Attach, Attach in new window,
// Close), not just one row-wide "select it" the way a mount or a
// firewall rule does.
//
// feature_ideas.txt's own "local stage" for this feature: only local
// sessions, real screen/tmux binaries never reimplemented, Attach reuses
// the exact same Suspend-based full-screen mechanism the bash line/
// Rsync/Firewall's own command execution already use. Remote sessions
// and "Attach in new window" (a real second window *inside*
// breakthrough) are both explicitly out of scope here — the first needs
// its own connection-reuse design, the second needs real PTY/ANSI
// terminal emulation (see feature_ideas.txt's own "Eingebetteter
// Remote-Terminal-Tab"), so it shows a plain "not available yet" notice
// instead of pretending to work.
package ui

import (
	"fmt"
	"os"
	"os/exec"
	"sort"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/multiplex"
)

const sessionsPage = "sessions"

// listMultiplexSessions is a package-level swappable var — the same
// mockable idiom readFirewallSnapshot/readMounts already establish —
// so this screen's own render logic can be exercised without ever
// touching a real host's actual screen/tmux state.
var listMultiplexSessions = multiplex.ListSessions

// runMultiplexCommand runs argv and reports whether it succeeded —
// swappable so a test can observe exactly which argv runCloseSession
// would run without a real screen/tmux binary needing to exist at all.
// attachSession's own exec.Command call has no equivalent need for
// this: it only ever runs inside r.app.Suspend, which is already a
// verified no-op without a real terminal (confirmed against tview's
// own Application.Suspend — see runShellCommandFullScreen's identical,
// already-relied-on assumption in bashconsole.go), so a test never
// actually reaches it either way.
var runMultiplexCommand = func(argv []string) error {
	return exec.Command(argv[0], argv[1:]...).Run()
}

// Column indices within sessionsTable — the same label/action-cell
// shape connectionmenu.go's own connectionMenuCol* constants establish,
// just with three action cells instead of two: a session has Attach,
// Attach in new window, and Close, all independent of each other.
const (
	sessionsColName = iota
	sessionsColBackend
	sessionsColStatus
	sessionsColAttach
	sessionsColNewWindow
	sessionsColClose
)

// sessionsAttachGlyph/sessionsNewWindowGlyph/sessionsCloseGlyph are this
// screen's own three action-cell glyphs — per the user's own explicit
// choice, from Unicode blocks already used elsewhere in this app
// (✕, matching connectionmenu.go's/tabswitcher.go's own established
// close glyph exactly) or picked directly by the user (⭢/⇶).
const (
	sessionsAttachGlyph    = "⭢"
	sessionsNewWindowGlyph = "⇶"
	sessionsCloseGlyph     = "✕"

	// sessionsAttachedGlyph/sessionsDetachedGlyph/sessionsUnknownGlyph are
	// the Status column's own glyphs, per the user's own explicit choice
	// for the first two — a heavy checkmark/ballot X (U+2714/U+2718)
	// colored green/red (see sessionsStatusColor), rather than the words
	// "Attached"/"Detached" themselves. sessionsUnknownGlyph (a plain em
	// dash, in the theme's own muted color) is this screen's own
	// addition for multiplex.StatusUnknown — Zellij's own list-sessions
	// never reports attach status at all (see multiplex.Status's own doc
	// comment), so showing ✔ or ✘ for it either way would just be a
	// guess dressed up as a fact.
	sessionsAttachedGlyph = "✔"
	sessionsDetachedGlyph = "✘"
	sessionsUnknownGlyph  = "–"
)

// sessionsStatusGlyph/sessionsStatusColor render one Session's own
// Status — split from renderSessionsRow so sessionsColStatus's cell
// construction reads as "glyph, then its own color" without a local
// if/else repeating the same three-way switch twice.
func sessionsStatusGlyph(status multiplex.Status) string {
	switch status {
	case multiplex.StatusAttached:
		return sessionsAttachedGlyph
	case multiplex.StatusDetached:
		return sessionsDetachedGlyph
	default:
		return sessionsUnknownGlyph
	}
}

func sessionsStatusColor(status multiplex.Status, theme config.ResolvedTheme) tcell.Color {
	switch status {
	case multiplex.StatusAttached:
		return theme.EntryExecutable
	case multiplex.StatusDetached:
		return theme.CriticalText
	default:
		return theme.MutedTextColor
	}
}

// sessionsBackendColor gives each backend's own name its own fixed
// color in the Backend column — per the user's own explicit choice, the
// same colors their own status-bar segments already use (see
// statusDiskColor/statusInodeColor/statusKernelColor in bottombar.go),
// so a backend reads consistently wherever this app already colors
// something by "which subsystem is this", not a fresh palette invented
// just for this column. mosh has no color here: see multiplex's own
// package doc comment for why it was never made a backend at all.
func sessionsBackendColor(backend multiplex.Backend) tcell.Color {
	switch backend {
	case multiplex.BackendScreen:
		return statusDiskColor
	case multiplex.BackendTmux:
		return statusInodeColor
	case multiplex.BackendZellij:
		return statusKernelColor
	default:
		return tcell.ColorDefault
	}
}

// sessionsColumnWidth is Name/Backend/Status's own padding floor — the
// same "floor, never a ceiling" reasoning firewallColumnWidth's own doc
// comment gives. The three action columns have no floor: each is a
// single glyph, never wider than its own fixed " ⭢ " padding. Status is
// a single glyph too now (✔/✘, see sessionsAttachedGlyph/
// sessionsDetachedGlyph), so its own floor only needs to fit "Status"
// itself, the wider of the two.
func sessionsColumnWidth(col int) int {
	switch col {
	case sessionsColName:
		return 24
	case sessionsColBackend:
		return 8
	case sessionsColStatus:
		return 8
	default:
		return 0
	}
}

// newSessionsScreen builds the whole screen once, at startup — the same
// build-once/repopulate-on-open shape newFirewallScreen already
// establishes. SetSelectable(true, true): cell-level selection, not
// just row-level, since each row's own three action cells (plus the
// name cell itself, which also attaches — see activateSessionsCell) are
// independently reachable targets, the same reason
// newConnectionMenuTable/newTabSwitcher both need it too.
func (r *Root) newSessionsScreen() {
	r.sessionsTable = tview.NewTable()
	r.sessionsTable.SetBorders(false)
	r.sessionsTable.SetBorderPadding(1, 0, 2, 1)
	r.sessionsTable.SetSelectable(true, true)
	r.sessionsTable.SetFixed(1, 0)
	r.sessionsTable.SetInputCapture(r.captureSessionsKey)
	r.sessionsTable.SetSelectedFunc(func(row, column int) { r.activateSessionsCell(row, column) })

	r.sessionsTitleBar = newPlainTitleBar("Sessions")

	r.sessionsHint = tview.NewTextView()
	r.sessionsHint.SetWrap(false)
	r.sessionsHint.SetText(" ↑/↓/←/→: move · Enter/Space/click: activate · r: refresh · Esc: close ")

	r.sessionsLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.sessionsTitleBar, 1, 0, false).
		AddItem(r.sessionsTable, 0, 1, true).
		AddItem(r.sessionsHint, 1, 0, false)
}

// openSessions shows the Sessions screen, freshly read every time —
// the same reasoning openMounts/openFirewall's own doc comments give:
// a session can be started, attached, or closed by something else
// entirely while this screen isn't open.
func (r *Root) openSessions() {
	r.reloadSessions()
	r.showOverlay(sessionsPage, r.sessionsLayout)
}

func (r *Root) closeSessions() {
	r.hideOverlay()
}

// reloadSessions re-reads this host's own local screen/tmux sessions
// and re-renders — the screen's own initial population, and "r"'s own
// manual refresh.
func (r *Root) reloadSessions() {
	sessions, err := listMultiplexSessions()
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].Backend != sessions[j].Backend {
			return sessions[i].Backend < sessions[j].Backend
		}
		return sessions[i].Name < sessions[j].Name
	})
	r.sessionsList = sessions
	r.sessionsErr = err
	r.renderSessions()
}

// renderSessions fills the table: a bold header row, then one row per
// local session actually found. A failed read, or no sessions at all,
// shows in place of any rows via showTablePlaceholder — see its own
// doc comment for why every one of this app's full-screen catalogs
// goes through it rather than a bare SetCell.
func (r *Root) renderSessions() {
	r.sessionsTable.Clear()

	header := func(col int, text string) {
		r.sessionsTable.SetCell(0, col,
			tview.NewTableCell(padRight(text, sessionsColumnWidth(col))).
				SetTextColor(r.theme.Text).
				SetAttributes(tcell.AttrBold).
				SetSelectable(false))
	}
	header(sessionsColName, "Name")
	header(sessionsColBackend, "Backend")
	header(sessionsColStatus, "Status")
	header(sessionsColAttach, "")
	header(sessionsColNewWindow, "")
	header(sessionsColClose, "")

	r.sessionsTitleBar.SetText(" " + sessionsTitle(r.sessionsList, r.sessionsErr) + " ")

	if r.sessionsErr != nil {
		showTablePlaceholder(r.sessionsTable, r.sessionsErr.Error(), r.theme.EntryError)
		return
	}
	if len(r.sessionsList) == 0 {
		showTablePlaceholder(r.sessionsTable, "No screen or tmux sessions found.", r.theme.PlaceholderText)
		return
	}

	for i, s := range r.sessionsList {
		r.renderSessionsRow(i+1, s)
	}
	// Not enableTableSelection (SetSelectable(true, false), rows only) —
	// unlike Mounts/Firewall/Activity Log, this screen's own rows each
	// carry three independent action cells, so both rows *and* columns
	// need to stay selectable, the same as connectionmenu.go's/
	// tabswitcher.go's own tables. A real, reported bug: with columns
	// not selectable, Left/Right silently fall back to tview's own
	// horizontal-scroll-offset handling instead of moving the cell
	// selection at all — invisible on a table narrow enough to need no
	// scrolling, which reads as "Left/Right do nothing whatsoever".
	r.sessionsTable.SetSelectable(true, true)

	if cur, _ := r.sessionsTable.GetSelection(); cur < 1 || cur > len(r.sessionsList) {
		r.sessionsTable.Select(1, sessionsColName)
	}
}

// sessionsTitle renders the title bar's own live summary.
func sessionsTitle(sessions []multiplex.Session, err error) string {
	if err != nil {
		return "Sessions — read failed"
	}
	return fmt.Sprintf("Sessions — %d found", len(sessions))
}

// renderSessionsRow fills one session's own row: Name/Backend/Status as
// plain text, then three independently selectable/clickable action
// cells — the same per-cell shape renderConnectionMenu's own eject/
// edit/remove cells already establish.
func (r *Root) renderSessionsRow(row int, s multiplex.Session) {
	// Clicked, not just selectable: the same "clicking this cell attaches,
	// exactly like Enter/Space would" behavior connectionmenu.go's own
	// label cell already gives its row's own primary action.
	cell := func(col int, text string, color tcell.Color) {
		r.sessionsTable.SetCell(row, col,
			tview.NewTableCell(padRight(text, sessionsColumnWidth(col))).
				SetTextColor(color).
				SetSelectable(true).
				SetClickedFunc(r.clickSessionsCell(row, col)))
	}
	cell(sessionsColName, s.Name, r.theme.Text)
	cell(sessionsColBackend, string(s.Backend), sessionsBackendColor(s.Backend))
	cell(sessionsColStatus, sessionsStatusGlyph(s.Status), sessionsStatusColor(s.Status, r.theme))

	action := func(col int, glyph string) {
		r.sessionsTable.SetCell(row, col,
			tview.NewTableCell(" "+glyph+" ").
				SetTextColor(r.theme.MutedTextColor).
				SetSelectable(true).
				SetClickedFunc(r.clickSessionsCell(row, col)))
	}
	action(sessionsColAttach, sessionsAttachGlyph)
	action(sessionsColNewWindow, sessionsNewWindowGlyph)
	action(sessionsColClose, sessionsCloseGlyph)
}

// sessionAt returns the session row's own data — false for the header
// row or any row past the end (neither ever reachable through the
// table's own selectable cells, but guarded rather than assumed, the
// same defensiveness activateConnectionMenuCell's own map lookup gives).
func (r *Root) sessionAt(row int) (multiplex.Session, bool) {
	i := row - 1
	if i < 0 || i >= len(r.sessionsList) {
		return multiplex.Session{}, false
	}
	return r.sessionsList[i], true
}

// activateSessionsCell is Enter, Space, or a click on one cell — the
// same dispatch-by-column shape activateConnectionMenuCell/
// activateTabSwitcherCell both already establish. The Name and Backend/
// Status cells all attach too, the same way clicking a connection
// history row's own label connects: "select this row and do the
// obvious thing" needs no separate, dedicated cell of its own.
func (r *Root) activateSessionsCell(row, column int) {
	s, ok := r.sessionAt(row)
	if !ok {
		return
	}
	switch column {
	case sessionsColNewWindow:
		r.showTransientError(fmt.Errorf("attach in a new window: not available yet"))
	case sessionsColClose:
		r.confirmCloseSession(s)
	default:
		r.attachSession(s)
	}
}

// clickSessionsCell is one action cell's own mouse action — see
// clickConnectionMenuCell's own doc comment for why this has to be
// per-cell rather than a table-wide mouse capture (tview's own Table
// never runs a row's SetSelectedFunc on a plain click, only moves the
// selection there). Returns true so tview does not also move the
// selection afterwards.
func (r *Root) clickSessionsCell(row, column int) func() bool {
	return func() bool {
		r.activateSessionsCell(row, column)
		return true
	}
}

// captureSessionsKey is the Sessions screen's own key handling: Escape
// closes it, "r" re-reads the live session list — the same two keys
// captureFirewallKey/captureMountsKey already handle — and "x"/Delete
// close the currently selected row's own session from anywhere in that
// row, the same "from-anywhere-in-the-row" convenience
// captureConnectionMenuKey's own identical keys already give Remove.
func (r *Root) captureSessionsKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeSessions()
		return nil
	}
	if event.Key() == tcell.KeyRune && event.Rune() == 'r' {
		r.reloadSessions()
		return nil
	}
	isCloseKey := (event.Key() == tcell.KeyRune && event.Rune() == 'x') || event.Key() == tcell.KeyDelete
	if isCloseKey {
		row, _ := r.sessionsTable.GetSelection()
		if s, ok := r.sessionAt(row); ok {
			r.confirmCloseSession(s)
			return nil
		}
	}
	return event
}

// attachSession is "Attach": suspends the TUI and execs
// multiplex.AttachCommand(s) directly (no shell — s.Name is exactly
// what the real backend itself reported, never something a shell would
// need to interpret) with the real terminal attached, the same
// Suspend-based mechanism runShellCommandFullScreen/
// runFirewallCommandFullScreen already use. Control returns to
// breakthrough the moment the child exits — by detaching or because the
// session itself ended — with no extra "press a key to continue"
// prompt: unlike a one-shot command's own output, there is nothing left
// on screen worth pausing to read once screen/tmux have already
// restored the terminal themselves.
func (r *Root) attachSession(s multiplex.Session) {
	argv := multiplex.AttachCommand(s)
	if len(argv) == 0 {
		return
	}
	var runErr error
	r.app.Suspend(func() {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		runErr = cmd.Run()
	})
	r.reloadSessions()
	if runErr != nil {
		r.showError(fmt.Errorf("attach to %s: %w", s.Name, runErr))
	}
}

// confirmCloseSession asks first — closing a session is irreversible
// (whatever was running inside it is gone), the same "irreversible
// action sichtbar machen und bestätigen" discipline Remove/Empty Trash
// already follow (see openConfirm in trash.go).
func (r *Root) confirmCloseSession(s multiplex.Session) {
	r.openConfirm(fmt.Sprintf("Close the %s session %q?", s.Backend, s.Name), "Yes, close", func() {
		r.runCloseSession(s)
	})
}

// runCloseSession ends s outright — screen -X quit/tmux kill-session,
// neither one interactive, so this runs directly (no Suspend, no real
// terminal needed) unlike attachSession.
func (r *Root) runCloseSession(s multiplex.Session) {
	argv := multiplex.CloseCommand(s)
	if len(argv) == 0 {
		return
	}
	if err := runMultiplexCommand(argv); err != nil {
		r.showError(fmt.Errorf("close %s: %w", s.Name, err))
	}
	r.reloadSessions()
}
