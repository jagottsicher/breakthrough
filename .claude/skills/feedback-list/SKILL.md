---
name: feedback-list
description: Add a status line to feedback_list.txt (the local, non-repo feedback tracker) after addressing an entry. Use whenever work touches something feedback_list.txt already has an entry for, or the user reports a bug/observation that belongs there.
---

# feedback_list.txt status lines

`feedback_list.txt` lives in the local working folder, is **not** a repository artifact (same tier as CLAUDE.md — never commit it, never mention it in commit messages), and is read, not authored, by this assistant except for one specific thing: a single status line per entry once it's been acted on.

## Format (from CLAUDE.md, worth restating because it's easy to get wrong)

Entries are chronological, each with date, time, release/commit state (if known), area, and the observation itself — all written by the human. The only thing this assistant adds is **exactly one** of:

```
Done. YYYY-MM-DD HH:MM
Delayed. YYYY-MM-DD HH:MM — bis <Stichwort>; Grund: <Stichworte>
Rejected. YYYY-MM-DD HH:MM — <kurze Stichworte>
```

## Rules

- No analysis, no solution write-up, no work log — just the status line, appended under the entry it belongs to. The entry itself already has all the context; don't restate it.
- Use the current date/time at the moment of actually finishing the work, not when the entry was first read.
- **"Rejected" needs a prior round of asking the user first**, whenever that's practical — don't unilaterally reject something and only report it after the fact unless the user has already made the call explicitly in the conversation.
- Check `feedback_list.txt` at the start of any work session touching an area it has entries for (CLAUDE.md step 1 of the standard workflow) — don't wait until the end to discover a relevant entry existed.
- Keep German Stichworte terse — this file is a working log for the human, not documentation; don't expand it into full sentences.
