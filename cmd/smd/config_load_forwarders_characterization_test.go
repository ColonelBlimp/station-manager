package main

// Characterization (W-0021 5C, commit a): loading a version-5 config.json with
// all four destinations TODAY. The fixture is explicit JSON, shaped like the
// station's file — QRZ, ClubLog and SM Cloud enabled, QRZCQ disabled — with
// synthetic, distinct credentials and non-default station settings. It runs in
// cmd/smd, where every forwarder package is linked as in the daemon, so
// config.Load validates and fills defaults exactly as startup does.
//
//   L1  KEPT     — the file loads; the four entries, and only they, come back.
//   L2  CHANGES  — each keeps its `name` and `enabled` state. 5C strips both from
//                  the file after Home's seed commits (they are binding facts).
//   L3  CHANGES  — every credential is preserved exactly, logbook-scoped keys
//                  included. 5C strips the logbook-scoped keys after the seed;
//                  the station-scoped ones (SM Cloud url, token) stay.
//   L4  KEPT     — station settings survive: label, action_filter, endpoints,
//                  tick, batch, retry and allow_insecure_http.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

const v5StationForwardersDoc = `{"version":5,"data_dir":"%DIR%","setup_complete":true,"default_logbook_id":1,
	"logging_station":{"station_callsign":"G4ABC"},
	"forwarders":[
		{"name":"qrz-home","type":"qrz","label":"QRZ (home)","enabled":true,"action_filter":["insert","update"],
		 "credentials":{"api_key":"QRZ-LOAD-KEY-51"}},
		{"name":"clublog-home","type":"clublog","enabled":true,"action_filter":["insert","delete"],
		 "tick_interval_sec":45,"batch_size":7,
		 "credentials":{"email":"clublog-load@example.test","password":"CL-LOAD-PASS-53","callsign":"CL-LOAD-CALL-59"}},
		{"name":"qrzcq-home","type":"qrzcq","enabled":false,"action_filter":["insert"],
		 "endpoints":{"insert":"https://qrzcq-load.example.test/api"},
		 "credentials":{"call":"QCQ-LOAD-CALL-61","key":"QCQ-LOAD-KEY-67"}},
		{"name":"cloud-home","type":"smcloud","enabled":true,"action_filter":["insert","update","delete"],
		 "allow_insecure_http":true,"retry":{"max_attempts":9,"initial_backoff_sec":11,"max_backoff_sec":601},
		 "credentials":{"url":"http://smc-load.example.test","token":"SMC-LOAD-TOKEN-71","logbook":"cloud-load-book"}}
	]}`

func TestCharacterize_LoadV5StationForwarders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	doc := strings.ReplaceAll(v5StationForwardersDoc, "%DIR%", dir)
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("L1: load: %v", err)
	}

	byName := map[string]types.ForwarderConfig{}
	for _, fc := range cfg.Forwarders {
		byName[fc.Name] = fc
	}
	if len(cfg.Forwarders) != 4 || len(byName) != 4 {
		names := make([]string, 0, len(cfg.Forwarders))
		for _, fc := range cfg.Forwarders {
			names = append(names, fc.Name+"/"+fc.Type)
		}
		t.Fatalf("L1: loaded %v; want exactly the four entries", names)
	}

	want := map[string]struct {
		typ     string
		enabled bool
		creds   map[string]string
	}{
		"qrz-home":     {"qrz", true, map[string]string{"api_key": "QRZ-LOAD-KEY-51"}},
		"clublog-home": {"clublog", true, map[string]string{"email": "clublog-load@example.test", "password": "CL-LOAD-PASS-53", "callsign": "CL-LOAD-CALL-59"}},
		"qrzcq-home":   {"qrzcq", false, map[string]string{"call": "QCQ-LOAD-CALL-61", "key": "QCQ-LOAD-KEY-67"}},
		"cloud-home":   {"smcloud", true, map[string]string{"url": "http://smc-load.example.test", "token": "SMC-LOAD-TOKEN-71", "logbook": "cloud-load-book"}},
	}
	for name, w := range want {
		fc, ok := byName[name]
		if !ok || fc.Type != w.typ || fc.Enabled != w.enabled {
			t.Fatalf("L2 %s: got %+v (present %v); want type %s enabled %v", name, fc, ok, w.typ, w.enabled)
		}
		var creds map[string]string
		if err := json.Unmarshal(fc.Credentials, &creds); err != nil {
			t.Fatalf("L3 %s: credentials: %v", name, err)
		}
		if !reflect.DeepEqual(creds, w.creds) {
			t.Fatalf("L3 %s: credentials %v; want %v exactly", name, creds, w.creds)
		}
	}

	// L4: station settings.
	if got := byName["qrz-home"]; got.Label != "QRZ (home)" || !reflect.DeepEqual(got.ActionFilter, []string{"insert", "update"}) {
		t.Fatalf("L4 qrz: label %q action_filter %v", got.Label, got.ActionFilter)
	}
	if got := byName["clublog-home"]; got.TickIntervalSec != 45 || got.BatchSize != 7 || !reflect.DeepEqual(got.ActionFilter, []string{"insert", "delete"}) {
		t.Fatalf("L4 clublog: tick %d batch %d action_filter %v", got.TickIntervalSec, got.BatchSize, got.ActionFilter)
	}
	if got := byName["qrzcq-home"]; got.Endpoints["insert"] != "https://qrzcq-load.example.test/api" {
		t.Fatalf("L4 qrzcq: endpoints %v", got.Endpoints)
	}
	got := byName["cloud-home"]
	if !got.AllowInsecureHTTP || got.Retry == nil || *got.Retry != (types.RetryConfig{MaxAttempts: 9, InitialBackoffSec: 11, MaxBackoffSec: 601}) ||
		!reflect.DeepEqual(got.ActionFilter, []string{"insert", "update", "delete"}) {
		t.Fatalf("L4 smcloud: allow_insecure_http %v retry %+v action_filter %v", got.AllowInsecureHTTP, got.Retry, got.ActionFilter)
	}
}
