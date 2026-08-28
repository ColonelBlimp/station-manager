package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	stderr "errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite/adapters"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite/models"
	"github.com/ColonelBlimp/station-manager/internal/database/txutil"
	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/origin"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/status"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/aarondl/null/v8"
	"github.com/aarondl/sqlboiler/v4/boil"
	"github.com/aarondl/sqlboiler/v4/queries"
	"github.com/aarondl/sqlboiler/v4/queries/qm"
	boiltypes "github.com/aarondl/sqlboiler/v4/types"
)

/**********************************************************************************************************************
 * QSO Methods
 **********************************************************************************************************************/

func (s *Service) InsertQsoWithContext(ctx context.Context, qso types.Qso) (int64, error) {
	const op errors.Op = "sqlite.Service.InsertQsoWithContext"
	if err := checkService(op, s); err != nil {
		return 0, err
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return 0, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	model, err := adapters.QsoTypeToModel(qso)
	if err != nil {
		return 0, errors.New(op).WithErr(err)
	}

	// Same soft-deleted-parent guard as InsertQsoTx — see requireLiveLogbook.
	// Autocommit here, so the two statements are not one atomic unit; this
	// path is the non-transactional convenience wrapper, and the production
	// submit/import flows go through InsertQsoTx.
	if err = requireLiveLogbook(ctx, h, op, model.LogbookID); err != nil {
		return 0, err
	}

	if err = model.Insert(ctx, h, boil.Infer()); err != nil {
		return 0, errors.New(op).WithErr(err)
	}

	return model.ID, nil
}

// FetchQsoSliceByCallsignWithContext returns prior QSOs matching a
// callsign (contact history). Pass logbookID=0 to search across all
// logbooks; pass a positive value to restrict to one logbook. limit
// caps the result count; pass limit<=0 to apply no cap. Soft-deleted
// rows are hidden.
//
// The callsign match accepts either an exact hit OR a portable-suffix
// variant ("M0CMC/P", "M0CMC/MM", "M0CMC/DX"). Implemented as two
// separate queries (exact equality + GLOB prefix) merged in Go rather
// than a single OR-with-LIKE: the OR confuses the SQLite planner into
// either a full table scan or picking the wrong index, while two
// single-predicate queries each cleanly use `idx_qso_active_call`.
// GLOB is preferred over LIKE for the portable branch because SQLite's
// default LIKE is case-insensitive (prevents index use); GLOB is
// case-sensitive by default and the planner recognises `GLOB 'X/*'`
// as a sargable prefix range. Callers pass the base callsign in
// canonical form (uppercase, trimmed); the handler layer does that
// before calling in.
func (s *Service) FetchQsoSliceByCallsignWithContext(ctx context.Context, callsign string, logbookID int64, limit int) ([]types.ContactHistory, error) {
	const op errors.Op = "sqlite.Service.FetchQsoSliceByCallsignWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}

	callsign = strings.TrimSpace(callsign)
	if callsign == "" {
		return nil, errors.New(op).WithMsg(errMsgEmptyCallsign)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	// Two-query strategy — see the function-level doc comment for the
	// "why." Each branch's WHERE has only equality / range predicates
	// on the indexed `call` column, so the planner reliably picks
	// `idx_qso_active_call`. The previous shape — `(call = X OR call
	// LIKE 'X/%')` — confused the planner badly enough that it
	// fell back to a full table scan once stats were populated
	// (caught during the 2026-05-09 mixed-mode stress run). Splitting
	// the OR is a deterministic fix that also reads more cleanly:
	// each query has a single, obvious purpose.
	//
	// Portable-variants branch uses GLOB instead of LIKE because
	// SQLite's default LIKE is **case-insensitive**, which prevents
	// the planner from using `idx_qso_active_call` for prefix matches
	// (it would have to walk the whole index doing case folding). GLOB
	// is case-sensitive by default and is recognised as a sargable
	// prefix predicate — the planner translates `call GLOB 'X/*'`
	// into a range scan `call >= 'X/' AND call < 'X0'` over the
	// indexed column. Callsigns are uppercase by validator contract
	// (handler upper-cases at submit; storage normalises), so the
	// case-sensitive match is semantically correct.
	//
	// The "X/*" pattern matches portable variants only (M0CMC/P,
	// M0CMC/MM, …) and excludes coincidental prefixes like M0CMCE.
	exactMods := []qm.QueryMod{models.QsoWhere.Call.EQ(callsign)}
	portableMods := []qm.QueryMod{qm.Where("call GLOB ?", callsign+"/*")}
	if logbookID > 0 {
		exactMods = append(exactMods, models.QsoWhere.LogbookID.EQ(logbookID))
		portableMods = append(portableMods, models.QsoWhere.LogbookID.EQ(logbookID))
	}
	exactMods = append(exactMods, qm.OrderBy(models.QsoColumns.CreatedAt+" DESC"))
	portableMods = append(portableMods, qm.OrderBy(models.QsoColumns.CreatedAt+" DESC"))
	if limit > 0 {
		// Each branch is capped at the full limit independently so the
		// merged + truncated result never returns fewer rows than the
		// caller asked for. Worst case we fetch 2N rows and discard
		// half — at typical N≤100 the cost is trivial vs the previous
		// 125k-row scan.
		exactMods = append(exactMods, qm.Limit(limit))
		portableMods = append(portableMods, qm.Limit(limit))
	}

	exact, err := models.Qsos(exactMods...).All(ctx, h)
	if err != nil && !stderr.Is(err, sql.ErrNoRows) {
		return nil, errors.New(op).WithErr(err).WithMsg("Failed to fetch contact history (exact match).")
	}
	portable, err := models.Qsos(portableMods...).All(ctx, h)
	if err != nil && !stderr.Is(err, sql.ErrNoRows) {
		return nil, errors.New(op).WithErr(err).WithMsg("Failed to fetch contact history (portable variants).")
	}

	if len(exact) == 0 && len(portable) == 0 {
		return nil, errors.ErrNotFound
	}

	// Merge: both branches are already CreatedAt-DESC, so a stable
	// merge by CreatedAt produces the right global order. Implemented
	// as a sort.Slice rather than a hand-rolled merge because the
	// total slice is small (≤ 2 × limit; typically ≤ 200 rows) and
	// SQL stability is not guaranteed across releases.
	slice := make(models.QsoSlice, 0, len(exact)+len(portable))
	slice = append(slice, exact...)
	slice = append(slice, portable...)
	sort.SliceStable(slice, func(i, j int) bool {
		return slice[i].CreatedAt.After(slice[j].CreatedAt)
	})
	if limit > 0 && len(slice) > limit {
		slice = slice[:limit]
	}

	history := make([]types.ContactHistory, 0, len(slice))
	for _, qso := range slice {
		typeQso, er := adapters.QsoModelToType(qso)
		if er != nil {
			s.LoggerService.WarnWith().Int64("qso.id", qso.ID).Err(er).Msg("Failed to adapt QSO for contact history.")
			continue
		}
		item := types.ContactHistory{
			ID:      typeQso.ID,
			UUID:    typeQso.UUID,
			Band:    typeQso.Band,
			Freq:    typeQso.Freq,
			Mode:    typeQso.Mode,
			QsoDate: typeQso.QsoDate,
			TimeOn:  typeQso.TimeOn,
			Name:    typeQso.Name,
			Country: typeQso.Country,
			Call:    typeQso.Call,
			RstSent: typeQso.RstSent,
			RstRcvd: typeQso.RstRcvd,
			Notes:   typeQso.Notes,
		}
		history = append(history, item)
	}

	return history, nil
}

func (s *Service) FetchQsoSliceByLogbookIdWithContext(ctx context.Context, id int64) (types.QsoSlice, error) {
	const op errors.Op = "sqlite.Service.FetchQsoSliceByLogbookIdWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}

	if id < 1 {
		return nil, errors.New(op).WithMsg(errMsgInvalidId)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	slice, err := models.Qsos(models.QsoWhere.LogbookID.EQ(id)).All(ctx, h)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("Failed to fetch QSO slice.")
	}

	var typeSlice []types.Qso
	if slice != nil {
		typeSlice = make([]types.Qso, 0, len(slice))

		for _, qso := range slice {
			typeQso, er := adapters.QsoModelToType(qso)
			if er != nil {
				s.LoggerService.WarnWith().Int64("qso.id", qso.ID).Err(er).Msg("Failed to adapt QSO for contact history.")
				continue
			}
			typeSlice = append(typeSlice, typeQso)
		}
	}

	return typeSlice, nil
}

func (s *Service) FetchQsoCountByLogbookIdWithContext(ctx context.Context, id int64, missingFromPrefix string, notEmailed bool) (int64, error) {
	const op errors.Op = "sqlite.Service.FetchQsoCountByLogbookIdWithContext"
	if err := checkService(op, s); err != nil {
		return 0, err
	}

	if id < 1 {
		return 0, errors.New(op).WithMsg(errMsgInvalidId)
	}
	missingMod, err := missingFromUploadMod(op, missingFromPrefix)
	if err != nil {
		return 0, err
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return 0, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	mods := []qm.QueryMod{models.QsoWhere.LogbookID.EQ(id)}
	if missingMod != nil {
		mods = append(mods, missingMod)
	}
	if m := notEmailedMod(notEmailed); m != nil {
		mods = append(mods, m)
	}
	count, err := models.Qsos(mods...).Count(ctx, h)
	if err != nil {
		return 0, errors.New(op).WithErr(err).WithMsg("Failed to fetch QSO count by logbook ID.")
	}

	return count, nil
}

// missingFromUploadMod builds the "not yet uploaded to <destination>" query
// predicate (ADR 0039 manual-backfill filter): the QSO's ADIF upload-status
// stamp for adifPrefix (<prefix>_qso_upload_status in additional_data) is absent
// or not "Y". Empty adifPrefix → (nil, nil) = no restriction. The stamp is the
// durable, import-surviving "uploaded to X?" signal — the SAME source the SPA
// colour and the enqueue skip-check use — so a restored already-uploaded log is
// not surfaced as a false gap. The prefix is validated against adifPrefixPattern
// (an invalid one is a caller bug → error, not a silent no-op); the JSON path is
// passed as a bound parameter so the statement stays static.
func missingFromUploadMod(op errors.Op, adifPrefix string) (qm.QueryMod, error) {
	if adifPrefix == "" {
		return nil, nil
	}
	if !adifPrefixPattern.MatchString(adifPrefix) {
		return nil, errors.New(op).WithMsgf("invalid adifPrefix %q", adifPrefix)
	}
	path := "$." + strings.ToLower(adifPrefix) + "_qso_upload_status"
	return qm.Where("COALESCE(json_extract(additional_data, ?), '') <> ?", path, adif.YesString), nil
}

// notEmailedMod builds the "not yet forwarded by email" predicate behind the
// logbook "Not emailed only" filter: the QSO's sm_fwrd_by_email_status stamp in
// additional_data is absent or not "Y" — the SAME durable stamp
// MarkSessionEmailedAtRevisionWithContext writes and the SPA "Emailed" column reads.
// notEmailed=false → nil (no restriction). Unlike missingFromUploadMod the JSON
// path is a static literal (no forwarder prefix to inject), so there is nothing
// to validate and no error return.
func notEmailedMod(notEmailed bool) qm.QueryMod {
	if !notEmailed {
		return nil
	}
	return qm.Where("COALESCE(json_extract(additional_data, ?), '') <> ?",
		"$.sm_fwrd_by_email_status", adif.YesString)
}

// HasUploadStampWithContext reports whether the QSO's ADIF upload-status stamp
// for adifPrefix is "Y" — i.e. it has already been uploaded to that destination.
// The stamp (written by the worker on a successful insert, atomically with the
// queue row) is the durable per-destination "done" signal that survives
// import/restore, so it is what the manual-backfill skip-check keys on (ADR
// 0039), consistent with the missing_from filter and the SPA colour.
func (s *Service) HasUploadStampWithContext(ctx context.Context, qsoID int64, adifPrefix string) (bool, error) {
	const op errors.Op = "sqlite.Service.HasUploadStampWithContext"
	if err := checkService(op, s); err != nil {
		return false, err
	}
	if qsoID < 1 {
		return false, errors.New(op).WithMsg(errMsgInvalidId)
	}
	if !adifPrefixPattern.MatchString(adifPrefix) {
		return false, errors.New(op).WithMsgf("invalid adifPrefix %q", adifPrefix)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return false, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	path := "$." + strings.ToLower(adifPrefix) + "_qso_upload_status"
	var val sql.NullString
	if err := h.QueryRowContext(ctx,
		`SELECT json_extract(additional_data, ?) FROM qso WHERE id = ?`, path, qsoID,
	).Scan(&val); err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, errors.New(op).WithErr(err)
	}
	return val.Valid && val.String == adif.YesString, nil
}

// HasQsoForCountryWithContext returns true when at least one
// non-deleted QSO row exists with `country = ?`. Used by the
// enrichment orchestrator to decide whether to mark a country as a
// "new entity" for the operator (no prior contact = new). Empty
// country string returns (false, nil) without a query — an empty
// country isn't a meaningful match against existing rows.
//
// Uses Exists() not Count() so the engine can short-circuit on the
// first match. The `idx_qso_country` index covers this lookup; the
// `deleted_at IS NULL` clause is enforced by sqlboiler's default
// soft-delete filter, so deleted rows don't count as prior contact
// (a deleted QSO is functionally a row the operator has decided
// didn't happen).
func (s *Service) HasQsoForCountryWithContext(ctx context.Context, country string) (bool, error) {
	const op errors.Op = "sqlite.Service.HasQsoForCountryWithContext"
	if err := checkService(op, s); err != nil {
		return false, err
	}
	country = strings.TrimSpace(country)
	if country == "" {
		return false, nil
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return false, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	exists, err := models.Qsos(models.QsoWhere.Country.EQ(country)).Exists(ctx, h)
	if err != nil {
		return false, errors.New(op).WithErr(err).WithMsg("checking QSO existence by country")
	}
	return exists, nil
}

// HasQsoForDxccWithContext returns true when at least one non-deleted QSO row
// exists whose stored ADIF DXCC entity code equals `dxcc`. Like
// HasQsoForCountryWithContext it backs the enrichment "new entity" check, but
// matches on the numeric DXCC code rather than the country-name string — the
// code distinguishes split entities (e.g. European vs Asiatic Russia) that the
// display name conflates, and it survives the naming differences between
// hamnut and an imported QSO's country field. An empty code returns
// (false, nil) without a query.
//
// The DXCC code lives in the QSO's additional_data JSON blob (no dedicated
// column), so this uses json_extract — a deliberate raw-SQL fragment, since
// sqlboiler's typed WHERE can't express a JSON path. The default soft-delete
// filter (deleted_at IS NULL) still applies via models.Qsos, so deleted rows
// don't count as prior contact. Not index-backed (a full scan over the JSON);
// fine at a single operator's log size, but a generated column + index is the
// scale-up path if it ever matters.
func (s *Service) HasQsoForDxccWithContext(ctx context.Context, dxcc string) (bool, error) {
	const op errors.Op = "sqlite.Service.HasQsoForDxccWithContext"
	if err := checkService(op, s); err != nil {
		return false, err
	}
	dxcc = strings.TrimSpace(dxcc)
	if dxcc == "" {
		return false, nil
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return false, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	exists, err := models.Qsos(
		qm.Where("json_extract(additional_data, '$.dxcc') = ?", dxcc),
	).Exists(ctx, h)
	if err != nil {
		return false, errors.New(op).WithErr(err).WithMsg("checking QSO existence by dxcc")
	}
	return exists, nil
}

// SchemaVersionWithContext returns the current migration version recorded
// in this connection's primary migration set's tracking table (maintained by
// golang-migrate). Returns 0 if no migrations have been applied yet (fresh
// DB). The `dirty` flag is true if the last migration attempt failed midway.
//
// With the reference.db / log-db split each set has its own
// schema_migrations_<set> table; this reports the first resolved set's version
// (the log set for the default all-domains connection), which is the schema
// version the operator-facing version readout means.
func (s *Service) SchemaVersionWithContext(ctx context.Context) (version uint64, dirty bool, err error) {
	const op errors.Op = "sqlite.Service.SchemaVersionWithContext"
	if err = checkService(op, s); err != nil {
		return 0, false, err
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return 0, false, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	// LIMIT 1 is defensive. golang-migrate guarantees exactly one row in the
	// tracking table (the current state), so the limit never actually clips
	// results — it just pins the query shape in case of corruption. The table
	// name is derived from an internal set constant, never user input.
	tbl := schemaMigrationsTable(s.resolvedMigrationSets()[0])
	row := h.QueryRowContext(ctx, `SELECT version, dirty FROM `+tbl+` LIMIT 1`)
	if err = row.Scan(&version, &dirty); err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, errors.New(op).WithErr(err)
	}
	return version, dirty, nil
}

// updateActiveQso updates a QSO's mutable columns by primary key, but ONLY
// while it is not soft-deleted: the `id = ? AND deleted_at IS NULL` predicate is
// part of the UPDATE itself (not a separate exists-check), so a DELETE that
// commits between a stale PATCH's read and this write cannot be written through.
// Without that, a stale update could mutate a tombstone, report success, and
// enqueue spurious update uploads/history — and the previous generated full-row
// Update(Infer) additionally resurrected the row by writing deleted_at = NULL.
//
// The column map is explicit and omits id / created_at / deleted_at — identity +
// audit columns an edit must never touch (modified_at IS refreshed). It must
// stay in sync with the promoted columns QsoTypeToModel populates. Returns
// errors.ErrNotFound when no active row matched. exec is *sql.DB or *sql.Tx.
func updateActiveQso(ctx context.Context, exec boil.ContextExecutor, model models.Qso) error {
	const op errors.Op = "sqlite.updateActiveQso"
	n, err := models.Qsos(
		models.QsoWhere.ID.EQ(model.ID),
		models.QsoWhere.DeletedAt.IsNull(),
	).UpdateAll(ctx, exec, activeQsoCols(model))
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	if n == 0 {
		return errors.New(op).WithErr(errors.ErrNotFound).WithMsgf("no active QSO with id %d", model.ID)
	}
	return nil
}

// activeQsoCols is the one column map both active-row updaters share, so the
// CAS variant below cannot drift from the plain one.
func activeQsoCols(model models.Qso) models.M {
	return models.M{
		models.QsoColumns.UUID:           model.UUID,
		models.QsoColumns.LogbookID:      model.LogbookID,
		models.QsoColumns.Call:           model.Call,
		models.QsoColumns.Band:           model.Band,
		models.QsoColumns.Mode:           model.Mode,
		models.QsoColumns.Freq:           model.Freq,
		models.QsoColumns.QsoDate:        model.QsoDate,
		models.QsoColumns.TimeOn:         model.TimeOn,
		models.QsoColumns.TimeOff:        model.TimeOff,
		models.QsoColumns.RstSent:        model.RstSent,
		models.QsoColumns.RstRcvd:        model.RstRcvd,
		models.QsoColumns.Country:        model.Country,
		models.QsoColumns.AdditionalData: model.AdditionalData,
		models.QsoColumns.DedupeKey:      model.DedupeKey,
		models.QsoColumns.ModifiedAt:     model.ModifiedAt,
	}
}

// updateActiveQsoAtRevision is updateActiveQso with an optimistic-concurrency
// guard on the trigger-maintained revision counter (ADR 0050; review
// 2026-08-07 #2): the UPDATE matches only while the row still holds the
// caller's fetched revision, so two edits that both read revision N cannot
// both write — the loser's UPDATE matches zero rows. A zero-row result is
// disambiguated with a same-executor active-row probe: still present means
// the revision moved (ErrStaleRevision → the edit path's 409 edit_conflict);
// absent keeps updateActiveQso's ErrNotFound semantics (deleted/vanished →
// 404). The revision column itself stays OUT of the column map — the 0005
// trigger owns the bump.
func updateActiveQsoAtRevision(ctx context.Context, exec boil.ContextExecutor, model models.Qso, expectedRev int64) error {
	const op errors.Op = "sqlite.updateActiveQsoAtRevision"
	n, err := models.Qsos(
		models.QsoWhere.ID.EQ(model.ID),
		models.QsoWhere.DeletedAt.IsNull(),
		models.QsoWhere.Revision.EQ(expectedRev),
	).UpdateAll(ctx, exec, activeQsoCols(model))
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	if n == 0 {
		exists, eerr := models.Qsos(
			models.QsoWhere.ID.EQ(model.ID),
			models.QsoWhere.DeletedAt.IsNull(),
		).Exists(ctx, exec)
		if eerr != nil {
			return errors.New(op).WithErr(eerr).WithMsg("disambiguating zero-row revision-guarded update")
		}
		if exists {
			return errors.New(op).WithErr(errors.ErrStaleRevision).
				WithMsgf("QSO %d changed since revision %d was fetched", model.ID, expectedRev)
		}
		return errors.New(op).WithErr(errors.ErrNotFound).WithMsgf("no active QSO with id %d", model.ID)
	}
	return nil
}

func (s *Service) UpdateQsoWithContext(ctx context.Context, qso types.Qso) error {
	const op errors.Op = "sqlite.Service.UpdateQsoWithContext"
	if err := checkService(op, s); err != nil {
		return err
	}

	if qso.ID < 1 {
		return errors.New(op).WithMsgf("QSO ID is invalid: %d", qso.ID)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	model, err := adapters.QsoTypeToModel(qso)
	if err != nil {
		return errors.New(op).WithErr(err)
	}

	model.ModifiedAt = null.TimeFrom(time.Now().UTC())

	return updateActiveQso(ctx, h, model)
}

func (s *Service) FetchQsoByIdWithContext(ctx context.Context, id int64) (types.Qso, error) {
	const op errors.Op = "sqlite.Service.FetchQsoByIdWithContext"
	if err := checkService(op, s); err != nil {
		return types.Qso{}, err
	}

	if id < 1 {
		return types.Qso{}, errors.New(op).WithMsg(errMsgInvalidId)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return types.Qso{}, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	model, err := models.FindQso(ctx, h, id)
	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return types.Qso{}, errors.ErrNotFound
		}
		return types.Qso{}, errors.New(op).WithErr(err)
	}

	qso, err := adapters.QsoModelToType(model)
	if err != nil {
		return types.Qso{}, errors.New(op).WithErr(err)
	}

	return qso, nil
}

// FetchQsoByUUIDWithContext fetches a QSO by its UUIDv7 (the canonical
// external identifier per ADR 0016). Mirrors FetchQsoByIdWithContext —
// soft-deleted rows return ErrNotFound. Input guarding here is only an
// empty-string reject; the lookup is parameterized (no injection risk) and the
// API route handler validates the path UUIDv7 before calling in, so a malformed
// non-empty string simply matches no row → ErrNotFound.
func (s *Service) FetchQsoByUUIDWithContext(ctx context.Context, uuid string) (types.Qso, error) {
	const op errors.Op = "sqlite.Service.FetchQsoByUUIDWithContext"
	if err := checkService(op, s); err != nil {
		return types.Qso{}, err
	}
	if uuid == "" {
		return types.Qso{}, errors.New(op).WithMsg("uuid is required")
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return types.Qso{}, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	model, err := models.Qsos(qm.Where(models.QsoColumns.UUID+"=?", uuid)).One(ctx, h)
	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return types.Qso{}, errors.ErrNotFound
		}
		return types.Qso{}, errors.New(op).WithErr(err)
	}

	qso, err := adapters.QsoModelToType(model)
	if err != nil {
		return types.Qso{}, errors.New(op).WithErr(err)
	}
	return qso, nil
}

// FetchQsoByUUIDIncludingDeletedWithContext is FetchQsoByUUIDWithContext
// without the soft-delete filter — same role as the int-PK variant for
// the uploads / forwarder paths that need to see soft-deleted rows.
func (s *Service) FetchQsoByUUIDIncludingDeletedWithContext(ctx context.Context, uuid string) (types.Qso, error) {
	const op errors.Op = "sqlite.Service.FetchQsoByUUIDIncludingDeletedWithContext"
	if err := checkService(op, s); err != nil {
		return types.Qso{}, err
	}
	if uuid == "" {
		return types.Qso{}, errors.New(op).WithMsg("uuid is required")
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return types.Qso{}, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	model := &models.Qso{}
	err = models.NewQuery(
		qm.Select("*"),
		qm.From(models.TableNames.Qso),
		qm.Where(models.QsoColumns.UUID+"=?", uuid),
		qm.Limit(1),
	).Bind(ctx, h, model)

	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return types.Qso{}, errors.ErrNotFound
		}
		return types.Qso{}, errors.New(op).WithErr(err)
	}

	qso, err := adapters.QsoModelToType(model)
	if err != nil {
		return types.Qso{}, errors.New(op).WithErr(err)
	}
	return qso, nil
}

// FetchQsoByIDIncludingDeletedWithContext is FetchQsoByIdWithContext without
// the soft-delete filter: it returns the QSO row even when deleted_at is
// non-null. Used by the forwarder worker on delete-action rows — the
// upstream still needs to be told to remove a QSO after the operator
// soft-deleted it locally, and the usual FetchQsoByIdWithContext hides
// the row too aggressively for that case.
//
// Uses models.NewQuery (the generated package's re-export of
// sqlboiler's query builder) rather than models.FindQso or
// models.Qsos(...): both of those bake `WHERE deleted_at IS NULL` into
// their SQL and provide no caller-level mod to bypass it. NewQuery +
// qm mods stays on the sqlboiler-idiomatic path — table and column
// references come from the generated TableNames / QsoColumns constants,
// so a schema rename still propagates through regen.
func (s *Service) FetchQsoByIDIncludingDeletedWithContext(ctx context.Context, id int64) (types.Qso, error) {
	const op errors.Op = "sqlite.Service.FetchQsoByIDIncludingDeletedWithContext"
	if err := checkService(op, s); err != nil {
		return types.Qso{}, err
	}
	if id < 1 {
		return types.Qso{}, errors.New(op).WithMsg(errMsgInvalidId)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return types.Qso{}, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	model := &models.Qso{}
	err = models.NewQuery(
		qm.Select("*"),
		qm.From(models.TableNames.Qso),
		qm.Where(models.QsoColumns.ID+"=?", id),
		qm.Limit(1),
	).Bind(ctx, h, model)

	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return types.Qso{}, errors.ErrNotFound
		}
		return types.Qso{}, errors.New(op).WithErr(err)
	}

	qso, err := adapters.QsoModelToType(model)
	if err != nil {
		return types.Qso{}, errors.New(op).WithErr(err)
	}
	return qso, nil
}

func (s *Service) FetchQsoByDedupeKeyWithContext(ctx context.Context, logbookID int64, dedupeKey string) (types.Qso, error) {
	const op errors.Op = "sqlite.Service.FetchQsoByDedupeKeyWithContext"
	if err := checkService(op, s); err != nil {
		return types.Qso{}, err
	}

	if logbookID < 1 {
		return types.Qso{}, errors.New(op).WithMsg(errMsgInvalidId)
	}
	if dedupeKey == "" {
		return types.Qso{}, errors.New(op).WithMsg("dedupe key cannot be empty")
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return types.Qso{}, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	model, err := models.Qsos(
		models.QsoWhere.LogbookID.EQ(logbookID),
		models.QsoWhere.DedupeKey.EQ(dedupeKey),
		models.QsoWhere.DeletedAt.IsNull(),
	).One(ctx, h)
	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return types.Qso{}, errors.ErrNotFound
		}
		return types.Qso{}, errors.New(op).WithErr(err)
	}

	qso, err := adapters.QsoModelToType(model)
	if err != nil {
		return types.Qso{}, errors.New(op).WithErr(err)
	}

	return qso, nil
}

// FetchQsoPageByLogbookWithContext returns a forward-cursor page of QSOs
// for a logbook, sorted newest-first by (qso_date, time_on, id) DESC.
// Soft-deleted rows are hidden (sqlboiler's default WHERE clause).
//
// Pass afterDate/afterTime empty and afterID=0 for the first page.
// For subsequent pages pass the (qso_date, time_on, id) tuple from the
// last row of the previous page; the query returns rows strictly before
// that tuple in DESC order.
//
// Fetches up to limit+1 rows so the caller can detect "has more" without
// a second query. Caller is responsible for trimming to `limit` and
// emitting the cursor from the last item actually returned.
func (s *Service) FetchQsoPageByLogbookWithContext(
	ctx context.Context,
	logbookID int64,
	afterDate, afterTime string,
	afterID int64,
	limit int,
	missingFromPrefix string,
	notEmailed bool,
) (types.QsoSlice, error) {
	const op errors.Op = "sqlite.Service.FetchQsoPageByLogbookWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}
	if logbookID < 1 {
		return nil, errors.New(op).WithMsg(errMsgInvalidId)
	}
	if limit < 1 {
		return nil, errors.New(op).WithMsgf("limit must be positive, got %d", limit)
	}
	missingMod, err := missingFromUploadMod(op, missingFromPrefix)
	if err != nil {
		return nil, err
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	mods := []qm.QueryMod{
		models.QsoWhere.LogbookID.EQ(logbookID),
	}
	if missingMod != nil {
		mods = append(mods, missingMod)
	}
	if m := notEmailedMod(notEmailed); m != nil {
		mods = append(mods, m)
	}
	// Cursor predicate on the (qso_date, time_on, id) tuple in DESC order:
	// rows strictly before the cursor.
	if afterDate != "" && afterTime != "" && afterID > 0 {
		mods = append(mods, qm.Where(
			"(qso_date < ? OR (qso_date = ? AND time_on < ?) OR (qso_date = ? AND time_on = ? AND id < ?))",
			afterDate,
			afterDate, afterTime,
			afterDate, afterTime, afterID,
		))
	}
	mods = append(mods,
		qm.OrderBy("qso_date DESC, time_on DESC, id DESC"),
		qm.Limit(limit+1),
	)

	slice, err := models.Qsos(mods...).All(ctx, h)
	if err != nil {
		return nil, errors.New(op).WithErr(err)
	}

	out := make(types.QsoSlice, 0, len(slice))
	for _, row := range slice {
		q, er := adapters.QsoModelToType(row)
		if er != nil {
			s.LoggerService.WarnWith().Int64("qso.id", row.ID).Err(er).Msg("failed to adapt QSO row")
			continue
		}
		out = append(out, q)
	}
	return out, nil
}

func (s *Service) FetchQsoSlicePagingWithContext(ctx context.Context, logbookId, pageNum, pageSize int64, ordering Ordering) (types.QsoSlice, error) {
	const op errors.Op = "sqlite.Service.FetchQsoByCallsignWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}

	if logbookId < 1 {
		return nil, errors.New(op).WithMsg(errMsgInvalidId)
	}
	if pageNum < 1 {
		return nil, errors.New(op).WithMsg("Invalid page number. Must be greater than 0.")
	}
	if pageSize < 1 {
		return nil, errors.New(op).WithMsg("Invalid page size. Must be greater than 0.")
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	offset := (pageNum - 1) * pageSize

	var mods []qm.QueryMod
	mods = append(mods, models.QsoWhere.LogbookID.EQ(logbookId))
	mods = append(mods, qm.OrderBy(models.QsoColumns.ID+" "+ordering.sqlDir()))
	mods = append(mods, qm.Limit(int(pageSize)))
	mods = append(mods, qm.Offset(int(offset)))

	slice, err := models.Qsos(mods...).All(ctx, h)
	if err != nil {
		return nil, errors.New(op).WithErr(err)
	}

	var typeSlice []types.Qso
	if slice != nil {
		typeSlice = make([]types.Qso, 0, len(slice))

		for _, qso := range slice {
			typeQso, er := adapters.QsoModelToType(qso)
			if er != nil {
				s.LoggerService.WarnWith().Int64("qso.id", qso.ID).Err(er).Msg("Failed to adapt QSO for contact history.")
				continue
			}
			typeSlice = append(typeSlice, typeQso)
		}
	}

	return typeSlice, nil
}

/**********************************************************************************************************************
 * ContactedStation Methods
 **********************************************************************************************************************/

func (s *Service) FetchContactedStationByCallsignWithContext(ctx context.Context, callsign string) (types.ContactedStation, error) {
	const op errors.Op = "sqlite.Service.FetchContactedStationByCallsignWithContext"
	if err := checkService(op, s); err != nil {
		return types.ContactedStation{}, err
	}

	callsign = strings.TrimSpace(callsign)
	if callsign == "" {
		return types.ContactedStation{}, errors.New(op).WithMsg(errMsgEmptyCallsign)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return types.ContactedStation{}, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	model, err := models.ContactedStations(models.ContactedStationWhere.Call.EQ(callsign)).One(ctx, h)
	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return types.ContactedStation{}, errors.ErrNotFound
		}
		return types.ContactedStation{}, errors.New(op).WithErr(err)
	}

	contactedStation, err := adapters.ContactedStationModelToType(model)
	if err != nil {
		return types.ContactedStation{}, errors.New(op).WithErr(err)
	}

	return contactedStation, nil
}

func (s *Service) InsertContactedStationWithContext(ctx context.Context, station types.ContactedStation) (int64, error) {
	const op errors.Op = "sqlite.Service.InsertContactedStationWithContext"
	if err := checkService(op, s); err != nil {
		return 0, err
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return 0, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	model, err := adapters.ContactedStationTypeToModel(station)
	if err != nil {
		return 0, errors.New(op).WithErr(err)
	}
	if err = model.Insert(ctx, h, boil.Infer()); err != nil {
		return 0, errors.New(op).WithErr(err).WithMsg("Inserting new contacted station failed.")
	}

	return model.ID, nil
}

func (s *Service) UpdateContactedStationWithContext(ctx context.Context, station types.ContactedStation) error {
	const op errors.Op = "sqlite.Service.UpdateContactedStationWithContext"
	if err := checkService(op, s); err != nil {
		return err
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	model, err := adapters.ContactedStationTypeToModel(station)
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	if model.ID < 1 {
		return errors.New(op).WithMsg(errMsgInvalidId)
	}
	model.ModifiedAt = null.TimeFrom(time.Now().UTC())

	// Active-row update (review 2026-06-19 M1, same shape as updateActiveQso /
	// UpdateLogbook): the `id = ? AND deleted_at IS NULL` predicate is in the
	// UPDATE itself, so a soft-deleted row can't be written through (the old
	// generated Update(Infer) matched by PK only and would resurrect a
	// tombstone), and zero rows affected → ErrNotFound instead of a false
	// success. Explicit map omits id / created_at / deleted_at; refreshes
	// modified_at. Keep in sync with the columns ContactedStationTypeToModel sets.
	cols := models.M{
		models.ContactedStationColumns.Name:            model.Name,
		models.ContactedStationColumns.Call:            model.Call,
		models.ContactedStationColumns.Country:         model.Country,
		models.ContactedStationColumns.AdditionalData:  model.AdditionalData,
		models.ContactedStationColumns.LastRefreshedAt: model.LastRefreshedAt,
		models.ContactedStationColumns.ModifiedAt:      model.ModifiedAt,
	}
	n, err := models.ContactedStations(
		models.ContactedStationWhere.ID.EQ(model.ID),
		models.ContactedStationWhere.DeletedAt.IsNull(),
	).UpdateAll(ctx, h, cols)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("Updating contacted station failed.")
	}
	if n == 0 {
		return errors.New(op).WithErr(errors.ErrNotFound).
			WithMsgf("no active contacted station with id %d", model.ID)
	}
	return nil
}

/**********************************************************************************************************************
 * Country Methods
 **********************************************************************************************************************/

func (s *Service) FetchCountryByCallsignWithContext(ctx context.Context, callsign string) (types.Country, error) {
	const op errors.Op = "sqlite.Service.FetchCountryByCallsignWithContext"
	if err := checkService(op, s); err != nil {
		return types.Country{}, err
	}

	callsign = strings.TrimSpace(callsign)
	if callsign == "" {
		return types.Country{}, errors.New(op).WithMsg(errMsgEmptyCallsign)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return types.Country{}, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	// Longest-prefix-match: <callsign> LIKE <prefix> || '%'. Bound `?`
	// is the LHS so it is the literal-text side of LIKE. The RHS reads
	// prefix from the country table; UpsertCountryWithContext rejects
	// rows whose prefix contains LIKE wildcards ('%', '_') or the
	// escape char ('\'), so the pattern is always well-formed without
	// an explicit ESCAPE clause. See review m5 for the threat model.
	mods := []qm.QueryMod{
		qm.Where("? LIKE "+models.TableNames.Country+".prefix || '%'", callsign),
		qm.OrderBy("LENGTH(" + models.TableNames.Country + ".prefix) DESC"),
		qm.Limit(1),
	}

	model, err := models.Countries(mods...).One(ctx, h)
	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return types.Country{}, errors.ErrNotFound
		}
		return types.Country{}, errors.New(op).WithErr(err)
	}

	country, err := adapters.CountryModelToType(model)
	if err != nil {
		return types.Country{}, errors.New(op).WithErr(err)
	}

	return country, nil
}

func (s *Service) FetchCountryByNameWithContext(ctx context.Context, name string) (types.Country, error) {
	const op errors.Op = "sqlite.Service.FetchCountryByNameWithContext"
	if err := checkService(op, s); err != nil {
		return types.Country{}, err
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return types.Country{}, errors.New(op).WithMsg("Country name cannot be empty")
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return types.Country{}, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	model, err := models.Countries(models.CountryWhere.Name.EQ(name)).One(ctx, h)
	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return types.Country{}, errors.ErrNotFound
		}
		return types.Country{}, errors.New(op).WithErr(err)
	}

	country, err := adapters.CountryModelToType(model)
	if err != nil {
		return types.Country{}, errors.New(op).WithErr(err)
	}

	return country, nil
}

// validateCountryPrefix trims country.Prefix and rejects it if empty or if it
// contains a SQL LIKE meta-character. The longest-prefix-match read path
// (FetchCountryByCallsignWithContext) interpolates the prefix directly into a
// `<callsign> LIKE <prefix> || '%'` pattern, so a wildcard ('%', '_') or the
// LIKE escape ('\') in a stored prefix would silently over-match every callsign.
// Centralised here and called from EVERY durable country writer (insert /
// update / upsert) so the invariant can't be bypassed via a direct helper
// (review L1). Writes the trimmed value back through the pointer.
// IsCacheableCountryPrefix reports whether a hamnut country prefix is safe to
// use as a longest-prefix-match cache key. A single-character prefix never is:
// ITU one-letter blocks routinely span multiple DXCC entities ('U' covers
// European/Asiatic Russia AND Ukraine, Uzbekistan, Kazakhstan; 'G' covers
// England AND Scotland/Wales/NI/Jersey/Guernsey/IoM), so a one-char row
// silently claims every callsign in the block. Not hypothetical: a cached
// prefix='U' → "European Russia" row (hamnut's group prefix, cached
// 2026-06-25) misfiled every Ukrainian UR–UZ call until reference migration
// 0002 purged it. Skipping the cache costs only a per-call hamnut lookup for
// calls in those blocks — the enrich result itself is always hamnut's
// per-call resolution — so correctness wins. Exported so the enrichment
// orchestrator can skip the writeback quietly instead of logging a warn per
// cold miss; validateCountryPrefix enforces the same rule as the durable
// invariant at every country writer.
func IsCacheableCountryPrefix(prefix string) bool {
	return len(strings.TrimSpace(prefix)) >= 2
}

func validateCountryPrefix(op errors.Op, country *types.Country) error {
	prefix := strings.TrimSpace(country.Prefix)
	if prefix == "" {
		return errors.New(op).WithMsg("country.Prefix cannot be empty")
	}
	// See IsCacheableCountryPrefix for why one-char prefixes are poison.
	if !IsCacheableCountryPrefix(prefix) {
		return errors.New(op).WithMsgf(
			"country.Prefix %q is a single character; one-letter ITU blocks span multiple DXCC entities and would over-match on the longest-prefix read",
			prefix)
	}
	if strings.ContainsAny(prefix, `%_\`) {
		return errors.New(op).WithMsgf(
			"country.Prefix %q contains SQL LIKE meta-character (%% _ \\); prefixes must be plain alphanumerics",
			prefix)
	}
	country.Prefix = prefix
	return nil
}

func (s *Service) InsertCountryWithContext(ctx context.Context, country types.Country) (int64, error) {
	const op errors.Op = "sqlite.Service.InsertCountryWithContext"
	if err := checkService(op, s); err != nil {
		return 0, err
	}

	if err := validateCountryPrefix(op, &country); err != nil {
		return 0, err
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return 0, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	model, err := adapters.CountryTypeToModel(country)
	if err != nil {
		return 0, errors.New(op).WithErr(err)
	}
	if err = model.Insert(ctx, h, boil.Infer()); err != nil {
		return 0, errors.New(op).WithErr(err).WithMsg("Inserting new country failed.")
	}

	return model.ID, nil
}

func (s *Service) UpdateCountryWithContext(ctx context.Context, country types.Country) error {
	const op errors.Op = "sqlite.Service.UpdateCountryWithContext"
	if err := checkService(op, s); err != nil {
		return err
	}

	if err := validateCountryPrefix(op, &country); err != nil {
		return err
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	model, err := adapters.CountryTypeToModel(country)
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	if model.ID < 1 {
		return errors.New(op).WithMsg(errMsgInvalidId)
	}
	model.ModifiedAt = null.TimeFrom(time.Now().UTC())

	// Active-row update (review 2026-06-19 M1) — see UpdateContactedStation:
	// soft-deleted rows are not written through, and a missing id is ErrNotFound.
	cols := models.M{
		models.CountryColumns.Name:            model.Name,
		models.CountryColumns.CQZone:          model.CQZone,
		models.CountryColumns.ItuZone:         model.ItuZone,
		models.CountryColumns.Continent:       model.Continent,
		models.CountryColumns.Prefix:          model.Prefix,
		models.CountryColumns.Ccode:           model.Ccode,
		models.CountryColumns.DXCCPrefix:      model.DXCCPrefix,
		models.CountryColumns.TimeOffset:      model.TimeOffset,
		models.CountryColumns.LastRefreshedAt: model.LastRefreshedAt,
		models.CountryColumns.ModifiedAt:      model.ModifiedAt,
	}
	n, err := models.Countries(
		models.CountryWhere.ID.EQ(model.ID),
		models.CountryWhere.DeletedAt.IsNull(),
	).UpdateAll(ctx, h, cols)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("Updating country failed.")
	}
	if n == 0 {
		return errors.New(op).WithErr(errors.ErrNotFound).WithMsgf("no active country with id %d", model.ID)
	}
	return nil
}

// FetchCountryByPrefixWithContext returns the country row for an exact
// prefix match. Distinct from FetchCountryByCallsignWithContext, which
// does the longest-prefix-match read-path lookup against a callsign.
// This helper exists for write-path uses (the hamnut upserter checks
// "is there already a row for this exact prefix?") and for tests that
// verify what was just written.
//
// Returns errors.ErrNotFound when no row exists for that prefix.
func (s *Service) FetchCountryByPrefixWithContext(ctx context.Context, prefix string) (types.Country, error) {
	const op errors.Op = "sqlite.Service.FetchCountryByPrefixWithContext"
	if err := checkService(op, s); err != nil {
		return types.Country{}, err
	}

	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return types.Country{}, errors.New(op).WithMsg("prefix cannot be empty")
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return types.Country{}, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	model, err := models.Countries(models.CountryWhere.Prefix.EQ(prefix)).One(ctx, h)
	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return types.Country{}, errors.ErrNotFound
		}
		return types.Country{}, errors.New(op).WithErr(err)
	}

	country, err := adapters.CountryModelToType(model)
	if err != nil {
		return types.Country{}, errors.New(op).WithErr(err)
	}
	return country, nil
}

// UpsertCountryWithContext writes the hamnut result for a prefix —
// inserts on conflict-free, full-row replaces on conflict. Sets
// last_refreshed_at to time.Now().UTC() before write so the orchestrator's
// staleness check sees a fresh row immediately after.
//
// Per ADR 0017 #2 + #4, hamnut is the only writer of country data;
// callsign-class providers must not call this. Per ADR 0017 #11, full
// replace is correct for country because hamnut returns full data on
// every write — there's no operator-typed-partial-then-merge concern
// the way contacted_station has.
//
// Prefix charset invariant (review m5): country.Prefix must contain
// only ASCII alphanumerics. SQL LIKE wildcards ('%', '_') and the
// LIKE escape char ('\') are rejected here because
// FetchCountryByCallsignWithContext interpolates the prefix directly
// into a LIKE pattern (`<callsign> LIKE <prefix>||'%'`). A row with a
// wildcard in its prefix would silently over-match every callsign on
// the longest-prefix-match read path. Hamnut's data is plain
// alphanumeric today; this guard is defence-in-depth for any future
// provider or admin-import write path.
func (s *Service) UpsertCountryWithContext(ctx context.Context, country types.Country) error {
	const op errors.Op = "sqlite.Service.UpsertCountryWithContext"
	if err := checkService(op, s); err != nil {
		return err
	}
	if err := validateCountryPrefix(op, &country); err != nil {
		return err
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	country.LastRefreshedAt = time.Now().UTC()

	// Preserve the existing PK on conflict so sqlboiler's Upsert generates
	// a stable update path; new rows let AUTOINCREMENT assign.
	if existing, ferr := s.FetchCountryByPrefixWithContext(ctx, country.Prefix); ferr == nil {
		country.ID = existing.ID
	} else if !stderr.Is(ferr, errors.ErrNotFound) {
		return errors.New(op).WithErr(ferr)
	}

	model, err := adapters.CountryTypeToModel(country)
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	model.ModifiedAt = null.TimeFrom(time.Now().UTC())

	if err = model.Upsert(
		ctx, h,
		true,               // updateOnConflict
		[]string{"prefix"}, // conflictColumns
		boil.Infer(),       // updateColumns
		boil.Infer(),       // insertColumns
	); err != nil {
		return errors.New(op).WithErr(err).WithMsg("upserting country failed")
	}
	return nil
}

// UpsertContactedStationWithContext writes a callsign-class enrichment
// result OR a QSO-submit-derived snapshot to contacted_station. Both
// write paths share this helper per ADR 0017 #10.
//
// Conflict policy is non-empty-wins-per-field: if the new station has
// a value for a field, it overwrites the existing row's value; if the
// new station has an empty field, the existing row's value is kept.
// The merge runs in Go (read existing, merge, write back) rather than
// in SQL because most fields live in additional_data JSON, where SQL-
// level merging is awkward.
//
// last_refreshed_at is set to time.Now().UTC() on every write — both insert
// and update — so the orchestrator's staleness check works uniformly.
//
// Per ADR 0017 #11, when both old and new have a value for the same
// field, the new value wins (refresh data wins). The qso row preserves
// the original operator-typed value as historical truth, so this is
// not a data-loss concern.
func (s *Service) UpsertContactedStationWithContext(ctx context.Context, station types.ContactedStation) error {
	return s.writeContactedStation(ctx, station, true)
}

// ReplaceContactedStationWithContext writes a callsign-provider refresh result
// to contacted_station, OVERWRITING every column from the incoming station and
// clearing any field the incoming leaves empty. This is the right policy for a
// provider refresh — force-refresh and the async stale-refresh — where the
// upstream is now authoritative, so a field that disappeared upstream must
// disappear from the cache too (review 2026-06-04 H1). Contrast
// UpsertContactedStationWithContext's non-empty-wins merge, which is correct
// only for the QSO-submit partial snapshot. last_refreshed_at is stamped on
// every write and the existing PK is preserved.
func (s *Service) ReplaceContactedStationWithContext(ctx context.Context, station types.ContactedStation) error {
	return s.writeContactedStation(ctx, station, false)
}

// writeContactedStation is the shared insert-or-update for contacted_station.
// On an existing row, merge=true field-merges (non-empty incoming wins, empty
// incoming keeps existing — the QSO-submit snapshot policy) while merge=false
// replaces the row wholesale (empty incoming fields clear — the provider-
// refresh policy). A cold insert is identical for both modes.
func (s *Service) writeContactedStation(ctx context.Context, station types.ContactedStation, merge bool) error {
	const op errors.Op = "sqlite.Service.UpsertContactedStationWithContext"
	if err := checkService(op, s); err != nil {
		return err
	}
	call := strings.TrimSpace(station.Call)
	if call == "" {
		return errors.New(op).WithMsg(errMsgEmptyCallsign)
	}
	station.Call = call

	h, err := s.getOpenHandle(op)
	if err != nil {
		return err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	// The read only supplies the merge BASE; it no longer decides which
	// statement to run. Choosing insert-vs-update from it was a read-then-write
	// race: two concurrent cold misses both saw "no row", and the loser's
	// INSERT then failed the active-callsign unique index (2026-07-22 sqlite
	// review, finding 4). The single upsert below resolves that same collision
	// into an update instead.
	//
	// The merge itself stays in Go: most fields live in the additional_data
	// JSON blob, where SQL-level merging is awkward. That leaves last-writer-
	// wins between two concurrent merges of the SAME row — accepted, not
	// overlooked: contacted_station is a re-derivable enrichment cache, every
	// writer stamps the fields it actually holds, and the next lookup refreshes
	// whatever a loser dropped. Serialising it would need a write-locked
	// transaction around a best-effort cache write on the QSO-submit path.
	existing, ferr := s.FetchContactedStationByCallsignWithContext(ctx, call)
	row := station
	switch {
	case ferr == nil:
		// Existing row. merge → non-empty-wins field merge; replace → the
		// incoming station overwrites all columns (empty fields clear).
		// Preserve PK from the existing row either way.
		if merge {
			row = mergeContactedStation(existing, station)
		}
		row.CSID = existing.CSID

	case stderr.Is(ferr, errors.ErrNotFound):
		// Cold miss — nothing to merge against; `row` is the incoming station.

	default:
		return errors.New(op).WithErr(ferr)
	}
	// One choke point for all three routes above (merge, replace, cold miss):
	// coordinates must not contradict the gridsquare they are stored beside.
	// The QSO-row write path applies the SAME function from its own choke point
	// (adapters.QsoTypeToModel) — guarding only this one left the copy that
	// actually leaves the station unreconciled (dogfood 2026-08-05).
	row = adapters.ReconcileStationCoords(row)
	row.LastRefreshedAt = time.Now().UTC()

	model, mErr := adapters.ContactedStationTypeToModel(row)
	if mErr != nil {
		return errors.New(op).WithErr(mErr)
	}

	// Conflict target is the PARTIAL unique index, so it matches exactly the
	// row the read above can return (the fetch is soft-delete filtered) — a
	// tombstone for the same callsign neither blocks the insert nor gets
	// resurrected. created_at keeps its column default on insert and is never
	// touched on update, matching sqlboiler's generated behaviour.
	if _, wErr := h.ExecContext(ctx, `
		INSERT INTO contacted_station (name, call, country, additional_data, last_refreshed_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (call) WHERE deleted_at IS NULL DO UPDATE SET
			name              = excluded.name,
			country           = excluded.country,
			additional_data   = excluded.additional_data,
			last_refreshed_at = excluded.last_refreshed_at,
			modified_at       = ?`,
		model.Name, model.Call, model.Country, model.AdditionalData, model.LastRefreshedAt,
		null.TimeFrom(time.Now().UTC()),
	); wErr != nil {
		return errors.New(op).WithErr(wErr).WithMsg("writing contacted_station failed")
	}
	return nil
}

// mergeContactedStation returns a copy of base with each field replaced
// by the corresponding field from incoming when incoming's field is
// non-empty (non-zero for typed values). Implements the ADR 0017 #11
// "refresh data wins, but only on the fields the writer actually
// supplied" merge semantic for contacted_station upserts.
//
// Internal to the Upsert helper — kept here so the merge rule lives
// next to the helper that uses it.
func mergeContactedStation(base, incoming types.ContactedStation) types.ContactedStation {
	merged := base
	if incoming.Address != "" {
		merged.Address = incoming.Address
	}
	if incoming.Age != "" {
		merged.Age = incoming.Age
	}
	if incoming.Altitude != "" {
		merged.Altitude = incoming.Altitude
	}
	// Call is the lookup key — incoming.Call must equal base.Call for a
	// merge to make sense; the caller has already trimmed and validated
	// it. No conditional needed.
	merged.Call = incoming.Call
	if incoming.Cont != "" {
		merged.Cont = incoming.Cont
	}
	if incoming.ContactedOp != "" {
		merged.ContactedOp = incoming.ContactedOp
	}
	if incoming.Country != "" {
		merged.Country = incoming.Country
	}
	if incoming.CQZ != "" {
		merged.CQZ = incoming.CQZ
	}
	if incoming.DXCC != "" {
		merged.DXCC = incoming.DXCC
	}
	if incoming.Email != "" {
		merged.Email = incoming.Email
	}
	if incoming.EqCall != "" {
		merged.EqCall = incoming.EqCall
	}
	if incoming.Gridsquare != "" {
		merged.Gridsquare = incoming.Gridsquare
	}
	if incoming.Iota != "" {
		merged.Iota = incoming.Iota
	}
	if incoming.IotaIslandId != "" {
		merged.IotaIslandId = incoming.IotaIslandId
	}
	if incoming.ITUZ != "" {
		merged.ITUZ = incoming.ITUZ
	}
	if incoming.Lat != "" {
		merged.Lat = incoming.Lat
	}
	if incoming.Lon != "" {
		merged.Lon = incoming.Lon
	}
	if incoming.Name != "" {
		merged.Name = incoming.Name
	}
	if incoming.QTH != "" {
		merged.QTH = incoming.QTH
	}
	if incoming.Sig != "" {
		merged.Sig = incoming.Sig
	}
	if incoming.SigInfo != "" {
		merged.SigInfo = incoming.SigInfo
	}
	if incoming.Web != "" {
		merged.Web = incoming.Web
	}
	if incoming.WwffRef != "" {
		merged.WwffRef = incoming.WwffRef
	}
	return merged
}

/**********************************************************************************************************************
 * Logbook Methods
 **********************************************************************************************************************/

// LogbookCallsignByIDWithContext returns the callsign of a logbook by
// ID. Cheaper than FetchLogbookByIDWithContext when the caller only
// needs the callsign (notably the 'submit' hot path, which compares it
// against STATION_CALLSIGN). Runs
//
//	SELECT callsign FROM logbook WHERE id=? AND deleted_at IS NULL
//
// and skips the full row scan + adapter work. Returns ErrNotFound if
// the logbook doesn't exist or is soft-deleted.
func (s *Service) LogbookCallsignByIDWithContext(ctx context.Context, id int64) (string, error) {
	const op errors.Op = "sqlite.Service.LogbookCallsignByIDWithContext"
	if err := checkService(op, s); err != nil {
		return "", err
	}

	if id < 1 {
		return "", errors.New(op).WithMsg(errMsgInvalidId)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return "", err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	var callsign string
	row := h.QueryRowContext(ctx,
		`SELECT callsign FROM logbook WHERE id=? AND deleted_at IS NULL`, id)
	if err = row.Scan(&callsign); err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return "", errors.ErrNotFound
		}
		return "", errors.New(op).WithErr(err)
	}
	return callsign, nil
}

// LogbookExistsByIDWithContext is a lightweight "does this logbook exist
// (and isn't soft-deleted)" check. Use this in preference to
// FetchLogbookByIDWithContext anywhere the handler only needs to know
// whether the row exists — it runs `SELECT EXISTS(...)` (an index
// probe) and skips the row scan + adapter work.
func (s *Service) LogbookExistsByIDWithContext(ctx context.Context, id int64) (bool, error) {
	const op errors.Op = "sqlite.Service.LogbookExistsByIDWithContext"
	if err := checkService(op, s); err != nil {
		return false, err
	}

	if id < 1 {
		return false, errors.New(op).WithMsg(errMsgInvalidId)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return false, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	// models.LogbookExists runs
	//   SELECT EXISTS(SELECT 1 FROM logbook WHERE id=? AND deleted_at IS NULL LIMIT 1)
	// which is an index-only probe — no row data read, no adapter call.
	exists, err := models.LogbookExists(ctx, h, id)
	if err != nil {
		return false, errors.New(op).WithErr(err)
	}
	return exists, nil
}

func (s *Service) FetchLogbookByIDWithContext(ctx context.Context, id int64) (types.Logbook, error) {
	const op errors.Op = "sqlite.Service.FetchLogbookByIDWithContext"
	if err := checkService(op, s); err != nil {
		return types.Logbook{}, err
	}

	if id < 1 {
		return types.Logbook{}, errors.New(op).WithMsg(errMsgInvalidId)
	}
	h, err := s.getOpenHandle(op)
	if err != nil {
		return types.Logbook{}, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	model, err := models.FindLogbook(ctx, h, id)
	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return types.Logbook{}, errors.ErrNotFound
		}
		return types.Logbook{}, errors.New(op).WithErr(err)
	}

	logbook, err := adapters.LogbookModelToType(model)
	if err != nil {
		return types.Logbook{}, errors.New(op).WithErr(err)
	}

	return logbook, nil
}

func (s *Service) FetchAllLogbooksWithContext(ctx context.Context) ([]types.Logbook, error) {
	const op errors.Op = "sqlite.Service.FetchAllLogbooksWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	list, err := models.Logbooks().All(ctx, h)
	if err != nil {
		return nil, errors.New(op).WithErr(err)
	}

	result := make([]types.Logbook, 0, len(list))
	for _, logbook := range list {
		typeLogbook, er := adapters.LogbookModelToType(logbook)
		if er != nil {
			return nil, errors.New(op).WithErr(er).WithMsg("Converting logbook model to type failed.")
		}
		result = append(result, typeLogbook)
	}

	return result, nil
}

func (s *Service) InsertLogbookWithContext(ctx context.Context, logbook types.Logbook) (int64, error) {
	const op errors.Op = "sqlite.Service.InsertLogbookWithContext"
	if err := checkService(op, s); err != nil {
		return 0, err
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return 0, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	model, err := adapters.LogbookTypeToModel(logbook)
	if err != nil {
		return 0, errors.New(op).WithErr(err)
	}
	if err = model.Insert(ctx, h, boil.Infer()); err != nil {
		// UNIQUE on logbook.name fires when the operator tries to
		// create a logbook whose name already exists. Promote to a
		// typed sentinel so the handler maps it to 409 via errors.Is
		// rather than string-matching the driver's message.
		if isUniqueConstraintError(err) {
			return 0, errors.New(op).WithErr(errors.ErrDuplicateName)
		}
		return 0, errors.New(op).WithErr(err).WithMsg("Inserting new logbook failed.")
	}

	return model.ID, nil
}

func (s *Service) DeleteLogbookByIDWithContext(ctx context.Context, id int64) error {
	const op errors.Op = "sqlite.Service.DeleteLogbookWithContext"
	if err := checkService(op, s); err != nil {
		return err
	}

	if id < 1 {
		return errors.New(op).WithMsg(errMsgInvalidId)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	// One conditional statement carries BOTH preconditions — the logbook is
	// live AND it holds no live QSOs — so the check cannot be outrun by a
	// concurrent submit (2026-07-22 sqlite review, finding 2). The previous
	// shape read those facts in separate statements and only then soft-
	// deleted, leaving a window for the interleaving "delete preflight sees
	// no QSOs → a submit commits a QSO → soft-delete lands", which orphans a
	// live QSO under a deleted logbook. The FK is no help: ON DELETE RESTRICT
	// fires on hard deletes only, and a soft-deleted parent still physically
	// satisfies it. (The symmetric guard on the insert side lives in
	// InsertQsoTx / InsertQsoWithContext.)
	//
	// NOT EXISTS matches .Exists() semantics — it short-circuits at the first
	// hit via idx_qso_logbook_id rather than scanning the partition — and
	// counts only LIVE QSOs, preserving the existing rule that a logbook
	// whose QSOs are all soft-deleted may still be deleted.
	//
	// deleted_at is stamped in UTC to match sqlboiler's generated soft-delete
	// (boil's timestamp location defaults to UTC).
	res, err := h.ExecContext(ctx, `
		UPDATE logbook SET deleted_at = ?
		WHERE id = ?
		  AND deleted_at IS NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM qso
		      WHERE qso.logbook_id = logbook.id AND qso.deleted_at IS NULL
		  )`, time.Now().UTC(), id)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("failed to delete logbook")
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("rows affected by logbook delete")
	}
	if affected > 0 {
		return nil
	}

	// No row matched: one of the two preconditions failed. Re-read to report
	// WHICH — this is diagnosis for the error message only, the mutation
	// above was already all-or-nothing. A logbook that is no longer live
	// (never existed, or already soft-deleted) is ErrNotFound, matching
	// FindLogbook's deleted_at-filtered semantics; anything else means the
	// QSO precondition is what blocked it. If a concurrent writer soft-
	// deleted the last QSO between the two statements this reports the
	// slightly-stale "has QSOs" — accurate for the moment the delete was
	// attempted, and it keeps the caller's two-outcome contract intact.
	var live bool
	if err = h.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM logbook WHERE id = ? AND deleted_at IS NULL)`,
		id).Scan(&live); err != nil {
		return errors.New(op).WithErr(err).WithMsg("classifying logbook delete failure")
	}
	if !live {
		return errors.ErrNotFound
	}
	return errors.New(op).WithErr(errors.ErrLogbookHasQsos)
}

func (s *Service) UpdateLogbookWithContext(ctx context.Context, logbook types.Logbook) error {
	const op errors.Op = "sqlite.Service.UpdateLogbookWithContext"
	if err := checkService(op, s); err != nil {
		return err
	}

	if logbook.ID < 1 {
		return errors.New(op).WithMsg(errMsgInvalidId)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	// Active-row update (review internal-api M3, same shape as updateActiveQso):
	// the `id = ? AND deleted_at IS NULL` predicate is in the UPDATE itself, so a
	// DELETE that commits between the handler's read and this write can't be
	// written through — no resurrection of a soft-deleted logbook. The column map
	// omits id / created_at / deleted_at and refreshes modified_at. Callsign is
	// included (unchanged by the handler, which only patches name/description) so
	// the stored value is preserved. Zero rows affected → ErrNotFound.
	cols := models.M{
		models.LogbookColumns.Name:        logbook.Name,
		models.LogbookColumns.Callsign:    logbook.Callsign,
		models.LogbookColumns.Description: null.StringFrom(logbook.Description),
		models.LogbookColumns.ModifiedAt:  null.TimeFrom(time.Now().UTC()),
	}
	n, err := models.Logbooks(
		models.LogbookWhere.ID.EQ(logbook.ID),
		models.LogbookWhere.DeletedAt.IsNull(),
	).UpdateAll(ctx, h, cols)
	if err != nil {
		// Same UNIQUE-on-name path as InsertLogbookWithContext: if the rename
		// collides with an existing logbook, surface a typed sentinel so the
		// handler can return 409 via errors.Is.
		if isUniqueConstraintError(err) {
			return errors.New(op).WithErr(errors.ErrDuplicateName)
		}
		return errors.New(op).WithErr(err).WithMsg("failed to update logbook")
	}
	if n == 0 {
		return errors.New(op).WithErr(errors.ErrNotFound).WithMsgf("no active logbook with id %d", logbook.ID)
	}

	return nil
}

/**********************************************************************************************************************
 * Contest Related Methods
 **********************************************************************************************************************/

// IsContestDuplicateByLogbookIDWithContext reports whether the given
// callsign has already been worked on (band [, mode]) within the given
// logbook. Soft-deleted rows are excluded by sqlboiler's default.
//
// Pass mode="" for band-only contests (ARRL DX, etc.). Pass mode="SSB"
// (or similar) for band+mode contests (CQ WW, etc.). The client owns the
// contest rule; the daemon just answers the filtered existence question.
func (s *Service) IsContestDuplicateByLogbookIDWithContext(ctx context.Context, id int64, callsign, band, mode string) (bool, error) {
	const op errors.Op = "sqlite.Service.IsContestDuplicateByLogbookIDWithContext"
	if err := checkService(op, s); err != nil {
		return false, err
	}

	if id < 1 {
		return false, errors.New(op).WithMsg(errMsgInvalidId)
	}
	callsign = strings.TrimSpace(callsign)
	if callsign == "" {
		return false, errors.New(op).WithMsg("Callsign cannot be empty")
	}

	band = strings.TrimSpace(band)
	if band == "" {
		return false, errors.New(op).WithMsg("Band cannot be empty")
	}

	mode = strings.TrimSpace(mode)

	h, err := s.getOpenHandle(op)
	if err != nil {
		return false, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	mods := []qm.QueryMod{
		models.QsoWhere.Call.EQ(callsign),
		models.QsoWhere.Band.EQ(band),
		models.QsoWhere.LogbookID.EQ(id),
	}
	if mode != "" {
		mods = append(mods, models.QsoWhere.Mode.EQ(mode))
	}

	exists, err := models.Qsos(mods...).Exists(ctx, h)
	if err != nil {
		return false, errors.New(op).WithErr(err)
	}

	return exists, nil
}

/**********************************************************************************************************************
 * Upload Methods — forwarder worker lifecycle over qso_upload rows.
 * See docs/v2-design/forwarding.md §4 and §7 for the design.
 **********************************************************************************************************************/

// ClaimPendingUploadsWithContext atomically transitions up to `limit`
// pending rows for the given forwarder from 'pending' to 'in_progress'
// and returns them. Rows are selected by next_attempt_at <= now (so
// retries don't jump the queue) and ordered deterministically by
// (next_attempt_at, qso_id, action-priority, id) — action-priority being
// insert < update < delete — so when several lifecycle rows for one QSO are
// pending at once they are claimed and processed in applied order rather than
// the same-second nondeterministic order the bare next_attempt_at key gave
// (review 2026-06-05 M2). The returned slice is re-sorted on the same key in
// Go because UPDATE ... RETURNING row order is unspecified. Returns an empty
// slice when there is nothing to claim.
//
// SQLite is single-writer, so the UPDATE...RETURNING is race-free by
// construction; the per-forwarder scope (forwarder_name = ?) also
// means two workers for different destinations never compete for a
// row.
//
// Load-bearing invariant: at most ONE worker per forwarder_name may
// call this concurrently. Violating it does not produce a per-row
// race (the atomic UPDATE still assigns each row to a single caller),
// but both callers would claim disjoint subsets of the pending set
// for the same destination, share any mutable state on the
// forwarding.Forwarder instance, and double-spend the upstream's
// rate budget. Enforcement lives at config load (unique Name per
// ForwarderConfig) and at spawn in cmd/smd/main.go (one
// spawnForwarderWorkers iteration over the validated set). Nothing
// at this layer checks it; don't add a second spawn site without
// auditing that chain.
//
// This method uses raw SQL rather than sqlboiler-generated queries
// because the shape requires two features sqlboiler does not express
// cleanly: (1) `UPDATE ... RETURNING *` — sqlboiler's UpdateAll
// returns rows-affected, not the modified rows, forcing an extra
// SELECT that reintroduces the very claim-race we used RETURNING to
// close; and (2) the `WHERE id IN (SELECT ... LIMIT N)` subquery,
// which sqlboiler query-mods don't compose naturally against the
// same table. The sibling methods (MarkUpload*, ResetOrphanedUploads)
// use sqlboiler idioms per the project convention.
func (s *Service) ClaimPendingUploadsWithContext(ctx context.Context, forwarderName string, limit int) ([]types.QsoUpload, error) {
	const op errors.Op = "sqlite.Service.ClaimPendingUploadsWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}
	if forwarderName == "" {
		return nil, errors.New(op).WithMsg("forwarderName is empty")
	}
	if limit < 1 {
		return nil, errors.New(op).WithMsg("limit must be >= 1")
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	// modified_at is maintained by the trg_qso_upload_set_updated_at
	// trigger, so we don't set it here.
	const q = `
UPDATE qso_upload
SET    status = ?,
       last_attempt_at = ?
WHERE  id IN (
    SELECT id FROM qso_upload
    WHERE  forwarder_name = ?
      AND  status         = ?
      AND  next_attempt_at <= ?
    ORDER BY next_attempt_at,
             qso_id,
             CASE action WHEN 'insert' THEN 0 WHEN 'update' THEN 1 WHEN 'delete' THEN 2 ELSE 3 END,
             id
    LIMIT  ?
)
RETURNING *`

	now := time.Now().Unix()

	var rows []*models.QsoUpload
	err = queries.Raw(q,
		status.InProgress.String(),
		now,
		forwarderName,
		status.Pending.String(),
		now,
		limit,
	).Bind(ctx, h, &rows)
	if err != nil && !stderr.Is(err, sql.ErrNoRows) {
		return nil, errors.New(op).WithErr(err).WithMsg("claim pending uploads")
	}

	out := make([]types.QsoUpload, 0, len(rows))
	for _, r := range rows {
		u, er := adapters.QsoUploadModelToType(r)
		if er != nil {
			return nil, errors.New(op).WithErr(er)
		}
		out = append(out, u)
	}

	// SQLite does not guarantee the order of UPDATE ... RETURNING rows even
	// though the subquery is ordered (the subquery's ORDER BY only decides
	// WHICH rows are claimed under LIMIT). Re-sort the claimed batch in Go on
	// the same key so the worker processes co-pending rows for one QSO in
	// insert→update→delete order (review 2026-06-05 M2).
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.NextAttemptAt != b.NextAttemptAt {
			return a.NextAttemptAt < b.NextAttemptAt
		}
		if a.QsoID != b.QsoID {
			return a.QsoID < b.QsoID
		}
		if pa, pb := uploadActionOrder(a.Action), uploadActionOrder(b.Action); pa != pb {
			return pa < pb
		}
		return a.ID < b.ID
	})
	return out, nil
}

// uploadActionOrder ranks a qso_upload action for deterministic claim
// processing: insert before update before delete, so a QSO's lifecycle rows
// (when several are pending at once) are forwarded in the order they were
// applied locally. Must match the SQL CASE in ClaimPendingUploadsWithContext.
func uploadActionOrder(a string) int {
	switch a {
	case action.Insert.String():
		return 0
	case action.Update.String():
		return 1
	case action.Delete.String():
		return 2
	default:
		return 3
	}
}

// MarkUploadSuccessWithContext records a successful Submit outcome:
// status → 'uploaded', attempts bumped, upstream_id persisted
// (empty string becomes NULL), last_error cleared.
func (s *Service) MarkUploadSuccessWithContext(ctx context.Context, id int64, upstreamID string) error {
	const op errors.Op = "sqlite.Service.MarkUploadSuccessWithContext"
	if err := checkService(op, s); err != nil {
		return err
	}
	if id < 1 {
		return errors.New(op).WithMsg(errMsgInvalidId)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	var upstreamArg any
	if upstreamID != "" {
		upstreamArg = upstreamID
	}
	// Conditional on status='in_progress' so a worker whose row was re-armed to
	// 'pending' by a concurrent operator edit mid-send cannot clobber it back to
	// 'uploaded' (stale-completion race). Zero rows affected = re-armed or gone
	// → no-op: the re-armed row stays pending to be re-claimed and re-forwarded
	// with the latest state.
	res, err := h.ExecContext(ctx, `
UPDATE qso_upload
SET    status      = ?,
       attempts    = attempts + 1,
       upstream_id = ?,
       last_error  = NULL
WHERE  id = ? AND status = ?`,
		status.Uploaded.String(), upstreamArg, id, status.InProgress.String())
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("mark upload success")
	}
	n, err := checkedRowsAffected(op, res, "mark upload success")
	if err != nil {
		return err
	}
	if n == 0 {
		return s.classifyZeroRowCompletion(ctx, h, id, "success")
	}
	return nil
}

// adifPrefixPattern enforces a safe shape for ADIF field prefixes
// before they are concatenated into column names. Prefixes come from
// Forwarder.AdifPrefix() — our own code — but the validator guards
// against typos (e.g. a stray lowercase letter or space) that would
// otherwise slip a malformed column name into a raw UPDATE. The ADIF
// specification uses uppercase ASCII for field-name prefixes.
var adifPrefixPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]*$`)

// MarkUploadSuccessWithAdifStampWithContext records a successful
// Submit outcome AND stamps the QSO row's per-destination ADIF
// upload-status fields, all in one transaction. The one-fails-all-
// fail invariant (docs/v1-analysis/invariants.md) requires that the
// operator never sees a queue row in state `uploaded` without the
// corresponding QSO-row stamp reflecting it, and vice versa.
//
// The stamp lands inside the qso row's `additional_data` JSON blob
// (the project idiom — additional_data absorbs ADIF spec evolution,
// per project_sm_design_invariants memory). Two JSON keys are
// written via SQLite's json_set function:
//
//	$.<prefix_lower>_qso_upload_status = "Y"          (adif.YesString)
//	$.<prefix_lower>_qso_upload_date   = today (UTC, YYYYMMDD)
//
// For QRZ (adifPrefix = "QRZCOM") that's
// $.qrzcom_qso_upload_status / $.qrzcom_qso_upload_date. These keys
// match the JSON tags on types.Qso so the next read via
// QsoModelToType surfaces the stamp as struct fields automatically.
//
// The caller (worker) only invokes this for action != delete with a
// non-empty AdifPrefix — delete never stamps (local QSO is
// soft-deleted so "uploaded" on a tombstone is nonsensical), and a
// forwarder with no ADIF slot uses MarkUploadSuccessWithContext.
//
// Adding a new forwarder does not require changing this method or
// the schema: prefix-agnostic keys mean any validated prefix just
// works.
//
// Returns ErrNotFound if either id or qsoID points at a missing row.
func (s *Service) MarkUploadSuccessWithAdifStampWithContext(
	ctx context.Context, id int64, upstreamID string, qsoID int64, adifPrefix string,
) (err error) {
	const op errors.Op = "sqlite.Service.MarkUploadSuccessWithAdifStampWithContext"
	if err := checkService(op, s); err != nil {
		return err
	}
	if id < 1 {
		return errors.New(op).WithMsg(errMsgInvalidId)
	}
	if qsoID < 1 {
		return errors.New(op).WithMsgf("qsoID must be >= 1, got %d", qsoID)
	}
	if !adifPrefixPattern.MatchString(adifPrefix) {
		return errors.New(op).WithMsgf(
			"invalid adifPrefix %q (must match %s)", adifPrefix, adifPrefixPattern,
		)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	tx, err := h.BeginTx(ctx, nil)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("begin tx")
	}
	defer txutil.Rollback(tx, &err)

	// Step 1 — qso_upload: same shape as MarkUploadSuccessWithContext.
	// Done via raw SQL (rather than sqlboiler Find+Update) to share one
	// transaction with step 2.
	var upstreamArg any
	if upstreamID != "" {
		upstreamArg = upstreamID
	} else {
		upstreamArg = nil
	}
	res, err := tx.ExecContext(ctx, `
UPDATE qso_upload
SET    status      = ?,
       attempts    = attempts + 1,
       upstream_id = ?,
       last_error  = NULL
WHERE  id = ? AND status = ?`,
		status.Uploaded.String(), upstreamArg, id, status.InProgress.String(),
	)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("update qso_upload")
	}
	n, err := checkedRowsAffected(op, res, "update qso_upload")
	if err != nil {
		return err
	}
	if n == 0 {
		// Re-armed by a concurrent edit (or gone): skip the QSO stamp and let
		// the deferred rollback discard the (no-op) tx — don't mark the QSO
		// uploaded for a send whose row no longer represents the latest state.
		return s.classifyZeroRowCompletion(ctx, tx, id, "success+stamp")
	}

	// Step 2 — qso row ADIF stamp via json_set on additional_data.
	// The JSON paths are built from the validated prefix; both paths
	// and values are bound parameters so the statement itself is
	// static (defence-in-depth beyond the regex validator above).
	// today is UTC per the ADIF spec.
	prefixLower := strings.ToLower(adifPrefix)
	statusPath := "$." + prefixLower + "_qso_upload_status"
	datePath := "$." + prefixLower + "_qso_upload_date"
	today := time.Now().UTC().Format("20060102")
	res, err = tx.ExecContext(ctx, `
UPDATE qso
SET    additional_data = json_set(additional_data, ?, ?, ?, ?)
WHERE  id = ?`,
		statusPath, adif.YesString, datePath, today, qsoID,
	)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsgf(
			"stamp qso row with prefix %q", adifPrefix,
		)
	}
	n, err = checkedRowsAffected(op, res, "stamp qso row")
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.ErrNotFound
	}

	if err := tx.Commit(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("commit")
	}
	return nil
}

// SessionEmailTarget names a QSO row to stamp as session-emailed, together with the
// revision that was composed into the sent attachment (PT-3, W-0008). The stamp
// matches on BOTH id and revision so a row edited (or deleted) between composition
// and stamp is not marked as carrying content it never sent.
type SessionEmailTarget struct {
	ID       int64
	Revision int64
}

// MarkSessionEmailedAtRevisionWithContext stamps the "forwarded by email" ADIF
// fields on the QSO rows that are STILL at the revision composed into the sent
// attachment, after a successful session email. It is the manual-send analogue of
// the forwarder's MarkUploadSuccessWithAdifStampWithContext: the stamp lands inside
// each qso row's `additional_data` JSON blob via json_set, writing
//
//	$.sm_fwrd_by_email_status = "Y"          (adif.YesString)
//	$.sm_fwrd_by_email_date   = stampDate (UTC YYYYMMDD, from the caller)
//
// These keys match the JSON tags on types.Qso (SmFwrdByEmailStatus /
// SmFwrdByEmailDate), so the next read via QsoModelToType surfaces the stamp as
// struct fields automatically — no schema column, no migration.
//
// The whole set is stamped in one atomic UPDATE. A row matches only when its id AND
// revision match a target AND it is not soft-deleted, so a QSO edited (its revision
// bumped by the 0005 trigger) or deleted after composition matches nothing and is
// left unmarked — a durable "emailed" stamp therefore certifies that the marked
// content is the content that was sent (PT-3). RETURNING reports the ids actually
// stamped; SQLite does not guarantee its row order, so the caller keys by id.
// Stamping a row already marked is harmless — json_set overwrites the date with the
// latest send. stampDate comes from the CALLER so the date it reports onward matches
// the date stored (codex 2026-08-08 P3).
func (s *Service) MarkSessionEmailedAtRevisionWithContext(ctx context.Context, targets []SessionEmailTarget, stampDate string) ([]int64, error) {
	const op errors.Op = "sqlite.Service.MarkSessionEmailedAtRevisionWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, nil
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	// Carry the whole target set in ONE JSON-array argument, matched through
	// json_each — NOT a generated OR-chain or a multi-row VALUES. At the documented
	// 10,000-target request cap an OR-chain would blow SQLITE_MAX_EXPR_DEPTH (1000)
	// and a multi-row VALUES risks SQLITE_MAX_COMPOUND_SELECT (500); json_each keeps
	// the statement at a constant three bound parameters at any scale. Every
	// id/revision rides inside the JSON (no interpolation of caller data); the two
	// stamp values are static literals.
	pairs := make([][2]int64, len(targets))
	for i, tgt := range targets {
		pairs[i] = [2]int64{tgt.ID, tgt.Revision}
	}
	targetsJSON, err := json.Marshal(pairs)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("marshal stamp targets")
	}

	const query = `
UPDATE qso
SET    additional_data = json_set(
           additional_data,
           '$.sm_fwrd_by_email_status', ?,
           '$.sm_fwrd_by_email_date', ?
       )
WHERE  deleted_at IS NULL
  AND  (id, revision) IN (
           SELECT json_extract(value, '$[0]'), json_extract(value, '$[1]')
           FROM   json_each(?)
       )
RETURNING id`

	rows, err := h.QueryContext(ctx, query, adif.YesString, stampDate, string(targetsJSON))
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("stamp session-emailed flag")
	}
	defer func() { _ = rows.Close() }()

	var stamped []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, errors.New(op).WithErr(err).WithMsg("scan stamped id")
		}
		stamped = append(stamped, id)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("iterate stamped ids")
	}
	return stamped, nil
}

// MarkUploadTransientRetryWithContext records a transient failure
// that is eligible for another attempt: status → 'pending' (so the
// worker's next claim picks it up), attempts bumped, next_attempt_at
// set (caller computes the backoff per docs/v2-design/forwarding.md
// §5), last_error stored.
func (s *Service) MarkUploadTransientRetryWithContext(ctx context.Context, id int64, nextAttemptAt int64, lastError string) error {
	const op errors.Op = "sqlite.Service.MarkUploadTransientRetryWithContext"
	if err := checkService(op, s); err != nil {
		return err
	}
	if id < 1 {
		return errors.New(op).WithMsg(errMsgInvalidId)
	}
	if nextAttemptAt < 1 {
		return errors.New(op).WithMsg("nextAttemptAt must be >= 1")
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	var lastErrArg any
	if lastError != "" {
		lastErrArg = lastError
	}
	// Conditional on status='in_progress' — see MarkUploadSuccessWithContext.
	// A re-armed row (operator edit) already cleared its retry state, so a stale
	// worker's retry must not overwrite next_attempt_at/last_error.
	res, err := h.ExecContext(ctx, `
UPDATE qso_upload
SET    status          = ?,
       attempts        = attempts + 1,
       next_attempt_at = ?,
       last_error      = ?
WHERE  id = ? AND status = ?`,
		status.Pending.String(), nextAttemptAt, lastErrArg, id, status.InProgress.String())
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("mark upload transient retry")
	}
	n, err := checkedRowsAffected(op, res, "mark upload transient retry")
	if err != nil {
		return err
	}
	if n == 0 {
		return s.classifyZeroRowCompletion(ctx, h, id, "transient-retry")
	}
	return nil
}

// MarkUploadFailedWithContext records a terminal outcome: status →
// 'failed', attempts bumped, last_error stored. Used for both an
// OutcomeTerminal from the forwarder and an OutcomeTransient that
// has exhausted its retry budget.
func (s *Service) MarkUploadFailedWithContext(ctx context.Context, id int64, lastError string) error {
	const op errors.Op = "sqlite.Service.MarkUploadFailedWithContext"
	if err := checkService(op, s); err != nil {
		return err
	}
	if id < 1 {
		return errors.New(op).WithMsg(errMsgInvalidId)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	var lastErrArg any
	if lastError != "" {
		lastErrArg = lastError
	}
	// Conditional on status='in_progress' — see MarkUploadSuccessWithContext.
	// A re-armed row must not be marked 'failed' for a stale send: it stays
	// pending so the operator's latest state is forwarded.
	res, err := h.ExecContext(ctx, `
UPDATE qso_upload
SET    status     = ?,
       attempts   = attempts + 1,
       last_error = ?
WHERE  id = ? AND status = ?`,
		status.Failed.String(), lastErrArg, id, status.InProgress.String())
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("mark upload failed")
	}
	n, err := checkedRowsAffected(op, res, "mark upload failed")
	if err != nil {
		return err
	}
	if n == 0 {
		return s.classifyZeroRowCompletion(ctx, h, id, "failed")
	}
	return nil
}

// checkedRowsAffected keeps result-contract failures distinct from a genuine
// zero-row update. Treating a failed count as zero sends completion methods
// through the concurrent-rearm classifier and makes session stamping look like
// a successful no-op even though the driver could not report the outcome.
func checkedRowsAffected(op errors.Op, res sql.Result, action string) (int64, error) {
	n, err := res.RowsAffected()
	if err != nil {
		return 0, errors.New(op).WithErr(err).WithMsg("rows affected by " + action)
	}
	return n, nil
}

// classifyZeroRowCompletion decides what a completion that affected zero rows
// means: the conditional `WHERE id=? AND status='in_progress'` matched nothing
// either because the row genuinely doesn't exist (a bug — wrong id → ErrNotFound,
// preserving the prior contract) or because it exists but is no longer
// in_progress — a concurrent operator edit re-armed it to 'pending' (the
// stale-completion race). The latter is an intentional no-op: the re-armed row
// stays pending for the next claim, so the latest QSO state is forwarded.
func (s *Service) classifyZeroRowCompletion(ctx context.Context, exec boil.ContextExecutor, id int64, kind string) error {
	const op errors.Op = "sqlite.Service.classifyZeroRowCompletion"
	exists, err := models.QsoUploadExists(ctx, exec, id)
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	if !exists {
		return errors.ErrNotFound
	}
	if s.LoggerService != nil {
		s.LoggerService.DebugWith().
			Int64("upload_id", id).
			Str("completion", kind).
			Msg("upload completion skipped: row re-armed by a concurrent edit (no longer in_progress)")
	}
	// Signal the no-op explicitly (was: nil). The worker must be able to tell a
	// re-armed no-op from a committed completion so it doesn't publish a
	// terminal forward.succeeded/failed event or fire the stamp mirror hook for
	// a transition that never happened (review 2026-07-20 internal/forwarding
	// #4). errors.Is-matchable via ErrUploadReArmed.
	return errors.ErrUploadReArmed
}

// ResetOrphanedUploadsWithContext transitions any 'in_progress' rows
// back to 'pending'. Run at daemon startup to recover from a crash
// that left rows claimed by a worker that never wrote their outcome.
// Returns the number of rows reset for logging.
//
// Safe because duplicates on the upstream are tolerable (most
// forwarders are idempotent on their own dedupe rules — see
// docs/v2-design/forwarding.md §7). A claimed-but-never-persisted row
// is treated as if the claim never happened.
func (s *Service) ResetOrphanedUploadsWithContext(ctx context.Context) (int64, error) {
	const op errors.Op = "sqlite.Service.ResetOrphanedUploadsWithContext"
	if err := checkService(op, s); err != nil {
		return 0, err
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return 0, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	n, err := models.QsoUploads(
		models.QsoUploadWhere.Status.EQ(status.InProgress.String()),
	).UpdateAll(ctx, h, models.M{
		models.QsoUploadColumns.Status: status.Pending.String(),
	})
	if err != nil {
		return 0, errors.New(op).WithErr(err).WithMsg("reset orphaned uploads")
	}
	return n, nil
}

// DiscardQueuedUploadsForForwarderWithContext deletes the not-yet-uploaded rows
// (pending / in_progress / failed) for a single forwarder, leaving 'uploaded'
// rows untouched. Run at daemon startup for each DISABLED forwarder (ADR 0039):
// `enabled` gates enqueue, so a disabled forwarder must not retain a queue — but
// its 'uploaded' rows are kept because they carry upstream_id (the remote LOGID
// needed to forward a later edit as an update if the forwarder is re-enabled).
// The affected QSOs revert to "not uploaded to X" — the ADIF upload stamp, not
// the queue row, is the source of truth — and are recoverable via the logbook
// SPA's manual upload. Returns the number of rows discarded, for logging.
func (s *Service) DiscardQueuedUploadsForForwarderWithContext(ctx context.Context, forwarderName string) (int64, error) {
	const op errors.Op = "sqlite.Service.DiscardQueuedUploadsForForwarderWithContext"
	if err := checkService(op, s); err != nil {
		return 0, err
	}
	if strings.TrimSpace(forwarderName) == "" {
		return 0, errors.New(op).WithMsg("forwarderName is empty")
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return 0, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	n, err := models.QsoUploads(
		models.QsoUploadWhere.ForwarderName.EQ(forwarderName),
		models.QsoUploadWhere.Status.IN([]string{
			status.Pending.String(),
			status.InProgress.String(),
			status.Failed.String(),
		}),
	).DeleteAll(ctx, h)
	if err != nil {
		return 0, errors.New(op).WithErr(err).WithMsg("discard queued uploads for forwarder")
	}
	return n, nil
}

// DiscardClearableUploadsForForwarderWithContext deletes a single forwarder's
// operator-clearable backlog — its `pending` and `failed` rows only — and leaves
// `in_progress` and `uploaded` rows untouched. It is the operator-triggered
// "drop the remaining backlog, finish the currently claimed batch" clear
// (W-0005), distinct from DiscardQueuedUploadsForForwarderWithContext (the
// startup disabled-forwarder discard, which also removes `in_progress`).
//
// Excluding `in_progress` makes the clear race-free against a live worker with no
// coordination: ClaimPendingUploads moves a whole batch pending→in_progress in one
// atomic UPDATE, so `in_progress` is exactly the batch the worker is processing;
// leaving it alone lets those rows complete normally while the not-yet-claimed
// backlog is dropped. `uploaded` rows are preserved because they carry the remote
// upstream_id. Returns the number of rows discarded.
func (s *Service) DiscardClearableUploadsForForwarderWithContext(ctx context.Context, forwarderName string) (int64, error) {
	const op errors.Op = "sqlite.Service.DiscardClearableUploadsForForwarderWithContext"
	if err := checkService(op, s); err != nil {
		return 0, err
	}
	if strings.TrimSpace(forwarderName) == "" {
		return 0, errors.New(op).WithMsg("forwarderName is empty")
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return 0, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	n, err := models.QsoUploads(
		models.QsoUploadWhere.ForwarderName.EQ(forwarderName),
		models.QsoUploadWhere.Status.IN([]string{
			status.Pending.String(),
			status.Failed.String(),
		}),
	).DeleteAll(ctx, h)
	if err != nil {
		return 0, errors.New(op).WithErr(err).WithMsg("discard clearable uploads for forwarder")
	}
	return n, nil
}

// FetchUploadsByQsoIDWithContext returns every qso_upload row for the
// given QSO, ordered by (forwarder_name, action), so the output is
// stable across calls. Drives the GET /v1/qso/:id/uploads endpoint.
func (s *Service) FetchUploadsByQsoIDWithContext(ctx context.Context, qsoID int64) ([]types.QsoUpload, error) {
	const op errors.Op = "sqlite.Service.FetchUploadsByQsoIDWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}
	if qsoID < 1 {
		return nil, errors.New(op).WithMsg(errMsgInvalidId)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	rows, err := models.QsoUploads(
		models.QsoUploadWhere.QsoID.EQ(qsoID),
		qm.OrderBy("forwarder_name, action"),
	).All(ctx, h)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("fetch uploads by qso id")
	}

	out := make([]types.QsoUpload, 0, len(rows))
	for _, r := range rows {
		u, er := adapters.QsoUploadModelToType(r)
		if er != nil {
			return nil, errors.New(op).WithErr(er)
		}
		out = append(out, u)
	}
	return out, nil
}

// UploadQueueDepth is a point-in-time snapshot of one forwarder's queue, for the periodic
// queue summary (L11). Pending is the count of rows awaiting upload (status=pending);
// OldestQueued is when the oldest pending row was enqueued (the zero value when Pending==0);
// Failed is the DURABLE count of rows that gave up (status=failed) — a DB count, not a
// process-lifetime counter, so it survives a restart.
type UploadQueueDepth struct {
	Pending      int64
	OldestQueued time.Time
	Failed       int64
}

// UploadQueueDepthWithContext returns the queue snapshot for one forwarder: the pending
// count, the failed count, and the oldest pending row's enqueue time. Scoped by
// forwarder_name, matching the claim query.
//
// ONE aggregate query, deliberately: all three values must describe the same instant, or a
// consumer could see e.g. Pending>0 with a zero OldestQueued (and compute a nonsensical age
// from it) when the queue changes between separate reads. A single statement observes one
// SQLite snapshot, so the three fields are always mutually consistent. `oldest` is NULL when
// nothing is pending (empty queue) → the zero-value OldestQueued, and Pending is then 0.
func (s *Service) UploadQueueDepthWithContext(ctx context.Context, forwarderName string) (UploadQueueDepth, error) {
	const op errors.Op = "sqlite.Service.UploadQueueDepthWithContext"
	if err := checkService(op, s); err != nil {
		return UploadQueueDepth{}, err
	}
	if forwarderName == "" {
		return UploadQueueDepth{}, errors.New(op).WithMsg("forwarderName is empty")
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return UploadQueueDepth{}, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	var row struct {
		Pending     int64      `boil:"pending"`
		Failed      int64      `boil:"failed"`
		OldestEpoch null.Int64 `boil:"oldest_epoch"`
	}
	// strftime('%s', created_at) converts the stored datetime to a Unix epoch in SQL, so we
	// bind a plain integer and never depend on the driver parsing a datetime STRING (raw Bind
	// into null.Time cannot). MIN(CASE ...) is NULL when no pending row exists.
	//
	// PERFORMANCE, accepted: no existing index services `status IN ('pending','failed')` (the
	// partial indexes cover pending/in_progress and uploaded), so SQLite scans qso_upload —
	// and uploaded rows are retained indefinitely, so the scan grows with total upload history,
	// not just the active set. The `status IN (...)` predicate only trims the RESULT, not the
	// scan. This is accepted rather than fixed: it is a 60-second best-effort diagnostic whose
	// scan is sub-millisecond at dogfood scale and stays well under the service timeout until
	// the table reaches millions of rows. The correct optimisation is a partial index on
	// (forwarder_name) WHERE status IN ('pending','failed'); it is DEFERRED because it is a
	// schema migration that bumps the schema head the migration characterization tests pin to,
	// and that churn is not justified for a diagnostic query until profiling shows it matters.
	err = queries.Raw(`
SELECT
    COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0)                  AS pending,
    COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0)                  AS failed,
    MIN(CASE WHEN status = ? THEN CAST(strftime('%s', created_at) AS INTEGER) END) AS oldest_epoch
