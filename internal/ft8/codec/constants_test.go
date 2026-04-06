package codec

import (
	"crypto/sha256"
	"encoding/hex"
	"math/bits"
	"testing"
)

// TestConstantsDimensions verifies that all arrays have the correct compile-time
// dimensions. These are trivially true due to Go's type system but serve as
// documentation and a guard against accidental constant changes.
func TestConstantsDimensions(t *testing.T) {
	if len(Mn) != N {
		t.Errorf("len(Mn) = %d, want %d", len(Mn), N)
	}
	if len(Mn[0]) != 3 {
		t.Errorf("len(Mn[0]) = %d, want 3", len(Mn[0]))
	}
	if len(Nm) != M {
		t.Errorf("len(Nm) = %d, want %d", len(Nm), M)
	}
	if len(Nm[0]) != 7 {
		t.Errorf("len(Nm[0]) = %d, want 7", len(Nm[0]))
	}
	if len(NmCount) != M {
		t.Errorf("len(NmCount) = %d, want %d", len(NmCount), M)
	}
	if len(G) != M {
		t.Errorf("len(G) = %d, want %d", len(G), M)
	}
	if len(G[0]) != KBytes {
		t.Errorf("len(G[0]) = %d, want %d", len(G[0]), KBytes)
	}
}

// TestMnRange verifies that every entry in Mn is a valid 1-indexed check-node
// ID in the range [1, M].
func TestMnRange(t *testing.T) {
	for v := range N {
		for j := range 3 {
			c := Mn[v][j]
			if c < 1 || c > M {
				t.Errorf("Mn[%d][%d] = %d, want value in [1, %d]", v, j, c, M)
			}
		}
	}
}

// TestNmRangeAndDegree verifies that active entries in Nm are valid 1-indexed
// variable-node IDs in [1, N], that inactive entries are zero, and that the
// active count matches NmCount.
func TestNmRangeAndDegree(t *testing.T) {
	for c := range M {
		deg := int(NmCount[c])
		if deg < 1 || deg > 7 {
			t.Errorf("NmCount[%d] = %d, want value in [1, 7]", c, deg)
			continue
		}
		// Active entries must be in [1, N].
		for j := range deg {
			v := Nm[c][j]
			if v < 1 || v > N {
				t.Errorf("Nm[%d][%d] = %d, want value in [1, %d]", c, j, v, N)
			}
		}
		// Inactive trailing entries must be zero.
		for j := deg; j < 7; j++ {
			if Nm[c][j] != 0 {
				t.Errorf("Nm[%d][%d] = %d, want 0 (inactive)", c, j, Nm[c][j])
			}
		}
	}
}

// TestNmCountValues verifies NmCount entries are either 6 or 7, the two
// possible check-node degrees for this LDPC code.
func TestNmCountValues(t *testing.T) {
	for c := range M {
		d := NmCount[c]
		if d != 6 && d != 7 {
			t.Errorf("NmCount[%d] = %d, want 6 or 7", c, d)
		}
	}
}

// TestBipartiteConsistency verifies that the Mn and Nm arrays describe the
// same Tanner graph. For every edge (v→c) in Mn, check node c must list
// variable node v in Nm, and vice versa.
func TestBipartiteConsistency(t *testing.T) {
	// Build edge set from Mn (variable → check).
	type edge struct{ v, c uint8 }
	mnEdges := make(map[edge]bool)
	for v := range N {
		for j := range 3 {
			c := Mn[v][j]
			mnEdges[edge{uint8(v + 1), c}] = true // store 1-indexed
		}
	}

	// Build edge set from Nm (check → variable).
	nmEdges := make(map[edge]bool)
	for c := range M {
		deg := int(NmCount[c])
		for j := range deg {
			v := Nm[c][j]
			nmEdges[edge{v, uint8(c + 1)}] = true // store 1-indexed
		}
	}

	// Every Mn edge must appear in Nm.
	for e := range mnEdges {
		if !nmEdges[e] {
			t.Errorf("edge (v=%d, c=%d) in Mn but not in Nm", e.v, e.c)
		}
	}

	// Every Nm edge must appear in Mn.
	for e := range nmEdges {
		if !mnEdges[e] {
			t.Errorf("edge (v=%d, c=%d) in Nm but not in Mn", e.v, e.c)
		}
	}

	// Total edge counts must match.
	if len(mnEdges) != len(nmEdges) {
		t.Errorf("edge count mismatch: Mn has %d edges, Nm has %d edges",
			len(mnEdges), len(nmEdges))
	}

	// Sanity: total edges should equal N*3 = 174*3 = 522.
	if len(mnEdges) != N*3 {
		t.Errorf("total edges = %d, want %d (N*3)", len(mnEdges), N*3)
	}
}

