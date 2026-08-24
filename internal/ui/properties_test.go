package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

func TestPermString(t *testing.T) {
	tests := []struct {
		mode os.FileMode
		want string
	}{
		{0o644, "-rw-r--r--"},
		{os.ModeDir | 0o755, "drwxr-xr-x"},
		{os.ModeSymlink | 0o777, "lrwxrwxrwx"},
		{0o600, "-rw-------"},
	}

	for _, tt := range tests {
		if got := permString(tt.mode); got != tt.want {
			t.Errorf("permString(%v) = %q, want %q", tt.mode, got, tt.want)
		}
	}
}

func TestHumanSize(t *testing.T) {
	tests := []struct {
		size int64
		want string
	}{
		{0, "0B"},
		{1023, "1023B"},
		{1024, "1.0K"},
		{1536, "1.5K"},
		{1024 * 1024, "1.0M"},
		{1024 * 1024 * 1024, "1.0G"},
	}

	for _, tt := range tests {
		if got := humanSize(tt.size); got != tt.want {
			t.Errorf("humanSize(%d) = %q, want %q", tt.size, got, tt.want)
		}
	}
}

func TestSizeWithBytes(t *testing.T) {
	tests := []struct {
		size int64
		want string
	}{
		{0, "0B"},       // below 1024: humanSize is already exact
		{1023, "1023B"}, // below 1024: humanSize is already exact
		{1024, "1.0K (1024 bytes)"},
		{2184, "2.1K (2184 bytes)"},
		{1024 * 1024, "1.0M (1048576 bytes)"},
	}

	for _, tt := range tests {
		if got := sizeWithBytes(tt.size); got != tt.want {
			t.Errorf("sizeWithBytes(%d) = %q, want %q", tt.size, got, tt.want)
		}
	}
}

