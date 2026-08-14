package evidence

/*
   Acceptance criteria for the §4.2 station-profiles slice
   (docs/v2-design/spot-network/spot-network-design.md §4.2/§5.4 dated
   amendments; operator rulings 2026-08-10). The config-validation half
   (PR6 and the O1/O3 declaration rules) lives in
   internal/config/validate_antennas_test.go. Written RED before the
   implementation (ATDD); each criterion names the nearest confusable state.

   PR1  Zero-config participation: capture enabled with NO antenna
        declaration stores every observation as before, profile_uuid NULL
        with reason no_declaration, and profile health reports
        none_declared — distinguishable from a broken resolver, which
        reports degraded and stamps profile_error and may never present as
        none_declared. No warning, no default-visible log line.
   PR2  Pinning follows change, not activation: first activation pins one
        immutable version per declared antenna; restarting/reconciling
        while the active declaration is UNCHANGED AFTER NORMALIZATION
        (trimmed strings, empty≡absent, entry and band order ignored,
        locator case canonicalized) mints nothing. Observable: the status
        active map band → {name, profile_uuid, version, valid_from}.
   PR3  The stamp follows the band, hands-off: observations stamp the
        version of the antenna whose declaration claims the band derived
        from the dial the slot was attributed to — never a live read at
        write time. A decoded observation from a slot whose dial could not
        be attributed carries NULL + dial_unattributed (dial_tracked keeps
        the finer CAT-absent vs CAT-unreadable split). A dial-moved slot
        produces dial_changed coverage and no observations (existing
        suppression rule, re-asserted).
   PR4  An unmapped band stays honest: capture continues, rows carry NULL +
        band_unmapped, health stays active — distinguishable from a broken
        resolver (profile_error + degraded) and from PR1. (The "no
        sync-side retry state" clause is recorded in §5.4 and repeats in
        the sync slice's acceptance header; it is not automated here.)
   PR5  History never rewrites: editing a declared fact pins a NEW version
        under the same lineage; rows stamped before the edit still resolve
        to the old values, byte-for-byte, after it.
   PR6  A band claimed twice is refused before anything pins (validated in
        internal/config; boot = fatal exit, PUT = 400 atomic — confirmed
        behaviors, config.go:603 / main.go:189 / handler_config.go:753).
   PR7  Honest silence, mandatory noise: unprofiled DATA is quiet (counters
        and rows only, no default-visible log); every TRANSITION into
        degraded emits exactly one default-visible warning-or-error record,
        not repeated per observation or status read, and degraded is
        unreachable without it.
   PR8  Removal retires, never deletes: a removed antenna's lineage stops,
        its bands go band_unmapped, every past row still resolves.
        Re-adding a retired name ALWAYS mints the next version in that
        lineage, even with identical facts — resumption is an event, not a
        no-op.
   PR9  Adoption preserves the archive: opening a v1 evidence.db migrates
        additively — profile storage + unprofiled_reason added, every
        pre-existing NULL backfills legacy_unprofiled (never
        no_declaration: that would claim an operator choice predating the
        feature), every observation/coverage/loss row otherwise unchanged,
        distinguishable from a reseeded archive by preserved UUIDs and
        counts.
   PR10 Bands are pinned facts (codex-P1 ruling 2026-08-10): adding or
        removing a band mints the next version under the same lineage, and
        every version row records the band set it governed (canonical
        sorted comma-joined), so observations stamped before and after the
        change resolve DIFFERENT versions and the governing declaration of
        any historical row stays reconstructable. Distinguishable from a
        normalized-identical restart — band ORDER shuffled — which must
        still mint nothing (PR2's fixture pins that side).

   O4 (activation): restart-only; activation = evidence-store startup;
   validFrom = mint time. The "stamp resolved at slot-evidence emission,
   never re-derived at async write time" rule is STRUCTURAL this slice —
   the mapping is process-immutable under restart-only, so no fixture can
   make emission-time and write-time resolution differ; a behavioural test
   would be decoration (house fixture rule) and deliberately does not
   exist. It arrives with any future live-write surface.

   O5 (atomicity/failure/cap): all versions + the active map reconcile in
   ONE transaction before the writer starts; failure activates none and
   the stale prior mapping is NOT used (globally degraded, profile_error).
   Cap boundary (codex-P1 ruling 2026-08-10): a post-write measurement
   cannot defend the cap — dirty pages spill to the -wal DURING a
   transaction and ROLLBACK does not shrink the file (measured against
   modernc.org/sqlite v1.48.1; numbers in migrateSchema's comment) — so
   room is reserved BEFORE writing. Activation refuses at the same
   watermark every slot write observes (cap − headroom), not merely below
   the cap; the v1→v2 migration, whose backfill dirties ~every
   observations page (WAL peak ≈ the whole db file), refuses on a
   pre-write projection, leaving the archive at v1 and evidence idle (the
   existing fail-soft), retryable at the next restart; a successful
   migration TRUNCATE-checkpoints the WAL so the transient peak is folded
   back before capture starts. A refused activation degrades until
   capacity/cap changes take effect at the NEXT restart. DB-cannot-open
   stays the existing Error-level fail-soft (evidence idle, decoding
   continues) — untouched here.

   O6 (health surface): profiles.state ∈ disabled | none_declared | active
   | degraded; when disabled the lineage/version counts are UNAVAILABLE
   (nil pointers → null/omitted JSON), never zero. Reason counters derive
   by GROUP BY; the reason commits inside the existing one-slot
   transaction (writeSlot, EV4), so there is no crash-lag caveat.

   Pinned observable DB surface (the archive is operator- and sync-facing):
   table `profiles` (uuid PK, lineage, version, valid_from, name, type,
   height_m NULLable — 0 is a real value distinct from NULL — feedline,
   locator NULLable, bands NOT NULL — the canonical sorted comma-joined
   ADIF set this version governed, a pinned fact per PR10 — noise_floor
   always 'not_measured' this slice, synced),
   and `observations.unprofiled_reason` with the exactly-one-of constraint
   against profile_uuid. The active band→version map is asserted through
   Status only (its storage is reconciliation-internal).

   Failure injection is real where possible; the one seam is
   profileFaultForTest (writerDelay pattern) for the O5 class-1 failure,
   which tests the HANDLING (degraded, loud-once, no stale mapping), not
   the detection.
*/

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

