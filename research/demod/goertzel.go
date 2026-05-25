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

// goertzelMultiComplex runs the same 8-parallel Goertzel recursion as
// goertzelMulti but returns complex amplitudes instead of squared
// magnitudes — the input that coherent demod needs.
//
// **Phase convention.** The standard Goertzel "extra step" closed-form
// (per Wikipedia / Mitra DSP) is
//
//	y(N) = s(N) - e^{-jω}·s(N-1)    with    s(N) = 2cos(ω)·s[N-1] - s[N-2]
//
// Substituting and simplifying:
//
//	y(N) = (2cos(ω) - e^{-jω})·s[N-1] - s[N-2]
//	     = e^{+jω}·s[N-1] - s[N-2]
//
// This `y(N)` equals e^{jωN}·X(ω) where X(ω) = Σ x[n]·e^{-jωn} is the
// standard DTFT — the extra factor e^{jωN} is unity at on-bin frequencies
// (ωN a multiple of 2π) and a known phase rotation at off-bin freqs.
// For FT8 with N=NSPS=1920 and tones at 1000+k·6.25 Hz at fs=12 kHz,
// ωN = 2π·(160+k) so e^{jωN}=1 — the on-bin DTFT comes back exactly.
//
// Verified by hand for x=[1,0,0,0] at ω=π/2 (Goertzel y=1, DTFT=1) and
// pinned by unit tests in goertzel_complex_test.go.
//
// unitDelays[k] must be `complex(cos(ω_k), +sin(ω_k))` = e^{+jω_k}
// where ω_k = 2π·f_k/Fs. Caller pre-computes once per (freq, fs) and
// reuses across symbols. Mismatched unitDelays vs coeffs (which uses
// c = 2·cos(ω)) will silently produce wrong phases — tests guard.
func goertzelMultiComplex(samples []float32, start, n int, coeffs [ft8ToneCount]float64, unitDelays [ft8ToneCount]complex128) [ft8ToneCount]complex128 {
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

	// Closed-form complex output per tone: y(N) = e^{+jω}·s[N-1] - s[N-2].
	// Equals e^{jωN}·X(ω) — exactly X(ω) at on-bin frequencies (which is
	// where every FT8 tone sits by construction of the 6.25 Hz grid).
	return [ft8ToneCount]complex128{
		unitDelays[0]*complex(a1, 0) - complex(a2, 0),
		unitDelays[1]*complex(b1, 0) - complex(b2, 0),
		unitDelays[2]*complex(c_1, 0) - complex(c_2, 0),
		unitDelays[3]*complex(d1, 0) - complex(d2, 0),
		unitDelays[4]*complex(e1, 0) - complex(e2, 0),
		unitDelays[5]*complex(f1, 0) - complex(f2, 0),
		unitDelays[6]*complex(g1, 0) - complex(g2, 0),
		unitDelays[7]*complex(h1, 0) - complex(h2, 0),
	}
}
