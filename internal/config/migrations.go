package config

import (
	"encoding/json"
	"fmt"
)

// currentConfigVersion is the config schema version this build writes and
// migrates up to. Bump it (and register a migration below) whenever a shipped
// change alters the config shape. A config file with no `version` field is the
// pre-versioning baseline — the catalogue-era shape, treated as v1. See
// docs/v2-design/config.md §13.
const currentConfigVersion = 1

// migration upgrades a raw config JSON document from version `from` to `from+1`.
// Migrations operate on the decoded document (a map), NOT the typed Config, so a
// migration can read keys that the current typed Config has since dropped (e.g. a
// field moved into a per-rig block) before they're gone — that's what lets the
// struct cleanly remove old fields. See config.md §13.2.
type migration struct {
	from  int
	apply func(doc map[string]any) error
}

// migrations is the ordered registry, each step from→from+1. Empty until the
// first shape-changing release registers one (§10's per-rig field moves become
// the v1→v2 migration). Pre-versioning loose-field configs (legacy
// bridge.serial / ft8.device) are still folded by applyRigProfiles — their
// fields aren't removed yet, so they need no raw migration here.
var migrations []migration

// migrateDocument upgrades raw config bytes to currentConfigVersion, returning
// the (possibly rewritten) bytes. A document newer than this build is a fatal
// error (downgrade is not supported). A document already at the current version
// is returned unchanged.
func migrateDocument(data []byte) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing config document: %w", err)
	}

	from := documentVersion(doc)
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
// missing or non-numeric version is the pre-versioning baseline (v1). JSON
// numbers decode to float64.
func documentVersion(doc map[string]any) int {
	v, ok := doc["version"]
	if !ok {
		return 1
	}
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 1
}

func migrationFrom(v int) *migration {
	for i := range migrations {
		if migrations[i].from == v {
			return &migrations[i]
		}
	}
	return nil
}
