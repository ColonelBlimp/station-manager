package smcloud

import (
	"bytes"
	"context"
	"encoding/json"
	stderr "errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/securehttp"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// AdoptionRequestTimeout bounds each request of an adoption attempt (ADR 0090,
// T2).
const AdoptionRequestTimeout = 10 * time.Second

// AdoptionFailure classifies a failed adoption request by what the server's
// answer proves (ADR 0090, T2 and T4; ADR 0091, A3).
type AdoptionFailure string

const (
	// AdoptionUnreachable: no answer, a timeout, or a 5xx. Retried hourly;
	// after an adoption request, its outcome is unknown.
	AdoptionUnreachable AdoptionFailure = "unreachable"
	// AdoptionUnauthorized: 401, the token was refused.
	AdoptionUnauthorized AdoptionFailure = "unauthorized"
	// AdoptionUnreadable: a 200 whose body is not the answer the protocol
	// defines.
	AdoptionUnreadable AdoptionFailure = "unreadable"
	// AdoptionConflict: 409; Code names the conflict when the server gave a
	// known one.
	AdoptionConflict AdoptionFailure = "conflict"
	// AdoptionRefused: any other status the protocol does not define.
	AdoptionRefused AdoptionFailure = "refused"
)

// AdoptionError is a classified adoption request failure. Its text names the
// request and the HTTP status, never the URL, the token or the body.
type AdoptionError struct {
	Failure AdoptionFailure
	Status  int
	Code    string
	err     error
}

func (e *AdoptionError) Error() string {
	if e.err == nil {
		return "smcloud adoption: " + string(e.Failure)
	}
	return "smcloud adoption: " + string(e.Failure) + ": " + e.err.Error()
}

func (e *AdoptionError) Unwrap() error { return e.err }

// AdoptRequest is POST /v1/archives/adopt.
type AdoptRequest struct {
	LegacyName   string `json:"legacy_name"`
	ArchiveUUID  string `json:"archive_uuid"`
	ArchiveLabel string `json:"archive_label"`
	LogbookUUID  string `json:"logbook_uuid"`
	LogbookName  string `json:"logbook_name"`
	Callsign     string `json:"callsign"`
}

// AdoptionClient makes Home's adoption requests to the station account's
// server. It never follows a redirect: a 3xx is an answer the protocol does
// not define, so the bearer token is never sent on.
type AdoptionClient struct {
	base    string
	token   string
	client  *http.Client
	timeout time.Duration
}

// NewAdoptionClient builds the client from the SM Cloud station account,
// validated exactly as the forwarder's (New): a URL and a token, https or
// loopback http unless allow_insecure_http.
func NewAdoptionClient(account types.ForwarderConfig) (*AdoptionClient, error) {
	const op errors.Op = "smcloud.NewAdoptionClient"
	if _, err := New(account); err != nil {
		return nil, errors.New(op).WithErr(err)
	}
	var creds credentials
	if err := json.Unmarshal(account.Credentials, &creds); err != nil {
		return nil, errors.New(op).WithMsg("the station account's credentials cannot be read")
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &AdoptionClient{
		base:    strings.TrimRight(strings.TrimSpace(creds.URL), "/"),
		token:   creds.Token,
		client:  client,
		timeout: AdoptionRequestTimeout,
	}, nil
}

// IdentityProtocol reads GET /v1/version's identity_protocol; 0 when absent.
func (c *AdoptionClient) IdentityProtocol(ctx context.Context) (int, error) {
	var out struct {
		IdentityProtocol int `json:"identity_protocol"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/version", nil, maxResponseBytes, &out); err != nil {
		return 0, err
	}
	return out.IdentityProtocol, nil
}

// CloudUUIDs lists every QSO UUID the cloud holds under the legacy logbook
// name, tombstones included; none when the name was never pushed.
func (c *AdoptionClient) CloudUUIDs(ctx context.Context, name string) ([]string, error) {
	var list struct {
		Logbooks *[]struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"logbooks"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/logbooks", nil, maxResponseBytes, &list); err != nil {
		return nil, err
	}
	if list.Logbooks == nil {
		return nil, &AdoptionError{Failure: AdoptionUnreadable, Status: http.StatusOK, err: fmt.Errorf("GET /v1/logbooks: no logbooks list")}
	}
	id := int64(0)
	for _, lb := range *list.Logbooks {
		if lb.Name == name {
			id = lb.ID
		}
	}
	if id == 0 {
		return nil, nil
	}
	var manifest struct {
		Entries *[]struct {
			UUID string `json:"uuid"`
		} `json:"entries"`
	}
	path := fmt.Sprintf("/v1/logbooks/%d/manifest", id)
	if err := c.do(ctx, http.MethodGet, path, nil, maxManifestBytes, &manifest); err != nil {
		return nil, err
	}
	if manifest.Entries == nil {
		return nil, &AdoptionError{Failure: AdoptionUnreadable, Status: http.StatusOK, err: fmt.Errorf("GET %s: no entries list", path)}
	}
	out := make([]string, 0, len(*manifest.Entries))
	for _, e := range *manifest.Entries {
		if strings.TrimSpace(e.UUID) == "" {
			return nil, &AdoptionError{Failure: AdoptionUnreadable, Status: http.StatusOK, err: fmt.Errorf("GET %s: an entry without a uuid", path)}
		}
		out = append(out, e.UUID)
	}
	return out, nil
}

// Adopt sends POST /v1/archives/adopt, UUIDs in their canonical lower case,
// and checks the answer names the archive and logbook sent. Whether the server
// changed anything (a replay is unchanged) does not matter: both confirm the
// mapping.
func (c *AdoptionClient) Adopt(ctx context.Context, req AdoptRequest) error {
	const path = "/v1/archives/adopt"
	req.ArchiveUUID, req.LogbookUUID = strings.ToLower(req.ArchiveUUID), strings.ToLower(req.LogbookUUID)
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	var out struct {
		ArchiveUUID string `json:"archive_uuid"`
		LogbookUUID string `json:"logbook_uuid"`
	}
	if err := c.do(ctx, http.MethodPost, path, body, maxResponseBytes, &out); err != nil {
		return err
	}
	if strings.ToLower(out.ArchiveUUID) != req.ArchiveUUID || strings.ToLower(out.LogbookUUID) != req.LogbookUUID {
		return &AdoptionError{Failure: AdoptionUnreadable, Status: http.StatusOK, err: fmt.Errorf("POST %s: the answer names another archive or logbook", path)}
	}
	return nil
}

// knownConflicts are the 409 codes the adoption can answer (ADR 0089); any
// other code is not repeated, since it is server-supplied text.
var knownConflicts = map[string]bool{
	"legacy_archive_adopted_elsewhere": true,
	"archive_uuid_in_use":              true,
	"logbook_mapping_conflict":         true,
}

// do sends one bounded request and decodes a 200 into out. Every failure is an
// *AdoptionError whose text names the method, the path and the status only.
func (c *AdoptionClient) do(ctx context.Context, method, path string, body []byte, limit int64, out any) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return &AdoptionError{Failure: AdoptionRefused, err: fmt.Errorf("%s %s: the request cannot be built", method, path)}
	}
	if path != "/v1/version" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := securehttp.Do(c.client, req)
	if err != nil {
		return &AdoptionError{Failure: AdoptionUnreachable, err: fmt.Errorf("%s %s: no answer: %w", method, path, transportCause(ctx, err))}
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return &AdoptionError{Failure: AdoptionUnreachable, Status: resp.StatusCode, err: fmt.Errorf("%s %s: HTTP %d, the answer was cut off: %w", method, path, resp.StatusCode, transportCause(ctx, err))}
	}
	fail := func(f AdoptionFailure) *AdoptionError {
		return &AdoptionError{Failure: f, Status: resp.StatusCode, err: fmt.Errorf("%s %s: HTTP %d", method, path, resp.StatusCode)}
	}
	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode >= 500:
		return fail(AdoptionUnreachable)
	case resp.StatusCode == http.StatusUnauthorized:
		return fail(AdoptionUnauthorized)
	case resp.StatusCode == http.StatusConflict:
		e := fail(AdoptionConflict)
		var answer struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(data, &answer) == nil && knownConflicts[answer.Code] {
			e.Code = answer.Code
		}
		return e
	default:
		return fail(AdoptionRefused)
	}
	if !json.Valid(data) || !bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		return fail(AdoptionUnreadable)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fail(AdoptionUnreadable)
	}
	return nil
}

// transportCause keeps a cancellation or a deadline reachable by errors.Is and
// drops the transport's text, which can carry the URL.
func transportCause(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	switch {
	case stderr.Is(err, context.Canceled):
		return context.Canceled
	case stderr.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	}
	return stderr.New("transport failure")
}
