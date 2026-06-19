package audio

// itoa is a tiny stdlib-free int formatter for FFT-size panic
// messages. Avoids pulling strconv just for one panic. Shared by
// the pure-Go complex (fft.go) and real (realfft.go) Plan
// implementations.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
