package evidence

import (
	"bytes"
	"encoding/json"
	stderrors "errors"
	"strings"
	"testing"
)

func TestStatus_QueryFailureMakesOnlyItsDatabaseGroupUnknown(t *testing.T) {
	tests := []struct {
		name       string
		group      string
		nullFields []string
		unknown    func(*testing.T, Status)
		healthy    func(*testing.T, Status)
	}{
		{
			name:       "observations total",
			group:      "observations_total",
			nullFields: []string{"observations", "unprofiled_observations"},
			unknown: func(t *testing.T, st Status) {
				t.Helper()
				if st.Observations != nil || st.UnprofiledObservations != nil {
					t.Fatalf("observation group = %v/%v, want null/null", st.Observations, st.UnprofiledObservations)
				}
			},
			healthy: assertStatusNonObservationGroups,
		},
		{
			name:       "unprofiled observation count",
			group:      "observations_unprofiled",
			nullFields: []string{"observations", "unprofiled_observations"},
			unknown: func(t *testing.T, st Status) {
				t.Helper()
				if st.Observations != nil || st.UnprofiledObservations != nil {
					t.Fatalf("observation group = %v/%v, want null/null", st.Observations, st.UnprofiledObservations)
				}
			},
			healthy: assertStatusNonObservationGroups,
		},
		{
			name:       "profile totals",
			group:      "profiles_total",
			nullFields: []string{"lineages", "versions", "unprofiled"},
			unknown: func(t *testing.T, st Status) {
				t.Helper()
				if st.Profiles.Lineages != nil || st.Profiles.Versions != nil || st.Profiles.Unprofiled != nil {
					t.Fatalf("profiles group = %+v, want database fields unknown", st.Profiles)
				}
			},
			healthy: assertStatusNonProfileGroups,
		},
		{
			name:       "profile grouping",
			group:      "profiles_unprofiled",
			nullFields: []string{"lineages", "versions", "unprofiled"},
			unknown: func(t *testing.T, st Status) {
				t.Helper()
				if st.Profiles.Lineages != nil || st.Profiles.Versions != nil || st.Profiles.Unprofiled != nil {
					t.Fatalf("profiles group = %+v, want database fields unknown", st.Profiles)
				}
			},
			healthy: assertStatusNonProfileGroups,
		},
		{
			name:       "retention totals",
			group:      "retention_total",
			nullFields: []string{"purged_observations", "purged_coverage", "records", "metadata_bytes"},
			unknown: func(t *testing.T, st Status) {
				t.Helper()
				if st.Retention.PurgedObservations != nil || st.Retention.PurgedCoverage != nil || st.Retention.Records != nil || st.Retention.MetadataBytes != nil {
					t.Fatalf("retention group = %+v, want database fields unknown", st.Retention)
				}
			},
			healthy: assertStatusNonRetentionGroups,
		},
		{
			name:       "retention metadata",
			group:      "retention_metadata",
			nullFields: []string{"purged_observations", "purged_coverage", "records", "metadata_bytes"},
			unknown: func(t *testing.T, st Status) {
				t.Helper()
				if st.Retention.PurgedObservations != nil || st.Retention.PurgedCoverage != nil || st.Retention.Records != nil || st.Retention.MetadataBytes != nil {
					t.Fatalf("retention group = %+v, want database fields unknown", st.Retention)
				}
			},
			healthy: assertStatusNonRetentionGroups,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig(t, true)
			var logs bytes.Buffer
			s := newRunningLogged(t, cfg, &logs)
			fault := stderrors.New(tt.name + " fault")
			statusQueryFaultForTest = func(group string) error {
				if group == tt.group {
					return fault
				}
				return nil
			}
			t.Cleanup(func() { statusQueryFaultForTest = nil })

			st := s.Status()
			if !st.Degraded || !strings.Contains(st.StatusError, fault.Error()) {
				t.Fatalf("status degraded/error = %t/%q, want fault", st.Degraded, st.StatusError)
			}
			tt.unknown(t, st)
			tt.healthy(t, st)
			encoded, err := json.Marshal(st)
			if err != nil {
				t.Fatalf("marshal status: %v", err)
			}
			for _, field := range tt.nullFields {
				if !strings.Contains(string(encoded), `"`+field+`":null`) {
					t.Fatalf("status JSON %s does not expose %q as null", encoded, field)
				}
			}
		})
	}
}

