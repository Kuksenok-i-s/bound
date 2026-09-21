package bound

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ignoredDirs are never searched or listed unless the caller names them.
var ignoredDirs = []string{
	".git", "node_modules", "vendor", "dist", "build", "out", "target", ".next", ".nuxt",
	"coverage", "__pycache__", ".venv", "venv", ".cache", ".idea", ".vscode", ".terraform",
	".gradle", "bin", "obj", ".tox", ".mypy_cache", ".pytest_cache", ".turbo",
}

var ignoredGlobs = []string{"*.lock", "*.min.js", "*.min.css", "*.map", "*.sum", "*.pb.go", "*_generated.go", "*.snap", "*.wasm"}

type match struct {
	file string
	line string
	text string
}

// Grep runs ripgrep (or grep -r) and prints a bounded, file-grouped result set
// with total/returned counts, so the agent refines the query instead of paging.
func Grep(args []string, w io.Writer) int {
	l := DefaultLimits()
	flags, pos, rest := parseFlags(args, "i", "w", "F", "hidden")
	pos = append(pos, rest...)
	if len(pos) == 0 {
		fmt.Fprintln(os.Stderr, "usage: bound grep [-n N] [-i] [-w] [-F] [-t ext] [-g glob] <pattern> [paths...]")
		return 2
	}
	max := flagInt(flags, "n", l.GrepDefault, l.GrepHard)
	pattern, paths := pos[0], pos[1:]
	if len(paths) == 0 {
		paths = []string{"."}
	}

	out, tool, err := runSearch(flags, pattern, paths)
	if err != nil && len(out) == 0 {
		fmt.Fprintf(w, "[bound grep] %s failed: %v\n", tool, err)
		return 1
	}
	var all []match
	perFile := map[string]int{}
	var order []string
	for _, ln := range bytes.Split(out, []byte{'\n'}) {
		if len(ln) == 0 {
			continue
		}
		parts := strings.SplitN(string(ln), ":", 3)
		if len(parts) < 3 {
			continue
		}
		if perFile[parts[0]] == 0 {
			order = append(order, parts[0])
		}
		perFile[parts[0]]++
		all = append(all, match{parts[0], parts[1], strings.TrimSpace(stripANSI(parts[2]))})
	}

	e := newEnvelope(l.RunChars)
	truncated := len(all) > max
	e.linef("[bound grep] %q matches=%d files=%d returned=%d truncated=%v (%s)", pattern, len(all), len(order), min(len(all), max), truncated, tool)
	if len(all) == 0 {
		e.flush(w)
		return 1
	}
	if truncated {
		files := append([]string{}, order...)
		sort.Slice(files, func(i, j int) bool { return perFile[files[i]] > perFile[files[j]] })
		e.section("top files")
		for i, f := range files {
			if i >= 10 {
				break
			}
			e.linef("%5d  %s", perFile[f], f)
		}
	}
	// Fair share per file so one noisy file doesn't consume the budget.
	quota := max / len(order)
	if quota < 3 {
		quota = 3
	}
	shown := 0
	used := map[string]int{}
	var kept []match
	for _, m := range all {
		if used[m.file] >= quota {
			continue
		}
		used[m.file]++
		kept = append(kept, m)
		shown++
		if shown >= max {
			break
		}
	}
	// Fill remaining budget in order if quota left gaps.
	if shown < max && shown < len(all) {
		for _, m := range all {
			if shown >= max {
				break
			}
			if used[m.file] >= quota && !inKept(kept, m) {
				kept = append(kept, m)
				shown++
			}
		}
	}
	e.section("matches")
	cur := ""
	for _, m := range kept {
		if m.file != cur {
			cur = m.file
			e.linef("%s (%d)", m.file, perFile[m.file])
		}
		t := m.text
		if len(t) > 160 {
			t = t[:160] + "…"
		}
		e.linef("  %s: %s", m.line, t)
	}
	if truncated {
		e.line("next: add -t <ext>, a path, or a longer pattern; bound read <file> A:B for context")
	}
	delivered := e.flush(w)
	ledger("grep", int64(len(out)), delivered, pattern)
	return 0
}

