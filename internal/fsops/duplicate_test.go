package fsops

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestComputeDuplicateNameSuffixText(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "report.txt")

	got, err := ComputeDuplicateName(src, DuplicateOptions{
		Separator: "_", Strategy: DuplicateSuffixText, SuffixText: "BAK",
	})
	if err != nil {
		t.Fatalf("ComputeDuplicateName: %v", err)
	}
	want := filepath.Join(dir, "report_BAK.txt")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestComputeDuplicateNameSuffixTextHasNoAutoRetry pins the deliberate
// design: with the computed candidate already on disk,
// ComputeDuplicateName still returns it rather than looping — the
// caller's own subsequent Copy is what reports "already exists". A
// second, separate Duplicate run on that returned name (not exercised
// by this call at all) is how "report_BAK_BAK.txt" would ever arise —
// never a loop inside one call.
func TestComputeDuplicateNameSuffixTextHasNoAutoRetry(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "report.txt")
	// The candidate this computes to already exists.
	if err := os.WriteFile(filepath.Join(dir, "report_BAK.txt"), []byte("already here"), 0o640); err != nil {
		t.Fatal(err)
	}

	got, err := ComputeDuplicateName(src, DuplicateOptions{
		Separator: "_", Strategy: DuplicateSuffixText, SuffixText: "BAK",
	})
	if err != nil {
		t.Fatalf("ComputeDuplicateName: %v", err)
	}
	want := filepath.Join(dir, "report_BAK.txt")
	if got != want {
		t.Errorf("got %q, want %q (the same already-taken candidate, not a retried alternative)", got, want)
	}
}

func TestComputeDuplicateNameNumberedStartsAtOne(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "report.txt")

	got, err := ComputeDuplicateName(src, DuplicateOptions{Separator: "_", Strategy: DuplicateNumbered})
	if err != nil {
		t.Fatalf("ComputeDuplicateName: %v", err)
	}
	want := filepath.Join(dir, "report_1.txt")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestComputeDuplicateNameNumberedScansUntilFree pins the user's own
// explicit answer: with "report_1.txt" and "report_2.txt" already
// taken, this keeps counting rather than failing at the first or second
// collision.
func TestComputeDuplicateNameNumberedScansUntilFree(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "report.txt")
	for _, n := range []string{"1", "2"} {
		if err := os.WriteFile(filepath.Join(dir, "report_"+n+".txt"), []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
	}

	got, err := ComputeDuplicateName(src, DuplicateOptions{Separator: "_", Strategy: DuplicateNumbered})
	if err != nil {
		t.Fatalf("ComputeDuplicateName: %v", err)
	}
	want := filepath.Join(dir, "report_3.txt")
	if got != want {
		t.Errorf("got %q, want %q (should have scanned past the two taken numbers)", got, want)
	}
}

func TestComputeDuplicateNameNumberedPadding(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "report.txt")

	got, err := ComputeDuplicateName(src, DuplicateOptions{
		Separator: "_", Strategy: DuplicateNumbered, NumberPadding: 3,
	})
	if err != nil {
		t.Fatalf("ComputeDuplicateName: %v", err)
	}
	want := filepath.Join(dir, "report_001.txt")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestComputeDuplicateNameNumberedReportsErrorOnceLimitExhausted pins
