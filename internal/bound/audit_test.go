package bound

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBoundAuditHelper(t *testing.T) {
	if os.Getenv("BOUND_AUDIT_HELPER") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	switch mode {
	case "short":
		io.WriteString(os.Stdout, strings.Repeat("A", 700)+"TAIL_EVIDENCE\n")
		os.Exit(7)
	case "streams":
		io.WriteString(os.Stdout, "OUT\n")
		io.WriteString(os.Stderr, "\x1b[31mERR\x1b[0m\rTAIL")
		os.Exit(17)
	case "exit124":
		io.WriteString(os.Stdout, "NORMAL_EXIT\n")
		os.Exit(124)
	case "timeout":
		io.WriteString(os.Stdout, "PREFIX\n")
		time.Sleep(5 * time.Second)
	case "ticks":
		// three unstamped lines 30ms apart, one with its own timestamp, one partial
		for i := 0; i < 3; i++ {
			io.WriteString(os.Stdout, "tick\n")
			time.Sleep(30 * time.Millisecond)
		}
		io.WriteString(os.Stderr, "2026-01-01T00:00:00Z own-stamp\n")
		io.WriteString(os.Stdout, "partial-no-newline")
	case "pytest":
		io.WriteString(os.Stdout, "WARNING dependency unavailable\nSKIPPED acceptance_login missing prerequisite\n"+strings.Repeat("routine\n", 20)+"================ 10 passed, 90 skipped in 0.1s ================\n")
	}
	os.Exit(0)
}

func helperArgs(mode string) []string {
	return []string{"--", os.Args[0], "-test.run=^TestBoundAuditHelper$", "--", mode}
}

func isolatedAudit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("BOUND_DIR", dir)
	t.Setenv("BOUND_AUDIT_HELPER", "1")
	return dir
}

func readCapture(t *testing.T, dir string) ([]byte, capture) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.log"))
	if err != nil || len(files) != 1 {
		t.Fatalf("raw artifacts: %v %v", files, err)
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	status, err := os.ReadFile(files[0] + ".meta.json")
	if err != nil {
		t.Fatal(err)
	}
	var c capture
	if err = json.Unmarshal(status, &c); err != nil {
		t.Fatal(err)
	}
	return raw, c
}

func TestRunRetainsLongSmallOutputAndStatus(t *testing.T) {
	dir := isolatedAudit(t)
	var out bytes.Buffer
	if code := Run(helperArgs("short"), &out); code != 7 {
		t.Fatalf("exit=%d", code)
	}
	raw, c := readCapture(t, dir)
	want := strings.Repeat("A", 700) + "TAIL_EVIDENCE\n"
	if string(raw) != want || !strings.Contains(out.String(), want) || c.Exit != 7 || c.TimedOut {
		t.Fatalf("loss: metadata=%+v output=%s", c, out.String())
	}
}

func TestExecutionRetainsRawStreamsAndNonzeroExit(t *testing.T) {
	for _, kind := range []string{"run", "log"} {
		t.Run(kind, func(t *testing.T) {
			dir := isolatedAudit(t)
			var out bytes.Buffer
			args := helperArgs("streams")
			code := 0
			if kind == "run" {
				code = Run(args, &out)
			} else {
				code = Log(args, &out)
			}
			raw, c := readCapture(t, dir)
			if code != 17 || c.Exit != 17 || string(raw) != "OUT\n\x1b[31mERR\x1b[0m\rTAIL" || !strings.Contains(out.String(), "exit=17") {
				t.Fatalf("kind=%s exit=%d raw=%q metadata=%+v", kind, code, raw, c)
			}
		})
	}
}

func TestTimeoutReasonIsIndependentOfExit124(t *testing.T) {
	for _, kind := range []string{"run", "log"} {
		for _, mode := range []string{"exit124", "timeout"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				dir := isolatedAudit(t)
				var out bytes.Buffer
				args := helperArgs(mode)
				if mode == "timeout" {
					args = append([]string{"--timeout", "50ms"}, args...)
				}
				code := 0
				if kind == "run" {
					code = Run(args, &out)
				} else {
					code = Log(args, &out)
				}
				_, c := readCapture(t, dir)
				if code != 124 || c.TimedOut != (mode == "timeout") {
					t.Fatalf("code=%d metadata=%+v", code, c)
				}
				if mode == "exit124" && strings.Contains(out.String(), "TIMEOUT") {
					t.Fatal("normal exit mislabeled")
				}
			})
		}
	}
}

