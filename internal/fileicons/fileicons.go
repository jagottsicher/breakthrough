// Package fileicons maps a listing entry's type and name to a Nerd
// Font glyph, for the "zi" display toggle (see config.Settings.FileIcons
// and internal/ui's own "z" chord family). Pure lookup, no tcell/tview
// dependency: internal/ui decides the actual on-screen color (reusing
// Panel.entryColor, the same color the name column already gets) and
// renders the rune this package returns.
//
// No font is bundled or redistributed here — these are bare Unicode
// codepoints in the Private Use Area that a Nerd Font (patched by the
// user's own terminal setup) renders as a glyph; on any other font they
// show as whatever that font's own PUA fallback is (commonly a blank or
// a box), which is exactly why this is an opt-in toggle rather than the
// default (see typeGlyph's own doc comment in internal/ui/panel.go for
// the unconditional ASCII/MC-compatible fallback this replaces).
//
// Every codepoint below is written as a plain hex rune conversion
// (rune(0x....)), never a '\uXXXX' rune literal or a raw character: a
// literal renders as the glyph itself in this source file (illegible
// without a Nerd Font active in whatever shows the file, including a
// diff or a code review tool), and a tool chain that normalizes or
// round-trips the file as text can silently mangle it. A hex constant
// stays plain ASCII and unambiguous everywhere.
//
// Deliberately restricted to the oldest, most universally bundled Nerd
// Fonts category — Font Awesome (nf-fa-*), patched into every Nerd Font
// release since v1 — never the newer "Seti-UI + Custom" block
// (nf-seti-*, nf-custom-*, roughly U+E5FA–U+E6B2) or Octicons
// (nf-oct-*): real-world reports against the same terminal found both
// nf-custom-folder and the Octicon symlink glyphs either missing
// (rendered as a literal ".notdef" box showing its own hex code) or
// just not legible, while most nf-fa-* glyphs tried rendered fine. Even
// within Devicons (nf-dev-*), two codepoints (nf-dev-go, nf-dev-python)
// turned out missing/wrong on that same font — see extensionIcons' own
// doc comment.
//
// Even within nf-fa-* itself, a whole cluster of classic FontAwesome 4
// "file type" icons (U+F1Cx: file_archive_o, file_pdf_o, file_word_o,
// file_code_o, ...) turned out to render *too small to make out* on
// the same font/terminal, despite not being missing outright — a
// different failure from a missing glyph, but just as disqualifying.
// glyphArchive and glyphImage were moved off that cluster onto bigger,
// standalone FontAwesome glyphs (nf-fa-archive, nf-fa-image) instead;
// PDF/Word/code/package icons that were tried from the same small
// cluster (nf-fa-file_pdf_o, nf-fa-file_word_o, nf-fa-cube) got the
// same complaint and were dropped back to glyphFile rather than kept
// hunting for yet another codepoint — see extensionIcons' own doc
// comment. The lesson: "classic FontAwesome 4" narrows the risk of a
// glyph being missing outright, but doesn't guarantee it reads clearly
// at terminal size — each addition here still needs a real look, not
// just a safe-sounding category.
package fileicons

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// Generic, type-based glyphs — mirror internal/ui's own typeGlyph
// switch (panel.go) one-for-one, so every distinction that single
// character carries (directory, the two live-symlink kinds, a broken
// symlink, the four special device/IPC types, executable) survives icon
// mode too; nothing here is allowed to lose information typeGlyph
// already shows. Codepoints verified against the upstream Nerd Fonts
// glyph table (github.com/ryanoasis/nerd-fonts, glyphnames.json), not
// guessed.
const (
	glyphFolder    = rune(0xF07B) // nf-fa-folder
	glyphFolderGit = rune(0xE702) // nf-dev-git — a directory literally named ".git"
	// glyphSymlinkFile/glyphSymlinkDir are nf-fa-link/nf-fa-external_link
	// — a plain chain link for "points at another file", and an
	// arrow-out-of-a-box for "points elsewhere, into a directory" —
	// replacing the Octicon pair (nf-oct-file_symlink_file/_directory)
	// originally picked here, after a real-world report that they
	// "dürften besser aussehen" (could look better) on top of this
	// package's own general "stick to Font Awesome" rule above.
	glyphSymlinkFile   = rune(0xF0C1) // nf-fa-link
	glyphSymlinkDir    = rune(0xF08E) // nf-fa-external_link
	glyphSymlinkBroken = rune(0xF127) // nf-fa-unlink
	glyphSocket        = rune(0xF1E6) // nf-fa-plug
	glyphFIFO          = rune(0xF0EC) // nf-fa-exchange
	// glyphCharDevice/glyphBlockDevice (nf-fa-microchip/nf-fa-hdd_o) are
	// also reused by name for disk-image files — see glyphDiskImage's
	// own doc comment. Both confirmed legible at terminal size.
	glyphCharDevice  = rune(0xF2DB) // nf-fa-microchip
	glyphBlockDevice = rune(0xF0A0) // nf-fa-hdd_o
	glyphExecutable  = rune(0xF120) // nf-fa-terminal
	// glyphArchive is for a real multi-file container (zip/tar/7z/rar/
	// jar — see archiveExtensions); glyphCompressed is the user's own
	// requested distinction for a bare single-file compression format
	// (.gz, .bz2, ... — see compressedExtensions) ending up on
	// something that reads as "squeezed" rather than "a box full of
	// other files", since conceptually it isn't one.
	//
	// glyphArchive is nf-fa-archive (a bold filing-cabinet-drawer
	// glyph), not nf-fa-file_archive_o (U+F1C6, a plain file outline
	// with a tiny zipper icon inset) — confirmed too small to make out
	// at terminal size, the same complaint that later also sank
	// file_pdf_o/file_word_o/cube (see extensionIcons' own doc comment)
	// — nf-fa-archive's own bigger, dedicated glyph reads fine instead.
	glyphArchive    = rune(0xF187) // nf-fa-archive
	glyphCompressed = rune(0xF066) // nf-fa-compress
	// glyphImage is nf-fa-image/nf-fa-photo/nf-fa-picture_o — three
	// names for the same classic FontAwesome 4 codepoint (a bold
	// landscape-in-a-frame glyph), picked over the smaller
	// nf-fa-file_image_o (U+F1C5, a plain file outline with a tiny
	// picture icon inset) after a real-world report that the latter was
	// "kaum erkennbar" (barely recognizable) at normal terminal size —
	// confirmed good since.
	glyphImage  = rune(0xF03E)
	glyphDocker = rune(0xE7B0) // nf-dev-docker
	// glyphText (nf-fa-file_text_o) is shared by every plain-text-ish
	// extension in textExtensions below — not just .txt itself, per the
	// user's own explicit request ("das nehmen wir auch für alle
	// anderen Texte wie yml, yaml, md usw"). Confirmed legible.
	glyphText = rune(0xF0F6)
	// glyphDiskImage reuses glyphBlockDevice's own codepoint
	// (nf-fa-hdd_o) rather than the flashier nf-fa-compact_disc: that
	// one is a Font Awesome 5/6 addition at a much higher codepoint
	// (U+EDE9) than anything else this package uses, the same
	// "recent addition, unproven on this terminal" risk already flagged
	// for nf-custom-*/nf-dev-go — not worth it for a file (.img/.iso)
	// that's conceptually a disk anyway. Confirmed good.
	glyphDiskImage = glyphBlockDevice
	// glyphBuildTool (nf-fa-wrench) marks a build-tool file recognized
	// by its exact filename (Makefile — see exactNameIcons), the same
	// "file with a name that means something" treatment Dockerfile
	// already gets via glyphDocker. Confirmed good.
	glyphBuildTool = rune(0xF0AD) // nf-fa-wrench
	// glyphConfig (nf-fa-cogs, a gearwheel pair) is for ".conf" — the
	// user's own explicit request for "die wrench ... oder ein Zahnrad"
	// (the wrench, or a gear, whichever we have) on a config file,
	// distinct from glyphBuildTool so "this builds something" and "this
	// configures something" stay two different icons. Same classic
	// FontAwesome 4 low-codepoint cluster as glyphBuildTool, which was
	// just confirmed legible — not independently re-verified, but the
	// closest available evidence.
	glyphConfig = rune(0xF085) // nf-fa-cogs
	// glyphDataFile (▦) is U+25A6 SQUARE WITH ORTHOGONAL CROSSHATCH FILL
	// — the user's own explicit pick for .json/.sql/.csv, deliberately a
	// plain Unicode Geometric Shapes codepoint rather than a Nerd Font
	// PUA glyph: the user asked for the exact same mark in both icon
	// modes (this Nerd-Font one and the font-free fallback in panel.go's
	// own fallbackData), which a PUA codepoint could never do since the
	// font-free mode can't render one at all. No legibility risk either
	// way — it's the same character every terminal font already knows.
	glyphDataFile = rune(0x25A6)
	glyphFile     = rune(0xF016) // nf-fa-file_o — plain, non-executable file, nothing more specific matched
)

