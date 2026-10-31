// Own calibration for Section 19.4: expected calibration error
// and threshold selection on the harness's own labeled records.
package models

import (
	"fmt"
	"sort"
)

// Sample is one labeled decider record: the probability the decider
// reported and whether acting on it was correct.
type Sample struct {
	Prob  float64 `json:"probability"`
	Label bool    `json:"label"`
}

// CalibrationReport summarizes a labeled set.
type CalibrationReport struct {
	Samples     int        `json:"samples"`
	ECE         float64    `json:"ece"`
	Accuracy    float64    `json:"accuracy"`
	Coverage    float64    `json:"coverage"`
	Thresholds  Thresholds `json:"thresholds"`
	Abstentions int        `json:"abstentions"`
}

// ECE returns the expected calibration error over bins equal-width
// bins in [0,1]. It is the mean |accuracy - confidence| weighted by
// bin mass. Samples outside [0,1] are clamped, matching Answer.
func ECE(samples []Sample, bins int) float64 {
	if len(samples) == 0 || bins <= 0 {
		return 0
	}
	type bin struct {
		n     int
		prob  float64
		right int
	}
	bs := make([]bin, bins)
	for _, s := range samples {
		p := s.Prob
		if p < 0 {
			p = 0
		}
		if p > 1 {
			p = 1
		}
		i := int(p * float64(bins))
		if i >= bins {
			i = bins - 1
		}
		bs[i].n++
		bs[i].prob += p
		if s.Label {
			bs[i].right++
		}
	}
	var ece float64
	for _, b := range bs {
		if b.n == 0 {
			continue
		}
		conf := b.prob / float64(b.n)
		acc := float64(b.right) / float64(b.n)
		diff := acc - conf
		if diff < 0 {
			diff = -diff
		}
		ece += float64(b.n) / float64(len(samples)) * diff
	}
	return ece
}

// SelectiveAccuracy applies the abstention rule to the labeled set:
// samples in the middle band abstain (neither right nor wrong).
// Accuracy is right/(right+wrong) over decided samples; coverage is
// decided/total.
func SelectiveAccuracy(samples []Sample, t Thresholds) (accuracy, coverage float64) {
	var right, decided int
	for _, s := range samples {
		switch Decide(s.Prob, t) {
		case OutcomeDefer:
			continue
		case OutcomeAct:
			decided++
			if s.Label {
				right++
			}
		case OutcomeAlternative:
			decided++
			if !s.Label {
				right++
			}
		}
	}
	if len(samples) == 0 {
		return 0, 0
	}
	coverage = float64(decided) / float64(len(samples))
	if decided == 0 {
		return 0, coverage
	}
	return float64(right) / float64(decided), coverage
}

// Calibrate picks the abstention band on the harness's own records.
// It grid-searches Low in [0.1,0.5] and High in [0.6,0.95] at 0.05
// steps and keeps the band with the best selective accuracy,
// breaking ties toward higher coverage and then toward the wider
// band. At least 10 samples are required; fewer is a calibration
// error, not a silent default.
func Calibrate(samples []Sample) (Thresholds, CalibrationReport, error) {
	if len(samples) < 10 {
		return Thresholds{}, CalibrationReport{}, fmt.Errorf("models: calibration needs at least 10 labeled samples, got %d", len(samples))
	}
	best := DefaultThresholds()
	bestAcc, bestCov := SelectiveAccuracy(samples, best)
	bestWidth := best.High - best.Low
	for low := 0.1; low <= 0.5001; low += 0.05 {
		for high := 0.6; high <= 0.9501; high += 0.05 {
			t := Thresholds{Low: round2(low), High: round2(high)}
			acc, cov := SelectiveAccuracy(samples, t)
			width := t.High - t.Low
			if acc > bestAcc+1e-9 ||
				(acc == bestAcc && cov > bestCov+1e-9) ||
				(acc == bestAcc && cov == bestCov && width > bestWidth+1e-9) {
				best, bestAcc, bestCov, bestWidth = t, acc, cov, width
			}
		}
	}
	acc, cov := SelectiveAccuracy(samples, best)
	abst := 0
	for _, s := range samples {
		if Decide(s.Prob, best) == OutcomeDefer {
			abst++
		}
	}
	return best, CalibrationReport{
		Samples:     len(samples),
		ECE:         ECE(samples, 10),
		Accuracy:    acc,
		Coverage:    cov,
		Thresholds:  best,
		Abstentions: abst,
	}, nil
}

// SortedProbs returns the sample probabilities in ascending order
// for calibration curves in tests and telemetry.
func SortedProbs(samples []Sample) []float64 {
	out := make([]float64, 0, len(samples))
	for _, s := range samples {
		out = append(out, s.Prob)
	}
	sort.Float64s(out)
	return out
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
