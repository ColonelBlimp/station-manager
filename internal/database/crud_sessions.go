package database

import (
	"context"
	sqmodels "github.com/Station-Manager/database/sqlite/models"
	"github.com/Station-Manager/errors"
	"github.com/aarondl/sqlboiler/v4/boil"
)

func (s *Service) GenerateNewSessionID() (int64, error) {
	const op errors.Op = "database.Service.InsertNewSessionID"
	if err := checkService(op, s); err != nil {
		return 0, errors.New(op).Err(err)
	}

	switch s.DatabaseConfig.Driver {
	case SqliteDriver:
		return s.sqliteInsertSessionIdContext(context.Background())
	case PostgresDriver:
		return 0, errors.New(op).Msg("Not supported. Desktop application only.")
	default:
		return 0, errors.New(op).Errorf("Unsupported database driver: %s", s.DatabaseConfig.Driver)
	}
}

func (s *Service) sqliteInsertSessionIdContext(ctx context.Context) (int64, error) {
	const op errors.Op = "database.Service.sqliteInsertSessionIdContext"
	if err := checkService(op, s); err != nil {
		return 0, errors.New(op).Err(err)
	}

	s.mu.RLock()
	h := s.handle
	isOpen := s.isOpen.Load()
	s.mu.RUnlock()
	if h == nil || !isOpen {
		return 0, errors.New(op).Msg(errMsgNotOpen)
	}

	// Apply default timeout if caller did not set one
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = s.withDefaultTimeout(ctx)
		defer cancel()
	}

	session := sqmodels.Session{}
	if err := session.Insert(ctx, h, boil.Infer()); err != nil {
		return 0, errors.New(op).Err(err)
	}

	return session.ID, nil
}

func (s *Service) SoftDeleteSessionID(id int64) error {
	const op errors.Op = "database.Service.SoftDeleteSessionID"
	if err := checkService(op, s); err != nil {
		return errors.New(op).Err(err)
	}

	switch s.DatabaseConfig.Driver {
	case SqliteDriver:
		return s.sqliteSoftDeleteSessionIDContext(context.Background(), id)
	case PostgresDriver:
		return errors.New(op).Msg("Not supported. Desktop application only.")
	default:
		return errors.New(op).Errorf("Unsupported database driver: %s", s.DatabaseConfig.Driver)
	}
}

func (s *Service) sqliteSoftDeleteSessionIDContext(ctx context.Context, id int64) error {
	const op errors.Op = "database.Service.sqliteSoftDeleteSessionIDContext"
	if err := checkService(op, s); err != nil {
		return errors.New(op).Err(err)
	}

	s.mu.RLock()
	h := s.handle
	isOpen := s.isOpen.Load()
	s.mu.RUnlock()
	if h == nil || !isOpen {
		return errors.New(op).Msg(errMsgNotOpen)
	}

	// Apply default timeout if caller did not set one
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = s.withDefaultTimeout(ctx)
		defer cancel()
	}

	session, err := sqmodels.FindSession(ctx, h, id)
	if err != nil { // Any error is a problem
		return errors.New(op).Err(err)
	}

	if _, err = session.Delete(ctx, h, false); err != nil {
		return errors.New(op).Err(err)
	}

	return nil
}