// scriptExtensions forces glyphExecutable for these extensions
// regardless of the actual executable bit, per the user's own explicit
// request: .sh scripts are conventionally chmod +x and so already get
// glyphExecutable via the mode check below, but .js/.php/.py scripts
// commonly aren't marked executable at all and should still look the
// same as .sh. Checked ahead of, and independently from, the mode&0o111
// fallback in For, so a non-executable .sh/.js/.php/.py still matches.
var scriptExtensions = []string{".sh", ".js", ".php", ".py"}

// textExtensions all share glyphText — plain or structured text a
// person reads/edits directly, as opposed to compiled/binary content.
// ".md" is deliberately here rather than mapped to its own Devicons
// markdown glyph: the user's own explicit request groups it with txt
// rather than giving it a distinct icon. ".yml"/".yaml" used to be here
// too, but moved to extensionIcons' own glyphConfig entry per the
// user's later, more specific request to treat them like ".conf"
// instead (see extensionIcons' own doc comment). ".rtf"/".text"/".asc"/
// ".log"/".env"/".tex"/".htm"/".html" were added later still, same
// request shape: "same color and both icons as .txt".
var textExtensions = []string{".txt", ".md", ".rtf", ".text", ".asc", ".log", ".env", ".tex", ".htm", ".html"}

