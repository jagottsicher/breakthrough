package ui

import (
	"math"
	"os"
	"strings"
	"testing"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
)

func TestPercentOf(t *testing.T) {
	tests := []struct {
		part, total int64
		want        int
	}{
		{0, 100, 0},
		{50, 100, 50},
		{80, 100, 80},
		{5, 0, 0},  // degenerate zero total: 0, not a divide-by-zero panic
		{5, -1, 0}, // a negative total is just as degenerate
		{100, 100, 100},
	}
	for _, tt := range tests {
		if got := percentOf(tt.part, tt.total); got != tt.want {
			t.Errorf("percentOf(%d, %d) = %d, want %d", tt.part, tt.total, got, tt.want)
		}
	}
}

func TestColoredStatLineWrapsTheWholeLineInOneColor(t *testing.T) {
	got := coloredStatLine(systemInfoMemoryColor, "Memory", "used 1G/2G (50%)")
	want := "[" + colorTag(systemInfoMemoryColor) + "]Memory:      used 1G/2G (50%)[-]"
	if got != want {
		t.Errorf("coloredStatLine = %q, want %q", got, want)
	}
}

func TestUsagePhraseColorsThePercentThenReturnsToBase(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	got := usagePhrase("used", 50, 100, theme, statusDiskColor)
	want := "used " + humanSize(50) + "/" + humanSize(100) + " (" + coloredPercentIn(50, theme.EntryExecutable, statusDiskColor) + ")"
	if got != want {
		t.Errorf("usagePhrase = %q, want %q", got, want)
	}
}

func TestSwapLineShowsNoneConfiguredWithoutAPercent(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	got := swapLine(theme, 0, 0)
	if !strings.Contains(got, "none configured") {
		t.Errorf("swapLine(0, 0) = %q, want it to say \"none configured\"", got)
	}
	if strings.Contains(got, "%") {
		t.Errorf("swapLine(0, 0) = %q, should show no percentage for a nonexistent swap", got)
	}

	got = swapLine(theme, 1000, 500)
	if !strings.Contains(got, "used") || !strings.Contains(got, "50%") {
		t.Errorf("swapLine(1000, 500) = %q, want a normal \"used .../... (50%%)\" phrase", got)
	}
}

// TestOpenFilesLineGuardsAgainstAnAbsurdCeiling pins the real,
// observed case (a Kali VM reporting fs.file-max as math.MaxInt64
// itself, the kernel's own way of saying "no limit is enforced") —
// see openFileHandlesSaneMax's own doc comment.
func TestOpenFilesLineGuardsAgainstAnAbsurdCeiling(t *testing.T) {
	theme := config.DefaultTheme().Resolve()

	absurd := openFilesLine(theme, 16137, math.MaxInt64)
	if strings.Contains(absurd, "%") {
		t.Errorf("openFilesLine with an absurd ceiling = %q, should show no percentage at all", absurd)
	}
	if !strings.Contains(absurd, "no practical limit") {
		t.Errorf("openFilesLine with an absurd ceiling = %q, want it to say so", absurd)
	}

	normal := openFilesLine(theme, 100, 1000)
	if !strings.Contains(normal, "used") || !strings.Contains(normal, "10%") {
		t.Errorf("openFilesLine(100, 1000) = %q, want a normal \"used 100/1,000 (10%%)\" phrase", normal)
	}
}

// TestShowingSystemInfoTriggersOnlyOnTheRootRowItself pins the user's
// own explicit correction: System Info must not win for every entry
// while merely browsing "/" (that made every real top-level directory
// permanently unreachable from Details) — only selecting the
// synthesized "/" row itself (see Panel.load's own doc comment on it,
// replacing the usual ".." there) should.
func TestShowingSystemInfoTriggersOnlyOnTheRootRowItself(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), "/")
	if err != nil {
		t.Fatalf("NewRoot(\"/\"): %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.showDetailsSidebar() // default cursor: row 0, the "/" row itself

	if r.detailsTarget != "/" {
		t.Fatalf("setup: detailsTarget = %q, want \"/\" (row 0 should be the root row)", r.detailsTarget)
	}
	if !r.showingSystemInfo() {
		t.Error("showingSystemInfo() = false with the \"/\" row selected, want true")
	}

	r.panel.focusRow(1) // a real top-level entry (e.g. "bin" or the first real one)
	if r.detailsTarget == "/" {
		t.Fatal("setup: row 1 should be a real entry, not \"/\" again")
	}
	if r.showingSystemInfo() {
		t.Errorf("showingSystemInfo() = true with %q selected, want false — real entries under \"/\" must still get ordinary Details", r.detailsTarget)
	}

	dir := t.TempDir()
	r2, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot(%q): %v", dir, err)
	}
	if r2.showingSystemInfo() {
		t.Errorf("showingSystemInfo() = true while browsing %q, want false", dir)
	}
}

