# bound

Bounded tool output for coding agents. One static Go binary, no dependencies.

`bound` sits between an agent (Cursor, Claude Code, Codex, cloud agents, anything with a
shell) and the commands it runs. Every subcommand returns a fixed-size envelope and writes
the full artifact to disk, so a test run, a search, a diff or a log can never dump
hundreds of kilotokens into the model's context. A pre-tool hook rewrites heavy shell
commands transparently; a skill and an `AGENTS.md` snippet teach the agent the protocol,
including a short interview before it starts exploring.

## Why

Independent measurements in 2026 ([JetBrains](https://blog.jetbrains.com/ai/2026/07/rtk-claude-code-token-savings/),
[Quesma](https://quesma.com/blog/does-rtk-make-ai-coding-cheaper/),
[arXiv 2607.12161](https://arxiv.org/html/2607.12161)) show that the bill of an agent
session is dominated by prompt-cache traffic: prefix size × number of turns. Compressing
shell output alone does not lower the bill and can raise it when compression causes a
retry. What does work:

1. No spikes: a single tool result must never exceed a hard ceiling.
2. Fewer turns: filter in code before results reach the model; give the exact next command.
3. Fresh sessions: fix scope up front (interview), hand off before the prefix gets expensive.

`bound` implements 1 and 2 as a tool, 3 as a skill. It deliberately does **not** do lossy
compression: failing lines are verbatim, exit codes are preserved, the full artifact path
is always printed.

## Install

```sh
go install github.com/Kuksenok-i-s/bound@latest      # needs Go 1.22+
bound init --agent all                                 # hooks + skill for Cursor, Claude Code, Codex
bound init --project --agents-md                       # per-repo hooks (cloud agents) + AGENTS.md section
```

`bound init` is idempotent and merges into existing config files. Use `--dry-run` to preview.

## Commands

```text
bound run   [--timeout 10m] [--lines 60] [-c] -- <cmd...>
bound grep  [-n 100] [-i] [-w] [-F] [-t ext] [-g glob] <pattern> [paths]
bound read  <file> [A:B] [--outline] [--full] [--grep RE -C 3]
bound diff  [git-diff args] [-- paths]
bound log   <file> | -- <cmd...>  [--tail 200] [--grep RE] [--since 30m|TS] [-C 0]
bound tree  [dir] [--depth 3] [--max 500]
bound hook  <cursor|claude|codex>          # stdin JSON → stdout JSON
bound init  [--agent all|cursor|claude|codex] [--project] [--no-skills] [--agents-md] [--dry-run]
bound stats [--clean]
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

## Budgets

Soft defaults, hard caps; override soft values with `BOUND_*` env vars.

- run envelope: 60 lines, 12 KB hard (`BOUND_RUN_LINES`, `BOUND_RUN_CHARS`)
- grep: 100 matches, 500 hard (`BOUND_GREP_MAX`)
- read: ≤500 lines full, >2000 requires a range (`BOUND_READ_SOFT`, `BOUND_READ_HARD`)
- diff: 800 lines, 5000 hard (`BOUND_DIFF_SOFT`)
- log: tail 200, 1000 hard (`BOUND_LOG_TAIL`)
- tree: depth 3, 500 entries (`BOUND_TREE_MAX`)
- spill dir: `$BOUND_DIR` or `$TMPDIR/bound`

## What the hook does

The pre-tool hook rewrites only **simple** commands (no pipes, redirects, substitutions,
globs, `&&`). Anything else passes through untouched: rewriting compound commands is where
other tools broke commands and caused retry turns.

- `go test`, `pytest`, `npm test`, `cargo test`, `make`, `docker build`, `rg`, `find`, … → `bound run --`
- `go test -v ./...` → `-v` dropped (repo-wide verbose adds nothing; failures still shown)
- `kubectl logs`, `docker logs`, `journalctl` without a bound → `--tail=300` / `-n 300` added
- `git diff` → `bound diff`; `git log` without a count → `-n 20`; `git status` → `--short --branch`
- `cat <file>` → `bound read <file>`; bare `find .` → `bound tree .`
- host `Grep` tool without `head_limit` → `head_limit=100` (Cursor, Claude Code)
- host `Read` tool on a file > 2000 lines without offset/limit → denied with an outline and
  the exact ranged command to run instead

Host support:

- Cursor: `~/.cursor/hooks.json` `preToolUse` (`Shell|Read|Grep`). Project-level
  `.cursor/hooks.json` is loaded by cloud agents; user-level is not.
- Claude Code: `~/.claude/settings.json` `PreToolUse` (`Bash|Read|Grep`). One dispatcher hook
  per tool; sibling hooks silently drop `updatedInput`.
- Codex: `~/.codex/hooks.json` `PreToolUse` (`Bash`). Codex reads files through the shell,
  so `cat` rewriting covers reads. Run `/hooks` once to trust the hook.
- Anything else (cloud agents, Gemini CLI, OpenCode): commit `.cursor/hooks.json` / the
  `AGENTS.md` section and ship the binary in the environment; the agent calls `bound` directly.

## The skill

`assets/skill/SKILL.md` is installed to `~/.cursor/skills/bound`, `~/.claude/skills/bound`
and `~/.agents/skills/bound`. It loads only when the agent decides the task needs it and
covers: a ≤10-question interview producing a Task Card, exploration order, SocratiCode
(`codebase_symbols` → `codebase_symbol` → `codebase_impact` instead of grep+read), git
usage, the `bound` protocol, and the handoff format.

## Measuring

`bound stats` shows raw vs delivered bytes per kind (chars/4 estimate). It is **not** your
bill. Compare paired sessions with and without `bound` in the host's usage panel; the
counterfactual that matters is billed input, not compressed bytes.

## Non-goals

- Token-count enforcement (the 150k/180k stop). No host exposes per-turn context size to a
  hook; only Cursor's `preCompact` reports it, at compaction time. Keep that as a prompt rule.
- Lossy compression of arbitrary output.
- An MCP server (would add a static tool manifest to every turn).

## License

MIT