func ptr(f float64) *float64 { return &f }

// The dogfood station is the worked example (operator, 2026-08-10):
// 80/40/30 through the DX Commander, 20m and up through the hex beam.
func dxCommander() types.AntennaDecl {
	return types.AntennaDecl{
		Name: "DX Commander", Type: "vertical",
		Bands: []string{"80m", "40m", "30m"}, HeightM: ptr(0), // ground-mounted: a REAL 0, distinct from absent
		Feedline: "coax",
	}
}

func hexBeam() types.AntennaDecl {
	return types.AntennaDecl{
		Name: "VHQ Hex beam", Type: "hexbeam",
		Bands: []string{"20m", "17m", "15m", "12m", "10m", "6m"}, HeightM: ptr(12),
		Locator: "KG49dj",
	}
}

func declaredConfig(t *testing.T) Config {
	t.Helper()
	cfg := testConfig(t, true)
	cfg.Antennas = []types.AntennaDecl{dxCommander(), hexBeam()}
	return cfg
}

// obsSlot is a one-decode slot on the given dial — the minimal observation
// carrier for stamp assertions (slot time is the row discriminator).
func obsSlot(start time.Time, dialMHz float64, tracked bool) SlotCapture {
	return SlotCapture{
		SlotStart: start, DialMHz: dialMHz, DialTracked: tracked, Outcome: OutcomeDecoded,
		Decodes: []goft8.DecodedMessage{{
			Text: "CQ K1ABC FN42", Payload: goft8.Payload77{0x10, 0x20},
			ParseStatus: goft8.ParseStatusParsed,
			Provenance:  goft8.DecodeProvenance{Algorithm: goft8.DecodeAlgorithmBP},
			SNR:         -8, FreqHz: 1204.5, DTSec: 0.3,
		}},
	}
}

// stampOf reads one observation's stamp pair by its slot time.
func stampOf(t *testing.T, db *sql.DB, slot time.Time) (profile, reason sql.NullString) {
	t.Helper()
	err := db.QueryRow(
		`SELECT profile_uuid, unprofiled_reason FROM observations WHERE slot_start_utc = ?`,
		slot.UTC().Format(time.RFC3339)).Scan(&profile, &reason)
	if err != nil {
		t.Fatalf("read stamp for %s: %v", slot.UTC().Format(time.RFC3339), err)
	}
	return profile, reason
}

// profileRow reads one pinned version's facts.
func profileRow(t *testing.T, db *sql.DB, lineage string, version int) (uuid string, heightM sql.NullFloat64, locator sql.NullString) {
	t.Helper()
	err := db.QueryRow(
		`SELECT uuid, height_m, locator FROM profiles WHERE lineage = ? AND version = ?`,
		lineage, version).Scan(&uuid, &heightM, &locator)
	if err != nil {
		t.Fatalf("read profile %s v%d: %v", lineage, version, err)
	}
	return uuid, heightM, locator
}

// assertStampInvariant: every observation carries exactly one of
// profile_uuid / unprofiled_reason (O6).
func assertStampInvariant(t *testing.T, db *sql.DB) {
	t.Helper()
	bad := countRows(t, db,
		`SELECT COUNT(*) FROM observations
		 WHERE (profile_uuid IS NULL AND unprofiled_reason IS NULL)
		    OR (profile_uuid IS NOT NULL AND unprofiled_reason IS NOT NULL)`)
	if bad != 0 {
		t.Fatalf("stamp invariant violated on %d rows: exactly one of profile_uuid/unprofiled_reason must be set", bad)
	}
}

// newRunningLogged is newRunning with a capturing log sink (PR1/PR7 log
// assertions). The sink is only read after Stop, so a plain buffer is safe.
func newRunningLogged(t *testing.T, cfg Config, buf *bytes.Buffer) *Service {
	t.Helper()
	s := New(cfg, logging.NewForWriter(buf))
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(s.Stop)
	return s
}

func defaultVisibleLines(buf *bytes.Buffer) []string {
	var out []string
	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.Contains(l, `"level":"warn"`) || strings.Contains(l, `"level":"error"`) {
			out = append(out, l)
		}
	}
	return out
}

