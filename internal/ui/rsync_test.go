package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
	"github.com/jagottsicher/breakthrough/internal/rsync"
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

// TestOpenRsyncFillsItsEntireRectSoNothingBehindItShowsThrough guards
// against openRsync's own height drifting out of sync with
// newRsyncContentLayout's real row count: every child there is a fixed-
// size Flex item (no proportional one to soak up leftover space), so a
// height taller than the content actually needs leaves genuine gaps at
// the bottom of the dialog's own rect that no widget ever paints —
// gaps that then show whatever the underlying panel last drew there
// instead of the dialog's own background. Filling the whole screen with
// a sentinel rune before Draw and checking that none of it survives
// inside the dialog's own rect catches exactly that regression,
// regardless of which literal height value openRsync happens to use.
func TestOpenRsyncFillsItsEntireRectSoNothingBehindItShowsThrough(t *testing.T) {
	r, _ := newTestRootForRsync(t)
	r.openRsync()

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(200, 50)

	const sentinel = "X"
	for y := 0; y < 50; y++ {
		for x := 0; x < 200; x++ {
			screen.SetContent(x, y, 'X', nil, tcell.StyleDefault)
		}
	}

	r.rsyncLayout.Draw(screen)

	x, y, width, height := r.rsyncLayout.GetRect()
	for row := y; row < y+height; row++ {
		for col := x; col < x+width; col++ {
			if ch, _, _ := screen.Get(col, row); ch == sentinel {
				t.Fatalf("cell (%d,%d) inside the dialog's own rect (%d,%d,%d,%d) still shows the sentinel — this row is never painted by any widget", col, row, x, y, width, height)
			}
		}
	}
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
	if got.text != want {
		t.Errorf("defaultRsyncSource().text = %q, want %q", got.text, want)
	}
}

func TestDefaultRsyncSourceFallsBackToThePanelPath(t *testing.T) {
	r, dir := newTestRootForRsync(t)

	got := r.defaultRsyncSource()

	if got.text != dir {
		t.Errorf("defaultRsyncSource().text = %q, want the panel's own directory %q", got.text, dir)
	}
}

// TestDefaultRsyncSourceUsesTheCursorRowWithNothingSelected pins the
// bare-right-click case: it moves the cursor to the clicked row but
// marks nothing, so the source must still be that row, not the panel's
// own directory.
func TestDefaultRsyncSourceUsesTheCursorRowWithNothingSelected(t *testing.T) {
	r, dir := newTestRootForRsync(t)
	r.panel.focusRow(1) // off ".." onto a.txt, nothing checked

	got := r.defaultRsyncSource()

	want := filepath.Join(dir, "a.txt")
	if got.text != want {
		t.Errorf("defaultRsyncSource().text = %q, want %q", got.text, want)
	}
}

// TestDefaultRsyncSourceMarksACursorFileAsIsFile pins the one bit
// applyRsyncCopyContentsFlagToField/endpoint both key their own
// file-vs-directory substitution on — see their own doc comments.
func TestDefaultRsyncSourceMarksACursorFileAsIsFile(t *testing.T) {
	r, _ := newTestRootForRsync(t)
	r.panel.focusRow(1) // off ".." onto a.txt, nothing checked

	got := r.defaultRsyncSource()

	if !got.isFile {
		t.Error("defaultRsyncSource().isFile = false, want true for a plain file under the cursor")
	}
}

