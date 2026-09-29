package ui

import (
	"sync"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/notify"
)

// attachTestNotify wires a fresh *notify.Store into r and returns a
// func collecting every Message pushed to it since — the same
// "attach and hand back a reader" shape attachTestActivityLog already
// establishes for internal/activitylog, adapted to notify.Subscribe
// instead of a real log file.
func attachTestNotify(t *testing.T, r *Root) func() []notify.Message {
	t.Helper()
	store := notify.New()
	var mu sync.Mutex
	var got []notify.Message
	store.Subscribe(func(m notify.Message) {
		mu.Lock()
		got = append(got, m)
		mu.Unlock()
	})
	r.notify = store
	return func() []notify.Message {
		mu.Lock()
		defer mu.Unlock()
		out := make([]notify.Message, len(got))
		copy(out, got)
		return out
	}
}
