package ui

import (
	"errors"
	"fmt"
	"os"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
	"github.com/jagottsicher/breakthrough/internal/sshkeys"
)

// The SSH Keys screen ("jk"): a seventh full-screen, read-only catalog
// alongside Options/Toolbox/Mounts/Firewall/Sessions/Activity Log —
// Stage 1 (see feature_ideas.txt's own SSH-Key-Verwaltung entry) of an
// eventual generate/distribute workflow. This first cut only inventories
// what already exists under ~/.ssh (internal/sshkeys): which key pairs
// are there, their algorithm and size, whether the private half is
// passphrase-protected, whether it's currently loaded in a running
// ssh-agent, and whether its own file permissions are as strict as sshd
// itself requires. Nothing here writes, generates, or copies anything
// yet — that is real, separate follow-up work of its own, tracked in
// feature_ideas.txt rather than folded into this first, purely
// observational cut, the same staged shape the Firewall screen's own
// doc comment already describes for a similarly higher-blast-radius
// feature.

const sshKeysPage = "sshkeys"

// sshState holds the SSH Keys screen's widgets/last read result, the
// "Generate key" and "Copy public key" dialogs, and the "gs" chord's
// own shell menu/dialog (see sshkeysgenerate.go, sshkeyscopy.go,
// sshshell.go).
type sshState struct {
	KeysLayout               *tview.Flex
	KeysTitleBar             *tview.TextView
	KeysHint                 *tview.TextView
	KeysHintSpans            []listHintSpan
	KeysTable                *tview.Table
	KeysScrollbar            *verticalScrollbar
	KeysPairs                []sshkeys.KeyPair
	KeysErr                  error
	KeysAgent                map[string]bool
	KeysGenerateLayout       *tview.Flex
	KeysGenerateTitleBar     *tview.TextView
	KeysGenerateForm         *tview.Form
	KeysGenerateStatus       *tview.TextView
	KeysGenerateButtons      *tview.Flex
	KeysGenerateCancelBtn    *tview.Button
	KeysGenerateGenerateBtn  *tview.Button
	KeysGenerateAlgorithm    string
	KeysGenerateBits         int
	KeysGenerateFilenameText string
	KeysGenerateCommentText  string
	KeysCopyLayout           *tview.Flex
	KeysCopyTitleBar         *tview.TextView
	KeysCopyForm             *tview.Form
	KeysCopyPickBtn          *tview.Button
	KeysCopyStatus           *tview.TextView
	KeysCopyButtons          *tview.Flex
	KeysCopyCancelBtn        *tview.Button
	KeysCopyGoBtn            *tview.Button
	KeysCopyKeyName          string
	KeysCopyHostText         string
	KeysCopyPortText         string
	KeysCopyUserText         string
	ShellMenuTable           *tview.Table
	ShellMenuTitleBar        *tview.TextView
	ShellMenuLayout          *tview.Flex
	ShellMenuHistoryRows     map[int]remotefs.Connection
	ShellForm                *tview.Form
	ShellHostField           *tview.InputField
	ShellPortField           *tview.InputField
	ShellUserField           *tview.InputField
	ShellAgentCheckbox       *plainFormCheckbox
	ShellCompressionCheckbox *plainFormCheckbox
	ShellVerboseCheckbox     *plainFormCheckbox
	ShellX11Checkbox         *plainFormCheckbox
	ShellStatus              *tview.TextView
	ShellCancelBtn           *tview.Button
	ShellOpenBtn             *tview.Button
	ShellButtons             *tview.Flex
	ShellTitleBar            *tview.TextView
	ShellLayout              *tview.Flex
	ShellSubmit              func(remotefs.Connection)
}

const (
	sshKeysColName = iota
	sshKeysColType
	sshKeysColEncrypted
	sshKeysColAgent
	sshKeysColPerms
	sshKeysColFingerprint
	sshKeysColComment
	sshKeysColNote
)

