package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/gitstatus"
)

// Git status: a compact, zsh-prompt-style summary of the current
// directory's own git state — branch, ahead/behind its upstream, and
// staged/unstaged/untracked/conflicted file counts — shown in two
// places that share the exact same rendered text (gitStatusText
// below), per the user's own framing of this as one feature with two
// displays, not two independent ones:
//
//   - The status bar (see bottombar.go's buildStatusBar): one segment,
//     for whichever directory the active panel is currently showing.
//   - The Details sidebar (see detailssidebar.go): its own section,
//     shown only while a *directory* is selected there and only once
//     it's confirmed to actually be part of a git working tree — per
//     the user's own explicit request ("wenn man einen Ordner anwählt
//     ... und da ist was git-mäßiges drin").
//
// Both read from internal/gitstatus, a thin wrapper around the
// system's own git(1) (see its own package doc for why that's not a
// reimplementation) — this file only ever turns a gitstatus.Status
// into colored text and decides when to go fetch one, the same
// gitstatus-vs-ui split every other feature in this app keeps.
//
// One shared setting (ShowGitStatus, Options → Status bar → "Git
// status") turns both displays off together — the user's own explicit
// framing treats this as a single on/off feature, not two.

// gitStatusColor picks the whole segment's own color from st's overall
// severity — conflicts (theme.CriticalText, red) outrank being merely
// dirty (theme.WarningText, orange), which outranks a clean working
// tree (theme.EntryExecutable, this app's own established "healthy"
// green) — the same three-color language percentStatusColor already
// speaks for a percentage, applied here to a repository's own state
// instead of a number crossing a threshold.
func gitStatusColor(st gitstatus.Status, theme config.ResolvedTheme) tcell.Color {
	switch {
	case st.Conflicts > 0:
		return theme.CriticalText
	case st.Dirty():
		return theme.WarningText
	default:
		return theme.EntryExecutable
	}
}

// gitStatusText renders st the way a zsh git prompt typically does —
// "git:(branch)" (the exact phrasing several popular themes, including
// oh-my-zsh's own default "robbyrussell", already use, so it reads as
// familiar rather than inventing new notation) followed by whichever
// of ahead/behind/staged/unstaged/untracked/conflicts are actually
// nonzero, each dropped entirely rather than shown as a "+0" that
// would just be noise. The whole line is one color throughout (see
// gitStatusColor) — no per-figure coloring the way, say, the load
// average's own three numbers each get their own: a repository's
// overall state is one fact, not several independent ones.
func gitStatusText(st gitstatus.Status, theme config.ResolvedTheme) string {
	var b strings.Builder
	b.WriteString("git:(")
	b.WriteString(st.Branch)
	b.WriteString(")")
	if st.Ahead > 0 {
		fmt.Fprintf(&b, " ⇡%d", st.Ahead)
	}
	if st.Behind > 0 {
		fmt.Fprintf(&b, " ⇣%d", st.Behind)
	}
	if st.Staged > 0 {
		fmt.Fprintf(&b, " +%d", st.Staged)
	}
	if st.Unstaged > 0 {
		fmt.Fprintf(&b, " !%d", st.Unstaged)
	}
	if st.Untracked > 0 {
		fmt.Fprintf(&b, " ?%d", st.Untracked)
	}
	if st.Conflicts > 0 {
		fmt.Fprintf(&b, " =%d", st.Conflicts)
	}
	return wrapColor(gitStatusColor(st, theme), b.String())
}

// statusBarGitStatusTimeout bounds how long the status bar's own
// once-a-second refresh (see StartClock) will wait on git itself
// before giving up and simply showing one less segment this tick — a
// huge untracked-file count or a repository on a slow network
// filesystem shouldn't be able to stall the whole status line, which
// several other segments (the clock included) share.
const statusBarGitStatusTimeout = 500 * time.Millisecond

// gitStatusForStatusBar fetches and renders dir's own git status for
// the status bar segment — "", false for anything not worth a segment
// at all: git itself missing, dir not part of a working tree, or the
// fetch failing/timing out.
func gitStatusForStatusBar(theme config.ResolvedTheme, dir string) (string, bool) {
	if dir == "" || !gitstatus.Available() {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), statusBarGitStatusTimeout)
	defer cancel()
	st, inRepo, err := gitstatus.Fetch(ctx, dir)
	if err != nil || !inRepo {
		return "", false
	}
	return gitStatusText(st, theme), true
}

// detailsGitStatusDebounce/detailsGitStatusTimeout mirror
// detailsPreviewDebounce's own reasoning (see its doc comment):
// holding an arrow key through a directory of git repositories should
// cost nothing, so a fetch is only even attempted once the cursor has
// rested briefly, and cancelled outright the instant it moves on
// again. The timeout is far more generous than the status bar's own
// (2s vs. 500ms): this one isn't competing with a once-a-second clock
// tick for how often it gets to run.
const (
	detailsGitStatusDebounce = 120 * time.Millisecond
	detailsGitStatusTimeout  = 2 * time.Second
)

// cancelDetailsGitStatus stops the git status fetch for whatever
// target the cursor has moved off — mirrors cancelDetailsPreview.
func (r *Root) cancelDetailsGitStatus() {
	if r.detailsGitCancel != nil {
		r.detailsGitCancel()
		r.detailsGitCancel = nil
	}
}

// startDetailsGitStatus fetches path's own git status in the
// background and shows it once ready, if the cursor is still there —
// same shape as startDetailsPreview (debounce, cancellable context,
// safeGo, QueueUpdateDraw, a final check against detailsTarget itself
// since the cursor can have moved again by the time this runs), for
// the identical reason: this is triggered by mere cursor movement, not
// a deliberate keypress, and git itself can occasionally be slow (a
// huge untracked count, a cold filesystem cache).
//
// Only ever attempted for a directory (see isDirish) — per the user's
// own explicit request ("wenn man einen Ordner anwählt ... und da ist
// was git-mäßiges drin"), not for a plain file selected inside a git
// repository.
func (r *Root) startDetailsGitStatus(path string) {
	if !r.settings.ShowGitStatus || path == "" || r.detailsStatErr != nil || !isDirish(r.detailsStat) || !gitstatus.Available() {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.detailsGitCancel = cancel

	r.safeGo("Details git status", func() { r.cancelDetailsGitStatus() }, func() {
		select {
		case <-ctx.Done():
			return // the cursor moved on before this even started
		case <-time.After(detailsGitStatusDebounce):
		}

		fetchCtx, fetchCancel := context.WithTimeout(ctx, detailsGitStatusTimeout)
		st, inRepo, err := gitstatus.Fetch(fetchCtx, path)
		fetchCancel()
		if ctx.Err() != nil {
			return
		}

		r.app.QueueUpdateDraw(func() {
			// Checked again on the UI goroutine, against the target
			// itself rather than only the context — see
			// startDetailsPreview's own identical check for why.
			if ctx.Err() != nil || r.detailsTarget != path {
				return
			}
			if err == nil && inRepo {
				r.detailsGitStatus = &st
				r.renderDetailsSidebar()
			}
		})
	})
}
