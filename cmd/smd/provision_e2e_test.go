package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/origin"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

/*
   W-0021 Q7 commit 6, against the real cloud server on the dev Postgres
   (operator direction and review 2026-10-19). The cloud holds archive U
   ("Field Day": Contest with a live row and a tombstone, Portable with a live
   row, an empty logbook) and an equal-labelled archive V with its own rows.
   The station is a stopped daemon: Home (legacy, with its summary) and W
   (managed, created by the daemon, pending).

     PE1 PA1: `smd provision --archive U --default-logbook Portable`, for a new
         entry and for an existing active entry whose file is missing. The
         whole working directory compared before and after, before anything
         inspects the file: only U's file appears, the config changes only by
         U's entry (a new one; an existing one leaves config.json byte for
         byte), the summary sidecar only by U's record, and the log only as an
         ordinary log. Both selections hold. Then, read back: U's identity,
         the cloud's logbook UUIDs, labels and callsigns, the default, only
         U's rows, no binding and no upload.
     PE2 PA2, as the operator does it: U activated (a restart onto it), SM
         Cloud enabled on every logbook through the bindings, a restart so
         each binding has its worker, then every row sent again through those
         bindings' routes: the live rows by the manual upload (force), the
         tombstone by the routed delete the reconciler would queue. Every
         acknowledgement is received 1, applied 0; the cloud's archives,
         logbooks, membership and rows are byte for byte what they were
         (display labels may change); the daemon's reconcile reports every
         binding in sync with nothing to heal.
*/

const (
	prV        = "0197f9a0-0000-7000-8000-0000000f2001"
	prVA       = "0197f9a0-0000-7000-8000-0000000f20a1"
	prVB       = "0197f9a0-0000-7000-8000-0000000f20b1"
	prQ5       = "0197f9a0-0000-7000-8000-0000000f2105"
	prQ6       = "0197f9a0-0000-7000-8000-0000000f2106"
	prE2EToken = "tok-e2e"
)

func prAccount(url string) types.ForwarderConfig {
	creds, _ := json.Marshal(map[string]string{"url": url, "token": prE2EToken})
	return types.ForwarderConfig{Name: "smcloud", Type: smcloud.Type, Credentials: creds}
}

// prSeedCloud pushes U's and V's rows through the real identity forwarder and
// adds U's empty logbook to the store.
func prSeedCloud(t *testing.T, cloud *realCloud) {
	t.Helper()
	ctx := context.Background()
	push := func(archiveUUID, logbook, label, callsign string, q types.Qso, act forwarding.Action) {
		f, err := smcloud.NewIdentity(prAccount(cloud.srv.URL), smcloud.IdentityTarget{
			ArchiveUUID: archiveUUID, ArchiveLabel: "Field Day", LogbookUUID: logbook, LogbookLabel: label, Callsign: callsign})
		if err != nil {
			t.Fatal(err)
		}
		if res := f.Submit(ctx, q, act, ""); res.Outcome != forwarding.OutcomeSuccess {
			t.Fatalf("seed %s: %+v", q.UUID, res)
		}
	}
	push(prArchive, prLBA, "Contest", "7Q5MLV", prQso(prQ1, "DL9UW", 3, false), action.Insert)
	push(prArchive, prLBA, "Contest", "7Q5MLV", prQso(prQ2, "EA1B", 3, false), action.Insert)
	push(prArchive, prLBA, "Contest", "7Q5MLV", prQso(prQ2, "EA1B", 4, true), action.Delete)
	push(prArchive, prLBB, "Portable", "7Q5MLV/P", prQso(prQ3, "OH2BH", 1, false), action.Insert)
	push(prV, prVA, "Contest", "7Q5MLV", prQso(prQ5, "K1ABC", 1, false), action.Insert)
	push(prV, prVB, "Portable", "7Q5MLV/P", prQso(prQ6, "JA1XYZ", 1, false), action.Insert)
	if _, err := cloud.db.Exec(`INSERT INTO logbooks (tenant_id, archive_id, uuid, label, callsign)
		SELECT tenant_id, id, $1, 'Empty', '7Q5MLV' FROM archives WHERE archive_uuid = $2`, prLBC, prArchive); err != nil {
		t.Fatalf("add U's empty logbook: %v", err)
	}
}

