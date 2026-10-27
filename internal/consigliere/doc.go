// Package consigliere is the AI Consigliere's own business logic —
// the text-advice-only chat feature reachable via "gC" (see
// internal/ui/keymap.go) from any view (see feature_ideas.txt's own
// "AI Consigliere" entry for the full spec).
//
// Provider is the seam every backend satisfies: Anthropic and
// OpenAICompat (the latter shared by OpenAI and DeepSeek, which
// deliberately cloned OpenAI's own Chat Completions API shape, and by
// any local OpenAI-compatible endpoint a user points its BaseURL at
// directly) make the real network call; Mock never leaves the process
// and answers with a fixed placeholder, kept around as a dependency-
// free Provider for exercising the UI panel's own layout/scrolling
// without a network call or API key.
//
// This package never imports internal/apikey: resolving which of the
// three key tiers (env var/file/encrypted store) actually supplies a
// given provider's key is internal/ui's own job (see
// internal/ui/consigliere.go's buildConsigliereProvider) — every real
// Provider here just takes a KeyFunc and calls it right before
// sending the request.
package consigliere
