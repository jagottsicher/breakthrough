package compare

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Available reports whether the system's own diff(1) can be found on
// $PATH — the same LookPath check internal/search's own
// ZgrepAvailable/LocateAvailable already make for their own external
// tools, so a caller can hide the "Show diff" action entirely rather
// than offer something that would just fail.
func Available() bool {
	_, err := exec.LookPath("diff")
	return err == nil
}

// UnifiedDiff runs `diff -u a b` and returns its output. diff(1) exits
// 1 — not an error — the moment the two files differ at all; only an
// exit code other than 0 or 1 (missing binary, unreadable file, a
// directory passed where a file was expected, ...) is reported as err.
// identical is true only for a real exit-0 "no difference", output
// empty in that case.
func UnifiedDiff(a, b string) (output string, identical bool, err error) {
	cmd := exec.Command("diff", "-u", a, b)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if runErr == nil {
		return "", true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) && exitErr.ExitCode() == 1 {
		return stdout.String(), false, nil
	}
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		return "", false, fmt.Errorf("diff: %s", msg)
	}
	return "", false, fmt.Errorf("diff: %w", runErr)
}
