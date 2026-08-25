package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestOpenDirPickerListsSubdirectoriesOnly pins loadDirPicker's own
// filtering: only subdirectories appear (see fixtureDir's own mix of
// files and exactly one directory, "app-data"), plus a leading ".."
// entry to go back up.
func TestOpenDirPickerListsSubdirectoriesOnly(t *testing.T) {
	dir := fixtureDir(t)

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openDirPicker(dir, nil, nil)

	if r.dirPickerPath != dir {
		t.Errorf("dirPickerPath = %q, want %q", r.dirPickerPath, dir)
	}
	if got := r.dirPickerHeader.GetText(true); got != dir {
		t.Errorf("dirPickerHeader text = %q, want %q", got, dir)
	}

	// ".." plus exactly one subdirectory ("app-data") — see fixtureDir.
	if got, want := r.dirPickerList.GetItemCount(), 2; got != want {
		t.Fatalf("dirPickerList has %d items, want %d", got, want)
	}
	first, _ := r.dirPickerList.GetItemText(0)
	if first != "../" {
		t.Errorf("first item = %q, want %q", first, "../")
	}
	second, _ := r.dirPickerList.GetItemText(1)
	if second != "app-data/" {
		t.Errorf("second item = %q, want %q", second, "app-data/")
	}
}

// TestDirPickerNavigatesIntoSubdirectory pins that picking a
// subdirectory entry navigates into it (updates dirPickerPath and
// repopulates the list) rather than confirming it — matching the
// user's own request that the picker can keep descending further.
func TestDirPickerNavigatesIntoSubdirectory(t *testing.T) {
	dir := fixtureDir(t)
	nested := filepath.Join(dir, "app-data", "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openDirPicker(dir, nil, nil)
	// Index 1 is "app-data/" (index 0 is "..") — see
	// TestOpenDirPickerListsSubdirectoriesOnly's own pin of that order.
	// SetCurrentItem alone doesn't run the item's own selected func —
	// only Enter/click does, hence the InputHandler call after it.
	r.dirPickerList.SetCurrentItem(1)
	r.dirPickerList.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	appData := filepath.Join(dir, "app-data")
	if r.dirPickerPath != appData {
		t.Fatalf("dirPickerPath = %q, want %q", r.dirPickerPath, appData)
	}
	// ".." plus "nested/" — the subdirectory created above.
	if got, want := r.dirPickerList.GetItemCount(), 2; got != want {
		t.Fatalf("dirPickerList has %d items after descending, want %d", got, want)
	}
	second, _ := r.dirPickerList.GetItemText(1)
	if second != "nested/" {
		t.Errorf("second item after descending = %q, want %q", second, "nested/")
	}
}

// TestDirPickerDotDotGoesUp pins that the leading ".." entry navigates
// to the parent directory.
func TestDirPickerDotDotGoesUp(t *testing.T) {
	dir := fixtureDir(t)
	appData := filepath.Join(dir, "app-data")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openDirPicker(appData, nil, nil)
	if r.dirPickerPath != appData {
		t.Fatalf("setup: dirPickerPath = %q, want %q", r.dirPickerPath, appData)
	}

	r.dirPickerList.SetCurrentItem(0) // ".."
	r.dirPickerList.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.dirPickerPath != dir {
		t.Errorf("dirPickerPath after '..' = %q, want %q", r.dirPickerPath, dir)
	}
}

// TestConfirmDirPickerRunsOnSelectWithCurrentPath pins the Select
// button's own action: closes the overlay and hands back whatever
// directory is currently being browsed.
func TestConfirmDirPickerRunsOnSelectWithCurrentPath(t *testing.T) {
	dir := fixtureDir(t)

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	var got string
	called := false
	r.openDirPicker(dir, func(path string) {
		called = true
		got = path
	}, func() {
		t.Error("onCancel should not run when Select is pressed")
	})

	r.confirmDirPicker()

	if !called {
		t.Fatal("onSelect was not called")
	}
	if got != dir {
		t.Errorf("onSelect got %q, want %q", got, dir)
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q after Select, want closed", r.activePage)
	}
}

// TestCancelDirPickerRunsOnCancelWithoutSelecting pins the Cancel
// button's (and Escape's, via the list's own SetDoneFunc) action:
// closes the overlay and runs onCancel, never onSelect.
func TestCancelDirPickerRunsOnCancelWithoutSelecting(t *testing.T) {
	dir := fixtureDir(t)

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	cancelled := false
	r.openDirPicker(dir, func(string) {
		t.Error("onSelect should not run when Cancel is pressed")
	}, func() {
		cancelled = true
	})

	r.cancelDirPicker()

	if !cancelled {
		t.Fatal("onCancel was not called")
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q after Cancel, want closed", r.activePage)
	}
}

// TestOpenDirPickerHandlesUnreadableStart pins loadDirPicker's own
// graceful fallback: a start path that can't be read (here, a file,
// not a directory) still shows — with an empty subdirectory list —
// rather than erroring out, since Select should still work for it.
func TestOpenDirPickerHandlesUnreadableStart(t *testing.T) {
	dir := fixtureDir(t)
	file := filepath.Join(dir, "apple.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openDirPicker(file, nil, nil)

	if r.dirPickerPath != file {
		t.Errorf("dirPickerPath = %q, want %q", r.dirPickerPath, file)
	}
	// Just ".." — os.ReadDir(file) fails, so no subdirectories are added.
	if got, want := r.dirPickerList.GetItemCount(), 1; got != want {
		t.Errorf("dirPickerList has %d items, want %d (just '..')", got, want)
	}
}
