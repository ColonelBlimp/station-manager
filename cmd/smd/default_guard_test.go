package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
)

/*
   W-0024 slice 4 (ADR 0097 §4) through the running daemon and its generations.

     SE1  after a change to B is applied at a restart, a tab that booted on A is
          refused on a live submit (never retargeted) and on an FT8 start (the
          service is never asked); a tab booted on B logs to B and reaches the
          FT8 service; /v1/version names B.
     SE2  while the change to B is pending its restart, /v1/version still names
          A: the effective default, not the target.
     SE3  B applied but config.json could not be written: /v1/version names B,
          the default this generation actually serves.

   Each check reports and continues (Errorf), so one red run shows every gap.
   FT8 is enabled with no bridge and no /v1/ft8/events subscriber, so capture
   never opens and nothing can be armed or keyed.
*/

func (f *changeFixture) liveHeaders(believed int64) map[string]string {
	return map[string]string{"Content-Type": "application/json", "X-SM-Archive-Id": f.archive, "X-SM-Default-Logbook": f.uuid[believed]}
}

func versionDefault(t *testing.T, d *daemon) string {
	t.Helper()
	code, body := apiCall(t, d, http.MethodGet, "/v1/version")
	var v struct {
		Archive struct {
			DefaultLogbookUUID string `json:"default_logbook_uuid"`
		} `json:"archive"`
	}
	if code != http.StatusOK || json.Unmarshal(body, &v) != nil {
		t.Fatalf("GET /v1/version = %d %s", code, body)
	}
	return v.Archive.DefaultLogbookUUID
}

func errCode(body []byte) string {
	var e struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(body, &e)
	return e.Code
}

const cqStartBody = `{"offset_hz":1500,"operating_freq_mhz":14.074}`

func TestDefaultGuard_SE1_AStaleTabAfterAnAppliedChange(t *testing.T) {
	f := newChangeFixture(t)
	f.editDisk(t, func(c *config.Config) { c.Ft8.Enabled = true })
	f.selector(t, f.b)
	d := f.mustStart(t)
	if got := d.cfgSvc.Snapshot().DefaultLogbookID; got != f.b {
		t.Fatalf("fixture: default = %d; want B (%d) applied at start", got, f.b)
	}

	code, body := f.submit(t, d, f.a, "1200") // the stale tab: believes A, logs live to A
	if code != http.StatusConflict || errCode(body) != "default_logbook_mismatch" {
		t.Errorf("stale live submit = %d %s; want 409 default_logbook_mismatch", code, body)
	}
	if a, b := f.qsosIn(t, f.a), f.qsosIn(t, f.b); a != 0 || b != 0 {
		t.Errorf("after the stale submit A holds %d and B %d; want nothing written anywhere", a, b)
	}

	code, body = apiCallHeaders(t, d, http.MethodPost, "/v1/ft8/cq/start", cqStartBody, f.liveHeaders(f.a))
	if code != http.StatusConflict || errCode(body) != "default_logbook_mismatch" {
		t.Errorf("stale FT8 start = %d %s; want 409 default_logbook_mismatch", code, body)
	}
	code, body = apiCallHeaders(t, d, http.MethodPost, "/v1/ft8/cq/start", cqStartBody, f.liveHeaders(f.b))
	if code != http.StatusConflict || errCode(body) != "ft8_tx_not_armed" {
		t.Errorf("fresh tab's FT8 start = %d %s; want the service's 409 ft8_tx_not_armed (TX was never armed)", code, body)
	}

	h := f.liveHeaders(f.b)
	h["Content-Type"] = "application/x-adif"
	code, body = apiCallHeaders(t, d, http.MethodPost, "/v1/qso?logbook="+itoa(f.b), strings.ReplaceAll(submitADIF, "%s", "1201"), h)
	if code != http.StatusCreated {
		t.Fatalf("fresh tab's live submit = %d %s; want 201", code, body)
	}
	if b := f.qsosIn(t, f.b); b != 1 {
		t.Fatalf("B holds %d; want the fresh tab's 1", b)
	}

	if got := versionDefault(t, d); got != f.uuid[f.b] {
		t.Errorf("/v1/version default = %q; want B %s", got, f.uuid[f.b])
	}
}

func TestDefaultGuard_SE2_VersionNamesTheEffectiveDefaultWhileAChangeIsPending(t *testing.T) {
	releaseRoutes(t)
	withRestartPort(t)
	f := newChangeFixture(t)
	d := f.mustStart(t)
	if got := versionDefault(t, d); got != f.uuid[f.a] {
		t.Errorf("/v1/version default before the request = %q; want A %s", got, f.uuid[f.a])
	}
	if code, body := f.ask(t, d, f.b); code != http.StatusAccepted {
		t.Fatalf("request the change = %d %s; want 202", code, body)
	}
	if got := versionDefault(t, d); got != f.uuid[f.a] {
		t.Fatalf("/v1/version default while B is pending = %q; want A %s (B is not effective until the restart)", got, f.uuid[f.a])
	}
}

func TestDefaultGuard_SE3_VersionNamesBAppliedEvenWhenConfigCouldNotBeWritten(t *testing.T) {
	f := newChangeFixture(t)
	f.selector(t, f.b)
	var d *daemon
	f.configDir(t, failingDir, func() { d = f.mustStart(t) })
	if disk := f.disk(t); disk.DefaultLogbookID != f.a {
		t.Fatalf("fixture: config.json's default = %d; want A (the write must have failed)", disk.DefaultLogbookID)
	}
	if got := versionDefault(t, d); got != f.uuid[f.b] {
		t.Fatalf("/v1/version default = %q; want B %s, the default this generation serves", got, f.uuid[f.b])
	}
}