FROM qso_upload
WHERE forwarder_name = ? AND status IN (?, ?)`,
		status.Pending.String(), status.Failed.String(), status.Pending.String(),
		forwarderName, status.Pending.String(), status.Failed.String(),
	).Bind(ctx, h, &row)
	if err != nil {
		return UploadQueueDepth{}, errors.New(op).WithErr(err).WithMsg("query upload queue depth")
	}

	out := UploadQueueDepth{Pending: row.Pending, Failed: row.Failed}
	if row.OldestEpoch.Valid {
		out.OldestQueued = time.Unix(row.OldestEpoch.Int64, 0).UTC()
	}
	return out, nil
}

// ForwarderQueueCounts is one forwarder's upload-queue breakdown for the
// operator-facing Settings → Forwarding surface (W-0005): Clearable is the
// pending+failed backlog an operator-triggered clear would remove; InFlight is
// the in_progress batch a live worker is processing and never clears. Uploaded
// rows are history and counted in neither.
type ForwarderQueueCounts struct {
	Clearable int64
	InFlight  int64
}

// ForwarderQueueCountsWithContext returns per-forwarder queue counts, keyed by
// forwarder_name, for every forwarder that has any qso_upload rows. A forwarder
// with only uploaded rows reads {0,0}; one with no rows at all is absent (its
// zero value is also {0,0}). The caller pairs this with the configured forwarder
// list, defaulting the rest to zero. One GROUP BY scan drives the whole surface.
func (s *Service) ForwarderQueueCountsWithContext(ctx context.Context) (map[string]ForwarderQueueCounts, error) {
	const op errors.Op = "sqlite.Service.ForwarderQueueCountsWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	var rows []struct {
		ForwarderName string `boil:"forwarder_name"`
		Clearable     int64  `boil:"clearable"`
		InFlight      int64  `boil:"in_flight"`
	}
	err = queries.Raw(`
