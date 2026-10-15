package filemeta

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrPopplerUtilsMissing means pdfinfo itself isn't on PATH — distinct
// from ErrNoMetadata/ErrNotSupported (both routine, expected outcomes
// for an ordinary file): this one names something installable, the
// same "(install poppler-utils)" framing internal/viewer's own
// rasterizePDFPage already uses for the identical gap on the preview
// side.
var ErrPopplerUtilsMissing = errors.New("pdfinfo not found (install poppler-utils)")

// pdfinfoFieldLabels maps pdfinfo's own "Label:   value" line labels to
// this package's display labels — a fixed, small subset (see
// ForPDF's own doc comment), in the order feature_ideas.txt #26
// decided on. Unlisted labels (ModDate aside — included for the same
// reason Office's own Created/Modified pair both get shown) are
// ignored outright: Tagged/Form/PDF version/page size/file size/... are
// pdfinfo-internal technical detail, not document metadata a user
// pressing "M" is looking for.
var pdfinfoFieldLabels = []struct{ raw, display string }{
	{"Title", "Title"},
	{"Author", "Author"},
	{"Creator", "Creator"},
	{"Producer", "Producer"},
	{"CreationDate", "Created"},
	{"ModDate", "Modified"},
	{"Keywords", "Keywords"},
	{"Pages", "Pages"},
}

// ForPDF reads path's own document info dictionary via pdfinfo (from
// poppler-utils — the same optional dependency internal/viewer's own
// pdftoppm-based page preview already uses, see
// internal/viewer/pdf.go), rather than a new PDF-parsing dependency:
// pdfinfo already prints exactly the fields a reader cares about
// (Title/Author/Creator/Producer/CreationDate/Keywords/page count) as
// plain "Label: value" text, with no need to parse the PDF's own object
// graph ourselves.
func ForPDF(path string) ([]Field, error) {
	return ForPDFContext(context.Background(), path)
}

// ForPDFContext is ForPDF with cancellation — mirrors
// internal/viewer's own LoadPDFPageContext in spirit, though a plain
// context.Background() call is all fetchDetailsMetadata actually uses
// today (see its own doc comment on why metadata reads don't need the
// same debounce/cancel machinery the preview pipeline does); exported
// now rather than added later specifically so a future caller that does
// need it (a batch "M" over several files, say) doesn't have to touch
// this function's own signature.
func ForPDFContext(ctx context.Context, path string) ([]Field, error) {
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		return nil, ErrPopplerUtilsMissing
	}

	cmd := exec.CommandContext(ctx, "pdfinfo", path)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("pdfinfo: %w", err)
	}

	fields := parsePdfinfoOutput(out.String())
	if len(fields) == 0 {
		return nil, ErrNoMetadata
	}
	return fields, nil
}

// parsePdfinfoOutput is pulled out as its own pure function so a test
// can cover the parsing logic itself against a canned string, without
// needing pdfinfo actually installed (see ForPDFContext's own real,
// shelling-out test, which skips if it isn't).
func parsePdfinfoOutput(output string) []Field {
	byRaw := make(map[string]string, len(pdfinfoFieldLabels))
	for _, line := range strings.Split(output, "\n") {
		i := strings.Index(line, ":")
		if i < 0 {
			continue
		}
		label := strings.TrimSpace(line[:i])
		value := strings.TrimSpace(line[i+1:])
		if value == "" {
			continue
		}
		byRaw[label] = value
	}

	var fields []Field
	for _, m := range pdfinfoFieldLabels {
		if v, ok := byRaw[m.raw]; ok {
			fields = append(fields, Field{Label: m.display, Value: v})
		}
	}
	return fields
}
