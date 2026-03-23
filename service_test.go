package config

import (
	"github.com/Station-Manager/types"
	"os"
	"path/filepath"
	"testing"
)

// TestInitialize_createsDefaultConfig ensures Initialize writes a default config and populates fields.
func TestInitialize_createsDefaultConfig(t *testing.T) {
	t.TempDir() // ensure testing.T has cleanup
	workDir := t.TempDir()

	svc := &Service{WorkingDir: workDir}
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	// Verify config.json exists
	cfgPath := filepath.Join(workDir, configFileName)
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected %s to exist, got error: %v", cfgPath, err)
	}

	// Verify getters work and initialized
	dbCfg, err := svc.DatastoreConfig()
	if err != nil {
		t.Fatalf("DatastoreConfig() error = %v", err)
	}
	if dbCfg.Driver != types.SqliteDriverName {
		t.Errorf("expected default driver %q, got %q", types.SqliteDriverName, dbCfg.Driver)
	}

	logCfg, err := svc.LoggingConfig()
	if err != nil {
		t.Fatalf("LoggingConfig() error = %v", err)
	}
	if logCfg.Level == "" {
		t.Errorf("expected logging level to be set, got empty")
	}
}

// TestInitialize_idempotent ensures multiple Initialize calls are safe and do not error.
func TestInitialize_idempotent(t *testing.T) {
	workDir := t.TempDir()
	svc := &Service{WorkingDir: workDir}

	if err := svc.Initialize(); err != nil {
		t.Fatalf("first Initialize() error = %v", err)
	}
	if err := svc.Initialize(); err != nil { // should be a no-op
		t.Fatalf("second Initialize() error = %v", err)
	}
}

// TestGetters_notInitialized ensures getters fail when service not initialized.
func TestGetters_notInitialized(t *testing.T) {
	svc := &Service{}
	if _, err := svc.DatastoreConfig(); err == nil {
		t.Errorf("expected error when not initialized for DatastoreConfig()")
	}
	if _, err := svc.LoggingConfig(); err == nil {
		t.Errorf("expected error when not initialized for LoggingConfig()")
	}
}

func TestInitialize_envSelectsSqlite(t *testing.T) {
	workDir := t.TempDir()
	// Ensure file does not exist and env selects sqlite
	_ = os.Unsetenv(EnvSmDefaultDB)
	if err := os.Setenv(EnvSmDefaultDB, "sqlite"); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv(EnvSmDefaultDB) })

	svc := &Service{WorkingDir: workDir}
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	cfgPath := filepath.Join(workDir, configFileName)
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected %s to exist, got error: %v", cfgPath, err)
	}

	dbCfg, err := svc.DatastoreConfig()
	if err != nil {
		t.Fatalf("DatastoreConfig() error = %v", err)
	}
	if dbCfg.Driver != types.SqliteDriverName {
		t.Errorf("expected driver %q, got %q", types.SqliteDriverName, dbCfg.Driver)
	}
}

func TestInitialize_envSelectsPostgres(t *testing.T) {
	workDir := t.TempDir()
	_ = os.Unsetenv(EnvSmDefaultDB)
	if err := os.Setenv(EnvSmDefaultDB, "postgres"); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv(EnvSmDefaultDB) })

	svc := &Service{WorkingDir: workDir}
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	dbCfg, err := svc.DatastoreConfig()
	if err != nil {
		t.Fatalf("DatastoreConfig() error = %v", err)
	}
	if dbCfg.Driver != types.PostgresDriverName {
		t.Errorf("expected driver %q, got %q", types.PostgresDriverName, dbCfg.Driver)
	}
}
