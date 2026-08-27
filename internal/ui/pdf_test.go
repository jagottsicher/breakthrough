package ui

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/viewer"
)

// buildMinimalPDF is internal/ui's own copy of internal/viewer's own
// test-only fixture builder of the same name — duplicated rather than
// exported and imported across packages for a single test fixture, the
// same convention this codebase already applies to small per-package
// test helpers (requireTool, writeFakeExecutable, ...). See its own
// twin in internal/viewer/pdf_test.go for the full rationale on why
// the xref offsets are computed here rather than hand-counted.
func buildMinimalPDF(content string) []byte {
	var buf bytes.Buffer
	var offsets [6]int

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

// TestShowBuiltinLookRendersPDFPage pins the end-to-end wiring for a
// PDF: Look opens over it (not an error), tracks page state, and shows
// a "Page 1 of 1" footer — checked regardless of whether pdftoppm is
// actually installed in this environment (see LoadPDFPage's own two
// tiers), since either way the page-navigation state and footer are
// the same.
func TestShowBuiltinLookRendersPDFPage(t *testing.T) {
	dir := t.TempDir()
	path := writePDFFixture(t, dir, "doc.pdf", "Hello PDF")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.showBuiltinLook(path)

	if r.activePage != viewerPage {
		t.Fatalf("activePage = %q, want %q for a PDF", r.activePage, viewerPage)
	}
	if r.viewerPDFPath != path {
		t.Errorf("viewerPDFPath = %q, want %q", r.viewerPDFPath, path)
	}
	if r.viewerPDFPage != 1 || r.viewerPDFPageCount != 1 {
		t.Errorf("viewerPDFPage/PageCount = %d/%d, want 1/1", r.viewerPDFPage, r.viewerPDFPageCount)
	}
	if got := r.viewerView.GetText(true); !strings.Contains(got, "Page 1 of 1") {
		t.Errorf("viewerView text doesn't include the page footer: %q", got)
	}
}

// TestShowBuiltinLookResetsPDFStateForNonPDFFile pins that opening
// Look on an ordinary file after a PDF leaves no stale PDF-navigation
// state behind — see captureViewerKey's own doc comment on why a
// leftover r.viewerPDFPath would misroute PageUp/PageDown on whatever's
// shown next.
func TestShowBuiltinLookResetsPDFStateForNonPDFFile(t *testing.T) {
	dir := t.TempDir()
	pdfPath := writePDFFixture(t, dir, "doc.pdf", "Hello PDF")
	txtPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(txtPath, []byte("just text\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.showBuiltinLook(pdfPath)
	if r.viewerPDFPath == "" {
		t.Fatal("setup: viewerPDFPath should be set after opening a PDF")
	}

	r.showBuiltinLook(txtPath)

	if r.viewerPDFPath != "" {
		t.Errorf("viewerPDFPath = %q, want \"\" after opening a plain text file", r.viewerPDFPath)
	}
}

// TestTurnPDFPageNoopsAtBoundaries pins that PageDown on the last page
// (and PageUp on the first) leaves the current page unchanged, rather
// than wrapping around or going out of range.
func TestTurnPDFPageNoopsAtBoundaries(t *testing.T) {
	dir := t.TempDir()
	path := writePDFFixture(t, dir, "doc.pdf", "Hello PDF")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.showBuiltinLook(path)

	r.turnPDFPage(1) // past the last (and only) page
	if r.viewerPDFPage != 1 {
		t.Errorf("viewerPDFPage = %d after PageDown past the last page, want still 1", r.viewerPDFPage)
	}

	r.turnPDFPage(-1) // before the first page
	if r.viewerPDFPage != 1 {
		t.Errorf("viewerPDFPage = %d after PageUp before the first page, want still 1", r.viewerPDFPage)
	}
}

// TestCaptureViewerKeyPassesThroughWithoutPDF pins that PageUp/PageDown
// are left completely alone (for TextView's own default scroll
// handling) whenever Look isn't currently showing a PDF at all.
func TestCaptureViewerKeyPassesThroughWithoutPDF(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.viewerPDFPath = "" // explicit, even though it's already the zero value — this is the condition under test

	event := tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone)
	if got := r.captureViewerKey(event); got != event {
		t.Error("captureViewerKey should pass PageDown through unchanged when not viewing a PDF")
	}
}

// TestCaptureViewerKeyTurnsPageForPDF pins the actual interception:
// with a PDF open, PageDown is consumed (nil returned, so TextView
// never sees it) and turns the page.
func TestCaptureViewerKeyTurnsPageForPDF(t *testing.T) {
	dir := t.TempDir()
	path := writePDFFixture(t, dir, "doc.pdf", "Hello PDF")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.showBuiltinLook(path)

	event := tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone)
	if got := r.captureViewerKey(event); got != nil {
		t.Error("captureViewerKey should consume PageDown (return nil) while viewing a PDF")
	}
	// This fixture only has one page, so turnPDFPage's own no-op-at-
	// the-boundary guard should have left the page unchanged — this
	// only confirms turnPDFPage was actually reached, not skipped.
	if r.viewerPDFPage != 1 {
		t.Errorf("viewerPDFPage = %d, want still 1 (single-page fixture, boundary no-op)", r.viewerPDFPage)
	}
}

// TestSetPDFViewModeSwitchesToText pins 't' forcing the text tier —
// works regardless of whether pdftoppm happens to be installed in this
// environment, since PDFViewText skips rasterization outright (see
// viewer.LoadPDFPage's own doc comment).
func TestSetPDFViewModeSwitchesToText(t *testing.T) {
	dir := t.TempDir()
	path := writePDFFixture(t, dir, "doc.pdf", "Hello PDF text mode")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.showBuiltinLook(path)

	r.setPDFViewMode(viewer.PDFViewText)

	if r.viewerPDFMode != viewer.PDFViewText {
		t.Errorf("viewerPDFMode = %v, want PDFViewText", r.viewerPDFMode)
	}
	if got := r.viewerView.GetText(true); !strings.Contains(got, "Hello PDF") {
		t.Errorf("viewerView text doesn't contain the page's own real text after switching to text mode: %q", got)
	}
}

// TestSetPDFViewModeGraphicWithoutPdftoppmReportsFailure pins 'g'
// forcing the graphic tier and reporting a real failure — not
// silently falling back to text — when pdftoppm genuinely can't be
// found ($PATH isolated, the same approach internal/viewer's own
// TestLoadPDFPageGraphicModeReportsFailureInsteadOfFallingBack uses).
func TestSetPDFViewModeGraphicWithoutPdftoppmReportsFailure(t *testing.T) {
	dir := t.TempDir()
	path := writePDFFixture(t, dir, "doc.pdf", "Hello PDF")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.showBuiltinLook(path)

	empty := t.TempDir()
	t.Setenv("PATH", empty)

	r.setPDFViewMode(viewer.PDFViewGraphic)

	if r.viewerPDFMode != viewer.PDFViewGraphic {
		t.Errorf("viewerPDFMode = %v, want PDFViewGraphic", r.viewerPDFMode)
	}
	got := r.viewerView.GetText(true)
	if strings.Contains(got, "Hello PDF") {
		t.Errorf("viewerView text shows the page's own text content, want a failure message instead (PDFViewGraphic must not fall back): %q", got)
	}
	if !strings.Contains(got, "poppler-utils") {
		t.Errorf("viewerView text doesn't explain the graphic-view failure: %q", got)
	}
}

// TestCaptureViewerKeyHandlesTextKey pins that pressing 't' while
// viewing a PDF is consumed (nil returned, so TextView never sees it
// as ordinary input) and switches the mode.
func TestCaptureViewerKeyHandlesTextKey(t *testing.T) {
	dir := t.TempDir()
	path := writePDFFixture(t, dir, "doc.pdf", "Hello PDF")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.showBuiltinLook(path)

	event := tcell.NewEventKey(tcell.KeyRune, 't', tcell.ModNone)
	if got := r.captureViewerKey(event); got != nil {
		t.Error("captureViewerKey should consume 't' (return nil) while viewing a PDF")
	}
	if r.viewerPDFMode != viewer.PDFViewText {
		t.Errorf("viewerPDFMode = %v, want PDFViewText after 't'", r.viewerPDFMode)
	}
}

// TestCaptureViewerKeyGraphicTextKeysPassThroughWithoutPDF pins that
// 'g'/'t' are left alone for TextView's own default handling whenever
// Look isn't currently showing a PDF — a plain text file's own real
// content might legitimately contain the letter g or t, and this must
// never intercept ordinary typing/scrolling in that case.
func TestCaptureViewerKeyGraphicTextKeysPassThroughWithoutPDF(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.viewerPDFPath = ""

	for _, ch := range []rune{'g', 't'} {
		event := tcell.NewEventKey(tcell.KeyRune, ch, tcell.ModNone)
		if got := r.captureViewerKey(event); got != event {
			t.Errorf("captureViewerKey should pass %q through unchanged when not viewing a PDF", ch)
		}
	}
}

// TestShowBuiltinLookResetsPDFModeForNewFile pins that a 'g'/'t'
// choice made on one PDF doesn't carry over to the next one opened —
// see showBuiltinLook's own reset, right alongside viewerPDFPath.
func TestShowBuiltinLookResetsPDFModeForNewFile(t *testing.T) {
	dir := t.TempDir()
	firstPath := writePDFFixture(t, dir, "first.pdf", "First PDF")
	secondPath := writePDFFixture(t, dir, "second.pdf", "Second PDF")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.showBuiltinLook(firstPath)
	r.setPDFViewMode(viewer.PDFViewText)
	if r.viewerPDFMode != viewer.PDFViewText {
		t.Fatal("setup: viewerPDFMode should be PDFViewText after explicitly switching")
	}

	r.showBuiltinLook(secondPath)

	if r.viewerPDFMode != viewer.PDFViewAuto {
		t.Errorf("viewerPDFMode = %v, want PDFViewAuto (the default) after opening a different PDF", r.viewerPDFMode)
	}
}
