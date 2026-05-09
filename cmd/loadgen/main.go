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

// suffixWidth picks the smallest fixed-width base-26 alphabetic suffix
// that fits `count` distinct values. Returned width is 3 (≤17576), 4
// (≤456976), or 5 (≤11881376). Anything above 5-wide is rejected at
// flag-parse time — five letters is already 11M unique callsigns,
// well past any sensible stress run.
func suffixWidth(count int) int {
	switch {
	case count <= 26*26*26:
		return 3
	case count <= 26*26*26*26:
		return 4
	default:
		return 5
	}
}

// callsignSuffix maps an integer 0..(26^width)-1 to a fixed-width
// alphabetic base-26 suffix. n=0/width=3 → "AAA"; n=25/width=3 →
// "AAZ"; n=17575/width=3 → "ZZZ". Width is chosen by suffixWidth so
// the suffix has enough capacity for the count requested.
func callsignSuffix(n, width int) string {
	out := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		out[i] = byte('A' + n%26)
		n /= 26
	}
	return string(out)
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
// being absent. endpoint tags which API path produced the result so
// the summary can break stats out per-endpoint in mixed-workload mode.
type result struct {
	endpoint string // "submit" / "enrich" / "history"
	status   int
	latency  time.Duration
	err      error
}

// doRequest runs a single HTTP call and emits a result. Centralising
// the do-and-time pattern means each per-iteration step in mixed mode
// stays a one-liner and the keepalive-conn drain is consistent.
func doRequest(ctx context.Context, c *http.Client, method, url, ct, body, endpoint string, results chan<- result) {
	var reader *bytes.Buffer
	if body != "" {
		reader = bytes.NewBufferString(body)
	}
	var req *http.Request
	if reader != nil {
		req, _ = http.NewRequestWithContext(ctx, method, url, reader)
	} else {
		req, _ = http.NewRequestWithContext(ctx, method, url, nil)
	}
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}

	start := time.Now()
	resp, err := c.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		results <- result{endpoint: endpoint, status: 0, latency: elapsed, err: err}
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	results <- result{endpoint: endpoint, status: resp.StatusCode, latency: elapsed}
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
		mode            = flag.String("mode", "submit", "workload mode: 'submit' = POST /v1/qso only; 'mixed' = enrich+history+submit per iteration (matches operator's Tab→submit flow)")
		sseSubscribe    = flag.Bool("sse", false, "open one /v1/events SSE subscriber in the background to test event-broadcast cost under load")
	)
	flag.Parse()

	if *count <= 0 {
		fmt.Fprintln(os.Stderr, "count must be positive")
		os.Exit(2)
	}
	if *count > 26*26*26*26*26 {
		fmt.Fprintf(os.Stderr, "count=%d exceeds 11881376 (the 5-letter suffix cap)\n", *count)
		os.Exit(2)
	}
	if *concurrency <= 0 {
		fmt.Fprintln(os.Stderr, "concurrency must be positive")
		os.Exit(2)
	}
	if *mode != "submit" && *mode != "mixed" {
		fmt.Fprintf(os.Stderr, "mode=%q invalid; must be 'submit' or 'mixed'\n", *mode)
		os.Exit(2)
	}
	width := suffixWidth(*count)

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

	submitURL := fmt.Sprintf("%s/v1/qso?logbook=%d", *target, *logbookID)

	// Channel sizing: in mixed mode each iteration emits 3 results,
	// so the buffer is sized 3× to absorb the extra fan-out without
	// blocking workers.
	resultsBuf := *concurrency * 2
	if *mode == "mixed" {
		resultsBuf *= 3
	}
	jobs := make(chan int, *concurrency*2)
	results := make(chan result, resultsBuf)

	// Optional SSE subscriber. Opens a single connection to /v1/events
	// in a background goroutine and discards the byte stream — the
	// daemon's events.Hub does a per-subscriber write on every QSO
	// stored, so this test surfaces broadcast cost under load. The
	// subscriber doesn't emit results (it's not a request to measure;
	// it's a daemon-side load contributor). Cancelled on ctx.Done.
	if *sseSubscribe {
		go func() {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, *target+"/v1/events", nil)
			req.Header.Set("Accept", "text/event-stream")
			resp, err := httpClient.Do(req)
			if err != nil {
				fmt.Fprintf(os.Stderr, "sse subscriber: dial failed: %v\n", err)
				return
			}
			defer resp.Body.Close()
			fmt.Fprintf(os.Stderr, "sse subscriber: connected (status=%d)\n", resp.StatusCode)
			_, _ = io.Copy(io.Discard, resp.Body)
		}()
	}

	var wg sync.WaitGroup
	for w := 0; w < *concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range jobs {
				if ctx.Err() != nil {
					return
				}
				call := *prefix + callsignSuffix(n, width)

				if *mode == "mixed" {
					// Operator's Tab-flow: enrich the contacted call,
					// fetch any prior history with that call, then
					// submit. enrich+history hit the read paths (cache
					// hit for repeated callsigns; cold lookup the first
					// time) and run sequentially per worker — concurrent
					// fan-out within a single QSO would overstate the
					// SPA's actual behaviour, which is also sequential.
					enrichURL := fmt.Sprintf("%s/v1/enrich/callsign?call=%s", *target, call)
					historyURL := fmt.Sprintf("%s/v1/contact-history?call=%s", *target, call)
					doRequest(ctx, httpClient, http.MethodGet, enrichURL, "", "", "enrich", results)
					doRequest(ctx, httpClient, http.MethodGet, historyURL, "", "", "history", results)
				}

				body := adifBody(call, *stationCallsign, qsoDate, timeOn)
				doRequest(ctx, httpClient, http.MethodPost, submitURL, "application/x-adif", body, "submit", results)
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
	// the per-request hot path lock-free. Live counters are kept per
	// endpoint so the periodic ticker can show progress that's
	// meaningful in either mode (submit-only or mixed).
	collected := make([]result, 0, *count)
	collectorDone := make(chan struct{})
	go func() {
		defer close(collectorDone)
		var live = map[string]*atomic.Int64{
			"submit":  {},
			"enrich":  {},
			"history": {},
		}
		var errs atomic.Int64
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		startedAt := time.Now()
		for {
			select {
			case r, ok := <-results:
				if !ok {
					fmt.Fprintf(os.Stderr, "collected=%d submit=%d enrich=%d history=%d err=%d in %s\n",
						len(collected),
						live["submit"].Load(), live["enrich"].Load(), live["history"].Load(),
						errs.Load(),
						time.Since(startedAt).Round(time.Millisecond))
					return
				}
				collected = append(collected, r)
				if r.err != nil {
					errs.Add(1)
				} else if c, ok := live[r.endpoint]; ok {
					c.Add(1)
				}
			case <-ticker.C:
				fmt.Fprintf(os.Stderr, "[%5s] total=%d submit=%d enrich=%d history=%d err=%d\n",
					time.Since(startedAt).Round(time.Second),
					len(collected),
					live["submit"].Load(), live["enrich"].Load(), live["history"].Load(),
					errs.Load())
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

	// Bucket results by endpoint for per-path stats. Submit-only mode
	// puts everything in one bucket; mixed mode splits across three.
	type bucket struct {
		latencies   []time.Duration
		ok          int // 2xx
		errs        int
		other       int // non-2xx (incl. 429 — broken out below for submit only)
		rate        int // 429
		stored, dup int // submit-specific
	}
	buckets := map[string]*bucket{}
	endpointOrder := []string{"submit", "enrich", "history"}

	for _, r := range collected {
		b, ok := buckets[r.endpoint]
		if !ok {
			b = &bucket{}
			buckets[r.endpoint] = b
		}
		b.latencies = append(b.latencies, r.latency)
		switch {
		case r.err != nil:
			b.errs++
		case r.status == 201:
			b.ok++
			b.stored++
		case r.status == 200:
			b.ok++
			if r.endpoint == "submit" {
				b.dup++
			}
		case r.status == 429:
			b.rate++
			b.other++
		default:
			b.other++
		}
	}

	pctile := func(latencies []time.Duration, q float64) time.Duration {
		if len(latencies) == 0 {
			return 0
		}
		idx := int(float64(len(latencies)-1) * q)
		return latencies[idx]
	}

	fmt.Println()
	fmt.Println("---- summary ----")
	fmt.Printf("mode       : %s\n", *mode)
	fmt.Printf("total reqs : %d\n", len(collected))

	// Per-endpoint detail block. Loop in stable order so the output
	// reads the same across runs.
	for _, ep := range endpointOrder {
		b, ok := buckets[ep]
		if !ok || len(b.latencies) == 0 {
			continue
		}
		sort.Slice(b.latencies, func(i, j int) bool { return b.latencies[i] < b.latencies[j] })
		var sum time.Duration
		for _, l := range b.latencies {
			sum += l
		}
		avg := sum / time.Duration(len(b.latencies))
		// Throughput per endpoint approximated as count / (sum/conc).
		// In mixed mode this is the per-path throughput within the
		// shared concurrency pool — useful for spotting which path
		// dominates a worker's time budget.
		approxWall := sum / time.Duration(*concurrency)
		thr := float64(len(b.latencies)) / approxWall.Seconds()

		fmt.Printf("\n[%s]\n", ep)
		fmt.Printf("  count    : %d\n", len(b.latencies))
		if ep == "submit" {
			fmt.Printf("  stored   : %d (201)\n", b.stored)
			fmt.Printf("  duplicate: %d (200)\n", b.dup)
			fmt.Printf("  rate-lim : %d (429)\n", b.rate)
		} else {
			fmt.Printf("  ok 2xx   : %d\n", b.ok)
			if b.rate > 0 {
				fmt.Printf("  rate-lim : %d (429)\n", b.rate)
			}
		}
		fmt.Printf("  error    : %d (transport / timeout)\n", b.errs)
		fmt.Printf("  other    : %d (non-2xx / non-429)\n", b.other-b.rate)
		fmt.Printf("  p50      : %s\n", pctile(b.latencies, 0.50).Round(time.Microsecond))
		fmt.Printf("  p95      : %s\n", pctile(b.latencies, 0.95).Round(time.Microsecond))
		fmt.Printf("  p99      : %s\n", pctile(b.latencies, 0.99).Round(time.Microsecond))
		fmt.Printf("  avg      : %s\n", avg.Round(time.Microsecond))
		fmt.Printf("  max      : %s\n", b.latencies[len(b.latencies)-1].Round(time.Microsecond))
		fmt.Printf("  throughput: ~%.1f req/sec (approx, sum/concurrency wallclock)\n", thr)
	}
}
