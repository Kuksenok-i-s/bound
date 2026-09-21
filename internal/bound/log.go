package bound

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var tsLayouts = []struct {
	re     *regexp.Regexp
	layout string
	year   bool // layout lacks year (syslog)
}{
	{regexp.MustCompile(`^\[?(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?(Z|[+-]\d\d:?\d\d)?)`), time.RFC3339Nano, false},
	{regexp.MustCompile(`^\[?(\d{4}-\d\d-\d\d \d\d:\d\d:\d\d)`), "2006-01-02 15:04:05", false},
	{regexp.MustCompile(`^\[?(\d{4}/\d\d/\d\d \d\d:\d\d:\d\d)`), "2006/01/02 15:04:05", false},
	{regexp.MustCompile(`^([A-Z][a-z]{2} +\d{1,2} \d\d:\d\d:\d\d)`), "Jan _2 15:04:05", true},
}

// Log prints the tail of a log file or command, optionally filtered by regex
// and start time. Unbounded log dumps are impossible by construction.
func Log(args []string, w io.Writer) int {
	l := DefaultLimits()
	flags, pos, rest := parseFlags(args)
	tail := flagInt(flags, "tail", l.LogTail, l.LogHard)
	ctxN := flagInt(flags, "C", 0, 20)
	var rx *regexp.Regexp
	if g, ok := flags["grep"]; ok {
		var err error
		if rx, err = regexp.Compile("(?i)" + g); err != nil {
			fmt.Fprintf(w, "[bound log] bad regex: %v\n", err)
			return 2
		}
	}
	var since time.Time
	if s, ok := flags["since"]; ok {
		t, err := parseSince(s)
		if err != nil {
			fmt.Fprintf(w, "[bound log] bad --since %q (use RFC3339, 'YYYY-MM-DD HH:MM', or 30m/2h)\n", s)
			return 2
		}
		since = t
	}

	var path, label string
	if len(rest) > 0 {
		spill, err := NewSpill(rest[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, "bound log:", err)
			return 2
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, rest[0], rest[1:]...)
		cmd.Stdout, cmd.Stderr = spill, spill
		cmd.Env = append(os.Environ(), "NO_COLOR=1", "TERM=dumb", "PAGER=cat", "SYSTEMD_PAGER=cat")
		_ = cmd.Run()
		_ = spill.Close()
		path, label = spill.Name(), ShellQuote(rest)
	} else if len(pos) > 0 {
		path, label = pos[0], pos[0]
	} else {
		fmt.Fprintln(os.Stderr, "usage: bound log <file> | -- <cmd...>  [--tail N] [--grep RE] [--since TS] [-C N]")
		return 2
	}

	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(w, "[bound log] %v\n", err)
		return 1
	}
	defer f.Close()
	total, size, _ := countLines(path)
	var lines []string
	var cur time.Time
	matched := 0
	s := scanner(f)
	for s.Scan() {
		ln := stripANSI(s.Text())
		if !since.IsZero() {
			if t, ok := lineTime(ln); ok {
				cur = t
			}
			if cur.Before(since) {
				continue
			}
		}
		lines = append(lines, ln)
	}
	var keep []string
	if rx != nil {
		show := map[int]bool{}
		for i, ln := range lines {
			if rx.MatchString(ln) {
				matched++
				for j := i - ctxN; j <= i+ctxN; j++ {
					if j >= 0 && j < len(lines) {
						show[j] = true
					}
				}
			}
		}
		for i, ln := range lines {
			if show[i] {
				keep = append(keep, ln)
			}
		}
	} else {
		keep = lines
	}
	if len(keep) > tail {
		keep = keep[len(keep)-tail:]
	}
	e := newEnvelope(l.RunChars * 2)
	hdr := fmt.Sprintf("[bound log] %s lines=%d bytes=%s shown=%d", label, total, Human(size), len(keep))
	if rx != nil {
		hdr += fmt.Sprintf(" grep=%q matched=%d", flags["grep"], matched)
	}
	if !since.IsZero() {
		hdr += " since=" + since.Format(time.RFC3339)
	}
	e.line(hdr)
	if len(rest) > 0 {
		e.linef("full: %s", path)
	}
	e.lines(keep)
	delivered := e.flush(w)
	ledger("log", size, delivered, label)
	return 0
}

func parseSince(s string) (time.Time, error) {
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-d), nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02", "15:04"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			if layout == "15:04" {
				now := time.Now()
				t = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, time.Local)
			}
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable")
}

func lineTime(ln string) (time.Time, bool) {
	for _, tl := range tsLayouts {
		m := tl.re.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		t, err := time.ParseInLocation(tl.layout, m[1], time.Local)
		if err != nil {
			// RFC3339 without zone falls back to local.
			if t2, err2 := time.ParseInLocation("2006-01-02T15:04:05", strings.TrimPrefix(m[1], "["), time.Local); err2 == nil {
				return t2, true
			}
			continue
		}
		if tl.year {
			t = t.AddDate(time.Now().Year(), 0, 0)
		}
		return t, true
	}
	return time.Time{}, false
}
