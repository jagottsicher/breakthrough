package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// dialFunc is remotefs.Dial, a package-level var so a test can fake a
// connection attempt without a real network round trip — the same
// reasoning sedPreviewFunc/searchRun already establish for anything a
// background goroutine calls that a test needs to observe or control.
var dialFunc = remotefs.Dial

// connectDialogState holds the "Connect" dialog's widgets, current
// mode, and in-flight-attempt state.
type connectDialogState struct {
	Form            *tview.Form
	HostField       *tview.InputField
	PortField       *tview.InputField
	UserField       *tview.InputField
	PasswordField   *tview.InputField
	IdentifierField *tview.InputField
	TagsField       *tview.InputField
	Status          *tview.TextView
	CancelBtn       *tview.Button
	OKBtn           *tview.Button
	ConnectBtn      *tview.Button
	Mode            connectDialogMode
	Buttons         *tview.Flex
	TitleBar        *tview.TextView
	Layout          *tview.Flex
	Cancel          context.CancelFunc
	AnimFrame       int
	// PendingSkipHostKey/PendingSkipHostKeyConn back the "Connect
	// anyway" retry after a HostKeyMismatchError (see finishConnect/
	// runConnect below) — set only once finishConnect has actually
	// shown the mismatch, consumed (and cleared) the instant runConnect
	// reads it, and only ever honored for the exact same Connection it
	// was shown for, so editing a field first can never silently carry
	// an accepted bypass over to a different host.
	PendingSkipHostKey     bool
	PendingSkipHostKeyConn remotefs.Connection
}

// connectDialogMode decides which buttons setConnectButtonsMode shows
// for this particular open of the Connect dialog.
type connectDialogMode int

const (
	// connectDialogModeNew is a brand-new connection (the dropdown's
	// own "+New" row) or the automatic, immediate reconnect-by-row-
	// click path (activateConnectionMenuCell's default case) — shows
	// all three buttons, Cancel/OK/Connect, with OK the default action
	// (Enter in the last field runs it, not Connect) per the user's
	// own explicit request: typing fresh data and pressing Enter must
	// save it, not immediately dial it. The row-click path is
	// unaffected by any of this — it calls runConnect directly, right
	// after opening, never through a button or Enter at all.
	connectDialogModeNew connectDialogMode = iota
	// connectDialogModeEdit is editConnectionHistoryRow's own "✎" path
	// — an existing entry whose Host/Port/User/Identifier/Tags can be
	// fixed. Per the user's own explicit request, this path never
	// offers a way to dial at all, only Cancel/OK.
	connectDialogModeEdit
)

