// Package clublog provides the Club Log forwarder. It pushes QSOs to
// Club Log via the real-time HTTP API and classifies upstream
// responses into Success / Transient / Terminal outcomes so the worker
// can retry, mark failed, or stamp success as appropriate.
//
// Credentials shape (three fields, all required) — the OPERATOR'S account
// details, which live in config.json:
//
//	{
//	  "email":    "operator@example.com",
//	  "password": "an-application-password",
//	  "callsign": "7Q5MLV"
//	}
//
// A Club Log upload needs those PLUS the application API key, which is NOT a
// config field (see InjectedAPIKey). The names ("application password" vs
// "application API key") sound alike but authenticate different things:
//
//   - email + password = the OPERATOR'S ACCOUNT. password should be a
//     Club Log "Application Password" (a scoped, revocable per-account
//     password the operator generates in their account settings, used
//     instead of their real login password). These tell Club Log whose
//     log the QSO belongs to and that the request is authorised.
//   - the application API key = the SOFTWARE making the request (Station
//     Manager). It is the SAME key for every SM install, so it carries no
//     operator identity — it is the app-identification / rate-limiting
//     layer, not account auth, which is why email + password are still
//     required. Club Log issues one CONFIDENTIAL key per *application* and
//     auto-deletes any key it finds PUBLISHED IN SOURCE CODE, so SM keeps it
//     out of both source AND config.json: it is stamped into the binary at
//     BUILD time via -ldflags from the gitignored .env (CLUBLOG_API_KEY →
//     InjectedAPIKey). A compiled binary is not source code, so this honours
//     Club Log's rule. See ADR 0054 + docs/v2-design/forwarding.md.
//
// (Contrast QRZ, whose single logbook-specific api_key both authenticates
// AND selects the logbook, so the QRZ forwarder needs nothing else.)
//
// callsign selects which of the account's logs the QSO lands in.
//
// Two endpoints back the two supported actions:
//
//	insert → POST realtime.php with the QSO as a single ADIF record
//	delete → POST delete.php identifying the record by dxcall +
//	         datetime + bandid (Club Log returns no upstream record id,
//	         so deletes match on QSO fields, not a stored id)
//
// Club Log's real-time API has no general field-update: re-uploading an
// edited QSO is detected as a duplicate and the edit is ignored. So
// action=update is classified Terminal with a clear message rather than
// silently no-op'ing. Operators correct QSO details via a bulk
// re-upload outside the real-time path.
//
// On HTTP 403 Club Log mandates that the client "STOP sending real-time
// requests immediately" or the IP is firewalled. The worker processes a
// batch of rows per tick, so a single bad credential would otherwise
// fire several 403s in quick succession. To honour the rule, the
// forwarder trips an internal circuit breaker on the first 403: every
// subsequent Submit short-circuits to Terminal WITHOUT a network call
// until the daemon restarts (by which point the operator has presumably
// fixed the credentials).
//
// Registers under type "clublog" via init(). Import the package with
//
//	_ "github.com/ColonelBlimp/station-manager/internal/forwarding/clublog"
//
// from cmd/smd (or a test) so the registry learns about it.
package clublog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/securehttp"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Type is the registry identifier for this forwarder.
const Type = "clublog"

// AdifFieldPrefix is the ADIF field-name prefix the worker stamps on
// the QSO row after a successful insert. Combined with the standard
// suffixes it produces CLUBLOG_QSO_UPLOAD_STATUS and
// CLUBLOG_QSO_UPLOAD_DATE, per the ADIF specification.
const AdifFieldPrefix = "CLUBLOG"

// DefaultRealtimeEndpoint is the Club Log real-time upload URL (insert).
// DefaultDeleteEndpoint is the real-time delete URL. Both are
// overridable via the package-internal constructor so httptest fixtures
// can stand in for the real service.
const (
	DefaultRealtimeEndpoint = "https://clublog.org/realtime.php"
	DefaultDeleteEndpoint   = "https://clublog.org/delete.php"
)

