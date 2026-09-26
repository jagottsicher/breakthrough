package ui

// operations_matrix_test.go is a deliberate, systematic cross-product of
// every core file operation (Copy, Cut) against every "shape" of source
// this app's own dispatch logic treats specially somewhere (a plain
// file/directory, an archive file itself, a member already inside one —
// including one only implied by its own descendants — a symlink, a
// remote entry) — built specifically to catch the exact class of bug a
// real, user-reported regression exposed: two features (archive
// browsing and plain Copy/Cut/Paste) were each individually well
// tested, but the *seam* between them — a clipboard entry that IS the
// archive file itself, not a member inside one — had no test of its own
// at all (see archiveExtractionFor's own doc comment). A new "shape" or
// a new operation's own dispatch check gets one deliberate row in the
// relevant table here, not just its own author's best guess about which
// combinations matter.
//
// Every archive fixture builder here (writeTestTarPlain/writeTestTarGz/
// writeTestTarXz/writeTestTarBz2) mirrors internal/archive's own
// archive_test.go byte-for-byte — _test.go helpers can't cross package
// boundaries, so this is deliberate, small duplication rather than
// exporting a test-only helper from a non-test file. writeTestZip
// itself, needing no such mirror, is already shared from
// archivepanel_test.go in this same package.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/ulikunitz/xz"

	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

func writeTestTarBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for path, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: path, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeTestTarPlain(t *testing.T, dir, name string, files map[string]string) string {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, writeTestTarBytes(t, files), 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

func writeTestTarGz(t *testing.T, dir, name string, files map[string]string) string {
	t.Helper()
	full := filepath.Join(dir, name)
	f, err := os.Create(full)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	gw := gzip.NewWriter(f)
	if _, err := gw.Write(writeTestTarBytes(t, files)); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return full
}

func writeTestTarXz(t *testing.T, dir, name string, files map[string]string) string {
	t.Helper()
	full := filepath.Join(dir, name)
	f, err := os.Create(full)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	xw, err := xz.NewWriter(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := xw.Write(writeTestTarBytes(t, files)); err != nil {
		t.Fatal(err)
	}
	if err := xw.Close(); err != nil {
		t.Fatal(err)
	}
	return full
}

// writeTestTarBz2 shells out to the real bzip2(1) binary — compress/bzip2
// is decode-only in the Go standard library (verified against its own
// package doc, not assumed — the same reasoning internal/archive's own
// archive_test.go already documents for its identical fixture). Skips
// cleanly wherever bzip2(1) isn't installed rather than failing the
// whole suite over a missing dev tool.
func writeTestTarBz2(t *testing.T, dir, name string, files map[string]string) string {
	t.Helper()
	requireCommand(t, "bzip2")
	full := filepath.Join(dir, name)
	cmd := exec.Command("bzip2", "-c")
	cmd.Stdin = bytes.NewReader(writeTestTarBytes(t, files))
	compressed, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, compressed, 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

// opShape describes one source "shape" — setup prepares dir on disk
// (and/or connects r's own active panel to a fake remote), marks
// nothing itself, and returns the exact display name Copy/Cut should
// find in the panel's own current listing (matching findRowByName's
// own lookup).
type opShape struct {
	name  string
	setup func(t *testing.T, r *Root, dir string) (entryName string)
}

// opShapes is every shape this matrix exercises — new dispatch logic
// that treats some kind of entry specially gets a new row here, not
// just its own author's own test file.
func opShapes() []opShape {
	return []opShape{
		{
			name: "plain file",
			setup: func(t *testing.T, r *Root, dir string) string {
				if err := os.WriteFile(filepath.Join(dir, "plain.txt"), []byte("hello\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				return "plain.txt"
			},
		},
		{
			name: "plain directory with nested content",
			setup: func(t *testing.T, r *Root, dir string) string {
				sub := filepath.Join(dir, "plaindir", "sub")
				if err := os.MkdirAll(sub, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(sub, "nested.txt"), []byte("nested\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				return "plaindir"
			},
		},
		{
			name: "archive file itself: zip",
			setup: func(t *testing.T, r *Root, dir string) string {
				writeTestZip(t, dir, "sample.zip", map[string]string{"a.txt": "hi\n"})
				return "sample.zip"
			},
		},
		{
			name: "archive file itself: tar",
			setup: func(t *testing.T, r *Root, dir string) string {
				writeTestTarPlain(t, dir, "sample.tar", map[string]string{"a.txt": "hi\n"})
				return "sample.tar"
			},
		},
		{
			name: "archive file itself: tar.gz",
			setup: func(t *testing.T, r *Root, dir string) string {
				writeTestTarGz(t, dir, "sample.tar.gz", map[string]string{"a.txt": "hi\n"})
				return "sample.tar.gz"
			},
		},
		{
			name: "archive file itself: tar.bz2",
			setup: func(t *testing.T, r *Root, dir string) string {
				writeTestTarBz2(t, dir, "sample.tar.bz2", map[string]string{"a.txt": "hi\n"})
				return "sample.tar.bz2"
			},
		},
		{
			name: "archive file itself: tar.xz",
			setup: func(t *testing.T, r *Root, dir string) string {
				writeTestTarXz(t, dir, "sample.tar.xz", map[string]string{"a.txt": "hi\n"})
				return "sample.tar.xz"
			},
		},
		{
			name: "working symlink to a file",
			setup: func(t *testing.T, r *Root, dir string) string {
				target := filepath.Join(dir, "target.txt")
				if err := os.WriteFile(target, []byte("hi\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				link := filepath.Join(dir, "link.txt")
				if err := os.Symlink(target, link); err != nil {
					t.Fatal(err)
				}
				return "link.txt"
			},
		},
		{
			name: "working symlink to a directory",
			setup: func(t *testing.T, r *Root, dir string) string {
				target := filepath.Join(dir, "targetdir")
				if err := os.Mkdir(target, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(target, "inside.txt"), []byte("hi\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				link := filepath.Join(dir, "linkdir")
				if err := os.Symlink(target, link); err != nil {
					t.Fatal(err)
				}
				return "linkdir"
			},
		},
		{
			name: "broken symlink",
			setup: func(t *testing.T, r *Root, dir string) string {
				link := filepath.Join(dir, "brokenlink")
				if err := os.Symlink(filepath.Join(dir, "does-not-exist"), link); err != nil {
					t.Fatal(err)
				}
				return "brokenlink"
			},
		},
		{
			name: "remote plain file",
			setup: func(t *testing.T, r *Root, dir string) string {
				// newTabHere first: tab 0 stays the plain local dir, so
				// the test itself can switch back to it before pasting —
				// pasteClipboard's own real path always reads its
				// destination from r.panel.path/r.panel.remote (see its
				// own doc comment), so a real Copy-from-remote-paste-to-
				// local always means switching the active panel back to
				// the local side first, exactly what a real "v" press
				// requires too.
				r.newTabHere()
				client := &fakeRemoteClient{root: "/remote", entries: map[string][]fsops.Entry{
					"/remote": {{Name: "remote.txt", Type: fsops.TypeFile}},
				}, content: map[string][]byte{"/remote/remote.txt": []byte("hi\n")}}
				if err := r.panel.connectRemote(client, remotefs.Connection{Host: "example.com", User: "tester"}); err != nil {
					t.Fatal(err)
				}
				return "remote.txt"
			},
		},
		{
			name: "remote plain directory",
			setup: func(t *testing.T, r *Root, dir string) string {
				r.newTabHere()
				client := &fakeRemoteClient{root: "/remote", entries: map[string][]fsops.Entry{
					"/remote":           {{Name: "remotedir", Type: fsops.TypeDir}},
					"/remote/remotedir": {{Name: "inside.txt", Type: fsops.TypeFile}},
				}, content: map[string][]byte{"/remote/remotedir/inside.txt": []byte("hi\n")}}
				if err := r.panel.connectRemote(client, remotefs.Connection{Host: "example.com", User: "tester"}); err != nil {
					t.Fatal(err)
				}
				return "remotedir"
			},
		},
	}
}

// markAndActivate finds name in r.panel's own current listing, marks it
// (the same checkbox toggle a real "Space" press does), and copies or
// cuts it — mirroring copyCurrentSelection/cutCurrentSelection's own
// real path (focus + checkbox, not a raw clipboard write), so this
// matrix exercises exactly what a real keypress does.
func markAndActivate(t *testing.T, r *Root, name string, cut bool) {
	t.Helper()
	row, ok := findRowByName(t, r.panel, name)
	if !ok {
		t.Fatalf("setup: %q not found in the current listing", name)
	}
	r.panel.focusRow(row)
	r.panel.toggleCheckbox(row)
	if cut {
		r.cutCurrentSelection()
	} else {
		r.copyCurrentSelection()
	}
}

// destHasEntry reports whether dest contains an entry named name —
// Copy/Cut's own most basic contract: something with the marked
// entry's own name actually landed, the exact thing that silently
// failed for the real bug this file exists to catch (an archive file
// marked for Copy landed nothing at all, with no error).
func destHasEntry(dest, name string) bool {
	_, err := os.Lstat(filepath.Join(dest, name))
	return err == nil
}

// TestCopyAcrossEverySourceShape pins Copy's own contract for every
// shape in opShapes: the marked entry's own name lands in the
// destination, and the source is left untouched — Copy never removes
// anything, regardless of what kind of entry it is.
func TestCopyAcrossEverySourceShape(t *testing.T) {
	for _, shape := range opShapes() {
		t.Run(shape.name, func(t *testing.T) {
			dir := t.TempDir()
			dest := filepath.Join(dir, "dest")
			if err := os.Mkdir(dest, 0o755); err != nil {
				t.Fatal(err)
			}
			r, err := NewRoot(tview.NewApplication(), dir)
			if err != nil {
				t.Fatalf("NewRoot: %v", err)
			}
			name := shape.setup(t, r, dir)
			// NewRoot's own initial load already ran before setup wrote
			// anything to disk — reload to pick it up, skipped for a
			// remote shape (setup's own connectRemote already switched
			// r.panel to the remote listing; reloading dir here would
			// stomp right back over that).
			if r.panel.remote == nil {
				if err := r.panel.load(dir); err != nil {
					t.Fatal(err)
				}
			}

			isRemote := r.panel.remote != nil
			markAndActivate(t, r, name, false)
			if len(r.clipboard) != 1 || r.clipboardCut {
				t.Fatalf("setup: expected exactly 1 copied (not cut) item, got clipboard=%v cut=%v", r.clipboard, r.clipboardCut)
			}
			// pasteClipboard's real "v" path always derives its own
			// destination from whichever panel is currently active (see
			// its own doc comment) — a remote shape marked its entry from
			// a second tab (see opShapes' own doc comment on why), so a
			// real Paste back to the plain local dir needs switching back
			// to tab 0 first, exactly like a real user would.
			if isRemote {
				r.switchToTab(0)
			}
			r.pasteInto(dest, false)
			// Real I/O — an ordinary paste job, a remote transfer, or an
			// archive extraction — always runs off the UI thread (see
			// extractClipboardArchive's own doc comment for why), so this
			// polls for the real, on-disk result rather than asserting
			// immediately after pasteInto returns, the same reasoning
			// TestCopyPasteExtractsMarkedArchiveEntry's own identical
			// wait already establishes.
			waitForCondition(t, func() bool { return destHasEntry(dest, name) })

			if !isRemote && !destHasEntry(dir, name) {
				t.Errorf("Copy removed the source %q, want it left in place", name)
			}
		})
	}
}

// TestCutAcrossEverySourceShape is Copy's own counterpart: the marked
// entry's own name lands in the destination, and — unlike Copy — the
// source is gone afterward, for every shape a plain Cut actually
// supports. A remote source is included: remotepaste.go's own engine
// handles a Cut from a remote connection the same as a Copy (see
// cutCurrentSelection's own doc comment), unlike an archive view, which
// refuses Cut outright — not exercised here at all, since that's
// already TestCutCurrentSelectionRefusedInsideArchive's own job in
// archivepanel_test.go, and every shape in opShapes lives outside an
// archive view by construction.
func TestCutAcrossEverySourceShape(t *testing.T) {
	for _, shape := range opShapes() {
		t.Run(shape.name, func(t *testing.T) {
			dir := t.TempDir()
			dest := filepath.Join(dir, "dest")
			if err := os.Mkdir(dest, 0o755); err != nil {
				t.Fatal(err)
			}
			r, err := NewRoot(tview.NewApplication(), dir)
			if err != nil {
				t.Fatalf("NewRoot: %v", err)
			}
			name := shape.setup(t, r, dir)
			isRemote := r.panel.remote != nil
			if !isRemote {
				if err := r.panel.load(dir); err != nil {
					t.Fatal(err)
				}
			}

			markAndActivate(t, r, name, true)
			if r.activePage == errorPage {
				t.Fatalf("Cut of %q was refused: %q", name, r.errorView.GetText(true))
			}
			if len(r.clipboard) != 1 || !r.clipboardCut {
				t.Fatalf("setup: expected exactly 1 cut item, got clipboard=%v cut=%v", r.clipboard, r.clipboardCut)
			}
			if isRemote {
				r.switchToTab(0)
			}
			r.pasteInto(dest, false)
			waitForCondition(t, func() bool { return destHasEntry(dest, name) })

			if !isRemote && destHasEntry(dir, name) {
				t.Errorf("Cut left the source %q in place, want it removed", name)
			}
		})
	}
}

// TestRenameAcrossEverySourceShape extends opShapes()'s own matrix to
// Rename: every local shape must be renamable exactly like any other
// entry — including the archive file itself, the exact shape whose
// neglect once broke Copy/Cut (see this file's own doc comment). Remote
// shapes are skipped: Rename's own remote path is a genuinely separate
// code path (remoteops.go's own renameRemote, already covered by
// remoteops_test.go), not part of the local dispatch this matrix exists
// to pin.
func TestRenameAcrossEverySourceShape(t *testing.T) {
	for _, shape := range opShapes() {
		if strings.HasPrefix(shape.name, "remote") {
			continue
		}
		t.Run(shape.name, func(t *testing.T) {
			dir := t.TempDir()
			r, err := NewRoot(tview.NewApplication(), dir)
			if err != nil {
				t.Fatalf("NewRoot: %v", err)
			}
			name := shape.setup(t, r, dir)
			if err := r.panel.load(dir); err != nil {
				t.Fatal(err)
			}

			row, ok := findRowByName(t, r.panel, name)
			if !ok {
				t.Fatalf("setup: %q not found in the current listing", name)
			}
			r.renameRow(row)
			if r.activePage != renamePage {
				t.Fatalf("Rename of %q did not open the rename prompt, activePage=%q", name, r.activePage)
			}
			r.rename.SetText("renamed")
			r.finishRename(tcell.KeyEnter)

			if !destHasEntry(dir, "renamed") {
				t.Errorf("Rename of %q did not produce %q", name, "renamed")
			}
			if destHasEntry(dir, name) {
				t.Errorf("Rename of %q left the original name in place too", name)
			}
		})
	}
}

// TestRenameRefusedForAMemberInsideAnArchive is a regression test for a
// real, newly-found gap: openRename (the context menu's "Rename" and
// renameRow's own click-pause-click gesture — see its own doc comment)
// never checked inArchiveView at all, unlike renameCurrentEntry (the
// "r" key), the only one of Rename's three entry points that did.
// r.target for an archive member is a virtual "archivePath/member"
// string, never a real filesystem path — finishRename's own
// fsops.Rename call on one would have just failed with a bare,
// confusing "no such file or directory" instead of the same clear
// errNotSupportedInArchive every other archive-view guard already
// gives. Exercises renameRow specifically, the one path with no
// existing coverage of this at all before openRename's own guard closed
// it for all three at once.
func TestRenameRefusedForAMemberInsideAnArchive(t *testing.T) {
	dir := t.TempDir()
	zipPath := writeTestZip(t, dir, "sample.zip", map[string]string{"a.txt": "hi\n"})
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	if err := r.panel.load(zipPath); err != nil {
		t.Fatal(err)
	}
	row, ok := findRowByName(t, r.panel, "a.txt")
	if !ok {
		t.Fatal("setup: a.txt row not found inside sample.zip")
	}

	r.renameRow(row)

	if r.activePage == renamePage {
		t.Error("renameRow opened the rename prompt for an archive member, want it refused")
	}
	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want the error overlay", r.activePage)
	}
	if got := r.errorView.GetText(true); !strings.Contains(got, "archive") {
		t.Errorf("error text = %q, want it to mention the archive refusal", got)
	}
}
