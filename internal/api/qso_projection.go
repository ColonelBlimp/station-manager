package api

import (
	"encoding/json"
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// projectPublicQso serializes a QSO for the public API boundary (AW-1). It DELEGATES to the
// canonical types.Qso JSON — never re-declaring the ADIF/enrichment fields, which would
// duplicate the one-line-per-ADIF-field invariant and reintroduce the mirror-struct trap
// (docs/v1-analysis/lessons-for-v2.md:168) — then PRUNES the server-internal identifiers
// that must not cross the wire: dedupe_key, csid (the contacted_station row PK), and
// country_details.id (the DXCC row PK). It does NOT mutate the source. It RETAINS, through
// v2.0.0-alpha.2, the transitional local id and contact_history[].id (both removed in
// v2.0.0-alpha.3); uuid is the canonical identifier and logbook_id stays (the public
// logbook key). Only the internal ids are pruned, so any field types.Qso gains in future is
// exposed by default unless it is added to the prune list — the safe direction for a public
// contract is deliberately NOT the mirror-struct's opposite.
func projectPublicQso(q types.Qso) (json.RawMessage, error) {
	raw, err := json.Marshal(q)
	if err != nil {
		return nil, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, err
	}
	delete(top, "dedupe_key")
	delete(top, "csid")
	pruneNestedKey(top, "country_details", "id")
	return json.Marshal(top)
}

// projectPublicQsos projects a slice in order. Returns a non-nil (possibly empty) slice so
// the list response serializes "items": [] rather than null.
func projectPublicQsos(qs types.QsoSlice) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, 0, len(qs))
	for i := range qs {
		p, err := projectPublicQso(qs[i])
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
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

// writePublicQso projects a single QSO and writes it as the response body. A projection
// (serialization) failure is a 500 — the row was fetched, so this is a server fault.
func (s *Server) writePublicQso(w http.ResponseWriter, op errors.Op, status int, qso types.Qso) {
	projected, err := projectPublicQso(qso)
	if err != nil {
		s.writeServerError(w, op, err, "serialize_error", "failed to serialize QSO")
		return
	}
	s.writeJSON(w, status, projected)
}
