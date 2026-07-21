// Package modes defines the ADIF main-mode and sub-mode catalogue used by
// Station Manager.
//
// The catalogue is data-driven: an embedded baseline file
// (`adif-modes.json`, loaded at package init via go:embed) defines the
// shipped main modes and submode→parent mapping; an optional operator
// override at `$SM_WORKING_DIR/modes.json` extends or overrides the
// baseline at daemon startup (callers invoke `LoadOverride(workingDir)`).
//
// The data-driven shape exists so an ADIF spec update doesn't strictly
// require a code change: operators can add new modes via the override
// file immediately; when a new daemon binary ships with the wider
// baseline, the override entries become redundant but never break.
//
// Validation contract (unchanged across the refactor):
//   - IsValidMode(s) — s is a known ADIF main mode (case-insensitive, trimmed)
//   - IsValidSubMode(s) — s is a known ADIF submode
//   - GetModeBySubmode(s) — parent main mode for a submode, plus ok
package modes

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	_ "embed"
)

// MaxModeLen bounds a main-mode name at the storage limit — the qso.mode column's
// CHECK(length(trim(mode)) <= 20) (migration 0006). Enforced when the catalogue is
// (re)loaded so IsValidMode never accepts, and GetModeBySubmode never resolves, a
// mode that would then violate the insert CHECK (a 500 on live submit, a hard abort
// on import). This keeps the validation domain inside the storage domain: an
// over-length operator mode is cleanly rejected as "not recognised" instead of
// reaching the DB (codex c03f36e5 P1). Kept in step with the migration by hand —
// SQL can't reference a Go const; the value here IS the CHECK's ceiling.
const MaxModeLen = 20

// Mode is the typed wrapper for ADIF main-mode strings. Kept as a
// distinct type so call sites that thread modes through can use
// modes.Mode rather than `string` for readability; the underlying
// value is always the uppercase ADIF MODE string.
type Mode string

func (m Mode) String() string { return string(m) }

// SubMode is the typed wrapper for ADIF submode strings, paralleling
// Mode. Same uppercase contract.
type SubMode string

func (s SubMode) String() string { return string(s) }

// Common ADIF main mode constants. Kept for the rare call sites that
// want a typed compile-time reference (e.g. `if m == modes.SSB`).
// Adding a new constant here is NOT required for IsValidMode to
// accept the value — validation is driven by adif-modes.json. These
// just save callers from re-typing the strings.
const (
	AM           Mode = "AM"
	CW           Mode = "CW"
	FM           Mode = "FM"
	RTTY         Mode = "RTTY"
	SSB          Mode = "SSB"
	DIGITALVOICE Mode = "DIGITALVOICE"
	MFSK         Mode = "MFSK"
	PSK          Mode = "PSK"
	HELL         Mode = "HELL"
	PACKET       Mode = "PACKET"
)

// Common ADIF submode constants. Same rationale as the Mode constants
// above — typed references for the few call sites that want them.
const (
	USB SubMode = "USB"
	LSB SubMode = "LSB"
)

// catalogue is the on-disk shape of adif-modes.json (and the optional
// operator override file). MainModes is a flat list; SubModes maps
// the submode → parent main mode.
type catalogue struct {
	Version   string            `json:"version,omitempty"`
	Comment   string            `json:"comment,omitempty"`
	MainModes []string          `json:"main_modes"`
	SubModes  map[string]string `json:"submodes"`
}

//go:embed adif-modes.json
var embeddedCatalogueJSON []byte

// mu guards the loaded sets so concurrent reads from request handlers
// don't race against a LoadOverride call. In practice LoadOverride
// runs once at daemon startup and the sets are read-mostly thereafter,
// but the mutex makes the contract explicit.
var (
	mu              sync.RWMutex
	mainModeSet     map[string]struct{}
	submodeToParent map[string]string
)

func init() {
	cat, err := parseCatalogue(embeddedCatalogueJSON)
	if err != nil {
		// An embedded asset that fails to parse is a programmer error
		// caught at first run; loud panic is the right surface.
		panic(fmt.Sprintf("modes: embedded adif-modes.json invalid: %v", err))
	}
	mu.Lock()
	mainModeSet = make(map[string]struct{}, len(cat.MainModes))
	submodeToParent = make(map[string]string, len(cat.SubModes))
	applyCatalogue(cat)
	mu.Unlock()
}

