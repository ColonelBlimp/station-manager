package serial

import (
	"bytes"
	"context"
	stderr "errors"
	"io/fs"
	"sync"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"go.bug.st/serial"
)

const (
	// responsesBufSize controls the capacity of the responses channel used to
	// deliver framed lines from the background reader loop to callers.
	responsesBufSize = 64

	// maxLineSize is the maximum number of bytes buffered for a single framed
	// line before it is dropped (the reader keeps running for subsequent
	// well-formed lines).
	maxLineSize = 4096

	// defaultBufSize is the reader loop's per-Read buffer size.
	defaultBufSize = 512
)

// Client is the high-level interface for sending CAT commands and
// receiving responses over a serial port. It is safe for concurrent use
// by multiple goroutines *for writes* via WriteCommandBytes; all writes are
// serialized internally. Reads are delivered on a single background
// reader goroutine and must be consumed by at most one goroutine at a
// time via ReadResponseBytes/ExecBytes.
type Client interface {
	// WriteCommandBytes writes a single CAT command as an opaque byte
	// slice to the port. Implementations will append the configured line
	// delimiter if it is not already present as the final byte.
	//
	// WriteCommandBytes is safe to call concurrently from multiple
	// goroutines; the implementation will serialize writes on the
	// underlying port.
	WriteCommandBytes(ctx context.Context, cmd []byte) error

	// ReadResponseBytes reads a single response line terminated by the
	// configured delimiter and returns the raw bytes excluding the
	// delimiter.
	//
	// ReadResponseBytes is not safe to call concurrently from multiple
	// goroutines on the same Client.
	ReadResponseBytes(ctx context.Context) ([]byte, error)

	// ExecBytes is a convenience that writes a command as bytes then reads
	// one response as bytes.
	//
	// Like ReadResponseBytes, ExecBytes must not be invoked concurrently
	// by multiple goroutines on the same Client.
	//
	// ExecBytes returns the next framed line, with NO correlation to the
	// command just written. It is only meaningful against a rig speaking
	// strictly request/response: if the rig is in AUTO/Transceive mode, an
	// unsolicited push already buffered will be returned as "the response".
	// The bridge never uses ExecBytes for this reason (it owns its own read loop).
	ExecBytes(ctx context.Context, cmd []byte) ([]byte, error)

	// Close closes the underlying port. It is safe to call multiple times.
	Close() error
}

// Port is the concrete implementation of Client backed by go.bug.st/serial.
//
// Port implements the same concurrency guarantees as Client: it permits
// multiple concurrent calls to WriteCommand/WriteCommandBytes, which are
// serialized on the underlying SerialPort, but requires that
// ReadResponse/ReadResponseBytes and Exec/ExecBytes are used from at most
// one goroutine at a time.
type Port struct {
	port SerialPort

	cfg Config

	// writeTimeout bounds a single WriteCommandBytes (0 = unbounded). Derived
	// from Config.WriteTimeoutMS at construction. See WriteCommandBytes.
	writeTimeout time.Duration

	writeMu sync.Mutex

	responses chan []byte
	closeCh   chan struct{}
	doneCh    chan struct{}

	closed bool
	// termErr holds the terminal reader-loop cause (EIO, ENODEV, cable yank)
	// so ReadResponseBytes returns the real reason instead of a bare ErrClosed
	// once the reader dies. It is set once, before the reader closes responses;
	// nil for a graceful Close. Guarded by mu.
	termErr error
	mu      sync.RWMutex
}

// classifyOpenError maps a serial.Open failure to one of the package's Open
// sentinels (errors.go) when it recognises the cause, else nil. Keeping the
// go.bug.st PortError-code and ENOENT knowledge here contains the driver/OS
// detail in the package that owns it, rather than leaking it to every caller.
func classifyOpenError(err error) error {
	if pe, ok := stderr.AsType[*serial.PortError](err); ok {
		switch pe.Code() {
		case serial.PermissionDenied:
			return ErrPermissionDenied
		case serial.PortBusy:
			return ErrPortBusy
		case serial.PortNotFound:
			return ErrPortNotFound
		default:
			// A PortError we don't map to an actionable sentinel — let the
			// caller fall back to the generic open-failed message.
			return nil
		}
	}
	// go.bug.st maps only EBUSY/EACCES to a PortError on Linux; a missing
	// device path (rig off / cable out) surfaces as a raw ENOENT, not a
	// PortError, so it's matched here rather than in the switch above.
	if stderr.Is(err, fs.ErrNotExist) {
		return ErrPortNotFound
	}
	return nil
}

