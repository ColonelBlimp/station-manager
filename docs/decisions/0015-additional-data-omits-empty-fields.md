---
number: 0015
title: additional_data blob omits empty fields
status: Accepted
date: 2026-05-03
---

# 0015 — additional_data blob omits empty fields

## Context

The `qso.additional_data` and `contacted_station.additional_data` JSON blobs are written by `json.Marshal(types.Qso)` / `json.Marshal(types.ContactedStation)` and read back via `json.Unmarshal` in the adapter layer (`internal/database/sqlite/adapters/model_to_type.go`). The blob's purpose is documented in `docs/v1-analysis/invariants.md` — "the `additional_data` JSON blob absorbs ADIF spec evolution," carrying every ADIF field that isn't promoted to a dedicated SQLite column.

Today, the blob is dominated by `"field":""` noise. A real QSO with eight or so user-set fields produces a record with ~80 keys, the rest empty. Inspection of QSO id=2 (logged 2026-05-03) showed the actual signal-to-noise ratio: 18 keys carrying values, ~60 keys carrying `""`, plus a fully-zero `country_details` object and `contact_history: null`. The asymmetry is structural — `types.QsoDetails` has eight fields tagged `omitempty` (Band, Freq, Mode, QsoDate, RstSent, RstRcvd, TimeOn, TimeOff) and twenty-three without. The package comment says the existing eight are tagged "for the importer tool, which marshalls the additional data using `json.Marshal` … not set will be omitted from the JSON output, and this will not trigger the duplicate check at the database level." That rationale applies to *every* field, not just dedupe-key inputs.

This is a textbook instance of the asymmetric round-trip lesson from `docs/v1-analysis/lessons-for-v2.md` — the partial `omitempty` tagging is the code telling us the rule was never finished.

## Decision

Every string and numeric field across `types.QsoDetails`, `types.ContactedStation`, `types.LoggingStation`, `types.Qsl`, `types.Country`, and the upload-stamp fields on `types.Qso` carries `,omitempty`. The `contact_history` slice on `types.Qso` is also tagged `,omitempty` so empty arrays disappear. `country_details` retains its current value-typed embed (no pointer change) — with omitempty applied to every inner field, an unenriched country marshals as `{}` rather than 14 empty strings, which is acceptable for now.

## Alternatives considered

### Keep the current asymmetric tagging

Status quo. Rejected because the asymmetric `omitempty` set is documented as "for dedupe inputs," but every other field has the same write-only-when-set rationale; the partial tagging is an unfinished pattern, not an intentional design. Inspection-time noise is real and accumulating: blob size scales with the field count, not with operator usage.

### Drop the value-typed `CountryDetails` embed in favor of `*Country`

A pointer would let us omit `country_details` entirely when enrichment hasn't run, instead of emitting `{}`. Rejected for this ADR because it's a wider blast radius — the adapter's `qso.CountryDetails.X = ...` assignments would need nil-checks in every call site, the SPA's read path would need optional-chaining, and the marginal noise from a literal `{}` is small. Worth revisiting if a follow-up review finds the empty-object pattern noisy in real use.

### Custom `MarshalJSON` on each struct

A hand-written marshaller could enforce the rule programmatically without per-field tags. Rejected — adds maintenance cost (a marshaller per struct), prevents `gofmt`-friendly tag inspection, and obscures the simple "field-is-tagged-omitempty" mental model that the rest of the codebase relies on. Tags are the canonical Go answer.

### Migrate the existing two rows to remove their empty-string keys

Rejected per operator decision: the project is in dev, the operator will blow the database away as part of this change. No migration code is justified.

## Consequences

- **Blob size shrinks ~75% on a typical QSO.** Disk and IO win is small in absolute terms (kilobytes per row), but readability of stored rows during debugging is materially better.
- **Read path is unchanged.** `json.Unmarshal` already treats absent fields as zero-valued, so the adapter (`QsoModelToType`, `ContactedStationModelToType`) sees identical Go-side state whether a field was absent or `""`. No adapter change.
- **SQL `json_set` stamps still work.** `MarkUploadSuccessWithAdifStamp` writes `sm_qso_upload_status` / `sm_qso_upload_date` / `<prefix>_qso_upload_status` keys via `json_set` post-insert; SQLite's `json_set` adds the key whether or not it pre-existed. No SQL change.
- **Existing test assertions stand.** All `additional_data`-asserting tests in `service_test.go` and `adapters_test.go` check for *key presence* via `strings.Contains` (after a stamp call) or `assert.NotEmpty`; none count keys or pin full blob content. `go test ./... -race` is expected green without test edits.
- **Package-level comments are now incorrect.** The `qso_details.go` and `contacted_station.go` package-level "Some fields are marked as 'omitempty'..." comments were written when the rule was partial; they get rewritten to describe the new uniform rule.
- **The dedupe-trigger rationale is preserved automatically.** The original concern (an empty `MODE` field from the importer would emit `"mode":""` and break dedupe-key match) is satisfied for every field now, not just the original eight.

## Triggers to revisit

- If a future consumer (a query layer, a debugging script, a future master daemon per ADR 0014) needs to distinguish "field absent because never written" from "field present but empty string," the rule no longer fits and we'd need either a sentinel pattern or re-introduction of partial tagging at specific fields. Provenance metadata under ADR 0014's prep item #4 is a candidate consumer — when that lands, audit it against this rule.
- If the blob ever grows a field whose zero value carries semantic meaning (e.g. an explicit `false` that differs from "field unset"), that field needs to drop `omitempty` and the rule becomes "omitempty by default, except for X" — at which point this ADR gets a status flip to Superseded.
- If `country_details: {}` becomes a noise complaint in real use, revisit the `*Country` pointer alternative above.

## References

- `docs/v1-analysis/invariants.md` — "The `additional_data` JSON blob absorbs ADIF spec evolution"
- `docs/v1-analysis/lessons-for-v2.md` — "Asymmetric round-trips are a clue"
- `internal/types/qso.go`, `qso_details.go`, `contacted_station.go`, `logging_station.go`, `qsl.go`, `country.go` — the structs whose tags are updated by this decision
- `internal/database/sqlite/adapters/model_to_type.go` — the read path (unchanged)
- `internal/database/sqlite/api_context.go` — `MarkUploadSuccessWithAdifStamp` (unchanged)
- ADR 0014 (`0014-upstream-forwarding-deferred.md`) — provenance metadata in `additional_data` is the most likely future consumer of the blob's shape
