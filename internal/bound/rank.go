package bound

import (
	"container/heap"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"unicode"
)

// Words and adjacent word bigrams share an explicit feature vocabulary. Query
// and source use identical tokenisation; identifiers are split at underscores.
// Code/number tokens remain searchable. No stemming, stop-word removal or LLM.
func features(text string) ([]string, int) {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	out := make([]string, 0, len(words)*2)
	out = append(out, words...)
	for i := 1; i < len(words); i++ {
		out = append(out, words[i-1]+" "+words[i])
	}
	return out, len(words)
}

type rankedLine struct {
	number int
	score  float64
	text   string
}
type rankHeap []rankedLine

func (h rankHeap) Len() int { return len(h) }
func (h rankHeap) Less(i, j int) bool {
	if h[i].score == h[j].score {
		return h[i].number > h[j].number
	}
	return h[i].score < h[j].score
}
func (h rankHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *rankHeap) Push(x any)   { *h = append(*h, x.(rankedLine)) }
func (h *rankHeap) Pop() any     { old := *h; n := len(old); x := old[n-1]; *h = old[:n-1]; return x }

// Two streaming passes retain O(query terms + k) ranking state. BM25 operates
// over individual lines; bounded source ranges restore neighbouring stacktrace
// context. The displayed top-k is explicitly not all matches or a DoD proof.
func rankFile(path, query string, k, contextLines int, e *envelope) error {
	q, _ := features(query)
	terms := map[string]bool{}
	for _, term := range q {
		terms[term] = true
	}
	ordered := make([]string, 0, len(terms))
	for term := range terms {
		ordered = append(ordered, term)
	}
	sort.Strings(ordered)
	if len(terms) == 0 {
		e.line("query has no searchable terms")
		return fmt.Errorf("empty query")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return err
	}
	df := map[string]int{}
	documents, length := 0, 0
	s := scanner(f)
	for s.Scan() {
		fs, n := features(s.Text())
		documents++
		length += n
		seen := map[string]bool{}
		for _, feature := range fs {
			if terms[feature] && !seen[feature] {
				df[feature]++
				seen[feature] = true
			}
		}
	}
	if s.Err() != nil {
		e.linef("incomplete=true read_error=%v", s.Err())
		return s.Err()
	}
	if _, err = f.Seek(0, 0); err != nil {
		return err
	}
	average := 1.0
	if documents > 0 && length > 0 {
		average = float64(length) / float64(documents)
	}
	chosen := &rankHeap{}
	heap.Init(chosen)
	matches, line := 0, 0
	s = scanner(f)
	for s.Scan() {
		line++
		original := s.Text()
		fs, n := features(original)
		tf := map[string]int{}
		for _, feature := range fs {
			if terms[feature] {
				tf[feature]++
			}
		}
		score := 0.0
		for _, term := range ordered {
			count := tf[term]
			if count == 0 {
				continue
			}
			idf := math.Log(1 + (float64(documents-df[term])+0.5)/(float64(df[term])+0.5))
			boost := 1.0
			if strings.Contains(term, " ") {
				boost = 1.5
			}
			score += boost * idf * float64(count) * 2.2 / (float64(count) + 1.2*(0.25+0.75*float64(n)/average))
		}
		if score <= 0 {
			continue
		}
		matches++
		preview := stripANSI(original)
		if len(preview) > 400 {
			preview = preview[:400] + " …"
		}
		candidate := rankedLine{line, score, preview}
		if chosen.Len() < k {
			heap.Push(chosen, candidate)
		} else if score > (*chosen)[0].score {
			heap.Pop(chosen)
			heap.Push(chosen, candidate)
		}
	}
	after, statErr := f.Stat()
	if s.Err() != nil {
		e.linef("incomplete=true read_error=%v", s.Err())
		return s.Err()
	}
	currentPath, pathErr := os.Stat(path)
	if pathErr != nil || !os.SameFile(before, currentPath) || statErr != nil || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || documents != line {
		e.line("incomplete=true source changed between ranking passes; retry after the log stabilises")
		return fmt.Errorf("source changed")
	}
	e.linef("BM25 words+bigrams documents=%d matches=%d returned=%d truncated=%v scan_complete=true (ranking, not completeness proof)", documents, matches, chosen.Len(), matches > chosen.Len())
	if matches == 0 {
		e.line("no matches for query terms in complete scan")
		return nil
	}
	sorted := make([]rankedLine, chosen.Len())
	for i := len(sorted) - 1; i >= 0; i-- {
		sorted[i] = heap.Pop(chosen).(rankedLine)
	}
	for _, r := range sorted {
		a, b := max(1, r.number-contextLines), min(documents, r.number+contextLines)
		e.linef("line=%d score=%.4f", r.number, r.score)
		e.line(r.text)
		e.linef("next: %s", ShellQuote([]string{"bound", "read", path, fmt.Sprintf("%d:%d", a, b)}))
	}
	return nil
}
