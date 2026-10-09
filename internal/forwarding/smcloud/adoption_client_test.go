package smcloud

import (
	"context"
	"encoding/json"
	stderr "errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0021 5F.3 commit 4b2: the adoption client (ADR 0090 T2 and T4, ADR 0091
   A3). Each request is bounded, carries the bearer token, and its failure is
   classified by what the answer proves:

     AC1  GET /v1/version: identity_protocol read; absent is 0; an unreadable
          200 is unreadable.
     AC2  the cloud UUIDs under a name: /v1/logbooks, then that logbook's
          manifest, tombstones included; a name the cloud lacks is none, with
          no manifest request.
     AC3  POST /v1/archives/adopt: the six fields; a 200 must echo the archive
          and logbook sent.
     AC4  the classes: no answer, a timeout and 5xx are unreachable; 401
          unauthorized; 409 a conflict (its known code kept); any other status
          refused, with the status.
     AC5  no error text carries the URL, the token or the body.
     AC6  the client is built only from a usable station account.
     AC7  the same against the real cloud server.
*/

const (
	acToken   = "tok-adopt-secret"
	acArchive = "019fd5c5-efcc-7193-be4f-1fee532ee3a1"
	acLogbook = "019fd5c5-efcc-7193-be4f-1fee532ee3b2"
)

func acClient(t *testing.T, url string) *AdoptionClient {
	t.Helper()
	creds, _ := json.Marshal(map[string]string{"url": url, "token": acToken})
	c, err := NewAdoptionClient(types.ForwarderConfig{Type: Type, Credentials: creds})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// acServer answers each request with handle, after checking the bearer token
// and the user agent.
func acServer(t *testing.T, handle func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); r.URL.Path != "/v1/version" && got != "Bearer "+acToken {
			t.Errorf("%s %s: Authorization = %q", r.Method, r.URL.Path, got)
		}
		if r.Header.Get("User-Agent") != UserAgent {
			t.Errorf("%s %s: User-Agent = %q", r.Method, r.URL.Path, r.Header.Get("User-Agent"))
		}
		handle(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func failureOf(t *testing.T, err error) *AdoptionError {
	t.Helper()
	var ae *AdoptionError
	if !stderr.As(err, &ae) {
		t.Fatalf("error %v is not an *AdoptionError", err)
	}
	return ae
}

func reply(status int, body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func TestAdoptionClient_AC1_Version(t *testing.T) {
	for name, tc := range map[string]struct {
		handle func(http.ResponseWriter, *http.Request)
		want   int
		fail   AdoptionFailure
		status int
	}{
		"protocol 1":         {handle: reply(200, `{"version":"v","identity_protocol":1}`), want: 1},
		"absent":             {handle: reply(200, `{"version":"v"}`), want: 0},
		"not JSON":           {handle: reply(200, `<html>`), fail: AdoptionUnreadable, status: 200},
		"not a number":       {handle: reply(200, `{"identity_protocol":"1"}`), fail: AdoptionUnreadable, status: 200},
		"not an object":      {handle: reply(200, `[1]`), fail: AdoptionUnreadable, status: 200},
		"a 503":              {handle: reply(503, `down`), fail: AdoptionUnreachable, status: 503},
		"a 404":              {handle: reply(404, `nope`), fail: AdoptionRefused, status: 404},
		"a 403":              {handle: reply(403, `no`), fail: AdoptionRefused, status: 403},
		"a 401 (unexpected)": {handle: reply(401, `no`), fail: AdoptionUnauthorized, status: 401},
	} {
		t.Run(name, func(t *testing.T) {
			ts := acServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/version" {
					t.Errorf("request %s %s", r.Method, r.URL.Path)
				}
				tc.handle(w, r)
			})
			got, err := acClient(t, ts.URL).IdentityProtocol(context.Background())
			if tc.fail == "" {
				if err != nil || got != tc.want {
					t.Fatalf("IdentityProtocol = %d, %v; want %d", got, err, tc.want)
				}
				return
			}
			if ae := failureOf(t, err); ae.Failure != tc.fail || ae.Status != tc.status {
				t.Fatalf("failure = %q HTTP %d; want %q HTTP %d", ae.Failure, ae.Status, tc.fail, tc.status)
			}
		})
	}
}

