// remotepaste.go is Copy/Cut/Paste's own remote-aware transfer engine
// — used whenever the clipboard's own source or the destination panel
// (or both) is a remote connection, in place of pasteconflict.go's own
// local-only engine (see pasteInto's own dispatch).
//
// Deliberately simpler than that one: every destination that already
// exists is skipped rather than offering pasteconflict.go's own rich
// per-item overwrite/rename/skip-all dialog, no "preserve attributes"
// or "follow symlinks" option (a symlink anywhere in the tree being
// copied is skipped outright, never followed and never recreated as a
// symlink on the other end — recreating one meaningfully across two
// different machines has no single right answer this project has
// picked yet), and no visible progress indicator while it runs, only
// a final summary once it's done. All of that is real, deliberately
// out-of-scope-for-now polish this first version doesn't have yet —
// see the user-guide's own "Remote connections" section for the
// current, honest list.
package ui

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/jagottsicher/breakthrough/internal/archive"
	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// transferSide wraps one endpoint of a remote-aware paste — nil
// client means the real local filesystem, matching remote/
// clipboardSourceClient's own "nil means local" convention throughout
// this package. Its own methods pick path/os or the Client's own
// method as appropriate, so copyTransferItem itself never needs to
// know which side of the transfer it's currently touching.
type transferSide struct {
	client remotefs.Client
}

func (s transferSide) join(dir, name string) string {
	if s.client == nil {
		return filepath.Join(dir, name)
	}
	return path.Join(dir, name)
}

// lstatType reports p's own fsops.EntryType without following a
// symlink — the same Lstat-based classification every other remote
// operation in this project already keys its own recursion/removal
// decisions on (see removeRemoteRecursive's own doc comment).
func (s transferSide) lstatType(p string) (fsops.EntryType, error) {
	if s.client == nil {
		fi, err := os.Lstat(p)
		if err != nil {
			return 0, err
		}
		switch {
		case fi.Mode()&os.ModeSymlink != 0:
			return fsops.TypeSymlinkFile, nil // exact symlink kind doesn't matter here — see copyTransferItem, every one is skipped alike
		case fi.IsDir():
			return fsops.TypeDir, nil
		default:
			return fsops.TypeFile, nil
		}
	}
	entry, err := s.client.Lstat(p)
	if err != nil {
		return 0, err
	}
	return entry.Type, nil
}

func (s transferSide) list(dir string) ([]fsops.Entry, error) {
	if s.client == nil {
		return fsops.ListDir(dir)
	}
	return s.client.ListDir(dir)
}

func (s transferSide) exists(p string) bool {
	if s.client == nil {
		_, err := os.Lstat(p)
		return err == nil
	}
	_, err := s.client.Lstat(p)
	return err == nil
}

// lstat is exists' own richer sibling, for the one caller (pasteWalk,
// once either side of a Paste is remote) that needs real file info —
// size/mtime/IsDir — for a conflict already confirmed to exist via
// exists, not just a yes/no answer. Returns a real os.FileInfo either
// way, so pasteConflict/resolveConflictAsync/pasteWalk's own
// autoMergeDirectories check never need to know or care which side of
// the transfer produced it — see entryFileInfo's own doc comment for
// how the remote half gets there.
func (s transferSide) lstat(p string) (os.FileInfo, error) {
	if s.client == nil {
		return os.Lstat(p)
	}
	entry, err := s.client.Lstat(p)
	if err != nil {
		return nil, err
	}
	return entryFileInfo{entry}, nil
}

// statOrNotExist is pasteWalk's own conflict check, generalized across
// local and remote: mirrors os.Lstat's own three-way "exists / genuinely
// doesn't / some other real error" contract exactly for the local side
// (nothing changes there — the identical os.Lstat/os.IsNotExist pair
// this always used), but folds the remote side's own coarser two-way
// answer into it rather than pretending a false distinction: an SFTP
// error has no reliable, protocol-guaranteed way to tell "no such file"
// apart from other failures the way os.IsNotExist can locally (nothing
// elsewhere in this project has ever needed to make that call before —
// see transferSide.exists' own doc comment, which already accepts the
// exact same simplification for every other remote existence check in
// this file). So a remote Lstat failure of any kind is treated as
// "doesn't exist" here, exactly like exists() already does — a
// genuinely different failure (permission denied on the parent
// directory, say) then simply surfaces moments later from whatever
// real operation pasteOne's own create/mkdir attempts next, still a
// real, reported error, just one step further downstream than the
// local path's own immediate one.
func (s transferSide) statOrNotExist(p string) (info os.FileInfo, notExist bool, err error) {
	if s.client == nil {
		info, err = os.Lstat(p)
		if err == nil {
			return info, false, nil
		}
		if os.IsNotExist(err) {
			return nil, true, nil
		}
		return nil, false, err
	}
	info, err = s.lstat(p)
	if err != nil {
		return nil, true, nil
	}
	return info, false, nil
}

