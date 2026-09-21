---
name: bound
description: Keep coding-task context small through bounded output, targeted exploration, and proportionate verification. Use for repository investigations, noisy commands, or growing sessions; keep clear, small tasks lightweight.
---

# bound

Minimize total work for a correct result. Bound large outputs; keep small outputs
intact. Avoid unnecessary questions, artifacts, delegation, and handoffs.

## Scope and explore

- Infer scope and verification from the request. Ask only about consequential
  ambiguity, in one message. Clear tasks need no interview. For multi-step work,
  keep any task card to three lines: deliverable/done-check, constraints, verification.
  Do not create a separate planning file unless useful or requested.
- Explore named files → callers/tests/config → package → repo. Skip dependencies,
  generated files, fixtures, and lockfiles unless relevant.
- Use available SocratiCode search/symbol/impact tools for navigation; verify source
  before editing. Follow project indexing/refresh policy. If unavailable, fall back
  to targeted rg and reads.
- Extract structured data once; keep it on disk and transform it programmatically
  instead of printing it into context and reproducing it in generated code.

## Evidence and Definition of Done

- Establish a lightweight Definition of Done (DoD): required outcome and checks.
  Before each call, identify the unresolved question or unmet check it addresses.
  Keep this internal unless explaining a material decision; no per-call narration.
- Read once by default: batch known files/ranges and reuse unchanged evidence.
  Retain a compact mental map of file, relevant symbols/lines, and findings; no
  separate tracking file is required. Reread for changed content, truncated reads,
  a new question needing another range, or exact source needed for a safe edit.
  Do not front-load oversized reads to satisfy a literal one-read limit.
- Combine independent checks and write complete files instead of fragmented edits.
  Once DoD checks pass, stop unless new evidence reveals a problem.

- Noisy commands: `bound run -- <cmd>`. Inspect diagnostic spills with
  `bound read <spill> --grep RE -C 6` or `A:B`; do not dump entire spills.
  Summaries guide navigation; verify the underlying evidence and exit status.
- Search: `bound grep <pattern> [path] [-t ext]`; narrow truncated queries.
- Read: `bound read <file>`; outline large files, then read targeted ranges.
  Consider bytes too: a few huge lines can overflow context.
- Diff/logs: `bound diff`, then targeted files; `bound log --tail 300 --grep RE`.
- Without bound, use host output limits and targeted reads; preserve exit status
  and full diagnostics on disk. Batch independent checks when useful.
- Reuse evidence, but rerun for changed inputs, transient failures, external-state
  changes, or necessary verification.

## Discover tools once; check affected work together

- In the first repository discovery batch, identify existing format, lint, type,
  build, and test commands from manifests, scripts, configs, and CI. Include package
  manager/tool versions, working directories, supported file filters, and dependencies
  between checks. Reuse this compact tool map; rediscover only changed configuration
  or an invalid assumption. Do not install or replace tools just for this workflow.
- Define the change baseline once. Include relevant staged, unstaged, untracked,
  renamed, and deleted paths, and changes since the last successful check. Preserve
  unrelated user edits. Pass filenames safely; do not assume extensions alone reveal
  impact. Never pass deleted paths as existing files.
- After a coherent edit batch, invoke the selected checks in one tool call, using an
  existing repository command or one orchestration script. Format/fix first, then run
  independent read-only checks together where safe. Avoid duplicate builds and
  concurrent checks that share mutable outputs. Run all selected checks and retain
  each exit status; fail the batch if any required check fails.
- For TypeScript/JavaScript/HTML, filter existing formatters and file-aware linters
  to changed files. Run type checking/builds at the smallest affected configured
  package/project scope, and related tests where supported. For C#, filter formatting
  where supported, but use the owning project and affected dependents for compiler,
  analyzer, build, and test checks. Do not invent single-file support.
- Widen for shared API, dependency, config, or schema changes, affected dependents,
  required CI checks, or unresolved risk. Changed files are a starting set, not proof
  that the rest of the project is unaffected. Explicitly report skipped/unavailable
  checks; never label them passed.
- Save full per-check stdout/stderr to disk. Return status, scope, diagnostic counts
  when available, a few actionable diagnostics, and log paths. Keep successful output
  to one line per check. Bound failure excerpts without hiding exit status or claiming
  omitted diagnostics are absent; inspect relevant logs only as needed.
- Reuse green results until relevant source/config/tool versions change. After fixes,
  rerun affected checks, not the whole batch by habit. Finish when DoD passes; do not
  launch a persistent watcher unless requested.
- Use scripts/codemods for mechanical transformations, preserving syntax and limiting
  scope. Reuse repository generators and formatters; inspect the resulting diff and
  validate behavior. Avoid regenerating unchanged code through model output.

## Complete and measure

Start verification focused; widen for risk, required checks, or unresolved failures.
Avoid verbose repo-wide output by default. After checks pass, repeat only for a
relevant change or unresolved concern. Continue through implementation and validation;
phase boundaries and compaction are not stopping points. Checkpoint when useful;
hand off when requested or actually unable to continue. Report outcome, checks, limits.

`bound stats` measures bytes, not token or billing savings. For A/B comparisons,
hold model, task, hooks, other skills, and verification constant; include delegated
work. Report provider input/output/cache counters when available, label estimates
and missing telemetry, and assess result quality alongside usage.
