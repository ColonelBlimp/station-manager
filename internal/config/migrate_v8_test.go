package config

import (
	"encoding/json"
	"strings"
	"testing"
)

// v8 (W-0029 part 3, criteria A1): an archive activation has an identity.
// `pending_qso_archive_activation_id` is written with the pending selector by the
// switch request; `qso_archive_activation_id` is the activation in effect, set by
// the start that processes one. The 7→8 step stamps the version and adds nothing.

const v7CatalogueDoc = `{"version":7,"data_dir":"/tmp/x","setup_complete":true,"default_logbook_id":1,
	"logging_station":{"station_callsign":"G4ABC"},
	"active_qso_archive_id":"0190aaaa-0000-7000-8000-000000000001",%s
	"qso_archives":[
	 {"id":"0190aaaa-0000-7000-8000-000000000001","label":"Home","ownership":"managed"},
	 {"id":"0190aaaa-0000-7000-8000-000000000002","label":"Contest","ownership":"managed"}]}`

func v7Doc(extra string) string { return strings.Replace(v7CatalogueDoc, "%s", extra, 1) }

func TestMigrateV7toV8_StampsTheVersionAndAddsNoIdentity(t *testing.T) {
	path := writeTempConfig(t, v7Doc(""))
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Version != 8 || CurrentSchemaVersion() != 8 {
		t.Fatalf("version: loaded %d, current %d; want 8", cfg.Version, CurrentSchemaVersion())
	}
	if cfg.QsoArchiveActivationID != "" || cfg.PendingQsoArchiveActivationID != "" || cfg.PendingActivationIdentity() != "" {
		t.Fatalf("an upgrade with nothing pending has identities: in effect %q, pending %q, derived %q",
			cfg.QsoArchiveActivationID, cfg.PendingQsoArchiveActivationID, cfg.PendingActivationIdentity())
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"qso_archive_activation_id", "pending_qso_archive_activation_id"} {
		if strings.Contains(string(b), key) {
			t.Fatalf("an empty identity is written as %q: %s", key, b)
		}
	}
}

// A pending selector written before v8 has no identity. It is processed under one
// derived from the archive it names: the same on every load of the same document,
// so a migration that never becomes durable cannot turn one activation into two
// (codex-214). The stored field stays empty; the identity is derived, not invented.
func TestPendingActivationIdentity_AnOldSelectorGetsTheSameIdentityOnEveryLoad(t *testing.T) {
	doc := v7Doc(`"pending_qso_archive_id":"0190aaaa-0000-7000-8000-000000000002",`)
	first, err := Load(writeTempConfig(t, doc))
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	second, err := Load(writeTempConfig(t, doc)) // the migrated form was never persisted
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	a, b := first.PendingActivationIdentity(), second.PendingActivationIdentity()
	if a == "" || a != b {
		t.Fatalf("the identities of two loads of one old document are %q and %q; want one, the same", a, b)
	}
	if !strings.Contains(a, "0190aaaa-0000-7000-8000-000000000002") {
		t.Fatalf("the derived identity %q does not name the archive the selector names", a)
	}
	if first.PendingQsoArchiveActivationID != "" {
		t.Fatalf("the load invented a stored identity: %q", first.PendingQsoArchiveActivationID)
	}

	// A selector naming another archive is another activation.
	other, err := Load(writeTempConfig(t, v7Doc(`"pending_qso_archive_id":"0190aaaa-0000-7000-8000-000000000001",`)))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if other.PendingActivationIdentity() == a {
		t.Fatalf("selectors naming different archives derive the same identity %q", a)
	}
}

// A selector written at v8 carries its identity, and that is the one used.
func TestPendingActivationIdentity_AStoredIdentityIsTheOneUsed(t *testing.T) {
	doc := strings.Replace(v7Doc(`"pending_qso_archive_id":"0190aaaa-0000-7000-8000-000000000002",
		"pending_qso_archive_activation_id":"0190bbbb-0000-7000-8000-00000000000a",
		"qso_archive_activation_id":"0190bbbb-0000-7000-8000-000000000009",`), `"version":7`, `"version":8`, 1)
	cfg, err := Load(writeTempConfig(t, doc))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.PendingActivationIdentity(); got != "0190bbbb-0000-7000-8000-00000000000a" {
		t.Fatalf("pending identity = %q, want the stored one", got)
	}
	if cfg.QsoArchiveActivationID != "0190bbbb-0000-7000-8000-000000000009" {
		t.Fatalf("activation in effect = %q, want the stored one", cfg.QsoArchiveActivationID)
	}
}

// An identity for a pending activation with no pending selector describes nothing,
// and is refused at load as a pending selector naming no archive is.
func TestValidate_APendingIdentityNeedsItsSelector(t *testing.T) {
	doc := strings.Replace(v7Doc(`"pending_qso_archive_activation_id":"0190bbbb-0000-7000-8000-00000000000a",`), `"version":7`, `"version":8`, 1)
	if _, err := Load(writeTempConfig(t, doc)); err == nil || !strings.Contains(err.Error(), "pending_qso_archive_activation_id") {
		t.Fatalf("load = %v; want a refusal naming pending_qso_archive_activation_id", err)
	}
}

// The identities are new keys at v8: a build at 7 refuses a file that holds them,
// and this build refuses a file from a newer one, as it always has.
func TestLoad_AFileNewerThanThisBuildIsRefused(t *testing.T) {
	doc := strings.Replace(v7Doc(""), `"version":7`, `"version":9`, 1)
	if _, err := Load(writeTempConfig(t, doc)); err == nil {
		t.Fatal("a version 9 file was loaded by a build at 8")
	}
}
