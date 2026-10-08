package smcloud

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0021 5F.3 commit 3 (ADR 0090, T3): the evidence a Home adoption needs.
   Adoption maps one legacy cloud logbook NAME to Home's default logbook, so
   it is safe only when nothing else in Home wrote, or can write, under that
   name, and the cloud holds nothing the default logbook does not own:

     E1  safe: the default binding alone in its name group, every cloud UUID
         (tombstones included) a local QSO of the default logbook (soft-deleted
         included), compared case-insensitively.
     E2  (a) another SM Cloud binding in the name group — disabled counts —
         under the forwarder's own normalization (TrimSpace, blank = "main",
         otherwise exact).
     E3  (b) another binding in the group with queued uploads (waiting, in
         flight, or failed) is named as such, while the manifest looks safe.
     E4  (c) a cloud UUID outside the default logbook, or held only by the
         cloud, refuses.
     E5  another binding whose cloud name cannot be read refuses: it may share
         the name.
     E6  no default SM Cloud binding, or an unreadable one, is an error: there
         is nothing to judge.
     E7  the RUNNING routes count beside the saved bindings (operator review,
         2026-10-08): a worker uploads under the name its binding had at start,
         and a deleted logbook's worker keeps draining its queue until the
         restart. A binding is judged on both names.
     S1  sequence, real database: both bindings started on "shack"; the other is
         renamed without a restart. Refused: its worker still uploads to "shack".
     S2  sequence, real database: another logbook's binding started on "shack";
         its last QSO is deleted (tombstone queued) and the logbook deleted.
         Refused: its worker can still send the tombstone after the manifest
         was read.
*/

const (
	defaultLB = 1
	otherLB   = 2
	thirdLB   = 3
	uuidA     = "0192aaaa-0000-7000-8000-000000000001"
	uuidB     = "0192aaaa-0000-7000-8000-000000000002"
	uuidGone  = "0192aaaa-0000-7000-8000-000000000003" // soft-deleted locally
	uuidOther = "0192bbbb-0000-7000-8000-000000000009"
)

func binding(logbookID int64, name string, enabled bool, creds string) types.LogbookDestination {
	b := types.LogbookDestination{LogbookID: logbookID, Destination: Type, ForwarderName: name, Enabled: enabled}
	if creds != "" {
		b.Credentials = json.RawMessage(creds)
	}
	return b
}

// safeEvidence: Home's default binding uploads to "shack"; a second logbook's
// disabled binding uses another name and has work queued; QRZ shares nothing.
func safeEvidence() AdoptionEvidence {
	e := AdoptionEvidence{
		DefaultLogbookID: defaultLB,
		Bindings: []types.LogbookDestination{
			binding(defaultLB, "smcloud", true, `{"logbook":"  shack "}`),
			binding(otherLB, "smcloud.u2", false, `{"logbook":"portable"}`),
			{LogbookID: otherLB, Destination: "qrz", ForwarderName: "qrz.u2", Enabled: true, Credentials: json.RawMessage(`{"logbook":"shack"}`)},
		},
		Queued: map[string]sqlite.ForwarderQueueCounts{
			"smcloud":    {Waiting: 3},
			"smcloud.u2": {Waiting: 2, Failed: 1},
			"qrz.u2":     {Waiting: 5},
		},
		LocalUUIDs: []string{uuidA, uuidB, uuidGone},
		CloudUUIDs: []string{strings.ToUpper(uuidA), uuidGone},
	}
	e.Running = append([]types.LogbookDestination(nil), e.Bindings...)
	return e
}

func judge(t *testing.T, e AdoptionEvidence) AdoptionVerdict {
	t.Helper()
	v, err := JudgeAdoption(e)
	if err != nil {
		t.Fatalf("JudgeAdoption: %v", err)
	}
	return v
}

func wantAmbiguous(t *testing.T, v AdoptionVerdict, reason string) {
	t.Helper()
	if v.Safe {
		t.Fatalf("verdict safe; want ambiguous (%s)", reason)
	}
	if !strings.Contains(v.Reason, reason) {
		t.Fatalf("reason = %q; want it to contain %q", v.Reason, reason)
	}
}

