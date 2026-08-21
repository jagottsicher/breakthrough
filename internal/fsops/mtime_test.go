package fsops

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSetModTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	want := time.Date(2020, time.March, 15, 10, 30, 0, 0, time.Local)
	if err := SetModTime(path, want); err != nil {
		t.Fatalf("SetModTime: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.ModTime().Equal(want) {
		t.Errorf("ModTime = %v, want %v", fi.ModTime(), want)
	}
}
