package activitylog

import "testing"

func TestLevelStringAndParseLevelRoundTrip(t *testing.T) {
	for _, level := range Levels() {
		if got := ParseLevel(level.String()); got != level {
			t.Errorf("ParseLevel(%q) = %v, want %v", level.String(), got, level)
		}
	}
}

func TestParseLevelIsCaseInsensitive(t *testing.T) {
	if got := ParseLevel("ACTIONS"); got != LevelActions {
		t.Errorf("ParseLevel(ACTIONS) = %v, want %v", got, LevelActions)
	}
}

func TestParseLevelDefaultsToOffForUnrecognized(t *testing.T) {
	for _, s := range []string{"", "bogus", "  ", "action"} {
		if got := ParseLevel(s); got != LevelOff {
			t.Errorf("ParseLevel(%q) = %v, want %v", s, got, LevelOff)
		}
	}
}

func TestLevelsAreInAscendingOrderOfDetail(t *testing.T) {
	levels := Levels()
	for i := 1; i < len(levels); i++ {
		if levels[i] <= levels[i-1] {
			t.Errorf("Levels()[%d] = %v should be more detailed than Levels()[%d] = %v", i, levels[i], i-1, levels[i-1])
		}
	}
}

func TestLevelLabelsAreNonEmptyAndDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, level := range Levels() {
		label := level.Label()
		if label == "" {
			t.Errorf("Level(%d).Label() is empty", level)
		}
		if seen[label] {
			t.Errorf("Label %q used by more than one Level", label)
		}
		seen[label] = true
	}
}
