// Package smcloud provides the SM Cloud forwarder (ADR 0040 /
// docs/v2-design/sm-cloud-p1.md, step S3) — the daemon-side backup client.
// It PUTs each QSO to the operator's own smcloud service (cmd/smcloud) as
// full-fidelity JSON: the whole types.Qso payload plus a modified_at /
// deleted_at envelope, so restore round-trips identity (UUID, seconds,
// additional_data) — never lossy ADIF.
//
// Credentials shape (url + token required):
//
//	{"url": "https://cloud.example.org", "token": "…", "logbook": "main"}
//
// url is the smcloud service base URL (the operator's own VPS — there is no
// canonical default, so this type registers NO default endpoints and is NOT
// auto-seeded into the non-sparse config; the operator adds it via the config
// SPA's add-forwarder form, which renders these fields data-drivenly).
// logbook is the cloud-side logbook NAME the QSOs land in (the service
// ensures it exists); empty defaults to "main".
//
// AdifPrefix() returns "" DELIBERATELY: the worker then skips the QSO-row
// ADIF stamp and only updates qso_upload. A stamp write would bump the row's
// modified_at, which would make reconcile (S4) see false drift on every push
// and risk a re-enqueue loop — backup status lives in qso_upload, never on
// the QSO row (sm-cloud-p1.md § AdifPrefix). OTHER forwarders' stamps (QRZ,
// ClubLog, session email) still bump rows after this mirror received them,
// so the type also registers as a ROW MIRROR (RegisterRowMirror): the stamp
// writers re-enqueue the bumped row here via qsoservice.EnqueueStampSync,
// keeping reconcile on its cheap hash-only path instead of a full-manifest
// heal every operating hour.
//
// Registers under type "smcloud" via init(). Import with
//
//	_ "github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
//
// from cmd/smd so the registry learns about it.
package smcloud

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Type is the registry identifier for this forwarder.
const Type = "smcloud"

// DefaultLogbook is the cloud-side logbook name used when the operator leaves
// the credentials field empty. P1 is single-logbook; the field exists so a
// deliberate name survives into S5 restore mapping.
const DefaultLogbook = "main"

// DefaultHTTPTimeout bounds one Submit call. The upstream is the operator's
// own VPS over the same flaky link as QRZ; 30s matches the qrz forwarder's
// reasoning (give a slow link time, never wedge the worker).
const DefaultHTTPTimeout = 30 * time.Second

// maxResponseBytes caps the response body read. Real responses are a tiny
// JSON object; the cap bounds memory against a misbehaving middlebox.
const maxResponseBytes = 1 << 20 // 1 MiB

// errorSnippetLen bounds the response-body excerpt stored in last_error.
const errorSnippetLen = 200

// UserAgent is sent on every request; cmd/smd overrides it at startup with
// the daemon's global Config.UserAgent (same pattern as qrz/clublog).
var UserAgent = "station-manager/dev"

// DefaultRetry is the retry policy when the config entry has no explicit
// `retry`. The flaky-link posture (ADR 0038) does the heavy lifting —
// unreachable retries forever regardless — so this bounds only the
// host-responded-but-erroring class. Same envelope as QRZ: ~60s → 30min.
var DefaultRetry = types.RetryConfig{
	MaxAttempts:       5,
	InitialBackoffSec: 60,
	MaxBackoffSec:     1800,
}

func init() {
	forwarding.Register(Type, New)
	forwarding.RegisterDefaultRetry(Type, DefaultRetry)
	// Row mirror: stamp writes elsewhere re-enqueue here (see the package doc).
	forwarding.RegisterRowMirror(Type)
	// NO RegisterAdifPrefix (this type stamps nothing — see the package doc)
	// and NO RegisterDefaultEndpoints (no canonical URL — the operator's own
	// service; the absence also keeps it out of the auto-seeded non-sparse
	// config, per DefaultForwarderConfigs' operator-must-supply-URL carve-out).
	forwarding.RegisterForwarderType(Type, "SM Cloud backup",
		[]forwarding.Action{action.Insert, action.Update, action.Delete},
		[]forwarding.CredentialField{
			{Key: "url", Label: "Service URL", Kind: "text",
				Help: "Your smcloud service's base URL, e.g. https://cloud.example.org"},
			{Key: "token", Label: "Bearer token", Kind: "password",
				Help: "The SMCLOUD_TOKEN the service was provisioned with."},
			// Clearable: New defaults an empty logbook to DefaultLogbook, so a blank
			// PUT is a genuine "reset to main" — unlike url/token, which New rejects.
			{Key: "logbook", Label: "Cloud logbook name", Kind: "text", Clearable: true,
				Help: "Cloud-side logbook the QSOs land in (created on first push). Leave empty for \"main\"."},
		})
}