SELECT
    forwarder_name,
    COALESCE(SUM(CASE WHEN status IN (?, ?) THEN 1 ELSE 0 END), 0) AS clearable,
    COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0)       AS in_flight
FROM qso_upload
GROUP BY forwarder_name`,
		status.Pending.String(), status.Failed.String(), status.InProgress.String(),
	).Bind(ctx, h, &rows)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("query forwarder queue counts")
	}

	out := make(map[string]ForwarderQueueCounts, len(rows))
	for _, r := range rows {
		out[r.ForwarderName] = ForwarderQueueCounts{Clearable: r.Clearable, InFlight: r.InFlight}
	}
	return out, nil
}

// FetchQsoHistoryByUUIDWithContext returns every qso_history row for
// the given QSO, ordered by `at ASC` so callers see mutations in the
// order they happened. The QSO's UUID is the lookup key (not the
// internal int PK) — qso_history is keyed on UUID per ADR 0016 so
// audit rows survive any future re-numbering.
func (s *Service) FetchQsoHistoryByUUIDWithContext(ctx context.Context, qsoUUID string) ([]types.QsoHistory, error) {
	const op errors.Op = "sqlite.Service.FetchQsoHistoryByUUIDWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}
	if qsoUUID == "" {
		return nil, errors.New(op).WithMsg("qsoUUID is empty")
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	rows, err := models.QsoHistories(
		models.QsoHistoryWhere.QsoUUID.EQ(qsoUUID),
		qm.OrderBy("at ASC, id ASC"),
	).All(ctx, h)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("fetch qso_history by uuid")
	}

	out := make([]types.QsoHistory, 0, len(rows))
	for _, r := range rows {
		h, er := adapters.QsoHistoryModelToType(r)
		if er != nil {
			return nil, errors.New(op).WithErr(er)
		}
		out = append(out, h)
	}
	return out, nil
}

// FetchPriorUpstreamIDWithContext returns the upstream_id recorded on the most
// recent successful UPSTREAM-CREATING action (insert OR update) for the given
// (qso_id, forwarder_name) pair. The QRZ delete forwarder needs this value to
// populate LOGIDS on the DELETE call — upstream's delete endpoint identifies
// records by its own id, not by our QSO id.
//
// Both insert (ACTION=INSERT) and update (ACTION=INSERT&OPTION=REPLACE) can be
// the action that created/owns the upstream record and returned its id. If an
// update is forwarded before its insert (the out-of-order backlog case, review
// 2026-06-05 M2), the LOGID lands on the UPDATE row — so a delete that only
// consulted insert rows could not find the record it must remove. Considering
// both upstream-creating actions closes that gap. (Delete rows never carry a
// created upstream_id, so including them would be harmless but pointless; they
// are excluded by the action filter for clarity.)
//
// UNIQUE(qso_id, forwarder_name, action) means at most one insert and one
// update row exist per pair; we want the one whose SUCCESS is most recent.
// Ordering is by `modified_at DESC, id DESC`, NOT created_at: created_at is set
// once at insert and never mutated, but InsertQsoUploadTx re-arms an existing
// row on conflict without touching its created_at, and the success transition
// bumps modified_at. So a re-armed update can be the latest successful upstream
// write while carrying an older created_at than the insert row — ordering by
// created_at would then hand a stale LOGID to the delete. modified_at tracks the
// last state transition; id DESC breaks ties (review 2026-06-19 M1).
//
// Returns:
//   - ("", nil) when no matching row exists. The worker reclassifies
//     this as a terminal failure because a delete without any prior
//     successful upstream-creating action is structurally unresolvable —
//     retrying cannot conjure an upstream id.
//   - (upstreamID, nil) on the happy path.
//   - ("", err) only for infrastructure failures (ctx cancel, DB error).
func (s *Service) FetchPriorUpstreamIDWithContext(
	ctx context.Context, qsoID int64, forwarderName string,
) (string, error) {
	const op errors.Op = "sqlite.Service.FetchPriorUpstreamIDWithContext"
	if err := checkService(op, s); err != nil {
		return "", err
	}
	if qsoID < 1 {
		return "", errors.New(op).WithMsg(errMsgInvalidId)
	}
	if forwarderName == "" {
		return "", errors.New(op).WithMsg("forwarderName is empty")
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return "", err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	row, err := models.QsoUploads(
		models.QsoUploadWhere.QsoID.EQ(qsoID),
		models.QsoUploadWhere.ForwarderName.EQ(forwarderName),
		models.QsoUploadWhere.Action.IN([]string{action.Insert.String(), action.Update.String()}),
		models.QsoUploadWhere.Status.EQ(status.Uploaded.String()),
		models.QsoUploadWhere.UpstreamID.IsNotNull(),
		qm.OrderBy("modified_at DESC, id DESC"),
		qm.Limit(1),
	).One(ctx, h)
	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", errors.New(op).WithErr(err).WithMsg("fetch prior upstream id")
	}

	if !row.UpstreamID.Valid || row.UpstreamID.String == "" {
		// Defensive: the IsNotNull filter plus the mark-success path
		// should make this unreachable, but if an older row somehow
		// has status=uploaded and no upstream_id, treat it as "no
		// match" rather than returning a useless empty string as if
		// it were real.
		return "", nil
	}
	return row.UpstreamID.String, nil
}

/**********************************************************************************************************************
 * Transactional variants — used by callers that compose multiple writes in a single transaction.
 * These methods run against the caller-supplied *sql.Tx and do not open their own handle or timeout.
 * The caller is responsible for Begin/Commit/Rollback.
 **********************************************************************************************************************/

func (s *Service) InsertQsoTx(ctx context.Context, tx *sql.Tx, qso types.Qso) (int64, error) {
	const op errors.Op = "sqlite.Service.InsertQsoTx"
	if err := checkService(op, s); err != nil {
		return 0, err
	}
	if tx == nil {
		return 0, errors.New(op).WithMsg("tx is nil")
	}

	model, err := adapters.QsoTypeToModel(qso)
	if err != nil {
		return 0, errors.New(op).WithErr(err)
	}

	// ORDER IS LOAD-BEARING: insert FIRST, then verify the parent. Checking
	// first made the guard read the transaction's opening statement, which
	// starts it as a READER holding a WAL snapshot — and SQLite then refuses
	// the later write upgrade with SQLITE_BUSY_SNAPSHOT (error 517, "database
	// is locked") if ANY other connection committed in between. Not just a
	// competing logbook delete: an unrelated concurrent submit was enough to
	// turn this transaction into a 500 (2026-07-23 review of 0517354b, verified
	// by probe). Writing first takes the write lock immediately, so a competing
	// delete blocks behind us instead, and the subsequent read is a writer's own
	// read — no upgrade, no snapshot to invalidate.
	//
	// The guarantee survives the reorder: a delete that committed BEFORE our
	// insert is caught by this check (we roll back, nothing is committed); a
	// delete that arrives AFTER it waits on our write lock and then finds our
	// live QSO, so its NOT EXISTS refuses. A logbook that never existed is
	// rejected by the foreign key on the insert above, as it always was.
	if err = model.Insert(ctx, tx, boil.Infer()); err != nil {
		// `qso` carries exactly ONE foreign key (fk_qso_logbook_id), so an FK
		// violation here can only mean the parent logbook row does not exist.
		// Map it to the same ErrNotFound the guard below returns for a
		// SOFT-deleted parent: writing first must not cost callers the sentinel
		// they classify on (qsoservice.submit → "logbook_not_found"). Before the
		// reorder the guard ran first and caught both shapes; now the FK catches
		// the missing-row shape and the guard catches the tombstone shape, and
		// the two must stay indistinguishable to the caller.
		if isForeignKeyConstraintError(err) {
			return 0, errors.New(op).WithErr(errors.ErrNotFound).
				WithMsgf("logbook %d does not exist", model.LogbookID)
		}
		return 0, errors.New(op).WithErr(err)
	}

	if err = requireLiveLogbook(ctx, tx, op, model.LogbookID); err != nil {
		return 0, err
	}

	return model.ID, nil
}

// requireLiveLogbook rejects a QSO write whose parent logbook has been
// soft-deleted (2026-07-22 sqlite review, finding 2). The FK constraint cannot
// do this: ON DELETE RESTRICT fires on hard deletes only, and a row with
// deleted_at set still physically satisfies the reference — so without this
// check a QSO submitted concurrently with a logbook deletion commits as a live
// row under a deleted parent, invisible to every logbook-scoped query.
//
// It is the insert-side half of the pair whose delete-side half is the
// conditional UPDATE in DeleteLogbookByIDWithContext. Inside a transaction it
// MUST run AFTER the write it guards — see the ordering note in InsertQsoTx;
// calling it as a transaction's first statement reintroduces the
// SQLITE_BUSY_SNAPSHOT failure. In autocommit (InsertQsoWithContext) there is
// no snapshot to strand, so the check goes first there.
//
// ErrNotFound is the deliberate sentinel: a soft-deleted logbook is already
// invisible to LogbookCallsignByIDWithContext and FindLogbook, so callers that
// map ErrNotFound to "logbook does not exist" stay correct whether the delete
// landed before their preflight or after it.
func requireLiveLogbook(ctx context.Context, exec boil.ContextExecutor, op errors.Op, logbookID int64) error {
	var live bool
	if err := exec.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM logbook WHERE id = ? AND deleted_at IS NULL)`,
		logbookID).Scan(&live); err != nil {
		return errors.New(op).WithErr(err).WithMsg("checking parent logbook is live")
	}
	if !live {
		return errors.New(op).WithErr(errors.ErrNotFound).WithMsgf("logbook %d is deleted or does not exist", logbookID)
	}
	return nil
}

