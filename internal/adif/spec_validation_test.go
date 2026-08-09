package adif

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// This file is the guard against shipping an ADIF field whose value is
// invalid for that field's ADIF data type — the class of bug that made
// QRZ file QSOs as NON-DXCC when the emitter put the alphabetic DXCC
// *prefix* ("K") into the numeric DXCC *entity* field.
//
// Three things lock it down:
//
//   - TestAdifSpec_EveryEmittedFieldClassified reflects over every field
//     the emitter can produce and fails if any lacks a declared spec
//     type — so a newly-added ADIF field can't ship un-vetted.
//   - TestAdifSpec_FullyPopulatedQsoEmitsValidValues runs a realistic,
//     fully-populated QSO through the real emit path and validates every
//     emitted value against its field's ADIF type.
//   - TestAdifSpec_ValidatorsRejectBadValues proves the validators have
//     teeth (so the round-trip test can't pass vacuously).

// adifType is the ADIF data-type class enforced on an emitted field value.
// Only the machine-checkable types are validated strictly; free-text
// String and the membership of Enumeration fields are validated loosely
// (non-empty / unpadded) because their correctness is a content concern,
// not the wrong-data-type concern this guard exists for. The strict types
// are exactly the ones a wrong-typed value silently corrupts at an
// uploader: integers, numbers, dates, times, grids, locations, booleans.
type adifType int

const (
	typString   adifType = iota // free text — any non-empty value is valid
	typEnum                     // enumeration — non-empty, unpadded (membership enforced elsewhere)
	typInt                      // ADIF PositiveInteger / Integer — ASCII digits
	typNumber                   // ADIF Number — optional sign, optional single decimal
	typBoolean                  // ADIF Boolean — "Y" or "N"
	typDate                     // ADIF Date — YYYYMMDD, a real calendar date
	typTime                     // ADIF Time — HHMM or HHMMSS
	typGrid                     // ADIF GridSquare — 2/4/6/8 chars, Maidenhead shape
	typLocation                 // ADIF Location — XDDD MM.MMM
)

var (
	reNumber   = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
	reGrid     = regexp.MustCompile(`^[A-Ra-r]{2}(\d{2}([A-Xa-x]{2}(\d{2})?)?)?$`)
	reLocation = regexp.MustCompile(`^[NSEWnsew]\d{3} \d{2}\.\d{3}$`)
)

func (t adifType) validate(v string) error {
	switch t {
	case typString:
		return nil
	case typEnum:
		if v == emptyString || v != strings.TrimSpace(v) {
			return errf("enumeration must be non-empty and unpadded")
		}
		return nil
	case typInt:
		if !isAsciiDigits(v) {
			return errf("not a positive integer")
		}
		return nil
	case typNumber:
		if !reNumber.MatchString(v) {
			return errf("not an ADIF Number")
		}
		return nil
	case typBoolean:
		if v != "Y" && v != "N" {
			return errf("not an ADIF Boolean (Y/N)")
		}
		return nil
	case typDate:
		if len(v) != 8 || !isAsciiDigits(v) {
			return errf("not 8-digit YYYYMMDD")
		}
		if _, err := time.Parse("20060102", v); err != nil {
			return errf("not a valid calendar date")
		}
		return nil
	case typTime:
		if len(v) != 4 && len(v) != 6 {
			return errf("not HHMM or HHMMSS length")
		}
		if !isAsciiDigits(v) {
			return errf("not all digits")
		}
		layout := "1504"
		if len(v) == 6 {
			layout = "150405"
		}
		if _, err := time.Parse(layout, v); err != nil {
			return errf("not a valid time")
		}
		return nil
	case typGrid:
		if n := len(v); n != 2 && n != 4 && n != 6 && n != 8 {
			return errf("grid length must be 2/4/6/8")
		}
		if !reGrid.MatchString(v) {
			return errf("malformed Maidenhead grid square")
		}
		return nil
	case typLocation:
		if !reLocation.MatchString(v) {
			return errf("not an ADIF Location (XDDD MM.MMM)")
		}
		return nil
	}
	return errf("unhandled adif type")
}

