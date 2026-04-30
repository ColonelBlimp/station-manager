# v2 design — CAT codec performance analysis

**Status:** code-read 2026-04-30. **No changes made.** This document captures where the actual perf cost lives in `internal/cat`, ranked optimisation opportunities, and an explicit "measure before optimising" plan. Picked up because the operator was concerned about CAT-to-UI latency budgets and recalled something about a map and rehashing in the codec.

## Operator's latency threshold

End-to-end dial-spin to UI update: **<300 ms feels okay, >300 ms feels noticeably laggy.** This document and [ui-toolkit.md](ui-toolkit.md) both work to that budget.

## End-to-end latency budget (for context)

| Stage | Typical | Worst | Notes |
|---|---|---|---|
| Rig internal → CAT response | 5–30 ms | 50 ms+ | FTdx10 has known quirks (FD-prefix mystery, session 16) |
| Serial / USB transport | 5–15 ms | 30 ms | At 38400 baud, ~10 bytes round-trip |
| **USB-serial latency timer** | 1–16 ms | 40 ms+ | FTDI default 16 ms; CH340 unpredictable |
| **CAT poll interval** (avg = ½ period) | 50 ms (100 ms poll) | 250 ms (500 ms poll) | The biggest contributor when polling |
| Bridge → SSE dispatch (loopback) | <1 ms | 2–5 ms | TCP_NODELAY required else +40 ms |
| Bridge → SSE dispatch (LAN) | 1–10 ms | 20 ms | One Wi-Fi hop |
| Browser handle event + DOM update | <1 ms | 5 ms | Single text node, fast path |
| Frame sync to display | 8–17 ms | 17 ms | 60 Hz vsync; 120 Hz halves |

**Common case:** ~95–115 ms total. **Practical worst case:** ~200–230 ms.

## Where the codec actually fits

Codec time (parsing one framed CAT line in `cat.Decode`) currently takes on the order of **3–5 µs per call** (estimated from code inspection — *needs benchmarking*). Even with no optimisation, that's 0.005% of a 100 ms poll cycle. **The codec is not on the critical latency path for any realistic budget.**

The reasons to optimise the codec are different:

1. **GC pressure under multi-client load.** If multiple clients subscribe to the bridge and the bridge broadcasts decoded state to all of them, allocation overhead compounds across the fan-out. Per-frame allocations add up.
2. **Predictability / jitter floor.** Allocator + GC can introduce occasional 1–2 ms hiccups under pressure. Removing per-frame allocations makes the latency distribution tighter, which helps the dial *feel* smoother even though mean latency is unchanged.

Neither is currently a measured bottleneck. **Do not optimise blindly.** Measure first.

## What's actually in the code

There are two maps in `internal/cat`, and they're not the same kind of thing:

### `rigDB map[string]RigDefinition` — `internal/cat/rigdb.go:17`

Built once in `init()` from embedded JSON, never written to again. Looked up via `Lookup(id)` once per rig-connection lifecycle. **Cold path. Not relevant to per-frame perf.** No rehashing happens because there are no writes after init. The "map and rehashing" the operator was recalling is not this.

### `Status = map[string]string` — `internal/cat/codec.go:14`

Type alias for the decoded-tag map produced by `Decode`. **Created fresh on every `Decode` call** (`internal/cat/codec.go:55`: `status := Status{}`). Populated with 1–N entries (typically 1 for a frequency update), returned, GC'd. **This is the real allocation hot-spot.** At 10–20 Hz of incoming CAT frames during a dial spin, that's 10–20 maps/sec just for state-flag updates.

### `lookupState` — `internal/cat/codec.go:107–130`

The other hot path. Not a map at all — an O(N) linear scan with `bytes.EqualFold` per state:

```go
for _, s := range states {
    pl := len(s.Prefix)
    if pl == 0 || pl > len(line) { continue }
    if !bytes.EqualFold(line[:pl], []byte(s.Prefix)) { continue }
    ...
}
```

For a typical rig def with 20–30 states, every incoming frame does up to 30 case-insensitive byte compares before deciding which one matched. The "playing around with keys and shifting" the operator was recalling could apply here — see Tier 1 below.

## Ranked optimisation opportunities

### Tier 1 — likely measurable wins

**1. Stop allocating a fresh `Status` per `Decode` call.** Two API options:

- **Caller-owned map, cleared between uses:**
  ```go
  func Decode(def RigDefinition, line []byte, status Status) error
  ```
  Caller does `for k := range status { delete(status, k) }` between frames. Zero allocation per frame after warmup.

- **Return a small slice instead of a map** (preferred):
  ```go
  type Tag struct { Name, Value string }
  func Decode(def RigDefinition, line []byte, out []Tag) ([]Tag, error)
  ```
  Most decoded frames have 1–3 entries — a slice with cap 4 is dramatically faster than a map at that scale, plus the caller usually iterates once anyway. The current `Status = map[string]string` API was chosen for v1-compat ergonomics (per the doc comment in `codec.go`), not perf.

**This is the single biggest win available.** Map allocation + insertion + GC on every frame is the actual cost.

**2. Index states by first byte instead of linear-scanning all of them.** A `[256][]State` array (2 KiB) keyed on the uppercased first byte gives O(1) bucket selection followed by a linear scan over a much smaller bucket — typically 1–3 states per first letter. Build it once at rig-def load time. Likely 5–10× faster for `lookupState`.