// credentials is the type-specific shape of ForwarderConfig.Credentials.
type credentials struct {
	URL     string `json:"url"`
	Token   string `json:"token"`
	Logbook string `json:"logbook"`
}

// Forwarder implements forwarding.Forwarder against the smcloud HTTP API.
// Fields are set at construction and read-only thereafter (concurrency-safe).
type Forwarder struct {
	putURL  string
	token   string
	logbook string
	client  *http.Client
}

// New constructs an SM Cloud Forwarder. url + token are required; logbook
// defaults to DefaultLogbook.
func New(fc types.ForwarderConfig) (forwarding.Forwarder, error) {
	const op errors.Op = "smcloud.New"

	if len(fc.Credentials) == 0 {
		return nil, errors.New(op).WithMsg("credentials required (url, token)")
	}
	var creds credentials
	if err := json.Unmarshal(fc.Credentials, &creds); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("parse credentials")
	}
	base := strings.TrimSpace(creds.URL)
	if base == "" {
		return nil, errors.New(op).WithMsg("credentials.url is required")
	}
	// Echo NOTHING derived from the URL — not the value, and not any component of
	// it. This error is logged as a fatal at startup (spawnForwarderWorkers) and
	// raised by the config PUT's startup probe, so anything included lands in the
	// daemon log. No part of a URL is inherently inert: url.Parse takes everything
	// before the first ':' as the scheme, so an operator who omits "https://"
	// turns "alice:token@host" into scheme "alice" — the username — and a pasted
	// token becomes the scheme outright. Name the field and the requirement only.
	// Same discipline as scrubURLError in internal/lookup/qrz.
	u, err := url.Parse(base)
	switch {
	case err != nil:
		return nil, errors.New(op).WithMsg("credentials.url is not a parseable URL")
	case u.Scheme != "http" && u.Scheme != "https":
		return nil, errors.New(op).WithMsg("credentials.url must start with http:// or https://")
	case u.Host == "":
		return nil, errors.New(op).WithMsg("credentials.url has no host")
	}
	if creds.Token == "" {
		return nil, errors.New(op).WithMsg("credentials.token is required")
	}
	logbook := strings.TrimSpace(creds.Logbook)
	if logbook == "" {
		logbook = DefaultLogbook
	}
	return &Forwarder{
		putURL:  strings.TrimRight(base, "/") + "/v1/qsos",
		token:   creds.Token,
		logbook: logbook,
		client:  &http.Client{Timeout: DefaultHTTPTimeout},
	}, nil
}

// Type returns the registry identifier for this forwarder.
func (f *Forwarder) Type() string { return Type }

// AdifPrefix returns "" so the worker never stamps the QSO row — a stamp
// write would bump modified_at and poison reconcile (see the package doc).
func (f *Forwarder) AdifPrefix() string { return "" }

