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

	// The Open sentinels below classify the common, operator-actionable
	// failure modes of Open so callers can render a specific fix rather than
	// leaking the raw OS/driver error. Open wraps one of these (errors.Is-
	// matchable) in place of the underlying error; the go.bug.st/serial
	// PortError-code detail stays inside this package, its owner.

	// ErrPermissionDenied — the OS denied access to the device (EACCES),
	// typically the daemon's user is not in the 'dialout' group.
	ErrPermissionDenied = errors.New("permission denied opening serial port")

	// ErrPortBusy — the device is already open in another process (EBUSY):
	// another logger, WSJT-X, a second Station Manager, minicom, etc.
	ErrPortBusy = errors.New("serial port is busy (in use by another process)")

	// ErrPortNotFound — the device path does not exist (ENOENT): the rig is
	// powered off or the USB cable is unplugged, so /dev/ttyUSB* never appeared.
	ErrPortNotFound = errors.New("serial port not found")
)