// sshKeysColumnWidth is each column's own padding floor — never a
// ceiling, the same reasoning firewallColumnWidth's own doc comment
// gives. Comment and Note have no floor: nothing follows either but the
// other, so there's nothing to keep a gap before.
func sshKeysColumnWidth(col int) int {
	switch col {
	case sshKeysColName:
		return 20
	case sshKeysColType:
		return 12
	case sshKeysColEncrypted:
		return 10
	case sshKeysColAgent:
		return 7
	case sshKeysColPerms:
		return 7
	case sshKeysColFingerprint:
		return 52
	default:
		return 0
	}
}

// readSSHKeys and readSSHAgent are package-level swappable vars (the
// same mockable-exec idiom internal/firewall's own detect.go already
// uses), so this screen's own render logic can be exercised without
// ever touching a real ~/.ssh directory or a real running ssh-agent.
var readSSHKeys = sshkeys.ScanKeyPairs
var readSSHAgent = sshkeys.AgentFingerprints

// sshKeysHintEntries is this screen's own bottom hint bar (see
// buildListHint) — a function, not a var, per activityLogHintEntries'
// own doc comment.
func sshKeysHintEntries() []listHintEntry {
	return []listHintEntry{
		{keys: []listHintKey{
			{"↑", simulateKeyOnFocused(tcell.KeyUp)},
			{"↓", simulateKeyOnFocused(tcell.KeyDown)},
		}, label: "move"},
		hintKey("r", "refresh", func(r *Root) { r.reloadSSHKeys() }),
		hintKey("a", "generate key", func(r *Root) { r.openSSHKeysGenerate() }),
		hintKey("c", "copy to server", func(r *Root) { r.openSSHKeysCopy() }),
		hintKey("Esc", "close", func(r *Root) { r.closeSSHKeys() }),
	}
}

// newSSHKeysScreen builds the whole screen once, at startup — the same
// build-once/repopulate-on-open shape newMountsScreen already
// establishes.
func (r *Root) newSSHKeysScreen() {
	r.ssh.KeysTable = tview.NewTable()
	r.ssh.KeysTable.SetBorders(false)
	r.ssh.KeysTable.SetBorderPadding(1, 0, 2, 1)
	r.ssh.KeysTable.SetSelectable(true, false)
	r.ssh.KeysTable.SetFixed(1, 0)
	r.ssh.KeysTable.SetInputCapture(r.captureSSHKeysKey)

	r.ssh.KeysTitleBar = newPlainTitleBar("SSH Keys")
	r.ssh.KeysTitleBar.SetMouseCapture(captureReloadCloseTitleBarMouse(r.ssh.KeysTitleBar, r.reloadSSHKeys, r.closeSSHKeys))

	r.ssh.KeysHint = tview.NewTextView()
	r.ssh.KeysHint.SetWrap(false)
	r.ssh.KeysHint.SetDynamicColors(true)
	sshKeysHintText, hintSpans := buildListHint(r.theme, sshKeysHintEntries())
	r.ssh.KeysHint.SetText(sshKeysHintText)
	r.ssh.KeysHintSpans = hintSpans
	r.ssh.KeysHint.SetMouseCapture(r.captureListHintMouse(r.ssh.KeysHint, &r.ssh.KeysHintSpans))

	r.ssh.KeysScrollbar = newVerticalScrollbarForTable(r, r.ssh.KeysTable)
	sshKeysTableRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.ssh.KeysTable, 0, 1, true).
		AddItem(r.ssh.KeysScrollbar, 1, 0, false)
	sshKeysTableRow.SetMouseCapture(r.ssh.KeysScrollbar.captureMouse)

	r.ssh.KeysLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.ssh.KeysTitleBar, 1, 0, false).
		AddItem(sshKeysTableRow, 0, 1, true).
		AddItem(r.ssh.KeysHint, 1, 0, false)
}

// openSSHKeys shows the SSH Keys screen, freshly read every time — the
// whole point is to reflect ~/.ssh's actual contents right now, the same
// reasoning openMounts' own doc comment gives.
func (r *Root) openSSHKeys() {
	r.reloadSSHKeys()
	r.showOverlay(sshKeysPage, r.ssh.KeysLayout)
}

