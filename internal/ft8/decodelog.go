package ft8

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// decodeLogFileName is the default ALL.TXT basename, written under the working
// dir's log/ directory (next to smd.log) when the operator sets no explicit path.
const decodeLogFileName = "ft8-all.txt"

// decodeLogQueue bounds the writer's pending-line buffer. A slot produces ~16 RX
// lines + a TX line every 15 s, so this holds many slots' worth — a backed-up disk
// has to stall for a long time before the queue fills, at which point lines are
// DROPPED rather than blocking the decode/TX path (see the "never blocks FT8"
// contract). Sized generously because a string queue is cheap.
const decodeLogQueue = 1024

// Rotation for the decode log. Unlike smd.log these are NOT operator-configurable —
// the file is a diagnostic stream with no tuning value, and the defaults already hold
// roughly a month of continuous operating (it grows ~2 MB/day while the FT8 view is
// open). Rotation matters because the log is append-only and previously unbounded:
// WSJT-X's ALL.TXT behaviour, left for the operator to clear by hand.
const (
	decodeLogMaxSizeMB  = 10
	decodeLogMaxBackups = 5
)

// DecodeLog is a fail-soft, append-only JTDX-style ALL.TXT writer for FT8 RX
// decodes and our own TX. It is deliberately independent of the daemon's zerolog
// level: the per-decode log line is gated off at the default info level (it's a
// 12–16×/slot firehose), so an operator who wants a durable decode record turns
// THIS on instead of running the whole daemon at debug.
//
// All disk I/O happens on a single dedicated goroutine: WriteRx/WriteTx only
// format a line and do a NON-BLOCKING enqueue (dropping on a full queue), so a
// slow/full/network-backed log path can never stall the decode goroutine, the
// occupancy/sequencer driving, or current-slot TX timing — the "FT8 fails soft,
// never blocks" invariant. Every method is safe on a nil *DecodeLog (no-op), so
// callers needn't branch on "is logging enabled".
type DecodeLog struct {
	lines     chan string
	quit      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	closed    atomic.Bool
	dropped   atomic.Uint64
	// nextDropWarn is the dropped-total at which the next live loss warning
	// fires (zero value catches the first drop). See enqueue.
	nextDropWarn atomic.Uint64

	// w/wc are touched ONLY by the writer goroutine (run), so they need no lock.
	// wc is the rotating sink (lumberjack), which owns size/backup/gzip policy.
	w   *bufio.Writer
	wc  io.WriteCloser
	log logging.Logger
}

// resolveDecodeLogPath returns the operator's explicit path, or the default
// <defaultDir>/log/ft8-all.txt. defaultDir is the daemon's RESOLVED working dir
// (cfgSvc.WorkingDir(), which honours --config / data_dir) so the decode log lands
// next to smd.log. When defaultDir is empty (a path not wired through the daemon —
// e.g. a unit test), it falls back to utils.WorkingDir(), then to the bare filename.
func resolveDecodeLogPath(path, defaultDir string, log logging.Logger) string {
	if p := strings.TrimSpace(path); p != "" {
		return p
	}
	dir := defaultDir
	if dir == "" {
		wd, err := utils.WorkingDir()
		if err != nil {
			log.WarnWith().Err(err).Msg("ft8: decode log working-dir resolution failed; using current dir")
			return decodeLogFileName
		}
		dir = wd
	}
	return filepath.Join(dir, "log", decodeLogFileName)
}

// openDecodeLog opens (creating + appending to) the decode-log file and starts its
// writer goroutine. Fail-soft: on any error it logs a warning and returns nil, so
// the subsystem keeps decoding without a log — consistent with the FT8 "fails soft,
// never crashes" invariant. defaultDir is the daemon's resolved working dir (see
// resolveDecodeLogPath).
func openDecodeLog(path, defaultDir string, log logging.Logger) *DecodeLog {
	if log == nil {
		log = logging.Noop()
	}
	p := resolveDecodeLogPath(path, defaultDir, log)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		log.WarnWith().Err(err).Str("path", p).Msg("ft8: decode log dir create failed; decode log off")
		return nil
	}
	// lumberjack opens lazily on first Write, so prove the target is writable NOW —
	// otherwise a bad path or permission surfaces only once decodes start flowing.
	// This also CREATES the file at 0600 when absent, which is the mode lumberjack
	// then copies onto every rotated file.
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		log.WarnWith().Err(err).Str("path", p).Msg("ft8: decode log open failed; decode log off")
		return nil
	}
	_ = f.Close()
	// Tighten a log left behind by an earlier build (it created 0644). lumberjack
	// copies the EXISTING file's mode on rotation, so without this an old install
	// would keep propagating the wider mode forever. Best-effort: a chmod failure is
	// not a reason to stop logging decodes.
	if err := os.Chmod(p, 0o600); err != nil {
		log.WarnWith().Err(err).Str("path", p).Msg("ft8: could not tighten decode log permissions")
	}
	wc := &lumberjack.Logger{
		Filename:   p,
		MaxSize:    decodeLogMaxSizeMB,
		MaxBackups: decodeLogMaxBackups,
		Compress:   true,
	}
	d := &DecodeLog{
		lines: make(chan string, decodeLogQueue),
		quit:  make(chan struct{}),
		done:  make(chan struct{}),
		w:     bufio.NewWriter(wc),
		wc:    wc,
		log:   log,
	}
	go d.run()
	log.InfoWith().Str("path", p).Int("max_size_mb", decodeLogMaxSizeMB).
		Int("max_backups", decodeLogMaxBackups).
		Msg("ft8: decode log on (JTDX ALL.TXT format, rotating + gzipped)")
	return d
}

