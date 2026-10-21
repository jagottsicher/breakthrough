// Package easteregg holds the pure, UI-free frame state for
// breakthrough's hidden "follow the white rabbit" animation — see
// feature_ideas.txt's own "Easter Egg" section for the full,
// phased plan this package is the foundation of.
//
// Everything here is a deterministic function of its own explicit
// inputs (frame number, seed, target text, ...), never of time.Now
// or math/rand's global source — the same contract internal/git's
// own pure helpers (DiffPage, AuthorColorIndex) already follow, for
// the identical reason: it's what makes an animation testable at all
// per CLAUDE.md ("neue Fachlogik immer mit _test.go"). internal/ui
// owns everything this package doesn't: the tview.Primitive, the
// ticker driving which frame to ask for next, and QueueUpdateDraw.
package easteregg
