// Package qrz provides the QRZ.com forwarder. It pushes QSOs
// to QRZ via the HTTP Logbook API and classifies upstream responses
// into Success / Transient / Terminal outcomes so the worker can
// retry, mark failed, or stamp success as appropriate.
//
// Credentials shape (only api_key is required):
//
//	{"api_key": "XXXX-XXXX-XXXX-XXXX"}
//
// Each QRZ logbook has a unique api_key that both authenticates the
// caller and identifies which logbook the QSO lands in. Each logbook
// is also keyed by a callsign, and QRZ rejects any QSO whose
// STATION_CALLSIGN doesn't match the logbook's callsign — but that
// is enforced by QRZ, not locally: the response parser classifies the
// rejection as terminal and the worker marks the row failed with
// QRZ's error message in last_error. Keeping a copy of the callsign
// in config would only introduce drift risk (stale config could
// reject correct QSOs) without adding a correctness guarantee.
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
package qrz

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
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

// DefaultEndpoint is the QRZ Logbook API URL. Overridable in tests via
// the package-internal constructor so httptest.NewServer fixtures can
// stand in for the real service.
const DefaultEndpoint = "https://logbook.qrz.com/api"

// DefaultHTTPTimeout is the per-call timeout for QRZ requests. QRZ is
// known to be slow on occasion, and the operator's internet link is
// flaky (see project_sm_operator_network memory), so the 30s balances
// "give the server time to respond" against "don't let a hung socket
// hold up the worker indefinitely".
const DefaultHTTPTimeout = 30 * time.Second

// UserAgent is the User-Agent header this forwarder sends. QRZ's
// developer guide requires a UA of ≤128 chars; the default embeds
// the daemon version once cmd/smd overrides this var at startup
// (stage 8). Until then the "dev" fallback keeps tests hermetic.
var UserAgent = "station-manager/dev"

// DefaultRetry is the retry policy the daemon uses when a QRZ
// forwarder config entry has no explicit `retry` block. Tuned for
// QRZ's web API: transient 5xx/429/network blips recover within
// minutes, and the operator's link is slow/unreliable so we don't
// hammer the server. Five attempts spans ~60s → ~1800s of backoff,
// a reasonable window before giving up and marking the row failed.
var DefaultRetry = types.RetryConfig{
	MaxAttempts:       5,
	InitialBackoffSec: 60,
	MaxBackoffSec:     1800,
}

func init() {
	forwarding.Register(Type, New)
	forwarding.RegisterDefaultRetry(Type, DefaultRetry)
}

// credentials is the type-specific shape of ForwarderConfig.Credentials
// for QRZ. Extra fields are ignored.
type credentials struct {
	APIKey string `json:"api_key"`
}

// Forwarder implements forwarding.Forwarder by POSTing to the QRZ
// Logbook API. Fields are set at construction and read-only thereafter,
// so it's safe for concurrent use, as the worker may call Submit from
// multiple goroutines if a future batch size > 1 is wired up.
type Forwarder struct {
	apiKey   string
	endpoint string
	client   *http.Client
}

// New constructs a QRZ Forwarder from the given ForwarderConfig. Only
// api_key is required — it both authenticates the caller and selects
// which logbook the QSO lands in.
func New(fc types.ForwarderConfig) (forwarding.Forwarder, error) {
	const op errors.Op = "qrz.New"

	if len(fc.Credentials) == 0 {
		return nil, errors.New(op).WithMsg("credentials required (api_key)")
	}

	var creds credentials
	if err := json.Unmarshal(fc.Credentials, &creds); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("parse credentials")
	}
	if creds.APIKey == "" {
		return nil, errors.New(op).WithMsg("credentials.api_key is required")
	}

	return newWithEndpoint(creds.APIKey, DefaultEndpoint, &http.Client{
		Timeout: DefaultHTTPTimeout,
	}), nil
}

// newWithEndpoint is the package-internal constructor that tests use
// to point the forwarder at an httptest.NewServer URL with an
// arbitrary http.Client. Production code goes through New, which
// wraps this with the real QRZ endpoint and a default-timeout client.
func newWithEndpoint(apiKey, endpoint string, client *http.Client) *Forwarder {
	if client == nil {
		client = &http.Client{Timeout: DefaultHTTPTimeout}
	}
	return &Forwarder{
		apiKey:   apiKey,
		endpoint: endpoint,
		client:   client,
	}
}

// Type returns the registry identifier for this forwarder.
func (f *Forwarder) Type() string { return Type }

// AdifPrefix returns "QRZCOM" so the worker stamps
// QRZCOM_QSO_UPLOAD_STATUS / QRZCOM_QSO_UPLOAD_DATE on the QSO row
// after a successful insert or update.
func (f *Forwarder) AdifPrefix() string { return AdifFieldPrefix }

