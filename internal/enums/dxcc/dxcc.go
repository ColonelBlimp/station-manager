// Package dxcc maps a DXCC entity's primary prefix (as hamnut emits it in
// `primaryDXCCPrefix`) to the ADIF DXCC entity number.
//
// Why this exists: the enrichment "new entity" check needs to know whether the
// operator has worked a given DXCC entity before. hamnut resolves a callsign to
// an entity but emits only the alpha prefix (e.g. "UA" vs "UA9" for European vs
// Asiatic Russia) and a display name ("European Russia") — never the numeric
// ADIF code. QSOs, however, store the numeric code (ADIF import / QRZ
// enrichment), and that code distinguishes the split entities the country-name
// string cannot. This table bridges hamnut's prefix to that number so the check
// can match by entity number instead of a (frequently mismatching) name string.
//
// Data-driven, mirroring internal/enums/modes: an embedded baseline
// (`dxcc-entities.json`, loaded at package init via go:embed) plus an optional
// operator override at `$SM_WORKING_DIR/dxcc-entities.json` (LoadOverride). The
// baseline covers the common entities; an unmapped prefix simply returns ok=false
// and the caller falls back to name-matching — so partial coverage is safe.
//
// Lookup contract:
//   - DXCCForPrefix(prefix) — ADIF DXCC code (string, as stored on QSOs) for a
//     hamnut primary prefix, plus ok. Case-insensitive, whitespace-trimmed.
package dxcc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	_ "embed"
)

// catalogue is the on-disk shape of dxcc-entities.json (and the optional
// operator override). Entities is a flat list; only Prefix + DXCC are
// load-bearing (Name is kept for human readability / debugging).
type catalogue struct {
	Version  string         `json:"version,omitempty"`
	Comment  string         `json:"comment,omitempty"`
	Entities []entityRecord `json:"entities"`
}

type entityRecord struct {
	Prefix string `json:"prefix"`
	DXCC   int    `json:"dxcc"`
	Name   string `json:"name,omitempty"`
}

//go:embed dxcc-entities.json
var embeddedCatalogueJSON []byte

// mu guards prefixToDXCC so concurrent reads from the enrichment orchestrator
// don't race a LoadOverride call (which runs once at daemon startup; the map is
// read-mostly thereafter, but the mutex makes the contract explicit).
var (
	mu           sync.RWMutex
	prefixToDXCC map[string]string
)

func init() {
	cat, err := parseCatalogue(embeddedCatalogueJSON)
	if err != nil {
		// An embedded asset that fails to parse is a programmer error caught at
		// first run; a loud panic is the right surface.
		panic(fmt.Sprintf("dxcc: embedded dxcc-entities.json invalid: %v", err))
	}
	mu.Lock()
	prefixToDXCC = make(map[string]string, len(cat.Entities))
	applyCatalogue(cat)
	mu.Unlock()
}

// LoadOverride loads an operator's dxcc-entities.json from workingDir if present,
// merging its entities on top of the embedded baseline (operator wins on a prefix
// collision). A missing file is not an error (returns nil) — most operators won't
// have one. A malformed file is an error (caller decides whether to fail startup
// or log and continue with the embedded baseline).
//
// Safe to call multiple times; each call merges additively. The mutex makes
// concurrent reads during a reload safe (readers see pre- or post-merge state,
// never half-applied).
func LoadOverride(workingDir string) error {
	path := filepath.Join(workingDir, "dxcc-entities.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("dxcc: read override %s: %w", path, err)
	}
	cat, err := parseCatalogue(data)
	if err != nil {
		return fmt.Errorf("dxcc: parse override %s: %w", path, err)
	}
	mu.Lock()
	applyCatalogue(cat)
	mu.Unlock()
	return nil
}

func parseCatalogue(data []byte) (*catalogue, error) {
	var cat catalogue
	if err := json.Unmarshal(data, &cat); err != nil {
		return nil, err
	}
	return &cat, nil
}

// applyCatalogue is mu-locked by the caller. Adds each entity's prefix→code to
// the map (later writes win — operator override beats an embedded entry of the
// same prefix). Entries with an empty prefix or non-positive DXCC are skipped.
func applyCatalogue(cat *catalogue) {
	for _, e := range cat.Entities {
		key := strings.ToUpper(strings.TrimSpace(e.Prefix))
		if key == "" || e.DXCC <= 0 {
			continue
		}
		prefixToDXCC[key] = strconv.Itoa(e.DXCC)
	}
}

// DXCCForPrefix returns the ADIF DXCC entity code (as a string, matching how the
// code is stored on QSOs) for a hamnut primary prefix. ok is false when the
// prefix isn't in the loaded catalogue — the caller should then fall back to
// name-matching. Case-insensitive, whitespace-trimmed.
func DXCCForPrefix(prefix string) (string, bool) {
	key := strings.ToUpper(strings.TrimSpace(prefix))
	if key == "" {
		return "", false
	}
	mu.RLock()
	code, ok := prefixToDXCC[key]
	mu.RUnlock()
	return code, ok
}
