// Package activitylog is breakthrough's own optional activity log: a
// plain-text, append-only record of what breakthrough itself did —
// file operations, permission changes, archive/Rsync runs, remote
// transfers, and so on — kept for two reasons the user's own request
// named explicitly: making what happened with breakthrough
// traceable after the fact, and giving a future Undo feature a real,
// structured record of past actions to work from, rather than having
// to reconstruct one from scratch.
//
// Off by default (see Level's own doc comment) — this is an
// intentionally optional, opt-in feature, not a background service
// breakthrough always runs. Once turned on in Options, two independent
// settings decide what actually gets written: a single Level (how much
// detail — Errors/Actions/Detailed/Debug) and, orthogonal to that, a
// per-Category on/off switch (file operations, permissions, archives,
// remote, Rsync, shell — see Category's own doc comment), since the
// user's own explicit request was to be able to turn "the individual
// things" off within a level, not just dial the level itself up or
// down.
//
// Deliberately a plain, one-line-per-entry text format (see Entry's own
// doc comment), not JSON or a binary format: this is meant to also be
// readable and greppable with ordinary Unix tools (tail -f, grep, less)
// the same way any real syslog-style log already is — this app's own
// in-app viewer (a later, separate piece of work) is a convenience on
// top of that, never the only way to read it.
//
// This package only ever decides *whether* and *how* to record an
// entry, and resolves *where* the file itself lives (see ResolvePath) —
// it has no dependency on internal/config or internal/ui, so it can be
// fully unit-tested on its own; internal/ui is what actually calls Log
// at each real action's own success/failure point, translating
// config.Settings' own LogLevel/LogCategory* fields into this package's
// Level/Category types exactly once, at startup and whenever Options
// changes them.
package activitylog
