// Package qrzcq provides the QRZCQ logbook forwarder. QRZCQ accepts an ADIF
// log wrapped in JSON at its account API and asks clients to POST no more than
// once per minute. Station Manager uses a deliberately gentler 90-second
// cadence and enforces that interval inside the forwarder as well as in the
// worker defaults, so a hand-edited tick or batch size cannot hammer the API.
//
// Credentials shape:
//
//	{"call":"7Q5MLV","key":"..."}
//
// QRZCQ documents log upload only, so this forwarder registers insert as its
// sole supported action. It has no ADIF-defined upload status/date field and
// therefore stamps only the durable qso_upload row, not the QSO ADIF payload.
package qrzcq

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/securehttp"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

const (
	// Type is the registry identifier used in config.forwarders[].type.
	Type = "qrzcq"

	// DefaultEndpoint is QRZCQ's documented authenticated log-upload URL.
	DefaultEndpoint = "https://ssl.qrzcq.com/api/logupload"

	// DefaultHTTPTimeout bounds a single upload over a slow field link.
	DefaultHTTPTimeout = 30 * time.Second

	// DefaultSubmitInterval is intentionally more conservative than QRZCQ's
	// documented one-request-per-minute ceiling.
	DefaultSubmitInterval = 90 * time.Second

	DefaultTickIntervalSec = 90
	DefaultBatchSize       = 1

	maxResponseBytes = 1 << 20 // 1 MiB; normal responses are tiny JSON objects.
)

// UserAgent is set from the daemon's global configured User-Agent at startup.
var UserAgent = "station-manager/dev"

// accountSubmitPacers makes the upstream cadence an account-level property,
// not a config-entry property. Operators may legitimately give two forwarders
// different names while reusing one QRZCQ account; those instances must not be
// able to open independent request windows.
var accountSubmitPacers = newSubmitPacerRegistry(DefaultSubmitInterval)

// DefaultRetry bounds host-replied temporary failures. A host that cannot be
// reached at all remains queued indefinitely under OutcomeUnreachable.
var DefaultRetry = types.RetryConfig{
	MaxAttempts:       5,
	InitialBackoffSec: 90,
	MaxBackoffSec:     1800,
}

func init() {
	forwarding.Register(Type, New)
	forwarding.RegisterDefaultRetry(Type, DefaultRetry)
	forwarding.RegisterWorkerDefaults(Type, forwarding.WorkerDefaults{
		TickIntervalSec: DefaultTickIntervalSec,
		BatchSize:       DefaultBatchSize,
	})
	forwarding.RegisterDefaultEndpoints(Type, map[string]string{
		action.Insert.String(): DefaultEndpoint,
	})
	forwarding.RegisterForwarderType(Type, "QRZCQ",
		[]forwarding.Action{action.Insert},
		[]forwarding.CredentialField{
			{Key: "call", Label: "QRZCQ callsign", Kind: "text",
				Help: "The callsign of your QRZCQ account."},
			{Key: "key", Label: "API key", Kind: "password",
				Help: "Your QRZCQ account API key."},
		})
}

type credentials struct {
	Call string `json:"call"`
	Key  string `json:"key"`
}

type uploadRequest struct {
	Auth credentials `json:"auth"`
	Data struct {
		ADIF string `json:"adif"`
	} `json:"data"`
}

type uploadResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// Forwarder implements forwarding.Forwarder for QRZCQ's logupload endpoint.
type Forwarder struct {
	call     string
	key      string
	endpoint string
	client   *http.Client
	pacer    *submitPacer
}

// New constructs a QRZCQ forwarder from operator credentials and the optional
// config endpoint override.
func New(fc types.ForwarderConfig) (forwarding.Forwarder, error) {
	const op errors.Op = "qrzcq.New"

	if len(fc.Credentials) == 0 {
		return nil, errors.New(op).WithMsg("credentials required (call, key)")
	}
	var creds credentials
	if err := json.Unmarshal(fc.Credentials, &creds); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("parse credentials")
	}
	creds.Call = strings.TrimSpace(creds.Call)
	creds.Key = strings.TrimSpace(creds.Key)
	if creds.Call == "" {
		return nil, errors.New(op).WithMsg("credentials.call is required")
	}
	if creds.Key == "" {
		return nil, errors.New(op).WithMsg("credentials.key is required")
	}

	endpoint := forwarding.ResolveEndpoint(fc.Endpoints, DefaultEndpoint, action.Insert.String())
	// Credentials travel in the request URL, so the endpoint must be https (http
	// only for a loopback mock). allow_insecure_http is SM-Cloud-only (ST-4a).
	if err := securehttp.CheckCredentialedURL(endpoint, false); err != nil {
		return nil, errors.New(op).WithErr(err).
			WithMsg("endpoint must use https (http allowed only for loopback)")
	}
	fwd := newWithEndpoint(creds.Call, creds.Key, endpoint,
		securehttp.Harden(utils.NewHTTPClient(DefaultHTTPTimeout)), DefaultSubmitInterval)
	fwd.pacer = accountSubmitPacers.ForAccount(creds.Call)
	return fwd, nil
}

