// decode-eval runs the full research-tree FT8 pipeline
// (candidates → demod → LLRs → ldpc.Decode) against every .wav file
// directly under the supplied directory and reports decode-parity
// vs the corresponding ground-truth manifest. The "decoded" filter
// is CRC14 — only candidates whose 91-bit info word passes CRC14
// over the 77 payload bits count as decodes.
//
// For each fixture the tool reports:
//
//	matched: truth signals with at least one CRC-passing decode within
//	         tolerance of (freq, dt). Headline parity number.
//	missed:  truth signals with no nearby CRC-passing decode.
//	extra:   CRC-passing decodes that don't match any truth entry. On
//	         synthetic fixtures these are CRC-14 false accepts
//	         (probability ~1/16384 per parity-clean codeword, so
//	         essentially never). On real captures they could also be
//	         signals jt9 missed — distinguishing requires manual review.
//
// Usage:
//
//	go run ./research/cmd/decode-eval                # walks research/
//	go run ./research/cmd/decode-eval -dir PATH      # walks PATH/
//	go run ./research/cmd/decode-eval -v             # verbose per-candidate
//
// Only top-level .wav files in the directory are scanned (no recursion).
// Files that aren't 12 kHz mono are skipped with a warning.
//
// Import rules: research code may use internal/audio (and stdlib +
// its own packages) but MUST NOT import internal/ft8/*.
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/research/candidates"
	"github.com/ColonelBlimp/station-manager/research/demod"
	"github.com/ColonelBlimp/station-manager/research/ldpc"
	"github.com/ColonelBlimp/station-manager/research/truth"
	"github.com/ColonelBlimp/station-manager/research/unpack"
)

const expectedSampleRate = 12000

// Match tolerances per truth source — same convention as find-candidates.
const (
	syntheticFreqTolHz  = 2.0
	syntheticDTMatchTol = 0.1
	jt9FreqMatchTolHz   = 5.0
	jt9DTMatchTolS      = 0.3
)

func tolerancesFor(manifest *truth.Manifest) (float64, float64) {
	if manifest != nil && manifest.Source != nil && *manifest.Source == "jt9-oracle" {
		return jt9FreqMatchTolHz, jt9DTMatchTolS
	}
	return syntheticFreqTolHz, syntheticDTMatchTol
}

// decodeRecord pairs a candidate with its decode outcome — used for
// matching and per-candidate reporting. CRCPass=true is the
// load-bearing acceptance flag; everything else is diagnostic.
type decodeRecord struct {
	cand      candidates.Candidate
	stats     ldpc.Stats
	crcPass   bool
	text      string // populated when crcPass && unpackOK
	unpackOK  bool   // Unpack returned no error
	unpackErr string // error string when unpackOK=false (for diagnostic display)
	msgType   uint8  // i3 from Unpack
}

