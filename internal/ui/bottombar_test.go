package ui

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
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
func TestMain(m *testing.M) {
	os.Setenv("HISTFILE", filepath.Join(os.TempDir(), "breakthrough-test-history-does-not-exist")) //nolint:errcheck

	loadInitialSettings = func() (config.Settings, []config.NamedTheme, []string) {
		return config.DefaultSettings(), config.LoadColorSchemes("", ""), nil
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

// TestFetchDiskUsageRealFilesystem runs the real df binary against a
// real, if throwaway, directory — sanity-checking shape (non-negative
// values, a 0-100 percent) rather than exact numbers, which depend on
// whatever this machine's own disk state happens to be.
func TestFetchDiskUsageRealFilesystem(t *testing.T) {
	requireCommand(t, "df")
	u, ok := fetchDiskUsage(t.TempDir())
	if !ok {
		t.Fatal("fetchDiskUsage should succeed against a real, existing directory")
	}
	if u.usedBytes < 0 || u.availBytes < 0 || u.usedInodes < 0 || u.availInodes < 0 {
		t.Errorf("negative usage: %+v", u)
	}
	if u.usePercent < 0 || u.usePercent > 100 || u.inodePercent < 0 || u.inodePercent > 100 {
		t.Errorf("percent out of [0,100]: %+v", u)
	}
}

// TestParseDfDataLine pins the field layout against real df output
// captured on this machine (GNU df, df -k and df -i) plus a simulated
// BSD-style wrapped line (Filesystem name on its own line, so the data
// line itself starts one field short) — parseDfDataLine indexes from
// the end specifically so that second case still works.
func TestParseDfDataLine(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantUsed    int64
		wantAvail   int64
		wantPercent int
	}{
		{
			name:        "GNU df -k",
			line:        "/dev/md0       480149504 454345216  1782272  97% /",
			wantUsed:    454345216,
			wantAvail:   1782272,
			wantPercent: 97,
		},
		{
			name:        "GNU df -i",
			line:        "/dev/md0        30515200 1316665 29198535    5% /",
			wantUsed:    1316665,
			wantAvail:   29198535,
			wantPercent: 5,
		},
		{
			name:        "wrapped Filesystem name (BSD-style, own line above)",
			line:        "        480149504 454345216  1782272  97% /",
			wantUsed:    454345216,
			wantAvail:   1782272,
			wantPercent: 97,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			used, avail, percent, ok := parseDfDataLine(tt.line)
			if !ok {
				t.Fatalf("parseDfDataLine(%q) ok = false, want true", tt.line)
			}
			if used != tt.wantUsed || avail != tt.wantAvail || percent != tt.wantPercent {
				t.Errorf("parseDfDataLine(%q) = (%d, %d, %d), want (%d, %d, %d)",
					tt.line, used, avail, percent, tt.wantUsed, tt.wantAvail, tt.wantPercent)
			}
		})
	}
}

func TestParseDfDataLineMalformed(t *testing.T) {
	for _, line := range []string{"", "too few fields", "not numbers at all here really"} {
		if _, _, _, ok := parseDfDataLine(line); ok {
			t.Errorf("parseDfDataLine(%q) ok = true, want false", line)
		}
	}
}

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
	u := diskUsage{usedBytes: 1024, availBytes: 2048, usedInodes: 10, availInodes: 20, usePercent: 50, inodePercent: 50}
	if got := diskUsageText(u); !strings.HasPrefix(got, "Disk ") || !strings.Contains(got, "used") || !strings.Contains(got, "free") {
		t.Errorf("diskUsageText(%+v) = %q, want it labeled with \"Disk\"/\"used\"/\"free\"", u, got)
	}
	if got := inodeUsageText(u); !strings.HasPrefix(got, "Inodes ") || !strings.Contains(got, "used") || !strings.Contains(got, "free") {
		t.Errorf("inodeUsageText(%+v) = %q, want it labeled with \"Inodes\"/\"used\"/\"free\"", u, got)
	}
}

// requireCommand skips t unless name is on $PATH.
func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not available in this environment: %v", name, err)
	}
}

