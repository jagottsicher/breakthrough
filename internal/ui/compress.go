package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// Compress and Extract: real archive creation and unpacking, both
// always through a real external tool (zip/unzip/tar/gzip/bzip2/xz/
// zstd) — never a reimplementation of any compression algorithm, the
// same "shell out to the real tool" principle already used for Rsync
// and Sed Replace. Deliberately separate from internal/archive (which
// only ever browses/lists/extracts specific members of an already-open
// archive in pure Go): this is "the whole archive, in one step",
// reachable directly from plain browsing without ever entering it.

const compressPage = "compress"

// archiveFormat is one Compress/Extract format this app offers.
type archiveFormat struct {
	label string // shown in the Format dropdown
	ext   string // appended to the typed output name, e.g. ".tar.gz"

	// compressTools/extractTools are the real binaries each direction
	// needs, checked with checkTools before either ever runs — kept
	// separate (not one merged list) so having zip but not unzip still
	// leaves Compress fully working, and vice versa.
	compressTools []string
	extractTools  []string

	// compress/extract build the real shell command line for each
	// direction. Arguments are passed in already shell-quoted (see
	// shellQuoteArg), so neither function needs to know anything about
	// quoting itself.
	compress func(out string, targets []string) string
	extract  func(archive, destDir string) string

	// matches reports whether lower (an already-lowercased path) is this
	// format's own extension — used by archiveFormatFor to recognize an
	// existing file as extractable.
	matches func(lower string) bool
}

// archiveFormats is every Compress/Extract format, in the order the
// Format dropdown offers them — the same five extensions
// internal/archive already recognizes for browsing (zip, tar, tar.gz,
// tar.bz2, tar.xz), plus tar.zst: that package can't read it back (no
// zstd decompressor in Go's own standard library), but a real zstd
// binary handles it exactly like any of the others here.
//
// Every tar-based compressed format runs a plain "tar -cf -" (or "tar
// -x") piped through the one real compressor binary for that
// extension, rather than relying on tar's own bundled "-z"/"-j"/"-J"
// support: that support varies by which tar is actually installed
// (GNU tar vs. macOS/BSD's own bsdtar), while a plain pipe through
// gzip/bzip2/xz/zstd works identically everywhere those are installed,
// and lets checkTools name exactly the one binary that's missing.
func archiveFormats() []archiveFormat {
	return []archiveFormat{
		{
			label:         "zip (.zip)",
			ext:           ".zip",
			compressTools: []string{"zip"},
			extractTools:  []string{"unzip"},
			compress: func(out string, targets []string) string {
				return "zip -r " + out + " " + strings.Join(targets, " ")
			},
			extract: func(archive, destDir string) string {
				return "unzip -o " + archive + " -d " + destDir
			},
			matches: func(lower string) bool { return strings.HasSuffix(lower, ".zip") },
		},
		{
			label:         "tar (.tar)",
			ext:           ".tar",
			compressTools: []string{"tar"},
			extractTools:  []string{"tar"},
			compress: func(out string, targets []string) string {
				return "tar -cf " + out + " " + strings.Join(targets, " ")
			},
			extract: func(archive, destDir string) string {
				return "tar -xf " + archive + " -C " + destDir
			},
			matches: func(lower string) bool { return strings.HasSuffix(lower, ".tar") },
		},
		{
			label:         "tar.gz (.tar.gz, .tgz)",
			ext:           ".tar.gz",
			compressTools: []string{"tar", "gzip"},
			extractTools:  []string{"gzip", "tar"},
			compress: func(out string, targets []string) string {
				return "tar -cf - " + strings.Join(targets, " ") + " | gzip > " + out
			},
			extract: func(archive, destDir string) string {
				return "gzip -dc " + archive + " | tar -x -C " + destDir
			},
			matches: func(lower string) bool {
				return strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz")
			},
		},
		{
			label:         "tar.bz2 (.tar.bz2, .tbz2)",
			ext:           ".tar.bz2",
			compressTools: []string{"tar", "bzip2"},
			extractTools:  []string{"bzip2", "tar"},
			compress: func(out string, targets []string) string {
				return "tar -cf - " + strings.Join(targets, " ") + " | bzip2 > " + out
			},
			extract: func(archive, destDir string) string {
				return "bzip2 -dc " + archive + " | tar -x -C " + destDir
			},
			matches: func(lower string) bool {
				return strings.HasSuffix(lower, ".tar.bz2") || strings.HasSuffix(lower, ".tbz2") || strings.HasSuffix(lower, ".tbz")
			},
		},
		{
			label:         "tar.xz (.tar.xz, .txz)",
			ext:           ".tar.xz",
			compressTools: []string{"tar", "xz"},
			extractTools:  []string{"xz", "tar"},
			compress: func(out string, targets []string) string {
				return "tar -cf - " + strings.Join(targets, " ") + " | xz > " + out
			},
			extract: func(archive, destDir string) string {
				return "xz -dc " + archive + " | tar -x -C " + destDir
			},
			matches: func(lower string) bool {
				return strings.HasSuffix(lower, ".tar.xz") || strings.HasSuffix(lower, ".txz")
			},
		},
		{
			label:         "tar.zst (.tar.zst, .tzst)",
			ext:           ".tar.zst",
			compressTools: []string{"tar", "zstd"},
			extractTools:  []string{"zstd", "tar"},
			compress: func(out string, targets []string) string {
				return "tar -cf - " + strings.Join(targets, " ") + " | zstd > " + out
			},
			extract: func(archive, destDir string) string {
				return "zstd -dc " + archive + " | tar -x -C " + destDir
			},
			matches: func(lower string) bool {
				return strings.HasSuffix(lower, ".tar.zst") || strings.HasSuffix(lower, ".tzst")
			},
		},
	}
}

