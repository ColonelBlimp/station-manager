package qrz

import (
	"database/sql"
	"testing"
)

// dummyDB implements database.Database for testing pointer-to-interface injection
type dummyDB struct{}

func (d *dummyDB) Open() error                        { return nil }
func (d *dummyDB) IsOpen() bool                       { return false }
func (d *dummyDB) Close() error                       { return nil }
func (d *dummyDB) Conn() *sql.DB                      { return nil }
func (d *dummyDB) BeginTransaction() (*sql.Tx, error) { return nil, nil }

func TestInitialise_WithInjectedServices_EmptyNameError(t *testing.T) {
	//	// Build a non-nil pointer to interface value for DatabaseService
	////	var dbIface database.Database = &dummyDB{}
	//	c := &Service{
	//		LoggerService:   &logging.Service{},
	//		ConfigService:   &config.Service{},
	////		DatabaseService: &dbIface,
	//		Config:          &types.ForwarderConfig{Name: ""},
	//	}
	//	if err := c.Initialise(); err == nil || !strings.Contains(err.Error(), "name parameter cannot be empty") {
	//		t.Fatalf("expected empty name error, got %v", err)
	//	}
}

func TestInitialise_WithInjectedServices_ConfigNotFound(t *testing.T) {
	//var dbIface database.Database = &dummyDB{}
	//c := &Client{
	//	LoggerService:   &logging.Service{},
	//	ConfigService:   &config.Service{},
	//	DatabaseService: &dbIface,
	//	Config:          &types.ForwarderConfig{Name: "qrz"},
	//}
	//if err := c.Initialise(); err == nil || !strings.Contains(err.Error(), "forwarder config not found") {
	//	t.Fatalf("expected config not found error, got %v", err)
	//}
}
