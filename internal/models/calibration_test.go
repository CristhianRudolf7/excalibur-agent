// Calibration tests (Section 19.4): expected calibration error
// on an own labeled set and threshold selection with abstention.
package models

import (
	"testing"
)

func labeledSet() []Sample {
	var out []Sample
	for i := 0; i < 10; i++ {
		out = append(out, Sample{Prob: 0.05 + 0.02*float64(i), Label: false})
	}
	for i := 0; i < 10; i++ {
		out = append(out, Sample{Prob: 0.85 + 0.01*float64(i), Label: true})
	}
	return out
}

func TestECEWellCalibratedNearZero(t *testing.T) {
	samples := labeledSet()
	ece := ECE(samples, 10)
	if ece > 0.15 {
		t.Fatalf("ece = %v, want small on a separable set", ece)
	}
	if ECE(nil, 10) != 0 {
		t.Fatal("empty set ece should be 0")
	}
}

func TestECEDetectsOverconfidence(t *testing.T) {
	var over []Sample
	for i := 0; i < 20; i++ {
		over = append(over, Sample{Prob: 0.95, Label: i%2 == 0})
	}
	if ECE(over, 10) < 0.3 {
		t.Fatalf("overconfident ece = %v, want large", ECE(over, 10))
	}
}

func TestCalibrateSeparatesBands(t *testing.T) {
	samples := labeledSet()
	th, rep, err := Calibrate(samples)
	if err != nil {
		t.Fatal(err)
	}
	if err := th.Validate(); err != nil {
		t.Fatalf("calibrated thresholds invalid: %+v", th)
	}
	if rep.Accuracy < 0.95 {
		t.Fatalf("selective accuracy = %v, want >= 0.95", rep.Accuracy)
	}
	if rep.Samples != len(samples) {
		t.Fatalf("report samples = %d, want %d", rep.Samples, len(samples))
	}
	// The middle band abstains: a 0.5 probe defers.
	if Decide(0.5, th) != OutcomeDefer {
		t.Fatalf("middle probe should defer with %+v", th)
	}
	t.Logf("calibrated=%+v ece=%.3f acc=%.3f cov=%.3f abst=%d",
		th, rep.ECE, rep.Accuracy, rep.Coverage, rep.Abstentions)
}

func TestCalibrateNeedsLabeledRecords(t *testing.T) {
	if _, _, err := Calibrate(labeledSet()[:3]); err == nil {
		t.Fatal("fewer than 10 samples should fail calibration")
	}
}
