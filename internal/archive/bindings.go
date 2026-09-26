package archive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Binding aggregate states (ADR 0082 part 1).
const (
	BindingStateOn    = "on"
	BindingStateOff   = "off"
	BindingStateMixed = "mixed"
)

// BindingFingerprint is a binding's content as the running daemon saw it at
// start: (name → enabled + credentials hash). GET compares the table with it
// to report restart_required (ADR 0082 part 5).
type BindingFingerprint map[string]string

// FingerprintBindings summarises a binding list for the restart comparison.
func FingerprintBindings(bindings []types.LogbookDestination) BindingFingerprint {
	fp := make(BindingFingerprint, len(bindings))
	for _, b := range bindings {
		sum := sha256.Sum256(b.Credentials)
		fp[b.ForwarderName] = fmt.Sprintf("%v:%s", b.Enabled, hex.EncodeToString(sum[:8]))
	}
	return fp
}

// BindingsDB is what the bindings view and PUT need from the active archive's
// database.
type BindingsDB interface {
	ListLogbookDestinationsWithContext(ctx context.Context) ([]types.LogbookDestination, error)
	FetchAllLogbooksWithContext(ctx context.Context) ([]types.Logbook, error)
	ForwarderQueueCountsWithContext(ctx context.Context) (map[string]sqlite.ForwarderQueueCounts, error)
	UpsertLogbookDestinationsWithContext(ctx context.Context, rows []sqlite.DestinationUpsert) error
}

// smcloudIdentityReason is the ADR 0082 part 7 remnant of the interim gate:
// until the identity-aware server (5F), SM Cloud is bindable on the adopted
// archive only.
const smcloudIdentityReason = "SM Cloud can be bound only on the adopted Home archive until the identity-aware server lands; this archive's QSOs stay local until then"

// BindingsView builds GET /v1/qso-archives/{uuid}/bindings for the ACTIVE
// archive (entry nil = the not-yet-catalogued adopted file). atStart is the
// running daemon's fingerprint of the bindings it started with.
func BindingsView(ctx context.Context, db BindingsDB, cfg config.Config, entry *types.QsoArchiveConfig, atStart BindingFingerprint) (types.ArchiveBindingsView, error) {
	bindings, err := db.ListLogbookDestinationsWithContext(ctx)
	if err != nil {
		return types.ArchiveBindingsView{}, err
	}
	logbooks, err := db.FetchAllLogbooksWithContext(ctx)
	if err != nil {
		return types.ArchiveBindingsView{}, err
	}
	counts, err := db.ForwarderQueueCountsWithContext(ctx)
	if err != nil {
		return types.ArchiveBindingsView{}, err
	}
	view := types.ArchiveBindingsView{RestartRequired: !fingerprintEqual(FingerprintBindings(bindings), atStart)}
	if entry != nil {
		view.ArchiveID, view.ArchiveLabel = entry.ID, entry.Label
	}
	byKey := make(map[string]types.LogbookDestination, len(bindings))
	for _, b := range bindings {
		byKey[fmt.Sprintf("%d/%s", b.LogbookID, b.Destination)] = b
	}
	accounts := accountsByType(cfg)
	for _, td := range forwarding.ForwarderTypes() {
		dv := types.DestinationBindingView{Type: td.Type, DisplayName: td.DisplayName}
		dv.Account = accountView(td, accounts[td.Type])
		_, hasEntry := accounts[td.Type]
		if reason := enableRefusal(td.Type, dv.Account, hasEntry, entry); reason != "" {
			dv.Reason = reason
		}
		enabled, total := 0, 0
		for _, lb := range logbooks {
			row := types.LogbookBindingView{LogbookID: lb.ID, LogbookUUID: lb.UUID, LogbookName: lb.Name, LogbookCallsign: lb.Callsign}
			if b, ok := byKey[fmt.Sprintf("%d/%s", lb.ID, td.Type)]; ok {
				row.Bound, row.Enabled, row.ForwarderName = true, b.Enabled, b.ForwarderName
				row.CredentialsSet = keysSet(b.Credentials)
				c := counts[b.ForwarderName]
				row.Queue = types.BindingQueueCount{Waiting: c.Waiting, Failed: c.Failed, InFlight: c.InFlight}
				if b.Enabled {
					enabled++
				}
			}
			total++
			dv.Logbooks = append(dv.Logbooks, row)
		}
		// A destination with no station entry and no binding here is left out:
		// the app cannot give it an account (SM Cloud is never auto-seeded — it
		// has no canonical URL), so listing it offered only an unactionable
		// "no station account" (fresh install, 2026-09-26). A bound one stays,
		// so its rows and queue never disappear.
		if !hasEntry && !anyBound(dv.Logbooks) {
			continue
		}
		switch {
		case total > 0 && enabled == total:
			dv.State = BindingStateOn
		case enabled == 0:
			dv.State = BindingStateOff
		default:
			dv.State = BindingStateMixed
		}
		view.Destinations = append(view.Destinations, dv)
	}
	return view, nil
}

