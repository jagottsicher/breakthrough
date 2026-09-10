package filterexpr

import (
	"testing"
	"time"
)

var mtimeTestNow = time.Date(2026, 9, 10, 12, 0, 0, 0, time.Local)

func TestParseMtimeBareRelativeMeansWithinTheLast(t *testing.T) {
	f, err := ParseMtime("7 days", mtimeTestNow)
	if err != nil {
		t.Fatalf("ParseMtime: %v", err)
	}
	if !f.Match(mtimeTestNow.Add(-24 * time.Hour)) {
		t.Error("1 day ago should count as \"within the last 7 days\"")
	}
	if f.Match(mtimeTestNow.Add(-10 * 24 * time.Hour)) {
		t.Error("10 days ago should not count as \"within the last 7 days\"")
	}
}

func TestParseMtimeLastPrefixIsOptional(t *testing.T) {
	withLast, err := ParseMtime("last 7 days", mtimeTestNow)
	if err != nil {
		t.Fatalf("ParseMtime(with last): %v", err)
	}
	withoutLast, err := ParseMtime("7 days", mtimeTestNow)
	if err != nil {
		t.Fatalf("ParseMtime(without last): %v", err)
	}
	probe := mtimeTestNow.Add(-3 * 24 * time.Hour)
	if withLast.Match(probe) != withoutLast.Match(probe) {
		t.Error("\"last 7 days\" and \"7 days\" should behave identically")
	}
}

func TestParseMtimeAgoSuffixIsOptional(t *testing.T) {
	f, err := ParseMtime("after 2 hours ago", mtimeTestNow)
	if err != nil {
		t.Fatalf("ParseMtime: %v", err)
	}
	if !f.Match(mtimeTestNow.Add(-1 * time.Hour)) {
		t.Error("1 hour ago should be \"after 2 hours ago\"")
	}
	if f.Match(mtimeTestNow.Add(-3 * time.Hour)) {
		t.Error("3 hours ago should not be \"after 2 hours ago\"")
	}
}

func TestParseMtimeBefore(t *testing.T) {
	f, err := ParseMtime("before 2026-01-01", mtimeTestNow)
	if err != nil {
		t.Fatalf("ParseMtime: %v", err)
	}
	if !f.Match(time.Date(2025, 12, 31, 23, 0, 0, 0, time.Local)) {
		t.Error("2025-12-31 should be before 2026-01-01")
	}
	if f.Match(time.Date(2026, 1, 2, 0, 0, 0, 0, time.Local)) {
		t.Error("2026-01-02 should not be before 2026-01-01")
	}
}

func TestParseMtimeAfter(t *testing.T) {
	f, err := ParseMtime("after 2026-01-01", mtimeTestNow)
	if err != nil {
		t.Fatalf("ParseMtime: %v", err)
	}
	if !f.Match(time.Date(2026, 1, 2, 0, 0, 0, 0, time.Local)) {
		t.Error("2026-01-02 should be after 2026-01-01")
	}
	if f.Match(time.Date(2025, 12, 31, 0, 0, 0, 0, time.Local)) {
		t.Error("2025-12-31 should not be after 2026-01-01")
	}
}

func TestParseMtimeBetweenIsInclusiveAndOrderIndependent(t *testing.T) {
	forward, err := ParseMtime("between 2026-01-01 and 2026-02-01", mtimeTestNow)
	if err != nil {
		t.Fatalf("ParseMtime(forward): %v", err)
	}
	backward, err := ParseMtime("between 2026-02-01 and 2026-01-01", mtimeTestNow)
	if err != nil {
		t.Fatalf("ParseMtime(backward): %v", err)
	}

	mid := time.Date(2026, 1, 15, 0, 0, 0, 0, time.Local)
	if !forward.Match(mid) || !backward.Match(mid) {
		t.Error("a date in the middle should match regardless of which endpoint was written first")
	}

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
	end := time.Date(2026, 2, 1, 0, 0, 0, 0, time.Local)
	if !forward.Match(start) || !forward.Match(end) {
		t.Error("between should be inclusive of both endpoints")
	}

	outside := time.Date(2026, 3, 1, 0, 0, 0, 0, time.Local)
	if forward.Match(outside) {
		t.Error("a date outside the range should not match")
	}
}

func TestParseMtimeBetweenWithRelativeMoments(t *testing.T) {
	f, err := ParseMtime("between 10 days and 2 days", mtimeTestNow)
	if err != nil {
		t.Fatalf("ParseMtime: %v", err)
	}
	if !f.Match(mtimeTestNow.Add(-5 * 24 * time.Hour)) {
		t.Error("5 days ago should be inside the 10-days-to-2-days-ago window")
	}
	if f.Match(mtimeTestNow.Add(-1 * 24 * time.Hour)) {
		t.Error("1 day ago should be outside a window ending 2 days ago")
	}
}

func TestParseMtimeAbsoluteWithTime(t *testing.T) {
	f, err := ParseMtime("after 2026-09-01 14:30", mtimeTestNow)
	if err != nil {
		t.Fatalf("ParseMtime: %v", err)
	}
	if !f.Match(time.Date(2026, 9, 1, 15, 0, 0, 0, time.Local)) {
		t.Error("15:00 should be after 14:30 the same day")
	}
	if f.Match(time.Date(2026, 9, 1, 14, 0, 0, 0, time.Local)) {
		t.Error("14:00 should not be after 14:30 the same day")
	}
}

func TestParseMtimeMonthsAndYearsAreApproximate(t *testing.T) {
	f, err := ParseMtime("1 year", mtimeTestNow)
	if err != nil {
		t.Fatalf("ParseMtime: %v", err)
	}
	if !f.Match(mtimeTestNow.Add(-100 * 24 * time.Hour)) {
		t.Error("100 days ago should be within the last (approximate) year")
	}
	if f.Match(mtimeTestNow.Add(-400 * 24 * time.Hour)) {
		t.Error("400 days ago should not be within the last (approximate) year")
	}
}

func TestParseMtimeRejectsEmptyExpression(t *testing.T) {
	if _, err := ParseMtime("", mtimeTestNow); err == nil {
		t.Error("an empty expression should be rejected")
	}
}

func TestParseMtimeRejectsGarbage(t *testing.T) {
	if _, err := ParseMtime("sometime soon-ish", mtimeTestNow); err == nil {
		t.Error("nonsense text should be rejected, not silently matched")
	}
}

func TestParseMtimeRejectsMalformedBetween(t *testing.T) {
	if _, err := ParseMtime("between 2026-01-01", mtimeTestNow); err == nil {
		t.Error("a between clause missing its own \"and Y\" half should be rejected")
	}
	if _, err := ParseMtime("between 2026-01-01 and 2026-02-01 and 2026-03-01", mtimeTestNow); err == nil {
		t.Error("a between clause with three moments should be rejected, not silently take the first two")
	}
}

func TestParseMtimeKeywordsCaseInsensitive(t *testing.T) {
	f, err := ParseMtime("BEFORE 2026-01-01", mtimeTestNow)
	if err != nil {
		t.Fatalf("ParseMtime: %v", err)
	}
	if !f.Match(time.Date(2025, 1, 1, 0, 0, 0, 0, time.Local)) {
		t.Error("uppercase \"BEFORE\" should work the same as lowercase")
	}
}
