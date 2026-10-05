package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// sshShellMenuCol{Label,Edit,Remove} are the three columns
// sshShellMenuTable's own rows carry — the identical shape
// connectionMenuCol* (connectionmenu.go) establishes, minus an eject
// column: launching a shell is a one-off, there's no ongoing
// connection here to detach from the way an SFTP-mounted panel has.
const (
	sshShellMenuColLabel = iota
	sshShellMenuColEdit
	sshShellMenuColRemove
)

// openSSHShellMenu shows the "gs" chord's own dropdown — see
// keymap.go's own "g" family. Centered on screen rather than anchored
// under a header button the way openConnectionMenu's is: there's no
// equivalent button for this action, so it gets the same centered
// placement every other chord-only dialog in this app already uses
// (openDuplicate, openChmod, ...).
func (r *Root) openSSHShellMenu() {
	r.renderSSHShellMenu()

	width, height := r.sshShellMenuSize()
	x, y := r.centeredOnScreen(width, height)
	x, y, width, height = r.clampToScreen(x, y, width, height)
	r.renderSSHShellMenuTitleBar(width)
	r.sshShellMenuLayout.SetRect(x, y, width, height)
	r.showOverlay(sshShellMenuPage, r.sshShellMenuTable)
}

// renderSSHShellMenu rebuilds the dropdown's own rows from the exact
// same persisted history openConnectionMenu reads (see
// remotefs.LoadHistory) — the user's own explicit "wiederverwenden"
// request: a host saved from either dropdown shows up in both. Each
// label gets shellFlagsSuffix appended, since — unlike the "@"/"gc"
// dropdown, which only ever shows one entry per distinct Host/User/
// Port — this one can show several rows that share all three, each
// opened with a different combination of the four Shell* flags (see
// remotefs.Connection's own doc comment); the suffix is what tells
// those apart from one another at a glance.
func (r *Root) renderSSHShellMenu() {
	table := r.sshShellMenuTable
	table.Clear()
	r.sshShellMenuHistoryRows = map[int]remotefs.Connection{}

	history, _ := remotefs.LoadHistory()
	row := 0
	for i, entry := range history {
		if i >= connectionMenuMaxHistoryRows {
			break
		}
		entry := entry
		thisRow := row

		color := r.theme.Text
		if entry.LastFailed {
			color = r.theme.CriticalText
		}

		table.SetCell(thisRow, sshShellMenuColLabel,
			tview.NewTableCell(entry.Label()+shellFlagsSuffix(entry.Connection)).
				SetTextColor(color).
				SetSelectable(true).
				SetClickedFunc(r.clickSSHShellMenuCell(thisRow, sshShellMenuColLabel)))
		table.SetCell(thisRow, sshShellMenuColEdit,
			tview.NewTableCell(" "+connectionHistoryEditGlyph+" ").
				SetTextColor(r.theme.MutedTextColor).
				SetSelectable(true).
				SetClickedFunc(r.clickSSHShellMenuCell(thisRow, sshShellMenuColEdit)))
		table.SetCell(thisRow, sshShellMenuColRemove,
			tview.NewTableCell(" "+connectionHistoryRemoveGlyph+" ").
				SetTextColor(r.theme.MutedTextColor).
				SetSelectable(true).
				SetClickedFunc(r.clickSSHShellMenuCell(thisRow, sshShellMenuColRemove)))

		r.sshShellMenuHistoryRows[thisRow] = entry.Connection
		row++
	}

	newRow := row
	table.SetCell(newRow, sshShellMenuColLabel,
		tview.NewTableCell(connectionMenuNewConnectionLabel).
			SetTextColor(r.theme.Text).
			SetSelectable(true).
			SetClickedFunc(r.clickSSHShellMenuCell(newRow, sshShellMenuColLabel)))
	table.SetCell(newRow, sshShellMenuColEdit, blankConnectionMenuCell())
	table.SetCell(newRow, sshShellMenuColRemove, blankConnectionMenuCell())

	table.Select(0, sshShellMenuColLabel)
}

