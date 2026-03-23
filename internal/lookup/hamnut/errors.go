package hamnut

import (
	"errors"
	"net"
	"net/url"
	"syscall"
)

// IsNetworkError checks if the error is network-related
func IsNetworkError(err error) bool {
	if err == nil {
		return false
	}

	// Check for net.Error interface (includes timeout, temporary errors)
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	// Check for DNS errors
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	// Check for connection refused, network unreachable, etc.
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}

	// Check for URL errors (which wrap network errors)
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return IsNetworkError(urlErr.Err)
	}

	// Check for specific syscall errors
	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.EHOSTUNREACH) {
		return true
	}

	return false
}
