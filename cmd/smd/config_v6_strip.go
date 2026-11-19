package main

import (
	"context"
	"encoding/json"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// stripLegacyForwarderFieldsAfterSeed removes the deprecated v5 fields from
// config.json once the adopted Home archive's binding seed has committed (ADR
// 0082 part 4, W-0021 5C): each forwarder entry keeps only its station-account
// fields. Never with another archive active — Home's seed is what made the
// fields redundant, and only Home's open file proves it. A failed strip write
// leaves the file as it was and is retried at the next start. No failure here
// fails the start: the bindings, not these fields, route uploads.
//
// No copy of the file is taken first (W-0027 C2e; operator ruling 2026-11-18,
// "remove the downgrade tooling"). The once-only config.v5.json that earlier
// builds wrote before the strip was recovery material for a rollback; it could
// be made only from a version 5 or 6 file, so a file already at a later version
// was never stripped. A config.v5.json an install already has is not read,
// changed or removed.
func (d *daemon) stripLegacyForwarderFieldsAfterSeed(ctx context.Context) {
	if d.paths.Entry != nil && d.paths.Entry.Ownership != types.QsoArchiveOwnershipLegacy {
		// Say so, once per start, while there is something waiting — a silent
		// wait looks like a cleanup that never ran (operator, 2026-10-06).
		if hasLegacyForwarderFields(d.cfgSvc.Snapshot().Forwarders) {
			d.logger.InfoWith().Str("active_archive", d.paths.Entry.Label).
				Msg("config v6: config.json keeps its deprecated forwarder fields until the Home archive is active")
		}
		return
	}
	seededAt, err := d.db.DestinationBindingsSeededAtWithContext(ctx)
	if err != nil || seededAt == nil {
		return
	}
	if !hasLegacyForwarderFields(d.cfgSvc.Snapshot().Forwarders) {
		return
	}
	if _, err := d.cfgSvc.Update(func(c *config.Config) error {
		stripLegacyForwarderFields(c.Forwarders)
		return nil
	}); err != nil {
		d.logger.WarnWith().Err(err).
			Msg("config v6: stripping the deprecated forwarder fields from config.json failed; retried at the next start")
		return
	}
	d.logger.InfoWith().Msg("config v6: config.json now holds station accounts only; names, on/off states and per-logbook credentials live in Home's destination bindings")
}

// hasLegacyForwarderFields reports whether any entry still carries a deprecated
// v5 field: a name, an enabled flag, or a logbook-scoped credential key.
func hasLegacyForwarderFields(fwds []types.ForwarderConfig) bool {
	for _, fc := range fwds {
		if fc.Name != "" || fc.Enabled {
			return true
		}
		creds, ok := credentialMap(fc.Credentials)
		if !ok {
			continue
		}
		for _, k := range forwarding.LogbookScopedKeys(fc.Type) {
			if _, present := creds[k]; present {
				return true
			}
		}
	}
	return false
}

// stripLegacyForwarderFields leaves each entry its station-account fields. A
// credential blob that is not a JSON object is left as it is: what it holds
// cannot be classified, and keeping it is the recoverable choice.
func stripLegacyForwarderFields(fwds []types.ForwarderConfig) {
	for i := range fwds {
		fc := &fwds[i]
		fc.Name = ""
		fc.Enabled = false
		creds, ok := credentialMap(fc.Credentials)
		if !ok {
			continue
		}
		for _, k := range forwarding.LogbookScopedKeys(fc.Type) {
			delete(creds, k)
		}
		fc.Credentials = nil
		if len(creds) > 0 {
			if b, err := json.Marshal(creds); err == nil {
				fc.Credentials = b
			}
		}
	}
}

func credentialMap(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]json.RawMessage{}, true
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false
	}
	return m, true
}