func TestReadFindsEventsAfterSeventeenMiBLine(t *testing.T) {
	isolatedAudit(t)
	path := filepath.Join(t.TempDir(), "long.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("X", 17*1024*1024)+"\nAFTER_BIG_CRITICAL\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, selector := range []string{"--grep", "--query"} {
		var out bytes.Buffer
		if code := Read([]string{path, selector, "AFTER_BIG_CRITICAL", "-C", "0"}, &out); code != 0 {
			t.Fatalf("%s: exit=%d", selector, code)
		}
		if !strings.Contains(out.String(), "AFTER_BIG_CRITICAL") || strings.Contains(out.String(), "no matches") || !strings.Contains(out.String(), "scan_complete=true") {
			t.Fatalf("false negative: %s", out.String())
		}
	}
}

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, errors.New("synthetic I/O failure") }
func TestReaderFailureCannotBecomeNoEvents(t *testing.T) {
	out := strings.Join(eventSummary(brokenReader{}), "\n")
	if !strings.Contains(out, "incomplete=true") || !strings.Contains(out, "synthetic I/O failure") {
		t.Fatal(out)
	}
}

func TestGreenRunStillReportsWarningsAndSkippedChecks(t *testing.T) {
	isolatedAudit(t)
	var out bytes.Buffer
	if code := Run(helperArgs("pytest"), &out); code != 0 {
		t.Fatal(code)
	}
	for _, want := range []string{"WARNING dependency unavailable", "SKIPPED acceptance_login", "90 skipped"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("lost %q: %s", want, out.String())
		}
	}
}

func TestEventGroupingCountsAndPreservesOtherCategories(t *testing.T) {
	input := strings.Repeat("ERROR request 123 failed\n", 100) + "WARNING certificate expires\nSKIPPED acceptance\ncoverage: 12% required: 80%\nDEGRADED service\nAUTH_DENIED principal=synthetic\n"
	out := strings.Join(eventSummary(strings.NewReader(input)), "\n")
	for _, want := range []string{"error=101", "warning=1", "checks=2", "degraded=1", "count=100 first=1 last=100", "AUTH_DENIED"} {
		if !strings.Contains(out, want) {
			t.Fatalf("lost %q: %s", want, out)
		}
	}
}

func TestBM25BigramsRankingLimitsAndContextPointers(t *testing.T) {
	isolatedAudit(t)
	path := filepath.Join(t.TempDir(), "query.log")
	os.WriteFile(path, []byte("connection request timeout refused\nconnection refused request timeout\nAUTH_DENIED principal=synthetic\n"), 0600)
	var out bytes.Buffer
	if code := Read([]string{path, "--query", "connection refused", "-k", "1", "-C", "1"}, &out); code != 0 {
		t.Fatal(code)
	}
	for _, want := range []string{"matches=2 returned=1 truncated=true", "line=2 score=", "1:3"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out.String())
		}
	}
	out.Reset()
	Read([]string{path, "--query", "auth denied"}, &out)
	if !strings.Contains(out.String(), "line=3 score=") {
		t.Fatal(out.String())
	}
	out.Reset()
	Read([]string{path, "--query", "absent vocabulary"}, &out)
	if !strings.Contains(out.String(), "matches=0") || !strings.Contains(out.String(), "scan_complete=true") {
		t.Fatal(out.String())
	}
}

func TestByteRangesRecoverExactTailOfLongLine(t *testing.T) {
	isolatedAudit(t)
	path := filepath.Join(t.TempDir(), "bytes.log")
	os.WriteFile(path, []byte(strings.Repeat("A", 700)+"TAIL_EVIDENCE"), 0600)
	var out bytes.Buffer
	if code := Read([]string{path, "--bytes", "701:713"}, &out); code != 0 {
		t.Fatal(code)
	}
	if !strings.HasSuffix(out.String(), "TAIL_EVIDENCE") {
		t.Fatal(out.String())
	}
}