// DefaultHTTPTimeout is the per-call timeout for Club Log requests. The
// operator's internet link is flaky (see project_sm_operator_network
// memory), so 30s balances "give the server time" against "don't let a
// hung socket wedge the worker".
const DefaultHTTPTimeout = 30 * time.Second

// maxResponseBytes caps the response body. Club Log responses are tiny
// (a short status string); the cap only bounds memory if a misbehaving
// upstream (DNS hijack, transparent proxy) returns a giant body.
const maxResponseBytes = 1 << 20 // 1 MiB

// errorSnippetLen bounds the response-body excerpt stored in last_error
// so it fits a log line.
const errorSnippetLen = 200

// UserAgent is the User-Agent header this forwarder sends. cmd/smd
// overrides this var at startup with the daemon's global
// Config.UserAgent. The "dev" fallback keeps in-package tests hermetic.
var UserAgent = "station-manager/dev"

// InjectedAPIKey is the Club Log application API key, stamped into the binary
// at BUILD time via
//
//	-ldflags "-X github.com/ColonelBlimp/station-manager/internal/forwarding/clublog.InjectedAPIKey=<key>"
//
// sourced from the gitignored .env (CLUBLOG_API_KEY). Club Log issues one
// confidential key per application and auto-deletes any key found published in
// source code, so it lives neither in this source nor in config.json — only in
// the operator's local .env and the resulting private build (a compiled binary
// is not source code). Empty when a build omits the key (CI, a fresh clone with
// no .env): the forwarder STILL constructs (New must not error — a Build error
// aborts the whole daemon in cmd/smd), but it short-circuits every Submit to
// Unreachable with no network call, so Club Log rows stay safely queued (and
// auto-ship once a keyed build is deployed) while logging and every other
// forwarder keep running.
var InjectedAPIKey string

// DefaultRetry is the retry policy the daemon uses when a Club Log
// forwarder config entry has no explicit `retry` block. Tuned like
// QRZ's: transient 5xx/network blips recover within minutes, and the
// operator's link is slow so we don't hammer the server.
var DefaultRetry = types.RetryConfig{
	MaxAttempts:       5,
	InitialBackoffSec: 60,
	MaxBackoffSec:     1800,
}

func init() {
	forwarding.Register(Type, New)
	forwarding.RegisterDefaultRetry(Type, DefaultRetry)
	// The ADIF stamp prefix the worker writes on a successful upload — so the
	// daemon can resolve clublog → its "uploaded to X?" stamp field without
	// building the forwarder (manual-backfill skip-check + missing_from filter,
	// ADR 0039).
	forwarding.RegisterAdifPrefix(Type, AdifFieldPrefix)
	// Per-action endpoints: insert → realtime, delete → delete. Seeded into
	// config so they're config-driven + overridable without a recompile (ADR
	// 0039); New resolves each from config with these as fallbacks.
	forwarding.RegisterDefaultEndpoints(Type, map[string]string{
		action.Insert.String(): DefaultRealtimeEndpoint,
		action.Delete.String(): DefaultDeleteEndpoint,
	})
	// ClubLog forbids catch-up batches of pre-existing QSOs on realtime.php
	// (helpdesk, 2026-07-19: that anti-pattern gets the application API key
	// BLOCKED; bulk belongs to putlogs.php, which SM does not speak yet).
	// Registering keeps the promise SM made when the key was granted: manual
	// enqueue re-arms only rows with prior ClubLog queue history (failed live
	// uploads — e.g. 403-era rows after a credential fix); history-less rows
	// are refused. Live QSOs (enqueued at logging time) are unaffected.
	// Historical catch-up = an ADIF export uploaded on clublog.org.
	forwarding.RegisterNoBulkBackfill(Type)
	// ClubLog real-time supports insert + delete only — it cannot edit a
	// logged QSO's fields (see Submit). Registering the supported set means
	// an omitted action_filter defaults to insert/delete (not all three),
	// so a natural config never queues update rows bound to fail. The
	// descriptor drives the config SPA's add-forwarder form: ClubLog needs the
	// account email + an Application Password + callsign. The Application API key
	// is NOT an operator-entered field — it is build-injected (see
	// InjectedAPIKey), so the form never prompts for it and it never reaches
	// config.json.
	forwarding.RegisterForwarderType(Type, "ClubLog",
		[]forwarding.Action{action.Insert, action.Delete},
		[]forwarding.CredentialField{
			{Key: "email", Label: "Account email", Kind: "text"},
			{Key: "password", Label: "Application password", Kind: "password",
				Help: "A ClubLog Application Password — create one in your ClubLog settings; NOT your main account password."},
			{Key: "callsign", Label: "Callsign", Kind: "text"},
		})
}