// removeAll deletes an existing p outright — the one step ReplaceEntirely
// needs before recreating a directory conflict from scratch (see
// pasteTransferItem), mirroring os.RemoveAll's own "remove p and
// everything under it" contract for the remote side via
// removeRemoteRecursive.
func (s transferSide) removeAll(p string) error {
	if s.client == nil {
		return os.RemoveAll(p)
	}
	return removeRemoteRecursive(s.client, p, true)
}

// entryFileInfo adapts a remotefs Client's own fsops.Entry (from Lstat)
// to the real os.FileInfo interface pasteconflict.go's job/dialog
// machinery already speaks throughout (pasteConflict.srcInfo/dstInfo,
// resolveConflictAsync's ModTime/Size comparisons, pasteWalk's own
// IsDir check) — letting that whole machinery stay completely unaware
// of whether a given conflict's info came from a real local os.Lstat or
// a remote Client.Lstat call. Sys() has no local-os-specific analogue
// to report for a remote entry, so it returns nil, the same as any
// os.FileInfo implementation with nothing meaningful to put there.
type entryFileInfo struct {
	entry fsops.Entry
}

func (e entryFileInfo) Name() string       { return e.entry.Name }
func (e entryFileInfo) Size() int64        { return e.entry.Size }
func (e entryFileInfo) Mode() os.FileMode  { return e.entry.Mode }
func (e entryFileInfo) ModTime() time.Time { return e.entry.ModTime }
func (e entryFileInfo) IsDir() bool        { return e.entry.IsDir }
func (e entryFileInfo) Sys() any           { return nil }

// remotePathOverlaps is fsops.Overlaps' own remote counterpart, for the
// one case its local, filepath.Rel-based logic can't be reused for
// directly: two paths on the very same remote connection (see
// pasteOverlaps). Remote paths are always POSIX-"/"-separated and
// already absolute (see remotefs's own package doc comment), so the
// same "dst is src itself, or lives inside it" question reduces to a
// plain string prefix check via the "path" package instead of
// filepath.Rel's OS-specific one.
func remotePathOverlaps(src, dst string) bool {
	src = path.Clean(src)
	dst = path.Clean(dst)
	return dst == src || strings.HasPrefix(dst, src+"/")
}

// pasteOverlaps is pasteWalk's own overlap check, generalized across
// however many of job's two ends are remote — see fsops.Overlaps' own
// doc comment for why this matters at all (pasting into the very thing
// being read from). Two genuinely different machines (a local src with
// a remote dst, or two different remote connections) can never overlap
// by construction — a path on one has no meaning on the other — so this
// only ever does real work for a same-machine transfer: both local, or
// the same live remote Client value on both ends (comparable directly,
// since every real implementation is a pointer — see
// runRemotePaste's own doc comment for the identical comparison it
// already made for its own same-connection fast path).
func pasteOverlaps(job *pasteJob, src, dst string) bool {
	switch {
	case job.srcClient == nil && job.destClient == nil:
		return fsops.Overlaps(src, dst)
	case job.srcClient != nil && job.srcClient == job.destClient:
		return remotePathOverlaps(src, dst)
	default:
		return false
	}
}

func (s transferSide) open(p string) (io.ReadCloser, error) {
	if s.client == nil {
		return os.Open(p)
	}
	return s.client.Open(p)
}

func (s transferSide) create(p string) (io.WriteCloser, error) {
	if s.client == nil {
		return os.Create(p)
	}
	return s.client.Create(p)
}

func (s transferSide) mkdir(p string) error {
	if s.client == nil {
		return os.Mkdir(p, 0o755)
	}
	return s.client.Mkdir(p)
}

