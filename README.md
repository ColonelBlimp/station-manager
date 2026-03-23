# Station Manager: adapter package

A high-performance, thread-safe Go library for struct-to-struct field adaptation with intelligent handling of `AdditionalData` fields, pluggable converters, and validation hooks.

## Features

- Direct Field Copying using name or JSON tag match
- Field Conversion via scoped converter functions (global, destination-type, (src,dst) pair)
- Validation Hooks (same scoping precedence as converters) for enforcing invariants post-conversion
- Smart `AdditionalData` marshal/unmarshal (opt-in/opt-out flags)
- Copy-on-Write atomic registries (no locks on fast path)
- Metadata caching & optional warming (zero allocations per field after warmup)
- Builder API for ergonomic configuration
- High test coverage (85%+) & concurrency tests

## Installation

```bash
go get github.com/Station-Manager/adapters
```

## API

- Core: `Into(dst, src) error` copies from src struct to dst struct. Both must be pointers to structs.
- Generics helpers:
  - `Copy[T any](a *Adapter, dst *T, src any) error`
  - `AdaptTo[T any](a *Adapter, src any) (*T, error)`
  - `Make[T any](a *Adapter, src any) (T, error)`
- Registration:
  - Converters: `RegisterConverter`, `RegisterConverterFor`, `RegisterConverterForPair`
  - Validators: `RegisterValidator`, `RegisterValidatorFor`, `RegisterValidatorForPair`
  - Batch: `Batch(func(*RegistryBatch))` to group registrations

### Tags

- Prefer `json:"name"` tags for field name matching.
- `adapter:"ignore"` skips a field.
- `adapter:"additional"` marks a field of type `null.JSON` or `sqlboiler/types.JSON` as AdditionalData.

### AdditionalData semantics

- Direct fields win by default (PreferFields). Switch to `PreferAdditionalData` via `WithOverwritePolicy`.
- Case-insensitive key matching is opt-in: `WithCaseInsensitiveAdditionalData(true)`.
- Control marshaling/unmarshaling with `WithDisableMarshalAdditionalData` and `WithDisableUnmarshalAdditionalData`.

## Performance

- Metadata is cached; call `WarmMetadata(samples...)` to prebuild for hot types.
- Case-insensitive matching uses precomputed lowercase maps to avoid per-key scans.
- AdditionalData map is lazily allocated when needed to reduce allocations.

See `PROFILING.md` for profiling commands and tuning notes.

## Quick Start

```go
package main

import (
    "fmt"
    "github.com/Station-Manager/adapters"
)

// Basic usage: destination-first Into(dst, src)
type ExampleSrc struct{ Name string }
type ExampleDst struct{ Name string }

func main() {
    adapter := adapters.New()
    src := ExampleSrc{Name: "Alice"}
    var dst ExampleDst
    if err := adapter.Into(&dst, &src); err != nil {
        panic(err)
    }
    fmt.Println(dst.Name)
}
```

### Generics helpers

```go
package main

import (
    "fmt"
    "github.com/Station-Manager/adapters"
)

// Examples showing package-level generic helpers: AdaptTo, Copy, Make
type Src struct{ Name string }
type Dest struct{ Name string }

func exampleAdaptTo() {
    a := adapters.New()
    s := Src{Name: "Bob"}
    out, err := adapters.AdaptTo[Dest](a, &s)
    if err != nil {
        panic(err)
    }
    fmt.Println("AdaptTo ->", out.Name)
}

func exampleCopy() {
    a := adapters.New()
    s := Src{Name: "Claire"}
    var d Dest
    if err := adapters.Copy[Dest](a, &d, &s); err != nil {
        panic(err)
    }
    fmt.Println("Copy ->", d.Name)
}

func exampleMake() {
    a := adapters.New()
    s := Src{Name: "Dan"}
    v, err := adapters.Make[Dest](a, &s)
    if err != nil {
        panic(err)
    }
    fmt.Println("Make ->", v.Name)
}

func main() {
    exampleAdaptTo()
    exampleCopy()
    exampleMake()
}
```

### Batch registration

```go
adapter.Batch(func(r *adapters.RegistryBatch){
    r.GlobalConverter("Temperature", conv)
    r.GlobalValidator("Name", notEmpty)
})
```

### AdditionalData field rename

