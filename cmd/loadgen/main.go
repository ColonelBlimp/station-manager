// Command loadgen drives the daemon's POST /v1/qso endpoint flat-out
// for stress / profiling. Pair with `cfg.Server.EnableProfiling=true`
// and capture pprof profiles in a third terminal:
//
//	go tool pprof http://localhost:8080/debug/pprof/profile?seconds=30
//	go tool pprof http://localhost:8080/debug/pprof/heap
//
// The harness generates unique sequential callsigns (e.g. 7Q1AAA,
// 7Q1AAB, …, 7Q1ZZZ) so the daemon's dedupe path never short-circuits
// — every record exercises the full insert + qso_history audit + events
// broadcast hot path. The cap is 26³ = 17576 unique callsigns per
// prefix; bump the prefix or extend to a 4-letter suffix if you need
// more.
//
// IMPORTANT: the daemon's submit-rate-limit defaults (20/sec, burst 40)
// will throttle this harness aggressively. For meaningful stress runs,
// bump cfg.Server.SubmitRatePerSec / SubmitRateBurst /
// MaxConcurrentRequests in build/config.json before starting the
// daemon, then restore them afterward — the production-default floor
// is a self-DoS guard, not a performance ceiling.
//
// Output: one line per worker progress beat, then a summary at end with
// total / stored / duplicate / rate-limited / error counts plus
// p50/p95/p99 latency and overall throughput.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// callsignSuffix maps an integer 0..17575 to a fixed-width 3-letter
// base-26 suffix (000 → "AAA", 25 → "AAZ", 17575 → "ZZZ"). Beyond
// 17575 the mapping wraps, which would produce duplicate callsigns
// the daemon's dedupe would reject — main.go enforces the bound up
// front so wrapping never reaches the daemon.
func callsignSuffix(n int) string {
	a := byte('A' + n/676)
	b := byte('A' + (n/26)%26)
	c := byte('A' + n%26)
	return string([]byte{a, b, c})
}

// adifBody builds a minimal valid ADIF record for the supplied
// contacted call + station call. Band/mode/freq are fixed (20m SSB
// on 14.250 MHz) — the goal is to exercise the daemon's write path,
// not realistic frequency variety. Unique callsigns mean each record
// has a unique dedupe key regardless.
//
// QSO_DATE / TIME_ON use a fixed timestamp shared across the whole
// run — the dedupe key includes them but is already unique via the
// callsign, so a constant date/time is fine and removes an axis of
// noise from the harness's behaviour.
func adifBody(call, station, qsoDate, timeOn string) string {
	return fmt.Sprintf(
		"<CALL:%d>%s\n"+
			"<BAND:3>20m\n"+
			"<MODE:3>SSB\n"+
			"<FREQ:6>14.250\n"+
			"<QSO_DATE:8>%s\n"+
			"<TIME_ON:4>%s\n"+
			"<TIME_OFF:4>%s\n"+
			"<RST_SENT:2>59\n"+
			"<RST_RCVD:2>59\n"+
			"<STATION_CALLSIGN:%d>%s\n"+
			"<COUNTRY:6>Malawi\n"+
			"<EOR>",
		len(call), call,
		qsoDate,
		timeOn, timeOn,
		len(station), station,
	)
}

// result is one request's outcome. Latency is recorded regardless of
// success so 429s and errors don't skew the percentile picture by
// being absent.
type result struct {
	status  int
	latency time.Duration
	err     error
}