// transferSkip records one path this transfer deliberately left
// alone — always a symlink (see copyTransferItem) — so the caller can
// summarize "N symlinks skipped" once the whole transfer finishes,
// rather than the skip passing by silently.
type transferSkip struct {
	path string
}

// copyTransferItem copies srcPath (on src) to destPath (on dest) —
// recursively, for a real directory; skipped outright, recorded in
// *skipped, for a symlink of any kind; a plain streamed Open-to-Create
// copy for anything else. Refuses to overwrite an existing destPath —
// this engine's own "never silently replace" policy (see this file's
// own package doc comment) — reporting that refusal as a real error
// rather than a silent skip, since unlike a symlink this is something
// the user most likely *did* want copied, just not clobbering
// something already there.
//
// thisSkipped reports whether srcPath itself (not some descendant
// found during a directory's own recursion, which is recorded in
// *skipped instead and never affects the parent directory's own
// success) was a symlink left untouched — runRemotePaste's own loop
// needs this to tell "genuinely copied" apart from "skipped, nothing
// there to count as succeeded or, for a move, to remove the source
// of".
func copyTransferItem(src, dest transferSide, srcPath, destPath string, skipped *[]transferSkip) (thisSkipped bool, err error) {
	srcType, err := src.lstatType(srcPath)
	if err != nil {
		return false, err
	}

	switch srcType {
	case fsops.TypeSymlinkFile, fsops.TypeSymlinkDir, fsops.TypeSymlinkBroken:
		*skipped = append(*skipped, transferSkip{path: srcPath})
		return true, nil
	case fsops.TypeDir:
		if dest.exists(destPath) {
			return false, fmt.Errorf("%s already exists", destPath)
		}
		if err := dest.mkdir(destPath); err != nil {
			return false, err
		}
		children, err := src.list(srcPath)
		if err != nil {
			return false, err
		}
		for _, child := range children {
			if _, err := copyTransferItem(src, dest, src.join(srcPath, child.Name), dest.join(destPath, child.Name), skipped); err != nil {
				return false, err
			}
		}
		return false, nil
	default: // plain file, or a special file (socket/FIFO/device) treated the same as one — copying its byte content, not recreating the special node itself, is the only thing Open/Create can do either way
		if dest.exists(destPath) {
			return false, fmt.Errorf("%s already exists", destPath)
		}
		return false, copyTransferFile(src, dest, srcPath, destPath)
	}
}