// PR1 — zero-config participation, quiet and distinguishable.
func TestPR1_ZeroConfigParticipation(t *testing.T) {
	cfg := testConfig(t, true) // capture ON, no antennas
	var buf bytes.Buffer
	s := newRunningLogged(t, cfg, &buf)

	s.CaptureSlot(obsSlot(slotAt(0), 14.074, true))
	drain(t, s)

	st := s.Status()
	if st.Profiles == nil {
		t.Fatal("PR1: Status.Profiles health surface missing")
	}
	if st.Profiles.State != ProfilesNoneDeclared {
		t.Fatalf("PR1: profiles.state = %q, want %q (a broken resolver must report degraded, never this)",
			st.Profiles.State, ProfilesNoneDeclared)
	}
	s.Stop()
	if lines := defaultVisibleLines(&buf); len(lines) != 0 {
		t.Fatalf("PR1: unprofiled data must be quiet; default-visible log lines: %v", lines)
	}

	db := openRaw(t, cfg.Path)
	assertStampInvariant(t, db)
	_, reason := stampOf(t, db, slotAt(0))
	if !reason.Valid || reason.String != ReasonNoDeclaration {
		t.Fatalf("PR1: unprofiled_reason = %v, want %q", reason, ReasonNoDeclaration)
	}
}

// PR2 — first activation pins; a normalized-identical restart mints nothing.
func TestPR2_DeclarationPinsOnceAndIdempotently(t *testing.T) {
	cfg := declaredConfig(t)
	s := newRunning(t, cfg)
	s.CaptureSlot(obsSlot(slotAt(0), 3.573, true)) // 80m → DX Commander
	drain(t, s)

	st := s.Status()
	if st.Profiles == nil || st.Profiles.State != ProfilesActive {
		t.Fatalf("PR2: profiles.state = %+v, want active", st.Profiles)
	}
	act, ok := st.Profiles.Active["80m"]
	if !ok || act.Name != "DX Commander" || act.Version != 1 || act.ProfileUUID == "" || act.ValidFrom == "" {
		t.Fatalf("PR2: active[80m] = %+v, want DX Commander v1 with uuid + valid_from", act)
	}
	s.Stop()

	db := openRaw(t, cfg.Path)
	dxUUID, _, _ := profileRow(t, db, "DX Commander", 1)
	profile, _ := stampOf(t, db, slotAt(0))
	if !profile.Valid || profile.String != dxUUID {
		t.Fatalf("PR2: observation stamp = %v, want DX Commander v1 uuid %s", profile, dxUUID)
	}

	// Restart with a NORMALIZED-identical declaration: padded strings,
	// entries and bands reordered, locator case shuffled, empty≡absent.
	cfg2 := cfg
	hb := hexBeam()
	hb.Name = "  VHQ Hex beam  "
	hb.Bands = []string{"6m", "10m", "12m", "15m", "17m", "20m"}
	hb.Locator = "kg49DJ"
	hb.Feedline = "   " // whitespace-only ≡ absent, same as before
	dx := dxCommander()
	dx.Type = " vertical "
	cfg2.Antennas = []types.AntennaDecl{hb, dx} // entry order carries nothing
	s2 := newRunning(t, cfg2)
	s2.Stop()

	if n := countRows(t, db, `SELECT COUNT(*) FROM profiles`); n != 2 {
		t.Fatalf("PR2: %d profile versions after identical restart, want 2 — a no-op save must not mint", n)
	}
}

// PR3 — the stamp derives from the slot's attributed dial; unattributable
// dials are honest NULLs; dial-moved slots stay suppressed.
func TestPR3_StampFollowsAttributedDial(t *testing.T) {
	cfg := declaredConfig(t)
	s := newRunning(t, cfg)

	s.CaptureSlot(obsSlot(slotAt(0), 3.573, true))   // 80m → DX Commander
	s.CaptureSlot(obsSlot(slotAt(15), 14.074, true)) // 20m → Hex beam
	s.CaptureSlot(obsSlot(slotAt(30), 0, false))     // no dial source
	s.CaptureSlot(obsSlot(slotAt(45), 0, true))      // CAT present, dial unreadable
	s.CaptureSlot(SlotCapture{                       // dial moved mid-window: coverage only (existing rule)
		SlotStart: slotAt(60), DialMHz: 0, DialTracked: true, Outcome: OutcomeDialChanged,
	})
	drain(t, s)
	s.Stop()

	db := openRaw(t, cfg.Path)
	assertStampInvariant(t, db)

	dxUUID, _, _ := profileRow(t, db, "DX Commander", 1)
	hexUUID, _, _ := profileRow(t, db, "VHQ Hex beam", 1)
	if p, _ := stampOf(t, db, slotAt(0)); !p.Valid || p.String != dxUUID {
		t.Fatalf("PR3: 80m stamp = %v, want DX Commander uuid", p)
	}
	if p, _ := stampOf(t, db, slotAt(15)); !p.Valid || p.String != hexUUID {
		t.Fatalf("PR3: 20m stamp = %v, want Hex beam uuid", p)
	}
	for _, sec := range []int{30, 45} {
		if _, r := stampOf(t, db, slotAt(sec)); !r.Valid || r.String != ReasonDialUnattributed {
			t.Fatalf("PR3: unattributable-dial slot %ds reason = %v, want %q", sec, r, ReasonDialUnattributed)
		}
	}
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM observations WHERE slot_start_utc = ?`,
		slotAt(60).UTC().Format(time.RFC3339)); n != 0 {
		t.Fatalf("PR3: dial-moved slot has %d observations, want 0 (suppression rule)", n)
	}
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM coverage WHERE slot_start_utc = ? AND outcome = ?`,
		slotAt(60).UTC().Format(time.RFC3339), string(OutcomeDialChanged)); n != 1 {
		t.Fatalf("PR3: dial-moved slot must keep its dial_changed coverage row")
	}
}

