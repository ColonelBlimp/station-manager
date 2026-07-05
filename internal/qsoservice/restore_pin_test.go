package qsoservice

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

// stampSuffixes identify the forwarder-owned "already uploaded" stamp fields on
// types.Qso by JSON-tag suffix. These are the state the ADR-0039 backfill filter
// reads (HasUploadStamp / missing_from), so Update must restore each from the
// stored row — never take it from the client body. Add a suffix here if a future
// forwarder introduces a new stamp shape.
var stampSuffixes = []string{"_upload_status", "_upload_date", "_logid", "_by_email_status", "_by_email_date"}

func isStampTag(tag string) bool {
	for _, sfx := range stampSuffixes {
		if strings.HasSuffix(tag, sfx) {
			return true
		}
	}
	return false
}

// TestUpdate_RestoresAllForwarderStamps pins Update's immutable-restore denylist
// against types.Qso drift (review 2026-07-05 finding 3). The restore list is
// hand-maintained; a future forwarder that adds a stamp field to types.Qso but
// forgets to add a restore line would let a PATCH client forge or clear the
// "already uploaded" signal, silently corrupting the backfill filter. This walks
// types.Qso for every stamp-tagged (top-level) string field, forges each in a
// PATCH body, and asserts Update restored every one to the stored value.
func TestUpdate_RestoresAllForwarderStamps(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	res, err := s.Submit(ctx, lbID, adif.Record{
		ContactedStation: types.ContactedStation{Call: "K1ABC"},
		QsoDetails:       types.QsoDetails{Band: "40m", Mode: "SSB", Freq: "7.050", QsoDate: "20260101", TimeOn: "1200"},
		LoggingStation:   types.LoggingStation{StationCallsign: "M0ABC"},
	}, false)
	require.NoError(t, err)
	existing, err := s.DB.FetchQsoByIdWithContext(ctx, res.ID)
	require.NoError(t, err)

	// Give every stamp field a distinctive STORED value on `existing`, and forge a
	// different value in the PATCH body. (Stamps live at the top level of types.Qso,
	// so a non-recursive walk over its fields covers them.)
	ev := reflect.ValueOf(&existing).Elem()
	tp := ev.Type()
	forged := map[string]string{}
	var stampFields []reflect.StructField
	for i := 0; i < tp.NumField(); i++ {
		f := tp.Field(i)
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		if f.Type.Kind() != reflect.String || !isStampTag(tag) {
			continue
		}
		ev.Field(i).SetString("STORED-" + tag)
		forged[tag] = "FORGED-" + tag
		stampFields = append(stampFields, f)
	}
	require.NotEmpty(t, stampFields, "no stamp fields discovered — stampSuffixes drifted from types.Qso")

	body, err := json.Marshal(forged)
	require.NoError(t, err)
	updated, err := s.Update(ctx, existing, body, source.API)
	require.NoError(t, err)

	// Each stamp must survive as the STORED value, not the forged one.
	uv := reflect.ValueOf(updated)
	for _, f := range stampFields {
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		got := uv.FieldByIndex(f.Index).String()
		require.Equalf(t, "STORED-"+tag, got,
			"stamp %s (%s) not restored by Update — a client could forge it; add it to the immutable-restore list",
			f.Name, tag)
	}
}
