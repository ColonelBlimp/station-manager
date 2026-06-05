package serial

import (
	"bytes"
	"context"
	stderr "errors"
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
	// line before it is dropped and an error is emitted on Errors().
	maxLineSize = 4096
)

// Client is the high-level interface for sending CAT commands and
// receiving responses over a serial port. It is safe for concurrent use
// by multiple goroutines *for writes* via WriteCommand; all writes are
// serialized internally. Reads are delivered on a single background
// reader goroutine and must be consumed by at most one goroutine at a
// time via ReadResponse/Exec.
type Client interface {
	// WriteCommand writes a single CAT command string to the port.
	// Implementations will append the configured line delimiter if missing.
	//
	// WriteCommand is safe to call concurrently from multiple goroutines;
	// the implementation will serialize writes on the underlying port.
	WriteCommand(ctx context.Context, cmd string) error

	// ReadResponse reads a single response line terminated by the
	// configured delimiter and returns it as a string. This is a
	// convenience wrapper over ReadResponseBytes and interprets the
	// response bytes as UTF-8 text without validation.
	//
	// ReadResponse is not safe to call concurrently from multiple
	// goroutines on the same Client. Use a single reader goroutine to
	// consume responses and fan them out if needed.
	ReadResponse(ctx context.Context) (string, error)

	// Exec is a convenience that writes a command then reads one response
	// as a string. It wraps ExecBytes and converts the returned bytes to a
	// string without validating UTF-8.
	//
	// Like ReadResponse, Exec must not be invoked concurrently by multiple
	// goroutines on the same Client.
	Exec(ctx context.Context, cmd string) (string, error)

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
	ExecBytes(ctx context.Context, cmd []byte) ([]byte, error)

	// Errors returns a receive-only channel that will yield at most one
	// terminal error from the reader loop, if any, and is closed when the
	// reader loop exits. Callers should not assume it will always produce
	// a value; a graceful close may result in the channel closing without
	// an error.
	//
	// A typical usage pattern is to run a small supervisor goroutine that
	// watches the channel and triggers a reconnection or shutdown when a
	// non-nil error is received:
	//
	//   go func() {
	//       if err, ok := <-c.Errors(); ok && err != nil {
	//           // log and trigger reconnect
	//       }
	//   }()
	//
	Errors() <-chan error

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

	// errCh carries a single terminal error from the reader loop, if any.
	// It is closed when readerLoop exits.
	errCh chan error

	closed bool
	mu     sync.RWMutex
}

// Open initializes and opens a serial port based on the given Config. It returns a Port or an error if unsuccessful.
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

	p, err := serial.Open(ncfg.PortName, mode)
	if err != nil {
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
		// errCh is buffered by one so the reader loop can report a terminal
		// error without blocking; it is closed when readerLoop exits.
		errCh: make(chan error, 1),
	}

	go po.readerLoop()

	return po
}

// WriteCommand implements Client, delegating to WriteCommandBytes.
func (p *Port) WriteCommand(ctx context.Context, cmd string) error {
	if len(cmd) == 0 {
		return nil
	}
	return p.WriteCommandBytes(ctx, []byte(cmd))
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
	// errors the stuck syscall, so the goroutine unwinds and reports on the
	// buffered channel (no leak); the now-closed port makes the bridge's
	// supervisor tear down and reopen, which also releases any active tune.
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

// ReadResponse implements Client, delegating to ReadResponseBytes and
// converting the returned bytes to a string.
func (p *Port) ReadResponse(ctx context.Context) (string, error) {
	const op errors.Op = "serial.ReadResponse"

	b, err := p.ReadResponseBytes(ctx)
	if err != nil {
		return "", errors.New(op).WithErr(err)
	}
	return string(b), nil
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
			return nil, errors.New(op).WithErr(ErrClosed)
		}
		return line, nil
	}
}

// Exec implements Client, delegating to ExecBytes and converting the
// response bytes to a string.
func (p *Port) Exec(ctx context.Context, cmd string) (string, error) {
	const op errors.Op = "serial.Exec"

	b, err := p.ExecBytes(ctx, []byte(cmd))
	if err != nil {
		return "", errors.New(op).WithErr(err)
	}
	return string(b), nil
}

// ExecBytes implements the byte-oriented Exec for Client.
func (p *Port) ExecBytes(ctx context.Context, cmd []byte) ([]byte, error) {
	const op errors.Op = "serial.ExecBytes"

	if err := p.WriteCommandBytes(ctx, cmd); err != nil {
		return nil, errors.New(op).WithErr(err)
	}
	return p.ReadResponseBytes(ctx)
}

// Errors implements Client.
//
// The returned channel will yield at most one non-timeout error from the
// background reader loop (for example, a permanent I/O error or a dropped
// over-long line) and is then closed when the reader exits. In the case of
// a graceful Close, the channel may close without producing any value.
//
// Callers typically spawn a goroutine to supervise this channel and decide
// whether to log the error, reconnect, or shut down:
//
//	go func() {
//	    if err, ok := <-port.Errors(); ok && err != nil {
//	        // handle terminal reader error
//	    }
//	}()
func (p *Port) Errors() <-chan error {
	return p.errCh
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
	defer close(p.errCh)

	buf := getReadBuf()
	defer putReadBuf(buf)

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

			// Non-timeout error: surface it to callers, then exit.
			select {
			case p.errCh <- errors.New(errors.Op("serial.readerLoop")).WithErr(err):
			default:
			}
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
					// drop overly long lines and notify via Errors() on a
					// best-effort basis without terminating the loop.
					lineBuf = lineBuf[:0]
					discarding = true
					select {
					case p.errCh <- errors.New(errors.Op("serial.readerLoop")).WithMsg("serial: dropped line exceeding maxLineSize (4096 bytes)"):
					default:
					}
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
