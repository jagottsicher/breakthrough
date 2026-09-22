package compare

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Verdict is what Walk decided about one Entry.
type Verdict int

const (
	// Identical means the two sides agree under whatever Mode was
	// used — a heuristic match (ModeQuick) or a real byte-exact match
	// (ModeHash, via a hash comparison).
	Identical Verdict = iota
	// Differs means the two sides are definitely not the same:
	// different size (either mode), different hash (ModeHash), or one
	// side is a file where the other is a directory.
	Differs
	// Uncertain is ModeQuick's own honest middle ground: the same
	// size, but a different modification time. Could be the same
	// content re-saved, or could be genuinely different — ModeQuick
	// has no way to tell without reading the content, which is exactly
	// what ModeHash is for. Never produced by ModeHash.
	Uncertain
	// OnlyInA/OnlyInB mark a path that exists on just one side — see
	// Walk's own doc comment for why its subtree, if it has one, is
	// never descended into: this one Entry stands for everything
	// underneath it too.
	OnlyInA
	OnlyInB
	// Errored means this one path — or a directory read on the way to
	// it — failed: permission denied, a dangling reference, and so on.
	// Err holds the reason.
	Errored
)

// Mode picks how two files that exist on both sides, and already agree
// in size, are told apart.
type Mode int

const (
	// ModeQuick trusts size+modification time alone — the same "quick
	// check" heuristic rsync's own default sync mode uses, essentially
	// free even on a very large tree.
	ModeQuick Mode = iota
	// ModeHash reads and hashes both files instead (see HashFunc) —
	// slower, but a definitive answer with no Uncertain verdicts; only
	// even attempted once sizes already match, since a size mismatch
	// already settles it.
	ModeHash
)

// Entry is one row Walk reports: a path relative to both roots, what
// Walk found there, and the metadata behind that verdict. IsDir is
// only ever meaningful for OnlyInA/OnlyInB (a matched directory is
// never reported as an Entry of its own — only its differing
// descendants are, see Walk).
type Entry struct {
	RelPath  string
	IsDir    bool
	Verdict  Verdict
	SizeA    int64
	SizeB    int64
	ModTimeA time.Time
	ModTimeB time.Time
	// Note is a short human-readable elaboration for a case the
	// Verdict alone doesn't fully explain (currently just the file-vs-
	// directory type mismatch) — most entries leave it empty.
	Note string
	// Err is set alongside Verdict Errored.
	Err error
}

// Stats is Walk's own running/final tally, kept alongside Entries
// rather than derived by a caller re-scanning them on every render.
type Stats struct {
	Identical, Differs, Uncertain, OnlyInA, OnlyInB, Errored int
}

// HashFunc is how Walk gets a comparable digest of one file under
// ModeHash — the real caller (internal/ui's Compare screen) always
// passes a thin wrapper over fsops.Hash reading off its SHA256 field;
// this package has no fsops dependency of its own, and tests substitute
// a fast fake instead of hashing real files. Ignored entirely under
// ModeQuick, where it may be nil.
type HashFunc func(ctx context.Context, path string) (string, error)