func main() {
	var (
		target          = flag.String("target", "http://localhost:8080", "daemon base URL")
		concurrency     = flag.Int("concurrency", 50, "concurrent workers")
		count           = flag.Int("count", 5000, "total QSOs to send (max 17576 per prefix; raise prefix or extend code for more)")
		prefix          = flag.String("prefix", "7Q1", "callsign prefix; 3-letter suffix is appended (7Q1AAA … 7Q1ZZZ)")
		stationCallsign = flag.String("station", "G4ABC", "STATION_CALLSIGN value (must match the target logbook's callsign)")
		logbookID       = flag.Int("logbook", 1, "logbook ID")
		timeoutSec      = flag.Int("request-timeout-sec", 10, "per-request timeout")
	)
	flag.Parse()

	if *count <= 0 {
		fmt.Fprintln(os.Stderr, "count must be positive")
		os.Exit(2)
	}
	if *count > 26*26*26 {
		fmt.Fprintf(os.Stderr, "count=%d exceeds the 17576 unique-callsign cap for a 3-letter suffix; raise prefix or extend code\n", *count)
		os.Exit(2)
	}
	if *concurrency <= 0 {
		fmt.Fprintln(os.Stderr, "concurrency must be positive")
		os.Exit(2)
	}

	// Single shared timestamp across the run — see adifBody comment.
	now := time.Now().UTC()
	qsoDate := now.Format("20060102")
	timeOn := now.Format("1504")

	// Ctrl-C interrupts and prints the partial summary.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpClient := &http.Client{
		Timeout: time.Duration(*timeoutSec) * time.Second,
		// MaxIdleConnsPerHost matters for sustained throughput against
		// a single target — the default of 2 forces TCP reconnects on
		// the third concurrent request.
		Transport: &http.Transport{
			MaxIdleConns:        *concurrency * 2,
			MaxIdleConnsPerHost: *concurrency * 2,
			MaxConnsPerHost:     *concurrency * 2,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	url := fmt.Sprintf("%s/v1/qso?logbook=%d", *target, *logbookID)

	jobs := make(chan int, *concurrency*2)
	results := make(chan result, *concurrency*2)

	var wg sync.WaitGroup
	for w := 0; w < *concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range jobs {
				if ctx.Err() != nil {
					return
				}
				call := *prefix + callsignSuffix(n)
				body := adifBody(call, *stationCallsign, qsoDate, timeOn)

				start := time.Now()
				req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBufferString(body))
				req.Header.Set("Content-Type", "application/x-adif")

				resp, err := httpClient.Do(req)
				elapsed := time.Since(start)

				if err != nil {
					results <- result{status: 0, latency: elapsed, err: err}
					continue
				}
				// Drain + close so the keepalive connection can be reused.
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				results <- result{status: resp.StatusCode, latency: elapsed}
			}
		}()
	}

	// Producer: feed integer indices to the workers.
	go func() {
		defer close(jobs)
		for i := 0; i < *count; i++ {
			select {
			case <-ctx.Done():
				return
			case jobs <- i:
			}
		}
	}()

	// Collector goroutine drains results into the slice. Using
	// channel-then-collect rather than a shared slice + mutex keeps
	// the per-request hot path lock-free.
	collected := make([]result, 0, *count)
	collectorDone := make(chan struct{})
	go func() {
		defer close(collectorDone)
		var stored, dup, rate, errs, other atomic.Int64
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		startedAt := time.Now()
		for {
			select {
			case r, ok := <-results:
				if !ok {
					fmt.Fprintf(os.Stderr, "collected=%d stored=%d dup=%d rate=%d err=%d other=%d in %s\n",
						len(collected), stored.Load(), dup.Load(), rate.Load(), errs.Load(), other.Load(),
						time.Since(startedAt).Round(time.Millisecond))
					return
				}
				collected = append(collected, r)
				switch {
				case r.err != nil:
					errs.Add(1)
				case r.status == 201:
					stored.Add(1)
				case r.status == 200:
					dup.Add(1)
				case r.status == 429:
					rate.Add(1)
				default:
					other.Add(1)
				}
			case <-ticker.C:
				fmt.Fprintf(os.Stderr, "[%5s] sent=%d stored=%d dup=%d rate=%d err=%d other=%d\n",
					time.Since(startedAt).Round(time.Second),
					len(collected), stored.Load(), dup.Load(), rate.Load(), errs.Load(), other.Load())
			}
		}
	}()

	wg.Wait()
	close(results)
	<-collectorDone

	// Summary.
	if len(collected) == 0 {
		fmt.Println("no requests completed (interrupted before any responses?)")
		return
	}

	latencies := make([]time.Duration, len(collected))
	var stored, dup, rate, errs, other int
	for i, r := range collected {
		latencies[i] = r.latency
		switch {
		case r.err != nil:
			errs++
		case r.status == 201:
			stored++
		case r.status == 200:
			dup++
		case r.status == 429:
			rate++
		default:
			other++
		}
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	p := func(q float64) time.Duration {
		idx := int(float64(len(latencies)-1) * q)
		return latencies[idx]
	}

	totalElapsed := latencies[len(latencies)-1]
	// Better elapsed metric: sum the wallclock window from first
	// request start to last response end. We don't track per-request
	// start timestamps so use total/concurrency*avg as an approximation
	// — acceptable for an order-of-magnitude reading.
	var sumLatency time.Duration
	for _, l := range latencies {
		sumLatency += l
	}
	avgLatency := sumLatency / time.Duration(len(latencies))
	approxWallclock := sumLatency / time.Duration(*concurrency)
	throughput := float64(len(collected)) / approxWallclock.Seconds()

	fmt.Println()
	fmt.Println("---- summary ----")
	fmt.Printf("total      : %d\n", len(collected))
	fmt.Printf("  stored   : %d (201)\n", stored)
	fmt.Printf("  duplicate: %d (200)\n", dup)
	fmt.Printf("  rate-lim : %d (429)\n", rate)
	fmt.Printf("  error    : %d (transport / timeout)\n", errs)
	fmt.Printf("  other    : %d (any non-2xx/429 status)\n", other)
	fmt.Printf("latency p50: %s\n", p(0.50).Round(time.Microsecond))
	fmt.Printf("latency p95: %s\n", p(0.95).Round(time.Microsecond))
	fmt.Printf("latency p99: %s\n", p(0.99).Round(time.Microsecond))
	fmt.Printf("latency avg: %s\n", avgLatency.Round(time.Microsecond))
	fmt.Printf("latency max: %s\n", totalElapsed.Round(time.Microsecond))
	fmt.Printf("throughput : ~%.1f req/sec (approx, sum/concurrency wallclock)\n", throughput)
}
