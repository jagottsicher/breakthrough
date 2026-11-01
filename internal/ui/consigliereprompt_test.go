package ui

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/compare"
	"github.com/jagottsicher/breakthrough/internal/firewall"
	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/git"
	"github.com/jagottsicher/breakthrough/internal/logview"
	"github.com/jagottsicher/breakthrough/internal/mounts"
	"github.com/jagottsicher/breakthrough/internal/multiplex"
	"github.com/jagottsicher/breakthrough/internal/operations"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
	"github.com/jagottsicher/breakthrough/internal/sshkeys"
)

// TestConsigliereEffectiveContextTierDefaultsToMax pins "" (no tier
// ever chosen for the current provider) resolving to "max" — the
// previous unconditional 50/300 behavior, so an existing config
// doesn't change until a smaller tier is actually chosen.
func TestConsigliereEffectiveContextTierDefaultsToMax(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.settings.ConsigliereProvider = "anthropic"
	root.settings.ConsigliereAnthropicContextTier = ""

	if got := root.consigliereEffectiveContextTier(); got != "max" {
		t.Errorf("effective tier = %q, want \"max\"", got)
	}
	if got := root.consigliereContextLineCap(); got != 50 {
		t.Errorf("line cap = %d, want 50", got)
	}
	if got := root.consigliereContextDiffLineCap(); got != 250 {
		t.Errorf("diff line cap = %d, want 250", got)
	}
}

// TestConsigliereEffectiveContextTierFallsBackToMaxForAnUnknownValue
// covers a hand-edited config file with a typo'd tier name — falls
// back to "max" rather than resolving to a zero cap that would
// silently forward nothing at all.
func TestConsigliereEffectiveContextTierFallsBackToMaxForAnUnknownValue(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.settings.ConsigliereProvider = "anthropic"
	root.settings.ConsigliereAnthropicContextTier = "huge"

	if got := root.consigliereEffectiveContextTier(); got != "max" {
		t.Errorf("effective tier = %q, want the \"max\" fallback", got)
	}
}

// TestConsigliereContextTierCapsEveryNamedTier pins every tier's own
// documented line/entry and diff caps exactly.
func TestConsigliereContextTierCapsEveryNamedTier(t *testing.T) {
	cases := []struct {
		tier        string
		lineCap     int
		diffLineCap int
	}{
		{"tiny", 3, 3},
		{"small", 10, 20},
		{"medium", 25, 75},
		{"large", 40, 160},
		{"max", 50, 250},
	}
	for _, c := range cases {
		root := newPlainKeyRoot(t)
		root.settings.ConsigliereProvider = "anthropic"
		root.settings.ConsigliereAnthropicContextTier = c.tier
		if got := root.consigliereContextLineCap(); got != c.lineCap {
			t.Errorf("tier %q: line cap = %d, want %d", c.tier, got, c.lineCap)
		}
		if got := root.consigliereContextDiffLineCap(); got != c.diffLineCap {
			t.Errorf("tier %q: diff line cap = %d, want %d", c.tier, got, c.diffLineCap)
		}
	}
}

// TestConsigliereContextTierIsStoredPerProviderNotShared pins the same
// per-provider isolation Model/Effort/APIKeyFile already have:
// choosing a tier for one provider must never apply it to another —
// switching Provider back and forth must never leak one provider's own
// choice into another's.
func TestConsigliereContextTierIsStoredPerProviderNotShared(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.settings.ConsigliereAnthropicContextTier = "tiny"
	root.settings.ConsigliereOllamaContextTier = ""

	root.settings.ConsigliereProvider = "anthropic"
	if got := root.consigliereEffectiveContextTier(); got != "tiny" {
		t.Errorf("anthropic tier = %q, want \"tiny\"", got)
	}

	root.settings.ConsigliereProvider = "ollama"
	if got := root.consigliereEffectiveContextTier(); got != "max" {
		t.Errorf("ollama tier = %q, want \"max\" (unaffected by anthropic's own tiny tier)", got)
	}
}

// TestConsigliereMachiavelliLogLinesRespectsALoweredTier proves the
// resolver is actually wired into the real forwarding path, not just
// correct in isolation.
func TestConsigliereMachiavelliLogLinesRespectsALoweredTier(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.settings.ConsigliereProvider = "anthropic"
	root.settings.ConsigliereAnthropicContextTier = "tiny"
	root.activePage = logAuditViewerPage
	var entries []logview.Entry
	for i := 0; i < 10; i++ {
		entries = append(entries, logview.Entry{Raw: fmt.Sprintf("distinct-log-line-%d", i)})
	}
	root.logAuditAllEntries = entries

	got := root.consigliereMachiavelliLogLines()

	if n := strings.Count(got, "distinct-log-line-"); n != 3 {
		t.Errorf("snapshot mentions %d lines, want exactly the tiny tier's cap of 3", n)
	}
	if !strings.Contains(got, "7 further matching lines, newer than these, not shown") {
		t.Errorf("snapshot = %q, want it to mention the 7 truncated, newer lines", got)
	}
}