// Open initializes and opens a serial port based on the given Config. It returns
// a Port or an error if unsuccessful. Recognised failure causes are wrapped as
// one of the errors.Is-matchable Open sentinels (ErrPermissionDenied,
// ErrPortBusy, ErrPortNotFound) so callers can render an actionable message.
func Open(cfg Config) (*Port, error) {
	const op errors.Op = "serial.Open"

	ncfg, err := validateConfig(cfg)
	if err != nil {
		return nil, errors.New(op).WithErr(err)
	}

	mode := &serial.Mode{
		BaudRate: ncfg.BaudRate,
		DataBits: ncfg.DataBits,
		StopBits: ncfg.StopBits,
		Parity:   ncfg.Parity,
	}

	// Set the initial DTR/RTS line state via InitialStatusBits when the config
	// specifies it. go.bug.st/serial asserts both when InitialStatusBits is nil;
	// for a rig whose PTT can be mapped to a control line (Icom USB SEND),
	// leaving them asserted would key the transmitter, so we de-assert. The
	// baseline is the library default (both true); a non-nil field overrides just
	// that line.
	//
	// IMPORTANT (review 2026-06-19 H1): this does NOT guarantee a pulse-free open
	// on Unix. go.bug.st/serial documents that Linux/macOS cannot set modem
	// output bits BEFORE the port is opened — InitialStatusBits is applied just
	// after open, so a de-asserted line can still go true for a few ms. On a rig
	// with USB SEND mapped to a control line that brief pulse can key TX. The real
	// mitigation is the rig-side setting (e.g. IC-7300 USB SEND = OFF); callers
	// warn via rigserial.OpenMayPulseLines. Only Windows can set the bits before
	// open.
	if ncfg.RTS != nil || ncfg.DTR != nil {
		bits := &serial.ModemOutputBits{RTS: true, DTR: true}
		if ncfg.RTS != nil {
			bits.RTS = *ncfg.RTS
		}
		if ncfg.DTR != nil {
			bits.DTR = *ncfg.DTR
		}
		mode.InitialStatusBits = bits
	}

	p, err := serial.Open(ncfg.PortName, mode)
	if err != nil {
		if sentinel := classifyOpenError(err); sentinel != nil {
			// Wrap the recognised cause so the bridge renders an actionable
			// message (join dialout / close the other program / power on the
			// rig) instead of the raw OS/driver error.
			return nil, errors.New(op).WithErr(sentinel)
		}
		return nil, errors.New(op).WithErr(err)
	}

	if ncfg.ReadTimeoutMS > 0 {
		if err = p.SetReadTimeout(time.Duration(ncfg.ReadTimeoutMS) * time.Millisecond); err != nil {
			// Close the port we just opened so the OS handle isn't leaked when
			// we return without handing the caller a *Port to Close themselves.
			_ = p.Close()
			return nil, errors.New(op).WithErr(err)
		}
	}

	sp := &bugstPort{Port: p}
	cl := newPort(sp, ncfg)
	return cl, nil
}

// newPort constructs a Port around an existing SerialPort.
func newPort(sp SerialPort, cfg Config) *Port {
	if cfg.LineDelimiter == 0 {
		cfg.LineDelimiter = '\r' // Default line delimiter, if not provided
	}

	po := &Port{
		port:         sp,
		cfg:          cfg,
		writeTimeout: time.Duration(cfg.WriteTimeoutMS) * time.Millisecond,
		responses:    make(chan []byte, responsesBufSize),
		closeCh:      make(chan struct{}),
		doneCh:       make(chan struct{}),
	}

	go po.readerLoop()

	return po
}

