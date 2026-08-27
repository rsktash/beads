# beads-epy — Statements: typed authority layer with EXECUTION CONTRACT rendering

Adds a native authority layer to bd, designed against forensic mining of 201 agent
sessions (274 owner-correction incidents / 669 owner questions): agents escalate
already-settled decisions because rulings live in chat or in free-text comments that
the model's body-over-comments prior skims, agent paraphrases are indistinguishable
from owner rulings, and ruled-out beads keep reappearing in `bd ready` because state
never changed. The fix is read-path and state-path: a `statements` table (ruling /
question / finding; notes remain untyped comments), actor-gated CLI (`BD_ACTOR`
executors cannot file rulings), state-effect flags (`--defer/--park/--close` atomic
with the ruling), inheritance-by-default scopes (bead/epic/project), open Questions
excluding beads from `bd ready`, and `bd show`'s text output becoming an EXECUTION
CONTRACT — active rulings above base text, provenance-labeled findings, legacy
comments rendered as UNTYPED HISTORY (never a false "no rulings"), the word
"comments" absent from model-facing output. Plus `bd settled` (FTS with a
true-negative sentinel) and `bd rulings --scope project` as query surfaces for
corpus-side hooks (ask-guard, session-start injection — out of this repo's scope).

Key decisions (owner, 2026-08-27): statements table over tagged comments; actor
gating over owner-confirmation or trust; `bd show` default over a new command;
inherit-by-default over mandatory scope; Questions block natively while plan-time
decision beads stay. Excluded by incident test: Evidence/Constraint/AgentRun node
types, confidence scores, embeddings, REST/service layer.

Acceptance: dual-engine migrations apply to a production-copy DB; executor ruling
refusal; atomic defer; ready-exclusion round-trip; contract render golden tests
(typed / legacy / mixed / superseded / inherited); `bd settled` canary; backfill
candidates from real marker fixtures; byte-compatible `--json` without
`--include statements`.

Evidence base: `~/.claude/outsource-review-runs/mining-solo.md`, `mining-zanjir.md`.
