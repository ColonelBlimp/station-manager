package demod

// goertzelMulti runs 8 Goertzel recursions in parallel over one
// NSPS-sample window of audio, returning |X(f_k)|² for each of the 8
// FT8 tones.
//
// This is a deliberate duplicate of the pure-Go goertzelMulti in
// research/candidates — kept inline while the demod package is small
// and exploratory. If a third caller appears (or demod ever needs the
// SIMD path), extract to a shared research/sigproc/ package then.
//
// Unrolled: the 8 Goertzel recursions are mathematically independent
// (only the serial chain WITHIN one recursion forces ordering), so
// holding the 16 state values as named locals lets the compiler keep
// them in registers and the CPU's superscalar pipeline dispatch all
// 8 multiply-add chains in parallel.
//
// Returns the closed-form Goertzel power expression
//
//	|X(f)|² = s_{N-1}² + s_{N-2}² - c · s_{N-1} · s_{N-2}
//
// at each of the 8 frequencies given by the supplied coefficients
// (c_k = 2 · cos(2π · f_k / fs)). Indexing of the returned array
// matches the FT8 8-FSK alphabet order: 0..7.
func goertzelMulti(samples []float32, start, n int, coeffs [ft8ToneCount]float64) [ft8ToneCount]float64 {
	c0, c1, c2, c3, c4, c5, c6, c7 := coeffs[0], coeffs[1], coeffs[2], coeffs[3], coeffs[4], coeffs[5], coeffs[6], coeffs[7]

	var (
		a1, a2   float64
		b1, b2   float64
		c_1, c_2 float64
		d1, d2   float64
		e1, e2   float64
		f1, f2   float64
		g1, g2   float64
		h1, h2   float64
	)

	audio := samples[start : start+n]

	for _, sample := range audio {
		x := float64(sample)

		na := x + c0*a1 - a2
		a2 = a1
		a1 = na
		nb := x + c1*b1 - b2
		b2 = b1
		b1 = nb
		nc := x + c2*c_1 - c_2
		c_2 = c_1
		c_1 = nc
		nd := x + c3*d1 - d2
		d2 = d1
		d1 = nd
		ne := x + c4*e1 - e2
		e2 = e1
		e1 = ne
		nf := x + c5*f1 - f2
		f2 = f1
		f1 = nf
		ng := x + c6*g1 - g2
		g2 = g1
		g1 = ng
		nh := x + c7*h1 - h2
		h2 = h1
		h1 = nh
	}

	return [ft8ToneCount]float64{
		a1*a1 + a2*a2 - c0*a1*a2,
		b1*b1 + b2*b2 - c1*b1*b2,
		c_1*c_1 + c_2*c_2 - c2*c_1*c_2,
		d1*d1 + d2*d2 - c3*d1*d2,
		e1*e1 + e2*e2 - c4*e1*e2,
		f1*f1 + f2*f2 - c5*f1*f2,
		g1*g1 + g2*g2 - c6*g1*g2,
		h1*h1 + h2*h2 - c7*h1*h2,
	}
}