// TestBuildStatusBarSpansLocateButtons pins that each of the four button
// spans in buildStatusBar's output actually covers that button's own
// rendered label, and nothing else — the click-routing tests below rely
// on this being right.
func TestBuildStatusBarSpansLocateButtons(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	text, spans := r.buildStatusBar()
	runes := []rune(text)

	wantActions := map[statusBarAction]string{
		statusActionEdit:         "^E Edit",
		statusActionLook:         "^L Look",
		statusActionRename:       "^R Rename",
		statusActionToggleHidden: "^G Hidden",
		statusActionSearch:       "^F Find",
		statusActionOptions:      "^O Options",
		statusActionHelp:         "F1 Help",
		statusActionTrash:        "^T Trash",
		statusActionRemove:       "^P Remove",
	}
	found := map[statusBarAction]bool{}
	for _, s := range spans {
		want, ok := wantActions[s.action]
		if !ok {
			t.Errorf("unexpected action %v in spans", s.action)
			continue
		}
		if s.endCol > len(runes) || s.startCol < 0 {
			t.Fatalf("span %v out of bounds for text %q", s, text)
		}
		if got := string(runes[s.startCol:s.endCol]); got != want {
			t.Errorf("span for action %v = %q, want %q", s.action, got, want)
		}
		found[s.action] = true
	}
	for action := range wantActions {
		if !found[action] {
			t.Errorf("no span found for action %v", action)
		}
	}

	if !strings.Contains(text, r.currentUser) {
		t.Errorf("status bar text should contain the current user %q, got:\n%s", r.currentUser, text)
	}
}

// TestBuildStatusBarSpansAccountForWideCharacters pins the fix for the
// user's own report: a current username containing double-width (e.g.
// CJK) characters must still leave every button span's start column at
// that button's real display width offset, not short by however many
// extra columns those characters occupy beyond their rune count (2
// runes, 4 terminal columns for "文档"). dfSummary shells out to the
// real, platform-specific df, so its output length isn't fixed across
// machines — the expected column is derived from the returned text
// itself via tview.TaggedStringWidth (the same measure buildStatusBar
// now uses), not a hardcoded offset.
func TestBuildStatusBarSpansAccountForWideCharacters(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.currentUser = "文档"

	text, spans := r.buildStatusBar()

	idx := strings.Index(text, "^E Edit")
	if idx < 0 {
		t.Fatalf("status bar text %q doesn't contain the Edit button label", text)
	}
	wantStart := tview.TaggedStringWidth(text[:idx])

	var editSpan statusBarSpan
	found := false
	for _, s := range spans {
		if s.action == statusActionEdit {
			editSpan, found = s, true
			break
		}
	}
	if !found {
		t.Fatal("no span for statusActionEdit")
	}
	if editSpan.startCol != wantStart {
		t.Errorf("edit span startCol = %d, want %d (real display width of %q, not its rune count)", editSpan.startCol, wantStart, text[:idx])
	}
}

// clickStatusBar simulates a real left-click on the status bar at the
// given column, the same way capturePropertiesMouse's own tests draw a
// real screen first so InRect/GetInnerRect have real layout to resolve
// coordinates against.
func clickStatusBar(t *testing.T, r *Root, col int) {
	t.Helper()

	// Sized to the text's own actual width, not a fixed guess: dfSummary
	// shells out to the real, platform-specific df, and GNU vs. BSD df
	// (let alone different filesystems/mount paths) don't produce the
	// same length line — a fixed 80-column screen was narrow enough on
	// a real macOS CI runner to push the later buttons past its edge,
	// making InRect reject clicks on them that a wider screen accepts
	// fine.
	width := tview.TaggedStringWidth(r.statusBar.GetText(true)) + 10
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(width, 24)
	r.statusBar.SetRect(0, 0, width, 1)
	r.statusBar.Draw(screen)

	rectX, _, _, _ := r.statusBar.GetInnerRect()
	r.captureStatusBarMouse(tview.MouseLeftClick, tcell.NewEventMouse(rectX+col, 0, tcell.Button1, 0))
}

func TestCaptureStatusBarMouseEditClickRunsEditAction(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.refreshStatusBar()
	r.panel.focusRow(1) // off ".." (the table's default initial selection) onto a real entry, so this exercises editCurrentEntry for real

	span, ok := statusBarSpanFor(r, statusActionEdit)
	if !ok {
		t.Fatal("no Edit span found")
	}

	// app.Suspend is a no-op here (no real screen behind r.app — see
	// runEditor's own doc comment on why this codebase can't unit-test
	// the actual editor invocation), so this only pins that the click
	// reaches editCurrentEntry/runEditor and the panel reloads cleanly
	// afterwards, not that an editor actually ran.
	clickStatusBar(t, r, span.startCol)

	if r.activePage == errorPage {
		t.Errorf("clicking Edit should not report an error here, got: %q", r.errorView.GetText(true))
	}
}

