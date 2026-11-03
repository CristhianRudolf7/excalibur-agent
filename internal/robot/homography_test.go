package robot

import (
	"math"
	"math/rand"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

func syntheticPoints(t *testing.T, h homography, n int, noise float64) []api.RobotCalibrationPoint {
	t.Helper()
	rng := rand.New(rand.NewSource(42))
	points := make([]api.RobotCalibrationPoint, n)
	corners := [][2]float64{{20, 30}, {600, 30}, {600, 450}, {20, 450}}
	for i := range points {
		pixel := []float64{20 + rng.Float64()*580, 30 + rng.Float64()*420}
		if i < 4 {
			pixel = []float64{corners[i][0], corners[i][1]}
		}
		base, err := h.project(pixel)
		if err != nil {
			t.Fatal(err)
		}
		points[i] = api.RobotCalibrationPoint{Pixel: []float64{pixel[0] + noise*rng.NormFloat64(), pixel[1] + noise*rng.NormFloat64()}, Base: []float64{base[0], base[1]}}
	}
	return points
}
func TestNormalizedDLTKnownHomography(t *testing.T) {
	for _, h := range []homography{
		{{.001, .0002, -.3}, {-.0001, .0012, -.2}, {.0002, -.0001, 1}},
		{{.002, 0, 1}, {0, .003, 2}, {.001, .002, 0}}, // h33=0 must work with homogeneous DLT.
	} {
		for _, n := range []int{4, 30} {
			fit, rms, err := fitHomography(syntheticPoints(t, h, n, 0))
			if err != nil {
				t.Fatal(err)
			}
			if rms > 1e-7 {
				t.Fatalf("reprojection RMS %g", rms)
			}
			for _, pixel := range [][]float64{{50, 60}, {320, 240}, {550, 400}} {
				want, _ := h.project(pixel)
				got, err := fit.project(pixel)
				if err != nil || math.Hypot(got[0]-want[0], got[1]-want[1]) > 1e-8 {
					t.Fatalf("got %v want %v err %v", got, want, err)
				}
			}
		}
	}
}
func TestNormalizedDLTNoisyLeastSquares(t *testing.T) {
	h := homography{{.001, .0002, -.3}, {-.0001, .0012, -.2}, {.0002, -.0001, 1}}
	points := syntheticPoints(t, h, 80, .5)
	fit, rms, err := fitHomography(points)
	if err != nil {
		t.Fatal(err)
	}
	if rms < .3 || rms > 1.2 {
		t.Fatalf("unexpected pixel RMS %g", rms)
	}
	exact, err := h.project([]float64{320, 240})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := fit.project([]float64{320, 240})
	if math.Hypot(got[0]-exact[0], got[1]-exact[1]) > .0005 {
		t.Fatalf("noisy fit %v want %v", got, exact)
	}
	// The reported value is independently computed through the inverse mapping.
	inv, err := fit.inverse()
	if err != nil {
		t.Fatal(err)
	}
	var sum float64
	for _, p := range points {
		q, err := inv.project(p.Base)
		if err != nil {
			t.Fatal(err)
		}
		sum += math.Pow(q[0]-p.Pixel[0], 2) + math.Pow(q[1]-p.Pixel[1], 2)
	}
	if math.Abs(math.Sqrt(sum/float64(len(points)))-rms) > 1e-7 {
		t.Fatal("error is not inverse pixel reprojection RMS")
	}
}
func TestNormalizedDLTRejectsDegeneracy(t *testing.T) {
	h := homography{{.001, 0, 0}, {0, .001, 0}, {0, 0, 1}}
	for _, kind := range []string{"few", "pixel_line", "base_line", "coincident", "three_collinear", "duplicate", "nonfinite", "malformed"} {
		t.Run(kind, func(t *testing.T) {
			p := syntheticPoints(t, h, 4, 0)
			switch kind {
			case "few":
				p = p[:3]
			case "pixel_line":
				for i := range p {
					p[i].Pixel = []float64{float64(i), float64(i) * 2}
				}
			case "base_line":
				for i := range p {
					p[i].Base = []float64{float64(i), float64(i) * 2}
				}
			case "coincident":
				for i := range p {
					p[i].Pixel = []float64{10, 10}
				}
			case "three_collinear":
				p[0].Pixel = []float64{0, 0}
				p[1].Pixel = []float64{1, 1}
				p[2].Pixel = []float64{2, 2}
				for i := range p {
					p[i].Base = []float64{p[i].Pixel[0] * .001, p[i].Pixel[1] * .001}
				}
			case "duplicate":
				p[3] = p[0]
			case "nonfinite":
				p[0].Pixel[0] = math.Inf(1)
			case "malformed":
				p[0].Base = []float64{1}
			}
			if _, _, err := fitHomography(p); err == nil {
				t.Fatalf("accepted %s", kind)
			}
		})
	}
	_, err := (homography{{1, 0, 0}, {0, 1, 0}, {1, 0, -100}}).project([]float64{100, 50})
	if err == nil || !strings.Contains(err.Error(), "infinity") {
		t.Fatal(err)
	}
}
