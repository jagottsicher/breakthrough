package replace

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// RunSed pipes input through sed and returns its stdout — see the
// package doc for why this never uses sed's own -i. extendedRegex adds
// -E (POSIX extended regex) — must match whatever mode script was
// actually written for: BuildScript's own escaping decisions (literal
// mode) already assume the same flag will be passed here, and an
// advanced/raw script written with ERE syntax (bare "(", "+", "|", ...
// as metacharacters) simply won't parse the way its author intended
// without it.
func RunSed(script string, extendedRegex bool, input []byte) ([]byte, error) {
	var args []string
	if extendedRegex {
		args = append(args, "-E")
	}
	args = append(args, script)

	cmd := exec.Command("sed", args...)
	cmd.Stdin = bytes.NewReader(input)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("sed: %s", msg)
		}
		return nil, fmt.Errorf("sed: %w", err)
	}
	return stdout.Bytes(), nil
}
