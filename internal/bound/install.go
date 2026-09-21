package bound

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kuksenok-i-s/bound/assets"
)

// Init installs pre-tool hooks and the skill for the selected agents.
// User-level by default; --project writes into the current repository so the
// same guardrails travel with the repo (cloud agents load project hooks).
func Init(args []string, w io.Writer) int {
	flags, _, _ := parseFlags(args, "project", "no-skills", "agents-md", "dry-run")
	agent := flags["agent"]
	if agent == "" {
		agent = "all"
	}
	dry := flags["dry-run"] == "true"
	project := flags["project"] == "true"
	self := Self()
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()

	base := func(dot string) string {
		if project {
			return filepath.Join(cwd, dot)
		}
		return filepath.Join(home, dot)
	}
	want := func(a string) bool { return agent == "all" || agent == a }
	report := func(format string, a ...any) { fmt.Fprintf(w, format+"\n", a...) }
	failed := 0

	if want("cursor") {
		path := filepath.Join(base(".cursor"), "hooks.json")
		entry := map[string]interface{}{"command": self + " hook cursor", "matcher": "Shell|Read|Grep"}
		err := mergeJSON(path, dry, func(doc map[string]interface{}) {
			if _, ok := doc["version"]; !ok {
				doc["version"] = 1
			}
			hooks := ensureMap(doc, "hooks")
			hooks["preToolUse"] = appendUnique(hooks["preToolUse"], entry, "bound hook cursor")
		})
		failed += done(report, "cursor hooks", path, err, dry)
	}
	if want("claude") {
		path := filepath.Join(base(".claude"), "settings.json")
		entry := map[string]interface{}{
			"matcher": "Bash|Read|Grep",
			"hooks":   []interface{}{map[string]interface{}{"type": "command", "command": self + " hook claude", "timeout": 10}},
		}
		err := mergeJSON(path, dry, func(doc map[string]interface{}) {
			hooks := ensureMap(doc, "hooks")
			hooks["PreToolUse"] = appendUnique(hooks["PreToolUse"], entry, "bound hook claude")
		})
		failed += done(report, "claude hooks", path, err, dry)
	}
	if want("codex") {
		path := filepath.Join(base(".codex"), "hooks.json")
		entry := map[string]interface{}{
			"matcher": "Bash",
			"hooks":   []interface{}{map[string]interface{}{"type": "command", "command": self + " hook codex", "timeout": 10}},
		}
		err := mergeJSON(path, dry, func(doc map[string]interface{}) {
			hooks := ensureMap(doc, "hooks")
			hooks["PreToolUse"] = appendUnique(hooks["PreToolUse"], entry, "bound hook codex")
		})
		failed += done(report, "codex hooks", path, err, dry)
		report("  note: Codex hooks are on by default; run /hooks in Codex once to trust the new hook")
	}

	if flags["no-skills"] != "true" {
		roots := map[string]string{
			"cursor": filepath.Join(base(".cursor"), "skills"),
			"claude": filepath.Join(base(".claude"), "skills"),
			"codex":  filepath.Join(base(".agents"), "skills"),
		}
		for _, a := range []string{"cursor", "claude", "codex"} {
			if !want(a) {
				continue
			}
			for src, name := range assets.Skills {
				body, _ := assets.FS.ReadFile(src)
				p := filepath.Join(roots[a], name, "SKILL.md")
				err := writeFile(p, body, dry)
				failed += done(report, a+" skill "+name, p, err, dry)
			}
		}
	}
	if flags["agents-md"] == "true" {
		snippet, _ := assets.FS.ReadFile("AGENTS.snippet.md")
		p := filepath.Join(cwd, "AGENTS.md")
		existing, _ := os.ReadFile(p)
		if strings.Contains(string(existing), "## Context budget (bound)") {
			report("skip  AGENTS.md already contains the bound section")
		} else {
			content := append(existing, []byte("\n")...)
			content = append(content, snippet...)
			err := writeFile(p, content, dry)
			failed += done(report, "AGENTS.md", p, err, dry)
		}
	}
	report("binary: %s  spill dir: %s", self, SpillDir())
	if failed > 0 {
		return 1
	}
	return 0
}

func done(report func(string, ...any), what, path string, err error, dry bool) int {
	switch {
	case err != nil:
		report("FAIL  %-13s %s: %v", what, path, err)
		return 1
	case dry:
		report("would %-13s %s", what, path)
	default:
		report("ok    %-13s %s", what, path)
	}
	return 0
}

func ensureMap(doc map[string]interface{}, key string) map[string]interface{} {
	if m, ok := doc[key].(map[string]interface{}); ok {
		return m
	}
	m := map[string]interface{}{}
	doc[key] = m
	return m
}

// appendUnique appends entry to a JSON array unless an element already
// mentions marker (so re-running init is idempotent).
func appendUnique(list interface{}, entry map[string]interface{}, marker string) []interface{} {
	arr, _ := list.([]interface{})
	for _, el := range arr {
		b, _ := json.Marshal(el)
		if strings.Contains(string(b), marker) {
			return arr
		}
	}
	return append(arr, entry)
}

func mergeJSON(path string, dry bool, mutate func(map[string]interface{})) error {
	doc := map[string]interface{}{}
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) > 0 {
		if err := json.Unmarshal(b, &doc); err != nil {
			return fmt.Errorf("existing file is not valid JSON: %w", err)
		}
	}
	mutate(doc)
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, append(out, '\n'), dry)
}

func writeFile(path string, content []byte, dry bool) error {
	if dry {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}