// Submit pushes one QSO+action pair to QRZ. The shape of the call
// depends on the action:
//
//	insert   ACTION=INSERT                                ADIF=<record>
//	update   ACTION=INSERT  OPTION=REPLACE                ADIF=<record>
//	delete   ACTION=DELETE  LOGIDS=<priorUpstreamID>
//
// For delete, priorUpstreamID must be the LOGID QRZ returned on the
// earlier successful insert; the worker fetches this from the
// qso_upload history before calling Submit. An empty priorUpstreamID
// here is a caller bug — surfaced as Terminal rather than sent to
// QRZ as a malformed delete.
//
// Outcome classification has two layers:
//   - transport (this function): ctx cancel, network error, or
//     non-2xx HTTP status → Transient or Terminal depending on
//     status class.
//   - response body (classifyResponse in response.go): RESULT=…
//     interpretation per the per-action matrix in §8.1 of the
//     implementation guide.
func (f *Forwarder) Submit(
	ctx context.Context,
	qso types.Qso,
	act forwarding.Action,
	priorUpstreamID string,
) forwarding.Result {
	const op errors.Op = "qrz.Submit"

	if err := ctx.Err(); err != nil {
		return forwarding.Result{Outcome: forwarding.OutcomeTransient, Err: err}
	}

	form, err := buildForm(f.apiKey, qso, act, priorUpstreamID)
	if err != nil {
		// A build-time error (unknown action, empty priorUpstreamID on
		// delete, adif conversion failure) is not something retries
		// can heal.
		return forwarding.Result{Outcome: forwarding.OutcomeTerminal, Err: err}
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, f.endpoint, strings.NewReader(form.Encode()),
	)
	if err != nil {
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err:     errors.New(op).WithErr(err).WithMsg("build HTTP request"),
		}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", UserAgent)

	resp, err := f.client.Do(req)
	if err != nil {
		// net errors (DNS, connection refused, tls, timeout, ctx cancel
		// mid-flight) are inherently retryable from our perspective.
		return forwarding.Result{
			Outcome: forwarding.OutcomeTransient,
			Err:     errors.New(op).WithErr(err).WithMsg("POST to QRZ"),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		// Partial body read is a transient transport fault.
		return forwarding.Result{
			Outcome: forwarding.OutcomeTransient,
			Err:     errors.New(op).WithErr(err).WithMsg("read QRZ response"),
		}
	}

	if tr, ok := classifyHTTPStatus(resp.StatusCode, body); ok {
		return tr
	}

	parsed, err := parseResponse(body)
	if err != nil {
		// Malformed body on a 2xx is a protocol violation by the upstream;
		// retrying won't fix it.
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err:     errors.New(op).WithErr(err).WithMsg("parse QRZ response"),
		}
	}

	return classifyResponse(act, parsed)
}

// buildForm assembles the x-www-form-urlencoded request body for the
// given action. Insert and update share ACTION=INSERT; update adds
// OPTION=REPLACE. Delete uses ACTION=DELETE + LOGIDS=priorUpstreamID,
// where priorUpstreamID is the LOGID QRZ returned on the earlier
// successful insert — resolved by the worker via
// sqlite.Service.FetchInsertUpstreamIDWithContext before Submit.
func buildForm(apiKey string, qso types.Qso, act forwarding.Action, priorUpstreamID string) (url.Values, error) {
	const op errors.Op = "qrz.buildForm"

	form := url.Values{}
	form.Set("KEY", apiKey)

	switch act {
	case action.Insert:
		form.Set("ACTION", "INSERT")
		form.Set("ADIF", adif.ConvertQsoToAdifNoHeader(qso))

	case action.Update:
		form.Set("ACTION", "INSERT")
		form.Set("OPTION", "REPLACE")
		form.Set("ADIF", adif.ConvertQsoToAdifNoHeader(qso))

	case action.Delete:
		// priorUpstreamID is the LOGID captured on the earlier successful
		// insert, resolved by the worker via
		// sqlite.Service.FetchInsertUpstreamIDWithContext before Submit.
		// Empty here is a caller bug — the worker should have
		// short-circuited with a terminal result before we got this far.
		if priorUpstreamID == "" {
			return nil, errors.New(op).WithMsg("delete requires non-empty priorUpstreamID (worker lookup failed)")
		}
		form.Set("ACTION", "DELETE")
		form.Set("LOGIDS", priorUpstreamID)

	default:
		return nil, errors.New(op).WithMsgf("unknown action %q", act)
	}

	return form, nil
}

// classifyHTTPStatus maps transport-level HTTP outcomes (non-2xx)
// into forwarding.Result. Returns (_, false) when the status is in
// the 2xx range and the caller should parse the body instead.
//
// Rules:
//
//	2xx                         → (zero, false) — caller parses body
//	408 Request Timeout         → Transient
//	429 Too Many Requests       → Transient
//	5xx                         → Transient (server-side, may recover)
//	other 4xx                   → Terminal (bad request, etc.)
//	other (1xx, 3xx unhandled)  → Terminal (unexpected for this API)
//
// The response body is included in the error message (truncated) so
// operators can diagnose without re-running the call.
func classifyHTTPStatus(status int, body []byte) (forwarding.Result, bool) {
	const op errors.Op = "qrz.classifyHTTPStatus"

	if status >= 200 && status < 300 {
		return forwarding.Result{}, false
	}

	snippet := bodySnippet(body, 200)

	switch {
	case status == http.StatusRequestTimeout,
		status == http.StatusTooManyRequests,
		status >= 500 && status < 600:
		return forwarding.Result{
			Outcome: forwarding.OutcomeTransient,
			Err: errors.New(op).WithMsgf(
				"QRZ returned HTTP %d (body: %s)", status, snippet,
			),
		}, true
	default:
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err: errors.New(op).WithMsgf(
				"QRZ returned HTTP %d (body: %s)", status, snippet,
			),
		}, true
	}
}

// bodySnippet returns a bounded view of the response body for error
// messages — full bodies can be large and are not useful in
// last_error, which is meant to fit in a log line.
func bodySnippet(body []byte, max int) string {
	s := strings.TrimSpace(string(body))
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
