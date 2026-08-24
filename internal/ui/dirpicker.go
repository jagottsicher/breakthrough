package ui

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/rivo/tview"
)

const dirPickerPage = "dir-picker"

// dirPickerSize is the picker's fixed width/height — tall enough to
// browse comfortably, but still an overlay rather than a full-screen
// replacement (see clampToPanel, which still shrinks it on a small
// terminal).
const dirPickerWidth, dirPickerHeight = 60, 20

// newDirPicker builds the directory-picker overlay: a header line
// showing the path currently being browsed, a scrollable list of its
// subdirectories (see loadDirPicker), and a Select/Cancel button row —
// its own navigation state, entirely separate from the main panel (see
// dirPickerPath), the same "eigener Navigationszustand" CLAUDE.md's own
// Copy-to/Move-to design note calls for. Built once and reused by every
// caller (openDirPicker resets its content each time), the same
// "one shared, repopulated widget" shape r.picker (owner/group) and
// r.optionsList already use.
func (r *Root) newDirPicker() *tview.Flex {
	r.dirPickerHeader = tview.NewTextView().SetDynamicColors(true)
	r.dirPickerHeader.SetBorderPadding(0, 0, 1, 1)

	r.dirPickerList = tview.NewList().ShowSecondaryText(false)
	r.dirPickerList.SetHighlightFullLine(true)
	r.dirPickerList.SetBorderPadding(0, 0, 1, 1)
	r.dirPickerList.SetDoneFunc(r.cancelDirPicker)

	r.dirPickerSelectBtn = tview.NewButton("Select").SetSelectedFunc(r.confirmDirPicker)
	r.dirPickerCancelBtn = tview.NewButton("Cancel").SetSelectedFunc(r.cancelDirPicker)

	buttons := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(nil, 0, 1, false).
		AddItem(r.dirPickerSelectBtn, 10, 0, false).
		AddItem(nil, 2, 0, false).
		AddItem(r.dirPickerCancelBtn, 10, 0, false).
		AddItem(nil, 0, 1, false)

	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.dirPickerHeader, 1, 0, false).
		AddItem(r.dirPickerList, 0, 1, true).
		AddItem(buttons, 1, 0, false)
	flex.SetBorder(true).SetTitle(" Browse ")
	return flex
}

// openDirPicker shows the directory browser seeded at start (falling
// back to the current working directory if start doesn't resolve to a
// readable directory — see loadDirPicker), on top of whatever overlay
// is already open (pushOverlay), the same "floats on top rather than
// replacing" behavior openOwnerGroupPicker already has. onSelect runs
// with whatever directory is currently being browsed once the user
// confirms it (the Select button); onCancel (nil is fine) runs instead
// on Escape or the Cancel button. Neither runs the overlay closed
// itself — both confirmDirPicker/cancelDirPicker already do that first.
func (r *Root) openDirPicker(start string, onSelect func(string), onCancel func()) {
	r.dirPickerOnSelect = onSelect
	r.dirPickerOnCancel = onCancel
	r.loadDirPicker(start)

	x, y := r.centeredOnScreen(dirPickerWidth, dirPickerHeight)
	x, y, w, h := r.clampToPanel(x, y, dirPickerWidth, dirPickerHeight)
	r.dirPicker.SetRect(x, y, w, h)

	r.pushOverlay(dirPickerPage, r.dirPicker, nil)
}

// loadDirPicker points the picker at path, repopulating its list with
// path's own subdirectories (sorted, hidden ones included — there's no
// separate toggle for this simple a picker) plus a leading ".." entry
// to go back up, omitted only once path has no parent left (the
// filesystem root). A path that can't be resolved to an absolute one,
// or read at all (permission denied, since deleted, ...), still shows
// with an empty subdirectory list rather than erroring out — Select
// still works for it, since picking a directory you can browse *into*
// isn't the same requirement as being allowed to browse it.
func (r *Root) loadDirPicker(path string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	r.dirPickerPath = abs
	r.dirPickerHeader.SetText(abs)
	r.dirPickerList.Clear()

	if parent := filepath.Dir(abs); parent != abs {
		r.dirPickerList.AddItem("../", "", 0, func() {
			r.loadDirPicker(parent)
		})
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		full := filepath.Join(abs, name)
		r.dirPickerList.AddItem(name+"/", "", 0, func() {
			r.loadDirPicker(full)
		})
	}
}

// confirmDirPicker is the Select button's own action: closes the
// picker and hands whatever directory is currently being browsed
// (dirPickerPath) to the caller's onSelect.
func (r *Root) confirmDirPicker() {
	r.hideOverlay()
	if r.dirPickerOnSelect != nil {
		r.dirPickerOnSelect(r.dirPickerPath)
	}
}

// cancelDirPicker is Escape/the Cancel button's own action: closes the
// picker without ever calling onSelect.
func (r *Root) cancelDirPicker() {
	r.hideOverlay()
	if r.dirPickerOnCancel != nil {
		r.dirPickerOnCancel()
	}
}