// the safety-net behavior itself — lowers duplicateNumberedScanLimit
// for the duration of this test rather than actually creating 100000
// files, the same reasoning duplicateNumberedScanLimit's own doc
// comment gives for being a var in the first place.
func TestComputeDuplicateNameNumberedReportsErrorOnceLimitExhausted(t *testing.T) {
	original := duplicateNumberedScanLimit
	duplicateNumberedScanLimit = 3
	t.Cleanup(func() { duplicateNumberedScanLimit = original })

	dir := t.TempDir()
	src := filepath.Join(dir, "report.txt")
	for n := 1; n <= 3; n++ {
		if err := os.WriteFile(filepath.Join(dir, "report_"+strconv.Itoa(n)+".txt"), []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
	}

	_, err := ComputeDuplicateName(src, DuplicateOptions{Separator: "_", Strategy: DuplicateNumbered})
	if err == nil {
		t.Fatal("expected an error once every candidate up to the (lowered) scan limit is taken")
	}
}

// TestComputeDuplicateNameDateTimeGoLayoutReproducesTheAgreedExample
// pins the exact example agreed with the user: the Go reference-time
// layout "2006-1-2 15:04:05" applied to 2026-11-09 23:59:59 produces
// "2026-11-9 23:59:59" — year and month need no padding help (both
// already two digits or more), but the day's own single "2" token
// (not "02") is what keeps "9" from becoming "09".
func TestComputeDuplicateNameDateTimeGoLayoutReproducesTheAgreedExample(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "report.txt")
	now := time.Date(2026, time.November, 9, 23, 59, 59, 0, time.UTC)

	got, err := ComputeDuplicateName(src, DuplicateOptions{
		Separator: "_", Strategy: DuplicateDateTime,
		DateTimeFormat: "2006-1-2 15:04:05", Now: now,
	})
	if err != nil {
		t.Fatalf("ComputeDuplicateName: %v", err)
	}
	want := filepath.Join(dir, "report_2026-11-9 23:59:59.txt")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestComputeDuplicateNameDateTimeStrftileReproducesTheAgreedExample is
// the same pin for the strftime-style toggle: "%Y-%-m-%-d %H:%M:%S"
// must translate to exactly the Go layout the test above already pins,
// producing the identical "2026-11-9 23:59:59" result.
func TestComputeDuplicateNameDateTimeStrftimeReproducesTheAgreedExample(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "report.txt")
	now := time.Date(2026, time.November, 9, 23, 59, 59, 0, time.UTC)

	got, err := ComputeDuplicateName(src, DuplicateOptions{
		Separator: "_", Strategy: DuplicateDateTime,
		DateTimeFormat: "%Y-%-m-%-d %H:%M:%S", DateTimeStrftime: true, Now: now,
	})
	if err != nil {
		t.Fatalf("ComputeDuplicateName: %v", err)
	}
	want := filepath.Join(dir, "report_2026-11-9 23:59:59.txt")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestComputeDuplicateNameDateTimeUsesRawUnixTimestamp(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "report.txt")
	now := time.Date(2026, time.November, 9, 23, 59, 59, 0, time.UTC)

	got, err := ComputeDuplicateName(src, DuplicateOptions{
		Separator: "_", Strategy: DuplicateDateTime,
		DateTimeUseUnix: true, Now: now,
		// Deliberately also set, to confirm DateTimeUseUnix wins over it.
		DateTimeFormat: "2006-1-2",
	})
	if err != nil {
		t.Fatalf("ComputeDuplicateName: %v", err)
	}
	want := filepath.Join(dir, "report_"+strconv.FormatInt(now.Unix(), 10)+".txt")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestComputeDuplicateNamePreservesExtensionAndHandlesNoExtension pins
// that the suffix always lands right before the extension ("report_BAK
// .txt", not "report.txt_BAK"), and that a file with no extension at
// all is handled the same way, with nothing appended after the suffix.
func TestComputeDuplicateNamePreservesExtensionAndHandlesNoExtension(t *testing.T) {
	dir := t.TempDir()

	got, err := ComputeDuplicateName(filepath.Join(dir, "report.txt"), DuplicateOptions{
		Separator: "_", Strategy: DuplicateSuffixText, SuffixText: "BAK",
	})
	if err != nil {
		t.Fatalf("ComputeDuplicateName: %v", err)
	}
	if want := filepath.Join(dir, "report_BAK.txt"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	got, err = ComputeDuplicateName(filepath.Join(dir, "README"), DuplicateOptions{
		Separator: "_", Strategy: DuplicateSuffixText, SuffixText: "BAK",
	})
	if err != nil {
		t.Fatalf("ComputeDuplicateName: %v", err)
	}
	if want := filepath.Join(dir, "README_BAK"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStrftimeToGoLayoutUnsupportedSpecifierErrors(t *testing.T) {
	if _, err := strftimeToGoLayout("%Q"); err == nil {
		t.Error("expected an error for an unsupported specifier")
	}
}

func TestStrftimeToGoLayoutDanglingPercentErrors(t *testing.T) {
	if _, err := strftimeToGoLayout("%Y-%"); err == nil {
		t.Error("expected an error for a dangling %% at the end")
	}
	if _, err := strftimeToGoLayout("%Y-%-"); err == nil {
		t.Error("expected an error for a dangling %%- at the end")
	}
}

func TestStrftimeToGoLayoutLiteralPercentEscape(t *testing.T) {
	got, err := strftimeToGoLayout("100%%")
	if err != nil {
		t.Fatalf("strftimeToGoLayout: %v", err)
	}
	if got != "100%" {
		t.Errorf("got %q, want %q", got, "100%")
	}
}
