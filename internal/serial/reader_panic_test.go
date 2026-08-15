package serial

import (
	"testing"
	"time"
)

// panickingReadPort is a SerialPort whose Read panics, to drive the reader
// goroutine's panic path. It embeds mockPort for Write/Close/the rest.
type panickingReadPort struct {
	*mockPort
}

func (p *panickingReadPort) Read([]byte) (int, error) {
	panic("boom: serial reader exploded")
}

// L9 — a panic in the serial reader goroutine must be recovered and reported via the
// injected PanicHandler (this package has no logger of its own), then LOG-AND-DIE:
// the reader closes its done channel on the way out so the caller sees the port end
// and its liveness/reopen recovers it. respawn=false — respawning here would race the
// higher layer's reopen.
func TestSerialReader_PanicIsHandledAndReaderDies(t *testing.T) {
	handled := make(chan string, 1)
	handler := func(name string, _ any, _ []byte) {
		select {
		case handled <- name:
		default:
		}
	}

	po := newPort(&panickingReadPort{mockPort: newMockPort()}, Config{PanicHandler: handler})
	t.Cleanup(func() { _ = po.Close() })

	// The reader read → panicked → was recovered and reported (attributed).
	select {
	case name := <-handled:
		if name != "serial.reader" {
			t.Errorf("panic handler name = %q, want serial.reader", name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("panic handler not called — a serial reader panic must be reported, not crash silently")
	}
	// And the reader DIED (log-and-die): doneCh closes on the way out.
	select {
	case <-po.doneCh:
	case <-time.After(time.Second):
		t.Fatal("reader did not die (doneCh not closed) after the panic")
	}
}
