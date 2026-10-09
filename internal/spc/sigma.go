package spc

import "math"

// DPMO is defects per million opportunities and its sigma level, with
// the customary 1.5 sigma shift. No defects is taken as half of one, so
// a small clean sample does not read as perfect.
func DPMO(defects, opportunities int) (float64, float64) {
	if opportunities == 0 {
		return 0, 0
	}
	d := float64(defects)
	if d == 0 {
		d = 0.5
	}
	rate := d / float64(opportunities)
	if rate >= 1 {
		rate = 1 - 1e-9
	}
	// A process failing every time has no sigma to speak of: zero, not a
	// negative level.
	return roundTo(float64(defects)/float64(opportunities)*1e6, 0), math.Max(0, roundTo(normInv(1-rate)+1.5, 2))
}

// normInv is the standard normal quantile (Acklam's approximation, good
// to about 1e-9), for turning a yield into a sigma level.
func normInv(p float64) float64 {
	a := []float64{-3.969683028665376e+01, 2.209460984245205e+02, -2.759285104469687e+02, 1.383577518672690e+02, -3.066479806614716e+01, 2.506628277459239e+00}
	b := []float64{-5.447609879822406e+01, 1.615858368580409e+02, -1.556989798598866e+02, 6.680131188771972e+01, -1.328068155288572e+01}
	c := []float64{-7.784894002430293e-03, -3.223964580411365e-01, -2.400758277161838e+00, -2.549732539343734e+00, 4.374664141464968e+00, 2.938163982698783e+00}
	d := []float64{7.784695709041462e-03, 3.224671290700398e-01, 2.445134137142996e+00, 3.754408661907416e+00}
	const lo, hi = 0.02425, 1 - 0.02425
	switch {
	case p <= 0:
		return math.Inf(-1)
	case p >= 1:
		return math.Inf(1)
	case p < lo:
		q := math.Sqrt(-2 * math.Log(p))
		return (((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) / ((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	case p > hi:
		q := math.Sqrt(-2 * math.Log(1-p))
		return -(((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) / ((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	}
	q := p - 0.5
	r := q * q
	return (((((a[0]*r+a[1])*r+a[2])*r+a[3])*r+a[4])*r + a[5]) * q / (((((b[0]*r+b[1])*r+b[2])*r+b[3])*r+b[4])*r + 1)
}

func roundTo(x float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(x*p) / p
}
