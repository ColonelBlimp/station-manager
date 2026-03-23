package facade

import (
	"context"
	"database/sql"

	"github.com/Station-Manager/types"
)

// DatabaseServiceInterface defines the interface for database operations used by the facade.
// This allows for mocking in tests.
type DatabaseServiceInterface interface {
	Open() error
	Close() error
	Migrate() error
	FetchLogbookByID(id int64) (types.Logbook, error)
	GenerateSession() (int64, error)
	InsertQso(qso types.Qso) (int64, error)
	UpdateQso(qso types.Qso) error
	FetchQsoById(id int64) (types.Qso, error)
	FetchQsoSliceBySessionID(sessionID int64) ([]types.Qso, error)
	FetchQsoSliceByCallsign(callsign string) ([]types.ContactHistory, error)
	FetchQsoCountByLogbookId(logbookId int64) (int64, error)
	InsertQsoUpload(qsoId int64, action interface{}, service interface{}) error
	UpdateQsoUploadStatus(id int64, status interface{}, action interface{}, attempts int, errState string) error
	FetchPendingUploads() ([]types.QsoUpload, error)
	FetchContactedStationByCallsign(callsign string) (types.ContactedStation, error)
	InsertContactedStation(station types.ContactedStation) (int64, error)
	UpdateContactedStation(station types.ContactedStation) error
	FetchCountryByCallsign(callsign string) (types.Country, error)
	FetchCountryByName(name string) (types.Country, error)
	InsertCountry(country types.Country) (int64, error)
	UpdateCountry(country types.Country) error
	IsContestDuplicateByLogbookID(logbookId int64, callsign string, band string) (bool, error)
	SoftDeleteSessionByID(sessionID int64) error
	BeginTxContext(ctx context.Context) (*sql.Tx, context.CancelFunc, error)
}

// ConfigServiceInterface defines the interface for configuration operations.
type ConfigServiceInterface interface {
	RequiredConfigs() (types.RequiredConfigs, error)
	LoggingStationConfigs() (types.LoggingStation, error)
	CatStateValues() (map[string]map[string]string, error)
	ForwarderConfigs() ([]types.ForwarderConfig, error)
}

// CatServiceInterface defines the interface for CAT service operations.
type CatServiceInterface interface {
	Start() error
	Stop() error
	EnqueueCommand(cmd interface{}) error
	StatusChannel() (<-chan map[string]string, error)
	RigConfig() types.RigConfig
}

// LookupServiceInterface defines the interface for callsign lookup services.
type LookupServiceInterface interface {
	Lookup(callsign string) (types.ContactedStation, error)
}

// CountryLookupServiceInterface defines the interface for country lookup services.
type CountryLookupServiceInterface interface {
	Lookup(callsign string) (types.Country, error)
}

// EmailServiceInterface defines the interface for email operations.
type EmailServiceInterface interface {
	BuildEmailWithADIFAttachment(subject, body, from string, to []string, qsos []types.Qso) (interface{}, error)
	Send(mail interface{}) error
}
