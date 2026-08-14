package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

func TestAcceptance_PrioritisedCallsignLookupConfig(t *testing.T) {
	t.Run("legacy array order becomes explicit priority with the initial completion policy", func(t *testing.T) {
		cfg := Config{Lookup: types.EnrichmentConfig{Chain: []types.LookupConfig{
			{Name: "first"},
			{Name: "second"},
		}}}

		Normalize(&cfg)

		if got := []int{cfg.Lookup.Chain[0].Priority, cfg.Lookup.Chain[1].Priority}; !reflect.DeepEqual(got, []int{1, 2}) {
			t.Fatalf("migrated priorities = %v, want [1 2]", got)
		}
		if !reflect.DeepEqual(cfg.Lookup.ContinueIfBlank, []string{"name", "gridsquare"}) {
			t.Fatalf("continue_if_blank = %v, want [name gridsquare]", cfg.Lookup.ContinueIfBlank)
		}
	})

	t.Run("explicit priority is authoritative and persisted in canonical order", func(t *testing.T) {
		cfg := Config{Lookup: types.EnrichmentConfig{
			ContinueIfBlank: []string{},
			Chain: []types.LookupConfig{
				{Name: "second", Priority: 2},
				{Name: "first", Priority: 1},
			},
		}}

		Normalize(&cfg)

		if got := []string{cfg.Lookup.Chain[0].Name, cfg.Lookup.Chain[1].Name}; !reflect.DeepEqual(got, []string{"first", "second"}) {
			t.Fatalf("canonical chain = %v, want [first second]", got)
		}
		if cfg.Lookup.ContinueIfBlank == nil || len(cfg.Lookup.ContinueIfBlank) != 0 {
			t.Fatalf("explicit empty continue_if_blank was not preserved: %#v", cfg.Lookup.ContinueIfBlank)
		}
	})

	t.Run("validation canonicalises a copy without reordering its caller", func(t *testing.T) {
		lc := types.EnrichmentConfig{Chain: []types.LookupConfig{
			{Name: "second", Priority: 2},
			{Name: "first", Priority: 1},
		}}
		if err := validateLookup(lc); err != nil {
			t.Fatalf("validateLookup: %v", err)
		}
		if got := []string{lc.Chain[0].Name, lc.Chain[1].Name}; !reflect.DeepEqual(got, []string{"second", "first"}) {
			t.Fatalf("validation mutated caller chain order: %v", got)
		}
	})

	for _, tc := range []struct {
		name string
		lc   types.EnrichmentConfig
		want string
	}{
		{
			name: "duplicate priority",
			lc: types.EnrichmentConfig{Chain: []types.LookupConfig{
				{Name: "a", Priority: 1}, {Name: "b", Priority: 1},
			}},
			want: "duplicate priority 1",
		},
		{
			name: "priority gap",
			lc: types.EnrichmentConfig{Chain: []types.LookupConfig{
				{Name: "a", Priority: 1}, {Name: "b", Priority: 3},
			}},
			want: "priority 2 is missing",
		},
		{
			name: "mixed implicit and explicit",
			lc: types.EnrichmentConfig{Chain: []types.LookupConfig{
				{Name: "a", Priority: 1}, {Name: "b"},
			}},
			want: "priority must be greater than zero",
		},
		{
			name: "unknown completion field",
			lc: types.EnrichmentConfig{
				ContinueIfBlank: []string{"name", "qth"},
				Chain:           []types.LookupConfig{{Name: "a", Priority: 1}},
			},
			want: `continue_if_blank[1]: unknown callsign field "qth"`,
		},
		{
			name: "duplicate completion field",
			lc: types.EnrichmentConfig{
				ContinueIfBlank: []string{"name", "name"},
				Chain:           []types.LookupConfig{{Name: "a", Priority: 1}},
			},
			want: `continue_if_blank[1]: duplicate callsign field "name"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateLookup(tc.lc); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateLookup error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestApplyDefaults_SeedsCallsignProviderAtNextPriority(t *testing.T) {
	withProvider(t, callsignDescriptor("newprov"))
	cfg := Config{Lookup: types.EnrichmentConfig{Chain: []types.LookupConfig{
		{Name: "existing", Priority: 1},
	}}}

	applyDefaults(&cfg, t.TempDir())

	if len(cfg.Lookup.Chain) != 2 {
		t.Fatalf("chain length = %d, want 2", len(cfg.Lookup.Chain))
	}
	if got := cfg.Lookup.Chain[1]; got.Name != "newprov" || got.Priority != 2 || got.Enabled {
		t.Fatalf("seeded provider = %+v, want disabled newprov at priority 2", got)
	}
}
