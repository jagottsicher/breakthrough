package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rivo/tview"
)

// isolatePasteIO overrides fsCopy/fsMove for the duration of t, still
// calling through to the real fsops implementations, but signaling
// done once each call actually returns — the one way a test can
// deterministically wait for pasteWalk's own background goroutine to
// have done its real work on a real filesystem, since nothing here
// drains Application.QueueUpdateDraw (see applyPasteOneResult's own
// doc comment for the synchronous alternative that's the better fit
// for anything gated behind it — clipboard-clearing, the Details
// refresh, the panel reload, an error overlay). This is only for a
// test that specifically wants to pin the real, on-disk effect of an
// actual startPaste/pasteClipboard call end to end.
func isolatePasteIO(t *testing.T) <-chan struct{} {
	t.Helper()
	done := make(chan struct{}, 64)
	origCopy, origMove := fsCopy, fsMove
	fsCopy = func(src, dst string, force bool, onFile func(string)) error {
		err := origCopy(src, dst, force, onFile)
		done <- struct{}{}
		return err
	}
	fsMove = func(src, dst string, force bool, onFile func(string)) error {
		err := origMove(src, dst, force, onFile)
		done <- struct{}{}
		return err
	}
	t.Cleanup(func() { fsCopy, fsMove = origCopy, origMove })
	return done
}

// waitPasteIO drains n signals from done, failing t if they don't all
// arrive within a generous timeout — pasteWalk's own background
// goroutine really does run concurrently with the test, so a bare
// receive with no timeout at all would hang the whole suite if
// something regressed instead of failing it.
func waitPasteIO(t *testing.T, done <-chan struct{}, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for paste I/O call %d of %d", i+1, n)
		}
	}
}

