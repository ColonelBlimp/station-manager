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
const currentConfigVersion = 2

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
var migrations = []migration{
	{from: 1, apply: migrateV1toV2},
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
