// consigliere.go is the AI Consigliere's own sidebar — a chat panel
// reachable via "gC" (see keymap.go) from any view, not just the file
// panel, per feature_ideas.txt's own explicit "NICHT an die
// Details-Sidebar gekoppelt" requirement (that note is about
// reachability, not shape: the spec explicitly asks to reuse the
// Details sidebar's own overlay/scroll infrastructure for the chat
// window itself — see detailssidebar.go). Docked flush against the
// right edge, non-modal (its own show/hide path, not pushOverlay/
// hideOverlay/activePage/overlayStack), the same shape Details uses —
// not a centered modal dialog. buildConsigliereProvider constructs the
// real Provider (internal/consigliere's Anthropic/OpenAICompat) for
// whichever backend consigliere_provider currently names, with its
// API key resolved via internal/apikey's own three-tier chain — see
// that function's own doc comment. consigliere.Mock{} still exists
// (internal/consigliere/mock.go) but nothing here constructs it
// anymore; it's kept only as a standalone, dependency-free Provider a
// future test or local experiment can still reach for directly.
package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/apikey"
	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/consigliere"
	"github.com/jagottsicher/breakthrough/internal/docsearch"
	"github.com/jagottsicher/breakthrough/internal/firewall"
)

const consiglierePage = "consigliere"

// consigliereInputPlaceholder is consigliereInput's own normal,
// ask-a-question placeholder — a named const rather than a literal
// repeated at both newConsigliereSidebar (initial) and
// submitConsigliereUnlock (restored after a successful unlock), so
// the two can never drift apart from each other.
const consigliereInputPlaceholder = "Ask a question… (Enter to send, Esc to close)"

// consigliereChordFamily is a one-member stand-in for the real "goto"
// family (chordFamilies' own 'g' entry in keymap.go), started from
// inside a full-screen modal (Log Audit, Git Breach, ...) on a bare
// 'g' key press that screen's own InputCapture would otherwise leave
// unclaimed — see startConsigliereChord's own doc comment for why this
// needs to be its own family rather than just reusing the real one by
// prefix. Reached the exact same way the main panel's own "gC" is
// (startChord -> resolveChord -> this family's own single member), so
// "gC" behaves identically everywhere it works at all; the only
// per-screen cost is the three-line case that starts it.
var consigliereChordFamily = chordFamily{
	prefix: 'g',
	name:   "AI Consigliere",
	members: []chordMember{
		{'C', "AI Consigliere", func(r *Root) { r.ConsigliereShortcut() }},
	},
}

// startConsigliereChord is what a full-screen modal's own InputCapture
// calls on a bare 'g' it doesn't otherwise claim, to make "gC" reach
// the AI Consigliere from there too. Calling startChord directly with
// this same family would already be correct by itself — the point of
// wrapping it here is just a single, named, grep-able call site every
// screen's own "case 'g':" can share, rather than each repeating the
// same comment about why it has to be this specific family and not
// chordFamilyFor('g')'s real "goto" one (see pendingChordFamily's own
// doc comment in root.go for that part).
// forwardExit is where Tab leaving consigliere's own input/history
// pair lands, once it actually opens (see consigliereForwardExit's
// own doc comment in root.go) — the fixed stop that follows wherever
// this host screen splices consigliere into its own ring, the same
// for every one of that screen's own 'g' entry points (e.g. Log
// Audit's table and timeline both splice it in right before Keyword;
// Git Breach's five entry points all splice it in right before
// Files). Pass nil for a single-node "ring" (Log Audit's selection/
// detail screens) — consigliereForwardExit then falls back to
// consigliereReturnFocus, which is exactly the one other node there
// is.
func (r *Root) startConsigliereChord(forwardExit tview.Primitive) {
	r.consigliereForwardExit = forwardExit
	r.startChord(consigliereChordFamily)
}

// consigliereMinWidth is consigliereSize's own floor on a terminal too
// narrow for a genuine one-third share to still be usable — the same
// floor detailsSidebarMinWidth already establishes for the sidebar
// this one's own sizing/positioning is modeled on.
const consigliereMinWidth = 40

// newConsigliereSidebar builds the chat panel once, at startup —
// title bar (renderConsigliereTitleBar) over a scrollable history
// (consigliereHistory, the same read-only-TextView-plus-scrollbar
// shape detailsSidebar/helpView already use) over a single-line input
// field (consigliereInput) for the next question, stacked in a FlexRow
// the same way detailsSidebarLayout stacks detailsTitleBar over
// detailsSidebar. Called once from NewRoot; content is reset fresh
// every time the sidebar opens (see showConsigliere), the same
// "rebuilt on open" contract tabSwitcher/filterMenu already follow.
func (r *Root) newConsigliereSidebar() {
	r.consigliereHistory = tview.NewTextView()
	r.consigliereHistory.SetDynamicColors(true)
	r.consigliereHistory.SetWrap(true)
	r.consigliereHistory.SetBorderPadding(0, 0, 1, 1)
	r.consigliereHistory.SetInputCapture(r.captureConsigliereHistoryKey)
	// FocusFunc/BlurFunc — not consigliereInput's own focus, and not a
	// generic "is the sidebar open" check: per the user's own explicit
	// request, the title bar only reads as active while the history
	// itself has real keyboard focus, i.e. while it's actually the
	// thing being scrolled — the same live-focus-driven title bar
	// newDetailsSidebarView already establishes for Details, reused
	// here for the identical reason (applyTheme's own re-derivation
	// of this, for a live theme switch mid-focus, lives in theme.go).
	r.consigliereHistory.SetFocusFunc(func() { r.consigliereTitleBar.SetBackgroundColor(r.consigliereTitleBarBackground(true)) })
	r.consigliereHistory.SetBlurFunc(func() { r.consigliereTitleBar.SetBackgroundColor(r.consigliereTitleBarBackground(false)) })
	r.consigliereScrollbar = newVerticalScrollbarForTextView(r, r.consigliereHistory)

	r.consigliereTitleBar = newPlainTitleBar("AI Consigliere")
	r.consigliereTitleBar.SetDynamicColors(false)
	r.consigliereTitleBar.SetMouseCapture(r.captureConsigliereTitleBarMouse)

	r.consigliereInput = tview.NewInputField()
	r.consigliereInput.SetPlaceholder(consigliereInputPlaceholder)
	r.consigliereInput.SetDoneFunc(r.handleConsigliereInputDone)

	historyRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.consigliereHistory, 0, 1, false).
		AddItem(r.consigliereScrollbar, 1, 0, false)
	historyRow.SetMouseCapture(r.consigliereScrollbar.captureMouse)

	r.consigliereLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.consigliereTitleBar, 1, 0, false).
		AddItem(historyRow, 0, 1, false).
		AddItem(r.consigliereInput, 1, 0, true)
}

// captureConsigliereHistoryKey closes the sidebar on Escape while
// consigliereHistory has real keyboard focus (reached via Tab from
// consigliereInput — see handleConsigliereInputDone). Deliberately
// SetInputCapture, not SetDoneFunc: TextView's own SetDoneFunc fires
// for Enter/Escape/Tab/Backtab alike (see viewer.go's own doc comment
// on newViewerView), which would swallow Tab here too and defeat the
// whole point of adding consigliereHistory to focusCycleStops — this
// only intercepts Escape, letting Tab/Backtab/every arrow and
// scrolling key fall through to TextView's own default handling and,
// for Tab, on to CycleFocusShortcut.
func (r *Root) captureConsigliereHistoryKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEscape:
		r.closeConsigliereWithConfirm()
		return nil
	case tcell.KeyTab, tcell.KeyBacktab:
		// Only ever reached once a full-screen modal has already
		// excluded Consigliere from the global focus cycle (see
		// focusCycleStops' own doc comment in detailssidebar.go) —
		// while nothing else is open, that global cycle still claims
		// Tab/Backtab here directly, before this InputCapture (or
		// TextView's own InputHandler, which — per tview's own
		// textview.go — does nothing at all for either key without a
		// SetDoneFunc) ever sees it. Backtab goes back to the field;
		// Tab exits consigliere's own tiny loop forward — continuing
		// the host screen's own ring past it, the same way
		// handleConsigliereInputDone's identical fallback does.
		if event.Key() == tcell.KeyBacktab {
			r.app.SetFocus(r.consigliereInput)
		} else {
			r.app.SetFocus(r.consigliereForwardExitTarget())
		}
		return nil
	}
	return event
}

