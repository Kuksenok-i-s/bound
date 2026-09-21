## Context budget (bound)

- Before a non-trivial task: interview the user (≤10 questions, one message), write a
  Task Card (goal, done-check, scope in/out, verify command), work from it.
- Explore narrow → wide: named files, then imports/callers/tests, then package, then repo.
  Prefer SocratiCode `codebase_symbols` / `codebase_symbol` / `codebase_impact` over grep+read.
- Run tests, builds, searches, logs and diffs through `bound` (`bound run -- <cmd>`,
  `bound grep`, `bound read <file> A:B`, `bound diff`, `bound log --tail 300`).
  Never `cat` a spill file; use `bound read <spill> --grep RE -C 6`.
- Files > 500 lines: outline first, then ranges. Searches: refine, don't page.
- Never re-read unchanged files or re-run unchanged commands.
- Multi-step data work: one script in /tmp, run once, read only its stdout.
- When the session is large or a phase ends: emit a compact HANDOFF and suggest a new session.
- Report findings, edits, tests, blockers. Skip narration.