// exactNameIcons recognizes a handful of filenames (not extensions)
// that carry their own meaning regardless of what extension (if any)
// they have — Dockerfile and friends, plus Makefile, a build-tool file
// the same "exact name, not a pattern" way. Matched case-insensitively,
// before every extension-based check in For, so e.g. a renamed
// "makefile.bak" still doesn't match (filepath.Ext alone would see
// ".bak"; this map is checked against the whole lowercased name, not
// an extension, by design).
var exactNameIcons = map[string]rune{
	"dockerfile":          glyphDocker,
	"docker-compose.yml":  glyphDocker,
	"docker-compose.yaml": glyphDocker,
	"makefile":            glyphBuildTool,
}

// extensionIcons is deliberately a modest, hand-picked set — the file
// types the user actually asked for, not a port of every icon a Nerd
// Font offers. Extend here as concrete need comes up. Checked
// case-insensitively against filepath.Ext, for a plain TypeFile only
// (see For).
//
// ".go" and ".py" are both deliberately absent: nf-dev-go (U+E724) and
// nf-dev-python (U+E73C) each hit a missing/wrong-glyph problem, real-
// world reports against the same terminal, both in Devicons (nf-dev-*),
// the one category this package otherwise still trusted after Seti-UI/
// Custom and Octicons already failed elsewhere. Every other "language
// logo" codepoint for Python (nf-cod-python, nf-fa-python, nf-fae-
// python, nf-md-language_python, nf-seti-python) lives in one of those
// same already-unreliable categories, or a newer Font Awesome 5/6
// Brands addition at a similarly high, unproven codepoint (nf-fa-
// python, U+ED1B — the same "recent addition" risk glyphDiskImage's
// own doc comment declines nf-fa-compact_disc for). Classic FontAwesome
// 4 has no per-language logos at all, snake included.
//
// ".pdf"/".docx"/".doc"/".odt"/".deb" were all tried and dropped for a
// different reason: nf-fa-file_pdf_o, nf-fa-file_word_o, and nf-fa-cube
// all rendered, but "superklein" (too small to make out) at terminal
// size — the same legibility problem glyphArchive's own doc comment
// describes for nf-fa-file_archive_o, confirmed on the exact same
// font/terminal. Unlike glyphArchive/glyphImage, no bigger, equally
// on-topic FontAwesome 4 glyph was found for "PDF", "Word document", or
// "package" specifically, so all five extensions fall through to
// glyphFile rather than keep guessing at codepoints sight-unseen.
// ".json" was originally dropped the same way for sharing that same
// small "_o" file-outline cluster — since superseded by the user's own
// explicit, later request for a dedicated glyphDataFile icon instead
// (shared with .sql/.csv — see glyphDataFile's own doc comment), which
// sidesteps the legibility risk entirely by not being from that cluster
// (or any Nerd Font PUA range) at all.
//
// ".pom"/".config"/".properties"/".cfg"/".ini"/".rc"/".cnf"/".yaml"/
// ".yml"/".toml" all share glyphConfig with ".conf" itself, per the
// user's own explicit request to treat this whole "settings/config
// file" family the same way — .yaml/.yml/.toml deliberately included
// despite reading as "data" rather than "config" the way the others
// do: the user asked for a distinct icon there only if one could be
// found without the same missing/wrong-glyph risk every other new
// codepoint in this file has already run into (see glyphDataFile's own
// doc comment on why that one specifically was safe) — no such
// risk-free alternative exists for YAML/TOML specifically, so they stay
// on glyphConfig, kept visually apart instead by name color alone (see
// internal/ui/panel.go's own yamlColor).
var extensionIcons = map[string]rune{
	".rs":  rune(0xE7A8), // nf-dev-rust — not yet reported broken, but same Devicons risk as nf-dev-go/nf-dev-python above
	".iso": glyphDiskImage,
	".img": glyphDiskImage,

	".conf":       glyphConfig,
	".pom":        glyphConfig,
	".config":     glyphConfig,
	".properties": glyphConfig,
	".cfg":        glyphConfig,
	".ini":        glyphConfig,
	".rc":         glyphConfig,
	".cnf":        glyphConfig,
	".yaml":       glyphConfig,
	".yml":        glyphConfig,
	".toml":       glyphConfig,

	".json": glyphDataFile,
	".sql":  glyphDataFile,
	".csv":  glyphDataFile,
}

