package adif

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// TestEmittedFields_MatchesTheWrittenRecord proves the comparison and the
// attachment share one definition of "emitted" (W-0010 outcome 11): the map
// holds exactly the (TAG, value) pairs Record.String writes, formatting
// included, and nothing it skips.
func TestEmittedFields_MatchesTheWrittenRecord(t *testing.T) {
	for i, q := range goldenQsos() {
		rec := QsoToRecord(q)
		got := EmittedFields(rec)

		var written strings.Builder
		for _, tag := range sortedKeys(got) {
			written.WriteString(fmt.Sprintf(elementFormat, tag, len(got[tag]), got[tag]))
		}
		// Record.String writes in struct order; compare as sets of elements.
		wantElems := splitElements(rec.String())
		gotElems := splitElements(written.String() + EorStr + NewLineStr)
		if !reflect.DeepEqual(wantElems, gotElems) {
			t.Errorf("qso %d: EmittedFields disagrees with Record.String\n got: %v\nwant: %v", i, gotElems, wantElems)
		}
	}
}

func TestEmittedFields_AppliesTheEmitterRules(t *testing.T) {
	got := EmittedFields(QsoToRecord(goldenQsos()[0]))
	for tag, want := range map[string]string{
		"FREQ":     "14.074",   // Hz → MHz
		"QSO_DATE": "20261106", // dashes removed
		"TIME_ON":  "065846",   // colons removed
		"LAT":      "N004 30.000",
	} {
		if got[tag] != want {
			t.Errorf("%s = %q, want %q", tag, got[tag], want)
		}
	}
	if _, ok := got["CSID"]; ok {
		t.Error("CSID (a non-string field) must not be emitted")
	}
	if _, ok := got["COMMENT"]; ok {
		t.Error("an empty field must not be emitted")
	}
	bad := EmittedFields(QsoToRecord(goldenQsos()[1]))
	if _, ok := bad["DXCC"]; ok {
		t.Error("a non-numeric DXCC must be dropped, as the attachment drops it")
	}
}

// approvedEmailMarkIgnored is the reviewed list (inbox triage item 1, W-0010
// outcome 11). Changing EmailMarkIgnoredTags means changing this list in the
// same reviewed commit.
var approvedEmailMarkIgnored = []string{
	"APP_QRZLOG_LOGID",
	"CLUBLOG_QSO_UPLOAD_DATE",
	"CLUBLOG_QSO_UPLOAD_STATUS",
	"QRZCOM_QSO_DOWNLOAD_DATE",
	"QRZCOM_QSO_DOWNLOAD_STATUS",
	"QRZCOM_QSO_UPLOAD_DATE",
	"QRZCOM_QSO_UPLOAD_STATUS",
	"SM_FWRD_BY_EMAIL_DATE",
	"SM_FWRD_BY_EMAIL_STATUS",
	"SM_QSO_UPLOAD_DATE",
	"SM_QSO_UPLOAD_STATUS",
}

func TestEmailMarkIgnoredTags_IsExactlyTheApprovedList(t *testing.T) {
	got := EmailMarkIgnoredTags()
	sort.Strings(got)
	if !reflect.DeepEqual(got, approvedEmailMarkIgnored) {
		t.Fatalf("ignored tags = %v\nwant the approved list %v", got, approvedEmailMarkIgnored)
	}
}

// Every ignored name must be a real tag of Record, so a typo cannot silently
// ignore nothing (the bookkeeping field would then be compared and the mark
// would miss again).
func TestEmailMarkIgnoredTags_AreRealRecordTags(t *testing.T) {
	tags := map[string]bool{}
	collectRecordTags(reflect.TypeOf(Record{}), tags)
	for _, tag := range EmailMarkIgnoredTags() {
		if !tags[tag] {
			t.Errorf("ignored tag %q is not a tag of adif.Record", tag)
		}
	}
}

func TestEmailMarkContent(t *testing.T) {
	base := goldenQsos()[0]
	baseContent := EmailMarkContent(base)
	for _, tag := range EmailMarkIgnoredTags() {
		if _, ok := baseContent[tag]; ok {
			t.Errorf("content carries ignored tag %s", tag)
		}
	}
	if baseContent["APP_SM_QSO_ID"] != base.UUID {
		t.Error("the QSO's identity (APP_SM_QSO_ID) must be compared")
	}

	// Bookkeeping only: equal content.
	stamped := base
	stamped.ClubLogUploadDate, stamped.QrzComUploadDate = "20261107", "20261107"
	stamped.SmFwrdByEmailDate, stamped.SmFwrdByEmailStatus = "20261107", "Y"
	stamped.QrzlogLogid = "999"
	if !reflect.DeepEqual(EmailMarkContent(stamped), baseContent) {
		t.Error("a bookkeeping-only change must leave the content equal")
	}

	for name, mut := range map[string]func(*types.Qso){
		"name":        func(q *types.Qso) { q.Name = "Bob" },
		"country":     func(q *types.Qso) { q.Country = "Gabon" },
		"qsl_sent":    func(q *types.Qso) { q.Qsl.QslSent = "N" },
		"uuid":        func(q *types.Qso) { q.UUID = "0190a1b2-c3d4-7e5f-8a9b-0000000000ff" },
		"freq":        func(q *types.Qso) { q.Freq = "14075000" },
		"run id":      func(q *types.Qso) { q.AppSmRunID = "" },
		"my_lat":      func(q *types.Qso) { q.MyLat = "-3.4" },
		"cleared":     func(q *types.Qso) { q.Name = "" },
		"request_qsl": func(q *types.Qso) { q.AppSmRequestQsl = false },
	} {
		q := base
		mut(&q)
		if reflect.DeepEqual(EmailMarkContent(q), baseContent) {
			t.Errorf("%s change left the content equal; it must count as a change", name)
		}
	}
}

func collectRecordTags(t reflect.Type, out map[string]bool) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Type.Kind() == reflect.Struct {
			collectRecordTags(f.Type, out)
			continue
		}
		tag := f.Tag.Get("adif")
		if tag == "" || tag == "-" {
			tag = f.Tag.Get(JsonStructTag)
		}
		tag = strings.TrimSuffix(tag, ",omitempty")
		if tag != "" && tag != "-" {
			out[strings.ToUpper(tag)] = true
		}
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// splitElements returns the record's "<TAG:n>value" elements, sorted.
func splitElements(s string) []string {
	s = strings.TrimSuffix(strings.TrimSpace(s), EorStr)
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	sort.Strings(out)
	return out
}
