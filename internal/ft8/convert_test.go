package ft8

import "testing"

func TestFloatToInt16(t *testing.T) {
	cases := []struct {
		name string
		in   float32
		want int16
	}{
		{"zero", 0, 0},
		{"full positive", 1.0, 32767},
		{"full negative", -1.0, -32767},
		{"half positive", 0.5, 16383},
		{"half negative", -0.5, -16383},
		{"clamp over +1", 1.5, 32767},
		{"clamp under -1", -2.0, -32768},
		{"tiny positive", 0.00003, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := floatToInt16(c.in); got != c.want {
				t.Errorf("floatToInt16(%v) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}