func anyBound(rows []types.LogbookBindingView) bool {
	for _, r := range rows {
		if r.Bound {
			return true
		}
	}
	return false
}

// applyBindings validates the WHOLE candidate of a PUT against the stored
// rows, then writes every affected row in one transaction and returns the
// fresh view (ADR 0082 parts 1, 8, 9). A refusal names the destination, the
// logbook and the field; nothing is written on a refusal. Every row that ends
// ENABLED is synthesized exactly as the next start will (BindingConfig over
// its station account) and built: a binding the daemon could not start with
// is refused here, not discovered as a startup failure (5D review, P1). The
// caller serializes the whole operation (Manager.ApplyBindings). log may be
// nil; it receives a refused build's cause, which never reaches the wire.
func applyBindings(ctx context.Context, log *logging.Service, db BindingsDB, cfg config.Config, entry *types.QsoArchiveConfig, atStart BindingFingerprint, req types.ArchiveBindingsRequest) (types.ArchiveBindingsView, error) {
	bindings, err := db.ListLogbookDestinationsWithContext(ctx)
	if err != nil {
		return types.ArchiveBindingsView{}, err
	}
	logbooks, err := db.FetchAllLogbooksWithContext(ctx)
	if err != nil {
		return types.ArchiveBindingsView{}, err
	}
	lbByID := make(map[int64]types.Logbook, len(logbooks))
	for _, lb := range logbooks {
		lbByID[lb.ID] = lb
	}
	existing := make(map[string]types.LogbookDestination, len(bindings))
	for _, b := range bindings {
		existing[fmt.Sprintf("%d/%s", b.LogbookID, b.Destination)] = b
	}
	accounts := accountsByType(cfg)
	var rows []sqlite.DestinationUpsert
	seen := map[string]struct{}{}
	for _, d := range req.Destinations {
		td, ok := forwarding.DescriptorFor(d.Type)
		if !ok {
			return types.ArchiveBindingsView{}, &RequestError{Code: "invalid_field_value", Message: fmt.Sprintf("destination %q is not a type this build knows", d.Type)}
		}
		account := accountView(td, accounts[d.Type])
		_, hasEntry := accounts[d.Type]
		for _, e := range d.Logbooks {
			key := fmt.Sprintf("%d/%s", e.LogbookID, d.Type)
			if _, dup := seen[key]; dup {
				return types.ArchiveBindingsView{}, &RequestError{Code: "invalid_field_value", Message: fmt.Sprintf("logbook %d appears twice for %s", e.LogbookID, d.Type)}
			}
			seen[key] = struct{}{}
			lb, ok := lbByID[e.LogbookID]
			if !ok {
				return types.ArchiveBindingsView{}, &RequestError{Code: "logbook_not_found", Message: fmt.Sprintf("logbook %d does not exist in this archive", e.LogbookID)}
			}
			if e.Enabled {
				if reason := enableRefusal(d.Type, account, hasEntry, entry); reason != "" {
					return types.ArchiveBindingsView{}, &RequestError{Code: "binding_not_enableable", Message: fmt.Sprintf("%s for logbook %q: %s", td.DisplayName, lb.Name, reason)}
				}
			}
			merged, err := mergeBindingCredentials(td, existing[key].Credentials, e.Credentials, e.CredentialsClear, e.Enabled, lb.Callsign)
			if err != nil {
				return types.ArchiveBindingsView{}, &RequestError{Code: err.code, Message: fmt.Sprintf("%s for logbook %q: %s", td.DisplayName, lb.Name, err.msg)}
			}
			name := existing[key].ForwarderName
			if name == "" {
				if lb.UUID == "" {
					return types.ArchiveBindingsView{}, &RequestError{Code: "logbook_no_identity", Message: fmt.Sprintf("logbook %q has no uuid yet; restart the daemon once", lb.Name)}
				}
				name = d.Type + "." + lb.UUID
			}
			if e.Enabled {
				if refusal := buildCandidate(log, td, lb, types.LogbookDestination{
					LogbookID: e.LogbookID, Destination: d.Type, ForwarderName: name, Enabled: true, Credentials: merged,
				}, accounts[d.Type]); refusal != nil {
					return types.ArchiveBindingsView{}, refusal
				}
			}
			rows = append(rows, sqlite.DestinationUpsert{LogbookID: e.LogbookID, Destination: d.Type, ForwarderName: name, Enabled: e.Enabled, Credentials: merged})
		}
	}
	if err := db.UpsertLogbookDestinationsWithContext(ctx, rows); err != nil {
		return types.ArchiveBindingsView{}, err
	}
	return BindingsView(ctx, db, cfg, entry, atStart)
}

