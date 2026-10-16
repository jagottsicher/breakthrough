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

func TestConnectionHostPortOmitsTheDefaultPort(t *testing.T) {
	c := Connection{Host: "example.com", User: "jens"}
	if got, want := c.HostPort(), "example.com"; got != want {
		t.Errorf("HostPort() = %q, want %q (User must never appear here)", got, want)
	}
}

func TestConnectionHostPortIncludesANonDefaultPort(t *testing.T) {
	c := Connection{Host: "example.com", Port: 2222}
	if got, want := c.HostPort(), "example.com:2222"; got != want {
		t.Errorf("HostPort() = %q, want %q", got, want)
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

// TestConnectionEqualIgnoresIdentifier pins the one deliberate
// exception Connection's own doc comment calls out: Identifier is
// never part of Equal, so renaming an entry's Identifier (see
// history.go's own RecordAttempt) matches and updates its existing
// history entry instead of creating a duplicate alongside it.
func TestConnectionEqualIgnoresIdentifier(t *testing.T) {
	a := Connection{Host: "example.com", User: "jens", Identifier: "build-1"}
	b := Connection{Host: "example.com", User: "jens", Identifier: "build-2"}
	if !a.Equal(b) {
		t.Error("Equal(a, b) = false, want true — differing only in Identifier must still compare equal")
	}
}
