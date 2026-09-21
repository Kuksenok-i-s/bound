package bound

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Read prints a file within budget: small files verbatim, medium files as a
// symbol outline (ask for a range), large files only by range or grep.
func Read(args []string, w io.Writer) int {
	l := DefaultLimits()
	flags, pos, _ := parseFlags(args, "outline", "full", "n")
	if len(pos) == 0 {
		fmt.Fprintln(os.Stderr, "usage: bound read <file> [A:B] [--outline] [--full] [--grep RE [-C N]]")
		return 2
	}
	path := pos[0]
	total, size, err := countLines(path)
	if err != nil {
		fmt.Fprintf(w, "[bound read] %s: %v\n", path, err)
		return 1
	}
	e := newEnvelope(l.RunChars * 3) // reads are the one place a bigger budget is legitimate
	hdr := fmt.Sprintf("[bound read] %s lines=%d bytes=%s (%s)", path, total, Human(size), Tokens(size))

	if re, ok := flags["grep"]; ok {
		ctx := flagInt(flags, "C", 3, 30)
		rx, err := regexp.Compile("(?i)" + re)
		if err != nil {
			fmt.Fprintf(w, "[bound read] bad regex: %v\n", err)
			return 2
		}
		e.line(hdr + " grep=" + re)
		n := grepFile(path, rx, ctx, 200, e)
		if n == 0 {
			e.line("no matches")
		}
		delivered := e.flush(w)
		ledger("read", size, delivered, path)
		return 0
	}

	var a, b int
	if len(pos) > 1 {
		a, b, err = parseRange(pos[1], total)
		if err != nil {
			fmt.Fprintf(w, "[bound read] bad range %q: %v\n", pos[1], err)
			return 2
		}
	}
	switch {
	case a > 0:
		if b-a+1 > l.ReadHard {
			b = a + l.ReadHard - 1
			hdr += fmt.Sprintf(" (range capped to %d lines)", l.ReadHard)
		}
		e.line(hdr + fmt.Sprintf(" range=%d:%d", a, b))
		printRange(path, a, b, e)
	case flags["outline"] == "true":
		e.line(hdr)
		e.lines(outline(path, 200))
	case total <= l.ReadSoft || flags["full"] == "true":
		if total > l.ReadHard && flags["full"] == "true" {
			hdr += " FULL READ FORCED"
		}
		e.line(hdr)
		printRange(path, 1, total, e)
	default:
		e.line(hdr + " > soft limit; showing outline")
		e.lines(outline(path, 200))
		e.linef("next: bound read %s A:B  (or --grep RE, or --full to override)", path)
	}
	delivered := e.flush(w)
	ledger("read", size, delivered, path)
	return 0
}

func parseRange(s string, total int) (int, int, error) {
	s = strings.ReplaceAll(s, "-", ":")
	a, bs, ok := strings.Cut(s, ":")
	if !ok {
		n, err := strconv.Atoi(a)
		if err != nil {
			return 0, 0, err
		}
		return n, n, nil
	}
	start, err := strconv.Atoi(a)
	if err != nil {
		return 0, 0, err
	}
	end := total
	if bs != "" && bs != "$" {
		end, err = strconv.Atoi(bs)
		if err != nil {
			return 0, 0, err
		}
	}
	if start < 1 {
		start = 1
	}
	if end > total {
		end = total
	}
	if end < start {
		return 0, 0, fmt.Errorf("end before start")
	}
	return start, end, nil
}

func printRange(path string, a, b int, e *envelope) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	s := scanner(f)
	n := 0
	for s.Scan() {
		n++
		if n < a {
			continue
		}
		if n > b {
			break
		}
		e.linef("%6d| %s", n, s.Text())
	}
}

func grepFile(path string, rx *regexp.Regexp, ctx, maxLines int, e *envelope) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	var lines []string
	s := scanner(f)
	for s.Scan() {
		lines = append(lines, s.Text())
	}
	show := map[int]bool{}
	hits := 0
	for i, ln := range lines {
		if rx.MatchString(ln) {
			hits++
			for j := i - ctx; j <= i+ctx; j++ {
				if j >= 0 && j < len(lines) {
					show[j] = true
				}
			}
		}
	}
	printed, last := 0, -2
	for i := range lines {
		if !show[i] {
			continue
		}
		if printed >= maxLines {
			e.linef("… %d matching lines total; cap %d reached", hits, maxLines)
			break
		}
		if i != last+1 && last >= 0 {
			e.line("   ---")
		}
		e.linef("%6d| %s", i+1, stripANSI(lines[i]))
		last = i
		printed++
	}
	return hits
}