// credentials is the type-specific shape of ForwarderConfig.Credentials
// for Club Log — the OPERATOR'S account details. Extra fields are ignored,
// so a stale `api` left in an older config is silently dropped rather than
// used: the app API key is build-injected only (see InjectedAPIKey).
type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Callsign string `json:"callsign"`
}

// Forwarder implements forwarding.Forwarder by POSTing to the Club Log
// real-time API. The credential/endpoint fields are set at construction
// and read-only thereafter; authFailed is the only mutable state (the
// 403 circuit breaker), guarded by atomic access so concurrent Submit
// calls from the worker are safe.
type Forwarder struct {
	email      string
	password   string
	callsign   string
	apiKey     string
	noKey      bool // built without CLUBLOG_API_KEY → Submit fails Terminal, no network
	realtime   string
	deleteURL  string
	client     *http.Client
	authFailed atomic.Bool
}

// New constructs a Club Log Forwarder from the given ForwarderConfig. The three
// operator credential fields (email, password, callsign) come from config; the
// application API key comes from the build (InjectedAPIKey). All four must
// resolve, so a binary built without the key refuses to construct rather than
// send keyless requests.
func New(fc types.ForwarderConfig) (forwarding.Forwarder, error) {
	const op errors.Op = "clublog.New"

	if len(fc.Credentials) == 0 {
		return nil, errors.New(op).WithMsg("credentials required (email, password, callsign)")
	}

	var creds credentials
	if err := json.Unmarshal(fc.Credentials, &creds); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("parse credentials")
	}
	switch {
	case creds.Email == "":
		return nil, errors.New(op).WithMsg("credentials.email is required")
	case creds.Password == "":
		return nil, errors.New(op).WithMsg("credentials.password is required")
	case creds.Callsign == "":
		return nil, errors.New(op).WithMsg("credentials.callsign is required")
	}

	// The app API key is BUILD-injected (never config or source). A binary built
	// WITHOUT it can't authenticate to Club Log — but New must NOT error on that,
	// because spawnForwarderWorkers aborts the ENTIRE daemon on a Build error
	// (cmd/smd), and a missing build-time env var must never brick logging and
	// every other forwarder. Instead we construct a forwarder that short-circuits
	// every Submit to a clear Terminal (no network call — parallels the 403
	// breaker), so the daemon runs and the operator sees "key not built in" on the
	// ClubLog upload rows. An empty apiKey sets f.noKey in newWithEndpoints.
	apiKey := strings.TrimSpace(InjectedAPIKey)

	// Config-driven per-action endpoints, with the package consts as fallbacks
	// (ADR 0039).
	realtime := forwarding.ResolveEndpoint(fc.Endpoints, DefaultRealtimeEndpoint, action.Insert.String())
	deleteURL := forwarding.ResolveEndpoint(fc.Endpoints, DefaultDeleteEndpoint, action.Delete.String())
	// BOTH endpoints carry the account password + shared app key, so BOTH must be
	// https (http only for a loopback mock) — a bad delete endpoint is caught even
	// when realtime is fine (A6). allow_insecure_http is SM-Cloud-only (ST-4a).
	for _, ep := range []struct{ field, url string }{
		{"realtime endpoint", realtime},
		{"delete endpoint", deleteURL},
	} {
		if err := securehttp.CheckCredentialedURL(ep.url, false); err != nil {
			return nil, errors.New(op).WithErr(err).
				WithMsgf("%s must use https (http allowed only for loopback)", ep.field)
		}
	}
	return newWithEndpoints(creds, apiKey, realtime, deleteURL,
		securehttp.NewClient(DefaultHTTPTimeout)), nil
}

