// Package docsembed exists for exactly one reason: go:embed patterns
// can't contain ".." (verified directly against the compiler, not
// guessed — "pattern ../docs/x.md: invalid pattern syntax"), so
// nothing under cmd/ or internal/ can embed docs/ itself. This tiny
// root-level package is the one place in the module tree that
// actually has docs/ as a direct subdirectory, so it's the one place
// that can.
//
// Deliberately holds nothing but the embed itself — internal/docsearch
// is where this content actually gets parsed and indexed; this
// package is purely the "how do the bytes get into the binary at all"
// seam, so docs/user-guide.md ships inside every breakthrough binary
// regardless of how it was installed (apt/rpm/brew/go install), the
// same "no external runtime dependency" guarantee the man page and
// README already have in .goreleaser.yaml's own packaging, just for a
// file that packaging doesn't otherwise ship.
package docsembed

import "embed"

// FS holds docs/user-guide.md — the AI Consigliere's own searchable
// documentation corpus (see internal/docsearch). Only the user guide
// for now: breakthrough.1 is roff source, not plain text, and would
// need its own macro-stripping before it's worth indexing at all.
//
//go:embed docs/user-guide.md
var FS embed.FS
