package sshshell

import (
	"testing"

	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestArgvOmitsPortWhenItIsTheDefault(t *testing.T) {
	got := Argv(remotefs.Connection{Host: "example.com", User: "jens"})
	want := []string{"jens@example.com"}
	if !equalStrings(got, want) {
		t.Errorf("Argv = %v, want %v", got, want)
	}
}

func TestArgvIncludesANonDefaultPort(t *testing.T) {
	got := Argv(remotefs.Connection{Host: "example.com", User: "jens", Port: 2222})
	want := []string{"-p", "2222", "jens@example.com"}
	if !equalStrings(got, want) {
		t.Errorf("Argv = %v, want %v", got, want)
	}
}

func TestArgvIncludesEveryCheckedFlag(t *testing.T) {
	conn := remotefs.Connection{
		Host: "example.com", User: "jens",
		ShellAgentForwarding: true, ShellCompression: true, ShellVerbose: true, ShellX11Forwarding: true,
	}
	got := Argv(conn)
	want := []string{"-A", "-C", "-v", "-X", "jens@example.com"}
	if !equalStrings(got, want) {
		t.Errorf("Argv = %v, want %v", got, want)
	}
}

func TestArgvFallsBackToTheCurrentUsernameWhenNoneIsSet(t *testing.T) {
	got := Argv(remotefs.Connection{Host: "example.com"})
	want := currentUsername() + "@example.com"
	if len(got) != 1 || got[0] != want {
		t.Errorf("Argv = %v, want a single arg %q", got, want)
	}
}

func TestRunReportsLocalErrorWhenSSHBinaryIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	outcome, err := Run(remotefs.Connection{Host: "example.com", User: "jens"})
	if outcome != LocalError {
		t.Errorf("outcome = %v, want LocalError", outcome)
	}
	if err == nil {
		t.Error("err = nil, want a non-nil error when the ssh binary cannot be found")
	}
}
