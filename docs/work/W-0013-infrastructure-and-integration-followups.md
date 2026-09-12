# W-0013 — Harden deferred infrastructure and integration seams

**Status:** Deferred — trigger or adjacent work required
**Selected:** Not selected
**Outcome:** Deferred infrastructure changes land only when a real consumer opens the seam, with
bounded resource use and no accidental hardware, credential, or network dependency in normal tests.

## Inventory

- one multiplexed SPA event stream, when separate SSE ownership becomes a measured maintenance
  cost;
- `/v1/hardware` per-direction audio availability and bounded enumeration caching;
- CI-V `sets_state` value-compatibility validation;
- `internal/iocdi` concurrency and build-time contract hardening;
- multi-tab operating ownership and explicit takeover, building on the shipped awareness banner;
- opt-in WSJT-X-compatible UDP decode broadcast as an independent non-blocking decode sink, after
  the recipient/filtering contract is verified from authoritative protocol documentation;
- spot-submitter registry only when a second destination exists;
- PSK Reporter optional upload attributes (operator question 2026-09-12; not selected): the
  uploader already sends every field the protocol asks for — sender records `senderCallsign`,
  `frequency`, `sNR`, `iMD` (always 0), `mode`, `informationSource` = 1, `senderLocator` (the decoded
  grid, empty when unknown), `flowStartSeconds`; the receiver record `receiverCallsign`,
  `receiverLocator` (`my_gridsquare`), `decoderSoftware` ("StationManager " + version) and
  `antennaInformation` (`my_antenna`) — `internal/pskreporter/ipfix.go`,
  `cmd/smd/lifecycle_adapters.go`, checked against the attribute table and the "should contain"
  lists at pskreporter.info/pskdev.html (fetched 2026-09-12). Defined there but not sent, all optional
  and outside those lists: `rigInformation` (30351.13, "a description of the Rig in use, most
  significant information first so entries can be grouped"), `persistentIdentifier` (30351.12, a
  random string "that may be used in the future as a primitive form of security" — distinct from
  the persisted IPFIX observation-domain id in `pskreporter.id`), `messageBits` (30351.14, the raw
  decoded bits), `deltaT` (30351.15, 100 µs units) and `fractionalFrequency` (30351.16). The marker
  popup's "Last LoTW upload" and "eQSL Authenticity Guaranteed" lines are NOT upload fields: the
  retrieve API returns them as server-side per-sender attributes (`senderLotwUpload`,
  `senderEqslAuthGuar`, beside `senderDXCC`/`senderRegion`), so nothing a reporter sends produces
  them; their source lists are not named on the developer page (inference: the public LoTW user
  activity file and the eQSL AG list). Not verified whether the site displays `rigInformation` or
  `antennaInformation` anywhere — the popup and the retrieve output show neither. Only candidate that
  changes the operator's own marker: locator precision (the site shows other stations' 8-character
  locators taken from their own receiver records; ours is 6) — check every consumer of
  `my_gridsquare` (ADIF `MY_GRIDSQUARE`, the FT8 CQ grid truncation) before lengthening it;
- config hot reload only as a deliberately scoped lifecycle consumer;
- datastore switch at runtime (open another log database file) only as the mechanism for the
  operator's data-files requirement recorded in W-0012 (2026-09-12); the restart path is the first
  candidate, an in-process swap the second and the trigger for W-0009's LC-5;
- before multi-instance SM Cloud: explicit migrate-only/serve-only operation and verification of
  concurrent migration locking. The single-instance boot migration remains current behavior.

## Verification boundary

Integration output must be bounded and non-blocking at its producer. Tests use local in-memory or
loopback fixtures and make dropped/slow consumers observable without delaying logging or decoding.
No ordinary command contacts an external service, opens operator hardware, or changes a live
database.

## References

- [`ADR 0040`](../decisions/0040-sm-cloud-p1-backup-restore.md)
- [`ADR 0034`](../decisions/0034-civ-codec-protocol-seam.md)
- Expanded rationale: `d0391ed7:docs/backlog.md` and `d0391ed7:docs/dogfood-inbox.md`.
