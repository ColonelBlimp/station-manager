package adif

import (
	"bytes"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Parse parses ADIF data provided as bytes and returns a populated Adif struct.
// It is intentionally tolerant: unknown fields are ignored, tags are case-insensitive,
// and only string fields are supported.
func Parse(data []byte) (Adif, error) {
	const op errors.Op = "adif.Parse"
	var res Adif
	if len(data) == 0 {
		return res, errors.New(op).WithMsg("input is empty")
	}

	// Work on a normalized copy for tag detection (case-insensitive markers),
	// but keep the original for value byte slicing.
	lower := strings.ToLower(string(data))

	// Split header/body on <EOH>
	eohIdx := strings.Index(lower, strings.ToLower(EohStr))
	var headerPart, bodyPart []byte
	if eohIdx >= 0 {
		headerPart = data[:eohIdx]
		// Skip the marker itself
		bodyPart = data[eohIdx+len(EohStr):]
		// Parse header fields into HeaderSection
		res.HeaderSection = parseHeader(headerPart)
	} else {
		// No header; entire content considered as body
		bodyPart = data
	}

	res.Records = parseRecords(bodyPart)
	return res, nil
}

func parseHeader(b []byte) HeaderSection {
	// Only care about a handful of fields
	hs := HeaderSection{}
	fields := parseFields(b)
	for tag, vals := range fields {
		if len(vals) == 0 {
			continue
		}
		val := vals[len(vals)-1] // last occurrence wins
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

// parseRecords splits the body by EOR and parses each block into a Record.
// Per ADIF convention this parser is liberal — malformed fields surface as
// empty tags, not errors — so no error return is needed.
//
// Capacity hint of 1 matches the daemon's hot path: POST /v1/qso accepts
// exactly one record per request, so the common case allocates a 1-record
// backing array (~3 KB for the embedded-struct-heavy Record value) rather
// than the prior 64-record default (~190 KB per call). Multi-record
// callers (cmd/importer) pay the doubling-grow cost — bounded O(log N)
// allocations for an N-record file — which is amortised over a rare
// bulk-import operation. Profiling 2026-05-09 found this change alone
// removed ~9 GB of cumulative allocation over a 50k-QSO stress run.
func parseRecords(body []byte) []Record {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}

	// We'll scan the original body for <EOR> in case-insensitive way.

	out := make([]Record, 0, 1)
	start := 0
	for {
		idx := indexOfCaseInsensitive(body[start:], EorStr)
		if idx < 0 {
			// No more <EOR>; also handle trailing block without EOR by ignoring if empty
			blk := bytes.TrimSpace(body[start:])
			if len(blk) > 0 {
				out = append(out, parseRecord(blk))
			}
			break
		}
		end := start + idx
		blk := body[start:end]
		blk = bytes.TrimSpace(blk)
		if len(blk) > 0 {
			out = append(out, parseRecord(blk))
		}
		// Move past the marker
		start = end + len(EorStr)
	}
	return out
}

var fieldRe = regexp.MustCompile(`(?is)<\s*([a-z0-9_]+)\s*:(\d+)(?::[^>]+)?\s*>`)

// parseFields returns a map of tag -> values encountered in the buffer (order kept by later use).
// It reads the exact number of bytes specified by the field length immediately following the tag.
func parseFields(b []byte) map[string][]string {
	m := make(map[string][]string)
	idx := 0
	for idx < len(b) {
		loc := fieldRe.FindSubmatchIndex(b[idx:])
		if loc == nil {
			break
		}
		end := idx + loc[1]
		nameStart, nameEnd := idx+loc[2], idx+loc[3]
		lenStart, lenEnd := idx+loc[4], idx+loc[5]

		tag := strings.ToUpper(string(b[nameStart:nameEnd]))
		n, _ := strconv.Atoi(string(b[lenStart:lenEnd]))
		valStart := end
		valEnd := min(valStart+n, len(b))
		val := string(b[valStart:valEnd])
		m[tag] = append(m[tag], strings.TrimRightFunc(val, unicode.IsSpace))
		idx = valEnd
	}
	return m
}

func parseRecord(b []byte) Record {
	fields := parseFields(b)
	rec := Record{}
	// recordSetters is computed once at package init from the Record
	// type's reflection metadata (see init below). Iterating it here
	// avoids the per-call reflection walk + map allocation + closure
	// captures that the original buildTagSetter did per record. For a
	// 50k-QSO stress run that's ~50k saved map allocations and ~50k×N
	// saved closure allocations where N is the number of tagged
	// fields on Record (~30+ given all the embedded sub-structs).
	rv := reflect.ValueOf(&rec).Elem()
	for _, s := range recordSetters {
		vals, ok := fields[s.tag]
		if !ok {
			continue
		}
		// Last value wins on duplicate tags — same semantic as the
		// previous closure-based loop (which would call set(v) for
		// each value, leaving the last write standing).
		v := vals[len(vals)-1]
		rv.FieldByIndex(s.path).SetString(v)
	}
	return rec
}

// recordSetter pairs an uppercase ADIF tag with the field-index path
// used to reach the corresponding string field on Record (or one of
// its embedded structs). Field-index paths are stable for the lifetime
// of the binary — Record's shape is fixed at compile time — so the
// table can be computed once and reused across every parseRecord call.
type recordSetter struct {
	tag  string
	path []int
}

// recordSetters is the cached tag → field-path table. Populated once
// in init() from Record's reflect.Type and read-only thereafter. The
// table is keyed by tag uppercase so parseRecord's lookup matches the
// uppercase keys parseFields returns.
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
// Tag resolution mirrors the original buildTagSetter:
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

// indexOfCaseInsensitive finds the first occurrence of pat in s, case-insensitive; returns -1 if not found.
func indexOfCaseInsensitive(s []byte, pat string) int {
	lowerS := strings.ToLower(string(s))
	idx := strings.Index(lowerS, strings.ToLower(pat))
	return idx
}