// newSSHShellMenuTable builds the dropdown's own Table — the identical
// shape newConnectionMenuTable already establishes.
func (r *Root) newSSHShellMenuTable() *tview.Table {
	table := tview.NewTable()
	table.SetBorders(false)
	table.SetBorderPadding(0, 0, 1, 1)
	table.SetSelectable(true, true)
	table.SetInputCapture(r.captureSSHShellMenuKey)
	table.SetSelectedFunc(func(row, column int) { r.activateSSHShellMenuCell(row, column) })
	return table
}

func (r *Root) newSSHShellMenuLayout() *tview.Flex {
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.sshShellMenuTitleBar, 1, 0, false).
		AddItem(r.sshShellMenuTable, 0, 1, true)
}

// newSSHShellMenuTitleBar builds the dropdown's own title bar — a
// plain TextView (colored in applyTheme, see theme.go), its own text
// set later by renderSSHShellMenuTitleBar once a width is known, with
// a mouse capture for its own close glyph.
func (r *Root) newSSHShellMenuTitleBar() *tview.TextView {
	bar := tview.NewTextView()
	bar.SetWrap(false)
	bar.SetMouseCapture(r.captureSSHShellMenuTitleBarMouse)
	return bar
}

// renderSSHShellMenuTitleBar sets sshShellMenuTitleBar's own text to
// "Open shell to", padded out to width columns, with the close glyph
// placed at toolWindowCloseButtonCol's own one-column-in-from-the-edge
// spot — the identical convention renderHelpTitleBar already
// establishes (see toolwindow.go's own toolWindowCloseGlyph/
// toolWindowCloseButtonCol), reused here rather than a second,
// differently-numbered close button, per the user's own explicit
// request that this read and behave the same way everywhere it
// appears. Called from openSSHShellMenu/removeSSHShellHistoryRow,
// since width can change (a live terminal resize, or a row removed —
// see sshShellMenuSize) between one render and the next.
func (r *Root) renderSSHShellMenuTitleBar(width int) {
	const label = " Open shell to "
	closeCol := toolWindowCloseButtonCol(0, width)
	padding := closeCol - len(label)
	if padding < 0 {
		padding = 0
	}
	r.sshShellMenuTitleBar.SetText(label + strings.Repeat(" ", padding) + string(toolWindowCloseGlyph) + " ")
}

// captureSSHShellMenuTitleBarMouse closes the dropdown when the click
// lands exactly on its own close glyph — the identical shape
// captureHelpTitleBarMouse already establishes; every other click on
// this title bar is otherwise inert, the same as every other modal
// overlay's own title bar in this app.
func (r *Root) captureSSHShellMenuTitleBarMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action != tview.MouseLeftClick {
		return action, event
	}
	x, y := event.Position()
	rectX, rectY, width, _ := r.sshShellMenuTitleBar.GetRect()
	if y != rectY || x != rectX+toolWindowCloseButtonCol(0, width) {
		return action, event
	}
	r.hideOverlay()
	return tview.MouseConsumed, nil
}

// activateSSHShellMenuCell is Enter, Space, or a click on one cell —
// the identical dispatch-by-column shape activateConnectionMenuCell
// already establishes, minus the eject case that doesn't apply here
// (see sshShellMenuColLabel's own doc comment). The label column
// launches the shell immediately, with no dialog in between at all —
// unlike reconnecting an SFTP panel, there's no in-progress dial to
// show a status line for; Suspend+exec either runs or it doesn't.
func (r *Root) activateSSHShellMenuCell(row, column int) {
	conn, ok := r.sshShellMenuHistoryRows[row]
	if !ok {
		r.hideOverlay()
		r.openSSHShellDialog(remotefs.Connection{})
		return
	}
	switch column {
	case sshShellMenuColEdit:
		r.hideOverlay()
		r.openSSHShellDialog(conn)
	case sshShellMenuColRemove:
		r.removeSSHShellHistoryRow(row)
	default:
		r.hideOverlay()
		r.launchSSHShell(conn)
	}
}

