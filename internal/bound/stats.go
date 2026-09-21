package bound

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Stats reports raw vs delivered bytes per command kind from the ledger and
// lists spill files. Numbers are chars/4 estimates of what the agent would
// have received, not billed tokens: compare with your host's usage panel.
func Stats(args []string, w io.Writer) int {
	flags, _, _ := parseFlags(args, "clean")
	dir := SpillDir()
	if flags["clean"] == "true" {
		n := cleanSpill(dir, 7*24*time.Hour)
		fmt.Fprintf(w, "[bound stats] removed %d artifacts older than 7d from %s\n", n, dir)
	}
	f, err := os.Open(filepath.Join(dir, "ledger.jsonl"))
	if err != nil {
		fmt.Fprintf(w, "[bound stats] no ledger yet in %s\n", dir)
		return 0
	}
	defer f.Close()
	type agg struct {
		n              int
		raw, delivered int64
	}
	byKind := map[string]*agg{}
	total := agg{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		var e ledgerEntry
		if json.Unmarshal(s.Bytes(), &e) != nil {
			continue
		}
		a := byKind[e.Kind]
		if a == nil {
			a = &agg{}
			byKind[e.Kind] = a
		}
		a.n++
		a.raw += e.Raw
		a.delivered += e.Delivered
		total.n++
		total.raw += e.Raw
		total.delivered += e.Delivered
	}
	kinds := make([]string, 0, len(byKind))
	for k := range byKind {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	fmt.Fprintf(w, "[bound stats] %s\n", dir)
	fmt.Fprintf(w, "%-6s %6s %10s %10s %6s\n", "kind", "calls", "raw", "delivered", "kept")
	for _, k := range kinds {
		a := byKind[k]
		fmt.Fprintf(w, "%-6s %6d %10s %10s %5.0f%%\n", k, a.n, Human(a.raw), Human(a.delivered), pct(a.delivered, a.raw))
	}
	fmt.Fprintf(w, "%-6s %6d %10s %10s %5.0f%%   (%s raw -> %s delivered, chars/4 estimate)\n",
		"total", total.n, Human(total.raw), Human(total.delivered), pct(total.delivered, total.raw), Tokens(total.raw), Tokens(total.delivered))
	entries, _ := os.ReadDir(dir)
	var files []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && e.Name() != "ledger.jsonl" {
			files = append(files, e)
		}
	}
	if len(files) > 0 {
		fmt.Fprintf(w, "artifacts: %d (bound stats --clean removes >7d)\n", len(files))
	}
	return 0
}

func pct(a, b int64) float64 {
	if b == 0 {
		return 100
	}
	return float64(a) * 100 / float64(b)
}

func cleanSpill(dir string, age time.Duration) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	cut := time.Now().Add(-age)
	for _, e := range entries {
		if e.IsDir() || e.Name() == "ledger.jsonl" {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cut) {
			if os.Remove(filepath.Join(dir, e.Name())) == nil {
				n++
			}
		}
	}
	return n
}