// openConnectDialog shows the "Connect" form, prefilled from prefill —
// the zero Connection for a blank "New connection…" open (see
// connectionmenu.go), or a history entry's own Connection when
// reconnecting to it or editing it (mode tells the two apart — see
// connectDialogMode's own doc comment). Never touches
// connect.PasswordField: a password is never persisted (see
// remotefs.Connection's own doc comment), so there's nothing to
// prefill it with even when reconnecting to an entry that originally
// needed one.
func (r *Root) openConnectDialog(prefill remotefs.Connection, mode connectDialogMode) {
	r.connect.HostField.SetText(prefill.Host)
	if prefill.Port != 0 {
		r.connect.PortField.SetText(strconv.Itoa(prefill.Port))
	} else {
		r.connect.PortField.SetText("")
	}
	r.connect.UserField.SetText(prefill.User)
	r.connect.PasswordField.SetText("")
	r.connect.IdentifierField.SetText(prefill.Identifier)
	r.connect.TagsField.SetText(strings.Join(prefill.Tags, ", "))
	r.setConnectStatus("", r.theme.Text)
	r.resetConnectAnywayState()
	r.setConnectButtonsMode(mode)

	// height covers connect.TitleBar's own row plus connect.Layout's three
	// stacked pieces (connect.Form's own 13, see newConnectLayout's own
	// doc comment on why; connect.Status's 1; connect.Buttons' 1) —
	// checked against a real render, not guessed; a shorter value
	// silently clipped the bottom rows (see openSedReplace's own
	// identical comment on its own dialog).
	width, height := 64, 16
	_, _, screenWidth, screenHeight := r.GetRect()
	if width > screenWidth-4 {
		width = screenWidth - 4
	}
	if height > screenHeight-4 {
		height = screenHeight - 4
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	r.connect.Layout.SetRect(x, y, width, height)
	r.renderConnectTitleBar(width)
	r.showOverlay(connectDialogPage, r.connect.Layout)
}

// renderConnectTitleBar sets connect.TitleBar's own text to "New SFTP
// Connection", padded out to width columns, with a close glyph
// (toolWindowCloseGlyph, the same one toolWindow's/Properties'/Help's
// own title bars already use) in its own top-right corner — the exact
// same "mouse-clickable close button that behaves exactly like
// Cancel" shape renderPropertiesTitleBar already establishes, per the
// user's own explicit request.
func (r *Root) renderConnectTitleBar(width int) {
	const label = " New SFTP Connection "
	closeCol := toolWindowCloseButtonCol(0, width)
	padding := closeCol - tview.TaggedStringWidth(label)
	if padding < 0 {
		padding = 0
	}
	r.connect.TitleBar.SetText(label + strings.Repeat(" ", padding) + string(toolWindowCloseGlyph) + " ")
}

// captureConnectTitleBarMouse detects a click on the close glyph
// renderConnectTitleBar's own text embeds — the dialog itself is
// never dragged (unlike Properties'/toolWindow's own title bars), so
// this is just the close-click check on its own, with no drag-start
// branch needed alongside it.
func (r *Root) captureConnectTitleBarMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action != tview.MouseLeftDown {
		return action, event
	}
	x, y := event.Position()
	wx, wy, width, _ := r.connect.Layout.GetRect()
	if y == wy && x == wx+toolWindowCloseButtonCol(0, width) {
		r.cancelConnect()
		return tview.MouseConsumed, nil
	}
	return action, event
}

// newConnectForm builds the (initially empty) "Connect" form once,
// from NewRoot — see resetSedForm's own doc comment on the equivalent
// choice there for why a fixed field set like this one doesn't need
// the same "rebuilt fresh on every open" treatment Sed Replace's own
// variable field set does.
func (r *Root) newConnectForm() *tview.Form {
	f := tview.NewForm()
	f.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			r.cancelConnect()
			return nil
		}
		return event
	})

	r.connect.HostField = tview.NewInputField().SetLabel("Host")
	f.AddFormItem(r.connect.HostField)
	r.connect.PortField = tview.NewInputField().SetLabel("Port (default 22)")
	f.AddFormItem(r.connect.PortField)
	r.connect.UserField = tview.NewInputField().SetLabel("User")
	f.AddFormItem(r.connect.UserField)
	// A short label, deliberately: tview.Form sizes the shared label
	// column to its widest field's own label, so a long explanatory one
	// here would squeeze every other field's input area too — the
	// explanation instead lives in the placeholder text, only visible
	// (as intended) once the field itself has real width to show it in.
	r.connect.PasswordField = tview.NewInputField().SetLabel("Password")
	r.connect.PasswordField.SetPlaceholder("only tried if key/agent auth doesn't apply")
	r.connect.PasswordField.SetMaskCharacter('*')
	f.AddFormItem(r.connect.PasswordField)

	// A short label like every other field here — the character-set/
	// length rule itself lives in the placeholder, only visible once
	// the field has real width to show it in (same reasoning
	// connect.PasswordField's own doc comment gives for its label).
	r.connect.IdentifierField = tview.NewInputField().SetLabel("Identifier")
	r.connect.IdentifierField.SetPlaceholder("optional, a-z/0-9/_/-/space, max 32 chars")
	r.connect.IdentifierField.SetAcceptanceFunc(connectIdentifierAcceptanceFunc)
	// Collapses internal whitespace runs and trims the ends only once
	// the field loses focus, never live — the same "don't move the text
	// out from under a still-typing cursor" reasoning connect.TagsField's
	// own SetBlurFunc already documents, just for a single free-text
	// value instead of a comma-split list.
	r.connect.IdentifierField.SetBlurFunc(func() { r.normalizeConnectIdentifierField() })
	f.AddFormItem(r.connect.IdentifierField)

	r.connect.TagsField = tview.NewInputField().SetLabel("Tags")
	r.connect.TagsField.SetPlaceholder("optional, comma-separated, max 10")
	r.connect.TagsField.SetAcceptanceFunc(connectTagsAcceptanceFunc)
	// Normalizes (trims, drops empties, dedupes, sorts, caps — see
	// remotefs.ParseTags) only once the field actually loses focus, not
	// on every keystroke: sorting/deduplicating live, mid-keystroke,
	// would move the text the user is still typing right out from
	// under their own cursor. SetBlurFunc fires regardless of *why*
	// focus left (our own Tab-to-Cancel below, tview's native
	// Backtab/click navigation, or connect.Form's own Escape capture
	// dismissing the whole dialog) — a single hook rather than
	// duplicating the normalization at every one of those exits.
	r.connect.TagsField.SetBlurFunc(func() { r.normalizeConnectTagsField() })
	f.AddFormItem(r.connect.TagsField)

	// Tab/Enter on the form's own last field would otherwise just wrap
	// back to its first one instead of ever reaching connect.CancelBtn/
	// connect.ConnectBtn: verified directly against tview's own form.go,
	// not guessed — Form.Focus unconditionally calls
	// item.SetFinishedFunc on every item whenever the form itself gains
	// focus, silently overwriting any SetDoneFunc set here beforehand,
	// so the only place left to actually intercept Tab is a
	// SetInputCapture, which runs before an item's own native handling
	// at all. Enter goes one step further and submits the form
	// outright, the same "last field, Enter submits" convenience a real
	// login form has — the same reasoning this is only wired on the
	// *last* field (now connect.TagsField, since it was added after
	// connect.IdentifierField): every other field's own Tab/Enter
	// already does the right thing (move to the next field) via the
	// form's own default handling.
	r.connect.TagsField.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			r.app.SetFocus(r.connect.CancelBtn)
			return nil
		case tcell.KeyEnter:
			r.runConnectOK()
			return nil
		}
		return event
	})

	return f
}

