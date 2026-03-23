package forwarding

import "github.com/Station-Manager/types"

// Forwarder defines the minimal interface for a QSO forwarder that can be
// registered and resolved via the iocdi container.
//
// Implementations are expected to forward a single QSO to a remote service
// (e.g., QRZ, HamQTH) and indicate whether they are enabled via configuration.
//
// Note: Email forwarding operates on slices and remains a separate concern
// and module. It does not implement this interface.
//
// Typical usage with iocdi:
//
//	type MyService struct {
//	    ForwardSvc forwarding.Forwarder `di.inject:"qrzforwarder"`
//	}
//
// The container will inject a concrete implementation registered with the
// bean ID "qrzforwarder" that implements Forwarder.
//
// The interface is deliberately small to simplify adoption across forwarders.
// Optional lifecycle methods (e.g., Initialize()) can be provided by also
// implementing iocdi.Initializer.
//
//go:generate echo "This file declares the Forwarder interface for DI registration"
type Forwarder interface {
	// Forward uploads or otherwise forwards the provided QSO to the destination
	// service. Implementations should be safe to call after successful
	// initialization (when applicable) and respect any configured timeouts.
	Forward(qso types.Qso, param ...string) error
}
