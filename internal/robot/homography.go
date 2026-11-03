package robot

import (
	"errors"
	"math"
	"sort"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

// homography maps homogeneous pixel coordinates to homogeneous base XY.
type homography [3][3]float64

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

func (h homography) project(p []float64) ([2]float64, error) {
	w := h[2][0]*p[0] + h[2][1]*p[1] + h[2][2]
	scale := math.Abs(h[2][0]*p[0]) + math.Abs(h[2][1]*p[1]) + math.Abs(h[2][2])
	if !finite(w) || math.Abs(w) <= 1e-12*scale || scale == 0 {
		return [2]float64{}, errors.New("calibration projects this point to infinity")
	}
	q := [2]float64{(h[0][0]*p[0] + h[0][1]*p[1] + h[0][2]) / w, (h[1][0]*p[0] + h[1][1]*p[1] + h[1][2]) / w}
	if !finite(q[0]) || !finite(q[1]) {
		return q, errors.New("non-finite calibration projection")
	}
	return q, nil
}

func multiply(a, b homography) (c homography) {
	for i := range 3 {
		for j := range 3 {
			for k := range 3 {
				c[i][j] += a[i][k] * b[k][j]
			}
		}
	}
	return
}

func (h homography) inverse() (homography, error) {
	var c homography
	for i := range 3 {
		for j := range 3 {
			c[j][i] = h[(i+1)%3][(j+1)%3]*h[(i+2)%3][(j+2)%3] - h[(i+1)%3][(j+2)%3]*h[(i+2)%3][(j+1)%3]
		}
	}
	det := h[0][0]*c[0][0] + h[0][1]*c[1][0] + h[0][2]*c[2][0]
	if !finite(det) || math.Abs(det) < 1e-14 {
		return c, errors.New("degenerate calibration transform")
	}
	for i := range 3 {
		for j := range 3 {
			c[i][j] /= det
		}
	}
	return c, nil
}

// normalizePoints implements Hartley's centering and mean-distance scaling.
func normalizePoints(points [][2]float64) ([][2]float64, homography, error) {
	var cx, cy, distance float64
	for _, p := range points {
		if !finite(p[0]) || !finite(p[1]) {
			return nil, homography{}, errors.New("calibration coordinates must be finite")
		}
		cx += p[0]
		cy += p[1]
	}
	cx /= float64(len(points))
	cy /= float64(len(points))
	for _, p := range points {
		distance += math.Hypot(p[0]-cx, p[1]-cy)
	}
	distance /= float64(len(points))
	if distance < 1e-12 || !finite(distance) {
		return nil, homography{}, errors.New("degenerate calibration: coincident points")
	}
	scale := math.Sqrt2 / distance
	t := homography{{scale, 0, -scale * cx}, {0, scale, -scale * cy}, {0, 0, 1}}
	out := make([][2]float64, len(points))
	var xx, yy, xy float64
	for i, p := range points {
		x, y := scale*(p[0]-cx), scale*(p[1]-cy)
		out[i] = [2]float64{x, y}
		xx += x * x
		yy += y * y
		xy += x * y
	}
	if xx*yy-xy*xy <= 1e-10*(xx+yy)*(xx+yy) {
		return nil, t, errors.New("degenerate calibration: collinear points")
	}
	return out, t, nil
}

// fitHomography uses normalized homogeneous DLT. The smallest eigenvector of
// A^T A minimizes the algebraic least-squares residual for all correspondences.
// Error is inverse reprojection RMS in pixels, rather than mixed base units.
func fitHomography(points []api.RobotCalibrationPoint) (homography, float64, error) {
	fail := func(err error) (homography, float64, error) { return homography{}, 0, err }
	if len(points) < 4 {
		return fail(errors.New("calibration needs at least four correspondences"))
	}
	pixels, base := make([][2]float64, len(points)), make([][2]float64, len(points))
	for i, p := range points {
		if len(p.Pixel) != 2 || len(p.Base) != 2 {
			return fail(errors.New("pixel and base must each contain two coordinates"))
		}
		pixels[i] = [2]float64{p.Pixel[0], p.Pixel[1]}
		base[i] = [2]float64{p.Base[0], p.Base[1]}
	}
	pn, tp, err := normalizePoints(pixels)
	if err != nil {
		return fail(err)
	}
	bn, tb, err := normalizePoints(base)
	if err != nil {
		return fail(err)
	}
	var a, v [9][9]float64
	for i, p := range pn {
		u, w, x, y := p[0], p[1], bn[i][0], bn[i][1]
		rows := [2][9]float64{{-u, -w, -1, 0, 0, 0, x * u, x * w, x}, {0, 0, 0, -u, -w, -1, y * u, y * w, y}}
		for _, r := range rows {
			for j := range 9 {
				for k := range 9 {
					a[j][k] += r[j] * r[k]
				}
			}
		}
	}
	for i := range 9 {
		v[i][i] = 1
	}
	// Symmetric Jacobi rotations diagonalize this fixed 9x9 matrix without dependencies.
	for iteration := 0; iteration < 10000; iteration++ {
		p, q, maxOff, diag := 0, 1, 0.0, 0.0
		for i := range 9 {
			diag += math.Abs(a[i][i])
			for j := i + 1; j < 9; j++ {
				if math.Abs(a[i][j]) > maxOff {
					p, q, maxOff = i, j, math.Abs(a[i][j])
				}
			}
		}
		if maxOff < 1e-15*diag {
			break
		}
		theta := .5 * math.Atan2(2*a[p][q], a[q][q]-a[p][p])
		c, s := math.Cos(theta), math.Sin(theta)
		app, aqq, apq := a[p][p], a[q][q], a[p][q]
		a[p][p] = c*c*app - 2*s*c*apq + s*s*aqq
		a[q][q] = s*s*app + 2*s*c*apq + c*c*aqq
		a[p][q] = 0
		a[q][p] = 0
		for k := range 9 {
			if k != p && k != q {
				akp, akq := a[k][p], a[k][q]
				a[k][p] = c*akp - s*akq
				a[p][k] = a[k][p]
				a[k][q] = s*akp + c*akq
				a[q][k] = a[k][q]
			}
			vp, vq := v[k][p], v[k][q]
			v[k][p] = c*vp - s*vq
			v[k][q] = s*vp + c*vq
		}
	}
	indices := []int{0, 1, 2, 3, 4, 5, 6, 7, 8}
	sort.Slice(indices, func(i, j int) bool { return a[indices[i]][indices[i]] < a[indices[j]][indices[j]] })
	if a[indices[1]][indices[1]] <= 1e-10*a[indices[8]][indices[8]] {
		return fail(errors.New("degenerate calibration: insufficient independent correspondences"))
	}
	var hn homography
	for i := range 9 {
		hn[i/3][i%3] = v[i][indices[0]]
	}
	// Check invertibility in normalized coordinates, where scale has meaning.
	if _, err = hn.inverse(); err != nil {
		return fail(err)
	}
	tbi, err := tb.inverse()
	if err != nil {
		return fail(err)
	}
	h := multiply(multiply(tbi, hn), tp)
	// Invert by composing normalized inverses to avoid a meter/pixel scale test.
	hni, _ := hn.inverse()
	tpi, _ := tp.inverse()
	inverse := multiply(multiply(tpi, hni), tb)
	var sum float64
	for _, p := range points {
		q, err := inverse.project(p.Base)
		if err != nil {
			return fail(err)
		}
		dx, dy := q[0]-p.Pixel[0], q[1]-p.Pixel[1]
		sum += dx*dx + dy*dy
		if _, err = h.project(p.Pixel); err != nil {
			return fail(err)
		}
	}
	rms := math.Sqrt(sum / float64(len(points)))
	if !finite(rms) {
		return fail(errors.New("non-finite reprojection error"))
	}
	return h, rms, nil
}
