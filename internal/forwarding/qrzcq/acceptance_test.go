package qrzcq_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/qrzcq"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/worker"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// TestAcceptance_QRZCQForwarder is the executable feature contract. It stays
// outside package qrzcq so it can use only the same public registration,
// configuration, and Forwarder boundary that the daemon uses in production.
func TestAcceptance_QRZCQForwarder(t *testing.T) {
	t.Run("a configured insert posts JSON-wrapped ADIF and accepts DATA_QUEUED", acceptanceConfiguredInsert)
	t.Run("forwarders for the same account share the ninety-second gate", acceptanceSharedAccountGate)
	t.Run("the seeded destination is insert-only and paced at ninety seconds", acceptanceSeededDestination)
	t.Run("local logging commits before the durable worker uploads", acceptanceDurableWorkerUpload)
}

func acceptanceConfiguredInsert(t *testing.T) {
	type capturedRequest struct {
		method      string
		contentType string
		userAgent   string
		body        []byte
	}
	requests := make(chan capturedRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			http.Error(w, "read failed", http.StatusInternalServerError)
			return
		}
		requests <- capturedRequest{
			method:      r.Method,
			contentType: r.Header.Get("Content-Type"),
			userAgent:   r.Header.Get("User-Agent"),
			body:        body,
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"OK","message":"DATA_QUEUED"}`)
	}))
	defer srv.Close()

	priorUserAgent := qrzcq.UserAgent
	qrzcq.UserAgent = "station-manager/acceptance"
	t.Cleanup(func() { qrzcq.UserAgent = priorUserAgent })
	fwd, err := forwarding.Build(types.ForwarderConfig{
		Name: "qrzcq",
		Type: qrzcq.Type,
		Credentials: json.RawMessage(
			`{"call":"7Q5MLV","key":"test-api-key"}`,
		),
		Endpoints: map[string]string{action.Insert.String(): srv.URL},
	})
	if err != nil {
		t.Fatalf("build registered qrzcq forwarder: %v", err)
	}

	res := fwd.Submit(context.Background(), sampleQSO(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeSuccess || res.Err != nil {
		t.Fatalf("Submit result = %+v, want success", res)
	}
	if got := fwd.AdifPrefix(); got != "" {
		t.Fatalf("AdifPrefix = %q, want empty (QRZCQ has no standard ADIF upload stamp)", got)
	}

	got := <-requests
	if got.method != http.MethodPost {
		t.Errorf("method = %q, want POST", got.method)
	}
	if got.contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got.contentType)
	}
	if got.userAgent != qrzcq.UserAgent {
		t.Errorf("User-Agent = %q, want %q", got.userAgent, qrzcq.UserAgent)
	}

	var payload struct {
		Auth struct {
			Call string `json:"call"`
			Key  string `json:"key"`
		} `json:"auth"`
		Data struct {
			ADIF string `json:"adif"`
		} `json:"data"`
	}
	if err := json.Unmarshal(got.body, &payload); err != nil {
		t.Fatalf("request body is not JSON: %v\n%s", err, got.body)
	}
	if payload.Auth.Call != "7Q5MLV" || payload.Auth.Key != "test-api-key" {
		t.Errorf("auth = %+v, want configured call/key", payload.Auth)
	}
	for _, field := range []string{"<CALL:6>M0TEST", "<STATION_CALLSIGN:6>7Q5MLV", "<EOR>"} {
		if !strings.Contains(payload.Data.ADIF, field) {
			t.Errorf("data.adif missing %q: %s", field, payload.Data.ADIF)
		}
	}
}

func acceptanceSharedAccountGate(t *testing.T) {
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"OK","message":"DATA_QUEUED"}`)
	}))
	defer srv.Close()

	build := func(name, call string) forwarding.Forwarder {
		t.Helper()
		credentials, err := json.Marshal(map[string]string{
			"call": call,
			"key":  "shared-test-api-key",
		})
		if err != nil {
			t.Fatalf("marshal credentials: %v", err)
		}
		fwd, err := forwarding.Build(types.ForwarderConfig{
			Name:        name,
			Type:        qrzcq.Type,
			Credentials: credentials,
			Endpoints:   map[string]string{action.Insert.String(): srv.URL},
		})
		if err != nil {
			t.Fatalf("build %s: %v", name, err)
		}
		return fwd
	}

	first := build("qrzcq-primary", "7Q5PCE")
	second := build("qrzcq-secondary", " 7q5pce ")
	if res := first.Submit(context.Background(), sampleQSO(), action.Insert, ""); res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("first Submit = %+v, want immediate success", res)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	res := second.Submit(ctx, sampleQSO(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeTransient || res.Err == nil {
		t.Fatalf("second Submit = %+v, want cancellable pacing wait", res)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("HTTP requests = %d, want 1; the shared account must remain gated", got)
	}
}

func acceptanceSeededDestination(t *testing.T) {
	cfg := config.DefaultConfig(t.TempDir())
	var got *types.ForwarderConfig
	for i := range cfg.Forwarders {
		if cfg.Forwarders[i].Type == qrzcq.Type {
			got = &cfg.Forwarders[i]
			break
		}
	}
	if got == nil {
		t.Fatal("DefaultConfig did not seed the registered qrzcq destination")
	}
	if got.Enabled {
		t.Error("seeded qrzcq destination is enabled; credentials must be opt-in")
	}
	if got.TickIntervalSec != 90 {
		t.Errorf("tick_interval_sec = %d, want 90", got.TickIntervalSec)
	}
	if got.BatchSize != 1 {
		t.Errorf("batch_size = %d, want 1", got.BatchSize)
	}
	if len(got.ActionFilter) != 1 || got.ActionFilter[0] != action.Insert.String() {
		t.Errorf("action_filter = %v, want [insert]", got.ActionFilter)
	}
	if got.Endpoints[action.Insert.String()] != qrzcq.DefaultEndpoint {
		t.Errorf("insert endpoint = %q, want %q", got.Endpoints[action.Insert.String()], qrzcq.DefaultEndpoint)
	}
}

func acceptanceDurableWorkerUpload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"OK","message":"DATA_QUEUED"}`)
	}))
	defer srv.Close()

	fc := types.ForwarderConfig{
		Name:         "qrzcq",
		Type:         qrzcq.Type,
		Enabled:      true,
		Credentials:  json.RawMessage(`{"call":"7Q5WRK","key":"test-api-key"}`),
		ActionFilter: []string{action.Insert.String()},
		Endpoints:    map[string]string{action.Insert.String(): srv.URL},
	}
	cfg := config.DefaultConfig(t.TempDir())
	cfg.Forwarders = []types.ForwarderConfig{fc}
	dbPath := filepath.Join(t.TempDir(), "acceptance.db")
	cfg.Datastore.Path = dbPath
	cfgSvc := config.New(cfg)
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatalf("initialize config: %v", err)
	}
	logger := logging.NewForWriter(io.Discard)
	db := &sqlite.Service{ConfigService: cfgSvc, LoggerService: logger}
	if err := db.Initialize(); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	db.DatabaseConfig = &types.DatastoreConfig{
		Driver:                    "sqlite",
		Path:                      dbPath,
		MaxOpenConns:              1,
		MaxIdleConns:              1,
		ContextTimeout:            10,
		TransactionContextTimeout: 10,
	}
	if err := db.Open(); err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	hub := events.NewHub()
	defer hub.Close()
	qsoSvc := &qsoservice.Service{DB: db, Logger: logger, Config: cfgSvc, Hub: hub}
	if err := qsoSvc.Initialize(); err != nil {
		t.Fatalf("initialize QSO service: %v", err)
	}

	logbookID, err := db.InsertLogbookWithContext(context.Background(), types.Logbook{
		Name: "Acceptance", Callsign: "7Q5MLV",
	})
	if err != nil {
		t.Fatalf("insert logbook: %v", err)
	}
	logged, err := qsoSvc.Submit(context.Background(), logbookID,
		adif.QsoToRecord(sampleQSO()), false)
	if err != nil {
		t.Fatalf("log QSO locally: %v", err)
	}
	rows, err := db.FetchUploadsByQsoIDWithContext(context.Background(), logged.ID)
	if err != nil || len(rows) != 1 || rows[0].Status != "pending" {
		t.Fatalf("upload immediately after local commit = %v (err=%v), want one pending row", rows, err)
	}

	fwd, err := forwarding.Build(fc)
	if err != nil {
		t.Fatalf("build QRZCQ forwarder: %v", err)
	}
	w, err := worker.New(worker.Config{
		Name:  fc.Name,
		Tick:  time.Hour,
		Batch: 1,
		Retry: qrzcq.DefaultRetry,
	}, fwd, db, logger, hub)
	if err != nil {
		t.Fatalf("construct worker: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(ctx) // Run performs its first queue-drain tick immediately.
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		rows, err = db.FetchUploadsByQsoIDWithContext(context.Background(), logged.ID)
		if err == nil && len(rows) == 1 && rows[0].Status == "uploaded" {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatalf("durable upload did not reach uploaded state: rows=%v err=%v", rows, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
}

func sampleQSO() types.Qso {
	return types.Qso{
		ContactedStation: types.ContactedStation{Call: "M0TEST"},
		QsoDetails: types.QsoDetails{
			QsoDate: "20260814",
			TimeOn:  "0322",
			Band:    "20m",
			Mode:    "SSB",
			Freq:    "14.250",
		},
		LoggingStation: types.LoggingStation{StationCallsign: "7Q5MLV"},
	}
}
