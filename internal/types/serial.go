package types

import (
	"go.bug.st/serial"
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
	ReadTimeoutMS  int // Milliseconds
	WriteTimeoutMS int // Milliseconds
	RTS            bool
	DTR            bool
	LineDelimiter  byte // If not provided, the default is '\r'.
}
