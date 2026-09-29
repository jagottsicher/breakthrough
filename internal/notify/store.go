package notify

import (
	"sync"
	"time"
)

// capacity is the ring buffer's own hard ceiling — see this package's
// own doc comment for why 999, not unlimited or configurable.
const capacity = 999

// timeNow is a package-level swappable var — the same mockable idiom
// internal/activitylog and internal/firewall already use — so a test
// can pin exactly what a pushed Message's own timestamp reads without
// a real clock's own jitter making that unpredictable.
var timeNow = time.Now

// Store is the live, in-process holder every real trigger this
// package's own doc comment lists reaches for at its own completion —
// a thread-safe ring buffer capped at capacity, plus a single callback
// a later UI layer registers via Subscribe to react to each new
// Message as it arrives, the same "callback Root registers" shape
// Panel.onError already establishes for a different event type. Safe
// for concurrent use — every real trigger this package's own doc
// comment lists runs on its own goroutine. The zero value is not
// ready for use; construct one with New.
type Store struct {
	mu       sync.Mutex
	messages []Message
	nextID   uint64
	onPush   func(Message)
}

// New returns an empty Store, ready for immediate use.
func New() *Store {
	return &Store{}
}

// Subscribe registers fn as the callback Push calls, synchronously,
// once for every new Message — replacing whatever was registered
// before, the same single-callback shape as Panel.onError (see this
// package's own doc comment). A nil fn turns the callback back off
// without discarding anything already in the ring buffer.
func (s *Store) Subscribe(fn func(Message)) {
	s.mu.Lock()
	s.onPush = fn
	s.mu.Unlock()
}

// Push records one new Message — level, category, and text supplied
// by the caller, Time and ID filled in here — evicting the oldest
// entry first whenever the ring buffer is already at capacity (FIFO:
// see this package's own doc comment on why the newest arrival is
// never the one silently dropped), then calls whatever Subscribe most
// recently registered, if anything.
func (s *Store) Push(level Level, category Category, text string) Message {
	s.mu.Lock()
	s.nextID++
	msg := Message{ID: s.nextID, Time: timeNow(), Level: level, Category: category, Text: text}
	if len(s.messages) >= capacity {
		s.messages = s.messages[1:]
	}
	s.messages = append(s.messages, msg)
	onPush := s.onPush
	s.mu.Unlock()

	if onPush != nil {
		onPush(msg)
	}
	return msg
}

// Messages returns a snapshot of every Message currently held, oldest
// first — a copy, safe for the caller to range over or mutate without
// affecting Store's own state or holding its lock.
func (s *Store) Messages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Message, len(s.messages))
	copy(out, s.messages)
	return out
}