// TestConsigliereMachiavelliLogLinesMatchesTheTableSOrderWhenReversed
// pins a real, reported gap: logAuditVisibleEntries always returns
// oldest-first, but flipping the Time column (logAuditNewestFirst)
// reverses what the table itself shows — the forwarded block used to
// still take the oldest entries regardless, so a user looking at this
// month's events at the top of their screen got lines from the start
// of a months-old log file instead, with no way to tell from the
// reply that the mismatch had even happened.
func TestConsigliereMachiavelliLogLinesMatchesTheTableSOrderWhenReversed(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.settings.ConsigliereProvider = "anthropic"
	root.settings.ConsigliereAnthropicContextTier = "tiny" // caps at 3, so truncation actually happens
	root.activePage = logAuditViewerPage
	// In file order (chronological, oldest first) — entry-0 is oldest,
	// entry-4 is newest.
	var entries []logview.Entry
	for i := 0; i < 5; i++ {
		entries = append(entries, logview.Entry{Raw: fmt.Sprintf("entry-%d", i)})
	}
	root.logAuditAllEntries = entries

	// Default order (oldest-first, logAuditNewestFirst's own false
	// zero value): the table's own top 3 rows are entry-0..entry-2 —
	// the forwarded block must be exactly those, not some other slice.
	got := root.consigliereMachiavelliLogLines()
	for _, want := range []string{"entry-0", "entry-1", "entry-2"} {
		if !strings.Contains(got, want) {
			t.Errorf("oldest-first snapshot = %q, want it to contain %q (the table's own top rows)", got, want)
		}
	}
	if strings.Contains(got, "entry-3") || strings.Contains(got, "entry-4") {
		t.Errorf("oldest-first snapshot = %q, must not include entries scrolled further down the table", got)
	}
	if !strings.Contains(got, "oldest first") {
		t.Errorf("oldest-first snapshot = %q, want it to state its own order explicitly", got)
	}

	// Reversed (newest-first, the Time column's own "↓" arrow): the
	// table's own top 3 rows are now entry-4..entry-2 — the forwarded
	// block must follow, not keep reading from the front of the
	// unreversed list.
	root.logAuditNewestFirst = true
	gotReversed := root.consigliereMachiavelliLogLines()
	for _, want := range []string{"entry-4", "entry-3", "entry-2"} {
		if !strings.Contains(gotReversed, want) {
			t.Errorf("newest-first snapshot = %q, want it to contain %q (the table's own top rows once reversed)", gotReversed, want)
		}
	}
	if strings.Contains(gotReversed, "entry-0") || strings.Contains(gotReversed, "entry-1") {
		t.Errorf("newest-first snapshot = %q, must not include entries scrolled off the bottom once reversed", gotReversed)
	}
	if !strings.Contains(gotReversed, "newest first") {
		t.Errorf("newest-first snapshot = %q, want it to state its own order explicitly", gotReversed)
	}
}

func TestPanelContextSnapshotIncludesDirectoryAndMarkedFiles(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.panel.selected = map[string]bool{
		root.panel.path + "/apple.txt":  true,
		root.panel.path + "/banana.txt": true,
	}

	got := root.panelContextSnapshot()

	if !strings.Contains(got, root.panel.path) {
		t.Errorf("snapshot = %q, want it to mention the current directory", got)
	}
	if !strings.Contains(got, "apple.txt") || !strings.Contains(got, "banana.txt") {
		t.Errorf("snapshot = %q, want it to mention both marked files", got)
	}
}

func TestPanelContextSnapshotCapsMarkedFiles(t *testing.T) {
	root := newPlainKeyRoot(t)
	const total = consigliereMaxMarkedPaths + 10
	root.panel.selected = map[string]bool{}
	for i := 0; i < total; i++ {
		root.panel.selected[fmt.Sprintf("%s/file%d.txt", root.panel.path, i)] = true
	}

	got := root.panelContextSnapshot()

	if !strings.Contains(got, fmt.Sprintf("%d total", total)) {
		t.Errorf("snapshot = %q, want it to state the real total count even though the list itself is capped", got)
	}
}

// TestPanelContextSnapshotMentionsARemoteConnection pins a real,
// reported gap: a question about "this directory" while browsing a
// remote listing over SFTP got answered as if it were a path on the
// local machine — nothing ever said it was actually remote, on a
// completely different host, which changes what advice (permissions,
// available tools, what "df"/"du" would even mean here) applies.
func TestPanelContextSnapshotMentionsARemoteConnection(t *testing.T) {
	root := newPlainKeyRoot(t)
	client := &fakeRemoteClient{root: "/remote", entries: map[string][]fsops.Entry{"/remote": nil}}
	if err := root.panel.connectRemote(client, remotefs.Connection{Host: "example.com", User: "tester"}); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}

	got := root.panelContextSnapshot()

	for _, want := range []string{"remote", "SFTP", "tester@example.com"} {
		if !strings.Contains(got, want) {
			t.Errorf("snapshot = %q, want it to mention %q", got, want)
		}
	}
}

// TestConsigliereContextSnapshotDescribesAnOpenTerminal pins the
// other half of the same real, reported gap: viewingShells is a
// separate flag from activePage entirely (root.go's own doc comment),
// so a question asked while looking at a Terminal used to be answered
// as if the file panel underneath were still on screen.
func TestConsigliereContextSnapshotDescribesAnOpenTerminal(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })
	r.openNewShellEmbedded(testShellConn("example.com"))
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()

	got := r.consigliereContextSnapshot()

	for _, want := range []string{"Current view: Terminal.", "example.com"} {
		if !strings.Contains(got, want) {
			t.Errorf("snapshot = %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "Current view: file panel") {
		t.Errorf("snapshot = %q, must not describe the file panel underneath while a Terminal is on screen", got)
	}
}

// waitForTerminalScreen polls (Session's own parseLoop runs on a
// separate goroutine — see termtab's own waitForCellRow for the
// identical reasoning) until tab's own current screen contains want,
// or fails the test after timeout.
func waitForTerminalScreen(t *testing.T, tab *shellTab, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var got string
	for time.Now().Before(deadline) {
		got = tab.session.Screen()
		if strings.Contains(got, want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("terminal screen = %q after %s, want it to contain %q", got, timeout, want)
}

// TestTerminalContextSnapshotNeverMentionsInputOrOutputByDefault pins
// the default boundary: nothing from a termtab.Session's own live
// screen reaches this snapshot unless Machiavelli Mode is on (see
// TestTerminalContextSnapshotIncludesScreenUnderMachiavelliMode for
// the opt-in exception) — a shell's own prompt can carry anything,
// including a password someone just typed.
func TestTerminalContextSnapshotNeverMentionsInputOrOutputByDefault(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "echo super-secret-password; sleep 5") })
	r.openNewShellEmbedded(testShellConn("example.com"))
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()

	got := r.terminalContextSnapshot()

	if strings.Contains(got, "secret") {
		t.Errorf("snapshot = %q, must never include anything from the shell's own live screen", got)
	}
	if !strings.Contains(got, "not visible") {
		t.Errorf("snapshot = %q, want it to explicitly say input/output aren't visible here", got)
	}
}