// closeSSHKeys hides the SSH Keys screen. Nothing to save — a pure
// read-only view, same as Mounts.
func (r *Root) closeSSHKeys() {
	r.hideOverlay()
}

// reloadSSHKeys re-scans ~/.ssh and re-checks the running ssh-agent, then
// re-renders — the screen's own initial population, and "r"'s own manual
// refresh, since a key can be generated, removed, loaded, or unloaded by
// something else entirely (a shell, another admin session) while this
// screen is open.
func (r *Root) reloadSSHKeys() {
	dir := sshkeys.DefaultDir()
	if dir == "" {
		r.ssh.KeysPairs = nil
		r.ssh.KeysErr = errors.New("could not determine the current user's home directory")
		r.ssh.KeysAgent = nil
		r.renderSSHKeys()
		return
	}

	pairs, err := readSSHKeys(dir)
	// A ~/.ssh that doesn't exist yet at all is not this screen's own
	// error to report — it just means no local SSH keys have ever been
	// generated on this host, same as Mounts' own "No real storage
	// mounted." for an edge case rather than a failure.
	if errors.Is(err, os.ErrNotExist) {
		pairs, err = nil, nil
	}
	r.ssh.KeysPairs = pairs
	r.ssh.KeysErr = err
	r.ssh.KeysAgent = readSSHAgent()
	r.renderSSHKeys()
}

// sshKeysTitle renders the title bar's own live summary: how many key
// pairs were found, and whether a running ssh-agent could even be asked
// about them at all.
func sshKeysTitle(pairs []sshkeys.KeyPair, agent map[string]bool, err error) string {
	if err != nil {
		return "SSH Keys — read failed"
	}
	suffix := " · no agent running"
	if agent != nil {
		suffix = " · agent running"
	}
	return fmt.Sprintf("SSH Keys — %d key pair(s)%s", len(pairs), suffix)
}

// renderSSHKeys fills the table: a bold header row, then one row per key
// pair found under ~/.ssh. A failed read shows in place of any rows
// rather than leaving the screen looking like an empty, successful "no
// keys" read — the same "report it, don't swallow it" principle
// renderMounts already follows.
func (r *Root) renderSSHKeys() {
	r.ssh.KeysTable.Clear()

	renderReloadCloseTitleBar(r.ssh.KeysTitleBar, " "+sshKeysTitle(r.ssh.KeysPairs, r.ssh.KeysAgent, r.ssh.KeysErr)+" ", r.lastScreenWidth)

	header := func(col int, text string) {
		r.ssh.KeysTable.SetCell(0, col,
			tview.NewTableCell(padRight(text, sshKeysColumnWidth(col))).
				SetTextColor(r.theme.Text).
				SetAttributes(tcell.AttrBold).
				SetSelectable(false))
	}
	header(sshKeysColName, "Name")
	header(sshKeysColType, "Type")
	header(sshKeysColEncrypted, "Encrypted")
	header(sshKeysColAgent, "Agent")
	header(sshKeysColPerms, "Perms")
	header(sshKeysColFingerprint, "Fingerprint")
	header(sshKeysColComment, "Comment")
	header(sshKeysColNote, "Note")

	// See showTablePlaceholder's own doc comment for why every early
	// return below goes through it rather than a bare SetCell — it's the
	// fix for a real, reported freeze, not just a message.
	if r.ssh.KeysErr != nil {
		showTablePlaceholder(r.ssh.KeysTable, r.ssh.KeysErr.Error(), r.theme.EntryError)
		return
	}
	if len(r.ssh.KeysPairs) == 0 {
		showTablePlaceholder(r.ssh.KeysTable, "No SSH key pairs found in ~/.ssh.", r.theme.PlaceholderText)
		return
	}

	// Real, selectable rows exist again — see showTablePlaceholder's own
	// doc comment for why this isn't optional cosmetic tidiness.
	enableTableSelection(r.ssh.KeysTable)

	for i, kp := range r.ssh.KeysPairs {
		r.renderSSHKeysRow(i+1, kp)
	}

	// Keep the cursor in range after a refresh changed how many rows
	// there are — the same restraint renderMounts' own tail already
	// shows.
	if row, _ := r.ssh.KeysTable.GetSelection(); row < 1 || row > len(r.ssh.KeysPairs) {
		r.ssh.KeysTable.Select(1, 0)
	}
}

