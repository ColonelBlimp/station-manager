package utils

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	// EnvSmWorkingDir is the internal constant name for the environment variable name used to specify
	// the working directory.
	EnvSmWorkingDir = "SM_WORKING_DIR"
)

// WorkingDir determines the working directory path, prioritizing function argument, environment variable, or executable location.
// It validates the directory exists and returns an absolute path or an error if validation fails.
func WorkingDir(workingDir ...string) (string, error) {
	var err error
	var workDir string

	if len(workingDir) > 0 {
		workDir = workingDir[0]
	} else {
		if envDir := os.Getenv(EnvSmWorkingDir); envDir != emptyString {
			workDir = envDir
		} else if workDir, err = AbsDirPathForExecutable(); err != nil {
			return emptyString, fmt.Errorf("failed to get executable directory: %w", err)
		}
	}

	if workDir, err = filepath.Abs(workDir); err != nil {
		return emptyString, fmt.Errorf("failed to determine absolute path of working directory: %w", err)
	}

	var exists bool
	exists, err = PathExists(workDir)
	if err != nil {
		return emptyString, fmt.Errorf("failed checking if working directory exists: %w", err)
	}
	if !exists {
		return emptyString, fmt.Errorf("working directory does not exist: %s", workDir)
	}

	return workDir, nil
}
