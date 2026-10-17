package remotefs

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseTagsTrimsWhitespaceAroundEachTag(t *testing.T) {
	got := ParseTags(" web , build ")
	want := []string{"build", "web"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseTags = %v, want %v", got, want)
	}
}

func TestParseTagsDropsEmptyPiecesFromDoubleOrTrailingCommas(t *testing.T) {
	got := ParseTags("web,,build,")
	want := []string{"build", "web"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseTags = %v, want %v (empty pieces dropped, not kept as blank tags)", got, want)
	}
}

func TestParseTagsSortsAlphanumerically(t *testing.T) {
	got := ParseTags("web,build,staging")
	want := []string{"build", "staging", "web"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseTags = %v, want %v (sorted, not input order)", got, want)
	}
}

func TestParseTagsDeduplicatesExactMatches(t *testing.T) {
	got := ParseTags("web,build,web")
	want := []string{"build", "web"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseTags = %v, want %v (duplicate \"web\" collapsed to one)", got, want)
	}
}

func TestParseTagsCapsAtTagsMaxCount(t *testing.T) {
	raw := strings.Join([]string{
		"t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8", "t9", "t10", "t11", "t12",
	}, ",")
	got := ParseTags(raw)
	if len(got) != tagsMaxCount {
		t.Fatalf("len(ParseTags(...)) = %d, want %d (capped)", len(got), tagsMaxCount)
	}
}

func TestParseTagsTruncatesAnOverlyLongTagInsteadOfDroppingIt(t *testing.T) {
	long := strings.Repeat("a", tagMaxLength+10)
	got := ParseTags(long)
	want := []string{strings.Repeat("a", tagMaxLength)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseTags(long) = %v, want %v (truncated to %d chars, not dropped)", got, want, tagMaxLength)
	}
}

// TestParseTagsDropsATagContainingADisallowedCharacter pins the
// defensive half of ParseTags' own contract: the Connect dialog's own
// field already filters live (connectTagsAcceptanceFunc), so this only
// matters for a caller that didn't go through that field at all — a
// piece outside IsValidTagRune is dropped, not partially sanitized.
func TestParseTagsDropsATagContainingADisallowedCharacter(t *testing.T) {
	got := ParseTags("web,slash/not-allowed,build")
	want := []string{"build", "web"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseTags = %v, want %v (the invalid tag dropped entirely)", got, want)
	}
}

func TestParseTagsOnAnEmptyStringReturnsAnEmptySliceNotNilConfusion(t *testing.T) {
	got := ParseTags("")
	if len(got) != 0 {
		t.Errorf("ParseTags(\"\") = %v, want empty", got)
	}
}

func TestIsValidTagRuneAllowsSpacesUnlikeIdentifier(t *testing.T) {
	if !IsValidTagRune(' ') {
		t.Error("IsValidTagRune(' ') = false, want true — a multi-word tag like \"web server\" must be allowed")
	}
	if IsValidTagRune('/') {
		t.Error("IsValidTagRune('/') = true, want false")
	}
}
