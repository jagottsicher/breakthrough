package operations

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewTrackerWithPersistenceEmptyPathBehavesLikeNewTracker(t *testing.T) {
	tr := NewTrackerWithPersistence("")
	tr.Start("paste", "/src", "/dst")

	if tr.persistPath != "" {
		t.Errorf("persistPath = %q, want empty", tr.persistPath)
	}
}

func TestSaveThenReloadRoundTripsFinishedOperations(t *testing.T) {
	fixed := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	isolateTime(t, fixed)

	path := filepath.Join(t.TempDir(), "operations.json")
	tr := NewTrackerWithPersistence(path)
	id := tr.Start("paste", "/src", "/dst")
	end := fixed.Add(5 * time.Second)
	isolateTime(t, end)
	tr.Finish(id, OutcomePartial, 2, 1, 0, []string{"permission denied"})

	reloaded := NewTrackerWithPersistence(path)
	ops := reloaded.List()
	if len(ops) != 1 {
		t.Fatalf("List() = %d operations, want 1", len(ops))
	}
	got := ops[0]
	if got.ID != id || got.Type != "paste" || got.Source != "/src" || got.Target != "/dst" {
		t.Errorf("reloaded identity fields = %+v, want ID=%d Type=paste Source=/src Target=/dst", got, id)
	}
	if got.State != StateDone {
		t.Errorf("State = %v, want StateDone", got.State)
	}
	if got.Outcome != OutcomePartial {
		t.Errorf("Outcome = %v, want OutcomePartial", got.Outcome)
	}
	if got.Succeeded != 2 || got.Failed != 1 {
		t.Errorf("counts = %+v, want Succeeded=2 Failed=1", got)
	}
	if len(got.Errors) != 1 || got.Errors[0] != "permission denied" {
		t.Errorf("Errors = %+v, want [\"permission denied\"]", got.Errors)
	}
	if !got.StartTime.Equal(fixed) || !got.EndTime.Equal(end) {
		t.Errorf("StartTime/EndTime = %v/%v, want %v/%v", got.StartTime, got.EndTime, fixed, end)
	}
}

// TestRunningOrPausedOperationsNeverPersist pins the spec's own
// "laufende Jobs nach Programmende nicht vortäuschen" — Start and
// SetState never write to disk, only Finish does (see this package's
// own doc comment on why that's enough, by construction, without a
// special case).
func TestRunningOrPausedOperationsNeverPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.json")
	tr := NewTrackerWithPersistence(path)
	id := tr.Start("paste", "/src", "/dst")
	tr.SetState(id, StatePaused)

	reloaded := NewTrackerWithPersistence(path)
	if ops := reloaded.List(); len(ops) != 0 {
		t.Errorf("a running/paused operation should never be persisted, reloaded %+v", ops)
	}
}

// TestFinishedOperationSurvivesTrackerBeingKilledMidRun pins the other
// half of the same spec line: an operation that was still running when
// the process "exits" (never calls Finish at all) simply isn't in the
// file afterward — not shown as cancelled, not shown as still running,
// just absent, the honest answer to "we don't know how it ended."
func TestFinishedOperationSurvivesTrackerBeingKilledMidRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.json")
	tr := NewTrackerWithPersistence(path)
	stillRunning := tr.Start("rsync", "/a", "/b")
	finished := tr.Start("paste", "/c", "/d")
	tr.Finish(finished, OutcomeSucceeded, 1, 0, 0, nil)
	// stillRunning never gets Finish called — simulates the process
	// exiting mid-job, with no further Tracker calls of any kind.

	reloaded := NewTrackerWithPersistence(path)
	ops := reloaded.List()
	if len(ops) != 1 {
		t.Fatalf("List() = %+v, want exactly the one finished operation", ops)
	}
	if ops[0].ID != finished {
		t.Errorf("got ID %d, want the finished operation's own ID %d — %d should never appear", ops[0].ID, finished, stillRunning)
	}
}

func TestNewTrackerWithPersistenceContinuesIDsPastWhatWasLoaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.json")
	tr := NewTrackerWithPersistence(path)
	id := tr.Start("paste", "/src", "/dst")
	tr.Finish(id, OutcomeSucceeded, 1, 0, 0, nil)

	reloaded := NewTrackerWithPersistence(path)
	newID := reloaded.Start("rsync", "/a", "/b")
	if newID <= id {
		t.Errorf("newID %d should be greater than the previously persisted ID %d", newID, id)
	}
}

// TestSaveStaysAtCapacityAfterEviction pins the save-side half of ring-
// buffer eviction at capacity — TestRingBufferEvictsOldestAtCapacity
// (tracker_test.go) already covers the in-memory List() side; this
// confirms the persisted file on disk tracks it too, rather than
// drifting to capacity+1 or keeping an entry List() itself already
// evicted.
func TestSaveStaysAtCapacityAfterEviction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.json")
	tr := NewTrackerWithPersistence(path)
	for i := 0; i < capacity; i++ {
		id := tr.Start("paste", "/src", "/dst")
		tr.Finish(id, OutcomeSucceeded, 1, 0, 0, nil)
	}
	firstID := tr.List()[0].ID

	// One more, past capacity — evicts firstID both in memory and, via
	// the very next Finish's own save(), on disk.
	id := tr.Start("paste", "/src", "/dst")
	tr.Finish(id, OutcomeSucceeded, 1, 0, 0, nil)

	reloaded := NewTrackerWithPersistence(path)
	ops := reloaded.List()
	if len(ops) != capacity {
		t.Fatalf("persisted file has %d entries after eviction, want capped at %d", len(ops), capacity)
	}
	for _, op := range ops {
		if op.ID == firstID {
			t.Errorf("persisted file still contains evicted ID %d", firstID)
		}
	}
}

func TestNewTrackerWithPersistenceMissingFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	tr := NewTrackerWithPersistence(path)
	if ops := tr.List(); len(ops) != 0 {
		t.Errorf("List() = %+v, want empty for a missing file", ops)
	}
}

func TestNewTrackerWithPersistenceMalformedFileStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "operations.json")
	if err := os.WriteFile(path, []byte("{ not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	tr := NewTrackerWithPersistence(path)
	if ops := tr.List(); len(ops) != 0 {
		t.Errorf("List() = %+v, want empty for a malformed file", ops)
	}
}
