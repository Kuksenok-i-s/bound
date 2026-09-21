package bound

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Doctor verifies an installation: binary, PATH, spill dir, external tools,
// and which hosts have hooks and skills installed. Exit 1 if nothing is wired.
func Doctor(args []string, w io.Writer) int {
	home, _ := os.UserHomeDir()
	self := Self()
	ok := func(cond bool) string {
		if cond {
			return "ok  "
		}
		return "MISS"
	}
	fmt.Fprintf(w, "[bound doctor] %s\n", version())
	fmt.Fprintf(w, "%s binary      %s\n", ok(true), self)
	_, onPath := exec.LookPath("bound")
	fmt.Fprintf(w, "%s on PATH     %v (hooks use the absolute path, PATH only matters for the agent typing `bound`)\n", ok(onPath == nil), onPath == nil)
	dir := SpillDir()
	probe := filepath.Join(dir, ".probe")
	writable := os.WriteFile(probe, []byte("x"), 0o644) == nil
	_ = os.Remove(probe)
	fmt.Fprintf(w, "%s spill dir   %s writable=%v\n", ok(writable), dir, writable)
	for _, tool := range []string{"rg", "git", "ctags"} {
		_, err := exec.LookPath(tool)
		note := ""
		if err != nil {
			switch tool {
			case "rg":
				note = " (falls back to grep -r)"
			case "ctags":
				note = " (falls back to regex outlines)"
			}
		}
		fmt.Fprintf(w, "%s tool %-6s %v%s\n", ok(err == nil), tool, err == nil, note)
	}

	wired := 0
	check := func(host, file, marker string) {
		b, err := os.ReadFile(file)
		has := err == nil && strings.Contains(string(b), marker)
		if has {
			wired++
		}
		fmt.Fprintf(w, "%s %-6s hooks  %s\n", ok(has), host, file)
	}
	check("cursor", filepath.Join(home, ".cursor", "hooks.json"), "bound hook cursor")
	check("claude", filepath.Join(home, ".claude", "settings.json"), "bound hook claude")
	check("codex", filepath.Join(home, ".codex", "hooks.json"), "bound hook codex")
	for _, s := range []struct{ host, dir string }{
		{"cursor", filepath.Join(home, ".cursor", "skills")},
		{"claude", filepath.Join(home, ".claude", "skills")},
		{"codex", filepath.Join(home, ".agents", "skills")},
	} {
		for _, name := range []string{"bound", "proto"} {
			p := filepath.Join(s.dir, name, "SKILL.md")
			_, err := os.Stat(p)
			fmt.Fprintf(w, "%s %-6s skill  %s\n", ok(err == nil), s.host, p)
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if b, err := os.ReadFile(filepath.Join(cwd, "AGENTS.md")); err == nil {
			has := strings.Contains(string(b), "## Context budget (bound)")
			fmt.Fprintf(w, "%s project AGENTS.md has bound section\n", ok(has))
		}
		if _, err := os.Stat(filepath.Join(cwd, ".cursor", "hooks.json")); err == nil {
			fmt.Fprintf(w, "ok   project .cursor/hooks.json present (cloud agents load this)\n")
		}
	}
	if wired == 0 {
		fmt.Fprintln(w, "no host hooks installed: run `bound init --agent all` (or --agent cursor|claude|codex)")
		return 1
	}
	return 0
}

// version is set from main via SetVersion.
var versionStr = "dev"

// SetVersion records the build version for doctor output.
func SetVersion(v string) { versionStr = v }

func version() string { return "bound " + versionStr }
