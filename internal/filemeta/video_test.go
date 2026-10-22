package filemeta

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIsVideoPath(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"clip.mp4", true},
		{"CLIP.MP4", true},
		{"movie.mkv", true},
		{"movie.webm", true},
		{"photo.jpg", false},
		{"report.docx", false},
	} {
		if got := IsVideoPath(tc.path); got != tc.want {
			t.Errorf("IsVideoPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

const canonicalFfprobeOutput = `{
  "streams": [
    {
      "codec_type": "video",
      "codec_name": "h264",
      "width": 1920,
      "height": 1080
    },
    {
      "codec_type": "audio",
      "codec_name": "aac"
    }
  ],
  "format": {
    "duration": "125.400000"
  }
}`

func TestParseFfprobeOutputCanonical(t *testing.T) {
	got, err := parseFfprobeOutput([]byte(canonicalFfprobeOutput))
	if err != nil {
		t.Fatalf("parseFfprobeOutput() error = %v, want nil", err)
	}
	want := []Field{
		{Label: "Duration", Value: "2:05"},
		{Label: "Codec", Value: "h264"},
		{Label: "Resolution", Value: "1920×1080"},
	}
	if len(got) != len(want) {
		t.Fatalf("parseFfprobeOutput() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("field %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseFfprobeOutputIgnoresAudioOnlyStreams(t *testing.T) {
	got, err := parseFfprobeOutput([]byte(`{"streams":[{"codec_type":"audio","codec_name":"mp3"}],"format":{"duration":"10.0"}}`))
	if err != nil {
		t.Fatalf("parseFfprobeOutput() error = %v, want nil", err)
	}
	for _, f := range got {
		if f.Label == "Codec" || f.Label == "Resolution" {
			t.Errorf("parseFfprobeOutput() included %+v from an audio-only stream, want none", f)
		}
	}
}

func TestParseFfprobeOutputPicksFirstVideoStreamOnly(t *testing.T) {
	got, err := parseFfprobeOutput([]byte(`{
		"streams": [
			{"codec_type": "video", "codec_name": "h264", "width": 1920, "height": 1080},
			{"codec_type": "video", "codec_name": "vp9", "width": 320, "height": 240}
		],
		"format": {"duration": "1.0"}
	}`))
	if err != nil {
		t.Fatalf("parseFfprobeOutput() error = %v, want nil", err)
	}
	codecCount := 0
	for _, f := range got {
		if f.Label == "Codec" {
			codecCount++
			if f.Value != "h264" {
				t.Errorf("Codec = %q, want %q (the first video stream)", f.Value, "h264")
			}
		}
	}
	if codecCount != 1 {
		t.Errorf("got %d Codec fields, want exactly 1", codecCount)
	}
}

func TestParseFfprobeOutputEmptyDurationAndNoVideoStream(t *testing.T) {
	got, err := parseFfprobeOutput([]byte(`{"streams":[],"format":{}}`))
	if err != nil {
		t.Fatalf("parseFfprobeOutput() error = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Errorf("parseFfprobeOutput() = %+v, want empty", got)
	}
}

func TestParseFfprobeOutputMalformedJSON(t *testing.T) {
	if _, err := parseFfprobeOutput([]byte("not json at all")); err == nil {
		t.Error("parseFfprobeOutput on malformed input: want an error, got nil")
	}
}

func TestFormatVideoDuration(t *testing.T) {
	for _, tc := range []struct {
		secs float64
		want string
	}{
		{0, "0:00"},
		{59, "0:59"},
		{60, "1:00"},
		{125.4, "2:05"},
		{3600, "1:00:00"},
		{3725, "1:02:05"},
	} {
		if got := formatVideoDuration(tc.secs); got != tc.want {
			t.Errorf("formatVideoDuration(%v) = %q, want %q", tc.secs, got, tc.want)
		}
	}
}

// TestForVideoRealFileWithFfprobe mirrors TestForPDFRealFile's own
// shape exactly: generates a tiny, real video with ffmpeg (skipped if
// either tool isn't installed) rather than hand-constructing one byte
// by byte — unlike a minimal PDF (plain text) or JPEG (a handful of
// documented markers), a video container's own compressed stream data
// genuinely can't be hand-built, so generating one with the real
// encoder is this format's own equivalent of ForPDFRealFile's
// buildMinimalPDFWithInfo.
func TestForVideoRealFileWithFfprobe(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skipf("ffmpeg not available in this environment: %v", err)
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skipf("ffprobe not available in this environment: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mp4")
	cmd := exec.Command("ffmpeg",
		"-y", "-v", "quiet",
		"-f", "lavfi", "-i", "testsrc=duration=2:size=320x240:rate=10",
		"-pix_fmt", "yuv420p",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg (generating the test fixture): %v\n%s", err, out)
	}

	got, err := ForVideo(path)
	if err != nil {
		t.Fatalf("ForVideo() error = %v, want nil", err)
	}

	foundResolution, foundDuration := false, false
	for _, f := range got {
		switch f.Label {
		case "Resolution":
			foundResolution = f.Value == "320×240"
		case "Duration":
			foundDuration = f.Value != ""
		}
	}
	if !foundResolution {
		t.Errorf("ForVideo() = %+v, want Resolution=320×240", got)
	}
	if !foundDuration {
		t.Errorf("ForVideo() = %+v, want a non-empty Duration", got)
	}
}

func TestForVideoViaForPath(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skipf("ffmpeg not available in this environment: %v", err)
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skipf("ffprobe not available in this environment: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mkv")
	cmd := exec.Command("ffmpeg",
		"-y", "-v", "quiet",
		"-f", "lavfi", "-i", "testsrc=duration=1:size=160x120:rate=5",
		"-pix_fmt", "yuv420p",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg (generating the test fixture): %v\n%s", err, out)
	}

	got, err := ForPath(path)
	if err != nil {
		t.Fatalf("ForPath() error = %v, want nil", err)
	}
	found := false
	for _, f := range got {
		if f.Label == "Resolution" && f.Value == "160×120" {
			found = true
		}
	}
	if !found {
		t.Errorf("ForPath() = %+v, want it to have dispatched to ForVideo and found Resolution=160×120", got)
	}
}