func TestClassifyKind(t *testing.T) {
	tests := []struct {
		name string
		info fsops.Info
		want string
	}{
		{"plain file", fsops.Info{}, "file"},
		{"directory", fsops.Info{IsDir: true}, "directory"},
		{"symlink to file", fsops.Info{IsSymlink: true}, "symlink to file"},
		{"symlink to directory", fsops.Info{IsSymlink: true, LinkIsDir: true}, "symlink to directory"},
		{"broken symlink", fsops.Info{IsSymlink: true, LinkBroken: true}, "broken symlink"},
		// A broken link takes priority even if LinkIsDir was somehow also
		// set (shouldn't happen together in practice, but the ordering
		// should still be unambiguous).
		{"broken wins over LinkIsDir", fsops.Info{IsSymlink: true, LinkBroken: true, LinkIsDir: true}, "broken symlink"},
	}

	for _, tt := range tests {
		if got := classifyKind(tt.info); got != tt.want {
			t.Errorf("%s: classifyKind() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestIsDirish(t *testing.T) {
	tests := []struct {
		name string
		info fsops.Info
		want bool
	}{
		{"plain file", fsops.Info{}, false},
		{"directory", fsops.Info{IsDir: true}, true},
		{"symlink to file", fsops.Info{IsSymlink: true}, false},
		{"symlink to directory", fsops.Info{IsSymlink: true, LinkIsDir: true}, true},
		{"broken symlink", fsops.Info{IsSymlink: true, LinkBroken: true}, false},
	}

	for _, tt := range tests {
		if got := isDirish(tt.info); got != tt.want {
			t.Errorf("%s: isDirish() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestFormatChain(t *testing.T) {
	tests := []struct {
		name  string
		chain fsops.LinkChain
		want  string
	}{
		{"resolves to file", fsops.LinkChain{Hops: []string{"/a", "/b"}}, "/a -> /b (file)"},
		{"resolves to directory", fsops.LinkChain{Hops: []string{"/a", "/b"}, FinalIsDir: true}, "/a -> /b (directory)"},
		{"broken", fsops.LinkChain{Hops: []string{"/a", "/b"}, Broken: true}, "/a -> /b (broken)"},
		{"cyclic", fsops.LinkChain{Hops: []string{"/a", "/b"}, Cyclic: true}, "/a -> /b (cycle detected)"},
		{"too deep", fsops.LinkChain{Hops: []string{"/a", "/b"}, TooDeep: true}, "/a -> /b (too many hops)"},
	}

	for _, tt := range tests {
		if got := formatChain(tt.chain); got != tt.want {
			t.Errorf("%s: formatChain() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestTextSize(t *testing.T) {
	width, height := textSize("ab\nabcd\na")
	if width != 6 { // longest line "abcd" (4) + 2 padding
		t.Errorf("width = %d, want 6", width)
	}
	if height != 3 {
		t.Errorf("height = %d, want 3", height)
	}
}

// seedProperties builds a real Root (any directory will do — this is for
// exercising renderProperties/propertiesBuilder with a hand-built Info,
// not real navigation) and seeds its Properties state directly, the same
// assignments openProperties itself makes, bypassing fsops.Stat so a
// fabricated fsops.Info can be used without a real file backing it.
// Callers that need Root.propertiesTarget to resolve to something real on
// disk (Save, hashing, symlink chains) should set info.Path themselves
// and create it under t.TempDir() first.
func seedProperties(t *testing.T, info fsops.Info) *Root {
	t.Helper()
	r, err := NewRoot(tview.NewApplication(), t.TempDir())
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.propertiesTarget = info.Path
	r.propertiesStat = info
	r.propertiesHashes = nil
	r.propertiesDirty = false
	r.stagedName = info.Name
	r.stagedMode = info.Mode.Perm()
	r.stagedMtime = info.ModTime
	r.renderProperties()
	return r
}

func TestPropertiesShowsLinkTargetOnlyForSymlinks(t *testing.T) {
	file := seedProperties(t, fsops.Info{Name: "a.txt", ModTime: time.Now()})
	if text := file.propertiesText.GetText(true); strings.Contains(text, "Link target") {
		t.Error("a regular file's Properties should not mention a link target")
	}

	link := seedProperties(t, fsops.Info{
		Name:       "b.txt",
		IsSymlink:  true,
		LinkTarget: "/somewhere/else",
		ModTime:    time.Now(),
	})
	text := link.propertiesText.GetText(true)
	wantLinkLine := fmt.Sprintf("%-13s%s", "Link target:", "/somewhere/else")
	if !strings.Contains(text, wantLinkLine) {
		t.Errorf("symlink Properties should contain %q, got:\n%s", wantLinkLine, text)
	}
}

// TestPropertiesShowsLinksOnlyWhenSharedAndNotForDirs pins the "Links"
// field's visibility rule: only shown for a non-directory with Nlink > 1
// (content that exists under another name too) — not for an ordinary
// single-link file, and not for a directory even though directories
// always have Nlink >= 2 trivially.
func TestPropertiesShowsLinksOnlyWhenSharedAndNotForDirs(t *testing.T) {
	single := seedProperties(t, fsops.Info{Name: "a.txt", Nlink: 1, ModTime: time.Now()})
	if text := single.propertiesText.GetText(true); strings.Contains(text, "Links") {
		t.Errorf("a single-link file should not mention Links, got:\n%s", text)
	}

	shared := seedProperties(t, fsops.Info{Name: "b.txt", Nlink: 2, ModTime: time.Now()})
	if text := shared.propertiesText.GetText(true); !strings.Contains(text, "Links") {
		t.Errorf("a file with Nlink=2 should mention Links, got:\n%s", text)
	}

	dir := seedProperties(t, fsops.Info{Name: "c", IsDir: true, Nlink: 2, ModTime: time.Now()})
	if text := dir.propertiesText.GetText(true); strings.Contains(text, "Links") {
		t.Errorf("a directory should not mention Links even with Nlink > 1, got:\n%s", text)
	}
}

// TestPropertiesShowsMountPoint pins that "Mount point: yes" only
// appears when Info.MountPoint is true.
func TestPropertiesShowsMountPoint(t *testing.T) {
	plain := seedProperties(t, fsops.Info{Name: "a", IsDir: true, ModTime: time.Now()})
	if text := plain.propertiesText.GetText(true); strings.Contains(text, "Mount point") {
		t.Errorf("an ordinary directory should not mention a mount point, got:\n%s", text)
	}

	mounted := seedProperties(t, fsops.Info{Name: "b", IsDir: true, MountPoint: true, ModTime: time.Now()})
	text := mounted.propertiesText.GetText(true)
	wantLine := fmt.Sprintf("%-13s%s", "Mount point:", "yes")
	if !strings.Contains(text, wantLine) {
		t.Errorf("a mount point should say so (want %q), got:\n%s", wantLine, text)
	}
}

// TestRenderPropertiesShowsChainForMultiHopSymlink exercises the full
// path from a real multi-hop symlink on disk through Root.openProperties
// to the rendered "Chain" line.
func TestRenderPropertiesShowsChainForMultiHopSymlink(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "final.txt")
	mid := filepath.Join(dir, "mid")
	start := filepath.Join(dir, "start")
	if err := os.WriteFile(final, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(final, mid); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(mid, start); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = start
	r.openProperties()

	text := r.propertiesText.GetText(true)
	wantChain := fmt.Sprintf("%s -> %s (file)", mid, final)
	if !strings.Contains(text, wantChain) {
		t.Errorf("Properties text should contain the chain %q, got:\n%s", wantChain, text)
	}
}

// TestRenderPropertiesOmitsChainForSingleHopSymlink pins that a simple,
// non-chained symlink doesn't get a redundant "Chain" line — "Link
// target" and "Type" already fully describe it.
func TestRenderPropertiesOmitsChainForSingleHopSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = link
	r.openProperties()

	// "Chain:" (with the colon infoField adds), not just "Chain" — the
	// bare word is a false-positive trap here: t.TempDir() names the
	// directory after this very test function, and this one's own name
	// contains "Chain" too, via the Path/Link target fields.
	if text := r.propertiesText.GetText(true); strings.Contains(text, "Chain:") {
		t.Errorf("a single-hop symlink should not show a Chain line, got:\n%s", text)
	}
}

func TestComputeHashesUpdatesPropertiesText(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o640); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path
	r.openProperties()

	before := r.propertiesText.GetText(true)
	if !strings.Contains(before, "Press h or click here") {
		t.Errorf("Properties text before computing hashes should show the hint, got:\n%s", before)
	}

	// computeHashes itself now only ever *starts* a background
	// computation (see its own doc comment) — the result lands via
	// r.app.QueueUpdateDraw once hashFile returns, which nothing here
	// drains (see isolateHashFile's own doc comment on why that's not
	// safe to do in a test without a running event loop). What's
	// actually being pinned here is renderProperties' own rendering of
	// an already-computed result, so set propertiesHashes directly
	// rather than going through the async path.
	r.propertiesHashes = &fsops.Hashes{
		MD5:    "5eb63bbbe01eeed093cb22bb8f5acdc3", // MD5("hello world")
		SHA1:   "2aae6c35c94fcfb415dbe95f408b9ce91ee846ed",
		SHA256: "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde",
	}
	r.rerenderProperties()

	after := r.propertiesText.GetText(true)
	if !strings.Contains(after, "5eb63bbbe01eeed093cb22bb8f5acdc3") {
		t.Errorf("Properties text after computing hashes should show the MD5 digest, got:\n%s", after)
	}
	if !strings.Contains(after, "SHA-256") {
		t.Errorf("Properties text after computing hashes should label the SHA-256 line, got:\n%s", after)
	}
}

func TestComputeHashesSkipsDirectories(t *testing.T) {
	dir := fixtureDir(t)

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "app-data")
	r.openProperties()

	text := r.propertiesText.GetText(true)
	if strings.Contains(text, "Press h or click here") {
		t.Errorf("a directory's Properties should not offer to compute a hash, got:\n%s", text)
	}

	r.computeHashes()
	if r.propertiesText.GetText(true) != text {
		t.Error("computeHashes on a directory should not change the Properties text")
	}
	if r.activePage == errorPage {
		t.Error("computeHashes on a directory should silently no-op, not report an error")
	}
}

// isolateHashFile overrides hashFile (see computeHashes) with a fake
// that blocks until t's own cleanup unblocks it, then returns
// (fsops.Hashes{}, context.Canceled) — restoring the real fsops.Hash
// afterward. Long enough that computeHashes' own background goroutine
// never reaches r.app.QueueUpdateDraw *during* the test itself (nothing
// runs the event loop to drain it — see StartClock's own doc comment
// for the same concern elsewhere), so tests using this only ever check
// computeHashes' own synchronous setup (hashInProgress, the animation's
// first frame).
//
// Deliberately NOT a bare "select {}": that would leave the goroutine
// blocked forever rather than reaped at the end of the test.
//
// The returned channel closes the instant the fake is actually called
// — i.e. once computeHashes' own background goroutine has done its
// one-time read of the package-level hashFile var. Callers MUST
// receive from it before returning (see the call sites below): the
// "go func(){...}()" in computeHashes only *schedules* that goroutine,
// it doesn't run it, so without this wait the goroutine's read can
// still be pending when the test ends and races under -race against
// this helper's own t.Cleanup restoring hashFile. Also register
// isolateHashFile(t) *before* t.Cleanup(r.cancelHashComputation) —
// cleanups run in LIFO order, so cancelHashComputation (which cancels
// this call's own ctx) runs first, and by the time the fake actually
// unblocks, computeHashes' own ctx.Err() check already sees it as
// cancelled and returns without touching hashFile or the UI again.
func isolateHashFile(t *testing.T) <-chan struct{} {
	t.Helper()
	original := hashFile
	started := make(chan struct{})
	unblock := make(chan struct{})
	hashFile = func(string) (fsops.Hashes, error) {
		close(started)
		<-unblock
		return fsops.Hashes{}, context.Canceled
	}
	t.Cleanup(func() {
		close(unblock)
		hashFile = original
	})
	return started
}

// TestComputeHashesShowsAnimationImmediately pins the user's own
// request: computing hashes (which can take a few seconds on a large
// file) shows a moving "in progress" indicator right away, rather than
// the hash section just sitting frozen with no feedback that anything
// is happening.
func TestComputeHashesShowsAnimationImmediately(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "banana.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	started := isolateHashFile(t)
	t.Cleanup(r.cancelHashComputation)
	r.target = path
	r.openProperties()

	r.computeHashes()
	<-started // wait for hashFile's one-time read (see isolateHashFile) before this test can safely end

	if !r.hashInProgress {
		t.Fatal("hashInProgress should be true right after computeHashes starts")
	}
	text := r.propertiesText.GetText(true)
	if !strings.Contains(text, hashAnimationFrames[0]) || !strings.Contains(text, "Computing hashes") {
		t.Errorf("propertiesText should show the first animation frame (%q) and \"Computing hashes\", got:\n%s", hashAnimationFrames[0], text)
	}
}

// TestComputeHashesIgnoresReentryWhileInProgress pins that pressing h
// or clicking again while a computation is already running (see
// hashInProgress) doesn't start a second, overlapping one.
func TestComputeHashesIgnoresReentryWhileInProgress(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "banana.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	started := isolateHashFile(t)
	t.Cleanup(r.cancelHashComputation)
	r.target = path
	r.openProperties()

	r.computeHashes()
	<-started // wait for hashFile's one-time read (see isolateHashFile) before this test can safely end
	if r.hashCancel == nil {
		t.Fatal("setup: hashCancel should be set after the first call")
	}

	// A real, newly-started computation always resets hashAnimFrame to 0
	// (see computeHashes' own setup) — setting it to something else and
	// confirming a second call leaves it alone is this test's way of
	// observing that the second call actually no-opped, since func
	// values (hashCancel) aren't comparable in Go and can't be checked
	// directly for "is this still the same one".
	r.hashAnimFrame = 3
	r.computeHashes() // should no-op — a computation is already in flight
	if r.hashAnimFrame != 3 {
		t.Errorf("hashAnimFrame = %d, want unchanged at 3 — a second computeHashes call while already in progress should have no-opped", r.hashAnimFrame)
	}
}

// TestOpenPropertiesCancelsStaleHashComputation pins that reopening
// Properties for a — possibly different — target cancels whatever hash
// computation was still running for the previous one (see
// cancelHashComputation), so a stale animation frame or result can
// never land on the new target's own display.
func TestOpenPropertiesCancelsStaleHashComputation(t *testing.T) {
	dir := fixtureDir(t)

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	started := isolateHashFile(t)
	t.Cleanup(r.cancelHashComputation)

	r.target = filepath.Join(dir, "banana.txt")
	r.openProperties()
	r.computeHashes()
	<-started // wait for hashFile's one-time read (see isolateHashFile) before this test can safely end
	if !r.hashInProgress {
		t.Fatal("setup: expected a hash computation to be in progress")
	}

	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()

	if r.hashInProgress {
		t.Error("hashInProgress should be false again after reopening Properties for a different target — the stale computation should have been cancelled")
	}
	if r.hashCancel != nil {
		t.Error("hashCancel should be nil again after reopening Properties for a different target")
	}
}

// TestPropertiesHPressTriggersHash pins the 'h' keyboard shortcut for
// computing hashes, dispatched through r.properties itself (see
// hashesInputCapture) the way a real keypress arrives, rather than
// calling capturePropertiesKey directly — that's no longer where this
// is handled (see its own doc comment on why).
func TestPropertiesHPressTriggersHash(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "banana.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	started := isolateHashFile(t)
	t.Cleanup(r.cancelHashComputation)
	r.target = path
	r.openProperties()

	r.properties.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'h', tcell.ModNone), func(tview.Primitive) {})
	<-started // wait for hashFile's one-time read (see isolateHashFile) before this test can safely end

	// Hashing now runs on a background goroutine (see computeHashes),
	// with an "in progress" animation shown while it's running — the
	// real result only ever lands via r.app.QueueUpdateDraw, which
	// nothing here drains (see isolateHashFile's own doc comment), so
	// this only pins that pressing h actually started a computation.
	if !r.hashInProgress {
		t.Error("pressing h should have started computing the hash")
	}
	if !strings.Contains(r.propertiesText.GetText(true), "Computing hashes") {
		t.Errorf("propertiesText should show the in-progress animation, got:\n%s", r.propertiesText.GetText(true))
	}
}

// TestPropertiesHPressTriggersHashWhileInlineEditorOpen pins the fix for
// the user's own report: pressing h stopped working once you'd tabbed
// onto an auto-editing field (see isAutoEditField — Name, the octal
// permission value, either half of Modified), since real keyboard focus
// moves to propertiesEditField then, not propertiesText, where 'h' used
// to be handled — it would just get typed into whatever field was open
// instead. hashesInputCapture (installed on r.properties, the shared
// ancestor of both) fixes this by running before propertiesEditField's
// own InputHandler ever gets a chance to.
func TestPropertiesHPressTriggersHashWhileInlineEditorOpen(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "banana.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	started := isolateHashFile(t)
	t.Cleanup(r.cancelHashComputation)
	r.target = path
	r.openProperties()

	r.setPropertiesFocus(0) // fieldName is propertyFieldOrder[0] — auto-opens the inline editor
	if !r.propertiesEditField.HasFocus() {
		t.Fatal("setup: expected the inline editor to have opened and taken focus")
	}
	nameBefore := r.propertiesEditField.GetText()

	r.properties.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'h', tcell.ModNone), func(tview.Primitive) {})
	<-started // wait for hashFile's one-time read (see isolateHashFile) before this test can safely end

	// See TestPropertiesHPressTriggersHash's own doc comment on why this
	// checks that a computation started, not a finished "MD5:" result.
	if !r.hashInProgress {
		t.Error("pressing h while the inline editor is open should still have started computing the hash")
	}
	if got := r.propertiesEditField.GetText(); got != nameBefore {
		t.Errorf("the inline editor's own text = %q, want unchanged %q — h should not have been typed into it", got, nameBefore)
	}
}

// TestPropertiesHashLineClickTriggersHash pins the click affordance: a
// click landing on (or below) the hash hint line computes the hash,
// dispatched through r.properties itself (see hashesMouseCapture) the
// way a real click arrives, with a genuinely drawn overlay
// (tcell.SimulationScreen) behind the coordinates rather than assumed
// ones.
func TestPropertiesHashLineClickTriggersHash(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "banana.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	started := isolateHashFile(t)
	t.Cleanup(r.cancelHashComputation)
	r.panel.SetRect(0, 0, 80, 24) // realistic — see clampToPanel's own default-rect gotcha noted elsewhere in this file
	r.target = path
	r.openProperties()

	screen := drawProperties(t, r)
	defer screen.Fini()

	x, y, _, _ := r.propertiesText.GetInnerRect()
	clickY := y + r.hashSectionRow

	consumed, _ := r.properties.MouseHandler()(tview.MouseLeftClick, tcell.NewEventMouse(x, clickY, tcell.Button1, 0), func(tview.Primitive) {})
	<-started // wait for hashFile's one-time read (see isolateHashFile) before this test can safely end
	if !consumed {
		t.Error("click on the hash line should be consumed")
	}
	// See TestPropertiesHPressTriggersHash's own doc comment on why this
	// checks that a computation started, not a finished "MD5:" result.
	if !r.hashInProgress {
		t.Error("clicking the hash line should have started computing the hash")
	}
}

// TestPropertiesHashLineClickTriggersHashWhileInlineEditorOpen pins the
// fix for the user's own report: clicking the hash line used to instead
// land on whichever of Properties' several other sub-widgets (an
// auto-opened inline editor, or the Cancel/Save button row) happened to
// occupy that screen position once a field had already been tabbed to —
// closing the overlay via Cancel/Save instead of computing hashes, even
// though nothing had actually been changed. hashesMouseCapture (also
// installed on r.properties) runs before any of that, so this now finds
// the hash line regardless.
func TestPropertiesHashLineClickTriggersHashWhileInlineEditorOpen(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "banana.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	started := isolateHashFile(t)
	t.Cleanup(r.cancelHashComputation)
	r.panel.SetRect(0, 0, 80, 24)
	r.target = path
	r.openProperties()

	r.setPropertiesFocus(0) // auto-opens the inline editor over Name
	if !r.propertiesEditField.HasFocus() {
		t.Fatal("setup: expected the inline editor to have opened and taken focus")
	}

	screen := drawProperties(t, r)
	defer screen.Fini()

	x, y, _, _ := r.propertiesText.GetInnerRect()
	clickY := y + r.hashSectionRow

	consumed, _ := r.properties.MouseHandler()(tview.MouseLeftClick, tcell.NewEventMouse(x, clickY, tcell.Button1, 0), func(tview.Primitive) {})
	<-started // wait for hashFile's one-time read (see isolateHashFile) before this test can safely end
	if !consumed {
		t.Error("click on the hash line should be consumed even while the inline editor is open")
	}
	// See TestPropertiesHPressTriggersHash's own doc comment on why this
	// checks that a computation started, not a finished "MD5:" result.
	if !r.hashInProgress {
		t.Error("clicking the hash line while the inline editor is open should still have started computing the hash")
	}
	if r.activePage != propertiesPage {
		t.Errorf("activePage = %q after clicking the hash line, want Properties to still be open (%q)", r.activePage, propertiesPage)
	}
}

// TestSavingUntouchedFieldsDoesNotWriteAnything pins the user's own
// expectation ("das Fenster sollte keine Werte schreiben, wenn sie nicht
// geändert wurden"): tabbing all the way through every field stop —
// which auto-opens an inline editor pre-filled with the current value
// for Name/the octal permission/either half of Modified (see
// isAutoEditField), and commits it right back, unchanged, on the very
// next Tab — without ever typing anything new, then Save, must leave the
// file exactly as it was. savePropertiesEdit already only applies a
// field whose staged value differs from the original (see its own doc
// comment); this pins that a real Tab-through-everything sequence,
// dispatched the way real keypresses arrive rather than by calling
// internal methods directly, never disturbs that.
func TestSavingUntouchedFieldsDoesNotWriteAnything(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path
	r.openProperties()

	tab := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	for i := 0; i < len(propertyFieldOrder); i++ {
		r.properties.InputHandler()(tab, func(tview.Primitive) {})
	}

	r.savePropertiesEdit()

	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Name() != before.Name() {
		t.Errorf("name changed from %q to %q despite no edit", before.Name(), after.Name())
	}
	if after.Mode() != before.Mode() {
		t.Errorf("mode changed from %v to %v despite no edit", before.Mode(), after.Mode())
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("mtime changed from %v to %v despite no edit", before.ModTime(), after.ModTime())
	}
}

// drawProperties draws root's Properties overlay into a same-sized
// SimulationScreen, so its text's InRect/GetInnerRect have real layout to
// resolve coordinates against — the same drawnRoot helper table_click_
// test.go uses, but scoped to just the overlay under test here.
func drawProperties(t *testing.T, r *Root) tcell.SimulationScreen {
	t.Helper()

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	screen.SetSize(80, 24)
	r.properties.SetRect(0, 0, 60, 20)
	r.properties.Draw(screen)
	return screen
}

// drawPropertiesAtCurrentRect is drawProperties' counterpart for tests
// that need r.properties' own real, already-computed rect (see
// resizeProperties) left alone rather than overridden to a fixed
// 60x20. That matters specifically for the Cancel/Save row: its own
// rect (see newPropertiesView's "buttons" page, added with resize:
// false) is never touched by tview.Pages' own per-Draw resizing, so it
// stays whatever resizeProperties last computed — which, against a
// realistic (wide) panel rect and a long enough t.TempDir() path in the
// rendered "Path:" line, can end up wider than drawProperties' fixed 60
// columns. Overriding to a narrower rect then leaves the button row's
// own already-computed position extending past r.properties' new
// (narrower) bounds, so a click there gets rejected by Pages' own
// InRect check before ever reaching the button underneath it — not a
// real app bug, just this fixed-size test helper being too narrow for
// realistic content.
func drawPropertiesAtCurrentRect(t *testing.T, r *Root) tcell.SimulationScreen {
	t.Helper()

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	screen.SetSize(120, 40) // generous enough for any realistic content width against an 80x24 panel
	r.properties.Draw(screen)
	return screen
}

// TestCapturePropertiesMouseClickOnPermissionBitToggles is
// TestRenamePositionsOverRightClickedRow's counterpart for this overlay:
// a real click, dispatched through capturePropertiesMouse against a
// genuinely drawn screen, must land on the actual permission bit under
// the cursor — not some other row's worth of column math, which a
// multi-row TextView (unlike the header's single-row one) has much more
// room to get wrong.
func TestCapturePropertiesMouseClickOnPermissionBitToggles(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path
	r.openProperties()

	screen := drawProperties(t, r)
	defer screen.Fini()

	span, ok := findPropertySpan(r, fieldPermOwnerWrite)
	if !ok {
		t.Fatal("no fieldPermOwnerWrite span found")
	}
	rectX, rectY, _, _ := r.propertiesText.GetInnerRect()
	clickX, clickY := rectX+span.startCol, rectY+span.row

	action, event := r.capturePropertiesMouse(tview.MouseLeftClick, tcell.NewEventMouse(clickX, clickY, tcell.Button1, 0))
	if action != tview.MouseConsumed || event != nil {
		t.Errorf("click on a permission bit should be consumed, got action=%v event=%v", action, event)
	}
	if r.stagedMode != 0o444 { // owner-write toggled off
		t.Errorf("stagedMode = %o, want %o", r.stagedMode, 0o444)
	}
	if !r.propertiesDirty {
		t.Error("clicking a permission bit should mark Properties dirty")
	}
}

func TestPropertySpanAt(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()

	nameSpan, ok := findPropertySpan(r, fieldName)
	if !ok {
		t.Fatal("no fieldName span found")
	}

	got, ok := r.propertySpanAt(nameSpan.row, nameSpan.startCol)
	if !ok || got.field != fieldName {
		t.Errorf("propertySpanAt(%d, %d) = %+v, %v, want fieldName", nameSpan.row, nameSpan.startCol, got, ok)
	}

	if _, ok := r.propertySpanAt(nameSpan.row, nameSpan.endCol); ok {
		t.Error("propertySpanAt at the span's end column (exclusive) should not match")
	}
}

// findPropertySpan returns the first span for field in r.propertySpans.
func findPropertySpan(r *Root, field propertyField) (propertySpan, bool) {
	for _, s := range r.propertySpans {
		if s.field == field {
			return s, true
		}
	}
	return propertySpan{}, false
}

// TestPropertiesNameSpanAccountsForWideCharacters pins the fix for the
// user's own report: a file name containing double-width (e.g. CJK)
// characters must produce a fieldName span whose width matches how many
// terminal columns the name actually occupies, not its rune count —
// "文档.txt" is 6 runes but 8 terminal columns. minWidth (see
// activatePropertyField's fieldName case) masks the gap in the inline
// editor's own on-screen width for a name this short, but propertySpanAt
// (used by capturePropertiesMouse to route a click) has no such floor:
// a click on the name's real last column used to fall short of the
// (too-narrow) span and miss it entirely.
func TestPropertiesNameSpanAccountsForWideCharacters(t *testing.T) {
	const name = "文档.txt"
	dir := fixtureDir(t)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path
	r.openProperties()

	span, ok := findPropertySpan(r, fieldName)
	if !ok {
		t.Fatal("no fieldName span found")
	}

	wantWidth := tview.TaggedStringWidth(name)
	if gotWidth := span.endCol - span.startCol; gotWidth != wantWidth {
		t.Errorf("fieldName span width = %d, want %d (real display width of %q, not its rune count of %d)", gotWidth, wantWidth, name, len([]rune(name)))
	}

	// A real click, dispatched through a genuinely drawn screen, on the
	// name's own actual last column must still land inside its span.
	screen := drawProperties(t, r)
	defer screen.Fini()
	rectX, rectY, _, _ := r.propertiesText.GetInnerRect()
	clickX, clickY := rectX+span.endCol-1, rectY+span.row

	action, event := r.capturePropertiesMouse(tview.MouseLeftClick, tcell.NewEventMouse(clickX, clickY, tcell.Button1, 0))
	if action != tview.MouseConsumed || event != nil {
		t.Errorf("click on the name field's own last column should be consumed, got action=%v event=%v", action, event)
	}
	if !r.propertiesEditField.HasFocus() {
		t.Error("clicking the name field's last column should open the inline editor, not miss the span")
	}
}

func TestOpenPropertiesInitializesStagedValues(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path
	r.openProperties()

	if r.stagedName != "apple.txt" {
		t.Errorf("stagedName = %q, want %q", r.stagedName, "apple.txt")
	}
	if r.stagedMode != 0o640 {
		t.Errorf("stagedMode = %o, want %o", r.stagedMode, 0o640)
	}
	if !r.stagedMtime.Equal(r.propertiesStat.ModTime) {
		t.Errorf("stagedMtime = %v, want %v", r.stagedMtime, r.propertiesStat.ModTime)
	}
	if r.propertiesDirty {
		t.Error("propertiesDirty should be false right after opening, before any field is touched")
	}
}

func TestTogglePermBitFlipsStagedModeAndMarksDirty(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path
	r.openProperties()

	// 0644 already has owner-write set, so toggling it flips it *off*
	// (0444) — matching the user's own example ("wenn ich bei r klicke,
	// wird aus r ein -").
	r.togglePermBit(fieldPermOwnerWrite)

	const want = 0o444
	if r.stagedMode != want {
		t.Errorf("stagedMode = %o, want %o", r.stagedMode, want)
	}
	if !r.propertiesDirty {
		t.Error("toggling a permission bit should mark Properties dirty")
	}
	if !strings.Contains(r.propertiesText.GetText(true), fmt.Sprintf("(%04o)", want)) {
		t.Errorf("rendered text should show the new octal value, got:\n%s", r.propertiesText.GetText(true))
	}

	r.togglePermBit(fieldPermOwnerWrite)
	if r.stagedMode != 0o644 {
		t.Errorf("stagedMode after toggling twice = %o, want %o (back to original)", r.stagedMode, 0o644)
	}
}

// TestPropertiesEditFieldUsesFocusedColor pins the fix for the user's
// own report ("manche Felder leuchten hell auf, wenn man drin ist,
// andere aber nicht"): the shared inline edit field always carries
// theme.FocusedBackground, the same bright color the currently-focused
// field's own span in propertiesText shows (see focusTag) — before this
// fix it used the plainer editableBackgroundColor instead, so a field
// that opened its own editor immediately (Name/Date/Time/the octal
// value) visually covered its own highlight with a duller color, while
// a field that doesn't auto-open (a permission bit, Owner/Group) kept
// showing the brighter one — the exact inconsistency being pinned here.
func TestPropertiesEditFieldUsesFocusedColor(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	_, bg, _ := r.propertiesEditField.GetFieldStyle().Decompose()
	if bg != r.theme.FocusedBackground {
		t.Errorf("propertiesEditField's field background = %v, want theme.FocusedBackground (%v)", bg, r.theme.FocusedBackground)
	}
}

// TestActivatePropertyFieldMarksDirty pins the literal "as soon as you
// click one to edit" behavior: dirty becomes true, and the Cancel/Save
// row is shown, on the click itself — not only once an actual change is
// confirmed.
func TestActivatePropertyFieldMarksDirty(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()

	if r.propertiesDirty {
		t.Fatal("setup: should not be dirty before any click")
	}

	span, ok := findPropertySpan(r, fieldName)
	if !ok {
		t.Fatal("no fieldName span found")
	}
	r.activatePropertyField(span)

	if !r.propertiesDirty {
		t.Error("clicking a field should mark Properties dirty immediately")
	}
	if got := r.propertiesEditField.GetText(); got != "apple.txt" {
		t.Errorf("edit field pre-filled with %q, want %q", got, "apple.txt")
	}
}

func TestFinishPropertyEditAppliesNameOnEnter(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()

	span, _ := findPropertySpan(r, fieldName)
	r.activatePropertyField(span)
	r.propertiesEditField.SetText("renamed.txt")
	r.finishPropertyEdit(tcell.KeyEnter)

	if r.stagedName != "renamed.txt" {
		t.Errorf("stagedName = %q, want %q", r.stagedName, "renamed.txt")
	}
	if !strings.Contains(r.propertiesText.GetText(true), "renamed.txt") {
		t.Error("rendered text should reflect the staged name")
	}
}

func TestFinishPropertyEditEscapeDiscardsFieldEdit(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()

	span, _ := findPropertySpan(r, fieldName)
	r.activatePropertyField(span)
	r.propertiesEditField.SetText("should-not-apply.txt")
	r.finishPropertyEdit(tcell.KeyEscape)

	if r.stagedName != "apple.txt" {
		t.Errorf("stagedName = %q, want unchanged %q", r.stagedName, "apple.txt")
	}
}

// TestFinishPropertyEditTabCommitsAndAdvances pins the fix for the bug
// the user reported: leaving a field via Tab (the primary way to move
// through fields, now that Properties has real keyboard navigation — see
// capturePropertiesKey) used to discard whatever was just typed, exactly
// like Escape — from the user's own perspective, the edited value
// appeared to silently reset itself. Tab must commit, the same as Enter,
// and then continue on to the next field stop.
func TestFinishPropertyEditTabCommitsAndAdvances(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()
	r.stagedMtime = time.Date(2020, time.January, 1, 14, 30, 45, 0, time.Local)

	dateSpan, _ := findPropertySpan(r, fieldMtimeDate)
	timeIdx, _ := propertyFieldIndex(fieldMtimeTime)
	r.activatePropertyField(dateSpan)
	r.propertiesEditField.SetText("2026-08-05")
	r.finishPropertyEdit(tcell.KeyTab)

	wantDate := time.Date(2026, time.August, 5, 14, 30, 45, 0, time.Local)
	if !r.stagedMtime.Equal(wantDate) {
		t.Errorf("stagedMtime = %v, want %v — Tab should commit the typed date, not discard it", r.stagedMtime, wantDate)
	}
	if r.propertiesFocusIndex != timeIdx {
		t.Errorf("propertiesFocusIndex = %d, want %d (the Time field) after Tab out of Date", r.propertiesFocusIndex, timeIdx)
	}
}

// TestFinishPropertyEditBacktabCommitsAndRetreats is
// TestFinishPropertyEditTabCommitsAndAdvances's Backtab counterpart.
func TestFinishPropertyEditBacktabCommitsAndRetreats(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()
	r.stagedMtime = time.Date(2020, time.January, 1, 14, 30, 45, 0, time.Local)

	timeSpan, _ := findPropertySpan(r, fieldMtimeTime)
	dateIdx, _ := propertyFieldIndex(fieldMtimeDate)
	r.activatePropertyField(timeSpan)
	r.propertiesEditField.SetText("09:05:03")
	r.finishPropertyEdit(tcell.KeyBacktab)

	wantTime := time.Date(2020, time.January, 1, 9, 5, 3, 0, time.Local)
	if !r.stagedMtime.Equal(wantTime) {
		t.Errorf("stagedMtime = %v, want %v — Backtab should commit the typed time, not discard it", r.stagedMtime, wantTime)
	}
	if r.propertiesFocusIndex != dateIdx {
		t.Errorf("propertiesFocusIndex = %d, want %d (the Date field) after Backtab out of Time", r.propertiesFocusIndex, dateIdx)
	}
}

// TestActivatePropertyFieldOctalOpensInlineEditor pins that clicking the
// octal permission value opens the same shared inline editor Name/
// Modified use, pre-filled with the current mode as 3 octal digits — the
// direct-entry alternative to toggling bits one at a time the user asked
// for.
func TestActivatePropertyFieldOctalOpensInlineEditor(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path
	r.openProperties()

	span, ok := findPropertySpan(r, fieldPermOctal)
	if !ok {
		t.Fatal("no fieldPermOctal span found")
	}
	r.activatePropertyField(span)

	if got := r.propertiesEditField.GetText(); got != "0644" {
		t.Errorf("edit field pre-filled with %q, want %q", got, "0644")
	}
}

// TestFinishPropertyEditOctalAppliesValidValue pins that typing a valid
// octal value into that field and confirming (Enter) stages the whole
// mode at once.
func TestFinishPropertyEditOctalAppliesValidValue(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path
	r.openProperties()

	span, _ := findPropertySpan(r, fieldPermOctal)
	r.activatePropertyField(span)
	r.propertiesEditField.SetText("755")
	r.finishPropertyEdit(tcell.KeyEnter)

	if r.stagedMode != 0o755 {
		t.Errorf("stagedMode = %o, want 0755", r.stagedMode)
	}
	if !strings.Contains(r.propertiesText.GetText(true), "(0755)") {
		t.Error("rendered text should show the new octal value")
	}
}

// TestFinishPropertyEditOctalInvalidValueKeepsPreviousStaged mirrors
// TestFinishPropertyEditInvalidDateKeepsPreviousStaged: invalid octal
// input (out of range, or not octal at all) is silently discarded, the
// same convention as an invalid date/time.
func TestFinishPropertyEditOctalInvalidValueKeepsPreviousStaged(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path
	r.openProperties()
	original := r.stagedMode

	span, _ := findPropertySpan(r, fieldPermOctal)
	r.activatePropertyField(span)
	r.propertiesEditField.SetText("999") // not valid octal
	r.finishPropertyEdit(tcell.KeyEnter)

	if r.stagedMode != original {
		t.Errorf("stagedMode = %o, want unchanged %o after invalid input", r.stagedMode, original)
	}
}

// TestSavePropertiesEditAppliesOctalEdit is the octal field's end-to-end
// counterpart to TestSavePropertiesEditAppliesAllStagedChanges: editing
// it and saving must actually chmod the real file.
func TestSavePropertiesEditAppliesOctalEdit(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path
	r.openProperties()

	span, _ := findPropertySpan(r, fieldPermOctal)
	r.activatePropertyField(span)
	r.propertiesEditField.SetText("600")
	r.finishPropertyEdit(tcell.KeyEnter)

	r.savePropertiesEdit()

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 0600", fi.Mode().Perm())
	}
}

func TestFinishPropertyEditDateValidShorthandPadsAndPreservesTime(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()
	r.stagedMtime = time.Date(2020, time.January, 1, 14, 30, 45, 0, time.Local)

	span, _ := findPropertySpan(r, fieldMtimeDate)
	r.activatePropertyField(span)
	r.propertiesEditField.SetText("2026-8-5") // shorthand: 1-digit month/day
	r.finishPropertyEdit(tcell.KeyEnter)

	want := time.Date(2026, time.August, 5, 14, 30, 45, 0, time.Local)
	if !r.stagedMtime.Equal(want) {
		t.Errorf("stagedMtime = %v, want %v", r.stagedMtime, want)
	}
	if !strings.Contains(r.propertiesText.GetText(true), "2026-08-05") {
		t.Error("rendered text should show the zero-padded date")
	}
}

func TestFinishPropertyEditTimeValidShorthandPadsAndPreservesDate(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()
	r.stagedMtime = time.Date(2020, time.January, 1, 0, 0, 0, 0, time.Local)

	span, _ := findPropertySpan(r, fieldMtimeTime)
	r.activatePropertyField(span)
	r.propertiesEditField.SetText("9:5:3")
	r.finishPropertyEdit(tcell.KeyEnter)

	want := time.Date(2020, time.January, 1, 9, 5, 3, 0, time.Local)
	if !r.stagedMtime.Equal(want) {
		t.Errorf("stagedMtime = %v, want %v", r.stagedMtime, want)
	}
	if !strings.Contains(r.propertiesText.GetText(true), "09:05:03") {
		t.Error("rendered text should show the zero-padded time")
	}
}

func TestFinishPropertyEditInvalidDateKeepsPreviousStaged(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()
	original := r.stagedMtime

	span, _ := findPropertySpan(r, fieldMtimeDate)
	r.activatePropertyField(span)
	r.propertiesEditField.SetText("2026-02-30") // no such day
	r.finishPropertyEdit(tcell.KeyEnter)

	if !r.stagedMtime.Equal(original) {
		t.Errorf("stagedMtime = %v, want unchanged %v after invalid input", r.stagedMtime, original)
	}
	if r.activePage == errorPage {
		t.Error("invalid date input should not open the error overlay (see finishPropertyEdit's doc comment)")
	}
}

func TestParseDate(t *testing.T) {
	base := time.Date(2000, time.January, 1, 10, 20, 30, 0, time.Local)

	tests := []struct {
		in      string
		want    time.Time
		wantErr bool
	}{
		{"2026-08-05", time.Date(2026, time.August, 5, 10, 20, 30, 0, time.Local), false},
		{"2026-8-5", time.Date(2026, time.August, 5, 10, 20, 30, 0, time.Local), false}, // shorthand accepted
		{"2026-13-01", time.Time{}, true},                                               // no month 13
		{"2026-02-30", time.Time{}, true},                                               // no Feb 30
		{"not-a-date", time.Time{}, true},
		{"2026-08", time.Time{}, true}, // missing day
	}

	for _, tt := range tests {
		got, err := parseDate(tt.in, base)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseDate(%q) = %v, want an error", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseDate(%q): unexpected error %v", tt.in, err)
			continue
		}
		if !got.Equal(tt.want) {
			t.Errorf("parseDate(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestParseTime(t *testing.T) {
	base := time.Date(2026, time.August, 5, 0, 0, 0, 0, time.Local)

	tests := []struct {
		in      string
		want    time.Time
		wantErr bool
	}{
		{"14:05:09", time.Date(2026, time.August, 5, 14, 5, 9, 0, time.Local), false},
		{"9:5:3", time.Date(2026, time.August, 5, 9, 5, 3, 0, time.Local), false}, // shorthand accepted
		{"25:00:00", time.Time{}, true},                                           // no hour 25
		{"12:60:00", time.Time{}, true},                                           // no minute 60
		{"garbage", time.Time{}, true},
		{"14:05", time.Time{}, true}, // missing seconds
	}

	for _, tt := range tests {
		got, err := parseTime(tt.in, base)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseTime(%q) = %v, want an error", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseTime(%q): unexpected error %v", tt.in, err)
			continue
		}
		if !got.Equal(tt.want) {
			t.Errorf("parseTime(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// TestSavePropertiesEditAppliesAllStagedChanges is the end-to-end test
// for the risky part of this feature: Save must actually rename, chmod,
// and touch the real file to match what was staged, and only then close
// the overlay and reload the panel.
func TestSavePropertiesEditAppliesAllStagedChanges(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path
	r.openProperties()

	r.togglePermBit(fieldPermOtherRead) // 0644 -> 0640

	nameSpan, _ := findPropertySpan(r, fieldName)
	r.activatePropertyField(nameSpan)
	r.propertiesEditField.SetText("saved.txt")
	r.finishPropertyEdit(tcell.KeyEnter)

	wantMtime := time.Date(2021, time.June, 15, 8, 0, 0, 0, time.Local)
	dateSpan, _ := findPropertySpan(r, fieldMtimeDate)
	r.activatePropertyField(dateSpan)
	r.propertiesEditField.SetText(wantMtime.Format("2006-01-02"))
	r.finishPropertyEdit(tcell.KeyEnter)
	timeSpan, _ := findPropertySpan(r, fieldMtimeTime)
	r.activatePropertyField(timeSpan)
	r.propertiesEditField.SetText(wantMtime.Format("15:04:05"))
	r.finishPropertyEdit(tcell.KeyEnter)

	r.savePropertiesEdit()

	if r.activePage != "" {
		t.Errorf("activePage = %q after Save, want closed", r.activePage)
	}

	newPath := filepath.Join(dir, "saved.txt")
	fi, err := os.Stat(newPath)
	if err != nil {
		t.Fatalf("saved.txt should exist after Save: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("apple.txt should no longer exist after renaming, stat err = %v", err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %o, want 0640", fi.Mode().Perm())
	}
	if !fi.ModTime().Equal(wantMtime) {
		t.Errorf("ModTime = %v, want %v", fi.ModTime(), wantMtime)
	}
}

// TestSavePropertiesEditNoopWhenNothingChanged pins that Save works
// cleanly even when nothing was actually edited (dirty could still be
// true from a field having been clicked into and back out of unchanged).
func TestSavePropertiesEditNoopWhenNothingChanged(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path
	r.openProperties()

	r.savePropertiesEdit()

	if r.activePage != "" {
		t.Errorf("activePage = %q after Save, want closed", r.activePage)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("apple.txt should still exist unchanged: %v", err)
	}
}

// TestSavePropertiesEditAppliesOwnerGroupChange pins that Save actually
// applies a changed stagedOwner/stagedGroup via fsops.Chown — kept
// privilege-independent the same way TestChownNoopToOwnUser in fsops is:
// staging the process's own uid/gid *as a numeric string* differs from
// propertiesStat.Owner/Group (which are resolved names), so the "did
// this change" comparison sees a real change and Chown actually runs,
// but resolves back to a no-op requiring no special privileges.
func TestSavePropertiesEditAppliesOwnerGroupChange(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path
	r.openProperties()

	r.stagedOwner = strconv.Itoa(os.Getuid())
	r.stagedGroup = strconv.Itoa(os.Getgid())
	r.markPropertiesDirty()

	r.savePropertiesEdit()

	if r.activePage == errorPage {
		t.Fatalf("Save should have succeeded, got error overlay: %q", r.errorView.GetText(true))
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("can't inspect raw uid/gid on this platform")
	}
	if int(stat.Uid) != os.Getuid() || int(stat.Gid) != os.Getgid() {
		t.Errorf("file uid:gid = %d:%d, want %d:%d", stat.Uid, stat.Gid, os.Getuid(), os.Getgid())
	}
}

// TestCancelPropertiesEditDiscardsChanges pins that Cancel never touches
// the real file, even after a permission bit was toggled.
func TestCancelPropertiesEditDiscardsChanges(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path
	r.openProperties()

	r.togglePermBit(fieldPermOtherRead)
	if !r.propertiesDirty {
		t.Fatal("setup: should be dirty after toggling a bit")
	}

	r.cancelPropertiesEdit()

	if r.activePage != "" {
		t.Errorf("activePage = %q after Cancel, want closed", r.activePage)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("mode = %o after Cancel, want unchanged 0644", fi.Mode().Perm())
	}
}

// TestPropertiesCancelButtonClickStillClosesOverlay pins the fix for a
// real regression: hashesMouseCapture (installed on r.properties itself
// — see its own doc comment) used to swallow a click meant for Cancel
// into computeHashes instead, and leave Properties open — because
// tview.Pages gives every visible page (propertiesText included) the
// same rect as the Pages itself, covering the Cancel/Save row too, not
// just propertiesText's own content lines. Dispatched through
// r.properties.MouseHandler() the way a real click arrives — the
// existing TestCancelPropertiesEditDiscardsChanges calls
// cancelPropertiesEdit directly, which bypasses this routing entirely
// and so never would have caught it.
func TestPropertiesCancelButtonClickStillClosesOverlay(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.SetRect(0, 0, 80, 24)
	r.target = path
	r.openProperties()

	screen := drawPropertiesAtCurrentRect(t, r)
	defer screen.Fini()

	x, y, w, h := r.propertiesCancelBtn.GetRect()
	clickX, clickY := x+w/2, y+h/2

	consumed, _ := r.properties.MouseHandler()(tview.MouseLeftClick, tcell.NewEventMouse(clickX, clickY, tcell.Button1, 0), func(tview.Primitive) {})
	if !consumed {
		t.Error("click on Cancel should be consumed")
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q after clicking Cancel, want closed", r.activePage)
	}
	if strings.Contains(r.propertiesText.GetText(true), "MD5:") {
		t.Error("clicking Cancel should not have computed hashes")
	}
}

// TestPropertiesSaveButtonClickStillSaves is
// TestPropertiesCancelButtonClickStillClosesOverlay's Save-button
// counterpart, pinning the same fix.
func TestPropertiesSaveButtonClickStillSaves(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.SetRect(0, 0, 80, 24)
	r.target = path
	r.openProperties()
	r.togglePermBit(fieldPermOtherRead) // a real, staged change for Save to actually apply

	screen := drawPropertiesAtCurrentRect(t, r)
	defer screen.Fini()

	x, y, w, h := r.propertiesSaveBtn.GetRect()
	clickX, clickY := x+w/2, y+h/2

	consumed, _ := r.properties.MouseHandler()(tview.MouseLeftClick, tcell.NewEventMouse(clickX, clickY, tcell.Button1, 0), func(tview.Primitive) {})
	if !consumed {
		t.Error("click on Save should be consumed")
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q after clicking Save, want closed", r.activePage)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %o after Save, want the staged change applied (0640 — other-read toggled off from 0644)", fi.Mode().Perm())
	}
}

// TestCaptureOutsideClickBlockedWhilePropertiesDirty pins the user's own
// requirement: once a field's been touched, a click outside Properties
// must not close it — Cancel or Save is the only way out from there.
func TestCaptureOutsideClickBlockedWhilePropertiesDirty(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()
	r.togglePermBit(fieldPermOtherRead) // any touch marks it dirty

	x, y := outsidePropertiesClick(r)
	action, event := r.captureOutsideClick(tview.MouseLeftClick, tcell.NewEventMouse(x, y, tcell.Button1, 0))

	if r.activePage != propertiesPage {
		t.Errorf("activePage = %q, want Properties to still be open", r.activePage)
	}
	if action != tview.MouseConsumed || event != nil {
		t.Errorf("outside click while dirty should be consumed and swallowed, got action=%v event=%v", action, event)
	}
}

// TestCaptureOutsideClickClosesPropertiesBeforeAnyEdit pins that nothing
// changes for the "haven't touched anything yet" case — the existing
// click-outside-closes behavior every other overlay already has.
func TestCaptureOutsideClickClosesPropertiesBeforeAnyEdit(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()

	x, y := outsidePropertiesClick(r)
	r.captureOutsideClick(tview.MouseLeftClick, tcell.NewEventMouse(x, y, tcell.Button1, 0))

	if r.activePage != "" {
		t.Errorf("activePage = %q, want Properties closed by the outside click", r.activePage)
	}
}

// outsidePropertiesClick returns a screen position guaranteed to fall
// outside r.properties' actual rect — computed from that rect rather
// than assumed (e.g. (0,0)), since r.properties is positioned relative to
// r.menu's own rect (see openProperties), which is wherever it happens
// to default to when a test opens Properties directly instead of through
// a real right-click (showMenu) first.
func outsidePropertiesClick(r *Root) (x, y int) {
	px, py, pw, ph := r.properties.GetRect()
	return px + pw + 10, py + ph + 10
}

// TestPropertiesButtonsVisibleImmediately pins the fix for the stray
// placeholder blank row bug the user reported, and the closely related
// request behind it: Cancel/Save show up the moment Properties opens,
// not only once a field's been touched — resizeProperties always
// reserved a row for them, but they used to stay hidden (and that row
// blank) until markPropertiesDirty first ran.
func TestPropertiesButtonsVisibleImmediately(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()

	if r.propertiesDirty {
		t.Fatal("setup: should not be dirty right after opening")
	}
	visible := r.properties.GetPageNames(true)
	for _, p := range visible {
		if p == "buttons" {
			return
		}
	}
	t.Errorf("buttons page should be visible right after opening, visible pages: %v", visible)
}

// TestCapturePropertiesKeyTabNavigatesInsteadOfClosing pins the fix for
// the bug the user reported ("Datum und Uhrzeit lassen sich nicht
// ändern"): Tab used to fall through to TextView's own default handling
// and close Properties outright — discarding every staged edit with no
// way back — instead of moving keyboard focus to the next field, the way
// it's supposed to.
func TestCapturePropertiesKeyTabNavigatesInsteadOfClosing(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()
	r.togglePermBit(fieldPermOtherRead) // any touch marks it dirty, same as the real bug report's state

	if got := r.capturePropertiesKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)); got != nil {
		t.Error("capturePropertiesKey should consume Tab")
	}
	if r.activePage != propertiesPage {
		t.Errorf("activePage = %q after Tab, want Properties to stay open", r.activePage)
	}
	if r.propertiesFocusIndex != 0 {
		t.Errorf("propertiesFocusIndex = %d after Tab from nothing focused, want 0 (Name)", r.propertiesFocusIndex)
	}
}

// TestCapturePropertiesKeyEnterActivatesFocusedField is
// TestCapturePropertiesKeyTabNavigatesInsteadOfClosing's Enter
// counterpart: once Tab has moved focus onto a field, Enter activates it
// (the same as clicking it) instead of closing the overlay.
func TestCapturePropertiesKeyEnterActivatesFocusedField(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()

	r.capturePropertiesKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)) // -> Name
	if got := r.capturePropertiesKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)); got != nil {
		t.Error("capturePropertiesKey should consume Enter")
	}

	if got := r.propertiesEditField.GetText(); got != "apple.txt" {
		t.Errorf("Enter on the focused Name field should open the inline editor pre-filled with %q, got %q", "apple.txt", got)
	}
	if r.activePage != propertiesPage {
		t.Errorf("activePage = %q, want Properties to stay open", r.activePage)
	}
}

