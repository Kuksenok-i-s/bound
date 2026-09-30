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
		fmt.Fprintln(os.Stderr, "usage: bound read <file> [A:B] [--outline] [--full] [--grep RE | --query TEXT -k N] [-C N] [--bytes A:B]")
		return 2
	}
	modes := 0
	for _, mode := range []string{"query", "grep", "bytes"} {
		if _, ok := flags[mode]; ok {
			modes++
		}
	}
	if modes > 1 {
		fmt.Fprintln(w, "bound read: --query, --grep and --bytes are mutually exclusive")
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

	e.raw(fmt.Sprintf("full: %s\n", path))
	if query, ok := flags["query"]; ok {
		if _, other := flags["grep"]; other {
			fmt.Fprintln(w, "bound read: --query and --grep are mutually exclusive")
			return 2
		}
		k := flagInt(flags, "k", l.QueryDefault, l.QueryHard)
		e.line(hdr + " query=" + query)
		err := rankFile(path, query, k, flagInt(flags, "C", 3, 30), e)
		delivered := e.flush(w)
		ledgerSource("read", size, delivered, path, path)
		if err != nil {
			return 2
		}
		return 0
	}
	if span, ok := flags["bytes"]; ok {
		a, b, err := parseRange(span, int(size))
		if err != nil {
			fmt.Fprintln(w, "bound read: bad byte range:", err)
			return 2
		}
		e.line(hdr + fmt.Sprintf(" byte_range=%d:%d (1-based inclusive)", a, b))
		f, err := os.Open(path)
		if err != nil {
			fmt.Fprintln(w, err)
			return 1
		}
		defer f.Close()
		_, err = f.Seek(int64(a-1), io.SeekStart)
		if err != nil {
			fmt.Fprintln(w, err)
			return 2
		}
		n := min(b-a+1, l.RunChars*3/2)
		data, err := io.ReadAll(io.LimitReader(f, int64(n)))
		if err != nil {
			fmt.Fprintln(w, "incomplete=true:", err)
			return 2
		}
		if n < b-a+1 {
			e.line("truncated=true; byte range capped to output budget")
		}
		e.raw(string(data))
		delivered := e.flush(w)
		ledgerSource("read", size, delivered, path, path)
		return 0
	}
	if re, ok := flags["grep"]; ok {
		ctx := flagInt(flags, "C", 3, 30)
		rx, err := regexp.Compile("(?i)" + re)
		if err != nil {
			fmt.Fprintf(w, "[bound read] bad regex: %v\n", err)
			return 2
		}
		e.line(hdr + " grep=" + re)
		n, readErr := grepFile(path, rx, ctx, 200, e)
		if readErr != nil {
			e.linef("incomplete=true read_error=%v", readErr)
		}
		if n == 0 && readErr == nil {
			e.line("no matches")
		}
		delivered := e.flush(w)
		ledgerSource("read", size, delivered, path, path)
		if readErr != nil {
			return 2
		}
		return 0
	}

	var readErr error
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
		readErr = printRange(path, a, b, e)
		if b-a+1 < total {
			e.line("truncated=true; explicit range covers only part of the source")
		}
	case flags["outline"] == "true":
		e.line(hdr)
		e.lines(outline(path, 200))
	case total <= l.ReadSoft || flags["full"] == "true":
		if total > l.ReadHard && flags["full"] == "true" {
			hdr += " FULL READ FORCED"
		}
		e.line(hdr)
		readErr = printRange(path, 1, total, e)
	default:
		e.line(hdr + " > soft limit; showing outline")
		e.lines(outline(path, 200))
		e.setNext(ShellQuote([]string{"bound", "read", path, fmt.Sprintf("1:%d", min(total, l.ReadSoft))}))
	}
	if readErr != nil {
		e.linef("incomplete=true read_error=%v", readErr)
	}
	delivered := e.flush(w)
	ledgerSource("read", size, delivered, path, path)
	if readErr != nil {
		return 2
	}
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

func printRange(path string, a, b int, e *envelope) error {
	f, err := os.Open(path)
	if err != nil {
		return err
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
	return s.Err()
}

// Search scans the complete source while retaining only bounded context. A
// bounded view never changes the total hit count or the scan completion flag.
func grepFile(path string, rx *regexp.Regexp, ctx, maxLines int, e *envelope) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	type row struct {
		number int
		text   string
	}
	selected := []row{}
	ring := []row{}
	hits, n, until, last := 0, 0, 0, 0
	trimmed := false
	add := func(r row) {
		if r.number <= last {
			return
		}
		if len(selected) >= maxLines {
			trimmed = true
			return
		}
		selected = append(selected, r)
		last = r.number
	}
	s := scanner(f)
	for s.Scan() {
		n++
		original := s.Text()
		text := stripANSI(original)
		if len(text) > 400 {
			text = text[:400] + " …"
			trimmed = true
		}
		current := row{n, text}
		if rx.MatchString(original) {
			hits++
			for _, r := range ring {
				add(r)
			}
			until = n + ctx
		}
		if n <= until {
			add(current)
		}
		ring = append(ring, current)
		if len(ring) > ctx {
			ring = ring[1:]
		}
	}
	e.linef("matches=%d shown_lines=%d scan_complete=%v truncated=%v", hits, len(selected), s.Err() == nil, trimmed)
	if trimmed {
		e.truncated = true
	}
	previous := 0
	for _, r := range selected {
		if previous > 0 && r.number > previous+1 {
			e.line("   ---")
		}
		e.linef("%6d| %s", r.number, r.text)
		previous = r.number
	}
	return hits, s.Err()
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
	// pages: structure only (headings, landmarks, forms, templates, anything with an id)
	".html": regexp.MustCompile(`(?i)^\s*<(h[1-6]|header|nav|main|section|article|aside|footer|form|table|dialog|template|script|style|link)\b|\sid="[^"]+"`),
	".pug":  regexp.MustCompile(`^\s*(h[1-6]|header|nav|main|section|article|aside|footer|form|table|dialog|template|mixin|block|extends|include|script|style)\b|^\s*\w*#[\w-]+`),
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
	outlineRules[".htm"] = outlineRules[".html"]
	outlineRules[".vue"] = outlineRules[".html"]
	outlineRules[".svelte"] = outlineRules[".html"]
	outlineRules[".jade"] = outlineRules[".pug"]
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
	if s.Err() != nil {
		out = append(out, "incomplete=true read_error="+s.Err().Error())
	} else if len(out) == 0 {
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
