// Package filelabels tracks the path → color-label assignments behind
// the "zl" chord (see internal/ui's own "z" ("disp") chord family and
// Panel.rowLabelBackground) — MC/Finder-style tagging of individual
// files and directories with one of nine freely renamable color labels,
// independent of the existing, purely type-based directory coloring.
//
// This is state, not configuration (see internal/config): which paths
// currently carry which label is something a session accumulates by
// using the application, the same "worth restoring across a restart,
// not portable configuration" reasoning internal/notify's own persisted
// messages and internal/session's own saved tabs already follow — so it
// lives under session.StateDir(), as a flat JSON file, not under
// $XDG_CONFIG_HOME.
//
// Keyed by path, not inode: a hardlink's two names are meant to be able
// to carry independent labels. Every path is normalized with
// filepath.Clean before it's used as a key, so "/a//b" and "/a/b" never
// end up as two different entries for what both callers believe to be
// the same file.
//
// No tree structure, no index beyond a single map — a flat
// map[string]int (path → label id, 1-9; id 0 means "no label" and is
// never actually stored) is more than adequate for the sizes this is
// ever going to see, and keeps Rehome/Delete's own prefix scans (see
// their doc comments) a single pass over the whole map rather than a
// walk through any kind of hierarchy.
//
// Concurrency between two simultaneously running breakthrough instances
// (e.g. two tmux panes) is explicitly out of scope for this phase:
// atomic writes (temp file plus rename, the same crash-safety
// session.SaveTabs already uses) only prevent a corrupted file on disk,
// never a lost update when two processes both hold an older copy in
// memory. A deliberate non-goal, not a forgotten case.
package filelabels

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jagottsicher/breakthrough/internal/session"
)

// MaxLabelID is the highest real label id — nine, per the user's own
// explicit "9 freely namable labels" spec. 0 is "no label" and is valid
// everywhere an id is accepted (it means "clear"), but is never actually
// present as a value in Store's own map.
const MaxLabelID = 9

// persistedFileName is the on-disk filename under session.StateDir().
const persistedFileName = "labels.json"

// currentVersion is written into every save and checked (loosely — see
// load) on every load, so a future, incompatible on-disk shape has
// somewhere to branch from instead of silently misreading an old file.
const currentVersion = 1

// DefaultPath is where NewWithPersistence's store keeps its data —
// session.StateDir's own directory plus "labels.json". Returns "" when
// StateDir does (no $XDG_STATE_HOME and no resolvable home directory):
// callers treat that the same as "no persistence available" rather than
// an error, the same forgiving fallback notify.DefaultPath already uses.
func DefaultPath() string {
	dir := session.StateDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, persistedFileName)
}

// persistedFile is labels.json's own on-disk shape: a version tag (see
// currentVersion) alongside the flat path→id map itself, rather than a
// bare JSON object of paths — so a later, incompatible format change has
// a field to key off of instead of having to guess from shape alone.
type persistedFile struct {
	Version int            `json:"version"`
	Labels  map[string]int `json:"labels"`
}

// Store is the in-memory, persisted path→label-id map. The zero value is
// not useful on its own — use NewWithPersistence (path == "" is fine,
// and simply disables persistence: every method still works purely in
// memory, it just never survives a restart).
//
// Every method is nil-receiver-safe (see each one's own doc comment):
// internal/ui's Root/Panel hold a *Store that can itself be nil whenever
// session.StateDir() couldn't be resolved at all (see DefaultPath), and
// routing that through "is there a store at all" checks at every single
// call site, rather than once here, would be exactly the kind of
// repeated boilerplate this package exists to avoid.
type Store struct {
	mu     sync.RWMutex
	path   string
	labels map[string]int
}

// NewWithPersistence creates a Store backed by path (see DefaultPath),
// loading whatever is already there. path == "" disables persistence
// outright (see Store's own doc comment) rather than erroring — the
// same "no state directory available" tolerance session.SaveTabs itself
// already has to accept from its own caller.
func NewWithPersistence(path string) *Store {
	s := &Store{path: path, labels: map[string]int{}}
	s.load()
	return s
}

// load reads back whatever save last wrote. A missing file, or any
// error reading/parsing it, leaves s with its already-empty map rather
// than failing the whole application over a corrupt or absent state
// file — "nothing labeled yet" is the ordinary state on a first run,
// and a damaged file is no worse than that.
func (s *Store) load() {
	if s.path == "" {
		return
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var pf persistedFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return
	}
	labels := make(map[string]int, len(pf.Labels))
	for p, id := range pf.Labels {
		if id < 1 || id > MaxLabelID {
			continue // a hand-edited or future-version file's own out-of-range entry — skip it, don't fail the rest
		}
		labels[filepath.Clean(p)] = id
	}
	s.mu.Lock()
	s.labels = labels
	s.mu.Unlock()
}

