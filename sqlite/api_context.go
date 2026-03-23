package sqlite

import (
	"context"
	"database/sql"
	stderr "errors"
	"strings"
	"time"

	"github.com/Station-Manager/database/sqlite/adapters"
	"github.com/Station-Manager/database/sqlite/models"
	"github.com/Station-Manager/enums/upload"
	"github.com/Station-Manager/enums/upload/action"
	"github.com/Station-Manager/enums/upload/status"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
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
		return 0, errors.New(op).Err(err)
	}

	if err = model.Insert(ctx, h, boil.Infer()); err != nil {
		return 0, errors.New(op).Err(err)
	}

	return model.ID, nil
}

func (s *Service) FetchQsoSliceBySessionIDWithContext(ctx context.Context, id int64) (types.QsoSlice, error) {
	const op errors.Op = "sqlite.Service.FetchQsoSliceBySessionIDWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}

	if id < 1 {
		return nil, errors.New(op).Msg(errMsgInvalidId)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	var mods []qm.QueryMod
	mods = append(mods, models.QsoWhere.SessionID.EQ(id))
	mods = append(mods, qm.OrderBy(models.QsoColumns.CreatedAt+" DESC"))

	slice, err := models.Qsos(mods...).All(ctx, h)
	if err != nil {
		return nil, errors.New(op).Err(err).Msg("Failed to fetch QSOs by session ID.")
	}

	result := make([]types.Qso, 0, len(slice))
	for _, qso := range slice {
		typeQso, er := adapters.QsoModelToType(qso)
		if er != nil {
			s.LoggerService.WarnWith().Int64("qso.id", qso.ID).Err(er).Msg("Failed to adapt QSO for contact history.")
			continue
		}
		result = append(result, typeQso)
	}
	return result, nil
}

func (s *Service) FetchQsoSliceByCallsignWithContext(ctx context.Context, callsign string) ([]types.ContactHistory, error) {
	const op errors.Op = "sqlite.Service.FetchContactHistoryWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}

	callsign = strings.TrimSpace(callsign)
	if callsign == "" {
		return nil, errors.New(op).Msg(errMsgEmptyCallsign)
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
		return nil, errors.New(op).Err(err).Msg("Failed to fetch contact history.")
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
		return nil, errors.New(op).Msg(errMsgInvalidId)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	slice, err := models.Qsos(models.QsoWhere.LogbookID.EQ(id)).All(ctx, h)
	if err != nil {
		return nil, errors.New(op).Err(err).Msg("Failed to fetch QSO slice.")
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
		return 0, errors.New(op).Msg(errMsgInvalidId)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return 0, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	count, err := models.Qsos(models.QsoWhere.LogbookID.EQ(id)).Count(ctx, h)
	if err != nil {
		return 0, errors.New(op).Err(err).Msg("Failed to fetch QSO count by logbook ID.")
	}

	return count, nil
}

func (s *Service) UpdateQsoWithContext(ctx context.Context, qso types.Qso) error {
	const op errors.Op = "sqlite.Service.UpdateQsoWithContext"
	if err := checkService(op, s); err != nil {
		return err
	}

	if qso.ID < 1 {
		return errors.New(op).Msgf("QSO ID is invalid: %d", qso.ID)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	model, err := adapters.QsoTypeToModel(qso)
	if err != nil {
		return errors.New(op).Err(err)
	}

	model.ModifiedAt = null.TimeFrom(time.Now())

	if _, err = model.Update(ctx, h, boil.Infer()); err != nil {
		return errors.New(op).Err(err)
	}

	return nil
}

func (s *Service) FetchQsoSliceNotForwardedWithContext(ctx context.Context) (types.QsoSlice, error) {
	const op errors.Op = "sqlite.Service.FetchQsoSliceNotForwardedWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	modelSlice, err := models.Qsos(
		qm.Where(
			"json_extract(\"qso\".\"additional_data\", '$.QrzComUploadStatus') IS NULL "+
				"OR json_extract(\"qso\".\"additional_data\", '$.SmQsoUploadStatus') IS NULL",
		),
	).All(ctx, h)

	if err != nil {
		return nil, errors.New(op).Err(err).Msg("Failed to non-forwarded QSO slice.")
	}

	var typeSlice []types.Qso
	if modelSlice != nil {
		typeSlice = make([]types.Qso, 0, len(modelSlice))

		for _, qso := range modelSlice {
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

func (s *Service) InsertQsoUploadWithContext(ctx context.Context, qsoId int64, action action.Action, service upload.OnlineService) error {
	const op errors.Op = "sqlite.Service.InsertQsoUploadWithContext"
	if err := checkService(op, s); err != nil {
		return err
	}

	if qsoId < 1 {
		return errors.New(op).Msgf("QSO ID is invalid: %d", qsoId)
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
		return errors.New(op).Err(err).Msg("Inserting new QSO upload failed.")
	}

	return nil
}

func (s *Service) FetchQsoByIdWithContext(ctx context.Context, id int64) (types.Qso, error) {
	const op errors.Op = "sqlite.Service.FetchQsoByIdWithContext"
	if err := checkService(op, s); err != nil {
		return types.Qso{}, err
	}

	if id < 1 {
		return types.Qso{}, errors.New(op).Msg(errMsgInvalidId)
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
		return types.Qso{}, errors.New(op).Err(err)
	}

	qso, err := adapters.QsoModelToType(model)
	if err != nil {
		return types.Qso{}, errors.New(op).Err(err)
	}

	return qso, nil
}

func (s *Service) FetchQsoSlicePagingWithContext(ctx context.Context, logbookId, pageNum, pageSize int64, ordering Ordering) (types.QsoSlice, error) {
	const op errors.Op = "sqlite.Service.FetchQsoByCallsignWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}

	if logbookId < 1 {
		return nil, errors.New(op).Msg(errMsgInvalidId)
	}
	if pageNum < 1 {
		return nil, errors.New(op).Msg("Invalid page number. Must be greater than 0.")
	}
	if pageSize < 1 {
		return nil, errors.New(op).Msg("Invalid page size. Must be greater than 0.")
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
		return nil, errors.New(op).Err(err)
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
		return types.ContactedStation{}, errors.New(op).Msg(errMsgEmptyCallsign)
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
		return types.ContactedStation{}, errors.New(op).Err(err)
	}

	contactedStation, err := adapters.ContactedStationModelToType(model)
	if err != nil {
		return types.ContactedStation{}, errors.New(op).Err(err)
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
		return 0, errors.New(op).Err(err)
	}
	if err = model.Insert(ctx, h, boil.Infer()); err != nil {
		return 0, errors.New(op).Err(err).Msg("Inserting new contacted station failed.")
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
		return errors.New(op).Err(err)
	}

	model.ModifiedAt = null.TimeFrom(time.Now())

	_, err = model.Update(ctx, h, boil.Infer())
	if err != nil {
		return errors.New(op).Err(err).Msg("Updating contacted station failed.")
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
		return types.Country{}, errors.New(op).Msg(errMsgEmptyCallsign)
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
		return types.Country{}, errors.New(op).Err(err)
	}

	country, err := adapters.CountryModelToType(model)
	if err != nil {
		return types.Country{}, errors.New(op).Err(err)
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
		return types.Country{}, errors.New(op).Msg("Country name cannot be empty")
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
		return types.Country{}, errors.New(op).Err(err)
	}

	country, err := adapters.CountryModelToType(model)
	if err != nil {
		return types.Country{}, errors.New(op).Err(err)
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
		return 0, errors.New(op).Err(err)
	}
	if err = model.Insert(ctx, h, boil.Infer()); err != nil {
		return 0, errors.New(op).Err(err).Msg("Inserting new country failed.")
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
		return errors.New(op).Err(err)
	}

	if _, err = model.Update(ctx, h, boil.Infer()); err != nil {
		return errors.New(op).Err(err).Msg("Updating country failed.")
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
		return types.Logbook{}, errors.New(op).Msg(errMsgInvalidId)
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
		return types.Logbook{}, errors.New(op).Err(err)
	}

	logbook, err := adapters.LogbookModelToType(model)
	if err != nil {
		return types.Logbook{}, errors.New(op).Err(err)
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
		return nil, errors.New(op).Err(err)
	}

	result := make([]types.Logbook, 0, len(list))
	for _, logbook := range list {
		typeLogbook, er := adapters.LogbookModelToType(logbook)
		if er != nil {
			return nil, errors.New(op).Err(er).Msg("Converting logbook model to type failed.")
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
		return 0, errors.New(op).Err(err)
	}
	if err = model.Insert(ctx, h, boil.Infer()); err != nil {
		return 0, errors.New(op).Err(err).Msg("Inserting new logbook failed.")
	}

	return model.ID, nil
}

func (s *Service) DeleteLogbookByIDWithContext(ctx context.Context, id int64) error {
	const op errors.Op = "sqlite.Service.DeleteLogbookWithContext"
	if err := checkService(op, s); err != nil {
		return err
	}

	if id < 1 {
		return errors.New(op).Msg(errMsgInvalidId)
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
		return errors.New(op).Err(err)
	}

	if _, err = logbook.Delete(ctx, h, false); err != nil {
		return errors.New(op).Err(err).Msg("Failed to delete logbook.")
	}

	return nil
}

func (s *Service) CheckDefaultLogbookExistsWithContext(ctx context.Context) (bool, error) {
	const op errors.Op = "sqlite.Service.CheckDefaultLogbookExistsWithContext"
	if err := checkService(op, s); err != nil {
		return false, err
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return false, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	model, err := models.Logbooks(models.LogbookWhere.Name.EQ("Default")).One(ctx, h)
	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return false, errors.ErrNotFound
		}
		return false, errors.New(op).Err(err)
	}
	if model == nil {
		return false, errors.New(op).Msg("Default logbook not found")
	}
	return true, nil
}

func (s *Service) UpsertLogbookWithContext(ctx context.Context, logbook types.Logbook) error {
	const op errors.Op = "sqlite.Service.UpsertLogbookWithContext"
	if err := checkService(op, s); err != nil {
		return err
	}

	if logbook.ID < 1 {
		return errors.New(op).Msg("Logbook ID must be greater than 0")
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

	if err = model.Upsert(ctx, h, false, nil, boil.Infer(), boil.Infer()); err != nil {
		return errors.New(op).Err(err).Msg("Upserting logbook failed.")
	}

	return nil
}

/**********************************************************************************************************************
 * Session Methods
 **********************************************************************************************************************/

func (s *Service) SoftDeleteSessionByIDWithContext(ctx context.Context, id int64) error {
	const op errors.Op = "sqlite.Service.SoftDeleteSessionByIDWithContext"
	if err := checkService(op, s); err != nil {
		return err
	}

	if id < 1 {
		return errors.New(op).Msg(errMsgInvalidId)
	}
	h, err := s.getOpenHandle(op)
	if err != nil {
		return err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	model, err := models.FindSession(ctx, h, id)
	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return errors.ErrNotFound
		}
		return errors.New(op).Err(err)
	}

	if _, err = model.Delete(ctx, h, false); err != nil {
		return errors.New(op).Err(err).Msgf("Failed to soft delete session: %d", id)
	}

	return nil
}

func (s *Service) GenerateSessionWithContext(ctx context.Context) (int64, error) {
	const op errors.Op = "sqlite.Service.InsertSessionWithContext"
	if err := checkService(op, s); err != nil {
		return 0, err
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return 0, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	session := models.Session{}
	if err = session.Insert(ctx, h, boil.Infer()); err != nil {
		return 0, errors.New(op).Err(err).Msg("Inserting new session failed.")
	}

	return session.ID, nil
}

/**********************************************************************************************************************
 * Contest Related Methods
 **********************************************************************************************************************/

func (s *Service) IsContestDuplicateByLogbookIDWithContext(ctx context.Context, id int64, callsign, band string) (bool, error) {
	const op errors.Op = "sqlite.Service.IsContestDuplicatByLogbookIDWithContext"
	if err := checkService(op, s); err != nil {
		return false, err
	}

	if id < 1 {
		return false, errors.New(op).Msg(errMsgInvalidId)
	}
	callsign = strings.TrimSpace(callsign)
	if callsign == "" {
		return false, errors.New(op).Msg("Callsign cannot be empty")
	}

	band = strings.TrimSpace(band)
	if band == "" {
		return false, errors.New(op).Msg("Band cannot be empty")
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return false, err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	var mods []qm.QueryMod
	mods = append(mods, models.QsoWhere.Call.EQ(callsign))
	mods = append(mods, models.QsoWhere.Band.EQ(band))
	mods = append(mods, models.QsoWhere.LogbookID.EQ(id))

	exists, err := models.Qsos(mods...).Exists(ctx, h)
	if err != nil {
		return false, errors.New(op).Err(err)
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
		return nil, errors.New(op).Err(err).Msg("Failed to reserve pending uploads")
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
		return nil, errors.New(op).Err(err).Msg("Failed to load reserved uploads")
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
		return errors.New(op).Msg(errMsgInvalidId)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return err
	}

	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	tx, err := h.BeginTx(ctx, nil)
	if err != nil {
		return errors.New(op).Err(err).Msg("Failed to begin transaction")
	}

	uploadModel, err := models.FindQsoUpload(ctx, tx, id)
	if err != nil {
		_ = tx.Rollback()
		return errors.New(op).Err(err).Msg("Failed to find QSO upload")
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
		return errors.New(op).Err(err).Msg("Failed to update QSO upload status")
	}

	// At this point, we don't need to update the QSO itself as that SHOULD have been
	// done by the online-forwarder, since the online-forwarder knows what fields in the
	// qso object to update based on the service.

	if err = tx.Commit(); err != nil {
		return errors.New(op).Err(err).Msg("Failed to commit transaction")
	}

	return nil
}