// consigliereBackwardExit is where Backtab leaving consigliere's own
// first stop (consigliereInput) lands: whatever had real keyboard
// focus the moment showConsigliere last ran (consigliereReturnFocus —
// see its own doc comment in root.go) — but only while
// consigliereExitPage still matches r.activePage (see
// consigliereCurrentHostEdges' own doc comment for why a stale one is
// actively wrong, not just imprecise, and gets re-derived instead).
// Falls back to the panel if even that re-derivation has nothing.
// Shared by handleConsigliereInputDone and
// captureConsigliereHistoryKey's own Backtab case.
func (r *Root) consigliereBackwardExit() tview.Primitive {
	if r.consigliereReturnFocus != nil && r.activePage == r.consigliereExitPage {
		return r.consigliereReturnFocus
	}
	backward, _ := r.consigliereCurrentHostEdges()
	if backward != nil {
		return backward
	}
	return r.panel.table
}

// consigliereForwardExit is where Tab leaving consigliere's own last
// stop lands: the fixed splice point the host screen that opened it
// recorded (consigliereForwardExit — see its own doc comment in
// root.go) — again only while consigliereExitPage still matches
// r.activePage, same reasoning consigliereBackwardExit's own doc
// comment gives. Falls back to consigliereBackwardExit for a
// single-node "ring" (nothing else was ever recorded, so both
// directions lead to the same one other node) or, failing that, the
// panel. Shared by handleConsigliereInputDone and
// captureConsigliereHistoryKey's own Tab case — the one change that
// made Tab actually continue a host screen's own ring past
// consigliere instead of looping it back to wherever consigliere was
// opened from, stranding every stop in between (a real, reported
// bug).
func (r *Root) consigliereForwardExitTarget() tview.Primitive {
	if r.consigliereForwardExit != nil && r.activePage == r.consigliereExitPage {
		return r.consigliereForwardExit
	}
	_, forward := r.consigliereCurrentHostEdges()
	if forward != nil {
		return forward
	}
	return r.consigliereBackwardExit()
}

// consigliereCurrentHostEdges re-derives the backward/forward splice
// targets consigliereBackwardExit/consigliereForwardExitTarget fall
// back to once the recorded ones have gone stale — r.activePage has
// moved on from consigliereExitPage, meaning the sidebar was opened
// from one page and has since stayed open while the user navigated to
// another (per the user's own explicit request that it survive
// exactly that), so consigliereReturnFocus/consigliereForwardExit
// still name real widgets, just ones on a page that isn't active any
// more — covered, unreachable, and otherwise indistinguishable from a
// dead end (a real, reported bug: Tab/Backtab out of the sidebar kept
// landing there anyway).
//
// One switch case per screen reachable from — the same per-screen
// cost startConsigliereChord's own callers already pay, not a
// separate registry to keep in sync with them. (nil, nil) for
// anything not listed (including "" — the bare panel, where the
// global focus cycle already handles this instead, see
// focusCycleStops' own doc comment) tells both callers above to fall
// back further on their own.
func (r *Root) consigliereCurrentHostEdges() (backward, forward tview.Primitive) {
	switch r.activePage {
	case logAuditSelectionPage:
		return r.logAuditTable, r.logAuditTable
	case logAuditViewerPage:
		return r.logAuditViewerTable, r.logAuditKeywordField
	case logAuditDetailPage:
		return r.logAuditDetailView, r.logAuditDetailView
	case gitBreachPage:
		focusables := r.gitBreachFocusables()
		if len(focusables) == 0 {
			return nil, nil
		}
		return focusables[len(focusables)-1].body, focusables[0].body
	}
	return nil, nil
}

// renderConsigliereTitleBar sets consigliereTitleBar's own text,
// padded out to width columns with the close glyph placed
// toolWindowCloseButtonCol's own one-column-in-from-the-edge spacing —
// the exact same convention renderHelpTitleBar/renderDetailsTitleBar
// already follow, reused here rather than duplicated with a different
// number.
func (r *Root) renderConsigliereTitleBar(width int) {
	label := " AI Consigliere "
	// Machiavelli Mode's own unmissable reminder: unlike every other
	// per-session detail, this one must stay visible in the title bar
	// itself for as long as it's on, not just noted once in Options —
	// see consigliereTitleBarBackground for the matching background
	// color, which overrides the ordinary focused/unfocused one for
	// the same reason.
	if r.settings.ConsigliereHardcoreMode {
		label = " AI Consigliere · MACHIAVELLI "
	}
	closeCol := toolWindowCloseButtonCol(0, width)
	padding := closeCol - len(label)
	if padding < 0 {
		padding = 0
	}
	r.consigliereTitleBar.SetText(label + strings.Repeat(" ", padding) + string(toolWindowCloseGlyph) + " ")
	r.consigliereTitleBar.SetBackgroundColor(r.consigliereTitleBarBackground(r.consigliereHistory.HasFocus()))
}

// consigliereTitleBarBackground is the title bar's own background for
// the given focus state — the ordinary focused/unfocused pair every
// other call site here already used directly, except while
// Machiavelli Mode is on, which forces the warning color regardless
// of focus: a reminder that must survive every focus change, not
// blink back to normal the instant history loses focus.
func (r *Root) consigliereTitleBarBackground(focused bool) tcell.Color {
	if r.settings.ConsigliereHardcoreMode {
		return r.theme.ErrorBackground
	}
	if focused {
		return r.theme.InputFocusedBackground
	}
	return r.theme.InputBackground
}

// captureConsigliereTitleBarMouse is dragConsigliereMouseCapture's own
// wrapper for the title bar's SetMouseCapture — there's nothing else
// for this bar to handle, unlike Properties' own hashesMouseCapture
// (which falls through to unrelated hash-section handling), so an
// unhandled result just passes the event through unchanged.
func (r *Root) captureConsigliereTitleBarMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	out, outEvent, _ := r.dragConsigliereMouseCapture(action, event)
	return out, outEvent
}

// dragConsigliereMouseCapture makes the sidebar's own title bar
// draggable — press and drag anywhere on it, except the close glyph
// (still closes the sidebar, same as before), to move the whole
// window; release to stop. No resizing, per the user's own explicit
// request — position only.
//
// Mirrors dragPropertiesMouseCapture's own shape exactly (see its own
// doc comment in properties.go), including why captureOutsideClick's
// own consigliereDragging check — not tview's own position-based
// dispatch — is what keeps a drag that moves up/left receiving its
// own subsequent events once the cursor is no longer over the
// not-yet-updated title bar rect: the identical bug propertiesDragging's
// own check already exists to prevent, for the identical reason.
//
// handled reports whether this call was about dragging (or the close
// glyph) at all, the same shape dragPropertiesMouseCapture's own
// third return value already has.
func (r *Root) dragConsigliereMouseCapture(action tview.MouseAction, event *tcell.EventMouse) (out tview.MouseAction, outEvent *tcell.EventMouse, handled bool) {
	x, y := event.Position()

	if r.consigliereDragging {
		switch action {
		case tview.MouseMove:
			if event.Buttons()&tcell.ButtonPrimary == 0 {
				r.consigliereDragging = false
				return tview.MouseConsumed, nil, true
			}
			r.moveConsigliere(x-r.consigliereDragOffsetX, y-r.consigliereDragOffsetY)
			return tview.MouseConsumed, nil, true
		case tview.MouseLeftUp:
			r.consigliereDragging = false
			return tview.MouseConsumed, nil, true
		}
		return tview.MouseConsumed, nil, true // swallow everything else for the duration of the drag
	}

	if action == tview.MouseLeftDown && r.consigliereTitleBar.InRect(x, y) {
		wx, wy, width, _ := r.consigliereLayout.GetRect()
		if y == wy && x == wx+toolWindowCloseButtonCol(0, width) {
			r.closeConsigliereWithConfirm()
			return tview.MouseConsumed, nil, true
		}
		r.consigliereDragging = true
		r.consigliereDragOffsetX, r.consigliereDragOffsetY = x-wx, y-wy
		return tview.MouseConsumed, nil, true
	}

	return action, event, false
}

