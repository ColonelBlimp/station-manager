package config

import (
	"fmt"

	"github.com/ColonelBlimp/station-manager/internal/enums/modes"
)

// Finding is one result from Validate — a config rule outcome carrying a stable
// code (an i18n-key candidate, ADR 0010), a human message, and a severity. The
// CALLER decides disposition (config.md §12): Load aborts fatally on any error
// finding; PUT /v1/config maps error findings to 400; warnings are advisory
// (logged at Load, returned in the PUT response) and never block.
//
// Field is the offending config path when known (e.g. "smtp"); whole-block
// checks may leave it coarse, since Message already carries the context.
type Finding struct {
	Field   string
	Code    string
	Message string
	Warning bool // false = error (fatal at Load / 400 at PUT); true = advisory
}

// Validate is the single source of truth for config rules (config.md §12), run
// by Load (errors → fatal) and — once §12b lands — PUT /v1/config (errors →
// 400). Pure: no mutation. Returns errors and advisory warnings as Findings;
// the caller filters by severity.
//
// §12a consolidates the four standalone validators (forwarders, lookup, smtp,
// bridge) + the non-loopback-bind advisory into this one entry point. Still
// folding in with §12b: rig-catalogue + per-rig mode-mapping validation
// (currently in applyRigProfiles) and the PUT-only field rules (callsign, grid,
// zones, amp/power, ft8_display feed-mode), plus the PUT handler rewired to call
// this instead of its own inline checks.
func Validate(cfg Config) []Finding {
	var out []Finding
	if err := validateForwarders(cfg.Forwarders); err != nil {
		out = append(out, Finding{Field: "forwarders", Code: "invalid_forwarder", Message: err.Error()})
	}
	if err := validateLookup(cfg.Lookup); err != nil {
		out = append(out, Finding{Field: "lookup", Code: "invalid_lookup", Message: err.Error()})
	}
	if err := validateSmtp(cfg.Smtp); err != nil {
		out = append(out, Finding{Field: "smtp", Code: "invalid_smtp", Message: err.Error()})
	}
	if err := validateBridge(cfg.ActiveBridge()); err != nil {
		out = append(out, Finding{Field: "bridge", Code: "invalid_bridge", Message: err.Error()})
	}
	out = append(out, validateRigs(cfg)...)
	// Advisory findings (non-fatal) — currently just the non-loopback-bind notice.
	for _, w := range Warnings(cfg) {
		out = append(out, Finding{Field: "socket_path", Code: "insecure_bind", Message: w, Warning: true})
	}
	return out
}

// validateRigs checks the rig catalogue (config.md §10): positive unique ids,
// non-empty model, default_rig_id resolves, and per-rig mode-mapping ADIF
// validity. Moved here from applyRigProfiles (which now only folds legacy loose
// fields into the catalogue) so the rules live in the one validator and run at
// both Load and PUT.
func validateRigs(cfg Config) []Finding {
	var out []Finding
	rigErr := func(field, msg string) Finding {
		return Finding{Field: field, Code: "invalid_field_value", Message: msg}
	}
	seen := make(map[int64]struct{}, len(cfg.Rigs))
	for i := range cfg.Rigs {
		rc := cfg.Rigs[i]
		if rc.ID <= 0 {
			out = append(out, rigErr(fmt.Sprintf("rigs[%d]", i),
				fmt.Sprintf("rigs[%d]: id must be a positive integer", i)))
		} else if _, dup := seen[rc.ID]; dup {
			out = append(out, rigErr(fmt.Sprintf("rigs[%d]", i),
				fmt.Sprintf("rigs: duplicate id %d", rc.ID)))
		} else {
			seen[rc.ID] = struct{}{}
		}
		if rc.Model == "" {
			out = append(out, rigErr(fmt.Sprintf("rigs[id=%d]", rc.ID),
				fmt.Sprintf("rigs[id=%d]: model must not be empty", rc.ID)))
		}
		for lit, mm := range rc.ModeMappings {
			field := fmt.Sprintf("rigs[id=%d].mode_mappings[%s]", rc.ID, lit)
			if mm.Mode == "" || !modes.IsValidMode(mm.Mode) {
				out = append(out, rigErr(field,
					fmt.Sprintf("%s: mode %q is not a known ADIF main mode", field, mm.Mode)))
			}
			if mm.SubMode != "" && !modes.IsValidSubMode(mm.SubMode) {
				out = append(out, rigErr(field,
					fmt.Sprintf("%s: submode %q is not a known ADIF submode", field, mm.SubMode)))
			}
		}
	}
	if len(cfg.Rigs) > 0 && cfg.RigByID(cfg.DefaultRigID) == nil {
		out = append(out, rigErr("default_rig_id",
			fmt.Sprintf("default_rig_id %d does not match any defined rig", cfg.DefaultRigID)))
	}
	return out
}