// prCloudState is the cloud's identities, membership and rows, labels left
// out: what an unchanged push must not change.
func prCloudState(t *testing.T, cloud *realCloud) []string {
	t.Helper()
	var out []string
	for _, q := range []string{
		`SELECT 'archive', archive_uuid::text, legacy::text FROM archives WHERE archive_uuid IS NOT NULL ORDER BY 2`,
		`SELECT 'logbook', a.archive_uuid::text || ' ' || l.uuid::text, l.callsign FROM logbooks l JOIN archives a ON a.id = l.archive_id ORDER BY 2`,
		`SELECT 'qso', q.uuid::text || ' in ' || l.uuid::text,
		        q.revision::text || ' ' || q.modified_at::text || ' ' || coalesce(q.deleted_at::text, 'live') || ' ' || q.payload::text
		   FROM qsos q JOIN logbooks l ON l.id = q.logbook_id ORDER BY 2`,
	} {
		rows, err := cloud.db.Query(q)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var kind, key, val string
			if err := rows.Scan(&kind, &key, &val); err != nil {
				t.Fatal(err)
			}
			out = append(out, kind+" "+key+" "+val)
		}
		_ = rows.Close()
	}
	return out
}

// prAPI sends one request with a JSON body to the generation's API.
func prAPI(t *testing.T, d *daemon, method, path, body string) (int, []byte) {
	t.Helper()
	sock := d.cfgSvc.Snapshot().SocketPath
	client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}
	req, err := http.NewRequest(method, "http://smd"+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// prUploadStatus is a QSO's upload status on one forwarder, tombstones too.
func prUploadStatus(t *testing.T, d *daemon, uuid, forwarder string) string {
	t.Helper()
	ctx := context.Background()
	q, err := d.db.FetchQsoByUUIDIncludingDeletedWithContext(ctx, uuid)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := d.db.FetchUploadsByQsoIDWithContext(ctx, q.ID)
	if err != nil {
		t.Fatal(err)
	}
	status := ""
	for _, r := range rows {
		if r.ForwarderName == forwarder {
			status = string(r.Status)
		}
	}
	return status
}

// prFootprint is the working directory's tree with what a provisioning may
// change taken out and checked: U's new file, config.json (compared as
// config), the summary sidecar (compared by record) and the log directory.
func prFootprint(t *testing.T, root, logDir, uFile, cfgPath, sumsPath string) map[string]string {
	t.Helper()
	tree := prTree(t, root)
	for p := range tree {
		if p == uFile || p == cfgPath || p == sumsPath || p == logDir || strings.HasPrefix(p, logDir+string(filepath.Separator)) {
			delete(tree, p)
		}
	}
	return tree
}

// prE2E is one case's world: the cloud, the stopped station and what PE1
// compares against.
type prE2E struct {
	cloud                               *realCloud
	cloudBefore                         []string
	d                                   *daemon
	existing                            bool
	cfg                                 config.Config
	cfgPath, root, uFile, sumsPath, log string
	cfgBytes                            []byte
	sumsBefore                          archive.Summaries
}

// newPRE2E seeds the cloud and builds the stopped station: Home adopted at the
// first start, W created by the daemon and pending, and for the existing case
// U's entry added as active.
func newPRE2E(t *testing.T, existing bool) *prE2E {
	t.Helper()
	ctx := context.Background()
	e := &prE2E{cloud: newRealCloud(t), existing: existing}
	prSeedCloud(t, e.cloud)
	e.cloudBefore = prCloudState(t, e.cloud)
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
		a := prAccount(e.cloud.srv.URL)
		a.Enabled, a.TickIntervalSec, a.BatchSize, a.ActionFilter = true, 1, 5, []string{"insert", "update", "delete"}
		c.Forwarders = []types.ForwarderConfig{a}
	})
	e.d = d
	ownerOnlySocketDir(t, d)
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatal(err)
	}
	w, err := d.archives.Create(ctx, archive.CreateRequest{RequestKey: utils.NewUUIDv7(), Label: "W", LogbookName: "W", LogbookCallsign: "7Q5MLV"})
	if err != nil {
		t.Fatal(err)
	}
	stopGen(t, orch)
	kept := types.QsoArchiveConfig{ID: prArchive, Label: "Kept label", Ownership: types.QsoArchiveOwnershipManaged, RequestKey: "kept-key", LastActivationError: archive.FailFileMissing}
	if _, err := d.cfgSvc.Update(func(c *config.Config) error {
		c.PendingQsoArchiveID = w.Entry.ID
		if existing {
			c.QsoArchives = append(c.QsoArchives, kept)
			c.ActiveQsoArchiveID = prArchive
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	e.cfgPath = d.cfgSvc.Path
	if e.cfg, err = config.Load(e.cfgPath); err != nil {
		t.Fatal(err)
	}
	e.root = os.Getenv("SM_WORKING_DIR")
	e.uFile = filepath.Join(archive.ManagedDir(e.cfg), prArchive+".db")
	e.sumsPath = archive.SummariesPath(e.cfg)
	e.log = filepath.Join(d.cfgSvc.WorkingDir(), e.cfg.Logging.RelLogFileDir)
	e.cfgBytes, _ = os.ReadFile(e.cfgPath)
	e.sumsBefore, _ = archive.ReadSummaries(e.sumsPath)
	return e
}

func (e *prE2E) footprint(t *testing.T) map[string]string {
	t.Helper()
	return prFootprint(t, e.root, e.log, e.uFile, e.cfgPath, e.sumsPath)
}

// checkFootprint is PE1's bound on the side effects, before anything opens
// U's file; it returns the config as the command left it.
func (e *prE2E) checkFootprint(t *testing.T, treeBefore map[string]string) config.Config {
	t.Helper()
	prSameTree(t, "the provisioning's footprint", treeBefore, e.footprint(t))
	if _, err := os.Stat(e.uFile); err != nil {
		t.Fatalf("U's file: %v", err)
	}
	for _, side := range []string{"-wal", "-shm", ".creating", ".creating-wal", ".creating-shm"} {
		if _, err := os.Stat(e.uFile + side); err == nil {
			t.Fatalf("%s left beside U's file", side)
		}
	}
	after, err := config.Load(e.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	wantCfg := e.cfg
	if e.existing {
		if b, _ := os.ReadFile(e.cfgPath); string(b) != string(e.cfgBytes) {
			t.Fatal("config.json was written; an existing entry needs no catalogue write")
		}
	} else {
		wantCfg.QsoArchives = append(slices.Clone(e.cfg.QsoArchives), types.QsoArchiveConfig{ID: prArchive, Label: "Field Day", Ownership: types.QsoArchiveOwnershipManaged})
	}
	if !reflect.DeepEqual(after, wantCfg) {
		t.Fatalf("config after\n%+v\nwant\n%+v", after, wantCfg)
	}
	sums, _ := archive.ReadSummaries(e.sumsPath)
	if _, ok := sums[prArchive]; !ok {
		t.Fatal("U's summary was not recorded")
	}
	delete(sums, prArchive)
	if !reflect.DeepEqual(sums, e.sumsBefore) {
		t.Fatalf("other summaries changed: %+v; want %+v", sums, e.sumsBefore)
	}
	return after
}

// checkFile is PE1's read-back of U's file; it returns U's logbooks.
func (e *prE2E) checkFile(t *testing.T) []types.Logbook {
	t.Helper()
	ctx := context.Background()
	db := openArchiveFile(t, e.cfg, e.uFile)
	identity, err := db.ArchiveIdentityWithContext(ctx)
	if err != nil || identity.ArchiveUUID != prArchive {
		t.Fatalf("identity %+v (%v); want U", identity, err)
	}
	lbs, err := db.FetchAllLogbooksWithContext(ctx)
	if err != nil || len(lbs) != 3 {
		t.Fatalf("logbooks %+v (%v); want three", lbs, err)
	}
	want := map[string][2]string{prLBA: {"Contest", "7Q5MLV"}, prLBB: {"Portable", "7Q5MLV/P"}, prLBC: {"Empty", "7Q5MLV"}}
	for _, lb := range lbs {
		if w, ok := want[lb.UUID]; !ok || lb.Name != w[0] || lb.Callsign != w[1] {
			t.Fatalf("logbook %+v; want the cloud's %v", lb, want)
		}
		if lb.UUID == prLBB && identity.DefaultLogbookID != lb.ID {
			t.Fatalf("default logbook %d; want Portable's %d", identity.DefaultLogbookID, lb.ID)
		}
	}
	if bindings, err := db.ListLogbookDestinationsWithContext(ctx); err != nil || len(bindings) != 0 {
		t.Fatalf("bindings %v (%v); provisioning creates none", bindings, err)
	}
	_ = db.Close()
	if n := riCount(t, e.uFile, `SELECT COUNT(*) FROM qso_upload`); n != 0 {
		t.Fatalf("%d upload(s) queued; provisioning queues none", n)
	}
	if got := uuidsIn(t, e.uFile); !slices.Equal(got, []string{prQ1, prQ2, prQ3}) {
		t.Fatalf("rows %v; want U's three, V's none", got)
	}
	return lbs
}

// activateAndBind is the operator's path to pushing U: activate it (a restart
// onto it), enable SM Cloud on every logbook through the bindings, restart so
// each binding has its worker. It returns the running generation and each
// logbook's forwarder name.
func (e *prE2E) activateAndBind(t *testing.T, after config.Config, lbs []types.Logbook) (*daemon, map[int64]string) {
	t.Helper()
	ctx := context.Background()
	activated := config.New(after)
	activated.SetPath(e.cfgPath)
	if _, err := activated.Update(func(c *config.Config) error {
		c.ActiveQsoArchiveID, c.PendingQsoArchiveID = prArchive, ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	d2, orch2 := buildOrchestratedDaemon(t, activated, activated.Snapshot())
	if err := orch2.Start(d2.workerCtx); err != nil {
		t.Fatalf("start on U: %v", err)
	}
	waitUntil(t, "the support check", func() bool { return supportStateIn(t, d2) == string(smcloud.SupportSupported) })
	var edits []types.LogbookBindingEdit
	for _, lb := range lbs {
		edits = append(edits, types.LogbookBindingEdit{LogbookID: lb.ID, Enabled: true})
	}
	if _, err := d2.archives.ApplyBindings(ctx, prArchive, types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
		Type: smcloud.Type, Logbooks: edits}}}); err != nil {
		t.Fatalf("enable SM Cloud on U's logbooks: %v", err)
	}
	stopGen(t, orch2)
	d3, orch3 := startGen(t, d2)
	t.Cleanup(func() { stopGen(t, orch3) })

	bindings, err := d3.db.ListLogbookDestinationsWithContext(ctx)
	if err != nil || len(bindings) != 3 {
		t.Fatalf("bindings %+v (%v); want one per logbook", bindings, err)
	}
	nameOf := map[int64]string{}
	for _, b := range bindings {
		if !b.Enabled || b.IdentitySince == nil {
			t.Fatalf("binding %+v; want enabled and born on identity", b)
		}
		nameOf[b.LogbookID] = b.ForwarderName
	}
	return d3, nameOf
}

// pushBack sends every row again through its binding's route: the live rows
// by the manual upload (force), the tombstone by the routed delete. It waits
// for each upload and returns the cloud request mark and each logbook's
// acknowledgement mark from before the sends.
func (e *prE2E) pushBack(t *testing.T, d3 *daemon, lbs []types.Logbook, nameOf map[int64]string) (int, map[string]int) {
	t.Helper()
	ctx := context.Background()
	mark := len(e.cloud.requests())
	ackMark := map[string]int{}
	for _, lb := range lbs {
		ackMark[lb.UUID] = len(e.cloud.answersTo("PUT", "/v1/archives/"+prArchive+"/logbooks/"+lb.UUID+"/qsos"))
	}
	sent := map[string]string{} // qso → forwarder
	for _, lb := range lbs {
		var live, gone []string
		for _, uuid := range []string{prQ1, prQ2, prQ3} {
			q, err := d3.db.FetchQsoByUUIDIncludingDeletedWithContext(ctx, uuid)
			if err != nil || q.LogbookID != lb.ID {
				continue
			}
			if q.DeletedAt.IsZero() {
				live = append(live, uuid)
			} else {
				gone = append(gone, uuid)
			}
			sent[uuid] = nameOf[lb.ID]
		}
		if len(live) > 0 {
			body, _ := json.Marshal(map[string]any{"uuids": live, "force": true})
			if code, out := prAPI(t, d3, http.MethodPost, "/v1/forwarder/"+nameOf[lb.ID]+"/uploads", string(body)); code != http.StatusOK {
				t.Fatalf("manual upload to %s: %d %s", nameOf[lb.ID], code, out)
			}
		}
		if len(gone) > 0 {
			if res, err := d3.qso.EnqueueDeleteUploads(ctx, nameOf[lb.ID], gone, origin.Reconcile); err != nil || res.Enqueued != len(gone) {
				t.Fatalf("routed delete to %s: %+v (%v)", nameOf[lb.ID], res, err)
			}
		}
	}
	for uuid, name := range sent {
		waitUntil(t, "the upload of "+uuid, func() bool { return prUploadStatus(t, d3, uuid, name) == "uploaded" })
	}
	return mark, ackMark
}

// checkUnchanged is PE2's verdict on the pushes: every acknowledgement applied
// 0 for all three rows, nothing on the name wire, the cloud as it was, and
// the daemon's reconcile in sync on every binding.
func (e *prE2E) checkUnchanged(t *testing.T, d3 *daemon, lbs []types.Logbook, mark int, ackMark map[string]int) {
	t.Helper()
	acks := 0
	for _, lb := range lbs {
		answers := e.cloud.answersTo("PUT", "/v1/archives/"+prArchive+"/logbooks/"+lb.UUID+"/qsos")[ackMark[lb.UUID]:]
		for _, a := range answers {
			var ack struct {
				Received int  `json:"received"`
				Applied  *int `json:"applied"`
			}
			if a.status != http.StatusOK || json.Unmarshal([]byte(a.body), &ack) != nil || ack.Applied == nil || *ack.Applied != 0 {
				t.Fatalf("logbook %s: acknowledgement %d %s; want applied 0", lb.Name, a.status, a.body)
			}
			acks += ack.Received
		}
	}
	if acks != 3 {
		t.Fatalf("%d row(s) acknowledged; want every row, 3", acks)
	}
	for _, r := range e.cloud.requests()[mark:] {
		if strings.HasPrefix(r, "PUT /v1/qsos") {
			t.Fatalf("a row took the name wire: %s", r)
		}
	}
	if got := prCloudState(t, e.cloud); !slices.Equal(got, e.cloudBefore) {
		t.Fatalf("the cloud changed:\nbefore %q\nafter  %q", e.cloudBefore, got)
	}
	code, results, body := reconcileAll(t, d3)
	if code != http.StatusOK || len(results) != 3 {
		t.Fatalf("reconcile = %d %s; want one result per binding", code, body)
	}
	for _, r := range results {
		var sum smcloud.ReconcileSummary
		if r.Error != "" || json.Unmarshal(r.Summary, &sum) != nil || !sum.InSync ||
			sum.EnqueuedUpserts+sum.EnqueuedDeletes+sum.CloudOnly+sum.CloudNewer != 0 {
			t.Fatalf("reconcile %s = %s (%s); want in sync with nothing to heal", r.ForwarderName, r.Summary, r.Error)
		}
	}
}

func TestProvisionE2E_PE1_PE2_ProvisionThenPushBackUnchanged(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing active entry %v", existing), func(t *testing.T) {
			e := newPRE2E(t, existing)
			treeBefore := e.footprint(t)

			code, stdout, stderr := prRun(t, "--config", e.cfgPath, "--archive", prArchive, "--default-logbook", prLBB)
			if code != 0 {
				t.Fatalf("provision exit %d\nstdout %s\nstderr %s", code, stdout, stderr)
			}
			if strings.Contains(stdout+stderr, "DL9UW") {
				t.Fatal("a value reached the command's output")
			}

			// PE1: the footprint, before anything opens the file; then the file.
			after := e.checkFootprint(t, treeBefore)
			lbs := e.checkFile(t)

			// PE2: activate, bind, push every row back, and check nothing changed.
			d3, nameOf := e.activateAndBind(t, after, lbs)
			mark, ackMark := e.pushBack(t, d3, lbs, nameOf)
			e.checkUnchanged(t, d3, lbs, mark, ackMark)
		})
	}
}