func (r *Root) clickSSHShellMenuCell(row, column int) func() bool {
	return func() bool {
		r.activateSSHShellMenuCell(row, column)
		return true
	}
}

// captureSSHShellMenuKey adds "x"/Delete and "e" as from-anywhere-in-
// the-row keyboard equivalents of the row's own "✕"/"✎" cells — the
// same shape captureConnectionMenuKey already establishes, minus its
// "e" eject case (see sshShellMenuColLabel's own doc comment on why
// there's no active-row concept here at all).
func (r *Root) captureSSHShellMenuKey(event *tcell.EventKey) *tcell.EventKey {
	row, column := r.sshShellMenuTable.GetSelection()

	switch {
	case event.Key() == tcell.KeyEscape:
		r.hideOverlay()
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == ' ':
		r.activateSSHShellMenuCell(row, column)
		return nil
	}

	isRemoveKey := (event.Key() == tcell.KeyRune && event.Rune() == 'x') || event.Key() == tcell.KeyDelete
	if isRemoveKey && r.removeSSHShellHistoryRow(row) {
		return nil
	}

	if event.Key() == tcell.KeyRune && event.Rune() == 'e' {
		if conn, ok := r.sshShellMenuHistoryRows[row]; ok {
			r.hideOverlay()
			r.openSSHShellDialog(conn)
			return nil
		}
	}

	return event
}

// removeSSHShellHistoryRow drops row's own Connection out of the
// shared persisted history (see remotefs.RemoveFromHistory) and
// re-renders the dropdown in place — the identical shape
// removeConnectionHistoryRow already establishes.
func (r *Root) removeSSHShellHistoryRow(row int) bool {
	conn, ok := r.sshShellMenuHistoryRows[row]
	if !ok {
		return false
	}
	r.showError(remotefs.RemoveFromHistory(conn))
	r.renderSSHShellMenu()

	width, height := r.sshShellMenuSize()
	x, y := r.centeredOnScreen(width, height)
	x, y, width, height = r.clampToScreen(x, y, width, height)
	r.renderSSHShellMenuTitleBar(width)
	r.sshShellMenuLayout.SetRect(x, y, width, height)
	return true
}

// sshShellMenuSize is the dropdown's own size — the identical
// per-column measurement connectionMenuSize already establishes (see
// its own doc comment for why this has to be measured per column
// rather than by summing a single row).
func (r *Root) sshShellMenuSize() (width, height int) {
	table := r.sshShellMenuTable
	rows := table.GetRowCount()

	columnWidths := make([]int, sshShellMenuColRemove+1)
	for row := 0; row < rows; row++ {
		for column := range columnWidths {
			cell := table.GetCell(row, column)
			if cell == nil {
				continue
			}
			if w := tview.TaggedStringWidth(cell.Text); w > columnWidths[column] {
				columnWidths[column] = w
			}
		}
	}
	for i, w := range columnWidths {
		width += w
		if i > 0 {
			width++
		}
	}

	width += 2             // the table's own left/right border padding
	return width, rows + 1 // +1 for the title bar's own row
}

// shellFlagsSuffix renders whichever of conn's four Shell* flags are
// set as a compact, space-separated letter list ("  [A C]") — "" if
// none are. This is what lets renderSSHShellMenu tell apart two
// history rows that otherwise share the exact same Host/User/Port
// (see remotefs.Connection's own doc comment on why that's possible
// here, unlike in the "@"/"gc" dropdown).
func shellFlagsSuffix(conn remotefs.Connection) string {
	var letters []string
	if conn.ShellAgentForwarding {
		letters = append(letters, "A")
	}
	if conn.ShellCompression {
		letters = append(letters, "C")
	}
	if conn.ShellVerbose {
		letters = append(letters, "v")
	}
	if conn.ShellX11Forwarding {
		letters = append(letters, "X")
	}
	if len(letters) == 0 {
		return ""
	}
	return "  [" + strings.Join(letters, " ") + "]"
}

