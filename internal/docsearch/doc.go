// Package docsearch is a small, dependency-free lexical (BM25) search
// over breakthrough's own embedded documentation (see docsembed and
// NewDefaultIndex) — the AI Consigliere's own way of answering a
// question like "how do I search the last 3 hours in this log file"
// correctly instead of guessing, by handing the model the actual
// documented syntax instead of leaving it to invent one.
//
// Deliberately lexical, not semantic: a real embeddings-based search
// would need an embeddings API call per index build, and Anthropic
// has no public embeddings endpoint at all (only OpenAI/DeepSeek do),
// which would make "which provider is configured" change whether
// documentation search even works. A BM25 index needs no network
// call, no API key, and no extra cost, and already does well on the
// kind of query this matters for: exact chord names, setting keys,
// and specific terminology ("relative", "hours ago") rather than
// deeply paraphrased questions.
package docsearch
