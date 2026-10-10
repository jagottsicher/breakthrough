// Package git is the Git breach dashboard's own business logic (see
// "jg" in internal/ui/keymap.go) — repository discovery, per-file
// working-tree status, stage/unstage, commit, and diff, all by
// shelling out to the system's own git(1), the same rationale
// internal/gitstatus already documents for not reimplementing git's
// on-disk format from scratch.
//
// Deliberately a separate package from internal/gitstatus rather than
// an extension of it: gitstatus is narrow, already tested, and feeds
// the status bar/Details sidebar with just branch/ahead-behind/file
// counts — widening it to also carry per-file paths and mutate the
// working tree would risk that existing, unrelated feature for no
// benefit here. The two packages happen to parse the same `git status
// --porcelain=v2` family of output, just asking different questions of
// it.
package git
