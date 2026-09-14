package batchrename

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOrderedByNameIsCaseInsensitiveAndStable(t *testing.T) {
	paths := []string{"/d/b.txt", "/d/A.txt", "/d/a.txt", "/d/C.txt"}
	got := Ordered(paths, Rules{NumberOrder: OrderByName})
	want := []string{"/d/A.txt", "/d/a.txt", "/d/b.txt", "/d/C.txt"}
	if !equal(got, want) {
		t.Errorf("Ordered by name = %v, want %v", got, want)
	}
	if !equal(paths, []string{"/d/b.txt", "/d/A.txt", "/d/a.txt", "/d/C.txt"}) {
		t.Error("Ordered modified its input")
	}
}

func TestOrderedReversedFlipsWhateverWasPicked(t *testing.T) {
	paths := []string{"/d/x", "/d/y", "/d/z"}
	got := Ordered(paths, Rules{NumberReversed: true})
	if want := []string{"/d/z", "/d/y", "/d/x"}; !equal(got, want) {
		t.Errorf("Ordered reversed as-listed = %v, want %v", got, want)
	}
	got = Ordered([]string{"/d/a", "/d/c", "/d/b"}, Rules{NumberOrder: OrderByName, NumberReversed: true})
	if want := []string{"/d/c", "/d/b", "/d/a"}; !equal(got, want) {
		t.Errorf("Ordered by name reversed = %v, want %v", got, want)
	}
}

func TestOrderedByModTimeIsOldestFirst(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-time.Hour)
	for i, name := range []string{"newest", "oldest", "middle"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		var age time.Duration
		switch i {
		case 0:
			age = 0
		case 1:
			age = 2 * time.Minute
		case 2:
			age = time.Minute
		}
		if err := os.Chtimes(p, base.Add(-age), base.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	paths := []string{filepath.Join(dir, "newest"), filepath.Join(dir, "oldest"), filepath.Join(dir, "middle")}
	var got []string
	for _, p := range Ordered(paths, Rules{NumberOrder: OrderByModTime}) {
		got = append(got, filepath.Base(p))
	}
	if want := []string{"oldest", "middle", "newest"}; !equal(got, want) {
		t.Errorf("Ordered by mtime = %v, want %v", got, want)
	}
}

func TestPresetRoundTripsThroughDisk(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rename-presets") // doesn't exist yet: SavePreset must create it
	rules := Rules{
		Find: "IMG_", Replace: "holiday-", Regex: true,
		Case:           CaseTitle,
		TrimFront:      1,
		NumberPosition: NumberSuffix, NumberStart: 10, NumberStep: 5, NumberDigits: 3,
		NumberOrder: OrderByModTime, NumberReversed: true,
		ExtensionMode: ExtensionSetTo, ExtensionValue: "jpeg", ExtensionOnDirs: true,
	}
	if err := SavePreset(dir, "Holiday photos", rules); err != nil {
		t.Fatalf("SavePreset: %v", err)
	}
	if !PresetExists(dir, "Holiday photos") {
		t.Error("PresetExists = false right after saving")
	}

	presets, err := LoadPresets(dir)
	if err != nil {
		t.Fatalf("LoadPresets: %v", err)
	}
	if len(presets) != 1 || presets[0].Name != "Holiday photos" {
		t.Fatalf("presets = %+v, want just \"Holiday photos\"", presets)
	}
	if presets[0].Rules != rules {
		t.Errorf("loaded rules = %+v, want %+v", presets[0].Rules, rules)
	}

	// Readable on disk: enums by name, not by number.
	data, err := os.ReadFile(filepath.Join(dir, "Holiday photos.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"case": "title"`, `"number_position": "suffix"`, `"number_order": "by-mtime"`, `"extension_mode": "set"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("preset file lacks %s:\n%s", want, data)
		}
	}

	if err := DeletePreset(dir, "Holiday photos"); err != nil {
		t.Fatalf("DeletePreset: %v", err)
	}
	if PresetExists(dir, "Holiday photos") {
		t.Error("preset still exists after DeletePreset")
	}
	if err := DeletePreset(dir, "Holiday photos"); err != nil {
		t.Errorf("deleting an already-deleted preset should be fine, got %v", err)
	}
}

func TestLoadPresetsSkipsABrokenFileButKeepsTheRest(t *testing.T) {
	dir := t.TempDir()
	if err := SavePreset(dir, "good", Rules{Find: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte(`{"case": "shouting"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}

	presets, err := LoadPresets(dir)
	if err == nil || !strings.Contains(err.Error(), `"bad"`) {
		t.Errorf("LoadPresets error = %v, want one naming the bad preset", err)
	}
	if len(presets) != 1 || presets[0].Name != "good" || presets[0].Rules.Find != "a" {
		t.Errorf("presets = %+v, want just the good one", presets)
	}
}

func TestLoadPresetsFromAMissingDirectoryIsEmptyNotAnError(t *testing.T) {
	presets, err := LoadPresets(filepath.Join(t.TempDir(), "nope"))
	if err != nil || len(presets) != 0 {
		t.Errorf("LoadPresets(missing) = %v, %v; want nil, nil", presets, err)
	}
}

func TestValidatePresetName(t *testing.T) {
	for _, bad := range []string{"", "   ", "a/b", `a\b`, ".hidden"} {
		if err := ValidatePresetName(bad); err == nil {
			t.Errorf("ValidatePresetName(%q) = nil, want an error", bad)
		}
	}
	for _, good := range []string{"Holiday photos", "camera-2024", "Ärger & Co."} {
		if err := ValidatePresetName(good); err != nil {
			t.Errorf("ValidatePresetName(%q) = %v, want nil", good, err)
		}
	}
}

func TestEnumSpellingsRoundTripAndRejectUnknowns(t *testing.T) {
	for _, m := range []CaseMode{CaseNone, CaseUpper, CaseLower, CaseTitle, CaseSentence} {
		got, err := ParseCaseMode(m.String())
		if err != nil || got != m {
			t.Errorf("ParseCaseMode(%q) = %v, %v; want %v", m.String(), got, err, m)
		}
	}
	for _, o := range []NumberOrder{OrderAsListed, OrderByName, OrderByModTime} {
		got, err := ParseNumberOrder(o.String())
		if err != nil || got != o {
			t.Errorf("ParseNumberOrder(%q) = %v, %v; want %v", o.String(), got, err, o)
		}
	}
	var rules Rules
	if err := json.Unmarshal([]byte(`{"extension_mode": "whatever"}`), &rules); err == nil {
		t.Error("an unknown extension_mode spelling should be an error")
	}
}