// TestCaptureStatusBarMouseTrashClickMovesFileToTrash pins the "^T Trash"
// button (see buildStatusBar/runStatusBarAction) to the same
// moveSelectionToTrash a right-click menu's "Move to Trash" and Ctrl+T/
// Entf already run — one action, three ways to reach it.
func TestCaptureStatusBarMouseTrashClickMovesFileToTrash(t *testing.T) {
	dir := fixtureDir(t)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.refreshStatusBar()
	r.panel.focusRow(1) // off ".." onto a real entry

	_, target, ok := r.panel.CurrentRowPath()
	if !ok {
		t.Fatal("no current row to trash")
	}

	span, ok := statusBarSpanFor(r, statusActionTrash)
	if !ok {
		t.Fatal("no Trash span found")
	}
	clickStatusBar(t, r, span.startCol)

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

// TestCaptureStatusBarMouseWithWideUsernameStillRoutesClicks is
// TestCaptureStatusBarMouseEditClickRunsEditAction's counterpart with a
// double-width (CJK) username ahead of the buttons on the same row —
// exercising the fix end to end (real Draw, real column math) rather
// than just the span numbers a plain unit test would check.
func TestCaptureStatusBarMouseWithWideUsernameStillRoutesClicks(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.currentUser = "文档使用者" // 5 runes, 10 terminal columns
	r.refreshStatusBar()
	r.panel.focusRow(1) // off ".." onto a real entry, so editCurrentEntry has something to act on

	span, ok := statusBarSpanFor(r, statusActionEdit)
	if !ok {
		t.Fatal("no Edit span found")
	}

	clickStatusBar(t, r, span.startCol)

	if r.activePage == errorPage {
		t.Errorf("clicking Edit should not report an error here, got: %q", r.errorView.GetText(true))
	}
}

func TestCaptureStatusBarMouseRenameClickOpensRename(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.refreshStatusBar()
	r.panel.focusRow(1) // off ".." (the table's default initial selection) onto a real entry

	row, path, ok := r.panel.CurrentRowPath()
	if !ok {
		t.Fatal("setup: no current row")
	}

	span, ok := statusBarSpanFor(r, statusActionRename)
	if !ok {
		t.Fatal("no Rename span found")
	}
	clickStatusBar(t, r, span.startCol)

	if r.activePage != renamePage {
		t.Errorf("activePage = %q, want %q", r.activePage, renamePage)
	}
	if r.target != path || r.targetRow != row {
		t.Errorf("target/targetRow = %q/%d, want %q/%d", r.target, r.targetRow, path, row)
	}
}

func TestCaptureStatusBarMouseHiddenClickTogglesShowHidden(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.refreshStatusBar()
	before := r.panel.showHidden

	span, ok := statusBarSpanFor(r, statusActionToggleHidden)
	if !ok {
		t.Fatal("no Hidden span found")
	}
	clickStatusBar(t, r, span.startCol)

	if r.panel.showHidden == before {
		t.Error("clicking Hidden should have toggled showHidden")
	}
}

// statusBarSpanFor returns the first span for action in r.statusBarSpans.
func statusBarSpanFor(r *Root, action statusBarAction) (statusBarSpan, bool) {
	for _, s := range r.statusBarSpans {
		if s.action == action {
			return s, true
		}
	}
	return statusBarSpan{}, false
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

// TestToggleHiddenShortcutRespectsGuard pins that Ctrl+G's actual action
// (Root.ToggleHiddenShortcut) is a real no-op — not just individually
// harmless — while the guard says no: showHidden must stay untouched.
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

// TestRenameShortcutTargetsCurrentRow pins Ctrl+R's actual action
// (Root.RenameShortcut): it targets whichever row the table's cursor is
// on, the same as clicking the status bar's Rename button.
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
	r.statusBarSpans = nil

	if err := r.panel.load(dir); err != nil {
		t.Fatalf("load: %v", err)
	}

	if r.statusBar.GetText(true) == "" {
		t.Error("navigating should have refreshed the status bar via Panel.onLoad")
	}
	if len(r.statusBarSpans) == 0 {
		t.Error("navigating should have rebuilt statusBarSpans via Panel.onLoad")
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
