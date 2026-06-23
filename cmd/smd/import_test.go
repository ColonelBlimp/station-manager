package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// setupImportTestbed wires a tmpdir with a working config.json + a
// pre-migrated sqlite that already has a logbook row at id=1, then
// closes the sqlite handle so the production runImport path can
// reopen it cleanly. Returns the tmpdir; cleanup is registered via
// t.Cleanup.
//
// The tmpdir is exported via SM_WORKING_DIR so runImport's loadConfig
// finds the seeded config.json without a --config flag. Tests
// override DefaultLogbookID / forwarder list via the optional `mut`
// hook before the config is written to disk.
func setupImportTestbed(t *testing.T, mut func(*config.Config)) string {
	t.Helper()
	tmp := t.TempDir()

	cfg := config.DefaultConfig(tmp)
	cfg.SetupComplete = true
	cfg.DefaultLogbookID = 1
	cfg.LoggingStation.StationCallsign = "M0TEST"
	// Disable file-logging so we don't pollute the tmpdir with smd.log
	// (and so the test's stderr stays readable). Stdout-only is fine
	// inside a test.
	cfg.Logging.FileLogging = false
	if mut != nil {
		mut(&cfg)
	}

	cfgPath := filepath.Join(tmp, "config.json")
	if err := config.WriteJSON(cfgPath, cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Open sqlite at the same path the daemon (and runImport) will use,
	// run migrations, insert a logbook row at id=1, close. runImport
	// later reopens this same file.
	cfgSvc := config.New(cfg)
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatalf("config init: %v", err)
	}
	logSvc := &logging.Service{
		WorkingDir:    cfgSvc.WorkingDir(),
		ConfigService: cfgSvc,
	}
	if err := logSvc.Initialize(); err != nil {
		t.Fatalf("logging init: %v", err)
	}
	dbSvc := &sqlite.Service{
		ConfigService: cfgSvc,
		LoggerService: logSvc,
	}
	if err := dbSvc.Initialize(); err != nil {
		t.Fatalf("sqlite init: %v", err)
	}
	if err := dbSvc.Open(); err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	if err := dbSvc.Migrate(); err != nil {
		t.Fatalf("sqlite migrate: %v", err)
	}
	id, err := dbSvc.InsertLogbookWithContext(context.Background(), types.Logbook{
		Name:        "Test",
		Callsign:    "M0TEST",
		Description: "Test logbook",
	})
	if err != nil {
		t.Fatalf("insert logbook: %v", err)
	}
	if id != 1 {
		t.Fatalf("expected logbook id=1, got %d", id)
	}
	if err := dbSvc.Close(); err != nil {
		t.Fatalf("sqlite close: %v", err)
	}
	if err := logSvc.Close(); err != nil {
		t.Fatalf("logger close: %v", err)
	}

	t.Setenv("SM_WORKING_DIR", tmp)
	return tmp
}

// reopenDB reopens the test sqlite for verification AFTER runImport
// has run and closed it. Caller takes ownership of Close via cleanup.
func reopenDB(t *testing.T, tmp string) *sqlite.Service {
	t.Helper()
	// Re-derive cfgSvc from the on-disk config (runImport may have
	// mutated it via the daemon path; in practice it doesn't, but
	// reading is the safe pattern).
	cfg, err := config.Load(filepath.Join(tmp, "config.json"))
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	cfgSvc := config.New(cfg)
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatalf("cfg init: %v", err)
	}
	logSvc := &logging.Service{
		WorkingDir:    cfgSvc.WorkingDir(),
		ConfigService: cfgSvc,
	}
	if err := logSvc.Initialize(); err != nil {
		t.Fatalf("logging init: %v", err)
	}
	dbSvc := &sqlite.Service{
		ConfigService: cfgSvc,
		LoggerService: logSvc,
	}
	if err := dbSvc.Initialize(); err != nil {
		t.Fatalf("sqlite init: %v", err)
	}
	if err := dbSvc.Open(); err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	t.Cleanup(func() {
		_ = dbSvc.Close()
		_ = logSvc.Close()
	})
	return dbSvc
}