// TestDefaultRsyncSourceDoesNotMarkTheDirectoryFallbackAsIsFile is the
// flip side: falling back to the panel's own current directory (cursor
// still on "..") must never be mistaken for a file default.
func TestDefaultRsyncSourceDoesNotMarkTheDirectoryFallbackAsIsFile(t *testing.T) {
	r, _ := newTestRootForRsync(t)

	got := r.defaultRsyncSource()

	if got.isFile {
		t.Error("defaultRsyncSource().isFile = true, want false for the panel's own directory")
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

	if got.text != dir {
		t.Errorf("defaultRsyncDestination().text = %q, want the split partner's own path %q", got.text, dir)
	}
}

func TestDefaultRsyncDestinationIsBlankWithoutASplitPartner(t *testing.T) {
	r, _ := newTestRootForRsync(t)

	if got := r.defaultRsyncDestination(); got.text != "" {
		t.Errorf("defaultRsyncDestination().text = %q, want empty with no split view open", got.text)
	}
}

// TestDefaultRsyncSourceUsesUserHostSyntaxWhenThePanelIsRemote pins
// the actual point of this feature: a tab already connected via the
// Connect dialog shouldn't need Host/User typed into Rsync a second
// time by hand.
func TestDefaultRsyncSourceUsesUserHostSyntaxWhenThePanelIsRemote(t *testing.T) {
	r, _ := newTestRootForRsync(t)
	client := &fakeRemoteClient{root: "/remote", entries: map[string][]fsops.Entry{
		"/remote": {{Name: "b.txt", Type: fsops.TypeFile}},
	}}
	if err := r.panel.connectRemote(client, remotefs.Connection{Host: "example.com", User: "tester"}); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}

	got := r.defaultRsyncSource()

	if want := "tester@example.com:/remote"; got.text != want {
		t.Errorf("defaultRsyncSource().text = %q, want %q", got.text, want)
	}
	if got.conn == nil {
		t.Fatal("defaultRsyncSource().conn = nil, want the connection tracked alongside it")
	}
}

// TestDefaultRsyncDestinationUsesUserHostSyntaxWhenTheSplitPartnerIsRemote
// mirrors the source-side test above, against the split partner's own
// panel instead.
func TestDefaultRsyncDestinationUsesUserHostSyntaxWhenTheSplitPartnerIsRemote(t *testing.T) {
	r, dir := newTestRootForRsync(t)
	r.newTabHere()
	client := &fakeRemoteClient{root: "/remote", entries: map[string][]fsops.Entry{
		"/remote": {{Name: "b.txt", Type: fsops.TypeFile}},
	}}
	if err := r.panel.connectRemote(client, remotefs.Connection{Host: "example.com", User: "tester"}); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}
	r.switchToTab(0)
	if r.panel.path != dir {
		t.Fatalf("setup: r.panel.path = %q, want %q", r.panel.path, dir)
	}
	r.splitWithTab(1)

	got := r.defaultRsyncDestination()

	if want := "tester@example.com:/remote"; got.text != want {
		t.Errorf("defaultRsyncDestination().text = %q, want %q", got.text, want)
	}
}

// TestCurrentRsyncJobPropagatesThePortFromAnUntouchedRemoteDefault
// pins the one thing rsync's own compact "user@host:path" syntax has
// no room for at all (see rsync.Endpoint's own Port field doc
// comment): a non-default port only ever travels through to the real
// -e 'ssh -p PORT' flag when the field still reads exactly what was
// defaulted from a live connection.
func TestCurrentRsyncJobPropagatesThePortFromAnUntouchedRemoteDefault(t *testing.T) {
	r, _ := newTestRootForRsync(t)
	client := &fakeRemoteClient{root: "/remote", entries: map[string][]fsops.Entry{
		"/remote": {{Name: "b.txt", Type: fsops.TypeFile}},
	}}
	if err := r.panel.connectRemote(client, remotefs.Connection{Host: "example.com", User: "tester", Port: 2222}); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}
	r.openRsync() // resetRsyncForm defaults Source from r.panel, untouched from here on

	job := r.currentRsyncJob()

	if job.Source.Host != "example.com" || job.Source.User != "tester" || job.Source.Port != 2222 {
		t.Errorf("Source = %+v, want Host=example.com User=tester Port=2222", job.Source)
	}
	if !strings.Contains(job.Command(), "'-e' 'ssh -p 2222'") {
		t.Errorf("Command() = %q, want it to carry the connection's own non-default port via -e", job.Command())
	}
}

// TestCurrentRsyncJobTreatsAnEditedRemoteDefaultAsPlainTextWithNoPort
// is the flip side: once the user has typed something different into
// the field, the port can no longer be assumed — the same reason a
// bare `ssh host` typed by hand doesn't know a non-standard port
// either.
func TestCurrentRsyncJobTreatsAnEditedRemoteDefaultAsPlainTextWithNoPort(t *testing.T) {
	r, _ := newTestRootForRsync(t)
	client := &fakeRemoteClient{root: "/remote", entries: map[string][]fsops.Entry{
		"/remote": {{Name: "b.txt", Type: fsops.TypeFile}},
	}}
	if err := r.panel.connectRemote(client, remotefs.Connection{Host: "example.com", User: "tester", Port: 2222}); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}
	r.openRsync()
	r.rsyncSourceField.SetText("tester@example.com:/remote/elsewhere") // edited: different path than what was defaulted

	job := r.currentRsyncJob()

	if job.Source.Port != 0 {
		t.Errorf("Source.Port = %d, want 0 (no port known for hand-typed text)", job.Source.Port)
	}
	if job.Source.Path != "tester@example.com:/remote/elsewhere" {
		t.Errorf("Source.Path = %q, want the whole typed string verbatim (Host unset)", job.Source.Path)
	}
}

