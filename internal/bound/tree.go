package bound

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Tree lists a directory to a bounded depth and entry count, skipping
// dependency and build directories.
func Tree(args []string, w io.Writer) int {
	l := DefaultLimits()
	flags, pos, _ := parseFlags(args, "all")
	root := "."
	if len(pos) > 0 {
		root = pos[0]
	}
	depth := flagInt(flags, "depth", l.TreeDepth, 8)
	max := flagInt(flags, "max", l.TreeMax, 2000)
	skip := map[string]bool{}
	if flags["all"] != "true" {
		for _, d := range ignoredDirs {
			skip[d] = true
		}
	}
	root = filepath.Clean(root)
	var out []string
	dirs, files, skipped, shown := 0, 0, 0, 0
	truncated := false
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if p == root {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		level := strings.Count(rel, string(filepath.Separator))
		if level >= depth {
			return nil // shouldn't happen: parents at depth-1 are skipped below
		}
		if d.IsDir() {
			if skip[d.Name()] || (strings.HasPrefix(d.Name(), ".") && flags["all"] != "true") {
				skipped++
				return filepath.SkipDir
			}
			dirs++
			if level == depth-1 {
				// Last visible level: show the directory with its entry count, don't descend.
				n := 0
				if ents, err := os.ReadDir(p); err == nil {
					n = len(ents)
				}
				if shown < max {
					out = append(out, fmt.Sprintf("%s%s/ (%d)", strings.Repeat("  ", level), d.Name(), n))
					shown++
				}
				return filepath.SkipDir
			}
		} else {
			files++
		}
		if shown >= max {
			truncated = true
			return filepath.SkipAll
		}
		name := d.Name()
		if d.IsDir() {
			name += "/"
		}
		out = append(out, fmt.Sprintf("%s%s", strings.Repeat("  ", level), name))
		shown++
		return nil
	})
	if err != nil {
		fmt.Fprintf(w, "[bound tree] %v\n", err)
		return 1
	}
	sort.Strings(out[:0]) // keep walk order (already sorted by WalkDir)
	e := newEnvelope(l.RunChars * 2)
	e.linef("[bound tree] %s depth=%d dirs=%d files=%d shown=%d skipped_dirs=%d truncated=%v", root, depth, dirs, files, shown, skipped, truncated)
	e.lines(out)
	if truncated {
		e.line("next: bound tree <subdir> --depth N, or --max N (hard 2000)")
	}
	e.flush(w)
	_ = os.Stdout
	return 0
}
