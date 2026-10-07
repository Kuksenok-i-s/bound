package bound

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// logProfile summarises the arrival process of timestamped log lines: rate per
// bin, duplicate collapse, inter-arrival spread, over-dispersion and an
// exponential-kernel Hawkes fit. Output is a bounded set of lines; the raw
// timestamps never leave the process.
type logProfile struct {
	ts      []float64 // seconds since first
	t0      time.Time
	untimed int
}

func (p *logProfile) add(t time.Time, ok bool) {
	if !ok {
		p.untimed++
		return
	}
	if len(p.ts) == 0 {
		p.t0 = t
	}
	p.ts = append(p.ts, t.Sub(p.t0).Seconds())
}

// niceBins are candidate bin widths for the automatic histogram.
var niceBins = []time.Duration{
	time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second,
	time.Minute, 5 * time.Minute, 10 * time.Minute, 30 * time.Minute,
	time.Hour, 2 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour,
}

func autoBin(span float64, maxBins int) time.Duration {
	for _, b := range niceBins {
		if span/b.Seconds() <= float64(maxBins) {
			return b
		}
	}
	return niceBins[len(niceBins)-1]
}

// render appends the profile to the envelope. bin==0 selects a bin width
// automatically; gap collapses lines closer than gap into one logical event.
func (p *logProfile) render(e *envelope, l Limits, bin, gap time.Duration) {
	e.section("profile")
	e.line("about: arrival-time statistics of the selected lines, not their content; heuristic, parameters shown so the verdict can be checked; use it to pick the next --since/--grep, not as a root cause")
	if len(p.ts) == 0 {
		e.linef("events=0 untimed=%d; no leading timestamp recognised, profile unavailable", p.untimed)
		return
	}
	sort.Float64s(p.ts)
	span := p.ts[len(p.ts)-1]
	if span <= 0 {
		span = 1e-3
	}
	if bin <= 0 {
		bin = autoBin(span, l.ProfileBins)
	}
	raw := len(p.ts)
	e.linef("events=%d untimed=%d span=%s rate=%s", raw, p.untimed, shortDur(span), ratePerSec(float64(raw)/span))

	counts := histogram(p.ts, span, bin.Seconds())
	binFano := fano(counts)
	e.linef("bins=%d width=%s shown=%d fano=%.1f (poisson=1; >1 clustered, <1 regular)", len(counts), shortDur(bin.Seconds()), min(len(counts), l.ProfileBins), binFano)
	p.renderBins(e, counts, bin, l.ProfileBins)

	ev := collapse(p.ts, gap.Seconds())
	if len(ev) < 3 {
		e.linef("collapsed gap=%s logical_events=%d; too few events for inter-arrival or fit", gap, len(ev))
		return
	}
	gaps := make([]float64, len(ev)-1)
	for i := 1; i < len(ev); i++ {
		gaps[i-1] = ev[i] - ev[i-1]
	}
	sort.Float64s(gaps)
	p10, p50, p90 := quantile(gaps, 0.1), quantile(gaps, 0.5), quantile(gaps, 0.9)
	e.linef("collapsed gap=%s logical_events=%d (%.1f lines/event) interarrival p10=%s p50=%s p90=%s", gap, len(ev), float64(raw)/float64(len(ev)), shortDur(p10), shortDur(p50), shortDur(p90))

	regular := p10 > 0 && p90/p10 < 1.15
	if len(ev) < 20 {
		e.line("hawkes: skipped (needs ≥20 logical events)")
		if regular {
			e.line("verdict: " + periodicVerdict(p50, binFano))
		}
		return
	}
	fitEv := ev
	if len(fitEv) > l.ProfileFit {
		fitEv = fitEv[len(fitEv)-l.ProfileFit:]
	}
	base := fitEv[0]
	rel := make([]float64, len(fitEv))
	for i, t := range fitEv {
		rel[i] = t - base
	}
	T := rel[len(rel)-1] + 1e-3
	h := fitHawkes(rel, T)
	llP := poissonLogLik(len(rel), T)
	gain := h.logLik - llP
	e.linef("hawkes fit_events=%d mu=%s branching=%.2f decay=%s poisson_mu=%s dlogL=%.0f", len(rel), ratePerSec(h.mu), h.branching(), shortDur(h.decay()), ratePerSec(float64(len(rel))/T), gain)
	e.line("verdict: " + verdict(h, gain, gap.Seconds(), regular, p50, binFano))
}