func TestAdoptionClient_AC2_CloudUUIDs(t *testing.T) {
	list := `{"logbooks":[{"id":3,"name":"main"},{"id":7,"name":"shack"}]}`
	manifest := `{"logbook_id":7,"entries":[
		{"uuid":"019fd5c5-0000-7000-8000-000000000001","modified_at":"2026-10-01T00:00:00Z","revision":1,"deleted":false},
		{"uuid":"019fd5c5-0000-7000-8000-000000000002","modified_at":"2026-10-01T00:00:00Z","revision":2,"deleted":true}]}`
	t.Run("the named logbook's manifest, tombstones included", func(t *testing.T) {
		var paths []string
		ts := acServer(t, func(w http.ResponseWriter, r *http.Request) {
			paths = append(paths, r.Method+" "+r.URL.Path)
			switch r.URL.Path {
			case "/v1/logbooks":
				_, _ = io.WriteString(w, list)
			case "/v1/logbooks/7/manifest":
				_, _ = io.WriteString(w, manifest)
			default:
				w.WriteHeader(500)
			}
		})
		got, err := acClient(t, ts.URL).CloudUUIDs(context.Background(), "shack")
		want := []string{"019fd5c5-0000-7000-8000-000000000001", "019fd5c5-0000-7000-8000-000000000002"}
		if err != nil || !slices.Equal(got, want) {
			t.Fatalf("CloudUUIDs = %v, %v; want %v", got, err, want)
		}
		if !slices.Equal(paths, []string{"GET /v1/logbooks", "GET /v1/logbooks/7/manifest"}) {
			t.Fatalf("requests = %v", paths)
		}
	})
	t.Run("a name the cloud lacks is none, without a manifest request", func(t *testing.T) {
		requests := 0
		ts := acServer(t, func(w http.ResponseWriter, r *http.Request) {
			requests++
			if r.URL.Path != "/v1/logbooks" {
				t.Errorf("unexpected %s", r.URL.Path)
			}
			_, _ = io.WriteString(w, list)
		})
		got, err := acClient(t, ts.URL).CloudUUIDs(context.Background(), "Shack")
		if err != nil || len(got) != 0 || requests != 1 {
			t.Fatalf("CloudUUIDs = %v, %v after %d requests; want none after 1", got, err, requests)
		}
	})
	for name, tc := range map[string]struct {
		list, manifest func(http.ResponseWriter, *http.Request)
		fail           AdoptionFailure
		status         int
	}{
		"list 401":              {list: reply(401, `{}`), fail: AdoptionUnauthorized, status: 401},
		"list 502":              {list: reply(502, ``), fail: AdoptionUnreachable, status: 502},
		"list not JSON":         {list: reply(200, `x`), fail: AdoptionUnreadable, status: 200},
		"list without logbooks": {list: reply(200, `{}`), fail: AdoptionUnreadable, status: 200},
		"manifest 404":          {list: reply(200, list), manifest: reply(404, ``), fail: AdoptionRefused, status: 404},
		"manifest 500":          {list: reply(200, list), manifest: reply(500, ``), fail: AdoptionUnreachable, status: 500},
		"manifest no entries":   {list: reply(200, list), manifest: reply(200, `{"logbook_id":7}`), fail: AdoptionUnreadable, status: 200},
		"manifest blank uuid":   {list: reply(200, list), manifest: reply(200, `{"entries":[{"uuid":" "}]}`), fail: AdoptionUnreadable, status: 200},
	} {
		t.Run(name, func(t *testing.T) {
			ts := acServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/logbooks" {
					tc.list(w, r)
					return
				}
				tc.manifest(w, r)
			})
			_, err := acClient(t, ts.URL).CloudUUIDs(context.Background(), "shack")
			if ae := failureOf(t, err); ae.Failure != tc.fail || ae.Status != tc.status {
				t.Fatalf("failure = %q HTTP %d; want %q HTTP %d", ae.Failure, ae.Status, tc.fail, tc.status)
			}
		})
	}
}