// TestRootDirectoryListingShowsASelectableSelfRowInsteadOfDotDot pins
// the other half of the same fix: filepath.Dir("/") is "/" itself, so
// the ordinary ".." row (see Panel.load) was never added there at all
// before this — silently leaving no way to select "/" as such.
func TestRootDirectoryListingShowsASelectableSelfRowInsteadOfDotDot(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), "/")
	if err != nil {
		t.Fatalf("NewRoot(\"/\"): %v", err)
	}
	_, path, ok := r.panel.CurrentRowPath()
	if !ok || path != "/" {
		t.Fatalf("CurrentRowPath() at row 0 = (%q, %v), want (\"/\", true)", path, ok)
	}

	ref, ok := r.panel.rowRef(0)
	if !ok || ref.name != "/" || ref.checkable {
		t.Errorf("row 0 = %+v, ok=%v; want name \"/\", not checkable", ref, ok)
	}
}

// TestDetailsAtRootShowsSystemInfoAndTitleSwitchesOnNavigation is the
// full, real integration: opening Details while at "/" shows System
// Info under that exact title, and navigating away (then back) flips
// both the content and the title bar — a real, observed bug during
// development was the title bar staying stale (it's normally only
// re-rendered on a terminal resize — see repositionDetailsSidebar's
// own doc comment) after loadDetailsTarget alone changed what should
// be shown.
func TestDetailsAtRootShowsSystemInfoAndTitleSwitchesOnNavigation(t *testing.T) {
	if _, err := os.Stat("/proc/uptime"); err != nil {
		t.Skip("no /proc on this platform — System Info would be near-empty")
	}
	r, err := NewRoot(tview.NewApplication(), "/")
	if err != nil {
		t.Fatalf("NewRoot(\"/\"): %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.showDetailsSidebar()

	text := r.detailsSidebar.GetText(true)
	for _, want := range []string{"Host:", "Kernel:", "Uptime:", "Load:"} {
		if !strings.Contains(text, want) {
			t.Errorf("System Info text missing %q, got:\n%s", want, text)
		}
	}
	if got := r.detailsTitleBar.GetText(true); !strings.Contains(got, "System Info") {
		t.Errorf("title bar = %q, want it to say \"System Info\" while at \"/\"", got)
	}

	// Navigate into a real subdirectory that definitely exists — /etc
	// is universal on every Linux system this could plausibly run on.
	if err := r.panel.navigate("/etc"); err != nil {
		t.Fatalf("navigate(/etc): %v", err)
	}
	if r.showingSystemInfo() {
		t.Fatal("showingSystemInfo() should be false once no longer browsing \"/\"")
	}
	if got := r.detailsTitleBar.GetText(true); strings.Contains(got, "System Info") {
		t.Errorf("title bar = %q, should have reverted to \"Details\" after leaving \"/\"", got)
	}

	if err := r.panel.navigate("/"); err != nil {
		t.Fatalf("navigate(/): %v", err)
	}
	if got := r.detailsTitleBar.GetText(true); !strings.Contains(got, "System Info") {
		t.Errorf("title bar = %q, want it to switch back to \"System Info\" on returning to \"/\"", got)
	}
}

// TestComputeDetailsHashesNoOpsWhileShowingSystemInfo pins the guard
// added to computeDetailsHashes: without it, isDirish(fsops.Info{})'s
// own zero-value false lets a stray "h" try to hash whatever real
// directory the cursor happens to sit on at "/", despite System Info
// offering no hash section at all to trigger it from.
func TestComputeDetailsHashesNoOpsWhileShowingSystemInfo(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), "/")
	if err != nil {
		t.Fatalf("NewRoot(\"/\"): %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.showDetailsSidebar()

	r.computeDetailsHashes()

	if r.detailsHashInProgress {
		t.Error("computeDetailsHashes should not start anything while showing System Info")
	}
	if r.detailsHashes != nil {
		t.Error("computeDetailsHashes should not have produced a hash result while showing System Info")
	}
}