// PR4 — an unmapped band is band_unmapped under an ACTIVE profile state.
func TestPR4_UnmappedBandStaysHonest(t *testing.T) {
	cfg := declaredConfig(t)
	s := newRunning(t, cfg)

	s.CaptureSlot(obsSlot(slotAt(0), 5.3574, true)) // 60m: no antenna claims it
	drain(t, s)

	st := s.Status()
	if st.Profiles == nil || st.Profiles.State != ProfilesActive {
		t.Fatalf("PR4: profiles.state = %+v, want active — an unmapped band is not a failure", st.Profiles)
	}
	if got := st.Profiles.Unprofiled[ReasonBandUnmapped]; got != 1 {
		t.Fatalf("PR4: unprofiled[%s] = %d, want 1", ReasonBandUnmapped, got)
	}
	s.Stop()

	db := openRaw(t, cfg.Path)
	if _, r := stampOf(t, db, slotAt(0)); !r.Valid || r.String != ReasonBandUnmapped {
		t.Fatalf("PR4: reason = %v, want %q", r, ReasonBandUnmapped)
	}
}

// PR5 — an edit mints a new version; old rows resolve old facts unchanged.
func TestPR5_HistoryNeverRewrites(t *testing.T) {
	cfg := declaredConfig(t)
	s := newRunning(t, cfg)
	s.CaptureSlot(obsSlot(slotAt(0), 14.074, true)) // stamps hex v1 (height 12)
	drain(t, s)
	s.Stop()

	cfg2 := cfg
	hb := hexBeam()
	hb.HeightM = ptr(14) // the hex beam went up 2 m
	cfg2.Antennas = []types.AntennaDecl{dxCommander(), hb}
	s2 := newRunning(t, cfg2)
	s2.CaptureSlot(obsSlot(slotAt(15), 14.074, true)) // stamps hex v2
	drain(t, s2)
	s2.Stop()

	db := openRaw(t, cfg.Path)
	v1uuid, v1h, _ := profileRow(t, db, "VHQ Hex beam", 1)
	v2uuid, v2h, _ := profileRow(t, db, "VHQ Hex beam", 2)
	if !v1h.Valid || v1h.Float64 != 12 {
		t.Fatalf("PR5: v1 height = %v after the edit, want the original 12 — history rewritten", v1h)
	}
	if !v2h.Valid || v2h.Float64 != 14 {
		t.Fatalf("PR5: v2 height = %v, want 14", v2h)
	}
	if p, _ := stampOf(t, db, slotAt(0)); !p.Valid || p.String != v1uuid {
		t.Fatalf("PR5: pre-edit row stamp = %v, must still resolve v1 %s", p, v1uuid)
	}
	if p, _ := stampOf(t, db, slotAt(15)); !p.Valid || p.String != v2uuid {
		t.Fatalf("PR5: post-edit row stamp = %v, want v2 %s", p, v2uuid)
	}
	// The DX Commander was untouched: still exactly one version.
	if n := countRows(t, db, `SELECT COUNT(*) FROM profiles WHERE lineage = ?`, "DX Commander"); n != 1 {
		t.Fatalf("PR5: untouched lineage minted %d versions, want 1", n)
	}
}

// PR7 + O5 class 1 — reconciliation failure: loud exactly once, globally
// degraded, the stale prior mapping is NOT used, capture continues.
func TestPR7_DegradedIsLoudOnceAndUsesNoStaleMapping(t *testing.T) {
	cfg := declaredConfig(t)
	s := newRunning(t, cfg) // boot 1 pins v1s
	s.Stop()

	profileFaultForTest = errors.New("injected reconcile fault")
	defer func() { profileFaultForTest = nil }()

	var buf bytes.Buffer
	s2 := newRunningLogged(t, cfg, &buf) // boot 2: reconciliation fails, capture continues
	s2.CaptureSlot(obsSlot(slotAt(0), 3.573, true))
	s2.CaptureSlot(obsSlot(slotAt(15), 3.573, true))
	drain(t, s2)
	_ = s2.Status()
	_ = s2.Status()

	st := s2.Status()
	if st.Profiles == nil || st.Profiles.State != ProfilesDegraded || st.Profiles.Reason == "" {
		t.Fatalf("PR7: profiles = %+v, want degraded with a reason", st.Profiles)
	}
	if len(st.Profiles.Active) != 0 {
		t.Fatalf("PR7/O5: active map = %+v after failed reconciliation — the stale prior mapping must not be used", st.Profiles.Active)
	}
	s2.Stop()

	lines := defaultVisibleLines(&buf)
	if len(lines) != 1 {
		t.Fatalf("PR7: %d default-visible records, want exactly 1 per transition into degraded: %v", len(lines), lines)
	}

	db := openRaw(t, cfg.Path)
	assertStampInvariant(t, db)
	for _, sec := range []int{0, 15} {
		p, r := stampOf(t, db, slotAt(sec))
		if p.Valid {
			t.Fatalf("PR7/O5: slot %ds stamped %s under a failed reconciliation — stale mapping used", sec, p.String)
		}
		if !r.Valid || r.String != ReasonProfileError {
			t.Fatalf("PR7: slot %ds reason = %v, want %q", sec, r, ReasonProfileError)
		}
	}
}