func TestRsyncRelayHintOnlyAppearsWhenBothEndpointsAreRemote(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	remote := rsync.Endpoint{Host: "example.com", Path: "/x"}
	local := rsync.Endpoint{Path: "/x"}

	cases := []struct {
		name                string
		source, destination rsync.Endpoint
		wantHint            bool
	}{
		{"both remote", remote, remote, true},
		{"source only", remote, local, false},
		{"destination only", local, remote, false},
		{"both local", local, local, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			job := rsync.Job{Source: c.source, Destination: c.destination}
			got := rsyncRelayHint(job, theme) != ""
			if got != c.wantHint {
				t.Errorf("rsyncRelayHint non-empty = %v, want %v", got, c.wantHint)
			}
		})
	}
}

// TestRsyncRelayHintAlsoCatchesHandTypedRemoteSyntax pins that the
// hint isn't limited to connections this project itself resolved —
// typing a remote address straight into Source/Destination for a host
// never connected to via the Connect dialog at all is common on its
// own, and the relay warning should still fire for it.
func TestRsyncRelayHintAlsoCatchesHandTypedRemoteSyntax(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	typed := rsync.Endpoint{Path: "tester@example.com:/data"} // Host unset — never went through a live connection
	local := rsync.Endpoint{Path: "/data/with:colon/deep/inside"}

	if got := rsyncRelayHint(rsync.Job{Source: typed, Destination: typed}, theme); got == "" {
		t.Error("rsyncRelayHint = \"\", want the warning for two hand-typed remote addresses")
	}
	if got := rsyncRelayHint(rsync.Job{Source: local, Destination: typed}, theme); got != "" {
		t.Errorf("rsyncRelayHint = %q, want no warning — the local path's own colon comes after a \"/\"", got)
	}
}

func TestRenderRsyncPreviewShowsTheRelayHintForRemoteToRemote(t *testing.T) {
	r, _ := newTestRootForRsync(t)
	r.openRsync()
	r.rsyncSourceField.SetText("tester@a.example.com:/src")
	r.rsyncDestinationField.SetText("tester@b.example.com:/dst")

	r.renderRsyncPreview()

	got := r.rsyncHintView.GetText(true)
	if !strings.Contains(got, "relayed through this machine") {
		t.Errorf("hint text = %q, want the remote-to-remote relay warning", got)
	}
}

