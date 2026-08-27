package viewer

import (
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

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
// LoadPDFPage fell back to it under PDFViewAuto because pdftoppm
// wasn't available — kept here, next to the code that actually decides
// which tier ran (Result.Kind alone doesn't distinguish "this is a
// text file" from "this is a PDF's fallback text"), so the message and
// the condition that triggers it can never drift out of sync with
// each other.
const PDFTextFallbackNotice = "showing extracted text only — install poppler-utils (pdftoppm) for full-page rendering"

// PDFViewMode selects which of LoadPDFPage's own two tiers to use for
// a page — a real user report: a text-heavy PDF rendered as a raster
// image is downsampled into illegible half-block mush at any
// realistic terminal size, exactly the failure mode extracting real
// text exists to avoid, so which tier runs needs to be a choice, not
// always whatever LoadPDFPage would have picked on its own.
type PDFViewMode int

const (
	// PDFViewAuto is LoadPDFPage's own original behavior, and still the
	// default for a PDF's very first page: rasterize via pdftoppm where
	// it's available, silently falling back to extracted text
	// otherwise — see PDFTextFallbackNotice.
	PDFViewAuto PDFViewMode = iota
	// PDFViewGraphic forces the rasterized-image tier — see
	// internal/ui's own 'g' key. Unlike PDFViewAuto, a failure to
	// rasterize is reported as KindUnsupported with a Reason instead of
	// silently falling back to text: the user asked for graphic view
	// specifically, so silently handing back something else instead
	// would look like the key press did nothing.
	PDFViewGraphic
	// PDFViewText forces the extracted-text tier outright, skipping
	// rasterization entirely even where pdftoppm is available — see
	// internal/ui's own 't' key.
	PDFViewText
)

// LoadPDFPage renders page (1-based) of the PDF at path, per mode: as
// a real raster image via pdftoppm (see rasterizePDFPage — going
// through the exact same image.Decode/ScaleForTerminal path a real
// PNG/JPEG already does) for PDFViewAuto/PDFViewGraphic, or as
// extracted plain text via ledongthuc/pdf's own Page.GetPlainText (see
// extractPDFPageText) for PDFViewText or whenever PDFViewAuto's own
// rasterization attempt didn't succeed. Result.Kind tells internal/ui
// which tier actually ran (KindImage or KindText); a page that fails
// the tier mode asked for (and, for PDFViewAuto only, the text
// fallback too) comes back as KindUnsupported with a Reason.
//
// A page whose extracted text comes back empty (or all whitespace) —
// a scanned page with no real text layer, the single most common real
// reason for this — is also reported as KindUnsupported with its own
// Reason naming that specifically, rather than silently showing a
// blank page: this can only actually happen for PDFViewText (an
// explicit choice to see text specifically), or for PDFViewAuto once
// its own rasterization attempt has already failed too — by that
// point there's genuinely nothing left to show either way, so the
// clear message is the honest answer, not a missing fallback.
func LoadPDFPage(path string, page int, mode PDFViewMode) (Result, error) {
	if mode != PDFViewText {
		img, format, err := rasterizePDFPage(path, page)
		if err == nil {
			return Result{Kind: KindImage, Image: img, ImageFormat: format}, nil
		}
		if mode == PDFViewGraphic {
			return Result{Kind: KindUnsupported, Reason: "couldn't render this page as an image (is poppler-utils/pdftoppm installed?)"}, nil
		}
	}

	text, err := extractPDFPageText(path, page)
	switch {
	case err != nil:
		return Result{Kind: KindUnsupported, Reason: "couldn't render or extract text from this PDF page"}, nil
	case strings.TrimSpace(text) == "":
		return Result{Kind: KindUnsupported, Reason: "no extractable text on this page — it may be a scanned image rather than real text"}, nil
	default:
		return Result{Kind: KindText, Content: text}, nil
	}
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
