package config

import (
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/utils"
	"github.com/goccy/go-json"
	"os"
	"path/filepath"
	"strings"
)

func (s *Service) loadConfigFile() error {
	const op errors.Op = "config.Service.loadConfigFile"

	filePath := filepath.Join(s.WorkingDir, configFileName)
	exists, err := utils.PathExists(filePath)
	if err != nil {
		return errors.New(op).Err(err)
	}

	if !exists {
		if err = s.generateDefaultConfig(); err != nil {
			return errors.New(op).Err(err)
		}
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return errors.New(op).Err(err)
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
		if dbSel == "postgres" || dbSel == "postgresql" || dbSel == "pg" {
			selected = defaultServerConfig
		}
	}

	// Pretty-print selected configuration for readability
	data, err := json.MarshalIndent(selected, "", "  ")
	if err != nil {
		return errors.New(op).Err(err)
	}

	return writeDataToFile(data, filepath.Join(s.WorkingDir, configFileName))
}