// moveConsigliere repositions the sidebar to (x, y) without changing
// its own size — the drag's own per-move step, called from
// dragConsigliereMouseCapture. consigliereManuallyPositioned marks
// that this has happened at all, so repositionConsigliereSidebar
// stops re-docking it flush right on every resize/reshow (see its own
// doc comment) and just re-clamps wherever the user actually left it.
func (r *Root) moveConsigliere(x, y int) {
	r.consigliereManuallyPositioned = true
	_, _, width, height := r.consigliereLayout.GetRect()
	x, y, width, height = r.clampToScreen(x, y, width, height)
	r.consigliereLayout.SetRect(x, y, width, height)
}

// consigliereSize sizes the sidebar against the whole screen (Root's
// own rect — see detailsSidebarSize's own doc comment on why Details
// uses the same base rather than clampToPanel): a third of the
// screen's own width, and half that many characters tall — per the
// user's own explicit request, deliberately not a literal
// width==height square: a terminal's own character cells are roughly
// twice as tall as they are wide, so half the width's own character
// count is what actually reads as visually square, not a literal
// equal count of columns and rows.
func (r *Root) consigliereSize() (width, height int) {
	_, _, screenWidth, _ := r.GetRect()
	width = screenWidth / 3
	if width < consigliereMinWidth {
		width = consigliereMinWidth
	}
	height = width / 2
	return width, height
}

// consigliereHistoryContentWidth is consigliereHistory's own real text
// width once laid out: consigliereSize's own outer width, minus the
// scrollbar's one fixed column (historyRow, see newConsigliereSidebar)
// and consigliereHistory's own 1-column left/right border padding.
// Computed analytically from consigliereSize rather than read back via
// consigliereHistory.GetInnerRect(): Flex only resolves a child's own
// rect during Draw, not synchronously when SetRect is called on the
// parent (see repositionConsigliereSidebar) — reading it back
// immediately after, the way renderConsigliereHistory needs to for
// consigliereSplashArt's own centering, would see the previous open's
// stale rect (or an uninitialized zero one, the first time ever), not
// the size just computed for this one.
func (r *Root) consigliereHistoryContentWidth() int {
	width, _ := r.consigliereSize()
	return width - 1 - 2
}

// repositionConsigliereSidebar (re)computes the sidebar's own rect
// from consigliereSize: flush against the right edge of the screen
// horizontally, centered on the whole screen vertically — per the
// user's own explicit request, the same regardless of whether this is
// docked over the bare file panel or a full-screen modal (Log Audit,
// Git Breach, ...), unlike the old fill-the-column shape this
// replaced. Hooked into Root's own resize handling (see
// handleBeforeDraw) so a live terminal resize keeps it correctly
// positioned while open.
//
// Once the user has actually dragged the sidebar
// (consigliereManuallyPositioned — see moveConsigliere), a resize no
// longer re-docks it flush right/centered: that would silently undo
// the move on every live terminal resize, the same "a drag must not
// keep snapping back" expectation a toolWindow's own manuallyResized
// already protects, just for position instead of size. The current
// x/y is kept as-is and only re-clamped, so it still can't end up
// off-screen.
func (r *Root) repositionConsigliereSidebar() {
	width, height := r.consigliereSize()
	_, _, screenWidth, screenHeight := r.GetRect()
	x, y := screenWidth-width, (screenHeight-height)/2
	if r.consigliereManuallyPositioned {
		x, y, _, _ = r.consigliereLayout.GetRect()
	}
	x, y, width, height = r.clampToScreen(x, y, width, height)
	r.renderConsigliereTitleBar(width)
	r.consigliereLayout.SetRect(x, y, width, height)
}

// ConsigliereShortcut is "gC"'s own guarded entry point — the same
// reasoning ToggleDetailsSidebarShortcut's own guard already follows
// (don't act while the Bash prompt has focus or a live Terminal is
// being viewed), minus Details' own "or Properties" carve-out: nothing
// about this sidebar needs to coexist with Properties specifically,
// since unlike "I" it isn't alsoOverProperties-gated. Not currently
// called from anywhere in cmd/breakthrough — "gC" reaches
// toggleConsigliere directly (see keymap.go) — kept anyway as the same
// kind of exported building block ToggleSplitShortcut/
// ToggleDetailsSidebarShortcut already are, for whatever future caller
// (a button, per the spec) reaches it some other way.
func (r *Root) ConsigliereShortcut() {
	if r.bashLine.HasFocus() || r.viewingShells {
		return
	}
	r.toggleConsigliere()
}

// toggleConsigliere is "gC"'s own action — toggleDetailsSidebar's own
// exact shape, just tracking consigliereVisible instead of
// detailsSidebarVisible.
func (r *Root) toggleConsigliere() {
	if r.consigliereVisible {
		r.closeConsigliereWithConfirm()
	} else {
		r.showConsigliere()
	}
}

// showConsigliere positions the sidebar flush against the right edge
// of the screen (see repositionConsigliereSidebar) and shows it —
// showDetailsSidebar's own shape, with one deliberate difference:
// Details never touches keyboard focus (see newDetailsSidebarView's
// own doc comment on why not), but this sidebar's entire point is
// typing a question, so it explicitly focuses consigliereInput instead
// of preserving whatever had focus before. Starts a fresh chat session
// every time, per the spec's own "Schließen und erneutes Öffnen
// startet eine frische Konversation".
func (r *Root) showConsigliere() {
	// consigliereReturnFocus is captured *before* anything below
	// changes focus or activePage — see hideConsigliere's own doc
	// comment for why a hardcoded r.panel.table fallback is wrong
	// whenever this was opened from inside a full-screen modal (Log
	// Audit, Git Breach, ...) rather than the bare file panel.
	// consigliereExitPage is the matching activePage snapshot — see
	// its own doc comment in root.go for why it's back, for a
	// different reason than the auto-close behavior it used to serve
	// under the removed consigliereHostPage name.
	r.consigliereReturnFocus = r.app.GetFocus()
	r.consigliereExitPage = r.activePage
	// A previous session's own manual drag must not carry over into
	// this fresh one — see repositionConsigliereSidebar's own doc
	// comment.
	r.consigliereManuallyPositioned = false

	r.consigliereMessages = nil
	r.consigliereInput.SetText("")

	// Forces the passphrase right here, before any question can even
	// be typed, rather than only discovering the lock once a question
	// already failed (errConsigliereLocked) — per the user's own
	// explicit request: if the passphrase has to be entered at all
	// this run, entering it should be unavoidable at the sidebar's own
	// start, not an error message after the fact.
	r.consigliereAwaitingPassphrase = r.consigliereNeedsUnlock()
	if r.consigliereAwaitingPassphrase {
		r.consigliereInput.SetMaskCharacter('*')
		r.consigliereInput.SetPlaceholder("Enter passphrase to unleash the AI Consigliere…")
	} else {
		r.consigliereInput.SetMaskCharacter(0)
		r.consigliereInput.SetPlaceholder(consigliereInputPlaceholder)
	}

	// Reposition before the first render, not after: consigliereSplashArt
	// centers itself against consigliereHistory's own current inner
	// width (see renderConsigliereHistory), which repositionConsigliereSidebar
	// is what actually sets.
	r.repositionConsigliereSidebar()
	r.renderConsigliereHistory() // shows consigliereSplashArt right away, before any question
	r.ShowPage(consiglierePage)
	r.SendToFront(consiglierePage)
	r.consigliereVisible = true
	r.app.SetFocus(r.consigliereInput)
}

