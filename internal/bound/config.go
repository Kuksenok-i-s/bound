package bound

import (
	"bufio"
	"bytes"
	"crypto/sha256"
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
	RunLines     int // lines shown for a long command (head+tail)
	RunChars     int // hard cap on envelope size in bytes
	GrepDefault  int
	GrepHard     int
	ReadSoft     int // files up to this many lines are shown in full
	ReadHard     int // above this a range is required
	DiffSoft     int
	DiffHard     int
	LogTail      int
	LogHard      int
	TreeDepth    int
	TreeMax      int
	QueryDefault int
	QueryHard    int
	EventMax     int
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
		QueryDefault: 10, QueryHard: 50,
		EventMax: 40,
	}
	envInt("BOUND_RUN_LINES", &l.RunLines)
	envInt("BOUND_RUN_CHARS", &l.RunChars)
	l.RunChars = min(12000, max(1024, l.RunChars))
	envInt("BOUND_GREP_MAX", &l.GrepDefault)
	envInt("BOUND_READ_SOFT", &l.ReadSoft)
	envInt("BOUND_READ_HARD", &l.ReadHard)
	envInt("BOUND_DIFF_SOFT", &l.DiffSoft)
	envInt("BOUND_LOG_TAIL", &l.LogTail)
	envInt("BOUND_TREE_MAX", &l.TreeMax)
	envInt("BOUND_QUERY_MAX", &l.QueryDefault)
	if l.QueryDefault > l.QueryHard {
		l.QueryDefault = l.QueryHard
	}
	envInt("BOUND_EVENT_MAX", &l.EventMax)
	if l.EventMax > 200 {
		l.EventMax = 200
	}
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
	Source    string `json:"source,omitempty"`
	Snapshot  string `json:"snapshot,omitempty"`
}

func ledger(kind string, raw, delivered int64, note string) {
	ledgerSource(kind, raw, delivered, note, "")
}

func ledgerSource(kind string, raw, delivered int64, note, source string) {
	f, err := os.OpenFile(filepath.Join(SpillDir(), "ledger.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if len(note) > 120 {
		note = note[:120]
	}
	entry := ledgerEntry{Time: time.Now().Format(time.RFC3339), Kind: kind, Raw: raw, Delivered: delivered, Note: note}
	if source != "" {
		if absolute, err := filepath.Abs(source); err == nil {
			entry.Source = absolute
			if info, err := os.Stat(source); err == nil {
				entry.Snapshot = fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", absolute, info.Size(), info.ModTime().UnixNano()))))
			}
		}
	}
	_ = json.NewEncoder(f).Encode(entry)
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

// Tokens is an estimate in UTF-8 bytes/4, never provider token telemetry.
func Tokens(bytes int64) string { return fmt.Sprintf("~%dtok; UTF-8 bytes/4 estimate", bytes/4) }

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
type lineScanner struct {
	reader *bufio.Reader
	text   string
	err    error
	done   bool
}

// Memory is proportional to the longest line, not the file size. Unlike
// bufio.Scanner, a long JSON/log line does not silently terminate the scan.
func scanner(r io.Reader) *lineScanner {
	return &lineScanner{reader: bufio.NewReader(r)}
}
func (s *lineScanner) Scan() bool {
	if s.done {
		return false
	}
	line, err := s.reader.ReadString('\n')
	if err != nil {
		s.done = true
		if err != io.EOF {
			s.err = err
		}
	}
	if len(line) == 0 {
		return false
	}
	s.text = strings.TrimSuffix(line, "\n")
	return true
}
func (s *lineScanner) Text() string  { return s.text }
func (s *lineScanner) Bytes() []byte { return []byte(s.text) }
func (s *lineScanner) Err() error    { return s.err }

// headTail returns the first h and last t lines of a file plus total count.
func headTail(path string, h, t int) (head, tail []string, total int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, 0, err
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
	return head, ring, total, s.Err()
}

// ShellQuote renders argv as a POSIX shell command line.
func ShellQuote(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = quoteWord(a)
	}
	return strings.Join(parts, " ")
}

// envelope accumulates bounded output. Sections that would push the total over
// the char cap are trimmed, never dropped silently.
type envelope struct {
	b         strings.Builder
	cap       int
	truncated bool
	next      string
}

const truncationNotice = "[bound] truncated=true; omitted text remains in the source artifact\n"

func newEnvelope(capChars int) *envelope {
	return &envelope{cap: max(0, capChars-len(truncationNotice)-1)}
}

func (e *envelope) linef(format string, a ...any) {
	e.line(fmt.Sprintf(format, a...))
}

func (e *envelope) line(s string) {
	if e.b.Len() >= e.cap {
		e.truncated = true
		return
	}
	if len(s) > 400 {
		e.truncated = true
		s = s[:400] + " …"
	}
	if e.b.Len()+len(s)+1 > e.cap {
		e.truncated = true
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

// raw keeps small output intact, including long lines and terminal bytes.
// On a budget overflow the complete source is retained and omission is explicit.
func (e *envelope) raw(s string) {
	room := e.cap - e.b.Len()
	if len(s) > room {
		e.truncated = true
		s = s[:max(0, room)]
	}
	e.b.WriteString(s)
}

// Reserve an executable follow-up before flush, including when excerpts fill
// the budget. Metadata and source references precede any optional body text.
func (e *envelope) setNext(command string) {
	candidate := "next: " + command + "\n"
	if len(candidate) > e.cap {
		e.truncated = true
		e.line("follow-up exceeds output budget; use the source reference above")
		return
	}
	e.next = candidate
	e.cap = max(0, e.cap-len(e.next))
	if e.b.Len() > e.cap {
		s := e.b.String()[:max(0, e.cap-1)]
		e.b.Reset()
		e.b.WriteString(s)
		if e.cap > 0 {
			e.b.WriteByte('\n')
		}
		e.truncated = true
	}
}

func (e *envelope) flush(w io.Writer) int64 {
	s := e.String() + e.next
	if e.truncated {
		if len(s) > 0 && !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		s += truncationNotice
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
