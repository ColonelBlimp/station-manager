package adoption

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0021 5F.3 commit 4b2: the adopter (ADR 0090 T1–T4; ADR 0091 and its
   rulings Q2–Q4, A1–A4; the operator's 4b2 acceptance set, 2026-10-09).

     AD1  an attempt: version, the cloud manifest, the judgement and the
          durable reservation, the adoption request with its six fields, then
          the confirmation under the station account.
     AD2  no request at all off Home, with no catalogued archive, for a
          missing, disabled or deleted default binding, with an incomplete
          account, or for a binding already confirmed under this account.
     AD3  no adoption request after a refused or failed reservation: an unsafe
          verdict (blocked when already reserved), a failed reservation write,
          an unreadable local archive, a pin that moved.
     AD4  recovery through a fresh judgement and an idempotent adoption: a lost
          response, a failed local confirmation, a restart with a reservation.
     AD5  a moved default or a changed account never inherits an attempt's
          status, a late completion included; the completion is discarded.
     AD6  terminal outcomes (401, unreadable, refused, each 409, unsupported)
          stay suppressed across unchanged checks, an unrelated config edit
          and the adopter's own reservation timestamp; each relevant change
          (account, binding name, default, eligibility) rearms them; transient
          outcomes and unsafe verdicts are retried at the next check.
     AD7  each status's text; an uncertain outcome or a recorded cloud success
          is kept through later transient failures; "checking" or
          "confirming" only while an attempt runs.
     AD8  the loop: a check at once, then hourly; it stays alive after a
          success and while nothing is eligible; shutdown cancels the request
          in flight and returns.
     AD9  each status transition is logged once.
*/

const (
	homeID = "019fd5c5-efcc-7193-be4f-1fee532ee3a1"
	token  = "tok-adopter-secret"
	qso1   = "019fd5c5-0000-7000-8000-000000000001"
	qso2   = "019fd5c5-0000-7000-8000-000000000002"
)

// ---- the fake cloud --------------------------------------------------------

// fakeCloud answers the adoption protocol. A forced status per path overrides
// the answer; the adoption keeps its first mapping, as the server does.
type fakeCloud struct {
	t   *testing.T
	srv *httptest.Server

	mu        sync.Mutex
	requests  []string
	protocol  any // nil: absent from /v1/version
	version   string
	forced    map[string]int
	uuids     []string // under "shack"; nil when the name is absent
	mapped    *smcloud.AdoptRequest
	bodies    []smcloud.AdoptRequest
	conflict  string
	dropReply bool   // the next adoption commits, then the connection drops
	badReply  string // each adoption commits, then answers 200 with this body
	hook      func(path string)
}

func newFakeCloud(t *testing.T) *fakeCloud {
	c := &fakeCloud{t: t, protocol: 1, forced: map[string]int{}, uuids: []string{qso1, qso2}}
	c.srv = httptest.NewServer(http.HandlerFunc(c.serve))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *fakeCloud) set(fn func(c *fakeCloud)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn(c)
}

func (c *fakeCloud) seen() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.requests)
}

func (c *fakeCloud) adopted() *smcloud.AdoptRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mapped
}

func (c *fakeCloud) adoptions() []smcloud.AdoptRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.bodies)
}

func (c *fakeCloud) serve(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	c.requests = append(c.requests, r.Method+" "+r.URL.Path)
	hook, status := c.hook, c.forced[r.URL.Path]
	c.mu.Unlock()
	if hook != nil {
		hook(r.URL.Path)
	}
	if status != 0 {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"code":"forced"}`)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	switch r.URL.Path {
	case "/v1/version":
		if c.version != "" {
			_, _ = io.WriteString(w, c.version)
			return
		}
		body := map[string]any{"version": "test"}
		if c.protocol != nil {
			body["identity_protocol"] = c.protocol
		}
		_ = json.NewEncoder(w).Encode(body)
	case "/v1/logbooks":
		books := []map[string]any{{"id": 3, "name": "main"}}
		if c.uuids != nil {
			books = append(books, map[string]any{"id": 7, "name": "shack"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"logbooks": books})
	case "/v1/logbooks/7/manifest":
		var entries []map[string]any
		for _, u := range c.uuids {
			entries = append(entries, map[string]any{"uuid": u, "modified_at": "2026-10-01T00:00:00Z", "deleted": false})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"entries": entries})
	case "/v1/archives/adopt":
		c.adopt(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (c *fakeCloud) adopt(w http.ResponseWriter, r *http.Request) {
	var req smcloud.AdoptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.t.Errorf("adopt body: %v", err)
	}
	c.bodies = append(c.bodies, req)
	if c.conflict != "" {
		w.WriteHeader(http.StatusConflict)
		_, _ = fmt.Fprintf(w, `{"code":%q,"message":"m"}`, c.conflict)
		return
	}
	if c.mapped != nil && *c.mapped != req {
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"code":"logbook_mapping_conflict","message":"m"}`)
		return
	}
	changed := c.mapped == nil
	c.mapped = &req
	if c.badReply != "" {
		_, _ = io.WriteString(w, c.badReply)
		return
	}
	if c.dropReply {
		c.dropReply = false
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			c.t.Errorf("hijack: %v", err)
			return
		}
		_ = conn.Close()
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"archive_uuid": req.ArchiveUUID, "logbook_uuid": req.LogbookUUID, "changed": changed})
}

// ---- the station -----------------------------------------------------------

// spyArchive is the real archive manager, counting judgements and able to
// fail a reservation or a confirmation.
type spyArchive struct {
	m          *archive.Manager
	mu         sync.Mutex
	judgements int
	reserveErr error
	recordErr  error // once
}

func (s *spyArchive) ReserveAdoption(ctx context.Context, pin archive.AdoptionPin, judge func(context.Context) (string, error)) (archive.ReserveOutcome, string, error) {
	s.mu.Lock()
	err := s.reserveErr
	s.mu.Unlock()
	if err != nil {
		return "", "", err
	}
	return s.m.ReserveAdoption(ctx, pin, func(ctx context.Context) (string, error) {
		s.mu.Lock()
		s.judgements++
		s.mu.Unlock()
		return judge(ctx)
	})
}

func (s *spyArchive) RecordAdoption(ctx context.Context, pin archive.AdoptionPin) (bool, error) {
	s.mu.Lock()
	err := s.recordErr
	s.recordErr = nil
	s.mu.Unlock()
	if err != nil {
		return false, err
	}
	return s.m.RecordAdoption(ctx, pin)
}