// TestGRowWidth verifies that each generator matrix row uses at most K=91 bits.
// Bits 91–95 of the 12th byte (the 5 LSBs of unused space) must be zero.
func TestGRowWidth(t *testing.T) {
	// In each 12-byte (96-bit) row, only bits 0–90 may be set.
	// Bit 91 is in byte 11, position 7 - (91 % 8) = 7 - 3 = 4.
	// So bits 4..0 of byte 11 must be zero → byte 11 & 0x1F == 0.
	for p := range M {
		tail := G[p][KBytes-1] & 0x1F
		if tail != 0 {
			t.Errorf("G[%d]: unused bits set in last byte: 0x%02x & 0x1F = 0x%02x",
				p, G[p][KBytes-1], tail)
		}
	}
}

// TestGKnownParityVectors verifies the GF(2) dot-product logic by computing
// parity bits for two known information vectors and comparing against
// pre-computed expected results. This exercises the generator matrix with
// non-trivial inputs (unlike an all-zero vector, which is a tautological zero).
func TestGKnownParityVectors(t *testing.T) {
	cases := []struct {
		name string
		info [KBytes]byte
		want [M]uint8
	}{
		{
			name: "bit0_set",
			info: [KBytes]byte{0x80},
			want: [M]uint8{
				1, 0, 1, 0, 0, 0, 0, 0, 1, 0, 1, 0, 0, 1, 0, 1,
				0, 0, 0, 0, 1, 0, 0, 0, 1, 1, 0, 1, 1, 0, 0, 0,
				1, 1, 0, 0, 0, 1, 1, 1, 0, 0, 1, 0, 0, 0, 0, 0,
				0, 0, 1, 0, 1, 0, 0, 1, 0, 1, 1, 1, 1, 1, 1, 0,
				1, 0, 0, 0, 0, 1, 1, 1, 0, 1, 0, 1, 0, 1, 1, 1,
				0, 0, 0,
			},
		},
		{
			name: "multi_bit",
			info: [KBytes]byte{0xA5, 0x3C},
			want: [M]uint8{
				0, 1, 0, 1, 1, 0, 1, 1, 1, 0, 1, 1, 1, 0, 0, 0,
				0, 1, 0, 1, 1, 1, 1, 0, 0, 0, 1, 1, 1, 0, 1, 1,
				0, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0,
				0, 0, 0, 0, 1, 1, 1, 0, 1, 0, 0, 0, 0, 1, 1, 0,
				1, 1, 1, 1, 0, 1, 0, 1, 0, 0, 1, 0, 1, 0, 1, 1,
				0, 1, 1,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for p := range M {
				var xor uint8
				for b := range KBytes {
					xor ^= G[p][b] & tc.info[b]
				}
				got := uint8(bits.OnesCount8(xor) % 2)
				if got != tc.want[p] {
					t.Errorf("parity[%d] = %d, want %d", p, got, tc.want[p])
				}
			}
		})
	}
}

// TestEdgeCounts verifies that the sum of NmCount entries equals N*3,
// confirming the bipartite graph has consistent edge counts.
func TestEdgeCounts(t *testing.T) {
	var total int
	for c := range M {
		total += int(NmCount[c])
	}
	want := N * 3 // every variable node has degree 3
	if total != want {
		t.Errorf("sum(NmCount) = %d, want %d", total, want)
	}
}

// TestMnSHA256 guards against accidental edits to the Mn array by comparing
// a SHA-256 digest over the serialised bytes.
func TestMnSHA256(t *testing.T) {
	h := sha256.New()
	for v := range N {
		h.Write(Mn[v][:])
	}
	got := hex.EncodeToString(h.Sum(nil))
	const want = "385a395a70901ac54d7787a5e1dcde932a752f5e3d902a98c8016f03312b6553"
	if got != want {
		t.Errorf("Mn SHA-256 = %s, want %s", got, want)
	}
}

