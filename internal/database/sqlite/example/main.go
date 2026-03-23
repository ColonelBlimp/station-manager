package main

import (
	"encoding/json"
	"github.com/Station-Manager/config"
	"github.com/Station-Manager/database"
	"github.com/Station-Manager/database/sqlite"
	"github.com/Station-Manager/logging"
	"github.com/Station-Manager/types"
	"os"
	"path/filepath"
)

func main() {
	// Use a temp working dir and in-memory style settings for speed
	tmp, _ := os.MkdirTemp("", "sm-sqlite-example-")
	defer func() { _ = os.RemoveAll(tmp) }()

	cfg := types.AppConfig{
		DatastoreConfig: types.DatastoreConfig{
			Driver:                    database.SqliteDriver,
			Path:                      filepath.Join(tmp, "example.db"),
			Options:                   map[string]string{"_foreign_keys": "on", "_journal_mode": "WAL", "_busy_timeout": "2000"},
			MaxOpenConns:              1,
			MaxIdleConns:              1,
			ConnMaxLifetime:           1,
			ConnMaxIdleTime:           1,
			ContextTimeout:            5,
			TransactionContextTimeout: 5,
		},
		LoggingConfig: types.LoggingConfig{
			Level:                  "debug",
			WithTimestamp:          false,
			ConsoleLogging:         true,
			FileLogging:            false,
			RelLogFileDir:          "logs",
			ShutdownTimeoutMS:      100,
			ShutdownTimeoutWarning: false,
		},
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	_ = os.WriteFile(filepath.Join(tmp, "config.json"), b, 0o640)

	cfgService := &config.Service{WorkingDir: tmp}
	if err := cfgService.Initialize(); err != nil {
		panic(err)
	}

	logService := &logging.Service{ConfigService: cfgService}
	if err := logService.Initialize(); err != nil {
		panic(err)
	}
	defer func() { _ = logService.Close() }()

	dbService := sqlite.Service{ConfigService: cfgService, LoggerService: logService}
	if err := dbService.Initialize(); err != nil {
		panic(err)
	}
	if err := dbService.Open(); err != nil {
		panic(err)
	}
	if err := dbService.Migrate(); err != nil {
		panic(err)
	}
	if err := dbService.Close(); err != nil {
		panic(err)
	}
}