// archiveExtensions is a real multi-file container format — mirrors
// internal/ui's own archiveHighlightExtensions (panel.go) minus the
// bare single-file compression suffixes, which get glyphCompressed
// instead (see compressedExtensions and For's own doc comment), plus
// ".jar" (a zip container by construction, just with a Java-specific
// extension) on top of internal/ui's own set. Otherwise kept as its
// own copy rather than imported, since internal/ui depends on this
// package, not the other way around.
var archiveExtensions = []string{
	".zip", ".tar", ".tgz", ".tbz", ".tbz2", ".txz",
	".tar.gz", ".tar.bz2", ".tar.xz",
	".7z", ".rar", ".jar",
}

// compressedExtensions is a bare single-file compression format — not
// a container the way archiveExtensions' entries are, so it gets its
// own, different icon (glyphCompressed) per the user's own explicit
// request ("gz Dateien brauchen etwas anderes"). Checked only after
// archiveExtensions in For, so "backup.tar.gz" (also ending in ".gz")
// still reads as the container it actually is rather than this.
var compressedExtensions = []string{".gz", ".bz2", ".xz", ".lzma", ".lz", ".zst"}

var imageExtensions = []string{
	".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".svg", ".ico", ".tiff",
}

// hasAnySuffix reports whether lowerName ends in one of exts (each
// already lowercase) — the same shape internal/ui's own isArchiveName
// uses.
func hasAnySuffix(lowerName string, exts []string) bool {
	for _, ext := range exts {
		if strings.HasSuffix(lowerName, ext) {
			return true
		}
	}
	return false
}

// For returns entryType/name/mode's icon — type first, exactly
// mirroring typeGlyph's own precedence (a live or broken symlink, a
// special device/IPC type, is never masked by what the name happens to
// look like), then a modest name-based lookup for a plain TypeFile.
func For(entryType fsops.EntryType, name string, mode os.FileMode) rune {
	switch entryType {
	case fsops.TypeDir:
		if name == ".git" {
			return glyphFolderGit
		}
		return glyphFolder
	case fsops.TypeSymlinkDir:
		return glyphSymlinkDir
	case fsops.TypeSymlinkFile:
		return glyphSymlinkFile
	case fsops.TypeSymlinkBroken:
		return glyphSymlinkBroken
	case fsops.TypeSocket:
		return glyphSocket
	case fsops.TypeFIFO:
		return glyphFIFO
	case fsops.TypeCharDevice:
		return glyphCharDevice
	case fsops.TypeBlockDevice:
		return glyphBlockDevice
	}

	// TypeFile from here on.
	if glyph, ok := exactNameIcons[strings.ToLower(name)]; ok {
		return glyph
	}
	lower := strings.ToLower(name)
	if hasAnySuffix(lower, archiveExtensions) {
		return glyphArchive
	}
	if hasAnySuffix(lower, compressedExtensions) {
		return glyphCompressed
	}
	if hasAnySuffix(lower, imageExtensions) {
		return glyphImage
	}
	if hasAnySuffix(lower, textExtensions) {
		return glyphText
	}
	if glyph, ok := extensionIcons[strings.ToLower(filepath.Ext(name))]; ok {
		return glyph
	}
	if hasAnySuffix(lower, scriptExtensions) || mode&0o111 != 0 {
		return glyphExecutable
	}
	return glyphFile
}
