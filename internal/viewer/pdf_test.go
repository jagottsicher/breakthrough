package viewer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildMinimalPDF constructs a tiny, valid, hand-built single-page PDF
// showing the literal text content — no external tool or library
// involved in building it, so this fixture never depends on anything
// beyond what's needed to read it back afterward. Each object's own
// byte offset is computed here as it's appended, rather than hand-
// counted: ledongthuc/pdf's own xref reader is token-based, not
// fixed-width the way the PDF spec technically requires (verified by
// reading its readXrefTableData, not guessed — it just wants correct
// offsets and valid whitespace-separated tokens), so this only needs
// to get the offsets right, not byte-perfect column alignment.
func buildMinimalPDF(content string) []byte {
	var buf bytes.Buffer
	var offsets [6]int // index 1..5 used; 0 is the xref free-list head

	obj := func(n int, body string) {
		offsets[n] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", n, body)
	}

	buf.WriteString("%PDF-1.4\n")
	obj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	obj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	obj(3, "<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 4 0 R >> >> /MediaBox [0 0 200 100] /Contents 5 0 R >>")
	obj(4, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	stream := fmt.Sprintf("BT /F1 12 Tf 10 50 Td (%s) Tj ET", content)
	obj(5, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))

	xrefStart := buf.Len()
	buf.WriteString("xref\n0 6\n")
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	buf.WriteString("trailer\n<< /Size 6 /Root 1 0 R >>\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF", xrefStart)

	return buf.Bytes()
}

func writePDFFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buildMinimalPDF(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSniffRecognizesPDFSignature(t *testing.T) {
	if kind := Sniff([]byte("%PDF-1.4\nsome binary-ish bytes\x00 follow")); kind != KindPDF {
		t.Errorf("Sniff(%%PDF-...) = %v, want KindPDF", kind)
	}
}

func TestLoadClassifiesPDFWithoutReadingPageContent(t *testing.T) {
	dir := t.TempDir()
	path := writePDFFixture(t, dir, "doc.pdf", "Hello PDF")

	result, err := Load(path, DefaultPreviewLimit)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != KindPDF {
		t.Fatalf("Kind = %v, want KindPDF", result.Kind)
	}
	if result.Content != "" || result.Image != nil {
		t.Error("Load should not populate Content/Image for KindPDF — see LoadPDFPage instead")
	}
}

func TestPDFPageCount(t *testing.T) {
	dir := t.TempDir()
	path := writePDFFixture(t, dir, "doc.pdf", "Hello PDF")

	count, err := PDFPageCount(path)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("PDFPageCount = %d, want 1", count)
	}
}

func TestPDFPageCountOnNonPDFFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notreally.pdf")
	if err := os.WriteFile(path, []byte("not a pdf at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := PDFPageCount(path); err == nil {
		t.Fatal("want an error for content that isn't actually a PDF, got nil")
	}
}

func TestExtractPDFPageTextFindsRealContent(t *testing.T) {
	dir := t.TempDir()
	path := writePDFFixture(t, dir, "doc.pdf", "Hello PDF from a test fixture")

	text, err := extractPDFPageText(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(text), []byte("Hello PDF")) {
		t.Errorf("extracted text = %q, want it to contain %q", text, "Hello PDF")
	}
}

func TestExtractPDFPageTextOutOfRange(t *testing.T) {
	dir := t.TempDir()
	path := writePDFFixture(t, dir, "doc.pdf", "Hello PDF")

	if _, err := extractPDFPageText(path, 2); err == nil {
		t.Fatal("want an error for a page number past the document's own single page, got nil")
	}
}

// TestLoadPDFPageFallsBackToTextWithoutPdftoppm pins LoadPDFPage's own
// tier order end to end, forced onto the text tier by isolating $PATH
// so pdftoppm can't be found — the same PATH-isolation approach
// internal/ui's own TestExternalPagerCommandChain already uses for a
// different external-tool fallback chain.
func TestLoadPDFPageFallsBackToTextWithoutPdftoppm(t *testing.T) {
	dir := t.TempDir()
	path := writePDFFixture(t, dir, "doc.pdf", "Hello PDF fallback")

	empty := t.TempDir()
	t.Setenv("PATH", empty)

	result, err := LoadPDFPage(path, 1, PDFViewAuto)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != KindText {
		t.Fatalf("Kind = %v, want KindText (pdftoppm unavailable — see PATH isolation above)", result.Kind)
	}
	if !bytes.Contains([]byte(result.Content), []byte("Hello PDF")) {
		t.Errorf("Content = %q, want it to contain the page's own real text", result.Content)
	}
}

