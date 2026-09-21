package bound

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Limits are the soft/hard budgets applied to every envelope. Soft limits are
// defaults the agent gets without asking; hard limits cannot be exceeded from
// the command line so a single tool result stays small even on a bad call.
type Limits struct {
	RunLines    int // lines shown for a long command (head+tail)
	RunChars    int // hard cap on envelope size in bytes
	GrepDefault int
	GrepHard    int
	ReadSoft    int // files up to this many lines are shown in full
	ReadHard    int // above this a range is required
	DiffSoft    int
	DiffHard    int
	LogTail     int
	LogHard     int
	TreeDepth   int
	TreeMax     int
}

// DefaultLimits returns the built-in budgets, overridable via BOUND_* env vars.
func DefaultLimits() Limits {
	l := Limits{
		RunLines: 60, RunChars: 12000,
		GrepDefault: 100, GrepHard: 500,
		ReadSoft: 500, ReadHard: 2000,
		DiffSoft: 800, DiffHard: 5000,
		LogTail: 200, LogHard: 1000,
		TreeDepth: 3, TreeMax: 500,
	}
	envInt("BOUND_RUN_LINES", &l.RunLines)
	envInt("BOUND_RUN_CHARS", &l.RunChars)
	envInt("BOUND_GREP_MAX", &l.GrepDefault)
	envInt("BOUND_READ_SOFT", &l.ReadSoft)
	envInt("BOUND_READ_HARD", &l.ReadHard)
	envInt("BOUND_DIFF_SOFT", &l.DiffSoft)
	envInt("BOUND_LOG_TAIL", &l.LogTail)
	envInt("BOUND_TREE_MAX", &l.TreeMax)
	return l
}

func envInt(key string, dst *int) {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			*dst = n
		}
	}
}

// SpillDir is where full artifacts are written. It always exists on return.
func SpillDir() string {
	dir := os.Getenv("BOUND_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "bound")
	}
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// NewSpill creates a new artifact file with a timestamped, unique name.
func NewSpill(prefix string) (*os.File, error) {
	name := fmt.Sprintf("%s-%s-", time.Now().Format("20060102-150405"), sanitize(prefix))
	return os.CreateTemp(SpillDir(), name+"*.log")
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
		if b.Len() >= 24 {
			break
		}
	}
	if b.Len() == 0 {
		return "cmd"
	}
	return b.String()
}

// ledgerEntry records how many bytes a command produced versus how many the
// agent actually received, so `bound stats` can report the real ratio.
type ledgerEntry struct {
	Time      string `json:"t"`
	Kind      string `json:"kind"`
	Raw       int64  `json:"raw"`
	Delivered int64  `json:"delivered"`
	Note      string `json:"note,omitempty"`
}

func ledger(kind string, raw, delivered int64, note string) {
	f, err := os.OpenFile(filepath.Join(SpillDir(), "ledger.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if len(note) > 120 {
		note = note[:120]
	}
	_ = json.NewEncoder(f).Encode(ledgerEntry{time.Now().Format(time.RFC3339), kind, raw, delivered, note})
}

// Human formats a byte count compactly.
func Human(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fK", float64(n)/float64(1<<10))
	}
	return fmt.Sprintf("%dB", n)
}

// Tokens is a rough chars/4 estimate, labelled as such wherever printed.
func Tokens(bytes int64) string { return fmt.Sprintf("~%dtok", bytes/4) }

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\r`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// countLines counts newline-terminated lines in a file without loading it.
func countLines(path string) (int, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	st, _ := f.Stat()
	var size int64
	if st != nil {
		size = st.Size()
	}
	buf := make([]byte, 64*1024)
	n := 0
	last := byte('\n')
	for {
		c, err := f.Read(buf)
		if c > 0 {
			n += bytes.Count(buf[:c], []byte{'\n'})
			last = buf[c-1]
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return n, size, err
		}
	}
	if last != '\n' && size > 0 {
		n++
	}
	return n, size, nil
}

// scanner returns a line scanner that tolerates very long lines.
func scanner(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	return s
}

// headTail returns the first h and last t lines of a file plus total count.
func headTail(path string, h, t int) (head, tail []string, total int) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, 0
	}
	defer f.Close()
	ring := make([]string, 0, t)
	s := scanner(f)
	for s.Scan() {
		line := stripANSI(s.Text())
		if total < h {
			head = append(head, line)
		} else if t > 0 {
			if len(ring) == t {
				ring = ring[1:]
			}
			ring = append(ring, line)
		}
		total++
	}
	return head, ring, total
}

// ShellQuote renders argv as a POSIX shell command line.
func ShellQuote(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = quoteWord(a)
	}
	return strings.Join(parts, " ")
}

func quoteWord(a string) string {
	if a == "" {
		return "''"
	}
	if !strings.ContainsAny(a, " \t\n'\"\\$`!*?[]{}()<>|&;#~") {
		return a
	}
	return "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
}

