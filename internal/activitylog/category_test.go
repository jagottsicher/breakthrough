package activitylog

import "testing"

func TestCategoryLabelsAreNonEmptyAndDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Categories() {
		label := c.Label()
		if label == "" {
			t.Errorf("Category(%q).Label() is empty", c)
		}
		if seen[label] {
			t.Errorf("Label %q used by more than one Category", label)
		}
		seen[label] = true
	}
}

func TestCategoriesHaveNoDuplicateValues(t *testing.T) {
	seen := map[Category]bool{}
	for _, c := range Categories() {
		if seen[c] {
			t.Errorf("Category %q listed more than once", c)
		}
		seen[c] = true
	}
}