// plainFormCheckbox renders one of sshShellForm's own boolean fields
// as plain themed text — a filled/hollow dot ("●"/"○") in the same
// foreground color used everywhere else, no highlighted background
// behind it — rather than tview.Checkbox's own default behavior of
// looking like a second kind of editable field (the same boxed
// look an InputField's own field area correctly gets, but a checkbox
// never should). Form.Draw calls SetFormAttributes on every single
// item on every single draw, unconditionally pushing its own shared
// field background onto whatever the item had before (see
// themeDuplicateDropDown's own doc comment on the identical fight for
// a DropDown's own closed-box style) — so without overriding that one
// method, nothing set directly on the embedded Checkbox would ever
// survive past the very next redraw. Per the user's own explicit,
// repeated report that this looked "wie ein Feld hinterlegt" instead
// of a plain radio-style toggle.
type plainFormCheckbox struct {
	*tview.Checkbox
}

// newPlainFormCheckbox builds one, labeled and already carrying the
// "●"/"○" glyphs (see this type's own doc comment on why not tview's
// own default "X"/" ") — colors are applied separately by paint, since
// they depend on the active theme.
func newPlainFormCheckbox(label string) *plainFormCheckbox {
	return &plainFormCheckbox{
		Checkbox: tview.NewCheckbox().SetLabel(label).
			SetCheckedString("●").SetUncheckedString("○"),
	}
}

// paint sets this checkbox's own label and dot colors from theme —
// called once from applyTheme (theme.go), the same "repainted
// explicitly, not just through Form's own generic pass" shape
// themeDuplicateDropDown already uses for the identical reason.
// Background matches the surrounding form (theme.SurfaceBackground),
// not theme.InputFocusedBackground: the whole point is that this
// reads as plain text sitting on the dialog's own background, not a
// second input box next to the real ones.
func (c *plainFormCheckbox) paint(theme config.ResolvedTheme) {
	// Box's own backgroundColor (tview's default Styles.
	// PrimitiveBackgroundColor unless set here) is what Draw actually
	// fills the whole item's rect with before the label/dot are drawn
	// on top — labelStyle itself never carries a background (SetLabel-
	// Color only ever touches its foreground, see tview's own Checkbox),
	// so leaving this call out left that default color showing behind
	// the label text and the padding after the dot, the exact "falsche
	// Backgroundfarbe (zum größten Teil)" the user reported live.
	c.SetBackgroundColor(theme.SurfaceBackground)
	c.SetLabelColor(theme.TextColor)
	style := tcell.StyleDefault.Foreground(theme.TextColor).Background(theme.SurfaceBackground)
	c.SetUncheckedStyle(style)
	c.SetCheckedStyle(style)
}

// SetFormAttributes ignores bgColor/fieldTextColor/fieldBgColor
// entirely — accepting those every draw is exactly what would
// otherwise repaint this checkbox's own dot in Form's shared
// highlighted field-background color (see this type's own doc
// comment). labelWidth/labelColor still apply, the same as every
// other item sharing this form, so labels all line up and match.
func (c *plainFormCheckbox) SetFormAttributes(labelWidth int, labelColor, bgColor, fieldTextColor, fieldBgColor tcell.Color) tview.FormItem {
	c.SetLabelWidth(labelWidth)
	c.SetLabelColor(labelColor)
	return c
}

