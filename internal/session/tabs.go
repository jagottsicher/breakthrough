package session

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// StateDir returns $XDG_STATE_HOME/breakthrough, falling back to
// ~/.local/state/breakthrough when that's unset — the XDG State directory,
// the same two-tier resolution dataDir already does for $XDG_DATA_HOME
// (see its own doc comment), just for the other half of the spec.
//
// State, deliberately, rather than config or data: the XDG spec reserves
// this directory for state that should persist between restarts but isn't
// either portable user configuration or the user's own documents —
// "current state of the application that can be reused on a restart" is
// close to a verbatim description of what SaveTabs writes here. A saved
// tab layout is exactly that: worth restoring, but not something anyone
// would hand-edit, sync to another machine, or lose sleep over if it were
// deleted.
//
// Returns "" (rather than an error) when neither $XDG_STATE_HOME nor a
// home directory can be determined — every caller here treats an
// unavailable state directory as "no saved state", not as a failure worth
// interrupting the program for.
func StateDir() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "breakthrough")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "breakthrough")
}

// TabsPath is where SaveTabs/LoadTabs keep the saved tab layout —
// StateDir's own directory plus "tabs". Returns "" when StateDir does
// (see its own doc comment).
func TabsPath() string {
	dir := StateDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "tabs")
}

// TabState is one saved tab layout: every open tab's directory, in tab
// order, plus which one was active when it was saved.
//
// Only the paths, deliberately — not the per-tab navigation history,
// filter text, sort order, or search results a live tab also carries.
// Restoring a path is the part that saves the user real work (getting
// back to a deep directory); restoring a half-finished search or a
// stale filter box would more often be a puzzle than a convenience,
// and every one of those is one keystroke away from being set up again
// anyway.
type TabState struct {
	Paths  []string
	Active int
}

// tabsFileHeader is written at the top of every saved tabs file. Purely
// for a human who stumbles across it — nothing parses it back (see
// LoadTabs, which skips comments like any other "#" line).
const tabsFileHeader = `# breakthrough saved tabs — rewritten automatically when breakthrough exits.
# Deleting this file simply starts the next run with a single tab.
`

// SaveTabs writes state to path atomically (temp file plus rename in the
// same directory, the same crash-safety config.SetKey already uses for
// the settings file), creating the parent directory if needed.
//
// The format is this project's own familiar "key = value" plaintext
// shape, with one deliberate difference from internal/config's: the
// "path" key repeats, once per tab, and its order carries meaning. That's
// why this has its own small parser here instead of reusing
// config.ParseFile, which returns a map — a map both collapses the
// repeats and loses the ordering, the two things this file is entirely
// made of. The repeated-key style itself is ordinary in this
// neighbourhood (sshd_config's own HostKey/ListenAddress do the same),
// so it stays readable to anyone who opens it.
//
// A path containing a newline can't be represented in a line-oriented
// format like this and is skipped rather than corrupting the file (see
// LoadTabs, which would otherwise read the remainder as a bogus entry).
// Such a filename is legal on POSIX but vanishingly rare, and losing one
// tab's restore is a far better outcome than an unparseable file.
func SaveTabs(path string, state TabState) error {
	if path == "" {
		return fmt.Errorf("no state directory available")
	}

	var b strings.Builder
	b.WriteString(tabsFileHeader)
	b.WriteString("active = " + strconv.Itoa(state.Active) + "\n")
	for _, p := range state.Paths {
		if strings.ContainsAny(p, "\r\n") {
			continue // see this func's own doc comment
		}
		b.WriteString("path = " + p + "\n")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".breakthrough-tabs-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(b.String()); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	// 0600, not the 0644 a config file gets: a tab layout is a list of
	// directories this user has been browsing, which is nobody else's
	// business on a shared machine.
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

// LoadTabs reads back what SaveTabs wrote. A missing file returns an
// empty TabState and no error — "nothing saved yet" is the normal state
// on a first run, not a problem.
//
// Malformed content is skipped rather than rejected wholesale, matching
// how config.Load treats a broken settings file: a garbled line costs
// that one tab, not the whole restore. Active is clamped into range
// against the paths actually read, so a truncated or hand-edited file
// can never hand the caller an out-of-range index.
func LoadTabs(path string) (TabState, error) {
	if path == "" {
		return TabState{}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return TabState{}, nil
		}
		return TabState{}, err
	}
	defer func() { _ = f.Close() }()

	var state TabState
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue // not an assignment at all — skip, don't fail
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "active":
			n, err := strconv.Atoi(value)
			if err != nil {
				continue // leaves Active at 0, clamped below anyway
			}
			state.Active = n
		case "path":
			if value != "" {
				state.Paths = append(state.Paths, value)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return TabState{}, err
	}

	// Clamp rather than reject: see this func's own doc comment.
	if state.Active < 0 || state.Active >= len(state.Paths) {
		state.Active = 0
	}
	return state, nil
}
