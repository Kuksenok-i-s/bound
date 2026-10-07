package bound

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

// Run executes a command with stdout+stderr captured to a spill file and prints
// a bounded envelope: exit code, size, a tool-specific summary (test failures,
// build errors), head/tail excerpts and the exact next command to dig deeper.
func Run(args []string, w io.Writer) int {
	l := DefaultLimits()
	flags, pos, rest := parseFlags(args, "c", "quiet", "stamp")
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
	timeout, err := commandTimeout(flags, 10*time.Minute)
	if err != nil {
		fmt.Fprintln(w, "bound run: invalid timeout:", err)
		return 2
	}
	lines := flagInt(flags, "lines", l.RunLines, 400)
	captured, err := captureCommandStamped(argv, timeout, []string{"NO_COLOR=1", "FORCE_COLOR=0", "TERM=dumb", "CLICOLOR=0", "GIT_PAGER=", "PAGER="}, flags["stamp"] == "true")
	if err != nil {
		fmt.Fprintf(w, "bound run: capture failed: %v full: %s\n", err, captured.Path)
		return 2
	}
	path, exit := captured.Path, captured.Exit
	dur := (time.Duration(captured.DurationMS) * time.Millisecond).Round(100 * time.Millisecond)
	total, size, scanErr := countLines(path)
	if scanErr != nil {
		fmt.Fprintf(w, "bound run: incomplete=true: %v full: %s\n", scanErr, path)
		return 2
	}
	e := newEnvelope(l.RunChars)
	display := ShellQuote(argv)
	if len(display) > 160 {
		display = display[:160] + "…"
	}
	e.linef("[bound run] %s", display)
	status := fmt.Sprintf("exit=%d lines=%d bytes=%s (%s) time=%s", exit, total, Human(size), Tokens(size), dur)
	if captured.TimedOut {
		status += " TIMEOUT"
	}
	if captured.Error != "" {
		status += " error=" + captured.Error
	}
	tsStatus, tsHint := captured.timestampsNote()
	e.line(status + " " + tsStatus)
	e.raw(fmt.Sprintf("full: %s\n", path))
	e.raw(fmt.Sprintf("status: %s.meta.json\n", path))
	if tsHint != "" && total > lines {
		e.line(tsHint)
	}

	// Green run of a recognised test runner: the per-package/per-test "ok" list
	// is the least useful output there is. Keep the summary, keep the spill.
	if exit == 0 && total > 8 {
		if kind, summary := summarize(argv, path); kind != "generic" && len(summary) > 0 {
			e.section("summary (" + kind + ")")
			e.lines(summary)
			e.lines(eventSummaryFile(path))
			e.setNext(ShellQuote([]string{"bound", "read", path, "--grep", diagnosticPattern, "-C", "6"}))
			delivered := e.flush(w)
			ledgerSource("run", size, delivered, argv[0], path)
			return exit
		}
	}

	// Small output is delivered verbatim, but always retain the source and status.
	if total <= lines && size <= int64(l.RunChars)*2/3 {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(w, "bound run: incomplete=true:", err)
			return 2
		}
		e.raw(string(data))
		delivered := e.flush(w)
		ledgerSource("run", size, delivered, argv[0], path)
		return exit
	}

	kind, summary := summarize(argv, path)
	if kind != "generic" {
		summary = append(summary, eventSummaryFile(path)...)
	}
	if len(summary) > 0 {
		e.section("summary (" + kind + ")")
		e.lines(summary)
	}
	h, t := lines/4, lines-lines/4
	if len(summary) > 1 {
		// A recognised parser found the signal; excerpts are only for orientation.
		h, t = lines/6, lines/3
	}
	head, tail, _, readErr := headTail(path, h, t)
	if readErr != nil {
		e.linef("incomplete=true read_error=%v", readErr)
	}
	e.section(fmt.Sprintf("head (%d)", len(head)))
	e.lines(head)
	e.section(fmt.Sprintf("tail (%d)", len(tail)))
	e.lines(tail)
	e.setNext(ShellQuote([]string{"bound", "read", path, "--grep", nextPattern(kind), "-C", "6"}))
	delivered := e.flush(w)
	ledgerSource("run", size, delivered, argv[0], path)
	return exit
}

func nextPattern(kind string) string {
	switch kind {
	case "go test":
		return "--- FAIL|panic:|FAIL\\s|" + diagnosticPattern
	case "pytest":
		return "^FAILED|^ERROR|^E |" + diagnosticPattern
	case "jest":
		return "●|✕|FAIL |" + diagnosticPattern
	case "cargo":
		return "FAILED|panicked|^error|" + diagnosticPattern
	}
	return diagnosticPattern
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
)

func parseGo(r io.Reader) []string {
	var fails, pkgs, build, panics []string
	okN, failN, noTest := 0, 0, 0
	lastFail := -1
	pending := "" // location seen after "=== RUN" but before "--- FAIL" (-v mode)
	loc := func(m []string) string {
		msg := m[2]
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
			case "?":
				noTest++
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
	out := []string{fmt.Sprintf("packages ok=%d fail=%d notest=%d  tests failed=%d", okN, failN, noTest, len(fails))}
	out = append(out, capLines(fails, 25)...)
	out = append(out, panics...)
	if len(build) > 0 {
		out = append(out, "build/vet:")
		out = append(out, build...)
	}
	if len(pkgs) > 0 && len(fails) == 0 {
		out = append(out, capLines(pkgs, 10)...)
	}
	if s.Err() != nil {
		out = append(out, "incomplete=true read_error="+s.Err().Error())
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
	if s.Err() != nil {
		out = append(out, "incomplete=true read_error="+s.Err().Error())
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
	out := append(summ, fails...)
	if s.Err() != nil {
		out = append(out, "incomplete=true read_error="+s.Err().Error())
	}
	return out
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
	out = append(out, errs...)
	if s.Err() != nil {
		out = append(out, "incomplete=true read_error="+s.Err().Error())
	}
	return out
}

func parseGeneric(r io.Reader) []string { return eventSummary(r) }

func capLines(ls []string, n int) []string {
	if len(ls) <= n {
		return ls
	}
	out := append([]string{}, ls[:n]...)
	return append(out, fmt.Sprintf("… %d more", len(ls)-n))
}
