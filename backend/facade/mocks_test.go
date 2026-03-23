package facade

import (
	"context"
	"database/sql"

	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
)

// MockDatabaseService is a mock implementation of DatabaseServiceInterface for testing.
type MockDatabaseService struct {
	OpenFunc                            func() error
	CloseFunc                           func() error
	MigrateFunc                         func() error
	FetchLogbookByIDFunc                func(id int64) (types.Logbook, error)
	GenerateSessionFunc                 func() (int64, error)
	InsertQsoFunc                       func(qso types.Qso) (int64, error)
	UpdateQsoFunc                       func(qso types.Qso) error
	FetchQsoByIdFunc                    func(id int64) (types.Qso, error)
	FetchQsoSliceBySessionIDFunc        func(sessionID int64) ([]types.Qso, error)
	FetchQsoSliceByCallsignFunc         func(callsign string) ([]types.ContactHistory, error)
	FetchQsoCountByLogbookIdFunc        func(logbookId int64) (int64, error)
	InsertQsoUploadFunc                 func(qsoId int64, action interface{}, service interface{}) error
	UpdateQsoUploadStatusFunc           func(id int64, status interface{}, action interface{}, attempts int, errState string) error
	FetchPendingUploadsFunc             func() ([]types.QsoUpload, error)
	FetchContactedStationByCallsignFunc func(callsign string) (types.ContactedStation, error)
	InsertContactedStationFunc          func(station types.ContactedStation) (int64, error)
	UpdateContactedStationFunc          func(station types.ContactedStation) error
	FetchCountryByCallsignFunc          func(callsign string) (types.Country, error)
	FetchCountryByNameFunc              func(name string) (types.Country, error)
	InsertCountryFunc                   func(country types.Country) (int64, error)
	UpdateCountryFunc                   func(country types.Country) error
	IsContestDuplicateByLogbookIDFunc   func(logbookId int64, callsign string, band string) (bool, error)
	SoftDeleteSessionByIDFunc           func(sessionID int64) error
	BeginTxContextFunc                  func(ctx context.Context) (*sql.Tx, context.CancelFunc, error)
}

func (m *MockDatabaseService) Open() error {
	if m.OpenFunc != nil {
		return m.OpenFunc()
	}
	return nil
}

func (m *MockDatabaseService) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

func (m *MockDatabaseService) Migrate() error {
	if m.MigrateFunc != nil {
		return m.MigrateFunc()
	}
	return nil
}

func (m *MockDatabaseService) FetchLogbookByID(id int64) (types.Logbook, error) {
	if m.FetchLogbookByIDFunc != nil {
		return m.FetchLogbookByIDFunc(id)
	}
	return types.Logbook{ID: id, Callsign: "W1AW", Name: "Test Logbook"}, nil
}

func (m *MockDatabaseService) GenerateSession() (int64, error) {
	if m.GenerateSessionFunc != nil {
		return m.GenerateSessionFunc()
	}
	return 12345, nil
}

func (m *MockDatabaseService) InsertQso(qso types.Qso) (int64, error) {
	if m.InsertQsoFunc != nil {
		return m.InsertQsoFunc(qso)
	}
	return 1, nil
}

func (m *MockDatabaseService) UpdateQso(qso types.Qso) error {
	if m.UpdateQsoFunc != nil {
		return m.UpdateQsoFunc(qso)
	}
	return nil
}

func (m *MockDatabaseService) FetchQsoById(id int64) (types.Qso, error) {
	if m.FetchQsoByIdFunc != nil {
		return m.FetchQsoByIdFunc(id)
	}
	return types.Qso{ID: id}, nil
}

func (m *MockDatabaseService) FetchQsoSliceBySessionID(sessionID int64) ([]types.Qso, error) {
	if m.FetchQsoSliceBySessionIDFunc != nil {
		return m.FetchQsoSliceBySessionIDFunc(sessionID)
	}
	return []types.Qso{}, nil
}

