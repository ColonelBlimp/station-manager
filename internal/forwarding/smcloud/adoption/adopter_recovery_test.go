package adoption

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0024 slice 2 (ADR 0097 §5): adoption follows its marker, not the
   default. A Home SM Cloud binding holding an adoption reservation, or an
   adoption not confirmed for the current station account, is recovered by
   the background adopter whichever logbook is the default; a new adoption
   still starts only on the effective default's unmarked binding. The real
   archive manager and SQLite throughout; two bindings, each with its own
   cloud name and its own, distinct, QSO UUIDs.

     FD1  a reserved former default is adopted for ITS logbook after the
          default moves, and the new default's unmarked binding is adopted
          independently.
     FD2  a former default adopted under another account is re-confirmed for
          the current account.
     FD3  a disabled former default is not attempted; re-enabled through the
          bindings PUT off the default, it is.
     FD4  evidence is each binding's own: a cloud UUID belonging to the other
          logbook refuses the binding it is foreign to, and only that one.
     FD5  state is per binding: an uncertain outcome or a terminal refusal on
          one survives the other's success, across repeated checks; an
          account change rearms both.
     FD6  preserved exclusions: an unmarked former default is never adopted;
          a binding with conflicting markers is never attempted.
     FD7  the default moving DURING an attempt: before the reservation, the
          unmarked initiation is abandoned; after it, the reserved binding's
          completion is recorded (it is now a marked recovery).
*/

const (
	qso3 = "019fd5c5-0000-7000-8000-000000000003"
	qso4 = "019fd5c5-0000-7000-8000-000000000004"
)

// formerDefault: newStation, plus the second logbook "Second" holding qso3
// and qso4 and bound to SM Cloud under "portable" (forwarder "cloud2"),
// enabled and unmarked; the cloud holds "portable" with qso3. mark is applied
// to the first binding ("cloud", "shack", qso1/qso2) before the default moves
// to the second logbook. The adopter and the manager's running bindings are
// rebuilt from the stored rows, as a start would.
func formerDefault(t *testing.T, mark string) *station {
	t.Helper()
	s := newStation(t)
	s.exec(`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
		VALUES (?, 'smcloud', 'cloud2', 1, '{"logbook":"portable"}')`, s.second)
	for i, u := range []string{qso3, qso4} {
		s.exec(`INSERT INTO qso (id, uuid, call, band, mode, freq, qso_date, time_on, time_off,
			rst_sent, rst_rcvd, country, dedupe_key, logbook_id)
			VALUES (?,?,'DL1A','20m','SSB',14200000,'20250509','0900','0900','59','59','Test',?,?)`,
			i+3, u, fmt.Sprintf("%064d", i+3), s.second)
	}
	s.cloud.set(func(c *fakeCloud) { c.others["portable"] = []string{qso3} })
	if mark != "" {
		s.exec(`UPDATE logbook_destination SET ` + mark + ` WHERE forwarder_name = 'cloud'`)
	}
	s.update(func(c *config.Config) { c.DefaultLogbookID = s.second })
	s.restart()
	return s
}

// restart rebuilds the adopter and the running bindings from the stored rows.
func (s *station) restart() {
	running := s.bindings()
	s.arch.m.SetActiveBindings(s.db, running)
	s.a = New(s.cfgSvc, s.arch, s.db, running, logging.NewForWriter(s.logs))
	s.arch.m.SetAdoptionStatus(s.a.StatusFor)
}

const (
	reservedMark = `adoption_reserved_at = datetime('now')`
	// adoptedElsewhere: adopted, but under another station account.
	adoptedElsewhere = `adoption_reserved_at = datetime('now'), remote_adopted_at = datetime('now'), remote_adopted_account = 'another-account'`
)

func (s *station) wantAdoptedFor(name string, logbookID int64, logbookName string) {
	s.t.Helper()
	reqs := s.cloud.adoptionsFor(name)
	if len(reqs) == 0 {
		s.t.Fatalf("no adoption request for %q; requests: %v", name, s.cloud.seen())
	}
	for _, r := range reqs {
		if r.LogbookUUID != s.logbookUUID(logbookID) || r.LogbookName != logbookName {
			s.t.Fatalf("adoption of %q mapped logbook %q (%s); want %q (%s)",
				name, r.LogbookName, r.LogbookUUID, logbookName, s.logbookUUID(logbookID))
		}
	}
	if !s.confirmed(logbookID) {
		s.t.Fatalf("logbook %d's binding is not confirmed: %+v", logbookID, s.binding(logbookID))
	}
}

