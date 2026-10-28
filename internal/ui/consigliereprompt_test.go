package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/git"
)

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

	if !strings.Contains(got, "Log Audit") {
		t.Errorf("snapshot = %q, want a Log Audit snapshot even though consigliereExitPage still names the selection screen", got)
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

	for _, want := range []string{"Log Audit", "/var/log/myapp", "timeout", "error", "2026-10-01", "2026-10-28"} {
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
