package ui

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// currentUserOrSkip resolves the process's own username the same way
// fsops.Stat's Owner field would report it — the one account guaranteed
// to exist (and be findable in the real /etc/passwd this test reads,
// same as fsops' own tests) wherever this test runs.
func currentUserOrSkip(t *testing.T) (name string, uid int) {
	t.Helper()
	u, err := user.Current()
	if err != nil {
		t.Skipf("could not determine current user: %v", err)
	}
	return u.Username, os.Getuid()
}

// TestOpenOwnerGroupPickerListsRealUsers exercises the real
// fsops.ListUsers path against whatever this machine's /etc/passwd
// actually has, checking the picker opens with entries and centers on
// the given current id.
func TestOpenOwnerGroupPickerListsRealUsers(t *testing.T) {
	name, uid := currentUserOrSkip(t)

	r, err := NewRoot(tview.NewApplication(), t.TempDir())
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	var picked string
	var pickedID int
	fellBack := false
	r.openOwnerGroupPicker(pickUser, uid, r.centeredOnScreen, func(n string, id int) {
		picked, pickedID = n, id
	}, nil, func() {
		fellBack = true
	})

	if fellBack {
		t.Skip("fsops.ListUsers unavailable in this environment (e.g. macOS)")
	}
	if r.activePage != pickerPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, pickerPage)
	}
	if r.picker.GetItemCount() == 0 {
		t.Fatal("picker should have at least one item")
	}

	// The item at the current selection should be the current user,
	// since currentIndex was computed from the matching uid.
	current := r.picker.GetCurrentItem()
	mainText, _ := r.picker.GetItemText(current)
	wantPrefix := name
	if len(mainText) < len(wantPrefix) || mainText[:len(wantPrefix)] != wantPrefix {
		t.Errorf("current item = %q, want it to start with %q", mainText, wantPrefix)
	}

	// Confirming it (Enter) should report that same user back.
	r.picker.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})
	if picked != name || pickedID != uid {
		t.Errorf("onPick(%q, %d), want (%q, %d)", picked, pickedID, name, uid)
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q after picking, want closed", r.activePage)
	}
}

// TestOpenOwnerGroupPickerCancelRunsOnCancel pins Escape's behavior.
func TestOpenOwnerGroupPickerCancelRunsOnCancel(t *testing.T) {
	_, uid := currentUserOrSkip(t)

	r, err := NewRoot(tview.NewApplication(), t.TempDir())
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	cancelled := false
	fellBack := false
	r.openOwnerGroupPicker(pickUser, uid, r.centeredOnScreen, func(string, int) {
		t.Error("onPick should not run on cancel")
	}, func() {
		cancelled = true
	}, func() {
		fellBack = true
	})
	if fellBack {
		t.Skip("fsops.ListUsers unavailable in this environment (e.g. macOS)")
	}

	r.picker.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(tview.Primitive) {})

	if !cancelled {
		t.Error("onCancel should have run")
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q after cancelling, want closed", r.activePage)
	}
}

// TestOwnerGroupPickerPositionedNearField pins the fix for the user's
// own report: the picker used to always appear dead center regardless
// of context — opened from Properties' Owner/Group fields, it now
// starts exactly where the clicked span itself was drawn, the same
// position activateInlineTextField uses for every other field, instead
// of the middle of the screen.
func TestOwnerGroupPickerPositionedNearField(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()

	screen := drawProperties(t, r)
	defer screen.Fini()
	// clampToPanel keeps the picker within r.panel's own rect — give it
	// a realistic (terminal-sized) one here; Box's own construction
	// default (15x10, a placeholder never meant to be drawn against) is
	// otherwise narrow enough to force the requested position back to
	// the panel's left edge, which would make this test fail for a
	// reason that has nothing to do with what it's actually pinning.
	r.panel.SetRect(0, 0, 80, 24)

	span, ok := findPropertySpan(r, fieldOwner)
	if !ok {
		t.Fatal("no fieldOwner span found")
	}
	rectX, rectY, _, _ := r.propertiesText.GetInnerRect()
	wantX, wantY := rectX+span.startCol, rectY+span.row

	r.activatePropertyField(span)
	if r.activePage != pickerPage {
		t.Skip("fsops.ListUsers unavailable in this environment (e.g. macOS): falls back to the inline text field instead")
	}

	gotX, gotY, _, _ := r.picker.GetRect()
	if gotX != wantX || gotY != wantY {
		t.Errorf("picker positioned at (%d,%d), want (%d,%d) — anchored to the Owner field, not centered on screen", gotX, gotY, wantX, wantY)
	}
}

