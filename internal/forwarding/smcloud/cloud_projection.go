package smcloud

import (
	"encoding/json"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// projectCloudQso serializes a QSO for the SM Cloud ingest payload (AW-1). It DELEGATES to
// the canonical types.Qso JSON — never re-declaring the ADIF/enrichment fields, which would
// reintroduce the mirror-struct trap (docs/v1-analysis/lessons-for-v2.md:168) — then PRUNES
// every daemon-LOCAL identifier the cloud never reads and keys nothing on: the local row id,
// logbook_id, dedupe_key, csid (the contacted_station row PK), country_details.id (the DXCC
// row PK), and EVERY contact_history[].id. The cloud keys on uuid (+ tenant); modified_at /
// revision / deleted_at ride the envelope beside the payload. It does NOT mutate the source.
//
// This is deliberately SEPARATE from the daemon's public API projection: the API keeps
// logbook_id (its public logbook key) and a transitional id, while the cloud needs neither.
func projectCloudQso(q types.Qso) (json.RawMessage, error) {
	raw, err := json.Marshal(q)
	if err != nil {
		return nil, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, err
	}
	delete(top, "id")
	delete(top, "logbook_id")
	delete(top, "dedupe_key")
	delete(top, "csid")
	pruneNestedKey(top, "country_details", "id")
	pruneArrayKey(top, "contact_history", "id")
	return json.Marshal(top)
}

// pruneNestedKey deletes one key from a JSON object member of top, if both are present. A
// member that is absent, null, or not an object is left untouched.
func pruneNestedKey(top map[string]json.RawMessage, member, key string) {
	raw, ok := top[member]
	if !ok {
		return
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return
	}
	if _, ok := obj[key]; !ok {
		return
	}
	delete(obj, key)
	if b, err := json.Marshal(obj); err == nil {
		top[member] = b
	}
}

// pruneArrayKey deletes one key from every object element of a JSON array member of top. A
// member that is absent, null, or not an array of objects is left untouched.
func pruneArrayKey(top map[string]json.RawMessage, member, key string) {
	raw, ok := top[member]
	if !ok {
		return
	}
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return
	}
	for _, el := range arr {
		delete(el, key)
	}
	if b, err := json.Marshal(arr); err == nil {
		top[member] = b
	}
}
