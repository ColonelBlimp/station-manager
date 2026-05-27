package sandbox

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
)

// loadFixtureSamples reads a fixture WAV and returns its float32 samples.
// Fatal-aborts the bench if the fixture is missing — these benches are
// pinned to the in-tree research/ corpus.
func loadFixtureSamples(b *testing.B, path string) []float32 {
	b.Helper()
	data, err := audio.ReadWAV(path)
	if err != nil {
		b.Fatalf("loadFixtureSamples %q: %v", path, err)
	}
	return data.Samples
}

// BenchmarkPipelineFull_Clean measures the operational per-slot
// decode time on the clean 10cq fixture: Prepare (forward FFT) →
// Spectrogram → FindCandidates → per-candidate refine/extract/BP/
// unpack. The Channelizer object is constructed ONCE outside the
// loop (matching a streaming decoder's reuse pattern); only its
// Prepare runs per iteration.
//
// All 10 truth signals decode via BP in 1 iteration each — no OSD
// load.
func BenchmarkPipelineFull_Clean(b *testing.B) {
	samples := loadFixtureSamples(b, "../10cq_clean.wav")
	benchPipelineFull(b, samples)
}

// BenchmarkPipelineFull_SNR20dB exercises the OSD fallback path: 4
// of the 10 truth signals fail BP and trigger the order-2 OSD
// enumeration (~4187 candidates each). Compared to the clean
// benchmark, the delta tells us the OSD cost contribution.
func BenchmarkPipelineFull_SNR20dB(b *testing.B) {
	samples := loadFixtureSamples(b, "../10cq_snr-20dB.wav")
	benchPipelineFull(b, samples)
}

// BenchmarkPipelineFull_SNR22dB stresses the BP-fail path further:
// most truth signals fail BP and trigger OSD, and most spurious
// candidates also reach OSD. Approximates the worst-case decode
// load.
func BenchmarkPipelineFull_SNR22dB(b *testing.B) {
	samples := loadFixtureSamples(b, "../10cq_snr-22dB.wav")
	benchPipelineFull(b, samples)
}

func benchPipelineFull(b *testing.B, samples []float32) {
	ch, err := NewChannelizer()
	if err != nil {
		b.Fatalf("NewChannelizer: %v", err)
	}
	defer ch.Close()
	rOpts := DefaultRefineOptions()
	bpOpts := DefaultBPOptions()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ch.Prepare(samples)
		spec := Spectrogram(samples)
		cands := FindCandidates(spec, DefaultSearchOptions())
		for _, c := range cands {
			r, err := RefineCandidate(ch, c, rOpts)
			if err != nil {
				continue
			}
			grid, err := ExtractSymbols(ch, r)
			if err != nil {
				continue
			}
			llrs := SoftLLRs(grid)
			br := BPDecode(llrs, bpOpts)
			if br.OK {
				var payload [LDPCPayloadBits]uint8
				copy(payload[:], br.Message91[:LDPCPayloadBits])
				_ = Unpack77(payload)
			}
		}
	}
}

// BenchmarkNewChannelizer measures the one-time setup cost of
// constructing a Channelizer (the 192k-point real-FFT plan
// allocation in PocketFFT). Paid once at decoder startup; reusable
// across all slots thereafter.
func BenchmarkNewChannelizer(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ch, err := NewChannelizer()
		if err != nil {
			b.Fatal(err)
		}
		ch.Close()
	}
}

// BenchmarkChannelizerPrepare measures the single 192k-point real
// FFT that the Channelizer does once per slot. Independent of
// candidate count — runs exactly once per Prepare call.
func BenchmarkChannelizerPrepare(b *testing.B) {
	samples := loadFixtureSamples(b, "../10cq_clean.wav")
	ch, _ := NewChannelizer()
	defer ch.Close()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ch.Prepare(samples)
	}
}

// BenchmarkChannelizerExtract measures one Extract call at the
// default 200 Hz bandwidth. Called 2× per RefineCandidate plus once
// per ExtractSymbols plus once per SNR measurement — typically 3-4
// Extracts per candidate.
func BenchmarkChannelizerExtract(b *testing.B) {
	samples := loadFixtureSamples(b, "../10cq_clean.wav")
	ch, _ := NewChannelizer()
	defer ch.Close()
	_ = ch.Prepare(samples)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = ch.Extract(1500.0, 200.0)
	}
}

// BenchmarkFindCandidates_Clean measures the matched-filter coarse
// scan over the entire spectrogram. Constant per slot — runs once.
func BenchmarkFindCandidates_Clean(b *testing.B) {
	samples := loadFixtureSamples(b, "../10cq_clean.wav")
	spec := Spectrogram(samples)
	opts := DefaultSearchOptions()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = FindCandidates(spec, opts)
	}
}

// BenchmarkRefineCandidate exercises one full refinement: channelize
// at coarse freq → dt sweep → df sweep → re-channelize → final dt
// sweep. Called once per coarse candidate.
func BenchmarkRefineCandidate(b *testing.B) {
	samples := loadFixtureSamples(b, "../10cq_clean.wav")
	ch, _ := NewChannelizer()
	defer ch.Close()
	_ = ch.Prepare(samples)
	spec := Spectrogram(samples)
	cands := FindCandidates(spec, DefaultSearchOptions())
	if len(cands) == 0 {
		b.Fatal("no candidates")
	}
	c0 := cands[0]
	rOpts := DefaultRefineOptions()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = RefineCandidate(ch, c0, rOpts)
	}
}

// BenchmarkExtractSymbols measures the 79 × 32-point complex FFTs
// that build the SymbolGrid for one refined candidate.
func BenchmarkExtractSymbols(b *testing.B) {
	samples := loadFixtureSamples(b, "../10cq_clean.wav")
	ch, _ := NewChannelizer()
	defer ch.Close()
	_ = ch.Prepare(samples)
	spec := Spectrogram(samples)
	cands := FindCandidates(spec, DefaultSearchOptions())
	if len(cands) == 0 {
		b.Fatal("no candidates")
	}
	r, _ := RefineCandidate(ch, cands[0], DefaultRefineOptions())
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = ExtractSymbols(ch, r)
	}
}

// BenchmarkBPDecode_StrongAllZero measures the BP fast path: strong
// LLRs decode on the first syndrome check (1 BP iteration, no OSD).
// Synthetic LLRs avoid loading a fixture.
func BenchmarkBPDecode_StrongAllZero(b *testing.B) {
	var llrs [LDPCCodewordBits]float64
	for i := range llrs {
		llrs[i] = 5.0
	}
	opts := DefaultBPOptions()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = BPDecode(llrs, opts)
	}
}

// BenchmarkOSDDecode_Order2 measures the OSD enumeration cost in
// isolation. Uses a pattern that forces OSD to enumerate all 4187
// candidates before either accepting one or giving up.
func BenchmarkOSDDecode_Order2(b *testing.B) {
	// LLRs structured so BP can't converge (mixed signs, weak
	// magnitudes) — BP fails, OSD runs full enumeration.
	var llrs [LDPCCodewordBits]float64
	for i := range llrs {
		if i%2 == 0 {
			llrs[i] = 0.4
		} else {
			llrs[i] = -0.4
		}
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _, _ = runOSD(llrs, 2)
	}
}
