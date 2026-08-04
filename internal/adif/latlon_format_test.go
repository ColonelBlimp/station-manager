package adif

/*
   LAT / LON leave in ADIF Location format, not decimal degrees.

   THE DEFECT (2026-08-04). The ADIF Location type is "XDDD MM.MMM" — degrees
   plus decimal minutes with a hemisphere letter. SM emits MY_LAT / MY_LON in
   exactly that form, because config.go derives them through
   utils.MaidenheadToADIFLatLon. The CONTACTED station's LAT / LON come from
   QRZ as decimal degrees and were passed straight to the writer, so one binary
   emitted both:

       <MY_LAT:11>S011 26.250      correct
       <LAT:10>-89.979167          not an ADIF Location

   This affects EVERY exported QSO carrying contacted-station coordinates, not
   only the handful with contradictory ones — it is unrelated to the map bug it
   was found beside.

   WHY THE EXISTING SPEC TEST DID NOT CATCH IT, which is the reusable part:
   spec_validation_test.go's fixture hand-supplies Lat: "N032 12.345" — a value
   already in the right format. It proves a correct value passes the spec check
   and never exercises what the PIPELINE produces. A fixture that makes the
   guarded and unguarded paths agree cannot demonstrate the rule it is named
   for; the value under test has to come from where real values come from.

   CORROBORATION from outside (2026-08-04, operator's QRZ logbook page): QRZ
   renders OUR side as "-11.437500 S, 34.041667 E" — our MY_LAT of "S011 26.250"
   parsed correctly — while the contacted station's decimal LAT we uploaded is
   nowhere on the page. Consistent with the format being wrong; not proof of it,
   since QRZ may override coordinates regardless (its profile for that station
   reads "Geo Source: From Grid").

   STORAGE IS DELIBERATELY UNCHANGED. Only the ADIF boundary converts. The SPA
   map parses these fields with parseFloat and falls back to the grid when that
   fails (mapData.svelte.ts says so explicitly about import-era ADIF strings),
   so storing the ADIF form would silently drop every station to cell-centre
   precision — fixing an export bug by breaking the display.

   THE NEAREST CONFUSABLE STATES:
     · C2 — a row that ALREADY holds an ADIF Location string. Import-era rows do
       (the map comment names them), so the conversion has to be idempotent, and
       "already correct" must not be mangled or blanked.
     · C3 — absent coordinates versus the equator. A blanket conversion would
       turn "" into "N000 00.000", inventing a position on the Gulf of Guinea
       for every QSO that never had one.
*/

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// emitted returns the value of an ADIF field in a composed record, and whether
// the field was emitted at all.
func emitted(t *testing.T, out, field string) (string, bool) {
	t.Helper()
	for _, f := range strings.Split(out, "<") {
		name, rest, ok := strings.Cut(f, ":")
		if !ok || !strings.EqualFold(name, field) {
			continue
		}
		_, val, ok := strings.Cut(rest, ">")
		if !ok {
			continue
		}
		return strings.TrimRight(val, " \r\n"), true
	}
	return "", false
}

func composeOne(t *testing.T, st types.ContactedStation) string {
	t.Helper()
	q := types.Qso{}
	q.ContactedStation = st
	out, err := ComposeToAdifString(types.QsoSlice{q})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	return out
}

func TestLatLonFormat_C1_DecimalCoordinatesLeaveAsAdifLocation(t *testing.T) {
	// The value shape enrichment actually stores, straight from QRZ.
	out := composeOne(t, types.ContactedStation{
		Call: "SV2CWV", Gridsquare: "KN10", Lat: "40.609066", Lon: "22.968657",
	})
	lat, ok := emitted(t, out, "LAT")
	if !ok {
		t.Fatal("LAT was not emitted at all")
	}
	if lat == "40.609066" {
		t.Fatalf("LAT left as decimal degrees: %q", lat)
	}
	if lat != "N040 36.544" {
		t.Fatalf("LAT is not the ADIF Location for 40.609066: %q", lat)
	}
	lon, _ := emitted(t, out, "LON")
	if lon != "E022 58.119" {
		t.Fatalf("LON is not the ADIF Location for 22.968657: %q", lon)
	}
}

