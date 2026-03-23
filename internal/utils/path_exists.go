package utils

import (
	"errors"
	"os"
)

// PathExists checks if a specified file or directory exists at the given path.
// It resolves symlinks because it uses os.Stat.
// It returns true if the path exists, false if it does not exist,
// and an error for permission-related issues or unexpected file system errors.
func PathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		// Path exists
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		// Path does not exist
		return false, nil
	}
	// Return other errors, e.g., permission issues, IO errors
	return false, err
}
