# Domain vocabulary — beads

- **statement** — a typed, id-bearing record (R-/Q-/F-) with explicit authority; rulings/questions/findings, distinct from untyped comments.
- **execution contract** — the model-facing render of a bead: active rulings above base text, open questions as blockers, findings with provenance, untyped history last.
- **actor gating** — BD_ACTOR-based refusal: executor contexts cannot file rulings.
- **untyped history** — legacy comments on a pre-statements bead, rendered newest-first above base text with a provenance-unknown banner.
- **state-effect flag** — a ruling flag (--defer/--park/--close) that changes bead status atomically with the ruling row.
- **migration cache** — the per-DSN fast-path file under the user cache dir that lets `store.Open` skip `migrateDB`; Release 3 keys it on sqlite file identity, not the path string.
- **tracker hygiene** — the rules that keep a smoke run off a live tracker: explicit `--db <path>`, fresh file per run, no recreate-in-place.
- **copy-based gate** — proving a migration on a dumped copy of a shared tracker in a throwaway database instead of on the live one.
