package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/git"
)

// newGitBreachBisectFixture builds a 5-commit repo (c1..c5, newest
// first in the Commits table — see git.Log's own ordering) and opens
// Git Breach on it, focused on the Commits table.
func newGitBreachBisectFixture(t *testing.T) *Root {
	t.Helper()
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	for i := 1; i <= 5; i++ {
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(fmt.Sprintf("commit %d\n", i)), 0o644); err != nil {
			t.Fatal(err)
		}
		runGitBreach(t, dir, "add", "f.txt")
		runGitBreach(t, dir, "commit", "-q", "-m", fmt.Sprintf("c%d", i))
	}

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreach.CommitsTable)
	return r
}

// TestOpenGitBreachBisectStartThenMarkThenEndRoundTrip exercises the
// whole feature through the UI layer — unlike the diff-fetching
// actions elsewhere in this package, every bisect action here runs
// synchronously (the same convention Stage/Unstage/Checkout/Delete
// already follow: a git bisect call is fast, not a diff fetch worth
// debouncing), so the full round trip is directly testable with no
// async-draining workaround needed.
func TestOpenGitBreachBisectStartThenMarkThenEndRoundTrip(t *testing.T) {
	r := newGitBreachBisectFixture(t)
	dir := r.gitBreach.Root

	if !r.gitBreachBisectAvailable() {
		t.Fatal("setup: a bisect is already available=false before starting one")
	}
	// Cursor on row 4 — c1, the oldest commit — as "good"; row 0 (c5,
	// the newest) is what HEAD/main already point at, so it has to be
	// a genuinely different commit from whatever gets picked as "bad"
	// below, or git itself refuses the range outright.
	r.gitBreach.CommitsTable.Select(4, 0)

	r.openGitBreachBisectStart()
	if r.activePage != pickerPage {
		t.Fatalf("activePage = %q, want %q (picking \"bad\")", r.activePage, pickerPage)
	}
	// Item 0: this repo has one branch (main, pointing at c5 — a
	// different commit from c1, the "good" ref above) and no tags, so
	// the picker's first entry is "main" itself.
	r.picker.SetCurrentItem(0)
	r.picker.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if !git.BisectInProgress(dir) {
		t.Fatal("BisectInProgress = false right after openGitBreachBisectStart, want true")
	}
	if got := r.gitBreach.DiffView.GetText(true); !strings.Contains(got, "Bisecting") {
		t.Errorf("Main = %q, want git's own \"Bisecting\" status shown", got)
	}
	if r.gitBreachBisectAvailable() {
		t.Error("gitBreachBisectAvailable = true while a bisect is running, want false")
	}

	r.openGitBreachBisectMark("bad")
	if !git.BisectInProgress(dir) {
		t.Fatal("BisectInProgress = false after marking bad, want still true (more revisions to test)")
	}

	r.openGitBreachBisectEnd()
	if git.BisectInProgress(dir) {
		t.Error("BisectInProgress = true after openGitBreachBisectEnd, want false")
	}
	if !r.gitBreachBisectAvailable() {
		t.Error("gitBreachBisectAvailable = false after ending the bisect, want true")
	}
}

// TestOpenGitBreachBisectStartDoesNothingWhileABisectIsAlreadyRunning
// pins the guard: a second "B" press mid-bisect is a no-op, not a
// second overlapping `git bisect start`.
func TestOpenGitBreachBisectStartDoesNothingWhileABisectIsAlreadyRunning(t *testing.T) {
	r := newGitBreachBisectFixture(t)
	r.gitBreach.CommitsTable.Select(4, 0) // c1 — see newGitBreachBisectFixture's own sibling test for why not row 0
	r.openGitBreachBisectStart()
	r.picker.SetCurrentItem(0)
	r.picker.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})
	if r.activePage != gitBreachPage {
		t.Fatalf("setup: activePage = %q, want back to %q after picking", r.activePage, gitBreachPage)
	}

	r.openGitBreachBisectStart()

	if r.activePage != gitBreachPage {
		t.Errorf("activePage = %q, want still %q — a second \"B\" mid-bisect should do nothing", r.activePage, gitBreachPage)
	}
}

// TestGitBreachBisectHintBarUpdatesThroughTheWholeLifecycle is the
// regression guard for a real, live-caught bug: the Commits table
// keeps keyboard focus throughout start/mark/end, so
// refreshGitBreachHint's usual trigger (a focus change) never fired
// on its own — the hint bar kept advertising "G mark good"/"X mark
// bad"/"E end bisect" after the bisect had already ended.
func TestGitBreachBisectHintBarUpdatesThroughTheWholeLifecycle(t *testing.T) {
	r := newGitBreachBisectFixture(t)

	if !gitBreachHintHasKey(r.gitBreachHintEntries(), "B") {
		t.Fatal("setup: \"B\" (bisect start) not advertised before any bisect")
	}

	r.gitBreach.CommitsTable.Select(4, 0) // c1
	r.openGitBreachBisectStart()
	r.picker.SetCurrentItem(0) // "main"
	r.picker.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})
	// Picking a ref always restores focus to Files (the same
	// restoreGitBreachFocus every overlay close falls back to) —
	// live-verified real usage needs an explicit Tab back to Commits
	// before "G"/"X"/"E" are reachable at all; mirrored here rather
	// than asserting against a focus state real usage never has.
	r.app.SetFocus(r.gitBreach.CommitsTable)

	if gitBreachHintHasKey(r.gitBreachHintEntries(), "B") {
		t.Error("\"B\" still advertised right after starting a bisect, want \"G\"/\"X\"/\"E\" instead")
	}
	if !gitBreachHintHasKey(r.gitBreachHintEntries(), "G") {
		t.Error("\"G\" (mark good) not advertised while a bisect is running")
	}

	r.openGitBreachBisectEnd()

	if !gitBreachHintHasKey(r.gitBreachHintEntries(), "B") {
		t.Error("\"B\" not advertised again after ending the bisect")
	}
	if gitBreachHintHasKey(r.gitBreachHintEntries(), "G") {
		t.Error("\"G\" still advertised after the bisect ended, want it gone")
	}
}

// TestOpenGitBreachBisectMarkAndEndDoNothingWithoutABisectRunning
// pins the mirror-image guard: "G"/"X"/"E" are no-ops before any
// bisect has started.
func TestOpenGitBreachBisectMarkAndEndDoNothingWithoutABisectRunning(t *testing.T) {
	r := newGitBreachBisectFixture(t)
	dir := r.gitBreach.Root

	r.openGitBreachBisectMark("good")
	r.openGitBreachBisectMark("bad")
	r.openGitBreachBisectEnd()

	if git.BisectInProgress(dir) {
		t.Error("BisectInProgress = true after no bisect was ever started, want false")
	}
}