// consigliereNeedsUnlock reports whether the currently selected
// provider's key can only come from the encrypted tier, and that tier
// is still locked — exactly the case errConsigliereLocked's own doc
// comment describes, just checked here before the user ever has to
// ask a question to discover it. False whenever a higher-priority
// tier (env var/file) already supplies a key regardless of lock
// state: forcing a passphrase prompt that would turn out to be
// unnecessary is exactly the friction this is trying to avoid.
func (r *Root) consigliereNeedsUnlock() bool {
	if r.apiKeySession == nil || r.apiKeySession.IsUnlocked() {
		return false
	}
	provider := r.settings.ConsigliereProvider
	if !r.apiKeySession.HasProvider(provider) {
		return false
	}
	_, tier, err := apikey.Resolve(consigliereEnvVarForProvider(provider), r.settings.ConsigliereAPIKeyFile, provider, r.apiKeySession)
	return err == nil && tier == apikey.TierNone
}

// closeConsigliereWithConfirm is every user-initiated close path's
// own entry point (the title bar's own close glyph, Escape from
// either focus stop, and "gC" toggling an already-open sidebar back
// shut) — hideConsigliere's own direct callers, replaced here per the
// user's own explicit request: closing discards the current
// conversation outright (see showConsigliere's own "a fresh session
// every time" behavior), so a close that would actually lose one now
// asks first. Skipped entirely once nothing has been asked yet
// (consigliereMessages still empty) — there is nothing to lose yet,
// so asking would only be friction for the "opened by mistake, close
// right away" case.
func (r *Root) closeConsigliereWithConfirm() {
	if len(r.consigliereMessages) == 0 {
		r.hideConsigliere()
		return
	}
	r.openConfirm("Close the AI Consigliere? The current conversation will be lost.", "Yes, close", r.hideConsigliere)
	// openConfirm's own pushOverlay always re-floats the sidebar above
	// whatever overlay it just opened (see pushOverlay's own doc
	// comment — written for ordinary within-task navigation, where the
	// sidebar is meant to stay on top and reachable). The opposite is
	// needed here: this confirmation is asking whether to close the
	// sidebar itself, so it must stay visible and clickable on top of
	// it, not end up hidden behind it.
	r.SendToFront(confirmPage)
}

// hideConsigliere closes the sidebar — showConsigliere's own
// counterpart, hideDetailsSidebar's own shape. Cancels whatever reply
// is still in flight (see submitConsigliereMessage) so a provider call
// that outlives the sidebar being closed can never still land a
// QueueUpdateDraw into a chat the user has already left, and restores
// focus to the panel if this sidebar is what currently has it — left
// to r.app.GetFocus() alone, focus would otherwise end up parked on a
// now-invisible primitive with nothing left able to receive it back
// (see preserveFocusAcross's own doc comment on the identical Details
// case).
func (r *Root) hideConsigliere() {
	if r.consigliereCancel != nil {
		r.consigliereCancel()
		r.consigliereCancel = nil
	}
	hadFocus := r.consigliereInput.HasFocus() || r.consigliereHistory.HasFocus()
	r.HidePage(consiglierePage)
	if hadFocus {
		// consigliereReturnFocus, not a hardcoded r.panel.table: opened
		// from inside Log Audit/Git Breach/etc. (see
		// closeConsigliereIfHostPageChanged's own doc comment for the
		// other half of this), the panel's own table isn't even
		// visible — it's still covered by that full-screen modal, so
		// focusing it would leave the keyboard dead against whatever's
		// actually on screen. Falls back to the panel only if nothing
		// was recorded (e.g. a direct call before the sidebar was ever
		// shown once).
		target := r.consigliereReturnFocus
		if target == nil {
			target = r.panel.table
		}
		r.app.SetFocus(target)
	}
	r.consigliereReturnFocus = nil
	r.consigliereExitPage = ""
	r.consigliereVisible = false
}

// handleConsigliereInputDone is consigliereInput's own SetDoneFunc —
// Enter submits the current text, Escape closes the sidebar. Tab/
// Backtab (the other two keys InputField itself calls this for) move
// focus onward instead of no-op'ing: InputField's own SetDoneFunc
// claims Tab before it could ever reach CycleFocusShortcut's global
// handling (see its own doc comment on every other field with a
// DoneFunc doing the same), so without this, there would be no
// keyboard path at all out of the question field once it has real
// focus — true unconditionally once a full-screen modal is open (see
// focusCycleStops' own doc comment on why Consigliere leaves the
// global cycle there), and true for plain Tab even otherwise, since
// it's always this SetDoneFunc, never the global cycle, that a key
// press actually reaches first while this field itself has real
// focus. Tab lands on consigliereHistory only when there's actually
// something in it to scroll to — the exact same
// consigliereScrollbar.maxOffset() > 0 condition focusCycleStops
// itself uses (see detailssidebar.go) to decide whether history is
// even a stop right now — exiting via consigliereExitFocus
// otherwise (also Backtab's own, unconditional case), per the user's
// own explicit request: a destination
// nothing can be done in once there is deserves to be skipped, not
// tabbed into as a dead end.
func (r *Root) handleConsigliereInputDone(key tcell.Key) {
	if key == tcell.KeyEscape {
		r.closeConsigliereWithConfirm()
		return
	}
	if r.consigliereAwaitingPassphrase {
		// Forced, per consigliereNeedsUnlock's own doc comment: Tab/
		// Backtab do nothing here instead of leaving the sidebar (or
		// jumping to a history that's empty anyway) — Escape (above)
		// is still the only way out without unlocking first.
		if key == tcell.KeyEnter {
			r.submitConsigliereUnlock()
		}
		return
	}
	if key == tcell.KeyBacktab {
		// Backward from the first stop in consigliere's own tiny
		// loop — back to wherever it was opened from.
		r.app.SetFocus(r.consigliereBackwardExit())
		return
	}
	if key == tcell.KeyTab {
		if r.consigliereScrollbar.maxOffset() > 0 {
			r.app.SetFocus(r.consigliereHistory)
		} else {
			// Forward, not backward: continues the host screen's own
			// ring past consigliere (consigliereForwardExit) instead
			// of looping back to wherever it was opened from — the
			// real, reported bug this fixes (see
			// consigliereForwardExitTarget's own doc comment).
			r.app.SetFocus(r.consigliereForwardExitTarget())
		}
		return
	}
	if key != tcell.KeyEnter {
		return
	}
	text := strings.TrimSpace(r.consigliereInput.GetText())
	if text == "" {
		return
	}
	r.consigliereInput.SetText("")
	r.submitConsigliereMessage(text)
}

// consigliereContextSnapshot is the spec's own "AdvisorContext()" idea
// — not yet a real per-view dispatch: "gC" currently only ever reaches
// the panel table (see acceptsPlainKeyCommand in keymap.go), and even
// there, no real snapshot method exists yet. A genuine dispatch point
// (by view, however that ends up being modeled once a second real
// provider of context exists — see feature_ideas.txt's own "AI
// Consigliere" entry for the open follow-up on reaching Log Audit/Git
// Breach/etc.) is next branch's work, not this slice's — only the chat
// mechanics (history/scrolling/input) are being judged here.
// consigliereMaxDocChunks/consigliereMaxDocChunkChars cap how much of
// the documentation search's own results go into a snapshot — the
// same anthropicMaxTokens-style reasoning every other capped piece of
// context here already follows.
const (
	consigliereMaxDocChunks = 3
	// consigliereMaxDocChunkChars was originally 600, then 1,500 — two
	// real, reported rounds of the same bug: several of this corpus's
	// own sections run 1,000-7,000+ characters, so a flat cap kept
	// silently cutting the actual answer out of an already-correctly-
	// retrieved chunk. The "Log Holepunch screen" section specifically has
	// ~1,660 characters of table rows alone (two separate key tables),
	// past even the 1,500 cap, which is what truncateDocChunkBody's
	// own table-priority logic exists for — 2,500 is sized to fit that
	// chunk's own tables in full, with roughly 800 characters left for
	// intro prose, rather than chasing the next individually-measured
	// section as each one gets reported.
	consigliereMaxDocChunkChars = 2500
)

