package bound

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Run executes a command with stdout+stderr captured to a spill file and prints
// a bounded envelope: exit code, size, a tool-specific summary (test failures,
// build errors), head/tail excerpts and the exact next command to dig deeper.
func Run(args []string, w io.Writer) int {
	l := DefaultLimits()
	flags, pos, rest := parseFlags(args, "c", "quiet")
	argv := rest
	if len(argv) == 0 {
		argv = pos
	}
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "bound run: no command; usage: bound run [--timeout 10m] [--lines N] [-c] -- <cmd...>")
		return 2
	}
	if flags["c"] == "true" || (len(argv) == 1 && strings.ContainsAny(argv[0], " |&;<>$`")) {
		argv = shellArgv(strings.Join(argv, " "))
	}
	timeout := 10 * time.Minute
	if v, ok := flags["timeout"]; ok {
		if d, err := time.ParseDuration(v); err == nil {
			timeout = d
		}
	}
	lines := flagInt(flags, "lines", l.RunLines, 400)

	spill, err := NewSpill(argv[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "bound run:", err)
		return 2
	}
	path := spill.Name()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = spill
	cmd.Stderr = spill
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "FORCE_COLOR=0", "TERM=dumb", "CLICOLOR=0", "GIT_PAGER=", "PAGER=")
	cmd.WaitDelay = 2 * time.Second
	start := time.Now()
	runErr := cmd.Run()
	dur := time.Since(start).Round(100 * time.Millisecond)
	_ = spill.Close()

	exit := 0
	switch {
	case runErr == nil:
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		exit = 124
	default:
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			exit = ee.ExitCode()
		} else {
			exit = 127
		}
	}

	total, size, _ := countLines(path)
	e := newEnvelope(l.RunChars)
	display := ShellQuote(argv)
	if len(display) > 160 {
		display = display[:160] + "…"
	}
	e.linef("[bound run] %s", display)
	status := fmt.Sprintf("exit=%d lines=%d bytes=%s (%s) time=%s", exit, total, Human(size), Tokens(size), dur)
	if exit == 124 {
		status += " TIMEOUT"
	}
	if exit == 127 && runErr != nil {
		status += " error=" + runErr.Error()
	}
	e.line(status)

	// Small output: show verbatim, nothing hidden.
	if total <= lines && size <= int64(l.RunChars)*2/3 {
		head, _, _ := headTail(path, total, 0)
		e.lines(head)
		delivered := e.flush(w)
		ledger("run", size, delivered, argv[0])
		_ = os.Remove(path)
		return exit
	}

	e.linef("full: %s", path)
	kind, summary := summarize(argv, path)
	if len(summary) > 0 {
		e.section("summary (" + kind + ")")
		e.lines(summary)
	}
	h, t := lines/4, lines-lines/4
	if len(summary) > 1 {
		// A recognised parser found the signal; excerpts are only for orientation.
		h, t = lines/6, lines/3
	}
	head, tail, _ := headTail(path, h, t)
	e.section(fmt.Sprintf("head (%d)", len(head)))
	e.lines(head)
	e.section(fmt.Sprintf("tail (%d)", len(tail)))
	e.lines(tail)
	e.linef("next: bound read %s --grep '%s' -C 6   | bound read %s A:B", path, nextPattern(kind), path)
	delivered := e.flush(w)
	ledger("run", size, delivered, argv[0])
	return exit
}

func nextPattern(kind string) string {
	switch kind {
	case "go test":
		return "--- FAIL|panic:|FAIL\\s"
	case "pytest":
		return "^FAILED|^ERROR|^E "
	case "jest":
		return "●|✕|FAIL "
	case "cargo":
		return "FAILED|panicked|^error"
	}
	return "error|fail|panic|exception|fatal"
}