// LoadOverride loads an operator's modes.json from workingDir if
// present. The file's main_modes are unioned into the loaded set;
// submodes are merged with operator values winning on key collision.
// Missing file is not an error (returns nil) — most operators won't
// have an override. Malformed file is an error (caller decides
// whether to fail startup or log+continue with embedded only).
//
// Safe to call multiple times; each call merges additively on top of
// the previous state. The mutex makes concurrent reads from request
// handlers safe during a reload (handlers see either the pre- or
// post-merge state, never a half-applied one).
func LoadOverride(workingDir string) error {
	path := filepath.Join(workingDir, "modes.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("modes: read override %s: %w", path, err)
	}
	cat, err := parseCatalogue(data)
	if err != nil {
		return fmt.Errorf("modes: parse override %s: %w", path, err)
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

// applyCatalogue is mu-locked by the caller. Adds main_modes to the
// set and merges submodes (later writes win — operator override beats
// embedded entry of the same key).
func applyCatalogue(cat *catalogue) {
	for _, m := range cat.MainModes {
		key := strings.ToUpper(strings.TrimSpace(m))
		// Skip empty or over-length names (rune count matches SQLite's length()):
		// an over-length mode would pass IsValidMode but fail the qso.mode CHECK on
		// insert, so it must never enter the catalogue (codex c03f36e5 P1).
		if key == "" || utf8.RuneCountInString(key) > MaxModeLen {
			continue
		}
		mainModeSet[key] = struct{}{}
	}
	// Resolve the override's submode entries into a canonical decision BEFORE
	// mutating the shared map, so the result doesn't depend on Go's map iteration
	// order (codex 26e55f6d P2). Distinct raw keys can canonicalize identically
	// (e.g. "USB" and " usb "): iterating SORTED keys makes two valid siblings
	// resolve the same way every run, and tracking valid vs. invalid-only per
	// canonical key makes a valid-length parent always beat an over-length sibling.
	// An over-length parent removes the key ONLY when no valid entry was supplied
	// for it — the 1680267a case (reassigning an existing submode to an invalid
	// parent) — without the nondeterministic erasure a bare delete-in-loop caused.
	rawKeys := make([]string, 0, len(cat.SubModes))
	for k := range cat.SubModes {
		rawKeys = append(rawKeys, k)
	}
	slices.Sort(rawKeys)
	validParents := make(map[string]string, len(rawKeys))
	invalidOnly := make(map[string]struct{})
	for _, rawKey := range rawKeys {
		key := strings.ToUpper(strings.TrimSpace(rawKey))
		val := strings.ToUpper(strings.TrimSpace(cat.SubModes[rawKey]))
		if key == "" || val == "" {
			continue
		}
		// The parent BECOMES the stored mode when a QSO is logged by submode
		// (GetModeBySubmode → prepareQso), so it faces the same storage bound.
		if utf8.RuneCountInString(val) > MaxModeLen {
			if _, ok := validParents[key]; !ok {
				invalidOnly[key] = struct{}{}
			}
			continue
		}
		validParents[key] = val
		delete(invalidOnly, key)
	}
	// Apply: valid parents override/add; invalid-only keys are removed so a bad
	// override doesn't leave an inherited mapping active (1680267a). These loops are
	// pure assignments/deletes on distinct keys, so their own iteration order is
	// immaterial.
	for key, val := range validParents {
		submodeToParent[key] = val
	}
	for key := range invalidOnly {
		delete(submodeToParent, key)
	}
}

// IsValidMode reports whether s is a known ADIF main mode in the
// loaded catalogue. Case-insensitive, whitespace-trimmed.
func IsValidMode(s string) bool {
	key := strings.ToUpper(strings.TrimSpace(s))
	if key == "" {
		return false
	}
	mu.RLock()
	_, ok := mainModeSet[key]
	mu.RUnlock()
	return ok
}

// IsValidSubMode reports whether s is a known ADIF submode in the
// loaded catalogue. Case-insensitive, whitespace-trimmed.
func IsValidSubMode(s string) bool {
	key := strings.ToUpper(strings.TrimSpace(s))
	if key == "" {
		return false
	}
	mu.RLock()
	_, ok := submodeToParent[key]
	mu.RUnlock()
	return ok
}

// GetModeBySubmode returns the parent main mode for a submode. The
// bool return is false if the submode is unknown.
func GetModeBySubmode(s string) (Mode, bool) {
	key := strings.ToUpper(strings.TrimSpace(s))
	if key == "" {
		return "", false
	}
	mu.RLock()
	parent, ok := submodeToParent[key]
	mu.RUnlock()
	if !ok {
		return "", false
	}
	return Mode(parent), true
}

// MainModes returns a sorted snapshot of the loaded main-mode set.
// Useful for the /v1/config response so the SPA can populate the
// Mode Mappings sub-tab's MODE dropdown.
func MainModes() []string {
	mu.RLock()
	out := make([]string, 0, len(mainModeSet))
	for m := range mainModeSet {
		out = append(out, m)
	}
	mu.RUnlock()
	slices.Sort(out)
	return out
}

// SubModes returns a sorted snapshot of the loaded submode keys. The
// daemon exposes these via /v1/config so the SPA's Mode Mappings
// sub-tab can populate the SUBMODE dropdown.
func SubModes() []string {
	mu.RLock()
	out := make([]string, 0, len(submodeToParent))
	for k := range submodeToParent {
		out = append(out, k)
	}
	mu.RUnlock()
	slices.Sort(out)
	return out
}
