package ft8

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/stretchr/testify/require"
)

// readLines returns the non-empty lines of the decode-log file at path.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func TestDecodeLog_RxFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ft8-all.txt")
	dl := openDecodeLog(path, logging.Noop())
	require.NotNil(t, dl)
	defer dl.Close()

	// Slot at 14:08:30 UTC; one decode. Matches the JTDX RX line in an op's ALL.TXT:
	//   20260618_140830 -7 0.3 2752 ~ JM1ISX 7Q5MLV -15
	slot := time.Date(2026, 6, 18, 14, 8, 30, 0, time.UTC)
	dl.WriteRx(slot, []goft8.DecodedMessage{
		{Text: "JM1ISX 7Q5MLV -15", SNR: -7, DTSec: 0.3, FreqHz: 2752},
	})

	lines := readLines(t, path)
	require.Len(t, lines, 1)
	require.Equal(t, "20260618_140830 -7 0.3 2752 ~ JM1ISX 7Q5MLV -15", lines[0])
}

func TestDecodeLog_RxRoundsFreqAndKeepsSign(t *testing.T) {
	path := filepath.Join(t.TempDir(), "all.txt")
	dl := openDecodeLog(path, logging.Noop())
	require.NotNil(t, dl)
	defer dl.Close()

	slot := time.Date(2026, 1, 2, 3, 4, 15, 0, time.UTC)
	dl.WriteRx(slot, []goft8.DecodedMessage{
		{Text: "CQ K1ABC FN42", SNR: 5, DTSec: -0.2, FreqHz: 992.75}, // +5, neg DT, frac Hz
	})
	lines := readLines(t, path)
	require.Len(t, lines, 1)
	require.Equal(t, "20260102_030415 5 -0.2 993 ~ CQ K1ABC FN42", lines[0])
}

func TestDecodeLog_TxFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "all.txt")
	dl := openDecodeLog(path, logging.Noop())
	require.NotNil(t, dl)
	defer dl.Close()

	// 14:08:45.104 UTC, dial 14.074 MHz, offset 2997 Hz. Matches the JTDX TX line:
	//   20260618_140845.104 Transmitting 14.074 MHz + 2997Hz FT8: 7Q5MLV JM1ISX R-07
	tx := time.Date(2026, 6, 18, 14, 8, 45, 104_000_000, time.UTC)
	dl.WriteTx(tx, 14.074, 2997, "7Q5MLV JM1ISX R-07")

	lines := readLines(t, path)
	require.Len(t, lines, 1)
	require.Equal(t, "20260618_140845.104 Transmitting 14.074 MHz + 2997Hz FT8: 7Q5MLV JM1ISX R-07", lines[0])
}

func TestDecodeLog_TxOmitsDialWhenUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "all.txt")
	dl := openDecodeLog(path, logging.Noop())
	require.NotNil(t, dl)
	defer dl.Close()

	// Manual transmit (dial 0): the band clause is omitted, offset still recorded.
	tx := time.Date(2026, 6, 18, 14, 8, 45, 0, time.UTC)
	dl.WriteTx(tx, 0, 1500, "CQ 7Q5MLV KH78")

	lines := readLines(t, path)
	require.Len(t, lines, 1)
	require.Equal(t, "20260618_140845.000 Transmitting 1500Hz FT8: CQ 7Q5MLV KH78", lines[0])
}

func TestDecodeLog_AppendsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "all.txt")
	slot := time.Date(2026, 6, 18, 14, 8, 30, 0, time.UTC)

	dl1 := openDecodeLog(path, logging.Noop())
	dl1.WriteRx(slot, []goft8.DecodedMessage{{Text: "CQ A B", SNR: 1, DTSec: 0.1, FreqHz: 1000}})
	dl1.Close()

	// A second session (a later capture-acquire) must append, not truncate.
	dl2 := openDecodeLog(path, logging.Noop())
	dl2.WriteRx(slot, []goft8.DecodedMessage{{Text: "CQ C D", SNR: 2, DTSec: 0.2, FreqHz: 2000}})
	dl2.Close()

	lines := readLines(t, path)
	require.Len(t, lines, 2)
	require.Contains(t, lines[0], "CQ A B")
	require.Contains(t, lines[1], "CQ C D")
}

func TestDecodeLog_NilAndClosedAreNoOps(t *testing.T) {
	// All methods must tolerate a nil receiver (the disabled-log case).
	var dl *DecodeLog
	require.NotPanics(t, func() {
		dl.WriteRx(time.Now(), []goft8.DecodedMessage{{Text: "x"}})
		dl.WriteTx(time.Now(), 14.074, 1500, "y")
		dl.Close()
	})

	// A write after Close is dropped, not a panic / not written.
	path := filepath.Join(t.TempDir(), "all.txt")
	real := openDecodeLog(path, logging.Noop())
	require.NotNil(t, real)
	real.Close()
	real.Close() // idempotent
	real.WriteRx(time.Now(), []goft8.DecodedMessage{{Text: "after close", SNR: 0, DTSec: 0, FreqHz: 0}})
	require.Empty(t, readLines(t, path))
}

func TestDecodeLog_EmptyDecodesWriteNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "all.txt")
	dl := openDecodeLog(path, logging.Noop())
	require.NotNil(t, dl)
	defer dl.Close()
	dl.WriteRx(time.Now(), nil)           // no decodes this slot
	dl.WriteTx(time.Now(), 14.074, 0, "") // empty message
	require.Empty(t, readLines(t, path))
}

func TestOpenDecodeLog_FailSoftReturnsNil(t *testing.T) {
	// A path whose parent is a FILE (not a directory) can't be created → fail-soft.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
	dl := openDecodeLog(filepath.Join(blocker, "nested", "all.txt"), logging.Noop())
	require.Nil(t, dl) // open failed, but no panic — the caller gets a nil no-op writer
}
