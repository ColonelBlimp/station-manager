package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// W-0024 slice 4: loadgen's submits carry what the daemon now requires — the
// archive it serves and the default logbook it serves, both read once from
// /v1/version at startup.

const (
	lgArchive = "01a163a7-0f04-7f69-85e9-255664b58d11"
	lgDefault = "01a187bc-0173-7649-8ff3-71db8170c309"
)

func versionServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/version" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestServedIdentity_ReadsTheArchiveAndTheDefaultLogbook(t *testing.T) {
	srv := versionServer(t, `{"instance":"i1","archive":{"id":"`+lgArchive+`","label":"Home","ownership":"legacy","default_logbook_uuid":"`+lgDefault+`"}}`)
	got, err := servedIdentity(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArchiveID != lgArchive || got.DefaultLogbook != lgDefault {
		t.Fatalf("identity = %+v; want archive %s and default %s", got, lgArchive, lgDefault)
	}
}

// A daemon that names no default (an older daemon, or none configured) would
// refuse every submit; loadgen says so at startup instead of measuring refusals.
func TestServedIdentity_RefusesATargetWithNoDefault(t *testing.T) {
	srv := versionServer(t, `{"instance":"i1","archive":{"id":"`+lgArchive+`","label":"Home","ownership":"legacy"}}`)
	if got, err := servedIdentity(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatalf("identity = %+v, nil error; want an error naming the missing default", got)
	}
}

func TestDoRequest_SubmitCarriesBothHeadersAndReadsCarryNeither(t *testing.T) {
	type seen struct{ archive, def []string }
	var got []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, seen{r.Header.Values("X-SM-Archive-Id"), r.Header.Values("X-SM-Default-Logbook")})
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)
	results := make(chan result, 2)
	id := targetIdentity{ArchiveID: lgArchive, DefaultLogbook: lgDefault}
	doRequest(context.Background(), srv.Client(), http.MethodPost, srv.URL+"/v1/qso?logbook=1", "application/x-adif", "<EOR>", id, "submit", results)
	doRequest(context.Background(), srv.Client(), http.MethodGet, srv.URL+"/v1/enrich/K1ABC", "", "", targetIdentity{}, "enrich", results)
	if len(got) != 2 {
		t.Fatalf("requests = %d; want 2", len(got))
	}
	if len(got[0].archive) != 1 || got[0].archive[0] != lgArchive || len(got[0].def) != 1 || got[0].def[0] != lgDefault {
		t.Fatalf("submit headers: archive %v default %v; want exactly %s and %s", got[0].archive, got[0].def, lgArchive, lgDefault)
	}
	if len(got[1].archive) != 0 || len(got[1].def) != 0 {
		t.Fatalf("enrich headers: archive %v default %v; want neither", got[1].archive, got[1].def)
	}
}