// TestPropertiesFocusCyclesFieldsThenButtonsThenWraps drives Tab through
// the real dispatch path all the way around the loop once: repeated
// capturePropertiesKey calls (propertiesText has real focus) through
// every field stop in propertyFieldOrder, then — once focus reaches a
// button — that button's own InputHandler (see newPropertiesButtons'
// SetExitFunc), through Cancel, Save, and the wrap back to the first
// field.
// propertiesEditFieldOpen reports whether the shared inline text editor
// (see activateInlineTextField) is currently the one showing — used
// below to decide whether the next Tab/Backtab should be dispatched
// through it (finishPropertyEdit, the same as InputField's own
// SetDoneFunc would) or through capturePropertiesKey (propertiesText's
// own navigation), matching whichever one actually holds real keyboard
// focus in the real app at that point.
func propertiesEditFieldOpen(r *Root) bool {
	for _, p := range r.properties.GetPageNames(true) {
		if p == "editfield" {
			return true
		}
	}
	return false
}

// TestPropertiesFocusCyclesFieldsThenButtonsThenWraps drives Tab all the
// way around the loop once, dispatching each press through whichever
// widget would really have focus at that point — propertiesEditField
// once a field has auto-opened (see isAutoEditField), propertiesText
// otherwise — through every field stop in propertyFieldOrder, Cancel,
// Save, and the wrap back to the first field (auto-opening it again).
func TestPropertiesFocusCyclesFieldsThenButtonsThenWraps(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()

	n := len(propertyFieldOrder)
	tab := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	noSetFocus := func(tview.Primitive) {}

	tabOnce := func() {
		if propertiesEditFieldOpen(r) {
			r.finishPropertyEdit(tcell.KeyTab) // commit + advance, same as InputField's own SetDoneFunc
		} else {
			r.capturePropertiesKey(tab)
		}
	}

	for i := 0; i < n; i++ {
		tabOnce()
		if r.propertiesFocusIndex != i {
			t.Fatalf("after %d Tabs, propertiesFocusIndex = %d, want %d", i+1, r.propertiesFocusIndex, i)
		}
	}

	tabOnce() // off the last field -> Cancel
	if r.propertiesFocusIndex != n {
		t.Fatalf("propertiesFocusIndex = %d, want Cancel (%d)", r.propertiesFocusIndex, n)
	}

	r.propertiesCancelBtn.InputHandler()(tab, noSetFocus) // Cancel -> Save
	if r.propertiesFocusIndex != n+1 {
		t.Fatalf("propertiesFocusIndex = %d, want Save (%d)", r.propertiesFocusIndex, n+1)
	}

	r.propertiesSaveBtn.InputHandler()(tab, noSetFocus) // Save -> wraps to the first field
	if r.propertiesFocusIndex != 0 {
		t.Errorf("propertiesFocusIndex = %d after wrapping, want 0", r.propertiesFocusIndex)
	}
	if !propertiesEditFieldOpen(r) {
		t.Error("wrapping back to Name should auto-open its inline editor again")
	}
}

