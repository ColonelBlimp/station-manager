package config

import (
	"encoding/json"
	"fmt"
	"math"
)

// currentConfigVersion is the config schema version this build writes and
// migrates up to. Bump it (and register a migration below) whenever a shipped
// change alters the config shape. A config file with no `version` field is the
// pre-versioning baseline — the catalogue-era shape, treated as v1. See
// docs/v2-design/config.md §13.
const currentConfigVersion = 3

// CurrentSchemaVersion is the config schema version this build writes and
// migrates up to — the public view of currentConfigVersion for callers (e.g.
// startup) that must compare it against an on-disk document's version.
func CurrentSchemaVersion() int { return currentConfigVersion }

// migration upgrades a raw config JSON document from version `from` to `from+1`.
// Migrations operate on the decoded document (a map), NOT the typed Config, so a
// migration can read keys that the current typed Config has since dropped (e.g. a
// field moved into a per-rig block) before they're gone — that's what lets the
// struct cleanly remove old fields. See config.md §13.2.
type migration struct {
	from  int
	apply func(doc map[string]any) error
}

// migrations is the ordered registry, each step from→from+1.
//   - v1→v2 (config.md §10): fold the removed global bridge.mode_mappings into the
//     per-rig RigConfig.mode_mappings. Raw because the BridgeConfig field is gone,
//     so the value can't be read typed after unmarshal.
//
// (The other §10 moves whose struct fields are RETAINED — ft8.tx.mode → the
// projection-target Ft8TXConfig.Mode — are folded typed in config.go, not here.)
//
//   - v2→v3 (ADR 0075): consume the version-2 keys whose struct fields were
//     removed, so they don't trip the ADR 0074 unknown-key gate on an upgrade
//     while ADR 0067's "boots over the legacy key" promise still holds. Deletes
//     ft8.tx.auto_work_callers (ADR 0067) and ft8.meter.alc_red (ADR 0064); folds
//     rigs[].audio.device into audio.rx/audio.tx (when absent) then deletes it;
//     moves psk_reporter.antenna into logging_station.my_antenna (only when the
//     canonical field is absent — otherwise it wins) then deletes it. Also
//     reconciles the alpha.1-generated qrzcq action_filter (W-0008 CC-5, see
//     migrateAlpha1QrzcqFilter).
var migrations = []migration{
	{from: 1, apply: migrateV1toV2},
	{from: 2, apply: migrateV2toV3},
}

// migrateV1toV2 folds the global bridge.mode_mappings[driver] block (removed from
// BridgeConfig in §10) into each rig's per-rig mode_mappings, keyed by rig literal
// (the rig knows its Model, so the driver key is dropped). Idempotent: a no-op once
// the global block is gone. Synthesises the id-1 rig from legacy loose fields when
// there's no catalogue yet, so the mappings still have a home.
func migrateV1toV2(doc map[string]any) error {
	bridgeValue, bridgePresent := doc["bridge"]
	if !bridgePresent {
		return nil
	}
	bridge, ok := bridgeValue.(map[string]any)
	if !ok {
		return fmt.Errorf("bridge: must be an object")
	}
	modeMappingsValue, modeMappingsPresent := bridge["mode_mappings"]
	if !modeMappingsPresent {
		return nil
	}
	byDriver, ok := modeMappingsValue.(map[string]any)
	if !ok {
		return fmt.Errorf("bridge.mode_mappings: must be an object")
	}
	if err := validateLegacyModeMappings("bridge.mode_mappings", byDriver); err != nil {
		return err
	}

	var rigs []any
	if rigsValue, present := doc["rigs"]; present {
		var ok bool
		rigs, ok = rigsValue.([]any)
		if !ok {
			return fmt.Errorf("rigs: must be an array")
		}
		for idx, value := range rigs {
			rig, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("rigs[%d]: must be an object", idx)
			}
			model, ok := rig["model"]
			if !ok {
				return fmt.Errorf("rigs[%d].model: is required while migrating bridge.mode_mappings", idx)
			}
			if _, ok := model.(string); !ok {
				return fmt.Errorf("rigs[%d].model: must be a string", idx)
			}
			if destination, present := rig["mode_mappings"]; present {
				destinationMappings, ok := destination.(map[string]any)
				if !ok {
					return fmt.Errorf("rigs[%d].mode_mappings: must be an object", idx)
				}
				if err := validateModeMappingEntries(fmt.Sprintf("rigs[%d].mode_mappings", idx), destinationMappings); err != nil {
					return err
				}
			}
		}
	}

	// Ensure a rigs catalogue exists. A pre-catalogue (loose) config carries its
	// identity in bridge.cat.driver / bridge.serial.port / ft8.device — synthesise
	// the id-1 rig from those (mirrors applyRigProfiles) so the mappings land.
	if len(rigs) == 0 {
		driver := nestedString(doc, "bridge", "cat", "driver")
		port := nestedString(doc, "bridge", "serial", "port")
		device := nestedString(doc, "ft8", "device")
		if driver != "" || port != "" || device != "" {
			rig := map[string]any{"id": float64(1), "model": driver, "port": port}
			if device != "" {
				rig["audio"] = map[string]any{"device": device}
			}
			rigs = []any{rig}
			doc["rigs"] = rigs
			if _, ok := doc["default_rig_id"]; !ok {
				doc["default_rig_id"] = float64(1)
			}
		}
	}

	// Place each driver's overrides on every rig of that Model (coinciding values
	// across same-model rigs is fine — config.md §9.4). Don't clobber a rig that
	// already carries its own mode_mappings.
	for _, r := range rigs {
		rig, _ := r.(map[string]any)
		if rig == nil {
			continue
		}
		model, _ := rig["model"].(string)
		mm, ok := byDriver[model].(map[string]any)
		if !ok || len(mm) == 0 {
			continue
		}
		if _, exists := rig["mode_mappings"]; !exists {
			rig["mode_mappings"] = mm
		}
	}

	delete(bridge, "mode_mappings")
	return nil
}

