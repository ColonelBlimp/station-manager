package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// readBody / readJSONBody delegate to the Server's httpkit.Kit (ADR 0043).
// Kept as Server methods so handler call sites are untouched by the extraction.

func (s *Server) readBody(w http.ResponseWriter, r *http.Request, op errors.Op) ([]byte, bool) {
	return s.kit.ReadBody(w, r, op)
}

func (s *Server) readJSONBody(w http.ResponseWriter, r *http.Request, op errors.Op, dst any) bool {
	return s.kit.ReadJSONBody(w, r, op, dst)
}

// readCommandJSON strictly decodes a state-control command body into dst, whose
// required fields are pointers (a nil pointer means the field was absent). Unknown
// fields and duplicate top-level keys are rejected with a 400 so a client schema typo
// can never be silently absorbed into a default — the AW-2 failure where an omitted
// boolean executed the `false` operation and returned 202. It replaces the lenient
// readJSONBody on command endpoints; the caller rejects any nil required pointer with
// missing_required_field. An empty body decodes as `{}`, leaving those pointers nil.
func (s *Server) readCommandJSON(w http.ResponseWriter, r *http.Request, op errors.Op, dst any) bool {
	body, ok := s.readBody(w, r, op)
	if !ok {
		return false
	}
	if len(body) == 0 {
		body = []byte("{}")
	}
	if !topLevelKeysUnique(body) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "request body has duplicate keys", op)
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "failed to parse request body", op)
		return false
	}
	// Reject trailing content after the first JSON value — a second document or a
	// stray delimiter like `{...}}`. dec.More() is unreliable for this: it returns
	// false when the next byte is a closing `}`/`]`, so decode again and require EOF
	// (codex e9af4f80 P1).
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "request body has trailing content", op)
		return false
	}
	return true
}

// topLevelKeysUnique reports whether a JSON object's top-level keys are all distinct.
// encoding/json silently resolves a duplicated key last-writer-wins; command bodies
// reject that ambiguity (AW-2). Non-objects and malformed input return true — the
// strict decode reports those with its own error.
func topLevelKeysUnique(body []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(body))
	tok, err := dec.Token()
	if err != nil {
		return true
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return true
	}
	seen := make(map[string]struct{})
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return true
		}
		key, ok := keyTok.(string)
		if !ok {
			return true
		}
		// encoding/json matches struct fields case-INSENSITIVELY, so two keys that
		// differ only in case decode into the same field last-writer-wins. Canonicalize
		// each key under the same Unicode simple fold the decoder uses (foldKey) and
		// detect collisions through a map — linear, not the quadratic pairwise scan a
		// large body could abuse — so a differently-cased or fold-equivalent alias like
		// {"armed":false,"Armed":true} or slot_utc/ſlot_utc cannot smuggle an ambiguous
		// or reversed command past this check (codex 63c1c8b9 P1, e9af4f80 / d9d59c7e P2).
		folded := foldKey(key)
		if _, dup := seen[folded]; dup {
			return false
		}
		seen[folded] = struct{}{}
		if err := dec.Decode(new(json.RawMessage)); err != nil {
			return true
		}
	}
	return true
}

// foldKey canonicalizes a JSON object key under Unicode simple case folding — the same
// equivalence encoding/json uses to match struct fields (strings.EqualFold) — by
// mapping each rune to the smallest rune in its fold orbit. Two keys the decoder would
// treat as the same field share a foldKey, so a map keyed by it detects fold-equivalent
// duplicates in linear time. strings.ToLower is not sufficient: it misses folds like
// ſ→s that the decoder honors (codex d9d59c7e P2).
func foldKey(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		lo := r
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			if f < lo {
				lo = f
			}
		}
		b.WriteRune(lo)
	}
	return b.String()
}