// normalizeConnectTagsField rewrites connect.TagsField's own text to
// remotefs.ParseTags' normalized form (trimmed, deduplicated, sorted,
// capped) — see connect.TagsField's own SetBlurFunc for why this runs
// only once the field loses focus, never live.
func (r *Root) normalizeConnectTagsField() {
	r.connect.TagsField.SetText(strings.Join(remotefs.ParseTags(r.connect.TagsField.GetText()), ", "))
}

// normalizeConnectIdentifier collapses raw's internal whitespace runs
// to a single space and trims its ends, via strings.Fields' own
// "split on whitespace, drop empties" behavior — so "  web   server "
// and "web server" persist and render identically, and anything that
// was only whitespace (now that space is a valid Identifier rune, see
// isConnectIdentifierRune) normalizes to "", the same "unset" value
// connectionMenuIdentifierSuffix and upsertHistory's own inherit rule
// already treat Identifier's zero value as.
func normalizeConnectIdentifier(raw string) string {
	return strings.Join(strings.Fields(raw), " ")
}

// normalizeConnectIdentifierField rewrites connect.IdentifierField's own
// text to normalizeConnectIdentifier's normalized form — see
// connect.IdentifierField's own SetBlurFunc for why this runs only once
// the field loses focus, never live.
func (r *Root) normalizeConnectIdentifierField() {
	r.connect.IdentifierField.SetText(normalizeConnectIdentifier(r.connect.IdentifierField.GetText()))
}

// connectIdentifierAcceptanceFunc enforces the Identifier field's own
// character set (letters, digits, underscore, hyphen, space — see
// isConnectIdentifierRune) and 32-character cap live, as each
// keystroke (or paste) arrives — the
// same SetAcceptanceFunc-as-validator shape chmodEditField/
// properties.EditField already use elsewhere in this app. tview's own
// InputField has no SetMaxLength in this version (verified directly
// against inputfield.go, not guessed — see chmodEditField's own doc
// comment, which found this gap first), so rejecting anything past the
// cap here is the actual mechanism. Checks every rune in textToCheck,
// not just lastChar, so a paste that introduces a disallowed character
// partway through is rejected outright rather than silently admitting
// everything before it.
func connectIdentifierAcceptanceFunc(textToCheck string, lastChar rune) bool {
	if len(textToCheck) > connectIdentifierMaxLength {
		return false
	}
	for _, r := range textToCheck {
		if !isConnectIdentifierRune(r) {
			return false
		}
	}
	return true
}