func TestRenderRsyncPreviewHintIsBlankForLocalToRemote(t *testing.T) {
	r, _ := newTestRootForRsync(t)
	r.openRsync()
	r.rsyncSourceField.SetText("/src")
	r.rsyncDestinationField.SetText("tester@b.example.com:/dst")

	r.renderRsyncPreview()

	if got := r.rsyncHintView.GetText(true); got != "" {
		t.Errorf("hint text = %q, want blank for a local source", got)
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

// TestToggleRsyncFlagCopyContentsAppendsTrailingSlashToSourceField pins
// the user's own explicit request: the live preview line already
// showed the trailing "/" this toggle means (via sourceArg), but the
// Source field itself stayed exactly as typed or defaulted, silently
// disagreeing with what the preview said was actually about to run.
func TestToggleRsyncFlagCopyContentsAppendsTrailingSlashToSourceField(t *testing.T) {
	r, dir := newTestRootForRsync(t)
	r.openRsync()
	r.rsyncSourceField.SetText(dir)

	r.toggleRsyncFlag(rsyncLabelCopyContents)

	if got, want := r.rsyncSourceField.GetText(), dir+"/"; got != want {
		t.Errorf("Source field = %q, want %q", got, want)
	}
}

// TestToggleRsyncFlagCopyContentsRemovesTrailingSlashWhenTurnedOff is
// the flip side: turning the toggle back off removes exactly the
// trailing "/" it added, restoring the field to what it showed before.
func TestToggleRsyncFlagCopyContentsRemovesTrailingSlashWhenTurnedOff(t *testing.T) {
	r, dir := newTestRootForRsync(t)
	r.openRsync()
	r.rsyncSourceField.SetText(dir)
	r.toggleRsyncFlag(rsyncLabelCopyContents) // on: dir -> dir + "/"

	r.toggleRsyncFlag(rsyncLabelCopyContents) // off again

	if got := r.rsyncSourceField.GetText(); got != dir {
		t.Errorf("Source field = %q, want %q (the trailing \"/\" removed again)", got, dir)
	}
}

// TestToggleRsyncFlagCopyContentsNeverDoublesAnExistingTrailingSlash
// pins that turning the toggle on when the field already ends with
// "/" (typed by hand, or already toggled on once) doesn't add a
// second one.
func TestToggleRsyncFlagCopyContentsNeverDoublesAnExistingTrailingSlash(t *testing.T) {
	r, dir := newTestRootForRsync(t)
	r.openRsync()
	r.rsyncSourceField.SetText(dir + "/")

	r.toggleRsyncFlag(rsyncLabelCopyContents)

	if got, want := r.rsyncSourceField.GetText(), dir+"/"; got != want {
		t.Errorf("Source field = %q, want %q (no doubled slash)", got, want)
	}
}

// TestToggleRsyncFlagCopyContentsIsANoOpOnAnEmptyField pins that there's
// no path to add or remove a slash from yet when the field is blank —
// toggling the flag itself still works (see
// TestToggleRsyncFlagFlipsStateAndRelabelsTheRow), only the field-text
// side effect is skipped.
func TestToggleRsyncFlagCopyContentsIsANoOpOnAnEmptyField(t *testing.T) {
	r, _ := newTestRootForRsync(t)
	r.openRsync()
	r.rsyncSourceField.SetText("")

	r.toggleRsyncFlag(rsyncLabelCopyContents)

	if got := r.rsyncSourceField.GetText(); got != "" {
		t.Errorf("Source field = %q, want it to stay empty", got)
	}
}

// TestToggleRsyncFlagCopyContentsKeepsThePortForARemoteSource pins the
// real regression this cosmetic field edit could otherwise cause: the
// trailing "/" it adds must not read as "the user edited the field" to
// rsyncFieldDefault.endpoint, which would silently drop the
// connection's own tracked port.
func TestToggleRsyncFlagCopyContentsKeepsThePortForARemoteSource(t *testing.T) {
	r, _ := newTestRootForRsync(t)
	client := &fakeRemoteClient{root: "/remote", entries: map[string][]fsops.Entry{
		"/remote": {{Name: "b.txt", Type: fsops.TypeFile}},
	}}
	if err := r.panel.connectRemote(client, remotefs.Connection{Host: "example.com", User: "tester", Port: 2222}); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}
	r.openRsync() // resetRsyncForm defaults Source from r.panel, e.g. "tester@example.com:/remote"

	r.toggleRsyncFlag(rsyncLabelCopyContents)

	if want := "tester@example.com:/remote/"; r.rsyncSourceField.GetText() != want {
		t.Fatalf("setup: Source field = %q, want %q", r.rsyncSourceField.GetText(), want)
	}
	job := r.currentRsyncJob()
	if job.Source.Host != "example.com" || job.Source.User != "tester" || job.Source.Port != 2222 {
		t.Errorf("Source = %+v, want Host=example.com User=tester Port=2222 (port lost after toggling Copy Contents)", job.Source)
	}
}

// TestToggleRsyncFlagCopyContentsSubstitutesTheParentDirectoryForAFileSource
// pins the user's own explicit request: opening the dialog on a single
// file, then checking "Copy the folder's contents in", obviously can't
// mean "the file's own contents" the way it does for a directory — it
// substitutes the file's own parent directory instead of appending a
// slash to the filename itself, which would just describe a directory
// that doesn't exist.
func TestToggleRsyncFlagCopyContentsSubstitutesTheParentDirectoryForAFileSource(t *testing.T) {
	r, dir := newTestRootForRsync(t)
	r.panel.focusRow(1) // off ".." onto a.txt, nothing checked
	r.openRsync()
	file := filepath.Join(dir, "a.txt")
	if got := r.rsyncSourceField.GetText(); got != file {
		t.Fatalf("setup: Source field = %q, want %q", got, file)
	}

	r.toggleRsyncFlag(rsyncLabelCopyContents)

	if got, want := r.rsyncSourceField.GetText(), dir+"/"; got != want {
		t.Errorf("Source field = %q, want the file's own parent directory %q", got, want)
	}
}

// TestToggleRsyncFlagCopyContentsRestoresTheOriginalFileWhenTurnedOffAgain
// is the flip side the user explicitly asked for: unchecking the box
// again brings back the exact file the dialog originally opened on —
// not just the substituted directory with its own trailing slash
// stripped, which would leave the whole directory selected instead of
// going back to the single file.
func TestToggleRsyncFlagCopyContentsRestoresTheOriginalFileWhenTurnedOffAgain(t *testing.T) {
	r, dir := newTestRootForRsync(t)
	r.panel.focusRow(1) // a.txt
	r.openRsync()
	file := filepath.Join(dir, "a.txt")
	r.toggleRsyncFlag(rsyncLabelCopyContents) // on: substitutes the parent directory

	r.toggleRsyncFlag(rsyncLabelCopyContents) // off again

	if got := r.rsyncSourceField.GetText(); got != file {
		t.Errorf("Source field = %q, want the original file %q restored", got, file)
	}
}

// TestToggleRsyncFlagCopyContentsFileSubstitutionSkipsAnEditedField
// pins that the file-to-parent-directory substitution only ever fires
// while the field still reads exactly the file default it came from —
// the same "never override something the user actually typed" guarantee
// the plain slash-toggle already gives (see
// TestToggleRsyncFlagCopyContentsNeverDoublesAnExistingTrailingSlash).
func TestToggleRsyncFlagCopyContentsFileSubstitutionSkipsAnEditedField(t *testing.T) {
	r, dir := newTestRootForRsync(t)
	r.panel.focusRow(1) // a.txt
	r.openRsync()
	edited := filepath.Join(dir, "other.txt")
	r.rsyncSourceField.SetText(edited) // edited away from the original file default

	r.toggleRsyncFlag(rsyncLabelCopyContents)

	if got, want := r.rsyncSourceField.GetText(), edited+"/"; got != want {
		t.Errorf("Source field = %q, want the plain slash-append fallback %q, not the original file's own parent directory", got, want)
	}
}

// TestToggleRsyncFlagCopyContentsFileSubstitutionKeepsThePortForARemoteFileSource
// mirrors TestToggleRsyncFlagCopyContentsKeepsThePortForARemoteSource
// for the file-specific substitution path: it must preserve a
// connection's own non-default port exactly the same way the plain
// directory case already does.
func TestToggleRsyncFlagCopyContentsFileSubstitutionKeepsThePortForARemoteFileSource(t *testing.T) {
	r, _ := newTestRootForRsync(t)
	client := &fakeRemoteClient{root: "/remote", entries: map[string][]fsops.Entry{
		"/remote": {{Name: "b.txt", Type: fsops.TypeFile}},
	}}
	if err := r.panel.connectRemote(client, remotefs.Connection{Host: "example.com", User: "tester", Port: 2222}); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}
	r.panel.focusRow(1) // b.txt, a plain file
	r.openRsync()
	if want := "tester@example.com:/remote/b.txt"; r.rsyncSourceField.GetText() != want {
		t.Fatalf("setup: Source field = %q, want %q", r.rsyncSourceField.GetText(), want)
	}

	r.toggleRsyncFlag(rsyncLabelCopyContents)

	if want := "tester@example.com:/remote/"; r.rsyncSourceField.GetText() != want {
		t.Fatalf("setup: Source field = %q, want %q", r.rsyncSourceField.GetText(), want)
	}
	job := r.currentRsyncJob()
	if job.Source.Host != "example.com" || job.Source.User != "tester" || job.Source.Port != 2222 {
		t.Errorf("Source = %+v, want Host=example.com User=tester Port=2222 preserved across the file-to-parent-directory substitution", job.Source)
	}
	if job.Source.Path != "/remote" {
		t.Errorf("Source.Path = %q, want the file's own parent directory %q", job.Source.Path, "/remote")
	}
}

