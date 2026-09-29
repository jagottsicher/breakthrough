package notify

import (
	"sync"
	"testing"
	"time"
)

func withFixedTime(t *testing.T, at time.Time) {
	t.Helper()
	orig := timeNow
	timeNow = func() time.Time { return at }
	t.Cleanup(func() { timeNow = orig })
}

func TestPushFillsTimeAndIncrementingID(t *testing.T) {
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	withFixedTime(t, at)

	s := New()
	first := s.Push(LevelSuccess, CategoryRsync, "rsync backup done")
	second := s.Push(LevelError, CategoryPaste, "paste failed")

	if first.ID != 1 || second.ID != 2 {
		t.Fatalf("got IDs %d, %d, want 1, 2", first.ID, second.ID)
	}
	if !first.Time.Equal(at) || !second.Time.Equal(at) {
		t.Fatalf("got times %v, %v, want both %v", first.Time, second.Time, at)
	}
	if first.Read {
		t.Fatal("a freshly pushed Message must start unread")
	}
}

func TestMessagesReturnsOldestFirst(t *testing.T) {
	s := New()
	s.Push(LevelInfo, CategoryFirewall, "first")
	s.Push(LevelInfo, CategoryFirewall, "second")
	s.Push(LevelInfo, CategoryFirewall, "third")

	got := s.Messages()
	if len(got) != 3 {
		t.Fatalf("got %d messages, want 3", len(got))
	}
	want := []string{"first", "second", "third"}
	for i, w := range want {
		if got[i].Text != w {
			t.Fatalf("message %d: got %q, want %q", i, got[i].Text, w)
		}
	}
}

func TestMessagesReturnsACopy(t *testing.T) {
	s := New()
	s.Push(LevelInfo, CategoryRsync, "original")

	got := s.Messages()
	got[0].Text = "mutated"

	if s.Messages()[0].Text != "original" {
		t.Fatal("mutating the slice Messages returned must not affect the Store")
	}
}

func TestPushEvictsOldestOnceCapacityIsReached(t *testing.T) {
	s := New()
	for i := 0; i < capacity+1; i++ {
		s.Push(LevelInfo, CategoryRsync, "msg")
	}

	got := s.Messages()
	if len(got) != capacity {
		t.Fatalf("got %d messages, want %d", len(got), capacity)
	}
	// The very first push (ID 1) must be the one evicted; the oldest
	// surviving entry is ID 2, the newest is capacity+1.
	if got[0].ID != 2 {
		t.Fatalf("got oldest surviving ID %d, want 2", got[0].ID)
	}
	if got[len(got)-1].ID != uint64(capacity+1) {
		t.Fatalf("got newest ID %d, want %d", got[len(got)-1].ID, capacity+1)
	}
}

func TestSubscribeIsCalledSynchronouslyOnPush(t *testing.T) {
	s := New()
	var got Message
	calls := 0
	s.Subscribe(func(m Message) {
		got = m
		calls++
	})

	pushed := s.Push(LevelSuccess, CategoryPaste, "copied 3 item(s)")

	if calls != 1 {
		t.Fatalf("got %d callback calls, want 1", calls)
	}
	if got != pushed {
		t.Fatalf("callback got %+v, want %+v", got, pushed)
	}
}

func TestSubscribeNilTurnsCallbackOff(t *testing.T) {
	s := New()
	calls := 0
	s.Subscribe(func(Message) { calls++ })
	s.Subscribe(nil)

	s.Push(LevelInfo, CategoryFirewall, "reverted")

	if calls != 0 {
		t.Fatalf("got %d callback calls after unsubscribing, want 0", calls)
	}
}

func TestSetReadUpdatesTheMatchingMessage(t *testing.T) {
	s := New()
	msg := s.Push(LevelSuccess, CategoryRsync, "done")

	if !s.SetRead(msg.ID, true) {
		t.Fatal("SetRead reported not found for a real ID")
	}
	got := s.Messages()
	if !got[0].Read {
		t.Error("Read was not updated")
	}
}

func TestSetReadReportsFalseForAnUnknownID(t *testing.T) {
	s := New()
	if s.SetRead(999, true) {
		t.Error("SetRead reported success for an ID that was never pushed")
	}
}

func TestDeleteRemovesTheMatchingMessage(t *testing.T) {
	s := New()
	first := s.Push(LevelInfo, CategoryFirewall, "first")
	s.Push(LevelInfo, CategoryFirewall, "second")

	if !s.Delete(first.ID) {
		t.Fatal("Delete reported not found for a real ID")
	}
	got := s.Messages()
	if len(got) != 1 || got[0].Text != "second" {
		t.Errorf("got %+v, want only \"second\" left", got)
	}
}

func TestDeleteReportsFalseForAnUnknownID(t *testing.T) {
	s := New()
	if s.Delete(999) {
		t.Error("Delete reported success for an ID that was never pushed")
	}
}

func TestUnreadCountCountsOnlyUnreadMessages(t *testing.T) {
	s := New()
	first := s.Push(LevelSuccess, CategoryRsync, "first")
	s.Push(LevelSuccess, CategoryRsync, "second")
	s.Push(LevelSuccess, CategoryRsync, "third")
	s.SetRead(first.ID, true)

	if got := s.UnreadCount(); got != 2 {
		t.Errorf("UnreadCount() = %d, want 2", got)
	}
}

func TestPushIsSafeForConcurrentUse(t *testing.T) {
	s := New()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			s.Push(LevelInfo, CategoryRsync, "concurrent")
		}()
	}
	wg.Wait()

	if len(s.Messages()) != goroutines {
		t.Fatalf("got %d messages, want %d", len(s.Messages()), goroutines)
	}
}