func TestStatsSeparatesOperationsAndSnapshots(t *testing.T) {
	isolatedAudit(t)
	path := filepath.Join(t.TempDir(), "stats.log")
	os.WriteFile(path, []byte(strings.Repeat("x", 80000)), 0600)
	ledgerSource("run", 80000, 5000, "cmd", path)
	ledgerSource("read", 80000, 300, path, path)
	ledgerSource("read", 80000, 300, path, path)
	var out bytes.Buffer
	Stats(nil, &out)
	for _, want := range []string{"234.4K", "identified source snapshots=1 source_bytes=80000", "not task savings", "UTF-8 bytes/4 estimate"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out.String())
		}
	}
}

func TestUnicodeEstimateLabelsActualUnits(t *testing.T) {
	if Tokens(int64(len("яя"))) != "~1tok; UTF-8 bytes/4 estimate" {
		t.Fatal(Tokens(4))
	}
}

func TestEnvelopeReportsTruncationWithinBudget(t *testing.T) {
	e := newEnvelope(500)
	e.line(strings.Repeat("x", 700))
	var out bytes.Buffer
	e.flush(&out)
	if out.Len() > 500 || !strings.Contains(out.String(), "truncated=true") {
		t.Fatalf("len=%d %s", out.Len(), out.String())
	}
}

func TestBudgetOverridesRespectHardCaps(t *testing.T) {
	t.Setenv("BOUND_QUERY_MAX", "99999")
	t.Setenv("BOUND_EVENT_MAX", "99999")
	t.Setenv("BOUND_RUN_CHARS", "99999")
	l := DefaultLimits()
	if l.QueryDefault != 50 || l.EventMax != 200 || l.RunChars != 12000 {
		t.Fatalf("%+v", l)
	}
}

func TestRegexSelectionRetainsCompleteHitCount(t *testing.T) {
	isolatedAudit(t)
	path := filepath.Join(t.TempDir(), "many.log")
	os.WriteFile(path, []byte(strings.Repeat("ERROR synthetic\n", 300)), 0600)
	var out bytes.Buffer
	Read([]string{path, "--grep", "ERROR", "-C", "0"}, &out)
	if !strings.Contains(out.String(), "matches=300 shown_lines=200 scan_complete=true truncated=true") {
		t.Fatal(out.String())
	}
}

func TestCaptureReportsTimestampCoverageAndHint(t *testing.T) {
	dir := isolatedAudit(t)
	var out bytes.Buffer
	if code := Log(append([]string{"--tail", "10"}, helperArgs("ticks")...), &out); code != 0 {
		t.Fatal(code, out.String())
	}
	raw, c := readCapture(t, dir)
	if c.Lines != 5 || c.Timed != 1 || c.Stamped != 0 || c.Stamp {
		t.Fatalf("%+v", c)
	}
	if !strings.Contains(out.String(), "timestamps=1/5") || strings.Contains(out.String(), "hint:") {
		t.Fatal(out.String())
	}
	if string(raw) != "tick\ntick\ntick\n2026-01-01T00:00:00Z own-stamp\npartial-no-newline" {
		t.Fatalf("spill altered without --stamp: %q", raw)
	}

	out.Reset()
	if code := Log(append([]string{"--tail", "10"}, helperArgs("short")...), &out); code != 7 {
		t.Fatal(code, out.String())
	}
	if !strings.Contains(out.String(), "timestamps=0/1") || !strings.Contains(out.String(), "hint: no leading timestamps") {
		t.Fatal(out.String())
	}
}