```go
type D struct {
    Extras null.JSON `adapter:"additional" json:"extras"`
}
```

### Converters

```go
adapter.RegisterConverter("Temperature", func(v any) (any, error) {
    c := v.(float64)
    return (c*9/5)+32, nil // Celsius -> Fahrenheit
})
```

### Validators

Validators run after setting a field (and after any converter). Return an error to abort adaptation.

```go
adapter.RegisterValidator("Name", func(v any) error {
    if len(v.(string)) == 0 { return errors.New("name required") }
    return nil
})
```

### Builder API

```go
ad := adapters.NewBuilder().
    WithOptions(adapters.WithCaseInsensitiveAdditionalData(true)).
    AddConverter("Name", adapters.MapString(strings.ToUpper)).
    AddValidator("Name", func(v any) error { if v.(string)=="" { return errors.New("empty") }; return nil }).
    Build()
```

### AdditionalData Controls

Options:
- `WithDisableUnmarshalAdditionalData(true)` skip source AdditionalData expansion
- `WithDisableMarshalAdditionalData(true)` skip marshaling remaining fields into destination AdditionalData
- `WithIncludeZeroValues(true)` include zero values when marshaling
- `WithCaseInsensitiveAdditionalData(true)` case-insensitive key matching
- `WithOverwritePolicy(PreferAdditionalData)` allow AdditionalData to overwrite direct fields

### JSON Tag Precedence

Field matching order:
1. Exact field name match
2. JSON tag name match
3. (If case-insensitive option on) case-insensitive variations

Adapter-specific struct tags (`adapter:"ignore"`) are minimal and only used to ignore fields. Prefer JSON tags for naming.

### Validation + Conversion Precedence

For both converters and validators: pair > destination-type > global.

### Opting Out of AdditionalData

Use the disable options (above). If disabled, no JSON marshal/unmarshal occurs.

### Metadata Warmup

```go
adapter.WarmMetadata(ExampleSrc{}, ExampleDst{})
```

Pre-builds metadata to reduce first-call latency in hot paths.

## Migration (vNext API change)

Previous API used `Adapt(dst, src)` with source first in some call sites. The new API standardizes on `Into(&dst, &src)` (destination first) for consistency with common Go patterns (io.Reader/io.Writer style). To migrate:
1. Replace `adapter.Adapt(a, b)` with `adapter.Into(b, a)`.
2. Update any generic wrappers to use `Copy/AdaptTo/Make` helpers.
3. Remove any direct field reflection; prefer batch registrations if adding many converters.

## BuildPlan Cache

A build-plan cache accelerates repeated adaptations between the same (src,dst) type pair. Each plan stores:
- Field index paths
- Pre-resolved converter & validator functions (respecting precedence: pair > dst > global)
- AdditionalData presence and indices
- Adapter generation stamp (invalidates automatically when registries change)

This reduces per-field map lookups and dynamic converter resolution. Benchmarks (Intel i3-10100F) improvements:
- BasicFieldCopy: ~1450ns -> ~508ns
- WithConverter: ~1710ns -> ~564ns
- LargeStruct: ~5140ns -> ~1560ns
- Concurrent: ~432ns -> ~167ns

No public API change was required; plans are transparent. For latency-sensitive services, pre-warm metadata and plans early:

```go
ad := adapters.New()
ad.WarmMetadata(ExampleSrc{}, ExampleDst{}) // metadata
// first Into call will create plan; optionally perform a single dry run during startup
_ = ad.Into(&ExampleDst{}, &ExampleSrc{})
```

A future helper `WarmPlans(pairs...)` could be added if needed.

## Performance (updated)

- Metadata & plan caches avoid repeated reflection and map lookups.
- Lowercase maps eliminate O(n) scans for case-insensitive AdditionalData.
- Lazy AdditionalData map & marshal skip allocations when not needed.
- Copy-on-write registries keep the hot path lock-free.

## Error Handling

Adapt returns an error if:
- src or dst nil
- Arguments not pointers to structs
- Converter returns error
- Validator returns error
- AdditionalData contains invalid JSON

## Concurrency

Registries use atomic pointer swaps with copy-on-write maps; Adapt performs only reads (no locks). Registering converters/validators is safe concurrently with adaptations.

## Performance

Fast path operations avoid reflection map lookups by cached metadata.

## License

See LICENSE.
