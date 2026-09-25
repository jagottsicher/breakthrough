package multiplex

import "os/exec"

// lookPath is a package-level swappable var — the same mockable-exec
// idiom internal/firewall's own detect.go already uses — so a test can
// simulate "screen/tmux isn't installed" without needing an actual
// machine that lacks either binary.
var lookPath = exec.LookPath

// ScreenInstalled/TmuxInstalled report whether the real binary is on
// $PATH at all — checked once up front by ListSessions rather than
// inferred from a failed list attempt, so "not installed" and "installed
// but genuinely errored" are never confused with each other.
func ScreenInstalled() bool {
	_, err := lookPath("screen")
	return err == nil
}

func TmuxInstalled() bool {
	_, err := lookPath("tmux")
	return err == nil
}
