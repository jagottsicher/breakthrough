package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/systemd"
)

// Parsing logic (systemctl's own JSON) is tested in internal/systemd
// itself — this file only covers the UI/rendering contract on top of
// it, the same split mounts_test.go already keeps for internal/mounts.

// TestOpenServicesShowsThePage pins openServices' own basic contract.
func TestOpenServicesShowsThePage(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openServices()

	if r.activePage != servicesPage {
		t.Fatalf("activePage = %q, want the Services screen", r.activePage)
	}
}

// TestCaptureServicesKeyEscapeClosesTheScreen mirrors the Mounts
// screen's own equivalent test.
func TestCaptureServicesKeyEscapeClosesTheScreen(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openServices()

	if got := r.captureServicesKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)); got != nil {
		t.Error("captureServicesKey should consume Escape")
	}
	if r.activePage == servicesPage {
		t.Error("Escape should have closed the Services screen")
	}
}

// TestGCReachesAIConsigliereFromServices pins "gC" reachability from
// this screen, the same shape TestGCReachesAIConsigliereFromMounts
// already establishes.
func TestGCReachesAIConsigliereFromServices(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openServices()

	if got := r.captureServicesKey(tcell.NewEventKey(tcell.KeyRune, 'g', tcell.ModNone)); got != nil {
		t.Fatalf("capture('g') = %v, want nil (consumed, chord started)", got)
	}
	if r.pendingChord != 'g' {
		t.Fatalf("pendingChord = %q, want 'g'", string(r.pendingChord))
	}
	if !r.HandlePlainKey(tcell.NewEventKey(tcell.KeyRune, 'C', tcell.ModNone)) {
		t.Fatal("the resolving 'C' should have been consumed")
	}
	if !r.consigliere.Visible {
		t.Error("\"gC\" should have opened the AI Consigliere sidebar")
	}
}

// TestCaptureServicesKeyRRefreshesWithoutClosing pins that "r" is
// consumed and re-reads the unit list (via reloadServices) without
// closing the screen — even when systemctl itself fails (a real
// possibility on a non-Linux build target), reloadServices reports
// that through services.Err rather than leaving the screen in an
// inconsistent state, so this doesn't need a systemctl-availability
// guard either way.
func TestCaptureServicesKeyRRefreshesWithoutClosing(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openServices()

	if got := r.captureServicesKey(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModNone)); got != nil {
		t.Error("captureServicesKey should consume \"r\"")
	}
	if r.activePage != servicesPage {
		t.Error("\"r\" should refresh in place, not close the screen")
	}
}

// TestRenderServicesShowsTheErrorInPlaceOfRows pins that a failed read
// (systemctl missing or erroring outright) is reported on screen
// rather than leaving the table looking like an empty, still-
// successful read.
func TestRenderServicesShowsTheErrorInPlaceOfRows(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.services.Err = errors.New("systemctl: command not found")
	r.services.Units = nil

	r.renderServices()

	got := strings.TrimSpace(r.services.Table.GetCell(1, servicesColName).Text)
	if !strings.Contains(got, "not found") {
		t.Errorf("row 1 = %q, want the error message", got)
	}
}

// TestRenderServicesGroupsByActiveStateFailedFirst pins the grouping
// contract: a bold "<group> (N)" header per servicesSections, Failed
// first regardless of input order, and a failed unit's own row shown
// in the error color so it stands out even inside a long Active group
// on a busy host.
func TestRenderServicesGroupsByActiveStateFailedFirst(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.services.Units = []systemd.Unit{
		{Name: "ok.service", Load: "loaded", Active: "active", Sub: "running", Description: "Fine"},
		{Name: "broken.service", Load: "loaded", Active: "failed", Sub: "failed", Description: "Broken"},
		{Name: "idle.service", Load: "loaded", Active: "inactive", Sub: "dead", Description: "Idle"},
	}

	r.renderServices()

	headerText := r.services.Table.GetCell(1, servicesColName).Text
	if !strings.Contains(headerText, "Failed") {
		t.Fatalf("row 1 = %q, want the Failed group header first", headerText)
	}
	failedRow := r.services.Table.GetCell(2, servicesColName).Text
	if !strings.Contains(strings.TrimSpace(failedRow), "broken.service") {
		t.Fatalf("row 2 = %q, want broken.service right under the Failed header", failedRow)
	}
	gotColor, _, _ := r.services.Table.GetCell(2, servicesColName).Style.Decompose()
	if gotColor != r.theme.EntryError {
		t.Errorf("broken.service name color = %v, want EntryError (%v)", gotColor, r.theme.EntryError)
	}
}

// TestRenderServicesPassesThroughAnUnrecognizedActiveValueAsOther pins
// that a unit whose Active value isn't Failed/Active/Inactive still
// shows up, under its own "Other" group, rather than being silently
// dropped by the section matching.
func TestRenderServicesPassesThroughAnUnrecognizedActiveValueAsOther(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.services.Units = []systemd.Unit{
		{Name: "weird.service", Load: "loaded", Active: "activating", Sub: "start", Description: "Mid-start"},
	}

	r.renderServices()

	headerText := r.services.Table.GetCell(1, servicesColName).Text
	if !strings.Contains(headerText, "Other") {
		t.Fatalf("row 1 = %q, want the Other group header", headerText)
	}
	row2 := strings.TrimSpace(r.services.Table.GetCell(2, servicesColName).Text)
	if !strings.Contains(row2, "weird.service") {
		t.Errorf("row 2 = %q, want weird.service under Other", row2)
	}
}

// TestRenderServicesEmptyListShowsPlaceholder pins the same "report an
// empty read explicitly" contract renderMounts/renderFirewall already
// have for their own zero-entries case.
func TestRenderServicesEmptyListShowsPlaceholder(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.services.Units = nil
	r.services.Err = nil

	r.renderServices()

	got := strings.TrimSpace(r.services.Table.GetCell(1, servicesColName).Text)
	if !strings.Contains(got, "No service units") {
		t.Errorf("row 1 = %q, want the empty-list placeholder", got)
	}
}
