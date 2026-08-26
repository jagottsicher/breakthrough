package search

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// tarDecompressor maps a tar-family compression suffix to the external
// tool that decompresses it to a plain, uncompressed tar stream (its
// own "-dc" flag combination — decompress, write to stdout — verified
// identical across gzip/bzip2/xz) — see materializePlainTar. Plain
// ".tar" has no entry here: it's already uncompressed, so
// materializePlainTar returns it unchanged, no decompression step at
// all.
var tarDecompressor = map[string]string{
	".tar.gz":  "gzip",
	".tar.bz2": "bzip2",
	".tar.xz":  "xz",
}

// startTarContentSearch is IncludeCompressed's own tar-family
// counterpart to startCompressedContentSearch (see compressed.go):
// zgrep/bzgrep/xzgrep/zipgrep can each only ever search a single
// compressed FILE, never a tar archive's own multiple member files
// behind that one compressed stream — searchTarMembersContent below
// does that instead, by decompressing each matching archive found
// under req.Scope exactly once (see materializePlainTar — avoiding the
// O(n²) trap of re-decompressing from the start for every member, the
// same class of cost archiveListConcurrency's own doc comment already
// flags for tar -tf) and running a real grep against each member's own
// extracted content in turn — real grep, not a hand-rolled matcher, so
// regex/whole-word/case-insensitive/binary-detection all stay exactly
// as correct as they already are for every other content search path
// in this package.
//
// A no-op, same shape and same gating as startCompressedContentSearch,
// when req.IncludeCompressed is false or Content isn't ContentGrep —
// see Request.IncludeCompressed's own doc comment for why a
// NamePattern-narrowed search (req.NamePattern != "") never reaches
// here at all: runContentSearch's own early branch for that case
// doesn't call either of these two starters, a separate, larger
// feature this doesn't attempt (matching an archive member's own name
// against NamePattern, not just the archive's outer filename).
func startTarContentSearch(ctx context.Context, req Request, results chan<- Result) (wait func()) {
	if !req.IncludeCompressed || req.Content != ContentGrep {
		return func() {}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		searchTarMembersContent(ctx, req, results)
	}()
	return func() { <-done }
}

// searchTarMembersContent finds every tar-family archive under
// req.Scope — reusing archiveCandidates/classifyArchive, the exact
// same candidate step Include Archives' own filename search already
// uses (see archive.go), filtered here to archiveTar only: a .zip
// candidate is skipped outright, since ContentZip/zipgrep (see
// compressed.go) already covers zip content in full — running it again
// here would just duplicate those results under a different code path.
// Archives are searched concurrently with each other, bounded by the
// same archiveListConcurrency pool listArchiveAndSend already uses and
// for the same reason (a directory tree with many matching archives
// shouldn't decompress hundreds of them at once); members within one
// archive are then searched sequentially (see searchOneTarContent) —
// deliberately not a second, nested worker pool: each member's own
// "tar -xO" is already cheap I/O against an uncompressed file at that
// point (no decompression cost left to parallelize), so the added
// complexity of a nested pool isn't worth it here.
func searchTarMembersContent(ctx context.Context, req Request, results chan<- Result) {
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
		kind, ok := classifyArchive(candidate)
		if !ok || kind != archiveTar {
			continue
		}
		candidate := candidate
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			searchOneTarContent(ctx, req, candidate, results)
		}()
	}
	wg.Wait()
}

// searchOneTarContent decompresses archivePath exactly once (see
// materializePlainTar), lists its own members (reusing
// listArchiveMembers — the same listing Include Archives' own filename
// search already runs, just pointed at the decompressed copy), and
// greps each member's own extracted content in turn (see
// grepTarMember). A candidate that fails to decompress or list at all
// (a corrupt archive, tar/gzip/bzip2/xz missing) is silently skipped —
// the same "always additive, never fails an otherwise-fine search"
// philosophy listArchiveAndSend's own doc comment already explains for
// the filename-search case.
func searchOneTarContent(ctx context.Context, req Request, archivePath string, results chan<- Result) {
	if req.OnProgress != nil {
		req.OnProgress(archivePath) // "now opening this archive" — same as listArchiveAndSend
	}

	plainPath, cleanup, err := materializePlainTar(ctx, archivePath)
	if err != nil {
		return
	}
	defer cleanup()

	members, err := listArchiveMembers(ctx, archiveTar, plainPath)
	if err != nil {
		return
	}

	for _, member := range members {
		if ctx.Err() != nil {
			return
		}
		if strings.HasSuffix(member, "/") {
			continue // a directory entry — nothing to grep
		}
		grepTarMember(ctx, req, archivePath, plainPath, member, results)
	}
}

