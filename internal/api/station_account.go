package api

// The station-account half of /v1/config's forwarders block under config v6
// (W-0021 5C, ADR 0082 parts 3, 4 and 8). A forwarder entry is the station
// ACCOUNT of its destination type — one per type, identified by type. Its
// `name`, `enabled` and logbook-scoped credentials are binding facts: the
// active archive's destination bindings own them, edited through
// /v1/qso-archives/{uuid}/bindings.

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// stationCredentialKeys narrows a set-keys list to the type's declared
// STATION-scoped fields: the masked view of an account never lists a logbook
// key, nor an undeclared one (a legacy ClubLog `api`, say).
func stationCredentialKeys(typeName string, keys []string) []string {
	td, ok := forwarding.DescriptorFor(typeName)
	if !ok {
		return nil
	}
	station := make(map[string]bool, len(td.CredentialFields))
	for _, f := range td.CredentialFields {
		if f.Scope == forwarding.ScopeStation {
			station[f.Key] = true
		}
	}
	var out []string
	for _, k := range keys {
		if station[k] {
			out = append(out, k)
		}
	}
	return out
}

// forwarderPrecheck runs before the config lock for a PUT that carries
// forwarders: the binding-owned refusal, then the read of the bindings the
// save must still build. handled is true when a response was written.
func (s *Server) forwarderPrecheck(ctx context.Context, w http.ResponseWriter, req *ConfigResponse, op errors.Op) (*bindingProbe, bool) {
	if s.refuseBindingOwnedForwarderEdit(w, req, op) {
		return nil, true
	}
	return s.loadBindingProbe(ctx, w, req, op)
}

// bindingProbe is what an account save is checked against: the active
// archive's ENABLED destination bindings, read before the config lock, with
// their logbooks' names for the refusal message.
type bindingProbe struct {
	bindings     []types.LogbookDestination
	logbookNames map[int64]string
}

// loadBindingProbe reads the active archive's enabled bindings for a PUT that
// carries forwarders. An unreadable list refuses the save (handled = true):
// without it the save could leave an enabled binding the next start cannot
// build, and an unbuildable worker stops the daemon starting.
func (s *Server) loadBindingProbe(ctx context.Context, w http.ResponseWriter, req *ConfigResponse, op errors.Op) (*bindingProbe, bool) {
	if req.Forwarders == nil || s.db == nil {
		return &bindingProbe{}, false
	}
	bindings, err := s.db.ListLogbookDestinationsWithContext(ctx)
	if err == nil {
		var logbooks []types.Logbook
		if logbooks, err = s.db.FetchAllLogbooksWithContext(ctx); err == nil {
			probe := &bindingProbe{logbookNames: make(map[int64]string, len(logbooks))}
			for _, lb := range logbooks {
				probe.logbookNames[lb.ID] = lb.Name
			}
			for _, b := range bindings {
				if b.Enabled {
					probe.bindings = append(probe.bindings, b)
				}
			}
			return probe, false
		}
	}
	s.writeServerError(w, op, err, "bindings_read_failed",
		"could not read the active archive's destination bindings to check the station account; nothing was saved")
	return nil, true
}

// forwarderSaveFinding is the construction check a save carrying forwarders
// must pass before it is written: every ENABLED binding of the active archive
// builds with the CANDIDATE station account, exactly as the workers node will
// at the next start (BindingConfig, then the type's constructor). A legacy
// enabled entry still in an unstripped file is checked too — Home's seed
// builds it before seeding. The finding names the binding and its logbook,
// never a value; the cause is for the log only.
func forwarderSaveFinding(cfg config.Config, probe *bindingProbe) (*config.Finding, error) {
	if f, cause := config.ForwarderStartupFinding(cfg.Forwarders); f != nil {
		return f, cause
	}
	accounts := make(map[string]types.ForwarderConfig, len(cfg.Forwarders))
	for _, fc := range cfg.Forwarders {
		accounts[fc.Type] = fc
	}
	for _, b := range probe.bindings {
		account, ok := accounts[b.Destination]
		var err error
		if !ok {
			err = fmt.Errorf("no %s station account", b.Destination)
		} else {
			var fc types.ForwarderConfig
			if fc, err = forwarding.BindingConfig(b, account); err == nil {
				_, err = forwarding.Build(fc)
			}
		}
		if err != nil {
			return &config.Finding{
				Field: "forwarders",
				Code:  "forwarder_unusable",
				Message: fmt.Sprintf("the %s station account cannot be used by the enabled destination binding %q (logbook %q): "+
					"it is missing or its settings are incomplete or invalid; check them", b.Destination, b.ForwarderName, probe.logbookNames[b.LogbookID]),
			}, err
		}
	}
	return nil, nil
}