// consigliereDocSnapshot runs question against r.docSearchIndex (see
// NewRoot) and renders whichever documentation sections actually
// matched — "" if the index isn't built (NewDefaultIndex failed,
// which would mean the embed itself is broken) or nothing matched at
// all, in which case this contributes nothing to the snapshot rather
// than padding it with irrelevant sections.
func (r *Root) consigliereDocSnapshot(ctx context.Context, provider consigliere.Provider, question string) string {
	if r.docSearchIndex == nil {
		return ""
	}
	hint := r.consigliereDocSearchViewHint()
	chunks := r.docSearchIndex.Search(appendConsigliereDocSearchHint(question, hint), consigliereMaxDocChunks)
	// A real, reported case: even with hint appended to the query, a
	// short, unrelated chunk that only mentions the current screen's
	// own name once in passing (a cross-reference sentence, not its
	// own topic) could still outrank that screen's actual section —
	// BM25 ranking alone isn't a strong enough guarantee here, unlike
	// the hint itself, which names the live, actually-open screen with
	// certainty. Force that screen's own section to the front rather
	// than leaving it to ranking, same "guaranteed over probabilistic"
	// reasoning logAuditViewerAvailableActions above already follows.
	if hint != "" {
		if hc, ok := r.docSearchIndex.ChunkByHeadingContains(hint); ok {
			chunks = prependDocChunk(hc, chunks, consigliereMaxDocChunks)
		}
	}
	if len(chunks) == 0 {
		// BM25 only ever matches shared vocabulary — this corpus is
		// English-only, so a question asked in a language it isn't
		// written in (confirmed with a real report: a German question
		// about opening a tab shared only the word "tab" with the
		// English docs) would otherwise silently retrieve nothing, not
		// because nothing relevant exists, but because no query term
		// happens to appear in the corpus at all. Retried once, in
		// English, via the same provider/key already resolved for the
		// real reply — a second real network call, only ever made when
		// the direct search found literally zero matches, not on every
		// question.
		if translated := r.translateConsigliereQueryToEnglish(ctx, provider, question); translated != "" {
			chunks = r.docSearchIndex.Search(appendConsigliereDocSearchHint(translated, hint), consigliereMaxDocChunks)
		}
	}
	if len(chunks) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Relevant documentation:\n")
	for _, c := range chunks {
		fmt.Fprintf(&b, "- %s: %s\n", c.Heading, truncateDocChunkBody(c.Body, consigliereMaxDocChunkChars))
	}
	return strings.TrimSpace(b.String())
}

// consigliereDocSearchViewHint names the current screen in plain
// English terms matching this corpus's own section headings (e.g.
// "Log Holepunch screen", not the "jL" chord) — appended to every doc
// search query (see appendConsigliereDocSearchHint) so a question
// asked while actually looking at one of two similarly-purposed
// screens retrieves that one's own section, not whichever one
// happens to share more vocabulary with the raw question text alone.
// A real, reported case: "wie suche ich nach exception in den
// letzten drei Tagen" while on Log Holepunch retrieved the Action
// Log's own section instead ("jl" vs "jL" — easy to conflate on
// vocabulary alone, never on which screen the question was actually
// asked from). "" for the bare file panel and anything else "gC"
// might reach from, where no such disambiguation is needed — the
// same switch consigliereContextSnapshot's own dispatch already uses.
func (r *Root) consigliereDocSearchViewHint() string {
	switch r.activePage {
	case logAuditSelectionPage, logAuditViewerPage, logAuditDetailPage, logAuditStatsPage:
		return "Log Holepunch screen"
	case gitBreachPage:
		return "Git Breach"
	default:
		return ""
	}
}

// prependDocChunk puts chunk at the front of chunks — removing any
// existing occurrence of it first (same Heading+Source, which is
// enough to identify it since both come from the same Index) — and
// caps the result back down to max, so forcing a guaranteed chunk to
// the front never grows the result past consigliereMaxDocChunks.
func prependDocChunk(chunk docsearch.Chunk, chunks []docsearch.Chunk, max int) []docsearch.Chunk {
	out := make([]docsearch.Chunk, 0, len(chunks)+1)
	out = append(out, chunk)
	for _, c := range chunks {
		if c.Heading == chunk.Heading && c.Source == chunk.Source {
			continue
		}
		out = append(out, c)
	}
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// appendConsigliereDocSearchHint appends hint to query, space-
// separated, or returns query unchanged if hint is "".
func appendConsigliereDocSearchHint(query, hint string) string {
	if hint == "" {
		return query
	}
	return query + " " + hint
}

// translateConsigliereQueryToEnglish asks provider itself to
// translate question — reusing Provider.Reply directly rather than
// adding a second HTTP code path, with a Request carrying nothing but
// the translation instruction (no History, no System, no Snapshot):
// this is a standalone, single-turn request, never part of the real
// conversation. "" on any failure (network error, empty reply) — the
// caller's own fallback is simply "no documentation found," not
// surfacing a second error for a best-effort retry.
func (r *Root) translateConsigliereQueryToEnglish(ctx context.Context, provider consigliere.Provider, question string) string {
	req := consigliere.Request{
		History: []consigliere.Message{{
			Role: consigliere.RoleUser,
			Text: "Translate the following into English. Reply with only the translation, nothing else:\n\n" + question,
		}},
	}
	translated, err := provider.Reply(ctx, req)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(translated)
}

// truncateRunes caps s at max runes (not bytes, to never split a
// multi-byte UTF-8 character), appending "…" when it actually cut
// something off.
func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// truncateDocChunkBody caps body at roughly max characters, like
// truncateRunes, but prioritizes keeping any Markdown table ("| Key |
// Action |"-style rows) intact over plain prose: this corpus's own
// longer reference sections consistently put their actual key/filter-
// field table well after an explanatory preamble (Log Holepunch's own
// "Step 1"/"Step 2" mechanics, for one real, reported case), so naive
// left-to-right truncation was cutting the one part that actually
// answers a question before ever reaching it, despite the chunk
// itself having already been correctly retrieved.
//
// Reorders prose before table (the table's own rows always come
// last), which is harmless here: these chunks are never shown to a
// person, only fed to a model as reference material, and the model
// doesn't need "Step 1 ... Step 2 ... | Key | Action |" in exactly
// that order to use the table correctly.
func truncateDocChunkBody(body string, max int) string {
	if len([]rune(body)) <= max {
		return body
	}

	var tableLines, proseLines []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "|") {
			tableLines = append(tableLines, line)
		} else {
			proseLines = append(proseLines, line)
		}
	}
	if len(tableLines) == 0 {
		return truncateRunes(body, max)
	}

	table := strings.Join(tableLines, "\n")
	tableLen := len([]rune(table))
	if tableLen >= max {
		// The table alone doesn't fit either — plain truncation is at
		// least predictable, rather than risking cutting the table
		// itself at a point that drops whichever row actually matters.
		return truncateRunes(body, max)
	}

	prose := truncateRunes(strings.Join(proseLines, "\n"), max-tableLen-1) // -1 for the joining newline
	return strings.TrimSpace(prose) + "\n" + table
}