// TestTerminalContextSnapshotIncludesScreenUnderMachiavelliMode pins
// the one deliberate, opt-in exception: once the user has turned the
// mode on, the Terminal's own current screen is forwarded verbatim —
// Session.Screen's own doc comment has the full reasoning.
func TestTerminalContextSnapshotIncludesScreenUnderMachiavelliMode(t *testing.T) {
	r := newTestRootForShells(t)
	r.settings.ConsigliereHardcoreMode = true
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "echo super-secret-password; sleep 5") })
	r.openNewShellEmbedded(testShellConn("example.com"))
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()
	waitForTerminalScreen(t, r.shells[r.activeShell], "super-secret-password", 2*time.Second)

	got := r.terminalContextSnapshot()

	if !strings.Contains(got, "super-secret-password") {
		t.Errorf("snapshot = %q, want the terminal's own current screen forwarded under Machiavelli Mode", got)
	}
}

// TestTerminalContextSnapshotExplainsVisibilityWhenScreenIsBlankUnderMachiavelliMode
// pins a real, reported gap: a blank screen made
// consigliereMachiavelliTerminalScreen return "" and left the
// snapshot saying nothing at all about visibility — turning
// Machiavelli Mode ON produced a LESS explanatory answer than leaving
// it off. The command here (sleep, no echo) never writes anything to
// its own stdout, so the screen stays deterministically blank for the
// whole test, not just a transient race at startup.
func TestTerminalContextSnapshotExplainsVisibilityWhenScreenIsBlankUnderMachiavelliMode(t *testing.T) {
	r := newTestRootForShells(t)
	r.settings.ConsigliereHardcoreMode = true
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })
	r.openNewShellEmbedded(testShellConn("example.com"))
	defer func() {
		for _, tab := range r.shells {
			_ = tab.session.Close()
		}
	}()

	got := r.terminalContextSnapshot()

	if !strings.Contains(got, "blank") {
		t.Errorf("snapshot = %q, want it to explain the screen is currently blank rather than saying nothing at all", got)
	}
}

// TestConsigliereContextSnapshotUsesLiveActivePageNotExitPage pins the
// advisor-flagged regression risk: dispatch must key off the *current*
// r.activePage, not consigliereExitPage (the page captured when the
// sidebar was first opened) — otherwise navigating from Log Audit's
// own overview into a specific log file (pushOverlay, which the
// sidebar is explicitly meant to survive) would keep describing the
// overview forever.
func TestConsigliereContextSnapshotUsesLiveActivePageNotExitPage(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.consigliereExitPage = logAuditSelectionPage // stale: wherever it was opened from
	root.activePage = logAuditViewerPage             // live: wherever it actually is now
	root.logAuditDir = "/var/log/myapp"

	got := root.consigliereContextSnapshot()

	if !strings.Contains(got, "Log Holepunch") {
		t.Errorf("snapshot = %q, want a Log Holepunch snapshot even though consigliereExitPage still names the selection screen", got)
	}
}

// TestConsigliereContextSnapshotDispatchesToFirewall pins the
// dispatch switch's own Firewall case — easy to leave out by mistake
// (it would then silently fall back to the generic bare-panel
// snapshot) since nothing else here would catch that.
func TestConsigliereContextSnapshotDispatchesToFirewall(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.activePage = firewallPage
	root.firewallSnapshot = firewall.Snapshot{Backend: firewall.BackendUFW}

	got := root.consigliereContextSnapshot()

	if !strings.Contains(got, "Current view: Firewall.") {
		t.Errorf("snapshot = %q, want the Firewall snapshot, not the generic panel one", got)
	}
}

func TestLogAuditContextSnapshotIncludesDirectoryAndFilters(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.logAuditDir = "/var/log/myapp"
	root.logAuditKeywordField.SetText("timeout")
	root.logAuditLevelField.SetText("error")
	root.logAuditTimelineStartLabel = "2026-10-01"
	root.logAuditTimelineEndLabel = "2026-10-28"

	got := root.logAuditContextSnapshot()

	for _, want := range []string{"Log Holepunch", "/var/log/myapp", "timeout", "error", "2026-10-01", "2026-10-28"} {
		if !strings.Contains(got, want) {
			t.Errorf("snapshot = %q, want it to contain %q", got, want)
		}
	}
}

// TestLogAuditContextSnapshotAlwaysIncludesAvailableActions pins the
// user's own explicit request: this view's own fields/keys must be
// present even when nothing has been typed into any filter yet —
// unlike the live filter values above, which are only reported once
// set, the screen's own available actions are a fixed fact about
// this screen, not something that depends on current state.
func TestLogAuditContextSnapshotAlwaysIncludesAvailableActions(t *testing.T) {
	root := newPlainKeyRoot(t) // no filters set at all

	got := root.logAuditContextSnapshot()

	for _, want := range []string{"Time field", "2 hours ago", "Level field", "Tab / Shift+Tab", "Escape"} {
		if !strings.Contains(got, want) {
			t.Errorf("snapshot = %q, want it to always contain %q", got, want)
		}
	}
}

