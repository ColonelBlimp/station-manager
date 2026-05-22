package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
)

func main() {
	var (
		subPasses = flag.Int("subtraction-passes", 0, "DecodeOptions.SubtractionPasses (0=fast baseline, 1=sensitive)")
		osdOrder  = flag.Int("osd-order", 0, "DecodeOptions.OSDOrder (0=default order-1, -1=disable BP-only, 2=order-2)")
		llrScale  = flag.Float64("llr-scale", 0, "DecodeOptions.LLRScale (0=default 1.0)")
		ldpcIters = flag.Int("ldpc-iters", 0, "DecodeOptions.LDPCMaxIterations (0=default 50)")
		useHash   = flag.Bool("hashtable", false, "use a fresh per-file codec.HashTable (retains hash-bearing Type 1/2/3 messages)")
		oracle    = flag.Bool("oracle", false, "run jt9 -8 (WSJT-X) as black-box ground truth; show SM-vs-jt9 parity")
		msgs      = flag.Bool("msgs", false, "list each decoded message per file")
		runs      = flag.Int("runs", 1, "decode each file N times; report the fastest wall time")
		mem       = flag.Bool("mem", false, "report heap bytes allocated per decode (first run, TotalAlloc delta)")
	)
	flag.Usage = usage
	flag.Parse()
	if flag.NArg() == 0 {
		usage()
		os.Exit(2)
	}
	if *runs < 1 {
		*runs = 1
	}

	files, err := expandWavs(flag.Args())
	if err != nil {
		fatal("%v", err)
	}
	if len(files) == 0 {
		fatal("no .wav files found in arguments")
	}

	opts := ft8.DecodeOptions{
		SubtractionPasses: *subPasses,
		OSDOrder:          *osdOrder,
		LLRScale:          *llrScale,
		LDPCMaxIterations: *ldpcIters,
	}

	oracleOK := *oracle
	if *oracle {
		if _, lookErr := exec.LookPath("jt9"); lookErr != nil {
			fmt.Fprintln(os.Stderr, "warning: -oracle requested but jt9 not on PATH; skipping oracle comparison")
			oracleOK = false
		}
	}

	printConfig(opts, *runs, *useHash)
	printHeader(oracleOK, *mem)

	var smTotal, jt9Total int
	for _, f := range files {
		data, err := audio.ReadWAV(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: read %s: %v\n", f, err)
			continue
		}

		var results []ft8.DecodedMessage
		best := time.Duration(1<<63 - 1)
		var allocBytes uint64
		for r := 0; r < *runs; r++ {
			var before runtime.MemStats
			if *mem && r == 0 {
				runtime.GC()
				runtime.ReadMemStats(&before)
			}
			t0 := time.Now()
			res := ft8.Decode(data.Samples, withHashTable(opts, *useHash))
			elapsed := time.Since(t0)
			if *mem && r == 0 {
				var after runtime.MemStats
				runtime.ReadMemStats(&after)
				allocBytes = after.TotalAlloc - before.TotalAlloc
			}
			if elapsed < best {
				best = elapsed
			}
			if r == 0 {
				results = res
			}
		}

		jt9Count := -1
		if oracleOK {
			jt9Count = runOracle(f)
			if jt9Count >= 0 {
				jt9Total += jt9Count
			}
		}
		smTotal += len(results)

		printRow(displayName(f), len(results), jt9Count, best, allocBytes, oracleOK, *mem)
		if *msgs {
			printMessages(results)
		}
	}

	printTotals(smTotal, jt9Total, oracleOK)
}

// withHashTable returns opts with a fresh single-slot HashTable attached
// when requested. A per-file fresh table is the honest single-slot
// measure; the daemon reuses one table across slots (cross-slot hash
// resolution), but for corpus evaluation each file stands alone.
func withHashTable(opts ft8.DecodeOptions, use bool) ft8.DecodeOptions {
	if use {
		opts.HashTable = codec.NewHashTable(0) // 0 → default capacity
	}
	return opts
}

