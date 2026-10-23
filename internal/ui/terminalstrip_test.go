package ui

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// terminalStripText mirrors stripText (tabstrip_test.go) — strips the
// inline color tags renderTerminalStrip bakes in, leaving just the
// plain glyphs.
func terminalStripText(count, active int) string {
	text, _ := renderTerminalStrip(count, active)
	var b strings.Builder
	inTag := false
	for _, r := range text {
		switch {
		case r == '[':
			inTag = true
		case r == ']':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestRenderTerminalStripAlwaysShowsThePlusEvenWithNoTerminals(t *testing.T) {
	if got, want := terminalStripText(0, -1), terminalStripNewLabel; got != want {
		t.Errorf("renderTerminalStrip(0, -1) = %q, want %q", got, want)
	}
}

func TestRenderTerminalStripPutsThePlusBeforeTheNumbers(t *testing.T) {
	if got, want := terminalStripText(3, 0), "+ 1 2 3"; got != want {
		t.Errorf("renderTerminalStrip(3, 0) = %q, want %q (the + leads, unlike the tab strip)", got, want)
	}
}

func TestRenderTerminalStripHighlightsOnlyTheActiveTerminal(t *testing.T) {
	text, _ := renderTerminalStrip(4, 2)

	tag := ":" + colorTag(tcell.ColorDimGray) + "]"
	if got := strings.Count(text, tag); got != 1 {
		t.Fatalf("found %d gray-background segments in %q, want exactly 1 (the active terminal)", got, text)
	}
	idx := strings.Index(text, tag)
	if rest := text[idx+len(tag):]; !strings.HasPrefix(rest, "3") {
		t.Errorf("the highlight precedes %q, want it on terminal 3", rest[:1])
	}
}

func TestRenderTerminalStripNoHighlightWhenNoneIsActive(t *testing.T) {
	text, _ := renderTerminalStrip(3, -1)
	tag := ":" + colorTag(tcell.ColorDimGray) + "]"
	if strings.Count(text, tag) != 0 {
		t.Errorf("renderTerminalStrip(3, -1) = %q, want no active-terminal highlight at all", text)
	}
}

func TestRenderTerminalStripSpansMapClicksToTerminals(t *testing.T) {
	text, spans := renderTerminalStrip(3, -1)
	plain := terminalStripText(3, -1)

	if got, want := len(spans), 4; got != want { // "+" plus three terminals
		t.Fatalf("got %d spans, want %d — %q", got, want, text)
	}
	first := spans[0]
	if first.shell != terminalStripSpanNew {
		t.Errorf("first span targets %d, want terminalStripSpanNew", first.shell)
	}
	if got := plain[first.start:first.end]; got != terminalStripNewLabel {
		t.Errorf("first span covers %q, want %q", got, terminalStripNewLabel)
	}
	for i, span := range spans[1:] {
		if span.shell != i {
			t.Errorf("span %d targets terminal %d, want %d", i+1, span.shell, i)
		}
		if got, want := plain[span.start:span.end], string(rune('1'+i)); got != want {
			t.Errorf("span %d covers %q, want %q", i+1, got, want)
		}
	}
}

func TestTerminalStripWindowNeverExceedsMaxVisible(t *testing.T) {
	const count = 25
	for active := 0; active < count; active++ {
		first, last := terminalStripWindow(count, active)
		if n := last - first + 1; n != terminalStripMaxVisible {
			t.Errorf("active %d: window holds %d terminals, want %d", active, n, terminalStripMaxVisible)
		}
		if active < first || active > last {
			t.Errorf("active %d: window [%d,%d] doesn't contain it", active, first, last)
		}
	}
}

func TestRenderTerminalStripWindowsPastTheVisibleLimit(t *testing.T) {
	const count = 20
	got := terminalStripText(count, 10)
	if !strings.Contains(got, terminalStripMoreBefore) || !strings.Contains(got, terminalStripMoreAfter) {
		t.Errorf("strip = %q, want both overflow markers with the active terminal mid-list", got)
	}
	if !strings.Contains(got, "11") {
		t.Errorf("strip = %q, want it to contain the active terminal's own number", got)
	}
}

func TestRenderTerminalStripHandlesOutOfRangeActive(t *testing.T) {
	// Out of range but non-negative falls back to "no highlight", not a
	// guess at the wrong terminal — unlike the tab strip, which always
	// has a real tab to fall back to.
	got := terminalStripText(2, 99)
	if want := "+ 1 2"; got != want {
		t.Errorf("out-of-range active: %q, want %q", got, want)
	}
}

// --- Panel-level, mirroring tabstrip_test.go's own equivalents ---

func TestPanelTerminalStripShowsJustPlusWithNoTerminals(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	want := strings.Repeat(" ", headerTerminalStripGap) + terminalStripNewLabel
	if got := r.panel.terminalStrip.GetText(true); got != want {
		t.Errorf("terminal strip with no terminals = %q, want %q", got, want)
	}
	if len(r.panel.terminalStripSpans) != 1 || r.panel.terminalStripSpans[0].shell != terminalStripSpanNew {
		t.Errorf("terminal strip spans = %v, want a single span targeting terminalStripSpanNew", r.panel.terminalStripSpans)
	}
}

func TestTerminalStripPlusClickOpensTheNewTerminalDialog(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.terminalStrip.SetRect(0, 0, 10, 1)

	r.panel.captureTerminalStripMouse(
		tview.MouseLeftClick,
		tcell.NewEventMouse(headerTerminalStripGap, 0, tcell.ButtonNone, 0),
	)

	if r.activePage != sshShellDialogPage {
		t.Errorf("activePage after clicking the terminal strip's + = %q, want %q", r.activePage, sshShellDialogPage)
	}
	if r.sshShellSubmit == nil {
		t.Error("sshShellSubmit wasn't set — the dialog would fall back to the default ssh launcher")
	}
}

func TestTerminalStripClickInTheGapOpensTheSwitcher(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.terminalStrip.SetRect(0, 0, 10, 1)

	gap := r.panel.terminalStripSpans[0].end
	r.panel.captureTerminalStripMouse(
		tview.MouseLeftClick,
		tcell.NewEventMouse(gap, 0, tcell.ButtonNone, 0),
	)

	if r.activePage != terminalSwitcherPage {
		t.Errorf("activePage = %q, want %q", r.activePage, terminalSwitcherPage)
	}
}

func TestTerminalStripClickSwitchesTerminals(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })

	r.openNewShellEmbedded(testShellConn("one"))
	r.openNewShellEmbedded(testShellConn("two"))
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()
	if r.activeShell != 1 {
		t.Fatalf("setup: activeShell = %d, want 1", r.activeShell)
	}

	r.panel.terminalStrip.SetRect(0, 0, 20, 1)
	span := r.panel.terminalStripSpans[1] // terminal 1 (the first one, index 0)
	captured, _ := r.panel.captureTerminalStripMouse(
		tview.MouseLeftClick,
		tcell.NewEventMouse(span.start, 0, tcell.ButtonNone, 0),
	)

	if captured != tview.MouseConsumed {
		t.Error("a click on the terminal strip should be consumed")
	}
	if r.activeShell != 0 {
		t.Errorf("activeShell after clicking terminal 1 = %d, want 0", r.activeShell)
	}
}

func TestRefreshTerminalStripsUpdatesEveryOpenPanel(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })
	r.newTab(r.panel.path) // a second panel, so there's more than one to keep in sync

	r.openNewShellEmbedded(testShellConn("one"))
	defer func() { _ = r.shells[0].session.Close() }()

	for i, p := range r.tabs {
		if p.terminalCount != 1 {
			t.Errorf("tab %d: terminalCount = %d, want 1", i, p.terminalCount)
		}
		if p.terminalActive != 0 {
			t.Errorf("tab %d: terminalActive = %d, want 0", i, p.terminalActive)
		}
	}
}
