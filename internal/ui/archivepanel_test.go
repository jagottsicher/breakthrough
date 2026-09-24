package ui

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/config"
)

// writeTestZip builds a small zip fixture at dir/name containing files
// (internal path -> content) and returns its full path — the same
// shape internal/archive's own test fixtures use, duplicated here
// rather than exported from that package: internal/ui has no business
// depending on internal/archive's own test helpers, only its public
// API, the same as production code.
func writeTestZip(t *testing.T, dir, name string, files map[string]string) string {
	t.Helper()
	full := filepath.Join(dir, name)
	f, err := os.Create(full)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	for path, content := range files {
		w, err := zw.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return full
}

func newTestPanel(t *testing.T, dir string) *Panel {
	t.Helper()
	p, err := NewPanel(nil, dir, config.DefaultTheme().Resolve(), config.DefaultSettings()) //nolint:staticcheck // app is only needed for the header edit field's own focus handling — not exercised here
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	return p
}

func TestLoadEntersArchiveOnRecognizedFile(t *testing.T) {
	dir := t.TempDir()
	zipPath := writeTestZip(t, dir, "sample.zip", map[string]string{
		"README.md":   "hello\n",
		"src/main.go": "package main\n",
	})
	p := newTestPanel(t, dir)

	if err := p.load(zipPath); err != nil {
		t.Fatalf("load(zipPath) failed: %v", err)
	}
	if !p.inArchiveView() {
		t.Fatal("expected inArchiveView() after loading a zip file directly")
	}
	if p.archivePath != zipPath {
		t.Errorf("archivePath = %q, want %q", p.archivePath, zipPath)
	}

	names := rowNames(t, p)
	want := map[string]bool{"..": true, "README.md": true, "src": true}
	if len(names) != len(want) {
		t.Fatalf("rows = %v, want exactly %v", names, want)
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected row %q", n)
		}
	}
}

func TestLoadNavigatesIntoArchiveSubdirectoryAndBack(t *testing.T) {
	dir := t.TempDir()
	zipPath := writeTestZip(t, dir, "sample.zip", map[string]string{
		"src/main.go":     "package main\n",
		"src/lib/util.go": "package lib\n",
	})
	p := newTestPanel(t, dir)
	if err := p.load(zipPath); err != nil {
		t.Fatal(err)
	}

	subPath := filepath.Join(zipPath, "src")
	if err := p.load(subPath); err != nil {
		t.Fatalf("load(%q) failed: %v", subPath, err)
	}
	if !p.inArchiveView() || p.archivePath != zipPath {
		t.Fatalf("expected still inside %q, got archivePath=%q inArchiveView=%v", zipPath, p.archivePath, p.inArchiveView())
	}
	names := rowNames(t, p)
	if !containsName(names, "main.go") || !containsName(names, "lib") {
		t.Errorf("rows at src/ = %v, want main.go and lib", names)
	}

	// ".." from src/ goes back to the archive's own root.
	parent := filepath.Dir(subPath)
	if err := p.load(parent); err != nil {
		t.Fatal(err)
	}
	if parent != zipPath {
		t.Fatalf("setup: filepath.Dir(%q) = %q, want %q", subPath, parent, zipPath)
	}
	if !p.inArchiveView() {
		t.Error("expected still inArchiveView() back at the archive's own root")
	}

	// One more ".." leaves the archive entirely, back to the real dir.
	if err := p.load(filepath.Dir(zipPath)); err != nil {
		t.Fatal(err)
	}
	if p.inArchiveView() {
		t.Error("expected inArchiveView() false after navigating above the archive's own root")
	}
	if p.archivePath != "" || p.archiveEntries != nil {
		t.Errorf("expected archivePath/archiveEntries reset on leaving the archive, got %q / %v", p.archivePath, p.archiveEntries)
	}
}

func TestActivateRowEntersRecognizedArchive(t *testing.T) {
	dir := t.TempDir()
	writeTestZip(t, dir, "sample.zip", map[string]string{"a.txt": "hi\n"})
	p := newTestPanel(t, dir)
	if err := p.load(dir); err != nil {
		t.Fatal(err)
	}

	row, ok := findRowByName(t, p, "sample.zip")
	if !ok {
		t.Fatal("setup: sample.zip row not found")
	}
	p.focusRow(row)
	p.activateRow(row)

	if !p.inArchiveView() {
		t.Error("activateRow on a recognized archive should have entered it")
	}
}

