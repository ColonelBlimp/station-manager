package ft8

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// decodeLogFileName is the default ALL.TXT basename, written under the working
// dir's log/ directory (next to smd.log) when the operator sets no explicit path.
const decodeLogFileName = "ft8-all.txt"

// DecodeLog is a fail-soft, append-only JTDX-style ALL.TXT writer for FT8 RX
// decodes and our own TX. It is deliberately independent of the daemon's zerolog
// level: the per-decode log line is gated off at the default info level (it's a
// 12–16×/slot firehose), so an operator who wants a durable decode record turns
// THIS on instead of running the whole daemon at debug.
//
// Every method is safe on a nil *DecodeLog (no-op), so callers needn't branch on
// "is logging enabled" — a disabled or failed-to-open log is just a nil pointer.
// Writes and Close are mutually exclusive under mu, and a write after Close is a
// no-op, so a TX write racing capture release can at worst drop one line, never
// touch a closed file. Each write Flushes so a crash keeps the record up to the
// last decoded slot.
type DecodeLog struct {
	mu     sync.Mutex
	w      *bufio.Writer
	f      *os.File
	closed bool
	log    logging.Logger
}

// resolveDecodeLogPath returns the operator's explicit path, or the default
// $SM_WORKING_DIR/log/ft8-all.txt. A working-dir resolution failure falls back to
// the bare filename in the current directory — openDecodeLog then surfaces any
// real open error fail-soft.
func resolveDecodeLogPath(path string, log logging.Logger) string {
	if p := strings.TrimSpace(path); p != "" {
		return p
	}
	wd, err := utils.WorkingDir()
	if err != nil {
		log.WarnWith().Err(err).Msg("ft8: decode log working-dir resolution failed; using current dir")
		return decodeLogFileName
	}
	return filepath.Join(wd, "log", decodeLogFileName)
}

// openDecodeLog opens (creating + appending to) the decode-log file. Fail-soft: on
// any error it logs a warning and returns nil, so the subsystem keeps decoding
// without a log — consistent with the FT8 "fails soft, never crashes" invariant.
func openDecodeLog(path string, log logging.Logger) *DecodeLog {
	if log == nil {
		log = logging.Noop()
	}
	p := resolveDecodeLogPath(path, log)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		log.WarnWith().Err(err).Str("path", p).Msg("ft8: decode log dir create failed; decode log off")
		return nil
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.WarnWith().Err(err).Str("path", p).Msg("ft8: decode log open failed; decode log off")
		return nil
	}
	log.InfoWith().Str("path", p).Msg("ft8: decode log on (JTDX ALL.TXT format)")
	return &DecodeLog{w: bufio.NewWriter(f), f: f, log: log}
}

// WriteRx appends one JTDX RX line per decode of the slot that started at
// slotStart, matching the receive format other ops' logs use:
//
//	20260618_140830 -7 0.3 2752 ~ JM1ISX 7Q5MLV -15
//
// i.e. "<YYYYMMDD_HHMMSS> <snr> <dt> <freqHz> ~ <message>" (slot time UTC, signed
// SNR, DT to 0.1 s, audio offset rounded to whole Hz). No dial frequency on RX —
// the receive line carries none, only the audio offset within the passband.
func (d *DecodeLog) WriteRx(slotStart time.Time, msgs []goft8.DecodedMessage) {
	if d == nil || len(msgs) == 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	ts := slotStart.UTC().Format("20060102_150405")
	for _, m := range msgs {
		fmt.Fprintf(d.w, "%s %d %.1f %d ~ %s\n", ts, m.SNR, m.DTSec, int(m.FreqHz+0.5), m.Text)
	}
	if err := d.w.Flush(); err != nil {
		d.warnFlush(err)
	}
}

// WriteTx appends one JTDX TX line for a transmission we started at t, matching the
// transmit format in other ops' logs:
//
//	20260618_140845.104 Transmitting 14.074 MHz + 2997Hz FT8: 7Q5MLV JM1ISX R-07
//
// i.e. "<YYYYMMDD_HHMMSS.fff> Transmitting <dial> MHz + <offset>Hz FT8: <message>".
// dialMHz <= 0 (a manual transmit with no session dial) omits the dial clause. t is
// stamped when the transmission is committed; for a sequencer rung (current-slot)
// that is within ~1 s of the on-air key, for a manual next-slot transmit it can be
// up to a slot early — close enough for the diagnostic record.
func (d *DecodeLog) WriteTx(t time.Time, dialMHz, offsetHz float64, message string) {
	if d == nil || message == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	ts := t.UTC().Format("20060102_150405.000")
	off := int(offsetHz + 0.5)
	if dialMHz > 0 {
		fmt.Fprintf(d.w, "%s Transmitting %.3f MHz + %dHz FT8: %s\n", ts, dialMHz, off, message)
	} else {
		fmt.Fprintf(d.w, "%s Transmitting %dHz FT8: %s\n", ts, off, message)
	}
	if err := d.w.Flush(); err != nil {
		d.warnFlush(err)
	}
}

// warnFlush reports a write/flush failure once-ish (best-effort): a full disk or
// a vanished mount shouldn't crash FT8, so we log and carry on. Caller holds mu.
func (d *DecodeLog) warnFlush(err error) {
	d.log.WarnWith().Err(err).Msg("ft8: decode log write failed")
}

// Close flushes and closes the file. Idempotent and nil-safe; after Close every
// write is a no-op.
func (d *DecodeLog) Close() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	d.closed = true
	_ = d.w.Flush()
	_ = d.f.Close()
}
