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

- Minimize total work for a correct result. Ask only about consequential ambiguity;
  clear tasks need no interview. Keep any task card to three lines, without an extra file.
- Explore named files → callers/tests → package → repo. Use SocratiCode for navigation,
  verify source before editing, and follow the shared indexing/refresh policy.
- Bound noisy output with `bound run`, `bound grep`, `bound read`, `bound diff`, and
  `bound log`. Keep small outputs intact. Read relevant spill ranges, not whole spills.
- Use outlines and targeted ranges for large files; consider bytes as well as lines.
- Reuse evidence; rerun for changed inputs, transient failures, external changes,
  or necessary verification. Keep structured intermediate data on disk.
- Verify proportionately and finish the task. Checkpoint when useful; do not stop
  merely because a phase ended or context was compacted.
- Define the required outcome/checks (DoD); each call must resolve a question or
  unmet check. Batch independent reads/checks and avoid fragmented writes.
- Read once by default and reuse unchanged evidence. Reread for changes, truncation,
  new relevant ranges, or exact source needed for safe edits; do not over-read upfront.
- Stop when DoD checks pass unless new evidence reveals a problem. Keep this internal.
- Discover existing repository tools once; reuse the command/scope map until relevant
  config changes. After coherent edits, run affected checks in one bounded batch,
  formatting first. Preserve per-check failures and full logs; summarize successes.
- Filter file-aware checks to changed files; scope compilers/builds/tests to affected
  projects and dependents. Widen when impact or required CI warrants it. Reuse green
  results until their inputs change. Use scripts/codemods for mechanical changes.
- Report outcomes, checks, and blockers. Byte counts are not actual token usage.