// validateV2toV3RetiredKeys checks every value the migration will consume before
// any of them is deleted or moved. Removed fields no longer have a typed decoder
// behind them, so accepting a wrong legacy type here would erase malformed
// operator data and recreate EH-3. JSON null remains valid wherever encoding/json
// accepted it for the retired Go field; it carries no value to fold.
func validateV2toV3RetiredKeys(doc map[string]any) error {
	if ft8, ok := doc["ft8"].(map[string]any); ok {
		if tx, ok := ft8["tx"].(map[string]any); ok {
			if value, present := tx["auto_work_callers"]; present && value != nil {
				if _, ok := value.(bool); !ok {
					return fmt.Errorf("ft8.tx.auto_work_callers: must be a boolean")
				}
			}
		}
		if meter, ok := ft8["meter"].(map[string]any); ok {
			if value, present := meter["alc_red"]; present && value != nil {
				f, ok := value.(float64)
				if !ok || math.Trunc(f) != f || f < float64(math.MinInt) || f > float64(math.MaxInt) {
					return fmt.Errorf("ft8.meter.alc_red: must be an integer JSON number")
				}
			}
		}
	}

	if rigs, ok := doc["rigs"].([]any); ok {
		for i, value := range rigs {
			rig, ok := value.(map[string]any)
			if !ok {
				continue // typed unmarshal will diagnose the non-object element
			}
			audio, ok := rig["audio"].(map[string]any)
			if !ok {
				continue // typed unmarshal will diagnose the non-object block
			}
			if device, present := audio["device"]; present && device != nil {
				if _, ok := device.(string); !ok {
					return fmt.Errorf("rigs[%d].audio.device: must be a string", i)
				}
			}
		}
	}

	psk, ok := doc["psk_reporter"].(map[string]any)
	if !ok {
		return nil // no consumable retired antenna; typed unmarshal owns the block
	}
	antenna, present := psk["antenna"]
	if !present {
		return nil
	}
	if antenna != nil {
		if _, ok := antenna.(string); !ok {
			return fmt.Errorf("psk_reporter.antenna: must be a string")
		}
	}

	lsValue, present := doc["logging_station"]
	if !present || lsValue == nil {
		return nil
	}
	ls, ok := lsValue.(map[string]any)
	if !ok {
		return fmt.Errorf("logging_station: must be an object")
	}
	if canonical, present := ls["my_antenna"]; present && canonical != nil {
		if _, ok := canonical.(string); !ok {
			return fmt.Errorf("logging_station.my_antenna: must be a string")
		}
	}
	return nil
}