// archiveFormatID is one archiveFormat's own stable identifier for
// config.Settings.CompressFormat — ext with its leading "." stripped
// ("zip", "tar.gz", ...), rather than label (which also carries every
// recognized extension in parentheses, not a stable machine-readable
// value) or the slice index itself (not stable across a reorder of
// archiveFormats).
func archiveFormatID(f archiveFormat) string {
	return strings.TrimPrefix(f.ext, ".")
}

// archiveFormatIndexByID returns formats' own index whose
// archiveFormatID matches id, or 0 (zip, archiveFormats' own first
// entry) if id is empty, stale (a format this build no longer offers),
// or otherwise unrecognized — the same forgiving, unvalidated-at-load-
// time handling every other enum setting already gets (see e.g.
// FindColorScheme's own fallback).
func archiveFormatIndexByID(formats []archiveFormat, id string) int {
	for i, f := range formats {
		if archiveFormatID(f) == id {
			return i
		}
	}
	return 0
}

// archiveFormatFor reports which archiveFormats entry path's own
// extension matches (case-insensitively), if any — Extract's own way
// of recognizing a file as an archive at all, independent of
// internal/archive's own, narrower Classify (see this file's own
// package doc comment on why the two are separate).
func archiveFormatFor(path string) (archiveFormat, bool) {
	lower := strings.ToLower(path)
	for _, f := range archiveFormats() {
		if f.matches(lower) {
			return f, true
		}
	}
	return archiveFormat{}, false
}

