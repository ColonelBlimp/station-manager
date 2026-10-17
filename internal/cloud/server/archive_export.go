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
// null only for a legacy logbook that was never adopted; such a logbook, and
// only such a one, carries its stored LegacyName, unique within the archive,
// so that two of them stay apart whatever their labels. Count includes
// tombstones.
type ArchiveLogbookJSON struct {
	UUID       *string `json:"uuid"`
	LegacyName *string `json:"legacy_name,omitempty"`
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
// plus the logbook it is in, named as the head names it: by UUID, or, for a
// not-adopted legacy logbook, by a null UUID and its legacy name.
type ArchiveExportQso struct {
	UUID              string          `json:"uuid"`
	LogbookUUID       *string         `json:"logbook_uuid"`
	LogbookLegacyName *string         `json:"logbook_legacy_name,omitempty"`
	ModifiedAt        time.Time       `json:"modified_at"`
	Revision          int64           `json:"revision,omitempty"`
	DeletedAt         *time.Time      `json:"deleted_at,omitempty"`
	Qso               json.RawMessage `json:"qso"`
}

type archiveExportHead struct {
	Archive struct {
		UUID   string `json:"uuid"`
		Label  string `json:"label"`
		Legacy bool   `json:"legacy"`
	} `json:"archive"`
	Logbooks []ArchiveLogbookJSON `json:"logbooks"`
}

// archiveLogbooksJSON is the wire form of an archive's logbooks. A logbook
// with neither a UUID nor a legacy name (the column is nullable) is an error:
// its rows could not be told from another such logbook's.
func archiveLogbooksJSON(lbs []store.ArchiveLogbook) ([]ArchiveLogbookJSON, error) {
	out := make([]ArchiveLogbookJSON, 0, len(lbs))
	for _, lb := range lbs {
		j := ArchiveLogbookJSON{UUID: lb.UUID, Label: lb.Label, Callsign: lb.Callsign, Count: lb.Count, Tombstones: lb.Tombstones}
		if lb.UUID == nil {
			if lb.LegacyName == nil {
				return nil, fmt.Errorf("logbook %d has neither a uuid nor a legacy name", lb.ID)
			}
			j.LegacyName = lb.LegacyName
		}
		out = append(out, j)
	}
	return out, nil
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
		lbs, err := archiveLogbooksJSON(a.Logbooks)
		if err != nil {
			s.log.Error("archives: list failed", "tenant_id", tenantID(r), "request_id", requestID(r), "archive_uuid", a.ArchiveUUID, "err", err)
			s.writeError(w, http.StatusInternalServerError, "internal_error", "list failed")
			return
		}
		out.Archives = append(out.Archives, ArchiveJSON{UUID: a.ArchiveUUID, Label: a.ArchiveLabel, Legacy: a.Legacy, Logbooks: lbs})
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
	// Each row names its logbook exactly as the head does, through the
	// logbook's store ID.
	logbooks := map[int64]ArchiveLogbookJSON{}
	err := s.store.ArchiveExportSnapshot(r.Context(), tenantID(r), store.CanonicalUUID(archiveUUID),
		func(a store.ArchiveHead) error {
			var h archiveExportHead
			h.Archive.UUID, h.Archive.Label, h.Archive.Legacy = a.ArchiveUUID, a.ArchiveLabel, a.Legacy
			var err error
			if h.Logbooks, err = archiveLogbooksJSON(a.Logbooks); err != nil {
				return err
			}
			for i, lb := range a.Logbooks {
				logbooks[lb.ID] = h.Logbooks[i]
			}
			head, err := json.Marshal(h)
			if err != nil {
				return err
			}
			// Reopen the head object to append the qsos array.
			return out.begin(append(head[:len(head)-1], `,"qsos":[`...))
		},
		func(rec store.Record) error {
			lb, ok := logbooks[rec.LogbookID]
			if !ok {
				// The snapshot makes this unreachable; an error truncates the
				// document rather than export a row under no logbook.
				return fmt.Errorf("row %s is in logbook %d, not in the head", rec.UUID, rec.LogbookID)
			}
			return out.row(ArchiveExportQso{
				UUID: rec.UUID, LogbookUUID: lb.UUID, LogbookLegacyName: lb.LegacyName, ModifiedAt: rec.ModifiedAt, Revision: rec.Revision,
				DeletedAt: rec.DeletedAt, Qso: rec.Payload,
			})
		})
	s.finishExport(w, r, out, err)
}
