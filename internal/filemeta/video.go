package filemeta

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"time"

	"github.com/jagottsicher/breakthrough/internal/viewer"
)

// ErrFfprobeMissing means ffprobe itself isn't on PATH — distinct from
// ErrNoMetadata/ErrNotSupported (both routine, expected outcomes for
// an ordinary file), the same "name something installable" framing
// ErrPopplerUtilsMissing already has for pdfinfo. internal/viewer's
// own mpv-based thumbnail pipeline doesn't cover this: a thumbnail is
// a decoded frame, not duration/codec/resolution, and mpv has no
// equivalent "just print the metadata" mode the way ffprobe does.
var ErrFfprobeMissing = errors.New("ffprobe not found (install ffmpeg)")

// IsVideoPath reports whether path's extension is one ForVideo (and
// ForPath, which dispatches to it) handles — exactly
// viewer.LooksLikeVideoPath's own allowlist, reused rather than
// duplicated so Look/the Details sidebar's thumbnail preview and this
// package's own metadata extraction can never disagree about which
// files qualify as "a video" (see LooksLikeVideoPath's own doc
// comment, which already states that shared-decision reasoning).
func IsVideoPath(path string) bool {
	return viewer.LooksLikeVideoPath(path)
}

// ffprobeOutput is the handful of fields this package actually reads
// out of `ffprobe -show_format -show_streams -print_format json`'s own
// much larger output — codec_type/codec_name/width/height for the
// first video stream, duration for the container overall. Every field
// ffprobe can report beyond these (bitrate, frame rate, audio
// streams, metadata tags, ...) is deliberately left unparsed: a
// user pressing "M" on a video is looking for the same handful of
// facts ForPDF/ForOfficeOpenXML already settled on for their own
// formats, not a full technical dump.
type ffprobeOutput struct {
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	} `json:"streams"`
}

// ForVideo reads path's own container/stream metadata via ffprobe —
// see ForVideoContext.
func ForVideo(path string) ([]Field, error) {
	return ForVideoContext(context.Background(), path)
}

// ForVideoContext is ForVideo with cancellation — mirrors
// ForPDFContext's own shape and reasoning (a future batch "M" over
// several files wouldn't need this function's own signature touched).
func ForVideoContext(ctx context.Context, path string) ([]Field, error) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return nil, ErrFfprobeMissing
	}

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffprobe: %w", err)
	}

	fields, err := parseFfprobeOutput(out.Bytes())
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, ErrNoMetadata
	}
	return fields, nil
}

// parseFfprobeOutput is pulled out as its own pure function so a test
// can cover the parsing logic itself against a canned JSON blob,
// without needing ffprobe actually installed — mirrors
// parsePdfinfoOutput's own reasoning exactly.
func parseFfprobeOutput(data []byte) ([]Field, error) {
	var probe ffprobeOutput
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("ffprobe: parse output: %w", err)
	}

	var fields []Field
	if probe.Format.Duration != "" {
		if secs, err := strconv.ParseFloat(probe.Format.Duration, 64); err == nil {
			fields = append(fields, Field{Label: "Duration", Value: formatVideoDuration(secs)})
		}
	}
	// The first video stream is what "the codec"/"the resolution"
	// means to a user pressing "M" — a file can carry more than one
	// (rare: alternate angles, a still-image "cover art" stream some
	// containers also tag as video), but there's exactly one thumbnail
	// frame shown elsewhere for it too (see internal/viewer's own
	// LoadVideoThumbnailContext), so showing more than this first one
	// here would describe streams the preview itself never reflects.
	for _, s := range probe.Streams {
		if s.CodecType != "video" {
			continue
		}
		if s.CodecName != "" {
			fields = append(fields, Field{Label: "Codec", Value: s.CodecName})
		}
		if s.Width > 0 && s.Height > 0 {
			fields = append(fields, Field{Label: "Resolution", Value: fmt.Sprintf("%d×%d", s.Width, s.Height)})
		}
		break
	}
	return fields, nil
}

// formatVideoDuration renders secs as "H:MM:SS" (an hour or longer) or
// "M:SS" (under an hour) — the same two-shape convention a video
// player's own scrubber typically uses, rather than always showing a
// leading "0:" hours place no video under an hour actually has.
func formatVideoDuration(secs float64) string {
	d := time.Duration(secs*float64(time.Second) + 0.5*float64(time.Second))
	hours := d / time.Hour
	d -= hours * time.Hour
	minutes := d / time.Minute
	d -= minutes * time.Minute
	seconds := d / time.Second
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}
