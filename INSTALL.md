# Installing bound

Use Part A for manual installation and Part B when asking an agent to install it.
Prebuilt packages need **no Go installation**. Building from source needs Go 1.22+.
`rg` and `ctags` are optional. Supported targets: Linux, macOS and Windows,
with amd64 and arm64 builds; WSL uses the Linux package.

## Part A — for humans

### 1. Download a tested build

Open the [Build workflow](https://github.com/Kuksenok-i-s/bound/actions/workflows/build.yml),
choose a successful run for `main` (or your requested commit), and download its
`bound-<first 12 commit characters>` artifact. GitHub requires sign-in and repository
read access to download workflow artifacts. Artifacts expire after 90 days; a
maintainer can run the workflow manually to produce them again.

Extract the outer artifact ZIP into a new directory. It contains six platform
archives, the source archive, `BUILDINFO.txt` with the full commit, and `SHA256SUMS`.
Check the commit against the selected run before installation.

| System | Architecture (`uname -m` on Unix) | Package suffix |
|---|---|---|
| macOS Apple Silicon | arm64 | `darwin-arm64.tar.gz` |
| macOS Intel | x86_64 | `darwin-amd64.tar.gz` |
| Linux / WSL | x86_64 | `linux-amd64.tar.gz` |
| Linux / WSL | aarch64 / arm64 | `linux-arm64.tar.gz` |
| Windows x64 | AMD64 | `windows-amd64.zip` |
| Windows ARM64 | ARM64 | `windows-arm64.zip` |

Optional, with an already authenticated GitHub CLI:

```sh
gh run list --repo Kuksenok-i-s/bound --workflow build.yml --branch main --status success --limit 5 --json databaseId,headSha,url
# Replace RUN_ID and SHORT_COMMIT with values from the chosen run; use a new directory.
gh run download RUN_ID --repo Kuksenok-i-s/bound --name bound-SHORT_COMMIT --dir bound-download
```

See [GitHub's artifact download guide](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/download-workflow-artifacts).

### 2. Verify and install the binary

Run these commands from the directory containing `SHA256SUMS`. Do not install if
any checksum fails. The checksums detect corruption; also verify the repository
and full commit of the build you selected.

#### macOS / Linux / WSL

```sh
# macOS:
shasum -a 256 -c SHA256SUMS
# Linux / WSL instead:
sha256sum -c SHA256SUMS
```

Example for a **new installation on Apple Silicon**; change `BOUND_TARGET` using
the table. For an update, set `BOUND_BIN` to the existing absolute binary path
used by your hooks. Keep that path so the hooks continue to call the updated binary.

```sh
BOUND_BUILD_VERSION="$(sed -n 's/^commit=//p' BUILDINFO.txt | cut -c1-12)"
BOUND_TARGET=darwin-arm64
BOUND_UNPACK="$(mktemp -d)"
tar -xzf "bound-${BOUND_BUILD_VERSION}-${BOUND_TARGET}.tar.gz" -C "$BOUND_UNPACK"
BOUND_BIN="$HOME/.local/bin/bound"
mkdir -p "$(dirname "$BOUND_BIN")"
if [ -e "$BOUND_BIN" ]; then
  cp -p "$BOUND_BIN" "${BOUND_BIN}.backup-$(date +%Y%m%d%H%M%S)"
fi
install -m 0755 "$BOUND_UNPACK/bound" "$BOUND_BIN"
"$BOUND_BIN" version
```

Expected version: `bound <first 12 commit characters>`. Use the absolute path if
its directory is not on PATH; add that directory to your shell profile if you
want to type `bound`. Inspect existing symlinks before choosing an update path.

#### Windows (PowerShell)

Verify all files in the extracted artifact:

```powershell
Get-Content SHA256SUMS | ForEach-Object {
    $boundParts = $_ -split '\s+', 2
    $boundHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $boundParts[1]).Hash
    if ($boundHash -ine $boundParts[0]) { throw "Checksum mismatch: $($boundParts[1])" }
}
```

Extract the matching `windows-amd64.zip` or `windows-arm64.zip` to a new directory.
Copy `bound.exe` to a stable user directory, for example
`$env:LOCALAPPDATA\bound\bound.exe`. When updating, back up the old binary and
replace it at the absolute path already used by the hooks. Set that path and verify:

```powershell
$BOUND_BIN = Join-Path $env:LOCALAPPDATA 'bound\bound.exe'
& $BOUND_BIN version
if ($LASTEXITCODE -ne 0) { throw 'bound version failed' }
```

Use `& $BOUND_BIN` wherever the Unix examples use `"$BOUND_BIN"`. No Go or
system-wide installation is needed. Add the binary's directory to user PATH only
if you want to invoke it without its absolute path.

### 3. Alternative: install from source

Check `go version` first. This route needs Go 1.22+; do not install a Go toolchain
just to use the prebuilt package. To install current `main`:

```sh
go install github.com/Kuksenok-i-s/bound@main
BOUND_INSTALL_DIR="$(go env GOBIN)"
if [ -z "$BOUND_INSTALL_DIR" ]; then
  BOUND_GOPATH="$(go env GOPATH)"
  BOUND_INSTALL_DIR="${BOUND_GOPATH%%:*}/bin"
fi
BOUND_BIN="$BOUND_INSTALL_DIR/bound"
"$BOUND_BIN" version
go version -m "$BOUND_BIN"
```

PowerShell uses the same `go install` command. Resolve its output path as follows:

```powershell
$boundInstallDir = go env GOBIN
if (-not $boundInstallDir) {
    $boundInstallDir = Join-Path ((go env GOPATH) -split ';')[0] 'bin'
}
$BOUND_BIN = Join-Path $boundInstallDir 'bound.exe'
& $BOUND_BIN version
go version -m $BOUND_BIN
```

Replace `@main` with `@FULL_COMMIT` to pin a specific revision. `@latest` selects
the latest release when tags exist and may differ from `main`. A plain `go install`
build can print `bound dev`; inspect its module version with `go version -m`.
For a local checkout, verify `git rev-parse HEAD`, then use `make install`.

### 4. Configure the selected hosts

Back up existing hook JSON and the skill files that will be replaced. `init`
merges hook entries but writes the bundled skill files. Choose only the hosts you
want to configure; the following example configures Codex:

```sh
"$BOUND_BIN" init --agent codex --dry-run
"$BOUND_BIN" init --agent codex
```

Use `--agent cursor|claude|codex`, or `--agent all` when all three are intended.
`init` installs **both `bound` and `proto`**. Use `--no-skills` for hooks only.
For a `bound`-only installation, use `--no-skills`, extract the source archive
from the same verified artifact, and copy its `assets/skill/SKILL.md` into the
selected host's `skills/bound/SKILL.md` below. Back up an existing file first.

| Host | Hook file | User-level skills root |
|---|---|---|
| Cursor | `~/.cursor/hooks.json` | `~/.cursor/skills/` |
| Claude Code | `~/.claude/settings.json` | `~/.claude/skills/` |
| Codex | `~/.codex/hooks.json` | `~/.agents/skills/` |

`init` leaves existing matching hook entries in place, including their old binary
paths. On update, inspect those paths; rerunning `init` alone does not migrate
them. If JSON is invalid, preserve the file and report the error. Complete any
hook trust step required by the host (the Codex installer prints a `/hooks` note).

Project configuration is optional and only applies to the repository you intend
to configure. From that repository, using Codex as the example:

```sh
"$BOUND_BIN" init --agent codex --project --agents-md --dry-run
"$BOUND_BIN" init --agent codex --project --agents-md
```

This writes project hook/skill files and appends the bound section to `AGENTS.md`.
The agent environment must also contain the binary at the configured hook path.

### 5. Verify the installed binary and integration

```sh
"$BOUND_BIN" version
"$BOUND_BIN" doctor
"$BOUND_BIN" run -- "$BOUND_BIN" version
```

Expected: the installed binary runs and the envelope shows `exit=0`. These checks
work without Go. `doctor` reports all hosts and optional tools: `MISS` for an
unselected host or optional tool is not a reason to install it. A CLI-only install
can make `doctor` exit 1 because no host hooks are configured.

For each selected host, inspect its hook JSON and confirm the command points to
the installed binary. Check installed skill contents against the same source
commit, not just file presence. In the host, run a harmless command and confirm
that the intended hook is invoked; `doctor` alone does not prove host activation.

### 6. Update, tune or uninstall

- Update: choose and verify a new successful build, back up the current binary
  and affected configs/skills, replace the binary at the same path, then repeat
  the selected configuration and verification steps.
- Budgets: see [README.md](README.md#budgets) for `BOUND_*` settings.
- Spill statistics: `bound stats`; `bound stats --clean` removes old spill logs.
- Uninstall: remove only bound hook entries, the skills you installed, and the
  bound binary. Preserve unrelated entries and restore backups when appropriate.

## Part B — for agents

### B1. Establish scope

Identify OS/architecture, the existing binary and hook paths, and the hosts/project
requested by the user. Default to the current host, not all hosts. Project files
are only in scope when project integration was requested. Back up files you will
replace; preserve unrelated configuration. Do not install Go, GitHub CLI or other
tools merely to complete this installation. Read the source/config fragments
needed to verify paths and avoid overwriting local changes.

### B2. Install the verified binary

Prefer a prebuilt artifact using A1–A2. Select a successful build for the requested
full commit, or current `main` when no revision was specified; record the run ID
and commit. A download/authentication failure is not proof that a build is absent.
If prebuilt download is unavailable and Go is already installed, use A3 and report
which source revision was resolved. Check command exit codes: a failed build must
not be reported as a successful update of an older binary. Do not repeatedly
retry unchanged failures.

### B3. Apply the requested integration

Use A4 for the selected host. Inspect the dry run, then apply the authorized
changes without another confirmation. If only bound was requested, use
`--no-skills` and install just its skill from the matching source archive.
If configuration cannot be merged safely, preserve it and report the concrete
conflict. Keep existing hook paths aligned with the installed binary.

### B4. Verify and report

Use A5 through the **installed binary**, including a command run without Go.
Verify the revision, affected skill contents and configured hook paths. Report
binary path, run/commit, configured hosts and skills, backup paths, and any host
activation step still needed. Distinguish CLI verification from host activation.
Use bounded output for noisy tasks; retain full logs and reread relevant ranges.

### Prompt you can paste into an agent

```text
Install or update bound from https://github.com/Kuksenok-i-s/bound.
Read https://raw.githubusercontent.com/Kuksenok-i-s/bound/main/INSTALL.md and follow Part B.
Prefer the verified prebuilt package for my OS/architecture; record the build's commit and check SHA256SUMS.
Configure only the current host, user-level, with the bound skill only. Preserve other skills and settings.
Keep the existing binary path on update. Verify the installed binary, skill contents and hook paths, then report results and backups.
```
