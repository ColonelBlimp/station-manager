package types

import (
	"go.bug.st/serial"
	"time"
)

type SerialConfig struct {
	PortName string
	BaudRate int
	DataBits int
	Parity   serial.Parity
	StopBits serial.StopBits

	// The serial drivers' read timeout. The unit is milliseconds.
	//
	// Default is 200ms.
	ReadTimeoutMS  time.Duration
	WriteTimeoutMS time.Duration // Milliseconds
	RTS            bool
	DTR            bool
	LineDelimiter  byte // If not provided, the default is '\r'.
}