// runOracle runs `jt9 -8 <wav>` in a throwaway working directory and
// returns the number of decoded-message lines (those carrying the ` ~ `
// decode marker; the trailing <DecodeFinished> line is excluded).
// Returns -1 on failure.
func runOracle(wav string) int {
	abs, err := filepath.Abs(wav)
	if err != nil {
		return -1
	}
	tmp, err := os.MkdirTemp("", "ft8-eval-jt9-")
	if err != nil {
		return -1
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "jt9", "-8", abs)
	cmd.Dir = tmp // keep jt9's side-effect files out of the repo
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: jt9 on %s: %v\n", filepath.Base(wav), err)
		return -1
	}
	n := 0
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		if strings.Contains(sc.Text(), " ~ ") {
			n++
		}
	}
	return n
}

// expandWavs turns the argument list (files and/or directories) into a
// sorted, de-duplicated list of *.wav paths. Directories are walked one
// level deep — enough for testdata/ and captures/, not a recursive find.
func expandWavs(args []string) ([]string, error) {
	seen := map[string]struct{}{}
	var out []string
	add := func(p string) {
		if _, dup := seen[p]; dup {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	for _, a := range args {
		info, err := os.Stat(a)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", a, err)
		}
		if !info.IsDir() {
			if strings.EqualFold(filepath.Ext(a), ".wav") {
				add(a)
			}
			continue
		}
		entries, err := os.ReadDir(a)
		if err != nil {
			return nil, fmt.Errorf("read dir %s: %w", a, err)
		}
		for _, e := range entries {
			if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".wav") {
				add(filepath.Join(a, e.Name()))
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func displayName(p string) string {
	// Keep the parent dir for disambiguation (cap1.wav vs slot1.wav live
	// in different dirs) but drop deeper path noise.
	dir := filepath.Base(filepath.Dir(p))
	return filepath.Join(dir, filepath.Base(p))
}

func printConfig(opts ft8.DecodeOptions, runs int, useHash bool) {
	fmt.Printf("DecodeOptions: subtraction_passes=%d osd_order=%d llr_scale=%g ldpc_iters=%d hashtable=%v  (runs=%d)\n\n",
		opts.SubtractionPasses, opts.OSDOrder, opts.LLRScale, opts.LDPCMaxIterations, useHash, runs)
}

func printHeader(oracle, mem bool) {
	line := fmt.Sprintf("%-26s %5s", "file", "sm")
	if oracle {
		line += fmt.Sprintf(" %5s %7s", "jt9", "parity")
	}
	line += fmt.Sprintf(" %9s", "time")
	if mem {
		line += fmt.Sprintf(" %9s", "alloc")
	}
	fmt.Println(line)
	fmt.Println(strings.Repeat("-", len(line)))
}

func printRow(name string, sm, jt9 int, dur time.Duration, allocBytes uint64, oracle, mem bool) {
	line := fmt.Sprintf("%-26s %5d", name, sm)
	if oracle {
		par := "-"
		if jt9 > 0 {
			par = fmt.Sprintf("%d%%", sm*100/jt9)
		} else if jt9 == 0 {
			par = "n/a"
		}
		jt9s := "-"
		if jt9 >= 0 {
			jt9s = fmt.Sprintf("%d", jt9)
		}
		line += fmt.Sprintf(" %5s %7s", jt9s, par)
	}
	line += fmt.Sprintf(" %9s", dur.Round(time.Millisecond))
	if mem {
		line += fmt.Sprintf(" %8.1fM", float64(allocBytes)/(1<<20))
	}
	fmt.Println(line)
}

func printMessages(results []ft8.DecodedMessage) {
	for i, r := range results {
		fmt.Printf("      [%2d] %7.2f Hz  %+.2f s  sync=%5.2f  %q\n",
			i, r.Freq, r.DT, r.SyncPower, r.Text)
	}
}

func printTotals(sm, jt9 int, oracle bool) {
	fmt.Println()
	if oracle && jt9 > 0 {
		fmt.Printf("TOTAL  sm=%d  jt9=%d  parity=%d%%\n", sm, jt9, sm*100/jt9)
		return
	}
	fmt.Printf("TOTAL  sm=%d\n", sm)
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: ft8-eval [flags] <wav-or-dir>...\n\n")
	fmt.Fprintf(os.Stderr, "Runs ft8.Decode on each WAV and reports decode count + timing.\n\n")
	flag.PrintDefaults()
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