func (s *station) wantStatusFor(forwarder, state, message string) {
	s.t.Helper()
	got := s.a.StatusFor(forwarder)
	if got.State != state || got.Message != message {
		s.t.Fatalf("%s status = %q %q; want %q %q", forwarder, got.State, got.Message, state, message)
	}
}

func (s *station) wantView(logbookID int64, state string) {
	s.t.Helper()
	v := s.view(logbookID)
	switch {
	case state == "" && v != nil:
		s.t.Fatalf("logbook %d shows %+v; want nothing", logbookID, *v)
	case state != "" && (v == nil || v.State != state):
		s.t.Fatalf("logbook %d shows %+v; want %q", logbookID, v, state)
	}
}

func TestAdopter_FD1_AReservedFormerDefaultIsRecoveredForItsOwnLogbook(t *testing.T) {
	s := formerDefault(t, reservedMark)
	s.check()
	s.wantAdoptedFor("shack", s.main, "Main")
	s.wantAdoptedFor("portable", s.second, "Second")
	s.wantView(s.main, archive.AdoptionStateAdoptedRestart)
	s.wantView(s.second, archive.AdoptionStateAdoptedRestart)
}

func TestAdopter_FD2_AFormerDefaultAdoptedElsewhereIsReconfirmed(t *testing.T) {
	s := formerDefault(t, adoptedElsewhere)
	s.wantView(s.main, archive.AdoptionStateNeedsConfirmation)
	s.check()
	s.wantAdoptedFor("shack", s.main, "Main")
	s.wantView(s.main, archive.AdoptionStateAdoptedRestart)
}

func TestAdopter_FD3_DisabledThenReEnabled(t *testing.T) {
	for name, mark := range map[string]string{"reserved": reservedMark, "adopted elsewhere": adoptedElsewhere} {
		t.Run(name, func(t *testing.T) {
			s := formerDefault(t, mark+`, enabled = 0`)
			s.check()
			if got := s.cloud.adoptionsFor("shack"); len(got) != 0 {
				t.Fatalf("a disabled binding was attempted: %v", got)
			}
			s.wantStatusFor("cloud", "", "")
			s.wantView(s.main, "")

			// Re-enabled through the bindings PUT, on a logbook that is not the
			// default: the claim already fixes the owner (ADR 0097 §5).
			if _, err := s.arch.m.ApplyBindings(context.Background(), homeID, types.ArchiveBindingsRequest{
				Destinations: []types.DestinationBindingEdit{{Type: smcloud.Type, Logbooks: []types.LogbookBindingEdit{{LogbookID: s.main, Enabled: true}}}},
			}); err != nil {
				t.Fatalf("re-enabling the claimed former default: %v", err)
			}
			if b := s.binding(s.main); !b.Enabled || b.IdentitySince != nil {
				t.Fatalf("after re-enabling: %+v; want enabled, never stamped", b)
			}
			s.check()
			s.wantAdoptedFor("shack", s.main, "Main")
		})
	}
}

func TestAdopter_FD4_EachBindingIsJudgedOnItsOwnLogbook(t *testing.T) {
	t.Run("a UUID of the other logbook refuses the former default only", func(t *testing.T) {
		s := formerDefault(t, reservedMark)
		s.cloud.set(func(c *fakeCloud) { c.uuids = append(c.uuids, qso3) }) // qso3 is Second's
		s.check()
		if got := s.cloud.adoptionsFor("shack"); len(got) != 0 {
			t.Fatalf("adopted with a foreign UUID: %v", got)
		}
		if s.confirmed(s.main) {
			t.Fatal("the former default was confirmed with a foreign UUID")
		}
		s.wantAdoptedFor("portable", s.second, "Second")
		s.wantStatusFor("cloud", StateBlocked, msgBlocked)
	})
	t.Run("a UUID of the other logbook refuses the new default only", func(t *testing.T) {
		s := formerDefault(t, reservedMark)
		s.cloud.set(func(c *fakeCloud) { c.others["portable"] = []string{qso3, qso1} }) // qso1 is Main's
		s.check()
		if got := s.cloud.adoptionsFor("portable"); len(got) != 0 {
			t.Fatalf("adopted with a foreign UUID: %v", got)
		}
		s.wantAdoptedFor("shack", s.main, "Main")
		s.wantStatusFor("cloud2", StateUnsafe, msgUnsafe)
	})
}

