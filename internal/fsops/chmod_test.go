package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMode(t *testing.T) {
	tests := []struct {
		in      string
		want    os.FileMode
		wantErr bool
	}{
		{"755", 0o755, false},
		{"0644", 0o644, false},
		{"000", 0, false},
		{"777", 0o777, false},
		{"778", 0, true},  // not a valid octal digit
		{"abc", 0, true},  // not octal at all
		{"4755", 0, true}, // setuid digit: explicitly rejected, not approximated
		{"-1", 0, true},
	}

	for _, tt := range tests {
		got, err := ParseMode(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseMode(%q) = %v, want an error", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseMode(%q): unexpected error %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseMode(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestChmod(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Chmod(path, 0o600); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
}