func copyTransferFile(src, dest transferSide, srcPath, destPath string) error {
	r, err := src.open(srcPath)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()

	w, err := dest.create(destPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, r); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

// pasteTransferItem is copyTransferItem's own richer sibling: the same
// recursive copy, but Force/Mode-aware (see fsops.OverwriteMode's own
// doc comment for the two choices, mirrored here exactly — Copy's own
// "remove dst first, then recurse into what's now empty space for
// ReplaceEntirely; recurse straight in, overwriting only names both
// sides share, for MergeInto" shape, including copyDir's own detail
// that every nested child is written unconditionally once Force is
// true at the top, never asked about individually a second time) and
// progress-reporting (onFile/onBytes, the same contract
// fsops.CopyOptions/MoveOptions already give pasteOne locally) — so
// pasteOne can drive a remote-involving paste through the exact same
// job/dialog machinery as a local one, once either side of it is
// remote (see pasteWalk/pasteOne's own doc comments).
//
// skippedSymlinks collects every symlink path left untouched, the same
// "never followed, never recreated" simplification copyTransferItem's
// own package doc comment already documents — still true here, for the
// same reason: recreating a symlink meaningfully across two different
// machines has no single right answer this project has picked yet.
//
// topLevelSkipped reports whether srcPath itself (never a descendant
// found during a directory's own recursion, which only ever affects
// *skippedSymlinks — see copyTransferItem's own identical
// thisSkipped/skipped split for the reasoning) was a symlink left
// untouched — pasteOneRemote needs this to tell "genuinely copied, safe
// to remove the source of for a Cut" apart from "skipped: nothing there
// to have moved at all, the original must survive".
//
// Recursion always calls this real function directly, never through
// runPasteTransferItem below — the same "the swappable var is only the
// top-level entry point, recursion never re-enters it" shape
// fsops.Copy/copyDir already establish for fsCopy.
func pasteTransferItem(src, dest transferSide, srcPath, destPath string, force bool, mode fsops.OverwriteMode, onFile func(string), onBytes func(int64), skippedSymlinks *[]string) (topLevelSkipped bool, err error) {
	srcType, err := src.lstatType(srcPath)
	if err != nil {
		return false, err
	}

	if srcType == fsops.TypeSymlinkFile || srcType == fsops.TypeSymlinkDir || srcType == fsops.TypeSymlinkBroken {
		*skippedSymlinks = append(*skippedSymlinks, srcPath)
		return true, nil
	}

	exists := dest.exists(destPath)
	if exists && !force {
		return false, fmt.Errorf("%s already exists", destPath)
	}

	if srcType == fsops.TypeDir {
		if exists && mode == fsops.ReplaceEntirely {
			if err := dest.removeAll(destPath); err != nil {
				return false, err
			}
			exists = false
		}
		if !exists {
			if err := dest.mkdir(destPath); err != nil {
				return false, err
			}
		}
		children, err := src.list(srcPath)
		if err != nil {
			return false, err
		}
		for _, child := range children {
			// Force always propagates unchanged into every nested level,
			// the same as fsops.copyDir's own recursion — a MergeInto's
			// own "ask again per nested conflict" would need this dialog
			// to somehow surface a second round of questions mid-recursion,
			// which neither engine does; a name both sides share below the
			// top level is always just overwritten once Force already
			// allowed proceeding at all.
			if _, err := pasteTransferItem(src, dest, src.join(srcPath, child.Name), dest.join(destPath, child.Name), true, mode, onFile, onBytes, skippedSymlinks); err != nil {
				return false, err
			}
		}
		return false, nil
	}

	// A plain file (or a special file — socket/FIFO/device — copied the
	// same way, the only thing Open/Create can do with one either way):
	// force alone is enough to proceed, the same as fsops.Copy's own
	// "mode only matters for a directory" contract — overwriting a
	// file's own content has only one sensible meaning regardless of
	// Merge vs Replace.
	if onFile != nil {
		onFile(srcPath)
	}
	return false, pasteTransferFile(src, dest, srcPath, destPath, onBytes)
}

// runPasteTransferItem is pasteOneRemote's own entry point into
// pasteTransferItem — a package-level var for the same reason
// fsCopy/fsMove are (see their own doc comment): a test can substitute
// a wrapper that still calls through to the real implementation but
// signals once it actually returns, the one way to deterministically
// wait for a remote-involving pasteWalk's own background goroutine to
// have done its real work against a fake remote client.
var runPasteTransferItem = pasteTransferItem

// pasteTransferFile is copyTransferItem's own progress-reporting
// sibling — see pasteTransferItem's own doc comment for why a second,
// parallel function exists rather than adding onBytes to
// copyTransferFile itself: the archive-extraction path
// (runRemoteArchiveExtraction) that still calls the plain version has
// no job-wide progress to report into at all.
//
// The progress hook wraps r (the source), never w (the destination) —
// see newProgressReader's own doc comment for why that side of the
// wrap is the one that actually matters: wrapping w the way an earlier
// version of this function did unconditionally hid w's own
// io.ReaderFrom from io.Copy's own dispatch, silently falling back to
// a plain byte-shuffling loop even once remotefs.Dial started asking
// pkg/sftp for concurrent writes (see its own doc comment) — a real,
// user-reported case of copying between two machines on the very same
// LAN feeling far slower than the link itself could explain, because
// every 32KB chunk was still waiting for its own round trip before the
// next one went out.
//
// A copy error truncates dest to empty before returning it, rather
// than leaving whatever partial write is already sitting there:
// concurrent writes can otherwise leave a "hole" instead of a clean
// truncation on failure — a later chunk at a higher offset landing
// before an earlier one that then fails, leaving a file with the
// *right* final size but silently missing data partway through,
// rather than obviously, visibly incomplete the way a sequential
// failure always was. Best-effort: Truncate failing too just means
// leaving whatever's there, no worse than before this existed.
func pasteTransferFile(src, dest transferSide, srcPath, destPath string, onBytes func(int64)) error {
	r, err := src.open(srcPath)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()

	w, err := dest.create(destPath)
	if err != nil {
		return err
	}

	var reader io.Reader = r
	if onBytes != nil {
		reader = newProgressReader(r, onBytes)
	}
	if _, err := io.Copy(w, reader); err != nil {
		if t, ok := w.(interface{ Truncate(int64) error }); ok {
			_ = t.Truncate(0)
		}
		_ = w.Close()
		return err
	}
	return w.Close()
}

// newProgressReader wraps r, reporting the running total of bytes read
// so far after every chunk — the same running-total contract
// fsops.CopyOptions.OnBytes already documents for the local engine
// (see pasteTransferItem's own doc comment), just measured on the read
// side instead of the write side (see pasteTransferFile's own doc
// comment for why that side of the wrap is the one that matters here).
//
// Returns one of two different concrete shapes depending on whether r
// itself already implements io.WriterTo (a remote download's own
// *sftp.File source, which uses it for pkg/sftp's own concurrent-read
// fast path): wrapping a WriterTo-capable reader in a value that
// *also* declares WriteTo keeps io.Copy calling into it — io.Copy
// checks src.(io.WriterTo) before ever looking at the destination, so
// hiding it here would silently downgrade a download back to a plain
// read/write loop. Wrapping anything else (an upload's own local
// os.File source, which has no WriteTo of its own) in a value that
// deliberately has *no* WriteTo instead makes io.Copy check the
// destination's own io.ReaderFrom next — a remote upload's own
// concurrent-*write* fast path, the one this whole function exists to
// stop hiding.
func newProgressReader(r io.Reader, onBytes func(int64)) io.Reader {
	if wt, ok := r.(io.WriterTo); ok {
		return &progressWriterToReader{r: r, wt: wt, onBytes: onBytes}
	}
	return &progressReader{r: r, onBytes: onBytes}
}

// progressReader is newProgressReader's own plain shape — no WriteTo
// of its own, so io.Copy always falls through to checking the
// destination's own io.ReaderFrom instead (see newProgressReader's own
// doc comment).
type progressReader struct {
	r       io.Reader
	read    int64
	onBytes func(int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.read += int64(n)
		p.onBytes(p.read)
	}
	return n, err
}

// progressWriterToReader is newProgressReader's own other shape —
// declares WriteTo so io.Copy still reaches for r's own real one, with
// progress reported as each chunk actually lands on the destination
// (via progressWriteWrap) rather than only once the whole WriteTo call
// returns.
type progressWriterToReader struct {
	r       io.Reader
	wt      io.WriterTo
	read    int64
	onBytes func(int64)
}

// Read exists only so progressWriterToReader still satisfies
// io.Reader (Go doesn't let one type declare WriteTo alone and be
// used where a plain io.Reader is expected, e.g. as the src argument
// of a fallback read/write loop) — never actually reached through
// io.Copy, which always prefers WriteTo once it's present at all.
func (p *progressWriterToReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.read += int64(n)
		p.onBytes(p.read)
	}
	return n, err
}

