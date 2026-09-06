package config

import (
	"slices"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
)

// W-0008 CC-5 (alpha.2 dogfood Finding #6). alpha.1 (config version 2) filled an
// omitted qrzcq action_filter with the historical all-three default because that
// build had no supported-action registration for the type; alpha.2 registers
// qrzcq as insert-only and validateForwarders refuses the stored filter, so an
// upgraded install could not start. The ruling is deliberately NARROW: only the
// identifiable alpha.1-generated shape — a pre-version-3 document, type "qrzcq",
// action_filter exactly the ORDERED slice ["insert","update","delete"] that
// alpha.1's omitted-filter path emitted — migrates to ["insert"]. Every
// other explicit unsupported filter, and the same shape in a version-3 document
// (which alpha.1 could never have written), stays rejected.

// ensureQrzcqInsertOnly mirrors the real qrzcq package registration for this test
// binary (the config package imports only the registry, not the forwarders).
func ensureQrzcqInsertOnly(t *testing.T) {
	t.Helper()
	if _, ok := forwarding.SupportedActionsFor("qrzcq"); !ok {
		forwarding.RegisterSupportedActions("qrzcq", []forwarding.Action{action.Insert})
	}
}

func forwarderFilter(t *testing.T, m map[string]any, idx int) []string {
	t.Helper()
	fwds, ok := m["forwarders"].([]any)
	if !ok || len(fwds) <= idx {
		t.Fatalf("forwarders[%d] missing in %v", idx, m["forwarders"])
	}
	fc, ok := fwds[idx].(map[string]any)
	if !ok {
		t.Fatalf("forwarders[%d]: not an object", idx)
	}
	raw, ok := fc["action_filter"].([]any)
	if !ok {
		t.Fatalf("forwarders[%d].action_filter: %T", idx, fc["action_filter"])
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		out = append(out, v.(string))
	}
	return out
}

const alpha1Qrzcq = `{"name":"qrzcq","type":"qrzcq","enabled":false,
	"action_filter":["insert","update","delete"],"batch_size":5,"tick_interval_sec":120}`

// PIN 1 — the exact alpha.1-generated shape migrates to ["insert"]; the
// forwarder's other persisted overrides are untouched.
func TestMigrateV3_ReconcilesAlpha1QrzcqFilter(t *testing.T) {
	m := migratedMap(t, `{"version":2,"forwarders":[`+alpha1Qrzcq+`]}`)
	if got := forwarderFilter(t, m, 0); len(got) != 1 || got[0] != "insert" {
		t.Fatalf("qrzcq action_filter = %v, want [insert]", got)
	}
	fc := m["forwarders"].([]any)[0].(map[string]any)
	if fc["batch_size"] != float64(5) || fc["tick_interval_sec"] != float64(120) || fc["enabled"] != false {
		t.Fatalf("neighbouring overrides changed: %v", fc)
	}
}

// PIN 2 — an operator-authored qrzcq filter that is NOT the legacy shape is left
// for validation to reject, even in a version-2 document.
func TestMigrateV3_LeavesOperatorAuthoredQrzcqFilter(t *testing.T) {
	m := migratedMap(t, `{"version":2,"forwarders":[
		{"name":"qrzcq","type":"qrzcq","action_filter":["insert","update"]}]}`)
	if got, want := forwarderFilter(t, m, 0), []string{"insert", "update"}; !slices.Equal(got, want) {
		t.Fatalf("operator-authored filter changed: got %v, want %v", got, want)
	}
}

// PIN 2b — a permutation of the legacy values is NOT the shape alpha.1 emitted
// (its omitted-filter path wrote the ordered slice) and migrates unchanged.
func TestMigrateV3_LeavesPermutedQrzcqFilter(t *testing.T) {
	m := migratedMap(t, `{"version":2,"forwarders":[
		{"name":"qrzcq","type":"qrzcq","action_filter":["delete","update","insert"]}]}`)
	if got, want := forwarderFilter(t, m, 0), []string{"delete", "update", "insert"}; !slices.Equal(got, want) {
		t.Fatalf("permuted filter changed: got %v, want %v", got, want)
	}
}

// PIN 3 — the all-three shape on any OTHER type is not the qrzcq legacy shape and
// migrates unchanged; a valid qrzcq ["insert"] is untouched.
func TestMigrateV3_LeavesOtherTypesAndValidFilters(t *testing.T) {
	m := migratedMap(t, `{"version":2,"forwarders":[
		{"name":"qrz","type":"qrz","action_filter":["insert","update","delete"]},
		{"name":"cl","type":"clublog","action_filter":["insert","delete"]},
		{"name":"qrzcq","type":"qrzcq","action_filter":["insert"]}]}`)
	for i, want := range [][]string{{"insert", "update", "delete"}, {"insert", "delete"}, {"insert"}} {
		if got := forwarderFilter(t, m, i); !slices.Equal(got, want) {
			t.Fatalf("forwarders[%d] filter changed: got %v, want %v", i, got, want)
		}
	}
}

// PIN 4 — end to end: an alpha.1-written file boots on this build with the
// reconciled filter (the dogfood pass-2 failure, now fixed).
func TestLoad_Alpha1QrzcqLegacyShapeBoots(t *testing.T) {
	ensureQrzcqInsertOnly(t)
	cfg, err := Load(writeCfg(t, `{"version":2,"forwarders":[`+alpha1Qrzcq+`]}`))
	if err != nil {
		t.Fatalf("Load of the alpha.1 shape: %v", err)
	}
	if got := cfg.Forwarders[0].ActionFilter; len(got) != 1 || got[0] != "insert" {
		t.Fatalf("loaded qrzcq action_filter = %v, want [insert]", got)
	}
}

// PIN 5 — the same content in a version-3 document is not alpha.1-generated and is
// still refused (RegisterSupportedActions contract, registry.go:185).
func TestLoad_V3QrzcqAllThreeStillRejected(t *testing.T) {
	ensureQrzcqInsertOnly(t)
	_, err := Load(writeCfg(t, `{"version":3,"forwarders":[
		{"name":"qrzcq","type":"qrzcq","action_filter":["insert","update","delete"]}]}`))
	if err == nil || !strings.Contains(err.Error(), "does not support action") {
		t.Fatalf("v3 all-three qrzcq filter: err = %v, want 'does not support action'", err)
	}
}

// PIN 6b — end to end: a v2 permutation of the legacy values is operator-authored,
// not alpha.1-emitted, and is still refused.
func TestLoad_V2QrzcqPermutedFilterStillRejected(t *testing.T) {
	ensureQrzcqInsertOnly(t)
	_, err := Load(writeCfg(t, `{"version":2,"forwarders":[
		{"name":"qrzcq","type":"qrzcq","action_filter":["delete","update","insert"]}]}`))
	if err == nil || !strings.Contains(err.Error(), "does not support action") {
		t.Fatalf("v2 permuted [delete update insert]: err = %v, want 'does not support action'", err)
	}
}

// PIN 6 — an operator-authored unsupported filter in a version-2 document is still
// refused: the migration reconciles the legacy shape only.
func TestLoad_V2QrzcqOperatorAuthoredStillRejected(t *testing.T) {
	ensureQrzcqInsertOnly(t)
	_, err := Load(writeCfg(t, `{"version":2,"forwarders":[
		{"name":"qrzcq","type":"qrzcq","action_filter":["insert","update"]}]}`))
	if err == nil || !strings.Contains(err.Error(), "does not support action") {
		t.Fatalf("v2 operator-authored [insert update]: err = %v, want 'does not support action'", err)
	}
}