// buildCandidate constructs one candidate binding the way the workers node
// will at the next start — BindingConfig over its station account, then the
// type's constructor — and refuses it as binding_unusable when either fails.
// The wire message names the destination and the logbook only; the cause
// (field and fault, never a value — the registry rule) goes to the log.
func buildCandidate(log *logging.Service, td forwarding.TypeDescriptor, lb types.Logbook, candidate types.LogbookDestination, account types.ForwarderConfig) error {
	fc, err := forwarding.BindingConfig(candidate, account)
	if err == nil {
		_, err = forwarding.Build(fc)
	}
	if err == nil {
		return nil
	}
	if log != nil {
		log.WarnWith().Str("forwarder", candidate.ForwarderName).Str("destination", candidate.Destination).
			Int64("logbook_id", candidate.LogbookID).Err(err).
			Msg("bindings: an enabled binding cannot be constructed with these settings; the PUT is refused and nothing was written")
	}
	return &RequestError{Code: "binding_unusable", Message: fmt.Sprintf(
		"%s for logbook %q cannot be turned on with these settings: its credentials are incomplete or invalid; check them", td.DisplayName, lb.Name)}
}

type bindingRefusal struct{ code, msg string }

func (e *bindingRefusal) Error() string { return e.code + ": " + e.msg }

