package sqlite

import (
	"context"
	"database/sql"
	stderr "errors"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite/adapters"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite/models"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/status"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/aarondl/null/v8"
	"github.com/aarondl/sqlboiler/v4/boil"
	"github.com/aarondl/sqlboiler/v4/queries"
	"github.com/aarondl/sqlboiler/v4/queries/qm"
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
// variant ("M0CMC/P", "M0CMC/MM", "M0CMC/DX"). The LIKE anchors on a
// slash — `call LIKE 'M0CMC/%'` — so unrelated callsigns that happen
// to share a prefix (e.g. "M0CMCE") are excluded. Slash is not a LIKE
// metacharacter, so no ESCAPE clause is needed. Callers are still
// expected to pass the base callsign in canonical form (uppercase,
// trimmed); the handler layer does that before calling in.
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

	// Wrap the call match in Expr so the OR group is parenthesised. Without
	// it, AND-ing additional predicates (logbook_id, the implicit
	// deleted_at IS NULL from sqlboiler's default) would bind tighter than
	// the OR and leak rows that don't satisfy the extra constraints.
	//
	// The LIKE pattern "X/%" matches portable variants only (M0CMC/P,
	// M0CMC/MM, …) and excludes coincidental prefixes like M0CMCE.
	mods := []qm.QueryMod{
		qm.Expr(
			models.QsoWhere.Call.EQ(callsign),
			qm.Or2(models.QsoWhere.Call.LIKE(callsign+"/%")),
		),
	}
	if logbookID > 0 {
		mods = append(mods, models.QsoWhere.LogbookID.EQ(logbookID))
	}
	mods = append(mods, qm.OrderBy(models.QsoColumns.CreatedAt+" DESC"))
	if limit > 0 {
		mods = append(mods, qm.Limit(limit))
	}
	slice, err := models.Qsos(mods...).All(ctx, h)
	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return nil, errors.ErrNotFound
		}
		return nil, errors.New(op).WithErr(err).WithMsg("Failed to fetch contact history.")
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

// SchemaVersionWithContext returns the current migration version recorded
// in the schema_migrations table (maintained by golang-migrate). Returns
// 0 if no migrations have been applied yet (fresh DB). The `dirty` flag
// is true if the last migration attempt failed mid-way.
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

	if _, err = model.Update(ctx, h, boil.Infer()); err != nil {
		return errors.New(op).WithErr(err)
	}

	return nil
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

func (s *Service) InsertCountryWithContext(ctx context.Context, country types.Country) (int64, error) {
	const op errors.Op = "sqlite.Service.InsertCountryWithContext"
	if err := checkService(op, s); err != nil {
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

/**********************************************************************************************************************
 * Logbook Methods
 **********************************************************************************************************************/

// LogbookCallsignByIDWithContext returns the callsign of a logbook by
// ID. Cheaper than FetchLogbookByIDWithContext when the caller only
// needs the callsign (notably the submit hot path, which compares it
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
		return errors.New(op).WithMsg("cannot delete a logbook that contains QSOs")
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

	model, err := models.FindLogbook(ctx, h, logbook.ID)
	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return errors.ErrNotFound
		}
		return errors.New(op).WithErr(err)
	}

	model.Name = logbook.Name
	model.Callsign = logbook.Callsign
	model.Description = null.StringFrom(logbook.Description)

	if _, err = model.Update(ctx, h, boil.Infer()); err != nil {
		return errors.New(op).WithErr(err).WithMsg("failed to update logbook")
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
// retries don't jump the queue) and ordered by next_attempt_at ASC
// (FIFO). Returns an empty slice when there is nothing to claim.
//
// SQLite is single-writer, so the UPDATE...RETURNING is race-free by
// construction; the per-forwarder scope (forwarder_name = ?) also
// means two workers for different destinations never compete for a
// row.
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
    ORDER BY next_attempt_at
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
	return out, nil
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
// given QSO, ordered by (forwarder_name, action) so the output is
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

	if _, err = model.Update(ctx, tx, boil.Infer()); err != nil {
		return errors.New(op).WithErr(err)
	}

	return nil
}

// DeleteQsoByIDTx soft-deletes a QSO within the caller-supplied tx by
// setting its deleted_at column (sqlboiler's generated Delete
// honours the add-soft-deletes flag). Returns ErrNotFound if the QSO
// does not exist or is already soft-deleted.
//
// Used by qsoservice.Delete to bundle the soft-delete with
// qso_upload(delete) inserts under the same one-fails-all-fail tx.
func (s *Service) DeleteQsoByIDTx(ctx context.Context, tx *sql.Tx, id int64) error {
	const op errors.Op = "sqlite.Service.DeleteQsoByIDTx"
	if err := checkService(op, s); err != nil {
		return err
	}
	if tx == nil {
		return errors.New(op).WithMsg("tx is nil")
	}
	if id < 1 {
		return errors.New(op).WithMsg(errMsgInvalidId)
	}

	qso, err := models.FindQso(ctx, tx, id)
	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return errors.ErrNotFound
		}
		return errors.New(op).WithErr(err)
	}

	if _, err = qso.Delete(ctx, tx, false); err != nil {
		return errors.New(op).WithErr(err).WithMsg("failed to delete QSO")
	}
	return nil
}

// InsertQsoUploadTx enqueues one qso_upload row within the caller-supplied tx.
// forwarderName is the per-instance config handle (e.g. "qrz-primary");
// forwarderType is the plugin identifier (e.g. "qrz"). Both are stored on the
// row so historical queue entries remain interpretable even after an operator
// renames or removes the destination from config. See docs/v2-design/forwarding.md §6.
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

	model := models.QsoUpload{
		QsoID:         qsoId,
		ForwarderName: forwarderName,
		ForwarderType: forwarderType,
		Action:        action.String(),
		Status:        status.Pending.String(),
	}

	if err := model.Insert(ctx, tx, boil.Infer()); err != nil {
		return errors.New(op).WithErr(err).WithMsg("Inserting new QSO upload failed.")
	}

	return nil
}
