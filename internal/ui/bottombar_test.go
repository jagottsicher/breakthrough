package ui

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// TestMain isolates every test in this package from whatever the real
// machine's own state might actually be:
//
//   - $HISTFILE/~/.bash_history: Root loads real bash history at
//     construction (see historyFilePath/loadBashHistory), and no test
//     here should depend on, or be thrown off by, this developer's or CI
//     runner's own command history. Pointed at a path that doesn't exist
//     rather than a real (even if temporary) file: loadBashHistory
//     already treats "doesn't exist" as "start empty", the same as a
//     first run would. Tests that specifically exercise
//     runShellCommand — the only thing that ever writes to this path
//     (see appendBashHistory) — additionally isolate themselves with
//     their own t.TempDir()-scoped HISTFILE (see isolateHistoryFile), so
//     they can't contaminate each other either, regardless of run order.
//   - /etc/breakthrough and ~/.config/breakthrough: Root now also loads
//     on-disk settings and color schemes at construction (see
//     loadInitialSettings in theme.go) — the same class of problem, and
//     the same fix: loadInitialSettings is a package-level var, reset
//     here to a fixed DefaultSettings()/DefaultTheme() (via
//     LoadColorSchemes("", ""), which still always includes "default" —
//     see its own doc comment) rather than anything actually read from
//     disk. Tests that exercise applyColorScheme's own persistence (the
//     one thing that writes anywhere here) isolate themselves further via
//     isolateUserConfigFile.
//   - $XDG_RUNTIME_DIR/$XDG_DATA_HOME: Root now also prunes the trash by
//     age/quota at construction (see pruneTrashAtStartup, wired in
//     NewRoot) — TrashPersistent defaults to true, so without this every
//     one of this package's hundreds of NewRoot calls, not just the
//     trash-specific ones, would create and prune
//     ~/.local/share/breakthrough/trash on whatever real machine runs
//     `go test`. Pointed at a shared (not per-test) fixed path: safe
//     because nothing here ever asserts on this directory's own content
//     — tests that actually do (see newTestRootWithFile) isolate further
//     with their own t.TempDir() for both variables, taking precedence
//     for their own duration the same way isolateHistoryFile does above.
func TestMain(m *testing.M) {
	os.Setenv("HISTFILE", filepath.Join(os.TempDir(), "breakthrough-test-history-does-not-exist")) //nolint:errcheck
	os.Setenv("XDG_RUNTIME_DIR", filepath.Join(os.TempDir(), "breakthrough-test-xdg-runtime"))     //nolint:errcheck
	os.Setenv("XDG_DATA_HOME", filepath.Join(os.TempDir(), "breakthrough-test-xdg-data"))          //nolint:errcheck

	loadInitialSettings = func() (config.Settings, map[string]config.Origin, []config.NamedTheme, []string) {
		return config.DefaultSettings(), map[string]config.Origin{}, config.LoadColorSchemes("", ""), nil
	}
	userConfigFilePath = func() string {
		return filepath.Join(os.TempDir(), "breakthrough-test-config-does-not-exist")
	}

	os.Exit(m.Run())
}

// isolateUserConfigFile points userConfigFilePath (see applyColorScheme
// in settings.go) at a fresh path within t's own TempDir for the
// duration of t — TestMain's own override already points it somewhere
// that doesn't exist, safe for tests that only read it, but a test that
// actually writes through it needs its own isolated path, the same
// reason isolateHistoryFile exists alongside TestMain's own HISTFILE
// override. Returns the path, for a test that wants to inspect the
// written file afterward.
func isolateUserConfigFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	original := userConfigFilePath
	userConfigFilePath = func() string { return path }
	t.Cleanup(func() { userConfigFilePath = original })
	return path
}

// t.Setenv (not os.Setenv/os.Unsetenv) throughout: it restores the
// original value automatically once the test ends, and "" is
// indistinguishable from unset as far as editorCommand/userShell's own
// `!= ""` checks are concerned, so it doubles as this test's way of
// clearing a variable mid-test too.
func TestEditorCommandPrecedence(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate from a real ~/.selected_editor on the machine running this test — see TestSelectedEditorParsesRealFileFormat
	t.Setenv("VISUAL", "visual-editor")
	t.Setenv("EDITOR", "editor-editor")
	if got := editorCommand(); got != "visual-editor" {
		t.Errorf("editorCommand() = %q, want VISUAL to win (%q)", got, "visual-editor")
	}

	t.Setenv("VISUAL", "")
	if got := editorCommand(); got != "editor-editor" {
		t.Errorf("editorCommand() = %q, want EDITOR as fallback (%q)", got, "editor-editor")
	}

	t.Setenv("EDITOR", "")
	if got := editorCommand(); got != "vi" {
		t.Errorf("editorCommand() = %q, want the last-resort fallback %q (no ~/.selected_editor in this isolated HOME)", got, "vi")
	}
}

// TestEditorCommandPrefersSelectedEditorOverFallback pins select-editor(1)'s
// own documented precedence (see editorCommand's own doc comment):
// SELECTED_EDITOR wins over the hardcoded "vi" fallback, but VISUAL/
// EDITOR still win over it.
func TestEditorCommandPrefersSelectedEditorOverFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	writeSelectedEditor(t, home, "/usr/bin/nano")

	if got := editorCommand(); got != "/usr/bin/nano" {
		t.Errorf("editorCommand() = %q, want the SELECTED_EDITOR value %q", got, "/usr/bin/nano")
	}

	t.Setenv("EDITOR", "editor-editor")
	if got := editorCommand(); got != "editor-editor" {
		t.Errorf("editorCommand() = %q, want EDITOR to still win over SELECTED_EDITOR (%q)", got, "editor-editor")
	}
}

// TestSelectedEditorParsesRealFileFormat pins selectedEditor's parser
// against select-editor(1)'s own real, observed output format (see its
// own doc comment) — a leading comment line, then the quoted assignment.
func TestSelectedEditorParsesRealFileFormat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSelectedEditor(t, home, "/usr/bin/vim.basic")

	if got := selectedEditor(); got != "/usr/bin/vim.basic" {
		t.Errorf("selectedEditor() = %q, want %q", got, "/usr/bin/vim.basic")
	}
}

func TestSelectedEditorMissingFileIsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if got := selectedEditor(); got != "" {
		t.Errorf("selectedEditor() = %q, want \"\" with no ~/.selected_editor present", got)
	}
}

