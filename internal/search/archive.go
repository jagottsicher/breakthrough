package search

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

// archiveKind is which listing tool an archive candidate needs — zip's
// own -Z1/zipinfo-style listing, or tar's -tf (which auto-detects
// gzip/bzip2/xz compression from the file's own magic bytes on both
// GNU and BSD/macOS tar, verified against both man pages — no separate
// flag per compression needed here, unlike archiveExtensions below,
// which still needs one entry per compression to find the files in the
// first place).
type archiveKind int

const (
	archiveZip archiveKind = iota
	archiveTar
)

// archiveExtensions is the "Stufe A" set of archive suffixes Include
// Archives recognizes — zip, plus the tar family (plain .tar and its
// three most common compressions) — per the user's own explicit scope
// decision, deliberately excluding 7z/rar (extra, not-always-installed
// dependencies) and a lone .gz/.bz2 (no real member listing of their
// own to search — just one implied name, already covered by a plain
// filename search matching the archive's own name). .tgz/.tbz/.txz's
// shorter aliases aren't included either, same reasoning — can be
// added later without touching anything else here.
var archiveExtensions = []struct {
	suffix string
	kind   archiveKind
}{
	{".zip", archiveZip},
	{".tar", archiveTar},
	{".tar.gz", archiveTar},
	{".tar.bz2", archiveTar},
	{".tar.xz", archiveTar},
}

// classifyArchive reports which archiveKind p's own extension matches
// (case-insensitively, the same as every other extension check in this
// package — see listThenGrep's own nameCaseSensitive doc comment), or
// ok=false if it matches none of archiveExtensions at all.
func classifyArchive(p string) (kind archiveKind, ok bool) {
	lower := strings.ToLower(p)
	for _, e := range archiveExtensions {
		if strings.HasSuffix(lower, e.suffix) {
			return e.kind, true
		}
	}
	return 0, false
}

// archiveListConcurrency bounds how many archive-listing subprocesses
// (unzip -Z1/tar -tf) — and, for EngineLocate, candidate-finding
// locate calls (see locateArchiveCandidates) — run at once. Per the
// user's own explicit request: tar has no central index the way zip
// does, so tar -tf must decompress and read an entire archive just to
// list it, and a single large tar.gz running alone would otherwise
// stall every archive queued behind it. A small, fixed pool rather
// than one goroutine per candidate outright: a directory tree with
// hundreds of matching archives shouldn't spawn hundreds of processes
// at once either.
const archiveListConcurrency = 4

// searchArchives finds every archive under req.Scope matching
// archiveExtensions (via req.Engine — the same find/locate choice a
// plain filename search already makes, see archiveCandidates), lists
// each one's own members one level deep (an archive containing another
// archive isn't itself opened — a deliberate "Stufe A" scope decision,
// not a limitation of the approach itself), and sends a Result with
// ArchiveMember set for every member whose own name matches
// req.Pattern/req.Mode the same way a real file name would (see
// archiveMemberMatches). Meant to run concurrently with whatever the
// caller's own primary filename-search stream is already doing (see
// runFilenameSearch/startArchiveSearch) — the two share one results
// channel via sendResult, already safe for concurrent use from more
// than one goroutine (an ordinary Go channel).
//
// A failed or empty candidate search isn't reported as an error here —
// Include Archives is always additive to a plain filename search, never
// something that should make an otherwise-fine search fail outright
// (the exact same reasoning listThenGrep's own per-file skip already
// follows for a single corrupt/unreadable archive).
func searchArchives(ctx context.Context, req Request, results chan<- Result) {
	candidates, _ := archiveCandidates(ctx, req)
	if len(candidates) == 0 {
		return
	}

	sem := make(chan struct{}, archiveListConcurrency)
	var wg sync.WaitGroup
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			break
		}
		candidate := candidate
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			listArchiveAndSend(ctx, req, candidate, results)
		}()
	}
	wg.Wait()
}