func (s *Service) UpdateQsoTx(ctx context.Context, tx *sql.Tx, qso types.Qso) error {
	const op errors.Op = "sqlite.Service.UpdateQsoTx"
	if err := checkService(op, s); err != nil {
		return err
	}
	if tx == nil {
		return errors.New(op).WithMsg("tx is nil")
	}
	if qso.ID < 1 {
		return errors.New(op).WithMsgf("QSO ID is invalid: %d", qso.ID)
	}

	model, err := adapters.QsoTypeToModel(qso)
	if err != nil {
		return errors.New(op).WithErr(err)
	}

	model.ModifiedAt = null.TimeFrom(time.Now().UTC())

	// The edit path is revision-guarded: qso.Revision is the caller's fetched
	// snapshot (json:"-", so it can only come from a fetch), and the write
	// refuses if the row has moved past it (review 2026-08-07 #2).
	return updateActiveQsoAtRevision(ctx, tx, model, qso.Revision)
}

// DeleteQsoByIDTx soft-deletes the QSO id ONLY while it still holds expectedRev —
// the same optimistic-concurrency guard (revision CAS, ADR 0050) the edit path
// uses — and returns the authoritative pre-delete image read inside this
// transaction, so the append-only audit history records the true last-live state
// rather than a caller snapshot a concurrent edit may have superseded (PT-2).
// Only deleted_at is set; the 0005 trigger bumps the revision.
//
// A still-live row at a DIFFERENT revision is ErrStaleRevision — the handler's
// 409 delete_conflict, parallel to the edit path's edit_conflict. A missing or
// already-tombstoned row is ErrNotFound (404). The returned logbook_id lets the
// caller emit an accurately-scoped qso.deleted event without a second round-trip.
//
// Used by qsoservice.Delete to bundle the soft-delete with qso_upload(delete)
// inserts and the qso_history row under the same one-fails-all-fail tx.
func (s *Service) DeleteQsoByIDTx(ctx context.Context, tx *sql.Tx, id, expectedRev int64) (types.Qso, int64, error) {
	const op errors.Op = "sqlite.Service.DeleteQsoByIDTx"
	if err := checkService(op, s); err != nil {
		return types.Qso{}, 0, err
	}
	if tx == nil {
		return types.Qso{}, 0, errors.New(op).WithMsg("tx is nil")
	}
	if id < 1 {
		return types.Qso{}, 0, errors.New(op).WithMsg(errMsgInvalidId)
	}

	// Write FIRST (mirrors updateActiveQsoAtRevision and the InsertQsoTx ordering
	// note): the revision-guarded soft-delete takes the write lock immediately, so a
	// read-then-write can't strand this transaction with SQLITE_BUSY_SNAPSHOT.
	deletedAt := null.TimeFrom(time.Now().In(boil.GetLocation()))
	n, err := models.Qsos(
		models.QsoWhere.ID.EQ(id),
		models.QsoWhere.DeletedAt.IsNull(),
		models.QsoWhere.Revision.EQ(expectedRev),
	).UpdateAll(ctx, tx, models.M{models.QsoColumns.DeletedAt: deletedAt})
	if err != nil {
		return types.Qso{}, 0, errors.New(op).WithErr(err).WithMsg("revision-guarded soft-delete")
	}
	if n == 0 {
		// Same-executor probe: a still-live row means the revision moved (a
		// conflict); an absent one means the row is gone / already tombstoned.
		exists, eerr := models.Qsos(
			models.QsoWhere.ID.EQ(id),
			models.QsoWhere.DeletedAt.IsNull(),
		).Exists(ctx, tx)
		if eerr != nil {
			return types.Qso{}, 0, errors.New(op).WithErr(eerr).WithMsg("disambiguating zero-row revision-guarded delete")
		}
		if exists {
			return types.Qso{}, 0, errors.New(op).WithErr(errors.ErrStaleRevision).
				WithMsgf("QSO %d changed since revision %d was fetched", id, expectedRev)
		}
		return types.Qso{}, 0, errors.New(op).WithErr(errors.ErrNotFound).WithMsgf("no active QSO with id %d", id)
	}

	// The authoritative pre-delete image: the soft-delete changed only deleted_at
	// (and, via the trigger, revision), so the row we just tombstoned still carries
	// the last live CONTENT. Read it here, inside the tx — WithDeleted, since it is
	// now soft-deleted — and present it as the live state it was immediately before
	// deletion (deleted_at cleared, revision at the value the guard matched) for the
	// history before_image.
	model, err := models.Qsos(qm.WithDeleted(), models.QsoWhere.ID.EQ(id)).One(ctx, tx)
	if err != nil {
		return types.Qso{}, 0, errors.New(op).WithErr(err).WithMsg("reading pre-delete image")
	}
	preimage, err := adapters.QsoModelToType(model)
	if err != nil {
		return types.Qso{}, 0, errors.New(op).WithErr(err).WithMsg("converting pre-delete image")
	}
	preimage.DeletedAt = time.Time{}
	preimage.Revision = expectedRev
	return preimage, model.LogbookID, nil
}

