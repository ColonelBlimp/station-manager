package database

import (
	"github.com/Station-Manager/adapters"
	pgmodels "github.com/Station-Manager/database/postgres/models"
	sqmodels "github.com/Station-Manager/database/sqlite/models"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
)

// checkService checks if the database service is not nil, has been initialized and is open.
func checkService(op errors.Op, s *Service) error {
	if s == nil {
		return errors.New(op).Msg(errMsgNilService)
	}

	if !s.isInitialized.Load() {
		return errors.New(op).Msg(errMsgNotInitialized)
	}

	if !s.isOpen.Load() {
		return errors.New(op).Msg(errMsgNotOpen)
	}
	return nil
}

// AdaptTypeToSqliteModelQso adapts a types.Qso into a sqlite model Qso.
func (s *Service) AdaptTypeToSqliteModelQso(q types.Qso) (*sqmodels.Qso, error) {
	if s == nil || s.adapterToModel == nil {
		return nil, errors.New("adapt").Msg("service or adapter nil")
	}
	m, err := adapters.AdaptTo[sqmodels.Qso](s.adapterToModel, &q)
	return m, err
}

// AdaptTypeToPostgresModelQso adapts a types.Qso into a postgres model Qso.
func (s *Service) AdaptTypeToPostgresModelQso(q types.Qso) (*pgmodels.Qso, error) {
	if s == nil || s.adapterToModel == nil {
		return nil, errors.New("adapt").Msg("service or adapter nil")
	}
	m, err := adapters.AdaptTo[pgmodels.Qso](s.adapterToModel, &q)
	return m, err
}

// AdaptSqliteModelToTypeQso adapts sqlite model to types.Qso.
func (s *Service) AdaptSqliteModelToTypeQso(m *sqmodels.Qso) (types.Qso, error) {
	if s == nil || s.adapterFromModel == nil {
		return types.Qso{}, errors.New("adapt").Msg("service or adapter nil")
	}
	out, err := adapters.Make[types.Qso](s.adapterFromModel, m)
	return out, err
}

// AdaptPostgresModelToTypeQso adapts postgres model to types.Qso.
func (s *Service) AdaptPostgresModelToTypeQso(m *pgmodels.Qso) (types.Qso, error) {
	if s == nil || s.adapterFromModel == nil {
		return types.Qso{}, errors.New("adapt").Msg("service or adapter nil")
	}
	out, err := adapters.Make[types.Qso](s.adapterFromModel, m)
	return out, err
}

// AdaptTypeToSqliteModelLogbook adapts a types.Logbook into a sqlite model Logbook.
func (s *Service) AdaptTypeToSqliteModelLogbook(lb types.Logbook) (*sqmodels.Logbook, error) {
	if s == nil || s.adapterToModel == nil {
		return nil, errors.New("adapt").Msg("service or adapter nil")
	}
	m, err := adapters.AdaptTo[sqmodels.Logbook](s.adapterToModel, &lb)
	return m, err
}

// AdaptTypeToPostgresModelLogbook adapts a types.Logbook into a postgres model Logbook.
func (s *Service) AdaptTypeToPostgresModelLogbook(lb types.Logbook) (*pgmodels.Logbook, error) {
	if s == nil || s.adapterToModel == nil {
		return nil, errors.New("adapt").Msg("service or adapter nil")
	}
	m, err := adapters.AdaptTo[pgmodels.Logbook](s.adapterToModel, &lb)
	return m, err
}

// AdaptSqliteModelToTypeLogbook adapts sqlite model Logbook to types.Logbook.
func (s *Service) AdaptSqliteModelToTypeLogbook(m *sqmodels.Logbook) (types.Logbook, error) {
	if s == nil || s.adapterFromModel == nil {
		return types.Logbook{}, errors.New("adapt").Msg("service or adapter nil")
	}
	out, err := adapters.Make[types.Logbook](s.adapterFromModel, m)
	return out, err
}

// AdaptPostgresModelToTypeLogbook adapts postgres model Logbook to types.Logbook.
func (s *Service) AdaptPostgresModelToTypeLogbook(m *pgmodels.Logbook) (types.Logbook, error) {
	if s == nil || s.adapterFromModel == nil {
		return types.Logbook{}, errors.New("adapt").Msg("service or adapter nil")
	}
	out, err := adapters.Make[types.Logbook](s.adapterFromModel, m)
	return out, err
}

func (s *Service) AdaptPostgresModelToTypeUser(m *pgmodels.User) (types.User, error) {
	if s == nil || s.adapterFromModel == nil {
		return types.User{}, errors.New("adapt").Msg("service or adapter nil")
	}
	out, err := adapters.Make[types.User](s.adapterFromModel, m)
	return out, err
}

func (s *Service) AdaptTypeToPostgresModelUser(u types.User) (*pgmodels.User, error) {
	if s == nil || s.adapterToModel == nil {
		return nil, errors.New("adapt").Msg("service or adapter nil")
	}
	m, err := adapters.AdaptTo[pgmodels.User](s.adapterToModel, &u)
	return m, err
}

// AdaptSqliteModelToTypeContactedStation converts a ContactedStation SQLite model to its corresponding domain type.
// Returns the converted types.ContactedStation and an error if the service or adapter is nil, or if the conversion fails.
func (s *Service) AdaptSqliteModelToTypeContactedStation(m *sqmodels.ContactedStation) (types.ContactedStation, error) {
	if s == nil || s.adapterFromModel == nil {
		return types.ContactedStation{}, errors.New("adapt").Msg("service or adapter nil")
	}
	out, err := adapters.Make[types.ContactedStation](s.adapterFromModel, m)
	return out, err
}

// AdaptTypeToSqliteModelContactedStation converts a types.ContactedStation object to its corresponding SQLite model representation.
// Returns the SQLite model instance and any error encountered during the adaptation process.
// If the service or its adapter is nil, it returns an error indicating the issue.
func (s *Service) AdaptTypeToSqliteModelContactedStation(u types.ContactedStation) (*sqmodels.ContactedStation, error) {
	if s == nil || s.adapterToModel == nil {
		return nil, errors.New("adapt").Msg("service or adapter nil")
	}
	m, err := adapters.AdaptTo[sqmodels.ContactedStation](s.adapterToModel, &u)
	return m, err
}
