package bound

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var statLineRE = regexp.MustCompile(`^\s*(\S.*?)\s+\|\s+(\d+)`)

// Diff shows `git diff --stat` first and per-file hunks only when paths are
// given; a diff larger than the soft limit is spilled and summarised.
func Diff(args []string, w io.Writer) int {
	l := DefaultLimits()
	var gitArgs, paths []string
	seenSep := false
	for _, a := range args {
		switch {
		case a == "--":
			seenSep = true
		case seenSep:
			paths = append(paths, a)
		case !strings.HasPrefix(a, "-") && fileExists(a):
			paths = append(paths, a)
		default:
			gitArgs = append(gitArgs, a)
		}
	}
	e := newEnvelope(l.RunChars * 2)
	if len(paths) == 0 {
		argv := append([]string{"diff", "--stat=140", "--stat-count=400"}, gitArgs...)
		out, err := git(argv...)
		if err != nil {
			fmt.Fprintf(w, "[bound diff] git %s: %v\n%s", strings.Join(argv, " "), err, out)
			return 1
		}
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		if len(lines) == 1 && lines[0] == "" {
			fmt.Fprintf(w, "[bound diff] no changes (%s)\n", strings.Join(gitArgs, " "))
			return 0
		}
		e.linef("[bound diff] git diff --stat %s", strings.Join(gitArgs, " "))
		e.line(lines[len(lines)-1]) // summary line
		type fs struct {
			name string
			n    int
		}
		var files []fs
		for _, ln := range lines[:len(lines)-1] {
			if m := statLineRE.FindStringSubmatch(ln); m != nil {
				n, _ := strconv.Atoi(m[2])
				files = append(files, fs{m[1], n})
			}
		}
		sort.SliceStable(files, func(i, j int) bool { return files[i].n > files[j].n })
		limit := 40
		for i, f := range files {
			if i >= limit {
				e.linef("… %d more files", len(files)-limit)
				break
			}
			e.linef("%6d  %s", f.n, f.name)
		}
		e.line("next: bound diff [args] -- <file>   (per-file hunks)")
		delivered := e.flush(w)
		ledger("diff", int64(len(out)), delivered, "stat")
		return 0
	}

	argv := append([]string{"diff", "--no-color"}, gitArgs...)
	argv = append(argv, "--")
	argv = append(argv, paths...)
	out, err := git(argv...)
	if err != nil {
		fmt.Fprintf(w, "[bound diff] git %s: %v\n%s", strings.Join(argv, " "), err, out)
		return 1
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	total := len(lines)
	if out == "" {
		total = 0
	}
	e.linef("[bound diff] %s lines=%d bytes=%s (%s)", strings.Join(paths, " "), total, Human(int64(len(out))), Tokens(int64(len(out))))
	if total <= l.DiffSoft {
		e.lines(lines)
		delivered := e.flush(w)
		ledger("diff", int64(len(out)), delivered, strings.Join(paths, " "))
		return 0
	}
	spill, err := NewSpill("diff")
	if err == nil {
		_, _ = spill.WriteString(out)
		_ = spill.Close()
		e.linef("full: %s", spill.Name())
	}
	e.section("hunks")
	for _, ln := range lines {
		if strings.HasPrefix(ln, "diff --git") || strings.HasPrefix(ln, "@@") {
			e.line(ln)
		}
	}
	e.section(fmt.Sprintf("first %d lines", l.DiffSoft/2))
	e.lines(lines[:l.DiffSoft/2])
	if spill != nil {
		e.linef("next: bound read %s A:B   | bound diff -- <single file>", spill.Name())
	}
	delivered := e.flush(w)
	ledger("diff", int64(len(out)), delivered, strings.Join(paths, " "))
	return 0
}

func git(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"--no-pager"}, args...)...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
