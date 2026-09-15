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

func (s transferSide) base(p string) string {
	if s.client == nil {
		return filepath.Base(p)
	}
	return path.Base(p)
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

// startRemotePaste is pasteInto's own remote-aware half — runs
// runRemotePaste in the background (see safeGo) so a slow network
// transfer never freezes the rest of the app, then hands the outcome
// to finishRemotePaste through QueueUpdateDraw once every item has
// been attempted.
func (r *Root) startRemotePaste(items []string, srcClient remotefs.Client, move bool, destClient remotefs.Client, destDir string) {
	if len(items) == 0 {
		return
	}
	panel := r.panel

	r.safeGo("remote paste", nil, func() {
		succeeded, skipped, firstErr := runRemotePaste(items, srcClient, move, destClient, destDir)
		r.app.QueueUpdateDraw(func() {
			r.finishRemotePaste(panel, succeeded, skipped, firstErr, move)
		})
	})
}

// runRemotePaste is startRemotePaste's own synchronous core — copies
// every item in turn (see copyTransferItem), then, for a move, removes
// each one's own top-level source once its copy actually succeeded
// (never for one that failed to copy in the first place), using the
// same Lstat-then-recurse removal every other delete in this project
// already uses (fsops.PurgeCompletely locally, removeRemoteRecursive
// remotely). srcClient/destClient follow the same "nil means local"
// convention as clipboardSourceClient/Panel.remote themselves.
//
// A move where both ends are the identical live remote connection
// (the same Client value — comparable here since every real
// implementation is a pointer, see remotefs.SFTPClient/fakeRemoteClient)
// skips all of that: it's really just one rename on the server's own
// filesystem, so it goes straight through Client.Rename instead,
// without ever streaming a single byte across the wire, let alone
// twice (down to this machine, then back up) the way copy-then-delete
// would otherwise cost for every file. Doesn't apply to a plain local
// move (pasteInto never routes one here at all — see its own doc
// comment) or to two panels merely connected to the same server as two
// separate sessions, which are two different Client values.
//
// Split out from startRemotePaste specifically so it's callable
// directly in a test with no real Application event loop behind it —
// the same reasoning connectdialog.go's own finishConnect doc comment
// gives for its identical split from runConnect.
func runRemotePaste(items []string, srcClient remotefs.Client, move bool, destClient remotefs.Client, destDir string) (succeeded int, skipped []transferSkip, firstErr error) {
	src := transferSide{client: srcClient}
	dest := transferSide{client: destClient}
	sameRemoteClient := move && srcClient != nil && srcClient == destClient

	for _, item := range items {
		destPath := dest.join(destDir, src.base(item))

		if sameRemoteClient {
			if err := destClient.Rename(item, destPath); err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", item, err)
				}
				continue
			}
			succeeded++
			continue
		}

		itemSkipped, err := copyTransferItem(src, dest, item, destPath, &skipped)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", item, err)
			}
			continue
		}
		if itemSkipped {
			continue // recorded in skipped already — not a success, and a move must leave a skipped symlink's own source alone
		}
		succeeded++
		if move {
			if err := removeTransferSource(srcClient, item); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("copied, but could not remove the original %s: %w", item, err)
			}
		}
	}
	return succeeded, skipped, firstErr
}

// finishRemotePaste applies runRemotePaste's own outcome: clears the
// clipboard once a clean move has fully landed (nothing left to paste
// again — mirrors finishPasteJob's identical local-paste behavior),
// reloads panel if it's still open, and reports any error/skip summary
// through the ordinary error overlay. Split out for the same
// directly-callable-without-a-real-event-loop reason runRemotePaste's
// own doc comment gives.
func (r *Root) finishRemotePaste(panel *Panel, succeeded int, skipped []transferSkip, firstErr error, move bool) {
	if move && firstErr == nil {
		r.setClipboard(nil, false)
	}
	if r.hasTab(panel) {
		r.showError(panel.load(panel.path))
	}
	r.showError(remotePasteSummaryError(succeeded, skipped, firstErr))
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