// writeADIF writes an ADIF file containing `records`, prefixed with a
// minimal header, into tmp/<name>. Returns the path.
func writeADIF(t *testing.T, tmp, name string, records ...string) string {
	t.Helper()
	header := "Test ADIF header\n<EOH>\n"
	body := ""
	for _, r := range records {
		body += r + "<EOR>\n"
	}
	path := filepath.Join(tmp, name)
	if err := os.WriteFile(path, []byte(header+body), 0o644); err != nil {
		t.Fatalf("write adif: %v", err)
	}
	return path
}

// Minimal QSO record matching the QRZ export field set the importer
// cares about. Includes app_qrzlog_logid + qrzcom_qso_upload_status=Y
// so the QRZ-stamping path is exercised when a QRZ forwarder is
// configured. Uses a 7Q5 portable callsign + KH78 Mzuzu grid mirroring
// the operator's real historical export.
const sampleRecord = `<call:5>M0ZNK<station_callsign:6>M0TEST<band:3>20m<mode:3>USB<freq:6>14.250<qso_date:8>20240615<time_on:4>1253<rst_sent:2>59<rst_rcvd:2>59<country:7>England<my_gridsquare:6>IO91vl<my_antenna:4>Yagi<my_rig:6>FTdx10<app_qrzlog_logid:10>1112032999<qrzcom_qso_upload_status:1>Y<qrzcom_qso_upload_date:8>20240617`

const secondRecord = `<call:5>G4XYZ<station_callsign:6>M0TEST<band:3>40m<mode:2>CW<freq:6>7.0250<qso_date:8>20240616<time_on:4>1430<rst_sent:3>599<rst_rcvd:3>599<country:7>England<my_gridsquare:6>IO91vl<app_qrzlog_logid:10>1112033000<qrzcom_qso_upload_status:1>Y<qrzcom_qso_upload_date:8>20240618`

func TestImport_HappyPath(t *testing.T) {
	tmp := setupImportTestbed(t, nil)
	adifPath := writeADIF(t, tmp, "input.adi", sampleRecord, secondRecord)

	if err := runImport([]string{adifPath}); err != nil {
		t.Fatalf("import: %v", err)
	}

	db := reopenDB(t, tmp)
	qsos, err := db.FetchQsoSliceByLogbookIdWithContext(context.Background(), 1)
	if err != nil {
		t.Fatalf("fetch qsos: %v", err)
	}
	if len(qsos) != 2 {
		t.Fatalf("expected 2 QSOs, got %d", len(qsos))
	}
}

func TestImport_DryRunSkipsDB(t *testing.T) {
	tmp := setupImportTestbed(t, nil)
	adifPath := writeADIF(t, tmp, "input.adi", sampleRecord)

	if err := runImport([]string{"--dry-run", adifPath}); err != nil {
		t.Fatalf("dry-run: %v", err)
	}

	db := reopenDB(t, tmp)
	qsos, err := db.FetchQsoSliceByLogbookIdWithContext(context.Background(), 1)
	if err != nil {
		t.Fatalf("fetch qsos: %v", err)
	}
	if len(qsos) != 0 {
		t.Fatalf("dry-run should not write to DB; got %d QSOs", len(qsos))
	}
}

func TestImport_IsIdempotent(t *testing.T) {
	tmp := setupImportTestbed(t, nil)
	adifPath := writeADIF(t, tmp, "input.adi", sampleRecord)

	if err := runImport([]string{adifPath}); err != nil {
		t.Fatalf("first import: %v", err)
	}
	// Re-importing the same file should produce all duplicates (per
	// the (logbook, dedupe_key) UNIQUE constraint). The summary line
	// goes to stdout; the import returns nil because no records
	// failed.
	if err := runImport([]string{adifPath}); err != nil {
		t.Fatalf("second import: %v", err)
	}

	db := reopenDB(t, tmp)
	qsos, err := db.FetchQsoSliceByLogbookIdWithContext(context.Background(), 1)
	if err != nil {
		t.Fatalf("fetch qsos: %v", err)
	}
	if len(qsos) != 1 {
		t.Fatalf("expected 1 QSO after dedup, got %d", len(qsos))
	}
}

