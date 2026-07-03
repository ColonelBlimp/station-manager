package serial

import (
	"github.com/ColonelBlimp/station-manager/internal/errors"

	"go.bug.st/serial"
)

// Config is the parameters for opening a serial port. It is typically
// supplied by the caller as a field of a larger rig configuration; see
// docs/v2-design/cat-serial-reuse.md §3c for how rig-level configuration
// composes this type.
//
// Zero values for DataBits, StopBits, Parity, and LineDelimiter are
// replaced with sensible defaults at Open time. PortName and BaudRate
// are required.
type Config struct {
	// PortName is the device path (e.g. "/dev/ttyUSB0"). Required.
	PortName string `json:"port_name"`

	// BaudRate in bits per second (e.g. 9600, 38400). Required; must be > 0.
	BaudRate int `json:"baud_rate"`

	// DataBits per frame. Defaults to 8 if zero.
	DataBits int `json:"data_bits"`

	// StopBits per frame. Defaults to serial.OneStopBit if zero.
	StopBits serial.StopBits `json:"stop_bits"`

	// Parity mode. Defaults to serial.NoParity if zero.
	Parity serial.Parity `json:"parity"`

	// LineDelimiter is the byte that terminates framed lines on reads and
	// is auto-appended to writes when missing. Defaults to '\r' if zero.
	// Most CAT rigs use ';'.
	LineDelimiter byte `json:"line_delimiter"`

	// ReadTimeoutMS is the per-read timeout applied to the underlying
	// serial port. Zero or negative disables the timeout (blocking reads).
	ReadTimeoutMS int `json:"read_timeout_ms"`

	// WriteTimeoutMS bounds a single WriteCommandBytes call. go.bug.st/serial
	// exposes no write deadline (only SetReadTimeout), so a blocking port.Write
	// on a driver/HW fault cannot be interrupted by context. When positive,
	// WriteCommandBytes runs the write under a watchdog: if it does not complete
	// in time the port is CLOSED to unblock the stuck syscall and the call
	// returns ErrWriteTimeout. This is a fault backstop, not a per-write SLA —
	// set it generously (seconds), well above any legitimate CAT-write latency,
	// so it only ever fires on a genuine hang. Zero or negative disables the
	// watchdog (writes block indefinitely — the historical behaviour).
	WriteTimeoutMS int `json:"write_timeout_ms"`

	// RTS and DTR set the initial state of the corresponding modem output
	// lines at open. nil leaves go.bug.st/serial's default (both asserted /
	// true) — the historical behaviour and correct for USB-CDC rigs where the
	// lines aren't flow control. A non-nil false DE-ASSERTS the line: required
	// for Icom CI-V, where the rig's USB SEND function can map PTT to RTS or
	// DTR, so a port opened with the line asserted would key the transmitter
	// (ADR 0034 bench finding). Applied via serial.Mode.InitialStatusBits.
	//
	// CAVEAT (review 2026-06-19 H1): on Unix this is NOT a pulse-free guarantee.
	// go.bug.st/serial cannot set modem bits before opening the port on
	// Linux/macOS, so a de-asserted line can momentarily assert (a few ms) right
	// after open — enough to key a control-line-PTT rig. The dependable fix is
	// the rig-side setting (IC-7300 USB SEND = OFF); see rigserial.OpenMayPulseLines.
	RTS *bool `json:"rts,omitempty"`
	DTR *bool `json:"dtr,omitempty"`
}

// validateConfig checks the configuration for obvious issues and returns a
// normalized copy with sensible defaults applied.
func validateConfig(cfg Config) (Config, error) {
	const op errors.Op = "serial.validateConfig"
	if cfg.PortName == "" {
		return cfg, errors.New(op).WithMsg("serial: missing port name")
	}
	if cfg.BaudRate <= 0 {
		return cfg, errors.New(op).WithMsgf("serial: invalid baud rate %d", cfg.BaudRate)
	}
	if cfg.DataBits == 0 {
		cfg.DataBits = 8
	}
	// StopBits and Parity intentionally have no default block: go.bug.st's
	// OneStopBit and NoParity are both the zero value, so "cfg.X == 0 → set to
	// the zero value" would be a no-op. The zero value is the desired default.
	return cfg, nil
}