// outlineRules map file extensions to declaration regexes. ctags is used first
// when installed; these are the dependency-free fallback.
var outlineRules = map[string]*regexp.Regexp{
	".go":    regexp.MustCompile(`^(func|type|var|const)\b.*`),
	".py":    regexp.MustCompile(`^\s*(def|class|async def)\s+\w+.*`),
	".rb":    regexp.MustCompile(`^\s*(def|class|module)\s+\S+.*`),
	".rs":    regexp.MustCompile(`^\s*(pub(\(\w+\))?\s+)?(fn|struct|enum|impl|trait|mod|type|const|static)\b.*`),
	".js":    regexp.MustCompile(`^\s*(export\s+)?(default\s+)?(async\s+)?(function|class|const|let|var|interface|type|enum)\s+\w+.*`),
	".ts":    regexp.MustCompile(`^\s*(export\s+)?(default\s+)?(async\s+)?(function|class|const|let|var|interface|type|enum|abstract class)\s+\w+.*`),
	".java":  regexp.MustCompile(`^\s*(public|private|protected|static|final|abstract|\s)*\s*(class|interface|enum|record)\s+\w+.*|^\s*(public|private|protected)[^=;]*\(.*\)\s*(throws [\w, ]+)?\s*\{?\s*$`),
	".kt":    regexp.MustCompile(`^\s*(public|private|internal|open|data|sealed|abstract|override|suspend|\s)*\s*(fun|class|object|interface|val|var)\s+\S+.*`),
	".cs":    regexp.MustCompile(`^\s*(public|private|protected|internal|static|async|override|virtual|\s)*\s*(class|interface|enum|record|struct)\s+\w+.*|^\s*(public|private|protected|internal)[^=;]*\(.*\)\s*\{?\s*$`),
	".c":     regexp.MustCompile(`^[A-Za-z_][\w\s\*]*\s\**\w+\s*\([^;]*\)\s*\{?\s*$|^(struct|enum|union|typedef)\b.*`),
	".h":     regexp.MustCompile(`^[A-Za-z_][\w\s\*]*\s\**\w+\s*\([^;]*\);?\s*$|^(struct|enum|union|typedef|#define)\b.*`),
	".cpp":   regexp.MustCompile(`^[A-Za-z_][\w\s\*:<>,]*\s\**[\w:]+\s*\([^;]*\)\s*(const)?\s*\{?\s*$|^(class|struct|enum|namespace|template)\b.*`),
	".sh":    regexp.MustCompile(`^\s*(function\s+)?\w+\s*\(\)\s*\{?|^[A-Z_]+=.*`),
	".md":    regexp.MustCompile(`^#{1,4}\s+.*`),
	".yaml":  regexp.MustCompile(`^[A-Za-z_][^:]*:`),
	".yml":   regexp.MustCompile(`^[A-Za-z_][^:]*:`),
	".toml":  regexp.MustCompile(`^\[.*\]`),
	".json":  regexp.MustCompile(`^\s{0,2}"[^"]+":`),
	".sql":   regexp.MustCompile(`(?i)^\s*(create|alter|drop)\s+.*`),
	".tf":    regexp.MustCompile(`^(resource|module|variable|output|data|provider|locals)\b.*`),
	".proto": regexp.MustCompile(`^\s*(message|service|enum|rpc)\s+\w+.*`),
}

func init() {
	outlineRules[".tsx"] = outlineRules[".ts"]
	outlineRules[".jsx"] = outlineRules[".js"]
	outlineRules[".mjs"] = outlineRules[".js"]
	outlineRules[".cc"] = outlineRules[".cpp"]
	outlineRules[".hpp"] = outlineRules[".cpp"]
	outlineRules[".bash"] = outlineRules[".sh"]
	outlineRules[".zsh"] = outlineRules[".sh"]
	outlineRules[".scala"] = outlineRules[".kt"]
}

// outline lists declarations with line numbers, capped to max entries.
func outline(path string, max int) []string {
	if out := ctagsOutline(path, max); len(out) > 0 {
		return out
	}
	ext := strings.ToLower(filepath.Ext(path))
	rx, ok := outlineRules[ext]
	if !ok {
		rx = regexp.MustCompile(`^[A-Za-z_][\w.]*\s*[=:(]|^\S.*\{\s*$`)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	n, hits := 0, 0
	s := scanner(f)
	for s.Scan() {
		n++
		t := s.Text()
		if !rx.MatchString(t) {
			continue
		}
		hits++
		if hits > max {
			continue
		}
		t = strings.TrimRight(strings.TrimSpace(t), "{ ")
		if len(t) > 110 {
			t = t[:110] + "…"
		}
		out = append(out, fmt.Sprintf("%6d: %s", n, t))
	}
	if hits > max {
		out = append(out, fmt.Sprintf("… %d more declarations", hits-max))
	}
	if len(out) == 0 {
		out = []string{"(no declarations recognised; use A:B ranges or --grep)"}
	}
	return out
}

var ctagsLineRE = regexp.MustCompile(`^(\S+)\s+(\S+)\s+(\d+)\s+\S+\s+(.*)$`)

func ctagsOutline(path string, max int) []string {
	ct, err := exec.LookPath("ctags")
	if err != nil {
		return nil
	}
	out, err := exec.Command(ct, "-x", "--sort=no", "--", path).Output()
	if err != nil || len(out) == 0 {
		return nil
	}
	var res []string
	n := 0
	for _, ln := range strings.Split(string(out), "\n") {
		m := ctagsLineRE.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		n++
		if n > max {
			continue
		}
		src := strings.TrimSpace(m[4])
		if len(src) > 90 {
			src = src[:90] + "…"
		}
		res = append(res, fmt.Sprintf("%6s: %-9s %s", m[3], m[2], src))
	}
	if n > max {
		res = append(res, fmt.Sprintf("… %d more symbols", n-max))
	}
	return res
}
