package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/consigliere"
)

func openConsigliereAPIKeyFileRow(t *testing.T, r *Root) {
	t.Helper()
	selectOptionCategory(t, r, "AI Consigliere")
	row, ok := optionRowByKey(r, "consigliere_anthropic_api_key_file")
	if !ok {
		t.Fatal("no consigliere_anthropic_api_key_file row")
	}
	r.activateOptionRow(row)
	if r.activePage != optionsInputPage {
		t.Fatalf("activePage = %q, want the input dialog %q", r.activePage, optionsInputPage)
	}
}

// TestOptionsTabStillClosesOnOrdinaryStringRows is the regression
// guard the pathCompletion addition specifically has to not break:
// every other KindString/KindInt row's own edit dialog keeps Tab's
// existing meaning there (cancel without applying, same as Escape).
func TestOptionsTabStillClosesOnOrdinaryStringRows(t *testing.T) {
	r, _ := newOptionsRoot(t)
	selectOptionCategory(t, r, "Trash")
	row, ok := optionRowByKey(r, "trash_max_age_days")
	if !ok {
		t.Fatal("no trash_max_age_days row")
	}
	original := r.settings.TrashMaxAgeDays

	r.activateOptionRow(row)
	r.optionsInput.SetText("999")
	r.optionsInput.InputHandler()(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.activePage != optionsPage {
		t.Errorf("activePage = %q after Tab, want back on the Options screen", r.activePage)
	}
	if r.settings.TrashMaxAgeDays != original {
		t.Errorf("TrashMaxAgeDays = %d after Tab, want the original %d (unapplied)", r.settings.TrashMaxAgeDays, original)
	}
}

// TestOptionsTabOnKeyFileRowCompletesInstead pins the opt-in half: the
// key-file row's own Tab tries path completion and stays open, rather
// than closing.
func TestOptionsTabOnKeyFileRowCompletesInstead(t *testing.T) {
	r, _ := newOptionsRoot(t)
	openConsigliereAPIKeyFileRow(t, r)

	r.optionsInput.SetText(r.panel.path + "/apr") // "apricot.txt" is fixtureDir's own only "apr"-prefixed entry
	r.optionsInput.InputHandler()(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.activePage != optionsInputPage {
		t.Errorf("activePage = %q after Tab, want to stay on the input dialog %q", r.activePage, optionsInputPage)
	}
	if want, got := filepath.Join(r.panel.path, "apricot.txt"), r.optionsInput.GetText(); got != want {
		t.Errorf("input text = %q after Tab, want the completed %q", got, want)
	}
}

// TestOptionsKeyFileRejectsADirectory pins the validation: a path that
// resolves to a directory, not a regular file, is rejected and the
// dialog stays open rather than silently closing over a bad value.
func TestOptionsKeyFileRejectsADirectory(t *testing.T) {
	r, _ := newOptionsRoot(t)
	openConsigliereAPIKeyFileRow(t, r)

	r.optionsInput.SetText(r.panel.path) // a real directory, not a file
	r.optionsInput.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	// showTransientErrorOverOverlay layers the rejection on top (see
	// validateOptionsInputPath's own doc comment) — activePage is now
	// the error, but the input dialog underneath must still be open,
	// not closed over.
	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want the rejection's own error layered on top", r.activePage)
	}
	assertPageVisible(t, r, optionsInputPage)
	if r.settings.ConsigliereAnthropicAPIKeyFile != "" {
		t.Errorf("ConsigliereAPIKeyFile = %q, want the rejected value never applied", r.settings.ConsigliereAnthropicAPIKeyFile)
	}
}

// assertPageVisible fails the test unless page is among the currently
// visible overlay pages (see tview.Pages.GetPageNames(true)) — used
// to confirm a layered error didn't close whatever it's layered over.
func assertPageVisible(t *testing.T, r *Root, page string) {
	t.Helper()
	for _, p := range r.GetPageNames(true) {
		if p == page {
			return
		}
	}
	t.Errorf("%q is not among the visible pages: %v", page, r.GetPageNames(true))
}

// TestOptionsKeyFileRejectsAMissingFile is validateOptionsInputPath's
// other failure path — directly catches the real, reported mistake
// this whole feature exists for: pasting the API key itself in here
// resolves to a path that simply doesn't exist.
func TestOptionsKeyFileRejectsAMissingFile(t *testing.T) {
	r, _ := newOptionsRoot(t)
	openConsigliereAPIKeyFileRow(t, r)

	r.optionsInput.SetText("sk-ant-definitely-not-a-real-path-12345")
	r.optionsInput.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want the rejection's own error layered on top", r.activePage)
	}
	assertPageVisible(t, r, optionsInputPage)
}

