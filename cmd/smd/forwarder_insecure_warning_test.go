package main

// ST-4a (A3) — a successfully-built smcloud forwarder whose URL is plain http to a remote
// host (built only because allow_insecure_http is set) triggers a standing startup
// warning naming the forwarder, NOT the URL, and stating that the bearer token + QSO and
// enabled evidence payloads travel in cleartext. An https or loopback URL emits nothing.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

const (
	insecureWarnFragment = "allow_insecure_http"
	urlSentinelHost      = "cloud.leaky.example"
	tokenSentinel        = "tok-sentinel-xyz"
)

func smcloudFwd(t *testing.T, url string, ack bool) types.ForwarderConfig {
	t.Helper()
	creds, err := json.Marshal(map[string]string{"url": url, "token": tokenSentinel})
	if err != nil {
		t.Fatal(err)
	}
	return types.ForwarderConfig{
		Name: "cloud", Type: "smcloud", Enabled: true,
		Credentials: creds, AllowInsecureHTTP: ack,
		// spawnAndCapture skips applyDefaults, so the worker knobs must be explicit
		// (worker.New rejects a zero Tick). Retry is filled from smcloud's registered
		// DefaultRetry.
		TickIntervalSec: 120, BatchSize: 5,
	}
}

func findInsecureWarning(recs []map[string]any) map[string]any {
	for _, rec := range recs {
		if msg, _ := rec["message"].(string); strings.Contains(msg, insecureWarnFragment) {
			return rec
		}
	}
	return nil
}

func TestSpawn_AcknowledgedCleartextSmcloud_WarnsWithoutLeaking(t *testing.T) {
	err, buf := spawnAndCapture(t, []types.ForwarderConfig{
		smcloudFwd(t, "http://"+urlSentinelHost+":8091", true),
	})
	if err != nil {
		t.Fatalf("spawn should succeed for an acknowledged cleartext smcloud forwarder: %v", err)
	}

	rec := findInsecureWarning(buildLogRecords(t, buf))
	if rec == nil {
		t.Fatalf("no cleartext-transport warning emitted\n%s", buf.String())
	}
	if rec["level"] != "warn" {
		t.Errorf("warning level = %v, want warn", rec["level"])
	}
	if rec["forwarder"] != "cloud" {
		t.Errorf("forwarder = %v, want %q", rec["forwarder"], "cloud")
	}
	// A9: neither the URL host nor the token may appear anywhere in the log.
	if s := buf.String(); strings.Contains(s, urlSentinelHost) || strings.Contains(s, tokenSentinel) {
		t.Errorf("startup log leaked the URL host or token:\n%s", s)
	}
}

func TestSpawn_EncryptedOrLoopbackSmcloud_NoWarning(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
		ack  bool
	}{
		{"https remote", "https://" + urlSentinelHost, true},
		{"http loopback", "http://127.0.0.1:8091", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err, buf := spawnAndCapture(t, []types.ForwarderConfig{smcloudFwd(t, tc.url, tc.ack)})
			if err != nil {
				t.Fatalf("spawn should succeed: %v", err)
			}
			if rec := findInsecureWarning(buildLogRecords(t, buf)); rec != nil {
				t.Errorf("unexpected cleartext warning for %s:\n%s", tc.name, buf.String())
			}
		})
	}
}