// mergeBindingCredentials lays the typed fields over the stored ones, removes
// the cleared ones (only when the row ends disabled), keeps only the type's
// LOGBOOK-scoped keys, and — when the row ends enabled — requires every
// logbook-scoped field that is not Clearable to hold a value.
func mergeBindingCredentials(td forwarding.TypeDescriptor, stored json.RawMessage, typed map[string]string, clear []string, enabled bool, logbookCallsign string) (json.RawMessage, *bindingRefusal) {
	merged := map[string]json.RawMessage{}
	if len(stored) > 0 && string(stored) != "null" {
		if err := json.Unmarshal(stored, &merged); err != nil {
			return nil, &bindingRefusal{"binding_credentials_corrupt", "the stored credentials are not a JSON object; fix the archive file before editing this binding"}
		}
	}
	scoped := map[string]forwarding.CredentialField{}
	for _, f := range td.CredentialFields {
		if f.Scope == forwarding.ScopeLogbook {
			scoped[f.Key] = f
		}
	}
	for k := range merged {
		if _, ok := scoped[k]; !ok {
			delete(merged, k)
		}
	}
	for k, v := range typed {
		if _, ok := scoped[k]; !ok {
			return nil, &bindingRefusal{"invalid_field_value", fmt.Sprintf("%q is not a logbook-scoped field of %s", k, td.Type)}
		}
		if strings.TrimSpace(v) == "" {
			continue // blank keeps the stored value
		}
		b, _ := json.Marshal(v)
		merged[k] = b
	}
	// A cleared key must be one the binding owns, exactly like a typed one
	// (5D review, P2): a station-scoped or undeclared key cannot be removed
	// here and must not be reported as removed. A key both typed and cleared
	// is a contradiction, not a last-one-wins.
	for _, k := range clear {
		if _, ok := scoped[k]; !ok {
			return nil, &bindingRefusal{"invalid_field_value", fmt.Sprintf("%q is not a logbook-scoped field of %s and cannot be cleared here", k, td.Type)}
		}
		if v, typedToo := typed[k]; typedToo && strings.TrimSpace(v) != "" {
			return nil, &bindingRefusal{"invalid_field_value", fmt.Sprintf("%q is both typed and cleared", k)}
		}
	}
	if len(clear) > 0 && enabled {
		return nil, &bindingRefusal{"binding_clear_requires_disabled", "a stored field can be removed only from a binding that ends disabled"}
	}
	for _, k := range clear {
		delete(merged, k)
	}
	if enabled {
		fillDefaults(td, merged, logbookCallsign)
		for _, f := range td.CredentialFields {
			if f.Scope != forwarding.ScopeLogbook || f.Clearable {
				continue
			}
			if v, ok := merged[f.Key]; !ok || strings.TrimSpace(strings.Trim(string(v), `"`)) == "" {
				return nil, &bindingRefusal{"binding_field_required", fmt.Sprintf("%s is required to turn this destination on", f.Label)}
			}
		}
	}
	if len(merged) == 0 {
		return nil, nil
	}
	out, err := json.Marshal(merged)
	if err != nil {
		return nil, &bindingRefusal{"invalid_field_value", "credentials could not be encoded"}
	}
	return out, nil
}

// fillDefaults sets each field declaring DefaultsTo that holds no value from
// its source, for a row that ENDS ENABLED (ADR 0082 part 3, ruled
// 2026-09-26). The value is written into the stored blob, so it commits with
// the binding and a later change at the source never retargets uploads; a
// stored or typed value is never replaced. A blank source fills nothing, and
// the required check then names the field.
func fillDefaults(td forwarding.TypeDescriptor, merged map[string]json.RawMessage, logbookCallsign string) {
	source := strings.TrimSpace(logbookCallsign)
	for _, f := range td.CredentialFields {
		if f.DefaultsTo != forwarding.DefaultsToLogbookCallsign || source == "" {
			continue
		}
		if v, ok := merged[f.Key]; ok && strings.TrimSpace(strings.Trim(string(v), `"`)) != "" {
			continue
		}
		b, _ := json.Marshal(source)
		merged[f.Key] = b
	}
}

// enableRefusal names why a destination cannot be turned on in this archive:
// no station entry in config.json at all, an entry missing a station field,
// or SM Cloud outside the adopted archive (5F remnant). The wording states the
// gap only; the SPA adds the way to fix it where one exists here.
func enableRefusal(typ string, account types.StationAccountView, hasEntry bool, entry *types.QsoArchiveConfig) string {
	if !hasEntry {
		return "this destination has no station account in config.json"
	}
	if !account.Configured {
		return "its station account is incomplete: a required station field is not set"
	}
	adopted := entry == nil || entry.Ownership == types.QsoArchiveOwnershipLegacy
	if typ == "smcloud" && !adopted {
		return smcloudIdentityReason
	}
	return ""
}

func accountsByType(cfg config.Config) map[string]types.ForwarderConfig {
	out := make(map[string]types.ForwarderConfig, len(cfg.Forwarders))
	for _, fc := range cfg.Forwarders {
		out[fc.Type] = fc
	}
	return out
}

