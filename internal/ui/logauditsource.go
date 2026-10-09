// logauditsource.go bridges internal/logview's own FileSource
// abstraction to remotefs.Client — the one piece of Log Audit glue
// that genuinely needs to know about a remote connection at all. Kept
// in its own file, separate from logaudit.go's everyday discovery/read
// glue, the same "local/remote concerns get their own dedicated code,
// never silently share a code path" rule every other remote-aware
// feature in this app already follows (see panel.go's own isRemote
// doc comment).
package ui

import (
	"io"
	"path"

	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/logview"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// currentLogAuditSource picks LocalFileSource or a remote-backed one
// depending on whether the active panel is currently connected — per
// the user's own explicit request: "jL" used to always browse the
// local filesystem even while a panel was already connected to a
// remote session, when the whole point of asking for a directory on
// that session was to read through its own log files, not this
// machine's. Called fresh each time (openLogAuditViewer, "r"), the
// same "reflect whatever is true right now" reasoning
// reloadLogAuditDiscovery's own doc comment already gives for
// re-running Discover itself — a panel can connect or disconnect
// between two "jL" presses just as easily as its directory can change.
func (r *Root) currentLogAuditSource() logview.FileSource {
	if r.panel.isRemote() {
		return remoteLogFileSource{client: r.panel.remote}
	}
	return logview.LocalFileSource{}
}

// remoteLogFileSource adapts remotefs.Client to logview.FileSource —
// Join always uses "/" (path.Join, never filepath.Join's OS-native
// separator), matching Client's own doc comment that every path it
// deals in is POSIX-absolute against the remote filesystem regardless
// of what platform breakthrough itself is running on.
type remoteLogFileSource struct {
	client remotefs.Client
}

func (s remoteLogFileSource) ReadDir(dir string) ([]logview.SourceEntry, error) {
	entries, err := s.client.ListDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]logview.SourceEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, logview.SourceEntry{
			Name:      e.Name,
			IsDir:     e.IsDir,
			IsRegular: e.Type == fsops.TypeFile,
			Size:      e.Size,
			ModTime:   e.ModTime,
		})
	}
	return out, nil
}

func (s remoteLogFileSource) Open(p string) (io.ReadCloser, error) {
	return s.client.Open(p)
}

func (remoteLogFileSource) Join(dir, name string) string {
	return path.Join(dir, name)
}