// TestOptionsKeyFileAcceptsARealFileAndResolvesToAbsolute pins the
// success path, including the resolve-then-persist decision: a
// relative value is stored as the resolved absolute path, not the raw
// typed text.
func TestOptionsKeyFileAcceptsARealFileAndResolvesToAbsolute(t *testing.T) {
	r, _ := newOptionsRoot(t)
	openConsigliereAPIKeyFileRow(t, r)

	r.optionsInput.SetText("apple.txt") // relative to r.panel.path, see fixtureDir
	r.optionsInput.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.activePage != optionsPage {
		t.Fatalf("activePage = %q, want back on the Options screen after a valid commit", r.activePage)
	}
	want := filepath.Join(r.panel.path, "apple.txt")
	if r.settings.ConsigliereAnthropicAPIKeyFile != want {
		t.Errorf("ConsigliereAPIKeyFile = %q, want the resolved absolute path %q", r.settings.ConsigliereAnthropicAPIKeyFile, want)
	}
}

// TestOptionsKeyFileCommitRetriesTheModelFetch pins the real,
// reported bug: Model/Effort's own choices stayed stuck on just
// "(default: ...)" after setting a key file while already in this
// category, because nothing retried refreshConsigliereModelCache
// until the category was left and re-entered.
func TestOptionsKeyFileCommitRetriesTheModelFetch(t *testing.T) {
	r, _ := newOptionsRoot(t)
	openConsigliereAPIKeyFileRow(t, r)
	// No key configured yet — the initial fetch (triggered by
	// selectOptionCategory) already failed and recorded
	// errConsigliereNoAPIKey.
	if _, ok := r.consigliereModelCache["anthropic"]; ok {
		t.Fatal("setup: should not be cached yet")
	}
	called := make(chan struct{}, 1)
	withFakeConsigliereModelList(t, func(ctx context.Context, providerName, key, ollamaBaseURLOverride string) ([]consigliere.ModelInfo, error) {
		called <- struct{}{}
		return []consigliere.ModelInfo{{ID: "claude-opus-5-5"}}, nil
	})

	keyFile := filepath.Join(t.TempDir(), "key.txt")
	if err := os.WriteFile(keyFile, []byte("sk-ant-real-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.optionsInput.SetText(keyFile)
	r.optionsInput.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("committing the key file should have retried the model fetch")
	}
}

// TestOptionsKeyFileBlankCommitUnsetsIt pins the other explicit
// requirement: "" is always valid and actually clears a previously
// set value, unlike every other KindString row's own "blank commit is
// a no-op" default (see editOptionValue's own doc comment).
func TestOptionsKeyFileBlankCommitUnsetsIt(t *testing.T) {
	r, _ := newOptionsRoot(t)
	r.settings.ConsigliereAnthropicAPIKeyFile = filepath.Join(r.panel.path, "apple.txt")
	openConsigliereAPIKeyFileRow(t, r)

	r.optionsInput.SetText("")
	r.optionsInput.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.settings.ConsigliereAnthropicAPIKeyFile != "" {
		t.Errorf("ConsigliereAPIKeyFile = %q, want a blank commit to unset it", r.settings.ConsigliereAnthropicAPIKeyFile)
	}
}

func TestConsigliereFilePermissionWarningFlagsLooserThan0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	got := consigliereFilePermissionWarning(path)

	if !strings.Contains(got, "0644") {
		t.Errorf("warning = %q, want it to mention the actual mode 0644", got)
	}
}

func TestConsigliereFilePermissionWarningSilentFor0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key.txt")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	if got := consigliereFilePermissionWarning(path); got != "" {
		t.Errorf("warning = %q, want no warning for an already-0600 file", got)
	}
}

func TestConsigliereAPIKeyStatusIncludesPermissionWarningForFileTier(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key.txt")
	if err := os.WriteFile(path, []byte("sk-ant-test"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}
	r := newPlainKeyRoot(t)
	r.settings.ConsigliereAnthropicAPIKeyFile = path

	got := r.consigliereAPIKeyStatus()

	if !strings.Contains(got, "set via file") || !strings.Contains(got, "0644") {
		t.Errorf("status = %q, want it to mention both the file tier and the loose permissions", got)
	}
}