// TestActivatePropertyFieldOwnerOpensPicker pins that clicking the Owner
// field routes to the picker (when available) rather than the inline
// text field Name/Date/Time use.
func TestActivatePropertyFieldOwnerOpensPicker(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()

	span, ok := findPropertySpan(r, fieldOwner)
	if !ok {
		t.Fatal("no fieldOwner span found")
	}
	r.activatePropertyField(span)

	if r.activePage != pickerPage {
		if r.activePage == propertiesPage {
			t.Skip("fsops.ListUsers unavailable in this environment (e.g. macOS): falls back to the inline text field instead")
		}
		t.Fatalf("activePage = %q, want %q", r.activePage, pickerPage)
	}
	if !r.propertiesDirty {
		t.Error("clicking Owner should mark Properties dirty immediately")
	}
}

// TestPropertiesOwnerPickResumesAndStages exercises the full round trip:
// click Owner, confirm the (current, safe) user in the picker, land back
// in Properties with stagedOwner set and the overlay showing again.
func TestPropertiesOwnerPickResumesAndStages(t *testing.T) {
	name, _ := currentUserOrSkip(t)

	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()

	span, _ := findPropertySpan(r, fieldOwner)
	r.activatePropertyField(span)
	if r.activePage != pickerPage {
		t.Skip("fsops.ListUsers unavailable in this environment (e.g. macOS)")
	}

	r.picker.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.activePage != propertiesPage {
		t.Errorf("activePage = %q after picking, want Properties to resume", r.activePage)
	}
	if r.stagedOwner != name {
		t.Errorf("stagedOwner = %q, want %q", r.stagedOwner, name)
	}
}

// TestOwnerGroupPickerLayersOverPropertiesWithoutClosingIt pins the
// user's own request ("ich möchte, dass der [Picker] praktisch als ein
// weiterer Layer darüberliegt"): opening the picker from Properties must
// not close Properties underneath it — both should be simultaneously
// open (see pushOverlay), the picker layered on top rather than
// replacing it, and cancelling the picker should leave Properties open,
// not close everything.
func TestOwnerGroupPickerLayersOverPropertiesWithoutClosingIt(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()

	span, ok := findPropertySpan(r, fieldOwner)
	if !ok {
		t.Fatal("no fieldOwner span found")
	}
	r.activatePropertyField(span)

	if r.activePage != pickerPage {
		t.Skip("fsops.ListUsers unavailable in this environment (e.g. macOS): falls back to the inline text field instead")
	}

	visible := r.GetPageNames(true)
	var hasProperties, hasPicker bool
	for _, p := range visible {
		hasProperties = hasProperties || p == propertiesPage
		hasPicker = hasPicker || p == pickerPage
	}
	if !hasProperties {
		t.Errorf("Properties should still be visible underneath the picker, visible pages: %v", visible)
	}
	if !hasPicker {
		t.Errorf("the picker should be visible, visible pages: %v", visible)
	}

	r.picker.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(tview.Primitive) {})
	if r.activePage != propertiesPage {
		t.Errorf("activePage = %q after cancelling the picker, want Properties to still be open", r.activePage)
	}
}

// TestOpenChownClosesContextMenuBehindThePicker is
// TestOwnerGroupPickerLayersOverPropertiesWithoutClosingIt's opposite
// case: the picker opened by the standalone "chown" menu action closes
// the context menu first (see Root.openChown), rather than leaving it
// open underneath — unlike Properties' Owner/Group fields, there's
// nothing worth keeping visible behind it here.
func TestOpenChownClosesContextMenuBehindThePicker(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(dir, "apple.txt")
	r.showMenu(0, 0) // open the context menu the way a real right-click would

	r.openChown()
	if r.activePage != pickerPage {
		t.Skip("fsops.ListUsers unavailable in this environment (e.g. macOS): falls back to the text prompt instead")
	}

	for _, p := range r.GetPageNames(true) {
		if p == contextMenuPage {
			t.Errorf("context menu should be closed once the picker opens, visible pages: %v", r.GetPageNames(true))
		}
	}
}