// PR8 — removal retires; re-adding the same name always mints.
func TestPR8_RemovalRetiresReaddAlwaysMints(t *testing.T) {
	cfg := declaredConfig(t)
	s := newRunning(t, cfg)
	s.Stop()

	cfg2 := cfg
	cfg2.Antennas = []types.AntennaDecl{dxCommander()} // hex beam removed
	s2 := newRunning(t, cfg2)
	s2.CaptureSlot(obsSlot(slotAt(0), 14.074, true)) // 20m now unmapped
	drain(t, s2)
	s2.Stop()

	db := openRaw(t, cfg.Path)
	if _, r := stampOf(t, db, slotAt(0)); !r.Valid || r.String != ReasonBandUnmapped {
		t.Fatalf("PR8: removed antenna's band reason = %v, want %q", r, ReasonBandUnmapped)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM profiles WHERE lineage = ?`, "VHQ Hex beam"); n != 1 {
		t.Fatalf("PR8: retired lineage has %d versions, want its 1 preserved", n)
	}

	// Re-add with IDENTICAL facts: resumption is an event → v2 mints.
	s3 := newRunning(t, cfg)
	s3.CaptureSlot(obsSlot(slotAt(15), 14.074, true))
	drain(t, s3)
	s3.Stop()

	v2uuid, _, _ := profileRow(t, db, "VHQ Hex beam", 2)
	if p, _ := stampOf(t, db, slotAt(15)); !p.Valid || p.String != v2uuid {
		t.Fatalf("PR8: post-re-add stamp = %v, want the freshly minted v2 %s", p, v2uuid)
	}
}

// PR9 — opening a v1 archive preserves it; legacy NULLs are legacy, not a
// claimed operator choice. v1SchemaForTest is the v1 DDL frozen VERBATIM at
// the moment the v2 migration was designed — it must never track schema.go.
const v1SchemaForTest = `
CREATE TABLE IF NOT EXISTS schema_meta (
	k TEXT PRIMARY KEY,
	v TEXT NOT NULL
);
INSERT INTO schema_meta (k, v) VALUES ('schema_version', '1')
	ON CONFLICT(k) DO NOTHING;

CREATE TABLE IF NOT EXISTS observations (
	uuid                    TEXT PRIMARY KEY,
	slot_start_utc          TEXT NOT NULL,
	dial_mhz                REAL NOT NULL,
	dial_tracked            INTEGER NOT NULL,
	freq_hz                 REAL NOT NULL,
	dt_sec                  REAL NOT NULL,
	snr                     INTEGER NOT NULL,
	payload                 BLOB NOT NULL,
	parse_status            TEXT NOT NULL,
	text                    TEXT,
	prov_algorithm          TEXT NOT NULL,
	prov_ap_profile         TEXT NOT NULL DEFAULT '',
	prov_ap_source          TEXT NOT NULL DEFAULT '',
	metric_sync             REAL NOT NULL,
	metric_hard_sync        INTEGER NOT NULL,
	metric_costas_geo       REAL NOT NULL,
	metric_costas_min_block REAL NOT NULL,
	metric_blocks           INTEGER NOT NULL,
	metric_hard_errors      INTEGER NOT NULL,
	metric_dmin             REAL NOT NULL,
	decoder_build           TEXT NOT NULL,
	profile_uuid            TEXT,
	synced                  INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS coverage (
	uuid           TEXT PRIMARY KEY,
	slot_start_utc TEXT NOT NULL,
	outcome        TEXT NOT NULL,
	dial_mhz       REAL NOT NULL,
	dial_tracked   INTEGER NOT NULL,
	decode_count   INTEGER NOT NULL,
	synced         INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS loss_intervals (
	uuid          TEXT PRIMARY KEY,
	start_utc     TEXT NOT NULL,
	end_utc       TEXT NOT NULL,
	slots         INTEGER NOT NULL,
	observations  INTEGER NOT NULL,
	reason        TEXT NOT NULL,
	remote_status TEXT NOT NULL,
	dial_mhz      REAL NOT NULL,
	synced        INTEGER NOT NULL DEFAULT 0
);
`

func TestPR9_AdoptionPreservesTheArchive(t *testing.T) {
	cfg := testConfig(t, true)

	// A genuine v1 archive with two observations, a coverage row and a loss
	// interval, fixed UUIDs.
	raw, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(v1SchemaForTest); err != nil {
		t.Fatalf("create v1 archive: %v", err)
	}
	for _, uuid := range []string{"v1-obs-a", "v1-obs-b"} {
		if _, err := raw.Exec(
			`INSERT INTO observations (uuid, slot_start_utc, dial_mhz, dial_tracked, freq_hz,
				dt_sec, snr, payload, parse_status, text, prov_algorithm,
				metric_sync, metric_hard_sync, metric_costas_geo, metric_costas_min_block,
				metric_blocks, metric_hard_errors, metric_dmin, decoder_build, profile_uuid)
			 VALUES (?, ?, 14.074, 1, 1200, 0.1, -5, X'00', 'parsed', 'CQ TEST', 'bp',
				1, 1, 1, 1, 1, 0, 1, 'v0.8.0', NULL)`,
			uuid, slotAt(0).UTC().Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := raw.Exec(
		`INSERT INTO coverage (uuid, slot_start_utc, outcome, dial_mhz, dial_tracked, decode_count)
		 VALUES ('v1-cov-a', ?, 'decoded', 14.074, 1, 2)`,
		slotAt(0).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(
		`INSERT INTO loss_intervals (uuid, start_utc, end_utc, slots, observations, reason, remote_status, dial_mhz)
		 VALUES ('v1-loss-a', ?, ?, 2, 3, 'cap', 'never_offered', 14.074)`,
		slotAt(15).UTC().Format(time.RFC3339), slotAt(45).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()

	s := newRunning(t, cfg) // adoption: migrate additively
	s.CaptureSlot(obsSlot(slotAt(60), 14.074, true))
	drain(t, s)
	s.Stop()

	db := openRaw(t, cfg.Path)
	var ver string
	// The current version, not a frozen one: adoption lands at whatever this
	// build runs (the 1→2→3 chain itself is pinned in syncschema_test.go).
	if err := db.QueryRow(`SELECT v FROM schema_meta WHERE k = 'schema_version'`).Scan(&ver); err != nil || ver != schemaVersion {
		t.Fatalf("PR9: schema_version = %q (err %v), want %q", ver, err, schemaVersion)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM observations WHERE uuid IN ('v1-obs-a','v1-obs-b')`); n != 2 {
		t.Fatalf("PR9: %d of the v1 observations survive, want 2 (UUIDs preserved)", n)
	}
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM observations WHERE uuid IN ('v1-obs-a','v1-obs-b') AND unprofiled_reason = ?`,
		ReasonLegacyUnprofiled); n != 2 {
		t.Fatalf("PR9: v1 NULLs must backfill %q — never a reason that claims an operator choice", ReasonLegacyUnprofiled)
	}
	if _, r := stampOf(t, db, slotAt(60)); !r.Valid || r.String != ReasonNoDeclaration {
		t.Fatalf("PR9: post-adoption row reason = %v, want %q (legacy is for pre-migration rows only)", r, ReasonNoDeclaration)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM coverage WHERE uuid = 'v1-cov-a'`); n != 1 {
		t.Fatal("PR9: v1 coverage row lost")
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM loss_intervals WHERE uuid = 'v1-loss-a' AND slots = 2 AND observations = 3`); n != 1 {
		t.Fatal("PR9: v1 loss interval lost or rewritten")
	}
	assertStampInvariant(t, db)
}

// O5 — a declaration that cannot fit under the hard cap does not activate.
func TestO5_ActivationRefusedAtCap(t *testing.T) {
	oldHeadroom := headroomBytes
	headroomBytes = 4096
	defer func() { headroomBytes = oldHeadroom }()

	cfg := testConfig(t, true)
	cfg.CapBytes = 1 << 20 // roomy for boot 1
	s := newRunning(t, cfg)
	for i := 0; i < 20; i++ {
		s.CaptureSlot(richSlot(slotAt(i * 15)))
	}
	drain(t, s)
	usage, _ := s.physicalUsage()
	s.Stop()
	if usage <= headroomBytes+1 {
		t.Fatalf("fixture failure: usage %d too small to exceed the boot-2 cap", usage)
	}

	cfg2 := declaredConfig(t)
	cfg2.Path = cfg.Path
	cfg2.CapBytes = headroomBytes + 1 // usage already exceeds the hard cap
	s2 := newRunning(t, cfg2)
	st := s2.Status()
	if st.Profiles == nil || st.Profiles.State != ProfilesDegraded {
		t.Fatalf("O5: profiles = %+v, want degraded — activation must refuse rather than exceed the cap", st.Profiles)
	}
	s2.Stop()

	db := openRaw(t, cfg.Path)
	if n := countRows(t, db, `SELECT COUNT(*) FROM profiles`); n != 0 {
		t.Fatalf("O5: %d profile versions minted over the hard cap, want 0 (all-or-nothing)", n)
	}
}

// bandsOf reads one pinned version's recorded band set (PR10).
func bandsOf(t *testing.T, db *sql.DB, lineage string, version int) string {
	t.Helper()
	var bands string
	if err := db.QueryRow(
		`SELECT bands FROM profiles WHERE lineage = ? AND version = ?`,
		lineage, version).Scan(&bands); err != nil {
		t.Fatalf("read bands %s v%d: %v", lineage, version, err)
	}
	return bands
}

// PR10 — a band-membership change mints; each version records the set it
// governed; rows before and after the change resolve different versions.
func TestPR10_BandSetChangeMintsNewVersion(t *testing.T) {
	cfg := declaredConfig(t)
	s := newRunning(t, cfg)
	s.CaptureSlot(obsSlot(slotAt(0), 3.573, true))   // 80m → DX Commander v1
	s.CaptureSlot(obsSlot(slotAt(15), 10.136, true)) // 30m → DX Commander v1
	drain(t, s)
	s.Stop()

	// The DX Commander stops claiming 30m; every OTHER fact is unchanged —
	// the exact fixture where a facts-without-bands comparison sees a no-op.
	cfg2 := cfg
	dx := dxCommander()
	dx.Bands = []string{"80m", "40m"}
	cfg2.Antennas = []types.AntennaDecl{dx, hexBeam()}
	s2 := newRunning(t, cfg2)
	s2.CaptureSlot(obsSlot(slotAt(30), 3.573, true))  // 80m → v2 now
	s2.CaptureSlot(obsSlot(slotAt(45), 10.136, true)) // 30m: nobody claims it
	drain(t, s2)
	s2.Stop()

	db := openRaw(t, cfg.Path)
	if n := countRows(t, db, `SELECT COUNT(*) FROM profiles WHERE lineage = ?`, "DX Commander"); n != 2 {
		t.Fatalf("PR10: %d versions after a band-set change, want 2 — bands are pinned facts", n)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM profiles WHERE lineage = ?`, "VHQ Hex beam"); n != 1 {
		t.Fatalf("PR10: untouched lineage minted %d versions, want 1", n)
	}
	if got := bandsOf(t, db, "DX Commander", 1); got != "30m,40m,80m" {
		t.Fatalf("PR10: v1 bands = %q, want the governed set \"30m,40m,80m\" preserved", got)
	}
	if got := bandsOf(t, db, "DX Commander", 2); got != "40m,80m" {
		t.Fatalf("PR10: v2 bands = %q, want \"40m,80m\"", got)
	}
	v1uuid, _, _ := profileRow(t, db, "DX Commander", 1)
	v2uuid, _, _ := profileRow(t, db, "DX Commander", 2)
	if p, _ := stampOf(t, db, slotAt(0)); !p.Valid || p.String != v1uuid {
		t.Fatalf("PR10: pre-change 80m stamp = %v, want v1 %s — before/after must stay distinguishable", p, v1uuid)
	}
	if p, _ := stampOf(t, db, slotAt(30)); !p.Valid || p.String != v2uuid {
		t.Fatalf("PR10: post-change 80m stamp = %v, want v2 %s — before/after must stay distinguishable", p, v2uuid)
	}
	if p, _ := stampOf(t, db, slotAt(15)); !p.Valid || p.String != v1uuid {
		t.Fatalf("PR10: pre-change 30m stamp = %v, must still resolve v1 (history never rewrites)", p)
	}
	if _, r := stampOf(t, db, slotAt(45)); !r.Valid || r.String != ReasonBandUnmapped {
		t.Fatalf("PR10: post-change 30m reason = %v, want %q", r, ReasonBandUnmapped)
	}
}

// junkObservations bulk-inserts opaque rows straight into an existing
// archive — fixture MASS for the size-boundary tests, where only bytes on
// disk matter. The column list works on both the v1 and v2 schemas (no
// unprofiled_reason), so these rows do not model the stamp invariant.
func junkObservations(t *testing.T, path string, rows, blobBytes int) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Closed before returning so the last-connection checkpoint folds the
	// WAL away and the on-disk size is final.
	defer func() { _ = db.Close() }()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < rows; i++ {
		if _, err := tx.Exec(
			`INSERT INTO observations (uuid, slot_start_utc, dial_mhz, dial_tracked,
				freq_hz, dt_sec, snr, payload, parse_status, text, prov_algorithm,
				metric_sync, metric_hard_sync, metric_costas_geo, metric_costas_min_block,
				metric_blocks, metric_hard_errors, metric_dmin, decoder_build, profile_uuid)
			 VALUES (?, '2026-08-10T00:00:00Z', 14.074, 1, 1200, 0.1, -5, zeroblob(?), 'parsed', NULL, 'bp',
				1, 1, 1, 1, 1, 0, 1, 'junk', NULL)`,
			fmt.Sprintf("junk-%06d", i), blobBytes); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// statUsage sums the archive's on-disk footprint the way physicalUsage
// does, without needing a service.
func statUsage(t *testing.T, path string) int64 {
	t.Helper()
	var total int64
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if fi, err := os.Stat(p); err == nil {
			total += fi.Size()
		}
	}
	return total
}

// buildV1Archive creates a v1-schema archive holding `rows` junk
// observations and returns its final on-disk size.
func buildV1Archive(t *testing.T, path string, rows int) int64 {
	t.Helper()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(v1SchemaForTest); err != nil {
		t.Fatalf("create v1 archive: %v", err)
	}
	_ = raw.Close()
	junkObservations(t, path, rows, 4096)
	return statUsage(t, path)
}

// O5/codex-P1 — activation refuses at the WATERMARK, not merely below the
// cap: between the two boundaries, minting would spend the headroom that
// makes the cap a physical guarantee. The fixture lands usage BETWEEN
// watermark and cap — exactly where a usage-below-cap gate activates.
func TestO5_ActivationRefusedAtWatermark(t *testing.T) {
	oldHeadroom := headroomBytes
	headroomBytes = 256 << 10
	defer func() { headroomBytes = oldHeadroom }()

	cfg := testConfig(t, true)
	cfg.CapBytes = 8 << 20
	s := newRunning(t, cfg) // creates the v2 archive
	s.Stop()
	junkObservations(t, cfg.Path, 300, 4096)
	usage := statUsage(t, cfg.Path)
	if usage <= headroomBytes/2 {
		t.Fatalf("fixture failure: usage %d B too small to straddle the watermark", usage)
	}

	cfg2 := declaredConfig(t)
	cfg2.Path = cfg.Path
	cfg2.CapBytes = usage + headroomBytes/2 // watermark = usage − headroom/2 < usage < cap
	s2 := newRunning(t, cfg2)
	st := s2.Status()
	if st.Profiles == nil || st.Profiles.State != ProfilesDegraded {
		t.Fatalf("O5: profiles = %+v, want degraded — between watermark and cap must refuse", st.Profiles)
	}
	s2.Stop()

	db := openRaw(t, cfg.Path)
	if n := countRows(t, db, `SELECT COUNT(*) FROM profiles`); n != 0 {
		t.Fatalf("O5: %d versions minted between watermark and cap, want 0 (all-or-nothing)", n)
	}
}

// O5/codex-P1 — the v1→v2 backfill dirties ~every observations page, so a
// near-cap v1 archive cannot migrate under the cap: refuse BEFORE writing,
// leave the archive at v1 (evidence idle, retry next restart).
func TestO5_MigrationRefusedNearCap(t *testing.T) {
	oldHeadroom := headroomBytes
	headroomBytes = 4096
	defer func() { headroomBytes = oldHeadroom }()

	cfg := testConfig(t, true) // no antennas: isolates the migration gate
	usage := buildV1Archive(t, cfg.Path, 600)
	// Above usage (a no-gate build proceeds) but far below the migration's
	// ~2× transient peak.
	cfg.CapBytes = usage + usage/2

	s := New(cfg, logging.Noop())
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	err := s.Start()
	if err == nil {
		s.Stop()
		t.Fatal("O5: Start succeeded — a migration whose WAL peak cannot fit under the cap must refuse")
	}
	if !strings.Contains(err.Error(), "cap") {
		t.Fatalf("O5: the refusal must name the cap; got: %v", err)
	}
	if after := statUsage(t, cfg.Path); after > usage+65536 {
		t.Fatalf("O5: refused migration grew the archive %d → %d B; refusal must come before writing", usage, after)
	}

	db := openRaw(t, cfg.Path)
	var ver string
	if err := db.QueryRow(`SELECT v FROM schema_meta WHERE k = 'schema_version'`).Scan(&ver); err != nil || ver != "1" {
		t.Fatalf("O5: schema_version = %q (err %v) after refused migration, want \"1\" untouched", ver, err)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM sqlite_master WHERE name = 'profiles'`); n != 0 {
		t.Fatal("O5: refused migration must write NOTHING — profiles table exists")
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM pragma_table_info('observations') WHERE name = 'unprofiled_reason'`); n != 0 {
		t.Fatal("O5: refused migration must write NOTHING — unprofiled_reason column exists")
	}
}

// O5/codex-P1 — a SUCCESSFUL migration folds its transient WAL peak back
// (checkpoint TRUNCATE) before capture starts; without it the -wal keeps
// its high-water size (≈ the whole db file) and a large adopted archive
// boots straight into drop_new.
func TestO5_MigrationFoldsTransientGrowth(t *testing.T) {
	cfg := testConfig(t, true)
	buildV1Archive(t, cfg.Path, 1500) // big enough that a retained WAL is unmistakable
	s := newRunning(t, cfg)

	usage, _ := s.physicalUsage()
	var dbSize int64
	if fi, err := os.Stat(cfg.Path); err == nil {
		dbSize = fi.Size()
	}
	if usage > dbSize+(1<<20) {
		t.Fatalf("O5: post-migration usage %d B vs db file %d B — the migration's transient WAL was not folded back", usage, dbSize)
	}
}

// O6 — disabled: the profiles surface says so, and DB-derived counts are
// UNAVAILABLE (nil → null/omitted), never zero.
func TestO6_DisabledProfilesCountsUnavailable(t *testing.T) {
	cfg := testConfig(t, false)
	cfg.Antennas = []types.AntennaDecl{dxCommander()}
	s := newRunning(t, cfg)

	st := s.Status()
	if st.Profiles == nil {
		t.Fatal("O6: Status.Profiles missing for a disabled service")
	}
	if st.Profiles.State != ProfilesDisabled {
		t.Fatalf("O6: profiles.state = %q, want %q", st.Profiles.State, ProfilesDisabled)
	}
	if st.Profiles.Lineages != nil || st.Profiles.Versions != nil {
		t.Fatalf("O6: disabled counts must be nil (unavailable), got lineages=%v versions=%v — zero would claim an opened, empty store",
			st.Profiles.Lineages, st.Profiles.Versions)
	}
}