// periodicVerdict describes a fixed-period source; a high bin dispersion means
// the timer only runs inside bursts of activity rather than continuously.
func periodicVerdict(period, binFano float64) string {
	s := fmt.Sprintf("periodic (period≈%s); a timer or fixed retry, not a random arrival process", shortDur(period))
	if binFano > 10 {
		s += "; activity itself comes in windows (bin fano≫1), see the histogram"
	}
	return s
}

func (p *logProfile) renderBins(e *envelope, counts []int, bin time.Duration, maxShown int) {
	type cell struct {
		idx, n int
	}
	cells := make([]cell, len(counts))
	peak := 0
	for i, c := range counts {
		cells[i] = cell{i, c}
		peak = max(peak, c)
	}
	if len(cells) > maxShown {
		sort.Slice(cells, func(a, b int) bool { return cells[a].n > cells[b].n })
		cells = cells[:maxShown]
		sort.Slice(cells, func(a, b int) bool { return cells[a].idx < cells[b].idx })
		e.linef("showing the %d largest bins; the full histogram needs a wider --bin", maxShown)
	}
	layout := "15:04:05"
	switch {
	case bin >= 24*time.Hour:
		layout = "01-02"
	case bin >= time.Minute:
		layout = "15:04"
	}
	width := 1
	for n := peak; n >= 10; n /= 10 {
		width++
	}
	for _, c := range cells {
		at := p.t0.Add(time.Duration(c.idx) * bin).Format(layout)
		bar := ""
		if c.n > 0 {
			bar = strings.Repeat("#", max(1, 30*c.n/peak))
		}
		e.linef("  %s %*d %s", at, width, c.n, bar)
	}
}

func histogram(ts []float64, span, bin float64) []int {
	nb := max(1, int(span/bin)+1)
	counts := make([]int, nb)
	for _, t := range ts {
		counts[min(nb-1, int(t/bin))]++
	}
	return counts
}

// fano is the index of dispersion var/mean of bin counts.
func fano(counts []int) float64 {
	if len(counts) < 2 {
		return math.NaN()
	}
	sum := 0.0
	for _, c := range counts {
		sum += float64(c)
	}
	mean := sum / float64(len(counts))
	if mean == 0 {
		return math.NaN()
	}
	v := 0.0
	for _, c := range counts {
		d := float64(c) - mean
		v += d * d
	}
	return v / float64(len(counts)-1) / mean
}

// collapse merges timestamps closer than gap into one logical event.
func collapse(ts []float64, gap float64) []float64 {
	out := make([]float64, 0, len(ts))
	for _, t := range ts {
		if len(out) == 0 || t-out[len(out)-1] >= gap {
			out = append(out, t)
		}
	}
	return out
}

func quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(q * float64(len(sorted)-1))
	return sorted[i]
}

// hawkes holds an exponential-kernel fit λ(t) = mu + Σ alpha·exp(-beta(t-ti)).
type hawkes struct {
	mu, alpha, beta float64
	logLik          float64
}

func (h hawkes) branching() float64 { return h.alpha / h.beta }
func (h hawkes) decay() float64     { return 1 / h.beta }

func poissonLogLik(n int, T float64) float64 {
	mu := float64(n) / T
	return float64(n)*math.Log(mu) - mu*T
}

// hawkesLogLik uses the O(n) recursion A_i = exp(-beta Δ)(1 + A_{i-1}).
func hawkesLogLik(ts []float64, T, mu, alpha, beta float64) float64 {
	ll, A := 0.0, 0.0
	for i, t := range ts {
		if i > 0 {
			A = math.Exp(-beta*(t-ts[i-1])) * (1 + A)
		}
		lam := mu + alpha*A
		if lam <= 0 {
			return math.Inf(-1)
		}
		ll += math.Log(lam)
	}
	comp := mu * T
	for _, t := range ts {
		comp += alpha / beta * (1 - math.Exp(-beta*(T-t)))
	}
	return ll - comp
}

func fitHawkes(ts []float64, T float64) hawkes {
	n := float64(len(ts))
	neg := func(x []float64) float64 {
		return -hawkesLogLik(ts, T, math.Exp(x[0]), math.Exp(x[1]), math.Exp(x[2]))
	}
	best, bestV := []float64(nil), math.Inf(1)
	for _, beta0 := range []float64{0.1, 1, 10} {
		x, v := nelderMead(neg, []float64{math.Log(0.5 * n / T), math.Log(0.5 * beta0), math.Log(beta0)}, 0.5, 300)
		if v < bestV {
			best, bestV = x, v
		}
	}
	return hawkes{mu: math.Exp(best[0]), alpha: math.Exp(best[1]), beta: math.Exp(best[2]), logLik: -bestV}
}