func (s *spyArchive) judged() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.judgements
}

type station struct {
	t      *testing.T
	db     *sqlite.Service
	cfgSvc *config.Service
	arch   *spyArchive
	cloud  *fakeCloud
	a      *Adopter
	main   int64
	second int64
	logs   *syncBuffer
}

type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func openArchive(t *testing.T) *sqlite.Service {
	t.Helper()
	cfg := config.DefaultConfig(t.TempDir())
	cfg.Datastore.Path = ":memory:"
	cfg.Logging.FileLogging = false
	cfgSvc := config.New(cfg)
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatal(err)
	}
	logSvc := &logging.Service{ConfigService: cfgSvc, WorkingDir: cfgSvc.WorkingDir()}
	if err := logSvc.Initialize(); err != nil {
		t.Fatal(err)
	}
	db := &sqlite.Service{ConfigService: cfgSvc, LoggerService: logSvc}
	if err := db.Initialize(); err != nil {
		t.Fatal(err)
	}
	db.DatabaseConfig = &types.DatastoreConfig{Driver: "sqlite", Path: ":memory:", MaxOpenConns: 1, MaxIdleConns: 1, ContextTimeout: 10, TransactionContextTimeout: 10}
	db.SetMigrationSets(sqlite.MigrationSetLog)
	if err := db.Open(); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(); _ = logSvc.Close() })
	return db
}

func (s *station) exec(q string, args ...any) {
	s.t.Helper()
	tx, cancel, err := s.db.BeginTxContext(context.Background())
	if err != nil {
		s.t.Fatal(err)
	}
	defer cancel()
	if _, err := tx.ExecContext(context.Background(), q, args...); err != nil {
		_ = tx.Rollback()
		s.t.Fatalf("%s: %v", q, err)
	}
	if err := tx.Commit(); err != nil {
		s.t.Fatal(err)
	}
}

