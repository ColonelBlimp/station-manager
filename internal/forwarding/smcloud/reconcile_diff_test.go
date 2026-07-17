package smcloud

import (
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Pure diff-logic tests — the decision table reconcile self-healing stands on.
// The end-to-end path (real sqlite + real cloud server) is reconcile_e2e_test.go.

var at = time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)

func local(uuid string, mod time.Time, deleted bool) types.QsoManifestEntry {
	return types.QsoManifestEntry{UUID: uuid, ModifiedAt: mod, Deleted: deleted}
}

func TestDiff_CloudMissingLiveRow(t *testing.T) {
	up, del, only, newer := diffManifests(
		[]types.QsoManifestEntry{local("A", at, false)}, map[string]cloudEntry{})
	if len(up) != 1 || up[0] != "A" || len(del) != 0 || only != 0 || newer != 0 {
		t.Fatalf("got up=%v del=%v only=%d newer=%d", up, del, only, newer)
	}
}

func TestDiff_CloudMissingTombstoneNeedsNothing(t *testing.T) {
	up, del, _, _ := diffManifests(
		[]types.QsoManifestEntry{local("A", at, true)}, map[string]cloudEntry{})
	if len(up) != 0 || len(del) != 0 {
		t.Fatalf("a never-pushed tombstone must not heal-push: up=%v del=%v", up, del)
	}
}

func TestDiff_EqualIsQuiet(t *testing.T) {
	up, del, only, newer := diffManifests(
		[]types.QsoManifestEntry{local("A", at, false)},
		map[string]cloudEntry{"a": {modified: at}})
	if len(up)+len(del)+only+newer != 0 {
		t.Fatalf("equal rows produced work: up=%v del=%v only=%d newer=%d", up, del, only, newer)
	}
}

func TestDiff_StaleCloudCopy(t *testing.T) {
	up, _, _, _ := diffManifests(
		[]types.QsoManifestEntry{local("A", at, false)},
		map[string]cloudEntry{"a": {modified: at.Add(-time.Hour)}})
	if len(up) != 1 {
		t.Fatalf("stale cloud copy must re-push: up=%v", up)
	}
}

func TestDiff_MissedDelete(t *testing.T) {
	up, del, _, _ := diffManifests(
		[]types.QsoManifestEntry{local("A", at, true)},
		map[string]cloudEntry{"a": {modified: at.Add(-time.Hour), deleted: false}})
	if len(del) != 1 || del[0] != "A" || len(up) != 0 {
		t.Fatalf("missed delete must push the tombstone: up=%v del=%v", up, del)
	}
}

func TestDiff_BothDeletedIsQuiet(t *testing.T) {
	up, del, _, _ := diffManifests(
		[]types.QsoManifestEntry{local("A", at, true)},
		map[string]cloudEntry{"a": {modified: at, deleted: true}})
	if len(up)+len(del) != 0 {
		t.Fatalf("matching tombstones produced work: up=%v del=%v", up, del)
	}
}

func TestDiff_ResurrectOverCloudTombstone(t *testing.T) {
	// Local live + newer (or equal) vs cloud tombstone: local is
	// authoritative — edit-after-delete wins, push the live row.
	for _, cloudMod := range []time.Time{at.Add(-time.Minute), at} {
		up, del, _, _ := diffManifests(
			[]types.QsoManifestEntry{local("A", at, false)},
			map[string]cloudEntry{"a": {modified: cloudMod, deleted: true}})
		if len(up) != 1 || len(del) != 0 {
			t.Fatalf("cloudMod=%v: resurrect must push: up=%v del=%v", cloudMod, up, del)
		}
	}
}

func TestDiff_CloudNewerUntouched(t *testing.T) {
	up, del, _, newer := diffManifests(
		[]types.QsoManifestEntry{local("A", at, false)},
		map[string]cloudEntry{"a": {modified: at.Add(time.Hour)}})
	if len(up)+len(del) != 0 || newer != 1 {
		t.Fatalf("cloud-newer must be counted, never healed: up=%v del=%v newer=%d", up, del, newer)
	}
}

func TestDiff_CloudOnlyCountedNeverTouched(t *testing.T) {
	up, del, only, _ := diffManifests(nil,
		map[string]cloudEntry{"x": {modified: at}, "y": {modified: at, deleted: true}})
	if len(up)+len(del) != 0 || only != 2 {
		t.Fatalf("cloud-only rows are the retentive superset: up=%v del=%v only=%d", up, del, only)
	}
}

func TestDiff_UuidCaseFolded(t *testing.T) {
	up, _, only, _ := diffManifests(
		[]types.QsoManifestEntry{local("0197F9A0-AAAA-7000-8000-000000000001", at, false)},
		map[string]cloudEntry{"0197f9a0-aaaa-7000-8000-000000000001": {modified: at}})
	if len(up) != 0 || only != 0 {
		t.Fatalf("case difference read as divergence: up=%v only=%d", up, only)
	}
}

func TestDiff_MicrosecondEquality(t *testing.T) {
	// Local ns-precision value vs its µs-truncated cloud copy = equal.
	nsLocal := at.Add(123456789 * time.Nanosecond)
	usCloud := at.Add(123456 * time.Microsecond)
	up, _, _, newer := diffManifests(
		[]types.QsoManifestEntry{local("A", nsLocal, false)},
		map[string]cloudEntry{"a": {modified: usCloud}})
	if len(up) != 0 || newer != 0 {
		t.Fatalf("sub-µs difference read as drift: up=%v newer=%d", up, newer)
	}
}
