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