func TestImport_PreservesMyStarFields(t *testing.T) {
	tmp := setupImportTestbed(t, func(c *config.Config) {
		// Operator's CURRENT identity differs from the imported QSO's
		// historical MY_GRIDSQUARE. The import must NOT replace the
		// historical value — per the project's "caller-supplied MY_*
		// fields win" rule.
		c.LoggingStation.StationCallsign = "M0CURRENT"
		c.LoggingStation.MyGridsquare = "IO82"
	})
	adifPath := writeADIF(t, tmp, "input.adi", sampleRecord)

	if err := runImport([]string{adifPath}); err != nil {
		t.Fatalf("import: %v", err)
	}

	db := reopenDB(t, tmp)
	qsos, err := db.FetchQsoSliceByLogbookIdWithContext(context.Background(), 1)
	if err != nil {
		t.Fatalf("fetch qsos: %v", err)
	}
	if len(qsos) != 1 {
		t.Fatalf("expected 1 QSO, got %d", len(qsos))
	}
	q := qsos[0]
	// MY_GRIDSQUARE on the record was "IO91vl"; current config is
	// "IO82". The stored row must reflect the record, not the config.
	if q.LoggingStation.MyGridsquare != "IO91vl" {
		t.Errorf("MyGridsquare overwritten: got %q, want %q (record value)", q.LoggingStation.MyGridsquare, "IO91vl")
	}
	if q.LoggingStation.MyAntenna != "Yagi" {
		t.Errorf("MyAntenna lost: got %q, want %q", q.LoggingStation.MyAntenna, "Yagi")
	}
	if q.LoggingStation.MyRig != "FTdx10" {
		t.Errorf("MyRig lost: got %q, want %q", q.LoggingStation.MyRig, "FTdx10")
	}
}

func TestImport_DefaultNoUploadPreservesLogid(t *testing.T) {
	// A QRZ forwarder is configured, but with no --forward the import must queue
	// NO upload rows (default no-upload — ADR 0022; seeding a historical log must
	// never re-send it). The QRZ LOGID and the historical upload status/date are
	// still preserved on the QSO row as provenance.
	tmp := setupImportTestbed(t, func(c *config.Config) {
		creds, _ := json.Marshal(map[string]string{"api_key": "fake-key"})
		c.Forwarders = []types.ForwarderConfig{{
			Name:        "qrz-main",
			Type:        "qrz",
			Enabled:     true,
			Credentials: creds,
		}}
	})
	adifPath := writeADIF(t, tmp, "input.adi", sampleRecord)

	if err := runImport([]string{adifPath}); err != nil {
		t.Fatalf("import: %v", err)
	}

	db := reopenDB(t, tmp)
	qsos, err := db.FetchQsoSliceByLogbookIdWithContext(context.Background(), 1)
	if err != nil {
		t.Fatalf("fetch qsos: %v", err)
	}
	if len(qsos) != 1 {
		t.Fatalf("expected 1 QSO, got %d", len(qsos))
	}
	qso := qsos[0]

	// Default import queues nothing — the whole point of the fix.
	uploads, err := db.FetchUploadsByQsoIDWithContext(context.Background(), qso.ID)
	if err != nil {
		t.Fatalf("fetch uploads: %v", err)
	}
	if len(uploads) != 0 {
		t.Fatalf("default import must queue no upload rows; got %d", len(uploads))
	}

	// The QRZ LOGID is preserved on the QSO as provenance (not on an upload row).
	if qso.QrzlogLogid != "1112032999" {
		t.Errorf("QrzlogLogid: got %q, want %q", qso.QrzlogLogid, "1112032999")
	}
	// Historical upload status/date round-trip too.
	if qso.QrzComUploadStatus != adif.YesString {
		t.Errorf("QrzComUploadStatus: got %q, want %q", qso.QrzComUploadStatus, adif.YesString)
	}
	if qso.QrzComUploadDate != "20240617" {
		t.Errorf("QrzComUploadDate: got %q, want %q (record's historical value)", qso.QrzComUploadDate, "20240617")
	}
}