// joinConsigliereSnapshotParts joins the view-specific context
// (consigliereContextSnapshot) and the documentation search's own
// result (consigliereDocSnapshot) into the one Snapshot string
// Request carries — blank-line separated, each part omitted outright
// if empty rather than leaving a stray blank line, the same
// "omit-if-empty" shape combineConsigliereSystem already establishes
// in internal/consigliere for an identical two-piece join.
func joinConsigliereSnapshotParts(parts ...string) string {
	var nonEmpty []string
	for _, p := range parts {
		if p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	return strings.Join(nonEmpty, "\n\n")
}

func (r *Root) consigliereContextSnapshot() string {
	switch r.activePage {
	case logAuditSelectionPage, logAuditViewerPage, logAuditDetailPage, logAuditStatsPage:
		return r.logAuditContextSnapshot()
	case gitBreachPage:
		return r.gitBreachContextSnapshot()
	case firewallPage:
		return r.firewallContextSnapshot()
	default: // "" (the bare panel) and anything else "gC" might someday reach from
		return r.panelContextSnapshot()
	}
}

// consigliereMaxMarkedPaths/consigliereMaxRecentCommits cap how much
// of a long list actually goes into a snapshot — the same
// anthropicMaxTokens-style reasoning: hundreds of marked files or a
// deep commit history would bloat every single question's own token
// cost for little real benefit over "N files marked"/"N commits,
// most recent: ...".
const (
	consigliereMaxMarkedPaths   = 20
	consigliereMaxRecentCommits = 10
)

// panelContextSnapshot describes the bare file panel: the current
// directory and, if any, the marked files' own names — never their
// contents, the same boundary every snapshot builder here keeps (see
// logAuditContextSnapshot's own doc comment on why that matters most
// there).
func (r *Root) panelContextSnapshot() string {
	p := r.panel
	if p == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Current view: file panel.\nDirectory: %s\n", p.path)
	if len(p.selected) > 0 {
		names := make([]string, 0, len(p.selected))
		for path := range p.selected {
			names = append(names, filepath.Base(path))
		}
		sort.Strings(names)
		shown := names
		if len(shown) > consigliereMaxMarkedPaths {
			shown = shown[:consigliereMaxMarkedPaths]
		}
		fmt.Fprintf(&b, "Marked files (%d total): %s\n", len(names), strings.Join(shown, ", "))
	}
	return strings.TrimSpace(b.String())
}

// logAuditContextSnapshot describes whichever Log Holepunch page is
// actually showing right now (selection, viewer, detail, or stats —
// see consigliereContextSnapshot's own switch): the log directory,
// the visible timeline range, whatever keyword/time/level filter is
// currently set, and — unlike those live values, which only exist
// once something is actually typed — a fixed, always-present list of
// every field and key this screen itself offers (see
// logAuditViewerAvailableActions's own doc comment on why this is
// hardcoded here rather than left to the documentation search to
// surface): a real, reported case asked the exact same question from
// both the file panel and this screen, and wanted a broad answer from
// the first and a concrete, this-screen-specific one from the second
// — the live UI state is the one source that's never a guess about
// which screen is actually open right now, unlike a BM25 search's own
// best-effort ranking.
//
// Never includes a single log line's own text by default —
// logAuditKeywordField etc. are the filter *controls*, not the
// matched entries themselves, and a real credential or secret sitting
// in a log line is exactly the kind of thing this must never forward
// to a third-party provider without the user explicitly having
// opted in. The one deliberate exception is Machiavelli Mode (see
// consigliereMachiavelliLogLines), which the Options screen only lets
// the user turn on after confirming a dedicated warning naming this
// exact risk.
func (r *Root) logAuditContextSnapshot() string {
	var b strings.Builder
	b.WriteString("Current view: Log Holepunch.\n")
	if r.logAuditDir != "" {
		fmt.Fprintf(&b, "Log directory: %s\n", r.logAuditDir)
	}
	if r.logAuditTimelineStartLabel != "" || r.logAuditTimelineEndLabel != "" {
		fmt.Fprintf(&b, "Visible time range: %s to %s\n", r.logAuditTimelineStartLabel, r.logAuditTimelineEndLabel)
	}
	if kw := strings.TrimSpace(r.logAuditKeywordField.GetText()); kw != "" {
		fmt.Fprintf(&b, "Current keyword filter: %s\n", kw)
	}
	if lvl := strings.TrimSpace(r.logAuditLevelField.GetText()); lvl != "" {
		fmt.Fprintf(&b, "Current level filter: %s\n", lvl)
	}
	if tf := strings.TrimSpace(r.logAuditTimeField.GetText()); tf != "" {
		fmt.Fprintf(&b, "Current time filter: %s\n", tf)
	}
	b.WriteString(logAuditViewerAvailableActions)
	if r.activePage == logAuditViewerPage && r.settings.ConsigliereHardcoreMode {
		b.WriteString(r.consigliereMachiavelliLogLines())
	}
	return strings.TrimSpace(b.String())
}

// consigliereMachiavelliMaxLogLines caps how many actual log lines
// consigliereMachiavelliLogLines forwards — this is already an
// explicit, confirmed opt-in (see optioncatalog.go's own
// withConfirmEnable wiring), but an unbounded dump of a large,
// loosely-filtered log would still blow past any provider's own
// context budget and cost real money per question for no benefit.
const consigliereMachiavelliMaxLogLines = 50

// consigliereMachiavelliLogLines renders up to
// consigliereMachiavelliMaxLogLines of the entries currently matching
// the Keyword/Time/Level filters, verbatim (Entry.Raw, not just the
// extracted Message) — Machiavelli Mode's own, explicit exception to
// this screen's usual rule against ever forwarding a log line's own
// text (see logAuditContextSnapshot's own doc comment). Reuses
// logAuditVisibleEntries so this always matches exactly what the
// table itself is showing, never a separately-computed approximation
// of it.
func (r *Root) consigliereMachiavelliLogLines() string {
	shown, _, _ := r.logAuditVisibleEntries()
	if len(shown) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nMachiavelli Mode is on — the following currently-matching log lines are forwarded verbatim:\n")
	n := len(shown)
	if n > consigliereMachiavelliMaxLogLines {
		n = consigliereMachiavelliMaxLogLines
	}
	for _, e := range shown[:n] {
		fmt.Fprintf(&b, "- %s\n", e.Raw)
	}
	if len(shown) > n {
		fmt.Fprintf(&b, "(%d further matching lines not shown)\n", len(shown)-n)
	}
	return b.String()
}

// logAuditViewerAvailableActions is Log Holepunch's own fields and
// keys, verified directly against captureLogAuditViewerTableKey and
// the Keyword/Time/Level fields' own SetDoneFunc wiring in
// logauditscreen.go, not recalled from the user guide — this is a
// second, independent statement of the same facts
// docs/user-guide.md's own "Log Holepunch screen" section makes, the same
// "verified against source, kept deliberately redundant with the
// docs" status internal/ui/help.go's own helpText already has. Needs
// updating by hand if that key handling ever changes; nothing
// enforces the two stay in sync automatically.
const logAuditViewerAvailableActions = `Available fields and keys on this screen:
- Filter field: case-insensitive substring over Source and Message, narrows the list live.
- Time field: "before <moment>", "after <moment>", or "between <moment> and <moment>"; a moment is an absolute date/time or a relative one ("7 days", "2 hours ago", "30 minutes"); a bare relative moment on its own ("last 7 days") means "within that span".
- Level field (>=): a minimum severity — "warn" shows WARN and everything worse.
- Tab / Shift+Tab: move between Filter, Time, Level, the timeline, and the list.
- e: jump to the next error. w: jump to the next warning.
- Enter (on the list): show the current entry's full text.
- r: re-read the same files. f: toggle follow mode (the "tail -f" equivalent, merged live across every selected file at once). s: show statistics.
- Escape: close, back to file selection.`

// gitBreachContextSnapshot describes the repository Git Breach has
// open: its root, current branch (plus ahead/behind if any), the
// Staged/Unstaged/Untracked/Conflicts counts, and a handful of recent
// commit subjects — only metadata already visible in the screen's
// own headers, never a diff's own content or a commit's full body,
// with Machiavelli Mode as the one deliberate exception (see
// consigliereMachiavelliGitBreachDiff).
func (r *Root) gitBreachContextSnapshot() string {
	var b strings.Builder
	b.WriteString("Current view: Git Breach.\n")
	if r.gitBreachRoot != "" {
		fmt.Fprintf(&b, "Repository: %s\n", r.gitBreachRoot)
	}
	fmt.Fprintf(&b, "Branch: %s", r.gitBreachStatus.Branch)
	if r.gitBreachStatus.Ahead > 0 || r.gitBreachStatus.Behind > 0 {
		fmt.Fprintf(&b, " (ahead %d, behind %d)", r.gitBreachStatus.Ahead, r.gitBreachStatus.Behind)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "Staged: %d, Unstaged: %d, Untracked: %d, Conflicts: %d\n",
		len(r.gitBreachStatus.Staged), len(r.gitBreachStatus.Unstaged),
		len(r.gitBreachStatus.Untracked), len(r.gitBreachStatus.Conflicts))
	if len(r.gitBreachCommits) > 0 {
		n := len(r.gitBreachCommits)
		if n > consigliereMaxRecentCommits {
			n = consigliereMaxRecentCommits
		}
		subjects := make([]string, 0, n)
		for _, c := range r.gitBreachCommits[:n] {
			subjects = append(subjects, c.Subject)
		}
		fmt.Fprintf(&b, "Recent commits: %s\n", strings.Join(subjects, "; "))
	}
	if r.settings.ConsigliereHardcoreMode {
		b.WriteString(r.consigliereMachiavelliGitBreachDiff())
	}
	return strings.TrimSpace(b.String())
}

// consigliereMachiavelliMaxDiffLines caps how many lines of the
// currently shown diff consigliereMachiavelliGitBreachDiff forwards —
// same reasoning as consigliereMachiavelliMaxLogLines: this is
// already an explicit, confirmed opt-in, but an unbounded multi-file
// commit diff would still blow past any provider's own context
// budget for no benefit.
const consigliereMachiavelliMaxDiffLines = 300

// consigliereMachiavelliGitBreachDiff renders Main's own currently
// shown diff, verbatim, once Machiavelli Mode is on — "" whenever
// nothing real is actually shown there (no selection, a conflict row,
// cancelled) rather than forwarding an empty diff block. The same
// explicit exception this function's own caller
// (gitBreachContextSnapshot) makes to the rest of this app's usual
// "never a line's own content"
// rule: a diff, like a log line, can carry real secrets (a changed
// .env file, a hardcoded credential being added or removed) that
// would otherwise never leave this machine.
func (r *Root) consigliereMachiavelliGitBreachDiff() string {
	if r.gitBreachDiffRawText == "" {
		return ""
	}
	lines := strings.Split(r.gitBreachDiffRawText, "\n")
	var b strings.Builder
	b.WriteString("\nMachiavelli Mode is on — the diff currently shown in Main is forwarded verbatim:\n")
	n := len(lines)
	if n > consigliereMachiavelliMaxDiffLines {
		n = consigliereMachiavelliMaxDiffLines
	}
	b.WriteString(strings.Join(lines[:n], "\n"))
	b.WriteString("\n")
	if len(lines) > n {
		fmt.Fprintf(&b, "(%d further diff lines not shown)\n", len(lines)-n)
	}
	return b.String()
}

// consigliereMaxFirewallRules caps how many rules
// firewallContextSnapshot lists individually — the same
// anthropicMaxTokens-style reasoning every other capped list here
// already follows.
const consigliereMaxFirewallRules = 50

// firewallContextSnapshot describes the Firewall screen: which real
// backend currently governs traffic, and how many rules it has —
// never the ruleset itself by default, the same boundary Log
// Holepunch's log lines and Git Breach's diff content already keep,
// with Machiavelli Mode as the identical opt-in exception (see
// consigliereMachiavelliFirewallRules): a full ruleset describes this
// host's complete inbound attack surface (source/destination CIDR,
// open ports, internal subnet/VPN layout), not something to hand a
// third-party provider on every question asked from this screen
// without the user explicitly choosing to.
func (r *Root) firewallContextSnapshot() string {
	var b strings.Builder
	b.WriteString("Current view: Firewall.\n")
	if r.firewallErr != nil {
		fmt.Fprintf(&b, "Could not read the current ruleset: %v\n", r.firewallErr)
		return strings.TrimSpace(b.String())
	}
	if r.firewallSnapshot.Backend == firewall.BackendNone {
		b.WriteString("No supported firewall backend (ufw/nftables/iptables) is currently active on this host.\n")
		return strings.TrimSpace(b.String())
	}
	fmt.Fprintf(&b, "Backend: %s\n", r.firewallSnapshot.Backend)
	fmt.Fprintf(&b, "%d rules configured.\n", len(r.firewallSnapshot.Rules))
	if r.settings.ConsigliereHardcoreMode {
		b.WriteString(r.consigliereMachiavelliFirewallRules())
	}
	return strings.TrimSpace(b.String())
}

// consigliereMachiavelliFirewallRules renders the actual ruleset,
// verbatim, once Machiavelli Mode is on — the full rule list (source/
// destination CIDR, port, interface) is withheld by default here the
// same way a log line or diff's own content already is, since it
// describes this host's complete inbound attack surface rather than
// something as low-stakes as a rule count: an admin's own LAN/VPN
// layout is exactly what a silently-included context would otherwise
// hand to a third-party provider on every single question asked from
// this screen, not just ones actually about the ruleset.
func (r *Root) consigliereMachiavelliFirewallRules() string {
	rules := r.firewallSnapshot.Rules
	if len(rules) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nMachiavelli Mode is on — the full ruleset is forwarded verbatim:\n")
	n := len(rules)
	if n > consigliereMaxFirewallRules {
		n = consigliereMaxFirewallRules
	}
	for _, rule := range rules[:n] {
		shadowed := ""
		if rule.ShadowedByOrder != 0 {
			shadowed = fmt.Sprintf(" [shadowed by #%d %s]", rule.ShadowedByOrder, rule.ShadowedByAction)
		}
		protocol := rule.Protocol
		if protocol == "" {
			protocol = "any"
		}
		fmt.Fprintf(&b, "#%d %s %s %s port=%s from=%s to=%s via=%s%s\n",
			rule.Order, rule.Direction, rule.Action, protocol,
			rule.PortLabel(r.firewallServices), rule.SourceLabel(), rule.DestinationLabel(), rule.InterfaceLabel(), shadowed)
	}
	if len(rules) > n {
		fmt.Fprintf(&b, "(%d further rules not shown)\n", len(rules)-n)
	}
	return b.String()
}

// errConsigliereNoAPIKey is what buildConsigliereProvider's own
// KeyFunc returns when apikey.Resolve finds no key in any of the
// three tiers (apikey.TierNone) — surfaced verbatim as the reply text
// (see submitConsigliereMessage's "Fehler: %v" below), naming exactly
// where to fix it rather than letting the request fail with a bare,
// confusing 401 from the provider itself. Deliberately never silently
// falls back to consigliere.Mock{} instead: a placeholder reply that
// reads like a real answer would be exactly the kind of silent
// failure CLAUDE.md's "keine stillen Fehler" rule rules out.
var errConsigliereNoAPIKey = errors.New("no API key configured — set one in Options → AI Consigliere")

// errConsigliereLocked is the more specific case of the above: an
// encrypted entry exists for this provider (apiKeySession.HasProvider
// says so), it's just not unlocked yet this run — see Session's own
// "prompt once per session" contract. Telling this apart from
// errConsigliereNoAPIKey matters: "no API key configured" would
// wrongly suggest nothing was ever set up at all, when a restart is
// really the only thing standing between here and a real reply.
var errConsigliereLocked = errors.New("API key is stored but locked — unlock it in Options → AI Consigliere")

// buildConsigliereProvider constructs the real Provider for whichever
// backend r.settings.ConsigliereProvider currently names, built fresh
// on every call rather than cached on Root: the provider/model/key-
// file settings, and whether the encrypted tier is unlocked, can all
// change between one question and the next (an Options screen edit,
// or an unlock, mid-session).
//
// The returned KeyFunc calls apikey.Resolve at call time — i.e. when
// Provider.Reply actually sends the request, not here — so a key in
// the encrypted tier that's still locked when the sidebar opens, but
// gets unlocked before this particular question is asked, is picked
// up correctly instead of staying permanently unresolved for the rest
// of the run (see consigliere.KeyFunc's own doc comment).
func (r *Root) buildConsigliereProvider() consigliere.Provider {
	providerName := r.settings.ConsigliereProvider
	model := r.settings.ConsigliereModel
	effort := r.settings.ConsigliereEffort
	keyFile := r.settings.ConsigliereAPIKeyFile
	session := r.apiKeySession

	keyFunc := func() (string, error) {
		key, tier, err := apikey.Resolve(consigliereEnvVarForProvider(providerName), keyFile, providerName, session)
		if err != nil {
			return "", err
		}
		if tier != apikey.TierNone {
			return key, nil
		}
		if session != nil && session.HasProvider(providerName) {
			return "", errConsigliereLocked
		}
		return "", errConsigliereNoAPIKey
	}

	switch providerName {
	case "openai":
		return consigliere.OpenAICompat{
			BaseURL: consigliere.OpenAIDefaultBaseURL,
			Model:   firstNonEmptyConsigliereModel(model, consigliere.OpenAIDefaultModel),
			Effort:  effort,
			APIKey:  keyFunc,
		}
	case "deepseek":
		return consigliere.OpenAICompat{
			BaseURL: consigliere.DeepSeekDefaultBaseURL,
			Model:   firstNonEmptyConsigliereModel(model, consigliere.DeepSeekDefaultModel),
			Effort:  effort,
			APIKey:  keyFunc,
		}
	case "gemini":
		return consigliere.OpenAICompat{
			BaseURL: consigliere.GeminiDefaultBaseURL,
			Model:   firstNonEmptyConsigliereModel(model, consigliere.GeminiDefaultModel),
			Effort:  effort,
			APIKey:  keyFunc,
		}
	case "ollama":
		// No real key, ever (see OllamaDefaultBaseURL's own doc
		// comment) — a constant placeholder, never internal/apikey at
		// all, so a local server with nothing configured in Options
		// still works immediately rather than reporting
		// errConsigliereNoAPIKey for a credential this provider never
		// needed in the first place.
		return consigliere.OpenAICompat{
			BaseURL: consigliere.OllamaDefaultBaseURL,
			Model:   firstNonEmptyConsigliereModel(model, consigliere.OllamaDefaultModel),
			Effort:  effort,
			APIKey:  func() (string, error) { return "ollama", nil },
		}
	default: // "anthropic", and anything unrecognized (e.g. a hand-edited config)
		return consigliere.Anthropic{
			Model:  model, // "" already means "use AnthropicDefaultModel" — see Anthropic.Reply
			Effort: effort,
			APIKey: keyFunc,
		}
	}
}

// firstNonEmptyConsigliereModel returns override if set, otherwise
// fallback — OpenAICompat (unlike Anthropic.Reply) expects its caller
// to always supply a concrete Model, since OpenAI and DeepSeek each
// need a different default (see OpenAICompat's own doc comment).
func firstNonEmptyConsigliereModel(override, fallback string) string {
	if override != "" {
		return override
	}
	return fallback
}

// submitConsigliereUnlock is Enter's own action while
// consigliereAwaitingPassphrase is set (see showConsigliere's own doc
// comment on how the sidebar ends up starting in this mode) — tries
// the typed text as the encrypted tier's passphrase instead of
// treating it as a chat question.
//
// A blank attempt is quietly ignored rather than treated as a wrong
// passphrase: Enter on an empty field is far more likely a stray
// keypress than a deliberate attempt. A wrong passphrase, unlike a
// real question, never leaves the typed text sitting in the field —
// retrying the real question later is the normal case, but retrying a
// rejected passphrase with the exact same (wrong) text would never
// succeed, so there's nothing worth preserving it for.
func (r *Root) submitConsigliereUnlock() {
	passphrase := r.consigliereInput.GetText()
	if passphrase == "" {
		return
	}
	r.consigliereInput.SetText("")

	if err := r.apiKeySession.Unlock(passphrase); err != nil {
		if errors.Is(err, apikey.ErrWrongPassphrase) {
			r.consigliereInput.SetPlaceholder("Wrong passphrase — try again…")
		} else {
			r.consigliereInput.SetPlaceholder(err.Error())
		}
		return
	}

	r.consigliereAwaitingPassphrase = false
	r.consigliereInput.SetMaskCharacter(0)
	r.consigliereInput.SetPlaceholder(consigliereInputPlaceholder)
}

// submitConsigliereMessage appends text as a user turn, re-renders the
// history immediately so the question shows up without waiting for the
// provider, then asks consigliereProvider for a reply off the UI
// goroutine — context/cancel per CLAUDE.md's own async rule, and a
// stale reply from a superseded question (the user already asked a new
// one, or closed the sidebar — see hideConsigliere) is dropped by the
// ctx.Err() check below rather than overwriting newer content.
func (r *Root) submitConsigliereMessage(text string) {
	if r.consigliereCancel != nil {
		r.consigliereCancel()
	}

	r.consigliereMessages = append(r.consigliereMessages, consigliere.Message{Role: consigliere.RoleUser, Text: text})
	r.renderConsigliereHistory()

	ctx, cancel := context.WithCancel(context.Background())
	r.consigliereCancel = cancel

	// Snapshotted now, not inside the goroutine below: history/view
	// state reflect this exact moment, the same "rebuilt fresh on
	// every new question" promise Request's own doc comment already
	// makes — consigliereDocSnapshot is the one piece that moves into
	// the goroutine itself, since its own translation fallback may
	// make a real network call (see its own doc comment), which must
	// never block the UI thread.
	history := r.consigliereMessages
	viewSnapshot := r.consigliereContextSnapshot()
	systemPrompt := config.LoadConsigliereSystemPrompt(consigliereSystemPromptPath())
	provider := r.buildConsigliereProvider()

	r.safeGo("AI Consigliere reply", func() { r.consigliereCancel = nil }, func() {
		req := consigliere.Request{
			History:  history,
			Snapshot: joinConsigliereSnapshotParts(viewSnapshot, r.consigliereDocSnapshot(ctx, provider, text)),
			System:   systemPrompt,
		}
		reply, err := provider.Reply(ctx, req)
		r.app.QueueUpdateDraw(func() {
			if ctx.Err() != nil {
				return // superseded by a newer question, or the sidebar was closed
			}
			r.consigliereCancel = nil
			if err != nil {
				reply = fmt.Sprintf("Fehler: %v", err)
			}
			r.consigliereMessages = append(r.consigliereMessages, consigliere.Message{Role: consigliere.RoleAssistant, Text: reply})
			r.renderConsigliereHistory()
		})
	})
}

// renderConsigliereHistory redraws consigliereHistory from
// consigliereMessages and scrolls to the end, so the most recent turn
// (question or reply) is always the one left visible — the same
// "newest content stays in view" behavior a real chat window has.
func (r *Root) renderConsigliereHistory() {
	var b strings.Builder
	// consigliereSplashArt (see consiglieresplash.go) always goes first,
	// never as a message of its own — per the user's own explicit
	// request, it simply scrolls out of view as the conversation below
	// it grows, exactly like any other content ScrollToEnd pushes
	// upward.
	b.WriteString(consigliereSplashArt(r.consigliereHistoryContentWidth()))
	if len(r.consigliereMessages) > 0 {
		b.WriteString("\n")
	}
	for i, m := range r.consigliereMessages {
		if i > 0 {
			b.WriteString("\n\n")
		}
		// Label4Background/Label5Background ("Blue Moon"/"Orange Crush"
		// — see config.DefaultSettings' own Label4Name/Label5Name) per
		// the user's own explicit request: neutral, easily told apart,
		// and both dark/saturated enough that a plain white foreground
		// (not the theme's own TextColor, which a custom scheme could
		// set close to either background) stays legible on both — the
		// same reused-label-color idea renderGitBreachTheme's own
		// "maximally-distinct" comment already relies on elsewhere,
		// just picked for being visually neutral here rather than
		// maximally distinct from a whole set.
		speaker, labelBackground := "You", r.theme.Label5Background
		if m.Role == consigliere.RoleAssistant {
			speaker, labelBackground = "Consigliere", r.theme.Label4Background
		}
		fmt.Fprintf(&b, "[white:%s:b]%s:[-:-:-] %s", colorTag(labelBackground), speaker, tview.Escape(m.Text))
	}
	r.consigliereHistory.SetText(b.String())
	r.consigliereHistory.ScrollToEnd()
}