func (m *MockDatabaseService) FetchQsoSliceByCallsign(callsign string) ([]types.ContactHistory, error) {
	if m.FetchQsoSliceByCallsignFunc != nil {
		return m.FetchQsoSliceByCallsignFunc(callsign)
	}
	return []types.ContactHistory{}, nil
}

func (m *MockDatabaseService) FetchQsoCountByLogbookId(logbookId int64) (int64, error) {
	if m.FetchQsoCountByLogbookIdFunc != nil {
		return m.FetchQsoCountByLogbookIdFunc(logbookId)
	}
	return 0, nil
}

func (m *MockDatabaseService) InsertQsoUpload(qsoId int64, action interface{}, service interface{}) error {
	if m.InsertQsoUploadFunc != nil {
		return m.InsertQsoUploadFunc(qsoId, action, service)
	}
	return nil
}

func (m *MockDatabaseService) UpdateQsoUploadStatus(id int64, status interface{}, action interface{}, attempts int, errState string) error {
	if m.UpdateQsoUploadStatusFunc != nil {
		return m.UpdateQsoUploadStatusFunc(id, status, action, attempts, errState)
	}
	return nil
}

func (m *MockDatabaseService) FetchPendingUploads() ([]types.QsoUpload, error) {
	if m.FetchPendingUploadsFunc != nil {
		return m.FetchPendingUploadsFunc()
	}
	return []types.QsoUpload{}, nil
}

func (m *MockDatabaseService) FetchContactedStationByCallsign(callsign string) (types.ContactedStation, error) {
	if m.FetchContactedStationByCallsignFunc != nil {
		return m.FetchContactedStationByCallsignFunc(callsign)
	}
	return types.ContactedStation{}, errors.ErrNotFound
}

func (m *MockDatabaseService) InsertContactedStation(station types.ContactedStation) (int64, error) {
	if m.InsertContactedStationFunc != nil {
		return m.InsertContactedStationFunc(station)
	}
	return 1, nil
}

func (m *MockDatabaseService) UpdateContactedStation(station types.ContactedStation) error {
	if m.UpdateContactedStationFunc != nil {
		return m.UpdateContactedStationFunc(station)
	}
	return nil
}

func (m *MockDatabaseService) FetchCountryByCallsign(callsign string) (types.Country, error) {
	if m.FetchCountryByCallsignFunc != nil {
		return m.FetchCountryByCallsignFunc(callsign)
	}
	return types.Country{}, errors.ErrNotFound
}

func (m *MockDatabaseService) FetchCountryByName(name string) (types.Country, error) {
	if m.FetchCountryByNameFunc != nil {
		return m.FetchCountryByNameFunc(name)
	}
	return types.Country{}, errors.ErrNotFound
}

func (m *MockDatabaseService) InsertCountry(country types.Country) (int64, error) {
	if m.InsertCountryFunc != nil {
		return m.InsertCountryFunc(country)
	}
	return 1, nil
}

func (m *MockDatabaseService) UpdateCountry(country types.Country) error {
	if m.UpdateCountryFunc != nil {
		return m.UpdateCountryFunc(country)
	}
	return nil
}

func (m *MockDatabaseService) IsContestDuplicateByLogbookID(logbookId int64, callsign string, band string) (bool, error) {
	if m.IsContestDuplicateByLogbookIDFunc != nil {
		return m.IsContestDuplicateByLogbookIDFunc(logbookId, callsign, band)
	}
	return false, nil
}

func (m *MockDatabaseService) SoftDeleteSessionByID(sessionID int64) error {
	if m.SoftDeleteSessionByIDFunc != nil {
		return m.SoftDeleteSessionByIDFunc(sessionID)
	}
	return nil
}

func (m *MockDatabaseService) BeginTxContext(ctx context.Context) (*sql.Tx, context.CancelFunc, error) {
	if m.BeginTxContextFunc != nil {
		return m.BeginTxContextFunc(ctx)
	}
	return nil, func() {}, nil
}

