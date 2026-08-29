package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/clublog"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// SHIP GATE (a), SITE B — THE STARTUP REWRITE.
//
// CRITERION (operator ruling 5, 2026-08-02): the every-boot rewrite of
// config.json is in scope for the save record, and the delta is what makes it
// useful rather than one noise line per start.
//
//	When the daemon changes config.json at startup, smd.log says what it
//	changed and that the daemon did it — and when the daemon changes nothing,
//	it says nothing, so a quiet log means a quiet file.
//
// WHY THIS IS TESTED AT persistResolvedConfig AND NOT AT THE EMIT. A test that
// fed a hand-built before/after pair into a logger would prove the record's
// shape and nothing about whether the startup path produces that pair — one
// fix, two sites, one proof. These rules drive the real function against a real
// config.Service on a real temp file, so the UserAgent fill and the ClubLog
// scrub are the actual inputs.
//
// The emit itself (source=startup, silence when empty) is asserted from the
// changes this function returns: len==0 is exactly the condition run() branches
// on, so a rule that pins the emptiness pins the silence.

// startupConfigService builds a config.Service backed by a real file, the way
// run() has one by the time persistResolvedConfig is called.
func startupConfigService(t *testing.T, mutate func(c *config.Config)) *config.Service {
	t.Helper()
	dir := t.TempDir()
	cfg := config.DefaultConfig(dir)
	if mutate != nil {
		mutate(&cfg)
	}
	path := filepath.Join(dir, "config.json")
	if _, err := config.WriteJSON(path, cfg); err != nil {
		t.Fatalf("fixture: seeding config.json must succeed: %v", err)
	}
	svc := config.New(cfg)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("fixture: config init: %v", err)
	}
	svc.SetPath(path)
	return svc
}

func changeFields(changes []config.FieldChange) []string {
	out := make([]string, 0, len(changes))
	for _, c := range changes {
		out = append(out, c.Field)
	}
	return out
}

// B1 — A STARTUP THAT FILLS THE USERAGENT REPORTS IT. The value is a real
// change to the operator's file and was previously invisible.
func TestPersistResolvedConfig_ReportsUserAgentFill(t *testing.T) {
	svc := startupConfigService(t, func(c *config.Config) { c.UserAgent = "" })

	changes, err := persistResolvedConfig(svc, "station-manager/2.0.0-test")
	if err != nil {
		t.Fatalf("persist: %v", err)
	}

	var found *config.FieldChange
	for i := range changes {
		if changes[i].Field == "useragent" {
			found = &changes[i]
		}
	}
	if found == nil {
		t.Fatalf("useragent fill not reported; changes = %v", changeFields(changes))
	}
	if found.To != "station-manager/2.0.0-test" {
		t.Errorf("to = %q, want the resolved UA — useragent is on the value allowlist", found.To)
	}
}

// B2 — A STARTUP THAT CHANGES NOTHING REPORTS NOTHING (ruling 5 + 4). This is
// the ordinary boot: the UA is already on disk and there is no legacy key. An
// unconditional record here would be one noise line per start, forever, and
// would restore exactly the "every config line means nothing" problem finding
// A4 is about.
//
// The state is WRITTEN FIRST — the fixture seeds the UA that the call then
// re-applies — so a broken delta cannot agree with this rule by accident.
func TestPersistResolvedConfig_UnchangedStartupIsSilent(t *testing.T) {
	const ua = "station-manager/2.0.0-test"
	svc := startupConfigService(t, func(c *config.Config) { c.UserAgent = ua })

	changes, err := persistResolvedConfig(svc, ua)
	if err != nil {
		t.Fatalf("persist: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("an unchanged startup reported %d change(s): %v — run() would log a record per boot",
			len(changes), changeFields(changes))
	}
}

// B3 — THE LEGACY CLUBLOG KEY SCRUB IS REPORTED, WITHOUT THE KEY. The scrub
// DELETES a credential from the operator's config file. Before this record it
// did so silently, so "where did my ClubLog key go?" had no answer anywhere.
//
// Both halves are asserted: the key's value must be absent from what would be
// logged, AND the removal must still be reported. Absence alone passes against
// a function that reports nothing at all.
func TestPersistResolvedConfig_ReportsClubLogScrubWithoutTheKey(t *testing.T) {
	const legacyKey = "LEGACYCLUBLOGKEY987"

	// A build WITH a baked replacement is the guarded precondition for the
	// scrub; without it the daemon must keep the operator's only usable key.
	prev := clublog.InjectedAPIKey
	clublog.InjectedAPIKey = "baked-replacement"
	t.Cleanup(func() { clublog.InjectedAPIKey = prev })

	svc := startupConfigService(t, func(c *config.Config) {
		c.UserAgent = "station-manager/2.0.0-test"
		c.Forwarders = []types.ForwarderConfig{{
			Name:        "clublog",
			Type:        clublog.Type,
			Enabled:     false,
			Credentials: json.RawMessage(`{"api":"` + legacyKey + `","email":"op@example.com"}`),
		}}
	})

	changes, err := persistResolvedConfig(svc, "station-manager/2.0.0-test")
	if err != nil {
		t.Fatalf("persist: %v", err)
	}

	rendered, mErr := json.Marshal(changes)
	if mErr != nil {
		t.Fatalf("marshal changes: %v", mErr)
	}
	if strings.Contains(string(rendered), legacyKey) {
		t.Fatalf("the legacy ClubLog key reached the record: %s", rendered)
	}

	var found *config.FieldChange
	for i := range changes {
		if strings.Contains(changes[i].Field, "credentials.api") {
			found = &changes[i]
		}
	}
	if found == nil {
		t.Fatalf("the scrub is not reported at all; changes = %v — a silent credential "+
			"deletion is the gap this record exists to close", changeFields(changes))
	}
	if !found.Secret {
		t.Errorf("the credential change is not marked secret: %+v", *found)
	}
	if found.To != "(unset)" {
		t.Errorf("to = %q, want %q — the key was removed", found.To, "(unset)")
	}

	// And the scrub really happened on disk, so the rule is not describing a
	// report of a change that never landed.
	raw, rErr := os.ReadFile(svc.Path)
	if rErr != nil {
		t.Fatalf("read back config.json: %v", rErr)
	}
	if strings.Contains(string(raw), legacyKey) {
		t.Error("the legacy key is still in config.json; the reported scrub did not happen")
	}
}