func inKept(kept []match, m match) bool {
	for _, k := range kept {
		if k == m {
			return true
		}
	}
	return false
}

func runSearch(flags map[string]string, pattern string, paths []string) ([]byte, string, error) {
	if rg, err := exec.LookPath("rg"); err == nil && os.Getenv("BOUND_NO_RG") == "" {
		argv := []string{"-n", "--no-heading", "-H", "--color", "never", "--max-columns", "300", "--max-columns-preview"}
		if flags["i"] == "true" {
			argv = append(argv, "-i")
		} else {
			argv = append(argv, "-S")
		}
		if flags["w"] == "true" {
			argv = append(argv, "-w")
		}
		if flags["F"] == "true" {
			argv = append(argv, "-F")
		}
		if flags["hidden"] == "true" {
			argv = append(argv, "--hidden")
		}
		if t, ok := flags["t"]; ok {
			argv = append(argv, "-g", "*."+strings.TrimPrefix(t, "."))
		}
		if g, ok := flags["g"]; ok {
			argv = append(argv, "-g", g)
		}
		for _, d := range ignoredDirs {
			argv = append(argv, "-g", "!"+d+"/")
		}
		for _, g := range ignoredGlobs {
			argv = append(argv, "-g", "!"+g)
		}
		argv = append(argv, "-e", pattern, "--")
		argv = append(argv, paths...)
		out, err := exec.Command(rg, argv...).Output()
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			err = nil // no matches
		}
		return out, "rg", err
	}
	out, err := walkSearch(flags, pattern, paths)
	return out, "walk", err
}

// walkSearch is the dependency-free fallback: a regexp walk over the tree
// honouring the same ignore lists and flags, emitting rg-style file:line:text.
func walkSearch(flags map[string]string, pattern string, paths []string) ([]byte, error) {
	expr := pattern
	if flags["F"] == "true" {
		expr = regexp.QuoteMeta(expr)
	}
	if flags["w"] == "true" {
		expr = `\b(?:` + expr + `)\b`
	}
	if flags["i"] == "true" || (flags["i"] != "true" && strings.ToLower(pattern) == pattern) {
		expr = "(?i)" + expr // smart case, like rg -S
	}
	rx, err := regexp.Compile(expr)
	if err != nil {
		return nil, err
	}
	ext := ""
	if t, ok := flags["t"]; ok {
		ext = "." + strings.TrimPrefix(t, ".")
	}
	glob := flags["g"]
	skipDir := map[string]bool{}
	for _, d := range ignoredDirs {
		skipDir[d] = true
	}
	var buf bytes.Buffer
	const maxFile = 4 << 20
	for _, root := range paths {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			name := d.Name()
			if d.IsDir() {
				if p != root && (skipDir[name] || (strings.HasPrefix(name, ".") && flags["hidden"] != "true")) {
					return filepath.SkipDir
				}
				return nil
			}
			if ext != "" && !strings.EqualFold(filepath.Ext(name), ext) {
				return nil
			}
			if glob != "" {
				if ok, _ := filepath.Match(glob, name); !ok {
					return nil
				}
			}
			for _, g := range ignoredGlobs {
				if ok, _ := filepath.Match(g, name); ok {
					return nil
				}
			}
			info, err := d.Info()
			if err != nil || info.Size() > maxFile {
				return nil
			}
			f, err := os.Open(p)
			if err != nil {
				return nil
			}
			defer f.Close()
			s := scanner(f)
			n := 0
			for s.Scan() {
				n++
				line := s.Bytes()
				if n == 1 && bytes.IndexByte(line, 0) >= 0 {
					return nil // binary
				}
				if rx.Match(line) {
					t := string(line)
					if len(t) > 300 {
						t = t[:300]
					}
					fmt.Fprintf(&buf, "%s:%d:%s\n", p, n, t)
				}
			}
			return nil
		})
	}
	return buf.Bytes(), nil
}
