package bound

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

const diagnosticPattern = `(^|[\W_])(error|fail(ed|ure)?|panic|exception|fatal|traceback|denied|timeout|warn(ing)?|skip(ped)?|degraded|coverage)($|[\W_])|no tests|below.required`

var eventRules = []struct {
	name string
	re   *regexp.Regexp
}{
	{"error", regexp.MustCompile(`(?i)(^|[\W_])(error|fail(ed|ure)?|panic|exception|fatal|traceback|denied|timeout)($|[\W_])`)},
	{"degraded", regexp.MustCompile(`(?i)degraded|below.required|not.ready`)},
	{"warning", regexp.MustCompile(`(?i)(^|[\W_])warn(ing)?($|[\W_])`)},
	{"checks", regexp.MustCompile(`(?i)(^|[\W_])(skip(ped)?|coverage)($|[\W_])|no tests`)},
}
var templateIDs = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f-]{27,}\b`)

var templateTimes = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:?\d{2})?`)
var templateRequestIDs = regexp.MustCompile(`(?i)\b(request[_ -]?id|trace[_ -]?id|span[_ -]?id|pid|tid)\s*[=:]\s*[a-z0-9_-]+`)

func normaliseTemplate(text string) string {
	text = templateIDs.ReplaceAllString(strings.ToLower(text), "<uuid>")
	text = templateTimes.ReplaceAllString(text, "<time>")
	return templateRequestIDs.ReplaceAllString(text, "$1=<id>")
}

type eventGroup struct {
	kind, firstText, lastText string
	count, first, last        int
}

func eventSummaryFile(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return []string{"incomplete=true read_error=" + err.Error()}
	}
	defer f.Close()
	return eventSummary(f)
}

// Counts are exact for the recognised categories. Representatives are bounded
// per category, so early repetitive errors cannot consume every category's slot.
// Normalisation is for grouping only; displayed examples remain original lines.
func eventSummary(r io.Reader) []string {
	perKind := max(1, DefaultLimits().EventMax/len(eventRules))
	groups := make([][]*eventGroup, len(eventRules))
	maps := make([]map[string]*eventGroup, len(eventRules))
	counts := make([]int, len(eventRules))
	omitted := 0
	for i := range maps {
		maps[i] = map[string]*eventGroup{}
	}
	s := scanner(r)
	line := 0
	for s.Scan() {
		line++
		original := stripANSI(s.Text())
		for i, rule := range eventRules {
			if !rule.re.MatchString(original) {
				continue
			}
			counts[i]++
			key := normaliseTemplate(original)
			key = fmt.Sprintf("%x", sha256.Sum256([]byte(key)))
			g := maps[i][key]
			if g == nil {
				if len(groups[i]) >= perKind {
					omitted++
					break
				}
				g = &eventGroup{kind: rule.name, firstText: original, first: line}
				groups[i] = append(groups[i], g)
				maps[i][key] = g
			}
			g.count++
			g.last = line
			g.lastText = original
			break
		}
	}
	total := 0
	parts := []string{}
	for i, c := range counts {
		total += c
		parts = append(parts, fmt.Sprintf("%s=%d", eventRules[i].name, c))
	}
	if total == 0 && s.Err() == nil {
		return nil
	}
	out := []string{fmt.Sprintf("diagnostic events=%d %s unrepresented_events=%d (heuristic categories; not a completeness proof)", total, strings.Join(parts, " "), omitted)}
	if omitted > 0 {
		out = append(out, "truncated=true; additional event templates require targeted source reads")
	}
	for _, gs := range groups {
		for _, g := range gs {
			out = append(out, fmt.Sprintf("%s count=%d first=%d last=%d", g.kind, g.count, g.first, g.last), fmt.Sprintf("%6d| %s", g.first, g.firstText))
			if g.last != g.first {
				out = append(out, fmt.Sprintf("%6d| %s", g.last, g.lastText))
			}
		}
	}
	if s.Err() != nil {
		out = append(out, "incomplete=true read_error="+s.Err().Error())
	}
	return out
}
