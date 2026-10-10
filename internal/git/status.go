package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Available reports whether git itself can be found on $PATH — the
// same check internal/gitstatus.Available already makes, duplicated
// here rather than imported since the two packages are deliberately
// independent (see doc.go).
func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// Root resolves dir's own repository root the same way a real `git`
// invocation would: walking up through dir's own parents looking for a
// working tree, exactly what the user asked Git breach ("jg") to do.
// Delegating this to git itself (`git rev-parse --show-toplevel`)
// rather than hand-rolling a parent-directory walk means this never
// drifts from git's own actual rules for what counts as a working
// tree (GIT_DIR, worktrees, bare repos accessed via --git-dir, ...).
//
// inRepo is false — not an error — for the common case of a directory
// with no working tree anywhere above it; err is reserved for git
// itself failing to run at all.
func Root(ctx context.Context, dir string) (root string, inRepo bool, err error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if runErr := cmd.Run(); runErr != nil {
		if ctx.Err() != nil {
			return "", false, ctx.Err()
		}
		if strings.Contains(stderr.String(), "not a git repository") {
			return "", false, nil
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", false, fmt.Errorf("git rev-parse: %s", msg)
		}
		return "", false, fmt.Errorf("git rev-parse: %w", runErr)
	}
	return strings.TrimSpace(stdout.String()), true, nil
}

// FileChange is one path's own entry in Status.Staged/Unstaged — Code
// is the single status letter git itself uses for it (M modified, A
// added, D deleted, R renamed, C copied, T type-changed, U updated-but-
// unmerged), OrigPath is only ever set for a rename/copy (Path is the
// new name).
type FileChange struct {
	Path     string
	OrigPath string
	Code     byte
}

// Status is one repository's own working-tree state, file paths and
// all — Status.Branch/Detached/Ahead/Behind mirror
// gitstatus.Status's own identically-named fields exactly (same
// underlying `git status --branch` data), kept as separate types
// rather than sharing one because this package's own Staged/Unstaged
// need real paths, not just counts.
//
// A path can legitimately appear in both Staged and Unstaged (git's
// own "MM": staged, then modified again) — both lists carry it rather
// than one picking a side, so the dashboard's own diff pane can show
// the right half (staged vs. worktree) regardless of which the user
// picked.
type Status struct {
	Branch   string
	Detached bool

	Ahead, Behind int

	Staged    []FileChange
	Unstaged  []FileChange
	Untracked []string
	Conflicts []string
}

// Fetch runs `git status` against root (expected to already be a
// resolved repository root — see Root) and parses its full per-file
// state. --ignore-submodules matches gitstatus.Fetch's own choice
// deliberately: a dirty submodule showing up here as a changed path in
// the containing repo would make this dashboard's own staged/unstaged
// counts disagree with the exact same repository's status-bar segment,
// which would read as a bug even though both would be "correct" by
// their own differing rules. -z switches git to NUL-separated,
// unquoted paths — without it, a path containing a space or a
// non-ASCII byte comes back shell-quoted (core.quotePath) and a
// rename's own old/new names are packed into one tab-separated field
// instead of two separate ones, either of which would corrupt the
// parse below.
func Fetch(ctx context.Context, root string) (Status, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain=v2", "--branch", "--ignore-submodules", "-z")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return Status{}, ctx.Err()
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return Status{}, fmt.Errorf("git status: %s", msg)
		}
		return Status{}, fmt.Errorf("git status: %w", err)
	}
	return parsePorcelainV2Z(stdout.String()), nil
}