// openSSHShellDialog shows the "Add new ssh connection" form, prefilled from
// prefill — the zero Connection for a blank "+ New connection…" open,
// or a history entry's own Connection when editing it. The identical
// shape openConnectDialog already establishes, minus the password
// field: see sshShellForm's own doc comment on Root for why.
func (r *Root) openSSHShellDialog(prefill remotefs.Connection) {
	r.sshShellHostField.SetText(prefill.Host)
	if prefill.Port != 0 {
		r.sshShellPortField.SetText(strconv.Itoa(prefill.Port))
	} else {
		r.sshShellPortField.SetText("")
	}
	r.sshShellUserField.SetText(prefill.User)
	r.sshShellAgentCheckbox.SetChecked(prefill.ShellAgentForwarding)
	r.sshShellCompressionCheckbox.SetChecked(prefill.ShellCompression)
	r.sshShellVerboseCheckbox.SetChecked(prefill.ShellVerbose)
	r.sshShellX11Checkbox.SetChecked(prefill.ShellX11Forwarding)
	r.setSSHShellStatus("", r.theme.Text)

	// height covers sshShellTitleBar's own row plus sshShellLayout's
	// three stacked pieces: sshShellForm's own 15 (2 border padding +
	// 7 fields [3 InputFields, 4 Checkboxes] + 6 gaps between them —
	// the identical accounting newConnectLayout's own doc comment
	// gives for its own, smaller field set), sshShellStatus's 1,
	// sshShellButtons' 1.
	width, height := 64, 18
	_, _, screenWidth, screenHeight := r.GetRect()
	if width > screenWidth-4 {
		width = screenWidth - 4
	}
	if height > screenHeight-4 {
		height = screenHeight - 4
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	r.renderSSHShellTitleBar(width)
	r.sshShellLayout.SetRect(x, y, width, height)
	r.showOverlay(sshShellDialogPage, r.sshShellLayout)
}

// newSSHShellForm builds the (initially empty) "Add new ssh
// connection" form once — the identical shape newConnectForm already
// establishes for its own, smaller field set.
func (r *Root) newSSHShellForm() *tview.Form {
	f := tview.NewForm()
	f.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			r.cancelSSHShell()
			return nil
		}
		return event
	})

	r.sshShellHostField = tview.NewInputField().SetLabel("Host")
	f.AddFormItem(r.sshShellHostField)
	r.sshShellPortField = tview.NewInputField().SetLabel("Port (default 22)")
	f.AddFormItem(r.sshShellPortField)
	r.sshShellUserField = tview.NewInputField().SetLabel("User")
	f.AddFormItem(r.sshShellUserField)
	// plainFormCheckbox, not a bare *tview.Checkbox — see its own doc
	// comment on Root for why: Form.Draw would otherwise repaint these
	// every frame in the same highlighted field-background color an
	// InputField's own editable area gets, which read as "ein Feld
	// hinterlegt" rather than a plain radio-style toggle, per the
	// user's own explicit, repeated report.
	r.sshShellAgentCheckbox = newPlainFormCheckbox("Agent forwarding (-A)")
	f.AddFormItem(r.sshShellAgentCheckbox)
	r.sshShellCompressionCheckbox = newPlainFormCheckbox("Compression (-C)")
	f.AddFormItem(r.sshShellCompressionCheckbox)
	r.sshShellVerboseCheckbox = newPlainFormCheckbox("Verbose (-v)")
	f.AddFormItem(r.sshShellVerboseCheckbox)
	r.sshShellX11Checkbox = newPlainFormCheckbox("X11 forwarding (-X)")
	f.AddFormItem(r.sshShellX11Checkbox)

	// Tab/Enter on the form's own last field (X11 forwarding) would
	// otherwise just wrap back to its first one instead of ever
	// reaching sshShellCancelBtn/sshShellOpenBtn — the identical
	// SetInputCapture-over-SetFinishedFunc reasoning newConnectForm's
	// own doc comment explains for its own last field.
	r.sshShellX11Checkbox.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			r.app.SetFocus(r.sshShellCancelBtn)
			return nil
		case tcell.KeyEnter:
			r.runSSHShellFromDialog()
			return nil
		}
		return event
	})

	return f
}

// newSSHShellButtons builds a real Cancel/"Open shell" button pair —
// the identical shape newConnectButtons already establishes.
func (r *Root) newSSHShellButtons() *tview.Flex {
	r.sshShellCancelBtn = tview.NewButton("Cancel").SetSelectedFunc(r.cancelSSHShell)
	r.sshShellOpenBtn = tview.NewButton("Open shell").SetSelectedFunc(r.runSSHShellFromDialog)
	r.sshShellCancelBtn.SetInputCapture(spaceAlsoActivates(r.cancelSSHShell))
	r.sshShellOpenBtn.SetInputCapture(spaceAlsoActivates(r.runSSHShellFromDialog))

	r.sshShellCancelBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.sshShellOpenBtn)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.sshShellX11Checkbox)
		case tcell.KeyEscape:
			r.cancelSSHShell()
		}
	})
	r.sshShellOpenBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.sshShellHostField)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.sshShellCancelBtn)
		case tcell.KeyEscape:
			r.cancelSSHShell()
		}
	})

	return tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.sshShellCancelBtn, 0, 1, false).
		AddItem(r.sshShellOpenBtn, 0, 1, false)
}

