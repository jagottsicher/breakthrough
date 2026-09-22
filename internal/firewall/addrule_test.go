package firewall

import "testing"

func TestNewRuleSpecValidateAcceptsAnyPort(t *testing.T) {
	spec := NewRuleSpec{}
	if err := spec.Validate(); err != nil {
		t.Errorf("Validate() with no port set = %v, want nil (any port)", err)
	}
}

func TestNewRuleSpecValidateAcceptsASinglePort(t *testing.T) {
	spec := NewRuleSpec{PortFrom: 22, PortTo: 22}
	if err := spec.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestNewRuleSpecValidateAcceptsARealRange(t *testing.T) {
	spec := NewRuleSpec{PortFrom: 6000, PortTo: 6063}
	if err := spec.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestNewRuleSpecValidateRefusesAnInvertedRange(t *testing.T) {
	spec := NewRuleSpec{PortFrom: 100, PortTo: 50}
	if err := spec.Validate(); err == nil {
		t.Error("Validate() = nil, want an error for PortFrom > PortTo")
	}
}

func TestNewRuleSpecValidateRefusesAHalfSetPort(t *testing.T) {
	// PortTo left at 0 (its own "any port" sentinel) while PortFrom is
	// set is exactly the malformed state HasAnyPort's own "both zero"
	// contract must never silently accept as "any port" instead of
	// catching it as the mistake it actually is.
	spec := NewRuleSpec{PortFrom: 22}
	if err := spec.Validate(); err == nil {
		t.Error("Validate() = nil, want an error when only one of PortFrom/PortTo is set")
	}
}

func TestNewRuleSpecIsSSHRelevantMatchesPort22(t *testing.T) {
	spec := NewRuleSpec{PortFrom: 22, PortTo: 22}
	if !spec.isSSHRelevant() {
		t.Error("a rule naming port 22 should be flagged as SSH-relevant")
	}
}

func TestNewRuleSpecIsSSHRelevantMatchesARangeIncluding22(t *testing.T) {
	spec := NewRuleSpec{PortFrom: 1, PortTo: 1024}
	if !spec.isSSHRelevant() {
		t.Error("a range spanning port 22 should be flagged as SSH-relevant")
	}
}

func TestNewRuleSpecIsSSHRelevantMatchesAnyPort(t *testing.T) {
	spec := NewRuleSpec{}
	if !spec.isSSHRelevant() {
		t.Error("a rule with no port restriction at all blocks port 22 too, and should be flagged")
	}
}

func TestNewRuleSpecIsSSHRelevantIgnoresAnUnrelatedPort(t *testing.T) {
	spec := NewRuleSpec{PortFrom: 80, PortTo: 80}
	if spec.isSSHRelevant() {
		t.Error("a rule naming only port 80 should not be flagged as SSH-relevant")
	}
}

func TestNewRuleSpecIsSSHRelevantIgnoresUDP(t *testing.T) {
	// SSH only ever runs over tcp — a rule pinned to udp specifically,
	// even with no port restriction, can never affect it.
	spec := NewRuleSpec{Protocol: "udp"}
	if spec.isSSHRelevant() {
		t.Error("a udp-only rule should never be flagged as SSH-relevant")
	}
}

func TestPortRangeArgUsesTheGivenSeparatorOnlyForARealRange(t *testing.T) {
	if got, want := portRangeArg(22, 22, "-"), "22"; got != want {
		t.Errorf("portRangeArg(22, 22, \"-\") = %q, want %q", got, want)
	}
	if got, want := portRangeArg(6000, 6063, "-"), "6000-6063"; got != want {
		t.Errorf("portRangeArg(6000, 6063, \"-\") = %q, want %q", got, want)
	}
	if got, want := portRangeArg(6000, 6063, ":"), "6000:6063"; got != want {
		t.Errorf("portRangeArg(6000, 6063, \":\") = %q, want %q", got, want)
	}
}
