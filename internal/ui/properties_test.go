package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestFormatInfoIncludesLinkTargetOnlyForSymlinks(t *testing.T) {
	file := formatInfo(fsops.Info{Name: "a.txt", ModTime: time.Now()})
	if strings.Contains(file, "Link target") {
		t.Error("a regular file's Info should not mention a link target")
	}

	link := formatInfo(fsops.Info{
		Name:       "b.txt",
		IsSymlink:  true,
		LinkTarget: "/somewhere/else",
		ModTime:    time.Now(),
	})
	wantLinkLine := fmt.Sprintf("%-13s%s", "Link target:", "/somewhere/else")
	if !strings.Contains(link, wantLinkLine) {
		t.Errorf("symlink Info should contain %q, got:\n%s", wantLinkLine, link)
	}
	wantTypeLine := fmt.Sprintf("%-13s%s", "Type:", "symlink")
	if !strings.Contains(link, wantTypeLine) {
		t.Errorf("symlink Info should contain %q, got:\n%s", wantTypeLine, link)
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

// TestFormatInfoShowsLinksOnlyWhenSharedAndNotForDirs pins the "Links"
// field's visibility rule: only shown for a non-directory with Nlink > 1
// (content that exists under another name too) — not for an ordinary
// single-link file, and not for a directory even though directories
// always have Nlink >= 2 trivially.
func TestFormatInfoShowsLinksOnlyWhenSharedAndNotForDirs(t *testing.T) {
	single := formatInfo(fsops.Info{Name: "a.txt", Nlink: 1, ModTime: time.Now()})
	if strings.Contains(single, "Links") {
		t.Errorf("a single-link file should not mention Links, got:\n%s", single)
	}

	shared := formatInfo(fsops.Info{Name: "b.txt", Nlink: 2, ModTime: time.Now()})
	if !strings.Contains(shared, "Links") {
		t.Errorf("a file with Nlink=2 should mention Links, got:\n%s", shared)
	}

	dir := formatInfo(fsops.Info{Name: "c", IsDir: true, Nlink: 2, ModTime: time.Now()})
	if strings.Contains(dir, "Links") {
		t.Errorf("a directory should not mention Links even with Nlink > 1, got:\n%s", dir)
	}
}

// TestFormatInfoShowsMountPoint pins that "Mount point: yes" only
// appears when Info.MountPoint is true.
func TestFormatInfoShowsMountPoint(t *testing.T) {
	plain := formatInfo(fsops.Info{Name: "a", IsDir: true, ModTime: time.Now()})
	if strings.Contains(plain, "Mount point") {
		t.Errorf("an ordinary directory should not mention a mount point, got:\n%s", plain)
	}

	mounted := formatInfo(fsops.Info{Name: "b", IsDir: true, MountPoint: true, ModTime: time.Now()})
	wantLine := fmt.Sprintf("%-13s%s", "Mount point:", "yes")
	if !strings.Contains(mounted, wantLine) {
		t.Errorf("a mount point should say so (want %q), got:\n%s", wantLine, mounted)
	}
}

// TestRenderInfoShowsChainForMultiHopSymlink exercises the full path
// from a real multi-hop symlink on disk through Root.openInfo to the
// rendered "Chain" line.
func TestRenderInfoShowsChainForMultiHopSymlink(t *testing.T) {
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
	r.openInfo()

	text := r.info.GetText(true)
	wantChain := fmt.Sprintf("%s -> %s (file)", mid, final)
	if !strings.Contains(text, wantChain) {
		t.Errorf("Info text should contain the chain %q, got:\n%s", wantChain, text)
	}
}

// TestRenderInfoOmitsChainForSingleHopSymlink pins that a simple,
// non-chained symlink doesn't get a redundant "Chain" line — "Link
// target" and "Type" already fully describe it.
func TestRenderInfoOmitsChainForSingleHopSymlink(t *testing.T) {
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
	r.openInfo()

	// "Chain:" (with the colon infoField adds), not just "Chain" — the
	// bare word is a false-positive trap here: t.TempDir() names the
	// directory after this very test function, and "...OmitsChainFor..."
	// contains "Chain" too, via the Path/Link target fields.
	if text := r.info.GetText(true); strings.Contains(text, "Chain:") {
		t.Errorf("a single-hop symlink should not show a Chain line, got:\n%s", text)
	}
}

// TestComputeHashesUpdatesInfoText exercises the full path: openInfo
// shows the hint, computeHashes replaces it with the real digests.
func TestComputeHashesUpdatesInfoText(t *testing.T) {
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
	r.openInfo()

	before := r.info.GetText(true)
	if !strings.Contains(before, "Press h or click here") {
		t.Errorf("Info text before computing hashes should show the hint, got:\n%s", before)
	}

	r.computeHashes()

	after := r.info.GetText(true)
	if !strings.Contains(after, "5eb63bbbe01eeed093cb22bb8f5acdc3") { // MD5("hello world")
		t.Errorf("Info text after computing hashes should show the MD5 digest, got:\n%s", after)
	}
	if !strings.Contains(after, "SHA-256") {
		t.Errorf("Info text after computing hashes should label the SHA-256 line, got:\n%s", after)
	}
}

// TestComputeHashesSkipsDirectories pins that directories neither offer
// nor accept the hash action — see fsops.Hash's own doc comment on why.
func TestComputeHashesSkipsDirectories(t *testing.T) {
	dir := fixtureDir(t)

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "app-data")
	r.openInfo()

	text := r.info.GetText(true)
	if strings.Contains(text, "Press h or click here") {
		t.Errorf("a directory's Info should not offer to compute a hash, got:\n%s", text)
	}

	r.computeHashes()
	if r.info.GetText(true) != text {
		t.Error("computeHashes on a directory should not change the Info text")
	}
	if r.activePage == errorPage {
		t.Error("computeHashes on a directory should silently no-op, not report an error")
	}
}

// TestCaptureInfoKeyTriggersHash pins the 'h' keybinding, dispatched the
// way it actually arrives (through captureInfoKey), not by calling
// computeHashes directly.
func TestCaptureInfoKeyTriggersHash(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "banana.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path
	r.openInfo()

	if got := r.captureInfoKey(tcell.NewEventKey(tcell.KeyRune, 'h', tcell.ModNone)); got != nil {
		t.Error("captureInfoKey should consume the 'h' key")
	}
	if !strings.Contains(r.info.GetText(true), "MD5:") {
		t.Error("pressing h should have computed and shown the hash")
	}
}

// TestCaptureInfoMouseClickOnHashLineTriggersHash pins the click
// affordance: a click landing on (or below) the hash hint line computes
// the hash, dispatched through captureInfoMouse the way a real click
// arrives, with a genuinely drawn overlay (tcell.SimulationScreen) behind
// the coordinates rather than assumed ones.
func TestCaptureInfoMouseClickOnHashLineTriggersHash(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "banana.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path
	r.openInfo()

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(80, 24)
	r.info.Draw(screen)

	x, y, _, _ := r.info.GetInnerRect()
	clickY := y + r.hashSectionRow

	action, event := r.captureInfoMouse(tview.MouseLeftClick, tcell.NewEventMouse(x, clickY, tcell.Button1, 0))
	if action != tview.MouseConsumed || event != nil {
		t.Errorf("click on the hash line should be consumed, got action=%v event=%v", action, event)
	}
	if !strings.Contains(r.info.GetText(true), "MD5:") {
		t.Error("clicking the hash line should have computed and shown the hash")
	}
}

// TestCaptureInfoMouseClickAboveHashLineDoesNothing is the click test's
// negative case: clicking one of the ordinary fields (e.g. "Name:") must
// not trigger hashing.
func TestCaptureInfoMouseClickAboveHashLineDoesNothing(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "banana.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path
	r.openInfo()

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(80, 24)
	r.info.Draw(screen)

	x, y, _, _ := r.info.GetInnerRect() // row 0: the "Name:" field

	action, _ := r.captureInfoMouse(tview.MouseLeftClick, tcell.NewEventMouse(x, y, tcell.Button1, 0))
	if action == tview.MouseConsumed {
		t.Error("a click on the Name field should not be treated as hitting the hash line")
	}
	if strings.Contains(r.info.GetText(true), "MD5:") {
		t.Error("clicking above the hash line should not have computed anything")
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
