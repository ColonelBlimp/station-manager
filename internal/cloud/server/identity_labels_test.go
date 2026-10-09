package server

import (
	"net/http"
	"strings"
	"testing"
)

// W-0021 5F.3, ruling on the Codex review of 14e9b94a (2026-10-09): the
// display labels' limits count Unicode code points, as the local limit and
// the cloud's own column CHECKs do, on the adoption and the identity-upload
// endpoints alike. Labels are stored exactly as sent.
//
//	DL1  33 é (66 bytes) succeed and round-trip unchanged.
//	DL2  64 multibyte characters succeed (2- and 4-byte); a decomposed label
//	     is kept decomposed, never normalized.
//	DL3  65 multibyte characters are refused, with nothing written.
//	DL4  the ASCII boundaries hold: 64 succeed, 65 are refused.
//	DL5  scoped to the labels: the legacy cloud name's and the callsign's
//	     byte limits are unchanged.

var (
	label33e     = strings.Repeat("é", 33)
	label64e     = strings.Repeat("é", 64)
	label64radio = strings.Repeat("📻", 64)
	label64nfd   = strings.Repeat("é", 32) // 64 code points, 96 bytes
	label65e     = strings.Repeat("é", 65)
)

// pushLabels pushes one QSO to a fresh managed archive with the given labels.
func pushLabels(t *testing.T, url, archiveLabel, logbookLabel string) (int, map[string]any) {
	t.Helper()
	var out map[string]any
	resp := do(t, http.MethodPut, url+"/v1/archives/"+contestUUID+"/logbooks/"+contestLB+"/qsos", testToken, identityPut{
		ArchiveLabel: archiveLabel, LogbookLabel: logbookLabel, Callsign: "7Q5MLV",
		Qsos: oneQso(t, "0197f9a0-0000-7000-8000-000000000201", "DL9UW", 1, pushAt)}, &out)
	return resp.StatusCode, out
}

func adoptLabels(archiveLabel, logbookLabel string) map[string]string {
	b := adoptBody("main", homeUUID, defaultLBUID)
	b["archive_label"], b["logbook_name"] = archiveLabel, logbookLabel
	return b
}

func TestDisplayLabels_DL1_DL2_AcceptedAndKeptExactly(t *testing.T) {
	for _, c := range []struct{ name, archive, logbook string }{
		{"33 é", label33e, label33e},
		{"64 é and 64 radios", label64e, label64radio},
		{"64 decomposed code points", label64nfd, label64nfd},
		{"64 ASCII", strings.Repeat("x", 64), strings.Repeat("y", 64)},
	} {
		t.Run("push "+c.name, func(t *testing.T) {
			ts, _, _, db := newTestServer(t, quietLog)
			if status, out := pushLabels(t, ts.URL, c.archive, c.logbook); status != http.StatusOK {
				t.Fatalf("push: %d %v", status, out)
			}
			if a, _ := readArchive(t, db, contestUUID); a.label != c.archive {
				t.Fatalf("archive label = %q; want %q exactly", a.label, c.archive)
			}
			if lb, _ := readLogbook(t, db, contestLB); lb.label != c.logbook {
				t.Fatalf("logbook label = %q; want %q exactly", lb.label, c.logbook)
			}
		})
		t.Run("adopt "+c.name, func(t *testing.T) {
			ts, _, _, db := newTestServer(t, quietLog)
			pushMain(t, ts)
			if status, out := adopt(t, ts.URL, adoptLabels(c.archive, c.logbook)); status != http.StatusOK {
				t.Fatalf("adopt: %d %v", status, out)
			}
			if got := readLegacy(t, db, "main"); got.archiveLabel.String != c.archive || got.label.String != c.logbook {
				t.Fatalf("adopted labels = %q, %q; want %q, %q exactly", got.archiveLabel.String, got.label.String, c.archive, c.logbook)
			}
		})
	}
}

func TestDisplayLabels_DL3_DL4_TooLongRefusedWithNothingWritten(t *testing.T) {
	for _, c := range []struct{ name, archive, logbook string }{
		{"65 é archive label", label65e, "main"},
		{"65 é logbook label", "Contest", label65e},
		{"65 radios logbook label", "Contest", strings.Repeat("📻", 65)},
		{"65 ASCII archive label", strings.Repeat("x", 65), "main"},
		{"65 ASCII logbook label", "Contest", strings.Repeat("y", 65)},
	} {
		t.Run("push "+c.name, func(t *testing.T) {
			ts, _, _, db := newTestServer(t, quietLog)
			if status, out := pushLabels(t, ts.URL, c.archive, c.logbook); status != http.StatusBadRequest || out["code"] != "invalid_field_value" {
				t.Fatalf("push: %d %v; want 400 invalid_field_value", status, out)
			}
			if _, ok := readArchive(t, db, contestUUID); ok {
				t.Fatal("the refused push created its archive")
			}
			if _, ok := readLogbook(t, db, contestLB); ok {
				t.Fatal("the refused push created its logbook")
			}
			if n := scalarRow(t, db, `SELECT count(*) FROM qsos`); n != 0 {
				t.Fatalf("the refused push stored %d QSOs", n)
			}
		})
		t.Run("adopt "+c.name, func(t *testing.T) {
			ts, _, _, db := newTestServer(t, quietLog)
			pushMain(t, ts)
			before := readLegacy(t, db, "main")
			if status, out := adopt(t, ts.URL, adoptLabels(c.archive, c.logbook)); status != http.StatusBadRequest || out["code"] != "invalid_field_value" {
				t.Fatalf("adopt: %d %v; want 400 invalid_field_value", status, out)
			}
			if got := readLegacy(t, db, "main"); got != before {
				t.Fatalf("the refused adoption wrote: %+v; was %+v", got, before)
			}
		})
	}
}

func TestDisplayLabels_DL5_OtherLimitsUnchanged(t *testing.T) {
	ts, _, _, _ := newTestServer(t, quietLog)
	pushMain(t, ts)
	// 17 é: 17 code points, 34 bytes, over the callsign's 32-byte limit.
	b := adoptLabels("Home", "Default")
	b["callsign"] = strings.Repeat("é", 17)
	if status, out := adopt(t, ts.URL, b); status != http.StatusBadRequest {
		t.Fatalf("a 34-byte callsign at adoption: %d %v; want 400", status, out)
	}
	var out map[string]any
	resp := do(t, http.MethodPut, ts.URL+"/v1/archives/"+contestUUID+"/logbooks/"+contestLB+"/qsos", testToken, identityPut{
		Callsign: strings.Repeat("é", 17), Qsos: oneQso(t, "0197f9a0-0000-7000-8000-000000000202", "DL9UW", 1, pushAt)}, &out)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a 34-byte callsign on push: %d %v; want 400", resp.StatusCode, out)
	}
	// 33 é: 33 code points, 66 bytes, over the legacy name's 64-byte limit.
	b = adoptLabels("Home", "Default")
	b["legacy_name"] = label33e
	if status, out := adopt(t, ts.URL, b); status != http.StatusBadRequest {
		t.Fatalf("a 66-byte legacy name: %d %v; want 400", status, out)
	}
}