// renderSSHKeysRow fills one key pair's own row.
func (r *Root) renderSSHKeysRow(row int, kp sshkeys.KeyPair) {
	cell := func(col int, text string, color tcell.Color) {
		r.ssh.KeysTable.SetCell(row, col,
			tview.NewTableCell(padRight(text, sshKeysColumnWidth(col))).
				SetTextColor(color).SetSelectable(true))
	}

	cell(sshKeysColName, kp.Name, r.theme.Text)
	cell(sshKeysColType, sshKeysTypeLabel(kp), r.theme.Text)

	if !kp.HasPrivate {
		cell(sshKeysColEncrypted, "–", r.theme.MutedTextColor)
		cell(sshKeysColPerms, "–", r.theme.MutedTextColor)
	} else {
		// statusGlyphText, not checkboxText — whether a key is
		// encrypted is a fact about the key file, not a clickable
		// setting, so it gets the same ✔/✘ Sessions' own Status column
		// uses for the identical reason instead of the ●/○ pair
		// reserved for an actual checkbox. Color unchanged (theme.Text,
		// same as Name/Type) — see statusGlyphText's own doc comment on
		// why this stays plain rather than green/red.
		cell(sshKeysColEncrypted, statusGlyphText(kp.Encrypted), r.theme.Text)
		permsColor := r.theme.Text
		if kp.PrivatePermissiveWarning {
			permsColor = r.theme.WarningText
		}
		cell(sshKeysColPerms, fmt.Sprintf("%03o", kp.PrivateMode.Perm()), permsColor)
	}

	cell(sshKeysColAgent, sshKeysAgentGlyph(kp, r.ssh.KeysAgent), sshKeysAgentColor(kp, r.ssh.KeysAgent, r.theme))
	cell(sshKeysColFingerprint, kp.Fingerprint, r.theme.Text)
	cell(sshKeysColComment, kp.Comment, r.theme.PlaceholderText)
	cell(sshKeysColNote, sshKeysNote(kp), r.theme.WarningText)
}

// sshKeysTypeLabel renders a key's own algorithm and size the same way
// `ssh-keygen -lf` itself would (e.g. "rsa 2048", "ed25519 256"), or
// "unknown" when neither the public nor an accessible private half ever
// told us.
func sshKeysTypeLabel(kp sshkeys.KeyPair) string {
	if kp.Type == "" {
		return "unknown"
	}
	if kp.Bits == 0 {
		return kp.Type
	}
	return fmt.Sprintf("%s %d", kp.Type, kp.Bits)
}

// sshKeysAgentGlyph/sshKeysAgentColor render the Agent column: loaded
// (✔, green), not loaded (✘, muted — absence here is completely normal,
// never an error), or unknown (–, muted) when there's no fingerprint to
// even look up (an encrypted key with no ".pub" file) or no agent was
// reachable at all to ask — the same three-state shape sessionsStatusGlyph/
// sessionsStatusColor already establish for Sessions' own Status column.
func sshKeysAgentGlyph(kp sshkeys.KeyPair, agent map[string]bool) string {
	if kp.Fingerprint == "" || agent == nil {
		return "–"
	}
	if agent[kp.Fingerprint] {
		return "✔"
	}
	return "✘"
}

func sshKeysAgentColor(kp sshkeys.KeyPair, agent map[string]bool, theme config.ResolvedTheme) tcell.Color {
	if kp.Fingerprint == "" || agent == nil {
		return theme.MutedTextColor
	}
	if agent[kp.Fingerprint] {
		return theme.EntryExecutable
	}
	return theme.MutedTextColor
}