func (p *progressWriterToReader) WriteTo(w io.Writer) (int64, error) {
	return p.wt.WriteTo(&progressWriteWrap{w: w, base: &p.read, onBytes: p.onBytes})
}

// progressWriteWrap is progressWriterToReader.WriteTo's own
// destination wrapper, incrementing the running total kept on the
// progressWriterToReader that created it (base) after every chunk.
// Plain, non-atomic arithmetic is safe here the same way it already
// is on progressReader.Read: pkg/sftp's own concurrent-read
// implementation fetches data over several parallel requests, but
// still delivers it to the io.Writer it was given — this wrap — one
// chunk at a time, in file order, from a single goroutine; a real
// io.Writer has no offset parameter to make anything else safe.
type progressWriteWrap struct {
	w       io.Writer
	base    *int64
	onBytes func(int64)
}

func (p *progressWriteWrap) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	*p.base += int64(n)
	p.onBytes(*p.base)
	return n, err
}

// finishRemotePaste applies runRemoteArchiveExtraction's own outcome —
// its only caller. An ordinary remote-involving Paste instead goes
// through startPaste/pasteconflict.go, sharing the exact same conflict
// dialog/queue a local Paste always has (see pasteJob's own
// srcClient/destClient doc comment). Reloads panel if it's still open
// and reports any error/skip summary through the ordinary error
// overlay. move is always false for archive extraction (Cut has
// nothing to remove afterward from inside a read-only archive) but
// kept as a parameter rather than hardcoded, since nothing about the
// logic itself assumes that.
func (r *Root) finishRemotePaste(panel *Panel, succeeded int, skipped []transferSkip, firstErr error, move bool) {
	if move && firstErr == nil {
		r.setClipboard(nil, false)
	}
	if r.hasTab(panel) {
		r.showError(panel.load(panel.path))
	}
	r.showError(remotePasteSummaryError(succeeded, skipped, firstErr))
}

