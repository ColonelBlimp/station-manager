package evidencewire

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// DigestV1Hex returns the hex SHA-256 of the version-1 canonical form of
// payload: object keys sorted recursively, no insignificant whitespace,
// number lexemes preserved exactly as submitted (json.Number, never
// float64 — re-formatting floats would make the digest depend on the
// parser rather than the content). Determinism holds because one producer
// (the SMD client marshalling its own rows) emits stable lexemes; the
// server never re-canonicalizes — it stores the digest at first accept
// and compares digests on re-offer (§5.2).
func DigestV1Hex(payload json.RawMessage) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", fmt.Errorf("evidencewire: canonicalize: %w", err)
	}
	// A digest must cover ONE complete document: trailing content would
	// let two different byte streams share a digest.
	if err := ensureEOF(dec); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := writeCanonicalV1(&buf, v); err != nil {
		return "", err
	}
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:]), nil
}

func ensureEOF(dec *json.Decoder) error {
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("evidencewire: canonicalize: trailing content after JSON document")
	}
	return nil
}

func writeCanonicalV1(buf *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return err
			}
			buf.Write(kb)
			buf.WriteByte(':')
			if err := writeCanonicalV1(buf, t[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
		return nil
	case []any:
		buf.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonicalV1(buf, e); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
		return nil
	case json.Number:
		buf.WriteString(t.String()) // the lexeme, verbatim (D3)
		return nil
	default:
		b, err := json.Marshal(t) // string, bool, nil
		if err != nil {
			return err
		}
		buf.Write(b)
		return nil
	}
}
