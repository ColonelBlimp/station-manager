package hardware

import (
	"testing"

	"go.bug.st/serial/enumerator"
)

func TestSerialLabel(t *testing.T) {
	cases := []struct {
		name string
		d    *enumerator.PortDetails
		want string
	}{
		{
			name: "product present is disambiguated by path",
			d:    &enumerator.PortDetails{Name: "/dev/ttyUSB0", Product: "CP2105 Dual UART"},
			want: "CP2105 Dual UART (/dev/ttyUSB0)",
		},
		{
			name: "no product falls back to VID:PID + serial (review L2)",
			d: &enumerator.PortDetails{
				Name: "/dev/ttyUSB1", VID: "10c4", PID: "ea60", SerialNumber: "0001",
			},
			want: "USB 10c4:ea60 #0001 (/dev/ttyUSB1)",
		},
		{
			name: "no product, VID:PID without serial",
			d:    &enumerator.PortDetails{Name: "/dev/ttyUSB2", VID: "0403", PID: "6001"},
			want: "USB 0403:6001 (/dev/ttyUSB2)",
		},
		{
			name: "no product, only a serial number",
			d:    &enumerator.PortDetails{Name: "/dev/ttyUSB3", SerialNumber: "A50285BI"},
			want: "USB #A50285BI (/dev/ttyUSB3)",
		},
		{
			name: "no usb metadata at all falls back to the bare path",
			d:    &enumerator.PortDetails{Name: "/dev/ttyS0"},
			want: "/dev/ttyS0",
		},
	}
	for _, c := range cases {
		if got := serialLabel(c.d); got != c.want {
			t.Errorf("%s: serialLabel = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestSerialPorts_NoError pins that enumeration succeeds on the host (pure-Go,
// all builds) and yields well-formed, sorted entries. The exact set is
// host-dependent, so the assertions are shape-only: no error, IDs non-empty
// and ascending, Label always populated.
func TestSerialPorts_NoError(t *testing.T) {
	ports, err := SerialPorts()
	if err != nil {
		t.Fatalf("SerialPorts: unexpected error: %v", err)
	}
	var prev string
	for i, p := range ports {
		if p.ID == "" {
			t.Errorf("port %d: empty ID", i)
		}
		if p.Label == "" {
			t.Errorf("port %d (%s): empty Label", i, p.ID)
		}
		if i > 0 && p.ID < prev {
			t.Errorf("ports not sorted by ID: %q before %q", prev, p.ID)
		}
		prev = p.ID
	}
}
