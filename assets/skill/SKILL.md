---
name: bound
description: Context-budget discipline for any coding task. Use at the start of every non-trivial task, before repository exploration, before running tests/builds/logs/diffs, and when a session has grown large. Runs a short interview to fix scope, then keeps tool output bounded via the `bound` CLI, SocratiCode code intelligence, and targeted git. Works in Cursor, Claude Code, Codex and cloud agents.
---

# bound

Cost = prefix size × turns. A big tool result early is re-sent every turn; a retry turn
re-sends everything. Avoid spikes, avoid turns, start fresh sessions.

## Interview (once, before exploring a non-trivial task)

One message, ≤10 questions, skip what the prompt or repo already answers. Use the host's
question tool if present. Pick from: goal + observable done-check · in/out-of-scope paths ·
entry points (files, symbols, errors) · constraints (APIs, style, no new deps) · verify
command + enough subset · environment/branch · prior attempts · output form · budget and
stop condition · where a wrong guess is expensive.
Write a Task Card (≤25 lines) and work from it. Do not re-ask.

## Explore narrow → wide

Named files → imports/callers/tests/config → package → repo. Skip `.git`, `vendor`,
`node_modules`, build output, generated code, lock files, fixtures.
If `codebase_*` tools exist: `codebase_symbols`/`codebase_search` to locate,
`codebase_symbol` before changing a function, `codebase_impact` before a wide edit.
Read a range only after these say which lines matter.

## Bounded tools

- `bound run -- <cmd>` for tests, builds, lint, scripts: exit, size, parsed failures,
  spill path. Dig with `bound read <spill> --grep 'FAIL|panic' -C 6`, never `cat`.
- `bound grep <pat> [path] [-t ext]`: `truncated=true` → narrow, don't page.
- `bound read <file>`: ≤500 lines full, else outline → `A:B` or `--grep RE`.
  Host Read tool: always offset/limit on files you have not outlined.
- `bound diff` → `bound diff -- <file>`; `git status --short`; `git log -n 20 --oneline`.
- `bound log <file|-- cmd> --tail 300 --grep RE --since 30m`. Never unbounded logs.
- Tests smallest scope first; widen on green. No repo-wide `-v`.
- Multi-step data work: one script in /tmp, run once via `bound run --`, read only stdout.
- Never re-read unchanged files or re-run unchanged commands.

## Handoff

When the session is visibly large or a phase ends: `HANDOFF` ≤2000 tokens — Goal ·
State · Findings · Files+symbols · Changes · Remaining · Decisions · Tests · Next commands ·
Unresolved. No raw logs/diffs/file bodies. Suggest a fresh session.

Report findings, edits, tests, blockers. No narration.
