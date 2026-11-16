package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// openCompareDiffWithLongLines opens Compare's own side-by-side diff on
// a pair whose lines are far too wide for either pane, which is the
// only situation soft wrap changes anything in.
//
// XDG_CONFIG_HOME is redirected first: toggling wrap persists the new
// value the moment it changes (see setCompareDiffWrap), and a test has
// no business writing into the real user's config file.
func openCompareDiffWithLongLines(t *testing.T, width, height int) *Root {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	r, dir := newCompareRoot(t)
	var left, right strings.Builder
	// Enough pairs that the diff overflows the pane even unwrapped —
	// otherwise tview clamps any scroll offset back to the few rows
	// that actually exist below the fold, and a test about keeping a
	// reading position would be testing that clamp instead.
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&left, "line %d %s\n", i, strings.Repeat("alpha ", 25))
		fmt.Fprintf(&right, "line %d %s\n", i, strings.Repeat("omega ", 25))
	}
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte(left.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(right.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	r.openCompareFile(a, b)
	r.openCompareDiff()

	r.SetRect(0, 0, width, height)
	r.compare.DiffLayout.SetRect(0, 0, width, height)
	return r
}

func compareDiffPaneRows(view *tview.TextView) []string {
	return strings.Split(strings.TrimSuffix(view.GetText(true), "\n"), "\n")
}

// TestCompareDiffWrapRendersThroughTheDrawHook pins the whole chain
// that makes soft wrap actually appear: the panes are only ever told
// their real width while drawing, so turning wrap on before the first
// draw has to still produce a wrapped render on that draw rather than
// waiting for some later event to notice.
func TestCompareDiffWrapRendersThroughTheDrawHook(t *testing.T) {
	r := openCompareDiffWithLongLines(t, 100, 30)

	drawCompareDiff(t, r, 100, 30)
	if r.compare.DiffWrapWidth != 0 {
		t.Fatalf("DiffWrapWidth = %d before turning wrap on, want 0", r.compare.DiffWrapWidth)
	}
	unwrapped := len(compareDiffPaneRows(r.compare.DiffLeft))

	r.toggleCompareDiffWrap()
	drawCompareDiff(t, r, 100, 30)

	if r.compare.DiffWrapWidth <= 0 {
		t.Fatalf("DiffWrapWidth = %d after turning wrap on and drawing, want a real width", r.compare.DiffWrapWidth)
	}
	if got := len(compareDiffPaneRows(r.compare.DiffLeft)); got <= unwrapped {
		t.Errorf("wrapped render has %d rows, unwrapped had %d — want more", got, unwrapped)
	}
}

// TestCompareDiffWrapKeepsBothPanesRowAligned is the invariant the
// whole two-pane view rests on: row N on the left and row N on the
// right have to stay counterparts, even though the two sides wrap
// different numbers of times.
func TestCompareDiffWrapKeepsBothPanesRowAligned(t *testing.T) {
	r := openCompareDiffWithLongLines(t, 100, 30)
	r.toggleCompareDiffWrap()
	drawCompareDiff(t, r, 100, 30)

	left := compareDiffPaneRows(r.compare.DiffLeft)
	right := compareDiffPaneRows(r.compare.DiffRight)
	if len(left) != len(right) {
		t.Fatalf("left pane has %d rows, right pane %d — they must match", len(left), len(right))
	}

	paneWidth := r.compare.DiffPaneWidth
	if paneWidth <= 0 {
		t.Fatalf("DiffPaneWidth = %d after drawing, want a real width", paneWidth)
	}
	for i, row := range left {
		if got := tview.TaggedStringWidth(row); got > paneWidth {
			t.Errorf("left row %d is %d columns wide, pane is %d", i, got, paneWidth)
		}
		if got := tview.TaggedStringWidth(right[i]); got > paneWidth {
			t.Errorf("right row %d is %d columns wide, pane is %d", i, got, paneWidth)
		}
	}
}

// TestToggleCompareDiffWrapKeepsTheReadingPosition pins that the
// toggle is a display preference, not a navigation accident: the line
// pair at the top of the view stays there, even though wrapping changes
// which row number that is.
func TestToggleCompareDiffWrapKeepsTheReadingPosition(t *testing.T) {
	r := openCompareDiffWithLongLines(t, 100, 30)
	drawCompareDiff(t, r, 100, 30)

	const startRow = 12
	r.compare.DiffLeft.ScrollTo(startRow, 0)
	r.compare.DiffRight.ScrollTo(startRow, 0)
	wantPair := diffPairAtRow(r.compare.DiffPairStartRows, startRow)

	r.toggleCompareDiffWrap()
	drawCompareDiff(t, r, 100, 30)

	row, _ := r.compare.DiffLeft.GetScrollOffset()
	if got := diffPairAtRow(r.compare.DiffPairStartRows, row); got != wantPair {
		t.Errorf("after turning wrap on the top row is pair %d, want %d", got, wantPair)
	}

	r.toggleCompareDiffWrap()
	drawCompareDiff(t, r, 100, 30)

	row, _ = r.compare.DiffLeft.GetScrollOffset()
	if got := diffPairAtRow(r.compare.DiffPairStartRows, row); got != wantPair {
		t.Errorf("after turning wrap back off the top row is pair %d, want %d", got, wantPair)
	}
}

// TestSetDiffWrapWithoutEverOpeningTheScreens pins the Options
// screen's own path into both toggles: it can reach them with neither
// diff screen ever having been shown, so every widget and slice they
// touch has to tolerate being empty rather than panicking on a
// never-populated view.
func TestSetDiffWrapWithoutEverOpeningTheScreens(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	r, _ := newCompareRoot(t)

	r.setCompareDiffWrap(true)
	if !r.settings.CompareDiffWrap {
		t.Error("compare_diff_wrap did not take")
	}
	r.setGitBreachDiffWrap(true)
	if !r.settings.GitBreachDiffWrap {
		t.Error("git_breach_diff_wrap did not take")
	}

	r.setCompareDiffWrap(false)
	r.setGitBreachDiffWrap(false)
	if r.settings.CompareDiffWrap || r.settings.GitBreachDiffWrap {
		t.Error("turning both back off did not take")
	}
}

// TestCompareDiffWrapIsReachableByKeyAndByButton pins both halves of
// the user-facing requirement: "w" does it, and so does clicking the
// title bar's own glyph — and the hint bar says which key that is.
func TestCompareDiffWrapIsReachableByKeyAndByButton(t *testing.T) {
	r := openCompareDiffWithLongLines(t, 100, 30)

	if hint := r.compare.DiffHint.GetText(true); !strings.Contains(hint, "wrap long lines") {
		t.Errorf("hint bar = %q, want it to advertise wrapping", hint)
	}

	// "w" through the pane's own real input capture, not a direct call
	// to the toggle — the key has to actually be wired up.
	handler := r.captureCompareDiffPaneKey(true)
	if ev := handler(tcell.NewEventKey(tcell.KeyRune, 'w', tcell.ModNone)); ev != nil {
		t.Error("the diff pane passed 'w' through instead of consuming it")
	}
	if !r.settings.CompareDiffWrap {
		t.Fatal("'w' did not turn soft wrap on")
	}
	if hint := r.compare.DiffHint.GetText(true); !strings.Contains(hint, "stop wrapping") {
		t.Errorf("hint bar = %q, want it to offer turning wrapping back off", hint)
	}

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	t.Cleanup(screen.Fini)
	screen.SetSize(100, 30)
	r.handleBeforeDraw(screen) // establishes lastScreenWidth, which the title bar's column math needs
	r.renderCompareDiffTitleBar()

	title := r.compare.DiffTitleBar.GetText(true)
	col := compareDiffWrapButtonCol(r.lastScreenWidth)
	if col >= len([]rune(title)) {
		t.Fatalf("title bar %q is shorter than the wrap button's own column %d", title, col)
	}
	if got := []rune(title)[col]; got != compareDiffWrapOnGlyph {
		t.Errorf("title bar column %d holds %q, want the wrap-on glyph %q", col, got, compareDiffWrapOnGlyph)
	}

	// And the same column, clicked, toggles back.
	r.compare.DiffTitleBar.SetRect(0, 0, r.lastScreenWidth, 1)
	mouse := r.captureCompareDiffTitleBarMouse()
	action, _ := mouse(tview.MouseLeftClick, tcell.NewEventMouse(col, 0, tcell.Button1, tcell.ModNone))
	if action != tview.MouseConsumed {
		t.Errorf("click on the wrap glyph returned %v, want it consumed", action)
	}
	if r.settings.CompareDiffWrap {
		t.Error("clicking the wrap glyph did not turn soft wrap back off")
	}
}