// newWithEndpoints is the package-internal constructor that tests use to
// point the forwarder at httptest.NewServer URLs with an arbitrary
// http.Client. The apiKey is passed explicitly (New sources it from
// InjectedAPIKey). Production code goes through New.
func newWithEndpoints(creds credentials, apiKey, realtime, deleteURL string, client *http.Client) *Forwarder {
	if client == nil {
		client = &http.Client{Timeout: DefaultHTTPTimeout}
	}
	return &Forwarder{
		email:     creds.Email,
		password:  creds.Password,
		callsign:  creds.Callsign,
		apiKey:    apiKey,
		noKey:     apiKey == "",
		realtime:  realtime,
		deleteURL: deleteURL,
		client:    client,
	}
}

// Type returns the registry identifier for this forwarder.
func (f *Forwarder) Type() string { return Type }

// AdifPrefix returns "CLUBLOG" so the worker stamps
// CLUBLOG_QSO_UPLOAD_STATUS / CLUBLOG_QSO_UPLOAD_DATE on the QSO row
// after a successful insert.
func (f *Forwarder) AdifPrefix() string { return AdifFieldPrefix }

// Submit pushes one QSO+action pair to Club Log.
//
//	insert → realtime.php, QSO as one ADIF record
//	update → Terminal (Club Log real-time can't edit QSO fields)
//	delete → delete.php, matched by dxcall + datetime + bandid
//
// The interface's priorUpstreamID arg is ignored (hence `_`): Club Log returns
// no upstream record id, so a delete is identified by the QSO's own fields. If
// the 403 circuit breaker has tripped, every action short-circuits to Terminal
// without touching the network.
func (f *Forwarder) Submit(
	ctx context.Context,
	qso types.Qso,
	act forwarding.Action,
	_ string,
) forwarding.Result {
	const op errors.Op = "clublog.Submit"

	if err := ctx.Err(); err != nil {
		return forwarding.Result{Outcome: forwarding.OutcomeTransient, Err: err}
	}

	// No build-time API key: this binary can't authenticate. Short-circuit
	// WITHOUT a network call (a keyless request would 403 and trip the breaker
	// for the whole run). Return UNREACHABLE, not Terminal: a Terminal would mark
	// every claimed row — including an existing backlog — permanently failed, and
	// startup only recovers in_progress rows, so those QSOs would silently never
	// reach ClubLog without a manual re-arm. Unreachable keeps the rows queued
	// and retried (cheaply — no I/O) so deploying a keyed build auto-ships them,
	// honouring the no-data-loss invariant (ADR 0038).
	if f.noKey {
		return forwarding.Result{
			Outcome: forwarding.OutcomeUnreachable,
			Err: errors.New(op).WithMsg(
				"ClubLog application API key not built into this binary; rebuild with CLUBLOG_API_KEY set in .env"),
		}
	}

	// Circuit breaker: once Club Log has returned a 403 we must not send
	// further real-time requests or the IP gets firewalled.
	if f.authFailed.Load() {
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err: errors.New(op).WithMsg(
				"ClubLog auth previously rejected (403); refusing further requests until restart — fix credentials",
			),
		}
	}

	switch act {
	case action.Insert:
		return f.submitInsert(ctx, qso)
	case action.Update:
		// Club Log real-time has no REPLACE: an edited QSO re-uploaded is
		// detected as a duplicate and the change is ignored. Fail loudly
		// rather than report a phantom success.
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err: errors.New(op).WithMsg(
				"ClubLog real-time API cannot edit QSO fields; correct via bulk re-upload",
			),
		}
	case action.Delete:
		return f.submitDelete(ctx, qso)
	default:
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err:     errors.New(op).WithMsgf("unknown action %q", act),
		}
	}
}

