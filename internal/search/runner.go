package search

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Result is one match: a plain filename hit (Line == 0), or a content
// hit — a specific line within Path, Text its own matching content.
type Result struct {
	Path string
	Line int
	Text string
}

// Run starts req in a background goroutine and streams each match it
// finds on the returned channel, closed once the search finishes or
// ctx is cancelled — internal/ui's search dialog cancels it when the
// dialog closes or a new search starts, so a slow "find /" left
// running never keeps working after the user has moved on. The second
// returned channel carries at most one error (buffered, so the send
// never blocks) — only ever a command that failed to even start (the
// binary missing, or similar). A non-zero exit after a successful
// start is never treated as an error: find/locate/grep all exit
// non-zero for reasons that don't mean the search itself failed (a
// permission-denied subdirectory find simply skips over; grep's own
// "no matches found" is its documented exit status 1) — surfacing
// every one of those as an error would misreport an ordinary empty or
// partial result as a failure.
//
// Both channels are closed once the search is done — errs strictly
// before results (see the defer order below), so a caller that first
// drains results with a plain "for range" and only then checks errs
// (rather than a select watching both concurrently) can read it
// straight after, with no risk of blocking forever on a "no error"
// outcome: a receive on an already-closed, never-sent-to channel
// returns its zero value immediately rather than blocking.
func Run(ctx context.Context, req Request) (<-chan Result, <-chan error) {
	results := make(chan Result)
	errs := make(chan error, 1)

	go func() {
		defer close(results)
		defer close(errs)
		var err error
		if req.Content == ContentNone {
			err = runFilenameSearch(ctx, req, results)
		} else {
			err = runContentSearch(ctx, req, results)
		}
		// A cancelled ctx can surface as ctx.Err() from cmd.Start() itself
		// (e.g. cancel() racing the very start of the search) — that's a
		// cancellation, not a failure, the same reasoning as this func's
		// own doc comment on non-zero exits; nothing worth reporting.
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			errs <- err
		}
	}()

	return results, errs
}

func runFilenameSearch(ctx context.Context, req Request, results chan<- Result) error {
	// A recursive EngineFind search reports files shallow-first — see
	// runFilenameSearchShallowFirst's own doc comment — rather than
	// whatever order find's own directory traversal happens to produce.
	// Meaningless for EngineLocate (Scope isn't used at all — see
	// Request.Scope's own doc comment) or an already-NonRecursive
	// search (already shallow-only, nothing to reorder).
	if req.Engine == EngineFind && !req.NonRecursive {
		return runFilenameSearchShallowFirst(ctx, req, results)
	}

	name, args, ok := filenameCommand(req)
	if !ok {
		return fmt.Errorf("locate: regex search isn't available on %s", runtime.GOOS)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	return streamNullSeparated(cmd, func(path string) bool {
		// See Request.Scope's own doc comment on why Scope no longer
		// filters EngineLocate's own results at all (it used to): a
		// user picks locate specifically *for* its whole-system index,
		// and Scope defaults to wherever the panel happens to be —
		// silently discarding every result outside that one directory
		// made locate come back empty for almost any real search, a
		// real user report ("locate findet wieder nichts"). IgnoreDirs
		// stays applied even for locate: unlike Scope, it's never
		// silently defaulted — a name only ends up there because the
		// user typed it in themselves.
		if req.Engine == EngineLocate && underIgnoredDir(path, req.IgnoreDirs) {
			return true // keep going, just filtered out
		}
		return sendResult(ctx, results, Result{Path: path})
	})
}

// runFilenameSearchShallowFirst runs a recursive EngineFind filename
// search in two passes — files directly in Scope first (via a
// -maxdepth 1 pass, the same one NonRecursive already produces), then
// everything deeper (a second, fully recursive pass, skipping whatever
// the first pass already reported) — rather than in whatever order
// find's own directory traversal happens to produce, which frequently
// dives straight into the first subdirectory it finds before ever
// reporting a single file from Scope's own top level. A real user
// report: searching didn't feel like it "stayed" in the chosen start
// directory at all.
//
// The second pass filters by comparing each result's own parent
// directory against Scope, rather than asking find itself for
// -mindepth 2 (a second depth flag this app would otherwise have to
// thread through everywhere -maxdepth already is, for a purely
// client-side, one-off ordering nicety) — a filepath.Dir and a string
// compare per result is cheap enough that it isn't worth that
// complexity.
func runFilenameSearchShallowFirst(ctx context.Context, req Request, results chan<- Result) error {
	shallow := req
	shallow.NonRecursive = true
	name, args, ok := filenameCommand(shallow)
	if !ok {
		return fmt.Errorf("locate: regex search isn't available on %s", runtime.GOOS)
	}
	if err := streamNullSeparated(exec.CommandContext(ctx, name, args...), func(path string) bool {
		return sendResult(ctx, results, Result{Path: path})
	}); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return nil // the shallow pass was itself cancelled — nothing left to do
	}

	scope := filepath.Clean(req.Scope)
	name, args, ok = filenameCommand(req) // the real request, still fully recursive
	if !ok {
		return fmt.Errorf("locate: regex search isn't available on %s", runtime.GOOS)
	}
	return streamNullSeparated(exec.CommandContext(ctx, name, args...), func(path string) bool {
		if filepath.Dir(path) == scope {
			return true // already reported by the shallow pass above
		}
		return sendResult(ctx, results, Result{Path: path})
	})
}