// shellQuoteArg wraps s in single quotes for safe use inside a real
// shell command line — the same POSIX sh idiom (and the same escaping)
// internal/rsync's own shellQuote already uses for exactly this reason;
// duplicated here rather than exported and shared, since it's a
// three-line, fully self-contained utility and internal/rsync has no
// other reason to be a dependency of internal/ui.
func shellQuoteArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// checkTools reports a clear, actionable error naming every one of
// names that isn't on $PATH, or nil if all of them are — checked
// before either Compress or Extract ever runs a real command, so a
// format whose tool isn't installed (zstd and xz especially, absent by
// default on plenty of systems, macOS included) fails with "install
// this" rather than a shell's own, less legible "command not found"
// buried in whatever output already scrolled past.
func checkTools(names []string) error {
	var missing []string
	for _, name := range names {
		if _, err := exec.LookPath(name); err != nil {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%s not found on $PATH — install it first (e.g. via apt/dnf/pacman on Linux, or Homebrew on macOS)", strings.Join(missing, ", "))
}

// openCompress is the context menu's "Compress…", and (through the "j"
// chord's own "jc" — see keymap.go) the keyboard's action: archives the
// current selection (one file, several files, or a whole directory —
// selectedOrCurrentPaths, the same target set Copy/Cut/Multiply already
// use) into a new file right beside it, via a real external tool
// chosen from the Format dropdown, or — once a split is active — into
// the other pane's own directory instead (see runCompress), the same
// default Extract already uses (see splitPartner).
//
// The active panel itself (where the selection actually lives) has to
// be local: there is no remote source path yet — compressing a
// selection that lives on a remote connection would first need to
// download all of it, the same larger, separate scope
// enterRemoteArchive's own "browse into a remote archive" already
// carved out for itself rather than folding into this feature. The
// *destination* may be remote, though — see runCompressToRemote.
func (r *Root) openCompress() {
	if r.panel.inArchiveView() {
		r.showError(errNotSupportedInArchive)
		return
	}
	if r.panel.remote != nil {
		r.showError(fmt.Errorf("compress: this panel must be local (the destination pane may be remote, once a split is active)"))
		return
	}
	targets := r.selectedOrCurrentPaths()
	if len(targets) == 0 {
		return
	}

	r.compressTargets = targets
	r.compressFormatIndex = archiveFormatIndexByID(archiveFormats(), r.settings.CompressFormat)
	r.compressOutputName = defaultCompressOutputName(targets, r.panel.path)
	r.renderCompressForm()
	r.renderCompressPreview()

	// 11 rows: Target/Format/Output name (3, all height 1) + 2 rows of
	// itemPadding between them + 2 rows of the Form's own top/bottom
	// border padding (7) + Preview (1) + Spacer (1) + Buttons (1) + the
	// title bar's own row (1) — the same derivation newDuplicateForm's
	// own doc comment spells out in full for its own, larger form.
	width, height := 70, 11
	_, _, screenWidth, screenHeight := r.GetRect()
	if width > screenWidth-4 {
		width = screenWidth - 4
	}
	if height > screenHeight-4 {
		height = screenHeight - 4
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	r.compressLayout.SetRect(x, y, width, height)
	r.showOverlay(compressPage, r.compressLayout)
}

// defaultCompressOutputName picks Compress's own starting "Output name"
// — the single target's own base name for exactly one target (the
// common case: "compress this one folder"), or the current directory's
// own name for several at once (the same "name it after where they
// live" convention a real GUI file manager's own "compress N items"
// already follows), falling back to the generic "archive" only once
// currentDir itself has no meaningful name of its own (the filesystem
// root).
func defaultCompressOutputName(targets []string, currentDir string) string {
	if len(targets) == 1 {
		return filepath.Base(targets[0])
	}
	if base := filepath.Base(currentDir); base != "" && base != "." && base != string(filepath.Separator) {
		return base
	}
	return "archive"
}

// newCompressForm builds the (initially empty) Compress form — called
// once from NewRoot; renderCompressForm populates it fresh on every
// open, the same reasoning newDuplicateForm's own doc comment gives.
func (r *Root) newCompressForm() *tview.Form {
	f := tview.NewForm()
	f.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			r.hideOverlay()
			return nil
		}
		return event
	})
	return f
}

// renderCompressForm (re)builds compressForm's own items: Target
// (read-only, the same duplicateTargetsLabel wording Multiply's own
// identical field already uses — nothing about naming a target set is
// specific to either dialog), Format, and Output name.
func (r *Root) renderCompressForm() {
	r.compressForm.Clear(true)

	r.compressForm.AddTextView("Target", duplicateTargetsLabel(r.compressTargets), 0, 1, true, false)

	formats := archiveFormats()
	labels := make([]string, len(formats))
	for i, f := range formats {
		labels[i] = f.label
	}
	dd := tview.NewDropDown().SetLabel("Format").SetOptions(labels, func(_ string, index int) {
		if index < 0 || index >= len(formats) || index == r.compressFormatIndex {
			return
		}
		r.compressFormatIndex = index
		r.renderCompressPreview()
	})
	dd.SetCurrentOption(r.compressFormatIndex)
	styleDropDown(dd, r.theme)
	r.compressFormatField = dd
	r.compressForm.AddFormItem(dd)

	nameField := tview.NewInputField().SetLabel("Output name").SetText(r.compressOutputName)
	nameField.SetChangedFunc(func(v string) {
		r.compressOutputName = v
		r.renderCompressPreview()
	})
	r.compressOutputNameField = nameField
	r.compressForm.AddFormItem(nameField)
}

