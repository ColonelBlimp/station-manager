// ft8-truth-from-jt9 walks a directory of FT8 .wav captures, runs
// jt9 -8 on each via subprocess, parses the decoded signals from
// jt9's stdout, and writes a truth manifest next to each WAV.
//
// Usage:
//
//	go run ./cmd/ft8-truth-from-jt9                    # walks captures/
//	go run ./cmd/ft8-truth-from-jt9 -dir PATH          # walks PATH/
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
