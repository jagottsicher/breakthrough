package ui

import "sync/atomic"

// suspendDepth counts how many of Root's own suspend calls are
// currently in flight — see SuspendedForExternalProcess's own doc
// comment for why cmd/breakthrough's signal handler needs this. A
// depth rather than a bool only because nothing here actually
// guarantees exactly one at a time (nested Suspends shouldn't happen
// in practice, but a counter costs nothing extra and can't go
// negative-surprising the way a bool left set by a missed decrement
// could).
var suspendDepth atomic.Int32

// SuspendedForExternalProcess reports whether this process is
// currently inside one of Root's own suspend calls (see that method's
// own doc comment) — handing the real terminal over to a child
// process: the bash console's full-screen commands, Look/Edit, mail,
// the SSH key/shell dialogs, Sessions' own screen/tmux attach, ....
//
// cmd/breakthrough's own signal handler checks this before treating a
// SIGINT as "quit breakthrough": while a child has the terminal,
// tcell's raw mode is off, so a real SIGINT reaches this process on
// Ctrl+C — unlike the normal, non-suspended case, where tcell's own
// raw mode already intercepts Ctrl+C as a key event before it ever
// becomes a signal at all (see installSignalHandler's own doc comment
// in main.go). That's exactly the moment the user most likely meant
// Ctrl+C for whatever they're watching (`tail -f`, `less`, an editor,
// an attached tmux session, ...), not for breakthrough itself — a
// real, user-reported bug: Ctrl+C during e.g. `tail -f` from the bash
// console used to exit breakthrough outright instead of just
// interrupting tail and returning to it.
func SuspendedForExternalProcess() bool {
	return suspendDepth.Load() > 0
}

// suspend wraps r.app.Suspend, additionally tracking suspendDepth for
// the duration of fn. Every one of Root's own full-screen operations
// goes through this now instead of calling r.app.Suspend directly, so
// none of them has to remember this bookkeeping on its own — see
// SuspendedForExternalProcess's own doc comment for what it's for.
func (r *Root) suspend(fn func()) {
	suspendDepth.Add(1)
	defer suspendDepth.Add(-1)
	r.app.Suspend(fn)
}
