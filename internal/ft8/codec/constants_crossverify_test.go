package codec

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestCrossVerifyFt8Lib parses the canonical ft8_lib constants.c source file
// and compares every value byte-for-byte against our Go constants.
//
// This guards against transcription errors that could otherwise go undetected
// — the SHA-256 tests only prove internal consistency, while this test proves
// equivalence with the authoritative upstream source.
//
// Source: https://github.com/kgoba/ft8_lib — constants.c (MIT license).
func TestCrossVerifyFt8Lib(t *testing.T) {
	raw, err := os.ReadFile("testdata/ft8_constants.c")
	if err != nil {
		t.Skipf("skipping cross-verification: %v (ensure testdata/ft8_constants.c is committed)", err)
	}
	src := string(raw)

	t.Run("Mn_vs_kFTX_LDPC_Mn", func(t *testing.T) {
		// ft8_lib: kFTX_LDPC_Mn[174][3] → our Mn[N][3]uint8 (variable→check)
		rows := extractCRows(t, src, `kFTX_LDPC_Mn\[.*?\]\[3\]\s*=\s*\{(.*?)\};`)
		if len(rows) != N {
			t.Fatalf("kFTX_LDPC_Mn: got %d rows, want %d", len(rows), N)
		}
		for i, row := range rows {
			vals := parseInts(t, row)
			if len(vals) != 3 {
				t.Fatalf("kFTX_LDPC_Mn[%d]: got %d cols, want 3", i, len(vals))
			}
			for j := range 3 {
				if uint8(vals[j]) != Mn[i][j] {
					t.Errorf("Mn[%d][%d]: Go=%d, ft8_lib=%d", i, j, Mn[i][j], vals[j])
				}
			}
		}
	})

	t.Run("Nm_vs_kFTX_LDPC_Nm", func(t *testing.T) {
		// ft8_lib: kFTX_LDPC_Nm[83][7] → our Nm[M][7]uint8 (check→variable)
		rows := extractCRows(t, src, `kFTX_LDPC_Nm\[.*?\]\[7\]\s*=\s*\{(.*?)\};`)
		if len(rows) != M {
			t.Fatalf("kFTX_LDPC_Nm: got %d rows, want %d", len(rows), M)
		}
		for i, row := range rows {
			vals := parseInts(t, row)
			if len(vals) != 7 {
				t.Fatalf("kFTX_LDPC_Nm[%d]: got %d cols, want 7", i, len(vals))
			}
			for j := range 7 {
				if uint8(vals[j]) != Nm[i][j] {
					t.Errorf("Nm[%d][%d]: Go=%d, ft8_lib=%d", i, j, Nm[i][j], vals[j])
				}
			}
		}
	})

	t.Run("NmCount_vs_kFTX_LDPC_Num_rows", func(t *testing.T) {
		// ft8_lib: kFTX_LDPC_Num_rows[83] → our NmCount[M]uint8
		re := regexp.MustCompile(`(?s)kFTX_LDPC_Num_rows\[.*?\]\s*=\s*\{(.*?)\};`)
		m := re.FindStringSubmatch(src)
		if m == nil {
			t.Fatal("kFTX_LDPC_Num_rows not found in C source")
		}
		vals := parseInts(t, m[1])
		if len(vals) != M {
			t.Fatalf("kFTX_LDPC_Num_rows: got %d values, want %d", len(vals), M)
		}
		for i, v := range vals {
			if uint8(v) != NmCount[i] {
				t.Errorf("NmCount[%d]: Go=%d, ft8_lib=%d", i, NmCount[i], v)
			}
		}
	})

	t.Run("G_vs_kFTX_LDPC_generator", func(t *testing.T) {
		// ft8_lib: kFTX_LDPC_generator[83][12] → our G[M][KBytes]byte
		rows := extractCRows(t, src, `kFTX_LDPC_generator\[.*?\]\[.*?\]\s*=\s*\{(.*?)\};`)
		if len(rows) != M {
			t.Fatalf("kFTX_LDPC_generator: got %d rows, want %d", len(rows), M)
		}
		for i, row := range rows {
			vals := parseHex(t, row)
			if len(vals) != KBytes {
				t.Fatalf("kFTX_LDPC_generator[%d]: got %d bytes, want %d", i, len(vals), KBytes)
			}
			for j := range KBytes {
				if byte(vals[j]) != G[i][j] {
					t.Errorf("G[%d][%d]: Go=0x%02x, ft8_lib=0x%02x", i, j, G[i][j], vals[j])
				}
			}
		}
	})
}

// extractCRows finds a C array body matching the regex pattern (which must
// have one capture group for the array contents) and returns the individual
// { ... } row strings.
func extractCRows(t *testing.T, src, pattern string) []string {
	t.Helper()
	re := regexp.MustCompile(`(?s)` + pattern)
	m := re.FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("pattern %q not found in C source", pattern)
	}
	body := m[1]
	rowRe := regexp.MustCompile(`\{([^}]+)\}`)
	matches := rowRe.FindAllStringSubmatch(body, -1)
	rows := make([]string, len(matches))
	for i, rm := range matches {
		rows[i] = rm[1]
	}
	return rows
}

// parseInts splits a comma-separated string of decimal integers.
func parseInts(t *testing.T, s string) []int {
	t.Helper()
	parts := strings.Split(s, ",")
	var vals []int
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.Atoi(p)
		if err != nil {
			t.Fatalf("parseInts: %q: %v", p, err)
		}
		vals = append(vals, v)
	}
	return vals
}

// parseHex splits a comma-separated string of hex byte values (0xNN or 0xNNu).
func parseHex(t *testing.T, s string) []int {
	t.Helper()
	parts := strings.Split(s, ",")
	var vals []int
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.TrimSuffix(p, "u")
		p = strings.TrimSuffix(p, "U")
		if p == "" {
			continue
		}
		v, err := strconv.ParseInt(strings.TrimPrefix(p, "0x"), 16, 64)
		if err != nil {
			t.Fatalf("parseHex: %q: %v", p, err)
		}
		vals = append(vals, int(v))
	}
	return vals
}