// newStation: Home active, its default logbook "Main" (M0ABC) holding qso1
// and a soft-deleted qso2, bound to SM Cloud under "shack" and enabled; a
// second logbook unbound; the station account pointing at the fake cloud.
func newStation(t *testing.T) *station {
	t.Helper()
	ctx := context.Background()
	s := &station{t: t, db: openArchive(t), cloud: newFakeCloud(t), logs: &syncBuffer{}}
	var err error
	if s.main, err = s.db.InsertLogbookWithContext(ctx, types.Logbook{Name: "Main", Callsign: "M0ABC"}); err != nil {
		t.Fatal(err)
	}
	if s.second, err = s.db.InsertLogbookWithContext(ctx, types.Logbook{Name: "Second", Callsign: "M0XYZ"}); err != nil {
		t.Fatal(err)
	}
	s.exec(`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
		VALUES (?, 'smcloud', 'cloud', 1, '{"logbook":"shack"}')`, s.main)
	for i, u := range []string{qso1, qso2} {
		var deleted any
		if i == 1 {
			deleted = "2026-10-08 06:00:00"
		}
		s.exec(`INSERT INTO qso (id, uuid, call, band, mode, freq, qso_date, time_on, time_off,
			rst_sent, rst_rcvd, country, dedupe_key, logbook_id, deleted_at)
			VALUES (?,?,'EA1B','40m','SSB',7050000,'20250508','0845','0845','59','59','Test',?,?,?)`,
			i+1, u, fmt.Sprintf("%064d", i+1), s.main, deleted)
	}

	tmp := t.TempDir()
	cfg := config.DefaultConfig(filepath.Join(tmp, "data"))
	cfg.Logging.FileLogging = false
	if err := os.MkdirAll(filepath.Join(tmp, "cfg"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(tmp, "cfg", "config.json")
	if _, err := config.WriteJSON(path, cfg); err != nil {
		t.Fatal(err)
	}
	s.cfgSvc = config.New(cfg)
	s.cfgSvc.SetPath(path)
	if err := s.cfgSvc.Initialize(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.cfgSvc.Update(func(c *config.Config) error {
		c.QsoArchives = []types.QsoArchiveConfig{{ID: homeID, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: "/x/home.db"}}
		c.ActiveQsoArchiveID = homeID
		c.DefaultLogbookID = s.main
		c.Forwarders = []types.ForwarderConfig{{Type: smcloud.Type, Credentials: s.account(token)}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	logger := logging.NewForWriter(s.logs)
	m := archive.NewManager(s.cfgSvc, logger, func(string) bool { return true })
	running := s.bindings()
	m.SetActiveBindings(s.db, running)
	s.arch = &spyArchive{m: m}
	s.a = New(s.cfgSvc, s.arch, s.db, running, logger)
	m.SetAdoptionStatus(s.a.Status)
	return s
}

func (s *station) account(tok string) json.RawMessage {
	raw, _ := json.Marshal(map[string]string{"url": s.cloud.srv.URL, "token": tok})
	return raw
}

func (s *station) update(fn func(c *config.Config)) {
	s.t.Helper()
	if _, err := s.cfgSvc.Update(func(c *config.Config) error { fn(c); return nil }); err != nil {
		s.t.Fatal(err)
	}
}

func (s *station) rotate(tok string) {
	s.update(func(c *config.Config) { c.Forwarders[0].Credentials = s.account(tok) })
}

func (s *station) bindings() []types.LogbookDestination {
	s.t.Helper()
	list, err := s.db.ListLogbookDestinationsWithContext(context.Background())
	if err != nil {
		s.t.Fatal(err)
	}
	return list
}

func (s *station) binding(logbookID int64) types.LogbookDestination {
	s.t.Helper()
	for _, b := range s.bindings() {
		if b.LogbookID == logbookID && b.Destination == smcloud.Type {
			return b
		}
	}
	s.t.Fatalf("no SM Cloud binding on logbook %d", logbookID)
	return types.LogbookDestination{}
}

func (s *station) account_() string {
	s.t.Helper()
	fp, err := archive.CurrentAccount(s.cfgSvc.Snapshot(), smcloud.Type, homeID)
	if err != nil {
		s.t.Fatal(err)
	}
	return fp
}

func (s *station) confirmed(logbookID int64) bool {
	s.t.Helper()
	return archive.AdoptionConfirmed(s.binding(logbookID), s.account_())
}

func (s *station) check() { s.a.check(context.Background()) }

func (s *station) wantStatus(state, message string) {
	s.t.Helper()
	got := s.a.Status()
	if got.State != state || got.Message != message {
		s.t.Fatalf("status = %q %q; want %q %q", got.State, got.Message, state, message)
	}
}

// view is what the bindings view shows on a logbook's SM Cloud row.
func (s *station) view(logbookID int64) *types.BindingAdoptionView {
	s.t.Helper()
	v, err := s.arch.m.Bindings(context.Background(), homeID)
	if err != nil {
		s.t.Fatal(err)
	}
	for _, d := range v.Destinations {
		for _, row := range d.Logbooks {
			if d.Type == smcloud.Type && row.LogbookID == logbookID {
				return row.Adoption
			}
		}
	}
	return nil
}

const (
	msgUnsupported = "Not yet: the server does not support archive identity."
	msgUnreachable = "Not yet: the server could not be reached (retrying)."
	msgUncertain   = "Adoption outcome uncertain: no confirmation was received from the server. Retrying."
	msgUnauth      = "Not adopted: the token was refused."
	msgUnreadable  = "Not adopted: the server's answer could not be read."
	msgUnsafe      = "Not adopted: the legacy cloud logbook cannot be matched safely to Home's default logbook; manual recovery is required."
	msgBlocked     = "Adoption confirmation blocked: the legacy cloud logbook can no longer be matched safely to Home's default logbook; manual recovery is required."
	msgLocal       = "Not yet: the local archive could not be read (retrying)."
	msgRecord      = "Cloud adoption succeeded; local confirmation could not be saved. Retrying."
	msgChecking    = "Checking whether the legacy cloud logbook can be adopted."
	msgConfirming  = "Confirming the adoption for the current station account."

	keptUncertain = "Adoption outcome remains uncertain."
	keptRecord    = "Cloud adoption succeeded; local confirmation could not be saved."
)

var fullAttempt = []string{"GET /v1/version", "GET /v1/logbooks", "GET /v1/logbooks/7/manifest", "POST /v1/archives/adopt"}

// ---- AD1 -------------------------------------------------------------------

func TestAdopter_AD1_AnAttemptAdoptsAndConfirms(t *testing.T) {
	s := newStation(t)
	s.check()
	if got := s.cloud.seen(); !slices.Equal(got, fullAttempt) {
		t.Fatalf("requests = %v; want %v", got, fullAttempt)
	}
	lb := s.binding(s.main)
	want := smcloud.AdoptRequest{LegacyName: "shack", ArchiveUUID: homeID, ArchiveLabel: "Home",
		LogbookUUID: s.logbookUUID(s.main), LogbookName: "Main", Callsign: "M0ABC"}
	if got := s.cloud.adoptions(); len(got) != 1 || got[0] != want {
		t.Fatalf("adoption = %+v; want %+v", got, want)
	}
	if !s.confirmed(s.main) || lb.AdoptionReservedAt == nil {
		t.Fatalf("binding after the attempt: %+v; want reserved and confirmed under the account", lb)
	}
	s.wantStatus("", "")
	if v := s.view(s.main); v == nil || v.State != archive.AdoptionStateAdoptedRestart {
		t.Fatalf("view = %+v; want adopted, applies after a restart", v)
	}
	// Confirmed: the next check sends nothing.
	s.check()
	if got := s.cloud.seen(); len(got) != len(fullAttempt) {
		t.Fatalf("a confirmed binding was checked again: %v", got)
	}
}

func (s *station) logbookUUID(id int64) string {
	s.t.Helper()
	books, err := s.db.FetchAllLogbooksWithContext(context.Background())
	if err != nil {
		s.t.Fatal(err)
	}
	for _, b := range books {
		if b.ID == id {
			return b.UUID
		}
	}
	s.t.Fatalf("no logbook %d", id)
	return ""
}

// ---- AD2 -------------------------------------------------------------------

func TestAdopter_AD2_NoRequestWhenNothingIsEligible(t *testing.T) {
	for name, setup := range map[string]func(s *station){
		"off Home": func(s *station) {
			s.update(func(c *config.Config) {
				c.QsoArchives[0].Ownership, c.QsoArchives[0].Path = types.QsoArchiveOwnershipManaged, ""
			})
		},
		"no catalogued archive": func(s *station) {
			s.update(func(c *config.Config) { c.QsoArchives, c.ActiveQsoArchiveID = nil, "" })
		},
		"no binding on the default": func(s *station) {
			s.update(func(c *config.Config) { c.DefaultLogbookID = s.second })
		},
		"a disabled default binding": func(s *station) {
			s.exec(`UPDATE logbook_destination SET enabled = 0 WHERE forwarder_name = 'cloud'`)
		},
		"a deleted default logbook": func(s *station) {
			s.exec(`UPDATE logbook SET deleted_at = '2026-10-09 00:00:00' WHERE id = ?`, s.main)
		},
		"an incomplete account": func(s *station) {
			s.update(func(c *config.Config) { c.Forwarders[0].Credentials = json.RawMessage(`{"url":"https://c"}`) })
		},
		"no account": func(s *station) {
			s.update(func(c *config.Config) { c.Forwarders = nil })
		},
		"confirmed under this account": func(s *station) {
			s.exec(`UPDATE logbook_destination SET adoption_reserved_at = datetime('now'), remote_adopted_at = datetime('now'), remote_adopted_account = ?
				WHERE forwarder_name = 'cloud'`, s.account_())
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := newStation(t)
			setup(s)
			s.check()
			if got := s.cloud.seen(); len(got) != 0 {
				t.Fatalf("requests = %v; want none", got)
			}
			s.wantStatus("", "")
		})
	}
}

// ---- AD3 -------------------------------------------------------------------

func TestAdopter_AD3_NoAdoptionAfterARefusedOrFailedReservation(t *testing.T) {
	noAdopt := []string{"GET /v1/version", "GET /v1/logbooks", "GET /v1/logbooks/7/manifest"}
	t.Run("unsafe: a cloud QSO outside the default logbook", func(t *testing.T) {
		s := newStation(t)
		s.cloud.set(func(c *fakeCloud) { c.uuids = append(c.uuids, "019fd5c5-0000-7000-8000-0000000000ff") })
		s.check()
		if got := s.cloud.seen(); !slices.Equal(got, noAdopt) {
			t.Fatalf("requests = %v; want %v", got, noAdopt)
		}
		s.wantStatus("unsafe", msgUnsafe)
		if b := s.binding(s.main); b.AdoptionReservedAt != nil || b.RemoteAdoptedAt != nil {
			t.Fatalf("an unsafe verdict wrote %+v", b)
		}
		if !strings.Contains(s.logs.String(), "019fd5c5-0000-7000-8000-0000000000ff") {
			t.Fatalf("the reason is not logged: %s", s.logs.String())
		}
	})
	t.Run("unsafe while reserved: blocked", func(t *testing.T) {
		s := newStation(t)
		s.exec(`UPDATE logbook_destination SET adoption_reserved_at = datetime('now') WHERE forwarder_name = 'cloud'`)
		s.cloud.set(func(c *fakeCloud) { c.uuids = append(c.uuids, "019fd5c5-0000-7000-8000-0000000000ff") })
		s.check()
		if got := s.cloud.seen(); !slices.Equal(got, noAdopt) {
			t.Fatalf("requests = %v; want %v", got, noAdopt)
		}
		s.wantStatus("blocked", msgBlocked)
	})
	t.Run("a failed reservation write", func(t *testing.T) {
		s := newStation(t)
		s.arch.reserveErr = fmt.Errorf("disk full")
		s.check()
		if got := s.cloud.seen(); !slices.Equal(got, noAdopt) {
			t.Fatalf("requests = %v; want %v", got, noAdopt)
		}
		s.wantStatus("local_unreadable", msgLocal)
	})
	t.Run("an unreadable local archive", func(t *testing.T) {
		s := newStation(t)
		s.exec(`UPDATE logbook_destination SET credentials = '{"logbook":"other"}' WHERE forwarder_name = 'cloud'`)
		s.exec(`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
			VALUES (?, 'smcloud', 'cloud2', 0, '[')`, s.second)
		s.exec(`UPDATE logbook_destination SET credentials = '{"logbook":"shack"}' WHERE forwarder_name = 'cloud'`)
		s.exec(`DROP TABLE qso_upload`)
		s.check()
		if got := s.cloud.seen(); !slices.Equal(got, noAdopt) {
			t.Fatalf("requests = %v; want %v", got, noAdopt)
		}
		s.wantStatus("local_unreadable", msgLocal)
	})
	t.Run("the pin moved before the reservation", func(t *testing.T) {
		s := newStation(t)
		s.cloud.set(func(c *fakeCloud) {
			c.hook = func(path string) {
				if path == "/v1/logbooks/7/manifest" {
					s.rotate("rotated-mid-attempt")
				}
			}
		})
		s.check()
		if got := s.cloud.seen(); !slices.Equal(got, noAdopt) {
			t.Fatalf("requests = %v; want %v", got, noAdopt)
		}
		if s.arch.judged() != 0 {
			t.Fatal("the judge ran for a moved pin")
		}
		s.wantStatus("", "")
	})
}

// ---- AD4 -------------------------------------------------------------------

func TestAdopter_AD4_RecoveryThroughAFreshJudgementAndAnIdempotentAdoption(t *testing.T) {
	t.Run("a lost response", func(t *testing.T) {
		s := newStation(t)
		s.cloud.set(func(c *fakeCloud) { c.dropReply = true })
		s.check()
		s.wantStatus("uncertain", msgUncertain)
		if b := s.binding(s.main); b.AdoptionReservedAt == nil || b.RemoteAdoptedAt != nil {
			t.Fatalf("after a lost response: %+v; want reserved, not confirmed", b)
		}
		s.check()
		s.wantRecovered(2)
	})
	t.Run("a failed local confirmation", func(t *testing.T) {
		s := newStation(t)
		s.arch.recordErr = fmt.Errorf("disk full")
		s.check()
		s.wantStatus("record_failed", msgRecord)
		if s.confirmed(s.main) {
			t.Fatal("confirmed although the write failed")
		}
		s.check()
		s.wantRecovered(2)
	})
	t.Run("a restart with a reservation", func(t *testing.T) {
		s := newStation(t)
		s.exec(`UPDATE logbook_destination SET adoption_reserved_at = '2026-10-08 12:00:00' WHERE forwarder_name = 'cloud'`)
		s.cloud.set(func(c *fakeCloud) {
			c.mapped = &smcloud.AdoptRequest{LegacyName: "shack", ArchiveUUID: homeID, ArchiveLabel: "Home",
				LogbookUUID: s.logbookUUID(s.main), LogbookName: "Main", Callsign: "M0ABC"}
		})
		s.check()
		s.wantRecovered(1)
		if b := s.binding(s.main); b.AdoptionReservedAt.UTC().Format(time.DateTime) != "2026-10-08 12:00:00" {
			t.Fatalf("the first reservation was not kept: %v", b.AdoptionReservedAt)
		}
	})
}

// wantRecovered: confirmed after `attempts` judgements and adoptions, every
// adoption the same request.
func (s *station) wantRecovered(attempts int) {
	s.t.Helper()
	if !s.confirmed(s.main) {
		s.t.Fatalf("not confirmed after the retry: %+v", s.binding(s.main))
	}
	if got := s.arch.judged(); got != attempts {
		s.t.Fatalf("judgements = %d; want %d (a fresh one per attempt)", got, attempts)
	}
	bodies := s.cloud.adoptions()
	if len(bodies) != attempts {
		s.t.Fatalf("adoptions = %d; want %d", len(bodies), attempts)
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			s.t.Fatalf("the retry sent %+v; the first sent %+v", b, bodies[0])
		}
	}
	s.wantStatus("", "")
}

// ---- AD5 -------------------------------------------------------------------

func TestAdopter_AD5_NoInheritedStatus(t *testing.T) {
	t.Run("the default moves during the attempt: the late completion is discarded", func(t *testing.T) {
		s := newStation(t)
		s.exec(`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
			VALUES (?, 'smcloud', 'cloud2', 1, '{"logbook":"portable"}')`, s.second)
		s.cloud.set(func(c *fakeCloud) {
			c.hook = func(path string) {
				if path == "/v1/archives/adopt" {
					s.update(func(c *config.Config) { c.DefaultLogbookID = s.second })
				}
			}
		})
		s.check()
		if b := s.binding(s.main); b.RemoteAdoptedAt != nil {
			t.Fatalf("the late completion was recorded: %+v", b)
		}
		if v := s.view(s.second); v != nil {
			t.Fatalf("the new default shows %+v", v)
		}
		if v := s.view(s.main); v != nil {
			t.Fatalf("the old default shows %+v", v)
		}
	})
	t.Run("the account changes during the attempt", func(t *testing.T) {
		s := newStation(t)
		s.cloud.set(func(c *fakeCloud) {
			c.hook = func(path string) {
				if path == "/v1/archives/adopt" {
					s.rotate("rotated")
				}
			}
		})
		s.check()
		if b := s.binding(s.main); b.RemoteAdoptedAt != nil {
			t.Fatalf("the completion under the old account was recorded: %+v", b)
		}
		if v := s.view(s.main); v != nil {
			t.Fatalf("the row shows %+v under the new account", v)
		}
		s.cloud.set(func(c *fakeCloud) { c.hook = nil })
		s.check()
		if !s.confirmed(s.main) {
			t.Fatal("not confirmed under the new account at the next check")
		}
	})
	t.Run("a terminal status is not inherited by the next account", func(t *testing.T) {
		s := newStation(t)
		s.cloud.set(func(c *fakeCloud) { c.forced["/v1/logbooks"] = http.StatusUnauthorized })
		s.check()
		if v := s.view(s.main); v == nil || v.State != "unauthorized" {
			t.Fatalf("view = %+v; want unauthorized", v)
		}
		s.rotate("fixed")
		if v := s.view(s.main); v != nil {
			t.Fatalf("the new account inherited %+v", v)
		}
	})
}

// ---- AD6 -------------------------------------------------------------------

func TestAdopter_AD6_TerminalOutcomesWaitForARelevantChange(t *testing.T) {
	terminal := map[string]struct {
		set   func(c *fakeCloud)
		state string
	}{
		"401":                    {set: func(c *fakeCloud) { c.forced["/v1/logbooks"] = 401 }, state: "unauthorized"},
		"an unreadable version":  {set: func(c *fakeCloud) { c.version = "<html>" }, state: "unreadable"},
		"a 404 manifest":         {set: func(c *fakeCloud) { c.forced["/v1/logbooks/7/manifest"] = 404 }, state: "refused"},
		"a 400 adoption":         {set: func(c *fakeCloud) { c.forced["/v1/archives/adopt"] = 400 }, state: "refused"},
		"409 adopted elsewhere":  {set: func(c *fakeCloud) { c.conflict = "legacy_archive_adopted_elsewhere" }, state: "conflict"},
		"409 archive in use":     {set: func(c *fakeCloud) { c.conflict = "archive_uuid_in_use" }, state: "conflict"},
		"409 mapping conflict":   {set: func(c *fakeCloud) { c.conflict = "logbook_mapping_conflict" }, state: "conflict"},
		"no identity protocol":   {set: func(c *fakeCloud) { c.protocol = nil }, state: "unsupported"},
		"identity protocol zero": {set: func(c *fakeCloud) { c.protocol = 0 }, state: "unsupported"},
	}
	for name, tc := range terminal {
		t.Run(name+": suppressed while nothing relevant changes", func(t *testing.T) {
			s := newStation(t)
			s.cloud.set(tc.set)
			s.check()
			s.wantState(tc.state)
			n := len(s.cloud.seen())
			s.check()
			s.update(func(c *config.Config) { c.Logging.Level = "debug" })
			s.check()
			s.exec(`UPDATE logbook_destination SET adoption_reserved_at = '2020-01-01 00:00:00' WHERE forwarder_name = 'cloud' AND adoption_reserved_at IS NOT NULL`)
			s.check()
			if got := s.cloud.seen(); len(got) != n {
				t.Fatalf("requests after unchanged checks: %v; want none beyond the first %d", got[n:], n)
			}
			s.wantState(tc.state)
		})
	}
	rearms := map[string]func(s *station){
		"the account": func(s *station) { s.rotate("rotated") },
		"the binding's name": func(s *station) {
			s.exec(`UPDATE logbook_destination SET credentials = '{"logbook":"shack2"}' WHERE forwarder_name = 'cloud'`)
		},
		"the default": func(s *station) {
			s.exec(`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
				VALUES (?, 'smcloud', 'cloud2', 1, '{"logbook":"portable"}')`, s.second)
			s.update(func(c *config.Config) { c.DefaultLogbookID = s.second })
		},
		"the eligibility": func(s *station) {
			s.exec(`UPDATE logbook_destination SET enabled = 0 WHERE forwarder_name = 'cloud'`)
			s.check()
			s.wantStatus("", "")
			s.exec(`UPDATE logbook_destination SET enabled = 1 WHERE forwarder_name = 'cloud'`)
		},
	}
	for name, change := range rearms {
		t.Run("rearmed by "+name, func(t *testing.T) {
			s := newStation(t)
			s.cloud.set(func(c *fakeCloud) { c.forced["/v1/logbooks"] = 401 })
			s.check()
			n := len(s.cloud.seen())
			change(s)
			s.check()
			if got := s.cloud.seen(); len(got) <= n || got[n] != "GET /v1/version" {
				t.Fatalf("requests after %s changed: %v; want a new attempt", name, got[n:])
			}
		})
	}
	for name, set := range map[string]func(c *fakeCloud){
		"a 503 version":     func(c *fakeCloud) { c.forced["/v1/version"] = 503 },
		"a 500 adoption":    func(c *fakeCloud) { c.forced["/v1/archives/adopt"] = 500 },
		"an unsafe verdict": func(c *fakeCloud) { c.uuids = append(c.uuids, "019fd5c5-0000-7000-8000-0000000000ff") },
	} {
		t.Run("retried at the next check: "+name, func(t *testing.T) {
			s := newStation(t)
			s.cloud.set(set)
			s.check()
			n := len(s.cloud.seen())
			s.check()
			if got := s.cloud.seen(); len(got) != 2*n {
				t.Fatalf("requests = %v; want the attempt repeated", got)
			}
		})
	}
}

func (s *station) wantState(state string) {
	s.t.Helper()
	if got := s.a.Status().State; got != state {
		s.t.Fatalf("state = %q (%q); want %q", got, s.a.Status().Message, state)
	}
}

// ---- AD7 -------------------------------------------------------------------

func TestAdopter_AD7_StatusTexts(t *testing.T) {
	for name, tc := range map[string]struct {
		set            func(c *fakeCloud)
		state, message string
	}{
		"unsupported":          {func(c *fakeCloud) { c.protocol = nil }, "unsupported", msgUnsupported},
		"unreachable":          {func(c *fakeCloud) { c.forced["/v1/logbooks"] = 502 }, "unreachable", msgUnreachable},
		"5xx adoption":         {func(c *fakeCloud) { c.forced["/v1/archives/adopt"] = 503 }, "uncertain", msgUncertain},
		"lost reply":           {func(c *fakeCloud) { c.dropReply = true }, "uncertain", msgUncertain},
		"401":                  {func(c *fakeCloud) { c.forced["/v1/archives/adopt"] = 401 }, "unauthorized", msgUnauth},
		"unreadable":           {func(c *fakeCloud) { c.version = `[1]` }, "unreadable", msgUnreadable},
		"refused 403":          {func(c *fakeCloud) { c.forced["/v1/archives/adopt"] = 403 }, "refused", "Adoption could not be confirmed: the server returned HTTP 403."},
		"conflict":             {func(c *fakeCloud) { c.conflict = "archive_uuid_in_use" }, "conflict", "Not adopted: the adoption conflicts (archive_uuid_in_use)."},
		"conflict, no code":    {func(c *fakeCloud) { c.conflict = "<b>" }, "conflict", "Not adopted: the adoption conflicts."},
		"unsafe":               {func(c *fakeCloud) { c.uuids = append(c.uuids, "019fd5c5-0000-7000-8000-0000000000ff") }, "unsafe", msgUnsafe},
		"a name never pushed":  {func(c *fakeCloud) { c.uuids = nil }, "", ""},
		"an idempotent replay": {func(c *fakeCloud) {}, "", ""},
	} {
		t.Run(name, func(t *testing.T) {
			s := newStation(t)
			s.cloud.set(tc.set)
			s.check()
			s.wantStatus(tc.state, tc.message)
			if v := s.view(s.main); tc.state != "" && (v == nil || v.State != tc.state || v.Message != tc.message) {
				t.Fatalf("view = %+v; want %q %q", v, tc.state, tc.message)
			}
		})
	}
	for _, adoptedElsewhere := range []bool{false, true} {
		t.Run(fmt.Sprintf("checking, or confirming, only while an attempt runs (adopted under another account: %v)", adoptedElsewhere), func(t *testing.T) {
			s := newStation(t)
			if adoptedElsewhere {
				s.exec(`UPDATE logbook_destination SET adoption_reserved_at = datetime('now'), remote_adopted_at = datetime('now'), remote_adopted_account = 'another'
					WHERE forwarder_name = 'cloud'`)
			}
			want := map[bool][2]string{false: {"checking", msgChecking}, true: {"confirming", msgConfirming}}[adoptedElsewhere]
			var during archive.AdoptionStatus
			s.cloud.set(func(c *fakeCloud) {
				c.forced["/v1/logbooks"] = 502
				c.hook = func(path string) {
					if path == "/v1/version" {
						during = s.a.Status()
					}
				}
			})
			s.check()
			if during.State != want[0] || during.Message != want[1] {
				t.Fatalf("during the attempt: %q %q; want %q %q", during.State, during.Message, want[0], want[1])
			}
			s.wantStatus("unreachable", msgUnreachable)
		})
	}
	for name, first := range map[string]struct {
		set                  func(c *fakeCloud)
		arch                 func(a *spyArchive)
		state, message, kept string
	}{
		"an uncertain outcome":     {set: func(c *fakeCloud) { c.dropReply = true }, state: "uncertain", message: msgUncertain, kept: keptUncertain},
		"a recorded cloud success": {arch: func(a *spyArchive) { a.recordErr = fmt.Errorf("disk full") }, state: "record_failed", message: msgRecord, kept: keptRecord},
	} {
		t.Run(name+" is kept through transient failures", func(t *testing.T) {
			s := newStation(t)
			if first.set != nil {
				s.cloud.set(first.set)
			}
			if first.arch != nil {
				first.arch(s.arch)
			}
			s.check()
			s.wantStatus(first.state, first.message)
			var during archive.AdoptionStatus
			s.cloud.set(func(c *fakeCloud) {
				c.forced["/v1/logbooks"] = 502
				c.hook = func(path string) {
					if path == "/v1/version" {
						during = s.a.Status()
					}
				}
			})
			s.check()
			s.wantStatus(first.state, first.message)
			if during.State != first.state {
				t.Fatalf("during the next attempt: %q; want %q kept", during.State, first.state)
			}
			s.cloud.set(func(c *fakeCloud) { delete(c.forced, "/v1/logbooks"); c.hook = nil })
			s.arch.mu.Lock()
			s.arch.reserveErr = fmt.Errorf("disk full")
			s.arch.mu.Unlock()
			s.check()
			s.wantStatus(first.state, first.message)
			// A later refusal does not resolve it either (ruling A5).
			s.arch.mu.Lock()
			s.arch.reserveErr = nil
			s.arch.mu.Unlock()
			s.cloud.set(func(c *fakeCloud) { c.forced["/v1/archives/adopt"] = 401 })
			s.check()
			s.wantStatus(first.state, first.kept+" Confirmation is blocked: the server rejected authentication (HTTP 401).")
		})
	}
}

// ---- AD8 -------------------------------------------------------------------

type fakeClock struct {
	waits chan time.Duration
	fire  chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{waits: make(chan time.Duration), fire: make(chan time.Time)}
}

func (c *fakeClock) after(d time.Duration) <-chan time.Time {
	c.waits <- d
	return c.fire
}

// tick fires the clock the loop is waiting on.
func (c *fakeClock) tick(t *testing.T) {
	t.Helper()
	select {
	case c.fire <- time.Now():
	case <-time.After(5 * time.Second):
		t.Fatal("the loop is not waiting on the clock")
	}
}

// waited waits for the loop to finish a check and wait for the next.
func (c *fakeClock) waited(t *testing.T) {
	t.Helper()
	select {
	case d := <-c.waits:
		if d != time.Hour {
			t.Fatalf("the loop waits %v; want an hour", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not wait for the next check")
	}
}

func TestAdopter_AD8_TheLoop(t *testing.T) {
	if Interval != time.Hour {
		t.Fatalf("Interval = %v; want an hour", Interval)
	}
	t.Run("at once, alive after a success and while nothing is eligible", func(t *testing.T) {
		s := newStation(t)
		s.exec(`UPDATE logbook_destination SET enabled = 0 WHERE forwarder_name = 'cloud'`)
		clock := newFakeClock()
		s.a.after = clock.after
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { s.a.Run(ctx); close(done) }()
		clock.waited(t) // the first check ran at once: nothing eligible
		if got := s.cloud.seen(); len(got) != 0 {
			t.Fatalf("requests = %v; want none", got)
		}
		s.exec(`UPDATE logbook_destination SET enabled = 1 WHERE forwarder_name = 'cloud'`)
		clock.tick(t)
		clock.waited(t)
		if !s.confirmed(s.main) {
			t.Fatal("not adopted once eligible")
		}
		s.rotate("rotated")
		clock.tick(t)
		clock.waited(t)
		if !s.confirmed(s.main) || len(s.cloud.adoptions()) != 2 {
			t.Fatalf("after a success the loop did not confirm the new account (%d adoptions)", len(s.cloud.adoptions()))
		}
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not return after cancel")
		}
	})
	t.Run("shutdown cancels the request in flight", func(t *testing.T) {
		s := newStation(t)
		entered, cancelled := make(chan struct{}), make(chan struct{})
		s.cloud.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(entered)
			<-r.Context().Done()
			close(cancelled)
		})
		clock := newFakeClock()
		s.a.after = clock.after
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { s.a.Run(ctx); close(done) }()
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatal("no request was made")
		}
		cancel()
		for _, ch := range []chan struct{}{cancelled, done} {
			select {
			case <-ch:
			case <-time.After(5 * time.Second):
				t.Fatal("shutdown did not cancel the request and return")
			}
		}
		if b := s.binding(s.main); b.AdoptionReservedAt != nil || b.RemoteAdoptedAt != nil {
			t.Fatalf("a cancelled attempt wrote %+v", b)
		}
	})
}

// ---- AD9 -------------------------------------------------------------------

func TestAdopter_AD9_EachTransitionLoggedOnce(t *testing.T) {
	s := newStation(t)
	s.cloud.set(func(c *fakeCloud) { c.forced["/v1/logbooks"] = 502 })
	s.check()
	s.check()
	s.check()
	if n := strings.Count(s.logs.String(), `"state":"unreachable"`); n != 1 {
		t.Fatalf("unreachable logged %d times; want once:\n%s", n, s.logs.String())
	}
	if n := strings.Count(s.logs.String(), `"state":"checking"`); n != 1 {
		t.Fatalf("checking logged %d times; want once:\n%s", n, s.logs.String())
	}
	s.cloud.set(func(c *fakeCloud) { delete(c.forced, "/v1/logbooks") })
	s.check()
	if !strings.Contains(s.logs.String(), `"state":"adopted"`) {
		t.Fatalf("the adoption is not logged:\n%s", s.logs.String())
	}
	if strings.Contains(s.logs.String(), token) || strings.Contains(s.logs.String(), s.account_()) {
		t.Fatalf("the log carries the token or the account fingerprint:\n%s", s.logs.String())
	}
}

// ---- AD10 ------------------------------------------------------------------

/*
   AD10 (ruling A5, 2026-10-09): a later refusal does not resolve an earlier
   uncertain outcome. The status keeps the uncertainty and names the current
   blocker; suppression still applies to the blocker. Only a recorded
   confirmation, or a 409 to the same subject's adoption request (a mapping in
   effect would answer 200), resolves it; a new subject never shows it.
*/

const blocked = keptUncertain + " Confirmation is blocked: "

func TestAdopter_AD10_ALaterRefusalKeepsTheUncertainty(t *testing.T) {
	t.Run("lost response, then 401, then suppressed while nothing changes", func(t *testing.T) {
		s := newStation(t)
		s.cloud.set(func(c *fakeCloud) { c.dropReply = true })
		s.check()
		s.wantStatus("uncertain", msgUncertain)
		s.cloud.set(func(c *fakeCloud) { c.forced["/v1/logbooks"] = http.StatusUnauthorized })
		s.check()
		want := blocked + "the server rejected authentication (HTTP 401)."
		s.wantStatus("uncertain", want)
		if v := s.view(s.main); v == nil || v.State != "uncertain" || v.Message != want {
			t.Fatalf("view = %+v; want the kept uncertainty and the blocker", v)
		}
		n := len(s.cloud.seen())
		s.check()
		s.update(func(c *config.Config) { c.Logging.Level = "debug" })
		s.check()
		if got := s.cloud.seen(); len(got) != n {
			t.Fatalf("requests after unchanged checks: %v; want none", got[n:])
		}
		s.wantStatus("uncertain", want)
		// The account is fixed: the next check confirms, which resolves it.
		s.cloud.set(func(c *fakeCloud) { delete(c.forced, "/v1/logbooks") })
		s.rotate("fixed")
		s.check()
		if !s.confirmed(s.main) {
			t.Fatal("not confirmed after the fix")
		}
		s.wantStatus("", "")
	})
	for name, tc := range map[string]struct {
		set     func(c *fakeCloud)
		blocker string
	}{
		"an unreadable answer": {func(c *fakeCloud) { c.version = "<html>" }, "the server's answer could not be read."},
		"a refusal":            {func(c *fakeCloud) { c.forced["/v1/logbooks/7/manifest"] = 403 }, "the server returned HTTP 403."},
		"an old server":        {func(c *fakeCloud) { c.protocol = nil }, "the server does not support archive identity."},
		"an unsafe judgement": {func(c *fakeCloud) { c.uuids = append(c.uuids, "019fd5c5-0000-7000-8000-0000000000ff") },
			"the legacy cloud logbook can no longer be matched safely to Home's default logbook; manual recovery is required."},
	} {
		t.Run("lost response, then "+name, func(t *testing.T) {
			s := newStation(t)
			s.cloud.set(func(c *fakeCloud) { c.dropReply = true })
			s.check()
			s.cloud.set(tc.set)
			s.check()
			s.wantStatus("uncertain", blocked+tc.blocker)
		})
	}
	t.Run("a 409 to the same adoption request resolves it", func(t *testing.T) {
		s := newStation(t)
		s.cloud.set(func(c *fakeCloud) { c.forced["/v1/archives/adopt"] = 503 })
		s.check()
		s.wantStatus("uncertain", msgUncertain)
		s.cloud.set(func(c *fakeCloud) {
			delete(c.forced, "/v1/archives/adopt")
			c.conflict = "legacy_archive_adopted_elsewhere"
		})
		s.check()
		s.wantStatus("conflict", "Not adopted: the adoption conflicts (legacy_archive_adopted_elsewhere).")
		// Resolved: once rearmed (an eligibility flip, which keeps an
		// unresolved outcome), a transient failure no longer shows it.
		s.exec(`UPDATE logbook_destination SET enabled = 0 WHERE forwarder_name = 'cloud'`)
		s.check()
		s.exec(`UPDATE logbook_destination SET enabled = 1 WHERE forwarder_name = 'cloud'`)
		s.cloud.set(func(c *fakeCloud) { c.conflict = ""; c.forced["/v1/version"] = 503 })
		s.check()
		s.wantStatus("unreachable", msgUnreachable)
	})
	t.Run("an eligibility flip keeps it", func(t *testing.T) {
		s := newStation(t)
		s.cloud.set(func(c *fakeCloud) { c.dropReply = true })
		s.check()
		s.exec(`UPDATE logbook_destination SET enabled = 0 WHERE forwarder_name = 'cloud'`)
		s.check()
		s.exec(`UPDATE logbook_destination SET enabled = 1 WHERE forwarder_name = 'cloud'`)
		s.cloud.set(func(c *fakeCloud) { c.forced["/v1/version"] = 503 })
		s.check()
		s.wantStatus("uncertain", msgUncertain)
	})
	t.Run("a cloud success kept through a later lost reply", func(t *testing.T) {
		s := newStation(t)
		s.arch.recordErr = fmt.Errorf("disk full")
		s.check()
		s.cloud.set(func(c *fakeCloud) { c.dropReply = true })
		s.check()
		s.wantStatus("record_failed", msgRecord)
	})
	t.Run("a cloud success kept through a 409", func(t *testing.T) {
		s := newStation(t)
		s.arch.recordErr = fmt.Errorf("disk full")
		s.check()
		s.cloud.set(func(c *fakeCloud) { c.conflict = "logbook_mapping_conflict" })
		s.check()
		s.wantStatus("record_failed", keptRecord+" Confirmation is blocked: the adoption conflicts (logbook_mapping_conflict).")
	})
	t.Run("a new subject never shows it", func(t *testing.T) {
		s := newStation(t)
		s.cloud.set(func(c *fakeCloud) { c.dropReply = true })
		s.check()
		s.rotate("rotated")
		if v := s.view(s.main); v != nil {
			t.Fatalf("the new account shows %+v", v)
		}
		s.cloud.set(func(c *fakeCloud) { c.forced["/v1/logbooks"] = http.StatusUnauthorized })
		s.check()
		s.wantStatus("unauthorized", msgUnauth)
	})
}

// ---- AD11 ------------------------------------------------------------------

/*
   AD11 (operator review of 4b2, 2026-10-09):
     1. an unreadable answer to the adoption request (the server may have
        committed) leaves the outcome uncertain, still suppressed as terminal;
     2. only a recognised 409 code resolves an uncertain outcome;
     3. a local read failure is not ineligibility: it keeps the suppression
        and the status.
*/

func TestAdopter_AD11_ReviewFindings(t *testing.T) {
	for name, body := range map[string]string{
		"malformed JSON": `{"archive_uuid":`,
		"a wrong echo":   `{"archive_uuid":"019fd5c5-efcc-7193-be4f-1fee532ee3c3","logbook_uuid":"019fd5c5-efcc-7193-be4f-1fee532ee3c4","changed":true}`,
	} {
		t.Run("committed, then "+name+", then a later refusal", func(t *testing.T) {
			s := newStation(t)
			s.cloud.set(func(c *fakeCloud) { c.badReply = body })
			s.check()
			if s.cloud.adopted() == nil {
				t.Fatal("the fake did not commit the adoption")
			}
			want := blocked + "the server's answer could not be read."
			s.wantStatus("uncertain", want)
			n := len(s.cloud.seen())
			s.check()
			if got := s.cloud.seen(); len(got) != n {
				t.Fatalf("requests after an unchanged check: %v; want none (terminal)", got[n:])
			}
			// Rearmed by an eligibility flip; the next attempt draws a 401.
			s.exec(`UPDATE logbook_destination SET enabled = 0 WHERE forwarder_name = 'cloud'`)
			s.check()
			s.exec(`UPDATE logbook_destination SET enabled = 1 WHERE forwarder_name = 'cloud'`)
			s.cloud.set(func(c *fakeCloud) { c.badReply = ""; c.forced["/v1/logbooks"] = http.StatusUnauthorized })
			s.check()
			s.wantStatus("uncertain", blocked+"the server rejected authentication (HTTP 401).")
			// Fixed: the replay confirms the committed mapping.
			s.cloud.set(func(c *fakeCloud) { delete(c.forced, "/v1/logbooks") })
			s.rotate("fixed")
			s.check()
			if !s.confirmed(s.main) {
				t.Fatal("not confirmed by the replay")
			}
			s.wantStatus("", "")
		})
	}
	t.Run("a 409 without a recognised code keeps the uncertainty", func(t *testing.T) {
		s := newStation(t)
		s.cloud.set(func(c *fakeCloud) { c.dropReply = true })
		s.check()
		s.cloud.set(func(c *fakeCloud) { c.conflict = "something_new" })
		s.exec(`UPDATE logbook_destination SET enabled = 0 WHERE forwarder_name = 'cloud'`)
		s.check()
		s.exec(`UPDATE logbook_destination SET enabled = 1 WHERE forwarder_name = 'cloud'`)
		s.check()
		s.wantStatus("uncertain", blocked+"the adoption conflicts.")
	})
	t.Run("a local read failure keeps the suppression and the status", func(t *testing.T) {
		s := newStation(t)
		s.cloud.set(func(c *fakeCloud) { c.forced["/v1/logbooks"] = http.StatusUnauthorized })
		s.check()
		s.wantStatus("unauthorized", msgUnauth)
		n := len(s.cloud.seen())
		s.exec(`ALTER TABLE logbook_destination RENAME TO logbook_destination_away`)
		s.check()
		s.wantStatus("unauthorized", msgUnauth)
		s.exec(`ALTER TABLE logbook_destination_away RENAME TO logbook_destination`)
		s.check()
		if got := s.cloud.seen(); len(got) != n {
			t.Fatalf("requests after a read failure and recovery: %v; want none (inputs unchanged)", got[n:])
		}
		s.wantStatus("unauthorized", msgUnauth)
	})
}
