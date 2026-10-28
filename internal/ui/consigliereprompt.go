package ui

import (
	"fmt"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// consigliereSystemPromptPath is where the AI Consigliere's own
// system prompt file lives — a swappable var (like userConfigFilePath/
// apiKeyStorePath) so a test never reads or writes a real
// ~/.config/breakthrough/consigliere-system-prompt.txt on the machine
// running `go test`.
var consigliereSystemPromptPath = config.UserConsigliereSystemPromptFile

// editConsigliereSystemPrompt is the "Edit system prompt" button's own
// action — creates the file with config.DefaultConsigliereSystemPrompt
// if it doesn't exist yet (never touching one that already does, the
// same contract editConfigFile's own EnsureUserFile call follows for
// the main config file), then hands it to the user's own editor.
// Nothing to reload afterwards: submitConsigliereMessage reads this
// file fresh on every question, there's no in-memory copy to go stale.
func (r *Root) editConsigliereSystemPrompt() {
	path := consigliereSystemPromptPath()
	if path == "" {
		r.showError(fmt.Errorf("no user config directory available to write a system prompt file to"))
		return
	}
	if err := config.EnsureConsigliereSystemPromptFile(path); err != nil {
		r.showError(fmt.Errorf("creating %s: %w", path, err))
		return
	}
	r.runEditor(path, 0)
}

// resetConsigliereSystemPrompt is the "Reset system prompt" button's
// own action — overwrites the file with
// config.DefaultConsigliereSystemPrompt outright, after the same
// explicit confirmation every other irreversible Options action
// requires (see resetCurrentOptionCategory/resetAllOptions), since
// this discards whatever the user customized rather than just
// stopping an override the way a normal setting's reset does.
func (r *Root) resetConsigliereSystemPrompt() {
	r.openConfirm(
		"Reset the AI Consigliere's own system prompt to its built-in default? Your own customized text will be lost.",
		"Yes, reset it",
		func() {
			path := consigliereSystemPromptPath()
			if path == "" {
				r.showError(fmt.Errorf("no user config directory available to write a system prompt file to"))
				return
			}
			if err := config.WriteDefaultConsigliereSystemPrompt(path); err != nil {
				r.showError(fmt.Errorf("resetting %s: %w", path, err))
			}
		},
	)
}