func filenameCommand(req Request) (name string, args []string, ok bool) {
	if req.Engine == EngineLocate {
		a, ok := LocateArgs(runtime.GOOS, req.Pattern, req.Mode, req.CaseSensitive)
		return "locate", a, ok
	}
	return "find", FindArgs(runtime.GOOS, req.Scope, req.Pattern, req.Mode, req.IgnoreDirs, req.CaseSensitive, req.NonRecursive, req.FollowSymlinks), true
}

// underIgnoredDir reports whether path has any of ignoreDirs matching
// one of its own path components (via filepath.Match — see
// Request.IgnoreDirs' own doc comment on why a glob, not just an exact
// name) — locate's own client-side equivalent of FindArgs' -prune (its
// results come from a prebuilt index, not a live walk, so there's no
// traversal to prune instead). A malformed pattern (filepath.Match's
// own ErrBadPattern) is treated as simply not matching rather than
// propagated — Request.IgnoreDirs already only ever gets there via
// typed-in names or this package's own "Skip hidden" ".*", neither of
// which can produce one.
func underIgnoredDir(path string, ignoreDirs []string) bool {
	if len(ignoreDirs) == 0 {
		return false
	}
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		for _, ignored := range ignoreDirs {
			if ok, _ := filepath.Match(ignored, part); ok {
				return true
			}
		}
	}
	return false
}

func runContentSearch(ctx context.Context, req Request, results chan<- Result) error {
	// NamePattern narrows a grep-backed content search to files matching
	// it first (see Request's own doc comment on NamePattern) — checked
	// ahead of the Content switch below since it changes ContentGrep's
	// own strategy entirely (findThenGrep instead of one recursive grep
	// call); ContentGzip/ContentZip already always go through
	// findThenGrep for their own, unrelated reason (zgrep/zipgrep can't
	// recurse at all — see findThenGrep's own doc comment), so
	// NamePattern doesn't change anything for those two.
	if req.NamePattern != "" && req.Content == ContentGrep {
		return findThenGrep(ctx, req, req.NamePattern, req.NameMode, req.CaseSensitive, "grep", func(f string) []string {
			return GrepArgs(req.Pattern, f, req.Mode, req.CaseSensitive, req.WholeWords, req.FirstHit)
		}, results)
	}
	switch req.Content {
	case ContentGrep:
		cmd := exec.CommandContext(ctx, "grep", GrepArgs(req.Pattern, req.Scope, req.Mode, req.CaseSensitive, req.WholeWords, req.FirstHit)...)
		return streamGrepLines(ctx, cmd, results)
	case ContentGzip:
		return findThenGrep(ctx, req, "*.gz", ModeGlob, false, "zgrep", func(f string) []string {
			return ZgrepArgs(req.Pattern, req.Mode, f, req.CaseSensitive, req.WholeWords, req.FirstHit)
		}, results)
	case ContentZip:
		return findThenGrep(ctx, req, "*.zip", ModeGlob, false, "zipgrep", func(f string) []string {
			return ZipgrepArgs(req.Pattern, req.Mode, f, req.CaseSensitive, req.WholeWords, req.FirstHit)
		}, results)
	}
	return nil
}