// newSSHShellLayout wraps sshShellTitleBar over the form, a one-line
// status area, and the Cancel/"Open shell" buttons — the identical
// shape newConnectLayout already establishes.
func (r *Root) newSSHShellLayout() *tview.Flex {
	r.sshShellTitleBar = tview.NewTextView()
	r.sshShellTitleBar.SetWrap(false)
	r.sshShellTitleBar.SetMouseCapture(r.captureSSHShellTitleBarMouse)
	r.sshShellStatus = tview.NewTextView().SetDynamicColors(true)
	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.sshShellForm, 15, 0, true).
		AddItem(r.sshShellStatus, 1, 0, false).
		AddItem(r.sshShellButtons, 1, 0, false)
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.sshShellTitleBar, 1, 0, false).
		AddItem(content, 0, 1, true)
}

// renderSSHShellTitleBar sets sshShellTitleBar's own text to "Add new
// ssh connection", padded out to width columns, with the close glyph
// placed the same way renderSSHShellMenuTitleBar's own does — see its
// doc comment. Called from openSSHShellDialog, since width can change
// (a live terminal resize) between one open and the next.
func (r *Root) renderSSHShellTitleBar(width int) {
	const label = " Add new ssh connection "
	closeCol := toolWindowCloseButtonCol(0, width)
	padding := closeCol - len(label)
	if padding < 0 {
		padding = 0
	}
	r.sshShellTitleBar.SetText(label + strings.Repeat(" ", padding) + string(toolWindowCloseGlyph) + " ")
}

// captureSSHShellTitleBarMouse closes the dialog (the same as Cancel/
// Escape — see cancelSSHShell) when the click lands exactly on its own
// close glyph — the identical shape captureSSHShellMenuTitleBarMouse/
// captureHelpTitleBarMouse already establish.
func (r *Root) captureSSHShellTitleBarMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action != tview.MouseLeftClick {
		return action, event
	}
	x, y := event.Position()
	rectX, rectY, width, _ := r.sshShellTitleBar.GetRect()
	if y != rectY || x != rectX+toolWindowCloseButtonCol(0, width) {
		return action, event
	}
	r.cancelSSHShell()
	return tview.MouseConsumed, nil
}

func (r *Root) setSSHShellStatus(text string, color tcell.Color) {
	r.sshShellStatus.SetTextColor(color)
	r.sshShellStatus.SetText(text)
}

func (r *Root) cancelSSHShell() {
	r.hideOverlay()
}

// runSSHShellFromDialog reads the form, validates just enough to dial
// (a host is required; a malformed port is rejected before ever
// touching the network) — the identical validation runConnect already
// does — then hides the dialog and launches the shell.
func (r *Root) runSSHShellFromDialog() {
	host := strings.TrimSpace(r.sshShellHostField.GetText())
	if host == "" {
		r.setSSHShellStatus("Host is required.", r.theme.CriticalText)
		return
	}
	port := 0
	if text := strings.TrimSpace(r.sshShellPortField.GetText()); text != "" {
		p, err := strconv.Atoi(text)
		if err != nil || p <= 0 || p > 65535 {
			r.setSSHShellStatus("Port must be a number from 1 to 65535.", r.theme.CriticalText)
			return
		}
		port = p
	}
	user := strings.TrimSpace(r.sshShellUserField.GetText())
	if user == "" {
		user = currentUsername()
	}

	conn := remotefs.Connection{
		Host: host,
		Port: port,
		User: user,

		ShellAgentForwarding: r.sshShellAgentCheckbox.IsChecked(),
		ShellCompression:     r.sshShellCompressionCheckbox.IsChecked(),
		ShellVerbose:         r.sshShellVerboseCheckbox.IsChecked(),
		ShellX11Forwarding:   r.sshShellX11Checkbox.IsChecked(),
	}

	r.hideOverlay()
	r.launchSSHShell(conn)
}