func TestLatLonFormat_C1b_SouthernAndWesternHemispheresCarryTheirLetter(t *testing.T) {
	// Negative values are where a sign-vs-letter mistake shows; 7Q5MLV's own
	// position is southern AND the test fixture's is western.
	out := composeOne(t, types.ContactedStation{
		Call: "ZS6ABC", Lat: "-11.437500", Lon: "-96.205750",
	})
	lat, _ := emitted(t, out, "LAT")
	lon, _ := emitted(t, out, "LON")
	if !strings.HasPrefix(lat, "S") {
		t.Fatalf("southern latitude lost its hemisphere: %q", lat)
	}
	if !strings.HasPrefix(lon, "W") {
		t.Fatalf("western longitude lost its hemisphere: %q", lon)
	}
	if strings.Contains(lat, "-") || strings.Contains(lon, "-") {
		t.Fatalf("a minus sign survived into an ADIF Location: lat=%q lon=%q", lat, lon)
	}
}

func TestLatLonFormat_C2_AlreadyFormattedCoordinatesPassThroughUntouched(t *testing.T) {
	// Import-era rows hold this shape already. Converting again would fail, and
	// a careless implementation would blank the field rather than leave it.
	out := composeOne(t, types.ContactedStation{
		Call: "G0ABC", Lat: "N051 30.000", Lon: "W000 07.000",
	})
	lat, ok := emitted(t, out, "LAT")
	if !ok {
		t.Fatal("an already-correct LAT was dropped")
	}
	if lat != "N051 30.000" {
		t.Fatalf("an already-correct LAT was altered: %q", lat)
	}
}

func TestLatLonFormat_C3_AbsentCoordinatesStayAbsent(t *testing.T) {
	out := composeOne(t, types.ContactedStation{Call: "G0ABC", Gridsquare: "IO91"})
	if lat, ok := emitted(t, out, "LAT"); ok {
		t.Fatalf("a position was invented for a QSO that had none: LAT=%q", lat)
	}
	if lon, ok := emitted(t, out, "LON"); ok {
		t.Fatalf("a position was invented for a QSO that had none: LON=%q", lon)
	}
}

func TestLatLonFormat_C4_MyLatIsUnaffected(t *testing.T) {
	// MY_LAT was already correct; the fix must not touch it on the way past.
	q := types.Qso{}
	q.ContactedStation = types.ContactedStation{Call: "G0ABC"}
	q.LoggingStation = types.LoggingStation{MyLat: "S011 26.250", MyLon: "E034 02.500"}
	out, err := ComposeToAdifString(types.QsoSlice{q})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if v, _ := emitted(t, out, "MY_LAT"); v != "S011 26.250" {
		t.Fatalf("MY_LAT was altered: %q", v)
	}
}

func TestLatLonFormat_C5_UnconvertibleValuesDoNotBreakTheExport(t *testing.T) {
	// Garbage in these fields must not abort a whole session export. Whatever
	// is there rides out as-is; the QSO still leaves.
	out := composeOne(t, types.ContactedStation{Call: "G0ABC", Lat: "not a number", Lon: "999"})
	if call, _ := emitted(t, out, "CALL"); call != "G0ABC" {
		t.Fatalf("the QSO did not survive an unconvertible coordinate: %q", call)
	}
}

/*
   D1/D2 — the round trip is lossless in SHAPE.

   Introduced by the export fix above and fixed here in the same change. Before
   it, SM exported decimal degrees and re-imported them as decimal degrees:
   wrong on the wire, self-consistent in storage. Converting only the export
   would have left our OWN export→import cycle storing ADIF strings the map
   cannot parse — trading a wire defect for a display one, which is worse
   because nothing shows it happened. "Asymmetric round-trips are a clue."
*/

func TestLatLonFormat_D1_AnAdifLocationImportsAsDecimalDegrees(t *testing.T) {
	rec := Record{}
	rec.ContactedStation = types.ContactedStation{
		Call: "G0ABC", Lat: "N051 30.000", Lon: "W000 07.000",
	}
	q := RecordToQso(rec, 1)
	if q.ContactedStation.Lat != "51.500000" {
		t.Fatalf("LAT was not converted to decimal on import: %q", q.ContactedStation.Lat)
	}
	if q.ContactedStation.Lon != "-0.116667" {
		t.Fatalf("LON was not converted to decimal on import: %q", q.ContactedStation.Lon)
	}
}

