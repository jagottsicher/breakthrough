package viewer

import "bytes"

// Kind is what Load decided a file's content actually is — see Load and
// Sniff.
type Kind int

const (
	// KindUnsupported is anything this package doesn't yet know how to
	// preview — currently every binary file (see Sniff), since Phase 1
	// only covers plain text. Later phases add their own Kind values
	// (images, PDF, archive listings) rather than teaching Sniff to
	// recognize them by content alone.
	KindUnsupported Kind = iota
	// KindText is plain, previewable text content — source code, config
	// files, diffs/patches, logs, anything else Sniff didn't flag as
	// binary.
	KindText
)

// sniffLen is how much of a file's content Sniff actually inspects — the
// same 8000-byte convention git itself uses to decide whether to diff a
// file as text or report it as binary (verified against git's own
// xdiff-interface.c buffer_is_binary, not guessed): generous enough not
// to be fooled by a text file that happens to start with a long run of
// plain content before anything binary-looking, cheap enough to read
// unconditionally.
const sniffLen = 8000

// Sniff decides Kind from sample, a prefix of a file's real content (see
// Load, which always passes the file's own first sniffLen bytes — never
// a shorter, truncated caller-supplied slice). A NUL byte anywhere in it
// is treated as the tell that this is binary content, not text — Sniff
// never actually attempts to validate UTF-8 or count printable-vs-control
// bytes, on the theory that a real NUL is the one byte no ordinary text
// file (any encoding, any language) legitimately contains, while "mostly
// printable" heuristics are far easier to fool one way or the other.
//
// Known limitation, accepted rather than solved here: UTF-16 text (a
// real, if uncommon, encoding for a Windows-authored file) legitimately
// contains a NUL byte after every plain-ASCII character, and would be
// misclassified as binary by this same rule — the identical limitation
// git's own binary detection has.
func Sniff(sample []byte) Kind {
	if bytes.IndexByte(sample, 0) != -1 {
		return KindUnsupported
	}
	return KindText
}
