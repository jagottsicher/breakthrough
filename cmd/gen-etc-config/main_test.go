package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestGenEtcConfigPrintsTheTemplate is a thin smoke test — the real
// content is already pinned by internal/config's own tests
// (DefaultFileTemplate, which this command does nothing but print). This
// only confirms the command itself actually runs and produces that
// output on stdout, which is the one thing internal/config's own tests
// can't see: whether the release pipeline's own generated file would
// come out empty due to a wiring mistake here.
func TestGenEtcConfigPrintsTheTemplate(t *testing.T) {
	out, err := exec.Command("go", "run", ".").Output()
	if err != nil {
		t.Fatalf("running gen-etc-config: %v", err)
	}
	if !strings.Contains(string(out), "# color_scheme = default") {
		t.Errorf("output doesn't look like the config template, got:\n%s", out)
	}
}
