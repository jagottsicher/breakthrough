package sshkeys

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateSpecValidate(t *testing.T) {
	cases := []struct {
		name    string
		spec    GenerateSpec
		wantErr bool
	}{
		{"ed25519 needs no bits", GenerateSpec{Algorithm: "ed25519", Filename: "id_ed25519"}, false},
		{"rsa 4096 valid", GenerateSpec{Algorithm: "rsa", Bits: 4096, Filename: "id_rsa"}, false},
		{"rsa 1024 rejected", GenerateSpec{Algorithm: "rsa", Bits: 1024, Filename: "id_rsa"}, true},
		{"ecdsa 256 valid", GenerateSpec{Algorithm: "ecdsa", Bits: 256, Filename: "id_ecdsa"}, false},
		{"ecdsa 999 rejected", GenerateSpec{Algorithm: "ecdsa", Bits: 999, Filename: "id_ecdsa"}, true},
		{"dsa rejected outright", GenerateSpec{Algorithm: "dsa", Filename: "id_dsa"}, true},
		{"empty filename rejected", GenerateSpec{Algorithm: "ed25519", Filename: ""}, true},
		{"path separator rejected", GenerateSpec{Algorithm: "ed25519", Filename: "sub/id_ed25519"}, true},
		{"dot rejected", GenerateSpec{Algorithm: "ed25519", Filename: "."}, true},
		{"dotdot rejected", GenerateSpec{Algorithm: "ed25519", Filename: ".."}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.spec.Validate()
			if (err != nil) != c.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}

func TestGenerateCommandEd25519(t *testing.T) {
	got, err := GenerateCommand("/home/jens/.ssh", GenerateSpec{Algorithm: "ed25519", Filename: "id_ed25519", Comment: "jens@host"})
	if err != nil {
		t.Fatalf("GenerateCommand: %v", err)
	}
	want := "ssh-keygen -t ed25519 -f '/home/jens/.ssh/id_ed25519' -C 'jens@host'"
	if got != want {
		t.Errorf("GenerateCommand() = %q, want %q", got, want)
	}
}

func TestGenerateCommandRSAIncludesBits(t *testing.T) {
	got, err := GenerateCommand("/home/jens/.ssh", GenerateSpec{Algorithm: "rsa", Bits: 4096, Filename: "id_rsa"})
	if err != nil {
		t.Fatalf("GenerateCommand: %v", err)
	}
	want := "ssh-keygen -t rsa -b 4096 -f '/home/jens/.ssh/id_rsa'"
	if got != want {
		t.Errorf("GenerateCommand() = %q, want %q", got, want)
	}
}

func TestGenerateCommandNeverEmitsDashN(t *testing.T) {
	got, err := GenerateCommand("/home/jens/.ssh", GenerateSpec{Algorithm: "ed25519", Filename: "id_ed25519"})
	if err != nil {
		t.Fatalf("GenerateCommand: %v", err)
	}
	if strings.Contains(got, "-N") {
		t.Errorf("GenerateCommand() = %q, must never pass a passphrase (-N) on the command line", got)
	}
}

func TestGenerateCommandQuotesACommentWithAnEmbeddedQuote(t *testing.T) {
	got, err := GenerateCommand("/home/jens/.ssh", GenerateSpec{Algorithm: "ed25519", Filename: "id_ed25519", Comment: "jens's laptop"})
	if err != nil {
		t.Fatalf("GenerateCommand: %v", err)
	}
	want := `ssh-keygen -t ed25519 -f '/home/jens/.ssh/id_ed25519' -C 'jens'"'"'s laptop'`
	if got != want {
		t.Errorf("GenerateCommand() = %q, want %q", got, want)
	}
}

func TestGenerateCommandRejectsAnInvalidSpec(t *testing.T) {
	if _, err := GenerateCommand("/home/jens/.ssh", GenerateSpec{Algorithm: "rsa", Bits: 1234, Filename: "id_rsa"}); err == nil {
		t.Error("GenerateCommand should reject an invalid spec rather than build a broken command")
	}
}

func TestFilenameAvailable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "id_ed25519.pub"), []byte("ssh-ed25519 AAAA\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := FilenameAvailable(dir, "id_ed25519"); err == nil {
		t.Error("FilenameAvailable should reject a name whose .pub half already exists")
	}
	if err := FilenameAvailable(dir, "id_rsa"); err != nil {
		t.Errorf("FilenameAvailable(id_rsa) = %v, want nil for a name that doesn't exist yet", err)
	}
}
