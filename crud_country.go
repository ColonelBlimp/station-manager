package database

//
//import (
//	"context"
//	"database/sql"
//	stderr "errors"
//	"github.com/Station-Manager/adapters"
//	"github.com/Station-Manager/adapters/converters/common"
//	sqmodels "github.com/Station-Manager/database/sqlite/models"
//	"github.com/Station-Manager/errors"
//	"github.com/Station-Manager/types"
//	"github.com/aarondl/null/v8"
//	"github.com/aarondl/sqlboiler/v4/boil"
//	"github.com/aarondl/sqlboiler/v4/queries/qm"
//	"strings"
//)
//
///*********************************************************************************************************************
//Insert Country Methods
//**********************************************************************************************************************/
//
//func (s *Service) InsertCountry(country types.Country) (types.Country, error) {
//	return s.InsertCountryContext(context.Background(), country)
//}
//
//func (s *Service) InsertCountryContext(ctx context.Context, country types.Country) (types.Country, error) {
//	return types.Country{}, nil
//}
//
///*********************************************************************************************************************
//Upsert Country Methods
//**********************************************************************************************************************/
//
//func (s *Service) UpsertCountry(country types.Country) (types.Country, error) {
//	return s.UpsertCountryContext(context.Background(), country)
//}
//
//func (s *Service) UpsertCountryContext(ctx context.Context, country types.Country) (types.Country, error) {
//	const op errors.Op = "database.Service.UpsertCountryContext"
//	emptyRetVal := types.Country{}
//	if err := checkService(op, s); err != nil {
//		return emptyRetVal, errors.New(op).Err(err)
//	}
//
//	//TODO: basic validation
//
//	switch s.DatabaseConfig.Driver {
//	case SqliteDriver:
//		return s.sqliteUpsertCountryContext(ctx, country)
//	case PostgresDriver:
//		return emptyRetVal, errors.New(op).Msg("Not supported. Desktop application only.")
//	default:
//		return emptyRetVal, errors.New(op).Errorf("Unsupported database driver: %s", s.DatabaseConfig.Driver)
//	}
//}
//
//func (s *Service) sqliteUpsertCountryContext(ctx context.Context, country types.Country) (types.Country, error) {
//	const op errors.Op = "database.Service.sqliteUpsertCountryContext"
//	emptyRetVal := types.Country{}
//
//	s.mu.RLock()
//	h := s.handle
//	isOpen := s.isOpen.Load()
//	s.mu.RUnlock()
//	if h == nil || !isOpen {
//		return emptyRetVal, errors.New(op).Msg(errMsgNotOpen)
//	}
//
//	// Apply default timeout if caller did not set one
//	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
//		var cancel context.CancelFunc
//		ctx, cancel = s.withDefaultTimeout(ctx)
//		defer cancel()
//	}
//
//	adapter := adapters.New()
//	adapter.RegisterConverter("Ccode", common.TypeToModelStringConverter)
//	adapter.RegisterConverter("DXCCPrefix", common.TypeToModelStringConverter)
//	adapter.RegisterConverter("TimeOffset", common.TypeToModelStringConverter)
//
//	model := sqmodels.Country{}
//	if err := adapter.Into(&model, &country); err != nil {
//		return emptyRetVal, errors.New(op).Err(err).Msg("Failed to adapt Country.")
//	}
//
//	s.Logger.DebugWith().Interface("country", country).Msg("Convertion.")
//
//	if err := model.Upsert(ctx, h, true, nil, boil.Infer(), boil.Infer()); err != nil {
//		return emptyRetVal, errors.New(op).Err(err)
//	}
//
//	return country, nil
//}
//
///*********************************************************************************************************************
//Fetch country Methods
//**********************************************************************************************************************/
//
//func (s *Service) FetchCountryByCallsign(callsign string) (types.Country, error) {
//	return s.FetchCountryByCallsignContext(context.Background(), callsign)
//}
//
//func (s *Service) FetchCountryByCallsignContext(ctx context.Context, callsign string) (types.Country, error) {
//	const op errors.Op = "database.Service.FetchQsosBySessionIdContext"
//	emptyRetVal := types.Country{}
//	if err := checkService(op, s); err != nil {
//		return emptyRetVal, errors.New(op).Err(err)
//	}
//	callsign = strings.TrimSpace(callsign)
//	if callsign == "" {
//		return emptyRetVal, errors.New(op).Msg("country name is empty")
//	}
//
//	switch s.DatabaseConfig.Driver {
//	case SqliteDriver:
//		return s.sqliteFetchCountryByCallsignContext(ctx, callsign)
//	case PostgresDriver:
//		return emptyRetVal, errors.New(op).Msg("Not supported. Desktop application only.")
//	default:
//		return emptyRetVal, errors.New(op).Errorf("Unsupported database driver: %s", s.DatabaseConfig.Driver)
//	}
//}
//
//func (s *Service) sqliteFetchCountryByCallsignContext(ctx context.Context, callsign string) (types.Country, error) {
//	const op errors.Op = "database.Service.sqliteFetchCountryByCallsignContext"
//	emptyRetVal := types.Country{}
//
//	s.mu.RLock()
//	h := s.handle
//	isOpen := s.isOpen.Load()
//	s.mu.RUnlock()
//	if h == nil || !isOpen {
//		return emptyRetVal, errors.New(op).Msg(errMsgNotOpen)
//	}
//
//	// Apply default timeout if caller did not set one
//	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
//		var cancel context.CancelFunc
//		ctx, cancel = s.withDefaultTimeout(ctx)
//		defer cancel()
//	}
//
//	mods := []qm.QueryMod{
//		qm.Where("? LIKE "+sqmodels.TableNames.Country+".prefix || '%'", callsign),
//		qm.OrderBy("LENGTH(" + sqmodels.TableNames.Country + ".prefix) DESC"),
//		qm.Limit(1),
//	}
//
//	model, err := sqmodels.Countries(mods...).One(ctx, h)
//	if err != nil && !stderr.Is(err, sql.ErrNoRows) {
//		return emptyRetVal, errors.New(op).Err(err)
//	}
//
//	if err != nil || model == nil {
//		return emptyRetVal, errors.ErrNotFound
//	}
//
//	s.initAdapters()
//	adapter := s.adapterFromModel
//	country := types.Country{}
//	if err = adapter.Into(&country, model); err != nil {
//		return emptyRetVal, errors.New(op).Err(err).Msg("Failed to adapt Country.")
//	}
//
//	return country, nil
//}
//
///*********************************************************************************************************************
//Country Exists Methods
//**********************************************************************************************************************/
//
//func (s *Service) CountryExistsByName(countryName string) (bool, error) {
//	return s.CountryExistsByNameContext(context.Background(), countryName)
//}
//
//func (s *Service) CountryExistsByNameContext(ctx context.Context, countryName string) (bool, error) {
//	const op errors.Op = "database.Service.CountryExistsByName"
//	if err := checkService(op, s); err != nil {
//		return false, errors.New(op).Err(err)
//	}
//	countryName = strings.TrimSpace(countryName)
//	if countryName == "" {
//		return false, errors.New(op).Msg("country name is empty")
//	}
//
//	switch s.DatabaseConfig.Driver {
//	case SqliteDriver:
//		return s.sqliteCountryExistsByNameContext(ctx, countryName)
//	case PostgresDriver:
//		return false, errors.New(op).Msg("Not supported. Desktop application only.")
//	default:
//		return false, errors.New(op).Errorf("Unsupported database driver: %s", s.DatabaseConfig.Driver)
//	}
//}
//
//func (s *Service) sqliteCountryExistsByNameContext(ctx context.Context, countryName string) (bool, error) {
//	const op errors.Op = "database.Service.sqliteCountryExistsByNameContext"
//	if err := checkService(op, s); err != nil {
//		return false, errors.New(op).Err(err)
//	}
//
//	s.mu.RLock()
//	h := s.handle
//	isOpen := s.isOpen.Load()
//	s.mu.RUnlock()
//	if h == nil || !isOpen {
//		return false, errors.New(op).Msg(errMsgNotOpen)
//	}
//
//	// Apply default timeout if caller did not set one
//	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
//		var cancel context.CancelFunc
//		ctx, cancel = s.withDefaultTimeout(ctx)
//		defer cancel()
//	}
//
//	exists, err := sqmodels.Countries(sqmodels.CountryWhere.Name.EQ(countryName)).Exists(ctx, h)
//	if err != nil {
//		return false, errors.New(op).Err(err)
//	}
//	return exists, nil
//}
//
//func (s *Service) CountryExistsByNameInQsoTable(countryName string) (bool, error) {
//	return s.CountryExistsByNameInQsoTableContext(context.Background(), countryName)
//}
//
//func (s *Service) CountryExistsByNameInQsoTableContext(ctx context.Context, countryName string) (bool, error) {
//	const op errors.Op = "database.Service.CountryExistsByNameInQsoTableContext"
//	if err := checkService(op, s); err != nil {
//		return false, errors.New(op).Err(err)
//	}
//	countryName = strings.TrimSpace(countryName)
//	if countryName == "" {
//		return false, errors.New(op).Msg("country name is empty")
//	}
//
//	switch s.DatabaseConfig.Driver {
//	case SqliteDriver:
//		return s.sqliteCountryExistsByNameInQsoTableContext(ctx, countryName)
//	case PostgresDriver:
//		return false, errors.New(op).Msg("Not supported. Desktop application only.")
//	default:
//		return false, errors.New(op).Errorf("Unsupported database driver: %s", s.DatabaseConfig.Driver)
//	}
//}
//
//func (s *Service) sqliteCountryExistsByNameInQsoTableContext(ctx context.Context, countryName string) (bool, error) {
//	const op errors.Op = "database.Service.sqliteCountryExistsByNameInQsoTableContext"
//
//	s.mu.RLock()
//	h := s.handle
//	isOpen := s.isOpen.Load()
//	s.mu.RUnlock()
//	if h == nil || !isOpen {
//		return false, errors.New(op).Msg(errMsgNotOpen)
//	}
//
//	// Apply default timeout if caller did not set one
//	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
//		var cancel context.CancelFunc
//		ctx, cancel = s.withDefaultTimeout(ctx)
//		defer cancel()
//	}
//
//	exists, err := sqmodels.Qsos(sqmodels.QsoWhere.Country.EQ(null.StringFrom(countryName))).Exists(ctx, h)
//	if err != nil {
//		return false, errors.New(op).Err(err)
//	}
//	return exists, nil
//}