// nelderMead is a compact simplex minimiser; adequate for three parameters.
func nelderMead(f func([]float64) float64, x0 []float64, step float64, iters int) ([]float64, float64) {
	n := len(x0)
	pts := make([][]float64, n+1)
	vals := make([]float64, n+1)
	for i := range pts {
		p := append([]float64(nil), x0...)
		if i > 0 {
			p[i-1] += step
		}
		pts[i], vals[i] = p, f(p)
	}
	idx := make([]int, n+1)
	for it := 0; it < iters; it++ {
		for i := range idx {
			idx[i] = i
		}
		sort.Slice(idx, func(a, b int) bool { return vals[idx[a]] < vals[idx[b]] })
		sp, sv := make([][]float64, n+1), make([]float64, n+1)
		for i, j := range idx {
			sp[i], sv[i] = pts[j], vals[j]
		}
		pts, vals = sp, sv
		if math.Abs(vals[n]-vals[0]) < 1e-7 {
			break
		}
		cen := make([]float64, n)
		for _, p := range pts[:n] {
			for j := range cen {
				cen[j] += p[j] / float64(n)
			}
		}
		move := func(k float64) []float64 {
			q := make([]float64, n)
			for j := range q {
				q[j] = cen[j] + k*(pts[n][j]-cen[j])
			}
			return q
		}
		refl := move(-1)
		fr := f(refl)
		switch {
		case fr < vals[0]:
			exp := move(-2)
			if fe := f(exp); fe < fr {
				pts[n], vals[n] = exp, fe
			} else {
				pts[n], vals[n] = refl, fr
			}
		case fr < vals[n-1]:
			pts[n], vals[n] = refl, fr
		default:
			con := move(0.5)
			if fc := f(con); fc < vals[n] {
				pts[n], vals[n] = con, fc
				continue
			}
			for i := 1; i <= n; i++ {
				for j := range pts[i] {
					pts[i][j] = pts[0][j] + 0.5*(pts[i][j]-pts[0][j])
				}
				vals[i] = f(pts[i])
			}
		}
	}
	bi := 0
	for i := range vals {
		if vals[i] < vals[bi] {
			bi = i
		}
	}
	return pts[bi], vals[bi]
}

// verdict turns fit parameters into one actionable sentence. Thresholds are
// heuristic and stated in the output; the parameters remain visible above.
func verdict(h hawkes, gain, gap float64, regular bool, period, binFano float64) string {
	n := h.branching()
	switch {
	case regular:
		return periodicVerdict(period, binFano)
	case n > 0.2 && h.decay() < 2*gap:
		return fmt.Sprintf("excitation decays within %s (<2×gap): multi-line events, not a cascade; raise --gap", shortDur(h.decay()))
	case n >= 0.95:
		return "branching≈1 with mu≈0: externally driven, non-stationary rate; read the bins, the Hawkes fit is degenerate"
	case n >= 0.15 && gain > 10:
		return fmt.Sprintf("self-exciting cascade: ~%.0f%% of events triggered by a prior event within ~%s", 100*n, shortDur(h.decay()))
	default:
		return "poisson-like: no significant self-excitation; a rate threshold per bin is sufficient"
	}
}

func shortDur(sec float64) string {
	switch {
	case sec < 1e-3:
		return fmt.Sprintf("%.0fµs", sec*1e6)
	case sec < 1:
		return fmt.Sprintf("%.0fms", sec*1e3)
	case sec < 60:
		return fmt.Sprintf("%.2fs", sec)
	case sec < 3600:
		return fmt.Sprintf("%.1fm", sec/60)
	}
	return fmt.Sprintf("%.1fh", sec/3600)
}

func ratePerSec(r float64) string {
	switch {
	case r >= 1:
		return fmt.Sprintf("%.2f/s", r)
	case r*60 >= 1:
		return fmt.Sprintf("%.2f/min", r*60)
	}
	return fmt.Sprintf("%.2f/h", r*3600)
}

func parseDurationFlag(flags map[string]string, key string, def time.Duration) (time.Duration, error) {
	v, ok := flags[key]
	if !ok {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("bad --%s %q (use 60s, 5m, 100ms)", key, v)
	}
	return d, nil
}
