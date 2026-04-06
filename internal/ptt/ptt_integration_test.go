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

func TestPTT_Assert_Integration(t *testing.T) {
	p, err := Open(Config{PortName: testPort(), Line: LineRTS})
	require.NoError(t, err)
	defer p.Close()

	require.NoError(t, p.Assert())
	require.True(t, p.IsActive())

	// Hold TX briefly so it can be observed on a meter or another device.
	time.Sleep(100 * time.Millisecond)

	require.NoError(t, p.Release())
	require.False(t, p.IsActive())
}

func TestPTT_DTR_Integration(t *testing.T) {
	p, err := Open(Config{PortName: testPort(), Line: LineDTR})
	require.NoError(t, err)
	defer p.Close()

	require.NoError(t, p.Assert())
	require.True(t, p.IsActive())

	time.Sleep(100 * time.Millisecond)

	require.NoError(t, p.Release())
	require.False(t, p.IsActive())
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
