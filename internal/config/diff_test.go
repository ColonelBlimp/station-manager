package config

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// SHIP GATE (a) — THE VALUE POLICY AND THE COMPARISON ITSELF.
//
// These rules sit at internal/config because that is where the policy lives.
// The daemon-boundary rules (internal/api/config_save_log_test.go CS1-CS10,
// cmd/smd/config_save_startup_test.go B1-B3) prove the record reaches smd.log;
// these prove what may legally be IN it, and that a committed change cannot
// diff to nothing.
//
// Both rules below come from the clean-room review of 7b21b2b1, and both were
// real. Recorded here rather than in a commit message because each is a
// standing constraint, not a one-off fix:
//
//   - D1: keying "is this a URL?" on the leaf NAME is a denylist wearing an
//     allowlist's clothes. forwarders[x].endpoints.insert is a URL whose leaf
//     is called "insert", so it sailed past a check that looked for leaves
//     named "url" and was then logged in full by the forwarders[ allowlist.
//     The question has to be asked of the VALUE.
//   - D2: lookup.chain authority is represented by its explicit priority
//     leaves. JSON array order is not data for that list under ADR 0068.

func diffFor(t *testing.T, before, after Config) []FieldChange {
	t.Helper()
	return Diff(before, after)
}

func findChange(changes []FieldChange, field string) (FieldChange, bool) {
	for _, c := range changes {
		if c.Field == field {
			return c, true
		}
	}
	return FieldChange{}, false
}

// D1 — A URL IS REDUCED WHEREVER IT APPEARS, NOT WHERE IT IS NAMED "url".
// Forwarder endpoints are a map keyed by action, so the leaf carries the action
// name. An operator who put a token in an endpoint's query would have had it
// copied verbatim into a 0644 log out of a 0600 file.
func TestDiff_ForwarderEndpointUrlIsReducedToOrigin(t *testing.T) {
	const secretInQuery = "ENDPOINTTOKEN456"

	before := DefaultConfig(t.TempDir())
	before.Forwarders = []types.ForwarderConfig{{
		Name: "qrz", Type: "qrz",
		Endpoints: map[string]string{"insert": "https://old.example.com/api?token=" + secretInQuery},
	}}
	after := before.Clone()
	after.Forwarders[0].Endpoints = map[string]string{"insert": "https://new.example.com/api/v2?token=OTHER"}

	changes := diffFor(t, before, after)

	rendered, err := json.Marshal(changes)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(rendered), secretInQuery) {
		t.Fatalf("an endpoint query value reached the record: %s", rendered)
	}
	if strings.Contains(string(rendered), "/api/v2") {
		t.Errorf("the endpoint PATH reached the record: %s", rendered)
	}

	ch, ok := findChange(changes, "forwarders[qrz].endpoints.insert")
	if !ok {
		t.Fatalf("the endpoint change is not reported at all: %v — silence would pass "+
			"the leak checks above vacuously", changes)
	}
	if ch.From != "https://old.example.com" {
		t.Errorf("from = %q, want the origin only", ch.From)
	}
	if ch.To != "https://new.example.com" {
		t.Errorf("to = %q, want the origin only", ch.To)
	}
}

// D2 — PRIORITY, NOT ARRAY ORDER, IS THE AUTHORITY. A pure JSON reorder must
// produce no audit noise, while swapping the two explicit priority values must
// report both meaningful leaves.
func TestDiff_LookupChainUsesExplicitPriority(t *testing.T) {
	qrz := types.LookupConfig{Name: "qrz", Priority: 1, Enabled: true, URL: "https://qrz.example.com"}
	hamqth := types.LookupConfig{Name: "hamqth", Priority: 2, Enabled: true, URL: "https://hamqth.example.com"}

	before := DefaultConfig(t.TempDir())
	before.Lookup.Chain = []types.LookupConfig{qrz, hamqth}
	reordered := before.Clone()
	reordered.Lookup.Chain = []types.LookupConfig{hamqth, qrz}

	if changes := diffFor(t, before, reordered); len(changes) != 0 {
		t.Fatalf("array-only reorder produced changes: %v", changes)
	}

	after := before.Clone()
	after.Lookup.Chain[0].Priority = 2
	after.Lookup.Chain[1].Priority = 1
	changes := diffFor(t, before, after)
	for _, field := range []string{
		"lookup.chain[qrz].priority",
		"lookup.chain[hamqth].priority",
	} {
		if _, ok := findChange(changes, field); !ok {
			t.Errorf("priority change %q not reported: %v", field, changes)
		}
	}
}

