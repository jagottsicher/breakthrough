package operations

import (
	"reflect"
	"testing"
	"time"
)

func isolateTime(t *testing.T, fixed time.Time) {
	t.Helper()
	original := timeNow
	timeNow = func() time.Time { return fixed }
	t.Cleanup(func() { timeNow = original })
}

func TestStartRecordsARunningOperation(t *testing.T) {
	fixed := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	isolateTime(t, fixed)

	tr := NewTracker()
	id := tr.Start("paste", "/src", "/dst")

	ops := tr.List()
	if len(ops) != 1 {
		t.Fatalf("List() = %d operations, want 1", len(ops))
	}
	got := ops[0]
	want := Operation{ID: id, Type: "paste", Source: "/src", Target: "/dst", StartTime: fixed, State: StateRunning}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Start() recorded %+v, want %+v", got, want)
	}
}

func TestStartAssignsIncreasingIDs(t *testing.T) {
	tr := NewTracker()
	first := tr.Start("paste", "a", "b")
	second := tr.Start("rsync", "c", "d")
	if second <= first {
		t.Errorf("second ID %d should be greater than first ID %d", second, first)
	}
}

func TestSetStateUpdatesExistingOperation(t *testing.T) {
	tr := NewTracker()
	id := tr.Start("paste", "/src", "/dst")

	if !tr.SetState(id, StatePaused) {
		t.Fatal("SetState() = false, want true for an existing operation")
	}
	ops := tr.List()
	if ops[0].State != StatePaused {
		t.Errorf("State = %v, want StatePaused", ops[0].State)
	}
}

func TestSetStateOnUnknownIDReportsFalse(t *testing.T) {
	tr := NewTracker()
	if tr.SetState(999, StatePaused) {
		t.Error("SetState() on an unknown ID = true, want false")
	}
}

func TestFinishRecordsOutcomeAndCounts(t *testing.T) {
	start := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	isolateTime(t, start)
	tr := NewTracker()
	id := tr.Start("paste", "/src", "/dst")

	end := start.Add(5 * time.Second)
	isolateTime(t, end)
	errs := []string{"permission denied: x"}
	if !tr.Finish(id, OutcomePartial, 3, 1, 2, errs) {
		t.Fatal("Finish() = false, want true for an existing operation")
	}

	ops := tr.List()
	got := ops[0]
	if got.State != StateDone {
		t.Errorf("State = %v, want StateDone", got.State)
	}
	if got.Outcome != OutcomePartial {
		t.Errorf("Outcome = %v, want OutcomePartial", got.Outcome)
	}
	if got.Succeeded != 3 || got.Failed != 1 || got.Skipped != 2 {
		t.Errorf("counts = %+v, want Succeeded=3 Failed=1 Skipped=2", got)
	}
	if len(got.Errors) != 1 || got.Errors[0] != "permission denied: x" {
		t.Errorf("Errors = %+v, want [\"permission denied: x\"]", got.Errors)
	}
	if !got.EndTime.Equal(end) {
		t.Errorf("EndTime = %v, want %v", got.EndTime, end)
	}
}

func TestFinishCopiesErrorsRatherThanAliasing(t *testing.T) {
	tr := NewTracker()
	id := tr.Start("paste", "/src", "/dst")

	errs := []string{"one error"}
	tr.Finish(id, OutcomeFailed, 0, 1, 0, errs)
	errs[0] = "mutated after Finish"

	ops := tr.List()
	if ops[0].Errors[0] != "one error" {
		t.Errorf("Errors[0] = %q, want it unaffected by mutating the caller's own slice afterward", ops[0].Errors[0])
	}
}

func TestFinishOnUnknownIDReportsFalse(t *testing.T) {
	tr := NewTracker()
	if tr.Finish(999, OutcomeSucceeded, 1, 0, 0, nil) {
		t.Error("Finish() on an unknown ID = true, want false")
	}
}

func TestActiveExcludesFinishedOperations(t *testing.T) {
	tr := NewTracker()
	running := tr.Start("paste", "a", "b")
	done := tr.Start("rsync", "c", "d")
	tr.Finish(done, OutcomeSucceeded, 1, 0, 0, nil)

	active := tr.Active()
	if len(active) != 1 || active[0].ID != running {
		t.Errorf("Active() = %+v, want only the still-running operation %d", active, running)
	}
}

func TestSubscribeReceivesStartSetStateAndFinish(t *testing.T) {
	tr := NewTracker()
	var seen []State
	tr.Subscribe(func(op Operation) { seen = append(seen, op.State) })

	id := tr.Start("paste", "a", "b")
	tr.SetState(id, StatePaused)
	tr.Finish(id, OutcomeSucceeded, 1, 0, 0, nil)

	want := []State{StateRunning, StatePaused, StateDone}
	if len(seen) != len(want) {
		t.Fatalf("Subscribe callback fired %d times, want %d: %+v", len(seen), len(want), seen)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("call %d: State = %v, want %v", i, seen[i], want[i])
		}
	}
}

func TestSubscribeNilTurnsCallbackOff(t *testing.T) {
	tr := NewTracker()
	called := false
	tr.Subscribe(func(Operation) { called = true })
	tr.Subscribe(nil)

	tr.Start("paste", "a", "b")
	if called {
		t.Error("callback fired after Subscribe(nil) turned it off")
	}
}

func TestRingBufferEvictsOldestAtCapacity(t *testing.T) {
	tr := NewTracker()
	for i := 0; i < capacity+5; i++ {
		tr.Start("paste", "a", "b")
	}
	ops := tr.List()
	if len(ops) != capacity {
		t.Fatalf("List() = %d operations, want capped at %d", len(ops), capacity)
	}
	if ops[0].ID != 6 {
		t.Errorf("oldest remaining ID = %d, want 6 (the first 5 evicted)", ops[0].ID)
	}
}

func TestStateStringAndOutcomeString(t *testing.T) {
	cases := []struct {
		s    State
		want string
	}{
		{StateRunning, "running"},
		{StatePaused, "paused"},
		{StateDone, "done"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Errorf("State(%d).String() = %q, want %q", c.s, got, c.want)
		}
	}

	outcomeCases := []struct {
		o    Outcome
		want string
	}{
		{OutcomeNone, "none"},
		{OutcomeSucceeded, "succeeded"},
		{OutcomePartial, "partial"},
		{OutcomeFailed, "failed"},
		{OutcomeCancelled, "cancelled"},
	}
	for _, c := range outcomeCases {
		if got := c.o.String(); got != c.want {
			t.Errorf("Outcome(%d).String() = %q, want %q", c.o, got, c.want)
		}
	}
}