var acRequest = AdoptRequest{
	LegacyName: "shack", ArchiveUUID: acArchive, ArchiveLabel: "Home",
	LogbookUUID: acLogbook, LogbookName: "Main", Callsign: "M0ABC",
}

func TestAdoptionClient_AC3_Adopt(t *testing.T) {
	t.Run("the six fields, and the echo checked", func(t *testing.T) {
		ts := acServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/v1/archives/adopt" {
				t.Errorf("request %s %s", r.Method, r.URL.Path)
			}
			if ct := r.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q", ct)
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("body: %v", err)
			}
			want := map[string]string{"legacy_name": "shack", "archive_uuid": acArchive, "archive_label": "Home",
				"logbook_uuid": acLogbook, "logbook_name": "Main", "callsign": "M0ABC"}
			if len(body) != len(want) {
				t.Errorf("body = %v; want %v", body, want)
			}
			for k, v := range want {
				if body[k] != v {
					t.Errorf("body[%s] = %q; want %q", k, body[k], v)
				}
			}
			// The server echoes UUIDs in canonical lower case.
			_, _ = io.WriteString(w, `{"archive_uuid":"`+acArchive+`","logbook_uuid":"`+acLogbook+`","changed":false}`)
		})
		req := acRequest
		req.ArchiveUUID = strings.ToUpper(acArchive)
		if err := acClient(t, ts.URL).Adopt(context.Background(), req); err != nil {
			t.Fatalf("Adopt = %v; want nil", err)
		}
	})
	other := "019fd5c5-efcc-7193-be4f-1fee532ee3c3"
	for name, tc := range map[string]struct {
		handle func(http.ResponseWriter, *http.Request)
		fail   AdoptionFailure
		status int
		code   string
	}{
		"another archive echoed": {handle: reply(200, `{"archive_uuid":"`+other+`","logbook_uuid":"`+acLogbook+`","changed":true}`), fail: AdoptionUnreadable, status: 200},
		"another logbook echoed": {handle: reply(200, `{"archive_uuid":"`+acArchive+`","logbook_uuid":"`+other+`","changed":true}`), fail: AdoptionUnreadable, status: 200},
		"no echo":                {handle: reply(200, `{}`), fail: AdoptionUnreadable, status: 200},
		"not JSON":               {handle: reply(200, `ok`), fail: AdoptionUnreadable, status: 200},
		"401":                    {handle: reply(401, `{"code":"unauthorized"}`), fail: AdoptionUnauthorized, status: 401},
		"409 adopted elsewhere":  {handle: reply(409, `{"code":"legacy_archive_adopted_elsewhere","message":"m"}`), fail: AdoptionConflict, status: 409, code: "legacy_archive_adopted_elsewhere"},
		"409 archive in use":     {handle: reply(409, `{"code":"archive_uuid_in_use","message":"m"}`), fail: AdoptionConflict, status: 409, code: "archive_uuid_in_use"},
		"409 mapping":            {handle: reply(409, `{"code":"logbook_mapping_conflict","message":"m"}`), fail: AdoptionConflict, status: 409, code: "logbook_mapping_conflict"},
		"409 unknown code":       {handle: reply(409, `{"code":"<script>","message":"m"}`), fail: AdoptionConflict, status: 409},
		"409 unreadable":         {handle: reply(409, `x`), fail: AdoptionConflict, status: 409},
		"400":                    {handle: reply(400, `{"code":"invalid_field_value"}`), fail: AdoptionRefused, status: 400},
		"500":                    {handle: reply(500, `{"code":"internal_error"}`), fail: AdoptionUnreachable, status: 500},
		"302, never followed": {handle: func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/v1/archives/adopt", http.StatusFound)
		}, fail: AdoptionRefused, status: 302},
	} {
		t.Run(name, func(t *testing.T) {
			ts := acServer(t, tc.handle)
			ae := failureOf(t, acClient(t, ts.URL).Adopt(context.Background(), acRequest))
			if ae.Failure != tc.fail || ae.Status != tc.status || ae.Code != tc.code {
				t.Fatalf("failure = %q HTTP %d code %q; want %q HTTP %d code %q", ae.Failure, ae.Status, ae.Code, tc.fail, tc.status, tc.code)
			}
		})
	}
}

