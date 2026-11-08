package ui

import "strings"

// gitBreachFilterMatches is the whole feature's own matching rule: a
// plain, case-insensitive substring — deliberately not this app's
// own panel glob/regex filter contract, a simpler one on purpose per
// Jens's own explicit "keep it simple" (2026-11-08). An empty filter
// always matches, so a caller never has to special-case "no filter
// active" separately from "this text happens to match".
func gitBreachFilterMatches(filter, text string) bool {
	if filter == "" {
		return true
	}
	return strings.Contains(strings.ToLower(text), strings.ToLower(filter))
}

// gitBreachFilterHintLabel is the hint bar's own label for "/" —
// "filter" if the box has none active yet, "clear filter" once it
// does, matching openGitBreachFilter's own toggle behavior exactly so
// the hint bar never advertises an action the key wouldn't actually
// do.
func gitBreachFilterHintLabel(filter string) string {
	if filter != "" {
		return "clear filter"
	}
	return "filter"
}

// openGitBreachFilter is "/" on a box's own table — toggles that
// box's filter: clears it directly with no prompt if one is already
// active (the shared prompt overlay's own Enter-on-empty-text means
// Cancel, see finishPrompt, so there is no way to clear a filter by
// submitting an empty one through it), or prompts for new filter text
// if none is active yet. render re-renders that one box afterward;
// refreshGitBreachHint always runs too, since none of this moves
// keyboard focus, the usual trigger for a hint-bar refresh.
//
// Built directly on r.pushOverlay rather than the shared openPrompt
// helper — same reasoning openGitBreachCreateTag's own doc comment
// already gives: openPrompt's own showOverlay would close Git Breach
// itself first.
func (r *Root) openGitBreachFilter(label string, get func() string, set func(string), render func()) {
	if get() != "" {
		set("")
		render()
		r.refreshGitBreachHint()
		return
	}

	r.prompt.SetLabel(label + " ")
	r.prompt.SetText("")
	r.promptSubmit = func(text string) {
		set(text)
		render()
		r.refreshGitBreachHint()
	}

	const width, height = 56, 1
	_, _, screenWidth, screenHeight := r.GetRect()
	x, y, w, h := r.clampToScreen((screenWidth-width)/2, (screenHeight-height)/2, width, height)
	r.prompt.SetRect(x, y, w, h)
	r.pushOverlay(promptPage, r.prompt, nil)
}

// openGitBreachFilesFilter filters Files by path.
func (r *Root) openGitBreachFilesFilter() {
	r.openGitBreachFilter("Filter files:",
		func() string { return r.gitBreach.FilesFilter },
		func(s string) { r.gitBreach.FilesFilter = s },
		r.renderGitBreachFiles,
	)
}

// openGitBreachBranchesOrTagsFilter filters whichever of Branches/
// Tags the sub-tab toggle (see toggleGitBreachBranchesTab) currently
// shows, by name — the same "a/d mean different things depending on
// BranchesShowTags" branch those two keys already use.
func (r *Root) openGitBreachBranchesOrTagsFilter() {
	if r.gitBreach.BranchesShowTags {
		r.openGitBreachFilter("Filter tags:",
			func() string { return r.gitBreach.TagsFilter },
			func(s string) { r.gitBreach.TagsFilter = s },
			r.renderGitBreachBranches,
		)
		return
	}
	r.openGitBreachFilter("Filter branches:",
		func() string { return r.gitBreach.BranchesFilter },
		func(s string) { r.gitBreach.BranchesFilter = s },
		r.renderGitBreachBranches,
	)
}

// openGitBreachStashFilter filters Stash by ref and message together
// (the same combined text renderGitBreachStash already shows in one
// cell).
func (r *Root) openGitBreachStashFilter() {
	r.openGitBreachFilter("Filter stash:",
		func() string { return r.gitBreach.StashFilter },
		func(s string) { r.gitBreach.StashFilter = s },
		r.renderGitBreachStash,
	)
}