func main() {
	dir := flag.String("dir", "research", "directory containing .wav files (non-recursive)")
	verbose := flag.Bool("v", false, "print per-candidate decode detail")
	flag.Parse()

	entries, err := os.ReadDir(*dir)
	if err != nil {
		log.Fatalf("read dir %q: %v", *dir, err)
	}

	var wavs []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".wav" {
			continue
		}
		wavs = append(wavs, filepath.Join(*dir, e.Name()))
	}
	sort.Strings(wavs)

	if len(wavs) == 0 {
		log.Fatalf("no .wav files found in %q", *dir)
	}

	fmt.Printf("Found %d .wav file(s) in %s/\n\n", len(wavs), *dir)

	// Cross-fixture totals — printed at the end for a single-line summary
	// across the whole corpus.
	var totalTruth, totalMatched, totalMissed, totalExtra, totalCrcPass int

	for _, wavPath := range wavs {
		data, err := audio.ReadWAV(wavPath)
		if err != nil {
			log.Printf("read %q: %v — skipping", wavPath, err)
			continue
		}
		if data.SampleRate != expectedSampleRate {
			log.Printf("%q: sample rate %d Hz, want %d — skipping", wavPath, data.SampleRate, expectedSampleRate)
			continue
		}
		if data.Channels != 1 {
			log.Printf("%q: %d channels, want mono — skipping", wavPath, data.Channels)
			continue
		}

		fmt.Printf("=== %s ===\n", wavPath)
		fmt.Printf("  %d samples @ %d Hz (%.2f s)\n",
			len(data.Samples), data.SampleRate,
			float64(len(data.Samples))/float64(data.SampleRate))

		truthPath := truth.PathFor(wavPath)
		manifest, err := truth.Read(truthPath)
		if err != nil {
			log.Printf("  read truth %q: %v — scoring skipped", truthPath, err)
		} else if manifest != nil {
			source := "synthetic"
			if manifest.Source != nil && *manifest.Source != "" {
				source = *manifest.Source
			}
			fmt.Printf("  truth manifest: %s (%d signals, source=%q)\n", truthPath, len(manifest.Signals), source)
		} else {
			fmt.Println("  truth manifest: (none)")
		}

		cands := candidates.Find(data.Samples)
		records := decodeAll(data.Samples, cands)

		matched, missed, extra := score(records, manifest, *verbose)

		if manifest != nil {
			totalTruth += len(manifest.Signals)
			totalMatched += matched
			totalMissed += missed
			totalExtra += extra
		}
		for _, r := range records {
			if r.crcPass {
				totalCrcPass++
			}
		}
		fmt.Println()
	}

	fmt.Println("=== Corpus totals ===")
	fmt.Printf("  truth signals:    %d\n", totalTruth)
	fmt.Printf("  CRC-pass decodes: %d\n", totalCrcPass)
	fmt.Printf("  matched:          %d\n", totalMatched)
	fmt.Printf("  missed:           %d\n", totalMissed)
	fmt.Printf("  extra:            %d\n", totalExtra)
	if totalTruth > 0 {
		fmt.Printf("  decode parity:    %d / %d (%.1f%%)\n",
			totalMatched, totalTruth, 100*float64(totalMatched)/float64(totalTruth))
	}
}

// decodeAll runs the demod + LLRs + ldpc.Decode + unpack chain on
// every candidate. Returns one decodeRecord per candidate, in the
// same order Find produced them. CRC-passing decodes also have
// their text unpacked so the caller can classify by message content.
func decodeAll(samples []float32, cands []candidates.Candidate) []decodeRecord {
	out := make([]decodeRecord, len(cands))
	for i, c := range cands {
		energies := demod.Demod(samples, c.Freq, c.DT)
		llrs := demod.LLRs(energies)

		var input [174]float64
		for k := 0; k < 174; k++ {
			input[k] = llrs[k]
		}
		result, stats := ldpc.Decode(input)

		rec := decodeRecord{
			cand:    c,
			stats:   stats,
			crcPass: stats.ConvergedCRC,
		}
		if rec.crcPass {
			ur, uerr := unpack.Unpack(result.Info)
			rec.msgType = ur.MsgType
			if uerr == nil {
				rec.text = ur.Text
				rec.unpackOK = true
			} else {
				rec.unpackErr = uerr.Error()
			}
		}
		out[i] = rec
	}
	return out
}

