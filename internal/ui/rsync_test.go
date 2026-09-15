package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rivo/tview"
)

func newTestRootForRsync(t *testing.T) (r *Root, dir string) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 200, 50) // openRsync sizes/centers against this
	return r, dir
}

func TestOpenRsyncOpensTheDialogWithFormItems(t *testing.T) {
	r, _ := newTestRootForRsync(t)

	r.openRsync()

	if r.activePage != rsyncPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, rsyncPage)
	}
	for _, label := range []string{"Source", "Destination", "Exclude (comma-separated patterns)", "Extra flags"} {
		if r.rsyncForm.GetFormItemByLabel(label) == nil {
			t.Errorf("form is missing an item labeled %q", label)
		}
	}
}

// TestDefaultRsyncSourceUsesTheSingleSelectedEntry pins that exactly
// one selected file becomes Source — several files at once have no
// single obvious source, so that case falls back to the panel's own
// directory instead (see TestDefaultRsyncSourceFallsBackToThePanelPath).
func TestDefaultRsyncSourceUsesTheSingleSelectedEntry(t *testing.T) {
	r, dir := newTestRootForRsync(t)
	r.panel.focusRow(1) // off ".." onto a.txt
	r.panel.toggleCheckbox(1)

	got := r.defaultRsyncSource()

	want := filepath.Join(dir, "a.txt")
	if got != want {
		t.Errorf("defaultRsyncSource() = %q, want %q", got, want)
	}
}

func TestDefaultRsyncSourceFallsBackToThePanelPath(t *testing.T) {
	r, dir := newTestRootForRsync(t)

	got := r.defaultRsyncSource()

	if got != dir {
		t.Errorf("defaultRsyncSource() = %q, want the panel's own directory %q", got, dir)
	}
}

// TestDefaultRsyncDestinationUsesTheSplitPartner pins the one case
// where "the other side" is unambiguous — see openRsync's own doc
// comment on why every other case is deliberately left blank instead
// of guessed.
func TestDefaultRsyncDestinationUsesTheSplitPartner(t *testing.T) {
	r, dir := newTestRootForRsync(t)
	otherDir := t.TempDir()
	r.newTabHere()
	if err := r.panel.load(otherDir); err != nil {
		t.Fatal(err)
	}
	// splitWithTab(i) splits the *active* tab (this new one, showing
	// otherDir) with tab i — passing 0 here pairs it with the original
	// tab (dir), which becomes the split partner; r.panel itself stays
	// on otherDir throughout, matching defaultRsyncSource's own read of
	// r.panel.path right alongside this.
	r.splitWithTab(0)

	if r.panel.path != otherDir {
		t.Fatalf("setup: r.panel.path = %q, want %q (the tab the split was started from)", r.panel.path, otherDir)
	}

	got := r.defaultRsyncDestination()

	if got != dir {
		t.Errorf("defaultRsyncDestination() = %q, want the split partner's own path %q", got, dir)
	}
}

func TestDefaultRsyncDestinationIsBlankWithoutASplitPartner(t *testing.T) {
	r, _ := newTestRootForRsync(t)

	if got := r.defaultRsyncDestination(); got != "" {
		t.Errorf("defaultRsyncDestination() = %q, want empty with no split view open", got)
	}
}

func TestSplitRsyncExcludesTrimsAndDropsEmptyEntries(t *testing.T) {
	got := splitRsyncExcludes(" *.log ,, node_modules ,")
	want := []string{"*.log", "node_modules"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("splitRsyncExcludes = %v, want %v", got, want)
	}
}

func TestSplitRsyncExcludesOnBlankTextReturnsNothing(t *testing.T) {
	if got := splitRsyncExcludes("   "); len(got) != 0 {
		t.Errorf("splitRsyncExcludes(blank) = %v, want none", got)
	}
}