// writeSelectedEditor writes home/.selected_editor in select-editor(1)'s
// own real format (see selectedEditor's own doc comment) — verified
// against its actual source and a live example file, not guessed.
func writeSelectedEditor(t *testing.T, home, editor string) {
	t.Helper()
	content := "# Generated by /usr/bin/select-editor\nSELECTED_EDITOR=\"" + editor + "\"\n"
	if err := os.WriteFile(filepath.Join(home, ".selected_editor"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUserShellFallback(t *testing.T) {
	t.Setenv("SHELL", "/usr/bin/fish")
	if got := userShell(); got != "/usr/bin/fish" {
		t.Errorf("userShell() = %q, want %q", got, "/usr/bin/fish")
	}

	t.Setenv("SHELL", "")
	if got := userShell(); got != "/bin/sh" {
		t.Errorf("userShell() = %q, want the fallback %q", got, "/bin/sh")
	}
}

func TestCurrentUsername(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Skipf("user.Current unavailable in this environment: %v", err)
	}
	if got := currentUsername(); got != u.Username {
		t.Errorf("currentUsername() = %q, want %q", got, u.Username)
	}
}

// TestClockTextFormat pins the rendered shape (date, time, zone
// abbreviation) — not an exact value, which would make this test flaky.
func TestClockTextFormat(t *testing.T) {
	re := regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} \S+$`)
	if got := clockText(); !re.MatchString(got) {
		t.Errorf("clockText() = %q, does not match the expected YYYY-MM-DD HH:MM:SS ZONE shape", got)
	}
}

// FetchDiskUsage/parseDfDataLine themselves now live in internal/fsops
// (see diskusage.go/diskusage_test.go there) — moved once PruneTrash
// needed the same "how big is this filesystem" logic the status bar
// already had, rather than duplicating it. What's left here is purely
// the UI-side formatting (diskUsageText/inodeUsageText/
// diskUsageWarnColor below) built on top of fsops.DiskUsage.

func TestDiskUsageWarnColor(t *testing.T) {
	tests := []struct {
		percent int
		want    tcell.Color
	}{
		{0, tcell.ColorDefault},
		{79, tcell.ColorDefault},
		{80, tcell.ColorOrange},
		{89, tcell.ColorOrange},
		{90, tcell.ColorRed},
		{100, tcell.ColorRed},
	}
	for _, tt := range tests {
		if got := diskUsageWarnColor(tt.percent); got != tt.want {
			t.Errorf("diskUsageWarnColor(%d) = %v, want %v", tt.percent, got, tt.want)
		}
	}
}

func TestFormatUsagePercentColorsAboveThresholds(t *testing.T) {
	if got := formatUsagePercent(50); got != "50%" {
		t.Errorf("formatUsagePercent(50) = %q, want plain %q (no warning)", got, "50%")
	}
	got := formatUsagePercent(95)
	if !strings.Contains(got, "95%") || !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "[-]") {
		t.Errorf("formatUsagePercent(95) = %q, want a color-tagged \"95%%\"", got)
	}
}

func TestHumanCount(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{512, "512"},
		{1024, "1.0K"},
		{1536, "1.5K"},
		{1316665, "1.3M"},
	}
	for _, tt := range tests {
		if got := humanCount(tt.n); got != tt.want {
			t.Errorf("humanCount(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestDiskUsageTextAndInodeUsageTextAreLabeled(t *testing.T) {
	u := fsops.DiskUsage{UsedBytes: 1024, AvailBytes: 2048, UsedInodes: 10, AvailInodes: 20, UsePercent: 50, InodePercent: 50}
	if got := diskUsageText(u); !strings.HasPrefix(got, "Disk ") || !strings.Contains(got, "used") || !strings.Contains(got, "free") {
		t.Errorf("diskUsageText(%+v) = %q, want it labeled with \"Disk\"/\"used\"/\"free\"", u, got)
	}
	if got := inodeUsageText(u); !strings.HasPrefix(got, "Inodes ") || !strings.Contains(got, "used") || !strings.Contains(got, "free") {
		t.Errorf("inodeUsageText(%+v) = %q, want it labeled with \"Inodes\"/\"used\"/\"free\"", u, got)
	}
}

func TestKernelVersionTextMatchesUnameR(t *testing.T) {
	requireCommand(t, "uname")
	want, err := exec.Command("uname", "-r").Output()
	if err != nil {
		t.Fatalf("uname -r: %v", err)
	}
	if got := kernelVersionText(); got != strings.TrimSpace(string(want)) {
		t.Errorf("kernelVersionText() = %q, want %q", got, strings.TrimSpace(string(want)))
	}
}

// TestUptimeAndLoadAverageTextOnLinux only runs where /proc/uptime
// actually exists (Linux) — see uptimeText/loadAverageText's own doc
// comment on why this is Linux-only rather than parsing `uptime`'s
// cross-platform-inconsistent output.
func TestUptimeAndLoadAverageTextOnLinux(t *testing.T) {
	if _, err := os.Stat("/proc/uptime"); err != nil {
		t.Skip("no /proc/uptime on this platform")
	}
	if up, ok := uptimeText(); !ok || !strings.HasPrefix(up, "up ") {
		t.Errorf("uptimeText() = %q, %v, want a \"up ...\" string, true", up, ok)
	}
	if load, ok := loadAverageText(); !ok || !strings.HasPrefix(load, "load ") {
		t.Errorf("loadAverageText() = %q, %v, want a \"load ...\" string, true", load, ok)
	}
}

func TestFormatUptime(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{90 * time.Minute, "01:30"},
		{25 * time.Hour, "1d 01:00"},
		{50*24*time.Hour + 4*time.Hour + 25*time.Minute, "50d 04:25"},
	}
	for _, c := range cases {
		if got := formatUptime(c.d); got != c.want {
			t.Errorf("formatUptime(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// requireCommand skips t unless name is on $PATH.
func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not available in this environment: %v", name, err)
	}
}

// renderedTextAt draws r's own button bar to a real (simulated) screen
// and reads back the plain runes actually shown in [startCol, endCol) —
// the technically correct way to inspect rendered content now that a
// cell's own label can carry color tags (see highlightKey in
// buildButtonBar/chordHintBar): a tag consumes rune positions in the
// raw string but zero columns on screen, so naively slicing []rune(text)
// by column drifts out of alignment the moment any earlier cell
// contains one — exactly what broke here the first time this test was
// written against tagged output.
func renderedTextAt(t *testing.T, r *Root, startCol, endCol int) string {
	t.Helper()
	width := tview.TaggedStringWidth(r.buttonBar.GetText(true)) + 10
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(width, 24)
	r.buttonBar.SetRect(0, 0, width, 1)
	r.buttonBar.Draw(screen)

	rectX, _, _, _ := r.buttonBar.GetInnerRect()
	var b strings.Builder
	for col := startCol; col < endCol; col++ {
		ch, _, _ := screen.Get(rectX+col, 0)
		b.WriteString(ch)
	}
	return b.String()
}

// TestBuildButtonBarSpansLocateButtons pins that each button span in
// buildButtonBar's output actually covers that button's own rendered
// label, and nothing else — the click-routing tests below rely on this
// being right. Only the plain-letter entries are checked here; the
// chord-family cascade cells (keyed by their own prefix letter) have
// their own separate test, TestBuildButtonBarShowsChordCascades.
func TestBuildButtonBarSpansLocateButtons(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	_, spans := r.buildButtonBar()

	wantLabels := map[rune]string{
		'?': " ? Help",
		'm': " m Menu",
		'l': " l Look",
		'i': " i Props",
		'I': " I Details",
		'c': " c Copy",
		'x': " x Cut",
		'v': " v Paste",
		'd': " d Trash",
		'.': " . Hide", // ShowHidden defaults to true — see config.DefaultSettings
		's': " s Split",
		't': " t Tabs",
	}
	found := map[rune]bool{}
	for _, s := range spans {
		if s.key == 'g' || s.key == 'p' || s.key == 'z' || s.key == 'o' {
			continue // a chord-family cascade cell — see TestBuildButtonBarShowsChordCascades
		}
		want, ok := wantLabels[s.key]
		if !ok {
			t.Errorf("unexpected key %q in spans", string(s.key))
			continue
		}
		if got := renderedTextAt(t, r, s.startCol, s.endCol); got != want {
			t.Errorf("span for key %q = %q, want %q", string(s.key), got, want)
		}
		found[s.key] = true
	}
	for key := range wantLabels {
		if !found[key] {
			t.Errorf("no span found for key %q", string(key))
		}
	}
}

// TestBuildButtonBarShowsChordCascades pins that every quick chord
// family (see chordFamily.quick) renders as "prefix… name" — the
// ellipsis is what marks it as leading to more keys rather than acting
// on its own, per the user's own explicit request that the bar show
// both single keypresses and cascades. The reserved "y" family is
// deliberately excluded (see chordFamilies' own doc comment): it has
// nothing real behind it yet, so it stays out of the one row that's
// always on screen.
func TestBuildButtonBarShowsChordCascades(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	_, spans := r.buildButtonBar()

	want := map[rune]string{'g': " g … go to", 'p': " p … perms", 'z': " z … display", 'o': " o … options"}
	for _, s := range spans {
		if label, ok := want[s.key]; ok {
			if got := renderedTextAt(t, r, s.startCol, s.endCol); got != label {
				t.Errorf("chord cascade for %q = %q, want %q", string(s.key), got, label)
			}
			delete(want, s.key)
		}
	}
	for key := range want {
		t.Errorf("no chord cascade span found for %q", string(key))
	}

	if strings.Contains(r.buttonBar.GetText(true), "y…") {
		t.Errorf("button bar should not advertise the reserved \"y\" chord")
	}
}

// buttonLabelFor returns the label currently rendered for the entry
// bound to key, read from the live r.buttonBarSpans/r.buttonBar (see
// buttonBarSpanFor/renderedTextAt) rather than calling buildButtonBar
// fresh — so this also pins that refreshButtonBar actually pushed a
// rebuilt bar into both fields after whatever state change the caller
// just made, not only that buildButtonBar would compute the right thing
// in isolation. false if key isn't showing at all right now.
func buttonLabelFor(t *testing.T, r *Root, key rune) (label string, present bool) {
	t.Helper()
	span, ok := buttonBarSpanFor(r, key)
	if !ok {
		return "", false
	}
	return renderedTextAt(t, r, span.startCol, span.endCol), true
}

// TestButtonBarHideUnhideLabelTracksShowHidden pins the one button whose
// own label is no longer fixed (see buildButtonBar's own doc comment):
// it names whichever action clicking it performs next, the same
// direction hiddenToggleLabel already uses for the context menu's own
// equivalent — "Hide" while dotfiles are shown, "Unhide" while they're
// not.
func TestButtonBarHideUnhideLabelTracksShowHidden(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	// ShowHidden defaults to true — see config.DefaultSettings.
	if got, ok := buttonLabelFor(t, r, '.'); !ok || got != " . Hide" {
		t.Errorf("label while shown = %q, present=%v, want %q", got, ok, " . Hide")
	}

	r.toggleHidden()

	if got, ok := buttonLabelFor(t, r, '.'); !ok || got != " . Unhide" {
		t.Errorf("label while hidden = %q, present=%v, want %q", got, ok, " . Unhide")
	}
}

// TestButtonBarStaysTheSameInsideTrash pins a deliberate simplification:
// unlike the button bar this replaced, "d Trash"/"D Remove" no longer
// relabel themselves while browsing the Trash — their own actions
// already branch on Root.inTrash (see moveSelectionToTrash/plainCommand
// 'D”s action in keymap.go), so the keys keep working correctly
// either way; only the permanent legend's own text stays fixed, which
// is what the help text and docs document instead of a bar that changes
// shape underfoot.
func TestButtonBarStaysTheSameInsideTrash(t *testing.T) {
	r, _, _ := newTestRootWithFile(t)

	before, ok := buttonLabelFor(t, r, 'd')
	if !ok {
		t.Fatal("setup: Trash should be showing before ever entering the trash")
	}

	r.moveSelectionToTrash()
	r.openTrash()

	after, ok := buttonLabelFor(t, r, 'd')
	if !ok || after != before {
		t.Errorf("label inside the trash = %q, present=%v, want unchanged %q", after, ok, before)
	}
}

// TestBuildStatusBarContainsUserNoButtons pins the split itself: the
// status bar shows the current user (and, best-effort, other system
// info — see buildStatusBar) but none of the button-bar labels any
// more, now that they live on their own row (see buildButtonBar). Before
// this split, a wide (e.g. CJK) username here could misalign every
// button span after it on the same line — that whole bug class is gone
// now that buttons never share a line with variable-width content at
// all (buildButtonBar's spans always start counting from column 0).
func TestBuildStatusBarContainsUserNoButtons(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.refreshStatusBar()
	text := r.statusBar.GetText(true)

	if !strings.Contains(text, r.currentUser) {
		t.Errorf("status bar text should contain the current user %q, got:\n%s", r.currentUser, text)
	}
	for _, label := range []string{"c Copy", "d Trash", "i Props", "I Details"} {
		if strings.Contains(text, label) {
			t.Errorf("status bar text should no longer contain button label %q, got:\n%s", label, text)
		}
	}
}

// TestClampFrac pins clampFrac's own [0,1] clamp, shared by every
// fraction pasteDualBar/pasteBytesColumn turn into a glyph or bar
// column.
func TestClampFrac(t *testing.T) {
	tests := []struct {
		in, want float64
	}{
		{-1, 0},
		{0, 0},
		{0.5, 0.5},
		{1, 1},
		{1.5, 1},
	}
	for _, tt := range tests {
		if got := clampFrac(tt.in); got != tt.want {
			t.Errorf("clampFrac(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// TestPasteDualBar pins pasteDualBar's own shape: width columns, each
// one a "▀" tagged with [fg:bg] — lit color when that column's own half
// has reached its fraction, dim otherwise — and a trailing reset tag so
// nothing appended after it inherits the bar's own last color.
func TestPasteDualBar(t *testing.T) {
	// topFrac 0.5 of 10 columns lights the top half of the first 5;
	// fileFrac 0.2 lights the bottom half of the first 2 of those same
	// 5 — so columns 0-1 are lit on both halves, columns 2-4 are lit on
	// top only, and columns 5-9 are dim on both.
	got := pasteDualBar(0.5, 0.2, 10)
	wantLitTopOnly := strings.Count(got, "["+pasteDualBarLit+":"+pasteDualBarDim+"]▀")
	if wantLitTopOnly != 3 {
		t.Errorf("pasteDualBar(0.5, 0.2, 10) has %d lit-top/dim-bottom columns, want 3", wantLitTopOnly)
	}
	wantLitBoth := strings.Count(got, "["+pasteDualBarLit+":"+pasteDualBarLit+"]▀")
	if wantLitBoth != 2 {
		t.Errorf("pasteDualBar(0.5, 0.2, 10) has %d lit-top/lit-bottom columns, want 2 (a fifth of 10)", wantLitBoth)
	}
	if !strings.HasSuffix(got, "[-:-]") {
		t.Errorf("pasteDualBar(0.5, 0.2, 10) = %q, want it to end with a reset tag", got)
	}

	full := pasteDualBar(1, 1, 4)
	if strings.Count(full, "["+pasteDualBarDim) != 0 {
		t.Errorf("pasteDualBar(1, 1, 4) = %q, want no dim columns at 100%%", full)
	}
	empty := pasteDualBar(0, 0, 4)
	if strings.Count(empty, "["+pasteDualBarLit) != 0 {
		t.Errorf("pasteDualBar(0, 0, 4) = %q, want no lit columns at 0%%", empty)
	}
}

// TestPasteBytesColumn pins pasteBytesColumn's own direction — 0%
// renders the thinnest of chordCountdownBlocks' own glyphs, 100% the
// full block — the opposite direction from chordIndicatorText's own
// draining countdown, since this fills up rather than runs out.
func TestPasteBytesColumn(t *testing.T) {
	if got := pasteBytesColumn(0, 100); got != "▁" {
		t.Errorf("pasteBytesColumn(0, 100) = %q, want the thinnest sliver", got)
	}
	if got := pasteBytesColumn(100, 100); got != "█" {
		t.Errorf("pasteBytesColumn(100, 100) = %q, want a full block", got)
	}
}

// TestPasteETA pins pasteETA's own "not meaningful yet" refusals (no
// time elapsed, nothing copied yet, or the total's already reached)
// alongside a real estimate from a known, steady rate.
func TestPasteETA(t *testing.T) {
	if _, ok := pasteETA(time.Now(), 0, 100); ok {
		t.Error("pasteETA with 0 bytes done should refuse an estimate, not divide by zero")
	}
	if _, ok := pasteETA(time.Time{}, 0, 100); ok {
		t.Error("pasteETA with a zero startedAt should refuse an estimate")
	}
	if _, ok := pasteETA(time.Now().Add(-time.Second), 100, 100); ok {
		t.Error("pasteETA once the total is already reached should refuse an estimate")
	}

	// 50 of 100 bytes done after 1 second of steady throughput: 50
	// bytes/sec, 50 bytes left, ~1s left.
	got, ok := pasteETA(time.Now().Add(-time.Second), 50, 100)
	if !ok {
		t.Fatal("pasteETA with real progress and elapsed time should return an estimate")
	}
	if got != "~1s left" {
		t.Errorf("pasteETA(1s ago, 50, 100) = %q, want %q", got, "~1s left")
	}
}

// TestFormatETA pins formatETA's own compact, at-most-two-unit shape.
func TestFormatETA(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "~0s left"},
		{5 * time.Second, "~5s left"},
		{90 * time.Second, "~1m 30s left"},
		{2*time.Hour + 15*time.Minute, "~2h 15m left"},
	}
	for _, tt := range tests {
		if got := formatETA(tt.d); got != tt.want {
			t.Errorf("formatETA(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

// TestPasteProgressText pins pasteProgressText's own shape: the verb
// naming Copy vs. Cut ("Copying"/"Moving"), the N/total count derived
// from total-remaining, the bar, and the current file's bare name (not
// its full path) appended when one is set — or nothing at all appended
// when it isn't (job.currentFile never stored to yet, e.g. right after
// startPaste before the first file's own onFile call has landed).
func TestPasteProgressText(t *testing.T) {
	job := &pasteJob{total: 5, remaining: 3}
	got := pasteProgressText(job)
	if !strings.Contains(got, "Copying 2/5") {
		t.Errorf("pasteProgressText = %q, want it to contain %q", got, "Copying 2/5")
	}
	if strings.Contains(got, "/") == false {
		t.Errorf("pasteProgressText = %q, want a progress bar in it", got)
	}

	job.cut = true
	if got := pasteProgressText(job); !strings.Contains(got, "Moving 2/5") {
		t.Errorf("pasteProgressText (cut) = %q, want it to contain %q", got, "Moving 2/5")
	}

	current := "/some/deep/path/apple.txt"
	job.currentFile.Store(&current)
	got = pasteProgressText(job)
	if !strings.HasSuffix(got, "apple.txt") {
		t.Errorf("pasteProgressText with a current file = %q, want it to end with the bare name %q, not the full path", got, "apple.txt")
	}
	if strings.Contains(got, "/some/deep/path") {
		t.Errorf("pasteProgressText = %q, want only the bare file name, not its full path", got)
	}
}

// TestPasteProgressTextOmitsByteBasedPartsUntilTheScanFinishes pins
// pasteJob.bytesTotal's own doc comment: a job whose byte scan hasn't
// finished yet (or found nothing to size) shows no byte-percentage
// column and no ETA at all — only once bytesTotal is actually positive
// do those segments appear (see TestPasteProgressTextShowsByteProgressOnceScanned).
func TestPasteProgressTextOmitsByteBasedPartsUntilTheScanFinishes(t *testing.T) {
	job := &pasteJob{total: 2, remaining: 1}
	got := pasteProgressText(job)
	for _, glyph := range chordCountdownBlocks {
		if strings.ContainsRune(got, glyph) {
			t.Errorf("pasteProgressText with no byte total yet = %q, should not contain a byte-percentage column glyph %q", got, string(glyph))
		}
	}
	if strings.Contains(got, "left") {
		t.Errorf("pasteProgressText with no byte total yet = %q, should not show an ETA", got)
	}
}

// TestPasteProgressTextShowsByteProgressOnceScanned pins the opposite
// case: once the background scan has stored a real bytesTotal (see
// scanPasteBytes) and at least one byte has actually copied, the
// leading byte-percentage column, the dual bar, and an ETA all appear.
func TestPasteProgressTextShowsByteProgressOnceScanned(t *testing.T) {
	job := &pasteJob{total: 2, remaining: 1, startedAt: time.Now().Add(-time.Second)}
	job.bytesTotal.Store(100)
	job.bytesBase.Store(40)
	job.currentFileSize.Store(20)
	job.currentFileBytes.Store(10) // 50 of 100 bytes done overall

	got := pasteProgressText(job)
	if !strings.ContainsRune(got, '▁') && !strings.ContainsRune(got, '▄') && !strings.ContainsRune(got, '█') {
		t.Errorf("pasteProgressText with a known byte total = %q, want a byte-percentage column glyph", got)
	}
	if !strings.Contains(got, "▀") {
		t.Errorf("pasteProgressText with a known byte total = %q, want the dual bar's own half-block glyphs", got)
	}
	if !strings.Contains(got, "left") {
		t.Errorf("pasteProgressText with real progress and elapsed time = %q, want an ETA", got)
	}
}

// TestClipboardIndicatorText pins clipboardIndicatorText's own shape:
// empty once nothing is held, "Copy"/"Cut" naming the pending
// operation rather than a progressive "Copying"/"Cutting" (nothing is
// actually in flight until Paste runs), a zero count dropped entirely
// rather than shown as "0 dirs", and singular/plural picked correctly
// either way.
func TestClipboardIndicatorText(t *testing.T) {
	tests := []struct {
		cut         bool
		dirs, files int
		want        string
	}{
		{false, 0, 0, ""},
		{false, 0, 1, "Copy: 1 file"},
		{false, 0, 3, "Copy: 3 files"},
		{false, 1, 0, "Copy: 1 dir"},
		{false, 2, 0, "Copy: 2 dirs"},
		{false, 1, 1, "Copy: 1 file, 1 dir"},
		{false, 2, 3, "Copy: 3 files, 2 dirs"},
		{true, 0, 1, "Cut: 1 file"},
		{true, 2, 3, "Cut: 3 files, 2 dirs"},
	}
	for _, tt := range tests {
		if got := clipboardIndicatorText(tt.cut, tt.dirs, tt.files); got != tt.want {
			t.Errorf("clipboardIndicatorText(cut=%v, dirs=%d, files=%d) = %q, want %q", tt.cut, tt.dirs, tt.files, got, tt.want)
		}
	}
}

// TestBuildStatusBarShowsClipboardIndicatorBetweenChordAndUser pins
// where the user's own explicit request placed this segment: between
// the chord indicator's own leading slot and the username, not
// trailing after everything else where an already-optional segment
// (disk usage, uptime, load) could end up shifting it around.
func TestBuildStatusBarShowsClipboardIndicatorBetweenChordAndUser(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	before := r.buildStatusBar()
	if strings.Contains(before, "Copy:") {
		t.Fatalf("status bar already mentions Copy before anything was copied: %q", before)
	}

	r.panel.toggleCheckbox(2) // apple.txt — see fixtureDir
	r.copyToClipboard()

	got := r.buildStatusBar()
	wantSeg := "Copy: 1 file"
	userIdx := strings.Index(got, r.currentUser)
	segIdx := strings.Index(got, wantSeg)
	if segIdx == -1 || userIdx == -1 || segIdx >= userIdx {
		t.Errorf("status bar = %q, want %q to appear before the username %q", got, wantSeg, r.currentUser)
	}
}

// TestBuildStatusBarPrefersPasteProgressOverClipboardIndicator pins
// buildStatusBar's own precedence rule: a running Paste's progress
// takes the clipboard indicator's own leading slot for as long as
// r.pasteJob is non-nil, since "what's copying right now" is more
// specific and more time-sensitive than "what's staged to paste" — even
// though both would technically apply here (a Copy's own clipboard
// stays populated through its own Paste, on purpose, in case of a
// second one elsewhere).
func TestBuildStatusBarPrefersPasteProgressOverClipboardIndicator(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.toggleCheckbox(2) // apple.txt
	r.copyToClipboard()
	r.pasteJob = &pasteJob{total: 3, remaining: 1}

	got := r.buildStatusBar()
	if strings.Contains(got, "Copy: 1 file") {
		t.Errorf("status bar = %q, should not show the clipboard indicator while a paste is running", got)
	}
	if !strings.Contains(got, "Copying 2/3") {
		t.Errorf("status bar = %q, want it to show the running paste's own progress instead", got)
	}
}

// clickButtonBar simulates a real left-click on the button bar at the
// given column, the same way capturePropertiesMouse's own tests draw a
// real screen first so InRect/GetInnerRect have real layout to resolve
// coordinates against.
func clickButtonBar(t *testing.T, r *Root, col int) {
	t.Helper()

	// Sized to the text's own actual width, not a fixed guess — the
	// button labels are fixed, but leaving headroom here costs nothing
	// and matches the same defensive sizing the old combined bar needed
	// (see git history) back when a variable-width df/username prefix
	// shared this line.
	width := tview.TaggedStringWidth(r.buttonBar.GetText(true)) + 10
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(width, 24)
	r.buttonBar.SetRect(0, 0, width, 1)
	r.buttonBar.Draw(screen)

	rectX, _, _, _ := r.buttonBar.GetInnerRect()
	r.captureButtonBarMouse(tview.MouseLeftClick, tcell.NewEventMouse(rectX+col, 0, tcell.Button1, 0))
}

// TestPlainKeyEditRunsEditAction is TestCaptureButtonBarMouseEditClickRunsEditAction's
// own successor: "e" isn't one of the button bar's own quick entries any
// more (see plainCommands' own quick field in keymap.go), so this
// exercises the same editCurrentEntry/runEditor path through the
// keyboard dispatch instead of a button click.
func TestPlainKeyEditRunsEditAction(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.focusRow(1) // off ".." (the table's default initial selection) onto a real entry, so this exercises editCurrentEntry for real
	r.app.SetFocus(r.panel.table)

	// app.Suspend is a no-op here (no real screen behind r.app — see
	// runEditor's own doc comment on why this codebase can't unit-test
	// the actual editor invocation), so this only pins that "e" reaches
	// editCurrentEntry/runEditor and the panel reloads cleanly
	// afterwards, not that an editor actually ran.
	if !r.HandlePlainKey(runeEvent('e')) {
		t.Fatal("'e' should have been consumed")
	}

	if r.activePage == errorPage {
		t.Errorf("pressing 'e' should not report an error here, got: %q", r.errorView.GetText(true))
	}
}

// TestCaptureButtonBarMouseTrashClickMovesFileToTrash pins the "d
// Trash" button (see buildButtonBar) to the same moveSelectionToTrash a
// right-click menu's "Move to Trash" and Entf already run — one action,
// three ways to reach it.
func TestCaptureButtonBarMouseTrashClickMovesFileToTrash(t *testing.T) {
	dir := fixtureDir(t)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.focusRow(1) // off ".." onto a real entry

	_, target, ok := r.panel.CurrentRowPath()
	if !ok {
		t.Fatal("no current row to trash")
	}

	span, ok := buttonBarSpanFor(r, 'd')
	if !ok {
		t.Fatal("no Trash span found")
	}
	clickButtonBar(t, r, span.startCol)

	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("%s still exists after clicking the Trash button (err=%v)", target, err)
	}
	if r.activePage == errorPage {
		t.Errorf("clicking Trash should not report an error here, got: %q", r.errorView.GetText(true))
	}
}

// TestRunEditorSkipsReloadWhileSearchResultsShowing pins the reload
// guard runEditor gained alongside the search-results-open-in-editor
// feature (see Panel.onOpenSearchResult's own doc comment):
// r.panel.path stays whatever real directory was current before the
// search that produced the results being edited ever ran, completely
// unrelated to whatever file was actually opened, so reloading it
// would both do nothing useful and — since Panel.load always exits
// search mode (see its own doc comment) — silently discard the results
// themselves the moment the editor closes. searchMode staying true
// here is exactly what proves no reload happened: app.Suspend never
// actually runs the editor in this environment (see
// TestCaptureStatusBarMouseEditClickRunsEditAction's own doc comment
// just above), so this pins runEditor's own post-Suspend guard
// specifically, not anything about the editor invocation itself.
func TestRunEditorSkipsReloadWhileSearchResultsShowing(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.showSearchResults()

	r.runEditor(filepath.Join(dir, "apple.txt"), 0)

	if !r.panel.searchMode {
		t.Error("searchMode = false after runEditor while search results were showing, want still true (no reload)")
	}
}

// Note: this file used to also have a "...WithWideUsernameStillRoutesClicks"
// test here, pinning that a double-width (CJK) username ahead of the
// buttons on the same row didn't shift every button span after it. That
// whole bug class no longer exists now that the username (buildStatusBar)
// and the buttons (buildButtonBar) live on separate rows — buttonBar's
// spans always start counting from column 0, with nothing variable-width
// ever sharing that line. See TestBuildStatusBarContainsUserNoButtons for
// the split itself.

// TestCaptureButtonBarMouseMenuClickOpensTheContextMenu pins the part
// of the "m" menu button that is easy to get wrong: several context-menu
// entries read r.target/r.targetRow rather than the panel's own cursor,
// so opening the menu by any route other than a right-click has to set
// both, or the menu would act on whatever was last right-clicked.
func TestCaptureButtonBarMouseMenuClickOpensTheContextMenu(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.focusRow(1) // off ".." (the table's default initial selection) onto a real entry

	row, path, ok := r.panel.CurrentRowPath()
	if !ok {
		t.Fatal("setup: no current row")
	}

	span, ok := buttonBarSpanFor(r, 'm')
	if !ok {
		t.Fatal("no Menu span found")
	}
	clickButtonBar(t, r, span.startCol)

	if r.activePage != contextMenuPage {
		t.Errorf("activePage = %q, want %q", r.activePage, contextMenuPage)
	}
	if r.target != path || r.targetRow != row {
		t.Errorf("target/targetRow = %q/%d, want %q/%d", r.target, r.targetRow, path, row)
	}
}

func TestCaptureButtonBarMouseHiddenClickTogglesShowHidden(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	before := r.panel.showHidden

	span, ok := buttonBarSpanFor(r, '.')
	if !ok {
		t.Fatal("no Hidden span found")
	}
	clickButtonBar(t, r, span.startCol)

	if r.panel.showHidden == before {
		t.Error("clicking Hidden should have toggled showHidden")
	}
}

// buttonBarSpanFor returns the first span bound to key in
// r.buttonBarSpans.
func buttonBarSpanFor(r *Root, key rune) (buttonBarSpan, bool) {
	for _, s := range r.buttonBarSpans {
		if s.key == key {
			return s, true
		}
	}
	return buttonBarSpan{}, false
}

// TestAcceptsGlobalShortcutGuards pins acceptsGlobalShortcut's two
// conditions: blocked while any overlay is open, and blocked while the
// bash line has keyboard focus — both real, not just "activePage is the
// zero value" bookkeeping.
func TestAcceptsGlobalShortcutGuards(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	if !r.acceptsGlobalShortcut() {
		t.Error("should accept the shortcut with nothing open and the panel focused")
	}

	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()
	if r.acceptsGlobalShortcut() {
		t.Error("should not accept the shortcut while an overlay is open")
	}
	r.hideOverlay()

	r.app.SetFocus(r.bashLine)
	if r.acceptsGlobalShortcut() {
		t.Error("should not accept the shortcut while the bash line has focus")
	}
}

// TestToggleHiddenShortcutRespectsGuard pins that ToggleHiddenShortcut's
// own guarded action is a real no-op — not just individually harmless —
// while the guard says no: showHidden must stay untouched.
func TestToggleHiddenShortcutRespectsGuard(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.app.SetFocus(r.bashLine)
	before := r.panel.showHidden
	r.ToggleHiddenShortcut()
	if r.panel.showHidden != before {
		t.Error("ToggleHiddenShortcut should no-op while the bash line has focus")
	}

	r.app.SetFocus(r.panel)
	r.ToggleHiddenShortcut()
	if r.panel.showHidden == before {
		t.Error("ToggleHiddenShortcut should toggle once the guard passes")
	}
}

// TestRenameShortcutTargetsCurrentRow pins RenameShortcut's actual
// action: it targets whichever row the table's cursor is on, the same
// as pressing "r" or clicking the status bar's Rename button.
func TestRenameShortcutTargetsCurrentRow(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.focusRow(1) // off ".." (the table's default initial selection) onto a real entry

	row, path, ok := r.panel.CurrentRowPath()
	if !ok {
		t.Fatal("setup: no current row")
	}

	r.RenameShortcut()

	if r.activePage != renamePage {
		t.Errorf("activePage = %q, want %q", r.activePage, renamePage)
	}
	if r.target != path || r.targetRow != row {
		t.Errorf("target/targetRow = %q/%d, want %q/%d", r.target, r.targetRow, path, row)
	}
}

// TestPropertiesShortcutTargetsCurrentRow pins PropertiesShortcut's own
// guarded action (see its doc comment on why it's kept despite no longer
// being wired to Ctrl+P): it targets whichever row the table's cursor is
// on, the same as pressing "i", clicking the button bar's Properties
// button, or opening Properties from the context menu after a
// right-click.
func TestPropertiesShortcutTargetsCurrentRow(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.focusRow(1) // off ".." (the table's default initial selection) onto a real entry

	row, path, ok := r.panel.CurrentRowPath()
	if !ok {
		t.Fatal("setup: no current row")
	}

	r.PropertiesShortcut()

	if r.activePage != propertiesPage {
		t.Errorf("activePage = %q, want %q", r.activePage, propertiesPage)
	}
	if r.target != path || r.targetRow != row {
		t.Errorf("target/targetRow = %q/%d, want %q/%d", r.target, r.targetRow, path, row)
	}
}

// TestPropertiesShortcutRespectsGuard pins that PropertiesShortcut's own
// guarded action stays closed — not just individually harmless — while
// the guard says no, the same as ToggleHiddenShortcut (see
// TestToggleHiddenShortcutRespectsGuard).
func TestPropertiesShortcutRespectsGuard(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.app.SetFocus(r.bashLine)
	r.PropertiesShortcut()
	if r.activePage == propertiesPage {
		t.Error("PropertiesShortcut should no-op while the bash line has focus")
	}
}

// TestPanelOnLoadRefreshesStatusBar pins the wiring itself: navigating
// the panel calls back into Root and re-renders the status bar, rather
// than it only ever reflecting whatever directory was current when
// Root was constructed.
func TestPanelOnLoadRefreshesStatusBar(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.statusBar.SetText("")

	if err := r.panel.load(dir); err != nil {
		t.Fatalf("load: %v", err)
	}

	if r.statusBar.GetText(true) == "" {
		t.Error("navigating should have refreshed the status bar via Panel.onLoad")
	}
}

// TestBashLineRunsThroughRunShellCommand pins that Enter in the bash
// line dispatches to runShellCommand (app.Suspend no-ops without a real
// screen — see TestCaptureStatusBarMouseEditClickRunsEditAction's own
// doc comment — so this only pins the wiring and the "line clears
// afterwards" behavior, not that a command actually ran).
// isolateHistoryFile points $HISTFILE at a path scoped to this test's
// own t.TempDir() — used by every test below that exercises
// runShellCommand (the only thing that writes to it — see
// appendBashHistory), so they can't contaminate each other via
// TestMain's single shared default path.
func isolateHistoryFile(t *testing.T) {
	t.Helper()
	t.Setenv("HISTFILE", filepath.Join(t.TempDir(), "history"))
}

func TestBashLineRunsThroughRunBashCommand(t *testing.T) {
	isolateHistoryFile(t)
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	// app.Suspend is a no-op here (no real screen behind r.app — see
	// TestCaptureStatusBarMouseEditClickRunsEditAction's own doc
	// comment), so this only pins that Enter reaches runBashCommand and
	// the line clears afterwards, not that a command actually ran.
	r.bashLine.SetText("echo hello", true)
	r.bashLine.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if got := r.bashLine.GetText(); got != "" {
		t.Errorf("bash line text = %q after Enter, want cleared", got)
	}
}

// TestRunBashCommandEmptyIsNoop pins that submitting a blank (or
// whitespace-only) command does nothing — no Suspend, no captured run,
// no panel reload, no error.
func TestRunBashCommandEmptyIsNoop(t *testing.T) {
	isolateHistoryFile(t)
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.runBashCommand("   ")

	if r.activePage == errorPage {
		t.Error("an empty command should not report an error")
	}
	if len(r.bashHistory) != 0 {
		t.Errorf("bashHistory = %v, want empty — a blank command should not be recorded", r.bashHistory)
	}
}

func TestParseCdCommand(t *testing.T) {
	tests := []struct {
		in         string
		wantTarget string
		wantOK     bool
	}{
		{"cd", "", true},
		{"  cd  ", "", true}, // stray whitespace around a bare cd
		{"cd /var/log", "/var/log", true},
		{"cd -", "-", true},
		{"cd ~", "~", true},
		{"cd ~/projects", "~/projects", true},
		{"ls", "", false},
		{"cdsomething", "", false},   // a different word, not "cd"
		{"cd a b", "", false},        // too many arguments to be plain cd
		{"echo cd", "", false},       // "cd" isn't the first word
		{"cd /foo && ls", "", false}, // compound command — left alone, see parseCdCommand's own doc comment
	}
	for _, tt := range tests {
		gotTarget, gotOK := parseCdCommand(tt.in)
		if gotOK != tt.wantOK || (gotOK && gotTarget != tt.wantTarget) {
			t.Errorf("parseCdCommand(%q) = (%q, %v), want (%q, %v)", tt.in, gotTarget, gotOK, tt.wantTarget, tt.wantOK)
		}
	}
}

// TestRunShellCommandCdNavigatesPanelDirectly pins the fix for the
// user's own report: "cd" in the bash line must change the panel's own
// directory, the same as Midnight Commander's command line does —
// running it as an ordinary subshell command (as everything else on
// this line does) can't do that, since a child process's own cd has no
// way to affect the parent's displayed directory once that child exits.
// app.Suspend is a no-op in this test (no real screen behind r.app — see
// TestCaptureStatusBarMouseEditClickRunsEditAction's own doc comment),
// so if this ran through the ordinary subshell path instead of being
// intercepted, the panel's directory would stay exactly where it
// started — which is exactly what this pins against.
func TestRunShellCommandCdNavigatesPanelDirectly(t *testing.T) {
	isolateHistoryFile(t)
	dir := fixtureDir(t)
	target := filepath.Join(dir, "app-data")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.runBashCommand("cd app-data")

	if r.panel.path != target {
		t.Errorf("panel.path = %q, want %q", r.panel.path, target)
	}
	if r.activePage == errorPage {
		t.Errorf("cd to a real directory should not report an error, got: %q", r.errorView.GetText(true))
	}
	if got := r.bashLine.GetText(); got != "" {
		t.Errorf("bash line text = %q after cd, want cleared", got)
	}
}

// TestChangeDirectoryBareGoesHome pins that a bare "cd" (target == "")
// goes home, the same as a real shell.
func TestChangeDirectoryBareGoesHome(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory in this environment: %v", err)
	}

	if err := r.changeDirectory(""); err != nil {
		t.Fatalf("changeDirectory(\"\"): %v", err)
	}
	if r.panel.path != home {
		t.Errorf("panel.path = %q, want home %q", r.panel.path, home)
	}
}

// TestChangeDirectoryDashGoesToPreviousPath pins "cd -" against a real
// navigation history: two hops in, "-" should land back on the first.
func TestChangeDirectoryDashGoesToPreviousPath(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	sub := filepath.Join(dir, "app-data")
	if err := r.panel.navigate(sub); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	if err := r.changeDirectory("-"); err != nil {
		t.Fatalf("changeDirectory(\"-\"): %v", err)
	}
	if r.panel.path != dir {
		t.Errorf("panel.path = %q, want the previous directory %q", r.panel.path, dir)
	}
}

// TestChangeDirectoryDashWithNoHistoryErrors pins that "cd -" reports an
// error rather than silently doing nothing when there's no previous
// directory to go back to.
func TestChangeDirectoryDashWithNoHistoryErrors(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	if err := r.changeDirectory("-"); err == nil {
		t.Error("changeDirectory(\"-\") with no previous directory should return an error")
	}
}

// TestPanelPreviousPath pins Panel.previousPath directly: false with no
// history to go back to, true (and the right path) once there is.
func TestPanelPreviousPath(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	if _, ok := r.panel.previousPath(); ok {
		t.Error("previousPath should be false right after construction")
	}

	sub := filepath.Join(dir, "app-data")
	if err := r.panel.navigate(sub); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	got, ok := r.panel.previousPath()
	if !ok || got != dir {
		t.Errorf("previousPath() = (%q, %v), want (%q, true)", got, ok, dir)
	}
}

// TestRunShellCommandRecordsHistory pins that every submitted command is
// appended, unconditionally — the same as a real shell, which remembers
// what was typed regardless of whether it succeeded.
func TestRunShellCommandRecordsHistory(t *testing.T) {
	isolateHistoryFile(t)
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.runBashCommand("echo one")
	r.runBashCommand("echo two")

	want := []string{"echo one", "echo two"}
	if len(r.bashHistory) != len(want) {
		t.Fatalf("bashHistory = %v, want %v", r.bashHistory, want)
	}
	for i, w := range want {
		if r.bashHistory[i] != w {
			t.Errorf("bashHistory[%d] = %q, want %q", i, r.bashHistory[i], w)
		}
	}
	if r.bashHistoryIdx != len(r.bashHistory) {
		t.Errorf("bashHistoryIdx = %d, want %d (not currently browsing)", r.bashHistoryIdx, len(r.bashHistory))
	}
}

// TestBashHistoryUpDownNavigation pins the full readline-style
// interaction — now Ctrl+P/Ctrl+N, not Up/Down, which TextArea's own
// default handling needs for moving the cursor between lines instead
// (see captureBashLineKey's own doc comment): Ctrl+P recalls older
// entries one at a time and stops at the oldest; Ctrl+N recalls newer
// entries and restores whatever was being typed (the draft) once it
// moves past the newest one.
func TestBashHistoryUpDownNavigation(t *testing.T) {
	isolateHistoryFile(t)
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.runBashCommand("first")
	r.runBashCommand("second")
	r.bashLine.SetText("in progress", true)

	up := tcell.NewEventKey(tcell.KeyCtrlP, 0, tcell.ModNone)
	down := tcell.NewEventKey(tcell.KeyCtrlN, 0, tcell.ModNone)

	r.captureBashLineKey(up) // -> "second" (newest), remembering "in progress" as the draft
	if got := r.bashLine.GetText(); got != "second" {
		t.Errorf("after one Ctrl+P, text = %q, want %q", got, "second")
	}

	r.captureBashLineKey(up) // -> "first" (oldest)
	if got := r.bashLine.GetText(); got != "first" {
		t.Errorf("after two Ctrl+Ps, text = %q, want %q", got, "first")
	}

	r.captureBashLineKey(up) // already at the oldest entry — stays put, does not wrap
	if got := r.bashLine.GetText(); got != "first" {
		t.Errorf("Ctrl+P past the oldest entry = %q, want it to stay at %q", got, "first")
	}

	r.captureBashLineKey(down) // -> "second"
	if got := r.bashLine.GetText(); got != "second" {
		t.Errorf("after one Ctrl+N, text = %q, want %q", got, "second")
	}

	r.captureBashLineKey(down) // -> back past the newest entry: restores the draft
	if got := r.bashLine.GetText(); got != "in progress" {
		t.Errorf("Ctrl+N past the newest entry = %q, want the draft %q restored", got, "in progress")
	}
}

// TestBashHistoryDownWithNoHistoryIsNoop pins that Ctrl+N is harmless
// when nothing has been recalled yet (no history at all, or history
// exists but Ctrl+P was never pressed).
func TestBashHistoryDownWithNoHistoryIsNoop(t *testing.T) {
	isolateHistoryFile(t)
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.bashLine.SetText("untouched", true)
	r.captureBashLineKey(tcell.NewEventKey(tcell.KeyCtrlN, 0, tcell.ModNone))

	if got := r.bashLine.GetText(); got != "untouched" {
		t.Errorf("text = %q after a stray Ctrl+N, want unchanged %q", got, "untouched")
	}
}

func TestHistoryFilePathPrefersHISTFILE(t *testing.T) {
	t.Setenv("HISTFILE", "/some/explicit/path")
	if got := historyFilePath(); got != "/some/explicit/path" {
		t.Errorf("historyFilePath() = %q, want %q", got, "/some/explicit/path")
	}
}

func TestHistoryFilePathFallsBackToBashHistory(t *testing.T) {
	t.Setenv("HISTFILE", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory in this environment: %v", err)
	}
	want := filepath.Join(home, ".bash_history")
	if got := historyFilePath(); got != want {
		t.Errorf("historyFilePath() = %q, want %q", got, want)
	}
}

// TestLoadBashHistorySkipsTimestampComments pins that bash's own
// optional "#<unix timestamp>" history-file comment lines (written when
// HISTTIMEFORMAT is set) are skipped rather than mistaken for commands,
// while an ordinary line starting with "#" some other way (a command
// that's genuinely a shell comment, or coincidentally starts with a
// word after the #) is kept.
func TestLoadBashHistorySkipsTimestampComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	content := "ls -la\n#1700000000\ncd /tmp\n#not-a-timestamp\necho hi\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got := loadBashHistory(path)
	want := []string{"ls -la", "cd /tmp", "#not-a-timestamp", "echo hi"}
	if len(got) != len(want) {
		t.Fatalf("loadBashHistory() = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("loadBashHistory()[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestLoadBashHistoryMissingFileIsEmpty(t *testing.T) {
	got := loadBashHistory(filepath.Join(t.TempDir(), "does-not-exist"))
	if len(got) != 0 {
		t.Errorf("loadBashHistory() = %v, want empty for a missing file", got)
	}
}

func TestAppendBashHistoryThenLoadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")

	if err := appendBashHistory(path, "first command"); err != nil {
		t.Fatalf("appendBashHistory: %v", err)
	}
	if err := appendBashHistory(path, "second command"); err != nil {
		t.Fatalf("appendBashHistory: %v", err)
	}

	got := loadBashHistory(path)
	want := []string{"first command", "second command"}
	if len(got) != len(want) {
		t.Fatalf("loadBashHistory() after appends = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("loadBashHistory()[%d] = %q, want %q", i, got[i], w)
		}
	}
}

// TestNewRootLoadsExistingHistory pins the end-to-end wiring: a
// pre-existing history file is what Ctrl+P recalls from the moment Root
// is constructed, before any command has been run in this session at
// all — inheriting an old session's history, not just recording a new
// one.
func TestNewRootLoadsExistingHistory(t *testing.T) {
	t.Setenv("HISTFILE", filepath.Join(t.TempDir(), "history"))
	if err := appendBashHistory(os.Getenv("HISTFILE"), "old session command"); err != nil {
		t.Fatal(err)
	}

	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.captureBashLineKey(tcell.NewEventKey(tcell.KeyCtrlP, 0, tcell.ModNone))
	if got := r.bashLine.GetText(); got != "old session command" {
		t.Errorf("Ctrl+P right after startup = %q, want the pre-existing history entry %q", got, "old session command")
	}
}
