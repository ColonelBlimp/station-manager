// Package qrz provides the QRZ.com forwarder. It pushes QSOs
// to QRZ via the HTTP Logbook API and classifies upstream responses
// into Success / Transient / Terminal outcomes so the worker can
// retry, mark failed, or stamp success as appropriate.
//
// Credentials shape (both fields required):
//
//	{"callsign": "M0CMC", "api_key": "XXXX-XXXX-XXXX-XXXX"}
//
// Each QRZ logbook is keyed by a callsign and has its own api_key;
// the api_key alone authenticates API calls, but every QSO submitted
// through the logbook must carry a matching STATION_CALLSIGN or QRZ
// will reject it. The callsign here is therefore a data-integrity
// constraint, not a log-tag — stage 4 validates each QSO against it
// before POSTing.
//
// (QRZ.com's website login — username + password — is a separate
// credential for the user account as a whole; it is not used here
// and operators may own multiple logbooks under a single account.)
//
// Registers under type "qrz" via init(). Import the package with
//
//	_ "github.com/ColonelBlimp/station-manager/internal/forwarding/qrz"
//
// from cmd/smd (or a test) so the registry learns about it.
//
// The Submit implementation lands in stages 3–5; this file only wires
// up the registry and validates credentials.
package qrz

import (
	"context"
	"encoding/json"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Type is the registry identifier for this forwarder.
const Type = "qrz"

// AdifFieldPrefix is the ADIF field-name prefix the worker stamps on
// the QSO row after a successful insert/update. Combined with the
// standard suffixes it produces QRZCOM_QSO_UPLOAD_STATUS and
// QRZCOM_QSO_UPLOAD_DATE, per the ADIF specification.
const AdifFieldPrefix = "QRZCOM"

func init() {
	forwarding.Register(Type, New)
}

// credentials is the type-specific shape of ForwarderConfig.Credentials
// for QRZ. Extra fields are ignored.
type credentials struct {
	Callsign string `json:"callsign"`
	APIKey   string `json:"api_key"`
}

// Forwarder implements forwarding.Forwarder by POSTing to the QRZ
// Logbook API. Fields are set at construction and read-only thereafter,
// so it's safe for concurrent use, as the worker may call Submit from
// multiple goroutines if a future batch size > 1 is wired up.
type Forwarder struct {
	callsign string
	apiKey   string
}

// New constructs a QRZ Forwarder from the given ForwarderConfig. Both
// callsign and api_key are required: the api_key authenticates the
// call, and the callsign identifies which QRZ logbook it targets, so
// stage-4 validation can reject a QSO whose STATION_CALLSIGN doesn't
// match this logbook.
func New(fc types.ForwarderConfig) (forwarding.Forwarder, error) {
	const op errors.Op = "qrz.New"

	if len(fc.Credentials) == 0 {
		return nil, errors.New(op).WithMsg("credentials required (callsign + api_key)")
	}

	var creds credentials
	if err := json.Unmarshal(fc.Credentials, &creds); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("parse credentials")
	}
	if creds.Callsign == "" {
		return nil, errors.New(op).WithMsg("credentials.callsign is required")
	}
	if creds.APIKey == "" {
		return nil, errors.New(op).WithMsg("credentials.api_key is required")
	}

	return &Forwarder{
		callsign: creds.Callsign,
		apiKey:   creds.APIKey,
	}, nil
}

// Type returns the registry identifier for this forwarder.
func (f *Forwarder) Type() string { return Type }

// AdifPrefix returns "QRZCOM" so the worker stamps
// QRZCOM_QSO_UPLOAD_STATUS / QRZCOM_QSO_UPLOAD_DATE on the QSO row
// after a successful insert or update.
func (f *Forwarder) AdifPrefix() string { return AdifFieldPrefix }

// Submit is stubbed until stage 4 lands the real HTTP call. Returning
// terminal here (rather than transient) keeps test fixtures predictable
// if a configured QRZ forwarder is accidentally exercised against this
// skeleton — the row fails once, no retry storm.
func (f *Forwarder) Submit(
	ctx context.Context,
	_ types.Qso,
	_ forwarding.Action,
	_ string,
) forwarding.Result {
	const op errors.Op = "qrz.Submit"
	if err := ctx.Err(); err != nil {
		return forwarding.Result{Outcome: forwarding.OutcomeTransient, Err: err}
	}
	return forwarding.Result{
		Outcome: forwarding.OutcomeTerminal,
		Err:     errors.New(op).WithMsg("QRZ Submit not yet implemented (stage 4)"),
	}
}
