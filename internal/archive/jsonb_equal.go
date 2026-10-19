package archive

import (
	"bytes"
	"encoding/json"
	stderr "errors"
	"io"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
)

// PostgreSQL's numeric holds at most 131072 digits before the decimal point
// and 16383 after; jsonb refuses a number beyond either. Refusing it here too
// keeps a hostile exponent from costing big.Rat memory.
const (
	maxNumericIntDigits  = 131072
	maxNumericFracDigits = 16383
)

// jsonbEqual reports whether a and b are equal as PostgreSQL jsonb values: the
// equality the cloud's exact-version guard applies to a payload
// (internal/cloud/store/store.go). Key order and whitespace do not matter; a
// repeated key keeps its last value; numbers compare by exact decimal value
// (1, 1.0 and 1e0 are equal; integers beyond float64 stay distinct); strings
// compare after unescaping; arrays keep order and repetition; a missing key
// differs from a null one. Input jsonb refuses (invalid JSON or UTF-8, data
// after the document, a \u0000 escape, an unpaired surrogate) is an error,
// never equality. The error names no value from either document.
func jsonbEqual(a, b []byte) (bool, error) {
	va, err := parseJSONB(a)
	if err != nil {
		return false, err
	}
	vb, err := parseJSONB(b)
	if err != nil {
		return false, err
	}
	return jsonbValuesEqual(va, vb), nil
}

func parseJSONB(doc []byte) (any, error) {
	if !utf8.Valid(doc) {
		return nil, stderr.New("the document is not valid UTF-8")
	}
	if err := checkJSONBEscapes(doc); err != nil {
		return nil, err
	}
	// Every number token is checked before decoding collapses a repeated key:
	// jsonb refuses a document whose overwritten value is out of range too.
	if err := checkJSONBNumbers(doc); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, stderr.New("the document is not valid JSON")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, stderr.New("the document has data after its value")
	}
	return v, nil
}

// checkJSONBEscapes refuses the \u escapes jsonb refuses and Go's decoder
// would accept: \u0000, and a surrogate that is not a high-low pair. Other
// malformed escapes are left to the decoder.
func checkJSONBEscapes(doc []byte) error {
	inString := false
	for i := 0; i < len(doc); i++ {
		c := doc[i]
		if !inString {
			if c == '"' {
				inString = true
			}
			continue
		}
		switch c {
		case '"':
			inString = false
		case '\\':
			if i+1 >= len(doc) || doc[i+1] != 'u' {
				i++ // a one-character escape; the decoder judges it
				continue
			}
			code, ok := hexEscape(doc, i)
			if !ok {
				return nil // malformed: the decoder refuses it
			}
			switch {
			case code == 0:
				return stderr.New("the document contains a \\u0000 escape")
			case code >= 0xDC00 && code <= 0xDFFF:
				return stderr.New("the document contains an unpaired low surrogate")
			case code >= 0xD800 && code <= 0xDBFF:
				low, ok := hexEscape(doc, i+6)
				if !ok || low < 0xDC00 || low > 0xDFFF {
					return stderr.New("the document contains an unpaired high surrogate")
				}
				i += 11
			default:
				i += 5
			}
		}
	}
	return nil
}

// hexEscape reads the \uXXXX escape at doc[i].
func hexEscape(doc []byte, i int) (rune, bool) {
	if i+6 > len(doc) || doc[i] != '\\' || doc[i+1] != 'u' {
		return 0, false
	}
	n, err := strconv.ParseUint(string(doc[i+2:i+6]), 16, 32)
	return rune(n), err == nil
}

// checkJSONBNumbers refuses a document holding any number beyond numeric's
// range, reading every token, so a value a later duplicate key replaces is
// checked as well. Structural errors are left to the decoder.
func checkJSONBNumbers(doc []byte) error {
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil
		}
		if n, ok := tok.(json.Number); ok && !numericHolds(n.String()) {
			return stderr.New("the document contains a number beyond numeric's range")
		}
	}
}

// maxShift is the largest exponent magnitude PostgreSQL parses (2^30 - 1:
// 0e1073741823 is zero, 0e1073741824 overflows). Bounding the exponent before
// any arithmetic also keeps that arithmetic from overflowing.
const maxShift = 1<<30 - 1

// numericHolds reports whether a JSON number fits numeric. Its digits, read
// as one run with the decimal point after the integer part and moved by the
// exponent, may put at most maxNumericIntDigits significant digits before the
// point (none for zero) and at most maxNumericFracDigits after it, trailing
// zeros counted.
func numericHolds(n string) bool {
	n = strings.TrimPrefix(strings.ToLower(n), "-")
	mantissa, exp := n, int64(0)
	if i := strings.IndexByte(n, 'e'); i >= 0 {
		e, err := strconv.ParseInt(strings.TrimPrefix(n[i+1:], "+"), 10, 64)
		if err != nil || e > maxShift || e < -maxShift {
			return false
		}
		mantissa, exp = n[:i], e
	}
	intPart, fracPart, _ := strings.Cut(mantissa, ".")
	digits := intPart + fracPart
	point := int64(len(intPart)) + exp // the point's position within digits
	if significant := strings.TrimLeft(digits, "0"); significant != "" {
		leading := int64(len(digits) - len(significant))
		if point-leading > maxNumericIntDigits {
			return false
		}
	}
	return int64(len(digits))-point <= maxNumericFracDigits
}

func jsonbValuesEqual(a, b any) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	case json.Number:
		y, ok := b.(json.Number)
		if !ok {
			return false
		}
		rx, okx := new(big.Rat).SetString(x.String())
		ry, oky := new(big.Rat).SetString(y.String())
		return okx && oky && rx.Cmp(ry) == 0
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !jsonbValuesEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, vx := range x {
			vy, ok := y[k]
			if !ok || !jsonbValuesEqual(vx, vy) {
				return false
			}
		}
		return true
	}
	return false
}