// TestLogAuditContextSnapshotOmitsLogLinesByDefault pins the ordinary,
// privacy-preserving case: even with real entries loaded and the
// viewer open, no actual log line text is forwarded unless
// Machiavelli Mode has been explicitly turned on.
func TestLogAuditContextSnapshotOmitsLogLinesByDefault(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.activePage = logAuditViewerPage
	root.logAuditAllEntries = []logview.Entry{{Raw: "2026-10-28 secret=topsecret123 user logged in"}}

	got := root.logAuditContextSnapshot()

	if strings.Contains(got, "topsecret123") {
		t.Errorf("snapshot = %q, must not contain actual log line text without Machiavelli Mode", got)
	}
}

// TestLogAuditContextSnapshotIncludesMatchedLinesUnderMachiavelliMode
// pins the explicit, opted-in exception: once the user has turned the
// mode on, the viewer's own currently-matching lines are forwarded
// verbatim.
func TestLogAuditContextSnapshotIncludesMatchedLinesUnderMachiavelliMode(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.activePage = logAuditViewerPage
	root.settings.ConsigliereHardcoreMode = true
	root.logAuditAllEntries = []logview.Entry{{Raw: "2026-10-28 secret=topsecret123 user logged in"}}

	got := root.logAuditContextSnapshot()

	if !strings.Contains(got, "topsecret123") {
		t.Errorf("snapshot = %q, want the matched log line forwarded under Machiavelli Mode", got)
	}
}

// TestLogAuditContextSnapshotIncludesDetailEntryUnderMachiavelliMode
// pins the Detail page's own exception: looking at one full entry is
// exactly the case Machiavelli Mode's own forwarding exists for, and
// the entry is resolved the real way (the Viewer table's own current
// selection), not a separately-tracked field.
func TestLogAuditContextSnapshotIncludesDetailEntryUnderMachiavelliMode(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.logAuditTable.Select(1, 0) // app.log group, sorted before syslog
	r.openLogAuditViewer()
	r.logAuditViewerTable.Select(1, 0)
	r.openLogAuditDetail(1)
	r.settings.ConsigliereHardcoreMode = true

	got := r.logAuditContextSnapshot()

	if !strings.Contains(got, "connection established") {
		t.Errorf("snapshot = %q, want the detail entry's own Raw text forwarded under Machiavelli Mode", got)
	}
}

// TestLogAuditContextSnapshotOmitsDetailEntryWithoutMachiavelliMode
// pins the default-off case for the same page: the entry's own text
// must not leak just because the Detail page happens to be open.
func TestLogAuditContextSnapshotOmitsDetailEntryWithoutMachiavelliMode(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.logAuditTable.Select(1, 0)
	r.openLogAuditViewer()
	r.logAuditViewerTable.Select(1, 0)
	r.openLogAuditDetail(1)

	got := r.logAuditContextSnapshot()

	if strings.Contains(got, "connection established") {
		t.Errorf("snapshot = %q, must not include the detail entry's own text without Machiavelli Mode", got)
	}
}

// TestLogAuditContextSnapshotIncludesStatisticsRegardlessOfMachiavelliMode
// pins that the Statistics page's own aggregate (counts by level/
// source, never a line's own content) is always included — the same
// risk tier Firewall's rule count or Action Log's category breakdown
// already get without needing Machiavelli Mode at all.
func TestLogAuditContextSnapshotIncludesStatisticsRegardlessOfMachiavelliMode(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.logAuditTable.Select(1, 0)
	r.openLogAuditViewer()
	r.openLogAuditStats()

	got := r.logAuditContextSnapshot()

	if !strings.Contains(got, "By level:") || !strings.Contains(got, "By source") {
		t.Errorf("snapshot = %q, want the Statistics aggregate included even with Machiavelli Mode off", got)
	}

	r.settings.ConsigliereHardcoreMode = true
	gotHardcore := r.logAuditContextSnapshot()
	if !strings.Contains(gotHardcore, "By level:") {
		t.Errorf("snapshot = %q, want the Statistics aggregate still included with Machiavelli Mode on", gotHardcore)
	}
}

// TestLogAuditContextSnapshotMachiavelliModeOnlyAppliesToTheViewerPage
// pins the scope: the selection screen has no matched entries of its
// own to leak, so the mode must not do anything there even if it's on.
func TestLogAuditContextSnapshotMachiavelliModeOnlyAppliesToTheViewerPage(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.activePage = logAuditSelectionPage
	root.settings.ConsigliereHardcoreMode = true
	root.logAuditAllEntries = []logview.Entry{{Raw: "2026-10-28 secret=topsecret123 user logged in"}}

	got := root.logAuditContextSnapshot()

	if strings.Contains(got, "topsecret123") {
		t.Errorf("snapshot = %q, must not include log lines outside the viewer page", got)
	}
}

func TestGitBreachContextSnapshotIncludesStatusAndCommits(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.gitBreachRoot = "/home/jens/project"
	root.gitBreachStatus = git.Status{
		Branch:    "feature/x",
		Ahead:     2,
		Staged:    []git.FileChange{{Path: "a.go"}},
		Untracked: []string{"b.go", "c.go"},
	}
	root.gitBreachCommits = []git.LogEntry{
		{Subject: "fix the thing"},
		{Subject: "add the feature"},
	}

	got := root.gitBreachContextSnapshot()

	for _, want := range []string{"Git Breach", "/home/jens/project", "feature/x", "ahead 2", "Staged: 1", "Untracked: 2", "fix the thing", "add the feature"} {
		if !strings.Contains(got, want) {
			t.Errorf("snapshot = %q, want it to contain %q", got, want)
		}
	}
}