// TestCurrentRsyncJobReflectsEveryFieldAndFlag pins that the Job
// actually run never disagrees with what the dialog's own fields and
// toggles say — the whole point of building it fresh from their
// current state every time rather than caching it anywhere.
func TestCurrentRsyncJobReflectsEveryFieldAndFlag(t *testing.T) {
	r, _ := newTestRootForRsync(t)
	r.openRsync()

	r.rsyncSourceField.SetText("/data/src")
	r.rsyncDestinationField.SetText("/data/dst")
	r.rsyncExcludesField.SetText("*.log, tmp")
	r.rsyncExtraArgsField.SetText("--bwlimit=500")
	r.toggleRsyncFlag(rsyncLabelDelete)
	r.toggleRsyncFlag(rsyncLabelDryRun)

	job := r.currentRsyncJob()

	if job.Source.Path != "/data/src" || job.Destination.Path != "/data/dst" {
		t.Errorf("Source/Destination = %q/%q, want /data/src //data/dst", job.Source.Path, job.Destination.Path)
	}
	if !job.Archive {
		t.Error("Archive = false, want the dialog's own default of true")
	}
	if !job.Delete || !job.DryRun {
		t.Errorf("Delete/DryRun = %v/%v, want both true after toggling them on", job.Delete, job.DryRun)
	}
	if job.Compress || job.CopyContents {
		t.Errorf("Compress/CopyContents = %v/%v, want both false (never toggled)", job.Compress, job.CopyContents)
	}
	if strings.Join(job.Excludes, ",") != "*.log,tmp" {
		t.Errorf("Excludes = %v, want [*.log tmp]", job.Excludes)
	}
	if job.ExtraArgs != "--bwlimit=500" {
		t.Errorf("ExtraArgs = %q, want %q", job.ExtraArgs, "--bwlimit=500")
	}
	if !job.Progress {
		t.Error("Progress = false, want it always on for a real run")
	}
}

func TestRenderRsyncPreviewShowsTheRealCommand(t *testing.T) {
	r, _ := newTestRootForRsync(t)
	r.openRsync()
	r.rsyncSourceField.SetText("/src")
	r.rsyncDestinationField.SetText("/dst")

	r.renderRsyncPreview()

	got := r.rsyncPreviewView.GetText(true)
	if !strings.Contains(got, "rsync") || !strings.Contains(got, "'/src'") || !strings.Contains(got, "'/dst'") {
		t.Errorf("preview text = %q, want it to contain the real rsync command", got)
	}
}

// TestRenderRsyncPreviewHighlightsDeleteWithAWarningTag pins that
// --delete specifically gets its own visibly different color once
// it's turned on — the one flag here that can permanently remove
// files at the destination.
func TestRenderRsyncPreviewHighlightsDeleteWithAWarningTag(t *testing.T) {
	r, _ := newTestRootForRsync(t)
	r.openRsync()
	r.rsyncSourceField.SetText("/src")
	r.rsyncDestinationField.SetText("/dst")
	r.toggleRsyncFlag(rsyncLabelDelete)

	got := r.rsyncPreviewView.GetText(false) // raw, with style tags still in place
	if !strings.Contains(got, colorTag(r.theme.WarningText)) {
		t.Errorf("preview text = %q, want it to carry the warning color tag around --delete", got)
	}
}

// TestRunRsyncRefusesAnEmptySourceOrDestination pins the guard
// against rsync's own real, surprising behavior for an empty
// positional argument (POSIX shell parameter rules make an empty,
// unquoted field simply vanish from the argument list, silently
// shifting every argument after it) — verified directly, not assumed,
// so this is refused outright before ever reaching a real shell.
func TestRunRsyncRefusesAnEmptySourceOrDestination(t *testing.T) {
	r, _ := newTestRootForRsync(t)
	r.openRsync()
	r.rsyncSourceField.SetText("/src")
	r.rsyncDestinationField.SetText("") // left blank

	r.runRsync()

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, errorPage)
	}
	got := strings.ReplaceAll(r.errorView.GetText(true), "\n", " ")
	if !strings.Contains(got, "required") {
		t.Errorf("error text = %q, want it to explain that both fields are required", got)
	}
}

func TestToggleRsyncFlagFlipsStateAndRelabelsTheRow(t *testing.T) {
	r, _ := newTestRootForRsync(t)
	r.openRsync()

	if r.rsyncFlags[rsyncLabelCompress] {
		t.Fatal("setup: Compress should start off")
	}

	r.toggleRsyncFlag(rsyncLabelCompress)

	if !r.rsyncFlags[rsyncLabelCompress] {
		t.Error("toggleRsyncFlag did not flip the flag's own state")
	}
	var rowText string
	for i, label := range rsyncFlagOrder {
		if label == rsyncLabelCompress {
			rowText, _ = r.rsyncFlagsList.GetItemText(i)
		}
	}
	if !strings.HasPrefix(rowText, "●") {
		t.Errorf("row text = %q, want it to start with the filled toggle glyph", rowText)
	}
}
