package viewer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// generateTestVideo synthesizes a tiny, short test fixture at path via
// ffmpeg's own lavfi testsrc generator — no real video file is checked
// into this repository, so tests that need an actual, mpv-decodable
// video (as opposed to the no-mpv-available path, which only needs a
// file with the right extension) build one on demand instead. Only
// ever called after requireTool(t, "ffmpeg") has already confirmed
// ffmpeg is present — a synthesis-only, test-time dependency, never a
// runtime one for the actual video preview feature itself (see
// LoadVideoThumbnailContext's own doc comment on why mpv alone is
// enough for that).
func generateTestVideo(t *testing.T, path string) {
	t.Helper()
	cmd := exec.Command("ffmpeg",
		"-f", "lavfi", "-i", "testsrc=duration=2:size=64x64:rate=5",
		"-y", path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg (test fixture generation): %v\n%s", err, out)
	}
}

func TestLooksLikeVideoPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"movie.mp4", true},
		{"movie.MKV", true},
		{"clip.webm", true},
		{"/some/dir/video.avi", true},
		{"photo.png", false},
		{"document.pdf", false},
		{"README", false},
		{"video.mp4.txt", false},
	}
	for _, c := range cases {
		if got := LooksLikeVideoPath(c.path); got != c.want {
			t.Errorf("LooksLikeVideoPath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// TestLoadVideoThumbnailWithoutMpv pins LoadVideoThumbnailContext's own
// "no mpv, no thumbnail, no crash" contract — the same PATH-isolation
// approach pdf_test.go's own TestLoadPDFPageFallsBackToTextWithoutPdftoppm
// uses for pdftoppm.
func TestLoadVideoThumbnailWithoutMpv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(path, []byte("not a real video"), 0o644); err != nil {
		t.Fatal(err)
	}

	empty := t.TempDir()
	t.Setenv("PATH", empty)

	if _, _, err := LoadVideoThumbnail(path); err == nil {
		t.Error("LoadVideoThumbnail succeeded with no mpv on $PATH, want an error")
	}
}

// TestLoadVideoThumbnailWithRealMpv only runs where mpv is actually
// installed — requireTool skips it otherwise, the same convention
// TestRasterizePDFPageWithRealPdftoppm uses for pdftoppm.
func TestLoadVideoThumbnailWithRealMpv(t *testing.T) {
	requireTool(t, "mpv")
	requireTool(t, "ffmpeg") // used only to synthesize the test fixture below, not by the code under test

	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mp4")
	generateTestVideo(t, path)

	img, format, err := LoadVideoThumbnail(path)
	if err != nil {
		t.Fatalf("LoadVideoThumbnail: %v", err)
	}
	if format != "png" {
		t.Errorf("format = %q, want %q (--vo-image-format=png)", format, "png")
	}
	if img.Bounds().Dx() == 0 || img.Bounds().Dy() == 0 {
		t.Error("thumbnail has zero-sized bounds")
	}
}

// TestLoadVideoThumbnailContextHonoursCancellation mirrors
// TestLoadPDFPageContextHonoursCancellation exactly, for mpv instead of
// pdftoppm: an already-cancelled context must stop this from ever
// running mpv to completion, so abandoning a video's preview as the
// cursor moves on is actually free, not one more subprocess left
// running invisibly per file passed over.
func TestLoadVideoThumbnailContextHonoursCancellation(t *testing.T) {
	requireTool(t, "mpv")
	requireTool(t, "ffmpeg")

	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mp4")
	generateTestVideo(t, path)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := LoadVideoThumbnailContext(ctx, path); err == nil {
		t.Error("a cancelled context still produced a thumbnail — mpv ran to completion")
	}
}