func TestAdoptionEvidence_E1_SafeWhenTheNameAndTheCloudBelongToTheDefaultLogbook(t *testing.T) {
	v := judge(t, safeEvidence())
	if !v.Safe || v.CloudName != "shack" || v.Reason != "" {
		t.Fatalf("verdict = %+v; want safe for cloud name shack", v)
	}
	e := safeEvidence()
	e.CloudUUIDs = nil // the name was never pushed: nothing in the cloud to own
	if v := judge(t, e); !v.Safe {
		t.Fatalf("an empty cloud logbook: %+v; want safe", v)
	}
	e = safeEvidence()
	e.Bindings[1].Credentials = json.RawMessage(`{"logbook":"Shack"}`) // exact match: another name
	if v := judge(t, e); !v.Safe {
		t.Fatalf("a name differing in case: %+v; want safe (the server matches names exactly)", v)
	}
}

func TestAdoptionEvidence_E2_AnotherBindingInTheNameGroupRefuses(t *testing.T) {
	for name, tc := range map[string]struct {
		defaultCreds, otherCreds string
		otherEnabled             bool
	}{
		"enabled, same name":           {`{"logbook":"shack"}`, `{"logbook":"shack"}`, true},
		"disabled, same name":          {`{"logbook":"shack"}`, `{"logbook":"shack"}`, false},
		"padding normalizes":           {`{"logbook":"shack"}`, `{"logbook":" shack\t"}`, false},
		"blank and absent are main":    {``, `{"logbook":"  "}`, false},
		"absent and explicit main":     {`{}`, `{"logbook":"main"}`, false},
		"null credentials are main":    {`null`, `{"logbook":"main"}`, false},
		"other has no credentials row": {`{"logbook":"main"}`, ``, false},
	} {
		t.Run(name, func(t *testing.T) {
			e := safeEvidence()
			e.Bindings[0] = binding(defaultLB, "smcloud", true, tc.defaultCreds)
			e.Bindings[1] = binding(otherLB, "smcloud.u2", tc.otherEnabled, tc.otherCreds)
			e.Queued = nil
			wantAmbiguous(t, judge(t, e), "smcloud.u2 shares the cloud name")
		})
	}
	t.Run("every other binding is examined", func(t *testing.T) {
		e := safeEvidence()
		e.Bindings = append(e.Bindings, binding(thirdLB, "smcloud.u3", false, `{"logbook":"shack"}`))
		wantAmbiguous(t, judge(t, e), "smcloud.u3 shares the cloud name")
	})
}

func TestAdoptionEvidence_E3_QueuedUploadsUnderTheNameAreNamed(t *testing.T) {
	for name, counts := range map[string]sqlite.ForwarderQueueCounts{
		"waiting":   {Waiting: 1},
		"in flight": {InFlight: 1},
		"failed":    {Failed: 1},
	} {
		t.Run(name, func(t *testing.T) {
			e := safeEvidence()
			e.Bindings[1].Credentials = json.RawMessage(`{"logbook":"shack"}`)
			e.Queued["smcloud.u2"] = counts
			wantAmbiguous(t, judge(t, e), "smcloud.u2 has 1 queued upload under the cloud name")
		})
	}
}

func TestAdoptionEvidence_E4_ACloudUUIDOutsideTheDefaultLogbookRefuses(t *testing.T) {
	e := safeEvidence()
	e.CloudUUIDs = append(e.CloudUUIDs, uuidOther)
	wantAmbiguous(t, judge(t, e), "1 QSO in the cloud logbook \"shack\" is not a QSO of the default logbook")
	e = safeEvidence()
	e.CloudUUIDs = append(e.CloudUUIDs, uuidOther, "0192cccc-0000-7000-8000-000000000001")
	wantAmbiguous(t, judge(t, e), "2 QSOs in the cloud logbook \"shack\" are not QSOs of the default logbook")
}

func TestAdoptionEvidence_E5_AnUnreadableNameOnAnotherBindingRefuses(t *testing.T) {
	for name, creds := range map[string]string{
		"not JSON":          `{"logbook":`,
		"not an object":     `["shack"]`,
		"name not a string": `{"logbook":7}`,
	} {
		t.Run(name, func(t *testing.T) {
			e := safeEvidence()
			e.Bindings[1].Credentials = json.RawMessage(creds)
			wantAmbiguous(t, judge(t, e), "smcloud.u2: its cloud name cannot be read")
		})
	}
}

func TestAdoptionEvidence_E6_NothingToJudgeIsAnError(t *testing.T) {
	e := safeEvidence()
	e.Bindings = e.Bindings[1:]
	if _, err := JudgeAdoption(e); err == nil {
		t.Fatal("no default SM Cloud binding: want an error")
	}
	e = safeEvidence()
	e.Bindings[0].Credentials = json.RawMessage(`{"logbook":`)
	if _, err := JudgeAdoption(e); err == nil {
		t.Fatal("an unreadable default binding: want an error")
	}
}