// TestNmSHA256 guards against accidental edits to the Nm array.
func TestNmSHA256(t *testing.T) {
	h := sha256.New()
	for c := range M {
		h.Write(Nm[c][:])
	}
	got := hex.EncodeToString(h.Sum(nil))
	const want = "55216b497afa4423959fe9cfee72ae49b0865c76ae6859b2e648519ade25028b"
	if got != want {
		t.Errorf("Nm SHA-256 = %s, want %s", got, want)
	}
}

// TestNmCountSHA256 guards against accidental edits to the NmCount array.
func TestNmCountSHA256(t *testing.T) {
	h := sha256.New()
	h.Write(NmCount[:])
	got := hex.EncodeToString(h.Sum(nil))
	const want = "5ba9a9d6fe820ff8352f3b4da3e1dcf460569c42f1931d2f630ee60f8cba6022"
	if got != want {
		t.Errorf("NmCount SHA-256 = %s, want %s", got, want)
	}
}

// TestGSHA256 guards against accidental edits to the generator matrix G.
func TestGSHA256(t *testing.T) {
	h := sha256.New()
	for p := range M {
		h.Write(G[p][:])
	}
	got := hex.EncodeToString(h.Sum(nil))
	const want = "e2ab9e8671804a168fec0eb8b81da28d4b55117f98dc4cc2ff57e5e9e5f07cec"
	if got != want {
		t.Errorf("G SHA-256 = %s, want %s", got, want)
	}
}

// TestConstants verifies the fundamental LDPC parameter relationships.
func TestConstants(t *testing.T) {
	if N != K+M {
		t.Errorf("N=%d != K+M=%d+%d=%d", N, K, M, K+M)
	}
	if NBytes != 22 {
		t.Errorf("NBytes = %d, want 22", NBytes)
	}
	if KBytes != 12 {
		t.Errorf("KBytes = %d, want 12", KBytes)
	}
}

// TestMnNoDuplicates verifies that no variable node lists the same check node
// twice.
func TestMnNoDuplicates(t *testing.T) {
	for v := range N {
		seen := make(map[uint8]bool, 3)
		for j := range 3 {
			c := Mn[v][j]
			if seen[c] {
				t.Errorf("Mn[%d]: duplicate check node %d", v, c)
			}
			seen[c] = true
		}
	}
}

// TestNmNoDuplicates verifies that no check node lists the same variable node
// twice.
func TestNmNoDuplicates(t *testing.T) {
	for c := range M {
		deg := int(NmCount[c])
		seen := make(map[uint8]bool, deg)
		for j := range deg {
			v := Nm[c][j]
			if seen[v] {
				t.Errorf("Nm[%d]: duplicate variable node %d", c, v)
			}
			seen[v] = true
		}
	}
}

// TestGParityCheckConsistency performs the strongest validation: for the
// generator matrix G, every single-bit information vector must produce a
// codeword that satisfies all 83 parity checks using the Nm/NmCount tables.
//
// This confirms that G and Nm/NmCount describe the same LDPC code.
func TestGParityCheckConsistency(t *testing.T) {
	// For each information bit position k (0..90), set only that bit,
	// compute the 83 parity bits via G, build the 174-bit codeword,
	// and verify H × codeword = 0 using Nm.
	for k := range K {
		// Build the info vector with only bit k set.
		var info [KBytes]byte
		byteIdx := k / 8
		bitIdx := uint(7 - k%8)
		info[byteIdx] = 1 << bitIdx

		// Compute 83 parity bits using G.
		var parity [M]uint8
		for p := range M {
			var xor uint8
			for b := range KBytes {
				xor ^= G[p][b] & info[b]
			}
			parity[p] = uint8(bits.OnesCount8(xor) % 2)
		}

		// Build the full 174-bit codeword (as an array of bits).
		var cw [N]uint8
		for i := range K {
			cw[i] = uint8((info[i/8] >> uint(7-i%8)) & 1)
		}
		for p := range M {
			cw[K+p] = parity[p]
		}

		// Verify all 83 parity checks pass: for each check node c,
		// XOR of all variable nodes in Nm[c] must be zero.
		for c := range M {
			deg := int(NmCount[c])
			var check uint8
			for j := range deg {
				v := int(Nm[c][j]) - 1 // convert to 0-indexed
				check ^= cw[v]
			}
			if check != 0 {
				t.Errorf("parity check %d failed for single info bit %d", c, k)
			}
		}
	}
}
