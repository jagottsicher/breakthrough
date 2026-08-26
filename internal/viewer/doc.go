// Package viewer implements the read-only, "just show me what's in this
// file" backend behind breakthrough's Look feature (see internal/ui —
// LookShortcut/openLook, the Look context-menu entry, and the ^L button in
// the bottom bar). Look is deliberately separate from Edit
// (internal/ui.runEditor): it never modifies the file, and it works even
// when $VISUAL/$EDITOR isn't set to anything at all.
//
// Phase 1 (this package's current scope) covers plain text content only —
// source code, config files, diffs/patches, logs — read directly by this
// package's own Load/ReadPreview rather than by shelling out to less/cat:
// no external dependency required for the common case (see internal/ui's
// showBuiltinLook, which renders whatever this package returns in its own
// scrollable overlay). config.Settings.Pager can still opt into an
// external pager (bat/less/$PAGER/more — see internal/ui's
// externalPagerCommand) for syntax highlighting or a user's own preferred
// tool, bypassing this package's own Kind detection entirely.
//
// A file this package doesn't recognize as text (see Sniff) comes back as
// KindUnsupported — internal/ui reports that as "no viewer yet for this
// file type" rather than attempting to dump binary content into a
// TextView. Later phases are expected to teach this package new Kind
// values (images, PDF, archive listings) rather than growing Sniff's own
// text-detection heuristic to cover them.
package viewer