// connectIdentifierMaxLength caps the Identifier field at a short,
// glance-readable length — long enough to tell several similarly-named
// servers apart, short enough to never dominate a connection history
// row next to the actual Host/User it's labeling.
const connectIdentifierMaxLength = 32

// isConnectIdentifierRune reports whether r is allowed in an
// Identifier: exactly remotefs.IsValidTagRune's own character set
// (ASCII letters, digits, underscore, hyphen, space) — Identifier used
// to exclude space on the theory that a rendered row's own
// "label, Identifier" shape needed to avoid containing its own
// separator, but connectionMenuIdentifierSuffix actually renders it
// parenthesized ("  (name)"), never comma-separated, so that reasoning
// never applied to the actual rendering and a free-text name like
// "build runner 1" has no real reason to be forced into
// "build-runner-1". Deliberately the same set as Tags, not a superset
// or subset of it: Identifier is a single free-text field rather than
// Tags' own comma-split list, but the set of individually-allowed
// characters is identical, so reusing IsValidTagRune here keeps the
// two from silently drifting apart.
func isConnectIdentifierRune(r rune) bool {
	return remotefs.IsValidTagRune(r)
}

// connectTagsFieldMaxLength is a generous live-typing cap on the Tags
// field's raw, comma-separated text — not a tight fit around 10 tags
// of 32 chars each, just a backstop against unbounded growth while
// typing. remotefs.ParseTags enforces the real per-tag length and
// count caps at commit time (normalizeConnectTagsField) regardless of
// how much raw text got this far.
const connectTagsFieldMaxLength = 400

// connectTagsAcceptanceFunc enforces the Tags field's own live
// character set: remotefs.IsValidTagRune's letters/digits/underscore/
// hyphen/space, plus "," itself as the only separator a raw Tags field
// needs that a single tag never contains (see
// connectTagsFieldMaxLength and remotefs.ParseTags, which is what
// actually turns this raw text into a well-formed Tags value once the
// field loses focus) — the same per-rune, paste-safe check
// connectIdentifierAcceptanceFunc already establishes for Identifier,
// just with a wider character set and no attempt to validate
// individual tags here (ParseTags' own job, not a single keystroke's).
func connectTagsAcceptanceFunc(textToCheck string, lastChar rune) bool {
	if len(textToCheck) > connectTagsFieldMaxLength {
		return false
	}
	for _, r := range textToCheck {
		if r != ',' && !remotefs.IsValidTagRune(r) {
			return false
		}
	}
	return true
}

// newConnectButtons builds the real Cancel/OK/Connect button trio —
// the same established look newChmodButtons/newSearchButtons/
// newPropertiesButtons/newDuplicateButtons all already give their own
// dialogs, per the user's own explicit, repeated request that every
// dialog's own action row match it rather than Sed Replace's own
// vertical two-item List (see newDuplicateButtons' own doc comment for
// the first time this exact correction was made). All three widgets
// are built once, here, and shared across every open — only which of
// them setConnectButtonsMode actually puts in the Flex (and therefore
// where Tab/Backtab lead) changes per mode, not the widgets
// themselves.
func (r *Root) newConnectButtons() *tview.Flex {
	r.connect.CancelBtn = tview.NewButton("Cancel").SetSelectedFunc(r.cancelConnect)
	r.connect.OKBtn = tview.NewButton("OK").SetSelectedFunc(r.runConnectOK)
	r.connect.ConnectBtn = tview.NewButton("Connect").SetSelectedFunc(r.runConnect)
	r.connect.CancelBtn.SetInputCapture(spaceAlsoActivates(r.cancelConnect))
	r.connect.OKBtn.SetInputCapture(spaceAlsoActivates(r.runConnectOK))
	r.connect.ConnectBtn.SetInputCapture(spaceAlsoActivates(r.runConnect))

	r.connect.Buttons = tview.NewFlex().SetDirection(tview.FlexColumn)
	r.setConnectButtonsMode(connectDialogModeNew)
	return r.connect.Buttons
}