// qsoUpload / putRequest mirror the smcloud service's PUT /v1/qsos wire
// (internal/cloud/server QsoUpload / PutQsosRequest). Declared locally rather
// than imported: the cloud packages must stay daemon-free, and a production
// import THIS direction would blur the boundary the other way — the shared
// contract is types.Qso itself; this envelope is four fields whose round-trip
// the integration test pins against the real server.
type qsoUpload struct {
	ModifiedAt time.Time `json:"modified_at"`
	// Revision is the row's monotonic edit counter (ADR 0050) — the primary
	// ordering the cloud's upsert guard applies. Envelope-only, like
	// modified_at (types.Qso tags it json:"-", so it never rides the payload).
	Revision  int64      `json:"revision,omitempty"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	Qso       types.Qso  `json:"qso"`
}

type putRequest struct {
	Logbook string      `json:"logbook"`
	Qsos    []qsoUpload `json:"qsos"`
}

type putResponse struct {
	Received int `json:"received"`
	Applied  int `json:"applied"`
}

// Submit PUTs one QSO to the smcloud service. Insert and update are the same
// upsert (the store is keyed by UUID and guards staleness on modified_at); a
// delete sends the same record with deleted_at set — the tombstone. The UUID
// is the upstream key, so priorUpstreamID is ignored.
func (f *Forwarder) Submit(
	ctx context.Context,
	qso types.Qso,
	act forwarding.Action,
	_ string, // priorUpstreamID — UUID-keyed upstream, not needed
) forwarding.Result {
	const op errors.Op = "smcloud.Submit"

	if err := ctx.Err(); err != nil {
		return forwarding.Result{Outcome: forwarding.OutcomeTransient, Err: err}
	}
	if qso.UUID == "" {
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err:     errors.New(op).WithMsg("qso has no UUID"),
		}
	}
	// modified_at is the reconcile drift signal — silently substituting
	// now() here would make the cloud copy never hash-match the local row.
	// A zero value means the fetch path didn't overlay the column: a bug,
	// surfaced loudly as terminal rather than papered over.
	if qso.ModifiedAt.IsZero() {
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err:     errors.New(op).WithMsgf("qso %s has no modified_at (fetch overlay missing)", qso.UUID),
		}
	}

	up := qsoUpload{ModifiedAt: qso.ModifiedAt, Revision: qso.Revision, Qso: qso}
	switch act {
	case action.Insert, action.Update:
		// plain upsert
	case action.Delete:
		// Tombstone: deleted_at set. The soft-deleted row carries its
		// deleted_at column; fall back to modified_at (the delete bumped it)
		// if a path ever yields a tombstone-less delete action.
		d := qso.DeletedAt
		if d.IsZero() {
			d = qso.ModifiedAt
		}
		up.DeletedAt = &d
	default:
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err:     errors.New(op).WithMsgf("unknown action %q", act),
		}
	}

	body, err := json.Marshal(putRequest{Logbook: f.logbook, Qsos: []qsoUpload{up}})
	if err != nil {
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err:     errors.New(op).WithErr(err).WithMsg("marshal request"),
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, f.putURL, bytes.NewReader(body))
	if err != nil {
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err:     errors.New(op).WithErr(err).WithMsg("build HTTP request"),
		}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.token)
	req.Header.Set("User-Agent", UserAgent)

	resp, err := f.client.Do(req)
	if err != nil {
		// No response at all — DNS, refused, TLS, timeout. The QSO is fine;
		// only the link is down. Unreachable → the worker retries forever
		// (ADR 0038) — the whole point of a backup on a flaky link.
		return forwarding.Result{
			Outcome: forwarding.OutcomeUnreachable,
			Err:     errors.New(op).WithErr(err).WithMsg("PUT to smcloud"),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return forwarding.Result{
			Outcome: forwarding.OutcomeTransient,
			Err:     errors.New(op).WithErr(err).WithMsg("read smcloud response"),
		}
	}

	if r, done := classifyHTTPStatus(resp.StatusCode, respBody); done {
		return r
	}

	var out putResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err:     errors.New(op).WithErr(err).WithMsg("parse smcloud response"),
		}
	}
	// The cloud must acknowledge exactly this ONE record: received == 1, and
	// applied is 1 (stored) or 0 (the stale-push guard held a newer cloud copy —
	// the cloud already has this QSO's future, which for a BACKUP is success).
	// Any other combination is a protocol-invalid 2xx (e.g. {} → received:0)
	// where the cloud did NOT take the QSO; treat it as transient so the backup
	// RETRIES rather than being silently marked uploaded and dropped (review
	// 2026-07-20 internal/forwarding #3). Transient, not terminal: a bad ack from
	// our own cloud is a server-side blip the backup must eventually clear.
	if out.Received != 1 || out.Applied < 0 || out.Applied > 1 {
		return forwarding.Result{
			Outcome: forwarding.OutcomeTransient,
			Err: errors.New(op).WithMsgf("smcloud ack invalid: received=%d applied=%d",
				out.Received, out.Applied),
		}
	}
	// The UUID doubles as the upstream id (the store's key).
	return forwarding.Result{Outcome: forwarding.OutcomeSuccess, UpstreamID: qso.UUID}
}

// classifyHTTPStatus maps a non-2xx response to a Result. Same matrix as the
// qrz forwarder: 408/429/5xx are transient (host up, try later); other 4xx —
// notably 401 (bad token) and 400 (malformed) — are terminal.
func classifyHTTPStatus(status int, body []byte) (forwarding.Result, bool) {
	const op errors.Op = "smcloud.classifyHTTPStatus"

	if status >= 200 && status < 300 {
		return forwarding.Result{}, false
	}
	snippet := bodySnippet(body, errorSnippetLen)
	switch {
	case status == http.StatusRequestTimeout,
		status == http.StatusTooManyRequests,
		status >= 500 && status < 600:
		return forwarding.Result{
			Outcome: forwarding.OutcomeTransient,
			Err:     errors.New(op).WithMsgf("smcloud returned HTTP %d (body: %s)", status, snippet),
		}, true
	default:
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err:     errors.New(op).WithMsgf("smcloud returned HTTP %d (body: %s)", status, snippet),
		}, true
	}
}

// bodySnippet bounds the response-body excerpt for last_error.
func bodySnippet(body []byte, max int) string {
	s := strings.TrimSpace(string(body))
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
