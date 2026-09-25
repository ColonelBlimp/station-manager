package main

import (
	"context"
	"encoding/json"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// destinationSeeds turns the legacy config entries into the one-time seed
// (ADR 0082 part 4): one seed per entry carrying its type, its durable name,
// its enabled state and ONLY its logbook-scoped credential keys, as the type's
// descriptor declares them. Station-scoped keys stay in config.json.
//
// It refuses, by entry name, a type with no descriptor in this build and a
// credential blob that is not a JSON object (a string or an array is valid
// JSON inside a valid config file): the seed marker makes a committed seed
// final, forwarder construction runs later and a disabled entry is never
// constructed, so "no credentials" here could never be corrected afterwards.
func destinationSeeds(fwds []types.ForwarderConfig) ([]sqlite.DestinationSeed, error) {
	const op errors.Op = "smd.destinationSeeds"
	seeds := make([]sqlite.DestinationSeed, 0, len(fwds))
	for _, fc := range fwds {
		if _, ok := forwarding.DescriptorFor(fc.Type); !ok {
			return nil, errors.New(op).WithMsgf("forwarder %q: type %q has no descriptor in this build; the binding seed cannot place its credentials", fc.Name, fc.Type)
		}
		seed := sqlite.DestinationSeed{Destination: fc.Type, LegacyName: fc.Name, Enabled: fc.Enabled}
		if len(fc.Credentials) > 0 && string(fc.Credentials) != "null" {
			var all map[string]json.RawMessage
			if err := json.Unmarshal(fc.Credentials, &all); err != nil {
				return nil, errors.New(op).WithMsgf("forwarder %q: credentials are not a JSON object; fix config.json before the binding seed runs", fc.Name)
			}
			subset := make(map[string]json.RawMessage)
			for _, k := range forwarding.LogbookScopedKeys(fc.Type) {
				if v, ok := all[k]; ok {
					subset[k] = v
				}
			}
			if len(subset) > 0 {
				b, err := json.Marshal(subset)
				if err != nil {
					return nil, errors.New(op).WithErr(err).WithMsgf("forwarder %q: marshal logbook-scoped credentials", fc.Name)
				}
				seed.Credentials = b
			}
		}
		seeds = append(seeds, seed)
	}
	return seeds, nil
}

// seedDestinationBindings records the open archive's binding decision ONCE
// (ADR 0082 part 4), after its identity is in place: the adopted (legacy)
// file receives one binding per legacy config entry on each of its logbooks,
// the default logbook keeping the entry's name; a managed or external file
// receives none. The marker makes the decision final, so a later start — with
// whatever config.json then carries — changes nothing, and a logbook created
// afterwards stays unbound. A failure fails the start: the bindings decide
// what the workers node drains and what every enqueue site routes.
func seedDestinationBindings(ctx context.Context, db *sqlite.Service, cfg config.Config, paths archive.Paths, logger *logging.Service) error {
	const op errors.Op = "smd.seedDestinationBindings"
	var seeds []sqlite.DestinationSeed
	legacy := paths.Entry == nil || paths.Entry.Ownership == types.QsoArchiveOwnershipLegacy
	if legacy {
		var err error
		if seeds, err = destinationSeeds(cfg.Forwarders); err != nil {
			return errors.New(op).WithErr(err)
		}
	}
	res, err := db.SeedLogbookDestinationsWithContext(ctx, seeds)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("seed destination bindings")
	}
	if res.Seeded {
		logger.InfoWith().Bool("legacy", legacy).Int("bindings", res.Inserted).Int64("queue_rows_renamed", res.Renamed).
			Msg("startup: destination bindings decided once (the adopted archive seeds from the station's forwarder entries; any other starts with none)")
	}
	return nil
}

// destinationSnapshot is what one start learned about the open archive's
// bindings: the routes that resolved against a station account, the names of
// EVERY binding the file holds, resolved or not, and the disabled ones. The
// workers node discards queued rows for a name outside the name set and for
// every disabled binding; an ENABLED binding that could not be resolved keeps
// its rows for the day its account is fixed.
type destinationSnapshot struct {
	routes       []forwarding.BoundForwarder
	bindingNames map[string]struct{}
	// orderedNames is every binding name in listing order — what the queue
	// endpoints list and accept.
	orderedNames []string
	// disabledNames is every DISABLED binding, resolved or not: ADR 0039's
	// per-binding discard applies to it whether or not its account resolves —
	// a disabled binding has no worker either way.
	disabledNames []string
}

// resolveDestinationRoutes reads the open archive's bindings and resolves each
// against its station account (ADR 0082 parts 5–6). The routes are the ONE
// snapshot the QSO service routes from and the workers node builds from. A
// binding that cannot be resolved — no account of its type, a malformed blob —
// is logged by name and left out of the routes: no worker, no routing. Its
// queue rows follow the binding's OWN state: kept while it is enabled (its
// name stays in the binding-name set), discarded like any disabled binding's
// when it is disabled.
func resolveDestinationRoutes(ctx context.Context, db *sqlite.Service, cfg config.Config, logger *logging.Service) (destinationSnapshot, error) {
	const op errors.Op = "smd.resolveDestinationRoutes"
	bindings, err := db.ListLogbookDestinationsWithContext(ctx)
	if err != nil {
		return destinationSnapshot{}, errors.New(op).WithErr(err).WithMsg("list destination bindings")
	}
	snap := destinationSnapshot{bindingNames: make(map[string]struct{}, len(bindings))}
	for _, b := range bindings {
		snap.bindingNames[b.ForwarderName] = struct{}{}
		snap.orderedNames = append(snap.orderedNames, b.ForwarderName)
		if !b.Enabled {
			snap.disabledNames = append(snap.disabledNames, b.ForwarderName)
		}
	}
	var faults []forwarding.BindingFault
	snap.routes, faults = forwarding.ResolveBindings(bindings, cfg.Forwarders)
	enabled := make(map[string]bool, len(bindings))
	for _, b := range bindings {
		enabled[b.ForwarderName] = b.Enabled
	}
	for _, f := range faults {
		ev := logger.ErrorWith().Str("forwarder", f.ForwarderName).Str("destination", f.Destination).Int64("logbook_id", f.LogbookID).Err(f.Err)
		// The fate of its queued rows depends on the binding's own state, not
		// on the fault: an ENABLED binding keeps them for the day its account
		// is fixed; a DISABLED one has no worker either way and the workers
		// node discards them (ADR 0039, per binding).
		if enabled[f.ForwarderName] {
			ev.Msg("destination binding cannot be resolved; no worker and no routing for it until the station account is fixed (its queued rows are kept)")
		} else {
			ev.Msg("destination binding cannot be resolved and is disabled; its queued rows are discarded like any disabled binding's")
		}
	}
	return snap, nil
}

// routeConfigs is the worker set: one ForwarderConfig per binding, enabled or
// not (the spawner skips and the discard sweep uses the disabled ones).
func routeConfigs(routes []forwarding.BoundForwarder) []types.ForwarderConfig {
	out := make([]types.ForwarderConfig, 0, len(routes))
	for _, r := range routes {
		out = append(out, r.Config)
	}
	return out
}