// setConnectButtonsMode rebuilds connect.Buttons' own contents and
// Tab/Backtab wiring for mode — called from openConnectDialog on every
// single open, not just once from newConnectButtons: the same three
// button widgets are shared across every open, so both which of them
// are actually in the Flex *and* where their own Tab/Backtab lead have
// to be set together here, every time. Wiring SetExitFunc only once in
// newConnectButtons would leave edit mode's own Tab cycle still
// pointing at connect.ConnectBtn, a button that by then isn't even in
// the Flex anymore — keeping both concerns in the one place they can't
// drift apart from each other.
//
// Equal proportion (0, 1) each, nothing else, so however many buttons
// are present this time fill the whole row edge to edge, split evenly
// — the same shape newChmodButtons/newDuplicateButtons already use for
// their own two-button case.
func (r *Root) setConnectButtonsMode(mode connectDialogMode) {
	r.connect.Mode = mode
	r.connect.Buttons.Clear()
	r.connect.Buttons.AddItem(r.connect.CancelBtn, 0, 1, false)
	r.connect.Buttons.AddItem(r.connect.OKBtn, 0, 1, false)

	// Escape always cancels, the same as everywhere else in this
	// dialog, regardless of mode.
	r.connect.CancelBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.connect.OKBtn)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.connect.TagsField)
		case tcell.KeyEscape:
			r.cancelConnect()
		}
	})

	if mode == connectDialogModeEdit {
		// No Connect button at all — per the user's own explicit
		// request, editing an entry never offers a way to dial from
		// here. OK's own Tab wraps straight back to the form, the
		// same "last real stop, wrap to Host" shape the three-button
		// case below gives Connect instead.
		r.connect.OKBtn.SetExitFunc(func(key tcell.Key) {
			switch key {
			case tcell.KeyTab:
				// r.connect.Form.SetFocus(0), not a direct
				// r.app.SetFocus(r.connect.HostField) — see
				// connect.ConnectBtn's own identical fix below for the
				// real, user-reported bug this works around.
				r.connect.Form.SetFocus(0)
				r.app.SetFocus(r.connect.Form)
			case tcell.KeyBacktab:
				r.app.SetFocus(r.connect.CancelBtn)
			case tcell.KeyEscape:
				r.cancelConnect()
			}
		})
		return
	}

	r.connect.Buttons.AddItem(r.connect.ConnectBtn, 0, 1, false)
	r.connect.OKBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.connect.ConnectBtn)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.connect.CancelBtn)
		case tcell.KeyEscape:
			r.cancelConnect()
		}
	})
	r.connect.ConnectBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			// A real, user-reported bug: r.app.SetFocus(r.connect.HostField)
			// here used to give the Host field real keyboard focus
			// correctly, but silently left connect.Form's own internal
			// focusedElement counter stale at whatever field was last
			// focused *through the form's own native Tab/Backtab* (its
			// own SetFinishedFunc machinery, verified directly against
			// tview's own form.go — Form.Focus never learns about a
			// focus change made by calling Application.SetFocus
			// directly on one of its items from outside). The next
			// Backtab the user pressed, from Host, then computed its
			// target from that stale index rather than from Host's own
			// position — landing on some other field entirely instead
			// of wrapping back to Tags. Form.SetFocus(index) (a real
			// tview.Form API, not guessed) updates that same counter
			// unconditionally, so delegating through the form itself —
			// rather than naming the field directly — keeps it correct.
			r.connect.Form.SetFocus(0)
			r.app.SetFocus(r.connect.Form)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.connect.OKBtn)
		case tcell.KeyEscape:
			r.cancelConnect()
		}
	})
}

