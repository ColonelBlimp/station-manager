package config

import (
	"encoding/json"
	"testing"
)

// ADR 0075 — the v2→v3 migration consumes the retired version-2 keys before the
// ADR 0074 unknown-key gate, reconciling ADR 0067's "boots over the legacy key"
// with ADR 0074's strictness. These are the operator's required pins.

func migratedMap(t *testing.T, doc string) map[string]any {
	t.Helper()
	out, err := migrateDocument([]byte(doc))
	if err != nil {
		t.Fatalf("migrateDocument(%s): %v", doc, err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal migrated: %v", err)
	}
	return m
}

func obj(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("expected %q to be an object; got %T", key, m[key])
	}
	return v
}

// PIN 1 — a v2 document carrying ALL four retired paths migrates: each retired
// key is gone and its value has landed on the canonical field.
func TestMigrateV3_ConsumesAllRetiredKeys(t *testing.T) {
	doc := `{
		"version": 2,
		"ft8": {"tx": {"auto_work_callers": true, "caller_answer_mode": "auto_first"},
		        "meter": {"alc_red": 42, "alc_amber": 7}},
		"rigs": [{"id": 1, "model": "ftdx10", "audio": {"device": "plughw:1,0"}}],
		"psk_reporter": {"enabled": true, "antenna": "dipole @ 10m"}
	}`
	m := migratedMap(t, doc)

	if v, _ := m["version"].(float64); int(v) != currentConfigVersion {
		t.Fatalf("version = %v, want %d", m["version"], currentConfigVersion)
	}

	ft8 := obj(t, m, "ft8")
	if _, present := obj(t, ft8, "tx")["auto_work_callers"]; present {
		t.Error("ft8.tx.auto_work_callers survived migration")
	}
	if got := obj(t, ft8, "tx")["caller_answer_mode"]; got != "auto_first" {
		t.Errorf("neighbouring ft8.tx.caller_answer_mode lost: %v", got)
	}
	if _, present := obj(t, ft8, "meter")["alc_red"]; present {
		t.Error("ft8.meter.alc_red survived migration")
	}
	if got := obj(t, ft8, "meter")["alc_amber"]; got == nil {
		t.Error("neighbouring ft8.meter.alc_amber lost")
	}

	rigs, _ := m["rigs"].([]any)
	if len(rigs) != 1 {
		t.Fatalf("rigs = %v", m["rigs"])
	}
	audio := obj(t, rigs[0].(map[string]any), "audio")
	if _, present := audio["device"]; present {
		t.Error("rigs[0].audio.device survived migration")
	}
	if audio["rx"] != "plughw:1,0" || audio["tx"] != "plughw:1,0" {
		t.Errorf("device did not fold into rx/tx: %v", audio)
	}

	if _, present := obj(t, m, "psk_reporter")["antenna"]; present {
		t.Error("psk_reporter.antenna survived migration")
	}
	if got := obj(t, m, "logging_station")["my_antenna"]; got != "dipole @ 10m" {
		t.Errorf("antenna did not move to logging_station.my_antenna: %v", got)
	}

	// And nothing is left for the gate to reject.
	if got := UnknownKeys([]byte(doc)); len(got) != 0 {
		t.Fatalf("retired keys still flagged as unknown after migration: %v", got)
	}
}

// PIN 2 — the SAME paths in a v3 (current) document are rejected as unknown: a
// current-version file has no business carrying a retired key.
func TestMigrateV3_RetiredPathsRejectedAtCurrentVersion(t *testing.T) {
	cases := []struct {
		doc  string
		want string
	}{
		{`{"version":3,"ft8":{"tx":{"auto_work_callers":true}}}`, "ft8.tx.auto_work_callers"},
		{`{"version":3,"ft8":{"meter":{"alc_red":1}}}`, "ft8.meter.alc_red"},
		{`{"version":3,"rigs":[{"audio":{"device":"x"}}]}`, "rigs[0].audio.device"},
		{`{"version":3,"psk_reporter":{"antenna":"y"}}`, "psk_reporter.antenna"},
	}
	for _, tc := range cases {
		got := UnknownKeys([]byte(tc.doc))
		if !containsPath(got, tc.want) {
			t.Errorf("v3 doc: %q not rejected; got %v", tc.want, got)
		}
	}
}

// PIN 3 — a retired path AND a genuine typo together: the retired path is
// migrated away and only the typo is reported.
func TestMigrateV3_RetiredConsumedButTypoReported(t *testing.T) {
	doc := `{"version":2,"ft8":{"tx":{"auto_work_callers":true}},"bogus_typo":1}`
	got := UnknownKeys([]byte(doc))
	if len(got) != 1 || got[0] != "bogus_typo" {
		t.Fatalf("want only [bogus_typo]; got %v", got)
	}
}

