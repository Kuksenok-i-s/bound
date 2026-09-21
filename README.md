# bound

Bounded tool output for coding agents. One static Go binary, no dependencies.
Linux, macOS, Windows.

`bound` sits between an agent (Cursor, Claude Code, Codex, cloud agents, anything with a
shell) and the commands it runs. Every subcommand returns a fixed-size envelope and writes
the full artifact to disk, so a test run, a search, a diff or a log can never dump
hundreds of kilotokens into the model's context. A pre-tool hook rewrites heavy shell
commands transparently. Two skills teach the agent the protocol: `bound` (targeted exploration, bounded output,
proportionate verification) and `proto` (token-frugal HTML prototyping).

## Quick start

```sh
go install github.com/Kuksenok-i-s/bound@latest   # Go 1.22+
bound init --agent all                              # hooks + skills for Cursor, Claude Code, Codex
bound doctor                                        # verify
```

Per repository (cloud agents load project hooks, not user hooks):

```sh
bound init --project --agents-md
```

Full walkthrough for humans and a copy-paste block for agents: **[INSTALL.md](INSTALL.md)**.

Give this to an agent to have it install itself:

```text
Install and configure bound following https://raw.githubusercontent.com/Kuksenok-i-s/bound/main/INSTALL.md Part B.
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
bound read   <file> [A:B] [--outline] [--full] [--grep RE -C 3]
bound diff   [git-diff args] [-- paths]
bound log    <file> | -- <cmd...>  [--tail 200] [--grep RE] [--since 30m|TS] [-C 0]
bound tree   [dir] [--depth 3] [--max 500]
bound hook   <cursor|claude|codex>          # stdin JSON → stdout JSON
bound init   [--agent all|cursor|claude|codex] [--project] [--no-skills] [--agents-md] [--dry-run]
bound doctor
bound stats  [--clean]
```

Example envelope:

```text
[bound run] go test ./...
exit=1 lines=811 bytes=18.0K (~4601tok) time=400ms
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
generic (error/fail/panic lines). Small outputs are printed verbatim, nothing hidden.
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

`bound stats` shows raw vs delivered bytes per kind (chars/4 estimate). It is **not** your
bill. Compare paired sessions with and without `bound` in the host's usage panel; the
counterfactual that matters is billed input, not compressed bytes.

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