// newConnectLayout wraps connect.TitleBar over the form, a one-line
// status area (blank until runConnect has something to say — an
// in-progress spinner, or an error), and the Cancel/Connect buttons —
// the same title-bar-over-content shape newSedLayout already
// establishes.
func (r *Root) newConnectLayout() *tview.Flex {
	// Text/close glyph are actually set by renderConnectTitleBar, once
	// width is known (openConnectDialog) — this initial text is
	// never actually seen, only newPlainTitleBar's own SetWrap(false)
	// matters here.
	r.connect.TitleBar = newPlainTitleBar("New SFTP Connection")
	r.connect.TitleBar.SetMouseCapture(r.captureConnectTitleBarMouse)
	r.connect.Status = tview.NewTextView().SetDynamicColors(true)
	// 13 rows for connect.Form: tview.NewForm() reserves a 1-row border
	// padding top and bottom of its own (SetBorderPadding(1,1,1,1),
	// independent of whether a visible border line is drawn at all —
	// verified directly against tview's own form.go, not guessed, after
	// a real render silently clipped the fourth field otherwise) on top
	// of its own six one-row fields (Host/Port/User/Password/
	// Identifier/Tags) plus itemPadding's default 1-row gap between
	// each: 2 (padding) + 6 (fields) + 5 (gaps) = 13.
	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.connect.Form, 13, 0, true).
		AddItem(r.connect.Status, 1, 0, false).
		AddItem(r.connect.Buttons, 1, 0, false)
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.connect.TitleBar, 1, 0, false).
		AddItem(content, 0, 1, true)
}

// resetConnectAnywayState clears whatever "Connect anyway" state a
// prior HostKeyMismatchError left behind (see finishConnect) and puts
// connect.ConnectBtn's own label back to plain "Connect" — called on
// every fresh dialog open and the instant runConnect consumes a
// pending bypass, so it can never linger stale across an unrelated
// attempt or a different host.
func (r *Root) resetConnectAnywayState() {
	r.connect.PendingSkipHostKey = false
	r.connect.PendingSkipHostKeyConn = remotefs.Connection{}
	r.connect.ConnectBtn.SetLabel("Connect")
}

func (r *Root) setConnectStatus(text string, color tcell.Color) {
	r.connect.Status.SetTextColor(color)
	r.connect.Status.SetText(text)
}

func (r *Root) cancelConnect() {
	r.cancelConnectAttempt()
	r.hideOverlay()
}

// cancelConnectAttempt stops an in-flight Dial, if any, without
// necessarily closing the dialog — split out from cancelConnect so
// runConnect can call just this half to replace an earlier, still-
// running attempt instead of piling a second one on top of it.
func (r *Root) cancelConnectAttempt() {
	if r.connect.Cancel != nil {
		r.connect.Cancel()
		r.connect.Cancel = nil
	}
}

// connectFormHostPortUser validates and returns Host/Port/User from
// the form's own fields — shared between runConnect (which also
// dials) and runConnectOK (which only saves), so the two can never
// validate differently. Sets an error status and returns ok=false on
// an invalid field (a missing Host, an out-of-range Port), the same
// messages either action already showed inline before this was
// pulled out.
func (r *Root) connectFormHostPortUser() (host string, port int, user string, ok bool) {
	host = strings.TrimSpace(r.connect.HostField.GetText())
	if host == "" {
		r.setConnectStatus("Host is required.", r.theme.CriticalText)
		return "", 0, "", false
	}
	if text := strings.TrimSpace(r.connect.PortField.GetText()); text != "" {
		p, err := strconv.Atoi(text)
		if err != nil || p <= 0 || p > 65535 {
			r.setConnectStatus("Port must be a number from 1 to 65535.", r.theme.CriticalText)
			return "", 0, "", false
		}
		port = p
	}
	user = strings.TrimSpace(r.connect.UserField.GetText())
	if user == "" {
		user = currentUsername()
	}
	return host, port, user, true
}