// sshConnectionFailedExitCode is the exit code ssh itself uses for "the
// connection could not be established at all" (refused, unreachable,
// auth failed, ...) — as opposed to the *remote shell's own* exit
// code, which ssh simply passes through unchanged and can be anything.
// Documented in ssh(1); used by launchSSHShell to tell the two apart
// before deciding whether to mark a history entry as having failed.
const sshConnectionFailedExitCode = 255

// launchSSHShell suspends the TUI (see tview.Application.Suspend) and
// runs a real `ssh` process with the terminal handed over for the
// duration — full interactivity, including a remote `sudo -i`/`su -`
// identity switch, exactly like running ssh from a bare terminal,
// since this *is* a bare terminal for as long as the child process
// runs. No working directory is set and no local panel ever reloads
// afterwards: unlike runShellCommandFullScreen's own local command,
// nothing here ever touches anything on this machine's own
// filesystem.
//
// Records the attempt in the shared connections.json history (see
// remotefs.RecordAttempt) once ssh itself returns — failed only when
// its own exit code is exactly sshConnectionFailedExitCode, the one
// code that actually means "never really connected" rather than
// "the remote shell exited with some status", which is none of this
// app's business. A local problem (ssh itself missing, say) surfaces
// immediately instead, without ever touching history at all: no
// connection attempt was actually made.
func (r *Root) launchSSHShell(conn remotefs.Connection) {
	argv := sshShellArgv(conn)

	var runErr error
	r.app.Suspend(func() {
		cmd := exec.Command("ssh", argv...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		runErr = cmd.Run()
	})

	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
		_ = remotefs.RecordAttempt(conn, false)
		r.activityLog.Action(activitylog.CategoryRemote, fmt.Sprintf("ssh shell to %s", conn.Label()))
	case errors.As(runErr, &exitErr):
		failed := exitErr.ExitCode() == sshConnectionFailedExitCode
		_ = remotefs.RecordAttempt(conn, failed)
		if failed {
			r.activityLog.Error(activitylog.CategoryRemote, fmt.Sprintf("ssh shell to %s: connection failed", conn.Label()))
		} else {
			r.activityLog.Action(activitylog.CategoryRemote, fmt.Sprintf("ssh shell to %s", conn.Label()))
		}
	default:
		// ssh itself never actually ran (missing binary, ...) — a local
		// problem, not a connection attempt at all, so history is left
		// untouched.
		r.showError(fmt.Errorf("ssh: %w", runErr))
	}
}

// sshShellArgv builds the argv (after "ssh" itself) that launchSSHShell
// runs — split out so the exact flag mapping can be pinned by a test
// without actually exec'ing ssh.
func sshShellArgv(conn remotefs.Connection) []string {
	var args []string
	if conn.Port != 0 && conn.Port != defaultSSHPort {
		args = append(args, "-p", strconv.Itoa(conn.Port))
	}
	if conn.ShellAgentForwarding {
		args = append(args, "-A")
	}
	if conn.ShellCompression {
		args = append(args, "-C")
	}
	if conn.ShellVerbose {
		args = append(args, "-v")
	}
	if conn.ShellX11Forwarding {
		args = append(args, "-X")
	}
	user := conn.User
	if user == "" {
		user = currentUsername()
	}
	args = append(args, user+"@"+conn.Host)
	return args
}

// defaultSSHPort mirrors remotefs' own unexported defaultSFTPPort —
// duplicated here rather than exported from remotefs purely to avoid
// growing that package's own public surface for a single int only
// this file needs: ssh itself already defaults to 22 without "-p" at
// all, so this just has to agree with what Connection.Port22IfZero
// already treats as "nothing typed".
const defaultSSHPort = 22
