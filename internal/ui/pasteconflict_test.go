package ui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/fsops"
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
	origCopy, origMove, origMoveFollowingSymlinks := fsCopy, fsMove, fsMoveFollowingSymlinks
	fsCopy = func(src, dst string, opts fsops.CopyOptions) error {
		err := origCopy(src, dst, opts)
		done <- struct{}{}
		return err
	}
	fsMove = func(src, dst string, opts fsops.MoveOptions) error {
		err := origMove(src, dst, opts)
		done <- struct{}{}
		return err
	}
	fsMoveFollowingSymlinks = func(src, dst string, opts fsops.MoveOptions) error {
		err := origMoveFollowingSymlinks(src, dst, opts)
		done <- struct{}{}
		return err
	}
	t.Cleanup(func() { fsCopy, fsMove, fsMoveFollowingSymlinks = origCopy, origMove, origMoveFollowingSymlinks })
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

// TestPasteWalkNeverAttemptsWhenDestinationOverlapsSource pins the
// user's own explicit request: pasting into the very directory the
// selection is already in must never even attempt the real copy/move at
// all — not "flagged as a conflict, where choosing Overwrite would have
// been real data loss" (see fsops.Overlaps' own doc comment), rejected
// outright before fsCopy/fsMove is ever called. Uses isolatePasteIO
// purely to observe whether a real I/O call happens at all — waiting
// for it to *not* happen within a generous window, rather than for it
// to happen (see waitPasteIO), so this doesn't depend on
// recordPasteError's own downstream QueueUpdateDraw hop, which never
// fires here either.
func TestPasteWalkNeverAttemptsWhenDestinationOverlapsSource(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	// destDir == dir: pasting these two right back into the very
	// directory they're already in — every item here overlaps its own
	// destination.
	items := []string{filepath.Join(dir, "apple.txt"), filepath.Join(dir, "banana.txt")}
	job := newPasteTestJob(r, false, dir, len(items))

	done := isolatePasteIO(t)
	r.pasteWalk(job, items)

	select {
	case <-done:
		t.Fatal("fsCopy was called — pasting into the same directory the selection is already in must never attempt a real copy")
	case <-time.After(200 * time.Millisecond):
		// Expected: no I/O call ever happened for either item.
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

// TestPasteClipboardFollowingSymlinksIsNoOpWhenClipboardEmpty pins the
// same "nothing to do" guard pasteClipboard itself has always had —
// checked explicitly here rather than left to startPaste's own
// len(items)==0 return, since there would otherwise be nothing to
// phrase a confirmation dialog about at all.
func TestPasteClipboardFollowingSymlinksIsNoOpWhenClipboardEmpty(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.pasteClipboardFollowingSymlinks()

	if r.activePage == confirmPage {
		t.Error("an empty clipboard should never open the confirmation dialog")
	}
	if r.pasteJob != nil {
		t.Error("an empty clipboard should never start a paste job")
	}
}

// TestPasteClipboardFollowingSymlinksAsksForConfirmationFirst pins the
// user's own explicit safety requirement: Shift+V must never be a
// single, undialogued keypress the way plain Paste is — it can turn a
// small, instant symlink into an arbitrarily large copy of whatever it
// points to (potentially over a network), so it always shows the
// generic confirmDialog first (see openConfirm) rather than starting a
// paste immediately.
func TestPasteClipboardFollowingSymlinksAsksForConfirmationFirst(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.copyToClipboard()

	r.pasteClipboardFollowingSymlinks()

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the confirmation dialog open", r.activePage)
	}
	if r.pasteJob != nil {
		t.Error("no paste job should start before the confirmation is answered")
	}
	if !strings.Contains(r.confirmDialogTitleBar.GetText(true), "following symlinks") {
		t.Errorf("confirmation message = %q, want it to mention following symlinks", r.confirmDialogTitleBar.GetText(true))
	}
}

// TestPasteClipboardFollowingSymlinksCancelledLeavesClipboardAndDiskUntouched
// pins the other half of that same safety requirement: declining the
// confirmation must be a true no-op, not just "the dialog closes" —
// nothing on disk changes and the clipboard survives for a later,
// ordinary Paste.
func TestPasteClipboardFollowingSymlinksCancelledLeavesClipboardAndDiskUntouched(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.copyToClipboard()

	r.pasteClipboardFollowingSymlinks()
	r.cancelConfirm()

	if r.pasteJob != nil {
		t.Error("cancelling the confirmation should never start a paste job")
	}
	if len(r.clipboard) == 0 {
		t.Error("cancelling should leave the clipboard exactly as it was, for a later ordinary Paste")
	}
}

// TestFollowSymlinksPasteConfirmTextDiffersForCutVsCopy pins the one
// sentence that actually has to differ between the two: only a Cut
// ever removes anything at the source at all, so only its own wording
// should say so — the symlink-handling sentence itself stays identical
// either way (see pasteOne's own followSymlinks branch, which really
// does treat both the same for that part).
func TestFollowSymlinksPasteConfirmTextDiffersForCutVsCopy(t *testing.T) {
	copyMessage, _ := followSymlinksPasteConfirmText(2, false)
	cutMessage, _ := followSymlinksPasteConfirmText(2, true)

	if strings.Contains(copyMessage, "removed") {
		t.Errorf("a Copy's own message should never claim anything is removed, got %q", copyMessage)
	}
	if !strings.Contains(cutMessage, "removed") {
		t.Errorf("a Cut's own message should say the original link is removed, got %q", cutMessage)
	}
	if !strings.Contains(cutMessage, "never its target") && !strings.Contains(cutMessage, "never touches") {
		t.Errorf("a Cut's own message should explicitly reassure that the target itself is never touched, got %q", cutMessage)
	}
}

// TestFollowSymlinksPasteConfirmTextUsesSingularForOneItem pins the
// same singular/plural care this app already takes for its clipboard
// status line ("Copy: 1 file" vs "2 files").
func TestFollowSymlinksPasteConfirmTextUsesSingularForOneItem(t *testing.T) {
	message, _ := followSymlinksPasteConfirmText(1, false)
	if !strings.Contains(message, "1 item,") {
		t.Errorf("message for a single item = %q, want it to say %q", message, "1 item,")
	}
	message, _ = followSymlinksPasteConfirmText(2, false)
	if !strings.Contains(message, "2 items,") {
		t.Errorf("message for two items = %q, want it to say %q", message, "2 items,")
	}
}

// TestPasteFollowingSymlinksOnCutMaterializesContentAndRemovesOnlyTheLink
// is this feature's own end-to-end safety test, run through the real
// async engine rather than calling fsops.MoveFollowingSymlinks directly
// (that guarantee is already pinned at the fsops level — see
// move_test.go's own TestMoveFollowingSymlinks* tests) — this one
// instead pins that the UI layer actually wires job.followSymlinks
// through to it at all. "remote" stands in for a network mount (NFS,
// EFS, ...) the user's own report was specifically worried about: an
// entirely separate sibling directory this whole path must never
// delete from, no matter what.
func TestPasteFollowingSymlinksOnCutMaterializesContentAndRemovesOnlyTheLink(t *testing.T) {
	srcDir := fixtureDir(t)
	dstDir := t.TempDir()
	remote := t.TempDir()
	if err := os.WriteFile(filepath.Join(remote, "data.txt"), []byte("remote content"), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(srcDir, "link-to-remote")
	if err := os.Symlink(remote, link); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), srcDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = link
	r.cutToClipboard()
	if err := r.panel.load(dstDir); err != nil {
		t.Fatalf("load(dstDir): %v", err)
	}

	done := isolatePasteIO(t)
	r.pasteClipboardFollowingSymlinks()
	r.acceptConfirm()
	waitPasteIO(t, done, 1)

	if fi, err := os.Lstat(filepath.Join(dstDir, "link-to-remote")); err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("dst should be a real, materialized directory, not a symlink (mode %v, err %v)", fi.Mode(), err)
	}
	if got, err := os.ReadFile(filepath.Join(dstDir, "link-to-remote", "data.txt")); err != nil || string(got) != "remote content" {
		t.Errorf("dst content = %q, %v, want %q", got, err, "remote content")
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("the original symlink should be gone, Lstat err = %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(remote, "data.txt")); err != nil || string(got) != "remote content" {
		t.Fatalf("the symlink's own remote target must survive completely untouched, got %q, %v", got, err)
	}
}

// TestPasteFollowingSymlinksOnCopyLeavesSourceSymlinkInPlace is the
// Copy-side counterpart: dereferencing never touches the source at
// all, symlink or not — only a Cut ever removes anything.
func TestPasteFollowingSymlinksOnCopyLeavesSourceSymlinkInPlace(t *testing.T) {
	srcDir := fixtureDir(t)
	dstDir := t.TempDir()
	target := filepath.Join(srcDir, "apple.txt") // an existing fixture file

	link := filepath.Join(srcDir, "link-to-apple")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), srcDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = link
	r.copyToClipboard()
	if err := r.panel.load(dstDir); err != nil {
		t.Fatalf("load(dstDir): %v", err)
	}

	done := isolatePasteIO(t)
	r.pasteClipboardFollowingSymlinks()
	r.acceptConfirm()
	waitPasteIO(t, done, 1)

	if fi, err := os.Lstat(filepath.Join(dstDir, "link-to-apple")); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("dst should be a real, dereferenced file, not a symlink (mode %v, err %v)", fi.Mode(), err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("a Copy must leave the original symlink completely in place, Lstat mode %v, err %v", fi.Mode(), err)
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

// TestFinishPasteJobReloadsSourceTabsAfterACut pins the user's own
// direct follow-up report: Cut+Paste across the same filesystem lands
// almost instantly (see fsops.Move's own os.Rename fast path), but the
// tab the moved file came *from* stayed showing it anyway, stale,
// until reloaded by hand — only destDir ever got the auto-reload
// TestFinishPasteJobReloadsEveryOpenTabShowingDestDir already pins.
// Mirrors that test's own shape for the opposite side of a Move: every
// open tab showing the source directory, not just r.panel, must lose
// the row that's actually gone.
func TestFinishPasteJobReloadsSourceTabsAfterACut(t *testing.T) {
	srcDir := fixtureDir(t) // rows: "..", app-data, apple.txt, apricot.txt, banana.txt
	destDir := t.TempDir()

	r, err := NewRoot(tview.NewApplication(), srcDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.newTab(srcDir) // a second tab on the very same source directory
	if len(r.tabs) != 2 {
		t.Fatalf("setup: want 2 tabs, got %d", len(r.tabs))
	}
	tabA, tabB := r.tabs[0], r.tabs[1]

	src := filepath.Join(srcDir, "apple.txt")
	dst := filepath.Join(destDir, "apple.txt")
	// The real move already happened by the time finishPasteJob's own
	// reload runs (applyPasteOneResult only ever fires after pasteOne's
	// own real fsMove call already returned) — renaming it away here
	// mirrors that end state without needing a real background paste
	// for this test.
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}

	job := newPasteTestJob(r, true, destDir, 1)
	job.sourceDirs = distinctParentDirs([]string{src})
	r.applyPasteOneResult(job, src, dst, nil)

	for name, p := range map[string]*Panel{"tab A": tabA, "tab B": tabB} {
		if _, ok := rowForPath(p, src); ok {
			t.Errorf("%s (showing the source dir): apple.txt still listed after the Cut moved it away", name)
		}
	}
}

// TestFinishPasteJobLeavesSourceTabsAloneAfterACopy pins Copy's own
// half of the same change: unlike a Cut, nothing at the source was ever
// removed, so a tab showing it must not even be reloaded — pinned via
// srcDir itself not existing as a real, loadable directory by the time
// finishPasteJob runs; a reload attempt there would record a load
// error that this test would then see in job.errors.
func TestFinishPasteJobLeavesSourceTabsAloneAfterACopy(t *testing.T) {
	srcDir := fixtureDir(t)
	destDir := t.TempDir()

	r, err := NewRoot(tview.NewApplication(), srcDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	src := filepath.Join(srcDir, "apple.txt")
	dst := filepath.Join(destDir, "apple.txt")
	if err := os.WriteFile(dst, []byte("copied"), 0o644); err != nil {
		t.Fatal(err)
	}

	job := newPasteTestJob(r, false, destDir, 1)
	job.sourceDirs = distinctParentDirs([]string{src})
	// Removed only after the job is set up, right before the reload
	// this test is actually pinning would run — if a Copy's own reload
	// ever mistakenly included the source, r.panel.load(srcDir) would
	// fail right here and land in job.errors.
	if err := os.RemoveAll(srcDir); err != nil {
		t.Fatal(err)
	}

	r.applyPasteOneResult(job, src, dst, nil)

	if len(job.errors) != 0 {
		t.Errorf("job.errors = %v, want none — a Copy's own source must never be reloaded", job.errors)
	}
}

// TestMaybeReloadPasteAffectedTabsReloadsWhileConflictDialogOpen pins
// the user's own exact report: pasting a selection where a couple of
// items already exist at the destination opens a dialog for the first
// conflict it finds, but everything else in the selection keeps
// copying/moving in the background regardless (see pasteWalk's own doc
// comment) — before this fix, none of that background progress ever
// became visible in the affected tab(s) until every conflict was
// answered, however long that took, since finishPasteJob's own reload
// was the *only* one and it never ran until job.current/job.pending
// were both clear. job.current being non-nil here (mimicking an open
// dialog, without needing a real one) must not block this reload at
// all — only whether an item's own outcome actually changed since the
// last check should.
func TestMaybeReloadPasteAffectedTabsReloadsWhileConflictDialogOpen(t *testing.T) {
	destDir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), destDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	job := newPasteTestJob(r, false, destDir, 2)
	c := pasteConflict{src: "/irrelevant/whatever.txt", dst: filepath.Join(destDir, "whatever.txt")}
	job.current = &c // a conflict dialog is "open" for a *different* item than the one below

	newFile := filepath.Join(destDir, "banana.txt")
	if err := os.WriteFile(newFile, []byte("just landed"), 0o644); err != nil {
		t.Fatal(err)
	}
	job.remaining-- // mimics pasteItemDone having recorded that item's own final outcome

	r.maybeReloadPasteAffectedTabs(job)

	if _, ok := rowForPath(r.panel, newFile); !ok {
		t.Error("destination tab should already show the newly landed file, even with the conflict dialog still open")
	}
	if job.current == nil {
		t.Error("this reload must not itself touch the open dialog's own state")
	}
}

// TestMaybeReloadPasteAffectedTabsSkipsWhenNothingChanged pins the
// other half: a tick where job.remaining hasn't moved since the last
// reload (mid-copy on one large file, or simply no tick has landed
// exactly when an item finished) must not reload at all. Pinned via a
// manual checkbox selection surviving (or not) across the call, since
// Panel.load unconditionally clears it even for a same-path reload
// that changes nothing else — a reliable, already-established way to
// detect whether a reload actually ran without needing a mock.
func TestMaybeReloadPasteAffectedTabsSkipsWhenNothingChanged(t *testing.T) {
	destDir := t.TempDir()
	existing := filepath.Join(destDir, "existing.txt")
	if err := os.WriteFile(existing, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := NewRoot(tview.NewApplication(), destDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	job := newPasteTestJob(r, false, destDir, 1)
	job.lastReloadedRemaining = job.remaining // as if a reload already ran and saw today's count
	r.panel.selected[existing] = true         // a selection unrelated to this paste

	r.maybeReloadPasteAffectedTabs(job)
	if !r.panel.selected[existing] {
		t.Error("nothing changed since the last reload — this call should have been skipped, not clearing an unrelated selection")
	}

	job.remaining-- // now something actually did change
	r.maybeReloadPasteAffectedTabs(job)
	if r.panel.selected[existing] {
		t.Error("remaining changed since the last reload — this call should have actually reloaded")
	}
	if job.lastReloadedRemaining != job.remaining {
		t.Errorf("lastReloadedRemaining = %d, want %d (updated to match)", job.lastReloadedRemaining, job.remaining)
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
	// The title bar carries the actual conflict message now, not a
	// generic "Paste conflict" caption — the same "the question is the
	// header" treatment confirmDialog's openConfirm gives its own
	// question (see renderPasteConflictDialog's own doc comment).
	if wantTitle := ` "apple.txt" already exists in this folder. `; r.pasteConflictDialogTitleBar.GetText(true) != wantTitle {
		t.Errorf("pasteConflictDialogTitleBar text = %q, want %q", r.pasteConflictDialogTitleBar.GetText(true), wantTitle)
	}
}

// TestPasteConflictDialogBlocksRightDragSelection is the paste-conflict
// dialog's own sibling of TestQuitConfirmBlocksRightDragSelection (see
// its own doc comment for the full reasoning): setting only
// pasteConflictDialogLayout's own rect, not r.pasteConflictDialog's
// (the real focus target — see showPasteConflictDialog), would leave
// the latter stale at tview.NewBox's own uninitialized default
// (0, 0, 15, 10), which overlaps the panel — letting a right-drag over
// it slip through captureOutsideClick's own bounds check as if it had
// landed on the dialog instead. resizePasteConflictDialog sets both,
// same as confirmDialog/quitConfirm/the context menu now do; this pins
// that it actually works for this dialog specifically, not just
// theirs.
func TestPasteConflictDialogBlocksRightDragSelection(t *testing.T) {
	dir := fixtureDir(t) // rows: "..", app-data, apple.txt, apricot.txt, banana.txt
	root, cleanup := drawnRoot(t, dir)
	defer cleanup()

	dstDir := t.TempDir()
	dst := filepath.Join(dstDir, "apple.txt")
	if err := os.WriteFile(dst, []byte("existing"), 0o640); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "apple.txt")

	job := newPasteTestJob(root, false, dstDir, 1)
	root.pasteConflictFound(job, newPasteTestConflict(t, src, dst))
	if root.activePage != pasteConflictPage {
		t.Fatalf("setup: activePage = %q, want %q", root.activePage, pasteConflictPage)
	}

	dragRight(t, root, 1, 3)

	for row := 1; row <= 3; row++ {
		ref, ok := root.panel.rowRef(row)
		if !ok {
			t.Fatalf("row %d: no rowRef", row)
		}
		if root.panel.selected[ref.path] {
			t.Errorf("row %d (%s) got selected by a drag while the paste-conflict dialog was open", row, ref.name)
		}
	}
	if root.activePage != pasteConflictPage {
		t.Errorf("activePage = %q after the drag, want still %q", root.activePage, pasteConflictPage)
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

// setUpPasteDirConflict mirrors setUpPasteConflict for a *directory*
// conflict specifically — the one case Overwrite and Merge actually
// differ (see conflictResolution's own doc comment): src has "shared"
// with its own content, dst already has "shared" with different
// content plus "extra", which src doesn't have at all.
func setUpPasteDirConflict(t *testing.T) (r *Root, job *pasteJob, src, dst string) {
	t.Helper()
	srcParent := t.TempDir()
	dstParent := t.TempDir()
	src = filepath.Join(srcParent, "somedir")
	dst = filepath.Join(dstParent, "somedir")

	if err := os.MkdirAll(src, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "shared"), []byte("clean"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "shared"), []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "extra"), []byte("not from source"), 0o640); err != nil {
		t.Fatal(err)
	}

	var err error
	r, err = NewRoot(tview.NewApplication(), srcParent)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)

	job = newPasteTestJob(r, false, dstParent, 1)
	r.pasteConflictFound(job, newPasteTestConflict(t, src, dst))
	if job.current == nil {
		t.Fatal("setup: conflict should be showing")
	}
	return r, job, src, dst
}

// TestChooseConflictResolutionOverwriteReplacesDirectoryEntirely pins
// the user's own explicit expectation: "Overwrite" on a directory
// conflict must mean fsops.ReplaceEntirely — the destination ends up
// identical to the source, "extra" (dst's own file src doesn't have at
// all) gone — not the old, merge-only behavior that left it sitting
// there untouched.
func TestChooseConflictResolutionOverwriteReplacesDirectoryEntirely(t *testing.T) {
	r, _, _, dst := setUpPasteDirConflict(t)

	done := isolatePasteIO(t)
	r.chooseConflictResolution(resolveOverwrite, false)
	waitPasteIO(t, done, 1)

	if got, err := os.ReadFile(filepath.Join(dst, "shared")); err != nil || string(got) != "clean" {
		t.Errorf("dst/shared = %q, %v, want %q", got, err, "clean")
	}
	if _, err := os.Stat(filepath.Join(dst, "extra")); !os.IsNotExist(err) {
		t.Errorf("dst/extra should be gone after Overwrite, stat err = %v", err)
	}
}

// TestChooseConflictResolutionMergeKeepsExtraDestFiles pins the
// explicit alternative: "Merge into existing folder" keeps whatever the
// source doesn't have.
func TestChooseConflictResolutionMergeKeepsExtraDestFiles(t *testing.T) {
	r, _, _, dst := setUpPasteDirConflict(t)

	done := isolatePasteIO(t)
	r.chooseConflictResolution(resolveMerge, false)
	waitPasteIO(t, done, 1)

	if got, err := os.ReadFile(filepath.Join(dst, "shared")); err != nil || string(got) != "clean" {
		t.Errorf("dst/shared = %q, %v, want %q", got, err, "clean")
	}
	if got, err := os.ReadFile(filepath.Join(dst, "extra")); err != nil || string(got) != "not from source" {
		t.Errorf("dst/extra = %q, %v, want it left untouched by Merge", got, err)
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
	msg := r.pasteConflictDialogTitleBar.GetText(true)
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

	// The message lives in the header now, not a list item (see
	// renderPasteConflictDialog's own doc comment) — wantWidth has to
	// account for that the same way resizePasteConflictDialog itself
	// does, or this would just be re-deriving listSize's own now-stale
	// answer instead of actually checking the resize.
	wantWidth, _ := listSize(r.pasteConflictDialog)
	if headerWidth := tview.TaggedStringWidth(r.pasteConflictDialogTitleBar.GetText(false)); headerWidth > wantWidth {
		wantWidth = headerWidth
	}
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
	msg := r.pasteConflictDialogTitleBar.GetText(true)
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
	fsCopy = func(src, dst string, opts fsops.CopyOptions) error {
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
		go r.pasteOne(job, filepath.Join(dir, fmt.Sprintf("item-%d.txt", i)), filepath.Join(dir, fmt.Sprintf("out-%d.txt", i)), false, fsops.ReplaceEntirely)
	}
	waitPasteIO(t, done, 5)
}

// TestPasteOneFoldsPreviousFileSizeIntoBytesBaseOnTheNextFile pins the
// job-wide byte bookkeeping pasteProgressText's own byte-percentage
// column and dual bar read from (see pasteJob.bytesBase's own doc
// comment): after one file's real Copy finishes, its own size sits in
// currentFileSize/currentFileBytes, not yet folded into bytesBase —
// only once a *second* file's own onFile fires (see pasteOne) is the
// first one's size safely known to be done and added there, with
// currentFileSize/currentFileBytes reset to the new file's own state.
// Uses isolatePasteIO (real fsCopy, signaled per call) rather than
// waiting for pasteOne itself to return, which would hang forever
// without a live Application.Run() loop to service its own final
// QueueUpdateDraw hand-off (see
// TestPasteOneSerializesRealIOAcrossConcurrentItems's own doc comment
// for the same reasoning).
func TestPasteOneFoldsPreviousFileSizeIntoBytesBaseOnTheNextFile(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	job := newPasteTestJob(r, false, dir, 2)

	file1 := filepath.Join(dir, "size100.txt")
	if err := os.WriteFile(file1, bytes.Repeat([]byte("a"), 100), 0o640); err != nil {
		t.Fatal(err)
	}
	file2 := filepath.Join(dir, "size200.txt")
	if err := os.WriteFile(file2, bytes.Repeat([]byte("b"), 200), 0o640); err != nil {
		t.Fatal(err)
	}
	out1 := filepath.Join(dir, "out1.txt")
	out2 := filepath.Join(dir, "out2.txt")

	done := isolatePasteIO(t)

	go r.pasteOne(job, file1, out1, false, fsops.ReplaceEntirely)
	waitPasteIO(t, done, 1)
	if got := job.currentFileSize.Load(); got != 100 {
		t.Errorf("after file1: currentFileSize = %d, want 100", got)
	}
	if got := job.currentFileBytes.Load(); got != 100 {
		t.Errorf("after file1: currentFileBytes = %d, want 100 (fully copied)", got)
	}
	if got := job.bytesBase.Load(); got != 0 {
		t.Errorf("after file1: bytesBase = %d, want 0 (not folded in until a second file starts)", got)
	}

	go r.pasteOne(job, file2, out2, false, fsops.ReplaceEntirely)
	waitPasteIO(t, done, 1)
	if got := job.bytesBase.Load(); got != 100 {
		t.Errorf("after file2 starts: bytesBase = %d, want 100 (file1's own size folded in)", got)
	}
	if got := job.currentFileSize.Load(); got != 200 {
		t.Errorf("after file2: currentFileSize = %d, want 200", got)
	}
	if got := job.currentFileBytes.Load(); got != 200 {
		t.Errorf("after file2: currentFileBytes = %d, want 200 (fully copied)", got)
	}
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
// TestStartPasteQueuesBehindARunningJob pins the user's own explicit
// report and follow-up request: starting a second Paste while one is
// already running used to cancel the first outright, silently dropping
// whatever it hadn't gotten to yet. It now queues behind it instead —
// r.pasteJob itself must stay exactly the running job, untouched, and
// the new request lands in r.pasteQueue rather than starting or
// replacing anything.
func TestStartPasteQueuesBehindARunningJob(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	running := newPasteTestJob(r, false, dir, 1)

	secondDestDir := t.TempDir()
	secondItems := []string{filepath.Join(dir, "banana.txt")}
	r.startPaste(secondItems, true, secondDestDir, false)

	if r.pasteJob != running {
		t.Fatal("starting a second Paste should not have touched the running job at all")
	}
	if running.ctx.Err() != nil {
		t.Error("the running job must not be cancelled just because a second Paste was asked for")
	}
	if len(r.pasteQueue) != 1 {
		t.Fatalf("pasteQueue = %+v, want exactly one queued entry", r.pasteQueue)
	}
	queued := r.pasteQueue[0]
	if !queued.cut || queued.destDir != secondDestDir || len(queued.items) != 1 || queued.items[0] != secondItems[0] {
		t.Errorf("queued entry = %+v, want cut=true destDir=%q items=%v", queued, secondDestDir, secondItems)
	}
}

// TestFinishPasteJobStartsNextQueuedPaste pins the other half: once the
// running job actually finishes, the next queued Paste starts
// automatically, in order, rather than needing anything further to
// trigger it. Uses applyPasteOneResult (via pasteItemDone), the same
// path a real completed item takes, rather than calling finishPasteJob
// directly, so this also exercises pasteItemDone's own "job is now
// fully done" detection reaching all the way through to the queue.
func TestFinishPasteJobStartsNextQueuedPaste(t *testing.T) {
	srcDir := fixtureDir(t)
	firstDestDir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), srcDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	firstJob := newPasteTestJob(r, false, firstDestDir, 1)

	secondDestDir := t.TempDir()
	secondItems := []string{filepath.Join(srcDir, "banana.txt")}
	r.pasteQueue = append(r.pasteQueue, queuedPaste{items: secondItems, cut: true, destDir: secondDestDir})

	src := filepath.Join(srcDir, "apple.txt")
	dst := filepath.Join(firstDestDir, "apple.txt")
	r.applyPasteOneResult(firstJob, src, dst, nil) // firstJob's only item — finishes it

	if r.pasteJob == nil {
		t.Fatal("finishing the first job should have started the next queued Paste")
	}
	if r.pasteJob == firstJob {
		t.Fatal("r.pasteJob should now be the newly started job, not the one that just finished")
	}
	if !r.pasteJob.cut || r.pasteJob.destDir != secondDestDir || r.pasteJob.total != len(secondItems) {
		t.Errorf("started job = %+v, want cut=true destDir=%q total=%d", r.pasteJob, secondDestDir, len(secondItems))
	}
	if len(r.pasteQueue) != 0 {
		t.Error("the queue should be empty once its one entry has started")
	}
}

// TestCancelPasteJobDropsTheWholeQueue pins a deliberate choice, not
// just an oversight: an explicit cancel (Ctrl+C, or the confirmed
// "cancel it and quit" answer — see cancelPasteJob's own doc comment)
// drops whatever was queued behind the cancelled job too, rather than
// letting it start right after. Continuing on to a paste the user never
// asked to see start next would be exactly the kind of surprise this
// whole queue exists to prevent in the first place.
func TestCancelPasteJobDropsTheWholeQueue(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	newPasteTestJob(r, false, dir, 1)
	r.pasteQueue = append(r.pasteQueue, queuedPaste{items: []string{filepath.Join(dir, "banana.txt")}, destDir: t.TempDir()})

	r.cancelPasteJob()

	if r.pasteJob != nil {
		t.Error("r.pasteJob should be cleared")
	}
	if len(r.pasteQueue) != 0 {
		t.Error("cancelling should have dropped the queue too, not left it to start next")
	}
}

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

// TestRequestCancelStopsARunningPaste pins the user's own explicit
// report: a Paste, once started, previously had no way to be
// interrupted at all — Ctrl+C (RequestCancel) now stops it the same way
// starting a second Paste already could (see cancelPasteJob), the
// moment there's no overlay open to route to instead.
func TestRequestCancelStopsARunningPaste(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	job := newPasteTestJob(r, false, dir, 3)

	r.RequestCancel()

	if r.pasteJob != nil {
		t.Error("r.pasteJob should be cleared after RequestCancel")
	}
	if job.ctx.Err() == nil {
		t.Error("the cancelled job's own context should now report an error")
	}
}

// TestRequestCancelFromConflictDialogCancelsWholeJob pins the sharper
// edge of the same report: Ctrl+C while the paste-conflict dialog
// itself is open must cancel the *whole* job, not just close that one
// dialog the way an unrelated overlay's own hideOverlay would — closing
// only the dialog would leave job.current pointing at a conflict
// nothing could ever resolve again (its own three buttons are the only
// path to chooseConflictResolution), silently stranding the job
// forever instead of either finishing or actually being cancelled.
func TestRequestCancelFromConflictDialogCancelsWholeJob(t *testing.T) {
	r, job, _, _ := setUpPasteConflict(t, "")
	if r.activePage != pasteConflictPage {
		t.Fatal("setup: the conflict dialog should be open")
	}

	r.RequestCancel()

	if r.activePage == pasteConflictPage {
		t.Error("the dialog should have closed")
	}
	if r.pasteJob != nil {
		t.Error("r.pasteJob should be cleared, not just the dialog closed")
	}
	if job.ctx.Err() == nil {
		t.Error("the whole job should be cancelled, not just its dialog dismissed")
	}
}

// TestCaptureOutsideClickBlockedForPasteConflictDialog pins the mouse-side
// sibling of TestRequestCancelFromConflictDialogCancelsWholeJob just
// above: a stray click outside the dialog — not a deliberate Ctrl+C —
// used to reach captureOutsideClick's ordinary hideOverlay path like any
// other overlay, closing the dialog while leaving job.current pointing
// at a conflict nothing could ever resolve again. The job then neither
// progressed (its one open conflict was never answered) nor ever
// reported as finished, and the dialog never reappeared either — a real,
// user-reported dead end after clicking anywhere outside it. The click
// must now be swallowed instead, leaving the dialog open and the
// conflict still pending until one of its own buttons (or Escape) is
// actually used.
func TestCaptureOutsideClickBlockedForPasteConflictDialog(t *testing.T) {
	r, job, _, _ := setUpPasteConflict(t, "")
	if r.activePage != pasteConflictPage {
		t.Fatal("setup: the conflict dialog should be open")
	}

	// r.SetRect(0, 0, 100, 40) in setUpPasteConflict centers a small
	// dialog somewhere in the middle of that area — (0, 0) is always
	// clear of it, the same corner-of-the-screen approach
	// outsidePropertiesClick's own doc comment uses for Properties.
	action, event := r.captureOutsideClick(tview.MouseLeftClick, tcell.NewEventMouse(0, 0, tcell.Button1, 0))

	if action != tview.MouseConsumed || event != nil {
		t.Errorf("outside click should be consumed and swallowed, got action=%v event=%v", action, event)
	}
	if r.activePage != pasteConflictPage {
		t.Errorf("activePage = %q, want the dialog to stay open", r.activePage)
	}
	if job.current == nil {
		t.Error("job.current should still be set — the conflict must still be pending, not silently stranded")
	}
	if r.pasteJob == nil {
		t.Error("the job itself should still be running, not cancelled by a stray outside click")
	}
}

// TestRequestCancelLeavesBackgroundPasteRunningForAnUnrelatedOverlay
// pins the other half: Ctrl+C while some *other* overlay is open
// (Properties, say) closes that overlay as it always has — a paste
// merely continuing in the background is not what the user is looking
// at or asking to stop, unlike the conflict dialog above, which is
// itself about the paste.
func TestRequestCancelLeavesBackgroundPasteRunningForAnUnrelatedOverlay(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	drawRoot(t, r, 120, 30)
	job := newPasteTestJob(r, false, dir, 3)

	r.panel.focusRow(2) // off ".." — see fixtureDir
	r.propertiesCurrentEntry()
	if r.activePage != propertiesPage {
		t.Fatal("setup: Properties should be open")
	}

	r.RequestCancel()

	if r.activePage == propertiesPage {
		t.Error("Properties should have closed")
	}
	if r.pasteJob == nil {
		t.Error("the background paste should still be running — Ctrl+C here was about Properties, not it")
	}
	if job.ctx.Err() != nil {
		t.Error("the background job's own context should be untouched")
	}
}
