package sshkeys

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadPublicKeyLine(t *testing.T) {
	dir := t.TempDir()
	const line = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample jens@host"
	if err := os.WriteFile(filepath.Join(dir, "id_ed25519.pub"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := ReadPublicKeyLine(dir, "id_ed25519")
	if err != nil {
		t.Fatalf("ReadPublicKeyLine: %v", err)
	}
	if got != line {
		t.Errorf("ReadPublicKeyLine() = %q, want %q", got, line)
	}
}

func TestReadPublicKeyLineRejectsEmptyFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "id_ed25519.pub"), []byte(""), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ReadPublicKeyLine(dir, "id_ed25519"); err == nil {
		t.Error("ReadPublicKeyLine should reject an empty .pub file")
	}
}

func TestReadPublicKeyLineRejectsMultipleLines(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "id_ed25519.pub"), []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ReadPublicKeyLine(dir, "id_ed25519"); err == nil {
		t.Error("ReadPublicKeyLine should reject a .pub file with more than one line")
	}
}

func TestInstallScriptChecksForDuplicateBeforeAppending(t *testing.T) {
	script := InstallScript("ssh-ed25519 AAAA jens@host")
	if !strings.Contains(script, "grep -qxF") {
		t.Error("InstallScript must check for an existing exact match before appending")
	}
	if !strings.Contains(script, "ALREADY_PRESENT") {
		t.Error("InstallScript must report ALREADY_PRESENT rather than appending a duplicate")
	}
}

func TestInstallScriptBacksUpBeforeAppending(t *testing.T) {
	script := InstallScript("ssh-ed25519 AAAA jens@host")
	backupIdx := strings.Index(script, "cp -p")
	appendIdx := strings.Index(script, ">> ~/.ssh/authorized_keys")
	if backupIdx < 0 || appendIdx < 0 || backupIdx > appendIdx {
		t.Errorf("InstallScript must back up authorized_keys (cp -p) before appending to it: %s", script)
	}
}

func TestInstallScriptSetsStrictPermissions(t *testing.T) {
	script := InstallScript("ssh-ed25519 AAAA jens@host")
	for _, want := range []string{"chmod 700 ~/.ssh", "chmod 600 ~/.ssh/authorized_keys"} {
		if !strings.Contains(script, want) {
			t.Errorf("InstallScript is missing %q", want)
		}
	}
}

func TestInstallCommand(t *testing.T) {
	got, err := InstallCommand("example.com", 0, "jens", "ssh-ed25519 AAAA jens@host")
	if err != nil {
		t.Fatalf("InstallCommand: %v", err)
	}
	if !strings.HasPrefix(got, "ssh ") {
		t.Errorf("InstallCommand() = %q, want it to start with \"ssh \"", got)
	}
	if !strings.Contains(got, "'jens@example.com'") {
		t.Errorf("InstallCommand() = %q, want it to target jens@example.com", got)
	}
	// "-p " also legitimately appears inside the embedded script itself
	// ("cp -p" backs up authorized_keys) — only the ssh invocation's own
	// leading portion, before the remote command argument, may never
	// carry a port flag for the default port.
	sshInvocation, _, _ := strings.Cut(got, " 'jens@example.com'")
	if strings.Contains(sshInvocation, "-p ") {
		t.Errorf("InstallCommand() ssh invocation = %q, must not add -p for the default port", sshInvocation)
	}
}

func TestInstallCommandAddsPortForNonDefault(t *testing.T) {
	got, err := InstallCommand("example.com", 2222, "jens", "ssh-ed25519 AAAA jens@host")
	if err != nil {
		t.Fatalf("InstallCommand: %v", err)
	}
	if !strings.Contains(got, "-p 2222") {
		t.Errorf("InstallCommand() = %q, want it to include -p 2222", got)
	}
}

func TestInstallCommandRejectsEmptyHostOrUser(t *testing.T) {
	if _, err := InstallCommand("", 0, "jens", "ssh-ed25519 AAAA"); err == nil {
		t.Error("InstallCommand should reject an empty host")
	}
	if _, err := InstallCommand("example.com", 0, "", "ssh-ed25519 AAAA"); err == nil {
		t.Error("InstallCommand should reject an empty user")
	}
}

func TestTestCommandForcesTheGivenKeyOnly(t *testing.T) {
	got, err := TestCommand("example.com", 0, "jens", "/home/jens/.ssh/id_ed25519")
	if err != nil {
		t.Fatalf("TestCommand: %v", err)
	}
	for _, want := range []string{"BatchMode=yes", "IdentitiesOnly=yes", "PasswordAuthentication=no", "-i '/home/jens/.ssh/id_ed25519'"} {
		if !strings.Contains(got, want) {
			t.Errorf("TestCommand() = %q, missing %q", got, want)
		}
	}
}

func TestTestCommandRejectsEmptyPrivateKeyPath(t *testing.T) {
	if _, err := TestCommand("example.com", 0, "jens", ""); err == nil {
		t.Error("TestCommand should reject an empty private key path")
	}
}

// TestInstallScriptAgainstARealShell runs InstallScript through a real
// sh, not just string-inspects it — the same "verified against a real
// shell, not just its own quoting logic" bar FuzzShellQuoteArg already
// holds this package's own quoting to. Covers the one thing string
// inspection can't: that grep -qxF's own idempotency check actually
// works end to end, including a comment with an embedded single quote
// (the exact character shellQuoteArg's own escaping exists for).
func TestInstallScriptAgainstARealShell(t *testing.T) {
	home := t.TempDir()
	script := InstallScript("ssh-ed25519 AAAA jens's key comment")
	run := func() ([]byte, error) {
		cmd := exec.Command("sh", "-c", script)
		cmd.Env = append(os.Environ(), "HOME="+home)
		return cmd.CombinedOutput()
	}

	out, err := run()
	if err != nil {
		t.Fatalf("first run failed: %v (%s)", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "ADDED" {
		t.Errorf("first run output = %q, want ADDED", got)
	}

	want, err := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	if err != nil {
		t.Fatalf("read authorized_keys: %v", err)
	}
	if !strings.Contains(string(want), "jens's key comment") {
		t.Errorf("authorized_keys = %q, want the embedded-quote comment intact", want)
	}

	out, err = run()
	if err != nil {
		t.Fatalf("second run failed: %v (%s)", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "ALREADY_PRESENT" {
		t.Errorf("second run output = %q, want ALREADY_PRESENT (idempotent)", got)
	}
	got, err := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	if err != nil {
		t.Fatalf("read authorized_keys after second run: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("authorized_keys changed on a second, idempotent run: %q vs %q", got, want)
	}
}