// migrateV2toV3 consumes the version-2 keys whose typed struct fields were
// removed, so an upgraded install boots (ADR 0067's promise) while the ADR 0074
// gate still rejects genuine typos — only keys STILL unknown after this run are
// refused (ADR 0075). It operates on the raw document because the fields are gone
// from the typed Config. Idempotent: once each retired key is consumed, a second
// pass finds nothing to move and deletes nothing.
func migrateV2toV3(doc map[string]any) error {
	if err := validateV2toV3RetiredKeys(doc); err != nil {
		return err
	}

	// ft8.tx.auto_work_callers (ADR 0067) and ft8.meter.alc_red (ADR 0064): pure
	// removals — the behaviour they toggled is gone, so drop the keys.
	if ft8, ok := doc["ft8"].(map[string]any); ok {
		if tx, ok := ft8["tx"].(map[string]any); ok {
			delete(tx, "auto_work_callers")
		}
		if meter, ok := ft8["meter"].(map[string]any); ok {
			delete(meter, "alc_red")
		}
	}

	// rigs[].audio.device → audio.rx / audio.tx. The single legacy device string
	// named both directions; fill each only when absent so an operator who already
	// split rx/tx is never overwritten. Then delete the retired key. (The v1→v2
	// step can SYNTHESISE audio.device from loose fields, so the v1→v2→v3 chain
	// consumes it here — migrations.go:94 stays untouched.)
	if rigs, ok := doc["rigs"].([]any); ok {
		for _, r := range rigs {
			rig, ok := r.(map[string]any)
			if !ok {
				continue
			}
			audio, ok := rig["audio"].(map[string]any)
			if !ok {
				continue
			}
			if device, ok := audio["device"].(string); ok && device != "" {
				if _, has := audio["rx"]; !has {
					audio["rx"] = device
				}
				if _, has := audio["tx"]; !has {
					audio["tx"] = device
				}
			}
			delete(audio, "device")
		}
	}

	// psk_reporter.antenna → logging_station.my_antenna, but only when the
	// canonical field is absent; otherwise the canonical value wins. Either way the
	// retired key is deleted.
	if psk, ok := doc["psk_reporter"].(map[string]any); ok {
		if _, present := psk["antenna"]; present {
			if antenna, ok := psk["antenna"].(string); ok && antenna != "" {
				ls, ok := doc["logging_station"].(map[string]any)
				if !ok {
					ls = map[string]any{}
					doc["logging_station"] = ls
				}
				if _, present := ls["my_antenna"]; !present {
					ls["my_antenna"] = antenna
				}
			}
			delete(psk, "antenna")
		}
	}

	migrateAlpha1QrzcqFilter(doc)
	return nil
}

// migrateAlpha1QrzcqFilter reconciles the one filter shape alpha.1 wrote that this
// build refuses (alpha.2 dogfood Finding #6, W-0008 CC-5). alpha.1 (config version
// 2) filled an OMITTED qrzcq action_filter with the historical all-three default
// because that build registered no supported set for the type; this build
// registers qrzcq insert-only, so validateForwarders would refuse the stored
// filter and the upgraded daemon could not start. The match is deliberately
// narrow — a pre-version-3 document (nothing after alpha.1 writes one), type
// "qrzcq", and a filter that is exactly the ORDERED slice ["insert","update",
// "delete"] alpha.1's omitted-filter path emitted — and the target is the literal
// ["insert"] alpha.1's successors write for an omitted filter, not a registry
// lookup: a migration is a frozen record of the v2 shape. Any other explicit
// unsupported action — a hand-authored ["insert","update"], a permutation such as
// ["delete","update","insert"], or this same content in a version-3 document —
// stays for validateForwarders to reject (the RegisterSupportedActions contract):
// it is a mistake to surface, not a legacy artefact. No reconciliation-specific
// log record: the existing one-time schema-migration "config saved" record
// (reason schema_version) covers the rewrite.
// alpha1QrzcqFilter is the exact ordered slice alpha.1's applyDefaults wrote for an
// omitted filter on a type with no registered supported set.
var alpha1QrzcqFilter = []string{"insert", "update", "delete"}

