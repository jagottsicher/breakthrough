package ui

import (
	"strings"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// controlledOnFetchEntries wires panel's own onFetchEntries to a hook
// that never calls done on its own — it captures the call instead, so
// a test can inspect the panel's own in-flight state (loading/
// loadingPath) before deciding when (and with what result) to let the
// fetch "land", exactly mirroring what the real one (wired by
// Root.wirePanel) would eventually do via QueueUpdateDraw, just
// without a real goroutine or Application.Run to drain it.
func controlledOnFetchEntries(p *Panel) (fire func(entries []fsops.Entry, err error)) {
	var done func([]fsops.Entry, error)
	p.onFetchEntries = func(abs string, remote remotefs.Client, d func([]fsops.Entry, error)) {
		done = d
	}
	return func(entries []fsops.Entry, err error) {
		if done == nil {
			panic("fire called before onFetchEntries was ever invoked")
		}
		done(entries, err)
	}
}

// TestLoadAsyncShowsALoadingIndicatorBeforeTheFetchLands pins
// loadAsync's own immediate, visible in-flight state (see Panel.loading's
// own doc comment on the struct, and the status bar's own "Loading…"
// segment in buildStatusBar) — the table stays showing whatever it
// already had the whole time, so this is the only signal a slow remote
// fetch is even happening at all.
func TestLoadAsyncShowsALoadingIndicatorBeforeTheFetchLands(t *testing.T) {
	r := newTestRemoteRoot(t)
	fire := controlledOnFetchEntries(r.panel)

	r.panel.navigate("/remote/elsewhere", func(error) {})

	if !r.panel.loading {
		t.Fatal("panel.loading = false, want true while the fetch is still in flight")
	}
	if r.panel.loadingPath != "/remote/elsewhere" {
		t.Errorf("panel.loadingPath = %q, want /remote/elsewhere", r.panel.loadingPath)
	}
	if got := r.buildStatusBar(); !strings.Contains(got, "Loading elsewhere") {
		t.Errorf("status bar = %q, want a Loading segment naming the target directory", got)
	}

	fire(nil, nil)
	if r.panel.loading {
		t.Error("panel.loading still true after the fetch landed")
	}
}

// TestLoadAsyncAppliesTheResultOnceItLands pins the other half: once
// the fetch actually lands, applyEntries runs and the table reflects
// the new directory's own real content — not before.
func TestLoadAsyncAppliesTheResultOnceItLands(t *testing.T) {
	r := newTestRemoteRoot(t)
	fire := controlledOnFetchEntries(r.panel)

	var navErr error
	r.panel.navigate("/remote/elsewhere", func(err error) { navErr = err })
	if r.panel.path == "/remote/elsewhere" {
		t.Fatal("panel.path already updated before the fetch landed")
	}

	fire([]fsops.Entry{{Name: "x.txt", Type: fsops.TypeFile}}, nil)

	if navErr != nil {
		t.Fatalf("navigate's own done callback reported an error: %v", navErr)
	}
	if r.panel.path != "/remote/elsewhere" {
		t.Errorf("panel.path = %q, want /remote/elsewhere once the fetch landed", r.panel.path)
	}
	var names []string
	for row := 1; row < r.panel.table.GetRowCount(); row++ {
		if ref, ok := r.panel.rowRef(row); ok {
			names = append(names, ref.name)
		}
	}
	if strings.Join(names, ",") != "x.txt" {
		t.Errorf("listed names = %v, want just x.txt", names)
	}
}

// TestLoadAsyncDiscardsAStaleResultSupersededByANewerRequest pins the
// same "a background answer for a target that's no longer current gets
// thrown away" rule applyPanelDiskUsage/applyLogAuditRotatedFile's own
// stale-result guards already establish: navigating a second time
// before the first request's own fetch has landed must mean the first
// one's eventual result never overwrites the second's.
func TestLoadAsyncDiscardsAStaleResultSupersededByANewerRequest(t *testing.T) {
	r := newTestRemoteRoot(t)

	var firstDone func([]fsops.Entry, error)
	r.panel.onFetchEntries = func(abs string, remote remotefs.Client, done func([]fsops.Entry, error)) {
		firstDone = done
	}
	r.panel.navigate("/remote/first", func(error) {})
	if firstDone == nil {
		t.Fatal("setup: first fetch never dispatched")
	}

	var secondDone func([]fsops.Entry, error)
	r.panel.onFetchEntries = func(abs string, remote remotefs.Client, done func([]fsops.Entry, error)) {
		secondDone = done
	}
	r.panel.navigate("/remote/second", func(error) {})
	if secondDone == nil {
		t.Fatal("setup: second fetch never dispatched")
	}

	secondDone([]fsops.Entry{{Name: "second.txt", Type: fsops.TypeFile}}, nil)
	if r.panel.path != "/remote/second" {
		t.Fatalf("setup: panel.path = %q, want /remote/second applied first", r.panel.path)
	}

	firstDone([]fsops.Entry{{Name: "first.txt", Type: fsops.TypeFile}}, nil) // the stale, superseded one

	if r.panel.path != "/remote/second" {
		t.Errorf("panel.path = %q after the stale result landed, want it still /remote/second", r.panel.path)
	}
}

// TestLoadAsyncDiscardsAResultAfterDisconnect pins the other staleness
// case: a fetch still in flight when the panel disconnects (or
// reconnects to a different session) must never apply once it lands —
// onFetchEntries is handed remote explicitly rather than reading
// p.remote again for exactly this reason (see loadAsync's own doc
// comment).
func TestLoadAsyncDiscardsAResultAfterDisconnect(t *testing.T) {
	r := newTestRemoteRoot(t)
	fire := controlledOnFetchEntries(r.panel)

	r.panel.navigate("/remote/elsewhere", func(error) {})
	if !r.panel.loading {
		t.Fatal("setup: expected a fetch in flight")
	}

	if err := r.panel.disconnectRemote(); err != nil {
		t.Fatalf("disconnectRemote: %v", err)
	}
	localPathAfterDisconnect := r.panel.path

	fire([]fsops.Entry{{Name: "x.txt", Type: fsops.TypeFile}}, nil)

	if r.panel.path != localPathAfterDisconnect {
		t.Errorf("panel.path = %q after a stale remote result landed post-disconnect, want it still %q", r.panel.path, localPathAfterDisconnect)
	}
}