func TestAdoptionClient_AC4_NoAnswerAndTimeout(t *testing.T) {
	t.Run("no answer", func(t *testing.T) {
		ts := httptest.NewServer(http.NotFoundHandler())
		url := ts.URL
		ts.Close()
		c := acClient(t, url)
		_, err := c.IdentityProtocol(context.Background())
		if ae := failureOf(t, err); ae.Failure != AdoptionUnreachable || ae.Status != 0 {
			t.Fatalf("version: %q HTTP %d", ae.Failure, ae.Status)
		}
		if ae := failureOf(t, c.Adopt(context.Background(), acRequest)); ae.Failure != AdoptionUnreachable {
			t.Fatalf("adopt: %q", ae.Failure)
		}
	})
	t.Run("each request is bounded", func(t *testing.T) {
		release := make(chan struct{})
		ts := acServer(t, func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-release:
			case <-r.Context().Done():
			case <-time.After(2 * time.Second): // an unbounded request then reads this empty answer
			}
		})
		defer close(release)
		c := acClient(t, ts.URL)
		if c.timeout != AdoptionRequestTimeout || AdoptionRequestTimeout != 10*time.Second {
			t.Fatalf("timeout = %v (constant %v); want 10s", c.timeout, AdoptionRequestTimeout)
		}
		c.timeout = 50 * time.Millisecond
		start := time.Now()
		err := c.Adopt(context.Background(), acRequest)
		if ae := failureOf(t, err); ae.Failure != AdoptionUnreachable || !stderr.Is(err, context.DeadlineExceeded) {
			t.Fatalf("adopt past its bound: %q (%v); want unreachable by deadline", ae.Failure, err)
		}
		if time.Since(start) > time.Second {
			t.Fatal("the request was not bounded")
		}
	})
	t.Run("a cancelled attempt stops its request", func(t *testing.T) {
		ts := acServer(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(20*time.Millisecond, cancel)
		_, err := acClient(t, ts.URL).CloudUUIDs(ctx, "shack")
		if !stderr.Is(err, context.Canceled) {
			t.Fatalf("CloudUUIDs after cancel = %v; want context.Canceled", err)
		}
	})
}

func TestAdoptionClient_AC5_ErrorsNameNoSecret(t *testing.T) {
	t.Run("no answer", func(t *testing.T) {
		ts := httptest.NewServer(http.NotFoundHandler())
		url := ts.URL
		ts.Close()
		_, err := acClient(t, url).IdentityProtocol(context.Background())
		if err == nil || strings.Contains(err.Error(), strings.TrimPrefix(url, "http://")) {
			t.Fatalf("a transport failure's text carries the address: %v", err)
		}
	})
	ts := acServer(t, reply(400, `{"code":"x","message":"`+acToken+`"}`))
	c := acClient(t, ts.URL)
	errs := []error{c.Adopt(context.Background(), acRequest)}
	_, err := c.IdentityProtocol(context.Background())
	errs = append(errs, err)
	_, err = c.CloudUUIDs(context.Background(), "shack")
	errs = append(errs, err)
	for _, err := range errs {
		if err == nil {
			t.Fatal("want an error")
		}
		msg := err.Error()
		if strings.Contains(msg, acToken) || strings.Contains(msg, ts.URL) || strings.Contains(msg, strings.TrimPrefix(ts.URL, "http://")) {
			t.Fatalf("error text carries a secret: %q", msg)
		}
		if !strings.Contains(msg, "400") {
			t.Fatalf("error text %q does not name the status", msg)
		}
	}
}

