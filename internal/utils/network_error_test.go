package utils

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"
	"time"
)

// mockNetError implements net.Error for testing
type mockNetError struct {
	timeout   bool
	temporary bool
	msg       string
}

func (e *mockNetError) Error() string   { return e.msg }
func (e *mockNetError) Timeout() bool   { return e.timeout }
func (e *mockNetError) Temporary() bool { return e.temporary }

func TestIsNetworkError(t *testing.T) {
	t.Run("returns false for nil error", func(t *testing.T) {
		if IsNetworkError(nil) {
			t.Error("expected false for nil error")
		}
	})

	t.Run("returns false for generic error", func(t *testing.T) {
		err := errors.New("generic error")
		if IsNetworkError(err) {
			t.Error("expected false for generic error")
		}
	})

	t.Run("returns true for timeout net.Error", func(t *testing.T) {
		netErr := &mockNetError{timeout: true, msg: "timeout error"}
		if !IsNetworkError(netErr) {
			t.Error("expected true for timeout net.Error")
		}
	})

	t.Run("returns true for temporary net.Error", func(t *testing.T) {
		netErr := &mockNetError{temporary: true, msg: "temporary error"}
		if !IsNetworkError(netErr) {
			t.Error("expected true for temporary net.Error")
		}
	})

	t.Run("returns false for non-timeout/non-temporary net.Error", func(t *testing.T) {
		netErr := &mockNetError{timeout: false, temporary: false, msg: "other error"}
		if IsNetworkError(netErr) {
			t.Error("expected false for non-timeout/non-temporary net.Error")
		}
	})

	t.Run("returns true for net.OpError with dial operation", func(t *testing.T) {
		opErr := &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: errors.New("connection refused"),
		}
		if !IsNetworkError(opErr) {
			t.Error("expected true for dial operation")
		}
	})

	t.Run("returns true for net.OpError with read operation", func(t *testing.T) {
		opErr := &net.OpError{
			Op:  "read",
			Net: "tcp",
			Err: errors.New("connection reset"),
		}
		if !IsNetworkError(opErr) {
			t.Error("expected true for read operation")
		}
	})

	t.Run("returns true for net.OpError with write operation", func(t *testing.T) {
		opErr := &net.OpError{
			Op:  "write",
			Net: "tcp",
			Err: errors.New("broken pipe"),
		}
		if !IsNetworkError(opErr) {
			t.Error("expected true for write operation")
		}
	})

	t.Run("returns true for net.OpError with accept operation", func(t *testing.T) {
		opErr := &net.OpError{
			Op:  "accept",
			Net: "tcp",
			Err: errors.New("too many open files"),
		}
		if !IsNetworkError(opErr) {
			t.Error("expected true for accept operation")
		}
	})

	t.Run("returns true for net.OpError with listen operation", func(t *testing.T) {
		opErr := &net.OpError{
			Op:  "listen",
			Net: "tcp",
			Err: errors.New("address already in use"),
		}
		if !IsNetworkError(opErr) {
			t.Error("expected true for listen operation")
		}
	})

	t.Run("returns false for net.OpError with non-network operation", func(t *testing.T) {
		opErr := &net.OpError{
			Op:  "other",
			Net: "tcp",
			Err: errors.New("some error"),
		}
		if IsNetworkError(opErr) {
			t.Error("expected false for non-network operation")
		}
	})

	t.Run("returns true for DNS error", func(t *testing.T) {
		dnsErr := &net.DNSError{
			Err:         "no such host",
			Name:        "invalid.example.com",
			IsTimeout:   false,
			IsTemporary: false,
		}
		if !IsNetworkError(dnsErr) {
			t.Error("expected true for DNS error")
		}
	})

	t.Run("returns true for address error", func(t *testing.T) {
		addrErr := &net.AddrError{
			Err:  "invalid address",
			Addr: "invalid:port",
		}
		if !IsNetworkError(addrErr) {
			t.Error("expected true for address error")
		}
	})

	t.Run("returns true for TLS RecordHeaderError", func(t *testing.T) {
		tlsErr := &tls.RecordHeaderError{
			Msg: "bad record MAC",
		}
		if !IsNetworkError(tlsErr) {
			t.Error("expected true for TLS error")
		}
	})

	t.Run("returns true for ECONNREFUSED", func(t *testing.T) {
		if !IsNetworkError(syscall.ECONNREFUSED) {
			t.Error("expected true for ECONNREFUSED")
		}
	})

	t.Run("returns true for ECONNRESET", func(t *testing.T) {
		if !IsNetworkError(syscall.ECONNRESET) {
			t.Error("expected true for ECONNRESET")
		}
	})

	t.Run("returns true for ECONNABORTED", func(t *testing.T) {
		if !IsNetworkError(syscall.ECONNABORTED) {
			t.Error("expected true for ECONNABORTED")
		}
	})

	t.Run("returns true for ETIMEDOUT", func(t *testing.T) {
		if !IsNetworkError(syscall.ETIMEDOUT) {
			t.Error("expected true for ETIMEDOUT")
		}
	})

	t.Run("returns true for ENETUNREACH", func(t *testing.T) {
		if !IsNetworkError(syscall.ENETUNREACH) {
			t.Error("expected true for ENETUNREACH")
		}
	})

	t.Run("returns true for EHOSTUNREACH", func(t *testing.T) {
		if !IsNetworkError(syscall.EHOSTUNREACH) {
			t.Error("expected true for EHOSTUNREACH")
		}
	})

	t.Run("returns true for EHOSTDOWN", func(t *testing.T) {
		if !IsNetworkError(syscall.EHOSTDOWN) {
			t.Error("expected true for EHOSTDOWN")
		}
	})

	t.Run("returns true for EPIPE", func(t *testing.T) {
		if !IsNetworkError(syscall.EPIPE) {
			t.Error("expected true for EPIPE")
		}
	})

	t.Run("returns true for ENETRESET", func(t *testing.T) {
		if !IsNetworkError(syscall.ENETRESET) {
			t.Error("expected true for ENETRESET")
		}
	})

	t.Run("returns true for EADDRINUSE", func(t *testing.T) {
		if !IsNetworkError(syscall.EADDRINUSE) {
			t.Error("expected true for EADDRINUSE")
		}
	})

	t.Run("returns true for EADDRNOTAVAIL", func(t *testing.T) {
		if !IsNetworkError(syscall.EADDRNOTAVAIL) {
			t.Error("expected true for EADDRNOTAVAIL")
		}
	})

	t.Run("returns false for non-network syscall error", func(t *testing.T) {
		if IsNetworkError(syscall.EINVAL) {
			t.Error("expected false for non-network syscall error")
		}
	})

	t.Run("returns true for wrapped network error", func(t *testing.T) {
		netErr := &mockNetError{timeout: true, msg: "timeout"}
		wrapped := fmt.Errorf("operation failed: %w", netErr)
		if !IsNetworkError(wrapped) {
			t.Error("expected true for wrapped network error")
		}
	})

	t.Run("returns true for deeply nested network error", func(t *testing.T) {
		netErr := &mockNetError{timeout: true, msg: "timeout"}
		layer1 := fmt.Errorf("layer 1: %w", netErr)
		layer2 := fmt.Errorf("layer 2: %w", layer1)
		layer3 := fmt.Errorf("layer 3: %w", layer2)

		if !IsNetworkError(layer3) {
			t.Error("expected true for deeply nested network error")
		}
	})

	t.Run("returns false for deeply nested non-network error", func(t *testing.T) {
		baseErr := errors.New("base error")
		layer1 := fmt.Errorf("layer 1: %w", baseErr)
		layer2 := fmt.Errorf("layer 2: %w", layer1)

		if IsNetworkError(layer2) {
			t.Error("expected false for deeply nested non-network error")
		}
	})

	t.Run("returns true for real dial error", func(t *testing.T) {
		// Attempt to connect to a port that's likely closed
		conn, dialErr := net.DialTimeout("tcp", "localhost:1", 10*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			t.Skip("localhost:1 is open, skipping test")
		}

		if !IsNetworkError(dialErr) {
			t.Errorf("expected true for real dial error, got false. Error: %v", dialErr)
		}
	})

	t.Run("returns true for wrapped syscall error", func(t *testing.T) {
		wrapped := fmt.Errorf("connection failed: %w", syscall.ECONNREFUSED)
		if !IsNetworkError(wrapped) {
			t.Error("expected true for wrapped ECONNREFUSED")
		}
	})
}
