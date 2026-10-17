package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cloud/store"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// The whole-archive reads (W-0021 Q7, ADR 0071 refinement): the listing a
// provisioning client chooses from, and the archive in one snapshot. Labels
// are display values; the export is authoritative.

// ArchiveLogbookJSON is one logbook of a listed or exported archive. UUID is
// null only for a legacy logbook that was never adopted; Count includes
// tombstones.
type ArchiveLogbookJSON struct {
	UUID       *string `json:"uuid"`
	Label      string  `json:"label"`
	Callsign   string  `json:"callsign"`
	Count      int     `json:"count"`
	Tombstones int     `json:"tombstones"`
}

// ArchiveJSON is one archive of GET /v1/archives.
type ArchiveJSON struct {
	UUID     string               `json:"uuid"`
	Label    string               `json:"label"`
	Legacy   bool                 `json:"legacy"`
	Logbooks []ArchiveLogbookJSON `json:"logbooks"`
}

// ArchivesResponse is GET /v1/archives.
type ArchivesResponse struct {
	Archives []ArchiveJSON `json:"archives"`
}

// ArchiveExportQso is one row of the whole-archive export: the scoped row
// plus the UUID of the logbook it is in (null for a not-adopted legacy
// logbook).
type ArchiveExportQso struct {
	UUID        string          `json:"uuid"`
	LogbookUUID *string         `json:"logbook_uuid"`
	ModifiedAt  time.Time       `json:"modified_at"`
	Revision    int64           `json:"revision,omitempty"`
	DeletedAt   *time.Time      `json:"deleted_at,omitempty"`
	Qso         json.RawMessage `json:"qso"`
}

type archiveExportHead struct {
	Archive struct {
		UUID   string `json:"uuid"`
		Label  string `json:"label"`
		Legacy bool   `json:"legacy"`
	} `json:"archive"`
	Logbooks []ArchiveLogbookJSON `json:"logbooks"`
}

func archiveLogbooksJSON(lbs []store.ArchiveLogbook) []ArchiveLogbookJSON {
	out := make([]ArchiveLogbookJSON, 0, len(lbs))
	for _, lb := range lbs {
		out = append(out, ArchiveLogbookJSON{UUID: lb.UUID, Label: lb.Label, Callsign: lb.Callsign, Count: lb.Count, Tombstones: lb.Tombstones})
	}
	return out
}

// handleArchives lists the tenant's archives that have a UUID, with their
// logbooks and counts.
func (s *Server) handleArchives(w http.ResponseWriter, r *http.Request) {
	archives, err := s.store.IdentityArchives(r.Context(), tenantID(r))
	if err != nil {
		s.log.Error("archives: list failed", "tenant_id", tenantID(r), "request_id", requestID(r), "err", err)
		s.writeError(w, http.StatusInternalServerError, "internal_error", "list failed")
		return
	}
	out := ArchivesResponse{Archives: make([]ArchiveJSON, 0, len(archives))}
	for _, a := range archives {
		out.Archives = append(out.Archives, ArchiveJSON{UUID: a.ArchiveUUID, Label: a.ArchiveLabel, Legacy: a.Legacy, Logbooks: archiveLogbooksJSON(a.Logbooks)})
	}
	s.writeJSON(w, http.StatusOK, out)
}

// handleArchiveExport streams one archive, by UUID, from one snapshot: the
// archive with its legacy marker and every logbook with its counts, then
// every record, tombstones included, each naming its logbook. Admitted
// through the export gate; the request's context reaches the store.
func (s *Server) handleArchiveExport(w http.ResponseWriter, r *http.Request) {
	archiveUUID := r.PathValue("archive_uuid")
	if !utils.IsValidUUIDv7(archiveUUID) {
		s.writeError(w, http.StatusNotFound, "not_found", "no such archive")
		return
	}
	release, ok := s.acquireExport(w, r)
	if !ok {
		return
	}
	defer release()
	out := &exportStream{w: w, notFound: "no such archive"}
	logbookUUID := map[int64]*string{}
	err := s.store.ArchiveExportSnapshot(r.Context(), tenantID(r), store.CanonicalUUID(archiveUUID),
		func(a store.ArchiveHead) error {
			var h archiveExportHead
			h.Archive.UUID, h.Archive.Label, h.Archive.Legacy = a.ArchiveUUID, a.ArchiveLabel, a.Legacy
			h.Logbooks = archiveLogbooksJSON(a.Logbooks)
			for _, lb := range a.Logbooks {
				logbookUUID[lb.ID] = lb.UUID
			}
			head, err := json.Marshal(h)
			if err != nil {
				return err
			}
			// Reopen the head object to append the qsos array.
			return out.begin(append(head[:len(head)-1], `,"qsos":[`...))
		},
		func(rec store.Record) error {
			lb, ok := logbookUUID[rec.LogbookID]
			if !ok {
				// The snapshot makes this unreachable; an error truncates the
				// document rather than export a row under no logbook.
				return fmt.Errorf("row %s is in logbook %d, not in the head", rec.UUID, rec.LogbookID)
			}
			return out.row(ArchiveExportQso{
				UUID: rec.UUID, LogbookUUID: lb, ModifiedAt: rec.ModifiedAt, Revision: rec.Revision,
				DeletedAt: rec.DeletedAt, Qso: rec.Payload,
			})
		})
	s.finishExport(w, r, out, err)
}
