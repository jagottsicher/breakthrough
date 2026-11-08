package ui

import (
	"strings"
	"testing"
)

// optionsButtonLabels is a tiny helper collecting the row's own current
// button labels in order, for assertions below.
func optionsButtonLabels(r *Root) []string {
	count := r.options.Buttons.GetItemCount()
	labels := make([]string, 0, count)
	for i := 0; i < count; i++ {
		if b, ok := r.options.Buttons.GetItem(i).(interface{ GetLabel() string }); ok {
			labels = append(labels, b.GetLabel())
		}
	}
	return labels
}

func containsLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

// TestOptionsButtonsOnlyGlobalOnesOnAnOrdinaryCategory pins the user's
// own explicit request (2026-11-08): on a category with no button of
// its own ("Behavior"), the row shows only the three screen-wide
// actions, not all seven.
func TestOptionsButtonsOnlyGlobalOnesOnAnOrdinaryCategory(t *testing.T) {
	r, _ := newOptionsRoot(t)
	selectOptionCategory(t, r, "Behavior")

	labels := optionsButtonLabels(r)
	if len(labels) != 3 {
		t.Fatalf("Behavior: button row = %+v, want exactly 3 global buttons", labels)
	}
	for _, want := range []string{"Edit config file", "Reset all"} {
		if !containsLabel(labels, want) {
			t.Errorf("Behavior: missing global button %q in %+v", want, labels)
		}
	}
	for _, unwanted := range []string{"New color scheme", "Remove orphaned labels", "Edit system prompt", "Reset system prompt"} {
		if containsLabel(labels, unwanted) {
			t.Errorf("Behavior: category-specific button %q should be hidden, got %+v", unwanted, labels)
		}
	}
}

// TestOptionsButtonsShowAppearanceOnlyOnAppearance pins that "New
// color scheme" appears on Appearance and nowhere else.
func TestOptionsButtonsShowAppearanceOnlyOnAppearance(t *testing.T) {
	r, _ := newOptionsRoot(t)
	selectOptionCategory(t, r, "Appearance")
	if !containsLabel(optionsButtonLabels(r), "New color scheme") {
		t.Fatal("Appearance: \"New color scheme\" should be visible")
	}

	selectOptionCategory(t, r, "Trash")
	if containsLabel(optionsButtonLabels(r), "New color scheme") {
		t.Error("Trash: \"New color scheme\" should be hidden")
	}
}

// TestOptionsButtonsShowLabelActionsOnlyOnLabel pins the same
// contract for "Remove orphaned labels".
func TestOptionsButtonsShowLabelActionsOnlyOnLabel(t *testing.T) {
	r, _ := newOptionsRoot(t)
	selectOptionCategory(t, r, "Label")
	if !containsLabel(optionsButtonLabels(r), "Remove orphaned labels") {
		t.Fatal("Label: \"Remove orphaned labels\" should be visible")
	}

	selectOptionCategory(t, r, "Firewall")
	if containsLabel(optionsButtonLabels(r), "Remove orphaned labels") {
		t.Error("Firewall: \"Remove orphaned labels\" should be hidden")
	}
}

// TestOptionsButtonsShowConsigliereActionsOnlyOnConsigliere pins the
// same contract for the two system-prompt buttons together.
func TestOptionsButtonsShowConsigliereActionsOnlyOnConsigliere(t *testing.T) {
	r, _ := newOptionsRoot(t)
	selectOptionCategory(t, r, "AI Consigliere")
	labels := optionsButtonLabels(r)
	for _, want := range []string{"Edit system prompt", "Reset system prompt"} {
		if !containsLabel(labels, want) {
			t.Errorf("AI Consigliere: missing %q in %+v", want, labels)
		}
	}

	selectOptionCategory(t, r, "Mail")
	labels = optionsButtonLabels(r)
	for _, unwanted := range []string{"Edit system prompt", "Reset system prompt"} {
		if containsLabel(labels, unwanted) {
			t.Errorf("Mail: %q should be hidden, got %+v", unwanted, labels)
		}
	}
}