// summarize picks a parser from argv and returns (kind, lines).
func summarize(argv []string, path string) (string, []string) {
	joined := strings.ToLower(strings.Join(argv, " "))
	var kind string
	switch {
	case strings.Contains(joined, "go test") || (strings.HasSuffix(argv[0], "go") && contains(argv, "test")):
		kind = "go test"
	case strings.Contains(joined, "pytest"):
		kind = "pytest"
	case strings.Contains(joined, "jest") || strings.Contains(joined, "vitest") ||
		regexp.MustCompile(`\b(npm|pnpm|yarn|bun)\b.*\btest\b`).MatchString(joined):
		kind = "jest"
	case strings.Contains(joined, "cargo"):
		kind = "cargo"
	case strings.HasSuffix(argv[0], "go") && (contains(argv, "build") || contains(argv, "vet")):
		kind = "go build"
	default:
		kind = "generic"
	}
	f, err := os.Open(path)
	if err != nil {
		return kind, nil
	}
	defer f.Close()
	var out []string
	switch kind {
	case "go test", "go build":
		out = parseGo(f)
	case "pytest":
		out = parsePytest(f)
	case "jest":
		out = parseJest(f)
	case "cargo":
		out = parseCargo(f)
	default:
		out = parseGeneric(f)
	}
	return kind, out
}

func contains(argv []string, s string) bool {
	for _, a := range argv {
		if a == s {
			return true
		}
	}
	return false
}

var (
	goFailRE    = regexp.MustCompile(`^\s*--- FAIL: (\S+)`)
	goPkgRE     = regexp.MustCompile(`^(ok|FAIL|\?)\s+(\S+)`)
	goLocRE     = regexp.MustCompile(`^\s+(\S+\.go:\d+): (.*)`)
	goBuildRE   = regexp.MustCompile(`^(\S+\.go:\d+:\d+: .*|# .*|.*: undefined: .*|.*cannot .*|vet: .*)$`)
	goPanicRE   = regexp.MustCompile(`^panic: (.*)`)
	pyFailRE    = regexp.MustCompile(`^(FAILED|ERROR) (\S+)(?: - (.*))?$`)
	pySummRE    = regexp.MustCompile(`^=+ (.*(passed|failed|error|skipped|no tests ran).*) =+$`)
	pyERE       = regexp.MustCompile(`^E\s+(.*)`)
	jestSummRE  = regexp.MustCompile(`^(Tests|Test Suites|Test Files):\s+(.*)`)
	jestFailRE  = regexp.MustCompile(`^\s*(●|✕|×|✗|FAIL) (.*)`)
	cargoResRE  = regexp.MustCompile(`^test result: (.*)`)
	cargoFailRE = regexp.MustCompile(`^test (\S+) \.\.\. FAILED`)
	cargoErrRE  = regexp.MustCompile(`^(error(\[E\d+\])?: .*|.*panicked at .*)`)
	genericRE   = regexp.MustCompile(`(?i)\b(error|fail(ed|ure)?|panic|exception|fatal|traceback|denied|timeout)\b`)
)

func parseGo(r io.Reader) []string {
	var fails, pkgs, build, panics []string
	okN, failN := 0, 0
	lastFail := -1
	pending := "" // location seen after "=== RUN" but before "--- FAIL" (-v mode)
	loc := func(m []string) string {
		msg := m[2]
		if len(msg) > 120 {
			msg = msg[:120] + "…"
		}
		return "  @" + m[1] + ": " + msg
	}
	s := scanner(r)
	for s.Scan() {
		line := stripANSI(s.Text())
		if strings.HasPrefix(line, "=== RUN") {
			pending = ""
			lastFail = -1
			continue
		}
		if m := goFailRE.FindStringSubmatch(line); m != nil {
			fails = append(fails, "FAIL "+m[1]+pending)
			pending = ""
			lastFail = len(fails) - 1
			continue
		}
		if m := goLocRE.FindStringSubmatch(line); m != nil {
			if lastFail >= 0 && !strings.Contains(fails[lastFail], "  @") {
				fails[lastFail] += loc(m)
			} else if lastFail < 0 && pending == "" {
				pending = loc(m)
			}
			continue
		}
		if m := goPkgRE.FindStringSubmatch(line); m != nil {
			switch m[1] {
			case "ok":
				okN++
			case "FAIL":
				failN++
				pkgs = append(pkgs, line)
			}
			lastFail = -1
			continue
		}
		if m := goPanicRE.FindStringSubmatch(line); m != nil && len(panics) < 5 {
			panics = append(panics, "panic: "+m[1])
			continue
		}
		if goBuildRE.MatchString(line) && len(build) < 20 && !strings.HasPrefix(line, "# ") {
			build = append(build, line)
		}
	}
	out := []string{fmt.Sprintf("packages ok=%d fail=%d  tests failed=%d", okN, failN, len(fails))}
	out = append(out, capLines(fails, 25)...)
	out = append(out, panics...)
	if len(build) > 0 {
		out = append(out, "build/vet:")
		out = append(out, build...)
	}
	if len(pkgs) > 0 && len(fails) == 0 {
		out = append(out, capLines(pkgs, 10)...)
	}
	return out
}

