package archive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
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
// start: (name → enabled + credentials hash + adopted + the account it was
// confirmed under). GET compares the table with it to report restart_required
// (ADR 0082 part 5); an adoption recorded since the start, or re-confirmed
// under another account, switches the binding's wire at the next one (ADR
// 0090, T1; ADR 0091).
type BindingFingerprint map[string]string

// FingerprintBindings summarises a binding list for the restart comparison.
func FingerprintBindings(bindings []types.LogbookDestination) BindingFingerprint {
	fp := make(BindingFingerprint, len(bindings))
	for _, b := range bindings {
		sum := sha256.Sum256(b.Credentials)
		fp[b.ForwarderName] = fmt.Sprintf("%v:%s:%v:%s:%v", b.Enabled, hex.EncodeToString(sum[:8]), b.RemoteAdoptedAt != nil, b.RemoteAdoptedAccount,
			b.IdentitySince != nil)
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
	RecordLogbookDestinationAdoptedWithContext(ctx context.Context, forwarderName string, logbookID int64, credentials json.RawMessage, account string) (bool, error)
	ReserveLogbookDestinationAdoptionWithContext(ctx context.Context, forwarderName string, logbookID int64, credentials json.RawMessage, account string) (bool, error)
	ListAdoptionClaimsWithContext(ctx context.Context) ([]types.LogbookDestination, error)
	LogbookSmcloudHistoryWithContext(ctx context.Context, logbookID int64) (sqlite.LogbookHistory, error)
}

// The SM Cloud enable gate's reasons (W-0021 5F.4, ADR 0092; rulings F5,
// J1–J6, 2026-10-12). A row prefixes them "Can't be turned on for this
// logbook:", so each starts lowercase unless it names SM Cloud.
const (
	reasonHistoryRecords = "previous SM Cloud upload history exists; automatic conversion needs manual recovery"
	reasonHistoryQsos    = "this logbook already holds QSOs and the archive cannot show that none reached SM Cloud; automatic conversion needs manual recovery"
	reasonRunning        = "this binding still uploads by its Cloud logbook name until the daemon restarts; restart, then turn it on"
	reasonClaimed        = "this binding belongs to an SM Cloud adoption; re-enabling it is supported only in the Home archive"
	reasonConflict       = "this binding carries conflicting identity and adoption markers; manual recovery is required"
	// supportUnverifiedMessage is the SM Cloud card's line while the start
	// could not reach the server (ruling J3).
	supportUnverifiedMessage = "SM Cloud server support is not verified: the server could not be reached at startup. Uploads wait and retry until a supporting server is available."
)

// supportReasons is why a new identity enable waits on the start's check;
// the allowing states have none.
var supportReasons = map[smcloud.SupportState]string{
	smcloud.SupportChecking:    "SM Cloud server support is still being checked; try again shortly",
	smcloud.SupportUnsupported: "the SM Cloud server does not support per-logbook backups; upgrade it",
	smcloud.SupportError:       "SM Cloud server support could not be checked; the daemon log has the cause",
	smcloud.SupportChanged:     "the SM Cloud server changed since the daemon started; restart to check it",
}

// gate is what the SM Cloud enable rules need from the running daemon: the
// start's support check as the saved config sees it, and the bindings this
// generation started with. The zero gate reads support as still checking and
// knows no running generation.
type gate struct {
	support func(config.Config) smcloud.SupportState
	start   map[string]types.LogbookDestination
}

func (g gate) supportState(cfg config.Config) smcloud.SupportState {
	if g.support == nil {
		return smcloud.SupportChecking
	}
	return g.support(cfg)
}

// smcloudPlan is what turning one SM Cloud row on would mean.
type smcloudPlan struct {
	reason       string // why it cannot be turned on; "" when it can
	stamp        bool   // the enable marks it born on identity (ADR 0092)
	requireEmpty bool   // the stamp re-asserts an empty logbook (ruling J1)
}

// planSmcloud judges a new SM Cloud enable of logbookID (0: a logbook created
// now, so empty and never the default), whose stored binding is b (nil when
// none), in the rules' order (ruling J6): the claim and conflict rules, then
// history, then the running generation, then the server's support. An already
// enabled binding is not a transition and keeps every rule it has. The name
// collision check comes after, over the whole candidate.
func (g gate) planSmcloud(ctx context.Context, db BindingsDB, cfg config.Config, entry *types.QsoArchiveConfig, logbookID int64, b *types.LogbookDestination) (smcloudPlan, error) {
	home := adoptedArchive(entry)
	isDefault := logbookID != 0 && logbookID == cfg.DefaultLogbookID
	switch {
	case b != nil && IdentityConflict(*b):
		return smcloudPlan{reason: reasonConflict}, nil
	case b != nil && BornOnIdentity(*b):
		return smcloudPlan{}, nil // its wire is decided: no history, running or support rule
	case b != nil && adoptionClaimed(*b):
		// The claim fixes the owner: in Home a claimed binding turns on again
		// on its own logbook, default or not, and keeps its adoption path
		// (ADR 0097 §5). Elsewhere only an enabled one stays as it is.
		if home || b.Enabled {
			return smcloudPlan{}, nil
		}
		return smcloudPlan{reason: reasonClaimed}, nil
	case home && isDefault, b != nil && b.Enabled:
		return smcloudPlan{}, nil // the adoption path, or a kept legacy binding
	}
	plan := smcloudPlan{stamp: true}
	if home {
		plan.requireEmpty = true
		if logbookID != 0 {
			h, err := db.LogbookSmcloudHistoryWithContext(ctx, logbookID)
			if err != nil {
				return smcloudPlan{}, err
			}
			switch {
			case h.SmcloudUploads > 0:
				return smcloudPlan{reason: reasonHistoryRecords}, nil
			case h.Qsos > 0:
				return smcloudPlan{reason: reasonHistoryQsos}, nil
			}
		}
		if b != nil {
			if s, ok := g.start[b.ForwarderName]; ok && s.Enabled && s.IdentitySince == nil {
				return smcloudPlan{reason: reasonRunning}, nil
			}
		}
	}
	if reason := supportReasons[g.supportState(cfg)]; reason != "" {
		return smcloudPlan{reason: reason}, nil
	}
	return plan, nil
}

// smcloudHidden reports whether an SM Cloud row does not use its legacy Cloud
// logbook name (ruling J5): a clean born-identity binding, or one whose next
// enable can only be by identity. A claimed binding's name is locked instead,
// never hidden; Home's unmarked default and a kept enabled legacy binding
// still upload by it.
func smcloudHidden(entry *types.QsoArchiveConfig, logbookID, defaultID int64, b *types.LogbookDestination) bool {
	switch {
	case b != nil && adoptionClaimed(*b):
		return false
	case b != nil && b.IdentitySince != nil:
		return true
	case adoptedArchive(entry) && logbookID == defaultID:
		return false
	case b != nil && b.Enabled:
		return false
	}
	return true
}

func adoptedArchive(entry *types.QsoArchiveConfig) bool {
	return entry == nil || entry.Ownership == types.QsoArchiveOwnershipLegacy
}

// BindingsView builds GET /v1/qso-archives/{uuid}/bindings for the ACTIVE
// archive (entry nil = the not-yet-catalogued adopted file). atStart is the
// running daemon's fingerprint of the bindings it started with.
func BindingsView(ctx context.Context, db BindingsDB, cfg config.Config, entry *types.QsoArchiveConfig, atStart BindingFingerprint) (types.ArchiveBindingsView, error) {
	return gate{}.bindingsView(ctx, db, cfg, entry, atStart)
}

func (g gate) bindingsView(ctx context.Context, db BindingsDB, cfg config.Config, entry *types.QsoArchiveConfig, atStart BindingFingerprint) (types.ArchiveBindingsView, error) {
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
		dv.NewLogbookReason = dv.Reason
		smc := td.Type == smcloud.Type
		if smc && hasEntry {
			state := g.supportState(cfg)
			dv.Support = &types.DestinationSupportView{State: string(state)}
			if state == smcloud.SupportUnverified {
				dv.Support.Message = supportUnverifiedMessage
			}
		}
		if smc && dv.NewLogbookReason == "" {
			plan, err := g.planSmcloud(ctx, db, cfg, entry, 0, nil)
			if err != nil {
				return types.ArchiveBindingsView{}, err
			}
			dv.NewLogbookReason = plan.reason
		}
		enabled, total := 0, 0
		for _, lb := range logbooks {
			row := types.LogbookBindingView{LogbookID: lb.ID, LogbookUUID: lb.UUID, LogbookName: lb.Name, LogbookCallsign: lb.Callsign}
			var stored *types.LogbookDestination
			if b, ok := byKey[fmt.Sprintf("%d/%s", lb.ID, td.Type)]; ok {
				stored = &b
				row.Bound, row.Enabled, row.ForwarderName = true, b.Enabled, b.ForwarderName
				row.CredentialsSet = keysSet(b.Credentials)
				if adoptionClaimed(b) {
					row.LockedFields = adoptionKeys(td)
				}
				c := counts[b.ForwarderName]
				row.Queue = types.BindingQueueCount{Waiting: c.Waiting, Failed: c.Failed, InFlight: c.InFlight}
				if b.Enabled {
					enabled++
				}
			}
			if smc && smcloudHidden(entry, lb.ID, cfg.DefaultLogbookID, stored) {
				row.HiddenFields = adoptionKeys(td)
			}
			if smc && dv.Reason == "" && !row.Enabled {
				plan, err := g.planSmcloud(ctx, db, cfg, entry, lb.ID, stored)
				if err != nil {
					return types.ArchiveBindingsView{}, err
				}
				row.Reason = plan.reason
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
	return gate{}.applyBindings(ctx, log, db, cfg, entry, atStart, req)
}

func (g gate) applyBindings(ctx context.Context, log *logging.Service, db BindingsDB, cfg config.Config, entry *types.QsoArchiveConfig, atStart BindingFingerprint, req types.ArchiveBindingsRequest) (types.ArchiveBindingsView, error) {
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
			var stored *types.LogbookDestination
			if b, ok := existing[key]; ok {
				stored = &b
			}
			plan, rerr := g.editRules(ctx, db, cfg, entry, td, account, hasEntry, lb, e, stored)
			if rerr != nil {
				return types.ArchiveBindingsView{}, rerr
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
			rows = append(rows, sqlite.DestinationUpsert{LogbookID: e.LogbookID, Destination: d.Type, ForwarderName: name, Enabled: e.Enabled, Credentials: merged,
				StampIdentity: plan.stamp, RequireEmptyLogbook: plan.requireEmpty})
		}
	}
	if err := refuseProtectedNames(ctx, db, bindings, rows, lbByID); err != nil {
		return types.ArchiveBindingsView{}, err
	}
	if err := db.UpsertLogbookDestinationsWithContext(ctx, rows); err != nil {
		return types.ArchiveBindingsView{}, err
	}
	return g.bindingsView(ctx, db, cfg, entry, atStart)
}

// editRules applies the enable and field rules to one edit, in ruling J6's
// order (account, then SM Cloud's claim, history, running-generation and
// support rules), then the field rules: a hidden field (J5) and a locked one
// (ADR 0090, T6). It returns the SM Cloud plan the edit's write carries; a
// row ending disabled, or another destination, carries none.
func (g gate) editRules(ctx context.Context, db BindingsDB, cfg config.Config, entry *types.QsoArchiveConfig, td forwarding.TypeDescriptor,
	account types.StationAccountView, hasEntry bool, lb types.Logbook, e types.LogbookBindingEdit, stored *types.LogbookDestination) (smcloudPlan, error) {
	var plan smcloudPlan
	if e.Enabled {
		reason := enableRefusal(td.Type, account, hasEntry, entry)
		if reason == "" && td.Type == smcloud.Type {
			var err error
			if plan, err = g.planSmcloud(ctx, db, cfg, entry, lb.ID, stored); err != nil {
				return smcloudPlan{}, err
			}
			reason = plan.reason
		}
		if reason != "" {
			return smcloudPlan{}, &RequestError{Code: "binding_not_enableable", Message: fmt.Sprintf("%s for logbook %q: %s", td.DisplayName, lb.Name, reason)}
		}
	}
	if td.Type == smcloud.Type && smcloudHidden(entry, lb.ID, cfg.DefaultLogbookID, stored) {
		if k := lockedKeyTouched(td, e); k != "" {
			return smcloudPlan{}, &RequestError{Code: "binding_field_unused", Message: fmt.Sprintf(
				"%s for logbook %q: %q is not used by a binding that uploads by SM Cloud identity and cannot be set or removed", td.DisplayName, lb.Name, k)}
		}
	}
	if stored != nil && adoptionClaimed(*stored) {
		if k := lockedKeyTouched(td, e); k != "" {
			return smcloudPlan{}, &RequestError{Code: "binding_field_locked", Message: fmt.Sprintf(
				"%s for logbook %q: %q is fixed since this binding was reserved for adoption and cannot be changed or removed", td.DisplayName, lb.Name, k)}
		}
	}
	return plan, nil
}

// lockedKeyTouched names the first adoption key the edit types a value into or
// clears; "" when it touches none. A blank typed value keeps the stored one, so
// it is not a change. Any other typed value is refused, even an equal one: the
// SPA never sends the hidden field, and comparing would need the type's own
// normalization.
func lockedKeyTouched(td forwarding.TypeDescriptor, e types.LogbookBindingEdit) string {
	for _, k := range adoptionKeys(td) {
		if v, ok := e.Credentials[k]; ok && strings.TrimSpace(v) != "" {
			return k
		}
		if slices.Contains(e.CredentialsClear, k) {
			return k
		}
	}
	return ""
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

// SetSmcloudSupport wires the start's SM Cloud support check (cmd/smd): the
// state a new identity enable is judged by, for the config saved now.
func (m *Manager) SetSmcloudSupport(state func(config.Config) smcloud.SupportState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.smcloudSupport = state
}

// gateNow is the gate as wired now, for one view or PUT.
func (m *Manager) gateNow() gate {
	m.mu.Lock()
	defer m.mu.Unlock()
	return gate{support: m.smcloudSupport, start: m.startBindings}
}

// SetActiveBindings wires the active archive's database and the bindings the
// running daemon started with (cmd/smd, at HTTP init).
func (m *Manager) SetActiveBindings(db BindingsDB, atStart []types.LogbookDestination) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeDB = db
	m.atStart = FingerprintBindings(atStart)
	m.startBindings = make(map[string]types.LogbookDestination, len(atStart))
	for _, b := range atStart {
		m.startBindings[b.ForwarderName] = b
	}
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
	view, err := m.gateNow().bindingsView(ctx, db, snap, entry, atStart)
	if err != nil {
		return types.ArchiveBindingsView{}, err
	}
	return view, m.annotateAdoption(ctx, db, snap, entry, &view)
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
	view, err := m.gateNow().applyBindings(ctx, m.logger, db, snap, entry, atStart, req)
	if err != nil {
		return types.ArchiveBindingsView{}, err
	}
	return view, m.annotateAdoption(ctx, db, snap, entry, &view)
}

// RecordAdoption records an adoption's confirmation on the binding the attempt
// pinned (ADR 0090 T1, ADR 0091): remote_adopted_at with the fingerprint of the
// station account it was made under, in one durable write. Under bindingsMu,
// as a PUT, and then the config read lock, so neither a bindings save nor an
// account save can land between the checks and the write: the pin must still
// hold (same active archive and account, and the default logbook unless the
// binding is claimed; underPin), and the binding must still be reserved,
// enabled, hold the credentials read and not be confirmed under this account
// already. false with a nil error means the pin
// or the binding changed: the completion is discarded and the reservation
// stays. A save started meanwhile waits for this local write.
func (m *Manager) RecordAdoption(ctx context.Context, pin AdoptionPin) (bool, error) {
	recorded := false
	err := m.underPin(ctx, pin, func(db BindingsDB) error {
		var err error
		recorded, err = db.RecordLogbookDestinationAdoptedWithContext(ctx, pin.Binding.ForwarderName, pin.Binding.LogbookID, pin.Binding.Credentials, pin.Account)
		return err
	})
	if errors.Is(err, errPinMoved) {
		return false, nil
	}
	return recorded, err
}

// AdoptionPin is what an adoption attempt read and acts for (ADR 0091): the
// active archive, the binding as read (the default logbook's, or a claimed
// one's; ADR 0097 §5), and the fingerprint of the station account the attempt
// uses (CurrentAccount). Each local write rechecks it.
type AdoptionPin struct {
	ArchiveID string
	Binding   types.LogbookDestination
	Account   string
}

// errPinMoved reports that the pinned archive or account no longer holds, or
// that the default moved off a binding that is not claimed.
var errPinMoved = errors.New("the adoption's pinned archive, default logbook or account changed")

// underPin runs write with the active database while holding bindingsMu and
// then the config read lock — the only order that cannot deadlock, since a
// bindings PUT reads the config while holding bindingsMu — once the pin still
// holds against the saved config: the same active archive and account, and
// the binding still on the default logbook unless its STORED row is claimed
// now (ADR 0097 §5). The stored row decides, not the pinned snapshot: an
// attempt that read the binding unclaimed and then reserved it carries its
// own reservation through a move of the default. write must not call the
// config service or the network: every save waits on it.
func (m *Manager) underPin(ctx context.Context, pin AdoptionPin, write func(db BindingsDB) error) error {
	m.bindingsMu.Lock()
	defer m.bindingsMu.Unlock()
	m.mu.Lock()
	db := m.activeDB
	m.mu.Unlock()
	if db == nil {
		return &RequestError{Code: "bindings_unavailable", Message: "this daemon has no active archive database wired for bindings"}
	}
	td, ok := forwarding.DescriptorFor(pin.Binding.Destination)
	if !ok || len(adoptionKeys(td)) == 0 {
		return fmt.Errorf("destination %q has no remote adoption", pin.Binding.Destination)
	}
	return m.cfg.WithSnapshot(func(cfg config.Config) error {
		if cfg.ActiveQsoArchiveID != pin.ArchiveID {
			return errPinMoved
		}
		if current, err := CurrentAccount(cfg, pin.Binding.Destination, pin.ArchiveID); err != nil || current != pin.Account {
			return errPinMoved
		}
		if cfg.DefaultLogbookID != pin.Binding.LogbookID {
			claimed, err := storedClaim(ctx, db, pin.Binding)
			if err != nil {
				return err
			}
			if !claimed {
				return errPinMoved
			}
		}
		return write(db)
	})
}

// storedClaim reports whether the binding read is claimed in the archive now:
// the stored row with its forwarder name, on its logbook, holds a reservation
// or an adoption.
func storedClaim(ctx context.Context, db BindingsDB, read types.LogbookDestination) (bool, error) {
	rows, err := db.ListLogbookDestinationsWithContext(ctx)
	if err != nil {
		return false, err
	}
	for _, r := range rows {
		if r.ForwarderName == read.ForwarderName && r.LogbookID == read.LogbookID {
			return adoptionClaimed(r), nil
		}
	}
	return false, nil
}

// CurrentAccount is the fingerprint of the station account cfg holds for typ,
// for the archive archiveID (ADR 0091). An adoption is confirmed for that
// account only. An error means no fingerprint can be made: no account, an
// incomplete one, or a type without one.
func CurrentAccount(cfg config.Config, typ, archiveID string) (string, error) {
	fingerprint, ok := forwarding.AccountFingerprintFor(typ)
	if !ok {
		return "", fmt.Errorf("destination %q has no account fingerprint", typ)
	}
	account, ok := accountsByType(cfg)[typ]
	if !ok {
		return "", fmt.Errorf("no %s station account", typ)
	}
	return fingerprint(archiveID, account.Credentials)
}

// AdoptionConfirmed reports whether b's adoption is confirmed for the station
// account whose fingerprint is account (ADR 0091): recorded, and recorded under
// that account. A binding adopted under another account, or under none, needs
// a fresh confirmation.
func AdoptionConfirmed(b types.LogbookDestination, account string) bool {
	return b.RemoteAdoptedAt != nil && account != "" && b.RemoteAdoptedAccount == account
}

// adoptionKeys lists the type's logbook-scoped fields that an adoption fixes.
func adoptionKeys(td forwarding.TypeDescriptor) []string {
	var keys []string
	for _, f := range td.CredentialFields {
		if f.AdoptionKey {
			keys = append(keys, f.Key)
		}
	}
	return keys
}

// BornOnIdentity reports a binding born on SM Cloud identity (ADR 0092) and
// holding no adoption marker: it uploads by UUID, and its legacy cloud name
// means nothing.
func BornOnIdentity(b types.LogbookDestination) bool {
	return b.IdentitySince != nil && !adoptionClaimed(b)
}

// IdentityConflict reports a binding holding both the identity stamp and an
// adoption reservation or adoption, which no build writes: it is held, with
// its own reason, until manual recovery (ruling H2).
func IdentityConflict(b types.LogbookDestination) bool {
	return b.IdentitySince != nil && adoptionClaimed(b)
}

// adoptionClaimed reports a binding reserved for, or recorded with, an
// adoption: from then on its adoption key is fixed (ADR 0090 T6, ADR 0091).
func adoptionClaimed(b types.LogbookDestination) bool {
	return b.AdoptionReservedAt != nil || b.RemoteAdoptedAt != nil
}

// refuseProtectedNames checks the WHOLE candidate a PUT would leave — the
// stored bindings of live logbooks with the PUT's rows laid over them, enabled
// or not — against every name an adoption reservation or a recorded adoption
// protects (ADR 0091). Protected names come from every claimed binding,
// disabled ones and deleted logbooks' included, so neither disabling nor
// deleting releases one. Names compare as the type uploads under them (a
// cleared name is the type's default). Only the claiming binding may hold its
// name. A candidate whose stored name cannot be read is refused: it may share
// one. The refusal names the logbook and the field, never the value.
func refuseProtectedNames(ctx context.Context, db BindingsDB, stored []types.LogbookDestination, rows []sqlite.DestinationUpsert, lbByID map[int64]types.Logbook) error {
	claims, err := db.ListAdoptionClaimsWithContext(ctx)
	if err != nil || len(claims) == 0 {
		return err
	}
	protected := map[string]map[string]string{} // type → name → claiming binding
	for _, c := range claims {
		normalize, ok := forwarding.AdoptionNameFor(c.Destination)
		if !ok {
			continue
		}
		name, err := normalize(c.Credentials)
		if err != nil {
			return fmt.Errorf("binding %s holds an adoption claim whose name cannot be read: %w", c.ForwarderName, err)
		}
		if protected[c.Destination] == nil {
			protected[c.Destination] = map[string]string{}
		}
		protected[c.Destination][name] = c.ForwarderName
	}
	for _, c := range candidateBindings(stored, rows) {
		names := protected[c.Destination]
		// A clean born-identity binding uploads by UUID; its absent name would
		// read as "main" and collide for nothing (ruling F1). A binding with
		// mixed markers is not clean, and keeps every legacy rule.
		if len(names) == 0 || BornOnIdentity(c) {
			continue
		}
		td, _ := forwarding.DescriptorFor(c.Destination)
		normalize, _ := forwarding.AdoptionNameFor(c.Destination)
		name, err := normalize(c.Credentials)
		if err != nil {
			return &RequestError{Code: "binding_credentials_corrupt", Message: fmt.Sprintf(
				"%s for logbook %q: the stored credentials cannot be read, so it may use a name an adoption reserved; fix the archive file before editing bindings", td.DisplayName, lbByID[c.LogbookID].Name)}
		}
		if owner, ok := names[name]; ok && owner != c.ForwarderName {
			return &RequestError{Code: "binding_name_reserved", Message: fmt.Sprintf(
				"%s for logbook %q: its %s is reserved by another logbook's adoption and cannot be used by a second binding; choose another",
				td.DisplayName, lbByID[c.LogbookID].Name, adoptionKeyLabel(td))}
		}
	}
	return nil
}

// candidateBindings is what the bindings would be after the write: each stored
// binding with its PUT row's credentials, then the PUT's new bindings. A row
// the save stamps counts as stamped (ruling J6) — only a qualifying proposed
// stamp, since a claimed row is never planned one.
func candidateBindings(stored []types.LogbookDestination, rows []sqlite.DestinationUpsert) []types.LogbookDestination {
	edited := make(map[string]sqlite.DestinationUpsert, len(rows))
	for _, r := range rows {
		edited[r.ForwarderName] = r
	}
	out := make([]types.LogbookDestination, 0, len(stored)+len(rows))
	stamped := time.Now()
	for _, b := range stored {
		if r, ok := edited[b.ForwarderName]; ok {
			b.Credentials = r.Credentials
			if r.StampIdentity && b.IdentitySince == nil {
				b.IdentitySince = &stamped
			}
			delete(edited, b.ForwarderName)
		}
		out = append(out, b)
	}
	for _, r := range rows {
		if _, isNew := edited[r.ForwarderName]; isNew {
			c := types.LogbookDestination{LogbookID: r.LogbookID, Destination: r.Destination, ForwarderName: r.ForwarderName, Credentials: r.Credentials}
			if r.StampIdentity {
				c.IdentitySince = &stamped
			}
			out = append(out, c)
		}
	}
	return out
}

func adoptionKeyLabel(td forwarding.TypeDescriptor) string {
	for _, f := range td.CredentialFields {
		if f.AdoptionKey {
			return f.Label
		}
	}
	return "name"
}

// ReserveOutcome is what ReserveAdoption did.
type ReserveOutcome string

const (
	// ReserveReserved: the binding's name is reserved, durably, and the
	// adoption request may be sent.
	ReserveReserved ReserveOutcome = "reserved"
	// ReserveUnsafe: the judge refused; nothing was written.
	ReserveUnsafe ReserveOutcome = "unsafe"
	// ReserveChanged: the binding is no longer the one the attempt read; the
	// judge did not run and nothing was written.
	ReserveChanged ReserveOutcome = "changed"
)

// ReserveAdoption is the step between an adoption attempt's unlocked reads and
// its request (ADR 0091). Under bindingsMu, as a PUT, and then the config read
// lock, it checks that the pin still holds (underPin: same active archive and
// account, and the default logbook unless the binding is claimed) and that the
// binding is still the one the attempt read
// (same name, logbook, credentials; enabled; not already confirmed under the
// pinned account), runs judge over the local evidence, and on a safe verdict
// ("") reserves the binding's name. No bindings or account save can come
// between the checks, the judgement and the reservation, so judge must read
// only the archive (no config call, no network). A reservation is committed
// before this returns; any error means nothing may be sent. Reserving an
// already reserved binding keeps its first reservation: a retry judges again
// (ruling R2), and so does a re-confirmation under a new account.
func (m *Manager) ReserveAdoption(ctx context.Context, pin AdoptionPin, judge func(context.Context) (string, error)) (ReserveOutcome, string, error) {
	outcome, reason := ReserveChanged, ""
	err := m.underPin(ctx, pin, func(db BindingsDB) error {
		bindings, err := db.ListLogbookDestinationsWithContext(ctx)
		if err != nil {
			return err
		}
		if !stillAsRead(bindings, pin) {
			return nil
		}
		if reason, err = judge(ctx); err != nil || reason != "" {
			outcome = ReserveUnsafe
			return err
		}
		reserved, err := db.ReserveLogbookDestinationAdoptionWithContext(ctx, pin.Binding.ForwarderName, pin.Binding.LogbookID, pin.Binding.Credentials, pin.Account)
		if err != nil || !reserved {
			return err
		}
		outcome = ReserveReserved
		return nil
	})
	switch {
	case errors.Is(err, errPinMoved):
		return ReserveChanged, "", nil
	case err != nil:
		return "", "", err
	}
	return outcome, reason, nil
}

// stillAsRead reports whether the binding pinned is unchanged in bindings and
// may still be reserved: same logbook, destination and credentials, enabled,
// and not already confirmed under the pinned account.
func stillAsRead(bindings []types.LogbookDestination, pin AdoptionPin) bool {
	read := pin.Binding
	for _, b := range bindings {
		if b.ForwarderName == read.ForwarderName {
			return b.LogbookID == read.LogbookID && b.Destination == read.Destination && b.Enabled && b.IdentitySince == nil &&
				!AdoptionConfirmed(b, pin.Account) && bytes.Equal(b.Credentials, read.Credentials)
		}
	}
	return false
}
