package utils

import (
	"os"
	"path/filepath"
)

// AbsDirPathForExecutable returns the absolute directory path of the currently running executable.
func AbsDirPathForExecutable() (string, error) {
	// Get the path to the executable
	execPath, err := os.Executable()
	if err != nil {
		return emptyString, err
	}

	// Get the absolute path of the executable's directory
	var dirPath string
	if dirPath, err = filepath.Abs(filepath.Dir(execPath)); err != nil {
		return emptyString, err
	}

	return dirPath, nil
}
