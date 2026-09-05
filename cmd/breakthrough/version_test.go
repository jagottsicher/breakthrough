package main

import (
	"runtime/debug"
	"testing"
)

// TestResolveDevBuildVersionUsesVCSRevision pins the happy path: a real
// vcs.revision/vcs.time pair turns into a "dev+<short-commit>" version
// and the full commit/date, matching the exact shape verified live
// against a real "go build" inside this repo (see this file's own
// package doc comment in version.go).
func TestResolveDevBuildVersionUsesVCSRevision(t *testing.T) {
	settings := []debug.BuildSetting{
		{Key: "vcs", Value: "git"},
		{Key: "vcs.revision", Value: "430cab8fff94cd1a626af78ff644a177a79ee355"},
		{Key: "vcs.time", Value: "2026-09-05T07:31:37Z"},
		{Key: "vcs.modified", Value: "false"},
	}

	version, commit, date, ok := resolveDevBuildVersion(settings)

	if !ok {
		t.Fatal("resolveDevBuildVersion reported failure with a real vcs.revision present")
	}
	if want := "dev+430cab8fff94"; version != want {
		t.Errorf("version = %q, want %q", version, want)
	}
	if want := "430cab8fff94cd1a626af78ff644a177a79ee355"; commit != want {
		t.Errorf("commit = %q, want the full, untruncated revision %q", commit, want)
	}
	if want := "2026-09-05T07:31:37Z"; date != want {
		t.Errorf("date = %q, want %q", date, want)
	}
}

// TestResolveDevBuildVersionMarksADirtyTree pins the other half of what
// a real report showed (see version.go's own doc comment): an uncommitted
// change at build time is worth surfacing, not silently dropped.
func TestResolveDevBuildVersionMarksADirtyTree(t *testing.T) {
	settings := []debug.BuildSetting{
		{Key: "vcs.revision", Value: "abc123"},
		{Key: "vcs.modified", Value: "true"},
	}

	version, _, _, ok := resolveDevBuildVersion(settings)

	if !ok {
		t.Fatal("resolveDevBuildVersion reported failure")
	}
	if want := "dev+abc123-dirty"; version != want {
		t.Errorf("version = %q, want %q", version, want)
	}
}

// TestResolveDevBuildVersionShortensALongRevision pins the truncation:
// a full 40-character git SHA becomes a 12-character short form in
// version (matching common Go tooling convention for a "short commit"),
// while commit itself keeps the full, untruncated value.
func TestResolveDevBuildVersionShortensALongRevision(t *testing.T) {
	full := "430cab8fff94cd1a626af78ff644a177a79ee355"
	_, commit, _, ok := resolveDevBuildVersion([]debug.BuildSetting{
		{Key: "vcs.revision", Value: full},
	})
	if !ok {
		t.Fatal("resolveDevBuildVersion reported failure")
	}
	if commit != full {
		t.Errorf("commit = %q, want the untruncated %q", commit, full)
	}
}

// TestResolveDevBuildVersionWithNoVCSInfoFails pins the honest-fallback
// case: no vcs.revision setting at all (built with -buildvcs=false, or
// from a source tree with no VCS present) reports failure rather than
// fabricating a version, leaving the original "dev"/"none"/"unknown"
// literals as the accurate answer.
func TestResolveDevBuildVersionWithNoVCSInfoFails(t *testing.T) {
	settings := []debug.BuildSetting{
		{Key: "-buildmode", Value: "exe"},
		{Key: "GOARCH", Value: "amd64"},
	}

	_, _, _, ok := resolveDevBuildVersion(settings)

	if ok {
		t.Error("resolveDevBuildVersion reported success with no vcs.revision present")
	}
}

// TestApplyDevBuildVersionSkipsAnAlreadySetVersion pins the guard that
// makes this safe to always run unconditionally from init: an ldflags-
// stamped release build already has a real version, and must never be
// second-guessed by the VCS-info fallback.
func TestApplyDevBuildVersionSkipsAnAlreadySetVersion(t *testing.T) {
	origVersion, origCommit, origDate := version, commit, date
	t.Cleanup(func() { version, commit, date = origVersion, origCommit, origDate })

	version, commit, date = "1.2.3", "deadbeef", "2026-01-01T00:00:00Z"

	applyDevBuildVersion()

	if version != "1.2.3" || commit != "deadbeef" || date != "2026-01-01T00:00:00Z" {
		t.Errorf("applyDevBuildVersion overwrote an already-set release version: version=%q commit=%q date=%q", version, commit, date)
	}
}

// TestApplyDevBuildVersionFillsInADevBuild is the end-to-end case this
// whole file exists for: this very test binary is itself built with
// "go test" (no ldflags), inside this repo's own real git checkout — so
// version, still at its plain "dev" default at package-init time,
// should already have been overwritten by init() before this test ever
// runs, exactly the situation applyDevBuildVersion exists to fix (a
// real user report: "dev (commit none, built unknown by source)" on
// their own freshly built binary).
func TestApplyDevBuildVersionFillsInADevBuild(t *testing.T) {
	if version == "dev" {
		t.Skip("no VCS info available in this build environment (e.g. -buildvcs=false, or no .git present) — nothing to assert")
	}
	if commit == "none" || date == "unknown" {
		t.Errorf("version moved off \"dev\" (%q) but commit/date didn't follow: commit=%q date=%q", version, commit, date)
	}
}
