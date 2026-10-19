package archive

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/cloud/store"
	_ "github.com/lib/pq"
)

// W-0021 Q7 commit 5 (operator ruling 2026-10-19): the round-trip check
// compares a provisioned row's payload with the cloud's as PostgreSQL's jsonb
// does, the equality the cloud's own exact-version guard applies
// (internal/cloud/store/store.go). One table drives both tests:
//
//	JB1 jsonbEqual agrees with the table, without PostgreSQL: numeric
//	    equivalents (1, 1.0, 1e0, -0), distinct integers beyond float64,
//	    large decimals, nested objects, array order and repetition, missing
//	    versus null, type differences, escaped strings, duplicate keys (the
//	    last wins, nested too), key order and whitespace.
//	JB2 malformed input is an error and never equality: a document cut off,
//	    trailing data, a bare word, an invalid escape, a \u0000 escape, an
//	    unpaired surrogate, invalid UTF-8 (all of which jsonb refuses), empty
//	    input, and a number beyond numeric's range: more than 131072 digits
//	    before the point once the exponent moves it (a fractional mantissa
//	    included) or 16383 after (trailing zeros counted), exponents at
//	    int64's extremes and beyond, nested, and hidden by a later duplicate
//	    key. A zero holds any exponent that leaves its scale in range.
//	JB3 the same table, malformed inputs included, through
//	    SELECT $1::jsonb = $2::jsonb on sm-pg: the comparator agrees with
//	    PostgreSQL on every case, and PostgreSQL refuses every input the
//	    comparator calls malformed. It logs the server version and the number
//	    of cases checked, so a run shows it reached PostgreSQL.

var jsonbCases = []struct {
	name  string
	a, b  string
	equal bool
}{
	{"identical scalars", `1`, `1`, true},
	{"1 and 1.0", `1`, `1.0`, true},
	{"1 and 1e0", `1`, `1e0`, true},
	{"1.0 and 1e0", `1.0`, `1e0`, true},
	{"0.1 and 0.10", `0.1`, `0.10`, true},
	{"0 and -0", `0`, `-0`, true},
	{"1e2 and 100", `1e2`, `100`, true},
	{"1E+2 and 100.00", `1E+2`, `100.00`, true},
	{"distinct integers beyond float64", `9007199254740993`, `9007199254740992`, false},
	{"equal integers beyond float64", `9007199254740993`, `9007199254740993`, true},
	{"integers beyond int64", `123456789012345678901234567890`, `123456789012345678901234567891`, false},
	{"large decimals that differ in the last place", `0.12345678901234567890123456789`, `0.12345678901234567890123456788`, false},
	{"large decimals with trailing zeros", `0.12345678901234567890123456789`, `0.123456789012345678901234567890`, true},
	{"a number and its string", `1`, `"1"`, false},
	{"true and 1", `true`, `1`, false},
	{"false and null", `false`, `null`, false},
	{"null and null", `null`, `null`, true},
	{"an empty object and an empty array", `{}`, `[]`, false},
	{"key order", `{"a":1,"b":2}`, `{"b":2,"a":1}`, true},
	{"whitespace", "{ \"a\" : [ 1 , 2 ] }", `{"a":[1,2]}`, true},
	{"nested objects", `{"a":{"b":{"c":[1,{"d":"x"}]}}}`, `{"a":{"b":{"c":[1,{"d":"x"}]}}}`, true},
	{"a nested difference", `{"a":{"b":{"c":[1,{"d":"x"}]}}}`, `{"a":{"b":{"c":[1,{"d":"y"}]}}}`, false},
	{"nested numeric equivalents", `{"a":[1,{"b":2.50}]}`, `{"a":[1.0,{"b":25e-1}]}`, true},
	{"array order", `[1,2]`, `[2,1]`, false},
	{"array repetition", `[1,1]`, `[1]`, false},
	{"a missing key and a null one", `{"a":1}`, `{"a":1,"b":null}`, false},
	{"null and an empty string", `{"a":null}`, `{"a":""}`, false},
	{"a unicode escape and its character", `"\u00e9"`, "\"é\"", true},
	{"an escaped solidus", `"a\/b"`, `"a/b"`, true},
	{"an escaped quote", `"say \"hi\""`, `"say \u0022hi\u0022"`, true},
	{"a surrogate pair and its character", `"\ud83d\ude00"`, "\"😀\"", true},
	{"strings differing in case", `"abc"`, `"ABC"`, false},
	{"duplicate keys: the last wins", `{"a":1,"a":2}`, `{"a":2}`, true},
	{"duplicate keys: the first does not", `{"a":1,"a":2}`, `{"a":1}`, false},
	{"nested duplicate keys", `{"x":{"a":"old","a":"new"}}`, `{"x":{"a":"new"}}`, true},
	{"a duplicate key replaced by null", `{"a":1,"a":null}`, `{"a":null}`, true},
	{"the most digits numeric holds before the point", `1e131071`, `1e131071`, true},
	{"the most digits numeric holds after the point", `1e-16383`, `1e-16383`, true},
	{"a fractional mantissa shifted to the integer limit", `0.1e131072`, `1e131071`, true},
	{"zero, whatever its exponent", `0e131073`, `0`, true},
	{"trailing zeros up to the fractional limit", "1." + strings.Repeat("0", 16383), `1`, true},
	{"zero at the largest exponent PostgreSQL parses", `0e1073741823`, `0`, true},
}