// newCompressPreviewView builds compressPreviewView once, from NewRoot
// — the same "plain, read-only TextView sibling of the Form, never one
// of its own items" shape newDuplicatePreviewView's own doc comment
// explains in full (a TextView added as a Form item needs an explicit
// height or tview substitutes a 5-row default).
func (r *Root) newCompressPreviewView() *tview.TextView {
	v := tview.NewTextView()
	v.SetBorderPadding(0, 0, 1, 0)
	return v
}

// renderCompressPreview shows the exact file name Compress would
// create right now — the typed Output name plus the selected Format's
// own extension, recomputed on every keystroke and every Format change
// so neither one can silently disagree with what "Compress" is about
// to do.
func (r *Root) renderCompressPreview() {
	if r.compressPreviewView == nil {
		return
	}
	format := archiveFormats()[r.compressFormatIndex]
	name := strings.TrimSpace(r.compressOutputName)
	if name == "" {
		r.compressPreviewView.SetText("Preview: (enter an output name)")
		return
	}
	r.compressPreviewView.SetText("Preview: " + name + format.ext)
}

// newCompressButtons builds compressForm's own action row once, from
// NewRoot — the same Cancel/action button pair newDuplicateButtons
// already establishes.
func (r *Root) newCompressButtons() *tview.Flex {
	r.compressCancelBtn = tview.NewButton("Cancel").SetSelectedFunc(r.hideOverlay)
	r.compressApplyBtn = tview.NewButton("Compress").SetSelectedFunc(r.runCompress)
	r.compressCancelBtn.SetInputCapture(spaceAlsoActivates(r.hideOverlay))
	r.compressApplyBtn.SetInputCapture(spaceAlsoActivates(r.runCompress))

	exitFunc := func(key tcell.Key) {
		if key == tcell.KeyEscape {
			r.hideOverlay()
		}
	}
	r.compressCancelBtn.SetExitFunc(exitFunc)
	r.compressApplyBtn.SetExitFunc(exitFunc)

	return tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.compressCancelBtn, 0, 1, false).
		AddItem(r.compressApplyBtn, 0, 1, false)
}

// newCompressLayout wraps compressTitleBar above compressContentLayout
// — the same widget/layout split newDuplicateLayout already establishes.
func (r *Root) newCompressLayout() *tview.Flex {
	r.compressTitleBar = newPlainTitleBar("Compress")
	r.compressContentLayout = r.newCompressContentLayout()
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.compressTitleBar, 1, 0, false).
		AddItem(r.compressContentLayout, 0, 1, true)
}

// newCompressContentLayout stacks compressForm (7 rows: 3 items, 2 rows
// of itemPadding between them, 2 rows of the Form's own border padding
// — the same derivation newDuplicateContentLayout's own doc comment
// spells out in full), compressPreviewView, compressSpacer, and
// compressButtons.
func (r *Root) newCompressContentLayout() *tview.Flex {
	layout := tview.NewFlex().SetDirection(tview.FlexRow)
	layout.AddItem(r.compressForm, 7, 0, true)
	layout.AddItem(r.compressPreviewView, 1, 0, false)
	layout.AddItem(r.compressSpacer, 1, 0, false)
	layout.AddItem(r.compressButtons, 1, 0, false)
	return layout
}

