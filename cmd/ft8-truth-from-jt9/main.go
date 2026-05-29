// ft8-truth-from-jt9 walks a directory of FT8 .wav captures, runs
// jt9 -8 on each via subprocess, parses the decoded signals from
// jt9's stdout, and writes a truth manifest next to each WAV.
//
// Two run modes:
//
//   - Per-file (default): jt9 is invoked once per WAV. Each file is
//     decoded in isolation with an empty callsign hash table, so any
//     message whose caller-side hash refers to a callsign decoded in
//     an earlier file stays as the `<...>` placeholder.
//
//   - Sequential (-sequential): jt9 is invoked once with all WAVs as
//     positional args, alphabetically sorted. jt9's internal hash
//     table is carried across files, so hash references can resolve
//     to plain callsigns introduced earlier in the run (e.g.
//     `<DG6JW/T> SV0TPN +01` in 20m_slot3 rather than the per-file
//     `<...> SV0TPN +01`). The output is partitioned back to one
//     truth.json per WAV via jt9's `<DecodeFinished>` markers.
//
// Usage:
//
//	go run ./cmd/ft8-truth-from-jt9                    # per-file, walks captures/
//	go run ./cmd/ft8-truth-from-jt9 -dir PATH          # walks PATH/
//	go run ./cmd/ft8-truth-from-jt9 -sequential        # single sorted jt9 run
//	go run ./cmd/ft8-truth-from-jt9 -overwrite         # rewrite even if truth exists
//	go run ./cmd/ft8-truth-from-jt9 -jt9 /path/to/jt9  # explicit binary path
//
// Output: <wav-base>.truth.json next to each <wav-base>.wav, with
// Source = "jt9-oracle" so downstream matchers can apply
// oracle-appropriate tolerances (jt9 quantises freq to ~1 Hz, dt to
// ~0.1 s; real signals also have ±5-10 ms operator clock drift).
//
// Black-box-oracle posture: jt9 is GPL but we only consume its
// stdout. No source crosses the firewall. Same pattern as cmd/ft8-eval.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ColonelBlimp/station-manager/research/truth"
)

// jt9SampleRate is the FT8 canonical 12 kHz that jt9 expects. We
// hardcode it because the tool's job is to produce truth for FT8
// captures; reading the WAV's actual sample rate would add dependency
// surface (this tool is otherwise stdlib + research/truth only).
const jt9SampleRate = 12000

// truthSource is the Source value recorded on every manifest this
// tool writes. Downstream matchers can branch on this.
const truthSource = "jt9-oracle"

func main() {
	dir := flag.String("dir", "captures", "directory of .wav files to process (non-recursive)")
	overwrite := flag.Bool("overwrite", false, "rewrite even if <wav-base>.truth.json already exists")
	sequential := flag.Bool("sequential", false, "invoke jt9 once with all WAVs (alphabetically sorted) so the callsign hash table carries across files")
	jt9 := flag.String("jt9", "jt9", "jt9 binary (PATH lookup if just the name)")
	flag.Parse()

	entries, err := os.ReadDir(*dir)
	if err != nil {
		log.Fatalf("read dir %q: %v", *dir, err)
	}

	var wavs []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".wav" {
			continue
		}
		wavs = append(wavs, filepath.Join(*dir, e.Name()))
	}
	if len(wavs) == 0 {
		log.Fatalf("no .wav files found in %q", *dir)
	}
	// Sort unconditionally: per-file mode is order-independent so
	// alphabetical ordering only affects log readability; sequential
	// mode depends on alphabetical order for the canonical fixture
	// sequence (20m_slot1 < 20m_slot2 < 20m_slot3 < live_slot1 …).
	sort.Strings(wavs)

	if *sequential {
		runSequential(*jt9, wavs, *overwrite)
		return
	}

	for _, wavPath := range wavs {
		truthPath := truth.PathFor(wavPath)
		if !*overwrite {
			if _, err := os.Stat(truthPath); err == nil {
				fmt.Printf("skip %s (truth exists; -overwrite to replace)\n", wavPath)
				continue
			}
		}

		signals, err := runJT9(*jt9, wavPath)
		if err != nil {
			log.Printf("jt9 failed on %q: %v — skipping", wavPath, err)
			continue
		}

		source := truthSource
		manifest := &truth.Manifest{
			Wav:        filepath.Base(wavPath),
			SampleRate: jt9SampleRate,
			Source:     &source,
			Signals:    signals,
		}
		if err := truth.Write(truthPath, manifest); err != nil {
			log.Printf("write %q: %v — skipping", truthPath, err)
			continue
		}

		fmt.Printf("%s → %d signals → %s\n", filepath.Base(wavPath), len(signals), filepath.Base(truthPath))
	}
}