// connectFormIdentifierAndTags reads Identifier/Tags from the form's
// own fields — shared the same way connectFormHostPortUser is, between
// runConnect and runConnectOK.
func (r *Root) connectFormIdentifierAndTags() (identifier string, tags []string) {
	// connect.IdentifierField's own text is already normalized by the
	// time either button can even be reached (normalizeConnectIdentifierField
	// fires on blur, same as Tags below), but normalizeConnectIdentifier
	// is idempotent on already-normalized input, so running it again
	// here is cheap insurance rather than trusting that invariant
	// blindly — mirrors the identical reasoning Tags already gives
	// just below.
	identifier = normalizeConnectIdentifier(r.connect.IdentifierField.GetText())
	// connect.TagsField's own text is already normalized by the time
	// either button can even be reached (normalizeConnectTagsField
	// fires on blur, and every exit from the field — Tab to Cancel, or
	// Enter — blurs it first), but ParseTags is idempotent on
	// already-normalized input, so running it again here is cheap
	// insurance rather than trusting that invariant blindly.
	tags = remotefs.ParseTags(r.connect.TagsField.GetText())
	return identifier, tags
}

// runConnectOK is the "OK" button's own action, and the default one
// for a +New or Edit dialog (anything other than the automatic,
// immediate reconnect-by-row-click path, which calls runConnect
// directly right after opening and never goes through a button or
// Enter at all — see activateConnectionMenuCell's own default case):
// validates and saves the typed fields into history via
// remotefs.SaveConnection, without ever dialing — see that function's
// own doc comment for why a brand-new entry it creates is marked
// Untested rather than excluded the way a never-attempted
// RecordAttempt failure would be.
func (r *Root) runConnectOK() {
	host, port, user, ok := r.connectFormHostPortUser()
	if !ok {
		return
	}
	identifier, tags := r.connectFormIdentifierAndTags()
	conn := remotefs.Connection{Host: host, Port: port, User: user, Identifier: identifier, Tags: tags}

	if err := remotefs.SaveConnection(conn); err != nil {
		r.setConnectStatus(err.Error(), r.theme.CriticalText)
		return
	}
	r.activityLog.Action(activitylog.CategoryRemote, fmt.Sprintf("saved %s", conn.Label()))
	r.hideOverlay()
}

// runConnect reads the form, validates just enough to dial (a host is
// required; a malformed port is rejected before ever touching the
// network), and attempts the connection in the background — see
// animateConnectProgress for the "in progress" status line and
// finishConnect for every one of its outcomes. r.panel is snapshotted
// immediately, not re-read once Dial returns: the user is free to
// switch tabs or close this one while a slow connection attempt is
// still in flight, and the result belongs to whichever tab was active
// when Connect was actually clicked (see finishConnect's own
// hasTab guard for the case that tab is gone by the time it matters).
func (r *Root) runConnect() {
	host, port, user, ok := r.connectFormHostPortUser()
	if !ok {
		return
	}
	var passwordPrompt remotefs.PasswordPrompt
	if password := r.connect.PasswordField.GetText(); password != "" {
		passwordPrompt = func() (string, error) { return password, nil }
	}
	identifier, tags := r.connectFormIdentifierAndTags()

	conn := remotefs.Connection{Host: host, Port: port, User: user, Identifier: identifier, Tags: tags}

	// skipHostKeyVerification only ever fires for the exact Connection a
	// prior attempt's own HostKeyMismatchError was just shown for (see
	// finishConnect) — consumed here, immediately, so a second,
	// unrelated click of this same button (after editing a field, or
	// after this very attempt resolves one way or the other) never
	// silently repeats the bypass. See
	// DialOptions.SkipHostKeyVerification's own doc comment for why
	// this is never anything but a deliberate, one-off, unsaved retry.
	skipHostKeyVerification := r.connect.PendingSkipHostKey && r.connect.PendingSkipHostKeyConn.Equal(conn)
	r.resetConnectAnywayState()

	r.cancelConnectAttempt()
	ctx, cancel := context.WithCancel(context.Background())
	r.connect.Cancel = cancel
	r.connect.AnimFrame = 0
	r.setConnectStatus(hashAnimationFrames[0]+" Connecting…", r.theme.Text)

	onPanic := func() { r.cancelConnectAttempt() }
	r.safeGo("connect progress animation", onPanic, func() { r.animateConnectProgress(ctx) })

	panel := r.panel
	r.safeGo("connect", onPanic, func() {
		client, err := dialFunc(ctx, remotefs.DialOptions{
			Connection:              conn,
			Auth:                    remotefs.AuthOptions{AgentSocket: os.Getenv("SSH_AUTH_SOCK"), Password: passwordPrompt},
			HostKeyPrompt:           r.askHostKeyTrust,
			SkipHostKeyVerification: skipHostKeyVerification,
		})
		r.app.QueueUpdateDraw(func() {
			if ctx.Err() != nil {
				return
			}
			r.finishConnect(panel, conn, client, err)
		})
	})
}

