# bound — agent instructions

Go 1.22+, standard library only. Single binary, no runtime dependencies. Keep it that way.

- Build: `go build ./...`  Test: `go test ./...`  Lint: `gofmt -l . && go vet ./...`
- Every subcommand must print a bounded envelope and never hide data silently: print
  totals, `truncated=true`, the spill path, and the exact next command.
- Shell rewriting (`internal/bound/rewrite.go`) touches only simple argv commands. Never
  rewrite pipes, redirects, substitutions, globs or compound commands.
- Hooks (`internal/bound/hook.go`) must not fail a tool call: on internal error print nothing.
- Parsers keep failing lines verbatim. Do not add lossy compression.
- New budgets go in `Limits` with a soft default, a hard cap, and a `BOUND_*` override.

## Context budget (bound)

- Before a non-trivial task: interview the user (≤10 questions, one message), write a
  Task Card (goal, done-check, scope in/out, verify command), work from it.
- Explore narrow → wide: named files, then imports/callers/tests, then package, then repo.
- Run tests, builds, searches, logs and diffs through `bound` (`bound run -- <cmd>`,
  `bound grep`, `bound read <file> A:B`, `bound diff`). Never `cat` a spill file.
- Files > 500 lines: outline first, then ranges. Searches: refine, don't page.
- Never re-read unchanged files or re-run unchanged commands.
- When the session is large or a phase ends: emit a compact HANDOFF.
- Report findings, edits, tests, blockers. Skip narration.
