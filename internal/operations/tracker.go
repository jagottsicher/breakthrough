package operations

import (
	"sync"
	"time"
)

// capacity is the ring buffer's own hard ceiling — mirrors notify.
// Store's own 999 (see its own doc comment for why that number): the
// same "never unbounded, never configurable" shape, reused rather than
// re-derived for a package with a much lower expected volume (jobs, not
// every notification).
const capacity = 999

// timeNow is a package-level swappable var — the same mockable idiom
// internal/notify/internal/activitylog already use, so a test can pin
// exactly what a Start/Finish call's own timestamp reads.
var timeNow = time.Now

// Tracker is the live, in-process holder every real job type this
// package's own doc comment lists reaches for at its own start and end
// — a thread-safe ring buffer capped at capacity, plus a single
// callback a later UI layer registers via Subscribe, the same "one
// Store, one callback" shape notify.Store already establishes. Safe for
// concurrent use. The zero value is not ready for use; construct one
// with NewTracker.
type Tracker struct {
	mu       sync.Mutex
	ops      []Operation
	nextID   uint64
	onChange func(Operation)
}

// NewTracker returns an empty Tracker, ready for immediate use.
func NewTracker() *Tracker {
	return &Tracker{}
}

// Subscribe registers fn as the callback Start/SetState/Finish each
// call, synchronously, whenever they change an Operation — replacing
// whatever was registered before. A nil fn turns the callback back off
// without discarding anything already recorded.
func (t *Tracker) Subscribe(fn func(Operation)) {
	t.mu.Lock()
	t.onChange = fn
	t.mu.Unlock()
}

// Start records a new Operation beginning now, returning its own ID for
// a later SetState/Finish call to reference — evicting the oldest
// entry first whenever the ring buffer is already at capacity, the
// same FIFO rule notify.Store.Push already follows. opType/source/
// target are never validated here — see Operation's own doc comment on
// what each is expected to hold.
func (t *Tracker) Start(opType, source, target string) uint64 {
	t.mu.Lock()
	t.nextID++
	op := Operation{
		ID:        t.nextID,
		Type:      opType,
		Source:    source,
		Target:    target,
		StartTime: timeNow(),
		State:     StateRunning,
	}
	if len(t.ops) >= capacity {
		t.ops = t.ops[1:]
	}
	t.ops = append(t.ops, op)
	onChange := t.onChange
	t.mu.Unlock()

	if onChange != nil {
		onChange(op)
	}
	return op.ID
}

// SetState updates id's own State in place — Paste's own pause/resume
// is the only real caller today (see this package's own doc comment) —
// reporting whether an Operation with that ID still exists to update
// (false once it's aged out of the ring buffer), the same shape
// notify.Store.SetRead already reports.
func (t *Tracker) SetState(id uint64, state State) bool {
	op, found := t.mutate(id, func(op *Operation) { op.State = state })
	if found {
		t.notify(op)
	}
	return found
}

// Finish records id's own ending — outcome plus the counts/errors the
// caller's own job already has at hand (see Operation's own doc
// comment on why these are summarized counts, not a full per-item
// breakdown). errs is copied, not kept by reference, so a caller
// reusing its own backing slice afterward can't retroactively change
// what this Tracker holds. Reports whether an Operation with that ID
// still existed to finish, the same reason SetState does.
func (t *Tracker) Finish(id uint64, outcome Outcome, succeeded, failed, skipped int, errs []string) bool {
	errsCopy := append([]string(nil), errs...)
	op, found := t.mutate(id, func(op *Operation) {
		op.State = StateDone
		op.Outcome = outcome
		op.Succeeded = succeeded
		op.Failed = failed
		op.Skipped = skipped
		op.Errors = errsCopy
		op.EndTime = timeNow()
	})
	if found {
		t.notify(op)
	}
	return found
}

// mutate finds id, applies fn to it under lock, and returns a copy of
// the updated Operation plus whether it was found at all — the one
// place SetState/Finish share their own "find by ID, mutate in place"
// logic, rather than each repeating the same loop.
func (t *Tracker) mutate(id uint64, fn func(*Operation)) (Operation, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := range t.ops {
		if t.ops[i].ID == id {
			fn(&t.ops[i])
			return t.ops[i], true
		}
	}
	return Operation{}, false
}

// notify calls whatever Subscribe most recently registered, if
// anything — split out from SetState/Finish only so neither has to
// re-read t.onChange under its own separate lock acquisition.
func (t *Tracker) notify(op Operation) {
	t.mu.Lock()
	onChange := t.onChange
	t.mu.Unlock()
	if onChange != nil {
		onChange(op)
	}
}

// List returns a snapshot of every Operation currently held, oldest
// first — a copy, safe for the caller to range over without holding
// this Tracker's own lock, the same shape notify.Store.Messages already
// uses.
func (t *Tracker) List() []Operation {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Operation, len(t.ops))
	copy(out, t.ops)
	return out
}

// Active returns every currently-held Operation whose State isn't
// StateDone yet, oldest first — what a future Transfer-Zentrale
// screen's own "what's running right now" section would show.
func (t *Tracker) Active() []Operation {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []Operation
	for _, op := range t.ops {
		if op.State != StateDone {
			out = append(out, op)
		}
	}
	return out
}