// submitInsert POSTs the QSO as a single ADIF record to realtime.php.
func (f *Forwarder) submitInsert(ctx context.Context, qso types.Qso) forwarding.Result {
	const op errors.Op = "clublog.submitInsert"

	form := url.Values{}
	form.Set("email", f.email)
	form.Set("password", f.password)
	form.Set("callsign", f.callsign)
	form.Set("api", f.apiKey)
	form.Set("adif", adif.ConvertQsoToAdifNoHeader(qso))

	return f.post(ctx, op, f.realtime, form)
}

// submitDelete POSTs a delete request to delete.php. Club Log identifies
// the record by the counterparty callsign, the QSO timestamp, and the
// numeric band id; all three must be derivable from the QSO or the
// delete can never resolve (Terminal, no network call).
func (f *Forwarder) submitDelete(ctx context.Context, qso types.Qso) forwarding.Result {
	const op errors.Op = "clublog.submitDelete"

	if qso.Call == "" {
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err:     errors.New(op).WithMsg("delete requires a contacted callsign"),
		}
	}
	datetime, err := formatDateTime(qso.QsoDate, qso.TimeOn)
	if err != nil {
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err:     errors.New(op).WithErr(err).WithMsg("delete requires a valid QSO date/time"),
		}
	}
	bandid, ok := bandID(qso.Band)
	if !ok {
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err:     errors.New(op).WithMsgf("delete: band %q has no ClubLog band id", qso.Band),
		}
	}

	form := url.Values{}
	form.Set("email", f.email)
	form.Set("password", f.password)
	form.Set("callsign", f.callsign)
	form.Set("api", f.apiKey)
	form.Set("dxcall", qso.Call)
	form.Set("datetime", datetime)
	form.Set("bandid", bandid)

	return f.post(ctx, op, f.deleteURL, form)
}

// post sends the form to the endpoint and classifies the outcome.
// Transport faults (network, ctx, body read) are decided here; HTTP
// status is handed to classifyHTTPStatus, which also trips the 403
// circuit breaker.
func (f *Forwarder) post(ctx context.Context, op errors.Op, endpoint string, form url.Values) forwarding.Result {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()),
	)
	if err != nil {
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err:     errors.New(op).WithErr(err).WithMsg("build HTTP request"),
		}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", UserAgent)

	resp, err := securehttp.Do(f.client, req)
	if err != nil {
		// No response came back — the host is unreachable (DNS, connection
		// refused, TLS, timeout, no route, or ctx cancel mid-flight), or a
		// cross-origin redirect was refused (ST-4b, URL-free error). The
		// QSO is fine; only the link is down. Unreachable → retry forever
		// so an outage never abandons the QSO (ADR 0038).
		return forwarding.Result{
			Outcome: forwarding.OutcomeUnreachable,
			Err:     errors.New(op).WithErr(err).WithMsg("POST to ClubLog"),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	// ClubLog encodes the entire result in the STATUS CODE; the body is
	// human-readable detail only. Read it BEST-EFFORT — a reset or truncated
	// body must NOT downgrade the outcome, because doing so on a 403 would
	// return a retryable Transient instead of tripping the circuit breaker,
	// and the worker would keep POSTing at an upstream that is blocking us
	// (violating the stop-immediately contract — review 2026-07-20
	// internal/forwarding #2). The status is authoritative regardless.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	return f.classifyHTTPStatus(op, resp.StatusCode, resp.Status, body)
}

// classifyHTTPStatus maps a Club Log HTTP response into a
// forwarding.Result. Club Log encodes the result entirely in the status
// code (the reason phrase / body carry human detail only):
//
//	2xx                    → Success (QSO OK / Modified / Duplicate on
//	                         insert; OK / Not Deleted on delete — all of
//	                         these mean "end state matches our intent")
//	403 Forbidden          → Terminal + trips the circuit breaker
//	408 / 429 / 5xx        → Transient
//	other 4xx (e.g. 400)   → Terminal (QSO Rejected — bad ADIF)
//	other                  → Terminal (unexpected for this API)
//
// status is the numeric code; statusText is the full status line (e.g.
// "200 QSO Duplicate") which Club Log uses to convey the sub-result and
// which we surface in diagnostics.
func (f *Forwarder) classifyHTTPStatus(op errors.Op, status int, statusText string, body []byte) forwarding.Result {
	if status >= 200 && status < 300 {
		// Club Log returns no upstream record id, so UpstreamID stays empty.
		return forwarding.Result{Outcome: forwarding.OutcomeSuccess}
	}

	detail := statusOrSnippet(statusText, body)

	if status == http.StatusForbidden {
		// Honour ClubLog's "STOP sending immediately" rule: trip the
		// breaker so no further request reaches the upstream this run.
		f.authFailed.Store(true)
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err: errors.New(op).WithMsgf(
				"ClubLog auth rejected (HTTP 403): %s — stopping requests until credentials are fixed", detail,
			),
		}
	}

	switch {
	case status == http.StatusRequestTimeout,
		status == http.StatusTooManyRequests,
		status >= 500 && status < 600:
		return forwarding.Result{
			Outcome: forwarding.OutcomeTransient,
			Err:     errors.New(op).WithMsgf("ClubLog returned HTTP %d (%s)", status, detail),
		}
	default:
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err:     errors.New(op).WithMsgf("ClubLog returned HTTP %d (%s)", status, detail),
		}
	}
}

