---
name: async-ui
description: Add or review a background goroutine in internal/ui — moving a blocking call (remote I/O, a slow local read) off the UI goroutine, a StartClock-ticker-driven function, or anything that calls safeGo/QueueUpdateDraw. Use whenever the user asks to make something async, non-blocking, backgrounded, or "nicht blockierend", or when reviewing/debugging a panic or data race in such code.
---

# Background work in internal/ui

Every async feature in this package (disk usage refresh, Look's remote
download, remote archive download, remote panel navigation, Log
Holepunch's progressive load) follows the same shape. Two real bugs
shipped from skipping pieces of it — a crash (nil `remotefs.Client`
dereference on a local panel) and a data race (a background fetch
triggered as a side effect of test setup, racing a test's own writes
to the same fake client) — both caught only after merge, by a user
report and by `go test -race` respectively. This skill exists so the
next one doesn't repeat either.

## The shape

1. **Capture, don't re-read.** Capture every value the background
   goroutine needs (a `remotefs.Client`, a path) into local variables
   *before* calling `safeGo` — never read `r.panel.remote` or similar
   from inside the goroutine itself. A disconnect mid-fetch must read
   the client it had, not whatever replaced it.
2. **Nil-check the capture, not just inside the goroutine.** If the
   captured value can legitimately be nil (a local panel's own
   `panel.remote`), guard *before* dispatching — inside the triggering
   function, not as a defensive check buried in the goroutine body.
   The exact bug this misses: a ticker-driven function that branches
   on `panel.remote` but gets called unconditionally for every panel,
   local and remote alike.
3. **`safeGo` + `QueueUpdateDraw`, never raw `go func()`.** `safeGo`
   recovers a panic into a logged crash + `showError` instead of
   killing the process; `QueueUpdateDraw` is the only safe way back
   onto the UI goroutine.
4. **Split into `fetchX`/`applyX`.** The apply half must be directly
   callable with an already-known result — `applyPanelDiskUsage`,
   `applyRemoteLookDownload`, `applyLogAuditRotatedFile`,
   `Panel.applyEntries` are the precedents. This is not optional
   polish: `r.app.QueueUpdateDraw` blocks forever without a running
   `Application.Run`, which no test in this package has, so an apply
   step buried inside a `QueueUpdateDraw` closure is untestable.
5. **Discard stale results.** Before applying, compare whatever
   identifies "is this still the thing we asked for" — a path, a
   `remotefs.Client` pointer, an incrementing token — against the
   current state. A superseded request (user already moved on) or a
   disconnect must silently discard, never apply. See `loadToken`/
   `diskUsageForPath`/`viewerLookDownloadPath` for the three shapes
   this guard takes depending on what can go stale.
6. **Rate-gate anything that re-triggers itself.** If the apply step
   repaints something that itself re-reads the triggering condition
   (status bar refresh → `diskUsageFor` → fetch again), add an
   explicit cooldown (`diskUsageFetchedAt`/`diskUsageRefreshInterval`)
   — otherwise a cheap, fast round trip turns into a tight loop instead
   of the ticker's own intended cadence.
7. **Root owns concurrency, Panel (or any plain data struct) doesn't.**
   `Panel` never calls `safeGo` or `QueueUpdateDraw` itself — it gets
   an injected hook (`onFetchEntries`, wired by `Root.wirePanel`) that
   Root implements. Keeps Panel testable without a running
   `Application`, and keeps the goroutine-spawning decision in one
   place per feature.
8. **`remotefs.Client` has no `context.Context` anywhere in its
   interface.** "Cancel" in this codebase always means *stop waiting
   on the result*, never *interrupt the read* — the blocking call
   itself always runs to completion in the background regardless.
   Full ctx-threading through the `Client` interface would be a
   separate, much larger change; don't reach for it by default.

## Testing

- Write a test against the fixture class the feature *isn't* about.
  `newTestRemoteRoot`'s own panel is always remote; `StartClock` is
  never started in any test. A ticker-driven function that branches on
  `panel.remote` has **zero** coverage for the local branch unless a
  test calls it directly against a plain local `Root`
  (`newTestRootForConnectionMenu` or equivalent) — this is exactly how
  the nil-deref crash shipped.
- For a remote-panel test suite that needs the async hook to resolve
  synchronously (most of them — the fake `remotefs.Client` is already
  in-memory and instant, there's no reason to exercise real
  goroutine/`QueueUpdateDraw` timing in an ordinary behavioral test),
  override the hook with a synchronous stub once, in the shared test
  helper — see `syncOnFetchEntries`/`newTestRemoteRoot`
  (remoteguards_test.go). Don't call the real async entry point
  directly in a test unless the test is specifically about async
  timing/staleness — it either hangs forever (nothing drains
  `QueueUpdateDraw`) or silently no-ops.
- For a test specifically about async timing — the loading indicator,
  stale-result discard, a request superseded by a newer one — use a
  *controlled* hook that captures `done` without calling it, so the
  test can inspect in-flight state and fire the result on demand. See
  `controlledOnFetchEntries` (panelload_test.go).
- `go test -race ./internal/ui/...` after any change here — not
  optional, this package's own concurrency bugs only ever surface
  under it.

## Related

`merge-release`'s own conflict-resolution section covers the
mechanical side of landing this kind of change (the `docs/code-index.json`
+ `callgraph.svg` conflict every rebase hits). This skill is about the
code shape itself.
