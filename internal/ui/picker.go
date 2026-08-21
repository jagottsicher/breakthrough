package ui

import (
	"fmt"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// pickerKind selects whether openOwnerGroupPicker lists users or groups.
type pickerKind int

const (
	pickUser pickerKind = iota
	pickGroup
)

// pickerHeight is the owner/group picker's fixed height: the current
// entry (see openOwnerGroupPicker's centering) plus 3 rows either side,
// per the user's own spec for this — an "endless scroll" field showing
// the current value with 3 entries above and below it.
const pickerHeight = 7

// openOwnerGroupPicker shows a scrollable list of every local user (kind
// == pickUser) or group (pickGroup), sorted alphabetically (see
// fsops.ListUsers/ListGroups), with the entry matching currentID
// centered in the visible window. Confirming an entry — click or Enter,
// tview.List's own behavior — closes the picker and runs onPick with its
// name and numeric id; Escape/Tab close it and run onCancel instead, if
// given (nil is fine: most callers have nothing special to do beyond the
// close, which happens either way).
//
// If fsops.ListUsers/ListGroups itself fails — always on macOS, or any
// other reason /etc/passwd or /etc/group couldn't be read — or comes
// back empty, this runs onFallback instead of showing anything, so the
// caller can offer its own equivalent plain text prompt (see
// Root.openChown and the Properties overlay's Owner/Group fields for
// what that looks like).
func (r *Root) openOwnerGroupPicker(kind pickerKind, currentID int, onPick func(name string, id int), onCancel func(), onFallback func()) {
	type namedID struct {
		name string
		id   int
	}

	var entries []namedID
	switch kind {
	case pickUser:
		// The err check below is always non-nil on macOS, by design (see
		// users_darwin.go) — not a mistake staticcheck needs to flag on
		// that platform's build, hence the nolints: this exact check is
		// what makes the fallback work everywhere ListUsers can't
		// succeed, on macOS or otherwise (e.g. an unreadable
		// /etc/passwd).
		users, err := fsops.ListUsers() //nolint:staticcheck
		if err != nil {                 //nolint:staticcheck
			onFallback()
			return
		}
		for _, u := range users {
			entries = append(entries, namedID{u.Name, u.UID})
		}
	case pickGroup:
		groups, err := fsops.ListGroups() //nolint:staticcheck // see the pickUser case above
		if err != nil {                   //nolint:staticcheck
			onFallback()
			return
		}
		for _, g := range groups {
			entries = append(entries, namedID{g.Name, g.GID})
		}
	}
	if len(entries) == 0 {
		onFallback()
		return
	}

	r.picker.Clear()
	currentIndex := 0
	for i, e := range entries {
		name, id := e.name, e.id // captured per-iteration, not the shared loop variable
		r.picker.AddItem(fmt.Sprintf("%s (%d)", name, id), "", 0, func() {
			r.hideOverlay()
			onPick(name, id)
		})
		if id == currentID {
			currentIndex = i
		}
	}
	r.picker.SetDoneFunc(func() {
		r.hideOverlay()
		if onCancel != nil {
			onCancel()
		}
	})

	width, _ := listSize(r.picker)
	height := pickerHeight
	if len(entries) < height {
		height = len(entries)
	}
	_, _, screenWidth, screenHeight := r.GetRect() // Root fills the whole screen
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	x, y, width, height = r.clampToPanel(x, y, width, height)
	r.picker.SetRect(x, y, width, height)

	r.picker.SetCurrentItem(currentIndex)
	offset := currentIndex - 3
	if offset < 0 {
		offset = 0
	}
	r.picker.SetOffset(offset, 0)

	r.showOverlay(pickerPage, r.picker)
}