// TestRsyncTabPickerLabelNumbersAndShowsTheEndpointText pins
// rsyncTabPickerLabel's own shape: 1-based, matching
// tabSwitcherRowLabel's own numbering, and the exact endpoint text
// passed in — untouched for anything short enough not to need
// shortenPathLeft's own truncation.
func TestRsyncTabPickerLabelNumbersAndShowsTheEndpointText(t *testing.T) {
	if got, want := rsyncTabPickerLabel(0, "/home/jens"), " 1  /home/jens"; got != want {
		t.Errorf("rsyncTabPickerLabel(0, ...) = %q, want %q", got, want)
	}
	if got, want := rsyncTabPickerLabel(9, "tester@example.com:/remote"), "10  tester@example.com:/remote"; got != want {
		t.Errorf("rsyncTabPickerLabel(9, ...) = %q, want %q", got, want)
	}
}

// TestOpenRsyncTabPickerListsEveryOpenTab pins the picker's own basic
// contract: one row per open tab, opened as its own overlay layered
// over the Rsync dialog.
func TestOpenRsyncTabPickerListsEveryOpenTab(t *testing.T) {
	r, _ := newTestRootForRsync(t)
	r.newTabHere()
	r.openRsync()

	r.openRsyncTabPicker(r.rsyncSourceField, &r.rsyncSourceDefault)

	if r.activePage != pickerPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, pickerPage)
	}
	if got, want := r.picker.GetItemCount(), len(r.tabs); got != want {
		t.Errorf("picker item count = %d, want one per open tab (%d)", got, want)
	}
}