func TestAdopter_FD5_StateIsPerBinding(t *testing.T) {
	// An unresolved outcome (uncertain, or a cloud success not recorded) is
	// kept per binding: the other binding's success must not resolve it, so a
	// later failed read of this binding still shows it (ADR 0091 A5).
	for _, tc := range []struct {
		name, kept, other, keptName, keptPath string
		keptID, otherID                       func(s *station) int64
		otherName, otherLogbook               string
		fail                                  func(s *station)
		state, message                        string
	}{
		{name: "B1 uncertain, B2 adopted", kept: "cloud", other: "cloud2", keptName: "shack", keptPath: "/v1/logbooks/7/manifest",
			keptID: func(s *station) int64 { return s.main }, otherID: func(s *station) int64 { return s.second },
			otherName: "portable", otherLogbook: "Second",
			fail:  func(s *station) { s.cloud.set(func(c *fakeCloud) { c.dropFor["shack"] = true }) },
			state: StateUncertain, message: msgUncertain},
		{name: "B2 uncertain, B1 adopted", kept: "cloud2", other: "cloud", keptName: "portable", keptPath: "/v1/logbooks/8/manifest",
			keptID: func(s *station) int64 { return s.second }, otherID: func(s *station) int64 { return s.main },
			otherName: "shack", otherLogbook: "Main",
			fail:  func(s *station) { s.cloud.set(func(c *fakeCloud) { c.dropFor["portable"] = true }) },
			state: StateUncertain, message: msgUncertain},
		{name: "B1 record failed, B2 adopted", kept: "cloud", other: "cloud2", keptName: "shack", keptPath: "/v1/logbooks/7/manifest",
			keptID: func(s *station) int64 { return s.main }, otherID: func(s *station) int64 { return s.second },
			otherName: "portable", otherLogbook: "Second",
			fail:  func(s *station) { s.arch.recordErrFor = map[string]error{"cloud": fmt.Errorf("disk full")} },
			state: StateRecordFailed, message: msgRecord},
		{name: "B2 record failed, B1 adopted", kept: "cloud2", other: "cloud", keptName: "portable", keptPath: "/v1/logbooks/8/manifest",
			keptID: func(s *station) int64 { return s.second }, otherID: func(s *station) int64 { return s.main },
			otherName: "shack", otherLogbook: "Main",
			fail:  func(s *station) { s.arch.recordErrFor = map[string]error{"cloud2": fmt.Errorf("disk full")} },
			state: StateRecordFailed, message: msgRecord},
	} {
		t.Run(tc.name+": kept through the other's success and its own failed read", func(t *testing.T) {
			s := formerDefault(t, reservedMark)
			tc.fail(s)
			s.check()
			s.wantAdoptedFor(tc.otherName, tc.otherID(s), tc.otherLogbook)
			s.wantStatusFor(tc.kept, tc.state, tc.message)
			s.wantView(tc.keptID(s), tc.state)

			// The kept binding's next attempt fails before any request that
			// could resolve it: it must still show its unresolved outcome.
			s.cloud.set(func(c *fakeCloud) { c.forced[tc.keptPath] = http.StatusServiceUnavailable })
			s.check()
			s.wantStatusFor(tc.kept, tc.state, tc.message)
			s.wantView(tc.keptID(s), tc.state)
			s.wantView(tc.otherID(s), archive.AdoptionStateAdoptedRestart)

			// Once the read recovers, the retry resolves it.
			s.cloud.set(func(c *fakeCloud) { delete(c.forced, tc.keptPath) })
			s.check()
			s.wantAdoptedFor(tc.keptName, tc.keptID(s), map[string]string{"shack": "Main", "portable": "Second"}[tc.keptName])
			s.wantView(tc.otherID(s), archive.AdoptionStateAdoptedRestart)
		})
	}
	t.Run("a terminal refusal stays suppressed while the other retries, until the account changes", func(t *testing.T) {
		s := formerDefault(t, reservedMark)
		s.cloud.set(func(c *fakeCloud) {
			c.conflictFor["shack"] = "legacy_archive_adopted_elsewhere"
			c.dropFor["portable"] = true
		})
		s.check()
		s.wantStatusFor("cloud", StateConflict, "Not adopted: the adoption conflicts (legacy_archive_adopted_elsewhere).")
		s.wantStatusFor("cloud2", StateUncertain, msgUncertain)
		shackTries := len(s.cloud.adoptionsFor("shack"))

		s.check() // the uncertain one retries; the refused one does not
		if got := len(s.cloud.adoptionsFor("shack")); got != shackTries {
			t.Fatalf("the refused binding was retried without a relevant change (%d → %d requests)", shackTries, got)
		}
		s.wantStatusFor("cloud", StateConflict, "Not adopted: the adoption conflicts (legacy_archive_adopted_elsewhere).")
		s.wantAdoptedFor("portable", s.second, "Second")
		s.check()
		s.wantStatusFor("cloud", StateConflict, "Not adopted: the adoption conflicts (legacy_archive_adopted_elsewhere).")

		// An account change is relevant to both: each is judged again.
		s.cloud.set(func(c *fakeCloud) { delete(c.conflictFor, "shack") })
		s.rotate("rotated")
		s.check()
		s.wantAdoptedFor("shack", s.main, "Main")
		s.wantAdoptedFor("portable", s.second, "Second") // re-confirmed for the new account
	})
}

