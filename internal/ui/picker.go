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

// pickerCenterRows is how many entries show above (and below) the
// current one in the picker's visible window — the user's own spec for
// this, an "endless scroll" field showing the current value with 3
// entries either side.
const pickerCenterRows = 3

// pickerHeight is the owner/group picker's fixed height: the current
// entry (see openOwnerGroupPicker's centering) plus pickerCenterRows
// rows either side.
const pickerHeight = 2*pickerCenterRows + 1

// pickerPosition resolves where a picker of the given width/height
// should appear — computed after the picker's own content is known
// (see openOwnerGroupPicker), since a caller anchoring it to a specific
// screen position rather than centering it doesn't get to pick its size.
// clampToPanel still runs on whatever this returns, so a position near
// the panel's edge is pulled back on-screen rather than clipped.
type pickerPosition func(width, height int) (x, y int)

// centeredOnScreen is the picker-position Root.openChown uses: dead
// center, the same as it always has been — there's no particular screen
// element it's opened "from" the way Properties' Owner/Group fields have
// their own row/column (see propertyFieldPosition).
func (r *Root) centeredOnScreen(width, height int) (x, y int) {
	_, _, screenWidth, screenHeight := r.GetRect() // Root fills the whole screen
	return (screenWidth - width) / 2, (screenHeight - height) / 2
}

// propertyFieldPosition is the picker-position Properties' Owner/Group
// fields use (see activatePropertyField): horizontally, the picker
// starts exactly where span itself is drawn, the same column
// activateInlineTextField positions the shared text editor at for every
// other field. Vertically, it's shifted up by pickerCenterRows, so the
// *currently selected entry* — pickerCenterRows rows into the visible
// window, not the window's own top edge — is what ends up level with
// span's row: the picker then reads as centered on the field it grew
// out of, the current value sitting right where "Owner: <name>" itself
// was, rather than appearing pickerCenterRows rows below it.
func (r *Root) propertyFieldPosition(span propertySpan) pickerPosition {
	return func(int, int) (x, y int) {
		rectX, rectY, _, _ := r.propertiesText.GetInnerRect()
		return rectX + span.startCol, rectY + span.row - pickerCenterRows
	}
}

// openOwnerGroupPicker shows a scrollable list of every local user (kind
// == pickUser) or group (pickGroup), sorted alphabetically (see
// fsops.ListUsers/ListGroups), with the entry matching currentID
// centered in the visible window, positioned per pos (see
// centeredOnScreen/propertyFieldPosition). Confirming an entry — click
// or Enter, tview.List's own behavior — closes the picker and runs
// onPick with its name and numeric id; Escape/Tab close it and run
// onCancel instead, if given (nil is fine: most callers have nothing
// special to do beyond the close, which happens either way).
//
// The picker always layers on top of whatever's currently open (see
// pushOverlay) rather than replacing it — Properties' Owner/Group fields
// (see activatePropertyField) rely on this to stay visible underneath
// the picker instead of disappearing while it's up, per the user's own
// request. Root.openChown, the other caller, closes the context menu
// itself first so the picker still ends up as the only thing shown there.
//
// If fsops.ListUsers/ListGroups itself fails — always on macOS, or any
// other reason /etc/passwd or /etc/group couldn't be read — or comes
// back empty, this runs onFallback instead of showing anything, so the
// caller can offer its own equivalent plain text prompt (see
// Root.openChown and the Properties overlay's Owner/Group fields for
// what that looks like).
func (r *Root) openOwnerGroupPicker(kind pickerKind, currentID int, pos pickerPosition, onPick func(name string, id int), onCancel func(), onFallback func()) {
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
	x, y := pos(width, height)
	x, y, width, height = r.clampToPanel(x, y, width, height)
	r.picker.SetRect(x, y, width, height)

	r.picker.SetCurrentItem(currentIndex)
	offset := currentIndex - pickerCenterRows
	if offset < 0 {
		offset = 0
	}
	r.picker.SetOffset(offset, 0)

	r.pushOverlay(pickerPage, r.picker, nil)
}