// WriteCommandBytes implements the byte-oriented write for Client.
func (p *Port) WriteCommandBytes(ctx context.Context, cmd []byte) error {
	const op errors.Op = "serial.WriteCommandBytes"

	p.mu.RLock()
	closed := p.closed
	p.mu.RUnlock()
	if closed {
		return errors.New(op).WithErr(ErrClosed)
	}

	if len(cmd) == 0 {
		return nil
	}

	// ensure delimiter; use a 3-index slice to prevent appending into the
	// caller's backing array if their slice has spare capacity.
	if cmd[len(cmd)-1] != p.cfg.LineDelimiter {
		cmd = append(cmd[:len(cmd):len(cmd)], p.cfg.LineDelimiter)
	}

	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	if p.writeTimeout <= 0 {
		// No watchdog: write directly (historical behaviour — blocks until the
		// OS write completes or errors).
		return p.writeAll(ctx, cmd)
	}

	// Watchdog path. go.bug.st/serial has no write deadline (only
	// SetReadTimeout), so a blocking port.Write on a driver/HW fault can't be
	// interrupted by ctx — the loop checks ctx only between Write calls. Run the
	// write off-goroutine; if it overruns writeTimeout, close the port. Closing
	// is a best-effort unblock: when the fault itself errors the write (USB
	// removal does), the goroutine unwinds and reports on the buffered `done`
	// channel; on a truly wedged driver, close(fd) does not interrupt a thread
	// already in write(2), so worst case one bounded goroutine parks until the
	// driver gives up (never a deadlock — Write holds no lock Close needs, and
	// the buffered channel means the parked goroutine leaks nothing else). Either
	// way the caller gets ErrWriteTimeout on time and the now-closed port makes
	// the bridge's supervisor tear down and reopen, releasing any active tune.
	// This bounds WriteCommandBytes so a hung write can never wedge writeMu (and
	// with it the tune guaranteed-stop) forever (review 2026-06-04 H4).
	done := make(chan error, 1)
	go func() { done <- p.writeAll(ctx, cmd) }()

	t := time.NewTimer(p.writeTimeout)
	defer t.Stop()
	select {
	case err := <-done:
		return err
	case <-t.C:
		// Close is idempotent and does not take writeMu, so calling it here is
		// safe. p.closed flips true, so any concurrent/subsequent writer short-
		// circuits to ErrClosed rather than racing the unwinding goroutine.
		_ = p.Close()
		return errors.New(op).WithErr(ErrWriteTimeout)
	}
}

// writeAll writes the full command to the port, looping over partial writes
// and honouring ctx between writes. It assumes writeMu is held by the caller
// (directly in the unbounded path, or via the single watchdog goroutine).
func (p *Port) writeAll(ctx context.Context, cmd []byte) error {
	const op errors.Op = "serial.WriteCommandBytes"

	written := 0
	for written < len(cmd) {
		select {
		case <-ctx.Done():
			return errors.New(op).WithErr(ctx.Err())
		default:
		}

		n, err := p.port.Write(cmd[written:])
		if err != nil {
			return errors.New(op).WithErr(err)
		}
		if n == 0 {
			// Protect against misbehaving SerialPort implementations that
			// report success but do not advance the write offset, which
			// would otherwise cause this loop to spin indefinitely.
			return errors.New(op).WithMsg("serial: write returned 0 bytes without error")
		}
		written += n
	}

	return nil
}

// ReadResponseBytes implements the byte-oriented read for Client.
func (p *Port) ReadResponseBytes(ctx context.Context) ([]byte, error) {
	const op errors.Op = "serial.ReadResponseBytes"

	p.mu.RLock()
	closed := p.closed
	p.mu.RUnlock()
	if closed {
		return nil, errors.New(op).WithErr(ErrClosed)
	}

	select {
	case <-ctx.Done():
		return nil, errors.New(op).WithErr(ctx.Err())
	case line, ok := <-p.responses:
		if !ok {
			// responses closed: either a graceful Close (termErr nil → ErrClosed)
			// or the reader died with a terminal cause (return it, wrapped, so
			// the bridge logs "EIO"/"no such device" instead of "port closed").
			p.mu.RLock()
			te := p.termErr
			p.mu.RUnlock()
			if te != nil {
				return nil, errors.New(op).WithErr(te)
			}
			return nil, errors.New(op).WithErr(ErrClosed)
		}
		return line, nil
	}
}