For CAT prefixes (short ASCII strings) this can go further: pack the first 1–8 bytes of each prefix into a `uint64` at load time, then compare the same packing of the incoming line head with a single CPU CMP. Replaces `bytes.EqualFold`'s loop with one machine instruction. **Worth it only if profiling shows `lookupState` matters; otherwise overkill.**

**3. Pre-uppercase prefixes at load time so `bytes.EqualFold` can be dropped.** `bytes.EqualFold` walks both inputs handling Unicode folding tables — for ASCII-only CAT prefixes that's pure overhead. If the rig-load step normalises every `State.Prefix` to uppercase, the hot path becomes:

```go
upHead := bytesToUpperASCII(line[:pl], buf)  // into reusable buffer
if bytes.Equal(upHead, s.Prefix) { ... }
```

Roughly 3–5× faster than `EqualFold` for short ASCII strings.

### Tier 2 — minor but free with the Tier 1 refactor

- The implicit `[]byte → string` conversion `status[m.Tag] = slice` (`codec.go:66`) allocates a string per marker. If `Status` becomes a `[]Tag` and the value is kept as `[]byte`, callers that don't actually need a string can skip the allocation.
- `string(line[bestLen:])` in `lookupState` (`codec.go:129`) allocates the entire tail as a string just so `Decode` can re-slice it. Pass `[]byte` through; only stringify at the leaf where needed.

### Tier 3 — leave alone

- `Encode` doing linear scan over `def.Commands` (`codec.go:92`). Rare path, fine as is.
- `fmt.Appendf` allocation in `Encode`. Same.
- Marker `ValueMappings` lookup linear scan (`codec.go:70–75`). Tables are 2–8 entries; linear is fastest at that size.

## Measurement-first plan

`internal/serial/serial_bench_test.go` exists but **nothing benchmarks `cat.Decode`**. Add benchmarks before optimising — otherwise it's guesswork:

```go
// internal/cat/codec_bench_test.go
package cat

import "testing"

func BenchmarkDecode(b *testing.B) {
    def, ok := Lookup("ftdx10")
    if !ok { b.Fatal("rig not found") }
    line := []byte("FA014250000;")
    b.ReportAllocs()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, _ = Decode(def, line)
    }
}

func BenchmarkLookupState(b *testing.B) {
    def, _ := Lookup("ftdx10")
    line := []byte("FA014250000;")
    b.ReportAllocs()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, _, _ = lookupState(line, def.States)
    }
}
```

Run with `-benchmem`. The `allocs/op` number tells whether map allocation is dominating (likely 2–4 allocs/op currently). After the Tier 1 refactor it should be 0 allocs/op for the steady state.

Recommended order:

1. Add the benchmarks. Establish baseline allocs/op and ns/op.
2. Refactor `Status` to a slice-of-tags (or caller-owned map). Re-bench. Expect this to dominate the win.
3. *Maybe* add the first-byte index for States. Re-bench. If the win is <20% under realistic load, skip it.
4. Skip the uint64-packing trick unless step 3 shows `lookupState` is still meaningful in the profile.

## Decision

**Don't optimise yet.** The codec isn't on the critical latency path. The real latency dominators are bridge-side (CAT poll cadence, USB-serial latency timer, async/auto-tx mode, TCP_NODELAY). Address those first if there's a perceptible lag problem.

If/when multi-client fan-out is implemented and GC pressure shows up in `runtime/trace`, come back to this document and execute the Tier 1 refactor.

## Quick perf wins on the bridge side (unrelated to codec)

For reference, since these are the actual latency dominators:

1. **Poll interval at 100 ms** by default. Some rigs handle 50 ms; safer to start at 100. Configurable per rig.
2. **Use rig's async/auto-tx mode wherever supported** (Icom CI-V "transceive on", Kenwood AI commands, K3 auto-info). Eliminates polling latency entirely.
3. **Drop USB-serial latency timer to 1 ms** (Linux: udev rule on FTDI devices; Windows: registry).
4. **Set `TCP_NODELAY` on the bridge's HTTP socket** — without it Nagle can coalesce small SSE events for ~40 ms.

## FTdx10 FD-prefix mystery

Open since session 16. Investigate when revisiting CAT perf — may be related to whichever commands the rig is sending unexpectedly that contribute to confused parser cycles. Run a serial trace, capture the unknown-prefix lines, decide whether to add them to the rig def or filter them at the transport layer.

## Cross-references

- `internal/cat/codec.go:14` — `Status` map type alias (the real hot-spot)
- `internal/cat/codec.go:55` — fresh `Status{}` allocation per `Decode`
- `internal/cat/codec.go:107` — `lookupState` linear scan
- `internal/cat/rigdb.go:17` — `rigDB` (cold path, not relevant to per-frame perf)
- `internal/serial/serial_bench_test.go` — existing serial benchmarks (codec equivalent missing)
- `cat-serial-reuse.md` — broader cat/serial reuse design
- [topology.md](topology.md) — bridge owns CAT; daemon doesn't
- [ui-toolkit.md](ui-toolkit.md) — why the UI toolkit's contribution to latency is in the noise