// sshKeysNote surfaces whatever this row's own scan couldn't cleanly
// resolve: a missing half of the pair, or a private key that failed to
// parse outright (corrupt or in a format this app's own dependency,
// golang.org/x/crypto/ssh, doesn't support) — never silently dropped
// from the list.
func sshKeysNote(kp sshkeys.KeyPair) string {
	if kp.ParseError != "" {
		return "parse error: " + kp.ParseError
	}
	if kp.HasPrivate && !kp.HasPublic {
		return "no public key file"
	}
	if kp.HasPublic && !kp.HasPrivate {
		return "no private key file"
	}
	return ""
}

// captureSSHKeysKey is the SSH Keys screen's own key handling: Escape
// closes it, "r" re-scans ~/.ssh and re-checks the agent — the same two
// keys captureMountsKey already handles, for the same reasons.
func (r *Root) captureSSHKeysKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeSSHKeys()
		return nil
	}
	// Tab/Backtab reach the AI Consigliere sidebar — see
	// startConsigliereChord's own doc comment in consigliere.go and
	// captureLogAuditSelectionTableKey's own identical shape.
	// ssh.KeysTable is this screen's only real focus target.
	if r.consigliere.Visible {
		if event.Key() == tcell.KeyTab {
			r.app.SetFocus(r.consigliere.Input)
			return nil
		}
		if event.Key() == tcell.KeyBacktab {
			if r.consigliere.Scrollbar.maxOffset() > 0 {
				r.app.SetFocus(r.consigliere.History)
			} else {
				r.app.SetFocus(r.consigliere.Input)
			}
			return nil
		}
	}
	if event.Key() == tcell.KeyRune && event.Rune() == 'r' {
		r.reloadSSHKeys()
		return nil
	}
	if event.Key() == tcell.KeyRune && event.Rune() == 'a' {
		r.openSSHKeysGenerate()
		return nil
	}
	if event.Key() == tcell.KeyRune && event.Rune() == 'c' {
		r.openSSHKeysCopy()
		return nil
	}
	// "gC" reaches the AI Consigliere from here too — see
	// startConsigliereChord's own doc comment in consigliere.go.
	if event.Key() == tcell.KeyRune && event.Rune() == 'g' {
		r.startConsigliereChord(nil)
		return nil
	}
	return event
}

// applySSHKeysTheme themes the SSH Keys screen — split out of applyTheme
// (see its own doc comment). Guarded the same way applyMountsTheme is.
func (r *Root) applySSHKeysTheme(theme config.ResolvedTheme) {
	if r.ssh.KeysTable == nil {
		return
	}
	// PanelBackground, not SurfaceBackground — see mounts.Layout's own
	// identical comment (mounts.go) for the full reasoning.
	r.ssh.KeysLayout.SetBackgroundColor(theme.PanelBackground)
	r.ssh.KeysTable.SetBackgroundColor(theme.PanelBackground)
	// Without this, tview's own default selected-cell style just
	// reverses each cell's own foreground into a background — fine for
	// Name/Type/Fingerprint (all plain Text), but the Agent/Comment/Note
	// columns carry their own semantic color (green ✔, muted placeholder
	// text, warning orange) that then paints the highlighted row in
	// mismatched patches instead of one consistent selection bar, live-
	// confirmed against a real run. The same fix connectionMenuTable/
	// sed.PreviewTable/tabSwitcher already apply for the identical reason.
	r.ssh.KeysTable.SetSelectedStyle(tcell.StyleDefault.
		Background(theme.SelectionBackground).
		Foreground(theme.TextColor))

	// FocusedBackground, fixed — same reasoning mounts.TitleBar's own
	// fixed FocusedBackground follows.
	r.ssh.KeysTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.ssh.KeysTitleBar.SetTextColor(theme.TextColor)
	// SurfaceBackground/TextColor, matching the main screen's own
	// buttonBar/statusBar — see mounts.Hint's own identical comment.
	r.ssh.KeysHint.SetBackgroundColor(theme.SurfaceBackground)
	r.ssh.KeysHint.SetTextColor(theme.TextColor)
	sshKeysHintText, hintSpans := buildListHint(theme, sshKeysHintEntries())
	r.ssh.KeysHint.SetText(sshKeysHintText)
	r.ssh.KeysHintSpans = hintSpans

	r.renderSSHKeys() // cell colors are baked in per cell, not looked up live at draw time
}
