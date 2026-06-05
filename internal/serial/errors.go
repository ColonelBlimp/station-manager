package serial

import "errors"

var (
	ErrClosed = errors.New("serial: port closed")

	// ErrWriteTimeout is returned by WriteCommandBytes when a write does not
	// complete within Config.WriteTimeoutMS. The port is closed as a side
	// effect (the only way to unblock a stuck write, since the underlying
	// library has no write deadline), so the caller should treat the port as
	// dead — in the bridge, the supervisor reopens it.
	ErrWriteTimeout = errors.New("serial: write timeout; port closed")
)