// noLogidRecord is an N1MM-style phone QSO with NO QRZ log-id / upload fields.
const noLogidRecord = `<call:5>7Q8AC<station_callsign:5>7Q8AC<band:3>10m<mode:3>SSB<freq:8>28.30000<qso_date:8>20260517<time_on:4>1243<rst_sent:2>59<rst_rcvd:2>59`

func TestImport_ForwardFlagQueuesNamedForwarder(t *testing.T) {
	// --forward qrz-main is the explicit opt-in: the imported QSO gets a
	// 'pending' qso_upload row for that forwarder, which the worker will drain.
	tmp := setupImportTestbed(t, func(c *config.Config) {
		creds, _ := json.Marshal(map[string]string{"api_key": "fake-key"})
		c.Forwarders = []types.ForwarderConfig{{
			Name: "qrz-main", Type: "qrz", Enabled: true, Credentials: creds,
		}}
	})
	adifPath := writeADIF(t, tmp, "input.adi", noLogidRecord)

	if err := runImport([]string{"--forward", "QRZ-MAIN", adifPath}); err != nil { // case-insensitive
		t.Fatalf("import: %v", err)
	}

	db := reopenDB(t, tmp)
	qsos, err := db.FetchQsoSliceByLogbookIdWithContext(context.Background(), 1)
	if err != nil {
		t.Fatalf("fetch qsos: %v", err)
	}
	if len(qsos) != 1 {
		t.Fatalf("expected 1 QSO, got %d", len(qsos))
	}
	uploads, err := db.FetchUploadsByQsoIDWithContext(context.Background(), qsos[0].ID)
	if err != nil {
		t.Fatalf("fetch uploads: %v", err)
	}
	var found bool
	for _, u := range uploads {
		if !strings.EqualFold(u.ForwarderName, "qrz-main") {
			continue
		}
		found = true
		if u.Status != "pending" {
			t.Errorf("status: got %q, want pending (queued for the worker to send)", u.Status)
		}
	}
	if !found {
		t.Fatal("no qrz-main qso_upload row found for the stored QSO (--forward should enqueue it)")
	}
}

func TestImport_ForwardFlag_UnknownForwarderFails(t *testing.T) {
	tmp := setupImportTestbed(t, nil) // no forwarders configured
	adifPath := writeADIF(t, tmp, "input.adi", noLogidRecord)

	err := runImport([]string{"--forward", "nope", adifPath})
	if err == nil {
		t.Fatal("expected an error when --forward names an unconfigured forwarder")
	}
	if !strings.Contains(err.Error(), "no forwarder named") {
		t.Errorf("error = %v, want it to mention 'no forwarder named'", err)
	}
}

// TestNormalizeImportedMode pins the mode massaging the QRZ export relies on:
// USB/LSB split to SSB + SUBMODE, while values that are already valid main
// modes (SSB with no sideband, FT8, CW) pass through untouched. Mirrors the
// five MODE values present in the operator's real 4509-record QRZ export.
func TestNormalizeImportedMode(t *testing.T) {
	cases := []struct{ in, wantMode, wantSub string }{
		{"USB", "SSB", "USB"},
		{"LSB", "SSB", "LSB"},
		{"SSB", "SSB", ""}, // valid main mode, no sideband to recover
		{"FT8", "FT8", ""}, // valid main mode, kept as-is
		{"CW", "CW", ""},
		{"", "", ""}, // empty MODE untouched
	}
	for _, c := range cases {
		var rec adif.Record
		rec.Mode = c.in
		normalizeImportedMode(&rec)
		if rec.Mode != c.wantMode || rec.Submode != c.wantSub {
			t.Errorf("normalizeImportedMode(%q): got mode %q sub %q; want mode %q sub %q",
				c.in, rec.Mode, rec.Submode, c.wantMode, c.wantSub)
		}
	}
}