func TestAdoptionClient_AC6_BuiltFromAUsableAccount(t *testing.T) {
	for name, creds := range map[string]string{
		"no url":             `{"token":"t"}`,
		"no token":           `{"url":"https://c"}`,
		"unreadable":         `[`,
		"cleartext remote":   `{"url":"http://cloud.example.org","token":"t"}`,
		"no credentials set": ``,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewAdoptionClient(types.ForwarderConfig{Type: Type, Credentials: json.RawMessage(creds)}); err == nil {
				t.Fatal("built a client from an unusable account")
			}
		})
	}
	c, err := NewAdoptionClient(types.ForwarderConfig{Type: Type, Credentials: json.RawMessage(`{"url":"https://c.example/ ","token":"t"}`)})
	if err != nil || c.base != "https://c.example" {
		t.Fatalf("NewAdoptionClient = %+v, %v; want base https://c.example", c, err)
	}
}

// AC7: the wire against the real cloud server (skips without the dev
// Postgres): the protocol, the UUIDs under a pushed name (tombstones
// included), an adoption and its replay, a mapping conflict's code, and a
// refused token.
func TestAdoptionClient_AC7_AgainstRealCloudServer(t *testing.T) {
	cloud := newCloudStack(t)
	ctx := context.Background()
	creds, _ := json.Marshal(map[string]string{"url": cloud.URL, "token": "tok-e2e"})
	c, err := NewAdoptionClient(types.ForwarderConfig{Type: Type, Credentials: creds})
	if err != nil {
		t.Fatal(err)
	}
	if p, err := c.IdentityProtocol(ctx); err != nil || p != 1 {
		t.Fatalf("IdentityProtocol = %d, %v; want 1", p, err)
	}
	if got, err := c.CloudUUIDs(ctx, "shack"); err != nil || len(got) != 0 {
		t.Fatalf("CloudUUIDs before any push = %v, %v; want none", got, err)
	}
	fwdCreds, _ := json.Marshal(map[string]string{"url": cloud.URL, "token": "tok-e2e", "logbook": "shack"})
	f, err := New(types.ForwarderConfig{Name: "smcloud", Type: Type, Credentials: fwdCreds})
	if err != nil {
		t.Fatal(err)
	}
	live, gone := testQso("019fd5c5-0000-7000-8000-0000000000a1"), testQso("019fd5c5-0000-7000-8000-0000000000a2")
	gone.DeletedAt = gone.ModifiedAt
	if res := f.Submit(ctx, live, action.Insert, ""); res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("push: %v", res.Err)
	}
	if res := f.Submit(ctx, gone, action.Delete, ""); res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("push tombstone: %v", res.Err)
	}
	got, err := c.CloudUUIDs(ctx, "shack")
	slices.Sort(got)
	if err != nil || !slices.Equal(got, []string{live.UUID, gone.UUID}) {
		t.Fatalf("CloudUUIDs = %v, %v; want both, the tombstone included", got, err)
	}
	req := AdoptRequest{LegacyName: "shack", ArchiveUUID: acArchive, ArchiveLabel: "Home", LogbookUUID: acLogbook, LogbookName: "Main", Callsign: "M0ABC"}
	if err := c.Adopt(ctx, req); err != nil {
		t.Fatalf("Adopt = %v", err)
	}
	if err := c.Adopt(ctx, req); err != nil {
		t.Fatalf("Adopt replay = %v; want the same confirmation", err)
	}
	other := req
	other.LogbookUUID = "019fd5c5-efcc-7193-be4f-1fee532ee3c3"
	if ae := failureOf(t, c.Adopt(ctx, other)); ae.Failure != AdoptionConflict || ae.Code != "logbook_mapping_conflict" {
		t.Fatalf("a second mapping: %q code %q; want conflict logbook_mapping_conflict", ae.Failure, ae.Code)
	}
	bad, _ := json.Marshal(map[string]string{"url": cloud.URL, "token": "wrong"})
	refused, err := NewAdoptionClient(types.ForwarderConfig{Type: Type, Credentials: bad})
	if err != nil {
		t.Fatal(err)
	}
	_, err = refused.CloudUUIDs(ctx, "shack")
	if ae := failureOf(t, err); ae.Failure != AdoptionUnauthorized {
		t.Fatalf("a refused token: %q; want unauthorized", ae.Failure)
	}
}
