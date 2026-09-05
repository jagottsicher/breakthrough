package main

import "runtime/debug"

// resolveDevBuildVersion fills in version/commit/date's own "dev"/"none"/
// "unknown" defaults from the running binary's own embedded VCS info —
// vcs.revision/vcs.time/vcs.modified, among the settings
// debug.ReadBuildInfo returns — whenever ldflags never touched them in
// the first place (see the doc comment on the vars themselves).
//
// Go has stamped this into every binary built from inside a real git
// checkout since 1.18, with zero extra build flags needed — verified
// directly here, not assumed: built a throwaway package inside this very
// repo and read its own debug.ReadBuildInfo() back, which showed
// exactly these three vcs.* settings (see the commit introducing this
// file's own history for the raw output). That means a plain
// "go build ./..." — the exact command this project's own CONTRIBUTING.md
// already tells a new contributor to run — already carries a real
// commit and dirty-tree flag on its own, at no cost to anyone. Before
// this, that information existed only in ldflags, which only
// .goreleaser.yaml's own release pipeline ever set — every other build
// showed a bare, uninformative "dev" no matter which commit it actually
// came from, a real gap a user building straight from a development
// branch ran into directly.
//
// Only ever asked to run when version is still literally "dev" (see
// main's own init call) — an ldflags-stamped release build already has
// everything real to show and must never be second-guessed by this.
//
// Returns ok=false (leaving every var untouched) when no vcs.revision
// setting exists at all — built with -buildvcs=false, or from a source
// tree with no VCS info available (a downloaded tarball of the source,
// say) — in which case the original "dev"/"none"/"unknown" defaults are
// exactly the honest answer: there is nothing more specific to report.
func resolveDevBuildVersion(settings []debug.BuildSetting) (version, commit, date string, ok bool) {
	var revision, vcsTime string
	var modified bool
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.time":
			vcsTime = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if revision == "" {
		return "", "", "", false
	}

	short := revision
	if len(short) > 12 {
		short = short[:12]
	}
	version = "dev+" + short
	if modified {
		version += "-dirty"
	}
	commit = revision
	date = vcsTime
	return version, commit, date, true
}

// applyDevBuildVersion is resolveDevBuildVersion plus the one thing that
// makes it real: reading the running binary's own actual build info and
// only overwriting version/commit/date when it's still the plain,
// ldflags-untouched "dev" — see resolveDevBuildVersion's own doc comment
// for the full reasoning. Called once, from an init func below, so
// every reader of these vars (--version, SetVersionInfo, ...) sees the
// resolved value regardless of which one happens to run first.
func applyDevBuildVersion() {
	if version != "dev" {
		return // ldflags already set a real release version — nothing to fall back for
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if v, c, d, ok := resolveDevBuildVersion(info.Settings); ok {
		version, commit, date = v, c, d
	}
}

func init() {
	applyDevBuildVersion()
}
