package fileicons

import (
	"os"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/rivo/uniseg"
)

func TestForTypeBased(t *testing.T) {
	cases := []struct {
		name      string
		entryType fsops.EntryType
		dispName  string
		mode      os.FileMode
		want      rune
	}{
		{"plain directory", fsops.TypeDir, "src", 0o755, glyphFolder},
		{".git directory", fsops.TypeDir, ".git", 0o755, glyphFolderGit},
		{"symlink to directory", fsops.TypeSymlinkDir, "whatever", 0, glyphSymlinkDir},
		{"symlink to file", fsops.TypeSymlinkFile, "whatever", 0, glyphSymlinkFile},
		{"broken symlink", fsops.TypeSymlinkBroken, "whatever", 0, glyphSymlinkBroken},
		{"socket", fsops.TypeSocket, "whatever", 0, glyphSocket},
		{"FIFO", fsops.TypeFIFO, "whatever", 0, glyphFIFO},
		{"char device", fsops.TypeCharDevice, "whatever", 0, glyphCharDevice},
		{"block device", fsops.TypeBlockDevice, "whatever", 0, glyphBlockDevice},
	}
	for _, c := range cases {
		got := For(c.entryType, c.dispName, c.mode)
		if got != c.want {
			t.Errorf("%s: For(%v, %q, %v) = %U, want %U", c.name, c.entryType, c.dispName, c.mode, got, c.want)
		}
	}
}

func TestForSymlinkIgnoresName(t *testing.T) {
	// A symlink to a directory named "main.go" must still render as a
	// symlink, never a language/text icon — typeGlyph's own precedence
	// (type before name) has to survive in icon mode too, per For's own
	// doc comment.
	if got := For(fsops.TypeSymlinkDir, "main.go", 0o755); got != glyphSymlinkDir {
		t.Errorf("For(TypeSymlinkDir, %q, ...) = %U, want %U (symlink glyph, not a name-based icon)", "main.go", got, glyphSymlinkDir)
	}
}

func TestForFileNameBased(t *testing.T) {
	tests := []struct {
		name string
		mode os.FileMode
		want rune
	}{
		{"main.go", 0o644, glyphFile},   // .go deliberately falls through, see extensionIcons' own doc comment
		{"script.py", 0o644, glyphFile}, // .py deliberately falls through too, see extensionIcons' own doc comment
		{"lib.rs", 0o644, rune(0xE7A8)},
		// txt/md/yml/yaml all share one icon, per the user's own
		// explicit request — see textExtensions' own doc comment.
		{"notes.txt", 0o644, glyphText},
		{"README.md", 0o644, glyphText},
		{"docker-compose.yml", 0o644, glyphDocker}, // exact name wins over the .yml text icon
		{"config.yml", 0o644, glyphText},
		{"config.yaml", 0o644, glyphText},
		{"Dockerfile", 0o644, glyphDocker},
		{"DOCKERFILE", 0o644, glyphDocker}, // case-insensitive, matching Docker itself
		{"Makefile", 0o644, glyphBuildTool},
		{"MAKEFILE", 0o644, glyphBuildTool}, // case-insensitive
		{"makefile.bak", 0o644, glyphFile},  // not the exact name anymore — no special-casing on a pattern
		{"app.conf", 0o644, glyphConfig},
		{"archive.tar.gz", 0o644, glyphArchive},
		{"backup.zip", 0o644, glyphArchive},
		{"app.jar", 0o644, glyphArchive},        // a zip container with a Java-specific extension
		{"data.gz", 0o644, glyphCompressed},     // bare single-file compression, not a container
		{"backup.tar.bz2", 0o644, glyphArchive}, // the ".tar." prefix still reads as a container, not just "ends in .bz2"
		{"photo.PNG", 0o644, glyphImage},        // case-insensitive extension match
		// PDF/Word/JSON/.deb were all tried and dropped (too small to
		// make out — see extensionIcons' own doc comment), so they fall
		// through to the generic file icon like any other unmatched
		// extension.
		{"report.pdf", 0o644, glyphFile},
		{"letter.docx", 0o644, glyphFile},
		{"settings.json", 0o644, glyphFile},
		{"package.deb", 0o644, glyphFile},
		{"disk.iso", 0o644, glyphDiskImage},
		{"disk.img", 0o644, glyphDiskImage},
		{"run.sh", 0o755, glyphExecutable},
		{"README", 0o644, glyphFile}, // no extension at all — the generic fallback
	}
	for _, c := range tests {
		got := For(fsops.TypeFile, c.name, c.mode)
		if got != c.want {
			t.Errorf("For(TypeFile, %q, %v) = %U, want %U", c.name, c.mode, got, c.want)
		}
	}
}

// TestGlyphsAreSingleWidth catches a codepoint picked from the wrong
// Nerd Fonts range landing in uniseg's own "this one is wide" table —
// the same width-calculation library tview's own TaggedStringWidth
// relies on (see fixedColumnsWidth, columns.go) — a different failure
// from "the user's own terminal font happens to draw it wide" (which
// this application has no way to detect or compensate for — see For's
// own package doc comment), and the one actually worth catching here.
func TestGlyphsAreSingleWidth(t *testing.T) {
	glyphs := []rune{
		glyphFolder, glyphFolderGit, glyphSymlinkDir, glyphSymlinkFile,
		glyphSymlinkBroken, glyphSocket, glyphFIFO, glyphCharDevice,
		glyphBlockDevice, glyphExecutable, glyphArchive, glyphCompressed,
		glyphImage, glyphDocker, glyphText, glyphDiskImage, glyphBuildTool,
		glyphConfig, glyphFile,
	}
	for _, g := range extensionIcons {
		glyphs = append(glyphs, g)
	}
	for _, g := range exactNameIcons {
		glyphs = append(glyphs, g)
	}
	for _, g := range glyphs {
		if w := uniseg.StringWidth(string(g)); w != 1 {
			t.Errorf("glyph %U has width %d, want 1", g, w)
		}
	}
}

func TestForExecutableLosesToMoreSpecificMatch(t *testing.T) {
	// An executable archive/language file still shows its more specific
	// icon, not the generic executable one — executable is checked last
	// among the TypeFile cases, per For's own doc comment.
	if got := For(fsops.TypeFile, "build.rs", 0o755); got != rune(0xE7A8) {
		t.Errorf("For(TypeFile, %q, 0o755) = %U, want the Rust icon, not the generic executable icon", "build.rs", got)
	}
}
