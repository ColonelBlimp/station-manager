package ft8

// EventIdentity is the first frame on every FT8 stream connection: the archive
// the daemon serving it has open (ADR 0094 update, 2026-10-23). A connection
// cannot outlive its process, so a client attributes every later frame on it —
// ft8-logged rows and ft8-qso engagement — to that archive.
const EventIdentity = "identity"

// streamIdentity is the identity frame's payload. An empty ArchiveID (never
// recorded) is sent in the same place; the client treats it as no identity.
type streamIdentity struct {
	ArchiveID string `json:"archive_id"`
}

// SetServedArchive records the archive this daemon generation has open.
// Called by cmd/smd before the HTTP server starts; internal/ft8 learns only the
// identifier, never config or the archive catalogue.
func (s *Service) SetServedArchive(id string) {
	s.servedArchive.Store(id)
}

func (s *Service) servedArchiveID() string {
	id, _ := s.servedArchive.Load().(string)
	return id
}
