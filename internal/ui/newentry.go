package ui

import (
	"fmt"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// openNewFile is the "mf" chord's own action (see keymap.go's "m"
// family) and the context menu's "New file" entry: prompts for a name
// and creates an empty file directly inside the active panel's own
// current directory — a bare `touch` on a name that doesn't exist yet,
// the same one-level-only restriction fsops.CreateFile/createFileRemote
// both enforce. Remote-aware the same way Rename already is (see
// finishRename/renameRemote): dispatches to the panel's own connection
// when it has one, rather than always touching this machine.
func (r *Root) openNewFile() {
	if r.panel.inArchiveView() {
		r.showError(errNotSupportedInArchive)
		return
	}
	r.openPrompt("New file:", "", func(name string) {
		var err error
		if remote := r.panel.remote; remote != nil {
			_, err = createFileRemote(remote, r.panel.path, name)
		} else {
			_, err = fsops.CreateFile(r.panel.path, name)
		}
		if err != nil {
			r.activityLog.Error(activitylog.CategoryFileOps, fmt.Sprintf("new file %q: %v", name, err))
			r.showError(err)
			return
		}
		r.activityLog.Action(activitylog.CategoryFileOps, fmt.Sprintf("created file %q", name))
		r.showError(r.panel.load(r.panel.path))
	})
}

// openNewDir is openNewFile's own sibling for the "md" chord and the
// context menu's "New dir" entry — see its doc comment for the shared
// reasoning, `mkdir` in place of `touch`.
func (r *Root) openNewDir() {
	if r.panel.inArchiveView() {
		r.showError(errNotSupportedInArchive)
		return
	}
	r.openPrompt("New directory:", "", func(name string) {
		var err error
		if remote := r.panel.remote; remote != nil {
			_, err = createDirRemote(remote, r.panel.path, name)
		} else {
			_, err = fsops.CreateDir(r.panel.path, name)
		}
		if err != nil {
			r.activityLog.Error(activitylog.CategoryFileOps, fmt.Sprintf("new dir %q: %v", name, err))
			r.showError(err)
			return
		}
		r.activityLog.Action(activitylog.CategoryFileOps, fmt.Sprintf("created directory %q", name))
		r.showError(r.panel.load(r.panel.path))
	})
}