// animateConnectProgress advances connect.AnimFrame every
// hashAnimationInterval until ctx is done — the same ticker-driven "in
// progress" animation animateSedPreviewProgress/animateSearchProgress
// already use, reusing hashAnimationFrames rather than a second,
// separately defined set.
func (r *Root) animateConnectProgress(ctx context.Context) {
	ticker := time.NewTicker(hashAnimationInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			r.app.QueueUpdateDraw(func() {
				if ctx.Err() != nil {
					return
				}
				r.connect.AnimFrame++
				frame := hashAnimationFrames[r.connect.AnimFrame%len(hashAnimationFrames)]
				r.setConnectStatus(frame+" Connecting…", r.theme.Text)
			})
		case <-ctx.Done():
			return
		}
	}
}

// finishConnect applies dialFunc's own outcome: records it in the
// connection history either way (see remotefs.RecordAttempt), then
// either reports the error back into the still-open dialog (so the
// user can fix a field and retry without starting over) or attaches
// the new client to panel and closes the dialog.
//
// Split out from runConnect specifically so it's callable directly,
// without needing a real Application event loop to drain
// QueueUpdateDraw first — see showSedPreviewResult's own identical
// doc comment for the same reasoning.
func (r *Root) finishConnect(panel *Panel, conn remotefs.Connection, client remotefs.Client, err error) {
	r.cancelConnectAttempt()
	if err != nil {
		_ = remotefs.RecordAttempt(conn, true)
		r.activityLog.Error(activitylog.CategoryRemote, fmt.Sprintf("connect to %s: %v", conn.Label(), err))

		var mismatchErr *remotefs.HostKeyMismatchError
		if errors.As(err, &mismatchErr) {
			r.connect.PendingSkipHostKey = true
			r.connect.PendingSkipHostKeyConn = conn
			r.connect.ConnectBtn.SetLabel("Connect anyway")
			r.setConnectStatus(fmt.Sprintf(
				"Host key changed: now %s %s (known_hosts has %s). Click Connect anyway to proceed without saving.",
				mismatchErr.NewKeyType, mismatchErr.NewFingerprint, strings.Join(mismatchErr.OldFingerprints, ", "),
			), r.theme.CriticalText)
			return
		}
		r.resetConnectAnywayState()
		r.setConnectStatus(err.Error(), r.theme.CriticalText)
		return
	}
	r.resetConnectAnywayState()
	// Recorded on conn itself, before RecordAttempt persists it to
	// history and connectRemote attaches it to panel — see
	// remotefs.AuthMethod's own doc comment for why this is the one
	// "how" worth keeping despite Connection otherwise only ever
	// describing "where": rsync.go's own refuseBackgroundPasswordAuth
	// reads exactly this back off the active panel's own remoteConn.
	conn.AuthMethod = client.AuthMethod()

	_ = remotefs.RecordAttempt(conn, false)
	r.activityLog.Action(activitylog.CategoryRemote, fmt.Sprintf("connected to %s", conn.Label()))

	if !r.hasTab(panel) {
		// The tab this connection was meant for closed while Dial was
		// still in flight — nothing left to attach it to.
		_ = client.Close()
		r.hideOverlay()
		return
	}
	r.hideOverlay()
	if err := panel.connectRemote(client, conn); err != nil {
		r.showError(err)
	}
}

// hasTab reports whether p is still one of r.tabs — see finishConnect's
// own doc comment for why this matters.
func (r *Root) hasTab(p *Panel) bool {
	for _, t := range r.tabs {
		if t == p {
			return true
		}
	}
	return false
}