// TestCopyToClipboardThenPasteLeavesSourceInPlace pins Copy's own real,
// end-to-end effect through the async engine (see isolatePasteIO): the
// file lands at dst, and — unlike Cut — src is left exactly where it
// was.
func TestCopyToClipboardThenPasteLeavesSourceInPlace(t *testing.T) {
	srcDir := fixtureDir(t)
	dstDir := t.TempDir()

	r, err := NewRoot(tview.NewApplication(), srcDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.target = filepath.Join(srcDir, "apple.txt")
	r.copyToClipboard()
	if err := r.panel.load(dstDir); err != nil {
		t.Fatalf("load(dstDir): %v", err)
	}

	done := isolatePasteIO(t)
	r.pasteClipboard()
	waitPasteIO(t, done, 1)

	if _, err := os.Stat(filepath.Join(dstDir, "apple.txt")); err != nil {
		t.Errorf("pasted file missing in dst: %v", err)
	}
	if _, err := os.Stat(filepath.Join(srcDir, "apple.txt")); err != nil {
		t.Errorf("Copy should leave the source file in place: %v", err)
	}
}

// TestCutToClipboardThenPasteRemovesSourceOnDisk is Copy's counterpart
// for Cut's own real, on-disk effect: the source must actually be gone
// afterwards. Clipboard-clearing (also part of Cut+Paste's contract) is
// pinned separately in TestApplyPasteOneResultClearsClipboardAfterCut —
// that part only ever happens inside applyPasteOneResult, which this
// real, async round trip has no way to wait for (see
// applyPasteOneResult's own doc comment).
func TestCutToClipboardThenPasteRemovesSourceOnDisk(t *testing.T) {
	srcDir := fixtureDir(t)
	dstDir := t.TempDir()

	r, err := NewRoot(tview.NewApplication(), srcDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.target = filepath.Join(srcDir, "banana.txt")
	r.cutToClipboard()
	if err := r.panel.load(dstDir); err != nil {
		t.Fatalf("load(dstDir): %v", err)
	}

	done := isolatePasteIO(t)
	r.pasteClipboard()
	waitPasteIO(t, done, 1)

	if _, err := os.Stat(filepath.Join(dstDir, "banana.txt")); err != nil {
		t.Errorf("pasted file missing in dst: %v", err)
	}
	if _, err := os.Stat(filepath.Join(srcDir, "banana.txt")); !os.IsNotExist(err) {
		t.Errorf("Cut should remove the source file, stat err = %v", err)
	}
}

// newPasteTestJob is a minimal, already-"in flight" pasteJob for a test
// that wants to call applyPasteOneResult/pasteConflictFound/
// resolveConflictAsync directly — bypassing startPaste/pasteWalk's own
// background goroutine and the QueueUpdateDraw hop neither of which
// ever completes in a test lacking a running Application.Run() loop
// (see applyPasteOneResult's own doc comment). Also sets r.pasteJob to
// the new job, the same as startPaste itself would: finishPasteJob's
// own "already superseded" guard checks r.pasteJob against the job it
// was called with, so leaving it nil here would make every completion
// silently no-op instead of exercising it.
func newPasteTestJob(r *Root, cut bool, destDir string, total int) *pasteJob {
	ctx, cancel := context.WithCancel(context.Background())
	job := &pasteJob{ctx: ctx, cancel: cancel, cut: cut, destDir: destDir, total: total, remaining: total}
	r.pasteJob = job
	return job
}

// TestFinishPasteJobReloadsEveryOpenTabShowingDestDir pins the user's
// own explicit request for an auto-reload of the destination once a
// paste completes: not just r.panel, but every open tab currently
// showing destDir (see finishPasteJob's own doc comment) — a directory
// open in two tabs at once must show the freshly pasted file in both,
// while a third tab showing something else entirely is left alone.
func TestFinishPasteJobReloadsEveryOpenTabShowingDestDir(t *testing.T) {
	srcDir := fixtureDir(t)
	destDir := t.TempDir()
	otherDir := t.TempDir()

	r, err := NewRoot(tview.NewApplication(), destDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.newTab(destDir) // a second tab on the very same destination
	r.newTab(otherDir)
	if len(r.tabs) != 3 {
		t.Fatalf("setup: want 3 tabs, got %d", len(r.tabs))
	}
	tabA, tabB, tabC := r.tabs[0], r.tabs[1], r.tabs[2]

	// Lands in destDir only after tabA/tabB already loaded it — the
	// same "not there yet at load time" gap a real background copy
	// leaves for finishPasteJob's own reload to close.
	src := filepath.Join(srcDir, "apple.txt")
	newFile := filepath.Join(destDir, "apple.txt")
	if err := os.WriteFile(newFile, []byte("pasted"), 0o644); err != nil {
		t.Fatal(err)
	}

	job := newPasteTestJob(r, false, destDir, 1)
	r.applyPasteOneResult(job, src, newFile, nil)

	for name, p := range map[string]*Panel{"tab A": tabA, "tab B": tabB} {
		if _, ok := rowForPath(p, newFile); !ok {
			t.Errorf("%s (showing destDir): apple.txt not visible after paste — not reloaded", name)
		}
	}
	if _, ok := rowForPath(tabC, newFile); ok {
		t.Error("tab C (a different directory) should never show destDir's own file")
	}
}

// TestApplyPasteOneResultClearsClipboardAfterCut pins Cut+Paste's own
// clipboard-clearing — logic that only ever runs inside
// applyPasteOneResult (via pasteItemDone/finishPasteJob), so it's
// exercised directly here rather than through a real async paste (see
// TestCutToClipboardThenPasteRemovesSourceOnDisk for that half).
func TestApplyPasteOneResultClearsClipboardAfterCut(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	src := filepath.Join(dir, "banana.txt")
	r.clipboard = []string{src}
	r.clipboardCut = true

	job := newPasteTestJob(r, true, dir, 1)
	r.applyPasteOneResult(job, src, filepath.Join(dir, "moved.txt"), nil)

	if r.clipboard != nil {
		t.Errorf("clipboard should be cleared after a successful cut-paste, got %v", r.clipboard)
	}
}

// TestApplyPasteOneResultClearsClipboardHighlightAndCountsAfterCut goes
// through the real cutToClipboard path (unlike
// TestApplyPasteOneResultClearsClipboardAfterCut above, which pokes
// r.clipboard directly) so clipboardDirs/Files and the panel's own row
// tint are actually primed — then pins that a clean Cut+Paste clears
// all three together (see finishPasteJob's own setClipboard(nil,
// false) call), not just r.clipboard itself.
func TestApplyPasteOneResultClearsClipboardHighlightAndCountsAfterCut(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	src := filepath.Join(dir, "banana.txt")
	r.panel.toggleCheckbox(4) // banana.txt
	r.cutToClipboard()
	if r.clipboardFiles != 1 {
		t.Fatalf("setup: clipboardFiles = %d, want 1", r.clipboardFiles)
	}
	row, ok := rowForPath(r.panel, src)
	if !ok {
		t.Fatal("setup: banana.txt row not found")
	}
	if _, tinted := cellBackground(r.panel.table.GetCell(row, colName)); !tinted {
		t.Fatal("setup: banana.txt should be tinted before Paste completes")
	}

	job := newPasteTestJob(r, true, dir, 1)
	r.applyPasteOneResult(job, src, filepath.Join(dir, "moved.txt"), nil)

	if r.clipboardDirs != 0 || r.clipboardFiles != 0 {
		t.Errorf("clipboardDirs/Files = %d/%d, want 0/0 once the clean cut-paste lands", r.clipboardDirs, r.clipboardFiles)
	}
	if _, tinted := cellBackground(r.panel.table.GetCell(row, colName)); tinted {
		t.Error("banana.txt's own row (now stale — the file itself moved) is still tinted after the clean cut-paste")
	}
}

// TestApplyPasteOneResultKeepsClipboardOnError mirrors the above for the
// failure path: a stray second Paste after a real error should still
// have something to retry.
func TestApplyPasteOneResultKeepsClipboardOnError(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	src := filepath.Join(dir, "banana.txt")
	r.clipboard = []string{src}
	r.clipboardCut = true

	job := newPasteTestJob(r, true, dir, 1)
	r.applyPasteOneResult(job, src, filepath.Join(dir, "moved.txt"), fmt.Errorf("boom"))

	if len(r.clipboard) != 1 {
		t.Errorf("clipboard should survive a failed cut-paste, got %v", r.clipboard)
	}
	if r.activePage != errorPage {
		t.Error("a genuine paste failure should open the error overlay")
	}
}

// TestPasteMoveRefreshesDetailsShowingSameFile pins the user's own
// explicit request extended to Cut+Paste: a moved file is the same real
// entry under a new path, exactly like a rename — Details needs to keep
// following it (see refreshDetailsIfShowing's own doc comment). Runs
// through applyPasteOneResult directly (see its own doc comment) since
// this is exactly the QueueUpdateDraw-gated effect a real async round
// trip has no way to wait for in a test.
func TestPasteMoveRefreshesDetailsShowingSameFile(t *testing.T) {
	dir := fixtureDir(t)
	otherDir := t.TempDir()

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)

	src := filepath.Join(dir, "banana.txt")
	r.panel.focusRow(4) // banana.txt — the panel itself stays in dir throughout
	r.showDetailsSidebar()
	if r.detailsTarget != src {
		t.Fatalf("setup: detailsTarget = %q, want %q", r.detailsTarget, src)
	}

	dst := filepath.Join(otherDir, "banana.txt")
	job := newPasteTestJob(r, true, otherDir, 1)
	r.applyPasteOneResult(job, src, dst, nil)

	if r.detailsTarget != dst {
		t.Errorf("detailsTarget after Cut+Paste = %q, want %q", r.detailsTarget, dst)
	}
}

// TestPasteCopyDoesNotDisturbDetails is the copy-side counterpart: the
// source is untouched by a copy, so Details showing it must not be
// redirected anywhere — unlike Move, there's no "same entry, new path"
// to follow. Deliberately checks that recordPasteSuccess never even
// tries — a wrong dst passed to refreshDetailsIfShowing here would
// prove this test was actually exercising something, unlike a copy of
// TestPasteMoveRefreshesDetailsShowingSameFile that just forgot to flip
// cut to false and would pass "successfully" either way.
func TestPasteCopyDoesNotDisturbDetails(t *testing.T) {
	dir := fixtureDir(t)
	otherDir := t.TempDir()

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)

	src := filepath.Join(dir, "banana.txt")
	r.panel.focusRow(4) // banana.txt
	r.showDetailsSidebar()
	if r.detailsTarget != src {
		t.Fatalf("setup: detailsTarget = %q, want %q", r.detailsTarget, src)
	}

	job := newPasteTestJob(r, false, otherDir, 1)
	r.applyPasteOneResult(job, src, filepath.Join(otherDir, "banana.txt"), nil)

	if r.detailsTarget != src {
		t.Errorf("detailsTarget after Copy+Paste = %q, want unchanged %q", r.detailsTarget, src)
	}
}

// newPasteTestConflict builds a pasteConflict from real stat info for
// src/dst — every conflict-resolution test needs real os.FileInfo (see
// resolveOverwriteIfNewer/resolveOverwriteIfSourceNotEmpty, both of
// which read ModTime/Size straight off it), not a zero-value stand-in.
func newPasteTestConflict(t *testing.T, src, dst string) pasteConflict {
	t.Helper()
	srcInfo, err := os.Lstat(src)
	if err != nil {
		t.Fatalf("Lstat(src): %v", err)
	}
	dstInfo, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("Lstat(dst): %v", err)
	}
	return pasteConflict{src: src, dst: dst, srcInfo: srcInfo, dstInfo: dstInfo}
}