// materializePlainTar returns a path to an uncompressed tar file with
// the same content as archivePath: archivePath itself if it's already
// a plain .tar (tarDecompressor has no entry for it — nothing to
// decompress), or a freshly created temp file holding the matching
// decompressor's own "-dc" output otherwise. cleanup is always safe to
// call unconditionally (a no-op for the "already plain" case, os.Remove
// on the temp file otherwise) — callers defer it right away, the same
// convention config.SetKey's own temp-file-then-rename uses for
// "cleanup is always safe, whether or not it's actually needed".
func materializePlainTar(ctx context.Context, archivePath string) (plainPath string, cleanup func(), err error) {
	lower := strings.ToLower(archivePath)
	tool := ""
	for suffix, t := range tarDecompressor {
		if strings.HasSuffix(lower, suffix) {
			tool = t
			break
		}
	}
	if tool == "" {
		return archivePath, func() {}, nil
	}

	tmp, err := os.CreateTemp("", "breakthrough-tar-content-*.tar")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { os.Remove(tmp.Name()) }

	cmd := exec.CommandContext(ctx, tool, "-dc", archivePath)
	cmd.Stdout = tmp
	runErr := cmd.Run()
	closeErr := tmp.Close()
	if runErr == nil {
		runErr = closeErr
	}
	if runErr != nil {
		cleanup()
		return "", nil, runErr
	}
	return tmp.Name(), cleanup, nil
}

// grepTarMember extracts member's own content from plainPath (an
// already-uncompressed tar — see materializePlainTar) via "tar -xO -f
// plainPath member", piped directly into a real grep — so binary
// detection and regex/whole-word/case-insensitive matching are all
// grep's own, already-correct behavior, not reimplemented here (see
// tarMemberGrepArgs) — and sends a Result per matching line with
// ArchiveMember set to member and Path set to archivePath, the real
// original archive (never plainPath, a throwaway temp file that may
// already be gone by the time a caller acts on the Result — see
// searchOneTarContent's own deferred cleanup).
//
// A failure starting either process (tar or grep missing, a corrupt
// member) is silently skipped, same as every other per-candidate
// failure in this package.
func grepTarMember(ctx context.Context, req Request, archivePath, plainPath, member string, results chan<- Result) {
	tarCmd := exec.CommandContext(ctx, "tar", "-xO", "-f", plainPath, member)
	tarOut, err := tarCmd.StdoutPipe()
	if err != nil {
		return
	}

	grepCmd := exec.CommandContext(ctx, "grep", tarMemberGrepArgs(req.Pattern, req.Mode, req.CaseSensitive, req.WholeWords, req.FirstHit)...)
	grepCmd.Stdin = tarOut
	grepOut, err := grepCmd.StdoutPipe()
	if err != nil {
		return
	}

	if err := tarCmd.Start(); err != nil {
		return
	}
	if err := grepCmd.Start(); err != nil {
		_ = tarCmd.Wait()
		return
	}

	scanner := bufio.NewScanner(grepOut)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // a long matching line shouldn't truncate the scan — same size streamGrepLines/listArchiveMembers already use
	for scanner.Scan() {
		line, text, ok := parseGrepStdinLine(scanner.Text())
		if !ok {
			continue
		}
		if !sendResult(ctx, results, Result{Path: archivePath, ArchiveMember: member, Line: line, Text: text}) {
			break
		}
	}
	_ = tarCmd.Wait()
	_ = grepCmd.Wait()
}

// tarMemberGrepArgs is plain grep's own argument list for a single
// member's content, read from stdin (see grepTarMember) — the same
// -n/-I/-i/-w/-m/pattern shape GrepArgs uses, minus -r (nothing to
// recurse into — stdin is one stream) and -H (stdin has no filename of
// its own for grep to print; Path/ArchiveMember are supplied by
// grepTarMember instead, from what searchOneTarContent already knows).
func tarMemberGrepArgs(pattern string, mode Mode, caseSensitive, wholeWords, firstHit bool) []string {
	args := []string{"-n", "-I"}
	if !caseSensitive {
		args = append(args, "-i")
	}
	if wholeWords {
		args = append(args, "-w")
	}
	if firstHit {
		args = append(args, "-m", "1")
	}
	args = append(args, matchModeFlag(mode))
	return append(args, "-e", pattern)
}

// parseGrepStdinLine splits one "-n"-only (no -H) grep output line
// ("line:text") into its two parts — grepTarMember's own counterpart to
// parseGrepLine, which expects a leading "path:" grep only ever adds
// via -H (meaningless for a stdin stream with no filename of its own).
func parseGrepStdinLine(line string) (lineNo int, text string, ok bool) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return 0, "", false
	}
	n, err := strconv.Atoi(line[:idx])
	if err != nil {
		return 0, "", false
	}
	return n, line[idx+1:], true
}
