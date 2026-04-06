//go:build integration

package ptt

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// These tests require a real serial PTT interface.
// Set PTT_TEST_PORT to override the default port:
//
//	PTT_TEST_PORT=/dev/ttyUSB0 go test -tags=integration ./ptt/ -v
//
// Falls back to /dev/ttyUSB1 if the variable is not set.

func testPort() string {
	if p := os.Getenv("PTT_TEST_PORT"); p != "" {
		return p
	}
	return "/dev/ttyUSB1"
}

func TestPTT_Open_Integration(t *testing.T) {
	p, err := Open(Config{PortName: testPort(), Line: LineRTS})
	require.NoError(t, err)
	defer p.Close()

	require.False(t, p.IsActive())
}

// TestPTT_RTS_Integration asserts and holds RTS for 1 second.
// Observe the RTS pin on a multimeter or oscilloscope during this window.
func TestPTT_RTS_Integration(t *testing.T) {
	port := testPort()
	t.Logf("opening %s, asserting RTS for 1s — check pin with meter", port)

	p, err := Open(Config{PortName: port, Line: LineRTS})
	require.NoError(t, err)
	defer p.Close()

	require.NoError(t, p.Assert())
	require.True(t, p.IsActive())
	t.Log("RTS asserted")

	time.Sleep(1 * time.Second)

	require.NoError(t, p.Release())
	require.False(t, p.IsActive())
	t.Log("RTS released")
}

// TestPTT_DTR_Integration asserts and holds DTR for 1 second.
// Observe the DTR pin on a multimeter or oscilloscope during this window.
func TestPTT_DTR_Integration(t *testing.T) {
	port := testPort()
	t.Logf("opening %s, asserting DTR for 1s — check pin with meter", port)

	p, err := Open(Config{PortName: port, Line: LineDTR})
	require.NoError(t, err)
	defer p.Close()

	require.NoError(t, p.Assert())
	require.True(t, p.IsActive())
	t.Log("DTR asserted")

	time.Sleep(1 * time.Second)

	require.NoError(t, p.Release())
	require.False(t, p.IsActive())
	t.Log("DTR released")
}

func TestPTT_Close_ReleasesLine_Integration(t *testing.T) {
	p, err := Open(Config{PortName: testPort(), Line: LineRTS})
	require.NoError(t, err)

	require.NoError(t, p.Assert())
	require.True(t, p.IsActive())

	// Close must drive RTS low before closing the port.
	require.NoError(t, p.Close())
	require.False(t, p.IsActive())
}