func parsePytest(r io.Reader) []string {
	var fails, es []string
	summary := ""
	s := scanner(r)
	for s.Scan() {
		line := stripANSI(s.Text())
		if m := pyFailRE.FindStringSubmatch(line); m != nil {
			l := m[1] + " " + m[2]
			if m[3] != "" {
				l += " - " + m[3]
			}
			fails = append(fails, l)
			continue
		}
		if m := pySummRE.FindStringSubmatch(line); m != nil {
			summary = m[1]
			continue
		}
		if m := pyERE.FindStringSubmatch(line); m != nil && len(es) < 15 {
			es = append(es, "E "+m[1])
		}
	}
	out := []string{}
	if summary != "" {
		out = append(out, summary)
	}
	out = append(out, capLines(fails, 25)...)
	if len(fails) > 0 {
		out = append(out, es...)
	}
	return out
}

func parseJest(r io.Reader) []string {
	var summ, fails []string
	seen := map[string]bool{}
	s := scanner(r)
	for s.Scan() {
		line := stripANSI(s.Text())
		if m := jestSummRE.FindStringSubmatch(line); m != nil {
			summ = append(summ, m[1]+": "+m[2])
			continue
		}
		if m := jestFailRE.FindStringSubmatch(line); m != nil {
			t := strings.TrimSpace(m[2])
			if !seen[t] && len(fails) < 25 {
				seen[t] = true
				fails = append(fails, "FAIL "+t)
			}
		}
	}
	return append(summ, fails...)
}

func parseCargo(r io.Reader) []string {
	var res, fails, errs []string
	s := scanner(r)
	for s.Scan() {
		line := stripANSI(s.Text())
		if m := cargoResRE.FindStringSubmatch(line); m != nil {
			res = append(res, "result: "+m[1])
			continue
		}
		if m := cargoFailRE.FindStringSubmatch(line); m != nil {
			fails = append(fails, "FAIL "+m[1])
			continue
		}
		if cargoErrRE.MatchString(line) && len(errs) < 20 {
			errs = append(errs, line)
		}
	}
	out := append(res, capLines(fails, 25)...)
	return append(out, errs...)
}

func parseGeneric(r io.Reader) []string {
	var hits []string
	n := 0
	s := scanner(r)
	for s.Scan() {
		line := stripANSI(s.Text())
		if genericRE.MatchString(line) {
			n++
			if len(hits) < 20 {
				hits = append(hits, strings.TrimSpace(line))
			}
		}
	}
	if n == 0 {
		return nil
	}
	out := []string{fmt.Sprintf("lines matching error|fail|panic|exception|fatal: %d (showing %d)", n, len(hits))}
	return append(out, hits...)
}

func capLines(ls []string, n int) []string {
	if len(ls) <= n {
		return ls
	}
	out := append([]string{}, ls[:n]...)
	return append(out, fmt.Sprintf("… %d more", len(ls)-n))
}