// Walk descends dirA and dirB in lock step, directory by directory,
// producing one Entry per path compared — every plain file that exists
// on both sides (Identical entries included: under a heuristic mode,
// "the two agree" is exactly as worth showing as "they don't", not a
// fact to take on faith and hide — a caller that only wants the
// interesting rows filters Identical out itself, the same way
// internal/ui's own Batch Rename preview dims, but still shows,
// unchanged rows) — plus one Entry for every path that exists on only
// one side.
//
// Never descends into a directory that only exists on one side: that
// whole subtree is exactly as one-sided as the directory itself, and
// walking it too would repeat the same answer once per descendant, for
// a possibly enormous tree — the classic "old, untouched backup
// folder" case this feature exists for in the first place.
//
// onProgress, if non-nil, is called after every path visited (whatever
// the outcome) with a running count. There's no meaningful "total" to
// report until both trees have been fully walked, so this is a live
// counter, not a percentage — the same tradeoff fsops.Hash's own
// progress callback makes for a single file's byte count against an
// a-priori total.
//
// ctx is checked before descending into each directory and before each
// file comparison (which, under ModeHash, is itself cancellable
// mid-read — see HashFunc); once cancelled, Walk returns everything
// already discovered (still correct, just incomplete) plus ctx.Err().
func Walk(ctx context.Context, dirA, dirB string, mode Mode, hash HashFunc, onProgress func(count int)) ([]Entry, Stats, error) {
	if _, err := os.Stat(dirA); err != nil {
		return nil, Stats{}, fmt.Errorf("%s: %w", dirA, err)
	}
	if _, err := os.Stat(dirB); err != nil {
		return nil, Stats{}, fmt.Errorf("%s: %w", dirB, err)
	}

	var (
		entries []Entry
		stats   Stats
		count   int
	)
	report := func(e Entry) {
		entries = append(entries, e)
		tally(&stats, e.Verdict)
		count++
		if onProgress != nil {
			onProgress(count)
		}
	}

	var walkDir func(rel, a, b string) error
	walkDir = func(rel, a, b string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		namesA, errA := readDirNames(a)
		namesB, errB := readDirNames(b)
		if errA != nil || errB != nil {
			err := errA
			if err == nil {
				err = errB
			}
			report(Entry{RelPath: rel, IsDir: true, Verdict: Errored, Err: err})
			return nil
		}

		for _, name := range unionSorted(namesA, namesB) {
			if err := ctx.Err(); err != nil {
				return err
			}
			relPath := name
			if rel != "" {
				relPath = rel + "/" + name
			}
			pathA, pathB := filepath.Join(a, name), filepath.Join(b, name)
			infoA, inA := statIfExists(pathA)
			infoB, inB := statIfExists(pathB)

			switch {
			case inA && !inB:
				report(oneSided(relPath, OnlyInA, infoA))
			case !inA && inB:
				report(oneSided(relPath, OnlyInB, infoB))
			case infoA.IsDir() != infoB.IsDir():
				report(Entry{
					RelPath: relPath, Verdict: Differs, Note: "file vs. directory",
					SizeA: infoA.Size(), SizeB: infoB.Size(),
					ModTimeA: infoA.ModTime(), ModTimeB: infoB.ModTime(),
				})
			case infoA.IsDir():
				if err := walkDir(relPath, pathA, pathB); err != nil {
					return err
				}
			default:
				report(compareFileEntry(ctx, relPath, pathA, pathB, infoA, infoB, mode, hash))
			}
		}
		return nil
	}

	err := walkDir("", dirA, dirB)
	return entries, stats, err
}

// compareFileEntry settles one plain-file Entry that exists on both
// sides — the only place Verdict Uncertain/the ModeHash path can come
// from.
func compareFileEntry(ctx context.Context, relPath, pathA, pathB string, infoA, infoB os.FileInfo, mode Mode, hash HashFunc) Entry {
	e := Entry{
		RelPath:  relPath,
		SizeA:    infoA.Size(),
		SizeB:    infoB.Size(),
		ModTimeA: infoA.ModTime(),
		ModTimeB: infoB.ModTime(),
	}
	if e.SizeA != e.SizeB {
		e.Verdict = Differs
		return e
	}
	if mode == ModeQuick {
		if e.ModTimeA.Equal(e.ModTimeB) {
			e.Verdict = Identical
		} else {
			e.Verdict = Uncertain
		}
		return e
	}

	hashA, err := hash(ctx, pathA)
	if err != nil {
		e.Verdict, e.Err = Errored, err
		return e
	}
	hashB, err := hash(ctx, pathB)
	if err != nil {
		e.Verdict, e.Err = Errored, err
		return e
	}
	if hashA == hashB {
		e.Verdict = Identical
	} else {
		e.Verdict = Differs
	}
	return e
}

func tally(s *Stats, v Verdict) {
	switch v {
	case Identical:
		s.Identical++
	case Differs:
		s.Differs++
	case Uncertain:
		s.Uncertain++
	case OnlyInA:
		s.OnlyInA++
	case OnlyInB:
		s.OnlyInB++
	case Errored:
		s.Errored++
	}
}

func oneSided(relPath string, v Verdict, info os.FileInfo) Entry {
	e := Entry{RelPath: relPath, IsDir: info.IsDir(), Verdict: v}
	if v == OnlyInA {
		e.SizeA, e.ModTimeA = info.Size(), info.ModTime()
	} else {
		e.SizeB, e.ModTimeB = info.Size(), info.ModTime()
	}
	return e
}

func readDirNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names, nil
}

// statIfExists Lstat's path — not Stat: a symlink is compared as
// itself, not as whatever it points to (see CompareFiles' own doc
// comment for the same choice and why).
func statIfExists(path string) (os.FileInfo, bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, false
	}
	return info, true
}

// unionSorted is every name in a or b, deduplicated, sorted — Walk's
// own iteration order (and so the order Entries come back in).
func unionSorted(a, b []string) []string {
	set := make(map[string]bool, len(a)+len(b))
	for _, n := range a {
		set[n] = true
	}
	for _, n := range b {
		set[n] = true
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