// TestGitBreachContextSnapshotOmitsDiffContentByDefault pins the
// ordinary, privacy-preserving case: even with a real diff currently
// shown in Main, no actual diff content is forwarded unless
// Machiavelli Mode has been explicitly turned on.
func TestGitBreachContextSnapshotOmitsDiffContentByDefault(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.gitBreachStatus = git.Status{Branch: "main"}
	root.gitBreachDiffRawText = "-	topsecret123\n+	newsecret456"

	got := root.gitBreachContextSnapshot()

	if strings.Contains(got, "topsecret123") {
		t.Errorf("snapshot = %q, must not contain actual diff content without Machiavelli Mode", got)
	}
}

// TestGitBreachContextSnapshotIncludesDiffUnderMachiavelliMode pins
// the explicit, opted-in exception: once the user has turned the mode
// on, whatever diff is currently shown in Main is forwarded verbatim.
func TestGitBreachContextSnapshotIncludesDiffUnderMachiavelliMode(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.gitBreachStatus = git.Status{Branch: "main"}
	root.settings.ConsigliereHardcoreMode = true
	root.gitBreachDiffRawText = "-	topsecret123\n+	newsecret456"

	got := root.gitBreachContextSnapshot()

	if !strings.Contains(got, "topsecret123") {
		t.Errorf("snapshot = %q, want the diff forwarded under Machiavelli Mode", got)
	}
}

// TestGitBreachContextSnapshotOmitsDiffWhenNothingShown pins the
// other edge: Machiavelli Mode being on doesn't forward anything if
// Main isn't actually showing real diff content right now (no
// selection, a conflict row, a cancelled fetch).
func TestGitBreachContextSnapshotOmitsDiffWhenNothingShown(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.gitBreachStatus = git.Status{Branch: "main"}
	root.settings.ConsigliereHardcoreMode = true
	root.gitBreachDiffRawText = ""

	got := root.gitBreachContextSnapshot()

	if strings.Contains(got, "Machiavelli") {
		t.Errorf("snapshot = %q, must not mention Machiavelli Mode when nothing real is shown", got)
	}
}

func TestGitBreachContextSnapshotCapsRecentCommits(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.gitBreachStatus = git.Status{Branch: "main"}
	for i := 0; i < consigliereMaxRecentCommits+5; i++ {
		root.gitBreachCommits = append(root.gitBreachCommits, git.LogEntry{Subject: "distinct-subject"})
	}

	got := root.gitBreachContextSnapshot()

	if got := strings.Count(got, "distinct-subject"); got != consigliereMaxRecentCommits {
		t.Errorf("snapshot mentions %d commit subjects, want exactly the cap of %d", got, consigliereMaxRecentCommits)
	}
}

