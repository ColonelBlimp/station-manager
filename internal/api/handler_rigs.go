package api

import (
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// RigsResponse is the wire shape for GET /v1/rigs — the config SPA's
// rig-profiles editor surface (config.md §10 / ADR 0028). It pairs the
// operator's configured rigs (each with its per-rig overrides) with the
// embedded rigdef catalogue (each model's DEFAULTS), so the editor renders
// "inheriting default X" vs "overridden to Y" by joining rig.model →
// catalogue.id (and derives the default MY_RIG from the matched catalogue name).
type RigsResponse struct {
	DefaultRigID int64             `json:"default_rig_id"`
	Rigs         []types.RigConfig `json:"rigs"`
	Catalogue    []RigDefSummary   `json:"catalogue"`
}

// RigDefSummary is the editor-facing projection of a cat.RigDefinition: the
// identity plus the defaults the editor surfaces. It deliberately omits the
// large Commands/States CAT tables — those are the codec substrate, not
// editable config, and would bloat the payload.
type RigDefSummary struct {
	ID           string                       `json:"id"`
	Name         string                       `json:"name"`
	Manufacturer string                       `json:"manufacturer"`
	Model        string                       `json:"model"`
	Family       string                       `json:"family"`
	Description  string                       `json:"description,omitempty"`
	Ft8Mode      string                       `json:"ft8_mode,omitempty"`
	RigModes     []string                     `json:"rig_modes,omitempty"`
	ModeMappings map[string]types.ModeMapping `json:"mode_mappings,omitempty"`
	Serial       cat.RigSerial                `json:"serial"`
}

// handleRigs serves the rig-profiles editor surface: the operator's configured
// rigs (RigConfig, overrides intact — nil means "inherit the rigdef default")
// and the embedded rigdef catalogue (defaults). Read-only; the write path (save
// a profile / set default) lands with the editor.
//
// Always 200. Independent of the bridge subsystem being enabled — this is config
// editing, not a live rig connection.
func (s *Server) handleRigs(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.Snapshot()

	ids := cat.List()
	catalogue := make([]RigDefSummary, 0, len(ids))
	for _, id := range ids {
		def, ok := cat.Lookup(id)
		if !ok {
			continue
		}
		catalogue = append(catalogue, RigDefSummary{
			ID:           def.ID,
			Name:         def.Name,
			Manufacturer: def.Manufacturer,
			Model:        def.Model,
			Family:       def.Family,
			Description:  def.Description,
			Ft8Mode:      def.Ft8Mode,
			RigModes:     cat.RigModes(def),
			ModeMappings: def.ModeMappings,
			Serial:       def.Serial,
		})
	}

	// Configured rigs as stored. A shallow slice copy decouples the response
	// from the live config backing array; the handler only reads + marshals
	// (no mutation), and the config service never mutates a stored Rigs slice
	// in place (PUT clones — see the config review), so this is race-free.
	rigs := make([]types.RigConfig, len(cfg.Rigs))
	copy(rigs, cfg.Rigs)

	s.writeJSON(w, http.StatusOK, RigsResponse{
		DefaultRigID: cfg.DefaultRigID,
		Rigs:         rigs,
		Catalogue:    catalogue,
	})
}