func TestStatus_SyncCountFailureMakesTheEntireSyncGroupUnknown(t *testing.T) {
	cfg := testConfig(t, true)
	cfg.Sync, cfg.SyncURL, cfg.SyncToken = true, "http://example.invalid", "test-token"
	var logs bytes.Buffer
	s := newRunningLogged(t, cfg, &logs)
	var target string
	statusQueryFaultForTest = func(group string) error {
		if group == target {
			return stderrors.New("sync count fault")
		}
		return nil
	}
	t.Cleanup(func() { statusQueryFaultForTest = nil })

	for _, kind := range syncTables {
		for _, prefix := range []string{"sync_unsynced_", "sync_quarantined_"} {
			target = prefix + kind.kind
			st := s.Status()
			if !st.Degraded || st.Sync == nil || st.Sync.Unsynced != nil || st.Sync.Quarantined != nil {
				t.Fatalf("%s: sync status = %+v, want degraded with null database count group", target, st.Sync)
			}
			assertStatusNonSyncGroups(t, st)
			encoded, err := json.Marshal(st)
			if err != nil {
				t.Fatalf("marshal status: %v", err)
			}
			if !strings.Contains(string(encoded), `"unsynced":null`) || !strings.Contains(string(encoded), `"quarantined":null`) {
				t.Fatalf("sync JSON %s does not expose its failed group as null", encoded)
			}
		}
	}
}

func TestStatus_QueryFailureLogsOneEdgeAndOneRecovery(t *testing.T) {
	cfg := testConfig(t, true)
	var logs bytes.Buffer
	s := newRunningLogged(t, cfg, &logs)
	statusQueryFaultForTest = func(group string) error {
		if group == "observations_total" {
			return stderrors.New("database unavailable")
		}
		return nil
	}
	t.Cleanup(func() { statusQueryFaultForTest = nil })

	for range 3 {
		_ = s.Status()
	}
	if got := len(rhLines(&logs, "status database reads degraded")); got != 1 {
		t.Fatalf("degraded status log lines = %d, want 1: %s", got, logs.String())
	}
	statusQueryFaultForTest = nil
	if st := s.Status(); st.Degraded {
		t.Fatalf("recovered status remains degraded: %+v", st)
	}
	if got := len(rhLines(&logs, "status database reads recovered")); got != 1 {
		t.Fatalf("recovered status log lines = %d, want 1: %s", got, logs.String())
	}
}

func assertStatusNonObservationGroups(t *testing.T, st Status) {
	t.Helper()
	if st.Profiles == nil || st.Profiles.Lineages == nil || st.Profiles.Versions == nil || st.Profiles.Unprofiled == nil {
		t.Fatalf("profiles became unavailable with observation fault: %+v", st.Profiles)
	}
	if st.Retention == nil || st.Retention.PurgedObservations == nil || st.Retention.PurgedCoverage == nil || st.Retention.Records == nil || st.Retention.MetadataBytes == nil {
		t.Fatalf("retention became unavailable with observation fault: %+v", st.Retention)
	}
}

func assertStatusNonProfileGroups(t *testing.T, st Status) {
	t.Helper()
	if st.Observations == nil || st.UnprofiledObservations == nil {
		t.Fatalf("observations became unavailable with profile fault: %+v", st)
	}
	if st.Retention == nil || st.Retention.PurgedObservations == nil || st.Retention.PurgedCoverage == nil || st.Retention.Records == nil || st.Retention.MetadataBytes == nil {
		t.Fatalf("retention became unavailable with profile fault: %+v", st.Retention)
	}
}

func assertStatusNonRetentionGroups(t *testing.T, st Status) {
	t.Helper()
	if st.Observations == nil || st.UnprofiledObservations == nil {
		t.Fatalf("observations became unavailable with retention fault: %+v", st)
	}
	if st.Profiles == nil || st.Profiles.Lineages == nil || st.Profiles.Versions == nil || st.Profiles.Unprofiled == nil {
		t.Fatalf("profiles became unavailable with retention fault: %+v", st.Profiles)
	}
}

func assertStatusNonSyncGroups(t *testing.T, st Status) {
	t.Helper()
	if st.Observations == nil || st.UnprofiledObservations == nil {
		t.Fatalf("observations became unavailable with sync fault: %+v", st)
	}
	if st.Profiles == nil || st.Profiles.Lineages == nil || st.Profiles.Versions == nil || st.Profiles.Unprofiled == nil {
		t.Fatalf("profiles became unavailable with sync fault: %+v", st.Profiles)
	}
	if st.Retention == nil || st.Retention.PurgedObservations == nil || st.Retention.PurgedCoverage == nil || st.Retention.Records == nil || st.Retention.MetadataBytes == nil {
		t.Fatalf("retention became unavailable with sync fault: %+v", st.Retention)
	}
}
