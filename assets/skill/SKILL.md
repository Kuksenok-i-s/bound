---
name: bound
description: Keep repository investigations and noisy tool output small through targeted exploration, retained evidence, and proportionate verification. Keep clear, small tasks lightweight.
---

# bound

Reduce what enters context while retaining the evidence required to finish.

## Scope and evidence

- Define the requested result and its readiness checks. Ask only about ambiguity
  that changes the result or risk. Keep planning internal unless useful to the user.
- Explore named files, then relevant callers/tests/config and affected packages.
  Use available navigation tools and verify current source. Skip unrelated generated
  files, dependencies and lockfiles; inspect them when the question requires them.
- Read once by default and reuse unchanged evidence. Reread for truncation, source
  changes, missing evidence after compaction, a new relevant range, or exact text
  needed for an edit. Do not over-read upfront to satisfy a literal one-read rule.
- Keep structured intermediate data on disk and transform it programmatically.
  Apply coherent edit batches with existing generators, formatters or codemods;
  preserve unrelated work and avoid regenerating unchanged code.

## Bounded output

- `bound run -- <cmd>` retains merged stdout/stderr and a `.meta.json` termination
  record, including small output. Check the actual exit status and timeout reason.
  Retain full per-check diagnostics; use targeted excerpts for context.
- Use `bound read <file> A:B`, `--grep RE -C 6`, or `--bytes A:B` (1-based inclusive)
  for exact evidence. Byte ranges recover parts of a long line; small output can be
  read in full when that is the cheapest complete check.
- For exploratory lexical retrieval use `bound read <file> --query 'connection refused' -k 10 -C 3`. BM25 ranks words and adjacent word bigrams. Results link to
  source ranges; top-k, normalised templates and snippets are selected views.
- Search events required by readiness criteria even when the command exits zero:
  failures, warnings, skipped checks, low coverage, degradation and completion.
  Automatic diagnostic categories are heuristic. BM25 scores and rarity do not
  establish operational importance. No query can prove absence of unrelated events.
- Before a negative conclusion, check source coverage and `scan_complete` /
  `incomplete` / `truncated` indicators. A missing excerpt or ranked result is not
  evidence of absence. Follow the source range or narrow the query as needed.
- Use `bound grep <pattern> [path]`, `bound diff`, and
  `bound log <file> --tail 300 --grep RE`. File views reference the original source;
  execution via `bound log -- <cmd>` retains output and child termination status.
- For a long log, profile before reading: `bound log <file> --grep RE --profile`.
  The `--- profile` section describes *when* matching lines arrived (rate per bin,
  lines collapsed into events, inter-arrival spread, a Hawkes fit) and ends with a
  heuristic `verdict:`. Treat it as a map, not a finding: it says where and in what
  shape the events are, never why. Periodic → a timer or retry loop, read one cycle.
  Duplicates → raise `--gap`. Non-stationary → read the bins, ignore the fit.
  Cascade → read the first events of a burst, the rest are consequences.
  Poisson-like → a per-bin threshold is enough. Verify the parameters printed on the
  `hawkes` line before quoting the verdict, and narrow with `--since`/`--grep`.
- Without bound, use targeted reads and host output limits while retaining full
  diagnostics and status on disk. Avoid dumping large spills into context.

## Verification and completion

- Discover the existing tools needed for this task, once. For code edits identify
  relevant format/lint/type/build/test commands, supported filters, package scope,
  versions and dependencies. A small read-only question needs no repository-wide
  tool inventory. Do not install or replace tools solely for this workflow.
- Establish relevant staged/unstaged/untracked/deleted changes. Run selected checks
  after a coherent edit batch: format/fix first, then independent read-only checks
  together where they do not share mutable outputs. Preserve each exit status.
- Start with changed files and affected projects/dependents. Widen for shared APIs,
  dependencies/config/schema, required CI or unresolved risks. Check filenames
  safely; deleted paths are not existing files. Do not invent single-file support.
- Reuse green results only while relevant source, configuration, dependency/tool
  versions, fixtures and environmental assumptions remain valid. Rerun affected
  checks after a fix or transient/external-state change.
- Report outcome, verification scope, selected diagnostics and full log paths.
  Label skipped or unavailable checks. Stop when the requested result and required
  checks are proved and known risks are resolved or explicitly reported. Compaction
  and phase boundaries are not completion; do not start watchers unless requested.

## Measurement

`bound stats` sums full source bytes per operation: rereading a log counts its
whole size again, including a one-line range. Printed bytes include envelopes.
Identified snapshots use path/size/mtime, not content deduplication or a task
counterfactual. Old entries may lack source identity. Ratios can exceed 100% on
small output. Stats output and host framing are outside these counters.

For task comparisons use a separate `BOUND_DIR`, a fixed baseline and equal
readiness checks; include all follow-up reads and agents. Hold model, task, hooks
and other skills constant, and assess quality as well as usage. UTF-8 bytes/4 is
only a labelled estimate, not chars/4 or actual tokens. Use provider input/output/
cache counters and applicable pricing for token and cost comparisons; disclose
missing telemetry.
