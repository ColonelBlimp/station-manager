package evidence

import (
	"database/sql"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// Station profiles — the §4.2 slice (operator rulings 2026-08-10; acceptance
// criteria PR1–PR9 in profiles_test.go, config-validation half in
// internal/config/validate_antennas_test.go).
//
// Activation is restart-only: reconcileProfiles runs once in Start, inside
// one transaction, before the writer goroutine exists. The resolved band →
// version mapping is immutable for the process's life, which is what makes
// stamp resolution at CaptureSlot (emission time, the caller's goroutine)
// structurally equivalent to any later resolution — and why the O4 "never
// re-derive at async write time" rule needs no behavioural test this slice.

// Profile-health states surfaced by Status.Profiles.State.
const (
	ProfilesDisabled     = "disabled"
	ProfilesNoneDeclared = "none_declared"
	ProfilesActive       = "active"
	ProfilesDegraded     = "degraded"
)

// Unprofiled reasons: why an observation row carries no profile UUID.
// Exactly one of profile_uuid / unprofiled_reason is set on every row.
// Precedence when several could apply:
// profile_error → no_declaration → dial_unattributed → band_unmapped.
const (
	// ReasonLegacyUnprofiled marks v1 rows adopted by the migration: their
	// NULL predates the feature and must not claim an operator choice. The
	// writer never stamps it — post-adoption rows use the reasons below.
	ReasonLegacyUnprofiled = "legacy_unprofiled"
	ReasonNoDeclaration    = "no_declaration"
	ReasonBandUnmapped     = "band_unmapped"
	// ReasonDialUnattributed: the slot had no attributable dial. The
	// observation's dial_tracked column preserves the finer CAT-absent vs
	// CAT-unreadable split; the reason does not duplicate it.
	ReasonDialUnattributed = "dial_unattributed"
	ReasonProfileError     = "profile_error"
)

// profileFaultForTest, when non-nil, makes startup profile reconciliation
// fail as if the archive refused the transaction — the O5 class-1 failure
// (archive writable, profiles not) — so the degraded path is testable
// without corrupting a real database (the writerDelay pattern).
var profileFaultForTest error

// ProfileActive is one band's entry in the active declaration map.
type ProfileActive struct {
	Name        string `json:"name"`
	ProfileUUID string `json:"profile_uuid"`
	Version     int64  `json:"version"`
	ValidFrom   string `json:"valid_from"`
}

// ProfilesStatus is the profile-health half of Status (§4.2 amendment).
// Lineages/Versions are pointers because a disabled service opens nothing:
// the counts are UNAVAILABLE, not zero, and must serialize as null/omitted.
type ProfilesStatus struct {
	State      string                   `json:"state"`
	Reason     string                   `json:"reason,omitempty"` // degraded only
	Lineages   *int64                   `json:"lineages,omitempty"`
	Versions   *int64                   `json:"versions,omitempty"`
	Active     map[string]ProfileActive `json:"active,omitempty"`     // band → active version
	Unprofiled map[string]int64         `json:"unprofiled,omitempty"` // reason → count (GROUP BY derived)
}

// normDecl is a declaration entry after normalization — the identity PR2's
// no-op rule compares: trimmed strings with empty ≡ absent, bands as a
// sorted set, locator in canonical Maidenhead case. Whitespace or ordering
// must never mint a version.
type normDecl struct {
	name, typ, feedline, locator string // "" = absent
	heightM                      *float64
	bands                        []string
}

func normalizeDecl(d types.AntennaDecl) normDecl {
	nd := normDecl{
		name:     strings.TrimSpace(d.Name),
		typ:      strings.TrimSpace(d.Type),
		feedline: strings.TrimSpace(d.Feedline),
		heightM:  d.HeightM,
	}
	if l := strings.TrimSpace(d.Locator); l != "" {
		nd.locator = utils.NormalizeMaidenhead(l)
	}
	nd.bands = append([]string(nil), d.Bands...)
	sort.Strings(nd.bands)
	return nd
}

// bandsCSV is the canonical stored form of the band set: sorted,
// comma-joined. Sorting happened in normalizeDecl, so band ORDER can never
// mint a version (PR2) while band MEMBERSHIP always does (PR10).
func (d normDecl) bandsCSV() string { return strings.Join(d.bands, ",") }

// storedFacts is the latest pinned version of one lineage, as read back for
// the facts comparison. Nullable columns read as "" / nil = absent, matching
// normDecl's convention.
type storedFacts struct {
	uuid, validFrom        string
	version                int64
	typ, feedline, locator string
	bands                  string // canonical sorted comma-joined (PR10)
	heightM                *float64
}

func factsEqual(d normDecl, f storedFacts) bool {
	if d.typ != f.typ || d.feedline != f.feedline || d.locator != f.locator || d.bandsCSV() != f.bands {
		return false
	}
	if (d.heightM == nil) != (f.heightM == nil) {
		return false
	}
	return d.heightM == nil || *d.heightM == *f.heightM
}

// nullIfEmpty maps the normalized absent value onto SQL NULL, so "absent"
// has exactly one stored representation (PR2's no-op rule depends on it).
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// reconcileProfiles activates the declaration: one transaction minting every
// needed version and rebuilding the active mapping, or nothing at all (O5).
// An entry mints when its lineage is new, its facts changed, or its lineage
// is not part of the CURRENTLY active mapping — that last clause is what
// makes re-adding a retired antenna an event (PR8): without it, identical
// facts would read as a no-op and resumption would be invisible in history.
// Called from Start under s.mu, before the writer goroutine exists.
func (s *Service) reconcileProfiles(now time.Time) error {
	const op errors.Op = "evidence.Service.reconcileProfiles"
	if profileFaultForTest != nil {
		return errors.New(op).WithErr(profileFaultForTest).WithMsg("injected reconcile fault")
	}
	decls := make([]normDecl, 0, len(s.cfg.Antennas))
	for _, a := range s.cfg.Antennas {
		decls = append(decls, normalizeDecl(a))
	}
	// The cap is a physical guarantee over the whole database (§4.1), and a
	// post-write measurement cannot defend it: dirty pages spill to the
	// -wal DURING a transaction and ROLLBACK does not shrink the file
	// (measured 2026-08-10 against modernc.org/sqlite v1.48.1 — numbers in
	// migrateSchema's comment). Exactness therefore comes from reserving
	// room FIRST: activation refuses at the same watermark every slot write
	// observes (cap − headroom), and headroom, far larger than the few KB
	// an activation mints, is what bounds this transaction's growth.
	// Effective again at the next restart once capacity returns or the cap
	// is raised. An EMPTY declaration only shrinks (mapping delete) and is
	// always allowed.
	if len(decls) > 0 {
		if usage, watermark := s.physicalUsage(), s.cfg.CapBytes-headroomBytes; usage >= watermark {
			return errors.New(op).WithMsgf(
				"physical usage %d B is at or past the activation watermark %d B (cap %d B minus reserved headroom); the declaration cannot activate without risking the hard cap",
				usage, watermark, s.cfg.CapBytes)
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("begin activation transaction")
	}
	defer func() { _ = tx.Rollback() }()

	// UUIDs referenced by the PRIOR active mapping: a lineage absent from it
	// was retired, whatever its stored facts say.
	activeUUIDs := map[string]bool{}
	rows, err := tx.Query(`SELECT DISTINCT profile_uuid FROM profile_active`)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("read prior active mapping")
	}
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			_ = rows.Close()
			return errors.New(op).WithErr(err).WithMsg("scan prior active mapping")
		}
		activeUUIDs[u] = true
	}
	if err := rows.Close(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("close prior active mapping")
	}

	newActive := map[string]ProfileActive{}
	for _, d := range decls {
		var f storedFacts
		var typ, feedline, locator sql.NullString
		var heightM sql.NullFloat64
		err := tx.QueryRow(
			`SELECT uuid, version, valid_from, type, height_m, feedline, locator, bands
			 FROM profiles WHERE lineage = ? ORDER BY version DESC LIMIT 1`, d.name).
			Scan(&f.uuid, &f.version, &f.validFrom, &typ, &heightM, &feedline, &locator, &f.bands)
		exists := err == nil
		if err != nil && err != sql.ErrNoRows {
			return errors.New(op).WithErr(err).WithMsgf("read lineage %q", d.name)
		}
		if exists {
			f.typ, f.feedline, f.locator = typ.String, feedline.String, locator.String
			if heightM.Valid {
				h := heightM.Float64
				f.heightM = &h
			}
		}
		if !exists || !factsEqual(d, f) || !activeUUIDs[f.uuid] {
			f.uuid = utils.NewUUIDv7At(now)
			f.version++
			f.validFrom = now.UTC().Format(time.RFC3339)
			var h any
			if d.heightM != nil {
				h = *d.heightM
			}
			if _, err := tx.Exec(
				`INSERT INTO profiles (uuid, lineage, version, valid_from, name, type, height_m, feedline, locator, bands)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				f.uuid, d.name, f.version, f.validFrom, d.name,
				nullIfEmpty(d.typ), h, nullIfEmpty(d.feedline), nullIfEmpty(d.locator), d.bandsCSV()); err != nil {
				return errors.New(op).WithErr(err).WithMsgf("mint %q v%d", d.name, f.version)
			}
		}
		for _, b := range d.bands {
			newActive[b] = ProfileActive{
				Name: d.name, ProfileUUID: f.uuid, Version: f.version, ValidFrom: f.validFrom,
			}
		}
	}

	if _, err := tx.Exec(`DELETE FROM profile_active`); err != nil {
		return errors.New(op).WithErr(err).WithMsg("clear active mapping")
	}
	for band, pa := range newActive {
		if _, err := tx.Exec(
			`INSERT INTO profile_active (band, profile_uuid) VALUES (?, ?)`,
			band, pa.ProfileUUID); err != nil {
			return errors.New(op).WithErr(err).WithMsgf("map band %q", band)
		}
	}
	if err := tx.Commit(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("commit activation")
	}

	s.profActive = newActive
	if len(decls) == 0 {
		s.profState = ProfilesNoneDeclared
	} else {
		s.profState = ProfilesActive
	}
	return nil
}

// stampLocked resolves one slot's profile stamp at emission time, in ruling
// precedence order. Requires s.mu held (Start's state write happens-before
// via the same lock); the mapping is immutable after Start.
func (s *Service) stampLocked(sc *SlotCapture) {
	switch {
	case s.profState == ProfilesDegraded:
		sc.unprofiledReason = ReasonProfileError
	case s.profState == ProfilesNoneDeclared:
		sc.unprofiledReason = ReasonNoDeclaration
	case !sc.DialTracked || sc.DialMHz <= 0:
		sc.unprofiledReason = ReasonDialUnattributed
	default:
		// A known dial outside every ADIF band allocation maps to no band
		// and therefore to no declared antenna: band_unmapped, honestly.
		band := utils.FrequencyToBand(strconv.FormatFloat(sc.DialMHz, 'f', -1, 64))
		if pa, ok := s.profActive[band]; ok {
			sc.profileUUID = pa.ProfileUUID
		} else {
			sc.unprofiledReason = ReasonBandUnmapped
		}
	}
}

// profilesStatusLocked builds the Status.Profiles object. Requires s.mu held.
// The db reads run on the shared handle, same as the other Status counts.
func (s *Service) profilesStatusLocked() *ProfilesStatus {
	p := &ProfilesStatus{State: s.profState, Reason: s.profReason}
	if s.db == nil {
		return p
	}
	var lineages, versions int64
	if err := s.db.QueryRow(`SELECT COUNT(DISTINCT lineage), COUNT(*) FROM profiles`).
		Scan(&lineages, &versions); err == nil {
		p.Lineages, p.Versions = &lineages, &versions
	}
	if s.profState == ProfilesActive && len(s.profActive) > 0 {
		p.Active = make(map[string]ProfileActive, len(s.profActive))
		for band, pa := range s.profActive {
			p.Active[band] = pa
		}
	}
	rows, err := s.db.Query(
		`SELECT unprofiled_reason, COUNT(*) FROM observations
		 WHERE unprofiled_reason IS NOT NULL GROUP BY unprofiled_reason`)
	if err != nil {
		return p
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var reason string
		var n int64
		if err := rows.Scan(&reason, &n); err != nil {
			return p
		}
		if p.Unprofiled == nil {
			p.Unprofiled = map[string]int64{}
		}
		p.Unprofiled[reason] = n
	}
	return p
}
