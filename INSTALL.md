# Installing bound

Two audiences. Humans: read Part A. Agents (Cursor, Claude Code, Codex, cloud agents):
Part B is written to be executed as-is.

Requirements: Go 1.22+ (`go version`). Optional: `rg` (faster search; without it `bound
grep` uses a built-in walker), `ctags` (better outlines). Linux, macOS, WSL, Windows.

---

## Part A — for humans

### 1. Install the binary

```sh
go install github.com/Kuksenok-i-s/bound@latest
```

The binary lands in `$(go env GOPATH)/bin` (usually `~/go/bin`). Hooks reference the
binary by absolute path, so PATH is only needed for you and the agent to type `bound`:

```sh
# if `which bound` prints nothing:
ln -s "$(go env GOPATH)/bin/bound" ~/.local/bin/bound      # or add ~/go/bin to PATH
```

#### Windows (PowerShell)

```powershell
winget install GoLang.Go          # if `go version` fails; reopen the terminal afterwards
go install github.com/Kuksenok-i-s/bound@latest
bound version                      # the Go installer puts %USERPROFILE%\go\bin on PATH
```

If `bound` is not found: `$env:Path += ";$(go env GOPATH)\bin"` for this session, and add
`%USERPROFILE%\go\bin` to the user PATH permanently (Settings → Environment Variables).

Everything below is identical on Windows. Config files live under `%USERPROFILE%`
(`~\.cursor\hooks.json`, `~\.claude\settings.json`, `~\.codex\hooks.json`), hook commands
are stored with the quoted absolute `bound.exe` path, `bound run -c` uses PowerShell
(`pwsh`, then `powershell`, then `cmd`), and the command rewriter understands
backslash paths and PowerShell quoting. Claude Code on Windows requires Git for Windows;
its hooks run through Git Bash, which handles the quoted path too. Spill files go to
`%TEMP%\bound`.

### 2. Wire the hooks and skills

Preview first, then apply:

```sh
bound init --agent all --dry-run
bound init --agent all
```

What this writes (merged into existing files, idempotent):

| Host        | Hook file                  | Event / matcher                 | Skills dir              |
|-------------|----------------------------|---------------------------------|-------------------------|
| Cursor      | `~/.cursor/hooks.json`     | `preToolUse` `Shell\|Read\|Grep` | `~/.cursor/skills/`     |
| Claude Code | `~/.claude/settings.json`  | `PreToolUse` `Bash\|Read\|Grep`  | `~/.claude/skills/`     |
| Codex       | `~/.codex/hooks.json`      | `PreToolUse` `Bash`             | `~/.agents/skills/`     |

Skills installed: `bound` (context budget, interview, handoff) and `proto` (HTML
prototyping workflow). Use `--agent cursor|claude|codex` for one host, `--no-skills` to
skip skills.

Codex only: open Codex once and run `/hooks` to trust the new hook.

### 3. Per repository (recommended)

Inside a project:

```sh
bound init --project --agents-md
```

Writes `.cursor/hooks.json`, `.claude/settings.json`, `.codex/hooks.json` into the repo
and appends a "Context budget (bound)" section to `AGENTS.md`. Cursor cloud agents load
project hooks; user-level hooks are not available there. The agent environment still
needs the binary (add `go install github.com/Kuksenok-i-s/bound@latest` to its setup).

If your project has its own design system and sample data, add two lines to `AGENTS.md`
so the `proto` skill skips those interview questions:

```md
- Design system: <path or URL>; components listed in <file>.
- Sample data: <path>; entities: <list>.
```

### 4. Verify

```sh
bound doctor
bound run -- go version        # any command; you should see a "[bound run]" envelope
```

`bound doctor` lists binary, spill dir, tools, and which hosts have hooks and skills.

### 5. Tune or uninstall

- Budgets: env vars `BOUND_RUN_LINES`, `BOUND_GREP_MAX`, `BOUND_READ_SOFT`,
  `BOUND_READ_HARD`, `BOUND_DIFF_SOFT`, `BOUND_LOG_TAIL`, `BOUND_TREE_MAX`, `BOUND_DIR`.
- Artifacts: `bound stats` (raw vs delivered), `bound stats --clean` (remove >7 days).
- Uninstall: remove the `bound hook …` entries from the hook files above, delete
  `~/.cursor/skills/{bound,proto}`, `~/.claude/skills/{bound,proto}`,
  `~/.agents/skills/{bound,proto}`, and the binary.

---

## Part B — for agents

You are installing `bound`, a CLI that keeps tool output bounded. Follow these steps in
order. Run each command through your shell tool. Do not read files that these steps do
not name. Stop and report if a step fails after one retry.

### B1. Preconditions

```sh
go version || echo NO_GO
```

- `NO_GO`: report to the user that Go 1.22+ is required and stop. Do not install Go
  without being asked.
- `rg` is optional; `bound grep` has a built-in fallback.

### B2. Install

POSIX shell (Linux, macOS, WSL, Git Bash):

```sh
go install github.com/Kuksenok-i-s/bound@latest
BOUND_BIN="$(go env GOPATH)/bin/bound"; "$BOUND_BIN" version
```

If `bound` is not on PATH and `~/.local/bin` exists and is on PATH:

```sh
ln -sf "$(go env GOPATH)/bin/bound" ~/.local/bin/bound
```

Windows PowerShell:

```powershell
go install github.com/Kuksenok-i-s/bound@latest
$BOUND_BIN = "$(go env GOPATH)\bin\bound.exe"; & $BOUND_BIN version
```

In the steps below, replace `"$BOUND_BIN"` with `& $BOUND_BIN` on PowerShell. Hooks store
the absolute path themselves, so PATH is not required for them to work.

### B3. Wire hooks and skills

User-level (local machine):

```sh
"$BOUND_BIN" init --agent all --dry-run
"$BOUND_BIN" init --agent all
```

Project-level (when working inside a repository the user wants covered, including cloud
agents):

```sh
"$BOUND_BIN" init --project --agents-md
```

`init` merges into existing JSON and is idempotent. If it prints `FAIL … not valid JSON`,
report the file path to the user; do not overwrite their config.

### B4. Verify

```sh
"$BOUND_BIN" doctor
"$BOUND_BIN" run -- go version
```

Expected: `doctor` shows `ok` for the hosts you installed; `run` prints a `[bound run]`
envelope with `exit=0`.

### B5. Report

Tell the user, in ≤8 lines: binary path, hosts wired, skills installed, whether project
files were written, and that Codex users must run `/hooks` once to trust the hook.
Then use `bound` for the rest of the session: `bound run -- <cmd>` for tests/builds,
`bound grep`, `bound read <file> A:B`, `bound diff`, `bound log --tail 300`. Never `cat` a
spill file; use `bound read <spill> --grep RE -C 6`.

### Prompt you can paste into any agent

```text
Install and configure bound from https://github.com/Kuksenok-i-s/bound.
Follow INSTALL.md Part B exactly (fetch the raw file:
https://raw.githubusercontent.com/Kuksenok-i-s/bound/main/INSTALL.md).
Install user-level for all agents and project-level in this repository with --agents-md.
Report with `bound doctor` output. Then use bound for all tests, searches, reads, diffs and logs.
```
