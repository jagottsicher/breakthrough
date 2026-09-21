package firewall

import (
	"strings"
	"testing"
)

func TestParseServices(t *testing.T) {
	const fixture = `# /etc/services fixture
ssh             22/tcp
ssh             22/udp                 # rarely used
http            80/tcp          www www-http
https           443/tcp
# a comment-only line
   ` + `
malformed-line-without-port
tcpmux          1/tcp                   # first name wins below
sink            1/tcp
`
	lookup, err := parseServices(strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("parseServices: %v", err)
	}

	cases := []struct {
		key  string
		want string
	}{
		{"22/tcp", "ssh"},
		{"22/udp", "ssh"},
		{"80/tcp", "http"},
		{"443/tcp", "https"},
		{"1/tcp", "tcpmux"}, // first name wins over "sink"
	}
	for _, c := range cases {
		if got := lookup[c.key]; got != c.want {
			t.Errorf("lookup[%q] = %q, want %q", c.key, got, c.want)
		}
	}
	if _, ok := lookup["not-a-port/tcp"]; ok {
		t.Error("malformed line should not have produced a lookup entry")
	}
}

func TestServiceLookupName(t *testing.T) {
	lookup := ServiceLookup{"22/tcp": "ssh", "53/udp": "domain"}

	if got := lookup.Name(22, "tcp"); got != "ssh" {
		t.Errorf("Name(22, tcp) = %q, want ssh", got)
	}
	if got := lookup.Name(22, ""); got != "ssh" {
		t.Errorf("Name(22, \"\") = %q, want ssh (protocol should default to tcp)", got)
	}
	if got := lookup.Name(53, "udp"); got != "domain" {
		t.Errorf("Name(53, udp) = %q, want domain", got)
	}
	if got := lookup.Name(9999, "tcp"); got != "" {
		t.Errorf("Name(9999, tcp) = %q, want empty for an unknown port", got)
	}
}