func TestCloudName_MatchesTheForwardersNormalization(t *testing.T) {
	for creds, want := range map[string]string{
		``:                      "main",
		`null`:                  "main",
		`{}`:                    "main",
		`{"logbook":""}`:        "main",
		`{"logbook":"  "}`:      "main",
		`{"logbook":" Shack "}`: "Shack",
	} {
		got, err := CloudName(json.RawMessage(creds))
		if err != nil || got != want {
			t.Errorf("CloudName(%s) = %q, %v; want %q", creds, got, err, want)
		}
		// The forwarder itself must agree: it is what uploads under the name.
		full := map[string]any{}
		if creds != "" {
			_ = json.Unmarshal([]byte(creds), &full)
		}
		if full == nil { // "null"
			full = map[string]any{}
		}
		full["url"], full["token"] = "https://c.example", "t"
		raw, _ := json.Marshal(full)
		f, err := New(types.ForwarderConfig{Type: Type, Credentials: raw})
		if err != nil {
			t.Fatalf("New(%s): %v", raw, err)
		}
		if f.(*Forwarder).logbook != want {
			t.Errorf("the forwarder uploads %s under %q; CloudName says %q", creds, f.(*Forwarder).logbook, want)
		}
	}
}

func TestAdoptionEvidence_E7_TheRunningRoutesCountBesideTheSavedBindings(t *testing.T) {
	t.Run("started on the name, saved under another", func(t *testing.T) {
		e := safeEvidence()
		e.Running[1].Credentials = json.RawMessage(`{"logbook":"shack"}`)
		e.Queued = nil
		wantAmbiguous(t, judge(t, e), `binding smcloud.u2 shares the cloud name "shack" (as started)`)
	})
	t.Run("saved under the name, started on another", func(t *testing.T) {
		e := safeEvidence()
		e.Bindings[1].Credentials = json.RawMessage(`{"logbook":"shack"}`)
		e.Queued = nil
		wantAmbiguous(t, judge(t, e), `binding smcloud.u2 shares the cloud name "shack" (as saved)`)
	})
	t.Run("started, then its logbook deleted, uploads still queued", func(t *testing.T) {
		e := safeEvidence()
		e.Running = append(e.Running, binding(thirdLB, "smcloud.u3", true, `{"logbook":"shack"}`))
		e.Queued["smcloud.u3"] = sqlite.ForwarderQueueCounts{InFlight: 1}
		wantAmbiguous(t, judge(t, e), `binding smcloud.u3 has 1 queued upload under the cloud name "shack" (as started)`)
	})
	t.Run("an unreadable running name refuses", func(t *testing.T) {
		e := safeEvidence()
		e.Running[1].Credentials = json.RawMessage(`{"logbook":`)
		wantAmbiguous(t, judge(t, e), "smcloud.u2: its cloud name cannot be read (as started)")
	})
	t.Run("a queue with no binding either way is inert", func(t *testing.T) {
		e := safeEvidence()
		e.Queued["smcloud.gone"] = sqlite.ForwarderQueueCounts{Waiting: 4}
		if v := judge(t, e); !v.Safe {
			t.Fatalf("rows no worker will ever send: %+v; want safe", v)
		}
	})
}

// ---- Sequences against the real database (S1, S2) ----