// specField is the ADIF data type for every field the emitter can produce.
// Keyed by the uppercase ADIF field name. EVERY field name reachable by the
// emitter MUST appear here — the classification test enforces it — so adding
// a new ADIF field forces a conscious decision about its type. APP_* and the
// SM_* user-defined stamps are String: their type is declared by USERDEF /
// the APP_ convention, not the core ADIF field-type table.
var specField = map[string]adifType{
	// QsoDetails
	"A_INDEX":      typNumber,
	"ANT_PATH":     typEnum,
	"BAND":         typEnum,
	"BAND_RX":      typEnum,
	"COMMENT":      typString,
	"CONTEST_ID":   typString,
	"CLASS":        typString, // Field Day class, e.g. "2A"
	"ARRL_SECT":    typString, // ARRL/RAC section, e.g. "EMA" / "DX"
	"DISTANCE":     typNumber,
	"FREQ":         typNumber,
	"FREQ_RX":      typNumber,
	"MODE":         typEnum,
	"SUBMODE":      typEnum,
	"NOTES":        typString,
	"QSO_DATE":     typDate,
	"QSO_DATE_OFF": typDate,
	"QSO_RANDOM":   typBoolean,
	"QSO_COMPLETE": typEnum,
	"RST_RCVD":     typString,
	"RST_SENT":     typString,
	"RX_PWR":       typNumber,
	"SRX":          typInt,
	"STX":          typInt,
	"TIME_OFF":     typTime,
	"TIME_ON":      typTime,
	"TX_PWR":       typNumber,
	"RIG":          typString,

	// ContactedStation
	"ADDRESS":        typString,
	"AGE":            typNumber,
	"ALTITUDE":       typNumber,
	"CALL":           typString,
	"CONT":           typEnum,
	"CONTACTED_OP":   typString,
	"COUNTRY":        typString,
	"CQZ":            typInt,
	"DXCC":           typInt,
	"EMAIL":          typString,
	"EQ_CALL":        typString,
	"GRIDSQUARE":     typGrid,
	"IOTA":           typString,
	"IOTA_ISLAND_ID": typInt,
	"ITUZ":           typInt,
	"LAT":            typLocation,
	"LON":            typLocation,
	"NAME":           typString,
	"QTH":            typString,
	"SIG":            typString,
	"SIG_INFO":       typString,
	"WEB":            typString,
	"WWFF_REF":       typString,

	// LoggingStation
	"ANT_AZ":            typNumber,
	"MY_ALTITUDE":       typNumber,
	"MY_ANTENNA":        typString,
	"MY_CITY":           typString,
	"MY_COUNTRY":        typString,
	"MY_CQ_ZONE":        typInt,
	"MY_DXCC":           typInt,
	"MY_GRIDSQUARE":     typGrid,
	"MY_IOTA":           typString,
	"MY_IOTA_ISLAND_ID": typInt,
	"MY_ITU_ZONE":       typInt,
	"MY_LAT":            typLocation,
	"MY_LON":            typLocation,
	"MY_MORSE_KEY_INFO": typString,
	"MY_MORSE_KEY_TYPE": typString,
	"MY_NAME":           typString,
	"MY_POSTAL_CODE":    typString,
	"MY_RIG":            typString,
	"MY_SIG":            typString,
	"MY_SIG_INFO":       typString,
	"MY_STREET":         typString,
	"MY_WWFF_REF":       typString,
	"OPERATOR":          typString,
	"OWNER_CALLSIGN":    typString,
	"STATION_CALLSIGN":  typString,

	// QslSection
	"QSLMSG":                     typString,
	"QSLMSG_INTL":                typString,
	"QSLMSG_RCVD":                typString,
	"QSLRDATE":                   typDate,
	"QSLSDATE":                   typDate,
	"QSL_RCVD":                   typEnum,
	"QSL_RCVD_VIA":               typEnum,
	"QSL_RCVD_NOTES":             typString,
	"QSL_SENT":                   typEnum,
	"QSL_SENT_VIA":               typEnum,
	"QSL_VIA":                    typString,
	"QRZCOM_QSO_DOWNLOAD_DATE":   typDate,
	"QRZCOM_QSO_DOWNLOAD_STATUS": typEnum,
	"QRZCOM_QSO_UPLOAD_DATE":     typDate,
	"QRZCOM_QSO_UPLOAD_STATUS":   typEnum,
	"CLUBLOG_QSO_UPLOAD_DATE":    typDate,
	"CLUBLOG_QSO_UPLOAD_STATUS":  typEnum,

	// UserDef (type declared by USERDEF, not the core spec)
	"SM_QSO_UPLOAD_DATE":      typString,
	"SM_QSO_UPLOAD_STATUS":    typBoolean,
	"SM_FWRD_BY_EMAIL_DATE":   typString,
	"SM_FWRD_BY_EMAIL_STATUS": typBoolean,
	"QSL_WANTED":              typBoolean,

	// APP_ extension fields (type by APP_ convention)
	"APP_SM_QSO_ID":      typString,
	"APP_SM_RUN_ID":      typString,
	"APP_SM_REQUEST_QSL": typBoolean,
	"APP_QRZLOG_LOGID":   typString,
}

