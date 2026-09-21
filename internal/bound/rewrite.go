package bound

import (
	"os"
	"path/filepath"
	"strings"
)

// Decision is the outcome of inspecting a shell command before it runs.
type Decision struct {
	Action  string // "allow" | "rewrite" | "deny"
	Command string // replacement command when Action == "rewrite"
	Reason  string // human/agent-readable explanation
}

// Self returns the absolute path of the running bound binary, falling back to
// "bound" on PATH. Hooks must use an absolute path: the agent's shell may not
// share the user's PATH.
func Self() string {
	if p, err := os.Executable(); err == nil {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			p = r
		}
		return p
	}
	return "bound"
}

// Only simple argv commands are rewritten. Anything with pipes, redirects,
// substitutions, globs or compound operators is left alone: rewriting those
// is where other tools broke commands and caused retry turns.
var (
	// commands wrapped in `bound run --` (bounded envelope, spill file)
	wrapPrefixes = [][]string{
		{"go", "test"}, {"go", "build"}, {"go", "vet"}, {"go", "run"}, {"go", "generate"}, {"go", "mod"},
		{"golangci-lint"}, {"staticcheck"},
		{"pytest"}, {"python", "-m", "pytest"}, {"python3", "-m", "pytest"}, {"tox"}, {"ruff"}, {"mypy"}, {"pip", "install"},
		{"npm", "test"}, {"npm", "run"}, {"npm", "install"}, {"npm", "ci"}, {"npx"},
		{"pnpm"}, {"yarn"}, {"bun", "test"}, {"bun", "run"}, {"node", "--test"}, {"jest"}, {"vitest"}, {"tsc"}, {"eslint"},
		{"cargo", "test"}, {"cargo", "build"}, {"cargo", "check"}, {"cargo", "clippy"}, {"cargo", "run"},
		{"make"}, {"mvn"}, {"gradle"}, {"./gradlew"}, {"dotnet", "test"}, {"dotnet", "build"},
		{"docker", "build"}, {"docker", "compose"}, {"docker-compose"},
		{"terraform", "plan"}, {"terraform", "apply"}, {"kubectl", "get"}, {"kubectl", "describe"},
		{"rg"}, {"grep"}, {"ag"}, {"find"}, {"tree"}, {"ls", "-R"}, {"du"},
		{"git", "show"}, {"git", "blame"}, {"git", "grep"}, {"git", "ls-files"},
		{"curl"}, {"wget"}, {"http"},
	}
	// commands that must carry a bound before they run
	tailFlags = map[string][]string{
		"journalctl":     {"-n", "--lines", "--since", "-f", "--follow"},
		"kubectl":        {"--tail", "--since", "--since-time"},
		"docker":         {"--tail", "--since", "-n"},
		"docker-compose": {"--tail", "--since"},
	}
)