// TestOptionsResetCategoryButtonNamesTheActualCategory pins the
// dynamic label rewrite (renderOptionsButtons) — "Reset category"
// alone doesn't say *which* category it would reset.
func TestOptionsResetCategoryButtonNamesTheActualCategory(t *testing.T) {
	r, _ := newOptionsRoot(t)
	selectOptionCategory(t, r, "Behavior")
	if got := r.options.ResetCategoryBtn.GetLabel(); got != "Reset Behavior" {
		t.Errorf("ResetCategoryBtn label = %q, want %q", got, "Reset Behavior")
	}

	selectOptionCategory(t, r, "Trash")
	if got := r.options.ResetCategoryBtn.GetLabel(); got != "Reset Trash" {
		t.Errorf("ResetCategoryBtn label = %q, want %q", got, "Reset Trash")
	}
}

// TestOptionsFocusRingShrinksAndGrowsWithCategory pins the focus-ring
// half of this change: Tab must only ever reach buttons that are
// actually in the row right now, and the ring's own length changes as
// the category does, without anything crashing or getting stuck.
func TestOptionsFocusRingShrinksAndGrowsWithCategory(t *testing.T) {
	r, _ := newOptionsRoot(t)

	selectOptionCategory(t, r, "Behavior")
	plainRing := r.optionsFocusRing()
	// Categories + Table + 3 global buttons.
	if len(plainRing) != 5 {
		t.Fatalf("Behavior: focus ring has %d stops, want 5", len(plainRing))
	}

	selectOptionCategory(t, r, "AI Consigliere")
	consigliereRing := r.optionsFocusRing()
	// Categories + Table + 3 global + 2 Consigliere-only buttons.
	if len(consigliereRing) != 7 {
		t.Fatalf("AI Consigliere: focus ring has %d stops, want 7", len(consigliereRing))
	}

	// Tab must still cycle cleanly through the now-longer ring without
	// losing focus or wrapping early.
	r.app.SetFocus(consigliereRing[0])
	for i := 1; i <= len(consigliereRing); i++ {
		if !r.cycleOptionsFocus(1) {
			t.Fatalf("step %d: cycleOptionsFocus found nothing focused", i)
		}
	}
	if !consigliereRing[0].HasFocus() {
		t.Error("Tab didn't wrap back to the first stop")
	}
}

// TestOptionsButtonListStillReturnsEveryButtonRegardlessOfVisibility
// pins that applyOptionsTheme's own styling loop (optionsButtonList)
// is unaffected by this change — a hidden button still needs correct
// styling for the moment it reappears.
func TestOptionsButtonListStillReturnsEveryButtonRegardlessOfVisibility(t *testing.T) {
	r, _ := newOptionsRoot(t)
	selectOptionCategory(t, r, "Behavior") // a category with no buttons of its own

	all := r.optionsButtonList()
	if len(all) != 7 {
		t.Fatalf("optionsButtonList() = %d buttons, want all 7 regardless of visibility", len(all))
	}
}

// TestOptionsButtonBarNeverEmpty is a cheap sanity sweep over every
// real category — the three global buttons must survive no matter
// which one is selected, so the row is never left with nothing in it.
func TestOptionsButtonBarNeverEmpty(t *testing.T) {
	r, _ := newOptionsRoot(t)
	for _, cat := range optionCategories() {
		selectOptionCategory(t, r, cat.name)
		labels := optionsButtonLabels(r)
		if len(labels) < 3 {
			t.Errorf("category %q: only %d buttons, want at least the 3 global ones: %+v", cat.name, len(labels), labels)
		}
		if !strings.HasPrefix(r.options.ResetCategoryBtn.GetLabel(), "Reset ") {
			t.Errorf("category %q: ResetCategoryBtn label = %q, want a \"Reset \" prefix", cat.name, r.options.ResetCategoryBtn.GetLabel())
		}
	}
}