// emittedFieldNames mirrors parseToAdifString's tag resolution (prefer the
// `adif` tag, fall back to `json`) to enumerate every ADIF field name the
// Record emitter can produce. Only string leaves are emittable; embedded
// structs are walked, time.Time is skipped (it has no emittable tags).
func emittedFieldNames() []string {
	seen := map[string]bool{}
	var out []string
	timeType := reflect.TypeOf(time.Time{})

	var walk func(rt reflect.Type)
	walk = func(rt reflect.Type) {
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			ft := f.Type
			if ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}
			if ft == timeType {
				continue
			}
			if ft.Kind() == reflect.Struct {
				walk(ft)
				continue
			}
			if ft.Kind() != reflect.String {
				continue
			}
			tag := f.Tag.Get("adif")
			if tag == emptyString || tag == "-" {
				tag = f.Tag.Get(JsonStructTag)
			}
			tag = strings.TrimSuffix(tag, ",omitempty")
			if tag == emptyString || tag == "-" {
				continue
			}
			name := strings.ToUpper(tag)
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	walk(reflect.TypeOf(Record{}))
	return out
}

type emittedField struct {
	name  string
	value string
}

// parseEmittedFields tokenises raw emitted ADIF (`<NAME:LEN>VALUE`) into
// name/value pairs, using the declared length to slice the value exactly
// (so values containing spaces or newlines are read faithfully). EOH/EOR
// markers are skipped.
func parseEmittedFields(t *testing.T, adifStr string) []emittedField {
	t.Helper()
	var out []emittedField
	s := adifStr
	for {
		open := strings.IndexByte(s, '<')
		if open < 0 {
			break
		}
		closeIdx := strings.IndexByte(s[open:], '>')
		if closeIdx < 0 {
			break
		}
		closeIdx += open
		tag := s[open+1 : closeIdx]
		rest := s[closeIdx+1:]
		if strings.EqualFold(tag, "EOR") || strings.EqualFold(tag, "EOH") {
			s = rest
			continue
		}
		parts := strings.Split(tag, ":")
		if len(parts) < 2 {
			s = rest
			continue
		}
		n, err := strconv.Atoi(parts[1])
		if err != nil || n > len(rest) {
			s = rest
			continue
		}
		out = append(out, emittedField{name: strings.ToUpper(parts[0]), value: rest[:n]})
		s = rest[n:]
	}
	return out
}

func TestAdifSpec_EveryEmittedFieldClassified(t *testing.T) {
	for _, name := range emittedFieldNames() {
		if _, ok := specField[name]; !ok {
			t.Errorf("ADIF field %q is emittable but has no spec-type classification.\n"+
				"Add it to specField in spec_validation_test.go so its emitted values are "+
				"validated against the ADIF data-type spec.", name)
		}
	}
}

