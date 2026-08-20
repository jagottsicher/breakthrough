package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

func TestPermString(t *testing.T) {
	tests := []struct {
		mode os.FileMode
		want string
	}{
		{0o644, "-rw-r--r--"},
		{os.ModeDir | 0o755, "drwxr-xr-x"},
		{os.ModeSymlink | 0o777, "lrwxrwxrwx"},
		{0o600, "-rw-------"},
	}

	for _, tt := range tests {
		if got := permString(tt.mode); got != tt.want {
			t.Errorf("permString(%v) = %q, want %q", tt.mode, got, tt.want)
		}
	}
}

func TestHumanSize(t *testing.T) {
	tests := []struct {
		size int64
		want string
	}{
		{0, "0B"},
		{1023, "1023B"},
		{1024, "1.0K"},
		{1536, "1.5K"},
		{1024 * 1024, "1.0M"},
		{1024 * 1024 * 1024, "1.0G"},
	}

	for _, tt := range tests {
		if got := humanSize(tt.size); got != tt.want {
			t.Errorf("humanSize(%d) = %q, want %q", tt.size, got, tt.want)
		}
	}
}

func TestFormatInfoIncludesLinkTargetOnlyForSymlinks(t *testing.T) {
	file := formatInfo(fsops.Info{Name: "a.txt", ModTime: time.Now()})
	if strings.Contains(file, "Link target") {
		t.Error("a regular file's Info should not mention a link target")
	}

	link := formatInfo(fsops.Info{
		Name:       "b.txt",
		IsSymlink:  true,
		LinkTarget: "/somewhere/else",
		ModTime:    time.Now(),
	})
	wantLinkLine := fmt.Sprintf("%-13s%s", "Link target:", "/somewhere/else")
	if !strings.Contains(link, wantLinkLine) {
		t.Errorf("symlink Info should contain %q, got:\n%s", wantLinkLine, link)
	}
	wantTypeLine := fmt.Sprintf("%-13s%s", "Type:", "symlink")
	if !strings.Contains(link, wantTypeLine) {
		t.Errorf("symlink Info should contain %q, got:\n%s", wantTypeLine, link)
	}
}

func TestTextSize(t *testing.T) {
	width, height := textSize("ab\nabcd\na")
	if width != 6 { // longest line "abcd" (4) + 2 padding
		t.Errorf("width = %d, want 6", width)
	}
	if height != 3 {
		t.Errorf("height = %d, want 3", height)
	}
}