// compressTargetName is one compress target's own name, as it should
// appear in the shell command line run with cmd.Dir already set to
// destDir (see runCompress) — target's own base name for an ordinary
// file or subdirectory, or "." for the one case that base name would be
// wrong: target *is* destDir itself, reached by selecting the current
// directory as a whole via the cursor sitting on ".." (see
// Panel.CurrentRowPath's own doc comment on why that row's path is the
// current directory, not its parent). "bt-verify-dir" (the directory's
// own name) doesn't exist as an entry *inside* "bt-verify-dir" itself —
// a real, live-tested failure ("zip warning: name not matched") this
// case needs "." for instead, the same relative name a real shell
// prompt would use to mean "everything right here".
func compressTargetName(target, destDir string) string {
	if target == destDir {
		return "."
	}
	return filepath.Base(target)
}

// runCompress is compressButtons' own "Compress": refuses an empty
// name, checks the chosen format's own required tools, then either
// hands the real command line straight to compressjob.go's own
// background engine (see startCompressJob) — the same "runs behind the
// scenes, the tab re-renders once it's done" shape Copy/Cut/Paste
// already have — or, once a split is active and the other pane is a
// remote connection, routes through runCompressToRemote instead (see
// its own doc comment).
//
// destPanel is the split partner's own panel once a split is active
// (the same default extractCurrentArchive already uses — see
// splitPartner), or r.panel itself otherwise: "compress into the other
// pane" only where there's a genuine "other pane" to mean, exactly the
// existing "no split, no guessing" reasoning defaultRsyncDestination's
// own doc comment already gives.
func (r *Root) runCompress() {
	format := archiveFormats()[r.compressFormatIndex]
	name := strings.TrimSpace(r.compressOutputName)
	if name == "" {
		r.showError(fmt.Errorf("compress: an output name is required"))
		return
	}

	// Self-adapting, per the user's own explicit request that this
	// follow the same shape Duplicate's own settings already have (see
	// applyDuplicateSelection's own doc comment): whichever format is
	// actually used becomes the new sticky default, through the exact
	// same optionSpec.apply the Options screen itself uses.
	id := archiveFormatID(format)
	if opt, ok := optionSpecByKey("compress_format"); ok {
		opt.apply(r, id)
	}

	sourceDir := r.panel.path
	destPanel := r.panel
	if partnerIdx, ok := r.splitPartner(); ok {
		destPanel = r.tabs[partnerIdx]
	}
	outputName := name + format.ext

	if err := checkTools(format.compressTools); err != nil {
		r.showError(fmt.Errorf("compress: %w", err))
		return
	}

	if destPanel.remote != nil {
		r.runCompressToRemote(format, outputName, sourceDir, destPanel)
		return
	}

	destDir := destPanel.path
	outPath := filepath.Join(destDir, outputName)
	if _, err := os.Stat(outPath); err == nil {
		r.showError(fmt.Errorf("compress: %s already exists — pick a different name", filepath.Base(outPath)))
		return
	}

	names := make([]string, len(r.compressTargets))
	for i, t := range r.compressTargets {
		names[i] = shellQuoteArg(compressTargetName(t, sourceDir))
	}
	// outPath, not just outputName: cmd.Dir (set to sourceDir — see
	// reallyStartCompressJob) has to stay the *source* directory so the
	// target names above resolve correctly, so the archive's own
	// destination — sourceDir itself, or a different local pane's own
	// directory once a split is active — needs its full path spelled
	// out instead of relying on cmd.Dir to supply it.
	command := format.compress(shellQuoteArg(outPath), names)

	r.hideOverlay()
	r.startCompressJob(compressRequest{
		command:    command,
		errContext: "compress",
		verb:       "Compressing",
		label:      filepath.Base(outPath),
		destDir:    destDir,
	})
}