// fullyPopulatedQso sets every emittable field to a value that is VALID for
// its ADIF type, so the round-trip test exercises the broadest possible
// surface. FREQ values are in Hz (the emitter converts to MHz).
func fullyPopulatedQso() types.Qso {
	q := types.Qso{
		UUID:                "019ec425-be6b-7f55-8d5e-243cfa8e9ce2",
		AppSmRequestQsl:     true,
		QrzComUploadDate:    "20260614",
		QrzComUploadStatus:  "Y",
		SmQsoUploadDate:     "20260614",
		SmQsoUploadStatus:   "Y",
		SmFwrdByEmailDate:   "20260614",
		SmFwrdByEmailStatus: "Y",
	}
	q.QsoDetails = types.QsoDetails{
		AIndex: "5", AntPath: "S", Band: "17m", BandRx: "17m", Comment: "Nice QSO",
		ContestId: "CQ-WW", Distance: "1287", Freq: "18140000", FreqRx: "18140000",
		Mode: "SSB", Submode: "USB", Notes: "operator notes", QsoDate: "20260614",
		QsoDateOff: "20260614", QsoRandom: "Y", QsoComplete: "Y", RstRcvd: "55",
		RstSent: "52", RxPwr: "100", SRX: "1", STX: "2", TimeOff: "0321",
		TimeOn: "0317", TxPwr: "45", Rig: "FTdx10",
	}
	q.ContactedStation = types.ContactedStation{
		Address: "123 Main St", Age: "45", Altitude: "100", Call: "W5IB", Cont: "NA",
		ContactedOp: "W5IB", Country: "United States", CQZ: "5", DXCC: "291",
		Email: "jay@example.com", EqCall: "W5IB", Gridsquare: "EM12xi", Iota: "NA-001",
		IotaIslandId: "123", ITUZ: "8", Lat: "N032 12.345", Lon: "W096 12.345",
		Name: "Jay", QTH: "Eustace, TX", Sig: "POTA", SigInfo: "K-1234",
		Web: "https://example.com", WwffRef: "KFF-1234",
	}
	q.LoggingStation = types.LoggingStation{
		AntennaAzimuth: "302.8", MyAltitude: "1311", MyAntenna: "Hex Beam", MyCity: "Mzuzu",
		MyCountry: "Malawi", MyCqZone: "37", MyDXCC: "440", MyGridsquare: "KH78an",
		MyIota: "AF-001", MyIotaIslandID: "456", MyITUZone: "53", MyLat: "S011 26.250",
		MyLon: "E034 02.500", MyMorseKeyInfo: "Begali Sculpture", MyMorseKeyType: "Twin Paddle",
		MyName: "Marc", MyPostalCode: "12345", MyRig: "Yaesu FTdx10", MySig: "POTA",
		MySigInfo: "info", MyStreet: "P.O. Box 806", MyWwffRef: "ref",
		Operator: "7Q5MLV", OwnerCallsign: "7Q5MLV", StationCallsign: "7Q5MLV",
	}
	q.Qsl = types.Qsl{
		QslMsg: "Thanks for the QSO", QslMsgRcvd: "TNX QSO 73", QslRDate: "20260614",
		QslSDate: "20260614", QslRcvd: "Y", QslRcvdVia: "B", QslRcvdNotes: "card filed",
		QslSent: "N", QslSendVia: "B", QslVia: "via the bureau",
	}
	return q
}

func TestAdifSpec_FullyPopulatedQsoEmitsValidValues(t *testing.T) {
	out := ConvertQsoToAdifNoHeader(fullyPopulatedQso())
	fields := parseEmittedFields(t, out)

	// Guard against a silently-empty fixture: a real QSO emits many fields.
	if len(fields) < 50 {
		t.Fatalf("expected the fully-populated QSO to emit a broad field set, got %d:\n%s", len(fields), out)
	}

	for _, f := range fields {
		typ, ok := specField[f.name]
		if !ok {
			t.Errorf("emitted field %q has no spec classification", f.name)
			continue
		}
		if err := typ.validate(f.value); err != nil {
			t.Errorf("field %s=%q is invalid for its ADIF type: %v", f.name, f.value, err)
		}
	}
}

// TestAdifSpec_RejectsNonNumericDxcc is the direct regression for the
// NON-DXCC bug: a non-numeric DXCC (a prefix that leaked into the numeric
// entity field) must never be emitted.
func TestAdifSpec_RejectsNonNumericDxcc(t *testing.T) {
	q := fullyPopulatedQso()
	q.ContactedStation.DXCC = "K" // hamnut prefix — not a numeric entity code
	out := ConvertQsoToAdifNoHeader(q)
	if strings.Contains(strings.ToUpper(out), "<DXCC:") {
		t.Errorf("emitter produced a DXCC field for non-numeric value %q; QRZ would file it NON-DXCC.\n%s",
			q.ContactedStation.DXCC, out)
	}
}

// TestAdifSpec_ValidatorsRejectBadValues proves the type validators have
// teeth — otherwise the round-trip test could pass vacuously.
func TestAdifSpec_ValidatorsRejectBadValues(t *testing.T) {
	bad := []struct {
		typ adifType
		val string
	}{
		{typInt, "K"},
		{typInt, "29.1"},
		{typInt, ""},
		{typNumber, "abc"},
		{typNumber, "14.0.7"},
		{typBoolean, "y"},
		{typBoolean, "true"},
		{typDate, "2026-06-14"},
		{typDate, "20261344"}, // month 13, day 44
		{typTime, "317"},
		{typTime, "2461"}, // hour 24, minute 61
		{typGrid, "ZZ99"}, // letters out of A-R range
		{typGrid, "EM1"},  // odd length
		{typLocation, "32.123"},
		{typLocation, "N32 12.345"}, // degrees not 3 digits
	}
	for _, b := range bad {
		if err := b.typ.validate(b.val); err == nil {
			t.Errorf("validator for type %d accepted invalid value %q", b.typ, b.val)
		}
	}
}

// errf is a tiny local error constructor so the validator stays free of an
// fmt import at the top (keeps the type switch terse).
func errf(msg string) error { return validationErr(msg) }

type validationErr string

func (e validationErr) Error() string { return string(e) }
