package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
)

// TestOpenCurrentEntryWithPromptsForCommand pins that "Open with…"
// asks for a command through the existing generic prompt overlay
// rather than running against a hardcoded program — the same shape
// TestOpenPingTestWindowPromptsForHost (toolbox.go's own predecessor)
// already established for a different feature.
func TestOpenCurrentEntryWithPromptsForCommand(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.focusRow(1) // off ".." onto a real entry

	r.openCurrentEntryWith()

	if r.activePage != promptPage {
		t.Fatalf("activePage = %q, want the prompt overlay", r.activePage)
	}
	if got := r.prompt.GetLabel(); got != "Open with: " {
		t.Errorf("prompt label = %q, want %q", got, "Open with: ")
	}
}

// TestOpenCurrentEntryWithPrefillsTheLastUsedCommand pins the one
// convenience this dialog adds over a bare prompt: typing "gimp" once
// and opening it again later starts from "gimp" rather than empty.
func TestOpenCurrentEntryWithPrefillsTheLastUsedCommand(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.focusRow(1)
	r.lastOpenWithCommand = "gimp"

	r.openCurrentEntryWith()

	if got := r.prompt.GetText(); got != "gimp" {
		t.Errorf("prompt text = %q, want the remembered command %q", got, "gimp")
	}
}

// TestOpenCurrentEntryWithRemembersTheTypedCommandForNextTime pins that
// submitting a command updates lastOpenWithCommand for the next open —
// app.Suspend is a no-op in this test environment (no real screen
// behind r.app — see runCommandOnFile's own doc comment on why this
// codebase can't unit-test the actual subprocess invocation), so this
// only pins the remembered-command bookkeeping, not that the program
// actually ran.
func TestOpenCurrentEntryWithRemembersTheTypedCommandForNextTime(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.focusRow(1)

	r.openCurrentEntryWith()
	r.prompt.SetText("vim")
	r.finishPrompt(tcell.KeyEnter)

	if r.lastOpenWithCommand != "vim" {
		t.Errorf("lastOpenWithCommand = %q, want %q", r.lastOpenWithCommand, "vim")
	}
}

// TestOpenCurrentEntryWithEmptySubmissionRemembersNothing pins that
// submitting a blank (or all-whitespace) command doesn't overwrite
// whatever was remembered before — the same "nothing typed means
// nothing happens" rule the generic prompt overlay's own finishPrompt
// already follows for every other caller.
func TestOpenCurrentEntryWithEmptySubmissionRemembersNothing(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.focusRow(1)
	r.lastOpenWithCommand = "vim"

	r.openCurrentEntryWith()
	r.prompt.SetText("   ")
	r.finishPrompt(tcell.KeyEnter)

	if r.lastOpenWithCommand != "vim" {
		t.Errorf("lastOpenWithCommand = %q, want it left at %q after an all-whitespace submission", r.lastOpenWithCommand, "vim")
	}
	if len(r.toolWindows) != 0 {
		t.Errorf("len(toolWindows) = %d, want 0 — nothing should run either", len(r.toolWindows))
	}
}

// TestOpenCurrentEntryWithOnALocalPanelDoesNotError mirrors
// TestPlainKeyEditRunsEditAction's own reasoning for the local path.
// TestOpenCurrentEntryWithLogsAnAction pins openCurrentEntryWith's own
// activity-log instrumentation via runCommandOnFileAndReload.
// app.Suspend is a no-op here (no real screen behind r.app), so this
// only pins the logging wiring on the "command exited 0" path, not that
// "less" actually ran — the same acknowledged limitation
// TestPlainKeyEditRunsEditAction's own doc comment notes in
// bottombar_test.go.
func TestOpenCurrentEntryWithLogsAnAction(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.focusRow(1)
	readLog := attachTestActivityLog(t, r)

	r.openCurrentEntryWith()
	r.prompt.SetText("less")
	r.finishPrompt(tcell.KeyEnter)

	got := readLog()
	if !strings.Contains(got, string(activitylog.CategoryShell)) || !strings.Contains(got, "ran less on") {
		t.Errorf("log = %q, want a shell entry about running less", got)
	}
}

func TestOpenCurrentEntryWithOnALocalPanelDoesNotError(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.focusRow(1)

	r.openCurrentEntryWith()
	r.prompt.SetText("less")
	r.finishPrompt(tcell.KeyEnter)

	if r.activePage == errorPage {
		t.Errorf("Open with on a local panel reported an error: %q", r.errorView.GetText(true))
	}
}

// TestOpenCurrentEntryWithOnARemotePanelDispatchesToOpenRemoteEntryWithCommand
// mirrors TestEditCurrentEntryOnARemotePanelDoesNotError: pins that the
// remote dispatch is reached at all (the actual upload-or-not decision
// is already pinned separately by finishRemoteEdit's own tests).
func TestOpenCurrentEntryWithOnARemotePanelDispatchesToOpenRemoteEntryWithCommand(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	client.content = map[string][]byte{"/remote/b.txt": []byte("hello")}

	r.openCurrentEntryWith()
	r.prompt.SetText("less")
	r.finishPrompt(tcell.KeyEnter)

	if r.activePage == errorPage {
		t.Errorf("Open with on a remote panel reported an error: %q", r.errorView.GetText(true))
	}
}

// TestOpenCurrentEntryWithRefusedInsideArchive mirrors
// TestCutCurrentSelectionRefusedInsideArchive for this action.
func TestOpenCurrentEntryWithRefusedInsideArchive(t *testing.T) {
	dir := t.TempDir()
	zipPath := writeTestZip(t, dir, "sample.zip", map[string]string{"a.txt": "hi\n"})
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	if err := r.panel.load(zipPath); err != nil {
		t.Fatal(err)
	}

	r.openCurrentEntryWith()

	if r.activePage != errorPage {
		t.Fatalf("expected an error overlay, activePage = %q", r.activePage)
	}
	got := strings.ReplaceAll(r.errorView.GetText(true), "\n", " ")
	if !strings.Contains(got, "not supported") || !strings.Contains(got, "archive") {
		t.Errorf("error text = %q, want it to mention errNotSupportedInArchive", got)
	}
}

// TestContextMenuOpenWithMnemonicOpensThePrompt pins "o" as the menu's
// own reachable shortcut for this entry (see menuEntry.mnemonic's own
// doc comment on why it's the entry's own first letter here, the same
// exception "Multiply" already established).
func TestContextMenuOpenWithMnemonicOpensThePrompt(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	// openCurrentEntryWith reads the panel's own current cursor row (see
	// its own doc comment) — focusRow first, the same as
	// TestContextMenuEditRunsEditCurrentEntry already does for the
	// identical reason, mirroring what a real right-click would have
	// already done before the menu ever opened.
	r.panel.focusRow(2) // off ".." onto apple.txt (row 1 is app-data/ — directories sort first, see fixtureDir)
	openMenuOnRow(t, r, 2)

	r.menu.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'o', tcell.ModNone), func(tview.Primitive) {})

	if r.activePage != promptPage {
		t.Fatalf("activePage = %q, want the \"Open with…\" prompt", r.activePage)
	}
}
