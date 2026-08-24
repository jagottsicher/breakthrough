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

func filenameCommand(req Request) (name string, args []string, ok bool) {
	if req.Engine == EngineLocate {
		a, ok := LocateArgs(runtime.GOOS, req.Pattern, req.Mode)
		return "locate", a, ok
	}
	return "find", FindArgs(runtime.GOOS, req.Scope, req.Pattern, req.Mode, req.IgnoreDirs), true
}

// underIgnoredDir reports whether path has any of ignoreDirs as one of
// its own path components — locate's own client-side equivalent of
// FindArgs' -prune (see Request.IgnoreDirs' own doc comment on why
// locate needs this separate check rather than a traversal-level
// prune: its results come from a prebuilt index, not a live walk).
func underIgnoredDir(path string, ignoreDirs []string) bool {
	if len(ignoreDirs) == 0 {
		return false
	}
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		for _, ignored := range ignoreDirs {
			if part == ignored {
				return true
			}
		}
	}
	return false
}

func runContentSearch(ctx context.Context, req Request, results chan<- Result) error {
	switch req.Content {
	case ContentGrep:
		cmd := exec.CommandContext(ctx, "grep", GrepArgs(req.Pattern, req.Scope, req.Mode)...)
		return streamGrepLines(ctx, cmd, results)
	case ContentGzip:
		return searchArchives(ctx, req, "*.gz", "zgrep", func(f string) []string { return ZgrepArgs(req.Pattern, req.Mode, f) }, results)
	case ContentZip:
		return searchArchives(ctx, req, "*.zip", "zipgrep", func(f string) []string { return ZipgrepArgs(req.Pattern, req.Mode, f) }, results)
	}
	return nil
}

// searchArchives finds every file under req.Scope matching
// namePattern (e.g. "*.gz"), then runs one tool invocation per file
// found — zgrep/zipgrep's own shared limitation, neither supports
// recursing a directory tree itself (see their own doc comments in
// grep.go) — streaming each one's matches as they're found. A single
// archive that fails to start (a corrupt file, an odd permission) is
// skipped rather than aborting the rest of the search: unlike the
// top-level find/locate/grep call, this isn't the one command the
// whole search depends on.
func searchArchives(ctx context.Context, req Request, namePattern, tool string, buildArgs func(file string) []string, results chan<- Result) error {
	findCmd := exec.CommandContext(ctx, "find", FindArgs(runtime.GOOS, req.Scope, namePattern, ModeGlob, req.IgnoreDirs)...)
	return streamNullSeparated(findCmd, func(archive string) bool {
		if ctx.Err() != nil {
			return false
		}
		cmd := exec.CommandContext(ctx, tool, buildArgs(archive)...)
		_ = streamGrepLines(ctx, cmd, results) // a single archive failing to start doesn't abort the rest — see this func's own doc comment
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
