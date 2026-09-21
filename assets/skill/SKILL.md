---
name: bound
description: Context-budget discipline for any coding task. Use at the start of every non-trivial task, before repository exploration, before running tests/builds/logs/diffs, and when a session has grown large. Runs a short interview to fix scope, then keeps tool output bounded via the `bound` CLI, SocratiCode code intelligence, and targeted git. Works in Cursor, Claude Code, Codex and cloud agents.
---

# bound: keep the context small

Cost is prefix size × number of turns. A large tool result on turn 3 is re-sent on
every later turn; an extra retry turn re-sends everything. Optimise for: no spikes,
few turns, fresh sessions.

## 1. Interview first (once per task)

Before exploring or editing a non-trivial task, ask the user in ONE message. Use the
host's question tool if present (AskQuestion / AskUserQuestion / request_user_input),
otherwise a numbered list. Ask at most 10, skip anything already answered by the prompt
or obvious from the repo. Pick from:

1. Goal in one sentence, and what "done" looks like (observable check).
2. In-scope paths. Out-of-scope paths (never read, never edit).
3. Known entry points: files, symbols, endpoints, error messages.
4. Constraints: APIs/behaviour that must not change, style rules, dependencies not to add.
5. Test/verify command and its expected runtime. Which subset is enough?
6. Environment: branch, language/toolchain versions, how to run locally.
7. Has this been attempted before? What failed?
8. Expected output: patch, PR, report, plan? How terse?
9. Budget: quick fix vs thorough; when to stop and hand off.
10. Any part where a wrong guess is expensive (data migration, public API, prod config)?

Then write a Task Card (≤ 25 lines: goal, done-check, scope in/out, entry points,
constraints, verify command, budget) and work from it. Do not re-ask; refine only if
the code contradicts the card.

## 2. Exploration order

1. Files named by the user → 2. their imports, callers, tests, config →
3. the owning package → 4. repo-wide, only if 1–3 failed.

Never read `.git/`, `node_modules/`, `vendor/`, build output, generated code, lock
files, fixtures, binaries unless the task is about them.

### SocratiCode (if `codebase_*` tools exist)

Prefer these over grep+read; they return symbols and edges, not file bodies.

- `codebase_status` once; if not indexed and the repo is big, ask before indexing.
- "where is X / what handles Y" → `codebase_search` (semantic) or `codebase_symbols` (name).
- Before changing a function → `codebase_symbol` (definition, callers, callees).
- Before a wide edit → `codebase_impact` (blast radius) instead of reading every caller.
- Schemas, API specs, infra → `codebase_context_search`.
Read a file range only after these say which lines matter.

### Search
`bound grep <pattern> [path] [-t ext]` — 100 matches default, hard 500, grouped by file
with totals. If `truncated=true`, narrow the pattern or path. Do not page.

### Read
`bound read <file>` — ≤500 lines full; larger → outline; then `bound read <file> A:B`
or `--grep RE -C 6`. Host Read tool: always pass offset/limit for files you have not
outlined.

## 3. Commands, tests, logs, diffs

Run heavy commands through `bound run -- <cmd>`: you get exit code, size, a parsed
summary (failing tests, build errors), head/tail, and the spill path. Then dig with
`bound read <spill> --grep 'FAIL|panic' -C 6`, never `cat` the spill.

- Tests: smallest scope first (`go test ./pkg/...`, `pytest path::test`), widen only
  on green. Never `go test -v ./...`, `pytest -v` repo-wide.
- Logs: `bound log <file|-- cmd> --tail 300 --grep RE --since 30m`. No unbounded
  `journalctl`, `kubectl logs`, `docker logs`.
- Diff: `bound diff` (stat) → `bound diff -- <file>`. `git status --short`.
  `git log -n 20 --oneline`, `git blame -L A,B file`, `git show --stat <rev>`.
- Multi-step data work: write one script (bash/python/go) into /tmp, run it via
  `bound run --`, read only its stdout. Intermediate results stay out of context.

Do not re-read unchanged files or re-run unchanged commands. Summarise findings in
your own words and continue from the summary.

## 4. Handoff

If the session is visibly large (many tool results, repeated summaries, compaction
happened) or the phase (investigation → implementation) is complete, stop exploring
and emit `HANDOFF` (≤ 2000 tokens): Goal · Current state · Findings · Relevant files
+ symbols · Changes made · Remaining work · Decisions/constraints · Tests run + results ·
Next commands · Unresolved. No raw logs, diffs or file bodies. Recommend a fresh session.

## 5. Report style

No narration of routine exploration. Report findings, decisions, edits, test results,
blockers. `bound stats` shows raw vs delivered bytes for the session.