// parsePorcelainV2Z reads `git status --porcelain=v2 --branch -z`'s own
// NUL-separated records — see git-status(1)'s "Porcelain Format
// Version 2" section for the exact grammar. The only wrinkle -z adds
// over the newline-separated form: a rename/copy entry's own original
// path isn't packed into the same record (there's no per-record
// separator left to pack it behind once records are NUL-separated) —
// it comes back as the *next* record instead, with no "2 ..." prefix
// of its own, so parsing one has to consciously consume an extra
// record immediately after it.
func parsePorcelainV2Z(output string) Status {
	var st Status
	var oid, head string

	records := strings.Split(output, "\x00")
	for i := 0; i < len(records); i++ {
		rec := records[i]
		switch {
		case rec == "":
			// -z's own trailing separator leaves one empty record at the
			// end; a renamed entry's own consumed orig-path record (see
			// below) could in principle be empty too for a repo root
			// itself being renamed, which can't happen — directories
			// aren't tracked, only files.
		case strings.HasPrefix(rec, "# branch.oid "):
			oid = strings.TrimPrefix(rec, "# branch.oid ")
		case strings.HasPrefix(rec, "# branch.head "):
			head = strings.TrimPrefix(rec, "# branch.head ")
		case strings.HasPrefix(rec, "# branch.ab "):
			for _, field := range strings.Fields(strings.TrimPrefix(rec, "# branch.ab ")) {
				n, err := strconv.Atoi(strings.TrimLeft(field, "+-"))
				if err != nil {
					continue
				}
				if strings.HasPrefix(field, "+") {
					st.Ahead = n
				} else {
					st.Behind = n
				}
			}
		case strings.HasPrefix(rec, "1 "):
			// "1 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <path>"
			fields := strings.SplitN(rec, " ", 9)
			if len(fields) < 9 || len(fields[1]) != 2 {
				continue
			}
			addOrdinary(&st, fields[1], fields[8])
		case strings.HasPrefix(rec, "2 "):
			// "2 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <X><score> <path>",
			// immediately followed by one more NUL-separated record
			// holding the origin path (see this function's own doc
			// comment) — consumed here, not re-visited by the loop.
			fields := strings.SplitN(rec, " ", 10)
			if len(fields) < 10 || len(fields[1]) != 2 {
				continue
			}
			var orig string
			if i+1 < len(records) {
				i++
				orig = records[i]
			}
			addRename(&st, fields[1], fields[9], orig)
		case strings.HasPrefix(rec, "u "):
			// "u <XY> <sub> <m1> <m2> <m3> <mW> <h1> <h2> <h3> <path>"
			fields := strings.SplitN(rec, " ", 11)
			if len(fields) < 11 {
				continue
			}
			st.Conflicts = append(st.Conflicts, fields[10])
		case strings.HasPrefix(rec, "? "):
			st.Untracked = append(st.Untracked, strings.TrimPrefix(rec, "? "))
		}
	}

	switch {
	case head == "(detached)":
		st.Detached = true
		st.Branch = shortOID(oid)
	case head != "":
		st.Branch = head
	}
	return st
}

// addOrdinary applies one "1 ..." (plain changed, not renamed/copied)
// record's own XY code to st — X is the staged/index half, Y the
// unstaged/worktree half, either or both '.' meaning "unchanged there"
// (see git-status(1)); a path with both set (git's own "MM") lands in
// both Staged and Unstaged, by design (see Status's own doc comment).
func addOrdinary(st *Status, xy, path string) {
	if xy[0] != '.' {
		st.Staged = append(st.Staged, FileChange{Path: path, Code: xy[0]})
	}
	if xy[1] != '.' {
		st.Unstaged = append(st.Unstaged, FileChange{Path: path, Code: xy[1]})
	}
}

// addRename is addOrdinary's own rename/copy sibling — same XY
// splitting, but every resulting FileChange also carries orig (the
// pre-rename path), which addOrdinary's entries never have.
func addRename(st *Status, xy, path, orig string) {
	if xy[0] != '.' {
		st.Staged = append(st.Staged, FileChange{Path: path, OrigPath: orig, Code: xy[0]})
	}
	if xy[1] != '.' {
		st.Unstaged = append(st.Unstaged, FileChange{Path: path, OrigPath: orig, Code: xy[1]})
	}
}

// shortOID mirrors gitstatus's own identically-named helper — `git log
// --oneline`/`git rev-parse --short`'s default length, long enough to
// be unambiguous, short enough to read as a branch name would.
func shortOID(oid string) string {
	const shortLen = 7
	if len(oid) <= shortLen {
		return oid
	}
	return oid[:shortLen]
}
