package smcloud

import (
	"encoding/json"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// UploadRecord is one QSO as Submit puts it on the wire: the element of the
// smcloud service's PUT bodies (internal/cloud/server QsoUpload). Declared here
// rather than imported: the cloud packages must stay daemon-free, and the
// shared contract is types.Qso itself; this envelope's round-trip is pinned
// against the real server by the integration tests.
type UploadRecord struct {
	ModifiedAt time.Time `json:"modified_at"`
	// Revision is the row's monotonic edit counter (ADR 0050), the primary
	// ordering the cloud's upsert guard applies. Envelope-only, like
	// modified_at (types.Qso tags it json:"-", so it never rides the payload).
	Revision  int64      `json:"revision,omitempty"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	// Qso is the projected QSO body (projectCloudQso): the canonical types.Qso
	// JSON with the daemon-local identifiers stripped (AW-1), never the raw
	// struct.
	Qso json.RawMessage `json:"qso"`
}

// UploadFor builds the record Submit sends for q under act: insert and update
// are the same upsert; a delete carries the tombstone, deleted_at falling back
// to modified_at (the delete bumped it) when the row has none. Provisioning's
// round-trip check (W-0021 Q7) compares this record with the cloud's, so "what
// the worker would push" is this function, not a copy of it.
func UploadFor(q types.Qso, act forwarding.Action) (UploadRecord, error) {
	const op errors.Op = "smcloud.UploadFor"
	if q.UUID == "" {
		return UploadRecord{}, errors.New(op).WithMsg("qso has no UUID")
	}
	// modified_at is the reconcile drift signal: silently substituting now()
	// would make the cloud copy never hash-match the local row. A zero value
	// means the fetch path didn't overlay the column: a bug, surfaced loudly.
	if q.ModifiedAt.IsZero() {
		return UploadRecord{}, errors.New(op).WithMsgf("qso %s has no modified_at (fetch overlay missing)", q.UUID)
	}
	projected, err := projectCloudQso(q)
	if err != nil {
		return UploadRecord{}, errors.New(op).WithErr(err).WithMsg("project qso for cloud payload")
	}
	up := UploadRecord{ModifiedAt: q.ModifiedAt, Revision: q.Revision, Qso: projected}
	switch act {
	case action.Insert, action.Update:
		// plain upsert
	case action.Delete:
		d := q.DeletedAt
		if d.IsZero() {
			d = q.ModifiedAt
		}
		up.DeletedAt = &d
	default:
		return UploadRecord{}, errors.New(op).WithMsgf("unknown action %q", act)
	}
	return up, nil
}