// TestSaveReachableViaKeyboardAfterEditingDate is the end-to-end
// regression test for the bug report itself: edit the Modified date via
// the inline field, then navigate onward and confirm Save using nothing
// but the real key-dispatch path (capturePropertiesKey, then the
// buttons' own InputHandler) — the edit must actually reach disk, not
// get silently discarded by Tab/Enter closing the overlay along the way.
func TestSaveReachableViaKeyboardAfterEditingDate(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path
	r.openProperties()

	dateSpan, ok := findPropertySpan(r, fieldMtimeDate)
	if !ok {
		t.Fatal("no fieldMtimeDate span found")
	}
	r.activatePropertyField(dateSpan) // click the date field, same as a real click
	wantMtime := time.Date(2021, time.June, 15, r.stagedMtime.Hour(), r.stagedMtime.Minute(), r.stagedMtime.Second(), 0, time.Local)
	r.propertiesEditField.SetText(wantMtime.Format("2006-01-02"))
	r.finishPropertyEdit(tcell.KeyEnter) // commit, same as a real Enter — focus returns to propertiesText

	tab := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	enter := tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
	noSetFocus := func(tview.Primitive) {}

	r.capturePropertiesKey(tab)                           // date -> time
	r.capturePropertiesKey(tab)                           // time -> Cancel
	r.propertiesCancelBtn.InputHandler()(tab, noSetFocus) // Cancel -> Save
	r.propertiesSaveBtn.InputHandler()(enter, noSetFocus) // activate Save

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.ModTime().Equal(wantMtime) {
		t.Errorf("ModTime = %v, want %v — the date edit should have survived all the way to Save", fi.ModTime(), wantMtime)
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q after Save, want closed", r.activePage)
	}
}

