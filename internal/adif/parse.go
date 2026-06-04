package adif

import (
	"bytes"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// Parse parses ADIF data and returns a populated Adif struct. It is
// intentionally tolerant: unknown fields are ignored, tags are
// case-insensitive, and only string fields are supported.
//
// Parsing is a single length-aware forward scan. ADIF field values are
// length-prefixed (`<TAG:n>` then exactly n bytes) and emitted unescaped, so
// a value may legitimately contain the literal text `<EOR>` or `<EOH>` (e.g.
// in a COMMENT, NOTES, or QSLMSG). The scanner skips each value by its
// declared byte length and only recognises `<EOR>` / `<EOH>` as markers when
// it is at a tag boundary — never inside a value. The previous marker-first
// splitter mis-cut such records (review 2026-06-04 H2); this scan cannot. It
// also drops the per-record whole-suffix lowercasing that splitter did, which
// was O(file × records) on bulk imports (M2).
//
// Structure: tokens before `<EOH>` are header fields; tokens after it are
// record fields, with `<EOR>` terminating each record. A file with no `<EOH>`
// has no header — its fields are the body (an `<EOR>` seen before any `<EOH>`
// settles that case). A trailing record without a closing `<EOR>` is still
// emitted. Empty blocks (`<EOR>` with no preceding fields) are skipped.
func Parse(data []byte) (Adif, error) {
	const op errors.Op = "adif.Parse"
	var res Adif
	if len(data) == 0 {
		return res, errors.New(op).WithMsg("input is empty")
	}

	headerFields := map[string][]string{}
	var cur map[string][]string // current record's fields; nil until its first field
	inBody := false

	i, n := 0, len(data)
	for i < n {
		rel := bytes.IndexByte(data[i:], '<')
		if rel < 0 {
			break // no further tags
		}
		i += rel

		// Field header `<TAG:len[:type]>` anchored exactly at i?
		if loc := fieldHeadRe.FindSubmatchIndex(data[i:]); loc != nil {
			tag := strings.ToUpper(string(data[i+loc[2] : i+loc[3]]))
			length, _ := strconv.Atoi(string(data[i+loc[4] : i+loc[5]]))
			valStart := i + loc[1]
			valEnd := valStart + length
			if valEnd > n {
				// Declared length overruns the buffer. Tolerant policy:
				// take what's there (review 2026-06-04 L1 — this is the
				// single point a future strict/warn mode would flag).
				valEnd = n
			}
			val := strings.TrimRightFunc(string(data[valStart:valEnd]), unicode.IsSpace)
			if inBody {
				if cur == nil {
					cur = make(map[string][]string)
				}
				cur[tag] = append(cur[tag], val)
			} else {
				headerFields[tag] = append(headerFields[tag], val)
			}
			i = valEnd
			continue
		}

		// `<EOH>` ends the header — only meaningful before the body starts.
		if !inBody && hasMarkerAt(data, i, EohStr) {
			inBody = true
			i += len(EohStr)
			continue
		}

		// `<EOR>` ends a record.
		if hasMarkerAt(data, i, EorStr) {
			if !inBody {
				// An `<EOR>` before any `<EOH>` means the file has no
				// header — the fields gathered so far are body record #1.
				inBody = true
				cur = headerFields
				headerFields = map[string][]string{}
			}
			if len(cur) > 0 {
				res.Records = append(res.Records, recordFromFields(cur))
			}
			cur = nil
			i += len(EorStr)
			continue
		}

		// Unrecognised `<…>` (or a stray `<`) — skip it and keep scanning.
		i++
	}

	if !inBody {
		// No `<EOH>` and no `<EOR>` at all: the whole input is body with no
		// header (matches the legacy "no header → all body" behaviour). Any
		// accumulated fields form a single trailing record.
		if len(headerFields) > 0 {
			res.Records = append(res.Records, recordFromFields(headerFields))
		}
		return res, nil
	}

	// `<EOH>` was seen: headerFields is the real header; a non-empty cur is a
	// trailing record with no closing `<EOR>`.
	if len(cur) > 0 {
		res.Records = append(res.Records, recordFromFields(cur))
	}
	res.HeaderSection = headerFromFields(headerFields)
	return res, nil
}

// fieldHeadRe matches an ADIF field header `<TAG:len[:type]>` anchored at the
// start of the slice. The leading `^` keeps the scan from running ahead — a
// non-match at the current position returns nil immediately rather than
// hunting for a later field. Submatch 1 is the tag, submatch 2 the declared
// value byte length. `<EOR>` / `<EOH>` carry no `:len`, so they never match
// here and fall through to the marker checks.
var fieldHeadRe = regexp.MustCompile(`(?is)^<\s*([a-z0-9_]+)\s*:(\d+)(?::[^>]+)?\s*>`)

// hasMarkerAt reports whether the case-insensitive marker (e.g. `<EOR>`) sits
// at b[i:]. EqualFold avoids the per-record string allocation the old
// whole-suffix lowercasing incurred (M2).
func hasMarkerAt(b []byte, i int, marker string) bool {
	return i+len(marker) <= len(b) && bytes.EqualFold(b[i:i+len(marker)], []byte(marker))
}

// headerFromFields projects the parsed header field map onto the handful of
// HeaderSection fields the daemon cares about. Last occurrence of a tag wins.
func headerFromFields(fields map[string][]string) HeaderSection {
	hs := HeaderSection{}
	for tag, vals := range fields {
		if len(vals) == 0 {
			continue
		}
		val := vals[len(vals)-1]
		switch strings.ToUpper(tag) {
		case "ADIF_VER":
			hs.ADIFVer = val
		case "CREATED_TIMESTAMP":
			hs.CreatedTimestamp = val
		case "PROGRAMID":
			hs.ProgramID = val
		case "PROGRAMVERSION":
			hs.ProgramVersion = val
		}
	}
	return hs
}

// recordFromFields builds a Record from a parsed tag→values map via the
// cached recordSetters reflection table. Last value wins on duplicate tags.
func recordFromFields(fields map[string][]string) Record {
	rec := Record{}
	rv := reflect.ValueOf(&rec).Elem()
	for _, s := range recordSetters {
		vals, ok := fields[s.tag]
		if !ok {
			continue
		}
		// Last value wins on duplicate tags — same semantic as the previous
		// map-then-set loop.
		v := vals[len(vals)-1]
		rv.FieldByIndex(s.path).SetString(v)
	}
	return rec
}

// recordSetter pairs an uppercase ADIF tag with the field-index path
// used to reach the corresponding string field on Record (or one of
// its embedded structs). Field-index paths are stable for the lifetime
// of the binary — Record's shape is fixed at compile time — so the
// table can be computed once and reused across every parse.
type recordSetter struct {
	tag  string
	path []int
}

// recordSetters is the cached tag → field-path table. Populated once
// in init() from Record's reflect.Type and read-only thereafter. The
// table is keyed by tag uppercase so the lookup matches the uppercase
// keys the scanner stores.
var recordSetters []recordSetter

func init() {
	recordSetters = computeRecordSetters(reflect.TypeOf(Record{}), nil)
}

// computeRecordSetters walks t recursively, collecting an entry for
// every settable string field that carries an `adif` (or JSON-fallback)
// tag. basePath accumulates the field-index path through embedded
// structs so the resulting entry can be applied via FieldByIndex on a
// concrete Record value at parse time.
//
// Tag resolution:
//   - prefer the `adif:"..."` tag, fall back to `json:"..."`,
//   - strip a trailing ",omitempty",
//   - reject empty / "-".
func computeRecordSetters(t reflect.Type, basePath []int) []recordSetter {
	var out []recordSetter
	for i := 0; i < t.NumField(); i++ {
		ft := t.Field(i)
		// FieldByIndex needs a fresh slice per entry — sharing
		// backing storage would cause earlier entries to mutate
		// when later ones append.
		path := make([]int, len(basePath)+1)
		copy(path, basePath)
		path[len(basePath)] = i

		if ft.Type.Kind() == reflect.Struct {
			out = append(out, computeRecordSetters(ft.Type, path)...)
			continue
		}
		if ft.Type.Kind() != reflect.String {
			continue
		}
		tag := ft.Tag.Get("adif")
		if tag == emptyString || tag == "-" {
			tag = ft.Tag.Get(JsonStructTag)
		}
		tag = strings.TrimSuffix(tag, ",omitempty")
		if tag == emptyString || tag == "-" {
			continue
		}
		out = append(out, recordSetter{
			tag:  strings.ToUpper(tag),
			path: path,
		})
	}
	return out
}
