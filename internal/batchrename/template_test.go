package batchrename

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var templateInput = Input{
	Name:    "IMG_0042.JPG",
	Index:   2,
	Parent:  "Holiday",
	ModTime: time.Date(2026, 3, 9, 14, 5, 0, 0, time.UTC),
}

func TestTemplateExpandsEveryToken(t *testing.T) {
	rules := Rules{
		Template:     "{parent}-{date}-{name}-{counter}.{ext}",
		NumberStart:  10,
		NumberDigits: 3,
	}
	got, err := Rename(rules, templateInput)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Holiday-2026-03-09-IMG_0042-012.JPG.JPG"; got != want {
		t.Errorf("Rename = %q, want %q", got, want)
	}
}

func TestTemplateSeesTheNameAfterTheEarlierSteps(t *testing.T) {
	rules := Rules{Find: "IMG_", Case: CaseLower, TrimBack: 1, Template: "[{name}]"}
	got, err := Rename(rules, templateInput)
	if err != nil {
		t.Fatal(err)
	}
	if want := "[004].JPG"; got != want {
		t.Errorf("Rename = %q, want %q", got, want)
	}
}

func TestTemplateCounterAndNumberingStepAgree(t *testing.T) {
	rules := Rules{Template: "{counter}_{name}", NumberPosition: NumberSuffix, NumberStart: 5, NumberStep: 5, NumberDigits: 2}
	got, err := Rename(rules, templateInput)
	if err != nil {
		t.Fatal(err)
	}
	if want := "15_IMG_0042-15.JPG"; got != want {
		t.Errorf("Rename = %q, want %q", got, want)
	}
}

func TestTemplateDateHonoursStrftimeAndGoLayouts(t *testing.T) {
	got, err := Rename(Rules{Template: "{date}", DateFormat: "%Y%m%d_%H%M", DateStrftime: true}, templateInput)
	if err != nil || got != "20260309_1405.JPG" {
		t.Errorf("strftime date = %q, %v; want 20260309_1405.JPG", got, err)
	}
	got, err = Rename(Rules{Template: "{date}", DateFormat: "Jan 2006"}, templateInput)
	if err != nil || got != "Mar 2026.JPG" {
		t.Errorf("Go layout date = %q, %v; want Mar 2026.JPG", got, err)
	}
	got, err = Rename(Rules{Template: "{date}", DateStrftime: true}, templateInput)
	if err != nil || got != "2026-03-09.JPG" {
		t.Errorf("default strftime date = %q, %v; want 2026-03-09.JPG", got, err)
	}
	if _, err := Rename(Rules{Template: "{date}", DateFormat: "%Q", DateStrftime: true}, templateInput); err == nil || !strings.Contains(err.Error(), "date format") {
		t.Errorf("an untranslatable strftime format should be reported, got %v", err)
	}
}

func TestTemplateLeavesUnknownBracesAlone(t *testing.T) {
	got, err := Rename(Rules{Template: "{name} {size} {{x}}"}, templateInput)
	if err != nil {
		t.Fatal(err)
	}
	if want := "IMG_0042 {size} {{x}}.JPG"; got != want {
		t.Errorf("Rename = %q, want %q", got, want)
	}
}

func TestEmptyTemplateIsANoOp(t *testing.T) {
	got, err := Rename(Rules{}, templateInput)
	if err != nil || got != "IMG_0042.JPG" {
		t.Errorf("Rename with no template = %q, %v", got, err)
	}
}

func TestTemplateTokensListMatchesWhatApplyTemplateExpands(t *testing.T) {
	var all strings.Builder
	for _, tok := range TemplateTokens {
		all.WriteString(tok.Token)
	}
	got, err := Rename(Rules{Template: all.String()}, Input{Name: "n.e", Parent: "p", ModTime: templateInput.ModTime})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "{") {
		t.Errorf("a listed token was not expanded: %q", got)
	}
}

func TestPlanFeedsParentAndModTimeToTheTemplate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Album")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2025, 12, 31, 0, 0, 0, 0, time.Local)
	if err := os.Chtimes(p, when, when); err != nil {
		t.Fatal(err)
	}
	result := Plan([]string{p}, Rules{Template: "{parent}_{date}"})
	if len(result.Changes) != 1 || filepath.Base(result.Changes[0].To) != "Album_2025-12-31.txt" {
		t.Errorf("Plan = %+v, want a.txt -> Album_2025-12-31.txt", result)
	}
}
