package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// SubmoduleState is the leading status character `git submodule
// status` puts on every line — see git-submodule(1)'s own "status"
// section for the full meaning of each one.
type SubmoduleState int

const (
	SubmoduleUpToDate SubmoduleState = iota
	SubmoduleModified
	SubmoduleUninitialized
	SubmoduleConflict
)

// Submodule is one line of `git submodule status` — Describe is
// whatever `git describe` resolved the submodule's own checked-out
// commit to (its own branch/tag-relative name, in parens in the raw
// output), empty for an uninitialized submodule since there's no
// checked-out commit to describe at all.
type Submodule struct {
	Path     string
	Hash     string
	Describe string
	State    SubmoduleState
}

// Submodules lists every submodule this repository has — empty, not
// an error, for a repository with none at all (`git submodule
// status` itself exits 0 with no output in that case).
func Submodules(ctx context.Context, root string) ([]Submodule, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "submodule", "status")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("git submodule status: %s", msg)
		}
		return nil, fmt.Errorf("git submodule status: %w", err)
	}

	var submodules []Submodule
	for line := range strings.SplitSeq(stdout.String(), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		sm := Submodule{}
		switch line[0] {
		case '+':
			sm.State = SubmoduleModified
		case '-':
			sm.State = SubmoduleUninitialized
		case 'U':
			sm.State = SubmoduleConflict
		default:
			sm.State = SubmoduleUpToDate
		}
		rest := strings.TrimSpace(line[1:])
		fields := strings.SplitN(rest, " ", 2)
		sm.Hash = fields[0]
		if len(fields) > 1 {
			remainder := fields[1]
			if idx := strings.Index(remainder, " ("); idx != -1 && strings.HasSuffix(remainder, ")") {
				sm.Path = remainder[:idx]
				sm.Describe = remainder[idx+2 : len(remainder)-1]
			} else {
				sm.Path = remainder
			}
		}
		submodules = append(submodules, sm)
	}
	return submodules, nil
}
