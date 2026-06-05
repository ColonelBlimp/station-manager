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
	if cfg.StopBits == 0 {
		cfg.StopBits = serial.OneStopBit
	}
	if cfg.Parity == 0 {
		cfg.Parity = serial.NoParity
	}
	return cfg, nil
}
