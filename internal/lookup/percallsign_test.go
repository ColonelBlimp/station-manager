package lookup_test

import (
	"context"
	stderrs "errors"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/lookup"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// W-0031: the country of a callsign is the provider's answer for THAT callsign.
//
// It used to be the longest prefix the local table already held, so a held
// parent block claimed the separate entities inside it: with `VK` (Australia)
// held, VK9XY (Christmas Island) read as Australia and the provider was never
// asked. Every test here starts from the state that made that fault: the table
// holds only the parent row. A table that happens to hold the specific prefix
// already would hide it.

var (
	australia = types.Country{Name: "Australia", Prefix: "VK", Continent: "OC", CQZone: "30", ITUZone: "59", DXCCPrefix: "VK"}
	christmas = types.Country{Name: "Christmas Island", Prefix: "VK9X", Continent: "OC", CQZone: "29", ITUZone: "54", DXCCPrefix: "VK9X"}
)

// parentHeld is a database whose country table holds only the parent block.
func parentHeld(t *testing.T) *lookup.Orchestrator {
	t.Helper()
	db := newTestSqlite(t)
	if err := db.UpsertCountry(australia); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return &lookup.Orchestrator{DB: db, CountryTTL: time.Hour, Refresher: &recordingRefresher{}}
}

func TestPerCallsign_AHeldParentBlockDoesNotAnswerForAnEntityInsideIt(t *testing.T) {
	o := parentHeld(t)
	hamnut := &stubCountryProvider{name: "hamnut", result: christmas}
	o.Country = hamnut

	got := o.Enrich(context.Background(), "VK9XY")

	if hamnut.calls != 1 {
		t.Fatalf("the provider was asked %d times, want 1: a held parent prefix must not stand in for it", hamnut.calls)
	}
	if got.CountrySource != lookup.SourceHamnut || got.Country.Name != "Christmas Island" || got.Country.CQZone != "29" {
		t.Fatalf("got %q / %q / CQ %q, want hamnut / Christmas Island / 29", got.CountrySource, got.Country.Name, got.Country.CQZone)
	}
}

func TestPerCallsign_TheAnswerIsRememberedForThatCallsign(t *testing.T) {
	o := parentHeld(t)
	hamnut := &stubCountryProvider{name: "hamnut", result: christmas}
	o.Country = hamnut
	o.Enrich(context.Background(), "VK9XY")

	// The provider now answers something else, to show the second read is local.
	hamnut.result = australia
	got := o.Enrich(context.Background(), "VK9XY")

	if hamnut.calls != 1 {
		t.Fatalf("the provider was asked %d times, want 1: the second lookup of a callsign is local", hamnut.calls)
	}
	if got.CountrySource != lookup.SourceCountryTable || got.Country.Name != "Christmas Island" {
		t.Fatalf("got %q / %q, want country_table / Christmas Island", got.CountrySource, got.Country.Name)
	}
}

// What is remembered for one callsign answers for no other: another call in the
// same block is asked about in its own right.
func TestPerCallsign_AnotherCallsignIsAskedInItsOwnRight(t *testing.T) {
	o := parentHeld(t)
	hamnut := &stubCountryProvider{name: "hamnut", result: christmas}
	o.Country = hamnut
	o.Enrich(context.Background(), "VK9XY")

	hamnut.result = australia
	got := o.Enrich(context.Background(), "VK2ABC")

	if hamnut.calls != 2 || got.Country.Name != "Australia" || got.CountrySource != lookup.SourceHamnut {
		t.Fatalf("calls %d, got %q / %q; want 2, hamnut / Australia", hamnut.calls, got.CountrySource, got.Country.Name)
	}
}

// When the provider cannot be asked, the best the local data gives is returned,
// and SAYS it is that: a prefix match, which may be a parent block.
func TestPerCallsign_ProviderUnreachable_FallsBackToTheHeldPrefixAndSaysSo(t *testing.T) {
	for name, provider := range map[string]lookup.CountryProvider{
		"a transport failure":   &stubCountryProvider{name: "hamnut", err: stderrs.New("dial tcp: connection refused")},
		"a bare not-found":      &stubCountryProvider{name: "hamnut", err: errors.ErrNotFound},
		"the disabled sentinel": &stubCountryProvider{name: "hamnut", result: types.Country{Name: "Unknown"}},
		"no provider at all":    nil,
	} {
		t.Run(name, func(t *testing.T) {
			o := parentHeld(t)
			if provider != nil {
				o.Country = provider
			}

			got := o.Enrich(context.Background(), "VK9XY")

			if got.CountrySource != lookup.SourceCountryPrefix {
				t.Fatalf("CountrySource = %q, want %q", got.CountrySource, lookup.SourceCountryPrefix)
			}
			if got.Country.Name != "Australia" {
				t.Fatalf("Country = %q, want the held prefix's (Australia)", got.Country.Name)
			}
			// Nothing was remembered for the callsign: a guess must not become its answer.
			if _, err := o.DB.FetchCallsignCountryWithContext(context.Background(), "VK9XY"); !stderrs.Is(err, errors.ErrNotFound) {
				t.Fatalf("a fallback answer was remembered for the callsign: %v", err)
			}
		})
	}
}

// ...and the next lookup asks again, so the guess is replaced as soon as the
// provider answers.
func TestPerCallsign_AFallbackIsNotSticky(t *testing.T) {
	o := parentHeld(t)
	hamnut := &stubCountryProvider{name: "hamnut", err: stderrs.New("timeout")}
	o.Country = hamnut
	if got := o.Enrich(context.Background(), "VK9XY"); got.CountrySource != lookup.SourceCountryPrefix {
		t.Fatalf("fixture: first lookup = %q", got.CountrySource)
	}

	hamnut.err, hamnut.result = nil, christmas
	got := o.Enrich(context.Background(), "VK9XY")

	if got.CountrySource != lookup.SourceHamnut || got.Country.Name != "Christmas Island" {
		t.Fatalf("got %q / %q, want hamnut / Christmas Island", got.CountrySource, got.Country.Name)
	}
}

func TestPerCallsign_ProviderUnreachable_NothingHeld_IsNone(t *testing.T) {
	o := &lookup.Orchestrator{DB: newTestSqlite(t), CountryTTL: time.Hour, Refresher: &recordingRefresher{},
		Country: &stubCountryProvider{name: "hamnut", err: stderrs.New("timeout")}}

	if got := o.Enrich(context.Background(), "VK9XY"); got.CountrySource != lookup.SourceNone {
		t.Fatalf("CountrySource = %q, want none", got.CountrySource)
	}
}

// The provider's explicit verdict is the provider's: a held parent prefix does
// not overrule "I do not recognise this".
func TestPerCallsign_TheProvidersNotRecognisedIsNotOverruledByAHeldPrefix(t *testing.T) {
	o := parentHeld(t)
	o.Country = &stubCountryProvider{name: "hamnut", err: lookup.ErrPrefixNotRecognised}

	if got := o.Enrich(context.Background(), "VK9XY"); got.CountrySource != lookup.SourceNotFound {
		t.Fatalf("CountrySource = %q, want not_found", got.CountrySource)
	}
}

// The provider holds entries for single callsigns (a portable call whose prefix
// does not show the island). Such an answer is for that callsign alone: it is
// remembered for it, and it is NOT written to the prefix table, where it would
// answer for any longer call that begins with it.
func TestPerCallsign_AWholeCallsignEntryIsNeverAppliedToAnotherCall(t *testing.T) {
	o := parentHeld(t)
	entry := christmas
	entry.Prefix = "VK9/SP9XYZ" // the provider matched the whole callsign
	hamnut := &stubCountryProvider{name: "hamnut", result: entry}
	o.Country = hamnut

	got := o.Enrich(context.Background(), "VK9/SP9XYZ")
	if got.Country.Name != "Christmas Island" {
		t.Fatalf("got %q", got.Country.Name)
	}
	if _, err := o.DB.FetchCountryByPrefix("VK9/SP9XYZ"); !stderrs.Is(err, errors.ErrNotFound) {
		t.Fatalf("a whole-callsign entry was written to the prefix table: %v", err)
	}

	// With the provider gone, a longer call beginning with it falls back to the
	// parent block, not to the other callsign's entry.
	hamnut.err = stderrs.New("timeout")
	other := o.Enrich(context.Background(), "VK9/SP9XYZA")
	if other.CountrySource != lookup.SourceCountryPrefix || other.Country.Name != "Australia" {
		t.Fatalf("got %q / %q, want country_prefix / Australia", other.CountrySource, other.Country.Name)
	}
	// And the callsign itself is still answered from what was remembered for it.
	same := o.Enrich(context.Background(), "VK9/SP9XYZ")
	if same.CountrySource != lookup.SourceCountryTable || same.Country.Name != "Christmas Island" {
		t.Fatalf("got %q / %q, want country_table / Christmas Island", same.CountrySource, same.Country.Name)
	}
}

// A prefix answer still feeds the prefix table, which is what the fallback reads.
func TestPerCallsign_APrefixAnswerStillFeedsTheFallbackTable(t *testing.T) {
	o := parentHeld(t)
	o.Country = &stubCountryProvider{name: "hamnut", result: christmas}
	o.Enrich(context.Background(), "VK9XY")

	row, err := o.DB.FetchCountryByPrefix("VK9X")
	if err != nil || row.Name != "Christmas Island" {
		t.Fatalf("the prefix row was not written: %q, %v", row.Name, err)
	}
}

// A remembered answer older than the country TTL is served and refreshed behind
// it, as a stale country row always was; the refresh rewrites what is remembered.
func TestPerCallsign_AStaleAnswerIsServedAndRefreshed(t *testing.T) {
	o := parentHeld(t)
	hamnut := &stubCountryProvider{name: "hamnut", result: australia} // an old, wrong answer
	o.Country = hamnut
	o.Enrich(context.Background(), "VK9XY")

	o.CountryTTL = time.Nanosecond
	o.Refresher = &syncRefresher{}
	time.Sleep(2 * time.Millisecond)
	hamnut.result = christmas

	stale := o.Enrich(context.Background(), "VK9XY")
	if stale.CountrySource != lookup.SourceCountryTable || stale.Country.Name != "Australia" {
		t.Fatalf("stale read = %q / %q, want country_table / Australia", stale.CountrySource, stale.Country.Name)
	}
	if hamnut.calls != 2 {
		t.Fatalf("provider calls = %d, want 2 (the first lookup and the refresh)", hamnut.calls)
	}
	remembered, err := o.DB.FetchCallsignCountryWithContext(context.Background(), "VK9XY")
	if err != nil || remembered.Name != "Christmas Island" {
		t.Fatalf("the refresh did not rewrite what is remembered: %q, %v", remembered.Name, err)
	}
}

// A forced refresh asks the provider whatever is remembered, and on a failure
// gives nothing rather than the old answer or a prefix guess (as it always did).
func TestPerCallsign_ForcedRefresh_AsksAndDoesNotFallBack(t *testing.T) {
	o := parentHeld(t)
	hamnut := &stubCountryProvider{name: "hamnut", result: australia}
	o.Country = hamnut
	o.Enrich(context.Background(), "VK9XY")

	hamnut.result = christmas
	got := o.EnrichRefresh(context.Background(), "VK9XY")
	if got.CountrySource != lookup.SourceHamnut || got.Country.Name != "Christmas Island" {
		t.Fatalf("got %q / %q, want hamnut / Christmas Island", got.CountrySource, got.Country.Name)
	}
	if again := o.Enrich(context.Background(), "VK9XY"); again.Country.Name != "Christmas Island" || hamnut.calls != 2 {
		t.Fatalf("after the refresh: %q, %d calls; want Christmas Island remembered, 2 calls", again.Country.Name, hamnut.calls)
	}

	hamnut.err = stderrs.New("timeout")
	if failed := o.EnrichRefresh(context.Background(), "VK9XY"); failed.CountrySource != lookup.SourceNone {
		t.Fatalf("a failed forced refresh = %q, want none", failed.CountrySource)
	}
}

// The station record's own country fields follow the callsign's answer too: the
// station refresh merges what is remembered for the callsign, not the held
// prefix. The two stores are made to DISAGREE before the refresh (Codex,
// codex-252): resolving the callsign wrote Christmas Island to both, so a refresh
// that read the prefix table would have passed unnoticed.
func TestPerCallsign_TheStationRecordIsMergedFromTheCallsignsAnswer(t *testing.T) {
	o := parentHeld(t)
	o.Country = &stubCountryProvider{name: "hamnut", result: christmas}
	qrz := &stubCallsignProvider{name: "qrz", result: types.ContactedStation{
		Call: "VK9XY", Name: "An Operator", Country: "Australia",
	}}
	o.Chain = []lookup.CallsignProvider{qrz}

	got := o.Enrich(context.Background(), "VK9XY")
	if got.Station.Country != "Christmas Island" {
		t.Fatalf("station country = %q, want Christmas Island", got.Station.Country)
	}

	// The longest held prefix for this call now says something else.
	wrong := australia
	wrong.Prefix = "VK9X"
	if err := o.DB.UpsertCountry(wrong); err != nil {
		t.Fatalf("seed the disagreeing prefix row: %v", err)
	}
	if held, err := o.DB.FetchCountryByCallsign("VK9XY"); err != nil || held.Name != "Australia" {
		t.Fatalf("fixture: the held prefix for VK9XY = %q, %v; want Australia", held.Name, err)
	}

	// The stale-station refresh re-merges from what is remembered for the callsign.
	o.StationTTL = time.Nanosecond
	refresher := &syncRefresher{}
	o.Refresher = refresher
	time.Sleep(2 * time.Millisecond)
	o.Enrich(context.Background(), "VK9XY")

	if refresher.scheduled != 1 || qrz.calls != 2 {
		t.Fatalf("the station refresh did not run: scheduled %d, provider calls %d; want 1 and 2", refresher.scheduled, qrz.calls)
	}
	row, err := o.DB.FetchContactedStationByCallsign("VK9XY")
	if err != nil || row.Country != "Christmas Island" {
		t.Fatalf("stored station country = %q, %v; want Christmas Island (the callsign's answer, not the held prefix)", row.Country, err)
	}
}

// The provider's explicit "I do not recognise this" is a verdict about the
// callsign, and what was remembered for that callsign does not outlive it (the
// clean-room review of a09f4adc). It used to: the stale refresh treated the
// verdict like a network failure, so the old country was served for ever and a
// refresh was scheduled at every lookup.
func TestPerCallsign_ARejectionAtTheStaleRefreshForgetsTheCallsignsAnswer(t *testing.T) {
	o := parentHeld(t)
	hamnut := &stubCountryProvider{name: "hamnut", result: christmas}
	o.Country = hamnut
	o.Enrich(context.Background(), "VK9XY")

	o.CountryTTL = time.Nanosecond
	o.Refresher = &syncRefresher{}
	time.Sleep(2 * time.Millisecond)
	hamnut.result, hamnut.err = types.Country{}, lookup.ErrPrefixNotRecognised

	// The stale answer is served this once, as a stale answer always is.
	if stale := o.Enrich(context.Background(), "VK9XY"); stale.Country.Name != "Christmas Island" {
		t.Fatalf("stale read = %q, want Christmas Island", stale.Country.Name)
	}
	if _, err := o.DB.FetchCallsignCountryWithContext(context.Background(), "VK9XY"); !stderrs.Is(err, errors.ErrNotFound) {
		t.Fatalf("the rejected answer is still remembered: %v", err)
	}
	// And the next lookup is the provider's verdict, not the old country.
	if next := o.Enrich(context.Background(), "VK9XY"); next.CountrySource != lookup.SourceNotFound {
		t.Fatalf("next lookup = %q / %q, want not_found", next.CountrySource, next.Country.Name)
	}
}

func TestPerCallsign_ARejectionAtAForcedRefreshForgetsTheCallsignsAnswer(t *testing.T) {
	o := parentHeld(t)
	hamnut := &stubCountryProvider{name: "hamnut", result: christmas}
	o.Country = hamnut
	o.Enrich(context.Background(), "VK9XY")

	hamnut.result, hamnut.err = types.Country{}, lookup.ErrPrefixNotRecognised
	if got := o.EnrichRefresh(context.Background(), "VK9XY"); got.CountrySource != lookup.SourceNotFound {
		t.Fatalf("forced refresh = %q, want not_found", got.CountrySource)
	}
	calls := hamnut.calls
	next := o.Enrich(context.Background(), "VK9XY")
	if next.CountrySource != lookup.SourceNotFound || hamnut.calls != calls+1 {
		t.Fatalf("next lookup = %q / %q with %d new calls; want not_found, asked afresh",
			next.CountrySource, next.Country.Name, hamnut.calls-calls)
	}
}

// A failure that is NOT a verdict forgets nothing: the provider could not be
// asked, and the remembered answer is the best there is.
func TestPerCallsign_AFailureThatIsNotAVerdictForgetsNothing(t *testing.T) {
	for name, failure := range map[string]error{
		"a transport failure": stderrs.New("timeout"),
		"a bare not-found":    errors.ErrNotFound,
	} {
		t.Run(name, func(t *testing.T) {
			o := parentHeld(t)
			hamnut := &stubCountryProvider{name: "hamnut", result: christmas}
			o.Country = hamnut
			o.Enrich(context.Background(), "VK9XY")

			hamnut.result, hamnut.err = types.Country{}, failure
			o.EnrichRefresh(context.Background(), "VK9XY") // forced, and it fails
			o.CountryTTL = time.Nanosecond
			o.Refresher = &syncRefresher{}
			time.Sleep(2 * time.Millisecond)
			o.Enrich(context.Background(), "VK9XY") // stale, and its refresh fails

			kept, err := o.DB.FetchCallsignCountryWithContext(context.Background(), "VK9XY")
			if err != nil || kept.Name != "Christmas Island" {
				t.Fatalf("the remembered answer was lost to a failure: %q, %v", kept.Name, err)
			}
		})
	}
}

// rejectingAfterAnotherAnswer is a provider that says "not recognised", but not
// before a newer answer for the same callsign has been remembered: the state a
// forced refresh is in when a second refresh of the same callsign succeeds
// while the first is still waiting for its station lookup.
type rejectingAfterAnotherAnswer struct {
	stubCountryProvider
	meanwhile func()
}

func (p *rejectingAfterAnotherAnswer) LookupWithContext(_ context.Context, _ string) (types.Country, error) {
	p.calls++
	time.Sleep(5 * time.Millisecond) // the request is out
	p.meanwhile()
	return types.Country{}, lookup.ErrPrefixNotRecognised
}

// An older rejection arriving late does not delete an answer remembered after
// the provider was asked (the clean-room review of 557cd26a).
func TestPerCallsign_AnOlderRejectionDoesNotDeleteANewerAnswer(t *testing.T) {
	for name, run := range map[string]func(o *lookup.Orchestrator){
		"at a forced refresh": func(o *lookup.Orchestrator) {
			o.EnrichRefresh(context.Background(), "VK9XY")
		},
		"at the stale refresh": func(o *lookup.Orchestrator) {
			o.CountryTTL = time.Nanosecond
			o.Refresher = &syncRefresher{}
			time.Sleep(2 * time.Millisecond)
			o.Enrich(context.Background(), "VK9XY")
		},
	} {
		t.Run(name, func(t *testing.T) {
			o := parentHeld(t)
			o.Country = &stubCountryProvider{name: "hamnut", result: australia}
			o.Enrich(context.Background(), "VK9XY") // something is remembered

			o.Country = &rejectingAfterAnotherAnswer{
				stubCountryProvider: stubCountryProvider{name: "hamnut"},
				meanwhile: func() {
					if err := o.DB.UpsertCallsignCountryWithContext(context.Background(), "VK9XY", christmas); err != nil {
						t.Errorf("the other refresh: %v", err)
					}
				},
			}
			run(o)

			kept, err := o.DB.FetchCallsignCountryWithContext(context.Background(), "VK9XY")
			if err != nil || kept.Name != "Christmas Island" {
				t.Fatalf("the newer answer was deleted by the older rejection: %q, %v", kept.Name, err)
			}
		})
	}
}

// ----- the new-entity check once the catalogue knows every entity -----

// newEntityFixture is a log holding one earlier contact with the given country
// and stored DXCC number, and an orchestrator whose provider answers `c`.
func newEntityFixture(t *testing.T, c types.Country, earlierCountry, earlierDxcc string) *lookup.Orchestrator {
	t.Helper()
	db := newTestSqlite(t)
	if _, err := db.InsertLogbookWithContext(context.Background(), types.Logbook{Name: "Default", Callsign: "G4ABC"}); err != nil {
		t.Fatalf("seed logbook: %v", err)
	}
	if _, err := db.InsertQsoWithContext(context.Background(), types.Qso{
		LogbookID: 1,
		UUID:      utils.NewUUIDv7(),
		DedupeKey: strings.Repeat("e", 64),
		QsoDetails: types.QsoDetails{
			Band: "10m", Mode: "FT8", Freq: "28074",
			QsoDate: "20261031", TimeOn: "1200", TimeOff: "1201", RstSent: "-10", RstRcvd: "-12",
		},
		ContactedStation: types.ContactedStation{Call: "TN8XX", Country: earlierCountry, DXCC: earlierDxcc},
	}); err != nil {
		t.Fatalf("seed the earlier contact: %v", err)
	}
	return &lookup.Orchestrator{DB: db, CountryTTL: time.Hour, StationTTL: time.Hour,
		Country: &stubCountryProvider{name: "hamnut", result: c}}
}

var congo = types.Country{Name: "Republic of the Congo", Prefix: "TN", Continent: "AF", DXCCPrefix: "TN"} // TN → 412

// A contact logged before the catalogue knew its entity carries the country and
// no number. Now that the prefix has a number, the check goes by number and
// would find nothing: the entity would look new though it has been worked (the
// clean-room review of 1952d50a). Such a contact still counts.
func TestNewEntity_AContactStoredWithoutItsNumberStillCountsAsWorked(t *testing.T) {
	o := newEntityFixture(t, congo, "Republic of the Congo", "")

	got := o.Enrich(context.Background(), "TN8GD")
	if got.Country.DXCCPrefix != "TN" {
		t.Fatalf("fixture: country not resolved: %+v", got.Country)
	}
	if got.Country.IsNewEntity {
		t.Fatal("IsNewEntity = true; the entity was worked, by a contact stored without its number")
	}
}

// The number still rules where a number was stored: a contact with the same
// country NAME and another entity's number is not this entity worked.
func TestNewEntity_AContactStoredWithAnotherNumberDoesNotCount(t *testing.T) {
	o := newEntityFixture(t, congo, "Republic of the Congo", "414") // 414 is another entity

	if got := o.Enrich(context.Background(), "TN8GD"); !got.Country.IsNewEntity {
		t.Fatal("IsNewEntity = false; a contact carrying another entity's number was counted")
	}
}

func TestNewEntity_NothingLoggedIsNew(t *testing.T) {
	o := newEntityFixture(t, congo, "Germany", "230")

	if got := o.Enrich(context.Background(), "TN8GD"); !got.Country.IsNewEntity {
		t.Fatal("IsNewEntity = false with no contact of that entity in the log")
	}
}
