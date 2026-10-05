package remotefs

import "testing"

func TestConnectionLabelOmitsUserAndDefaultPort(t *testing.T) {
	c := Connection{Host: "example.com"}
	if got, want := c.Label(), "example.com"; got != want {
		t.Errorf("Label() = %q, want %q", got, want)
	}
}

func TestConnectionLabelIncludesUserAndNonDefaultPort(t *testing.T) {
	c := Connection{Host: "example.com", User: "jens", Port: 2222}
	if got, want := c.Label(), "jens@example.com:2222"; got != want {
		t.Errorf("Label() = %q, want %q", got, want)
	}
}

func TestConnectionLabelOmitsPortWhenItIsExactlyTheDefault(t *testing.T) {
	c := Connection{Host: "example.com", User: "jens", Port: 22}
	if got, want := c.Label(), "jens@example.com"; got != want {
		t.Errorf("Label() = %q, want %q (port 22 is the default, not worth showing)", got, want)
	}
}

func TestConnectionAddrFillsInTheDefaultPort(t *testing.T) {
	c := Connection{Host: "example.com"}
	if got, want := c.Addr(), "example.com:22"; got != want {
		t.Errorf("Addr() = %q, want %q", got, want)
	}
}

func TestConnectionEqualTreatsAnUnsetPortAsTwentyTwo(t *testing.T) {
	a := Connection{Host: "example.com", User: "jens"}
	b := Connection{Host: "example.com", User: "jens", Port: 22}
	if !a.Equal(b) {
		t.Error("Equal(a, b) = false, want true (an unset port and an explicit 22 are the same endpoint)")
	}
}

func TestConnectionEqualDistinguishesDifferentUsersOnTheSameHost(t *testing.T) {
	a := Connection{Host: "example.com", User: "jens"}
	b := Connection{Host: "example.com", User: "root"}
	if a.Equal(b) {
		t.Error("Equal(a, b) = true, want false — different users are different endpoints, even on the same host")
	}
}

// TestConnectionEqualDistinguishesDifferentShellFlags pins the user's
// own explicit request: the same Host/User/Port, opened once with an
// SSH shell flag on and once with it off, must stay two distinct
// history entries rather than being treated as the same connection.
func TestConnectionEqualDistinguishesDifferentShellFlags(t *testing.T) {
	a := Connection{Host: "example.com", User: "jens"}
	b := Connection{Host: "example.com", User: "jens", ShellCompression: true}
	if a.Equal(b) {
		t.Error("Equal(a, b) = true, want false — differing only in ShellCompression must still compare unequal")
	}
}

func TestConnectionEqualIgnoresNothingWhenAllShellFlagsMatch(t *testing.T) {
	a := Connection{Host: "example.com", User: "jens", ShellAgentForwarding: true, ShellVerbose: true}
	b := Connection{Host: "example.com", User: "jens", ShellAgentForwarding: true, ShellVerbose: true}
	if !a.Equal(b) {
		t.Error("Equal(a, b) = false, want true — identical Host/User/Port and identical shell flags are the same endpoint")
	}
}
