package sqlite

import (
	"context"
	"fmt"
	"testing"
)

// ADR 0082 part 6 (W-0021 5B): the two reads the routing-by-binding paths need.

func TestLogbookIDsByQsoIDs(t *testing.T) {
	svc := testService(t)
	seedTwoLogbookFile(t, svc, 1, lbUUID2) // QSO 1 → logbook 1, QSO 2 → logbook 2
	got, err := svc.LogbookIDsByQsoIDsWithContext(context.Background(), []int64{1, 2, 99})
	if err != nil || len(got) != 2 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("got %v (%v); want {1:1 2:2}, 99 absent", got, err)
	}
	if empty, err := svc.LogbookIDsByQsoIDsWithContext(context.Background(), nil); err != nil || len(empty) != 0 {
		t.Fatalf("empty input = %v (%v)", empty, err)
	}
}

func TestQueuedForwarderNames_UndrainedOnly(t *testing.T) {
	svc := testService(t)
	seedTwoLogbookFile(t, svc, 1, lbUUID2) // two pending rows named qrz
	execT(t, svc, `INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status, origin) VALUES (1, 'gone', 'qrz', 'delete', 'failed', 'edit')`)
	execT(t, svc, `INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status, origin) VALUES (2, 'done', 'qrz', 'update', 'uploaded', 'edit')`)
	names, err := svc.QueuedForwarderNamesWithContext(context.Background())
	if err != nil || fmt.Sprint(names) != "[gone qrz]" {
		t.Fatalf("names = %v (%v); want [gone qrz] — uploaded rows never count", names, err)
	}
}