// TestSpaceActivatesFocusedButton pins the user's explicit request that
// either Enter or Space confirm a focused Cancel/Save button.
func TestSpaceActivatesFocusedButton(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path
	r.openProperties()
	r.togglePermBit(fieldPermOtherRead) // 0644 -> 0640, something for Save to actually apply

	r.setPropertiesFocus(len(propertyFieldOrder) + 1) // Save

	space := tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone)
	if got := r.propertiesSaveBtn.GetInputCapture()(space); got != nil {
		t.Error("space should be consumed by the Save button's input capture")
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %o, want 0640 — space on the focused Save button should have saved", fi.Mode().Perm())
	}
}

// TestTabAutoOpensTextFieldsButNotTogglesOrPicker pins isAutoEditField's
// exact split, per the user's own request: landing keyboard focus on
// Name/the octal value/Date/Time opens the inline editor immediately —
// no separate Enter needed just to start typing — but landing on a
// permission bit or Owner/Group does not, since those aren't free text
// (a toggle, or a heavier picker overlay not appropriate to pop open
// just by tabbing past on the way to some other field).
func TestTabAutoOpensTextFieldsButNotTogglesOrPicker(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()

	tab := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)

	r.capturePropertiesKey(tab) // -> Name (index 0), auto-edit
	if !propertiesEditFieldOpen(r) {
		t.Error("landing on Name should auto-open its inline editor")
	}

	r.finishPropertyEdit(tcell.KeyTab) // commit + advance -> first permission bit
	if propertiesEditFieldOpen(r) {
		t.Error("landing on a permission bit should not auto-open the inline editor")
	}
	if r.activePage != propertiesPage {
		t.Errorf("activePage = %q, want Properties still open", r.activePage)
	}

	octalIdx, _ := propertyFieldIndex(fieldPermOctal)
	r.setPropertiesFocus(octalIdx)
	if !propertiesEditFieldOpen(r) {
		t.Error("landing on the octal value should auto-open its inline editor")
	}

	r.finishPropertyEdit(tcell.KeyEnter) // conclude editing, stay on the octal field

	ownerIdx, _ := propertyFieldIndex(fieldOwner)
	r.setPropertiesFocus(ownerIdx)
	if propertiesEditFieldOpen(r) {
		t.Error("landing on Owner should not auto-open the inline editor (it opens the picker instead, on an explicit key/click)")
	}
}

