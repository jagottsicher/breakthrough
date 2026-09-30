package viewer

import (
	"context"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// VideoExtensions names the file extensions internal/ui treats as "this
// is a video, hand it to mpv" rather than anything this package's own
// Sniff/Load could ever classify — a video container's own binary
// structure gives Sniff nothing to recognize it by (no shared magic
// bytes across formats the way image.DecodeConfig covers a handful of
// picture formats), so unlike KindImage this is a plain extension
// allowlist, the same convention internal/ui's own
// probablyImageExtensions already uses for a format-shaped file this
// package has no decoder for at all.
var VideoExtensions = []string{
	".mp4", ".m4v", ".mkv", ".webm", ".avi", ".mov", ".wmv", ".flv",
	".mpg", ".mpeg", ".m2ts", ".mts", ".ts", ".3gp", ".3g2", ".ogv",
	".vob", ".rm", ".rmvb", ".asf", ".divx", ".f4v",
}

// LooksLikeVideoPath reports whether path's own extension is on
// VideoExtensions, case-insensitively — used by both Look (full-screen
// playback) and the Details sidebar (thumbnail preview) as the one
// shared decision of "is this a video", so the two can never disagree
// about which files qualify.
func LooksLikeVideoPath(path string) bool {
	lower := strings.ToLower(path)
	for _, ext := range VideoExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// VideoThumbnailSeek is the point into a video's own duration
// LoadVideoThumbnailContext seeks to before grabbing a single frame —
// 10%, the same convention ffmpegthumbnailer (and, downstream of it,
// most desktop file managers' own video thumbnailers) defaults to: far
// enough past a video's own opening titles or studio logo — frequently
// black or near-black, and the single most common reason a naive
// "just grab frame 1" thumbnail looks broken — to actually be
// representative, while still scaling with a video's own real length
// instead of a fixed absolute offset that would be overkill for a
// short clip and still too early into a multi-hour one. mpv's own
// --start option accepts this "pp%" form directly, so no separate
// duration probe is needed first.
const VideoThumbnailSeek = "10%"

// LoadVideoThumbnail is LoadVideoThumbnailContext with no
// cancellation — see its own doc comment.
func LoadVideoThumbnail(path string) (image.Image, string, error) {
	return LoadVideoThumbnailContext(context.Background(), path)
}

// LoadVideoThumbnailContext runs mpv on path, seeking to
// VideoThumbnailSeek and writing exactly one decoded frame to a temp
// directory (mpv's own "image" video-output driver — --vo=image —
// does the decoding; no ffmpeg binary or CGO/libmpv binding needed on
// top of the mpv install Look's own full-screen playback already
// requires), then decodes that frame via this package's own
// DecodeImage — from here on a video's thumbnail is just an ordinary
// KindImage Result, reusing every bit of Phase 2's image machinery
// (ScaleForTerminal, renderImageHalfBlocks) exactly like a rasterized
// PDF page already does (see pdf.go's own rasterizePDFPage, which this
// mirrors).
//
// Returns an error if mpv isn't on PATH at all, or fails on this
// particular file — internal/ui's Details sidebar treats either the
// same way pdftoppm's own absence/failure is treated: no preview, not
// a crash. ctx is threaded through to exec.CommandContext so a cursor
// that moves on before this finishes actually kills mpv instead of
// merely discarding its result — see rasterizePDFPage's own doc
// comment on why that matters for a background per-row preview.
func LoadVideoThumbnailContext(ctx context.Context, path string) (image.Image, string, error) {
	if _, err := exec.LookPath("mpv"); err != nil {
		return nil, "", err
	}

	tmpDir, err := os.MkdirTemp("", "breakthrough-video-*")
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }() // best-effort temp-dir cleanup

	cmd := exec.CommandContext(ctx, "mpv",
		"--no-config",
		"--really-quiet",
		"--start="+VideoThumbnailSeek,
		"--frames=1",
		"--vo=image",
		"--vo-image-format=png",
		"--vo-image-outdir="+tmpDir,
		path,
	)
	if err := cmd.Run(); err != nil {
		return nil, "", err
	}

	matches, err := filepath.Glob(filepath.Join(tmpDir, "*.png"))
	if err != nil {
		return nil, "", err
	}
	if len(matches) == 0 {
		return nil, "", fmt.Errorf("%s: mpv produced no thumbnail frame", filepath.Base(path))
	}

	data, err := os.ReadFile(matches[0])
	if err != nil {
		return nil, "", err
	}
	return DecodeImage(data)
}