// ExecBytes implements the byte-oriented Exec for Client.
func (p *Port) ExecBytes(ctx context.Context, cmd []byte) ([]byte, error) {
	const op errors.Op = "serial.ExecBytes"

	if err := p.WriteCommandBytes(ctx, cmd); err != nil {
		return nil, errors.New(op).WithErr(err)
	}
	return p.ReadResponseBytes(ctx)
}

// setTermErr records the reader loop's terminal cause once, before the reader
// closes responses, so ReadResponseBytes can hand it to the blocked reader.
func (p *Port) setTermErr(err error) {
	p.mu.Lock()
	if p.termErr == nil {
		p.termErr = err
	}
	p.mu.Unlock()
}

// Close implements Client.
func (p *Port) Close() error {
	const op errors.Op = "serial.Close"

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.closeCh)
	p.mu.Unlock()

	// Close the underlying port first to unblock any in-flight Read calls.
	closeErr := p.port.Close()

	// Always wait for the reader loop to finish cleanup, even if the port
	// close failed, to prevent goroutine leaks.
	<-p.doneCh

	if closeErr != nil {
		return errors.New(op).WithErr(closeErr)
	}
	return nil
}

// readerLoop continuously reads from the serial port and emits
// complete lines onto the response channel.
func (p *Port) readerLoop() {
	defer close(p.doneCh)
	defer close(p.responses)

	// One buffer for this Port's whole lifetime (allocated once per reader
	// goroutine); no pool — reconnects are rare, so pooling this saved nothing.
	buf := make([]byte, defaultBufSize)

	var lineBuf []byte
	// discarding is set when the current line exceeds maxLineSize.
	// While true, incoming bytes are skipped until the next delimiter
	// so that the tail of the oversized line is not emitted as a
	// spurious partial response.
	discarding := false

	for {
		select {
		case <-p.closeCh:
			return
		default:
			// No-op
		}

		n, err := p.port.Read(buf)
		if err != nil {
			// Treat timeout-like errors as recoverable and keep looping.
			var to interface{ Timeout() bool }
			if stderr.As(err, &to) && to.Timeout() {
				continue
			}

			// Don't surface errors caused by a graceful Close.
			select {
			case <-p.closeCh:
				return
			default:
			}

			// Non-timeout terminal error: record the real cause so the reader
			// blocked on ReadResponseBytes gets it (EIO, ENODEV, cable yank)
			// instead of a bare ErrClosed, then exit — the deferred
			// close(responses) unblocks that reader.
			p.setTermErr(errors.New(errors.Op("serial.readerLoop")).WithErr(err))
			return
		}
		if n == 0 {
			continue
		}

		chunk := buf[:n]
		for len(chunk) > 0 {
			idx := bytes.IndexByte(chunk, p.cfg.LineDelimiter)
			if idx == -1 {
				if discarding {
					// Still inside an oversized line; skip the
					// entire chunk.
					break
				}
				lineBuf = append(lineBuf, chunk...)
				if len(lineBuf) > maxLineSize {
					// Drop the overly long line and keep running (recoverable).
					// This deliberately touches nothing that could hide a later
					// terminal read error — it just resets the frame buffer and
					// skips to the next delimiter (review 2026-06-19 M1).
					lineBuf = lineBuf[:0]
					discarding = true
				}
				break
			}

			if discarding {
				// Found the delimiter that ends the oversized line.
				// Discard everything up to and including it, then
				// resume normal framing.
				discarding = false
				chunk = chunk[idx+1:]
				continue
			}

			lineBuf = append(lineBuf, chunk[:idx]...)
			// emit line
			// copy to avoid retaining the entire backing array across sends
			lineCopy := make([]byte, len(lineBuf))
			copy(lineCopy, lineBuf)
			select {
			case p.responses <- lineCopy:
			case <-p.closeCh:
				return
			}
			lineBuf = lineBuf[:0]

			chunk = chunk[idx+1:]
		}
	}
}
