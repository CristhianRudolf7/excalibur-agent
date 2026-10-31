package eval

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
)

// MeanCI is a mean with its 95% confidence interval.
type MeanCI struct {
	Mean float64 `json:"mean"`
	Low  float64 `json:"ci95_low"`
	High float64 `json:"ci95_high"`
	N    int     `json:"n"`
	Boot int     `json:"bootstrap_reps"`
	Seed int64   `json:"seed"`
}

// BootstrapCI computes the mean and the percentile 95% interval over
// reps bootstrap resamples with a seeded RNG (deterministic). It never
// calls a paid API and never touches the engine.
func BootstrapCI(xs []float64, reps int, seed int64) (MeanCI, error) {
	if len(xs) == 0 {
		return MeanCI{}, fmt.Errorf("eval: empty sample")
	}
	if reps <= 0 {
		reps = 10000
	}
	rng := rand.New(rand.NewSource(seed))
	means := make([]float64, reps)
	n := float64(len(xs))
	for r := 0; r < reps; r++ {
		var s float64
		for i := 0; i < len(xs); i++ {
			s += xs[rng.Intn(len(xs))]
		}
		means[r] = s / n
	}
	sort.Float64s(means)
	lo := means[int(0.025*float64(reps))]
	hi := means[int(0.975*float64(reps))-1]
	if hi < lo {
		lo, hi = hi, lo
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return MeanCI{Mean: s / n, Low: lo, High: hi, N: len(xs), Boot: reps, Seed: seed}, nil
}

// PairedDiffCI compares two configurations with paired bootstrap over
// tasks (Section 23.7): diffs[i] = b[i]-a[i] per task, resampled by
// task. Positive Low means B beats A with the interval excluding 0.
func PairedDiffCI(a, b []float64, reps int, seed int64) (MeanCI, error) {
	if len(a) != len(b) || len(a) == 0 {
		return MeanCI{}, fmt.Errorf("eval: paired samples need equal non-zero length")
	}
	diffs := make([]float64, len(a))
	for i := range a {
		diffs[i] = b[i] - a[i]
	}
	return BootstrapCI(diffs, reps, seed)
}

// PassAtK estimates pass@k from n trials with c successes: the
// probability that at least one of k draws succeeds (Section 23.1).
// Uses the unbiased estimator 1 - C(n-c,k)/C(n,k); returns 1 when the
// task is solved within k and 0 when k draws cannot cover the fails.
func PassAtK(n, c, k int) (float64, error) {
	if n <= 0 || k <= 0 {
		return 0, fmt.Errorf("eval: n and k must be positive")
	}
	if c < 0 || c > n {
		return 0, fmt.Errorf("eval: successes out of range")
	}
	if c == 0 {
		return 0, nil
	}
	if n-c < k {
		return 1, nil
	}
	return 1 - choose(n-c, k)/choose(n, k), nil
}

// PassPowK is pass^k: the probability of succeeding k times in a row,
// the reliability measure of tau-bench (Section 23.1). Estimated as
// (c/n)^k with Laplace smoothing only when n == 0 is rejected.
func PassPowK(n, c, k int) (float64, error) {
	if n <= 0 || k <= 0 {
		return 0, fmt.Errorf("eval: n and k must be positive")
	}
	if c < 0 || c > n {
		return 0, fmt.Errorf("eval: successes out of range")
	}
	p := float64(c) / float64(n)
	return math.Pow(p, float64(k)), nil
}

// PowerAnalysis returns the number of trials per arm needed to detect
// an absolute difference delta at ~80% power and 95% confidence for a
// Bernoulli outcome with base rate p, using the normal approximation.
// It always returns at least MinTrials (Section 23.7: never less than
// 5 per task).
const MinTrials = 5

// RequiredTrials guards the Appendix F floor.
func RequiredTrials(n int) int {
	if n < MinTrials {
		return MinTrials
	}
	return n
}

// PowerTrials estimates trials per arm for difference delta on base
// rate p (two-sided 95%, 80% power).
func PowerTrials(p, delta float64) (int, error) {
	if delta <= 0 || delta >= 1 {
		return 0, fmt.Errorf("eval: delta must be in (0,1)")
	}
	if p < 0 || p > 1 {
		return 0, fmt.Errorf("eval: base rate must be in [0,1]")
	}
	p2 := p + delta
	if p2 > 1 {
		p2 = 1
	}
	// Normal approximation: n = 2*pbar*(1-pbar)*(z_a+z_b)^2/delta^2.
	pbar := (p + p2) / 2
	const za = 1.96 // 95% two-sided
	const zb = 0.84 // 80% power
	n := 2 * pbar * (1 - pbar) * (za + zb) * (za + zb) / (delta * delta)
	return RequiredTrials(int(math.Ceil(n))), nil
}

// AcceptImprovement applies the Section 23.7 decision rule: accept when
// the paired interval excludes 0 and the point gain clears the
// practical bar (3 points on Terminal-Bench, or the suite threshold).
func AcceptImprovement(diff MeanCI, practicalBar float64) bool {
	if diff.Low <= 0 && diff.High >= 0 {
		return false
	}
	if diff.Mean <= 0 {
		return false
	}
	return diff.Mean >= practicalBar
}

func choose(n, k int) float64 {
	if k < 0 || k > n {
		return 0
	}
	if k > n-k {
		k = n - k
	}
	var num, den float64 = 1, 1
	for i := 1; i <= k; i++ {
		num *= float64(n - k + i)
		den *= float64(i)
	}
	return num / den
}