func TestLatLonFormat_D2_ExportThenImportPreservesThePosition(t *testing.T) {
	// The property, stated end to end: what enrichment stored is what comes back.
	orig := types.ContactedStation{Call: "SV2CWV", Lat: "40.609066", Lon: "22.968657"}
	rec := QsoToRecord(types.Qso{ContactedStation: orig})
	back := RecordToQso(rec, 1).ContactedStation

	// ADIF Location holds 3 decimal minutes (~1.85 m), so the value returns to
	// that precision rather than bit-identical. Assert the position, not the string.
	if !within(back.Lat, orig.Lat, 0.001) || !within(back.Lon, orig.Lon, 0.001) {
		t.Fatalf("round trip moved the station: (%s,%s) -> (%s,%s)",
			orig.Lat, orig.Lon, back.Lat, back.Lon)
	}
	// And it must come back as a DECIMAL, or the map cannot plot it.
	if strings.ContainsAny(back.Lat, "NSEW") {
		t.Fatalf("an ADIF Location survived into storage: %q", back.Lat)
	}
}

func TestLatLonFormat_D3_ADecimalBearingFileImportsUnchanged(t *testing.T) {
	// Files SM itself wrote before 2026-08-04 carry decimals. They are already
	// in storage shape and must not be touched.
	rec := Record{}
	rec.ContactedStation = types.ContactedStation{Call: "G0ABC", Lat: "40.609066", Lon: "22.968657"}
	q := RecordToQso(rec, 1)
	if q.ContactedStation.Lat != "40.609066" {
		t.Fatalf("a decimal LAT was altered on import: %q", q.ContactedStation.Lat)
	}
}

func within(got, want string, tol float64) bool {
	g, err1 := strconv.ParseFloat(got, 64)
	w, err2 := strconv.ParseFloat(want, 64)
	return err1 == nil && err2 == nil && math.Abs(g-w) <= tol
}

/*
   D4/D5 — MY_LAT / MY_LON cross the same perimeter as LAT / LON.

   Config now stores the operator's own position in decimal (2026-08-04), so the
   field that was previously correct BY ACCIDENT — storage happening to match
   the wire — needs the conversion the contacted station already had. Without
   it, changing storage would have silently started emitting decimals in MY_LAT,
   which is the defect this file exists for, reintroduced from the other side.
*/

func TestLatLonFormat_D4_MyCoordinatesLeaveAsAdifLocation(t *testing.T) {
	q := types.Qso{}
	q.LoggingStation = types.LoggingStation{MyLat: "-11.437500", MyLon: "34.041667"}
	out, err := ComposeToAdifString(types.QsoSlice{q})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	lat, ok := emitted(t, out, "MY_LAT")
	if !ok {
		t.Fatal("MY_LAT was not emitted")
	}
	if lat != "S011 26.250" {
		t.Fatalf("MY_LAT did not leave as an ADIF Location: %q", lat)
	}
	if lon, _ := emitted(t, out, "MY_LON"); lon != "E034 02.500" {
		t.Fatalf("MY_LON did not leave as an ADIF Location: %q", lon)
	}
}

func TestLatLonFormat_D5_MyCoordinatesReturnToDecimalOnImport(t *testing.T) {
	rec := Record{}
	rec.LoggingStation = types.LoggingStation{MyLat: "S011 26.250", MyLon: "E034 02.500"}
	got := RecordToQso(rec, 1).LoggingStation
	if got.MyLat != "-11.437500" || got.MyLon != "34.041667" {
		t.Fatalf("MY_LAT/MY_LON were not returned to storage shape: lat=%q lon=%q",
			got.MyLat, got.MyLon)
	}
}

func TestLatLonFormat_C6_AnUnrenderableCoordinateIsOmittedNotEmittedRaw(t *testing.T) {
	// codex fbaafe73 P1, second half. adifLocation returned the input unchanged
	// on any conversion error, so an out-of-range value left as "<MY_LAT:4>91.0"
	// — not an ADIF Location, and a consumer may reject the whole record.
	// Omitting the field is valid ADIF; emitting nonsense is not. The value is
	// still in storage, so nothing is lost that was not already unusable.
	q := types.Qso{}
	q.LoggingStation = types.LoggingStation{MyLat: "91.0", MyLon: "10.0"}
	q.ContactedStation = types.ContactedStation{Call: "G0ABC", Lat: "not a number", Lon: "5.0"}
	out, err := ComposeToAdifString(types.QsoSlice{q})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if v, ok := emitted(t, out, "MY_LAT"); ok {
		t.Fatalf("an impossible latitude was emitted: %q", v)
	}
	if v, ok := emitted(t, out, "LAT"); ok {
		t.Fatalf("an unrenderable latitude was emitted: %q", v)
	}
	// The QSO itself still leaves — a bad coordinate must not cost the record.
	if call, _ := emitted(t, out, "CALL"); call != "G0ABC" {
		t.Fatalf("the QSO did not survive: %q", call)
	}
}
