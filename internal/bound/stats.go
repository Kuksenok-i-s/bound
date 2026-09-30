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
// lists spill files. Source bytes are summed per operation, including rereads.
// Printed bytes include envelopes, but not host framing or provider billing.
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
	snapshots := map[string]int64{}
	unattributed, invalid := 0, 0
	s := bufio.NewScanner(f)
	for s.Scan() {
		var e ledgerEntry
		if json.Unmarshal(s.Bytes(), &e) != nil || e.Kind == "" || e.Raw < 0 || e.Delivered < 0 {
			invalid++
			continue
		}
		if e.Snapshot != "" {
			snapshots[e.Snapshot] = e.Raw
		} else {
			unattributed++
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
	fmt.Fprintf(w, "%-6s %6s %10s %10s %6s\n", "kind", "calls", "source", "printed", "ratio")
	for _, k := range kinds {
		a := byKind[k]
		fmt.Fprintf(w, "%-6s %6d %10s %10s %5.0f%%\n", k, a.n, Human(a.raw), Human(a.delivered), pct(a.delivered, a.raw))
	}
	fmt.Fprintf(w, "%-6s %6d %10s %10s %5.0f%%   (%s raw -> %s delivered, UTF-8 bytes/4 estimate; operations only)\n",
		"total", total.n, Human(total.raw), Human(total.delivered), pct(total.delivered, total.raw), Tokens(total.raw), Tokens(total.delivered))
	sourceBytes := int64(0)
	for _, size := range snapshots {
		sourceBytes += size
	}
	fmt.Fprintf(w, "identified source snapshots=%d source_bytes=%d unattributed_calls=%d invalid_records=%d\n", len(snapshots), sourceBytes, unattributed, invalid)
	fmt.Fprintln(w, "Source totals count the full source again on each read; ratios are not task savings. Snapshots use path/size/mtime identity, not content deduplication. Printed bytes exclude this stats output and host framing; use provider input/output/cache counters for task A/B and cost.")
	if invalid > 0 {
		fmt.Fprintln(w, "incomplete=true; invalid ledger entries excluded from totals")
	}
	if s.Err() != nil {
		fmt.Fprintf(w, "incomplete=true ledger_read_error=%v\n", s.Err())
		return 2
	}
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
	if invalid > 0 {
		return 2
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