func TestLoadPDFPageOnCorruptFileIsUnsupported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4\nnot a real pdf structure at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	empty := t.TempDir() // isolate from a real pdftoppm too, so this only exercises the text-extraction failure path
	t.Setenv("PATH", empty)

	result, err := LoadPDFPage(path, 1, PDFViewAuto)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != KindUnsupported {
		t.Errorf("Kind = %v, want KindUnsupported for a file neither tier can process", result.Kind)
	}
	if result.Reason == "" {
		t.Error("Reason is empty, want an explanation")
	}
}

// TestLoadPDFPageGraphicModeReportsFailureInsteadOfFallingBack pins
// PDFViewGraphic's own explicit-choice semantics: unlike PDFViewAuto,
// a failed rasterization attempt is reported directly as
// KindUnsupported rather than silently handing back extracted text —
// the user asked for graphic view specifically.
func TestLoadPDFPageGraphicModeReportsFailureInsteadOfFallingBack(t *testing.T) {
	dir := t.TempDir()
	path := writePDFFixture(t, dir, "doc.pdf", "Hello PDF")

	empty := t.TempDir()
	t.Setenv("PATH", empty) // no pdftoppm — rasterization must fail

	result, err := LoadPDFPage(path, 1, PDFViewGraphic)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != KindUnsupported {
		t.Errorf("Kind = %v, want KindUnsupported (PDFViewGraphic must not fall back to text)", result.Kind)
	}
	if result.Reason == "" {
		t.Error("Reason is empty, want an explanation naming the failed rasterization")
	}
}

// TestLoadPDFPageTextModeSkipsRasterizationEvenIfAvailable pins that
// PDFViewText always uses the text tier, never attempting to
// rasterize at all — checked by requiring a real pdftoppm to be
// present (see requireTool) and confirming the result is still
// KindText, not KindImage.
func TestLoadPDFPageTextModeSkipsRasterizationEvenIfAvailable(t *testing.T) {
	requireTool(t, "pdftoppm")
	dir := t.TempDir()
	path := writePDFFixture(t, dir, "doc.pdf", "Hello PDF text mode")

	result, err := LoadPDFPage(path, 1, PDFViewText)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != KindText {
		t.Fatalf("Kind = %v, want KindText even with pdftoppm available (PDFViewText forces the text tier)", result.Kind)
	}
	if !bytes.Contains([]byte(result.Content), []byte("Hello PDF")) {
		t.Errorf("Content = %q, want it to contain the page's own real text", result.Content)
	}
}

// TestLoadPDFPageEmptyTextIsReportedNotBlank pins that a page whose
// extracted text is empty (or all whitespace) — the real fixture here
// uses an empty Tj string, standing in for a scanned page with no
// text layer — is reported as KindUnsupported with a Reason rather
// than silently showing nothing.
func TestLoadPDFPageEmptyTextIsReportedNotBlank(t *testing.T) {
	dir := t.TempDir()
	path := writePDFFixture(t, dir, "blank.pdf", "   ") // whitespace-only content string

	result, err := LoadPDFPage(path, 1, PDFViewText)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != KindUnsupported {
		t.Errorf("Kind = %v, want KindUnsupported for a page with no real extractable text", result.Kind)
	}
	if result.Reason == "" {
		t.Error("Reason is empty, want an explanation")
	}
}