// save atomically rewrites the whole file (temp file in the same
// directory, then rename — the same crash-safety pattern
// session.SaveTabs and config.SetKey already use), creating the parent
// directory if needed. A no-op, successfully, when persistence is
// disabled (s.path == "").
func (s *Store) save() error {
	if s.path == "" {
		return nil
	}
	s.mu.RLock()
	labels := make(map[string]int, len(s.labels))
	for p, id := range s.labels {
		labels[p] = id
	}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(persistedFile{Version: currentVersion, Labels: labels}, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".breakthrough-labels-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	// 0600: this is a list of this user's own file paths and the labels
	// they put on them — the same "nobody else's business on a shared
	// machine" reasoning session.SaveTabs' own 0600 already follows.
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

// Get returns the label id currently set on path (0, "no label", if
// none is, or if s is nil). path is cleaned the same way Set/SetMany
// store it, so a caller doesn't have to pre-clean every lookup itself.
func (s *Store) Get(path string) int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.labels[filepath.Clean(path)]
}

// Set assigns id (0 clears) to the single path. See SetMany for
// assigning the same id to several paths in one save.
func (s *Store) Set(path string, id int) error {
	return s.SetMany([]string{path}, id)
}

// SetMany assigns id (0 clears) to every one of paths, in a single
// save — the "zl" chord's own multi-select case calls this once rather
// than looping a single-path Set, so a selection of a hundred files
// doesn't also mean a hundred separate atomic rewrites of the same
// file. A no-op, successfully, on a nil Store (see Store's own doc
// comment on why every method tolerates that) or an empty paths slice.
func (s *Store) SetMany(paths []string, id int) error {
	if s == nil || len(paths) == 0 {
		return nil
	}
	if id < 0 || id > MaxLabelID {
		return fmt.Errorf("filelabels: invalid label id %d", id)
	}
	s.mu.Lock()
	for _, p := range paths {
		p = filepath.Clean(p)
		if id == 0 {
			delete(s.labels, p)
		} else {
			s.labels[p] = id
		}
	}
	s.mu.Unlock()
	return s.save()
}

// Rehome moves every label at oldPath itself, and every label nested
// under it (any key with oldPath plus a path separator as a literal
// prefix — not a bare strings.HasPrefix(key, oldPath), which would
// wrongly also match e.g. "/a/foo" against an oldPath of "/a/fo"), over
// to the same relative position under newPath.
//
// This is the one place a real filesystem move/rename and the trash
// lifecycle meet: internal/ui calls this with two ordinary, real
// filesystem paths for an actual move or rename, and with exactly the
// same two real paths again for Move to Trash (the trashed file's own
// real location under trashDir/files/) and Restore (that same trashed
// location back to TrashItem.OriginalPath) — there is no separate
// "trash id" namespace in this package at all: the file's own path
// inside the trash directory already is a perfectly good real path, so
// reusing this one function for all four cases (move, rename, trash,
// restore) needed nothing special-cased for trash specifically.
//
// A no-op, successfully, when nothing was actually found at or under
// oldPath (including on a nil Store), rather than treating "this path
// never had a label" as an error worth reporting — every one of this
// function's own callers runs it unconditionally after a successful
// move, without first checking whether the moved path happened to carry
// a label.
func (s *Store) Rehome(oldPath, newPath string) error {
	if s == nil {
		return nil
	}
	oldPath = filepath.Clean(oldPath)
	newPath = filepath.Clean(newPath)
	if oldPath == newPath {
		return nil
	}
	prefix := oldPath + string(filepath.Separator)

	s.mu.Lock()
	changed := false
	if id, ok := s.labels[oldPath]; ok {
		delete(s.labels, oldPath)
		s.labels[newPath] = id
		changed = true
	}
	for p, id := range s.labels {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		delete(s.labels, p)
		s.labels[newPath+string(filepath.Separator)+strings.TrimPrefix(p, prefix)] = id
		changed = true
	}
	s.mu.Unlock()

	if !changed {
		return nil
	}
	return s.save()
}

// Delete removes the label at path itself and every label nested under
// it (the same exact-plus-prefix match Rehome uses — see its own doc
// comment on why a bare HasPrefix isn't enough) — used when path is
// permanently removed rather than moved or trashed: Move to Trash goes
// through Rehome instead (see its own doc comment), since trashing
// isn't a deletion as far as a label is concerned.
//
// A no-op, successfully, when nothing was found (including on a nil
// Store), for the same "run unconditionally after the real operation
// succeeded" reason Rehome's own doc comment gives.
func (s *Store) Delete(path string) error {
	if s == nil {
		return nil
	}
	path = filepath.Clean(path)
	prefix := path + string(filepath.Separator)

	s.mu.Lock()
	changed := false
	if _, ok := s.labels[path]; ok {
		delete(s.labels, path)
		changed = true
	}
	for p := range s.labels {
		if strings.HasPrefix(p, prefix) {
			delete(s.labels, p)
			changed = true
		}
	}
	s.mu.Unlock()

	if !changed {
		return nil
	}
	return s.save()
}
