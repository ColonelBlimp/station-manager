package sqlite

import (
	"context"
	"database/sql"
	stderr "errors"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite/adapters"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite/models"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload"
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

func (s *Service) FetchQsoSliceByCallsignWithContext(ctx context.Context, callsign string) ([]types.ContactHistory, error) {
	const op errors.Op = "sqlite.Service.FetchContactHistoryWithContext"
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

	var mods []qm.QueryMod
	mods = append(mods, models.QsoWhere.Call.EQ(callsign))
	mods = append(mods, qm.Or2(
		models.QsoWhere.Call.LIKE(callsign+"%"),
	))

	mods = append(mods, qm.OrderBy(models.QsoColumns.CreatedAt+" DESC"))
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

// DeleteQsoByIDWithContext soft-deletes a QSO by setting its deleted_at
// column. Subsequent FetchQsoByIdWithContext calls return ErrNotFound
// because FindQso filters deleted rows. Returns ErrNotFound if the QSO
// does not exist (or is already soft-deleted). No FK check is needed —
// QSO is the child row.
func (s *Service) DeleteQsoByIDWithContext(ctx context.Context, id int64) error {
	const op errors.Op = "sqlite.Service.DeleteQsoByIDWithContext"
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

	qso, err := models.FindQso(ctx, h, id)
	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return errors.ErrNotFound
		}
		return errors.New(op).WithErr(err)
	}

	if _, err = qso.Delete(ctx, h, false); err != nil {
		return errors.New(op).WithErr(err).WithMsg("failed to delete QSO")
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

	if _, err = model.Update(ctx, h, boil.Infer()); err != nil {
		return errors.New(op).WithErr(err)
	}

	return nil
}

func (s *Service) InsertQsoUploadWithContext(ctx context.Context, qsoId int64, action action.Action, service upload.OnlineService) error {
	const op errors.Op = "sqlite.Service.InsertQsoUploadWithContext"
	if err := checkService(op, s); err != nil {
		return err
	}

	if qsoId < 1 {
		return errors.New(op).WithMsgf("QSO ID is invalid: %d", qsoId)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	model := models.QsoUpload{
		QsoID:   qsoId,
		Service: service.String(),
		Action:  action.String(),
		Status:  status.Pending.String(),
	}

	if err = model.Insert(ctx, h, boil.Infer()); err != nil {
		return errors.New(op).WithErr(err).WithMsg("Inserting new QSO upload failed.")
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
	qsoCount, err := models.Qsos(models.QsoWhere.LogbookID.EQ(id)).Count(ctx, h)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("checking logbook QSO count")
	}
	if qsoCount > 0 {
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
 * Upload Methods
 **********************************************************************************************************************/

func (s *Service) FetchPendingUploadsWithContext(ctx context.Context) ([]types.QsoUpload, error) {
	const op errors.Op = "sqlite.Service.FetchPendingUploadsWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	batchLimit := defaultUploadBatchLimit
	if s.requiredCfgs.QsoForwardingRowLimit > 0 {
		batchLimit = s.requiredCfgs.QsoForwardingRowLimit
	}

	now := time.Now()
	cutoff := now.Add(-uploadRetryCooldown).Unix()

	// Reserve a batch and capture their IDs
	updateAndReturn := `
		UPDATE qso_upload
		   SET status = ?, modified_at = ?, last_attempt_at = ?
		 WHERE id IN (
		     SELECT id
		       FROM qso_upload
		      WHERE status IN (?, ?)
		        AND (last_attempt_at IS NULL OR last_attempt_at < ?)
		      LIMIT ?
		   )
		RETURNING id`

	type returnID struct {
		ID int64 `boil:"id"`
	}

	var rows []returnID

	err = queries.Raw(
		updateAndReturn,
		status.InProgress.String(),
		null.TimeFrom(now),
		null.Int64From(now.Unix()),
		status.Pending.String(),
		status.Failed.String(),
		cutoff,
		batchLimit,
	).Bind(ctx, h, &rows)
	if err != nil && !stderr.Is(err, sql.ErrNoRows) {
		return nil, errors.New(op).WithErr(err).WithMsg("Failed to reserve pending uploads")
	}

	if len(rows) == 0 {
		return nil, nil
	}

	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	idArgs := make([]interface{}, len(ids))
	for i, v := range ids {
		idArgs[i] = v
	}

	// Fetch the reserved rows with QSO eagerly loaded.
	uploads, err := models.QsoUploads(
		qm.WhereIn("qso_upload.id IN ?", idArgs...),
		qm.Load(models.QsoUploadRels.Qso),
	).All(ctx, h)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("Failed to load reserved uploads")
	}

	// Adapt to types.QsoUpload.
	out := make([]types.QsoUpload, 0, len(uploads))
	for _, ref := range uploads {
		up := types.QsoUpload{
			ID:            ref.ID,
			QsoID:         ref.QsoID,
			Service:       ref.Service,
			Action:        ref.Action,
			Status:        ref.Status,
			Attempts:      ref.Attempts,
			LastError:     ref.LastError.String,
			LastAttemptAt: ref.LastAttemptAt.Int64,
		}
		if ref.R != nil && ref.R.Qso != nil {
			qso, er := adapters.QsoModelToType(ref.R.Qso)
			if er != nil {
				s.LoggerService.ErrorWith().Err(er).Msg("Failed to adapt QSO for QsoUpload.")
				continue
			}
			up.Qso = qso
		}
		out = append(out, up)
	}

	return out, nil
}

func (s *Service) UpdateQsoUploadStatusWithContext(ctx context.Context, id int64, status status.Status, action action.Action, attempts int64, lastError string) error {
	const op errors.Op = "sqlite.Service.UpdateQsoUploadStatusWithContext"
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

	tx, err := h.BeginTx(ctx, nil)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("Failed to begin transaction")
	}

	uploadModel, err := models.FindQsoUpload(ctx, tx, id)
	if err != nil {
		_ = tx.Rollback()
		return errors.New(op).WithErr(err).WithMsg("Failed to find QSO upload")
	}

	uploadModel.Status = status.String()
	uploadModel.Action = action.String()
	uploadModel.Attempts = attempts
	uploadModel.LastError = null.NewString(lastError, lastError != "")
	uploadModel.ModifiedAt = null.TimeFrom(time.Now())

	// Clear last_attempt_at on failure so it can be retried on next poll
	if uploadModel.Status == "failed" {
		uploadModel.LastAttemptAt = null.Int64{}
	}

	_, err = uploadModel.Update(ctx, tx, boil.Infer())
	if err != nil {
		_ = tx.Rollback()
		return errors.New(op).WithErr(err).WithMsg("Failed to update QSO upload status")
	}

	// At this point, we don't need to update the QSO itself as that SHOULD have been
	// done by the online-forwarder, since the online-forwarder knows what fields in the
	// qso object to update based on the service.

	if err = tx.Commit(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("Failed to commit transaction")
	}

	return nil
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

func (s *Service) InsertQsoUploadTx(ctx context.Context, tx *sql.Tx, qsoId int64, action action.Action, service upload.OnlineService) error {
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

	model := models.QsoUpload{
		QsoID:   qsoId,
		Service: service.String(),
		Action:  action.String(),
		Status:  status.Pending.String(),
	}

	if err := model.Insert(ctx, tx, boil.Infer()); err != nil {
		return errors.New(op).WithErr(err).WithMsg("Inserting new QSO upload failed.")
	}

	return nil
}