// RewriteShell decides what to do with a shell command the agent is about to run.
func RewriteShell(cmd string, cwd string, l Limits) Decision {
	self := quoteForHook(Self())
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" || strings.Contains(trimmed, "bound ") || strings.HasPrefix(trimmed, self) {
		return Decision{Action: "allow"}
	}
	argv, ok := SimpleArgv(trimmed)
	if !ok {
		return Decision{Action: "allow", Reason: "compound command; not rewritten"}
	}
	// Drop `sudo`/`env`/`time` prefixes for classification only.
	base := argv
	for len(base) > 1 && (base[0] == "time" || base[0] == "env" || base[0] == "nice") {
		base = base[1:]
	}
	name := filepath.Base(base[0])

	// Unbounded log readers: add a tail instead of refusing.
	if flags, ok := tailFlags[name]; ok && isLogRead(name, base) {
		if !hasAny(base, flags) {
			add := "-n 300"
			if name != "journalctl" {
				add = "--tail=300"
			}
			argv = append(argv, strings.Fields(add)...)
			return Decision{Action: "rewrite", Command: self + " run -- " + ShellQuote(argv),
				Reason: "log read without a bound; added " + add + " and wrapped in bound run"}
		}
		return Decision{Action: "rewrite", Command: self + " run -- " + ShellQuote(argv), Reason: "wrapped in bound run"}
	}

	switch name {
	case "cat", "less", "more", "bat", "head", "tail", "type", "Get-Content", "gc":
		if f, ok := singleFile(base[1:], cwd); ok && name != "head" && name != "tail" {
			return Decision{Action: "rewrite", Command: self + " read " + quoteWord(f), Reason: "file read routed through bound read (outline for big files)"}
		}
		return Decision{Action: "allow"}
	case "git":
		if len(base) > 1 {
			switch base[1] {
			case "diff":
				return Decision{Action: "rewrite", Command: self + " diff " + ShellQuote(base[2:]), Reason: "git diff routed through bound diff (--stat first)"}
			case "log":
				if !hasAny(base, []string{"-n", "--max-count", "--oneline", "-1", "-2", "-3", "-5", "-10", "-20"}) && !hasNumFlag(base) {
					argv = append(argv, "-n", "20")
					return Decision{Action: "rewrite", Command: self + " run -- " + ShellQuote(argv), Reason: "git log without a count; added -n 20"}
				}
			case "status":
				if len(base) == 2 {
					return Decision{Action: "rewrite", Command: "git status --short --branch", Reason: "compact status"}
				}
				return Decision{Action: "allow"}
			}
		}
	case "go":
		// `go test -v ./...` dumps every passing test; -v adds nothing for a repo-wide run.
		if len(base) > 1 && base[1] == "test" && contains(base, "-v") && hasEllipsis(base) {
			argv = removeArg(argv, "-v")
			return Decision{Action: "rewrite", Command: self + " run -- " + ShellQuote(argv), Reason: "dropped -v from repo-wide go test; failures are still reported in full"}
		}
	case "find":
		if isWindows { // cmd.exe `find` is a string filter, not a file walker
			return Decision{Action: "allow"}
		}
		if len(base) == 1 || (len(base) >= 2 && (base[1] == "." || base[1] == "/")) && !contains(base, "-maxdepth") && !contains(base, "-name") && !contains(base, "-path") {
			return Decision{Action: "rewrite", Command: self + " tree " + strings.Join(base[1:min(len(base), 2)], " "), Reason: "unfiltered find routed through bound tree (depth 3, 500 entries)"}
		}
	}

	for _, p := range wrapPrefixes {
		if hasPrefix(base, p) {
			return Decision{Action: "rewrite", Command: self + " run -- " + ShellQuote(argv), Reason: "wrapped in bound run (bounded envelope, full output spilled to file)"}
		}
	}
	return Decision{Action: "allow"}
}

func isLogRead(name string, argv []string) bool {
	switch name {
	case "journalctl":
		return true
	case "kubectl", "docker", "docker-compose":
		return len(argv) > 1 && argv[1] == "logs" || (len(argv) > 2 && argv[1] == "compose" && argv[2] == "logs")
	}
	return false
}

func hasPrefix(argv, prefix []string) bool {
	if len(argv) < len(prefix) {
		return false
	}
	for i, p := range prefix {
		a := argv[i]
		if i == 0 {
			a = filepath.Base(a)
		}
		if a != p {
			return false
		}
	}
	return true
}

func hasAny(argv []string, flags []string) bool {
	for _, a := range argv {
		for _, f := range flags {
			if a == f || strings.HasPrefix(a, f+"=") {
				return true
			}
		}
	}
	return false
}

func hasNumFlag(argv []string) bool {
	for _, a := range argv {
		if len(a) > 1 && a[0] == '-' && a[1] >= '0' && a[1] <= '9' {
			return true
		}
	}
	return false
}

func hasEllipsis(argv []string) bool {
	for _, a := range argv {
		if strings.HasSuffix(a, "...") {
			return true
		}
	}
	return false
}

func removeArg(argv []string, s string) []string {
	out := make([]string, 0, len(argv))
	for _, a := range argv {
		if a != s {
			out = append(out, a)
		}
	}
	return out
}

func singleFile(args []string, cwd string) (string, bool) {
	var files []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return "", false
		}
		files = append(files, a)
	}
	if len(files) != 1 {
		return "", false
	}
	p := files[0]
	if !filepath.IsAbs(p) && cwd != "" {
		p = filepath.Join(cwd, p)
	}
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return "", false
	}
	return files[0], true
}