// statusOrSnippet prefers the HTTP status line (Club Log's sub-result,
// e.g. "200 QSO Rejected") and falls back to a bounded body excerpt when
// the status line is empty.
func statusOrSnippet(statusText string, body []byte) string {
	if s := strings.TrimSpace(statusText); s != "" {
		return s
	}
	return bodySnippet(body, errorSnippetLen)
}

// bodySnippet returns a bounded view of the response body for error
// messages — full bodies can be large and aren't useful in last_error.
func bodySnippet(body []byte, max int) string {
	s := strings.TrimSpace(string(body))
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// formatDateTime converts an ADIF QSO date (YYYYMMDD) and time (HHMM or
// HHMMSS) into Club Log's delete.php datetime format "YYYY-MM-DD
// HH:MM:SS". An HHMM time is zero-padded to whole seconds.
func formatDateTime(qsoDate, timeOn string) (string, error) {
	const op errors.Op = "clublog.formatDateTime"

	if len(qsoDate) != 8 || !allDigits(qsoDate) {
		return "", errors.New(op).WithMsgf("qso_date %q is not YYYYMMDD", qsoDate)
	}
	t := timeOn
	switch len(t) {
	case 4:
		t += "00"
	case 6:
		// already HHMMSS
	default:
		return "", errors.New(op).WithMsgf("time_on %q is not HHMM or HHMMSS", timeOn)
	}
	if !allDigits(t) {
		return "", errors.New(op).WithMsgf("time_on %q is not numeric", timeOn)
	}

	return fmt.Sprintf("%s-%s-%s %s:%s:%s",
		qsoDate[0:4], qsoDate[4:6], qsoDate[6:8],
		t[0:2], t[2:4], t[4:6],
	), nil
}

// bandIDByBand maps the ADIF band string (as stored on the QSO, lower-
// case) to Club Log's numeric band id used by delete.php. Bands without
// a Club Log id (e.g. 1.25m, 33cm) are absent — a delete on such a band
// classifies Terminal rather than guessing.
var bandIDByBand = map[string]string{
	"160m": "160",
	"80m":  "80",
	"60m":  "60",
	"40m":  "40",
	"30m":  "30",
	"20m":  "20",
	"17m":  "17",
	"15m":  "15",
	"12m":  "12",
	"10m":  "10",
	"6m":   "6",
	"4m":   "4",
	"2m":   "2",
	"70cm": "70",
	"23cm": "23",
	"13cm": "13",
}

// bandID returns the Club Log numeric band id for an ADIF band string,
// and whether a mapping exists.
func bandID(b string) (string, bool) {
	id, ok := bandIDByBand[strings.ToLower(strings.TrimSpace(b))]
	return id, ok
}

// allDigits reports whether s is non-empty and all ASCII digits.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
