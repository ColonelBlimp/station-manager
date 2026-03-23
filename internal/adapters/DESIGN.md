# Design Goals

1. Copy exported struct fields when names and types match
2. Apply converters when names match and a converter is registered (by field name). Precedence: pair > destination-type > global
3. Marshal remaining source fields to `dst.AdditionalData` (null.JSON or sqlboiler/types.JSON) when present
4. Unmarshal `src.AdditionalData` (null.JSON or sqlboiler/types.JSON) into matching destination fields; by default, do not overwrite fields already set by direct mapping
5. Prefer JSON tags (`json:"name"`) for field name matching; minimal adapter tags: `adapter:"ignore"` and `adapter:"additional"`
6. Prioritize speed, thread-safety, and memory efficiency
7. High test coverage (85%+), including concurrency tests
8. Comprehensive documentation and examples
9. Optimize performance using benchmarks and profiling; maintain a fast path with caches

## API

- Core: `Into(dst, src) error` (destination first). Both are pointers to structs.
- Generics: `Copy`, `AdaptTo`, `Make` helpers.
- Builder: scope-aware registration and batching.

## Implementation Notes

- Metadata cache of fields, names, and JSON tags
- Precomputed lowercase maps for case-insensitive AdditionalData key lookup (opt-in)
- BuildPlan cache per (src,dst,gen): field index paths, pre-resolved converter/validator functions, AdditionalData indices
- Copy-on-write registries for converters/validators; atomic generation increments invalidate plans
- Pooled small maps for processed/dstSet bookkeeping
- Lazy allocation of AdditionalData map when marshaling

## Tags

- `json:"name"` preferred for naming
- `adapter:"ignore"` to skip a field
- `adapter:"additional"` to mark null.JSON or sqlboiler/types.JSON as AdditionalData

## Thread Safety

- Adaptation path is lock-free (reads only); registry updates swap atomically

## Examples

See `README.md` for usage, builder, and generics examples.

---

1. Copy exported struct fields if they are the same name and type
2. Copy and convert exported struct fields if they have the same name but also have a converter function registered for that field (registration is based on field name only)
3. If the destination struct has had all its exported fields assigned a value (using point 1 above), and also has field `AdditionalData` on type `null.JSON` from `github.com/aarondl/null/v8`, then the remaining fields from the source struct are marshaled to JSON and stored in the `AdditionalData` field.
4. If the source struct has an exported field with the name `AdditionalData` and the type is `null.JSON` from `github.com/aarondl/null/v8`, then it should be it should be unmarshalled (JSON) and the data used to populate the destination struct. However, exported fields fulfilling the criteria in point 1 above should not be overwritten, and data convertion is point 2 above should also be applied.
5. If the source struct has an exported field with the name `AdditionalData` and the type is not `null.JSON` from `github.com/aarondl/null/v8`, then it should be ignored.
6. The code should be built for speed, thread safety and memory safety.
7. The code should be tested to 85%+
8. The code should be documented, with examples.
9. The code should be optimized for performance, with benchmarks and profiling used to identify and fix bottlenecks.

Example structs to be copied are:

    `database/postgres/models/Qso` <-> `types/Qso`
    `database/sqlite/models/Qso` <-> `types/Qso`
