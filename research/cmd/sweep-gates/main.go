// sweep-gates runs the candidate-finder gate cube against the full
// decode-eval pipeline (Find → Demod → LLRs → ldpc.Decode → Unpack)
// across every .wav file under the supplied directory, scoring each
// configuration at the text level (matched / text-extra / malformed)
// plus runtime + candidate count.
//
// This is the harness that drives gate-relaxation decisions. The
// adoption rule, per session 92 directive: only adopt a relaxed
// configuration if matched goes up without text-extra or malformed
// regressing and runtime stays acceptable.
//
// **Status: measured negative on the real-capture corpus (Session 92,
// 2026-05-25).** All 30 cells of the (stage1 ∈ {1.0, 0.8, 0.6}) ×
// (wins ∈ {8..4}) × (perBlock ∈ {1, 0}) cube produced identical
// outcomes: matched=96, text-extra=2, malformed=0, CRC-pass=98.
// Candidate count saturated at 600 = `stage2MaxResults` × 6 slots
// the moment any gate relaxed, but no admitted candidate produced
// a CRC pass — the LLRs at those (freq, dt) positions are too noisy
// for BP+OSD-2 to converge regardless of how generous the gate is.
//
// What this empirically falsified: the working hypothesis that the
// 7 finder-recall misses (5 WINS_LOW + 2 STAGE1_LOW at their
// truth-nearest bin) were candidate-phase problems. They're
// decoder problems. Relaxing the gates costs runtime (584 → 600
// verifies/slot) and yields zero new decodes.
//
// The harness is retained because (a) the result is the artifact —
// the next session that proposes a gate tweak should re-run rather
// than re-argue from first principles, (b) the next planned cap
// sweep (stage2MaxResults ∈ {100, 200, 500}) plugs into the same
// harness with one new dimension, and (c) any future change to
// demod or LDPC may shift the result and the harness becomes the
// regression detector for that shift.
//
// Usage:
//
//	go run ./research/cmd/sweep-gates             # walks captures/
//	go run ./research/cmd/sweep-gates -dir PATH
//
// Output: a wide table, one row per (winsTotal, winsPerBlock, s1)
// combination, with the corpus totals across every wav.
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
	"time"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/research/candidates"
	"github.com/ColonelBlimp/station-manager/research/demod"
	"github.com/ColonelBlimp/station-manager/research/ldpc"
	"github.com/ColonelBlimp/station-manager/research/truth"
	"github.com/ColonelBlimp/station-manager/research/unpack"
)

const expectedSampleRate = 12000

// Match tolerances (same as decode-eval).
const (
	jt9FreqMatchTolHz = 5.0
	jt9DTMatchTolS    = 0.3
	synthFreqTolHz    = 2.0
	synthDTMatchTol   = 0.1
)

func tolerancesFor(manifest *truth.Manifest) (float64, float64) {
	if manifest != nil && manifest.Source != nil && *manifest.Source == "jt9-oracle" {
		return jt9FreqMatchTolHz, jt9DTMatchTolS
	}
	return synthFreqTolHz, synthDTMatchTol
}

type slotData struct {
	path     string
	samples  []float32
	manifest *truth.Manifest
	freqTol  float64
	dtTol    float64
}

type cellResult struct {
	gates       candidates.Gates
	candCount   int
	crcPass     int
	matched     int
	textExtra   int
	unsupported int
	malformed   int
	elapsed     time.Duration
}

func main() {
	dir := flag.String("dir", "captures", "directory containing .wav files (non-recursive)")
	capSweep := flag.Bool("cap-sweep", false, "sweep Stage2MaxResults ∈ {100,200,500} at default other gates, instead of the (stage1 × wins × perBlock) cube")
	flag.Parse()

	slots, err := loadSlots(*dir)
	if err != nil {
		log.Fatalf("load slots: %v", err)
	}
	if len(slots) == 0 {
		log.Fatalf("no usable .wav files in %q", *dir)
	}

	// Baseline (DefaultGates) first as the comparison anchor — both
	// sweep modes share it.
	baselineCell := runCell(slots, candidates.DefaultGates)
	printHeader()
	printRow(baselineCell, "baseline")

	if *capSweep {
		// Cap-only sweep: hold gates at default, raise Stage2MaxResults.
		// Answers the question "are good-but-rank-50+ candidates being
		// dropped by the 100-cap" — distinct from gate relaxation
		// (which admits low-quality candidates). Session 92's gate
		// sweep showed flat results at 600 candidates per corpus with
		// relaxed gates; this re-asks the question at strict gates.
		fmt.Printf("cap-sweep mode: 3 caps × %d slots = %d (Find + decode) runs\n\n",
			len(slots), 3*len(slots))
		caps := []int{100, 200, 500}
		var cells []cellResult
		for _, capN := range caps {
			g := candidates.DefaultGates
			g.Stage2MaxResults = capN
			if g == candidates.DefaultGates {
				continue // baseline already printed
			}
			c := runCell(slots, g)
			cells = append(cells, c)
			printRow(c, fmt.Sprintf("cap=%d", capN))
		}
		printAdoption(cells, baselineCell)
		return
	}

	winsTotalLevels := []int{8, 7, 6, 5, 4}
	winsPerBlockLevels := []int{1, 0}
	s1Levels := []float64{1.0, 0.8, 0.6}

	fmt.Printf("sweep over %d configs × %d slots = %d (Find + decode) runs\n\n",
		len(winsTotalLevels)*len(winsPerBlockLevels)*len(s1Levels),
		len(slots),
		len(winsTotalLevels)*len(winsPerBlockLevels)*len(s1Levels)*len(slots),
	)

	var cells []cellResult
	for _, s1 := range s1Levels {
		for _, wt := range winsTotalLevels {
			for _, wpb := range winsPerBlockLevels {
				g := candidates.Gates{
					Stage1Threshold:    s1,
					SanityWinsTotal:    wt,
					SanityWinsPerBlock: wpb,
					Stage2MaxResults:   candidates.DefaultGates.Stage2MaxResults,
				}
				// Skip the cell that IS the baseline (avoid double-count).
				if g == candidates.DefaultGates {
					continue
				}
				c := runCell(slots, g)
				cells = append(cells, c)
				printRow(c, "")
			}
		}
	}

	printAdoption(cells, baselineCell)
}

