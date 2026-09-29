// Package notify is breakthrough's own in-process notification
// service (see feature_ideas.txt's own "3a. Benachrichtigungen" for
// the full, staged plan this package's own Stufe 1 implements): the
// foundation a background job that finishes while the user is
// plausibly looking elsewhere — a backgrounded Rsync run, the paste/
// move queue, the firewall screen's own self-lockout rollback — reaches
// for at its own completion, so that outcome isn't lost the moment its
// status-bar line disappears once the job is done.
//
// A short, curated list of triggers, not a general event bus: see each
// one's own call site in internal/ui for why it qualifies (runs in the
// background while the user plausibly does something else, and its
// outcome would otherwise go unseen) and feature_ideas.txt for which
// ordinary, immediately-visible actions deliberately don't push here.
//
// This package only ever holds and hands out Message values — Time,
// Level, Category, Text, Read status — in a thread-safe ring buffer
// hard-capped at 999 (the 1000th push evicts the oldest, FIFO; a full
// buffer never blocks a new arrival and never silently drops it
// instead). It has no dependency on internal/config, tview, or
// internal/ui, so it's fully unit-testable without a real terminal
// run; internal/ui is what actually calls Push at each real trigger's
// own completion and, in a later stage, Subscribes to render what
// arrives.
package notify