func TestAdopter_FD6_PreservedExclusions(t *testing.T) {
	t.Run("an unmarked former default is never adopted", func(t *testing.T) {
		s := formerDefault(t, "")
		s.check()
		s.check()
		if got := s.cloud.adoptionsFor("shack"); len(got) != 0 {
			t.Fatalf("an unmarked former default was adopted: %v", got)
		}
		if b := s.binding(s.main); b.AdoptionReservedAt != nil || b.RemoteAdoptedAt != nil {
			t.Fatalf("an unmarked former default was marked: %+v", b)
		}
		s.wantStatusFor("cloud", "", "")
		s.wantView(s.main, "")
		s.wantAdoptedFor("portable", s.second, "Second")
	})
	t.Run("conflicting markers are never attempted", func(t *testing.T) {
		s := formerDefault(t, reservedMark+`, identity_since = datetime('now')`)
		s.check()
		if got := s.cloud.adoptionsFor("shack"); len(got) != 0 {
			t.Fatalf("a binding with conflicting markers was attempted: %v", got)
		}
		s.wantStatusFor("cloud", "", "")
	})
}

func TestAdopter_FD7_TheDefaultMovesDuringAnAttempt(t *testing.T) {
	t.Run("before the reservation: the unmarked initiation is abandoned", func(t *testing.T) {
		s := newStation(t)
		s.exec(`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
			VALUES (?, 'smcloud', 'cloud2', 1, '{"logbook":"portable"}')`, s.second)
		s.restart()
		s.cloud.set(func(c *fakeCloud) {
			c.hook = func(path string) {
				if path == "/v1/logbooks/7/manifest" {
					s.update(func(c *config.Config) { c.DefaultLogbookID = s.second })
				}
			}
		})
		s.check()
		if got := s.cloud.adoptionsFor("shack"); len(got) != 0 {
			t.Fatalf("an abandoned initiation sent an adoption: %v", got)
		}
		if b := s.binding(s.main); b.AdoptionReservedAt != nil {
			t.Fatalf("an abandoned initiation reserved: %+v", b)
		}
	})
	t.Run("after the reservation: the completion is recorded on the reserved binding", func(t *testing.T) {
		s := newStation(t)
		s.exec(`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
			VALUES (?, 'smcloud', 'cloud2', 1, '{"logbook":"portable"}')`, s.second)
		s.restart()
		s.cloud.set(func(c *fakeCloud) {
			c.hook = func(path string) {
				if path == "/v1/archives/adopt" {
					s.update(func(c *config.Config) { c.DefaultLogbookID = s.second })
				}
			}
		})
		s.check()
		if !s.confirmed(s.main) {
			t.Fatalf("the reserved binding's completion was discarded: %+v", s.binding(s.main))
		}
		s.wantView(s.main, archive.AdoptionStateAdoptedRestart)
	})
}