// startRemoteArchiveExtraction is pasteInto's own action once
// Root.remoteArchiveExtractionFor has confirmed the clipboard names
// members inside a remote-staged archive and the destination is
// itself a remote directory — archive.Extract only ever writes to a
// real local directory, so this extracts into a fresh local temp
// directory first and then uploads each of the resulting top-level
// items (named exactly as archive.Extract's own doc comment
// describes: destDir/<member's own base name>) the same way an
// ordinary local-source Paste to a remote destination already uploads
// a real local tree (copyTransferItem) — no second, bespoke upload
// path. Reports through finishRemotePaste, the same as any other
// remote-aware paste; move is always false here, since Cut is already
// refused for an archive member before this is ever reached (see
// pasteInto).
func (r *Root) startRemoteArchiveExtraction(archiveLocalPath string, members []archive.Entry, destClient remotefs.Client, destDir string) {
	panel := r.panel
	r.safeGo("archive extract", nil, func() {
		succeeded, skipped, firstErr := runRemoteArchiveExtraction(archiveLocalPath, members, destClient, destDir)
		r.app.QueueUpdateDraw(func() {
			r.finishRemotePaste(panel, succeeded, skipped, firstErr, false)
		})
	})
}

// runRemoteArchiveExtraction is startRemoteArchiveExtraction's own
// synchronous core, split out the same directly-testable way every
// other remote transfer's own core function in this file already is:
// archive.Extract into a throwaway local temp directory (removed
// again once every item's own upload has been attempted, success or
// not), then copyTransferItem uploads each of its own top-level
// results in turn, the same as any other local-source tree headed to
// a remote destination.
func runRemoteArchiveExtraction(archiveLocalPath string, members []archive.Entry, destClient remotefs.Client, destDir string) (succeeded int, skipped []transferSkip, firstErr error) {
	tempDir, err := os.MkdirTemp("", "breakthrough-archive-extract-*")
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	if err := archive.Extract(archiveLocalPath, members, tempDir); err != nil {
		return 0, nil, fmt.Errorf("extract from %s: %w", archiveLocalPath, err)
	}

	src := transferSide{client: nil}
	dest := transferSide{client: destClient}
	seen := make(map[string]bool, len(members))
	for _, m := range members {
		name := path.Base(path.Clean(m.Path))
		if seen[name] {
			// Several marked members sharing one common top-level
			// ancestor (e.g. a directory plus one of its own already-
			// included descendants) already land in the same uploaded
			// tree the first time around — nothing left for a repeat to
			// do.
			continue
		}
		seen[name] = true

		itemSkipped, err := copyTransferItem(src, dest, src.join(tempDir, name), dest.join(destDir, name), &skipped)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", name, err)
			}
			continue
		}
		if !itemSkipped {
			succeeded++
		}
	}
	return succeeded, skipped, firstErr
}

// removeTransferSource deletes item outright once it's been
// successfully copied elsewhere, for a Cut+Paste — Lstat-based, the
// same "never resolve a symlink into its own target before deciding
// how to delete it" care every other remote/local delete in this
// project already takes.
func removeTransferSource(client remotefs.Client, item string) error {
	if client == nil {
		return fsops.PurgeCompletely(item)
	}
	entry, err := client.Lstat(item)
	if err != nil {
		return err
	}
	return removeRemoteRecursive(client, item, entry.Type == fsops.TypeDir)
}

// remotePasteSummaryError turns a remote paste's own outcome into one
// user-facing error, or nil if there's nothing worth reporting at all
// (everything copied, nothing skipped) — showError already treats a
// nil error as "say nothing further", the same as every other call
// site in this package.
func remotePasteSummaryError(succeeded int, skipped []transferSkip, firstErr error) error {
	if firstErr == nil && len(skipped) == 0 {
		return nil
	}
	var b strings.Builder
	if firstErr != nil {
		b.WriteString(firstErr.Error())
	}
	if len(skipped) > 0 {
		if b.Len() > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%d symlink(s) skipped (not supported yet)", len(skipped))
	}
	return fmt.Errorf("%s", b.String())
}