// score matches CRC-passing decodes against the truth manifest by
// TEXT equality + (freq, dt) proximity, then classifies any
// unmatched CRC-passes by message-content category. Reports counts
// so main can aggregate corpus totals.
//
// A "match" requires BOTH the unpacked text and the (freq, dt) to
// align with the same truth entry — same standard jt9 uses.
// Unmatched CRC-passes fall into:
//
//	extraTextValid:   unpack succeeded, text is well-formed, but no
//	                  truth entry has this text within tolerance.
//	                  Candidates: jt9-miss or rare CRC false-accept.
//	extraMalformed:   CRC passed but Unpack returned a parse error
//	                  on a SUPPORTED message type. Should be ~zero.
//	extraUnsupported: CRC passed and Unpack rejected as i3 != 1.
//	                  Real captures will have these — Type 4 (nonstandard
//	                  callsign) etc. — until more types are implemented.
func score(records []decodeRecord, manifest *truth.Manifest, verbose bool) (matched, missed, extra int) {
	crcPasses, unpackOK, unsupported, malformed := 0, 0, 0, 0
	for _, r := range records {
		if !r.crcPass {
			continue
		}
		crcPasses++
		if r.unpackOK {
			unpackOK++
		} else if r.msgType != 1 && r.msgType != 0 {
			// Non-Type-1 — unpacker hasn't implemented this type yet.
			unsupported++
		} else {
			malformed++
		}
	}
	fmt.Printf("  candidates: %d  (CRC-pass: %d, unpack-OK: %d, unsupported-type: %d, malformed: %d)\n",
		len(records), crcPasses, unpackOK, unsupported, malformed)

	if manifest == nil {
		if verbose {
			for i, r := range records {
				if !r.crcPass {
					continue
				}
				if r.unpackOK {
					fmt.Printf("    %2d. freq=%8.2f dt=%+.3f → %q\n", i+1, r.cand.Freq, r.cand.DT, r.text)
				} else {
					fmt.Printf("    %2d. freq=%8.2f dt=%+.3f  unpack-failed (i3=%d): %s\n",
						i+1, r.cand.Freq, r.cand.DT, r.msgType, r.unpackErr)
				}
			}
		}
		return 0, 0, crcPasses
	}

	freqTol, dtTol := tolerancesFor(manifest)

	matchedDecode := make([]int, len(records))
	for i := range matchedDecode {
		matchedDecode[i] = -1
	}
	matchedTruth := make([]int, len(manifest.Signals))
	for i := range matchedTruth {
		matchedTruth[i] = -1
	}

	// Match by TEXT + (freq, dt). A decode whose text doesn't equal
	// the truth text can't claim that truth entry, even if it's at
	// the right position — that's a wrong decode at the right place.
	for ti, ts := range manifest.Signals {
		bestIdx := -1
		bestDistSq := math.Inf(1)
		for di, r := range records {
			if !r.crcPass || !r.unpackOK {
				continue
			}
			if matchedDecode[di] >= 0 {
				continue
			}
			if r.text != ts.Text {
				continue
			}
			df := r.cand.Freq - ts.FreqHz
			ddt := r.cand.DT - ts.DTSec
			if math.Abs(df) > freqTol || math.Abs(ddt) > dtTol {
				continue
			}
			d := df*df + ddt*ddt
			if d < bestDistSq {
				bestDistSq = d
				bestIdx = di
			}
		}
		if bestIdx >= 0 {
			matchedDecode[bestIdx] = ti
			matchedTruth[ti] = bestIdx
		}
	}

	matched = 0
	for _, di := range matchedTruth {
		if di >= 0 {
			matched++
		}
	}
	missed = len(manifest.Signals) - matched

	extra = 0
	for di, r := range records {
		if r.crcPass && matchedDecode[di] < 0 {
			extra++
		}
	}

	fmt.Printf("  truth: %d matched, %d missed, %d extra (CRC-pass off-target)\n",
		matched, missed, extra)

	if verbose {
		for i, r := range records {
			if !r.crcPass && !verbose {
				continue
			}
			tag := "    "
			if r.crcPass {
				if ti := matchedDecode[i]; ti >= 0 {
					ts := manifest.Signals[ti]
					df := r.cand.Freq - ts.FreqHz
					ddt := r.cand.DT - ts.DTSec
					tag = fmt.Sprintf(" OK %q (df=%+.2f Hz ddt=%+.3f s)", ts.Text, df, ddt)
				} else if !r.unpackOK {
					if r.msgType != 1 {
						tag = fmt.Sprintf(" UNSUPPORTED type i3=%d", r.msgType)
					} else {
						tag = fmt.Sprintf(" MALFORMED: %s", r.unpackErr)
					}
				} else {
					tag = fmt.Sprintf(" EXTRA %q (no truth-text match within tolerance)", r.text)
				}
			} else if r.stats.ConvergedParity {
				tag = fmt.Sprintf(" parity-only iters=%d", r.stats.Iterations)
			} else {
				tag = fmt.Sprintf(" no-decode iters=%d bestSyn=%d", r.stats.Iterations, r.stats.BestSyndromeWeight)
			}
			crcStr := "    "
			if r.crcPass {
				crcStr = "CRC "
			}
			fmt.Printf("    %2d. freq=%8.2f dt=%+.3f s1=%.2f  %s%s\n",
				i+1, r.cand.Freq, r.cand.DT, r.cand.Score, crcStr, tag)
		}

		if missed > 0 {
			fmt.Printf("  missed (%d):\n", missed)
			for ti, di := range matchedTruth {
				if di < 0 {
					ts := manifest.Signals[ti]
					fmt.Printf("    - %q at %.2f Hz dt=%+.3f s\n", ts.Text, ts.FreqHz, ts.DTSec)
				}
			}
		}
	}

	return matched, missed, extra
}