// SimpleArgv splits a command line into words. It returns ok=false when the
// line contains shell metacharacters (pipes, redirects, substitutions, globs,
// compound commands) so callers leave such commands untouched.
func SimpleArgv(cmd string) (argv []string, ok bool) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil, false
	}
	var cur strings.Builder
	inWord := false
	quote := byte(0)
	flush := func() {
		if inWord {
			argv = append(argv, cur.String())
			cur.Reset()
			inWord = false
		}
	}
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else if quote == '"' && c == '\\' && i+1 < len(cmd) {
				i++
				cur.WriteByte(cmd[i])
			} else if quote == '"' && (c == '$' || c == '`') {
				return nil, false
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote = c
			inWord = true
		case c == '\\' && i+1 < len(cmd):
			i++
			cur.WriteByte(cmd[i])
			inWord = true
		case c == ' ' || c == '\t':
			flush()
		case strings.IndexByte("|&;<>`$()\n*?[]{}~#", c) >= 0:
			return nil, false
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	flush()
	return argv, len(argv) > 0
}

// envelope accumulates bounded output. Sections that would push the total over
// the char cap are trimmed, never dropped silently.
type envelope struct {
	b   strings.Builder
	cap int
}

func newEnvelope(capChars int) *envelope { return &envelope{cap: capChars} }

func (e *envelope) linef(format string, a ...any) {
	e.line(fmt.Sprintf(format, a...))
}

func (e *envelope) line(s string) {
	if e.b.Len() >= e.cap {
		return
	}
	if len(s) > 400 {
		s = s[:400] + " …"
	}
	if e.b.Len()+len(s)+1 > e.cap {
		room := e.cap - e.b.Len() - 1
		if room > 0 {
			s = s[:room]
		} else {
			return
		}
	}
	e.b.WriteString(s)
	e.b.WriteByte('\n')
}

func (e *envelope) lines(ls []string) {
	for _, l := range ls {
		e.line(l)
	}
}

func (e *envelope) section(name string) { e.line("--- " + name) }

func (e *envelope) String() string { return e.b.String() }

func (e *envelope) flush(w io.Writer) int64 {
	s := e.String()
	if e.b.Len() >= e.cap {
		s += "[bound] envelope cap reached; use the full artifact path above\n"
	}
	_, _ = io.WriteString(w, s)
	return int64(len(s))
}

// parseFlags is a tiny flag splitter: -k v / --k v / --k=v pairs plus positional
// args. Boolean flags are declared via bools. "--" ends flag parsing.
func parseFlags(args []string, bools ...string) (flags map[string]string, pos []string, rest []string) {
	flags = map[string]string{}
	isBool := map[string]bool{}
	for _, b := range bools {
		isBool[b] = true
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = args[i+1:]
			return
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			pos = append(pos, a)
			continue
		}
		key := strings.TrimLeft(a, "-")
		if k, v, found := strings.Cut(key, "="); found {
			flags[k] = v
			continue
		}
		if isBool[key] || i+1 >= len(args) {
			flags[key] = "true"
			continue
		}
		flags[key] = args[i+1]
		i++
	}
	return
}

func flagInt(flags map[string]string, key string, def, hard int) int {
	if v, ok := flags[key]; ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if hard > 0 && n > hard {
				return hard
			}
			return n
		}
	}
	return def
}
