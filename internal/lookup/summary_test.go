package lookup_test

import (
	"os"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/lookup/hamnut"
	"github.com/ColonelBlimp/station-manager/internal/lookup/qrz"
	"github.com/ColonelBlimp/station-manager/internal/lookup/qrzcq"
	"github.com/ColonelBlimp/station-manager/internal/lookupdef"
)

// Lives here, not in lookupdef: lookupdef's own tests reset the registry, which
// would erase the providers' init() registrations this test reads.
//
// Settings → Enrichment shows each source collapsed as "<name> — <summary>"
// (fresh-install ruling 2026-09-26: e.g. "Hamnut — country and zones, free").
// Every shipped provider carries one, in plain words: short, lower-case
// start, no trailing full stop — it sits after a dash in a heading.
func TestShippedProvidersCarryAPlainSummary(t *testing.T) {
	for _, name := range []string{hamnut.ServiceName, qrz.ServiceName, qrzcq.ServiceName} {
		d, ok := lookupdef.Descriptor(name)
		if !ok {
			t.Fatalf("%s is not registered", name)
		}
		s := d.Summary
		switch {
		case s == "":
			t.Errorf("%s has no summary", name)
		case len(s) > 48:
			t.Errorf("%s summary %q is longer than a heading can carry", name, s)
		case s[len(s)-1] == '.':
			t.Errorf("%s summary %q ends with a full stop", name, s)
		}
	}
	if d, _ := lookupdef.Descriptor(hamnut.ServiceName); d.Summary != "country and zones, free" {
		t.Errorf("hamnut summary = %q, want the ruled wording", d.Summary)
	}
}

// The completion choices read "Name" and "Locator" (ruling 2026-09-26): the
// operator-facing word for a Maidenhead gridsquare. The JSON names are unchanged.
func TestCompletionFieldsDisplayPlainNames(t *testing.T) {
	got := map[string]string{}
	for _, f := range lookupdef.CompletionFields() {
		got[f.Name] = f.DisplayName
	}
	if got["name"] != "Name" || got["gridsquare"] != "Locator" {
		t.Fatalf("completion display names = %v, want name→Name, gridsquare→Locator", got)
	}
}

// Each source's ⓘ in Settings → Enrichment opens the manual at lookup-<name>
// (ruling 2026-09-26: the source descriptions live in the manual). The SPA
// builds the anchor from the descriptor, so a shipped provider without its
// manual section would link nowhere.
func TestShippedProvidersHaveAManualSection(t *testing.T) {
	raw, err := os.ReadFile("../../manual/content/chapters/enrichment.md")
	if err != nil {
		t.Fatalf("read the Enrichment chapter: %v", err)
	}
	for _, name := range []string{hamnut.ServiceName, qrz.ServiceName, qrzcq.ServiceName} {
		if !strings.Contains(string(raw), "{#lookup-"+name+"}") {
			t.Errorf("manual has no {#lookup-%s} section", name)
		}
	}
}
