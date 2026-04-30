package cat

import "testing"

// Baseline benchmarks for the CAT codec hot path. See
// docs/v2-design/cat-performance.md for the analysis these measure.
// BenchmarkDecode covers the per-frame allocator pressure (the Status
// map is freshly allocated on every call); BenchmarkLookupState covers
// the prefix-match linear scan that runs unconditionally before any
// marker work.

func benchRig(b *testing.B) RigDefinition {
	b.Helper()
	def, ok := Lookup("yaesu-ftdx10")
	if !ok {
		b.Fatal("rig yaesu-ftdx10 not found in embedded rigDB")
	}
	return def
}

func BenchmarkDecode(b *testing.B) {
	def := benchRig(b)
	line := []byte("FA014250000;")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Decode(def, line)
	}
}

func BenchmarkLookupState(b *testing.B) {
	def := benchRig(b)
	line := []byte("FA014250000;")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = lookupState(line, def.States)
	}
}