// findThenGrep finds every file under req.Scope matching namePattern
// (an archive extension glob for ContentGzip/ContentZip, or the user's
// own NamePattern — see runContentSearch), then runs one tool
// invocation per file found, streaming each one's matches as they're
// found. Originally just for zgrep/zipgrep, neither of which supports
// recursing a directory tree itself (see their own doc comments in
// grep.go) — reused for a NamePattern-narrowed plain grep too, since
// it's the exact same shape: find the candidates first, run the
// content tool once per candidate rather than once across the whole
// tree. A single file that fails to start (a corrupt archive, an odd
// permission) is skipped rather than aborting the rest of the search:
// unlike the top-level find/locate/grep call, this isn't the one
// command the whole search depends on.
//
// nameCaseSensitive is separate from req.CaseSensitive (used for the
// content pattern itself, passed to buildArgs): for the archive-glob
// callers it's always false regardless — a user almost certainly wants
// both cases of ".gz"/".zip" either way — but for a real user-typed
// NamePattern it should follow the same toggle the user actually
// checked, so callers pass it explicitly rather than this func
// assuming either default.
func findThenGrep(ctx context.Context, req Request, namePattern string, nameMode Mode, nameCaseSensitive bool, tool string, buildArgs func(file string) []string, results chan<- Result) error {
	findCmd := exec.CommandContext(ctx, "find", FindArgs(runtime.GOOS, req.Scope, namePattern, nameMode, req.IgnoreDirs, nameCaseSensitive, req.NonRecursive, req.FollowSymlinks)...)
	return streamNullSeparated(findCmd, func(file string) bool {
		if ctx.Err() != nil {
			return false
		}
		cmd := exec.CommandContext(ctx, tool, buildArgs(file)...)
		_ = streamGrepLines(ctx, cmd, results) // a single file failing to start doesn't abort the rest — see this func's own doc comment
		return true
	})
}

// sendResult sends r on results, reporting via its own bool return
// whether the caller's loop should keep going — false once ctx is
// done, so a cancelled search's own producer (streamNullSeparated's
// onPath callback, or streamGrepLines directly) stops reading further
// output instead of blocking on a channel nothing's draining anymore.
func sendResult(ctx context.Context, results chan<- Result, r Result) bool {
	select {
	case results <- r:
		return true
	case <-ctx.Done():
		return false
	}
}

// streamNullSeparated runs cmd, calling onPath with each NUL-delimited
// token its stdout produces (see FindArgs/LocateArgs' own -print0/-0),
// stopping early if onPath returns false. Only a failure to start cmd
// is returned as an error — see Run's own doc comment on why a
// non-zero exit afterward isn't.
func streamNullSeparated(cmd *exec.Cmd, onPath func(path string) bool) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	reader := bufio.NewReader(stdout)
	for {
		token, readErr := reader.ReadString(0)
		token = strings.TrimSuffix(token, "\x00")
		if token != "" {
			if !onPath(token) {
				break
			}
		}
		if readErr != nil {
			break // io.EOF, or the pipe closing once the process exits
		}
	}
	_ = cmd.Wait() // see Run's own doc comment — a non-zero exit here isn't itself an error
	return nil
}

// streamGrepLines runs cmd (grep/zgrep/zipgrep, all -n'd — see
// GrepArgs/ZgrepArgs/ZipgrepArgs), parsing each "path:line:text" line
// of its stdout into a content Result. Only a failure to start cmd is
// returned as an error, the same as streamNullSeparated.
func streamGrepLines(ctx context.Context, cmd *exec.Cmd, results chan<- Result) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // a long matching line shouldn't truncate the scan
	for scanner.Scan() {
		path, line, text, ok := parseGrepLine(scanner.Text())
		if !ok {
			continue
		}
		if !sendResult(ctx, results, Result{Path: path, Line: line, Text: text}) {
			break
		}
	}
	_ = cmd.Wait()
	return nil
}

// parseGrepLine splits one "-n -H"-formatted grep output line
// ("path:line:text") into its three parts. A path containing a ":" is
// a real, if rare, ambiguity this can't resolve perfectly — SplitN
// with n=3 assumes the first two colons are the separators between
// path/line/text, which holds unless the path itself contains one
// before its own line number's colon.
func parseGrepLine(line string) (path string, lineNo int, text string, ok bool) {
	parts := strings.SplitN(line, ":", 3)
	if len(parts) != 3 {
		return "", 0, "", false
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, "", false
	}
	return parts[0], n, parts[2], true
}