// accountView reports a station account's presence: configured when config
// holds an entry of the type AND every station-scoped field that is not
// Clearable holds a value.
func accountView(td forwarding.TypeDescriptor, fc types.ForwarderConfig) types.StationAccountView {
	buildKey := ""
	if present, applicable := forwarding.BuildKeyPresent(td.Type); applicable {
		buildKey = "absent"
		if present {
			buildKey = "present"
		}
	}
	if fc.Type == "" {
		return types.StationAccountView{BuildKey: buildKey}
	}
	set := keysSet(fc.Credentials)
	setMap := map[string]struct{}{}
	for _, k := range set {
		setMap[k] = struct{}{}
	}
	configured := true
	var stationSet []string
	for _, f := range td.CredentialFields {
		if f.Scope != forwarding.ScopeStation {
			continue
		}
		if _, ok := setMap[f.Key]; ok {
			stationSet = append(stationSet, f.Key)
		} else if !f.Clearable {
			configured = false
		}
	}
	return types.StationAccountView{Configured: configured, Label: fc.Label, FieldsSet: stationSet, BuildKey: buildKey}
}

// keysSet lists the keys of a credential blob that hold a non-empty string,
// sorted; never the values.
func keysSet(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	var out []string
	for k, v := range m {
		if strings.TrimSpace(strings.Trim(string(v), `"`)) != "" && string(v) != "null" {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func fingerprintEqual(a, b BindingFingerprint) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// SetActiveBindings wires the active archive's database and the bindings the
// running daemon started with (cmd/smd, at HTTP init).
func (m *Manager) SetActiveBindings(db BindingsDB, atStart []types.LogbookDestination) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeDB = db
	m.atStart = FingerprintBindings(atStart)
}

// activeEntryForBindings resolves id to the ACTIVE archive's catalogue entry:
// bindings are edited on the active archive only, whose file is the one open
// (ADR 0082 part 9, ruled 2026-09-25). Another archive is refused
// archive_not_active, an unknown id archive_not_found. db is the database the
// caller captured under m.mu.
func (m *Manager) activeEntryForBindings(id string, db BindingsDB) (*types.QsoArchiveConfig, config.Config, error) {
	snap := m.cfg.Snapshot()
	if db == nil {
		return nil, snap, &RequestError{Code: "bindings_unavailable", Message: "this daemon has no active archive database wired for bindings"}
	}
	entry := snap.QsoArchiveByID(id)
	if entry == nil {
		return nil, snap, &RequestError{Code: "archive_not_found", Message: "no archive with that id in the catalogue"}
	}
	if snap.ActiveQsoArchiveID != id {
		return nil, snap, &RequestError{Code: "archive_not_active", Message: "bindings are edited on the active archive only; activate this archive first"}
	}
	return entry, snap, nil
}

// Bindings serves GET /v1/qso-archives/{uuid}/bindings through the port.
func (m *Manager) Bindings(ctx context.Context, id string) (types.ArchiveBindingsView, error) {
	m.mu.Lock()
	db, atStart := m.activeDB, m.atStart
	m.mu.Unlock()
	entry, snap, err := m.activeEntryForBindings(id, db)
	if err != nil {
		return types.ArchiveBindingsView{}, err
	}
	return BindingsView(ctx, db, snap, entry, atStart)
}

// ApplyBindings serves PUT /v1/qso-archives/{uuid}/bindings through the port.
// The WHOLE operation — read the stored rows, merge the masked edit onto them,
// validate and build, write — runs under bindingsMu (5D review, P1): two PUTs
// editing disjoint fields of one binding would otherwise both merge onto the
// same old blob and the last writer would restore the other's old value. A
// process lock suffices because the running daemon is the only writer of the
// open archive's bindings: the seed runs before HTTP serves, and the offline
// commands (import, restore, db-downgrade) run with the daemon stopped.
func (m *Manager) ApplyBindings(ctx context.Context, id string, req types.ArchiveBindingsRequest) (types.ArchiveBindingsView, error) {
	m.bindingsMu.Lock()
	defer m.bindingsMu.Unlock()
	m.mu.Lock()
	db, atStart := m.activeDB, m.atStart
	m.mu.Unlock()
	entry, snap, err := m.activeEntryForBindings(id, db)
	if err != nil {
		return types.ArchiveBindingsView{}, err
	}
	return applyBindings(ctx, m.logger, db, snap, entry, atStart, req)
}
