package firewall

import "testing"

func TestRunningOverSSHTrueWhenSSHConnectionSet(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "10.0.0.1 22 10.0.0.2 22")
	t.Setenv("SSH_TTY", "")
	if !RunningOverSSH() {
		t.Error("RunningOverSSH() = false, want true with SSH_CONNECTION set")
	}
}

func TestRunningOverSSHTrueWhenSSHTTYSet(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "/dev/pts/0")
	if !RunningOverSSH() {
		t.Error("RunningOverSSH() = false, want true with SSH_TTY set")
	}
}

func TestRunningOverSSHFalseWhenNeitherSet(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	if RunningOverSSH() {
		t.Error("RunningOverSSH() = true, want false with neither SSH_CONNECTION nor SSH_TTY set")
	}
}

func TestAddRuleCommandDispatchesByBackend(t *testing.T) {
	spec := NewRuleSpec{PortFrom: 80, PortTo: 80, Protocol: "tcp"}

	got, err := AddRuleCommand(BackendUFW, spec)
	if err != nil {
		t.Fatalf("AddRuleCommand(BackendUFW): %v", err)
	}
	want, _ := UFWAddRuleCommand(spec)
	if got != want {
		t.Errorf("AddRuleCommand(BackendUFW) = %q, want %q", got, want)
	}

	got, err = AddRuleCommand(BackendIPTables, spec)
	if err != nil {
		t.Fatalf("AddRuleCommand(BackendIPTables): %v", err)
	}
	want, _ = IPTablesAddRuleCommand(spec)
	if got != want {
		t.Errorf("AddRuleCommand(BackendIPTables) = %q, want %q", got, want)
	}

	if _, err := AddRuleCommand(BackendNFTables, spec); err == nil {
		t.Error("AddRuleCommand(BackendNFTables) = nil error, want a refusal (see NFTAddRuleCommand)")
	}
	if _, err := AddRuleCommand(BackendNone, spec); err == nil {
		t.Error("AddRuleCommand(BackendNone) = nil error, want a refusal (no active backend)")
	}
}

func TestDeleteRuleCommandDispatchesByBackend(t *testing.T) {
	spec := NewRuleSpec{PortFrom: 80, PortTo: 80, Protocol: "tcp"}

	got, err := DeleteRuleCommand(BackendUFW, spec)
	if err != nil {
		t.Fatalf("DeleteRuleCommand(BackendUFW): %v", err)
	}
	want, _ := UFWDeleteRuleCommand(spec)
	if got != want {
		t.Errorf("DeleteRuleCommand(BackendUFW) = %q, want %q", got, want)
	}

	got, err = DeleteRuleCommand(BackendIPTables, spec)
	if err != nil {
		t.Fatalf("DeleteRuleCommand(BackendIPTables): %v", err)
	}
	want, _ = IPTablesDeleteRuleCommand(spec)
	if got != want {
		t.Errorf("DeleteRuleCommand(BackendIPTables) = %q, want %q", got, want)
	}

	if _, err := DeleteRuleCommand(BackendNFTables, spec); err == nil {
		t.Error("DeleteRuleCommand(BackendNFTables) = nil error, want a refusal (see NFTAddRuleCommand)")
	}
	if _, err := DeleteRuleCommand(BackendNone, spec); err == nil {
		t.Error("DeleteRuleCommand(BackendNone) = nil error, want a refusal (no active backend)")
	}
}

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
	if !spec.IsSSHRelevant() {
		t.Error("a rule naming port 22 should be flagged as SSH-relevant")
	}
}

func TestNewRuleSpecIsSSHRelevantMatchesARangeIncluding22(t *testing.T) {
	spec := NewRuleSpec{PortFrom: 1, PortTo: 1024}
	if !spec.IsSSHRelevant() {
		t.Error("a range spanning port 22 should be flagged as SSH-relevant")
	}
}

func TestNewRuleSpecIsSSHRelevantMatchesAnyPort(t *testing.T) {
	spec := NewRuleSpec{}
	if !spec.IsSSHRelevant() {
		t.Error("a rule with no port restriction at all blocks port 22 too, and should be flagged")
	}
}

func TestNewRuleSpecIsSSHRelevantIgnoresAnUnrelatedPort(t *testing.T) {
	spec := NewRuleSpec{PortFrom: 80, PortTo: 80}
	if spec.IsSSHRelevant() {
		t.Error("a rule naming only port 80 should not be flagged as SSH-relevant")
	}
}

func TestNewRuleSpecIsSSHRelevantIgnoresUDP(t *testing.T) {
	// SSH only ever runs over tcp — a rule pinned to udp specifically,
	// even with no port restriction, can never affect it.
	spec := NewRuleSpec{Protocol: "udp"}
	if spec.IsSSHRelevant() {
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