func newWithEndpoint(call, key, endpoint string, client *http.Client, interval time.Duration) *Forwarder {
	if client == nil {
		client = utils.NewHTTPClient(DefaultHTTPTimeout)
	}
	return &Forwarder{
		call:     call,
		key:      key,
		endpoint: endpoint,
		client:   client,
		pacer:    &submitPacer{interval: interval},
	}
}

func (f *Forwarder) Type() string       { return Type }
func (f *Forwarder) AdifPrefix() string { return "" }

// Submit uploads one QSO as an ADIF record. QRZCQ documents neither replace
// nor delete semantics, so non-insert actions fail locally without a request.
func (f *Forwarder) Submit(
	ctx context.Context,
	qso types.Qso,
	act forwarding.Action,
	_ string,
) forwarding.Result {
	const op errors.Op = "qrzcq.Submit"

	if err := ctx.Err(); err != nil {
		return forwarding.Result{Outcome: forwarding.OutcomeTransient, Err: err}
	}
	if act != action.Insert {
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err:     errors.New(op).WithMsgf("QRZCQ log upload does not support action %q", act),
		}
	}

	payload := uploadRequest{Auth: credentials{Call: f.call, Key: f.key}}
	payload.Data.ADIF = adif.ConvertQsoToAdifNoHeader(qso)
	body, err := json.Marshal(payload)
	if err != nil {
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err:     errors.New(op).WithErr(err).WithMsg("encode upload request"),
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.endpoint, bytes.NewReader(body))
	if err != nil {
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err:     errors.New(op).WithErr(err).WithMsg("build HTTP request"),
		}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)

	if err := f.pacer.Wait(ctx); err != nil {
		return forwarding.Result{Outcome: forwarding.OutcomeTransient, Err: err}
	}
	resp, err := securehttp.Do(f.client, req)
	if err != nil {
		if ctx.Err() != nil {
			return forwarding.Result{Outcome: forwarding.OutcomeTransient, Err: ctx.Err()}
		}
		return forwarding.Result{
			Outcome: forwarding.OutcomeUnreachable,
			Err:     errors.New(op).WithErr(err).WithMsg("POST to QRZCQ"),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return forwarding.Result{
			Outcome: forwarding.OutcomeTransient,
			Err:     errors.New(op).WithErr(err).WithMsg("read QRZCQ response"),
		}
	}
	if result, handled := classifyHTTPStatus(resp.StatusCode); handled {
		return result
	}

	var decoded uploadResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err:     errors.New(op).WithErr(err).WithMsg("parse QRZCQ response"),
		}
	}
	if !strings.EqualFold(strings.TrimSpace(decoded.Status), "OK") {
		status := redact(decoded.Status, f.key)
		message := redact(decoded.Message, f.key)
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err: errors.New(op).WithMsgf(
				"QRZCQ rejected upload (status=%q message=%q)", status, message),
		}
	}

	return forwarding.Result{Outcome: forwarding.OutcomeSuccess}
}

func classifyHTTPStatus(status int) (forwarding.Result, bool) {
	const op errors.Op = "qrzcq.classifyHTTPStatus"
	if status >= 200 && status < 300 {
		return forwarding.Result{}, false
	}
	err := errors.New(op).WithMsgf("QRZCQ returned HTTP %d", status)
	if status == http.StatusRequestTimeout || status == http.StatusTooManyRequests ||
		(status >= 500 && status < 600) {
		return forwarding.Result{Outcome: forwarding.OutcomeTransient, Err: err}, true
	}
	return forwarding.Result{Outcome: forwarding.OutcomeTerminal, Err: err}, true
}

func redact(value, secret string) string {
	value = strings.TrimSpace(value)
	if secret == "" {
		return value
	}
	return strings.ReplaceAll(value, secret, "[REDACTED]")
}

type submitPacerRegistry struct {
	mu       sync.Mutex
	interval time.Duration
	byCall   map[string]*submitPacer
}

func newSubmitPacerRegistry(interval time.Duration) *submitPacerRegistry {
	return &submitPacerRegistry{
		interval: interval,
		byCall:   make(map[string]*submitPacer),
	}
}

func (r *submitPacerRegistry) ForAccount(call string) *submitPacer {
	canonicalCall := strings.ToUpper(strings.TrimSpace(call))
	r.mu.Lock()
	defer r.mu.Unlock()
	if pacer, ok := r.byCall[canonicalCall]; ok {
		return pacer
	}
	pacer := &submitPacer{interval: r.interval}
	r.byCall[canonicalCall] = pacer
	return pacer
}

// submitPacer reserves request start times at least interval apart. Reservation
// happens before waiting so even concurrent Submit calls cannot burst. A
// cancelled reservation may leave a harmless quiet gap; it can never shorten
// the upstream interval.
type submitPacer struct {
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
}

func (p *submitPacer) Wait(ctx context.Context) error {
	if p == nil || p.interval <= 0 {
		return nil
	}

	now := time.Now()
	p.mu.Lock()
	slot := now
	if p.next.After(slot) {
		slot = p.next
	}
	p.next = slot.Add(p.interval)
	p.mu.Unlock()

	delay := time.Until(slot)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