// TestFinishPropertyEditEnterConcludesWithoutReopening pins the
// subtlety behind "jedes Feld braucht für jede Eingabe ein eigenes
// Return, um die Eingabe abzuschließen": pressing Enter while editing an
// auto-edit field must commit and *close* the editor, not immediately
// reopen it again on the very same keystroke — landing on such a field
// via a fresh Tab auto-opens it, but concluding an edit with its own
// Enter is a deliberate "I'm done with this one" action, not something
// that should loop back into edit mode by itself.
func TestFinishPropertyEditEnterConcludesWithoutReopening(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()

	nameSpan, _ := findPropertySpan(r, fieldName)
	r.activatePropertyField(nameSpan)
	r.propertiesEditField.SetText("renamed.txt")
	r.finishPropertyEdit(tcell.KeyEnter)

	if r.stagedName != "renamed.txt" {
		t.Errorf("stagedName = %q, want %q", r.stagedName, "renamed.txt")
	}
	if propertiesEditFieldOpen(r) {
		t.Error("Enter should close the inline editor, not immediately reopen it")
	}
	nameIdx, _ := propertyFieldIndex(fieldName)
	if r.propertiesFocusIndex != nameIdx {
		t.Errorf("propertiesFocusIndex = %d, want to stay on Name (%d)", r.propertiesFocusIndex, nameIdx)
	}
}

