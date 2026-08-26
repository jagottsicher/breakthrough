package viewer

import (
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/ledongthuc/pdf"
)

// PDFPageCount returns path's own total page count, via ledongthuc/pdf
// — used regardless of which tier LoadPDFPage ends up rendering any
// given page through (see its own doc comment), so internal/ui's page
// navigation ("page N of M") stays consistent either way, and so it
// can be checked once, up front, before ever asking for a specific
// page.
//
// Known, accepted limitation: an encrypted PDF, or one malformed
// enough that even ledongthuc/pdf's own lenient, token-based xref
// reader (verified by reading its own readXrefTableData, not guessed
// — it tolerates a great deal that the PDF spec's strict fixed-width
// xref format technically requires) can't open it, is reported as an
// error here even if pdftoppm — poppler's own, separate, much stricter
// parser — could have rendered it anyway. Not worth a second, separate
// "can pdftoppm even open this" probe just to hedge against that rare
// case.
func PDFPageCount(path string) (int, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }() // read-only — nothing further to report on a Close failure
	return r.NumPage(), nil
}

// PDFTextFallbackNotice is what internal/ui appends to a PDF page's
// own extracted text (see its own renderPDFPageContent) whenever
// LoadPDFPage had to fall back to it because pdftoppm wasn't
// available — kept here, next to the code that actually decides which
// tier ran (Result.Kind alone doesn't distinguish "this is a text
// file" from "this is a PDF's fallback text"), so the message and the
// condition that triggers it can never drift out of sync with each
// other.
const PDFTextFallbackNotice = "showing extracted text only — install poppler-utils (pdftoppm) for full-page rendering"

// LoadPDFPage renders page (1-based) of the PDF at path: as a real
// raster image via pdftoppm where it's installed (see
// rasterizePDFPage — going through the exact same
// image.Decode/ScaleForTerminal path a real PNG/JPEG already does), or
// — where pdftoppm isn't available, or fails on this particular file —
// as extracted plain text via ledongthuc/pdf's own Page.GetPlainText
// instead (see extractPDFPageText). Result.Kind tells internal/ui
// which tier actually ran (KindImage or KindText); a page that fails
// both comes back as KindUnsupported with a Reason.
func LoadPDFPage(path string, page int) (Result, error) {
	if img, format, err := rasterizePDFPage(path, page); err == nil {
		return Result{Kind: KindImage, Image: img, ImageFormat: format}, nil
	}

	text, err := extractPDFPageText(path, page)
	if err == nil {
		return Result{Kind: KindText, Content: text}, nil
	}
	return Result{Kind: KindUnsupported, Reason: "couldn't render or extract text from this PDF page"}, nil
}

// rasterizePDFPage runs pdftoppm on path, rendering exactly page to a
// temp PNG (-singlefile: per pdftoppm's own -h output, "write only the
// first page and do not add digits" — combined with -f/-l both set to
// page, this produces a single, predictably-named "<prefix>.png" with
// no page-number suffix to compute or glob for) and decodes it via
// this package's own DecodeImage — a rendered PDF page is just a PNG
// from here on, reusing every bit of Phase 2's own image machinery.
//
// Returns an error (never partial output) if pdftoppm isn't on PATH
// at all, or if it runs but fails on this particular file (an
// encrypted PDF poppler itself can't open without a password, page
// out of range, ...) — LoadPDFPage's own text-extraction fallback is
// what actually handles either case, not this function.
func rasterizePDFPage(path string, page int) (img image.Image, format string, err error) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		return nil, "", err
	}

	tmpDir, err := os.MkdirTemp("", "breakthrough-pdf-*")
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }() // best-effort temp-dir cleanup

	outPrefix := filepath.Join(tmpDir, "page")
	p := strconv.Itoa(page)
	cmd := exec.Command("pdftoppm", "-png", "-singlefile", "-f", p, "-l", p, path, outPrefix)
	if err := cmd.Run(); err != nil {
		return nil, "", err
	}

	data, err := os.ReadFile(outPrefix + ".png")
	if err != nil {
		return nil, "", err
	}
	return DecodeImage(data)
}

// extractPDFPageText opens path fresh (see PDFPageCount's own doc
// comment on the same open call's real limitations) and returns page's
// own plain text via ledongthuc/pdf's Page.GetPlainText — nil for the
// font map argument: that parameter exists to let a caller reuse font
// metrics already loaded for an earlier page in the same document
// (this package never keeps a Reader open across calls, so there's
// nothing to reuse), and GetPlainText itself resolves whatever it
// needs from the page's own font resources either way.
func extractPDFPageText(path string, page int) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }() // read-only — nothing further to report on a Close failure

	if page < 1 || page > r.NumPage() {
		return "", fmt.Errorf("page %d out of range (1-%d)", page, r.NumPage())
	}
	return r.Page(page).GetPlainText(nil)
}