// runSequential invokes jt9 once with every WAV in wavs as a positional
// argument. jt9 processes them in argv order; its internal callsign
// hash table accumulates across files, which is the only way to make
// hash-prefixed messages (e.g. `<DG6JW/T> SV0TPN +01`) resolve to plain
// callsigns rather than the `<...>` placeholder. The output is
// partitioned back to per-WAV truth manifests via the `<DecodeFinished>`
// marker jt9 emits between files.
//
// Order matters: the wavs slice must already be in the same order that
// the caller wants jt9 to see them. main sorts alphabetically before
// calling, so the canonical fixture sequence is deterministic.
func runSequential(jt9Bin string, wavs []string, overwrite bool) {
	if !overwrite {
		// Per-file skip semantics are tricky in sequential mode — if
		// any one truth exists, the safest is to still produce the
		// full sequential run (because skipping a file would mean an
		// incomplete sequence, defeating the hash-carry purpose).
		// Document the behaviour and proceed.
		for _, wavPath := range wavs {
			if _, err := os.Stat(truth.PathFor(wavPath)); err == nil {
				fmt.Printf("note: %s already has truth; -sequential without -overwrite refuses to replace\n", filepath.Base(wavPath))
				return
			}
		}
	}

	perFile, err := runJT9Sequential(jt9Bin, wavs)
	if err != nil {
		log.Fatalf("jt9 sequential run failed: %v", err)
	}

	for i, wavPath := range wavs {
		source := truthSource
		manifest := &truth.Manifest{
			Wav:        filepath.Base(wavPath),
			SampleRate: jt9SampleRate,
			Source:     &source,
			Signals:    perFile[i],
		}
		truthPath := truth.PathFor(wavPath)
		if err := truth.Write(truthPath, manifest); err != nil {
			log.Printf("write %q: %v — skipping", truthPath, err)
			continue
		}
		fmt.Printf("%s → %d signals → %s\n", filepath.Base(wavPath), len(perFile[i]), filepath.Base(truthPath))
	}
}

// runJT9 invokes `jt9 -8 <wavPath>` and parses the decoded signals
// from its stdout. Returns the parsed list or an error if jt9 itself
// failed (non-zero exit). An empty list (no decodes) is NOT an error.
func runJT9(jt9 string, wavPath string) ([]truth.Signal, error) {
	cmd := exec.Command(jt9, "-8", wavPath) // #nosec G204 — jt9 path & wavPath are operator-supplied (CLI flags / dir scan).
	stdout, err := cmd.Output()
	if err != nil {
		// CombinedOutput-style diagnostic on failure — jt9 sometimes
		// emits warnings to stderr that point at the cause.
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%w: stderr=%q", err, string(exitErr.Stderr))
		}
		return nil, err
	}
	return parseJT9Output(stdout), nil
}

// parseJT9Output reads jt9 -8's stdout line by line, picking out the
// decoded-signal lines and ignoring headers / framing. jt9 emits
// decoded signals in the form:
//
//	HHMMSS  SNR  DT  Freq ~  TEXT
//
// e.g.:
//
//	000000   2  0.5  650 ~  CQ K1JT FN20
//
// Header lines, <DecodeFinished>, and anything without " ~ " is
// skipped.
func parseJT9Output(output []byte) []truth.Signal {
	var signals []truth.Signal
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		sig, ok := parseJT9Line(scanner.Text())
		if !ok {
			continue
		}
		signals = append(signals, sig)
	}
	return signals
}

// runJT9Sequential invokes `jt9 -8 wav1 wav2 ...` in a single
// subprocess and returns one slice of signals per WAV, partitioned by
// the `<DecodeFinished>` markers jt9 emits between files.
func runJT9Sequential(jt9Bin string, wavPaths []string) ([][]truth.Signal, error) {
	args := append([]string{"-8"}, wavPaths...)
	cmd := exec.Command(jt9Bin, args...) // #nosec G204 — jt9 path & wav paths are operator-supplied.
	stdout, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%w: stderr=%q", err, string(exitErr.Stderr))
		}
		return nil, err
	}
	return parseJT9OutputPerFile(stdout, len(wavPaths))
}

// parseJT9OutputPerFile partitions jt9's multi-file stdout into one
// signal list per input file. The boundary marker is a line beginning
// with `<DecodeFinished>` (jt9 emits one per file in argv order).
//
// Returns an error if the number of `<DecodeFinished>` blocks doesn't
// match nFiles — that's a contract violation (a jt9 invocation given N
// WAVs is documented to emit N <DecodeFinished> markers) and silently
// papering over it would attach decodes from the wrong file to a
// truth manifest.
func parseJT9OutputPerFile(output []byte, nFiles int) ([][]truth.Signal, error) {
	perFile := make([][]truth.Signal, 0, nFiles)
	var current []truth.Signal
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "<DecodeFinished>") {
			// Promote current (which may be nil for a no-decode file)
			// to an explicit empty slice so the per-file index aligns
			// with the input WAV order even when a file produced zero
			// signals.
			if current == nil {
				current = []truth.Signal{}
			}
			perFile = append(perFile, current)
			current = nil
			continue
		}
		if sig, ok := parseJT9Line(line); ok {
			current = append(current, sig)
		}
	}
	if len(perFile) != nFiles {
		return nil, fmt.Errorf("jt9 output: expected %d <DecodeFinished> blocks, got %d", nFiles, len(perFile))
	}
	return perFile, nil
}

// parseJT9Line tries to parse one jt9 stdout line as a decoded
// signal. Returns (signal, true) on success; (zero, false) on any
// line that isn't a decode (header/footer/blank). Whitespace-tolerant
// — jt9 right-aligns the SNR / DT / Freq columns with variable padding.
func parseJT9Line(line string) (truth.Signal, bool) {
	sepIdx := strings.Index(line, " ~ ")
	if sepIdx < 0 {
		return truth.Signal{}, false
	}
	header := strings.TrimSpace(line[:sepIdx])
	text := strings.TrimSpace(line[sepIdx+3:])
	if text == "" {
		return truth.Signal{}, false
	}

	// Expect 4 whitespace-separated fields in the header:
	//   HHMMSS  SNR  DT  Freq
	fields := strings.Fields(header)
	if len(fields) != 4 {
		return truth.Signal{}, false
	}
	dt, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return truth.Signal{}, false
	}
	freq, err := strconv.ParseFloat(fields[3], 64)
	if err != nil {
		return truth.Signal{}, false
	}

	return truth.Signal{
		Text:   text,
		FreqHz: freq,
		DTSec:  dt,
	}, true
}