// TestPermBitKeyboardShortcuts pins the three keys the user asked for,
// alongside the existing Space/Enter toggle: the matching letter (r/w/x
// — see permFieldLetter) sets a permission bit on; Delete or '-' clears
// it; a non-matching letter does nothing.
func TestPermBitKeyboardShortcuts(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path
	r.openProperties()

	idx, _ := propertyFieldIndex(fieldPermOwnerRead)
	r.setPropertiesFocus(idx)

	// The matching letter ('r' for a read bit) sets it on.
	if got := r.capturePropertiesKey(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModNone)); got != nil {
		t.Error("the matching letter should be consumed")
	}
	if r.stagedMode != 0o400 {
		t.Errorf("stagedMode = %o, want 0400 after 'r'", r.stagedMode)
	}

	// A non-matching letter ('w' on a read bit) does nothing.
	if got := r.capturePropertiesKey(tcell.NewEventKey(tcell.KeyRune, 'w', tcell.ModNone)); got == nil {
		t.Error("a non-matching letter should not be consumed")
	}
	if r.stagedMode != 0o400 {
		t.Errorf("stagedMode = %o, want unchanged 0400 after a non-matching letter", r.stagedMode)
	}

	// Delete clears it.
	r.capturePropertiesKey(tcell.NewEventKey(tcell.KeyDelete, 0, tcell.ModNone))
	if r.stagedMode != 0 {
		t.Errorf("stagedMode = %o, want 0 after Delete", r.stagedMode)
	}

	// '-' also clears it (a no-op here, it's already off, but pins the key works).
	r.setPermBit(fieldPermOwnerRead, true) // back on, so '-' has something to clear
	r.capturePropertiesKey(tcell.NewEventKey(tcell.KeyRune, '-', tcell.ModNone))
	if r.stagedMode != 0 {
		t.Errorf("stagedMode = %o, want 0 after '-'", r.stagedMode)
	}

	// Space toggles, same as Enter.
	r.capturePropertiesKey(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone))
	if r.stagedMode != 0o400 {
		t.Errorf("stagedMode = %o, want 0400 after Space toggles it on", r.stagedMode)
	}
}