func TestActivateRowDoesNotEnterArchiveNestedInsideAnother(t *testing.T) {
	dir := t.TempDir()
	inner := writeTestZip(t, dir, "inner.zip", map[string]string{"x.txt": "hi\n"})
	_ = inner
	outerDir := t.TempDir()
	outerZip := filepath.Join(outerDir, "outer.zip")
	// Build outer.zip containing inner.zip's own bytes as a member —
	// simulating an archive found *inside* another one.
	innerBytes, err := os.ReadFile(inner)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(outerZip)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("inner.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(innerBytes); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	p := newTestPanel(t, outerDir)
	if err := p.load(outerZip); err != nil {
		t.Fatal(err)
	}
	if !p.inArchiveView() {
		t.Fatal("setup: expected to be inside outer.zip")
	}

	row, ok := findRowByName(t, p, "inner.zip")
	if !ok {
		t.Fatal("setup: inner.zip row not found inside outer.zip")
	}
	p.focusRow(row)
	before := p.archivePath
	p.activateRow(row)
	// Per this feature's own deliberate scope (see archivepanel.go's own
	// doc comment): a nested archive is never entered automatically —
	// activateRow should have left archivePath exactly as it was (still
	// outer.zip), not switched into inner.zip.
	if p.archivePath != before {
		t.Errorf("archivePath after activating a nested archive = %q, want unchanged %q", p.archivePath, before)
	}
}

func TestCutCurrentSelectionRefusedInsideArchive(t *testing.T) {
	dir := t.TempDir()
	zipPath := writeTestZip(t, dir, "sample.zip", map[string]string{"a.txt": "hi\n"})
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	if err := r.panel.load(zipPath); err != nil {
		t.Fatal(err)
	}

	r.cutCurrentSelection()

	if len(r.clipboard) != 0 {
		t.Errorf("expected nothing cut inside an archive, got clipboard=%v", r.clipboard)
	}
	if r.activePage != errorPage {
		t.Fatalf("expected an error overlay, activePage = %q", r.activePage)
	}
	// wrapText (see showError) has already broken the message across
	// several lines by the time it reaches errorView — normalized back
	// to a single line before comparing, the same reason as any other
	// wrapped-text assertion elsewhere in this package.
	got := strings.ReplaceAll(r.errorView.GetText(true), "\n", " ")
	if !strings.Contains(got, "not supported") || !strings.Contains(got, "archive") {
		t.Errorf("error text = %q, want it to mention errNotSupportedInArchive", got)
	}
}

// TestCopyPasteExtractsMarkedArchiveEntry is this feature's own
// end-to-end pin: mark a directory while browsing inside a zip, Copy,
// navigate back out to a real destination, Paste — and confirm real
// files land there with their own nested structure intact. Reuses
// Copy/Cut/Paste's own real keyboard actions rather than calling
// archive.Extract directly, since the point is exercising the whole
// wire-up (Panel.selected -> clipboard -> pasteInto's own archive
// branch), the same live path a keypress actually takes.
func TestCopyPasteExtractsMarkedArchiveEntry(t *testing.T) {
	dir := t.TempDir()
	zipPath := writeTestZip(t, dir, "sample.zip", map[string]string{
		"src/main.go":     "package main\n",
		"src/lib/util.go": "package lib\n",
	})
	dest := filepath.Join(dir, "dest")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	if err := r.panel.load(zipPath); err != nil {
		t.Fatal(err)
	}
	row, ok := findRowByName(t, r.panel, "src")
	if !ok {
		t.Fatal("setup: src row not found inside sample.zip")
	}
	r.panel.focusRow(row)
	r.panel.toggleCheckbox(row)
	r.copyCurrentSelection()

	if len(r.clipboard) != 1 || r.clipboardCut {
		t.Fatalf("setup: expected exactly 1 copied (not cut) item, got clipboard=%v cut=%v", r.clipboard, r.clipboardCut)
	}

	r.pasteInto(dest, false)
	waitForCondition(t, func() bool {
		_, err := os.Stat(filepath.Join(dest, "src", "lib", "util.go"))
		return err == nil
	})

	got, err := os.ReadFile(filepath.Join(dest, "src", "main.go"))
	if err != nil {
		t.Fatalf("dest/src/main.go: %v", err)
	}
	if string(got) != "package main\n" {
		t.Errorf("dest/src/main.go content = %q, want %q", got, "package main\n")
	}
	got, err = os.ReadFile(filepath.Join(dest, "src", "lib", "util.go"))
	if err != nil {
		t.Fatalf("dest/src/lib/util.go: %v", err)
	}
	if string(got) != "package lib\n" {
		t.Errorf("dest/src/lib/util.go content = %q, want %q", got, "package lib\n")
	}
}

// TestFinishExtractClipboardArchiveLogsAnAction pins
// finishExtractClipboardArchive's own success path — split out from
// extractClipboardArchive specifically so it's directly, synchronously
// testable (see its own doc comment) rather than needing a running
// Application to drain the real QueueUpdateDraw hand-off.
func TestFinishExtractClipboardArchiveLogsAnAction(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	readLog := attachTestActivityLog(t, r)

	r.finishExtractClipboardArchive("sample.zip", 2, dir, nil)

	got := readLog()
	if !strings.Contains(got, string(activitylog.CategoryArchive)) || !strings.Contains(got, "extracted 2 item(s) from sample.zip") {
		t.Errorf("log = %q, want an archive entry about the 2 extracted items", got)
	}
}

// TestFinishExtractClipboardArchiveLogsAnError is the failure
// counterpart.
func TestFinishExtractClipboardArchiveLogsAnError(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	readLog := attachTestActivityLog(t, r)

	r.finishExtractClipboardArchive("sample.zip", 2, dir, errors.New("boom"))

	got := readLog()
	if !strings.Contains(got, string(activitylog.CategoryArchive)) || !strings.Contains(got, "boom") {
		t.Errorf("log = %q, want an archive error entry mentioning the failure", got)
	}
}

// TestArchiveExtractionForSynthesizesImpliedDirectory pins a real bug
// found while writing TestCopyPasteExtractsMarkedArchiveEntry: a zip
// whose own directory members are only ever implied by their children's
// paths (no explicit entry of their own — see archive.Children's own
// doc comment, and writeTestZip's own doc comment on why this file's
// own fixtures never write one) still needs to resolve to a real,
// extractable archive.Entry here, the same as one with an explicit
// entry does — without this, marking such a directory and copying it
// out would silently extract nothing at all.
func TestArchiveExtractionForSynthesizesImpliedDirectory(t *testing.T) {
	dir := t.TempDir()
	zipPath := writeTestZip(t, dir, "sample.zip", map[string]string{
		"src/main.go": "package main\n",
	})
	clipboard := []string{filepath.Join(zipPath, "src")}

	archivePath, members, ok := archiveExtractionFor(clipboard)
	if !ok {
		t.Fatal("archiveExtractionFor should have recognized the clipboard as archive members")
	}
	if archivePath != zipPath {
		t.Errorf("archivePath = %q, want %q", archivePath, zipPath)
	}
	if len(members) != 1 || members[0].Path != "src" || !members[0].IsDir {
		t.Errorf("members = %v, want exactly one IsDir entry for \"src\"", members)
	}
}

// waitForCondition polls cond briefly — extractClipboardArchive runs
// off the UI thread (see its own doc comment), so the test has to wait
// for its QueueUpdateDraw hand-off the same way a real Application.Run
// loop would service it, rather than asserting immediately after
// pasteInto returns.
func waitForCondition(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition never became true")
}

// rowNames/findRowByName/containsName are small local helpers rather
// than reusing anything from panel_test.go's own larger fixtures: this
// file's own newTestPanel deliberately builds the narrowest Panel these
// tests need (no Root, no screen), so it reads its rows back through
// Panel's own rowRef directly instead of a rendered table.
func rowNames(t *testing.T, p *Panel) []string {
	t.Helper()
	var names []string
	for row := 0; row < p.table.GetRowCount(); row++ {
		ref, ok := p.rowRef(row)
		if !ok {
			continue
		}
		names = append(names, ref.name)
	}
	return names
}

func findRowByName(t *testing.T, p *Panel, name string) (int, bool) {
	t.Helper()
	for row := 0; row < p.table.GetRowCount(); row++ {
		ref, ok := p.rowRef(row)
		if ok && ref.name == name {
			return row, true
		}
	}
	return 0, false
}

func containsName(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}
