package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/goccy/go-json"
)

func (s *Service) loadConfigFile() error {
	const op errors.Op = "config.Service.loadConfigFile"

	filePath := filepath.Join(s.WorkingDir, configFileName)

	data, err := os.ReadFile(filePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return errors.New(op).Err(err)
		}
		// Config file does not exist; generate and write the default.
		if err = s.generateDefaultConfig(); err != nil {
			return errors.New(op).Err(err)
		}
		data, err = os.ReadFile(filePath)
		if err != nil {
			return errors.New(op).Err(err)
		}
	}

	if err = json.Unmarshal(data, &s.AppConfig); err != nil {
		return errors.New(op).Err(err)
	}

	return nil
}

func (s *Service) generateDefaultConfig() error {
	const op errors.Op = "config.Service.generateDefaultConfig"

	// Decide which datastore config to embed based on env
	selected := defaultDesktopConfig // start with sqlite default
	if dbSel := strings.ToLower(strings.TrimSpace(os.Getenv(EnvSmDefaultDB))); dbSel != "" {
		switch dbSel {
		case "postgres", "postgresql", "pg":
			selected = defaultServerConfig
		case "sqlite":
			// explicit sqlite: keep desktop default
		default:
			return errors.New(op).Msgf("unsupported %s value %q; accepted: sqlite, postgres, postgresql, pg",
				EnvSmDefaultDB, dbSel)
		}
	}

	// Pretty-print selected configuration for readability
	data, err := json.MarshalIndent(selected, "", "  ")
	if err != nil {
		return errors.New(op).Err(err)
	}

	return writeDataToFile(data, filepath.Join(s.WorkingDir, configFileName))
}