func migrateAlpha1QrzcqFilter(doc map[string]any) {
	fwds, ok := doc["forwarders"].([]any)
	if !ok {
		return
	}
	for _, f := range fwds {
		fc, ok := f.(map[string]any)
		if !ok {
			continue
		}
		if typ, _ := fc["type"].(string); typ != "qrzcq" {
			continue
		}
		raw, ok := fc["action_filter"].([]any)
		if !ok || len(raw) != len(alpha1QrzcqFilter) {
			continue
		}
		exact := true
		for i, v := range raw {
			if s, ok := v.(string); !ok || s != alpha1QrzcqFilter[i] {
				exact = false
				break
			}
		}
		if exact {
			fc["action_filter"] = []any{"insert"}
		}
	}
}

// validateLegacyModeMappings validates the complete removed block before the
// migration mutates either its source or any destination rig. That ordering is
// important: BridgeConfig no longer has this field, so deleting a malformed
// value would otherwise let typed unmarshal accept a document whose operator
// data had silently disappeared.
func validateLegacyModeMappings(path string, byDriver map[string]any) error {
	for driver, value := range byDriver {
		mappings, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s[%q]: must be an object", path, driver)
		}
		if err := validateModeMappingEntries(fmt.Sprintf("%s[%q]", path, driver), mappings); err != nil {
			return err
		}
	}
	return nil
}

func validateModeMappingEntries(path string, mappings map[string]any) error {
	for literal, value := range mappings {
		mapping, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s[%q]: must be an object", path, literal)
		}
		mode, present := mapping["mode"]
		if !present {
			return fmt.Errorf("%s[%q].mode: is required", path, literal)
		}
		if _, ok := mode.(string); !ok {
			return fmt.Errorf("%s[%q].mode: must be a string", path, literal)
		}
		if submode, present := mapping["submode"]; present {
			if _, ok := submode.(string); !ok {
				return fmt.Errorf("%s[%q].submode: must be a string", path, literal)
			}
		}
	}
	return nil
}

// nestedString walks a chain of map keys and returns the string at the end, or
// "" if any hop is missing or not the expected type.
func nestedString(doc map[string]any, keys ...string) string {
	cur := doc
	for i, k := range keys {
		v, ok := cur[k]
		if !ok {
			return ""
		}
		if i == len(keys)-1 {
			s, _ := v.(string)
			return s
		}
		cur, ok = v.(map[string]any)
		if !ok {
			return ""
		}
	}
	return ""
}

// migrateDocument upgrades raw config bytes to currentConfigVersion, returning
// the (possibly rewritten) bytes. A document newer than this build is a fatal
// error (downgrade is not supported). A document already at the current version
// is returned unchanged.
func migrateDocument(data []byte) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing config document: %w", err)
	}

	from, err := documentVersion(doc)
	if err != nil {
		return nil, err
	}
	if from > currentConfigVersion {
		return nil, fmt.Errorf(
			"config version %d is newer than this Station Manager supports (max %d); "+
				"downgrade is not supported", from, currentConfigVersion)
	}
	if from == currentConfigVersion {
		return data, nil
	}

	for v := from; v < currentConfigVersion; v++ {
		m := migrationFrom(v)
		if m == nil {
			return nil, fmt.Errorf("no config migration registered from version %d", v)
		}
		if err := m.apply(doc); err != nil {
			return nil, fmt.Errorf("migrating config v%d→v%d: %w", v, v+1, err)
		}
	}
	doc["version"] = currentConfigVersion

	out, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("re-marshalling migrated config: %w", err)
	}
	return out, nil
}

// documentVersion reads the `version` field from a decoded config document. A
// missing version is the pre-versioning baseline (v1). A present version must
// be an integer JSON number; treating a malformed present value as "missing"
// could run the wrong migration and then stamp the document current.
func documentVersion(doc map[string]any) (int, error) {
	v, ok := doc["version"]
	if !ok {
		return 1, nil
	}
	f, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("version: must be an integer JSON number")
	}
	if math.Trunc(f) != f || f < 0 || f > float64(math.MaxInt) {
		return 0, fmt.Errorf("version: must be an integer JSON number")
	}
	return int(f), nil
}

func migrationFrom(v int) *migration {
	for i := range migrations {
		if migrations[i].from == v {
			return &migrations[i]
		}
	}
	return nil
}
