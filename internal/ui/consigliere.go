// consigliere.go is the AI Consigliere's own sidebar — a chat panel
// reachable via "gC" (see keymap.go) from any view, not just the file
// panel, per feature_ideas.txt's own explicit "NICHT an die
// Details-Sidebar gekoppelt" requirement (that note is about
// reachability, not shape: the spec explicitly asks to reuse the
// Details sidebar's own overlay/scroll infrastructure for the chat
// window itself — see detailssidebar.go). Docked flush against the
// right edge, non-modal (its own show/hide path, not pushOverlay/
// hideOverlay/activePage/overlayStack), the same shape Details uses —
// not a centered modal dialog. This first slice is UI-only:
// r.consigliereProvider is consigliere.Mock{} (see NewRoot), so no
// network call, API key, or consent checkbox exists yet — all three
// are still open product decisions per the spec. The point of this
// slice is purely to see and judge the panel's own layout, scrolling,
// and chat mechanics before any of that is decided.
package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/consigliere"
)

const consiglierePage = "consigliere"

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
func (r *Root) startConsigliereChord() {
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
	r.consigliereHistory.SetFocusFunc(func() { r.consigliereTitleBar.SetBackgroundColor(r.theme.InputFocusedBackground) })
	r.consigliereHistory.SetBlurFunc(func() { r.consigliereTitleBar.SetBackgroundColor(r.theme.InputBackground) })
	r.consigliereScrollbar = newVerticalScrollbarForTextView(r, r.consigliereHistory)

	r.consigliereTitleBar = newPlainTitleBar("AI Consigliere")
	r.consigliereTitleBar.SetDynamicColors(false)
	r.consigliereTitleBar.SetMouseCapture(r.captureConsigliereTitleBarMouse)

	r.consigliereInput = tview.NewInputField()
	r.consigliereInput.SetPlaceholder("Ask a question… (Enter to send, Esc to close)")
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
		r.hideConsigliere()
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
		// Tab exits consigliere's own tiny loop the same host-agnostic
		// way handleConsigliereInputDone's identical fallback does.
		if event.Key() == tcell.KeyBacktab {
			r.app.SetFocus(r.consigliereInput)
		} else {
			r.app.SetFocus(r.consigliereExitFocus())
		}
		return nil
	}
	return event
}

// consigliereExitFocus is where Tab/Backtab leaving consigliere's own
// input/history pair lands: whatever had real keyboard focus the
// moment showConsigliere last ran (consigliereReturnFocus — see its
// own doc comment in root.go), falling back to the panel only if
// nothing was ever recorded. The one place this logic lives, shared
// by handleConsigliereInputDone and captureConsigliereHistoryKey, so
// every host screen's own exit path behaves identically without
// either of them needing to know which screen is actually hosting it.
func (r *Root) consigliereExitFocus() tview.Primitive {
	if r.consigliereReturnFocus != nil {
		return r.consigliereReturnFocus
	}
	return r.panel.table
}

// renderConsigliereTitleBar sets consigliereTitleBar's own text,
// padded out to width columns with the close glyph placed
// toolWindowCloseButtonCol's own one-column-in-from-the-edge spacing —
// the exact same convention renderHelpTitleBar/renderDetailsTitleBar
// already follow, reused here rather than duplicated with a different
// number.
func (r *Root) renderConsigliereTitleBar(width int) {
	const label = " AI Consigliere "
	closeCol := toolWindowCloseButtonCol(0, width)
	padding := closeCol - len(label)
	if padding < 0 {
		padding = 0
	}
	r.consigliereTitleBar.SetText(label + strings.Repeat(" ", padding) + string(toolWindowCloseGlyph) + " ")
}

// captureConsigliereTitleBarMouse closes the sidebar when the click
// lands exactly on its own close glyph — see
// captureDetailsTitleBarMouse's own doc comment for why every other
// click on this bar stays inert, and for the InRect-first shape, which
// matters here for the identical reason: without it, a MouseLeftDown
// landing inside this bar could let tview's own default
// TextView.MouseHandler steal real keyboard focus onto the title bar
// itself before hideConsigliere's own hadFocus check ever runs.
func (r *Root) captureConsigliereTitleBarMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if !r.consigliereTitleBar.InRect(event.Position()) {
		return action, event
	}
	if action != tview.MouseLeftClick {
		return tview.MouseConsumed, nil
	}
	x, y := event.Position()
	rectX, rectY, width, _ := r.consigliereTitleBar.GetRect()
	if y != rectY || x != rectX+toolWindowCloseButtonCol(0, width) {
		return action, event
	}
	r.hideConsigliere()
	return tview.MouseConsumed, nil
}

