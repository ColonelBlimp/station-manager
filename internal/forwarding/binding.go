package forwarding

import (
	"encoding/json"
	"fmt"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// BoundForwarder is one destination binding resolved against its station
// account (ADR 0082 parts 5–6): the synthesized ForwarderConfig the existing
// constructor, worker and enqueue path consume — named by the binding's
// forwarder_name — and the logical logbook it serves.
type BoundForwarder struct {
	LogbookID int64
	Config    types.ForwarderConfig
}

// BindingFault names a binding that could not be resolved (no station account
// for its destination, a malformed credential blob…). The daemon logs it and
// starts no worker for it; its queue rows are left as they are.
type BindingFault struct {
	ForwarderName string
	Destination   string
	LogbookID     int64
	Err           error
}

// BindingConfig synthesizes the ForwarderConfig for one binding: the account's
// STATION-scoped credential keys and the binding's LOGBOOK-scoped keys — each
// store contributes only what its scope owns, so a key of the wrong scope can
// never cross ownership — plus the account's transport, cadence, retry,
// endpoints, label and action filter, and the binding's name and enabled
// state. A credential blob that is not a JSON object is refused — never read
// as "no credentials". The error text names fields, never values.
func BindingConfig(b types.LogbookDestination, account types.ForwarderConfig) (types.ForwarderConfig, error) {
	if b.ForwarderName == "" {
		return types.ForwarderConfig{}, fmt.Errorf("binding for %s on logbook %d has no forwarder_name", b.Destination, b.LogbookID)
	}
	if account.Type != b.Destination {
		return types.ForwarderConfig{}, fmt.Errorf("binding %q is %s but the station account %q is %s", b.ForwarderName, b.Destination, account.Name, account.Type)
	}
	if _, ok := DescriptorFor(b.Destination); !ok {
		return types.ForwarderConfig{}, fmt.Errorf("binding %q: destination type %q has no descriptor in this build", b.ForwarderName, b.Destination)
	}
	stationKeys, err := credentialObject(account.Credentials)
	if err != nil {
		return types.ForwarderConfig{}, fmt.Errorf("station account %q: credentials %w", account.Name, err)
	}
	logbookKeys, err := credentialObject(b.Credentials)
	if err != nil {
		return types.ForwarderConfig{}, fmt.Errorf("binding %q: credentials %w", b.ForwarderName, err)
	}
	// Each store contributes ONLY the keys its scope owns, as the descriptor
	// declares them: a binding cannot carry a station key (an SM Cloud binding
	// with `url` cannot redirect the station's token elsewhere), an account
	// cannot carry a logbook key, and a key the type never declared is dropped.
	desc, _ := DescriptorFor(b.Destination)
	merged := make(map[string]json.RawMessage, len(desc.CredentialFields))
	for _, f := range desc.CredentialFields {
		switch f.Scope {
		case ScopeStation:
			if v, ok := stationKeys[f.Key]; ok {
				merged[f.Key] = v
			}
		case ScopeLogbook:
			if v, ok := logbookKeys[f.Key]; ok {
				merged[f.Key] = v
			}
		}
	}
	fc := account
	fc.Name = b.ForwarderName
	fc.Type = b.Destination
	fc.Enabled = b.Enabled
	fc.Credentials = nil
	if len(merged) > 0 {
		raw, err := json.Marshal(merged)
		if err != nil {
			return types.ForwarderConfig{}, fmt.Errorf("binding %q: marshal credentials: %w", b.ForwarderName, err)
		}
		fc.Credentials = raw
	}
	return fc, nil
}

// credentialObject decodes a credentials blob as a JSON object; empty or null
// is no credentials, anything else that is not an object is an error.
func credentialObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("are not a JSON object")
	}
	return obj, nil
}

// ResolveBindings pairs every binding with the station account of its
// destination. Bindings that resolve become routes, in input order; the rest
// are faults, named — a binding is never silently dropped.
func ResolveBindings(bindings []types.LogbookDestination, accounts []types.ForwarderConfig) ([]BoundForwarder, []BindingFault) {
	byType := make(map[string]types.ForwarderConfig, len(accounts))
	for _, a := range accounts {
		byType[a.Type] = a
	}
	var routes []BoundForwarder
	var faults []BindingFault
	for _, b := range bindings {
		account, ok := byType[b.Destination]
		if !ok {
			faults = append(faults, BindingFault{ForwarderName: b.ForwarderName, Destination: b.Destination, LogbookID: b.LogbookID,
				Err: fmt.Errorf("no station account of type %q in config.forwarders", b.Destination)})
			continue
		}
		fc, err := BindingConfig(b, account)
		if err != nil {
			faults = append(faults, BindingFault{ForwarderName: b.ForwarderName, Destination: b.Destination, LogbookID: b.LogbookID, Err: err})
			continue
		}
		routes = append(routes, BoundForwarder{LogbookID: b.LogbookID, Config: fc})
	}
	return routes, faults
}