// D2b — AND AN ORDER CHANGE IS NOT INVENTED WHEN MEMBERSHIP CHANGES. Adding a
// provider necessarily shifts the sequence; reporting that as a reorder on top
// of the per-field changes would be double-counting, and would make the
// ordering signal meaningless by firing on every list edit.
func TestDiff_AddingChainMemberIsNotReportedAsReorder(t *testing.T) {
	qrz := types.LookupConfig{Name: "qrz", Enabled: true, URL: "https://qrz.example.com"}
	hamqth := types.LookupConfig{Name: "hamqth", Enabled: true, URL: "https://hamqth.example.com"}

	before := DefaultConfig(t.TempDir())
	before.Lookup.Chain = []types.LookupConfig{qrz}
	after := before.Clone()
	after.Lookup.Chain = []types.LookupConfig{qrz, hamqth}

	changes := diffFor(t, before, after)
	if _, ok := findChange(changes, "lookup.chain"); ok {
		t.Errorf("adding a member was reported as a reorder as well: %v", changes)
	}
	if _, ok := findChange(changes, "lookup.chain[hamqth].enabled"); !ok {
		t.Errorf("the added provider is not reported at all: %v", changes)
	}
}

// D4 — A SIBLING FIELD DOES NOT INHERIT A CONTAINER'S ALLOWLIST ENTRY. From the
// clean-room review of 479245e9, and it was a regression I had just introduced:
// widening "forwarders[" to bare "forwarders" so a reorder could render its
// order also made strings.HasPrefix match every future top-level field STARTING
// with that word. `forwarders_api_token` would have been logged verbatim.
//
// The rule is stated over valuePolicy rather than over a Config, because the
// fields it guards against do not exist yet — that is the entire point. A test
// that could only use today's schema could not express "and whatever is added
// next".
func TestValuePolicy_SiblingFieldsDoNotInheritContainerEntries(t *testing.T) {
	for _, path := range []string{
		"forwarders_api_token", "rigs_private_data", "operators_secret_value",
		"useragent_token", "versionsecret", "map_credentials",
	} {
		if got := valuePolicy(path); got == policyValue {
			t.Errorf("valuePolicy(%q) = policyValue; an unrecognised field must be "+
				"redacted by default or the allowlist fails open", path)
		}
	}

	// ...while the containers and their real children still log values, or the
	// fix above would have been achieved by breaking the feature.
	for _, path := range []string{
		"forwarders", "forwarders[qrz].name", "rigs", "rigs[1].model",
		"operators", "operators[7Q5MLV].name", "useragent", "version",
	} {
		if got := valuePolicy(path); got != policyValue {
			t.Errorf("valuePolicy(%q) = %v, want policyValue", path, got)
		}
	}
}

// D5 — EVERY PREFIX ENTRY IS DELIMITER-BOUND. The structural half of D4: that
// rule catches the six paths it happens to name, this one catches the next
// entry someone adds. A prefix that does not end in "." or "[" matches sibling
// words, which is exactly how the regression arrived.
func TestValueAllowlist_PrefixesAreDelimiterBound(t *testing.T) {
	for _, p := range valueAllowlistPrefix {
		if !strings.HasSuffix(p, ".") && !strings.HasSuffix(p, "[") {
			t.Errorf("prefix %q is not delimiter-bound; it will match sibling "+
				"fields such as %q. Put exact paths in valueAllowlistExact instead.", p, p+"_token")
		}
	}
	if len(valueAllowlistPrefix) == 0 {
		t.Fatal("no prefixes to check; this rule would pass vacuously")
	}
}

// D3 — AN UNRECOGNISED PATH IS REDACTED, NOT LOGGED. The allowlist's whole
// purpose: a field added later is silent about its contents until someone
// decides otherwise. A denylist would publish it on the day it lands.
func TestDiff_UnknownPathIsRedacted(t *testing.T) {
	before := DefaultConfig(t.TempDir())
	before.Smtp.Password = "OLDPASSWORD"
	after := before.Clone()
	after.Smtp.Password = "NEWPASSWORD"

	changes := diffFor(t, before, after)

	rendered, err := json.Marshal(changes)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, leaked := range []string{"OLDPASSWORD", "NEWPASSWORD"} {
		if strings.Contains(string(rendered), leaked) {
			t.Fatalf("password value %q reached the record: %s", leaked, rendered)
		}
	}
	ch, ok := findChange(changes, "smtp.password")
	if !ok {
		t.Fatalf("the password change is not reported at all: %v", changes)
	}
	if !ch.Secret {
		t.Errorf("smtp.password is not marked secret: %+v", ch)
	}
}
