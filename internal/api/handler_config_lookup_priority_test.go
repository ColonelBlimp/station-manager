package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

func TestAcceptance_LookupPriorityAndCompletionPolicyRoundTrip(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Lookup.ContinueIfBlank = []string{"name", "gridsquare"}
		cfg.Lookup.Chain = []types.LookupConfig{
			{Name: "first", Priority: 1, Password: "first-secret"},
			{Name: "second", Priority: 2, Password: "second-secret"},
		}
	})

	body := `{"lookup":{"hamnut":{"name":"hamnutlookupservice"},` +
		`"continue_if_blank":["gridsquare"],"chain":[` +
		`{"name":"second","enabled":false,"priority":2},` +
		`{"name":"first","enabled":false,"priority":1}]}}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	got := srv.cfg.Snapshot().Lookup
	if !reflect.DeepEqual(got.ContinueIfBlank, []string{"gridsquare"}) {
		t.Fatalf("stored continue_if_blank = %v, want [gridsquare]", got.ContinueIfBlank)
	}
	if len(got.Chain) != 2 || got.Chain[0].Name != "first" || got.Chain[0].Priority != 1 ||
		got.Chain[1].Name != "second" || got.Chain[1].Priority != 2 {
		t.Fatalf("stored chain was not canonicalised by priority: %+v", got.Chain)
	}
	if got.Chain[0].Password != "first-secret" || got.Chain[1].Password != "second-secret" {
		t.Fatalf("priority round-trip disturbed masked passwords: %+v", got.Chain)
	}

	var resp ConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Lookup == nil || resp.Lookup.ContinueIfBlank == nil ||
		!reflect.DeepEqual(*resp.Lookup.ContinueIfBlank, []string{"gridsquare"}) {
		t.Fatalf("response lookup policy = %+v", resp.Lookup)
	}
	if resp.Lookup.Chain[0].Priority != 1 || resp.Lookup.Chain[1].Priority != 2 {
		t.Fatalf("response priorities = %+v", resp.Lookup.Chain)
	}
}

func TestAcceptance_ExplicitEmptyCompletionPolicyRoundTripsAsEmptyList(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Lookup.ContinueIfBlank = []string{"name", "gridsquare"}
		cfg.Lookup.Chain = []types.LookupConfig{{Name: "only", Priority: 1}}
	})

	body := `{"lookup":{"hamnut":{"name":"hamnutlookupservice"},` +
		`"continue_if_blank":[],"chain":[{"name":"only","priority":1}]}}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	stored := srv.cfg.Snapshot().Lookup.ContinueIfBlank
	if stored == nil || len(stored) != 0 {
		t.Fatalf("stored continue_if_blank = %#v, want explicit []", stored)
	}
	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	lookup := response["lookup"].(map[string]any)
	if fields, ok := lookup["continue_if_blank"].([]any); !ok || len(fields) != 0 {
		t.Fatalf("response continue_if_blank = %#v, want []", lookup["continue_if_blank"])
	}
}

func TestAcceptance_OmittedCompletionPolicyPreservesStoredPolicy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stored []string
	}{
		{name: "custom field set", stored: []string{"gridsquare"}},
		{name: "explicit legacy behavior", stored: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := testServerWithCfg(t, func(cfg *config.Config) {
				cfg.Lookup.ContinueIfBlank = slices.Clone(tc.stored)
				cfg.Lookup.Chain = []types.LookupConfig{{Name: "only", Priority: 1}}
			})

			// Pre-ADR-0068 clients replace the whole lookup block but know
			// neither continue_if_blank nor explicit priorities. Omission must
			// preserve the stored completion policy; all-zero priorities still
			// follow the documented legacy array-order migration.
			body := `{"lookup":{"hamnut":{"name":"hamnutlookupservice"},` +
				`"chain":[{"name":"only","enabled":false}]}}`
			w := putConfigSmtp(t, srv, body)
			if w.Code != http.StatusOK {
				t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
			}

			got := srv.cfg.Snapshot().Lookup.ContinueIfBlank
			if !reflect.DeepEqual(got, tc.stored) || (tc.stored != nil && got == nil) {
				t.Fatalf("stored continue_if_blank = %#v, want preserved %#v", got, tc.stored)
			}
		})
	}
}
