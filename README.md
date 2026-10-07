# bound

Bounded tool output for coding agents. One static Go binary, no dependencies.
Linux, macOS, Windows.

`bound` sits between an agent (Cursor, Claude Code, Codex, cloud agents, anything with a
shell) and the commands it runs. Command execution retains full merged stdout/stderr
and a separate termination record; file views link to the original source. Bounded
envelopes select evidence without deleting its source. Omitted text and incomplete
scans are explicit. A pre-tool hook rewrites heavy shell
commands transparently. Two skills teach the agent the protocol: `bound` (targeted exploration, bounded output,
proportionate verification) and `proto` (token-frugal HTML prototyping).

## Quick start

For prebuilt packages (no Go required), follow **[INSTALL.md](INSTALL.md)**.
To build current `main` from source:

```sh
go install github.com/Kuksenok-i-s/bound@main   # Go 1.22+
bound init --agent codex                      # choose your host
bound doctor
```

`init` bundles the `bound` and `proto` skills. See INSTALL.md for a bound-only
installation, updates at an existing hook path, or another host.
Per repository, when project integration is intended:

```sh
bound init --agent codex --project --agents-md
```

Full walkthrough for humans and a copy-paste block for agents: **[INSTALL.md](INSTALL.md)**.

Prebuilt binaries are available from the **[Build workflow](https://github.com/Kuksenok-i-s/bound/actions/workflows/build.yml)**.
Open a successful run and download its `bound-<commit>` artifact. It contains
Linux, macOS and Windows builds for amd64/arm64 (`.tar.gz` or `.zip`), an archive
of the exact source commit, `BUILDINFO.txt` and `SHA256SUMS`. Extract the archive
for your platform and run `bound version`; Go is only needed to build from source.
The workflow verifies Go 1.22 and the current stable Go version before packaging.
Artifacts are retained for 90 days; the workflow can also be run manually.

Give this to an agent to have it install itself:

```text
Install or update bound following https://raw.githubusercontent.com/Kuksenok-i-s/bound/main/INSTALL.md Part B. Configure the current host, user-level, with the bound skill only.
```

## Why

Large tool results and repeated turns grow context. Smaller outputs help only when
agents retain enough evidence to finish correctly; extra questions, retries, and
workflow artifacts can offset the reduction.

1. Bound noisy outputs and retain full diagnostics on disk.
2. Reuse evidence and transform structured data outside model context.
3. Ask only about consequential ambiguity and continue through verification.

`bound stats` reports bytes, not provider token usage or billing savings. Measure
input, output, and cache tokens across all agents, with task, model, hooks, and
verification held constant; assess result quality as well.

## Commands

```text
bound run    [--timeout 10m] [--lines 60] [-c] -- <cmd...>
bound grep   [-n 100] [-i] [-w] [-F] [-t ext] [-g glob] <pattern> [paths]
bound read   <file> [A:B] [--outline] [--full] [--grep RE | --query TEXT -k 10] [-C 3] [--bytes A:B]
bound diff   [git-diff args] [-- paths]
bound log    <file> | -- <cmd...>  [--tail 200] [--grep RE] [--since 30m|TS] [-C 0] [--timeout 5m]
             [--profile [--bin 1m] [--gap 100ms]]
bound tree   [dir] [--depth 3] [--max 500]
bound hook   <cursor|claude|codex>          # stdin JSON → stdout JSON
bound init   [--agent all|cursor|claude|codex] [--project] [--no-skills] [--agents-md] [--dry-run]
bound doctor
bound stats  [--clean]
```

Example envelope:

```text
[bound run] go test ./...
exit=1 lines=811 bytes=18.0K (~4601tok; UTF-8 bytes/4 estimate) time=400ms
full: /tmp/bound/20260921-232116-go-390972712.log
--- summary (go test)
packages ok=0 fail=1  tests failed=2
FAIL TestA    @a_test.go:3: expected 2 got 1
FAIL TestBad  @big_test.go:403: expected 2 got 1
--- head (10)
...
--- tail (20)
...
next: bound read /tmp/bound/...log --grep '--- FAIL|panic:|FAIL\s' -C 6
```

Parsers: `go test`, `go build/vet`, `pytest`, `jest`/`vitest`/`npm test`, `cargo`,
generic diagnostic events (errors, warnings, degradation, skipped checks, coverage).
Repeated diagnostic templates show counts, first/last line numbers and original
examples. Timestamps, UUIDs and labelled request/trace IDs are normalised only for grouping;
status codes, versions and other numeric evidence remain distinct. Categories are
heuristic; task-specific completion and coverage checks remain necessary. Small
command output is printed verbatim within the envelope budget and always retained.

`bound run` and execution through `bound log -- <cmd>` retain `<spill>.meta.json`
with exit, timeout reason and duration. A normal exit 124 is distinct from a timeout.
`bound log --profile` replaces the tail with a bounded summary of *when* the selected
lines arrived, for a day of logs without reading it: counts per bin (auto width or
`--bin`), lines collapsed into logical events (`--gap`, default 100ms, because one
event usually writes several lines), inter-arrival percentiles, bin dispersion
(Fano factor) and an exponential-kernel Hawkes fit. The verdict line distinguishes a
periodic timer or retry loop, multi-line duplicates, an externally driven
non-stationary rate, a self-exciting cascade (branching ratio = share of events
triggered by a prior one, decay = how long one event raises the risk), and a
Poisson-like source where a per-bin threshold suffices. Thresholds are heuristic and
the fitted parameters are printed alongside, so the verdict can be checked.
`bound read --bytes 401:800` retrieves a portion of a long line (1-based inclusive).
Regex search scans the full file while retaining bounded context; long lines do
not end the scan at 16 MiB. Reading still needs memory proportional to the longest
individual line. Source views may omit text even when `scan_complete=true`.

For exploratory retrieval:

```sh
bound read build.log --query 'connection refused' -k 10 -C 3
```

This uses two streaming BM25 passes over lines with words and adjacent word
bigrams (k1=1.2, b=0.75, bigram weight 1.5). Top-k results have source range commands
for context, including neighbouring stacktrace lines. Query/source tokenisation
is identical; identifiers split at underscores, while code/number tokens remain.
Ranking does not establish event severity or prove readiness. A missing ranked
result is not evidence of absence. Detected size/mtime, pathname identity or line-count changes during ranking report
an incomplete result; retry against a stable file.
Outlines (`bound read` on big files): ~20 languages via regex, `ctags` when installed,
plus HTML/Pug structure (headings, landmarks, forms, templates, ids).

## Budgets

Soft defaults, hard caps; override soft values with `BOUND_*` env vars.

| Kind | Soft | Hard | Env |
|------|------|------|-----|
| run envelope | 60 lines | 12 KB | `BOUND_RUN_LINES`, `BOUND_RUN_CHARS` |
| grep | 100 matches | 500 | `BOUND_GREP_MAX` |
| read | ≤500 lines full | >2000 needs a range | `BOUND_READ_SOFT`, `BOUND_READ_HARD` |
| diff | 800 lines | 5000 | `BOUND_DIFF_SOFT` |
| log | tail 200 | 1000 | `BOUND_LOG_TAIL` |
| tree | depth 3, 500 entries | 2000 | `BOUND_TREE_MAX` |
| BM25 results | 10 | 50 | `BOUND_QUERY_MAX` |
| event templates | 40 | 200 | `BOUND_EVENT_MAX` |
| log profile | 24 bins, fit on last 50k events | 60, 200k | `BOUND_PROFILE_BINS`, `BOUND_PROFILE_FIT` |
| spill dir | `$TMPDIR/bound` | | `BOUND_DIR` |

## What the hook does

Rewrites only **simple** commands (no pipes, redirects, substitutions, globs, `&&`).
Anything else passes through untouched: rewriting compound commands is where other tools
broke commands and caused retry turns.

- `go test`, `pytest`, `npm test`, `cargo test`, `make`, `docker build`, `rg`, `find`, … → `bound run --`
- `go test -v ./...` → `-v` dropped (repo-wide verbose adds nothing; failures still shown)
- `kubectl logs`, `docker logs`, `journalctl` without a bound → `--tail=300` / `-n 300` added
- `git diff` → `bound diff`; `git log` without a count → `-n 20`; `git status` → `--short --branch`
- `cat <file>` → `bound read <file>`; bare `find .` → `bound tree .`
- host `Grep` tool without `head_limit` → `head_limit=100` (Cursor, Claude Code)
- host `Read` tool on a file > 2000 lines without offset/limit → denied, with an outline
  and the exact ranged command to run instead

Host notes:

- Cursor: `~/.cursor/hooks.json` `preToolUse`. Project `.cursor/hooks.json` is loaded by
  cloud agents; user-level is not.
- Claude Code: `~/.claude/settings.json` `PreToolUse`. Keep one dispatcher hook per tool;
  sibling hooks silently drop `updatedInput`.
- Codex: `~/.codex/hooks.json` `PreToolUse` on `Bash`. Codex reads files through the shell,
  so `cat` rewriting covers reads. Run `/hooks` once to trust the hook.
- Others (Gemini CLI, OpenCode, cloud sandboxes): commit the project hooks and the
  `AGENTS.md` section, install the binary in the environment; the agent calls `bound` directly.
- Windows: same files under `%USERPROFILE%`; hook commands carry the quoted `bound.exe`
  path; `-c` uses PowerShell; the rewriter parses backslash paths and skips cmd's `find`.

## Skills

Installed to `~/.cursor/skills/`, `~/.claude/skills/`, `~/.agents/skills/`. They load only
when the agent decides a task needs them.

**`bound`** — skip unnecessary interviews; keep any task card to three lines;
explore narrowly with source verification; bound noisy output; reuse evidence with
justified reruns; verify proportionately and complete the task. Phase boundaries
and compaction do not require stopping. Read once by default and reuse unchanged
evidence, with justified rereads for changes, missing context, or safe edits. Each
call should resolve an open question or unmet Definition of Done check; batch
independent work and stop when required checks pass. Keep this reasoning internal.
Discover repository tools once and reuse their command/scope map. Batch affected
checks after coherent edits, retain full logs, and report compact per-check results.
Filter file-aware checks to changed files; use affected package/project scopes for
compilers, builds, and tests, widening when required. Use scripts for mechanical
transformations. Use provider counters for token comparisons.

**`proto`** — HTML prototypes for pre-production research with an existing design system
and sample data. Mockup pages are output tokens (≈5× input) that then sit in context every
turn, so: interview (≤7) → spec approved by a human → reusable primitives + `INDEX.md`
written once by the strong model → page assembly delegated to a cheap model that gets only
the spec section and the index → human review with one yes/no question; on "no", ≤5
targeted questions, then spec update / primitive fix / re-assemble one section. Hard rules:
no markup before primitives exist, design-system classes only, sample data by reference,
shell written once, search-replace edits only, never read a page back.

## Measuring

`bound stats` reports source and printed **bytes per operation**, not task savings.
Reading one line of an 80,000-byte log still adds 80,000 source bytes; ten rereads
add 800,000. Printed bytes include the envelope and can exceed source bytes on a
small command. They exclude this stats output and host framing/transport effects.

Identified source snapshots deduplicate path/size/mtime identities across operations;
they are not content deduplication, unique underlying bytes or a task baseline.
Old ledger entries can lack identity and are reported as unattributed. Invalid
records and incomplete ledger reads are disclosed rather than treated as complete.

Use a separate `BOUND_DIR` for each task comparison, a fixed baseline and equal
readiness criteria. Count follow-up reads and all agents. UTF-8 bytes/4 is a labelled
heuristic, not chars/4, a tokenizer result or billed usage. Compare provider input,
output and cache counters, quality and applicable rates before claiming token/cost
savings. No provider telemetry is collected by this CLI.

## Non-goals

- Token-count enforcement (the 150k/180k stop). No host exposes per-turn context size to a
  hook; only Cursor's `preCompact` reports it, at compaction time. Keep that as a prompt rule.
- Lossy compression of arbitrary output.
- An MCP server (would add a static tool manifest to every turn).

## Development

```sh
make lint test build     # gofmt + vet, tests, bin/bound
make release             # cross-builds into dist/
```

Standard library only. See `AGENTS.md` for the rules agents follow in this repo.

## License

MIT