// consigliereSize sizes the sidebar against the whole screen (Root's
// own rect — see detailsSidebarSize's own doc comment on why Details
// uses the same base rather than clampToPanel): at least a third of
// the screen's width, full height top to bottom minus
// detailsSidebarBottomRows at the very end — the exact same floor/
// bottom-margin Details itself uses, reused rather than duplicated
// with different numbers, since both dock against the same screen
// edge above the same three reserved bottom rows (bashConsole/
// buttonBar/statusBar).
func (r *Root) consigliereSize() (width, height int) {
	_, _, screenWidth, screenHeight := r.GetRect()
	width = screenWidth / 3
	if width < consigliereMinWidth {
		width = consigliereMinWidth
	}
	height = screenHeight - detailsSidebarBottomRows
	if height < 1 {
		height = screenHeight
	}
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
// from consigliereSize, flush against the right edge of the screen —
// repositionDetailsSidebar's own exact shape, just for
// consigliereLayout instead of detailsSidebarLayout. Hooked into
// Root's own resize handling (see handleBeforeDraw) so a live terminal
// resize keeps it correctly positioned while open, the same as
// Details.
func (r *Root) repositionConsigliereSidebar() {
	width, height := r.consigliereSize()
	_, _, screenWidth, _ := r.GetRect()
	x, y, width, height := r.clampToScreen(screenWidth-width, 0, width, height)
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
		r.hideConsigliere()
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
	// Both sidebars dock to the exact same rect (flush right, same
	// width/height — see consigliereSize/detailsSidebarSize), so having
	// both visible at once would just stack one over the other rather
	// than showing anything useful of whichever is underneath — closing
	// Details here, rather than letting them silently overlap, is the
	// same "no silent broken state" reasoning hideConsigliere's own
	// cancel-in-flight cleanup already follows.
	if r.detailsSidebarVisible {
		r.hideDetailsSidebar()
	}

	// consigliereReturnFocus/consigliereHostPage are both captured
	// *before* anything below changes focus or activePage — see
	// hideConsigliere's own doc comment for why a hardcoded
	// r.panel.table fallback is wrong whenever this was opened from
	// inside a full-screen modal (Log Audit, Git Breach, ...) rather
	// than the bare file panel.
	r.consigliereReturnFocus = r.app.GetFocus()
	r.consigliereHostPage = r.activePage

	r.consigliereMessages = nil
	r.consigliereInput.SetText("")

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
	r.consigliereHostPage = ""
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
		r.hideConsigliere()
		return
	}
	if key == tcell.KeyBacktab {
		// Backward from the first stop in consigliere's own tiny
		// loop — exits the same host-agnostic way Tab's own "nothing
		// to scroll" case does, just one key earlier in the sequence.
		r.app.SetFocus(r.consigliereExitFocus())
		return
	}
	if key == tcell.KeyTab {
		if r.consigliereScrollbar.maxOffset() > 0 {
			r.app.SetFocus(r.consigliereHistory)
		} else {
			r.app.SetFocus(r.consigliereExitFocus())
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
func (r *Root) consigliereContextSnapshot() string {
	return "Placeholder context — no real view snapshot wired up yet (see feature_ideas.txt)."
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

	req := consigliere.Request{
		History:  r.consigliereMessages,
		Snapshot: r.consigliereContextSnapshot(),
	}
	provider := r.consigliereProvider

	r.safeGo("AI Consigliere reply", func() { r.consigliereCancel = nil }, func() {
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
		speaker := "You"
		if m.Role == consigliere.RoleAssistant {
			speaker = "Consigliere"
		}
		fmt.Fprintf(&b, "[::b]%s:[::-] %s", speaker, tview.Escape(m.Text))
	}
	r.consigliereHistory.SetText(b.String())
	r.consigliereHistory.ScrollToEnd()
}
