package bound

import (
	"bytes"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func profileOf(t *testing.T, ts []float64) string {
	t.Helper()
	var p logProfile
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, s := range ts {
		p.add(base.Add(time.Duration(s*float64(time.Second))), true)
	}
	e := newEnvelope(24000)
	p.render(e, DefaultLimits(), 0, 100*time.Millisecond)
	return e.String()
}

func TestProfilePeriodicSourceIsCalledATimer(t *testing.T) {
	ts := make([]float64, 0, 3000)
	for i := 0; i < 600; i++ {
		for k := 0; k < 5; k++ { // five log lines per tick, 1ms apart
			ts = append(ts, float64(i)+float64(k)*0.001)
		}
	}
	out := profileOf(t, ts)
	for _, want := range []string{"events=3000", "logical_events=600 (5.0 lines/event)", "verdict: periodic (period≈1.00s)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("lost %q: %s", want, out)
		}
	}
}

func TestProfilePoissonSourceHasNoCascade(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	ts, t0 := []float64{}, 0.0
	for len(ts) < 2000 {
		t0 += r.ExpFloat64() / 2 // rate 2/s
		ts = append(ts, t0)
	}
	out := profileOf(t, ts)
	if !strings.Contains(out, "verdict: poisson-like") {
		t.Fatalf("%s", out)
	}
}

// simulateHawkes draws from λ(t)=mu+Σ alpha·e^{-beta(t-ti)} by Ogata thinning.
func simulateHawkes(r *rand.Rand, mu, alpha, beta, T float64) []float64 {
	var ts []float64
	t, lamStar := 0.0, mu
	for t < T {
		t += r.ExpFloat64() / lamStar
		lam := mu
		for _, ti := range ts {
			lam += alpha * math.Exp(-beta*(t-ti))
		}
		if r.Float64()*lamStar <= lam {
			ts = append(ts, t)
			lamStar = lam + alpha
		} else {
			lamStar = lam
		}
	}
	return ts
}

func TestProfileRecoversHawkesBranchingRatio(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	ts := simulateHawkes(r, 0.2, 1.0, 2.0, 4000) // branching 0.5, decay 0.5s
	out := profileOf(t, ts)
	if !strings.Contains(out, "verdict: self-exciting cascade") {
		t.Fatalf("%s", out)
	}
	h := fitHawkes(ts, ts[len(ts)-1]+1e-3)
	if n, d := h.branching(), h.decay(); math.Abs(n-0.5) > 0.12 || d < 0.25 || d > 1.0 {
		t.Fatalf("branching=%.2f decay=%.2fs (want ≈0.5, ≈0.5s): %s", n, d, out)
	}
}

func TestProfileWithoutTimestampsSaysSo(t *testing.T) {
	var p logProfile
	p.add(time.Time{}, false)
	e := newEnvelope(4000)
	p.render(e, DefaultLimits(), 0, time.Second)
	if !strings.Contains(e.String(), "events=0 untimed=1") {
		t.Fatal(e.String())
	}
}

func TestLogProfileCountsOnlyMatchedLinesAndStaysBounded(t *testing.T) {
	isolatedAudit(t)
	var b strings.Builder
	base := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5000; i++ {
		at := base.Add(time.Duration(i) * 200 * time.Millisecond).Format("2006-01-02 15:04:05.000")
		if i%10 == 0 {
			fmt.Fprintf(&b, "%s E  svc[1] failed\n", at)
		} else {
			fmt.Fprintf(&b, "%s I  svc[1] ok\n", at)
		}
	}
	path := filepath.Join(t.TempDir(), "svc.log")
	os.WriteFile(path, []byte(b.String()), 0600)
	var out bytes.Buffer
	if code := Log([]string{path, "--grep", "failed", "--profile", "--bin", "1m"}, &out); code != 0 {
		t.Fatal(code, out.String())
	}
	s := out.String()
	for _, want := range []string{"matched=500", "shown=0", "--- profile", "events=500 untimed=0", "width=1.0m", "verdict: periodic (period≈2.00s)"} {
		if !strings.Contains(s, want) {
			t.Fatalf("lost %q: %s", want, s)
		}
	}
	if out.Len() > 4000 || strings.Count(s, "\n") > 60 {
		t.Fatalf("profile not bounded: %d bytes\n%s", out.Len(), s)
	}
	if code := Log([]string{path, "--profile", "--bin", "nope"}, &out); code != 2 {
		t.Fatal("bad --bin must fail", code)
	}
}
