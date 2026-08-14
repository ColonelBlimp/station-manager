package lookup

import (
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/lookupdef"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// fillCallsignBlanks applies ADR 0068's authority rule: preferred data never
// moves, while a lower-priority provider may fill every callsign-owned blank it
// happens to know. Call is the canonical lookup input; country-class and cache
// metadata fields are deliberately absent from this list.
func fillCallsignBlanks(preferred, fallback types.ContactedStation) types.ContactedStation {
	fill := func(dst *string, src string) {
		if strings.TrimSpace(*dst) == "" && strings.TrimSpace(src) != "" {
			*dst = src
		}
	}

	fill(&preferred.Address, fallback.Address)
	fill(&preferred.Age, fallback.Age)
	fill(&preferred.Altitude, fallback.Altitude)
	fill(&preferred.ContactedOp, fallback.ContactedOp)
	fill(&preferred.Email, fallback.Email)
	fill(&preferred.EqCall, fallback.EqCall)
	fill(&preferred.Gridsquare, fallback.Gridsquare)
	fill(&preferred.Iota, fallback.Iota)
	fill(&preferred.IotaIslandId, fallback.IotaIslandId)
	fill(&preferred.Lat, fallback.Lat)
	fill(&preferred.Lon, fallback.Lon)
	fill(&preferred.Name, fallback.Name)
	fill(&preferred.QTH, fallback.QTH)
	fill(&preferred.Sig, fallback.Sig)
	fill(&preferred.SigInfo, fallback.SigInfo)
	fill(&preferred.Web, fallback.Web)
	fill(&preferred.WwffRef, fallback.WwffRef)
	return preferred
}

func completionFieldBlank(station types.ContactedStation, field string) bool {
	switch field {
	case lookupdef.CompletionFieldName:
		return strings.TrimSpace(station.Name) == ""
	case lookupdef.CompletionFieldGridsquare:
		return strings.TrimSpace(station.Gridsquare) == ""
	default:
		// Config validation refuses unknown fields. A directly-constructed
		// Orchestrator fails safe by exhausting the chain, not by declaring an
		// unrecognised completion requirement satisfied.
		return true
	}
}

func (o *Orchestrator) needsNextCallsignProvider(station types.ContactedStation) bool {
	if IsEmpty(station) {
		return true
	}
	for _, field := range o.ContinueIfBlank {
		if completionFieldBlank(station, field) {
			return true
		}
	}
	return false
}