// TestFirewallContextSnapshotOmitsRuleContentByDefault pins the
// ordinary, privacy-preserving case: even with a real ruleset loaded,
// only the backend and a count are included unless Machiavelli Mode
// has been explicitly turned on — a full ruleset describes this
// host's complete inbound attack surface, the same reasoning
// Log Holepunch's log lines and Git Breach's diff content already
// follow.
func TestFirewallContextSnapshotOmitsRuleContentByDefault(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.firewallSnapshot = firewall.Snapshot{
		Backend: firewall.BackendUFW,
		Rules: []firewall.Rule{
			{Order: 1, Direction: firewall.DirectionIn, Action: firewall.ActionAllow, Protocol: "tcp", PortFrom: 22, PortTo: 22, Source: "10.0.0.0/8"},
		},
	}

	got := root.firewallContextSnapshot()

	for _, want := range []string{"Firewall", "ufw", "1 rules configured"} {
		if !strings.Contains(got, want) {
			t.Errorf("snapshot = %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "10.0.0.0/8") {
		t.Errorf("snapshot = %q, must not contain actual rule content without Machiavelli Mode", got)
	}
}

// TestFirewallContextSnapshotIncludesRulesUnderMachiavelliMode pins
// the explicit, opted-in exception: once the user has turned the mode
// on, the full ruleset is forwarded verbatim.
func TestFirewallContextSnapshotIncludesRulesUnderMachiavelliMode(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.settings.ConsigliereHardcoreMode = true
	root.firewallSnapshot = firewall.Snapshot{
		Backend: firewall.BackendUFW,
		Rules: []firewall.Rule{
			{Order: 1, Direction: firewall.DirectionIn, Action: firewall.ActionAllow, Protocol: "tcp", PortFrom: 22, PortTo: 22, Source: "10.0.0.0/8"},
			{Order: 2, Direction: firewall.DirectionIn, Action: firewall.ActionDeny, Protocol: "tcp", PortFrom: 22, PortTo: 22,
				ShadowedByOrder: 1, ShadowedByAction: firewall.ActionAllow},
		},
	}

	got := root.firewallContextSnapshot()

	for _, want := range []string{"ALLOW", "10.0.0.0/8", "shadowed by #1"} {
		if !strings.Contains(got, want) {
			t.Errorf("snapshot = %q, want it to contain %q under Machiavelli Mode", got, want)
		}
	}
}

// TestFirewallContextSnapshotReportsNoBackendActive pins the other
// real case: no supported firewall is currently governing traffic at
// all, which must read as a clear statement, not an empty or
// confusing snapshot.
func TestFirewallContextSnapshotReportsNoBackendActive(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.firewallSnapshot = firewall.Snapshot{Backend: firewall.BackendNone}

	got := root.firewallContextSnapshot()

	if !strings.Contains(got, "No supported firewall backend") {
		t.Errorf("snapshot = %q, want it to clearly state no backend is active", got)
	}
}

// TestFirewallContextSnapshotReportsReadError pins the real,
// live-observed case (no permission to read the ruleset, the common
// case on a non-root dev run): the error itself is surfaced, not a
// silently empty or misleading snapshot.
func TestFirewallContextSnapshotReportsReadError(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.firewallErr = errors.New("permission denied (you must be root)")

	got := root.firewallContextSnapshot()

	if !strings.Contains(got, "permission denied") {
		t.Errorf("snapshot = %q, want it to surface the read error", got)
	}
}

// TestFirewallContextSnapshotCapsRuleList pins the same cap-list
// discipline every other snapshot builder here already follows, once
// Machiavelli Mode is on.
func TestFirewallContextSnapshotCapsRuleList(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.settings.ConsigliereHardcoreMode = true
	cap := root.consigliereContextLineCap()
	var rules []firewall.Rule
	for i := 0; i < cap+5; i++ {
		rules = append(rules, firewall.Rule{Order: i + 1, Action: firewall.ActionAllow, Source: "distinct-source"})
	}
	root.firewallSnapshot = firewall.Snapshot{Backend: firewall.BackendUFW, Rules: rules}

	got := root.firewallContextSnapshot()

	if got := strings.Count(got, "distinct-source"); got != cap {
		t.Errorf("snapshot mentions %d rules, want exactly the cap of %d", got, cap)
	}
}

// TestMountsContextSnapshotIncludesEntries pins the real mount table
// being included by default — unlike Firewall's own ruleset, a mount
// entry says at most "this box talks to this remote host", already
// exactly as visible the moment the panel itself browses into it.
func TestMountsContextSnapshotIncludesEntries(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.mountsEntries = []mounts.Entry{
		{Target: "/mnt/home", Source: "family-nas:/export/home", Fstype: "nfs4", Persistent: true},
	}

	got := root.mountsContextSnapshot()

	for _, want := range []string{"Mounts", "1 mounts", "family-nas:/export/home", "/mnt/home", "nfs4", "survives reboot"} {
		if !strings.Contains(got, want) {
			t.Errorf("snapshot = %q, want it to contain %q", got, want)
		}
	}
}

// TestMountsContextSnapshotReportsReadError pins the real error case:
// a failed findmnt read is surfaced, not a silently empty snapshot.
func TestMountsContextSnapshotReportsReadError(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.mountsErr = errors.New("findmnt: command not found")

	got := root.mountsContextSnapshot()

	if !strings.Contains(got, "findmnt: command not found") {
		t.Errorf("snapshot = %q, want it to surface the read error", got)
	}
}

// TestMountsContextSnapshotCapsEntryList pins the same cap-list
// discipline every other snapshot builder here already follows.
func TestMountsContextSnapshotCapsEntryList(t *testing.T) {
	root := newPlainKeyRoot(t)
	var entries []mounts.Entry
	for i := 0; i < consigliereMaxMountEntries+5; i++ {
		entries = append(entries, mounts.Entry{Target: "distinct-target", Source: "src", Fstype: "ext4"})
	}
	root.mountsEntries = entries

	got := root.mountsContextSnapshot()

	if got := strings.Count(got, "distinct-target"); got != consigliereMaxMountEntries {
		t.Errorf("snapshot mentions %d mounts, want exactly the cap of %d", got, consigliereMaxMountEntries)
	}
}

// TestConsigliereContextSnapshotDispatchesToMounts pins the dispatch
// switch's own Mounts case.
func TestConsigliereContextSnapshotDispatchesToMounts(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.activePage = mountsPage
	root.mountsEntries = []mounts.Entry{{Target: "/mnt/home"}}

	got := root.consigliereContextSnapshot()

	if !strings.Contains(got, "Current view: Mounts.") {
		t.Errorf("snapshot = %q, want the Mounts snapshot, not the generic panel one", got)
	}
}

// TestSessionsContextSnapshotIncludesSessions pins the real session
// list being included by default — a session name is a similar risk
// to a Git Breach commit subject, already sent by default.
func TestSessionsContextSnapshotIncludesSessions(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.sessionsList = []multiplex.Session{
		{Name: "work", Backend: multiplex.BackendTmux, Status: multiplex.StatusAttached},
	}

	got := root.sessionsContextSnapshot()

	for _, want := range []string{"Sessions", "1 sessions", "work", "tmux", "attached"} {
		if !strings.Contains(got, want) {
			t.Errorf("snapshot = %q, want it to contain %q", got, want)
		}
	}
}

// TestSessionsContextSnapshotReportsReadError pins the real error
// case: neither backend installed is surfaced, not a silently empty
// snapshot.
func TestSessionsContextSnapshotReportsReadError(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.sessionsErr = errors.New("neither screen nor tmux nor zellij is installed")

	got := root.sessionsContextSnapshot()

	if !strings.Contains(got, "neither screen nor tmux nor zellij is installed") {
		t.Errorf("snapshot = %q, want it to surface the read error", got)
	}
}

// TestSessionsContextSnapshotCapsSessionList pins the same cap-list
// discipline every other snapshot builder here already follows.
func TestSessionsContextSnapshotCapsSessionList(t *testing.T) {
	root := newPlainKeyRoot(t)
	var sessions []multiplex.Session
	for i := 0; i < consigliereMaxSessions+5; i++ {
		sessions = append(sessions, multiplex.Session{Name: "distinct-session", Backend: multiplex.BackendScreen})
	}
	root.sessionsList = sessions

	got := root.sessionsContextSnapshot()

	if got := strings.Count(got, "distinct-session"); got != consigliereMaxSessions {
		t.Errorf("snapshot mentions %d sessions, want exactly the cap of %d", got, consigliereMaxSessions)
	}
}

// TestConsigliereContextSnapshotDispatchesToSessions pins the
// dispatch switch's own Sessions case.
func TestConsigliereContextSnapshotDispatchesToSessions(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.activePage = sessionsPage
	root.sessionsList = []multiplex.Session{{Name: "work"}}

	got := root.consigliereContextSnapshot()

	if !strings.Contains(got, "Current view: Sessions.") {
		t.Errorf("snapshot = %q, want the Sessions snapshot, not the generic panel one", got)
	}
}

// TestSSHKeysContextSnapshotIncludesKeyPairs pins the real key
// inventory being included by default — never the key material
// itself, which this screen's own design never reads back out in the
// first place.
func TestSSHKeysContextSnapshotIncludesKeyPairs(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.ssh.KeysPairs = []sshkeys.KeyPair{
		{Name: "id_ed25519", Type: "ed25519", Bits: 256, Encrypted: true, AgentLoaded: true},
	}

	got := root.sshKeysContextSnapshot()

	for _, want := range []string{"SSH Keys", "1 key pairs", "id_ed25519", "ed25519", "256-bit", "passphrase-encrypted", "loaded in agent"} {
		if !strings.Contains(got, want) {
			t.Errorf("snapshot = %q, want it to contain %q", got, want)
		}
	}
}

// TestSSHKeysContextSnapshotReportsReadError pins the real error
// case: a failed scan is surfaced, not a silently empty snapshot.
func TestSSHKeysContextSnapshotReportsReadError(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.ssh.KeysErr = errors.New("reading ~/.ssh: permission denied")

	got := root.sshKeysContextSnapshot()

	if !strings.Contains(got, "permission denied") {
		t.Errorf("snapshot = %q, want it to surface the read error", got)
	}
}

// TestSSHKeysContextSnapshotCapsKeyList pins the same cap-list
// discipline every other snapshot builder here already follows.
func TestSSHKeysContextSnapshotCapsKeyList(t *testing.T) {
	root := newPlainKeyRoot(t)
	var pairs []sshkeys.KeyPair
	for i := 0; i < consigliereMaxSSHKeys+5; i++ {
		pairs = append(pairs, sshkeys.KeyPair{Name: "distinct-key"})
	}
	root.ssh.KeysPairs = pairs

	got := root.sshKeysContextSnapshot()

	if got := strings.Count(got, "distinct-key"); got != consigliereMaxSSHKeys {
		t.Errorf("snapshot mentions %d key pairs, want exactly the cap of %d", got, consigliereMaxSSHKeys)
	}
}

// TestConsigliereContextSnapshotDispatchesToSSHKeys pins the dispatch
// switch's own SSH Keys case.
func TestConsigliereContextSnapshotDispatchesToSSHKeys(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.activePage = sshKeysPage
	root.ssh.KeysPairs = []sshkeys.KeyPair{{Name: "id_ed25519"}}

	got := root.consigliereContextSnapshot()

	if !strings.Contains(got, "Current view: SSH Keys.") {
		t.Errorf("snapshot = %q, want the SSH Keys snapshot, not the generic panel one", got)
	}
}

// TestActivityLogContextSnapshotOmitsMessagesByDefault pins the
// ordinary, privacy-preserving case: even with real entries loaded,
// only the total count and a category breakdown are included unless
// Machiavelli Mode has been explicitly turned on — an entry's own
// Message is a real file path from the user's own filesystem, the
// same risk category a Log Holepunch log line already has.
func TestActivityLogContextSnapshotOmitsMessagesByDefault(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.activityLogAllEntries = []activitylog.Entry{
		{Level: activitylog.LevelActions, Category: activitylog.CategoryFileOps, Message: "copied /home/jens/topsecret123.txt"},
	}

	got := root.activityLogContextSnapshot()

	for _, want := range []string{"Action Log", "1 entries", "fileops: 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("snapshot = %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "topsecret123") {
		t.Errorf("snapshot = %q, must not contain an entry's own Message without Machiavelli Mode", got)
	}
}

// TestActivityLogContextSnapshotIncludesMessagesUnderMachiavelliMode
// pins the explicit, opted-in exception: once the user has turned the
// mode on, the currently-matching entries are forwarded verbatim.
func TestActivityLogContextSnapshotIncludesMessagesUnderMachiavelliMode(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.settings.ConsigliereHardcoreMode = true
	root.activityLogAllEntries = []activitylog.Entry{
		{Level: activitylog.LevelActions, Category: activitylog.CategoryFileOps, Message: "copied /home/jens/topsecret123.txt"},
	}

	got := root.activityLogContextSnapshot()

	if !strings.Contains(got, "topsecret123") {
		t.Errorf("snapshot = %q, want the matched entry forwarded under Machiavelli Mode", got)
	}
}

// TestConsigliereContextSnapshotDispatchesToActivityLog pins the
// dispatch switch's own Action Log case.
func TestConsigliereContextSnapshotDispatchesToActivityLog(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.activePage = activityLogPage
	root.activityLogAllEntries = []activitylog.Entry{{Message: "copied a.txt"}}

	got := root.consigliereContextSnapshot()

	if !strings.Contains(got, "Current view: Action Log.") {
		t.Errorf("snapshot = %q, want the Action Log snapshot, not the generic panel one", got)
	}
}

// TestTransferCenterContextSnapshotOmitsPathsByDefault pins the
// ordinary, privacy-preserving case: even with real operations
// loaded, only the total count and a type breakdown are included
// unless Machiavelli Mode has been explicitly turned on — an
// operation's own Source/Target is a real file path from the user's
// own filesystem, the same risk category an Action Log entry's own
// Message already has.
func TestTransferCenterContextSnapshotOmitsPathsByDefault(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.transferCenterOps = []operations.Operation{
		{Type: "paste", Source: "/home/jens/topsecret123.txt", Target: "/mnt/backup"},
	}

	got := root.transferCenterContextSnapshot()

	for _, want := range []string{"Undertow", "1 operations", "paste: 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("snapshot = %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "topsecret123") {
		t.Errorf("snapshot = %q, must not contain an operation's own Source/Target without Machiavelli Mode", got)
	}
}

// TestTransferCenterContextSnapshotIncludesPathsUnderMachiavelliMode
// pins the explicit, opted-in exception: once the user has turned the
// mode on, the current operations' own Source/Target are forwarded
// verbatim.
func TestTransferCenterContextSnapshotIncludesPathsUnderMachiavelliMode(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.settings.ConsigliereHardcoreMode = true
	root.transferCenterOps = []operations.Operation{
		{Type: "paste", Source: "/home/jens/topsecret123.txt", Target: "/mnt/backup"},
	}

	got := root.transferCenterContextSnapshot()

	if !strings.Contains(got, "topsecret123") {
		t.Errorf("snapshot = %q, want the operation's own Source forwarded under Machiavelli Mode", got)
	}
}

// TestTransferCenterContextSnapshotCapsOperationList pins the same
// anthropicMaxTokens-style cap every other list-returning snapshot
// here already follows.
func TestTransferCenterContextSnapshotCapsOperationList(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.settings.ConsigliereHardcoreMode = true
	for i := 0; i < root.consigliereContextLineCap()+5; i++ {
		root.transferCenterOps = append(root.transferCenterOps, operations.Operation{
			Type:   "paste",
			Source: fmt.Sprintf("/src/%d", i),
			Target: "/dst",
		})
	}

	got := root.transferCenterContextSnapshot()

	if !strings.Contains(got, "5 further operations not shown") {
		t.Errorf("snapshot = %q, want it to mention the 5 truncated operations", got)
	}
}

// TestConsigliereContextSnapshotDispatchesToTransferCenter pins the
// dispatch switch's own Undertow case.
func TestConsigliereContextSnapshotDispatchesToTransferCenter(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.activePage = transferCenterPage
	root.transferCenterOps = []operations.Operation{{Type: "paste", Source: "/a", Target: "/b"}}

	got := root.consigliereContextSnapshot()

	if !strings.Contains(got, "Current view: Undertow.") {
		t.Errorf("snapshot = %q, want the Undertow snapshot, not the generic panel one", got)
	}
}

// TestToolboxContextSnapshotIncludesEntries pins the ungated case:
// the catalog is a fixed, built-in list of tool names and
// descriptions, never a live read of this host's own state, so it is
// always forwarded in full, with no Machiavelli Mode gate.
func TestToolboxContextSnapshotIncludesEntries(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.openNetworkTools()

	got := root.toolboxContextSnapshot()

	if !strings.Contains(got, "Toolbox") {
		t.Errorf("snapshot = %q, want it to mention the Toolbox view", got)
	}
	if !strings.Contains(got, "Ping:") {
		t.Errorf("snapshot = %q, want it to list the Ping entry", got)
	}
}

// TestConsigliereContextSnapshotDispatchesToToolbox pins the dispatch
// switch's own Toolbox case.
func TestConsigliereContextSnapshotDispatchesToToolbox(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.openNetworkTools()

	got := root.consigliereContextSnapshot()

	if !strings.Contains(got, "Current view: Toolbox.") {
		t.Errorf("snapshot = %q, want the Toolbox snapshot, not the generic panel one", got)
	}
}

// TestCompareTreeContextSnapshotIncludesDifferingEntries pins the
// ungated case: a relative path shared between two local directories
// is the same risk tier Git Breach's own file status list already
// sends by default, so it is never withheld behind Machiavelli Mode.
func TestCompareTreeContextSnapshotIncludesDifferingEntries(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.compareTreeA, root.compareTreeB = "/tmp/a", "/tmp/b"
	root.compareTreeStats = compare.Stats{Differs: 1, OnlyInA: 1}
	root.compareTreeEntries = []compare.Entry{
		{RelPath: "changed.txt", Verdict: compare.Differs},
		{RelPath: "only-a.txt", Verdict: compare.OnlyInA},
		{RelPath: "same.txt", Verdict: compare.Identical},
	}

	got := root.compareTreeContextSnapshot()

	for _, want := range []string{"Compare Faultline", "/tmp/a", "/tmp/b", "changed.txt: differs", "only-a.txt: only in A"} {
		if !strings.Contains(got, want) {
			t.Errorf("snapshot = %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "same.txt") {
		t.Errorf("snapshot = %q, must not list an identical entry", got)
	}
}

// TestCompareTreeContextSnapshotCapsEntryList pins the same
// anthropicMaxTokens-style cap every other list-returning snapshot
// here already follows.
func TestCompareTreeContextSnapshotCapsEntryList(t *testing.T) {
	root := newPlainKeyRoot(t)
	for i := 0; i < consigliereMaxCompareTreeEntries+5; i++ {
		root.compareTreeEntries = append(root.compareTreeEntries, compare.Entry{
			RelPath: fmt.Sprintf("file-%d.txt", i),
			Verdict: compare.Differs,
		})
	}

	got := root.compareTreeContextSnapshot()

	if !strings.Contains(got, "5 further entries not shown") {
		t.Errorf("snapshot = %q, want it to mention the 5 truncated entries", got)
	}
}

// TestConsigliereContextSnapshotDispatchesToCompareTree pins the
// dispatch switch's own Compare Faultline case.
func TestConsigliereContextSnapshotDispatchesToCompareTree(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.activePage = compareTreePage
	root.compareTreeEntries = []compare.Entry{{RelPath: "a.txt", Verdict: compare.Differs}}

	got := root.consigliereContextSnapshot()

	if !strings.Contains(got, "Current view: Compare Faultline.") {
		t.Errorf("snapshot = %q, want the Compare Faultline snapshot, not the generic panel one", got)
	}
}