// runCompressToRemote is runCompress' own remote-destination path:
// compresses locally into a throwaway temp file (no real shell command
// can write directly to an SFTP path), then, once that succeeds,
// uploads it to destPanel's own remote directory (see
// compressRequest.onSuccess) — its own separate background stage, with
// its own "Uploading" status-bar entry, rather than one job silently
// doing two unrelated things. Refuses up front, before compressing
// anything, if a file already exists at the remote destination — the
// same "never silently overwrite" refusal the local path already
// makes, just checked with transferSide.exists instead of os.Stat.
func (r *Root) runCompressToRemote(format archiveFormat, outputName, sourceDir string, destPanel *Panel) {
	remote := destPanel.remote
	destSide := transferSide{client: remote}
	destPath := destSide.join(destPanel.path, outputName)
	if destSide.exists(destPath) {
		r.showError(fmt.Errorf("compress: %s already exists on %s — pick a different name", outputName, destPanel.remoteConn.Label()))
		return
	}

	tempPath, err := newCompressTempFile(format.ext)
	if err != nil {
		r.showError(fmt.Errorf("compress: %w", err))
		return
	}

	names := make([]string, len(r.compressTargets))
	for i, t := range r.compressTargets {
		names[i] = shellQuoteArg(compressTargetName(t, sourceDir))
	}
	command := format.compress(shellQuoteArg(tempPath), names)

	r.hideOverlay()
	r.startCompressJob(compressRequest{
		command:    command,
		errContext: "compress",
		verb:       "Compressing",
		label:      outputName,
		onSuccess: func(r *Root) {
			r.startCompressUpload(tempPath, destPanel, destPath, outputName)
		},
	})
}

