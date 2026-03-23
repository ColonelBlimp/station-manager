package database

//
//import (
//	"context"
//	"database/sql"
//	stderr "errors"
//	"fmt"
//	pgmodels "github.com/Station-Manager/database/postgres/models"
//	sqmodels "github.com/Station-Manager/database/sqlite/models"
//	"github.com/Station-Manager/errors"
//	"github.com/Station-Manager/types"
//	"github.com/aarondl/sqlboiler/v4/boil"
//	"github.com/aarondl/sqlboiler/v4/queries/qm"
//	"strings"
//)
//
///*********************************************************************************************************************
//Fetch QSO Methods
//**********************************************************************************************************************/
//
//// FetchQsoById retrieves a QSO record by its unique ID and returns it, defaulting to the background context for execution.
//func (s *Service) FetchQsoById(id int64) (types.Qso, error) {
//	return s.FetchQsoByIdContext(context.Background(), id)
//}
//
//// FetchQsoByIdContext fetches a QSO record by its unique ID within the provided context and returns it or an error.
//func (s *Service) FetchQsoByIdContext(ctx context.Context, id int64) (types.Qso, error) {
//	const op errors.Op = "database.Service.FetchQsoByIdContext"
//	emptyRetVal := types.Qso{}
//	if err := checkService(op, s); err != nil {
//		return emptyRetVal, errors.New(op).Err(err)
//	}
//	if id < 1 {
//		return emptyRetVal, errors.New(op).Msg(errMsgInvalidId)
//	}
//
//	switch s.DatabaseConfig.Driver {
//	case SqliteDriver:
//		return s.sqliteFetchQsoContext(ctx, id)
//	case PostgresDriver:
//		return s.postgresFetchQsoContext(ctx, id)
//	default:
//		return emptyRetVal, errors.New(op).Errorf("Unsupported database driver: %s", s.DatabaseConfig.Driver)
//	}
//}
//
//// sqliteFetchQsoContext retrieves a QSO record by its ID within the provided context using SQLite as the database driver.
//// It initializes required adapters, checks the service state, and returns the QSO record or an error.
//func (s *Service) sqliteFetchQsoContext(ctx context.Context, id int64) (types.Qso, error) {
//	const op errors.Op = "database.Service.sqliteFetchQsoContext"
//	emptyRetVal := types.Qso{}
//	if err := checkService(op, s); err != nil {
//		return emptyRetVal, errors.New(op).Err(err)
//	}
//
//	s.mu.RLock()
//	h := s.handle
//	isOpen := s.isOpen.Load()
//	s.mu.RUnlock()
//	if h == nil || !isOpen {
//		return emptyRetVal, errors.New(op).Msg(errMsgNotOpen)
//	}
//
//	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
//		var cancel context.CancelFunc
//		ctx, cancel = s.withDefaultTimeout(ctx)
//		defer cancel()
//	}
//
//	model, err := sqmodels.FindQso(ctx, h, id)
//	if err != nil {
//		return emptyRetVal, errors.New(op).Err(err)
//	}
//
//	s.initAdapters()
//	adapter := s.adapterFromModel
//	out := types.Qso{}
//	if err := adapter.Into(&out, model); err != nil {
//		return emptyRetVal, errors.New(op).Err(err)
//	}
//	return out, nil
//}
//
//// postgresFetchQsoContext retrieves a QSO record by its ID from a PostgreSQL database and returns a populated Qso object.
//// It validates the service state, uses a database connection, applies a default timeout if none is set, and adapts the
//// database result into the application's Qso type. Returns an error if any operation fails.
//func (s *Service) postgresFetchQsoContext(ctx context.Context, id int64) (types.Qso, error) {
//	const op errors.Op = "database.Service.postgresFetchQsoContext"
//	emptyRetVal := types.Qso{}
//	if err := checkService(op, s); err != nil {
//		return emptyRetVal, errors.New(op).Err(err)
//	}
//
//	s.mu.RLock()
//	h := s.handle
//	isOpen := s.isOpen.Load()
//	s.mu.RUnlock()
//	if h == nil || !isOpen {
//		return emptyRetVal, errors.New(op).Msg(errMsgNotOpen)
//	}
//
//	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
//		var cancel context.CancelFunc
//		ctx, cancel = s.withDefaultTimeout(ctx)
//		defer cancel()
//	}
//
//	model, err := pgmodels.FindQso(ctx, h, id)
//	if err != nil {
//		return emptyRetVal, errors.New(op).Err(err)
//	}
//
//	s.initAdapters()
//	adapter := s.adapterFromModel
//	out := types.Qso{}
//	if err := adapter.Into(&out, model); err != nil {
//		return emptyRetVal, errors.New(op).Err(err)
//	}
//	return out, nil
//}
//
//func (s *Service) FetchQsosBySessionId(id int64, includeForwarded bool, includeDeleted bool) ([]types.Qso, error) {
//	return s.FetchQsosBySessionIdContext(context.Background(), id, includeForwarded, includeDeleted)
//}
//
//func (s *Service) FetchQsosBySessionIdContext(ctx context.Context, sessionId int64, includeForwarded bool, includeDeleted bool) ([]types.Qso, error) {
//	const op errors.Op = "database.Service.FetchQsosBySessionIdContext"
//	if err := checkService(op, s); err != nil {
//		return nil, errors.New(op).Err(err)
//	}
//	if sessionId < 1 {
//		return nil, errors.New(op).Msg("Invalid session ID")
//	}
//
//	switch s.DatabaseConfig.Driver {
//	case SqliteDriver:
//		return s.sqliteFetchQsosBySessionIdContext(ctx, sessionId, includeForwarded, includeDeleted)
//	case PostgresDriver:
//		return nil, errors.New(op).Msg("Not supported. Desktop application only.")
//	default:
//		return nil, errors.New(op).Errorf("Unsupported database driver: %s", s.DatabaseConfig.Driver)
//	}
//}
//
//func (s *Service) sqliteFetchQsosBySessionIdContext(ctx context.Context, sessionID int64, includeForwarded bool, includeDeleted bool) ([]types.Qso, error) {
//	const op errors.Op = "database.Service.sqliteFetchQsosBySessionIdContext"
//
//	s.mu.RLock()
//	h := s.handle
//	isOpen := s.isOpen.Load()
//	s.mu.RUnlock()
//	if h == nil || !isOpen {
//		return nil, errors.New(op).Msg(errMsgNotOpen)
//	}
//
//	var mods []qm.QueryMod
//	mods = append(mods, sqmodels.QsoWhere.SessionID.EQ(sessionID))
//	mods = append(mods, qm.OrderBy(sqmodels.QsoColumns.CreatedAt+" DESC"))
//	//if !includeForwarded {
//	//	// Only include QSOs that are directly marked as NOT ("N") forwarded by email.
//	//	mods = append(mods, sqmodels.QsoWhere.SmFwrdByEmailStatus.EQ(null.StringFrom(adif.NoString)))
//	//}
//	if includeDeleted {
//		mods = append(mods, qm.WithDeleted())
//	}
//
//	// Apply default timeout if caller did not set one
//	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
//		var cancel context.CancelFunc
//		ctx, cancel = s.withDefaultTimeout(ctx)
//		defer cancel()
//	}
//
//	slice, err := sqmodels.Qsos(mods...).All(ctx, h)
//	if err != nil {
//		return nil, errors.New(op).Err(err).Msg("sqmodels.Qsos(mods...).All")
//	}
//
//	s.initAdapters()
//	adapter := s.adapterFromModel
//
//	list := make(types.QsoSlice, 0, len(slice))
//	for _, qsoModel := range slice {
//		qso := types.Qso{}
//		if err = adapter.Into(&qso, qsoModel); err != nil {
//			return nil, errors.New(op).Err(err).Msg("Failed to adapt QSO for contact history.")
//		}
//		list = append(list, qso)
//	}
//
//	return list, nil
//}
//
///*********************************************************************************************************************
//Insert QSO Methods
//**********************************************************************************************************************/
//
//// InsertQso inserts a QSO using a background context with default timeout semantics.
//func (s *Service) InsertQso(qso types.Qso) (types.Qso, error) {
//	return s.InsertQsoContext(context.Background(), qso)
//}
//
//// InsertQsoContext inserts a QSO with caller-provided context.
//// If the context has no deadline, a default timeout is applied.
//func (s *Service) InsertQsoContext(ctx context.Context, qso types.Qso) (types.Qso, error) {
//	const op errors.Op = "database.Service.InsertQsoContext"
//	if err := checkService(op, s); err != nil {
//		return qso, errors.New(op).Err(err)
//	}
//
//	switch s.DatabaseConfig.Driver {
//	case SqliteDriver:
//		return s.sqliteInsertQsoContext(ctx, qso)
//	case PostgresDriver:
//		return s.postgresInsertQsoContext(ctx, qso)
//	default:
//		return qso, errors.New(op).Errorf("Unsupported database driver: %s", s.DatabaseConfig.Driver)
//	}
//}
//
//// sqliteInsertQsoContext inserts a QSO entry into the SQLite database within the given context and returns the updated QSO.
//func (s *Service) sqliteInsertQsoContext(ctx context.Context, qso types.Qso) (types.Qso, error) {
//	const op errors.Op = "database.Service.sqliteInsertQsoContext"
//	if err := checkService(op, s); err != nil {
//		return qso, errors.New(op).Err(err)
//	}
//	if qso.LogbookID < 1 {
//		return qso, errors.New(op).Msg("LogbookID is required")
//	}
//	// SessionID is required for SQLite, but not for PostgreSQL. This is because SQLite is only used for desktop apps
//	// where sessions are used.
//	if qso.SessionID < 1 {
//		return qso, errors.New(op).Msg("SessionID is required")
//	}
//
//	s.mu.RLock()
//	h := s.handle
//	isOpen := s.isOpen.Load()
//	s.mu.RUnlock()
//	if h == nil || !isOpen {
//		return qso, errors.New(op).Msg(errMsgNotOpen)
//	}
//
//	// Apply default timeout if caller did not set one
//	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
//		var cancel context.CancelFunc
//		ctx, cancel = s.withDefaultTimeout(ctx)
//		defer cancel()
//	}
//
//	s.initAdapters()
//	model, err := s.AdaptTypeToSqliteModelQso(qso)
//	if err != nil {
//		return qso, errors.New(op).Err(err)
//	}
//
//	if len(model.AdditionalData) == 0 {
//		model.AdditionalData = []byte("{}")
//	}
//
//	if err = model.Insert(ctx, h, boil.Infer()); err != nil {
//		return qso, errors.New(op).Err(err)
//	}
//	qso.ID = model.ID
//	return qso, nil
//}
//
//// postgresInsertQsoContext inserts a QSO record into the PostgreSQL database and returns the updated QSO object or an error.
//// It ensures the database service is initialized and open, and applies a default timeout if none exists in the context.
//// The method adapts the QSO type to the PostgreSQL model before performing the insert operation.
//func (s *Service) postgresInsertQsoContext(ctx context.Context, qso types.Qso) (types.Qso, error) {
//	const op errors.Op = "database.Service.postgresInsertQsoContext"
//	if err := checkService(op, s); err != nil {
//		return qso, errors.New(op).Err(err)
//	}
//	if qso.LogbookID < 1 {
//		return qso, errors.New(op).Msg("LogbookID is required")
//	}
//
//	s.mu.RLock()
//	h := s.handle
//	isOpen := s.isOpen.Load()
//	s.mu.RUnlock()
//	if h == nil || !isOpen {
//		return qso, errors.New(op).Msg(errMsgNotOpen)
//	}
//
//	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
//		var cancel context.CancelFunc
//		ctx, cancel = s.withDefaultTimeout(ctx)
//		defer cancel()
//	}
//
//	s.initAdapters()
//	model, err := s.AdaptTypeToPostgresModelQso(qso)
//	if err != nil {
//		return qso, errors.New(op).Err(err)
//	}
//
//	if err = model.Insert(ctx, h, boil.Infer()); err != nil {
//		return qso, errors.New(op).Err(err)
//	}
//
//	qso.ID = model.ID
//	return qso, nil
//}
//
///*********************************************************************************************************************
//Update QSO Methods
//**********************************************************************************************************************/
//
//// UpdateQso delegates to UpdateQsoContext with a background context.
//func (s *Service) UpdateQso(qso types.Qso) error {
//	return s.UpdateQsoContext(context.Background(), qso)
//}
//
//// UpdateQsoContext updates an existing QSO with caller-provided context.
//func (s *Service) UpdateQsoContext(ctx context.Context, qso types.Qso) error {
//	const op errors.Op = "database.Service.UpdateQsoContext"
//	if err := checkService(op, s); err != nil {
//		return errors.New(op).Err(err)
//	}
//	if qso.ID < 1 {
//		return errors.New(op).Msg(errMsgInvalidId)
//	}
//
//	switch s.DatabaseConfig.Driver {
//	case SqliteDriver:
//		return s.sqliteUpdateQsoContext(ctx, qso)
//	case PostgresDriver:
//		return s.postgresUpdateQsoContext(ctx, qso)
//	default:
//		return errors.New(op).Errorf("Unsupported database driver: %s", s.DatabaseConfig.Driver)
//	}
//}
//
//// sqliteUpdateQsoContext updates a QSO record in the SQLite database within a transactional context.
//// It ensures the service is operational, adapts a QSO object to the SQLite model, and commits changes if successful.
//// Returns an error if the service is not ready, the transaction fails, or the update operation encounters issues.
//func (s *Service) sqliteUpdateQsoContext(ctx context.Context, qso types.Qso) error {
//	const op errors.Op = "database.Service.sqliteUpdateQsoContext"
//	if err := checkService(op, s); err != nil {
//		return errors.New(op).Err(err)
//	}
//
//	s.mu.RLock()
//	h := s.handle
//	isOpen := s.isOpen.Load()
//	s.mu.RUnlock()
//
//	if h == nil || !isOpen {
//		return errors.New(op).Msg(errMsgNotOpen)
//	}
//
//	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
//		var cancel context.CancelFunc
//		ctx, cancel = s.withDefaultTimeout(ctx)
//		defer cancel()
//	}
//
//	tx, txCancel, err := s.BeginTxContext(ctx)
//	if err != nil {
//		return errors.New(op).Err(err)
//	}
//	defer txCancel()
//
//	s.initAdapters()
//
//	model, err := s.AdaptTypeToSqliteModelQso(qso)
//	if err != nil {
//		_ = tx.Rollback()
//		return errors.New(op).Err(err)
//	}
//
//	model.ID = qso.ID
//
//	if len(model.AdditionalData) == 0 {
//		model.AdditionalData = []byte("{}")
//	}
//	rows, err := model.Update(ctx, tx, boil.Infer())
//	if err != nil {
//		_ = tx.Rollback()
//		return errors.New(op).Err(err)
//	}
//	if rows == 0 {
//		_ = tx.Rollback()
//		return errors.New(op).Msg(errMsgInvalidId)
//	}
//
//	if err = tx.Commit(); err != nil {
//		_ = tx.Rollback()
//		return errors.New(op).Err(err)
//	}
//	return nil
//}
//
//// postgresUpdateQsoContext updates the QSO record in the PostgreSQL database using the provided context and QSO data.
//// Returns an error if the update fails, the database service is not open, or validation conditions are not met.
//func (s *Service) postgresUpdateQsoContext(ctx context.Context, qso types.Qso) error {
//	const op errors.Op = "database.Service.postgresUpdateQsoContext"
//	if err := checkService(op, s); err != nil {
//		return errors.New(op).Err(err)
//	}
//
//	s.mu.RLock()
//	h := s.handle
//	isOpen := s.isOpen.Load()
//	s.mu.RUnlock()
//
//	if h == nil || !isOpen {
//		return errors.New(op).Msg(errMsgNotOpen)
//	}
//
//	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
//		var cancel context.CancelFunc
//		ctx, cancel = s.withDefaultTimeout(ctx)
//		defer cancel()
//	}
//
//	tx, txCancel, err := s.BeginTxContext(ctx)
//	if err != nil {
//		return errors.New(op).Err(err)
//	}
//	defer txCancel()
//
//	s.initAdapters()
//
//	model, err := s.AdaptTypeToPostgresModelQso(qso)
//	if err != nil {
//		_ = tx.Rollback()
//		return errors.New(op).Err(err)
//	}
//
//	model.ID = qso.ID
//
//	rows, err := model.Update(ctx, tx, boil.Infer())
//	if err != nil {
//		_ = tx.Rollback()
//		return errors.New(op).Err(err)
//	}
//	if rows == 0 {
//		_ = tx.Rollback()
//		return errors.New(op).Msg(errMsgInvalidId)
//	}
//
//	if err = tx.Commit(); err != nil {
//		_ = tx.Rollback()
//		return errors.New(op).Err(err)
//	}
//	return nil
//}
//
///*********************************************************************************************************************
//Delete QSO Methods
//**********************************************************************************************************************/
//
//// DeleteQso delegates to DeleteQsoContext with a background context.
//func (s *Service) DeleteQso(id int64) error {
//	return s.DeleteQsoContext(context.Background(), id)
//}
//
//// DeleteQsoContext deletes a QSO with a caller-provided context.
//func (s *Service) DeleteQsoContext(ctx context.Context, id int64) error {
//	const op errors.Op = "database.Service.DeleteQsoContext"
//	if err := checkService(op, s); err != nil {
//		return errors.New(op).Err(err)
//	}
//	if id < 1 {
//		return errors.New(op).Msg(errMsgInvalidId)
//	}
//
//	s.mu.RLock()
//	h := s.handle
//	isOpen := s.isOpen.Load()
//	s.mu.RUnlock()
//	if h == nil || !isOpen {
//		return errors.New(op).Msg(errMsgNotOpen)
//	}
//
//	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
//		var cancel context.CancelFunc
//		ctx, cancel = s.withDefaultTimeout(ctx)
//		defer cancel()
//	}
//
//	tx, txCancel, err := s.BeginTxContext(ctx)
//	if err != nil {
//		return errors.New(op).Err(err)
//	}
//	defer txCancel()
//
//	switch s.DatabaseConfig.Driver {
//	case SqliteDriver:
//		if rows, err := sqmodels.Qsos(sqmodels.QsoWhere.ID.EQ(id)).DeleteAll(ctx, tx, false); err != nil {
//			_ = tx.Rollback()
//			return errors.New(op).Err(err)
//		} else if rows == 0 {
//			_ = tx.Rollback()
//			return errors.New(op).Msg(errMsgInvalidId)
//		}
//	case PostgresDriver:
//		if rows, err := pgmodels.Qsos(pgmodels.QsoWhere.ID.EQ(id)).DeleteAll(ctx, tx); err != nil {
//			_ = tx.Rollback()
//			return errors.New(op).Err(err)
//		} else if rows == 0 {
//			_ = tx.Rollback()
//			return errors.New(op).Msg(errMsgInvalidId)
//		}
//	default:
//		_ = tx.Rollback()
//		return errors.New(op).Errorf("Unsupported database driver: %s", s.DatabaseConfig.Driver)
//	}
//
//	if err = tx.Commit(); err != nil {
//		_ = tx.Rollback()
//		return errors.New(op).Err(err)
//	}
//	return nil
//}
//
///*********************************************************************************************************************
//History Methods
//**********************************************************************************************************************/
//
//// ContactHistory retrieves the contact history for the specified callsign from the database.
//// It returns a slice of types.ContactHistory or an error if the operation fails.
//func (s *Service) ContactHistory(callsign string) ([]types.ContactHistory, error) {
//	return s.ContactHistoryContext(context.Background(), callsign)
//}
//
//// ContactHistoryContext retrieves contact history for a given callsign using the specified database driver and context.
//// Returns a slice of types.ContactHistory or an error if the operation fails.
//func (s *Service) ContactHistoryContext(ctx context.Context, callsign string) ([]types.ContactHistory, error) {
//	const op errors.Op = "database.Service.ContactHistory"
//
//	callsign = strings.TrimSpace(callsign)
//	if len(callsign) < 3 {
//		return nil, errors.New(op).Msg("Callsign must be at least 3 characters long")
//	}
//
//	switch s.DatabaseConfig.Driver {
//	case SqliteDriver:
//		return s.sqliteContactHistoryContext(ctx, callsign)
//	case PostgresDriver:
//		return nil, errors.New(op).Msg("Not supported. Desktop application only.")
//	default:
//		return nil, errors.New(op).Errorf("Unsupported database driver: %s", s.DatabaseConfig.Driver)
//	}
//}
//
//// sqliteContactHistoryContext retrieves the contact history for a given callsign from the SQLite database in descending order.
//// The context is required and applies a default timeout if none is set by the caller.
//// Returns a slice of ContactHistory or an error if the service is not open or the query fails.
//func (s *Service) sqliteContactHistoryContext(ctx context.Context, callsign string) ([]types.ContactHistory, error) {
//	const op errors.Op = "database.Service.sqliteContactHistoryContext"
//	if err := checkService(op, s); err != nil {
//		return nil, errors.New(op).Err(err)
//	}
//
//	s.mu.RLock()
//	h := s.handle
//	isOpen := s.isOpen.Load()
//	s.mu.RUnlock()
//	if h == nil || !isOpen {
//		return nil, errors.New(op).Msg(errMsgNotOpen)
//	}
//
//	// Apply default timeout if caller did not set one
//	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
//		var cancel context.CancelFunc
//		ctx, cancel = s.withDefaultTimeout(ctx)
//		defer cancel()
//	}
//
//	// We want pattern matching here as we might have QSOs with prefixes and/or suffixes.
//	callsign = fmt.Sprintf("%%%s%%", callsign)
//
//	var mods []qm.QueryMod
//	mods = append(mods, sqmodels.QsoWhere.Call.LIKE(callsign))
//	mods = append(mods, qm.OrderBy(sqmodels.QsoColumns.CreatedAt+" DESC"))
//	slice, err := sqmodels.Qsos(mods...).All(ctx, h)
//	if err != nil && !stderr.Is(err, sql.ErrNoRows) {
//		return nil, errors.New(op).Err(err).Msg("Failed to fetch contact history.")
//	}
//
//	if err != nil || slice == nil { // sql.ErrNoRows
//		return nil, errors.ErrNotFound
//	}
//	s.initAdapters()
//	adapter := s.adapterFromModel
//
//	history := make([]types.ContactHistory, 0, len(slice))
//	for _, qsoModel := range slice {
//		qso := types.Qso{}
//		if err = adapter.Into(&qso, qsoModel); err != nil {
//			return nil, errors.New(op).Err(err).Msg("Failed to adapt QSO for contact history.")
//		}
//		history = append(history, copyToHistory(qso))
//	}
//
//	return history, nil
//}
//
//// copyToHistory creates a ContactHistory instance from a Qso object by mapping relevant fields.
//func copyToHistory(qso types.Qso) types.ContactHistory {
//	return types.ContactHistory{
//		ID:      qso.ID,
//		Band:    qso.Band,
//		Freq:    qso.Freq,
//		Mode:    qso.Mode,
//		QsoDate: qso.QsoDate,
//		TimeOn:  qso.TimeOn,
//		Name:    qso.Name,
//		Country: qso.Country,
//		Call:    qso.Call,
//		RstRcvd: qso.RstRcvd,
//		RstSent: qso.RstSent,
//		Notes:   qso.Notes,
//	}
//}
//
///*********************************************************************************************************************
//Misc QSO Methods
//**********************************************************************************************************************/
//
//func (s *Service) QsoCountByLogbookId(logbookId int64) (int64, error) {
//	return s.QsoCountByLogbookIdContext(context.Background(), logbookId)
//}
//
//func (s *Service) QsoCountByLogbookIdContext(ctx context.Context, logbookId int64) (int64, error) {
//	const op errors.Op = "database.Service.QsoCountByLogbookIdContext"
//
//	switch s.DatabaseConfig.Driver {
//	case SqliteDriver:
//		return s.sqliteQsoCountByLogbookIdContext(ctx, logbookId)
//	case PostgresDriver:
//		return 0, errors.New(op).Msg("Not supported. Desktop application only.")
//	default:
//		return 0, errors.New(op).Errorf("Unsupported database driver: %s", s.DatabaseConfig.Driver)
//	}
//}
//
//func (s *Service) sqliteQsoCountByLogbookIdContext(ctx context.Context, logbookId int64) (int64, error) {
//	const op errors.Op = "database.Service.sqliteQsoCountByLogbookIdContext"
//	if err := checkService(op, s); err != nil {
//		return 0, errors.New(op).Err(err)
//	}
//
//	s.mu.RLock()
//	h := s.handle
//	isOpen := s.isOpen.Load()
//	s.mu.RUnlock()
//	if h == nil || !isOpen {
//		return 0, errors.New(op).Msg(errMsgNotOpen)
//	}
//
//	// Apply default timeout if caller did not set one
//	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
//		var cancel context.CancelFunc
//		ctx, cancel = s.withDefaultTimeout(ctx)
//		defer cancel()
//	}
//
//	count, err := sqmodels.Qsos(sqmodels.QsoWhere.LogbookID.EQ(logbookId)).Count(ctx, h)
//	if err != nil {
//		return 0, errors.New(op).Err(err).Msg("models.Qsos(...).Count")
//	}
//
//	return count, nil
//}
