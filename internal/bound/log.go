package bound

import (
	"fmt"
	"io"
	"os"
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
// --profile replaces the tail with a bounded arrival-process summary of the
// selected lines (rate per bin, duplicates, inter-arrival, Hawkes fit).
func Log(args []string, w io.Writer) int {
	l := DefaultLimits()
	flags, pos, rest := parseFlags(args, "profile")
	profile := flags["profile"] == "true"
	defaultTail := l.LogTail
	if profile {
		defaultTail = 0
	}
	tail := flagInt(flags, "tail", defaultTail, l.LogHard)
	ctxN := flagInt(flags, "C", 0, 20)
	bin, binErr := parseDurationFlag(flags, "bin", 0)
	gap, gapErr := parseDurationFlag(flags, "gap", 100*time.Millisecond)
	for _, err := range []error{binErr, gapErr} {
		if err != nil {
			fmt.Fprintf(w, "[bound log] %v\n", err)
			return 2
		}
	}
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
	exit := 0
	var captured capture
	if len(rest) > 0 {
		timeout, err := commandTimeout(flags, 5*time.Minute)
		if err != nil {
			fmt.Fprintln(w, "bound log: invalid timeout:", err)
			return 2
		}
		var captureErr error
		captured, captureErr = captureCommand(rest, timeout, []string{"NO_COLOR=1", "TERM=dumb", "PAGER=cat", "SYSTEMD_PAGER=cat"})
		if captureErr != nil {
			fmt.Fprintf(w, "bound log: capture failed: %v full: %s\n", captureErr, captured.Path)
			return 2
		}
		path, label, exit = captured.Path, ShellQuote(rest), captured.Exit

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
	total, size, countErr := countLines(path)
	if countErr != nil {
		fmt.Fprintln(w, "bound log: incomplete=true:", countErr)
		return 2
	}
	var cur time.Time
	matched, selectedTotal, lineNumber, lastSelected, until := 0, 0, 0, 0, 0
	type row struct {
		number int
		text   string
	}
	keep := []string{}
	prior := []row{}
	shortened := false
	selectRow := func(r row) {
		if r.number <= lastSelected {
			return
		}
		selectedTotal++
		lastSelected = r.number
		keep = append(keep, r.text)
		if len(keep) > tail {
			keep = keep[1:]
		}
	}
	var prof logProfile
	s := scanner(f)
	for s.Scan() {
		lineNumber++
		original := stripANSI(s.Text())
		var lt time.Time
		timed := false
		if !since.IsZero() || profile {
			lt, timed = lineTime(original)
		}
		if !since.IsZero() {
			if timed {
				cur = lt
			}
			if cur.Before(since) {
				continue
			}
		}
		preview := original
		if len(preview) > 400 {
			preview = preview[:400] + " …"
			shortened = true
		}
		current := row{lineNumber, preview}
		if rx == nil {
			selectRow(current)
			if profile {
				prof.add(lt, timed)
			}
		} else {
			if rx.MatchString(original) {
				matched++
				if profile {
					prof.add(lt, timed)
				}
				for _, r := range prior {
					selectRow(r)
				}
				until = lineNumber + ctxN
			}
			if lineNumber <= until {
				selectRow(current)
			}
		}
		prior = append(prior, current)
		if len(prior) > ctxN {
			prior = prior[1:]
		}
	}
	if len(label) > 160 {
		label = label[:160] + "…"
	}
	e := newEnvelope(l.RunChars * 2)
	hdr := fmt.Sprintf("[bound log] %s lines=%d bytes=%s shown=%d selected_total=%d scan_complete=%v", label, total, Human(size), len(keep), selectedTotal, s.Err() == nil)
	if rx != nil {
		hdr += fmt.Sprintf(" grep=%q matched=%d", flags["grep"], matched)
	}
	if !since.IsZero() {
		hdr += " since=" + since.Format(time.RFC3339)
	}
	if len(rest) > 0 {
		hdr += fmt.Sprintf(" exit=%d timed_out=%v", exit, captured.TimedOut)
		if captured.Error != "" {
			hdr += " error=" + captured.Error
		}
	}
	e.line(hdr)
	e.raw(fmt.Sprintf("full: %s\n", path))
	if s.Err() != nil {
		e.linef("incomplete=true read_error=%v", s.Err())
		if exit == 0 {
			exit = 2
		}
	}
	if shortened || len(keep) < total {
		e.line("truncated=true; tail/filter is a selected view of the source")
	}
	if len(rest) > 0 {
		e.raw(fmt.Sprintf("status: %s.meta.json\n", path))
	}
	if profile {
		prof.render(e, l, bin, gap)
	}
	e.lines(keep)
	delivered := e.flush(w)
	ledgerSource("log", size, delivered, label, path)
	return exit
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
