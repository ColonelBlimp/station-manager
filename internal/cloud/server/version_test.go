package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// W-0021 5F.2, ruling Q1 (ADR 0088): GET /v1/version advertises the identity
// wire with an integer identity_protocol, unauthenticated, beside the build
// version. It lands in the LAST 5F.2 commit, so no deployed server between
// commits ever claimed support it did not have; a client treats its absence
// as "not identity-ready".
func TestVersion_AdvertisesIdentityProtocol1(t *testing.T) {
	ts := httptest.NewServer(versionServer().Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/v1/version")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var v map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || v["version"] != "test-version" || v["identity_protocol"] != float64(1) {
		t.Fatalf("GET /v1/version: %d %v; want the version and identity_protocol 1", resp.StatusCode, v)
	}
}
