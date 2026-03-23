package adif

import (
	"bytes"
	"github.com/Station-Manager/errors"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Marshal parses ADIF data provided as bytes and returns a populated Adif struct.
// It is intentionally tolerant: unknown fields are ignored, tags are case-insensitive,
// and only string fields are supported.
func Marshal(data []byte) (Adif, error) {
	const op errors.Op = "adif.Marshal"
	var res Adif
	if len(data) == 0 {
		return res, errors.New(op).Msg("input is empty")
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

	records, err := parseRecords(bodyPart)
	if err != nil {
		return Adif{}, err
	}
	res.Records = records
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
func parseRecords(body []byte) ([]Record, error) {
	var out []Record
	if len(bytes.TrimSpace(body)) == 0 {
		return out, nil
	}

	// We'll scan the original body for <EOR> in case-insensitive way.

	out = make([]Record, 0, 64)
	start := 0
	for {
		idx := indexOfCaseInsensitive(body[start:], EorStr)
		if idx < 0 {
			// No more <EOR>; also handle trailing block without EOR by ignoring if empty
			blk := bytes.TrimSpace(body[start:])
			if len(blk) > 0 {
				rec, err := parseRecord(blk)
				if err == nil {
					out = append(out, rec)
				}
			}
			break
		}
		end := start + idx
		blk := body[start:end]
		blk = bytes.TrimSpace(blk)
		if len(blk) > 0 {
			rec, err := parseRecord(blk)
			if err != nil {
				return nil, err
			}
			out = append(out, rec)
		}
		// Move past the marker
		start = end + len(EorStr)
	}
	return out, nil
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
		// Translate to absolute positions
		off := idx + loc[0]
		end := idx + loc[1]
		nameStart, nameEnd := idx+loc[2], idx+loc[3]
		lenStart, lenEnd := idx+loc[4], idx+loc[5]

		tag := strings.ToUpper(string(b[nameStart:nameEnd]))
		n, _ := strconv.Atoi(string(b[lenStart:lenEnd]))
		valStart := end
		valEnd := valStart + n
		if valEnd > len(b) {
			valEnd = len(b)
		}
		val := string(b[valStart:valEnd])
		m[tag] = append(m[tag], strings.TrimRightFunc(val, unicode.IsSpace))
		idx = valEnd
		_ = off // off unused but computed
	}
	return m
}

func parseRecord(b []byte) (Record, error) {
	fields := parseFields(b)
	rec := Record{}
	// Build a map tag -> setter function on the rec struct using reflection over fields with `adif` tags (or json fallback)
	setter := buildTagSetter(&rec)
	for tag, vals := range fields {
		for _, v := range vals {
			if set := setter[tag]; set != nil {
				set(v)
			}
		}
	}
	return rec, nil
}

// buildTagSetter prepares a map of uppercase tag -> setter(value) that writes into the struct field.
func buildTagSetter(rec *Record) map[string]func(string) {
	m := make(map[string]func(string))
	var walk func(rv reflect.Value)
	walk = func(rv reflect.Value) {
		if rv.Kind() == reflect.Ptr {
			rv = rv.Elem()
		}
		for i := 0; i < rv.NumField(); i++ {
			f := rv.Field(i)
			ft := rv.Type().Field(i)
			if ft.Name == "validate" {
				continue
			}
			if f.Kind() == reflect.Struct {
				walk(f)
				continue
			}
			if f.Kind() != reflect.String || !f.CanSet() {
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
			tag = strings.ToUpper(tag)
			field := f
			m[tag] = func(s string) { field.SetString(s) }
		}
	}
	walk(reflect.ValueOf(rec).Elem())
	return m
}

// indexOfCaseInsensitive finds the first occurrence of pat in s, case-insensitive; returns -1 if not found.
func indexOfCaseInsensitive(s []byte, pat string) int {
	lowerS := strings.ToLower(string(s))
	idx := strings.Index(lowerS, strings.ToLower(pat))
	return idx
}

// splitKeepDelimiter is retained for reference; not used in final flow.
//func splitKeepDelimiter(s, delim string) []string {
//	var result []string
//	start := 0
//	for {
//		idx := strings.Index(s[start:], delim)
//		if idx < 0 {
//			if start < len(s) {
//				result = append(result, s[start:])
//			}
//			break
//		}
//		end := start + idx + len(delim)
//		result = append(result, s[start:end])
//		start = end
//	}
//	return result
//}