// TestOpenRsyncTabPickerPickingATabFillsTheFieldAndItsOwnDefault pins
// the actual point of this feature: choosing a tab writes exactly what
// opening Rsync fresh from that tab would have defaulted to (see
// rsyncFieldDefaultFor), and updates the tracked default too, not just
// the field's own visible text — currentRsyncJob reads the former, not
// the latter, for anything beyond a plain local path.
func TestOpenRsyncTabPickerPickingATabFillsTheFieldAndItsOwnDefault(t *testing.T) {
	r, dir := newTestRootForRsync(t)
	otherDir := t.TempDir()
	r.newTabHere()
	if err := r.panel.load(otherDir); err != nil {
		t.Fatal(err)
	}
	r.openRsync()
	if r.rsyncSourceField.GetText() != otherDir {
		t.Fatalf("setup: Source field = %q, want the active tab's own path %q", r.rsyncSourceField.GetText(), otherDir)
	}

	r.openRsyncTabPicker(r.rsyncSourceField, &r.rsyncSourceDefault)
	r.picker.SetCurrentItem(0) // tab 0 — the original tab, showing dir
	r.picker.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.activePage != rsyncPage {
		t.Errorf("activePage = %q, want back to %q once a tab is picked", r.activePage, rsyncPage)
	}
	if r.rsyncSourceField.GetText() != dir {
		t.Errorf("Source field = %q, want the picked tab's own path %q", r.rsyncSourceField.GetText(), dir)
	}
	if r.rsyncSourceDefault.text != dir {
		t.Errorf("rsyncSourceDefault.text = %q, want %q", r.rsyncSourceDefault.text, dir)
	}
}

// TestOpenRsyncTabPickerPickingARemoteTabTracksItsConnection pins the
// same "no need to type Host/User a second time" guarantee
// defaultRsyncSource already gives an opening dialog, extended to a
// tab picked afterward instead: currentRsyncJob's own Source ends up
// with the picked tab's real Host/User, not just a "user@host:path"
// string with no connection behind it.
func TestOpenRsyncTabPickerPickingARemoteTabTracksItsConnection(t *testing.T) {
	r, _ := newTestRootForRsync(t)
	r.newTabHere()
	client := &fakeRemoteClient{root: "/remote", entries: map[string][]fsops.Entry{
		"/remote": {{Name: "b.txt", Type: fsops.TypeFile}},
	}}
	if err := r.panel.connectRemote(client, remotefs.Connection{Host: "example.com", User: "tester"}); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}
	r.switchToTab(0) // back to the original, local tab
	r.openRsync()

	r.openRsyncTabPicker(r.rsyncDestinationField, &r.rsyncDestinationDefault)
	r.picker.SetCurrentItem(1) // tab 1 — the remote one
	r.picker.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if want := "tester@example.com:/remote"; r.rsyncDestinationField.GetText() != want {
		t.Fatalf("Destination field = %q, want %q", r.rsyncDestinationField.GetText(), want)
	}
	job := r.currentRsyncJob()
	if job.Destination.Host != "example.com" || job.Destination.User != "tester" {
		t.Errorf("Destination = %+v, want Host=example.com User=tester tracked from the picked tab's own connection", job.Destination)
	}
}