// MockConfigService is a mock implementation of ConfigServiceInterface for testing.
type MockConfigService struct {
	RequiredConfigsFunc       func() (types.RequiredConfigs, error)
	LoggingStationConfigsFunc func() (types.LoggingStation, error)
	CatStateValuesFunc        func() (map[string]map[string]string, error)
	ForwarderConfigsFunc      func() ([]types.ForwarderConfig, error)
}

func (m *MockConfigService) RequiredConfigs() (types.RequiredConfigs, error) {
	if m.RequiredConfigsFunc != nil {
		return m.RequiredConfigsFunc()
	}
	return types.RequiredConfigs{
		DefaultLogbookID:                 1,
		QsoForwardingPollIntervalSeconds: 120,
		QsoForwardingWorkerCount:         5,
		QsoForwardingQueueSize:           100,
		DatabaseWriteQueueSize:           100,
	}, nil
}

func (m *MockConfigService) LoggingStationConfigs() (types.LoggingStation, error) {
	if m.LoggingStationConfigsFunc != nil {
		return m.LoggingStationConfigsFunc()
	}
	return types.LoggingStation{
		StationCallsign: "W1AW",
		OwnerCallsign:   "W1AW",
		MyGridsquare:    "FN31pr",
	}, nil
}

func (m *MockConfigService) CatStateValues() (map[string]map[string]string, error) {
	if m.CatStateValuesFunc != nil {
		return m.CatStateValuesFunc()
	}
	return map[string]map[string]string{}, nil
}

func (m *MockConfigService) ForwarderConfigs() ([]types.ForwarderConfig, error) {
	if m.ForwarderConfigsFunc != nil {
		return m.ForwarderConfigsFunc()
	}
	return []types.ForwarderConfig{}, nil
}

// MockCatService is a mock implementation of CatServiceInterface for testing.
type MockCatService struct {
	StartFunc          func() error
	StopFunc           func() error
	EnqueueCommandFunc func(cmd interface{}) error
	StatusChannelFunc  func() (<-chan map[string]string, error)
	RigConfigFunc      func() types.RigConfig
}

func (m *MockCatService) Start() error {
	if m.StartFunc != nil {
		return m.StartFunc()
	}
	return nil
}

func (m *MockCatService) Stop() error {
	if m.StopFunc != nil {
		return m.StopFunc()
	}
	return nil
}

func (m *MockCatService) EnqueueCommand(cmd interface{}) error {
	if m.EnqueueCommandFunc != nil {
		return m.EnqueueCommandFunc(cmd)
	}
	return nil
}

func (m *MockCatService) StatusChannel() (<-chan map[string]string, error) {
	if m.StatusChannelFunc != nil {
		return m.StatusChannelFunc()
	}
	ch := make(chan map[string]string)
	return ch, nil
}

func (m *MockCatService) RigConfig() types.RigConfig {
	if m.RigConfigFunc != nil {
		return m.RigConfigFunc()
	}
	return types.RigConfig{Name: "Test Rig"}
}

// MockLookupService is a mock implementation of LookupServiceInterface for testing.
type MockLookupService struct {
	LookupFunc func(callsign string) (types.ContactedStation, error)
}

func (m *MockLookupService) Lookup(callsign string) (types.ContactedStation, error) {
	if m.LookupFunc != nil {
		return m.LookupFunc(callsign)
	}
	return types.ContactedStation{
		Call:       callsign,
		Gridsquare: "FN31pr",
		Country:    "United States",
	}, nil
}

// MockCountryLookupService is a mock implementation of CountryLookupServiceInterface for testing.
type MockCountryLookupService struct {
	LookupFunc func(callsign string) (types.Country, error)
}

func (m *MockCountryLookupService) Lookup(callsign string) (types.Country, error) {
	if m.LookupFunc != nil {
		return m.LookupFunc(callsign)
	}
	return types.Country{
		Name:      "United States",
		Continent: "NA",
	}, nil
}
