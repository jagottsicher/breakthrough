// Package consigliere is the AI Consigliere's own business logic —
// the text-advice-only chat feature reachable via "gC" (see
// internal/ui/keymap.go) from any view, not wired to a real LLM
// provider yet (see feature_ideas.txt's own "AI Consigliere" entry for
// the full spec this first slice only partially builds).
//
// This first slice ships only Provider and Mock: the request/response
// shape a real provider (Claude/ChatGPT/DeepSeek/a local OpenAI-
// compatible endpoint) will later satisfy, so the UI panel it drives
// (internal/ui/consigliere.go) is built against the real seam from the
// start instead of an inline placeholder that would need tearing out
// once a real provider lands. Mock never leaves the process and makes
// no network call — it exists purely to exercise that seam and let the
// panel's own layout/scrolling be judged before any provider,
// API-key-storage, or consent-checkbox decision (all still open per
// the spec) is made.
package consigliere