// run is the sole writer: it drains the line queue to disk, flushing whenever it
// catches up (so lines land promptly when idle, batched under load). It exits when
// Close signals quit, draining any buffered lines first. A recover keeps an I/O
// panic from ever taking the daemon down.
func (d *DecodeLog) run() {
	defer close(d.done)
	defer func() {
		if r := recover(); r != nil {
			d.log.WarnWith().Interface("panic", r).Msg("ft8: decode log writer panicked; stopping")
		}
		// The teardown flush carries everything still buffered at exit — the
		// one flush whose failure is data loss with no retry behind it, so it
		// must not be the silent one (the mid-session flush below already
		// warns). Distinct message: that one is retryable, this one is final.
		if err := d.w.Flush(); err != nil {
			d.log.WarnWith().Err(err).Msg("ft8: decode log final flush failed — buffered lines lost")
		}
		if err := d.wc.Close(); err != nil {
			d.log.WarnWith().Err(err).Msg("ft8: decode log close failed")
		}
	}()
	flush := func() {
		if err := d.w.Flush(); err != nil {
			d.log.WarnWith().Err(err).Msg("ft8: decode log flush failed")
		}
	}
	for {
		select {
		case line := <-d.lines:
			_, _ = d.w.WriteString(line)
			if len(d.lines) == 0 {
				flush()
			}
		case <-d.quit:
			for { // drain whatever is buffered, then exit (defer flushes + closes)
				select {
				case line := <-d.lines:
					_, _ = d.w.WriteString(line)
				default:
					return
				}
			}
		}
	}
}

// enqueue hands a formatted line to the writer goroutine without blocking. A full
// queue (a stalled disk) drops the line and bumps the dropped counter — a dropped
// diagnostic line is always preferable to stalling FT8.
func (d *DecodeLog) enqueue(line string) {
	if d == nil || d.closed.Load() {
		return
	}
	select {
	case d.lines <- line:
	default:
		n := d.dropped.Add(1)
		// Report loss as it happens, not only at Close — the decode log is
		// service-lifetime, so Close means daemon shutdown, hours after the
		// ALL.TXT record went incomplete. Warn on the first drop, then on each
		// doubling of the total (1, 2, 4, …): immediate visibility without
		// handing smd.log the per-slot firehose this queue exists to absorb.
		// A lost CAS just skips one re-warn, which the backoff tolerates.
		if next := d.nextDropWarn.Load(); n >= next && d.nextDropWarn.CompareAndSwap(next, n*2) {
			// Emitted OFF the producer goroutine: enqueue sits on the decode and
			// TX paths, whose non-blocking contract is this queue's whole reason
			// to exist — and the daemon log's write is synchronous, on a file
			// that by default lives NEXT TO the decode log, so the moment a drop
			// happens is exactly the moment that write is likely to stall too
			// (codex P1 on 891a3920). One goroutine per threshold crossing,
			// bounded by the doubling to ≤64 over the daemon's lifetime.
			go func() {
				// A lost diagnostics line must never take the daemon down.
				defer func() { _ = recover() }()
				d.log.WarnWith().Uint64("dropped", n).
					Msg("ft8: decode log dropping lines (queue full / slow disk)")
			}()
		}
	}
}

// WriteRx enqueues one JTDX RX line per decode of the slot that started at
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
	ts := slotStart.UTC().Format("20060102_150405")
	for _, m := range msgs {
		d.enqueue(fmt.Sprintf("%s %d %.1f %d ~ %s\n", ts, m.SNR, m.DTSec, int(m.FreqHz+0.5), m.Text))
	}
}

// WriteTx enqueues one JTDX TX line for a transmission we started at t, matching the
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
	ts := t.UTC().Format("20060102_150405.000")
	off := int(offsetHz + 0.5)
	if dialMHz > 0 {
		d.enqueue(fmt.Sprintf("%s Transmitting %.3f MHz + %dHz FT8: %s\n", ts, dialMHz, off, message))
	} else {
		d.enqueue(fmt.Sprintf("%s Transmitting %dHz FT8: %s\n", ts, off, message))
	}
}

// Close stops the writer goroutine after it drains and flushes the queue, then
// closes the file. Idempotent and nil-safe; after Close every write is a no-op.
func (d *DecodeLog) Close() {
	if d == nil {
		return
	}
	d.closeOnce.Do(func() {
		d.closed.Store(true)
		close(d.quit)
		<-d.done
		if n := d.dropped.Load(); n > 0 {
			d.log.WarnWith().Uint64("dropped", n).Msg("ft8: decode log dropped lines (queue full / slow disk)")
		}
	})
}
