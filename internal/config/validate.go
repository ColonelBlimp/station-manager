package config

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
	// Advisory findings (non-fatal) — currently just the non-loopback-bind notice.
	for _, w := range Warnings(cfg) {
		out = append(out, Finding{Field: "socket_path", Code: "insecure_bind", Message: w, Warning: true})
	}
	return out
}
