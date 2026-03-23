package utils

import (
	"crypto/tls"
	"errors"
	"net"
	"syscall"
)

// IsNetworkError determines if an error represents a network-related failure
// using type assertions and error checking without string matching.
// This includes timeouts, connection failures, DNS errors, TLS errors,
// and various syscall network errors.
func IsNetworkError(err error) bool {
	if err == nil {
		return false
	}

	// Check for net.Error interface (timeout/temporary)
	var ne net.Error
	if errors.As(err, &ne) {
		if ne.Timeout() || ne.Temporary() {
			return true
		}
	}

	// Check for net.OpError (dial/read/write/accept/listen failures)
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Op == "dial" || opErr.Op == "read" || opErr.Op == "write" ||
			opErr.Op == "accept" || opErr.Op == "listen" {
			return true
		}
	}

	// Check for TLS errors
	var tlsErr *tls.RecordHeaderError
	if errors.As(err, &tlsErr) {
		return true
	}

	// Check for DNS lookup errors
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	// Check for address parsing errors
	var addrErr *net.AddrError
	if errors.As(err, &addrErr) {
		return true
	}

	// Check for syscall network errors using errors.Is
	// Connection errors
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}

	// Network unreachable errors
	if errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.EHOSTDOWN) {
		return true
	}

	// Pipe and broken connection errors
	if errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ENETRESET) {
		return true
	}

	// Address already in use (bind/listen failures)
	if errors.Is(err, syscall.EADDRINUSE) || errors.Is(err, syscall.EADDRNOTAVAIL) {
		return true
	}

	// Recursively check wrapped errors
	if unwrapped := errors.Unwrap(err); unwrapped != nil {
		return IsNetworkError(unwrapped)
	}

	return false
}
