// Package batchrename implements the pure name-transform, planning,
// and apply/undo logic behind the context menu's "Batch rename" entry
// (see internal/ui/batchrename.go for the screen built on top of it) —
// the same split this project already keeps between filesystem/
// transform logic (here, and internal/fsops, internal/replace) and the
// UI code that drives it.
//
// A batch rename is a fixed pipeline of independent steps, always
// applied in this order to one file's base name (its extension is
// split off first and only rejoined at the very end, see splitName):
//
//  1. Search & replace (plain text or regex)
//  2. Case transform (upper/lower/title/sentence)
//  3. Trim (a fixed number of characters off the front and/or back)
//  4. Numbering (a zero-padded counter inserted as a prefix or suffix)
//  5. Extension handling (lower/upper/remove/replace — acts on the
//     extension split off in step 0, not the base name steps 1-4 work
//     on, so a case transform never accidentally reaches into ".JPG")
//
// A step whose fields are left at their zero value is a no-op — there
// is no separate per-step "enabled" toggle to also set. This is a
// deliberately simpler model than Total Commander's Multi-Rename Tool,
// which pairs every field with its own checkbox: here, a blank Find
// field, a Case of CaseNone, and so on already mean "don't touch this",
// so there's nothing to reconcile if a field and its checkbox ever
// disagreed.
//
// Search & replace deliberately reimplements matching with Go's own
// strings/regexp rather than reusing internal/replace's real-sed
// engine: a filename has none of the multi-line, binary-detection, or
// file-I/O concerns that engine exists for, and the batch-rename
// screen recomputes every selected file's new name on every keystroke
// (see internal/ui's own live preview table) — shelling out to sed once
// per file on every keystroke would make that preview visibly lag once
// more than a handful of files are selected. The trade-off is a second
// regex dialect (Go's RE2, not GNU sed's BRE/ERE) for anyone who wants
// regex mode here too — a deliberate, considered choice given the live
// preview is the whole point of this screen, not an oversight.
//
// Renaming never crosses filesystems (the new path always sits in the
// same directory the file already does — see Plan), so unlike a Move,
// there is no EXDEV case to fall back on here.
package batchrename