var jsonbMalformed = []struct{ name, doc string }{
	{"cut off", `{"a":1`},
	{"trailing data", `{"a":1} {}`},
	{"a bare word", `nope`},
	{"an invalid escape", `"\x"`},
	{"a NUL escape", `"a\u0000b"`},
	{"empty", ``},
	{"a single quote", `{'a':1}`},
	{"an unpaired high surrogate", `"\ud83d"`},
	{"an unpaired low surrogate", `"\ude00"`},
	{"invalid UTF-8", "\"\xff\""},
	{"too many digits before the point", `1e131072`},
	{"too many digits after the point", `1e-16384`},
	{"a nested number out of range", `{"a":[1e999999999]}`},
	{"a fractional mantissa shifted past the integer limit", `0.1e131073`},
	{"a mantissa shifted past the integer limit", `123.45e131070`},
	{"trailing zeros past the fractional limit", "1." + strings.Repeat("0", 16384)},
	{"zero with too many fractional digits", `0e-20000`},
	{"the largest int64 exponent", `1e9223372036854775807`},
	{"the smallest int64 exponent", `1e-9223372036854775808`},
	{"an exponent beyond int64", `1e9223372036854775808`},
	{"zero one past the largest exponent PostgreSQL parses", `0e1073741824`},
	{"an invalid number overwritten by a duplicate key", `{"a":1e131072,"a":1}`},
	{"a nested invalid number overwritten by a duplicate key", `[{"b":{"a":0.1e131073,"a":2}}]`},
}

func TestJSONBEqual_JB1_TheTable(t *testing.T) {
	for _, c := range jsonbCases {
		t.Run(c.name, func(t *testing.T) {
			for _, order := range [][2]string{{c.a, c.b}, {c.b, c.a}} {
				got, err := jsonbEqual([]byte(order[0]), []byte(order[1]))
				if err != nil || got != c.equal {
					t.Fatalf("jsonbEqual(%s, %s) = %v, %v; want %v", order[0], order[1], got, err, c.equal)
				}
			}
		})
	}
}

func TestJSONBEqual_JB2_MalformedIsNeverEqual(t *testing.T) {
	// The control: a well-formed pair is compared, not refused.
	if got, err := jsonbEqual([]byte(`{"a":1}`), []byte(`{"a":1.0}`)); err != nil || !got {
		t.Fatalf("control: jsonbEqual({\"a\":1}, {\"a\":1.0}) = %v, %v; want equal", got, err)
	}
	for _, c := range jsonbMalformed {
		t.Run(c.name, func(t *testing.T) {
			for _, pair := range [][2]string{{c.doc, c.doc}, {c.doc, `{"a":1}`}, {`{"a":1}`, c.doc}} {
				got, err := jsonbEqual([]byte(pair[0]), []byte(pair[1]))
				if err == nil || got {
					t.Fatalf("jsonbEqual(%q, %q) = %v, %v; want an error and no equality", pair[0], pair[1], got, err)
				}
			}
		})
	}
}

func TestJSONBEqual_JB3_AgreesWithPostgreSQL(t *testing.T) {
	dsn, skip := store.ResolveTestDSN()
	if skip != "" {
		t.Skip(skip)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	var version string
	if err := db.QueryRowContext(ctx, `SHOW server_version`).Scan(&version); err != nil {
		t.Fatalf("PostgreSQL unreachable at the test DSN: %v", err)
	}
	checked := 0
	for _, c := range jsonbCases {
		var pg bool
		if err := db.QueryRowContext(ctx, `SELECT $1::jsonb = $2::jsonb`, c.a, c.b).Scan(&pg); err != nil {
			t.Fatalf("%s: PostgreSQL refused %s / %s: %v", c.name, c.a, c.b, err)
		}
		if pg != c.equal {
			t.Fatalf("%s: the table says %v, PostgreSQL says %v; the table is wrong", c.name, c.equal, pg)
		}
		got, err := jsonbEqual([]byte(c.a), []byte(c.b))
		if err != nil || got != pg {
			t.Fatalf("%s: jsonbEqual = %v, %v; PostgreSQL says %v", c.name, got, err, pg)
		}
		checked++
	}
	for _, c := range jsonbMalformed {
		var pg bool
		if err := db.QueryRowContext(ctx, `SELECT $1::jsonb = $1::jsonb`, c.doc).Scan(&pg); err == nil {
			t.Fatalf("%s: PostgreSQL accepted %q; the comparator must not call it malformed", c.name, c.doc)
		}
		if _, err := jsonbEqual([]byte(c.doc), []byte(c.doc)); err == nil {
			t.Fatalf("%s: jsonbEqual accepted %q, which PostgreSQL refuses", c.name, c.doc)
		}
		checked++
	}
	t.Logf("jsonbEqual agreed with PostgreSQL %s on %d cases", version, checked)
}
