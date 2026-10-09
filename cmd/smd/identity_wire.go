package main

import (
	"context"
	"sort"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// newIdentityForwarder builds an adopted binding's identity forwarder; a test
// seam so a failed construction can be proved to stop the start.
var newIdentityForwarder = smcloud.NewIdentity

// wireSelection is how each adopted SM Cloud binding uploads in this
// generation (ADR 0090 T1; ADR 0091; ruling C1): by identity, through a
// forwarder built here, or held, with no worker. Every other binding keeps
// the wire its type builds.
type wireSelection struct {
	identity map[string]forwarding.Forwarder
	held     map[string]struct{}
}

func (w wireSelection) heldNames() []string {
	names := make([]string, 0, len(w.held))
	for n := range w.held {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// selectWires decides the wire of every enabled adopted SM Cloud binding from
// the start snapshot the routes came from, for the archive file being opened.
// Confirmed for the station account the daemon starts with: the identity
// forwarder, built now. Adopted otherwise (another account, or none
// recorded): held, so its uploads stay queued with no name fallback (ADR
// 0088). A reservation alone changes nothing. A failed construction is an
// error that stops the start: it never falls back to a legacy worker.
func selectWires(ctx context.Context, db *sqlite.Service, cfg config.Config, snap destinationSnapshot) (wireSelection, error) {
	const op errors.Op = "smd.selectWires"
	sel := wireSelection{identity: map[string]forwarding.Forwarder{}, held: map[string]struct{}{}}
	var adopted []types.LogbookDestination
	for _, b := range snap.bindings {
		if b.Destination == smcloud.Type && b.Enabled && b.RemoteAdoptedAt != nil {
			adopted = append(adopted, b)
		}
	}
	if len(adopted) == 0 {
		return sel, nil
	}
	identity, err := db.ArchiveIdentityWithContext(ctx)
	if err != nil {
		return sel, errors.New(op).WithErr(err).WithMsg("read the archive's identity")
	}
	logbooks, err := db.FetchAllLogbooksWithContext(ctx)
	if err != nil {
		return sel, errors.New(op).WithErr(err).WithMsg("list the logbooks")
	}
	byID := make(map[int64]types.Logbook, len(logbooks))
	for _, lb := range logbooks {
		byID[lb.ID] = lb
	}
	routes := make(map[string]forwarding.BoundForwarder, len(snap.routes))
	for _, r := range snap.routes {
		routes[r.Config.Name] = r
	}
	label := ""
	if e := cfg.QsoArchiveByID(identity.ArchiveUUID); e != nil {
		label = e.Label
	}
	account, accountErr := archive.CurrentAccount(cfg, smcloud.Type, identity.ArchiveUUID)
	for _, b := range adopted {
		route, routed := routes[b.ForwarderName]
		if accountErr != nil || !archive.AdoptionConfirmed(b, account) || !routed {
			sel.held[b.ForwarderName] = struct{}{}
			continue
		}
		lb := byID[b.LogbookID]
		f, err := newIdentityForwarder(route.Config, smcloud.IdentityTarget{
			ArchiveUUID: identity.ArchiveUUID, ArchiveLabel: label,
			LogbookUUID: lb.UUID, LogbookLabel: lb.Name, Callsign: lb.Callsign,
		})
		if err != nil {
			return sel, errors.New(op).WithErr(err).WithMsgf("build the identity forwarder for %q; it is not started on the legacy wire", b.ForwarderName)
		}
		sel.identity[b.ForwarderName] = f
	}
	return sel, nil
}
