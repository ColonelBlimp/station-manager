// internal/ptt/ptt.go
package ptt

import (
	stderr "errors"
	"sync"
	"sync/atomic"

	"go.bug.st/serial"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// ErrClosed is returned when any operation is attempted after Close.
var ErrClosed = stderr.New("ptt closed")

// Line selects which serial control pin drives PTT.
type Line int

const (
	// LineRTS uses the Request To Send pin (default).
	LineRTS Line = iota
	// LineDTR uses the Data Terminal Ready pin.
	LineDTR
)

// Config holds PTT configuration.
type Config struct {
	// PortName is the serial device, e.g. "/dev/ttyUSB1".
	PortName string
	// Line selects which control pin to toggle; defaults to LineRTS.
	Line Line
	// Logger is optional; nil defaults to a no-op logger.
	Logger logging.Logger
}

// pttPort is the subset of go.bug.st/serial.Port used by PTT.
// Defined as an interface so tests can inject a mock without opening real hardware.
type pttPort interface {
	SetRTS(rts bool) error
	SetDTR(dtr bool) error
	Close() error
}

// PTT controls a push-to-talk line via a serial port control pin.
// Create with Open; do not copy after first use.
//
// Assert/Release are idempotent: repeated calls in the same state return nil.
// Both operations are safe for concurrent use.
type PTT struct {
	config Config

	port pttPort

	// active is true when PTT has been asserted (TX).
	active atomic.Bool
	// closed is set permanently by Close.
	closed atomic.Bool

	// mu serialises port operations and protects the active→port transition.
	mu sync.Mutex
}

// Open opens the named serial port and returns a PTT ready for use.
// The control line is driven low (released) immediately on open.
//
// The baud rate is irrelevant for PTT; the port is opened at 9600/8N1 by convention.
func Open(cfg Config) (*PTT, error) {
	const op errors.Op = "ptt.Open"

	if cfg.Logger == nil {
		cfg.Logger = logging.Noop()
	}

	mode := &serial.Mode{
		BaudRate: 9600,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	port, err := serial.Open(cfg.PortName, mode)
	if err != nil {
		return nil, errors.New(op).Err(err)
	}

	p, err := newPTT(cfg, port)
	if err != nil {
		return nil, errors.New(op).Err(err)
	}
	return p, nil
}

// newPTT is the internal constructor; it accepts any pttPort to allow test injection.
func newPTT(cfg Config, port pttPort) (*PTT, error) {
	const op errors.Op = "ptt.newPTT"

	if cfg.Logger == nil {
		cfg.Logger = logging.Noop()
	}

	p := &PTT{config: cfg, port: port}

	// Ensure the line is low (released) on open regardless of prior hardware state.
	if err := p.setLineLocked(false); err != nil {
		_ = port.Close()
		return nil, errors.New(op).Err(err)
	}

	return p, nil
}

// Assert drives the PTT line high (TX).
// Idempotent: returns nil if already asserted.
// Returns ErrClosed if Close has been called.
func (p *PTT) Assert() error {
	const op errors.Op = "ptt.PTT.Assert"

	if p.closed.Load() {
		return ErrClosed
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed.Load() { // double-check: Close may have run between the pre-lock check and here
		return ErrClosed
	}
	if p.active.Load() {
		return nil // already asserted
	}
	if err := p.setLineLocked(true); err != nil {
		return errors.New(op).Err(err)
	}
	p.active.Store(true)
	return nil
}

// Release drives the PTT line low (RX).
// Idempotent: returns nil if already released.
// Returns ErrClosed if Close has been called.
func (p *PTT) Release() error {
	const op errors.Op = "ptt.PTT.Release"

	if p.closed.Load() {
		return ErrClosed
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed.Load() { // double-check: Close may have run between the pre-lock check and here
		return ErrClosed
	}
	if !p.active.Load() {
		return nil // already released
	}
	if err := p.setLineLocked(false); err != nil {
		return errors.New(op).Err(err)
	}
	p.active.Store(false)
	return nil
}

// IsActive returns true when PTT is currently asserted (TX).
func (p *PTT) IsActive() bool {
	return p.active.Load()
}

// Close releases PTT (if asserted) and closes the serial port.
// Idempotent: safe to call more than once.
func (p *PTT) Close() error {
	const op errors.Op = "ptt.PTT.Close"

	if !p.closed.CompareAndSwap(false, true) {
		return nil // already closed
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.active.Load() {
		if err := p.setLineLocked(false); err != nil {
			p.config.Logger.WarnWith().Err(err).Msg("release PTT on close")
		}
		p.active.Store(false)
	}

	if err := p.port.Close(); err != nil {
		return errors.New(op).Err(err)
	}
	return nil
}

// setLineLocked sets the configured control line.
// Must be called with p.mu held.
func (p *PTT) setLineLocked(active bool) error {
	switch p.config.Line {
	case LineDTR:
		return p.port.SetDTR(active)
	default: // LineRTS
		return p.port.SetRTS(active)
	}
}
