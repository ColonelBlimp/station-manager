// Package stub provides a test-only Forwarder implementation whose outcome
// is configurable via the credentials blob. It exists so that the
// forwarding subsystem's plumbing (ingest → queue → claim → submit →
// persist outcome → pull endpoint) can be exercised end-to-end without
// depending on a real upstream service.
//
// Credentials shape (all fields optional unless noted):
//
//	{"mode": "always_success"}              // default when credentials omitted
//	{"mode": "always_transient"}            // exercises retry path
//	{"mode": "always_terminal"}             // exercises terminal-fail path
//	{"mode": "flap_n", "flap_n": 3}         // first 3 Submits transient, then success
//
// Registers under type "stub" via init(). Import the package with
// `_ "github.com/ColonelBlimp/station-manager/internal/forwarding/stub"`
// from cmd/smd (or a test) to make it available to the registry.
package stub

import (
	"context"
	"encoding/json"
	stderr "errors"
	"fmt"
	"sync/atomic"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Type is the registry identifier for this forwarder.
const Type = "stub"

// Mode constants select the stub's Submit behavior. Set via the `mode`
// field in the forwarder's credentials blob.
const (
	ModeAlwaysSuccess   = "always_success"
	ModeAlwaysTransient = "always_transient"
	ModeAlwaysTerminal  = "always_terminal"
	ModeFlapN           = "flap_n"
)

// DefaultRetry is the retry policy the daemon uses when a stub
// forwarder config entry has no explicit `retry` block. Tight
// values — the stub exists for plumbing verification, not real
// retries; tests that want to exercise backoff set Config.Retry
// directly.
var DefaultRetry = types.RetryConfig{
	MaxAttempts:       3,
	InitialBackoffSec: 1,
	MaxBackoffSec:     5,
}

func init() {
	forwarding.Register(Type, New)
	forwarding.RegisterDefaultRetry(Type, DefaultRetry)
}

// credentials is the type-specific shape of ForwarderConfig.Credentials
// for this forwarder. Extra fields are ignored.
type credentials struct {
	Mode  string `json:"mode"`
	FlapN int64  `json:"flap_n"`
}

// Forwarder implements forwarding.Forwarder with behavior determined by
// its configured mode. Safe for concurrent use; the only mutable state
// is a single atomic call counter.
type Forwarder struct {
	mode  string
	flapN int64
	calls atomic.Int64
}

// New constructs a stub Forwarder from a ForwarderConfig. Credentials
// are optional — an empty blob defaults to mode=always_success, which
// makes the stub useful as "just make the plumbing work" out of the
// box.
func New(fc types.ForwarderConfig) (forwarding.Forwarder, error) {
	const op errors.Op = "stub.New"

	creds := credentials{Mode: ModeAlwaysSuccess}
	if len(fc.Credentials) > 0 {
		if err := json.Unmarshal(fc.Credentials, &creds); err != nil {
			return nil, errors.New(op).WithErr(err).WithMsg("parse credentials")
		}
		if creds.Mode == "" {
			creds.Mode = ModeAlwaysSuccess
		}
	}

	switch creds.Mode {
	case ModeAlwaysSuccess, ModeAlwaysTransient, ModeAlwaysTerminal:
		// ok
	case ModeFlapN:
		if creds.FlapN < 1 {
			return nil, errors.New(op).WithMsgf("flap_n must be >= 1 for mode=flap_n, got %d", creds.FlapN)
		}
	default:
		return nil, errors.New(op).WithMsgf(
			"unknown mode %q (valid: always_success, always_transient, always_terminal, flap_n)",
			creds.Mode,
		)
	}

	return &Forwarder{mode: creds.Mode, flapN: creds.FlapN}, nil
}

// Type returns the registry identifier for this forwarder.
func (f *Forwarder) Type() string { return Type }

// AdifPrefix returns "" — the stub has no ADIF upload-status slot.
// The worker will skip the qso-row stamp for stub-backed uploads.
func (f *Forwarder) AdifPrefix() string { return "" }

// Submit honours ctx cancellation, increments the call counter, and
// returns a Result per the configured mode. Ctx cancellation returns
// a transient outcome without consuming a call slot so tests can
// reason about the counter cleanly.
//
// priorUpstreamID is ignored — the stub doesn't use it.
func (f *Forwarder) Submit(ctx context.Context, _ types.Qso, _ forwarding.Action, _ string) forwarding.Result {
	if err := ctx.Err(); err != nil {
		return forwarding.Result{Outcome: forwarding.OutcomeTransient, Err: err}
	}

	n := f.calls.Add(1)

	switch f.mode {
	case ModeAlwaysSuccess:
		return forwarding.Result{Outcome: forwarding.OutcomeSuccess, UpstreamID: "stub-ok"}
	case ModeAlwaysTransient:
		return forwarding.Result{
			Outcome: forwarding.OutcomeTransient,
			Err:     stderr.New("stub: always_transient"),
		}
	case ModeAlwaysTerminal:
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err:     stderr.New("stub: always_terminal"),
		}
	case ModeFlapN:
		if n <= f.flapN {
			return forwarding.Result{
				Outcome: forwarding.OutcomeTransient,
				Err:     fmt.Errorf("stub: flapping attempt %d/%d", n, f.flapN),
			}
		}
		return forwarding.Result{Outcome: forwarding.OutcomeSuccess, UpstreamID: "stub-ok"}
	}

	// New validates mode, so reaching here is a programming bug. Surface
	// as terminal so the caller doesn't loop on it.
	return forwarding.Result{
		Outcome: forwarding.OutcomeTerminal,
		Err:     fmt.Errorf("stub: unreachable mode %q", f.mode),
	}
}

// Calls returns the number of times Submit has done real work (i.e.
// not short-circuited on a cancelled ctx). Useful for test assertions.
func (f *Forwarder) Calls() int64 {
	return f.calls.Load()
}