// printAdoption filters the swept cells against the adoption rule
// — matched up AND extras/malformed not regressed — and prints the
// surviving rows. Shared between the gate-cube and cap-sweep modes
// so both honour the same operator-stated adoption criteria.
func printAdoption(cells []cellResult, baseline cellResult) {
	fmt.Println()
	fmt.Println("=== Configurations meeting adoption criteria (matched↑, extras/malformed not regressed) ===")
	any := false
	for _, c := range cells {
		if c.matched > baseline.matched &&
			c.textExtra <= baseline.textExtra &&
			c.malformed <= baseline.malformed {
			any = true
			printRow(c, "★")
		}
	}
	if !any {
		fmt.Println("  (none)")
	}
}

func loadSlots(dir string) ([]slotData, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var wavs []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".wav" {
			continue
		}
		wavs = append(wavs, filepath.Join(dir, e.Name()))
	}
	sort.Strings(wavs)

	var out []slotData
	for _, p := range wavs {
		data, err := audio.ReadWAV(p)
		if err != nil {
			log.Printf("read %q: %v — skipping", p, err)
			continue
		}
		if data.SampleRate != expectedSampleRate || data.Channels != 1 {
			log.Printf("%q: rate=%d channels=%d — skipping", p, data.SampleRate, data.Channels)
			continue
		}
		manifest, _ := truth.Read(truth.PathFor(p))
		fTol, dTol := tolerancesFor(manifest)
		out = append(out, slotData{
			path: p, samples: data.Samples, manifest: manifest,
			freqTol: fTol, dtTol: dTol,
		})
	}
	return out, nil
}

func runCell(slots []slotData, gates candidates.Gates) cellResult {
	cell := cellResult{gates: gates}
	start := time.Now()
	for _, s := range slots {
		cands := candidates.FindWithGates(s.samples, gates)
		cell.candCount += len(cands)

		records := decodeAll(s.samples, cands)
		matched, textExtra, unsupported, malformed, crcPass := score(records, s.manifest, s.freqTol, s.dtTol)
		cell.matched += matched
		cell.textExtra += textExtra
		cell.unsupported += unsupported
		cell.malformed += malformed
		cell.crcPass += crcPass
	}
	cell.elapsed = time.Since(start)
	return cell
}

type decodeRecord struct {
	cand     candidates.Candidate
	crcPass  bool
	unpackOK bool
	text     string
	msgType  uint8
}

func decodeAll(samples []float32, cands []candidates.Candidate) []decodeRecord {
	out := make([]decodeRecord, len(cands))
	for i, c := range cands {
		var input [174]float64
		energies := demod.Demod(samples, c.Freq, c.DT)
		llrs := demod.LLRs(energies)
		for k := 0; k < 174; k++ {
			input[k] = llrs[k]
		}
		result, stats := ldpc.Decode(input)
		rec := decodeRecord{cand: c, crcPass: stats.ConvergedCRC}
		if rec.crcPass {
			ur, uerr := unpack.Unpack(result.Info)
			rec.msgType = ur.MsgType
			if uerr == nil {
				rec.text = ur.Text
				rec.unpackOK = true
			}
		}
		out[i] = rec
	}
	return out
}

func score(records []decodeRecord, manifest *truth.Manifest, freqTol, dtTol float64) (matched, textExtra, unsupported, malformed, crcPass int) {
	for _, r := range records {
		if r.crcPass {
			crcPass++
		}
	}
	if manifest == nil {
		for _, r := range records {
			if !r.crcPass {
				continue
			}
			if !r.unpackOK {
				if r.msgType != 1 {
					unsupported++
				} else {
					malformed++
				}
			} else {
				textExtra++
			}
		}
		return matched, textExtra, unsupported, malformed, crcPass
	}

	matchedDecode := make([]int, len(records))
	for i := range matchedDecode {
		matchedDecode[i] = -1
	}
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
			matched++
		}
	}
	for di, r := range records {
		if !r.crcPass || matchedDecode[di] >= 0 {
			continue
		}
		if !r.unpackOK {
			if r.msgType != 1 {
				unsupported++
			} else {
				malformed++
			}
		} else {
			textExtra++
		}
	}
	return matched, textExtra, unsupported, malformed, crcPass
}

func printHeader() {
	fmt.Printf("%-5s %-5s %-5s | %-6s %-6s %-6s %-6s %-6s | %-9s | %s\n",
		"s1", "wins", "/blk", "cands", "CRC", "match", "extra", "malf", "runtime", "tag")
	fmt.Println("------------------+-------------------------------------+-----------+----")
}

func printRow(c cellResult, tag string) {
	fmt.Printf("%-5.2f %-5d %-5d | %-6d %-6d %-6d %-6d %-6d | %9s | %s\n",
		c.gates.Stage1Threshold,
		c.gates.SanityWinsTotal,
		c.gates.SanityWinsPerBlock,
		c.candCount,
		c.crcPass,
		c.matched,
		c.textExtra,
		c.malformed,
		c.elapsed.Round(time.Millisecond),
		tag,
	)
}