// TestPasteConflictOpensDialogInsteadOfErroring pins the whole point of
// this feature: an existing dst entry no longer silently refuses with
// an error overlay (the old behavior) — it opens r.pasteConflictDialog
// and leaves the existing file untouched until a decision is made.
// Calls pasteConflictFound directly (see applyPasteOneResult's own doc
// comment on why: pasteWalk only ever reaches this through
// QueueUpdateDraw, which never fires in a test lacking a running
// Application.Run() loop) to simulate "pasteWalk just found this
// conflict" without needing one.
func TestPasteConflictOpensDialogInsteadOfErroring(t *testing.T) {
	srcDir := fixtureDir(t)
	dstDir := t.TempDir()
	dst := filepath.Join(dstDir, "apple.txt")
	if err := os.WriteFile(dst, []byte("existing"), 0o640); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(srcDir, "apple.txt")

	r, err := NewRoot(tview.NewApplication(), srcDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)

	job := newPasteTestJob(r, false, dstDir, 1)
	r.pasteConflictFound(job, newPasteTestConflict(t, src, dst))

	if r.activePage != pasteConflictPage {
		t.Errorf("activePage = %q, want %q — a conflict should open the dialog, not error out", r.activePage, pasteConflictPage)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "existing" {
		t.Errorf("existing dst file should be untouched until a decision is made, got %q, %v", got, err)
	}
}

// setUpPasteConflict is the shared setup every resolution test below
// starts from: a real conflict, already shown in r.pasteConflictDialog
// (via pasteConflictFound), ready for a resolution choice.
func setUpPasteConflict(t *testing.T, srcContent string) (r *Root, job *pasteJob, src, dst string) {
	t.Helper()
	srcDir := fixtureDir(t)
	dstDir := t.TempDir()
	src = filepath.Join(srcDir, "apple.txt")
	dst = filepath.Join(dstDir, "apple.txt")
	if srcContent != "" {
		if err := os.WriteFile(src, []byte(srcContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(dst, []byte("existing"), 0o640); err != nil {
		t.Fatal(err)
	}

	var err error
	r, err = NewRoot(tview.NewApplication(), srcDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)

	job = newPasteTestJob(r, false, dstDir, 1)
	r.pasteConflictFound(job, newPasteTestConflict(t, src, dst))
	if job.current == nil {
		t.Fatal("setup: conflict should be showing")
	}
	return r, job, src, dst
}

// TestChooseConflictResolutionOverwriteRunsRealCopy pins "Overwrite":
// the real fsCopy call goes through with force=true (see isolatePasteIO)
// and the existing dst content is replaced.
func TestChooseConflictResolutionOverwriteRunsRealCopy(t *testing.T) {
	r, _, _, dst := setUpPasteConflict(t, "new content")

	done := isolatePasteIO(t)
	r.chooseConflictResolution(resolveOverwrite, false)
	waitPasteIO(t, done, 1)

	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "new content" {
		t.Errorf("dst after Overwrite = %q, %v, want %q", got, err, "new content")
	}
	if r.activePage == pasteConflictPage {
		t.Error("the dialog should have closed")
	}
}

// TestChooseConflictResolutionSkipLeavesDestUntouched pins "Skip": no
// I/O at all, the existing dst content survives, and the item's own
// outcome is still recorded as final (job.remaining reaches 0).
func TestChooseConflictResolutionSkipLeavesDestUntouched(t *testing.T) {
	r, job, _, dst := setUpPasteConflict(t, "new content")

	r.chooseConflictResolution(resolveSkip, false)

	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "existing" {
		t.Errorf("dst after Skip = %q, %v, want unchanged %q", got, err, "existing")
	}
	if job.remaining != 0 {
		t.Errorf("job.remaining = %d, want 0 — Skip still finalizes the item", job.remaining)
	}
	if r.pasteJob != nil {
		t.Error("the job should be finished (nothing left pending, no more items) and cleared")
	}
}

// TestChooseConflictResolutionOverwriteAllAppliesToQueuedConflict pins
// the whole point of "for all": a second conflict already queued behind
// the first (see pasteConflictFound) resolves immediately once "for
// all" is chosen for the first one, with no dialog of its own.
func TestChooseConflictResolutionOverwriteAllAppliesToQueuedConflict(t *testing.T) {
	r, job, _, dst1 := setUpPasteConflict(t, "new content 1")
	// A second, independent conflict, queued behind the first.
	src2 := filepath.Join(t.TempDir(), "second.txt")
	if err := os.WriteFile(src2, []byte("new content 2"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst2 := filepath.Join(job.destDir, "second.txt")
	if err := os.WriteFile(dst2, []byte("existing 2"), 0o640); err != nil {
		t.Fatal(err)
	}
	job.total = 2
	job.remaining = 2
	r.pasteConflictFound(job, newPasteTestConflict(t, src2, dst2))
	if len(job.pending) != 1 {
		t.Fatalf("setup: want the second conflict queued, got %d pending", len(job.pending))
	}

	done := isolatePasteIO(t)
	r.chooseConflictResolution(resolveOverwrite, true)
	waitPasteIO(t, done, 2) // both conflicts resolve to a real overwrite

	if job.forAll == nil || *job.forAll != resolveOverwrite {
		t.Fatal("forAll should now be resolveOverwrite")
	}
	if got, _ := os.ReadFile(dst1); string(got) != "new content 1" {
		t.Errorf("dst1 = %q, want %q", got, "new content 1")
	}
	if got, _ := os.ReadFile(dst2); string(got) != "new content 2" {
		t.Errorf("dst2 = %q, want %q", got, "new content 2")
	}
	if len(job.pending) != 0 || job.current != nil {
		t.Error("no conflict should still be pending or showing onceForAll cleared the queue")
	}
}

// TestChooseConflictResolutionSkipAllAppliesToFutureConflicts pins
// forAll's other half: a conflict pasteWalk finds *after* "skip all" was
// already chosen never opens a dialog at all — it resolves against the
// policy immediately (see pasteConflictFound's own forAll check).
func TestChooseConflictResolutionSkipAllAppliesToFutureConflicts(t *testing.T) {
	r, job, _, dst1 := setUpPasteConflict(t, "")
	before, _ := os.ReadFile(dst1)

	r.chooseConflictResolution(resolveSkip, true)
	if job.forAll == nil || *job.forAll != resolveSkip {
		t.Fatal("forAll should now be resolveSkip")
	}

	// A brand new conflict, discovered later — same job, forAll already set.
	src2 := filepath.Join(t.TempDir(), "second.txt")
	if err := os.WriteFile(src2, []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst2 := filepath.Join(t.TempDir(), "existing2.txt")
	if err := os.WriteFile(dst2, []byte("existing 2"), 0o640); err != nil {
		t.Fatal(err)
	}
	job.total++
	job.remaining++
	r.pasteConflictFound(job, newPasteTestConflict(t, src2, dst2))

	if r.activePage == pasteConflictPage {
		t.Error("a conflict found after \"skip all\" was chosen should never open its own dialog")
	}
	if got, _ := os.ReadFile(dst2); string(got) != "existing 2" {
		t.Errorf("dst2 = %q, want untouched %q — skip all should have applied", got, "existing 2")
	}
	if got, _ := os.ReadFile(dst1); string(got) != string(before) {
		t.Errorf("dst1 changed unexpectedly: %q", got)
	}
}

// TestResolveOverwriteIfNewerOverwritesWhenSourceIsNewer and
// TestResolveOverwriteIfNewerSkipsWhenSourceIsNotNewer pin both branches
// of "Overwrite all if source is newer" — decided synchronously from
// the stat info already gathered when the conflict was found (see
// resolveConflictAsync), no further disk access needed either way.
func TestResolveOverwriteIfNewerOverwritesWhenSourceIsNewer(t *testing.T) {
	dstDir := t.TempDir()
	dst := filepath.Join(dstDir, "f.txt")
	if err := os.WriteFile(dst, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(dst, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "f.txt")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil { // written just now — newer than dst
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), srcDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	job := newPasteTestJob(r, false, dstDir, 1)
	c := newPasteTestConflict(t, src, dst)

	done := isolatePasteIO(t)
	r.resolveConflictAsync(job, c, resolveOverwriteIfNewer)
	waitPasteIO(t, done, 1)

	if got, _ := os.ReadFile(dst); string(got) != "new" {
		t.Errorf("dst = %q, want overwritten with %q — source is newer", got, "new")
	}
}

func TestResolveOverwriteIfNewerSkipsWhenSourceIsNotNewer(t *testing.T) {
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "f.txt")
	if err := os.WriteFile(src, []byte("old-src"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(src, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	dstDir := t.TempDir()
	dst := filepath.Join(dstDir, "f.txt")
	if err := os.WriteFile(dst, []byte("existing"), 0o640); err != nil { // written just now — newer than src
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), srcDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	job := newPasteTestJob(r, false, dstDir, 1)
	c := newPasteTestConflict(t, src, dst)

	r.resolveConflictAsync(job, c, resolveOverwriteIfNewer)

	if got, _ := os.ReadFile(dst); string(got) != "existing" {
		t.Errorf("dst = %q, want untouched %q — source is not newer", got, "existing")
	}
	if job.remaining != 0 {
		t.Errorf("job.remaining = %d, want 0 — a skip still finalizes the item", job.remaining)
	}
}

// TestResolveOverwriteIfSourceNotEmptySkipsEmptySource and
// TestResolveOverwriteIfSourceNotEmptyOverwritesNonEmptySource pin both
// branches of "Overwrite all if source is not empty".
func TestResolveOverwriteIfSourceNotEmptySkipsEmptySource(t *testing.T) {
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "f.txt") // empty — os.WriteFile with nil content
	if err := os.WriteFile(src, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	dstDir := t.TempDir()
	dst := filepath.Join(dstDir, "f.txt")
	if err := os.WriteFile(dst, []byte("existing"), 0o640); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), srcDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	job := newPasteTestJob(r, false, dstDir, 1)
	c := newPasteTestConflict(t, src, dst)

	r.resolveConflictAsync(job, c, resolveOverwriteIfSourceNotEmpty)

	if got, _ := os.ReadFile(dst); string(got) != "existing" {
		t.Errorf("dst = %q, want untouched %q — source is empty", got, "existing")
	}
}

func TestResolveOverwriteIfSourceNotEmptyOverwritesNonEmptySource(t *testing.T) {
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "f.txt")
	if err := os.WriteFile(src, []byte("real content"), 0o644); err != nil {
		t.Fatal(err)
	}
	dstDir := t.TempDir()
	dst := filepath.Join(dstDir, "f.txt")
	if err := os.WriteFile(dst, []byte("existing"), 0o640); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), srcDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	job := newPasteTestJob(r, false, dstDir, 1)
	c := newPasteTestConflict(t, src, dst)

	done := isolatePasteIO(t)
	r.resolveConflictAsync(job, c, resolveOverwriteIfSourceNotEmpty)
	waitPasteIO(t, done, 1)

	if got, _ := os.ReadFile(dst); string(got) != "real content" {
		t.Errorf("dst = %q, want overwritten with %q — source is not empty", got, "real content")
	}
}

// TestPasteConflictFoundQueuesBehindAnOpenDialog pins the user's own
// explicit request: a second conflict discovered while the first's own
// dialog is already open queues behind it instead of stacking a second
// dialog on top, and the open dialog's own message grows to say so.
func TestPasteConflictFoundQueuesBehindAnOpenDialog(t *testing.T) {
	r, job, _, _ := setUpPasteConflict(t, "")

	src2 := filepath.Join(t.TempDir(), "second.txt")
	if err := os.WriteFile(src2, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	dst2 := filepath.Join(t.TempDir(), "existing2.txt")
	if err := os.WriteFile(dst2, []byte("existing 2"), 0o640); err != nil {
		t.Fatal(err)
	}
	r.pasteConflictFound(job, newPasteTestConflict(t, src2, dst2))

	if len(job.pending) != 1 {
		t.Fatalf("pending = %d, want the second conflict queued behind the first", len(job.pending))
	}
	msg, _ := r.pasteConflictDialog.GetItemText(pasteConflictMessageItem)
	if !strings.Contains(msg, "1 more waiting") {
		t.Errorf("dialog message = %q, want it to mention the queued conflict", msg)
	}
}

// TestPasteConflictDialogResizesWhenMessageGrows pins a real bug found in
// live testing: the dialog's own box is only ever sized once, when it
// first opens, for whatever its message says at that exact moment — a
// second conflict queuing up behind it (see
// TestPasteConflictFoundQueuesBehindAnOpenDialog) lengthens that message
// with "(N more waiting)", but without also resizing the box, a List
// widget silently clips text past its own fixed width instead of ever
// showing it, no matter how long it's given. Checks the actual on-screen
// rect, not just the stored item text TestPasteConflictFoundQueuesBehindAnOpenDialog
// already pins — that one alone would have passed even with this bug
// still in place.
func TestPasteConflictDialogResizesWhenMessageGrows(t *testing.T) {
	r, job, _, _ := setUpPasteConflict(t, "")
	_, _, widthBefore, _ := r.pasteConflictDialog.GetRect()

	src2 := filepath.Join(t.TempDir(), "second.txt")
	if err := os.WriteFile(src2, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	dst2 := filepath.Join(t.TempDir(), "existing2.txt")
	if err := os.WriteFile(dst2, []byte("existing 2"), 0o640); err != nil {
		t.Fatal(err)
	}
	r.pasteConflictFound(job, newPasteTestConflict(t, src2, dst2))

	wantWidth, _ := listSize(r.pasteConflictDialog)
	_, _, gotWidth, _ := r.pasteConflictDialog.GetRect()
	if gotWidth != wantWidth {
		t.Errorf("dialog width = %d, want %d (resized to fit the now-longer message)", gotWidth, wantWidth)
	}
	if gotWidth <= widthBefore {
		t.Errorf("dialog width should have grown past its original %d to fit \"(1 more waiting)\", got %d", widthBefore, gotWidth)
	}
}

// TestAdvancePasteConflictsRefreshesCountForReQueuedConflict pins the
// other half of the same class of bug: answering the first of three
// already-queued conflicts shows the second immediately (see
// TestAdvancePasteConflictsShowsNextQueuedConflict) — but
// advancePasteConflicts's own loop re-queues the third one *after*
// already showing the second, which left the second's dialog stuck
// saying nothing was waiting behind it, even though one genuinely was.
func TestAdvancePasteConflictsRefreshesCountForReQueuedConflict(t *testing.T) {
	r, job, _, _ := setUpPasteConflict(t, "")
	for _, name := range []string{"second.txt", "third.txt"} {
		src := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(src, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(t.TempDir(), "existing-"+name)
		if err := os.WriteFile(dst, []byte("existing"), 0o640); err != nil {
			t.Fatal(err)
		}
		job.total++
		job.remaining++
		r.pasteConflictFound(job, newPasteTestConflict(t, src, dst))
	}

	r.chooseConflictResolution(resolveSkip, false) // answers the first of three

	if len(job.pending) != 1 {
		t.Fatalf("pending = %d, want the third conflict still queued behind the second", len(job.pending))
	}
	msg, _ := r.pasteConflictDialog.GetItemText(pasteConflictMessageItem)
	if !strings.Contains(msg, "1 more waiting") {
		t.Errorf("dialog message = %q, want it to mention the third conflict still waiting", msg)
	}
}

// TestAdvancePasteConflictsShowsNextQueuedConflict pins what happens
// right after answering the first of two conflicts without a "for all":
// the second, queued one gets its own dialog next, rather than the
// whole job just ending with one conflict silently dropped.
func TestAdvancePasteConflictsShowsNextQueuedConflict(t *testing.T) {
	r, job, _, _ := setUpPasteConflict(t, "")
	src2 := filepath.Join(t.TempDir(), "second.txt")
	if err := os.WriteFile(src2, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	dst2 := filepath.Join(t.TempDir(), "existing2.txt")
	if err := os.WriteFile(dst2, []byte("existing 2"), 0o640); err != nil {
		t.Fatal(err)
	}
	job.total = 2
	job.remaining = 2
	r.pasteConflictFound(job, newPasteTestConflict(t, src2, dst2))

	r.chooseConflictResolution(resolveSkip, false) // answers the first — not "for all"

	if r.activePage != pasteConflictPage {
		t.Fatal("the second, queued conflict should now be showing its own dialog")
	}
	if job.current == nil || job.current.dst != dst2 {
		t.Errorf("job.current = %+v, want it to be the second conflict (%q)", job.current, dst2)
	}
	if len(job.pending) != 0 {
		t.Errorf("pending = %d, want 0 once the only queued conflict is now showing", len(job.pending))
	}
}

// TestPasteSummaryError pins pasteSummaryError's own two shapes: a
// TestPasteOneSerializesRealIOAcrossConcurrentItems pins job.ioMu's own
// whole point: however many goroutines pasteWalk/resolveConflictAsync
// start at once, only one of them is ever actually inside fsCopy/fsMove
// at a time — the property that makes currentFile a well-defined single
// answer instead of however many concurrent copies used to race over
// it. Fires several pasteOne calls concurrently against a fake fsCopy
// that fails the test the moment it finds itself entered while another
// call is still inside it, with a short sleep in the middle to give a
// real race an actual window to land in if the lock isn't doing its
// job. Signals its own "done" channel from inside the fake fsCopy, not
// by waiting for pasteOne itself to return — pasteOne's own
// QueueUpdateDraw hand-off at the end blocks forever without a live
// Application.Run() loop (see isolatePasteIO's own doc comment for the
// same reasoning), so every one of these goroutines is left stuck
// there once its own I/O is done, exactly as isolatePasteIO's own
// callers already tolerate elsewhere in this file.
func TestPasteOneSerializesRealIOAcrossConcurrentItems(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	job := newPasteTestJob(r, false, dir, 5)

	origCopy := fsCopy
	t.Cleanup(func() { fsCopy = origCopy })
	var inFlight atomic.Int32
	done := make(chan struct{}, 5)
	fsCopy = func(src, dst string, force bool, onFile func(string)) error {
		if inFlight.Add(1) != 1 {
			t.Errorf("fsCopy entered while another call was already inside it — job.ioMu isn't serializing real I/O")
		}
		time.Sleep(5 * time.Millisecond) // a real window for a race to actually land in
		inFlight.Add(-1)
		done <- struct{}{}
		return nil
	}

	for i := 0; i < 5; i++ {
		i := i
		go r.pasteOne(job, filepath.Join(dir, fmt.Sprintf("item-%d.txt", i)), filepath.Join(dir, fmt.Sprintf("out-%d.txt", i)), false)
	}
	waitPasteIO(t, done, 5)
}

// TestPasteSummaryError pins pasteSummaryError's own two shapes: a
// single failure reports as itself, unchanged; more than one collects
// into one "N of M items failed" message rather than only ever
// reporting the first, per the user's own explicit request.
func TestPasteSummaryError(t *testing.T) {
	one := &pasteJob{total: 3, errors: []error{fmt.Errorf("boom")}}
	if got := pasteSummaryError(one); got.Error() != "boom" {
		t.Errorf("single error = %q, want it unwrapped, not summarized", got.Error())
	}

	many := &pasteJob{total: 3, errors: []error{fmt.Errorf("a failed"), fmt.Errorf("b failed")}}
	got := pasteSummaryError(many).Error()
	if !strings.Contains(got, "2 of 3 items failed") || !strings.Contains(got, "a failed") || !strings.Contains(got, "b failed") {
		t.Errorf("multi-error summary = %q, want it to name the count and every failure", got)
	}
}

// TestCancelPasteJobClosesOpenDialog pins starting a second Paste while
// the first still has a conflict dialog open: the old job's dialog
// closes rather than staying stuck on screen for a job that no longer
// exists.
func TestCancelPasteJobClosesOpenDialog(t *testing.T) {
	r, job, _, _ := setUpPasteConflict(t, "")
	if r.activePage != pasteConflictPage {
		t.Fatal("setup: the conflict dialog should be open")
	}

	r.cancelPasteJob()

	if r.activePage == pasteConflictPage {
		t.Error("cancelling the job should have closed its own conflict dialog")
	}
	if r.pasteJob != nil {
		t.Error("r.pasteJob should be cleared")
	}
	if job.ctx.Err() == nil {
		t.Error("the cancelled job's own context should now report an error")
	}
}