// TestRasterizePDFPageWithRealPdftoppm is the one test in this file
// that exercises the actual external tool — skipped outright where
// it isn't installed (see requireTool), same as every other real-
// external-tool test in this codebase (internal/search's own
// requireTool-gated tests).
func TestRasterizePDFPageWithRealPdftoppm(t *testing.T) {
	requireTool(t, "pdftoppm")
	dir := t.TempDir()
	path := writePDFFixture(t, dir, "doc.pdf", "Hello PDF rasterized")

	img, format, err := rasterizePDFPage(context.Background(), path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if format != "png" {
		t.Errorf("format = %q, want %q (pdftoppm -png)", format, "png")
	}
	b := img.Bounds()
	if b.Dx() < 1 || b.Dy() < 1 {
		t.Errorf("rasterized page has a degenerate size: %v", b)
	}
}

// requireTool mirrors internal/search's own helper of the same name —
// duplicated locally rather than exported and imported across
// packages for a single small skip-helper, the same call internal/ui's
// own test files already make for their own local copies of similarly
// small helpers.
func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not available in this environment: %v", name, err)
	}
}

// malformedPDFs writes a set of files that get far enough into
// ledongthuc/pdf to reach its lexer, and returns their paths.
//
// Padded past 100 bytes deliberately: the library reads the last 100
// bytes to find "%%EOF", so anything shorter is rejected up front with
// a clean error and never exercises the code that actually panics.
func malformedPDFs(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	pad := "% " + strings.Repeat("padding past the 100-byte tail window ", 3) + "\n"

	files := map[string]string{
		// startxref pointing past the end of the file — reading there
		// panics inside the library's own buffer.
		"bogus-offset.pdf": "%PDF-1.5\n" + pad + "trailer\n<< >>\nstartxref\n99999\n%%EOF\n",
		// An "object" that is really the xref keyword, the shape that
		// crashed a real user (see withPDFRecover).
		"xref-keyword.pdf": "%PDF-1.5\n1 0 xref\n" + pad + "trailer\n<< /Size 2 >>\nstartxref\n9\n%%EOF\n",
		"garbage.pdf":      "%PDF-1.7\n" + pad + "\x00\x01\x02\xff\xfe trailer startxref 12 \n%%EOF\n",
	}

	var paths []string
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return paths
}

// TestPDFPageCountSurvivesAMalformedFile is a regression test for a
// crash that killed breakthrough for a real user: ledongthuc/pdf
// reports malformed input by panicking rather than returning an error,
// and the Details sidebar calls this for whatever the cursor is on — so
// arrowing past a broken PDF took the whole application down from
// inside tview's input handler.
//
// Removing withPDFRecover makes this fail with a panic, not a failed
// assertion.
func TestPDFPageCountSurvivesAMalformedFile(t *testing.T) {
	for _, path := range malformedPDFs(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			count, err := PDFPageCount(path)
			if err == nil {
				t.Errorf("got %d pages and no error — a malformed file should report one", count)
			}
		})
	}
}

// TestExtractPDFPageTextSurvivesAMalformedFile covers the other route
// into the same library. Its lexer is used lazily when pages and text
// are read, so a file that opened cleanly can still panic here.
func TestExtractPDFPageTextSurvivesAMalformedFile(t *testing.T) {
	for _, path := range malformedPDFs(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			if _, err := extractPDFPageText(path, 1); err == nil {
				t.Error("expected an error for a malformed file, got none")
			}
		})
	}
}

// TestLoadPDFPageContextHonoursCancellation pins the wiring that makes
// abandoning a preview actually free: rasterizing runs pdftoppm as a
// subprocess, and a context that only guarded the *result* would leave
// that process running to completion. A caller that drops previews as a
// cursor moves would then accumulate one pdftoppm per file it passed
// over — the stall gone from the screen and still there on the machine.
//
// exec.CommandContext is what kills it; with a plain exec.Command this
// returns a rendered page instead of an error.
func TestLoadPDFPageContextHonoursCancellation(t *testing.T) {
	requireTool(t, "pdftoppm")
	dir := t.TempDir()
	path := writePDFFixture(t, dir, "doc.pdf", "cancel me")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already dead before the subprocess is even started

	// Graphic mode reports a failed rasterize as KindUnsupported rather
	// than an error (see LoadPDFPageContext), so what proves the
	// subprocess was actually killed is the absence of an image.
	result, err := LoadPDFPageContext(ctx, path, 1, PDFViewGraphic)
	if err != nil {
		t.Fatalf("LoadPDFPageContext: %v", err)
	}
	if result.Kind == KindImage {
		t.Error("a cancelled context still produced a rendered page — pdftoppm ran to completion")
	}
}
