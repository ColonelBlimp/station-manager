package sqlite

import (
	"context"
	"encoding/json"
	stderr "errors"
	"fmt"
	"testing"
)

/*
   W-0021 5F.4 commit 4 (ruling J1, 2026-10-12): what a logbook's rows say
   about its SM Cloud past, and the stamp's recheck inside its transaction.

     HI1  LogbookSmcloudHistoryWithContext counts the logbook's SM Cloud upload
          records whatever their status, action or worker name, on live and
          deleted QSOs; another destination's records and another logbook's
          QSOs do not count. QSO rows count live and deleted.
     MI7  a stamping row with RequireEmptyLogbook fails the WHOLE save when the
          logbook holds a QSO row, live or deleted, and writes nothing; on an
          empty logbook it stamps.
*/

func historyQso(t *testing.T, svc *Service, id, logbookID int64, deleted bool) {
	t.Helper()
	del := "NULL"
	if deleted {
		del = "datetime('now')"
	}
	execT(t, svc, `INSERT INTO qso (id, uuid, call, band, mode, freq, qso_date, time_on, time_off, rst_sent, rst_rcvd, country, dedupe_key, logbook_id, deleted_at)
		VALUES (?, ?, 'EA1B', '40m', 'SSB', 7050000, '20250508', '0845', '0845', '59', '59', 'Test', ?, ?, `+del+`)`,
		id, fmt.Sprintf("01920000-0000-7000-8000-%012d", id), fmt.Sprintf("%064d", id), logbookID)
}

func historyUpload(t *testing.T, svc *Service, qsoID int64, name, typ, act, status string) {
	t.Helper()
	execT(t, svc, `INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status, origin) VALUES (?, ?, ?, ?, ?, 'live')`,
		qsoID, name, typ, act, status)
}

func TestSmcloudHistory_HI1_CountsEveryRecordOfTheLogbook(t *testing.T) {
	svc := testService(t)
	seedSmcloudBinding(t, svc) // logbooks 1 and 2
	ctx := context.Background()
	if h, err := svc.LogbookSmcloudHistoryWithContext(ctx, 2); err != nil || h != (LogbookHistory{}) {
		t.Fatalf("empty logbook: %+v, %v; want nothing", h, err)
	}
	historyQso(t, svc, 10, 2, false)
	historyQso(t, svc, 11, 2, true)
	historyQso(t, svc, 12, 2, false)
	historyQso(t, svc, 20, 1, false) // another logbook's QSO and records
	historyUpload(t, svc, 10, "smcloud", "smcloud", "insert", "pending")
	historyUpload(t, svc, 10, "smcloud.old", "smcloud", "update", "failed")
	historyUpload(t, svc, 11, "cloud-retired", "smcloud", "delete", "uploaded")
	historyUpload(t, svc, 11, "smcloud", "smcloud", "insert", "in_progress")
	historyUpload(t, svc, 12, "qrz", "qrz", "insert", "uploaded")
	historyUpload(t, svc, 20, "cloud", "smcloud", "insert", "uploaded")
	h, err := svc.LogbookSmcloudHistoryWithContext(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if h.SmcloudUploads != 4 || h.Qsos != 3 {
		t.Fatalf("history = %+v; want 4 SM Cloud records and 3 QSO rows", h)
	}
}

func TestIdentitySince_MI7_TheEmptyLogbookIsRecheckedInTheTransaction(t *testing.T) {
	row := func() DestinationUpsert {
		return DestinationUpsert{LogbookID: 2, Destination: "smcloud", ForwarderName: "smcloud.second", Enabled: true,
			Credentials: json.RawMessage(`{"logbook":"x"}`), StampIdentity: true, RequireEmptyLogbook: true}
	}
	other := DestinationUpsert{LogbookID: 1, Destination: "smcloud", ForwarderName: "cloud", Enabled: false,
		Credentials: json.RawMessage(`{"logbook":"shack"}`)}
	for name, deleted := range map[string]bool{"a live QSO": false, "a deleted QSO": true} {
		t.Run(name, func(t *testing.T) {
			svc := testService(t)
			seedSmcloudBinding(t, svc)
			historyQso(t, svc, 10, 2, deleted)
			if err := svc.UpsertLogbookDestinationsWithContext(context.Background(), []DestinationUpsert{other, row()}); !stderr.Is(err, ErrLogbookNotEmpty) {
				t.Fatalf("save = %v; want ErrLogbookNotEmpty", err)
			}
			if n := countT(t, svc, `SELECT COUNT(*) FROM logbook_destination WHERE logbook_id = 2`); n != 0 {
				t.Fatal("the failed save wrote the stamping row")
			}
			if n := countT(t, svc, `SELECT COUNT(*) FROM logbook_destination WHERE forwarder_name = 'cloud' AND enabled = 1`); n != 1 {
				t.Fatal("the failed save wrote the other row")
			}
		})
	}
	t.Run("an empty logbook is stamped", func(t *testing.T) {
		svc := testService(t)
		seedSmcloudBinding(t, svc)
		historyQso(t, svc, 10, 1, false) // a QSO in ANOTHER logbook does not count
		if err := svc.UpsertLogbookDestinationsWithContext(context.Background(), []DestinationUpsert{row()}); err != nil {
			t.Fatal(err)
		}
		if stamped, _ := stampOf(t, svc, "smcloud.second"); !stamped {
			t.Fatal("the empty logbook's binding was not stamped")
		}
	})
}