func insertQso(t *testing.T, db *sqlite.Service, id int64, uuid string, logbookID int64, deleted bool) {
	t.Helper()
	tx, cancel, err := db.BeginTxContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	var deletedAt any
	if deleted {
		deletedAt = "2026-10-08 06:00:00"
	}
	if _, err := tx.ExecContext(context.Background(), `INSERT INTO qso
		(id, uuid, call, band, mode, freq, qso_date, time_on, time_off,
		 rst_sent, rst_rcvd, country, dedupe_key, logbook_id, deleted_at)
		VALUES (?,?,'EA1B','40m','SSB',7050000,'20250508','0845','0845','59','59','Test',?,?,?)`,
		id, uuid, fmt.Sprintf("%064d", id), logbookID, deletedAt); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func queueUpload(t *testing.T, db *sqlite.Service, qsoID int64, forwarder, act string) {
	t.Helper()
	tx, cancel, err := db.BeginTxContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if _, err := tx.ExecContext(context.Background(), `INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status, origin)
		VALUES (?, ?, 'smcloud', ?, 'pending', 'live')`, qsoID, forwarder, act); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// homeDB: Home's default logbook (two QSOs, one soft-deleted) bound to SM
// Cloud under "shack", and a second logbook whose binding also starts on
// "shack". It returns the database and the second logbook's id.
func homeDB(t *testing.T) (*sqlite.Service, int64, int64) {
	t.Helper()
	_, db, _, _ := newLocalStack(t, "https://c.example")
	ctx := context.Background()
	main, err := db.InsertLogbookWithContext(ctx, types.Logbook{Name: "Main", Callsign: "M0ABC"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.InsertLogbookWithContext(ctx, types.Logbook{Name: "Second", Callsign: "M0XYZ"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertLogbookDestinationsWithContext(ctx, []sqlite.DestinationUpsert{
		{LogbookID: main, Destination: Type, ForwarderName: "smcloud", Enabled: true, Credentials: json.RawMessage(`{"logbook":"shack"}`)},
		{LogbookID: second, Destination: Type, ForwarderName: "smcloud.u2", Enabled: true, Credentials: json.RawMessage(`{"logbook":"shack"}`)},
	}); err != nil {
		t.Fatal(err)
	}
	insertQso(t, db, 1, uuidA, main, false)
	insertQso(t, db, 2, uuidGone, main, true)
	return db, main, second
}

func startedRoutes(t *testing.T, db *sqlite.Service) []types.LogbookDestination {
	t.Helper()
	running, err := db.ListLogbookDestinationsWithContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return running
}

func localEvidence(t *testing.T, db *sqlite.Service, main int64, running []types.LogbookDestination) AdoptionEvidence {
	t.Helper()
	e, err := LocalAdoptionEvidence(context.Background(), db, main, running)
	if err != nil {
		t.Fatalf("LocalAdoptionEvidence: %v", err)
	}
	e.CloudUUIDs = []string{uuidA, uuidGone} // the manifest looks safe
	return e
}

func TestAdoptionEvidence_S1_ARenameWithoutARestartStillCountsTheRunningName(t *testing.T) {
	db, main, second := homeDB(t)
	running := startedRoutes(t, db)
	if err := db.UpsertLogbookDestinationsWithContext(context.Background(), []sqlite.DestinationUpsert{
		{LogbookID: second, Destination: Type, ForwarderName: "smcloud.u2", Enabled: true, Credentials: json.RawMessage(`{"logbook":"portable"}`)},
	}); err != nil {
		t.Fatal(err)
	}
	e := localEvidence(t, db, main, running)
	if got := strings.Join(e.LocalUUIDs, ","); !strings.Contains(got, uuidA) || !strings.Contains(got, uuidGone) {
		t.Fatalf("local UUIDs = %v; want the default logbook's QSOs, soft-deleted included", e.LocalUUIDs)
	}
	wantAmbiguous(t, judge(t, e), `binding smcloud.u2 shares the cloud name "shack" (as started)`)
	// After a restart the worker uploads under the saved name: safe.
	if v := judge(t, localEvidence(t, db, main, startedRoutes(t, db))); !v.Safe {
		t.Fatalf("after the restart: %+v; want safe", v)
	}
}

func TestAdoptionEvidence_S2_ADeletedLogbooksWorkerStillCounts(t *testing.T) {
	db, main, second := homeDB(t)
	running := startedRoutes(t, db)
	insertQso(t, db, 3, uuidOther, second, true)
	queueUpload(t, db, 3, "smcloud.u2", "delete")
	if err := db.DeleteLogbookByIDWithContext(context.Background(), second); err != nil {
		t.Fatalf("delete the second logbook: %v", err)
	}
	e := localEvidence(t, db, main, running)
	for _, b := range e.Bindings {
		if b.ForwarderName == "smcloud.u2" {
			t.Fatalf("fixture: the deleted logbook's binding is still listed as saved: %+v", b)
		}
	}
	wantAmbiguous(t, judge(t, e), `binding smcloud.u2 has 1 queued upload under the cloud name "shack" (as started)`)
	// After a restart no worker serves the deleted logbook: its row is inert.
	if v := judge(t, localEvidence(t, db, main, startedRoutes(t, db))); !v.Safe {
		t.Fatalf("after the restart: %+v; want safe", v)
	}
}