// newCompressTempFile reserves a real, unique temporary path for a
// compress-to-remote stage's own local staging copy, then removes it
// again immediately — this only ever needs the *name*, never the
// (empty) file os.CreateTemp itself creates: zip in particular treats
// an already-existing target as an archive to update rather than
// create fresh, which an empty, invalid placeholder file would only
// confuse. The same real, unique-name-then-remove idiom
// downloadRemoteToTemp's own os.CreateTemp call establishes, just
// without that one's own subsequent write.
func newCompressTempFile(ext string) (string, error) {
	f, err := os.CreateTemp("", "breakthrough-compress-*"+ext)
	if err != nil {
		return "", err
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return name, nil
}

// startCompressUpload is runCompressToRemote's own follow-on stage,
// started only once the local compress itself has actually succeeded
// (see its own onSuccess) — runs uploadCompressedFile in the
// background, then reloads whichever tab shows destPanel's own
// directory once the upload succeeds.
func (r *Root) startCompressUpload(localPath string, destPanel *Panel, destPath, label string) {
	remote := destPanel.remote
	r.startCompressJob(compressRequest{
		errContext: "compress",
		verb:       "Uploading",
		label:      label,
		goFunc:     func(context.Context) error { return uploadCompressedFile(localPath, remote, destPath) },
		onSuccess: func(r *Root) {
			r.forEachTab(func(p *Panel) {
				if p.remote == remote && p.path == destPanel.path {
					r.showError(p.load(p.path))
				}
			})
		},
	})
}

// uploadCompressedFile is startCompressUpload's own synchronous core —
// split out so a test can call it directly against a fake
// remotefs.Client, without needing to run the enclosing background job
// machinery at all (the same reasoning watchAndWaitRsync's own doc
// comment in rsyncjob.go gives for its identical split — this project's
// own tests never run a real Application event loop, and
// reallyStartCompressGoFunc's own r.app.QueueUpdateDraw call needs one
// to mean anything for a job that's actually still in flight).
//
// Uploads localPath to destPath via copyTransferItem (the same
// recursive local↔remote transfer primitive Copy/Cut/Paste's own
// remote engine already uses — see remotepaste.go's own package doc
// comment), and removes the local staging copy regardless of outcome —
// its only reason to exist was to get uploaded.
func uploadCompressedFile(localPath string, remote remotefs.Client, destPath string) error {
	defer func() { _ = os.Remove(localPath) }()
	var skipped []transferSkip
	_, err := copyTransferItem(transferSide{}, transferSide{client: remote}, localPath, destPath, &skipped)
	return err
}

// extractCurrentArchive is the context menu's "Extract"/"Extract,
// delete original", and (through the "j" chord's own "je"/"jE" — see
// keymap.go) the keyboard's action: unpacks the archive currently under
// the cursor with a real external tool (see archiveFormatFor), into
// either the archive's own directory or, if a split is currently
// active, the other pane's own current directory — the same "the other
// pane is the destination once a split exists" default Rsync/Compare
// already use (see splitPartner), rather than asking every time. Runs
// in the background (see startCompressJob) exactly like Compress —
// finishCompressJob reloads whichever tab(s) show destDir once it's
// actually done, so there is no "toOtherPane" branch to reload one
// place or another synchronously here any more.
//
// The archive itself has to sit on a local panel: there is no remote
// source path yet, the same scope limit openCompress' own doc comment
// explains in full. The *destination* may be remote, once a split is
// active and the other pane is a connected tab — see
// extractCurrentArchiveToRemote.
//
// deleteOriginal, once extraction has actually succeeded, moves the
// original archive to the Trash (never a hard delete outright — see
// deleteExtractedArchive for what happens if that fails).
func (r *Root) extractCurrentArchive(deleteOriginal bool) {
	if r.panel.inArchiveView() {
		r.showError(errNotSupportedInArchive)
		return
	}
	if r.panel.remote != nil {
		r.showError(fmt.Errorf("extract: this panel must be local (the destination pane may be remote, once a split is active)"))
		return
	}
	_, archivePath, ok := r.panel.CurrentRowPath()
	if !ok {
		return
	}
	format, ok := archiveFormatFor(archivePath)
	if !ok {
		r.showError(fmt.Errorf("extract: %s is not a recognized archive format", filepath.Base(archivePath)))
		return
	}

	destPanel := r.panel
	if partnerIdx, ok := r.splitPartner(); ok {
		destPanel = r.tabs[partnerIdx]
	}

	if err := checkTools(format.extractTools); err != nil {
		r.showError(fmt.Errorf("extract: %w", err))
		return
	}

	if destPanel.remote != nil {
		r.extractCurrentArchiveToRemote(format, archivePath, destPanel, deleteOriginal)
		return
	}

	destDir := destPanel.path
	command := format.extract(shellQuoteArg(archivePath), shellQuoteArg(destDir))
	req := compressRequest{
		command:    command,
		errContext: fmt.Sprintf("extract %s", filepath.Base(archivePath)),
		verb:       "Extracting",
		label:      filepath.Base(archivePath),
		destDir:    destDir,
	}
	if deleteOriginal {
		req.deleteOriginal = archivePath
	}
	r.startCompressJob(req)
}

// extractCurrentArchiveToRemote is extractCurrentArchive's own remote-
// destination path: extracts locally into a throwaway temp directory
// (no real shell command can write directly to an SFTP path), then,
// once that succeeds, uploads what it produced into destPanel's own
// remote directory (see startExtractUpload) — its own separate
// background stage, with its own "Uploading" status-bar entry, rather
// than one job silently doing two unrelated things.
func (r *Root) extractCurrentArchiveToRemote(format archiveFormat, archivePath string, destPanel *Panel, deleteOriginal bool) {
	tempDir, err := os.MkdirTemp("", "breakthrough-extract-*")
	if err != nil {
		r.showError(fmt.Errorf("extract: %w", err))
		return
	}

	command := format.extract(shellQuoteArg(archivePath), shellQuoteArg(tempDir))
	r.startCompressJob(compressRequest{
		command:    command,
		errContext: fmt.Sprintf("extract %s", filepath.Base(archivePath)),
		verb:       "Extracting",
		label:      filepath.Base(archivePath),
		onSuccess: func(r *Root) {
			r.startExtractUpload(tempDir, destPanel, archivePath, deleteOriginal)
		},
	})
}

// startExtractUpload is extractCurrentArchiveToRemote's own follow-on
// stage: runs uploadExtractedTree in the background, then, once it
// succeeds, reloads whichever tab shows destPanel's own directory and
// runs deleteOriginal's own Trash step — only now that the whole
// extraction has actually landed on the real remote destination, the
// same "only after a real success" guarantee the local path already
// gives.
func (r *Root) startExtractUpload(tempDir string, destPanel *Panel, archivePath string, deleteOriginal bool) {
	remote := destPanel.remote
	r.startCompressJob(compressRequest{
		errContext: fmt.Sprintf("extract %s", filepath.Base(archivePath)),
		verb:       "Uploading",
		label:      filepath.Base(archivePath),
		goFunc:     func(context.Context) error { return uploadExtractedTree(tempDir, destPanel) },
		onSuccess: func(r *Root) {
			r.forEachTab(func(p *Panel) {
				if p.remote == remote && p.path == destPanel.path {
					r.showError(p.load(p.path))
				}
			})
			if deleteOriginal {
				r.deleteExtractedArchive(archivePath)
			}
		},
	})
}

// uploadExtractedTree is startExtractUpload's own synchronous core —
// split out so a test can call it directly against a fake
// remotefs.Client, the same reasoning uploadCompressedFile's own doc
// comment gives.
//
// Uploads every entry directly inside tempDir into destPanel's own
// remote directory via copyTransferItem — one call per top-level
// entry, never the temp directory itself, which would try to create
// destPanel's own already-existing directory anew and fail outright.
// Every entry is checked for a remote conflict before any of them are
// actually uploaded, so a name collision partway through never leaves
// the remote destination half-extracted. tempDir is removed afterward
// regardless of outcome — its only reason to exist was to get
// uploaded.
func uploadExtractedTree(tempDir string, destPanel *Panel) error {
	defer func() { _ = os.RemoveAll(tempDir) }()
	local := transferSide{}
	dest := transferSide{client: destPanel.remote}
	children, err := local.list(tempDir)
	if err != nil {
		return err
	}
	for _, child := range children {
		if dest.exists(dest.join(destPanel.path, child.Name)) {
			return fmt.Errorf("%s already exists on %s", child.Name, destPanel.remoteConn.Label())
		}
	}
	var skipped []transferSkip
	for _, child := range children {
		srcPath := local.join(tempDir, child.Name)
		destPath := dest.join(destPanel.path, child.Name)
		if _, err := copyTransferItem(local, dest, srcPath, destPath, &skipped); err != nil {
			return err
		}
	}
	return nil
}

// deleteExtractedArchive is extractCurrentArchive's own "delete
// original" half, split out so it only ever runs after extraction has
// already succeeded — moves archivePath to the Trash, the same
// reversible-by-default choice moveSelectionToTrash already makes for
// "d" elsewhere in this app. If that fails outright (trash
// unavailable, or the move itself errors), this does not just silently
// leave the archive behind: it asks, explicitly naming that the
// fallback is a real, permanent delete, before ever doing one — the
// same "irreversible actions must be clearly flagged and confirmed"
// principle openRemoveConfirm's own confirmation already follows.
//
// Reloads whichever open tab shows archivePath's own directory by path
// (see forEachTab) rather than blindly reloading r.panel — this now
// always runs well after a backgrounded Extract has finished (see
// finishCompressJob), by which point the user is free to have switched
// to a different tab, so "the current panel" and "the archive's own
// former directory" are no longer guaranteed to be the same thing.
func (r *Root) deleteExtractedArchive(archivePath string) {
	sourceDir := filepath.Dir(archivePath)
	reloadSource := func() {
		r.forEachTab(func(p *Panel) {
			if p.path == sourceDir {
				r.showError(p.load(p.path))
			}
		})
	}

	dir, err := r.trashDir()
	if err == nil {
		if err = fsops.MoveToTrash(archivePath, dir); err == nil {
			r.activityLog.Action(activitylog.CategoryFileOps, fmt.Sprintf("moved extracted archive %q to trash", archivePath))
			reloadSource()
			return
		}
	}
	r.openPurgeConfirm(
		fmt.Sprintf("Moving %q to Trash failed (%v) — delete it completely instead?", filepath.Base(archivePath), err),
		func() {
			if err := fsops.PurgeCompletely(archivePath); err != nil {
				r.activityLog.Error(activitylog.CategoryFileOps, fmt.Sprintf("delete extracted archive %q: %v", archivePath, err))
				r.showError(err)
				return
			}
			r.activityLog.Action(activitylog.CategoryFileOps, fmt.Sprintf("permanently deleted extracted archive %q", archivePath))
			reloadSource()
		},
	)
}