// listArchiveAndSend lists candidatePath's own members (see
// listArchiveMembers) and sends a Result for each one matching
// req.Pattern. A candidate that fails to list at all (a corrupt
// archive, unzip/tar missing) is silently skipped — see searchArchives'
// own doc comment.
func listArchiveAndSend(ctx context.Context, req Request, candidatePath string, results chan<- Result) {
	kind, ok := classifyArchive(candidatePath)
	if !ok {
		return
	}
	members, err := listArchiveMembers(ctx, kind, candidatePath)
	if err != nil {
		return
	}
	for _, member := range members {
		if ctx.Err() != nil {
			return
		}
		if !archiveMemberMatches(member, req.Pattern, req.Mode, req.CaseSensitive) {
			continue
		}
		if !sendResult(ctx, results, Result{Path: candidatePath, ArchiveMember: member}) {
			return
		}
	}
}

// listArchiveMembers runs unzip -Z1 (zip's own zipinfo-style,
// filenames-only listing — no header/footer lines to skip, unlike
// unzip -l's own columnar table) or tar -tf (see archiveKind's own doc
// comment on why one flag covers every compression), returning one
// member path per line exactly as the tool reports it — including a
// trailing "/" for a directory member, left as-is here rather than
// stripped: archiveMemberMatches strips it only for matching, but the
// real, unmodified name is still what internal/ui shows (see
// Result.ArchiveMember's own doc comment).
func listArchiveMembers(ctx context.Context, kind archiveKind, archivePath string) ([]string, error) {
	var cmd *exec.Cmd
	switch kind {
	case archiveZip:
		cmd = exec.CommandContext(ctx, "unzip", "-Z1", archivePath)
	case archiveTar:
		cmd = exec.CommandContext(ctx, "tar", "-tf", archivePath)
	default:
		return nil, fmt.Errorf("search: unknown archive kind %d", kind)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var members []string
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // a long member path shouldn't truncate the scan — same buffer size streamGrepLines already uses for the same reason
	for scanner.Scan() {
		if line := scanner.Text(); line != "" {
			members = append(members, line)
		}
	}
	_ = cmd.Wait() // a non-zero exit (e.g. a partially corrupt archive tar/unzip still partially listed) doesn't discard whatever it did manage to report — same "don't over-report a failure" philosophy as Run's own doc comment
	return members, nil
}

// archiveMemberMatches reports whether member (a raw archive-listing
// line — possibly with a trailing "/" for a directory, see
// listArchiveMembers) matches pattern/mode/caseSensitive the same way
// FindArgs' own -iname/-iregex distinction already works for a real
// file: Glob/Keyword match just member's own base name (the trailing
// "/" stripped first, so a directory member's own name isn't defeated
// by it), Regex matches the member's whole internal path instead —
// mirroring find's own -iname-vs-iregex scope split exactly (see
// FindArgs' own doc comment).
func archiveMemberMatches(member, pattern string, mode Mode, caseSensitive bool) bool {
	trimmed := strings.TrimSuffix(member, "/")

	if mode == ModeRegex {
		expr := pattern
		if !caseSensitive {
			expr = "(?i)" + expr
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return false
		}
		return re.MatchString(trimmed)
	}

	glob := pattern
	if mode == ModeKeyword {
		glob = "*" + pattern + "*" // the same find/locate ModeKeyword convention — see FindArgs/LocateArgs
	}
	name := path.Base(trimmed)
	if !caseSensitive {
		name = strings.ToLower(name)
		glob = strings.ToLower(glob)
	}
	ok, err := path.Match(glob, name)
	return err == nil && ok
}

// archiveCandidates lists every real file under req.Scope whose name
// matches one of archiveExtensions — the "which files are archives at
// all" step, entirely independent of req.Pattern (that's matched
// against each archive's own *members* afterward, in
// listArchiveAndSend) — via req.Engine, the same find/locate choice a
// plain filename search already makes.
func archiveCandidates(ctx context.Context, req Request) ([]string, error) {
	if req.Engine == EngineLocate {
		return locateArchiveCandidates(ctx, req)
	}
	return findArchiveCandidates(ctx, req)
}

// findArchiveCandidates runs one grouped find call ("-iname *.zip -o
// -iname *.tar -o ...", the same OR-clause idiom FindArgs already uses
// for its own IgnoreDirs prune group — see findArchiveArgs) — a single
// tree walk covering every extension at once, since find's own walk is
// the expensive part here, not the number of name tests within it.
func findArchiveCandidates(ctx context.Context, req Request) ([]string, error) {
	cmd := exec.CommandContext(ctx, "find", findArchiveArgs(req.Scope, req.IgnoreDirs, req.NonRecursive, req.FollowSymlinks)...)
	var candidates []string
	err := streamNullSeparated(cmd, func(p string) bool {
		candidates = append(candidates, p)
		return true
	})
	return candidates, err
}

// findArchiveArgs mirrors FindArgs' own structure (see its doc
// comment) but with a fixed OR-group of archiveExtensions' own globs as
// the real match test, instead of one caller-supplied pattern —
// FindArgs' own single-pattern signature can't express "any of these
// five", so this is a deliberate, small duplication rather than trying
// to force it through that one shared function.
func findArchiveArgs(scope string, ignoreDirs []string, nonRecursive, followSymlinks bool) []string {
	var args []string
	if followSymlinks {
		args = append(args, "-L")
	}
	args = append(args, scope)
	if nonRecursive {
		args = append(args, "-maxdepth", "1")
	}

	if len(ignoreDirs) > 0 {
		args = append(args, "(")
		for i, name := range ignoreDirs {
			if i > 0 {
				args = append(args, "-o")
			}
			args = append(args, "-name", name)
		}
		args = append(args, ")", "-prune", "-o")
	}

	args = append(args, "(")
	for i, e := range archiveExtensions {
		if i > 0 {
			args = append(args, "-o")
		}
		args = append(args, "-iname", "*"+e.suffix)
	}
	return append(args, ")", "-print0")
}

// locateArchiveCandidates runs one locate call *per* extension in
// archiveExtensions instead of trying to express their union as a
// single glob — locate's own pattern matching has no brace-alternation
// syntax to ask for that (verified against the mlocate/plocate glob(7)
// contract, not guessed: that's a shell feature, not part of fnmatch).
// Acceptable here specifically because each individual locate call
// answers from its own prebuilt index near-instantly, unlike find's
// live walk — five small calls cost nothing close to five full tree
// walks — so they're run through the same bounded worker pool
// (archiveListConcurrency) as the member-listing step, not as five more
// unbounded goroutines.
//
// req.Scope, otherwise ignored outright for EngineLocate (see
// Request.Scope's own doc comment on why — a real, previously-shipped
// bug), is re-applied here as a client-side filter (see underScope) —
// but *only* for this archive-candidate step, never for locate's own
// plain filename results a few lines up in runFilenameSearch: opening
// and listing every archive anywhere on the whole system just because
// Include Archives is on would be needlessly expensive and almost never
// what "search under /home/jens" is meant to include, once there's a
// real, additional per-archive cost involved rather than just filtering
// an already-cheap listing.
func locateArchiveCandidates(ctx context.Context, req Request) ([]string, error) {
	sem := make(chan struct{}, archiveListConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var candidates []string
	var firstErr error

	for _, e := range archiveExtensions {
		e := e
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			// false: always case-insensitive here, the same as every other
			// archive-glob match in this package (see listThenGrep's own
			// nameCaseSensitive doc comment) — this is about finding files
			// named e.g. ".ZIP" just as readily as ".zip", unrelated to
			// req.CaseSensitive, which only ever applies to matching a
			// member's own name against req.Pattern (see
			// archiveMemberMatches).
			args, ok := LocateArgs(runtime.GOOS, "*"+e.suffix, ModeGlob, false)
			if !ok {
				return
			}
			var found []string
			err := streamNullSeparated(exec.CommandContext(ctx, "locate", args...), func(p string) bool {
				if underIgnoredDir(p, req.IgnoreDirs) {
					return true // keep going, just filtered out — see underIgnoredDir's own doc comment
				}
				if !underScope(p, req.Scope) {
					return true // see this func's own doc comment on why Scope applies here specifically
				}
				found = append(found, p)
				return true
			})

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			candidates = append(candidates, found...)
		}()
	}
	wg.Wait()
	return candidates, firstErr
}

// underScope reports whether p lies at or under scope — the archive-
// candidate step's own, deliberately isolated exception to Scope being
// otherwise ignored for EngineLocate (see locateArchiveCandidates' own
// doc comment on why). filepath.Rel, not strings.HasPrefix: a scope
// like "/home/jens" shouldn't also match an unrelated sibling such as
// "/home/jens2" — a real class of bug plain prefix-matching a path
// would otherwise have.
func underScope(p, scope string) bool {
	rel, err := filepath.Rel(scope, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