// PIN 4 — the v1→v2→v3 chain consumes a rigs[].audio.device that the v1→v2 step
// SYNTHESISES from loose fields.
func TestMigrateV3_ChainConsumesSynthesizedAudioDevice(t *testing.T) {
	v1 := `{
		"bridge": {"cat": {"driver": "ftdx10"}, "mode_mappings": {"ftdx10": {"USB": {"mode": "USB"}}}},
		"ft8": {"device": "plughw:2,0"}
	}`
	m := migratedMap(t, v1)
	rigs, _ := m["rigs"].([]any)
	if len(rigs) != 1 {
		t.Fatalf("v1→v2 did not synthesise a rig: %v", m["rigs"])
	}
	audio := obj(t, rigs[0].(map[string]any), "audio")
	if _, present := audio["device"]; present {
		t.Error("synthesised audio.device was not consumed by v2→v3")
	}
	if audio["rx"] != "plughw:2,0" || audio["tx"] != "plughw:2,0" {
		t.Errorf("synthesised device did not fold into rx/tx: %v", audio)
	}
	if got := UnknownKeys([]byte(v1)); len(got) != 0 {
		t.Fatalf("chain left unknown keys: %v", got)
	}
}

// Fold semantics — device fills rx/tx only when ABSENT; an operator's split
// rx/tx is never overwritten.
func TestMigrateV3_DeviceDoesNotOverwriteSplitAudio(t *testing.T) {
	doc := `{"version":2,"rigs":[{"audio":{"device":"legacy","rx":"keep-rx","tx":"keep-tx"}}]}`
	m := migratedMap(t, doc)
	audio := obj(t, m["rigs"].([]any)[0].(map[string]any), "audio")
	if audio["rx"] != "keep-rx" || audio["tx"] != "keep-tx" {
		t.Errorf("split rx/tx overwritten by legacy device: %v", audio)
	}
	if _, present := audio["device"]; present {
		t.Error("device not deleted")
	}
}

// Fold semantics — the canonical my_antenna wins when present; the retired key is
// still deleted.
func TestMigrateV3_CanonicalAntennaWins(t *testing.T) {
	doc := `{"version":2,"psk_reporter":{"antenna":"retired"},"logging_station":{"my_antenna":"canonical"}}`
	m := migratedMap(t, doc)
	if got := obj(t, m, "logging_station")["my_antenna"]; got != "canonical" {
		t.Errorf("canonical my_antenna was overwritten: %v", got)
	}
	if _, present := obj(t, m, "psk_reporter")["antenna"]; present {
		t.Error("retired psk_reporter.antenna not deleted")
	}
}

// Presence, rather than non-emptiness, decides precedence: an explicitly blank
// canonical field must not resurrect the retired antenna value.
func TestMigrateV3_ExplicitBlankCanonicalAntennaWins(t *testing.T) {
	doc := `{"version":2,"psk_reporter":{"antenna":"retired"},"logging_station":{"my_antenna":""}}`
	m := migratedMap(t, doc)
	if got := obj(t, m, "logging_station")["my_antenna"]; got != "" {
		t.Errorf("explicit blank canonical my_antenna was overwritten: %v", got)
	}
	if _, present := obj(t, m, "psk_reporter")["antenna"]; present {
		t.Error("retired psk_reporter.antenna not deleted")
	}
}

// PIN 5 (idempotency half) — running the migration twice is a no-op the second
// time: the retired keys are already gone and nothing else moves.
func TestMigrateV3_Idempotent(t *testing.T) {
	doc := `{
		"version": 2,
		"ft8": {"tx": {"auto_work_callers": true}, "meter": {"alc_red": 1}},
		"rigs": [{"audio": {"device": "d"}}],
		"psk_reporter": {"antenna": "a"}
	}`
	once, err := migrateDocument([]byte(doc))
	if err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	// A v3 document is returned unchanged by migrateDocument, so re-running the
	// step directly is the honest idempotency check.
	var m map[string]any
	if err := json.Unmarshal(once, &m); err != nil {
		t.Fatal(err)
	}
	if err := migrateV2toV3(m); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	twice, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	// Re-marshal `once` through the same map round-trip for a key-order-independent
	// comparison.
	var m2 map[string]any
	_ = json.Unmarshal(once, &m2)
	onceNorm, _ := json.Marshal(m2)
	if string(twice) != string(onceNorm) {
		t.Fatalf("migration not idempotent:\n once:  %s\n twice: %s", onceNorm, twice)
	}
}
