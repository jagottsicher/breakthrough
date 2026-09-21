package firewall

import "testing"

func TestHasAnyPort(t *testing.T) {
	if !(Rule{}).HasAnyPort() {
		t.Error("zero-value Rule should report HasAnyPort")
	}
	if (Rule{PortFrom: 22, PortTo: 22}).HasAnyPort() {
		t.Error("a rule pinned to port 22 should not report HasAnyPort")
	}
}

func TestPortLabel(t *testing.T) {
	lookup := ServiceLookup{"22/tcp": "ssh"}

	cases := []struct {
		name string
		rule Rule
		want string
	}{
		{"any port", Rule{}, "any"},
		{"single named port", Rule{PortFrom: 22, PortTo: 22, Protocol: "tcp"}, "22 (ssh)"},
		{"single unnamed port", Rule{PortFrom: 51820, PortTo: 51820, Protocol: "udp"}, "51820"},
		{"range", Rule{PortFrom: 6000, PortTo: 6063}, "6000-6063"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.rule.PortLabel(lookup); got != c.want {
				t.Errorf("PortLabel() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestSourceDestinationInterfaceLabelsFallBackToAny(t *testing.T) {
	r := Rule{}
	if got := r.SourceLabel(); got != "any" {
		t.Errorf("SourceLabel() = %q, want %q", got, "any")
	}
	if got := r.DestinationLabel(); got != "any" {
		t.Errorf("DestinationLabel() = %q, want %q", got, "any")
	}
	if got := r.InterfaceLabel(); got != "any" {
		t.Errorf("InterfaceLabel() = %q, want %q", got, "any")
	}

	r = Rule{Source: "10.0.0.0/8", Destination: "192.168.1.5", Interface: "eth0"}
	if got := r.SourceLabel(); got != "10.0.0.0/8" {
		t.Errorf("SourceLabel() = %q, want %q", got, "10.0.0.0/8")
	}
	if got := r.DestinationLabel(); got != "192.168.1.5" {
		t.Errorf("DestinationLabel() = %q, want %q", got, "192.168.1.5")
	}
	if got := r.InterfaceLabel(); got != "eth0" {
		t.Errorf("InterfaceLabel() = %q, want %q", got, "eth0")
	}
}

func TestDirectionAndActionString(t *testing.T) {
	if DirectionIn.String() != "IN" {
		t.Errorf("DirectionIn.String() = %q, want IN", DirectionIn.String())
	}
	if DirectionOut.String() != "OUT" {
		t.Errorf("DirectionOut.String() = %q, want OUT", DirectionOut.String())
	}
	if ActionAllow.String() != "ALLOW" {
		t.Errorf("ActionAllow.String() = %q, want ALLOW", ActionAllow.String())
	}
	if ActionDeny.String() != "DENY" {
		t.Errorf("ActionDeny.String() = %q, want DENY", ActionDeny.String())
	}
	if ActionReject.String() != "REJECT" {
		t.Errorf("ActionReject.String() = %q, want REJECT", ActionReject.String())
	}
}
