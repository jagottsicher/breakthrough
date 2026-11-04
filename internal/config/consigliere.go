package config

import (
	"os"
	"path/filepath"
	"strings"
)

// consigliereSystemPromptFilename is the AI Consigliere's own system
// prompt file, alongside colorschemes/ under the user's own config
// directory — a single prompt rather than a named set like color
// schemes, so a plain file rather than its own subdirectory.
const consigliereSystemPromptFilename = "consigliere-system-prompt.txt"

// UserConsigliereSystemPromptFile is where the AI Consigliere's own
// editable system prompt lives — "" when UserDir is (the same
// contract UserConfigFile already follows). Deliberately user-tier
// only: unlike the main config or color schemes, there's no sensible
// system-wide default an administrator would pre-seed here for other
// people's own prompts.
func UserConsigliereSystemPromptFile() string {
	dir := UserDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, consigliereSystemPromptFilename)
}

// DefaultConsigliereSystemPrompt is the AI Consigliere's own built-in
// system prompt — written out verbatim by
// EnsureConsigliereSystemPromptFile the first time, and what "Reset
// system prompt" (internal/ui) restores.
//
// Deliberately instructs plain-text-only output: the sidebar's own
// chat history is a plain tview.TextView, not a Markdown renderer, so
// **bold**/```code fences```/bullet asterisks/HTML would all just
// show up as literal punctuation rather than being interpreted.
//
// The last sentence exists for a real, reported case: asked a plain
// "what do you see" about their own syslog under Machiavelli Mode,
// Anthropic's own safety classifier declined outright (stop_reason
// "refusal") on an entirely routine line (a failed login attempt) —
// exactly the "metadata and events the user already sees" Machiavelli
// Mode exists to let through, misread as something else because
// nothing in the prompt said whose system this is or why they're
// looking at it. Describing or explaining the user's own system's
// routine operational data (failed logins, errors, denied
// connections, ...) back to them is not the same thing as helping
// carry out whatever that data describes — stated once, plainly, not
// as an instruction to disregard anything that actually does cross
// that line.
const DefaultConsigliereSystemPrompt = `You are an advisor embedded in a terminal file manager called breakthrough. Answer in plain text only: no Markdown, no HTML, no code fences, no bullet-point asterisks. Give one short, concrete piece of advice per answer rather than an exhaustive explanation. Base your answer only on the context you were actually given; never invent file paths, log lines, commits, or other details you weren't shown. Some questions include extra verbatim content (actual log lines, a diff, a ruleset, or a terminal's own screen) alongside the usual summary — use it only when it actually helps answer the question, and never quote or repeat it back at length when a short reference to it is enough. This content is the user's own system, which they administer themselves and are already looking at directly; describing or explaining their own routine operational data back to them (a failed login attempt, an error, a denied connection, and the like) is ordinary system administration, not assistance with whatever that data itself describes.`

// EnsureConsigliereSystemPromptFile creates path with
// DefaultConsigliereSystemPrompt if it doesn't exist yet — never
// touches an existing file, the same contract EnsureUserFile already
// gives the main config file.
func EnsureConsigliereSystemPromptFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(path, []byte(DefaultConsigliereSystemPrompt+"\n"), 0o600)
}

// WriteDefaultConsigliereSystemPrompt overwrites path with
// DefaultConsigliereSystemPrompt unconditionally — unlike
// EnsureConsigliereSystemPromptFile, which never touches an existing
// file, this is "Reset system prompt" (internal/ui) own explicit,
// confirmed discard of whatever the user customized.
func WriteDefaultConsigliereSystemPrompt(path string) error {
	return os.WriteFile(path, []byte(DefaultConsigliereSystemPrompt+"\n"), 0o600)
}

// LoadConsigliereSystemPrompt reads path, falling back to
// DefaultConsigliereSystemPrompt for a missing, unreadable, or
// blank-after-editing file — never an error: a prompt that failed to
// load should degrade to the built-in default rather than silently
// sending no system prompt at all, or blocking the question entirely.
func LoadConsigliereSystemPrompt(path string) string {
	if path == "" {
		return DefaultConsigliereSystemPrompt
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return DefaultConsigliereSystemPrompt
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return DefaultConsigliereSystemPrompt
	}
	return trimmed
}