// InsertQsoUploadTx enqueues one qso_upload row within the caller-supplied tx.
// forwarderName is the per-instance config handle (e.g. "qrz-primary");
// forwarderType is the plugin identifier (e.g. "qrz"). Both are stored on the
// row, so historical queue entries remain interpretable even after an operator
// renames or removes the destination from config. See docs/v2-design/forwarding.md §6.
//
// Re-arm semantics on conflict: the qso_upload UNIQUE (qso_id, forwarder_name, action)
// constraint says "at most one row per (QSO, destination, action-kind) ever." A second
// PATCH or DELETE on the same QSO triggers another InsertQsoUploadTx for the same triple;
// rather than fail with a constraint violation, this method UPSERTs — re-arming any
// existing row to status='pending', clearing retry state, and resetting next_attempt_at
// so the worker re-forwards the latest state. This is correct: the second operator action
// represents a new state that needs forwarding, regardless of whether the prior action's
// row was uploaded, failed, or still pending. Raw SQL is used here because sqlboiler's
// Upsert helper can't express the "overwrite specific columns, leave id alone" update set
// with a conflict target cleanly.
func (s *Service) InsertQsoUploadTx(ctx context.Context, tx *sql.Tx, qsoId int64, action action.Action, forwarderName, forwarderType string, org origin.Origin) error {
	const op errors.Op = "sqlite.Service.InsertQsoUploadTx"
	if err := checkService(op, s); err != nil {
		return err
	}
	if tx == nil {
		return errors.New(op).WithMsg("tx is nil")
	}
	if qsoId < 1 {
		return errors.New(op).WithMsgf("QSO ID is invalid: %d", qsoId)
	}
	if forwarderName == "" {
		return errors.New(op).WithMsg("forwarderName is empty")
	}
	if forwarderType == "" {
		return errors.New(op).WithMsg("forwarderType is empty")
	}
	// Validated Go-side as well as by the column CHECK: this fails at the call
	// site, naming both the offending value and this operation, before issuing any
	// qso_upload SQL. Not "before the transaction does any work" — the caller has
	// usually already written the QSO row in the same tx.
	if _, err := origin.Parse(org.String()); err != nil {
		return errors.New(op).WithErr(err)
	}

	// Re-arm preserves upstream_id deliberately: FetchPriorUpstreamIDWithContext
	// reads it back for the QRZ delete-after-insert flow, and the worker's
	// own success path overwrites it on the next successful attempt.
	// Clearing it here would lose history a re-armed insert (the rare
	// force=true edge case) might want to keep.
	// origin is REPLACED on re-arm while upstream_id is PRESERVED — deliberately
	// opposite treatments in one statement, so do not "tidy" them into agreement.
	// upstream_id is history the QRZ delete-after-insert flow reads back; origin
	// answers "why does this queue entry exist NOW", and after a re-enqueue the
	// honest answer is whatever just re-armed it, not what first created it.
	//
	// created_at is RESET on re-arm, like next_attempt_at/attempts: it means "when
	// this row entered its CURRENT pending state", not "first ever created". A row
	// that uploaded months ago and is re-enqueued by an edit is new work, not a
	// months-old backlog — the queue-depth oldest-age signal (UploadQueueDepth)
	// reads MIN(created_at) and would otherwise report the stale original age.
	const q = `
		INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status, origin)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (qso_id, forwarder_name, action) DO UPDATE SET
			forwarder_type  = excluded.forwarder_type,
			origin          = excluded.origin,
			status          = 'pending',
			attempts        = 0,
			created_at      = datetime('now'),
			next_attempt_at = strftime('%s', 'now'),
			last_attempt_at = NULL,
			last_error      = NULL`

	if _, err := tx.ExecContext(ctx, q,
		qsoId, forwarderName, forwarderType, action.String(), status.Pending.String(), org.String(),
	); err != nil {
		return errors.New(op).WithErr(err).WithMsg("upserting qso_upload row failed")
	}

	return nil
}

