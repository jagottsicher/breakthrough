// Package operations is breakthrough's own internal, UI-independent
// record of what the app is currently doing and has recently done —
// Paste, backgrounded Rsync, and (as each gets wired in) every other
// job type that already runs on its own goroutine — per
// feature_ideas.txt's own "2. Operationen als einheitliche Grundlage
// erfassen": the shared foundation a future Transfer-Zentrale screen
// (seeing everything running at once, regardless of which feature
// started it) and, later, a real action history/Undo both need, rather
// than each job type staying an island only internal/ui itself can see
// into.
//
// Deliberately lifecycle-only for this first pass, not live progress:
// a Start call when a job begins, exactly one Finish call when it ends
// (succeeded, partially, failed, or cancelled), and SetState for the
// one additional transition that already exists today (Paste's own
// pause/resume, see internal/ui/pastepause.go). Continuous
// byte/item-level progress already lives in each job's own existing
// state (pasteJob.remaining, rsyncJob's own progress line) and stays
// there — duplicating it into a second, independently-updated copy
// here would only create a second source of truth that can drift from
// the first, for a number no caller of this package actually needs
// yet. Add it later, once a real screen exists that would use it.
//
// Persistence (see persist.go) covers *finished* Operations only, per
// feature_ideas.txt's own explicit "Nur abgeschlossene, relevante
// Zusammenfassungen persistieren; laufende Jobs nach Programmende
// nicht vortäuschen" — a running/paused Operation is never written to
// disk, so one killed mid-run by the process exiting is simply absent
// from the file afterward, by construction rather than by a special
// case checking for it. A future Undo feature's own durable record is
// still a separate design question this summary-only file was never
// meant to answer.
//
// Type reuses the exact string values notify.Category/
// activitylog.Category already use ("paste", "rsync", ...) — a plain
// string, not a third, separately-defined enum of the same handful of
// words, and deliberately not an import of internal/notify itself
// (keeps this package free to be imported in the other direction later,
// e.g. by a notify trigger that wants to reference an Operation's own
// ID, without a cycle).
//
// A Paste still queued behind an already-running one (see internal/ui's
// own pasteQueue/queuedPaste) has no Operation here yet — one is only
// ever Start'd once reallyStartPaste actually begins it, not when
// startPaste merely enqueues it. A future Transfer-Zentrale wanting to
// show "N more waiting" needs its own look at that queue; this
// package's own Active() alone can't answer it.
package operations
