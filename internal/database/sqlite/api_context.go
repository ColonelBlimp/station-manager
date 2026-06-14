package sqlite

import (
	"context"
	"database/sql"
	stderr "errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite/adapters"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite/models"
	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
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

func (s *Service) FetchQsoCountByLogbookIdWithContext(ctx context.Context, id int64) (int64, error) {
	const op errors.Op = "sqlite.Service.FetchQsoCountByLogbookIdWithContext"
	if err := checkService(op, s); err != nil {
		return 0, err
	}

	if id < 1 {
		return 0, errors.New(op).WithMsg(errMsgInvalidId)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return 0, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	count, err := models.Qsos(models.QsoWhere.LogbookID.EQ(id)).Count(ctx, h)
	if err != nil {
		return 0, errors.New(op).WithErr(err).WithMsg("Failed to fetch QSO count by logbook ID.")
	}

	return count, nil
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

// SchemaVersionWithContext returns the current migration version recorded
// in the schema_migrations table (maintained by golang-migrate). Returns
// 0 if no migrations have been applied yet (fresh DB). The `dirty` flag
// is true if the last migration attempt failed midway.
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

	// LIMIT 1 is defensive. golang-migrate guarantees exactly one row in
	// schema_migrations (the current state), so the limit never actually
	// clips results — it just pins the query shape in case of corruption.
	row := h.QueryRowContext(ctx, `SELECT version, dirty FROM schema_migrations LIMIT 1`)
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
	cols := models.M{
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
	n, err := models.Qsos(
		models.QsoWhere.ID.EQ(model.ID),
		models.QsoWhere.DeletedAt.IsNull(),
	).UpdateAll(ctx, exec, cols)
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	if n == 0 {
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

	model.ModifiedAt = null.TimeFrom(time.Now())

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
// soft-deleted rows return ErrNotFound; format validation is a quick
// reject on obviously-malformed input so the DB doesn't see garbage.
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

	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	mods := []qm.QueryMod{
		models.QsoWhere.LogbookID.EQ(logbookID),
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
	mods = append(mods, qm.OrderBy(models.QsoColumns.ID+" "+ordering.String()))
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

	model.ModifiedAt = null.TimeFrom(time.Now())

	_, err = model.Update(ctx, h, boil.Infer())
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("Updating contacted station failed.")
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
func validateCountryPrefix(op errors.Op, country *types.Country) error {
	prefix := strings.TrimSpace(country.Prefix)
	if prefix == "" {
		return errors.New(op).WithMsg("country.Prefix cannot be empty")
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

	if _, err = model.Update(ctx, h, boil.Infer()); err != nil {
		return errors.New(op).WithErr(err).WithMsg("Updating country failed.")
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
// last_refreshed_at to time.Now() before write so the orchestrator's
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

	country.LastRefreshedAt = time.Now()

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
	model.ModifiedAt = null.TimeFrom(time.Now())

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
// last_refreshed_at is set to time.Now() on every write — both insert
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

	existing, ferr := s.FetchContactedStationByCallsignWithContext(ctx, call)
	switch {
	case ferr == nil:
		// Existing row. merge → non-empty-wins field merge; replace → the
		// incoming station overwrites all columns (empty fields clear).
		// Preserve PK from the existing row either way.
		row := station
		if merge {
			row = mergeContactedStation(existing, station)
		}
		row.CSID = existing.CSID
		row.LastRefreshedAt = time.Now()

		model, mErr := adapters.ContactedStationTypeToModel(row)
		if mErr != nil {
			return errors.New(op).WithErr(mErr)
		}
		model.ModifiedAt = null.TimeFrom(time.Now())
		if _, uErr := model.Update(ctx, h, boil.Infer()); uErr != nil {
			return errors.New(op).WithErr(uErr).WithMsg("updating contacted_station failed")
		}
		return nil

	case stderr.Is(ferr, errors.ErrNotFound):
		// Cold insert — no existing row to merge or replace.
		station.LastRefreshedAt = time.Now()
		model, mErr := adapters.ContactedStationTypeToModel(station)
		if mErr != nil {
			return errors.New(op).WithErr(mErr)
		}
		if iErr := model.Insert(ctx, h, boil.Infer()); iErr != nil {
			return errors.New(op).WithErr(iErr).WithMsg("inserting contacted_station failed")
		}
		return nil

	default:
		return errors.New(op).WithErr(ferr)
	}
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

	logbook, err := models.FindLogbook(ctx, h, id)
	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return errors.ErrNotFound
		}
		return errors.New(op).WithErr(err)
	}

	// Check for QSOs referencing this logbook before soft-deleting.
	// The FK RESTRICT only fires on hard deletes; soft-delete (setting
	// deleted_at) would silently succeed, orphaning QSOs under a
	// deleted logbook.
	//
	// .Exists() is cheaper than .Count(): it short-circuits at the
	// first hit rather than summing the whole match set. For a
	// logbook with thousands of QSOs this is the difference between
	// "read one row via idx_qso_logbook_id" and "scan the whole
	// partition".
	hasQsos, err := models.Qsos(models.QsoWhere.LogbookID.EQ(id)).Exists(ctx, h)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("checking logbook QSO count")
	}
	if hasQsos {
		return errors.New(op).WithErr(errors.ErrLogbookHasQsos)
	}

	if _, err = logbook.Delete(ctx, h, false); err != nil {
		return errors.New(op).WithErr(err).WithMsg("failed to delete logbook")
	}

	return nil
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
		models.LogbookColumns.ModifiedAt:  null.TimeFrom(time.Now()),
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

func (s *Service) UpsertLogbookWithContext(ctx context.Context, logbook types.Logbook) error {
	const op errors.Op = "sqlite.Service.UpsertLogbookWithContext"
	if err := checkService(op, s); err != nil {
		return err
	}

	if logbook.ID < 1 {
		return errors.New(op).WithMsg("Logbook ID must be greater than 0")
	}
	//TODO: Other validation

	h, err := s.getOpenHandle(op)
	if err != nil {
		return err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	model := models.Logbook{
		ID:          logbook.ID,
		Name:        logbook.Name,
		Callsign:    logbook.Callsign,
		Description: null.StringFrom(logbook.Description),
	}

	// updateOnConflict=true means: on conflict (by primary key), update
	// the non-key columns. conflictColumns=nil uses the primary key.
	if err = model.Upsert(ctx, h, true, nil, boil.Infer(), boil.Infer()); err != nil {
		return errors.New(op).WithErr(err).WithMsg("upserting logbook failed")
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

	row, err := models.FindQsoUpload(ctx, h, id)
	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return errors.ErrNotFound
		}
		return errors.New(op).WithErr(err)
	}

	row.Status = status.Uploaded.String()
	row.Attempts++
	if upstreamID != "" {
		row.UpstreamID = null.StringFrom(upstreamID)
	} else {
		row.UpstreamID = null.String{}
	}
	row.LastError = null.String{}

	if _, err = row.Update(ctx, h, boil.Infer()); err != nil {
		return errors.New(op).WithErr(err).WithMsg("mark upload success")
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
) error {
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
	// Safe on commit — tx.Rollback returns an error after commit but we
	// ignore it; the rollback-only-on-failure idiom keeps the control
	// flow readable.
	defer func() { _ = tx.Rollback() }()

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
WHERE  id = ?`,
		status.Uploaded.String(), upstreamArg, id,
	)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("update qso_upload")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.ErrNotFound
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
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.ErrNotFound
	}

	if err := tx.Commit(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("commit")
	}
	return nil
}

// MarkSessionEmailedWithContext stamps the "forwarded by email" ADIF
// fields on a set of QSO rows after a successful SessionPanel email
// send. It is the manual-send analogue of the forwarder's
// MarkUploadSuccessWithAdifStampWithContext: the stamp lands inside
// each qso row's `additional_data` JSON blob via json_set, writing
//
//	$.sm_fwrd_by_email_status = "Y"          (adif.YesString)
//	$.sm_fwrd_by_email_date   = today (UTC, YYYYMMDD)
//
// These keys match the JSON tags on types.Qso (SmFwrdByEmailStatus /
// SmFwrdByEmailDate), so the next read via QsoModelToType surfaces the
// stamp as struct fields automatically — no schema column, no migration.
//
// Unlike the forwarder path there is no qso_upload queue row to update:
// the session email is a one-shot operator action, not a retried
// upload. The whole set is stamped in a single UPDATE (one atomic
// statement) so the operator never sees a partial mark. Soft-deleted
// rows are skipped. Stamping a QSO that is already marked emailed is
// harmless — json_set overwrites the date with the latest send, which
// is the desired "most recent send" semantics.
//
// Returns the number of rows actually stamped. A caller passing UUIDs
// that resolved to ids can compare the count against len(ids) to detect
// rows that vanished (soft-deleted) between fetch and stamp; the
// session-email handler logs that gap rather than failing the send,
// since the mail has already gone out (the "missed" case the Logbook
// SPA reconciles).
func (s *Service) MarkSessionEmailedWithContext(ctx context.Context, qsoIDs []int64) (int64, error) {
	const op errors.Op = "sqlite.Service.MarkSessionEmailedWithContext"
	if err := checkService(op, s); err != nil {
		return 0, err
	}
	if len(qsoIDs) == 0 {
		return 0, nil
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return 0, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	// Build the IN-list placeholders. The JSON paths and the two stamp
	// values are static literals / bound params — only the id list is
	// dynamic, and every element is an int64, so the statement carries
	// no string interpolation of caller data.
	placeholders := make([]string, len(qsoIDs))
	args := make([]any, 0, len(qsoIDs)+2)
	today := time.Now().UTC().Format("20060102")
	args = append(args, adif.YesString, today)
	for i, id := range qsoIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}

	query := `
UPDATE qso
SET    additional_data = json_set(
           additional_data,
           '$.sm_fwrd_by_email_status', ?,
           '$.sm_fwrd_by_email_date', ?
       )
WHERE  id IN (` + strings.Join(placeholders, ", ") + `)
  AND  deleted_at IS NULL`

	res, err := h.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, errors.New(op).WithErr(err).WithMsg("stamp session-emailed flag")
	}
	n, _ := res.RowsAffected()
	return n, nil
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

	row, err := models.FindQsoUpload(ctx, h, id)
	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return errors.ErrNotFound
		}
		return errors.New(op).WithErr(err)
	}

	row.Status = status.Pending.String()
	row.Attempts++
	row.NextAttemptAt = nextAttemptAt
	if lastError != "" {
		row.LastError = null.StringFrom(lastError)
	} else {
		row.LastError = null.String{}
	}

	if _, err = row.Update(ctx, h, boil.Infer()); err != nil {
		return errors.New(op).WithErr(err).WithMsg("mark upload transient retry")
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

	row, err := models.FindQsoUpload(ctx, h, id)
	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return errors.ErrNotFound
		}
		return errors.New(op).WithErr(err)
	}

	row.Status = status.Failed.String()
	row.Attempts++
	if lastError != "" {
		row.LastError = null.StringFrom(lastError)
	} else {
		row.LastError = null.String{}
	}

	if _, err = row.Update(ctx, h, boil.Infer()); err != nil {
		return errors.New(op).WithErr(err).WithMsg("mark upload failed")
	}
	return nil
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
// update row exist per pair; ORDER BY created_at DESC LIMIT 1 picks the most
// recent of them. created_at is set once at insert and never mutated, so the
// ordering is stable regardless of retry bookkeeping.
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
		qm.OrderBy("created_at DESC"),
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

	if err = model.Insert(ctx, tx, boil.Infer()); err != nil {
		return 0, errors.New(op).WithErr(err)
	}

	return model.ID, nil
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

	model.ModifiedAt = null.TimeFrom(time.Now())

	return updateActiveQso(ctx, tx, model)
}

// DeleteQsoByIDTx soft-deletes a QSO within the caller-supplied tx by
// setting its deleted_at column (sqlboiler's generated Delete
// honours the add-soft-deletes flag). Returns the QSO's logbook_id
// on success so the caller can emit an accurately-scoped
// qso.deleted event without a second round-trip, or ErrNotFound if
// the QSO does not exist or is already soft-deleted.
//
// Used by qsoservice.Delete to bundle the soft-delete with
// qso_upload(delete) inserts under the same one-fails-all-fail tx.
func (s *Service) DeleteQsoByIDTx(ctx context.Context, tx *sql.Tx, id int64) (int64, error) {
	const op errors.Op = "sqlite.Service.DeleteQsoByIDTx"
	if err := checkService(op, s); err != nil {
		return 0, err
	}
	if tx == nil {
		return 0, errors.New(op).WithMsg("tx is nil")
	}
	if id < 1 {
		return 0, errors.New(op).WithMsg(errMsgInvalidId)
	}

	qso, err := models.FindQso(ctx, tx, id)
	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return 0, errors.ErrNotFound
		}
		return 0, errors.New(op).WithErr(err)
	}

	if _, err = qso.Delete(ctx, tx, false); err != nil {
		return 0, errors.New(op).WithErr(err).WithMsg("failed to delete QSO")
	}
	return qso.LogbookID, nil
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
func (s *Service) InsertQsoUploadTx(ctx context.Context, tx *sql.Tx, qsoId int64, action action.Action, forwarderName, forwarderType string) error {
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

	// Re-arm preserves upstream_id deliberately: FetchPriorUpstreamIDWithContext
	// reads it back for the QRZ delete-after-insert flow, and the worker's
	// own success path overwrites it on the next successful attempt.
	// Clearing it here would lose history a re-armed insert (the rare
	// force=true edge case) might want to keep.
	const q = `
		INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (qso_id, forwarder_name, action) DO UPDATE SET
			forwarder_type  = excluded.forwarder_type,
			status          = 'pending',
			attempts        = 0,
			next_attempt_at = strftime('%s', 'now'),
			last_attempt_at = NULL,
			last_error      = NULL`

	if _, err := tx.ExecContext(ctx, q,
		qsoId, forwarderName, forwarderType, action.String(), status.Pending.String(),
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