// InsertQsoHistoryTx appends a row to qso_history within the
// caller-supplied tx. The row records that a QSO identified by
// qsoUUID was about to be mutated (mutationOp is action.Update or
// action.Delete) by src, with beforeImage as the json.Marshal of the
// row's pre-mutation state.
//
// ADR 0016 prep #2 (audit trail for SM Cloud sync). The audit row
// must share the same transaction as the QSO mutation it records — a
// committed mutation with no audit row, or an audit row with no
// mutation, both violate the one-fails-all-fail invariant
// (docs/v1-analysis/invariants.md). Callers (qsoservice.Update,
// qsoservice.Delete) are responsible for placing this call inside
// the same tx as the QSO write.
//
// The `at` column is left to its SQL DEFAULT (datetime('now',
// 'localtime')) so the snapshot timestamp comes from the database,
// not from drift-prone Go wall-clock.
func (s *Service) InsertQsoHistoryTx(
	ctx context.Context,
	tx *sql.Tx,
	qsoUUID string,
	mutationOp action.Action,
	src source.Source,
	beforeImage []byte,
) error {
	const op errors.Op = "sqlite.Service.InsertQsoHistoryTx"
	if err := checkService(op, s); err != nil {
		return err
	}
	if tx == nil {
		return errors.New(op).WithMsg("tx is nil")
	}
	if qsoUUID == "" {
		return errors.New(op).WithMsg("qsoUUID is empty")
	}
	// qso_history.op CHECK accepts only 'update' / 'delete'; reject
	// action.Insert here so the constraint violation surfaces as a
	// clear Go-side error rather than a generic SQLite CHECK failure.
	if mutationOp != action.Update && mutationOp != action.Delete {
		return errors.New(op).WithMsgf(
			"invalid op for qso_history: %q (must be %q or %q)",
			mutationOp, action.Update, action.Delete,
		)
	}
	if src == "" {
		return errors.New(op).WithMsg("source is empty")
	}
	if len(beforeImage) == 0 {
		return errors.New(op).WithMsg("beforeImage is empty")
	}

	row := &models.QsoHistory{
		QsoUUID:     qsoUUID,
		Op:          mutationOp.String(),
		Source:      src.String(),
		BeforeImage: boiltypes.JSON(beforeImage),
	}

	if err := row.Insert(ctx, tx, boil.Infer()); err != nil {
		return errors.New(op).WithErr(err).WithMsg("failed to insert qso_history row")
	}

	return nil
}