func TestStampPrefixesReceiveTimeOnlyWhereMissing(t *testing.T) {
	dir := isolatedAudit(t)
	var out bytes.Buffer
	if code := Log(append([]string{"--stamp", "--profile"}, helperArgs("ticks")...), &out); code != 0 {
		t.Fatal(code, out.String())
	}
	raw, c := readCapture(t, dir)
	if c.Lines != 5 || c.Timed != 1 || c.Stamped != 4 || !c.Stamp {
		t.Fatalf("%+v", c)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 5 || lines[3] != "2026-01-01T00:00:00Z own-stamp" {
		t.Fatalf("own timestamp must stay untouched: %q", lines)
	}
	var prev time.Time
	for _, i := range []int{0, 1, 2, 4} {
		stamp, rest, ok := strings.Cut(lines[i], " ")
		at, err := time.Parse(time.RFC3339Nano, stamp)
		if !ok || err != nil || (rest != "tick" && rest != "partial-no-newline") {
			t.Fatalf("line %d not stamped verbatim: %q", i, lines[i])
		}
		if i < 3 && !prev.IsZero() && at.Sub(prev) < 20*time.Millisecond {
			t.Fatalf("receive times not increasing: %v %v", prev, at)
		}
		if i < 3 {
			prev = at
		}
	}
	s := out.String()
	for _, want := range []string{"timestamps=1/5 stamped=true(4)", "about: times are when bound received each line", "events=5 untimed=0"} {
		if !strings.Contains(s, want) {
			t.Fatalf("lost %q: %s", want, s)
		}
	}
	if code := Log([]string{filepath.Join(dir, "x.log"), "--stamp"}, &out); code != 2 {
		t.Fatal("--stamp on a file must fail", code)
	}
}

func TestRunStampAndCoverageInStatus(t *testing.T) {
	isolatedAudit(t)
	var out bytes.Buffer
	if code := Run(append([]string{"--stamp"}, helperArgs("ticks")...), &out); code != 0 {
		t.Fatal(code, out.String())
	}
	if !strings.Contains(out.String(), "timestamps=1/5 stamped=true(4)") {
		t.Fatal(out.String())
	}
}

func TestLogTailHasBoundedContextAndExactCounts(t *testing.T) {
	isolatedAudit(t)
	path := filepath.Join(t.TempDir(), "tail.log")
	os.WriteFile(path, []byte("start\nERROR first\nmiddle\nERROR second\nend\n"), 0600)
	var out bytes.Buffer
	if code := Log([]string{path, "--grep", "ERROR", "-C", "1", "--tail", "3"}, &out); code != 0 {
		t.Fatal(code)
	}
	for _, want := range []string{"shown=3 selected_total=5 scan_complete=true", "matched=2", "middle\nERROR second\nend\n", "truncated=true"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("lost %q: %s", want, out.String())
		}
	}
}

func TestExecutableFollowupSurvivesFullEnvelope(t *testing.T) {
	e := newEnvelope(500)
	e.raw(strings.Repeat("x", 1000))
	e.setNext(ShellQuote([]string{"bound", "read", "file with spaces.log", "1:10"}))
	var out bytes.Buffer
	e.flush(&out)
	if out.Len() > 500 || !strings.Contains(out.String(), "\nnext: bound read 'file with spaces.log' 1:10\n") || !strings.Contains(out.String(), "truncated=true") {
		t.Fatalf("len=%d %s", out.Len(), out.String())
	}
}

func TestStatsDisclosesLegacyAndInvalidEntries(t *testing.T) {
	dir := isolatedAudit(t)
	os.WriteFile(filepath.Join(dir, "ledger.jsonl"), []byte("{\"kind\":\"run\",\"raw\":80000,\"delivered\":5000}\ninvalid json\n"), 0600)
	var out bytes.Buffer
	if code := Stats(nil, &out); code != 2 || !strings.Contains(out.String(), "unattributed_calls=1 invalid_records=1") || !strings.Contains(out.String(), "incomplete=true") {
		t.Fatalf("code=%d %s", code, out.String())
	}
}

func TestTemplateGroupingPreservesCodesAndNormalisesRequestIDs(t *testing.T) {
	input := "ERROR status=500 request_id=123\nERROR status=403 request_id=777\nERROR status=500 request_id=456\n"
	out := strings.Join(eventSummary(strings.NewReader(input)), "\n")
	for _, want := range []string{"count=2 first=1 last=3", "status=403 request_id=777"} {
		if !strings.Contains(out, want) {
			t.Fatalf("lost %q: %s", want, out)
		}
	}
}

func TestOversizedFollowupCannotBreakEnvelopeCap(t *testing.T) {
	e := newEnvelope(500)
	e.line("full: source.log")
	e.setNext(strings.Repeat("x", 1000))
	var out bytes.Buffer
	e.flush(&out)
	if out.Len() > 500 || !strings.Contains(out.String(), "truncated=true") {
		t.Fatalf("len=%d %s", out.Len(), out.String())
	}
}
