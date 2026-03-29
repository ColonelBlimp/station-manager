package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// EnvSmWorkingDir is the internal constant name for the environment variable name used to specify
	// the working directory.
	EnvSmWorkingDir = "SM_WORKING_DIR"

	// xdgAppName is the directory name used under XDG_DATA_HOME.
	xdgAppName = "station-manager"
)

// systemPrefixes lists directory prefixes that indicate a system-wide install
// (e.g. via an nfpm .deb/.rpm package). When the executable lives under one of
// these paths and no explicit working directory has been provided, WorkingDir
// falls back to the XDG data directory instead.
var systemPrefixes = []string{"/usr/bin", "/usr/local/bin", "/usr/sbin"}

// WorkingDir determines the working directory path, prioritizing function argument, environment variable, or executable location.
// It validates the directory exists and returns an absolute path or an error if validation fails.
//
// Resolution order:
//  1. Explicit argument
//  2. SM_WORKING_DIR environment variable
//  3. XDG data directory ($XDG_DATA_HOME/station-manager) when the binary is in a system path
//  4. Executable's own directory (development / portable use)
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
		} else if isSystemPath(workDir) {
			workDir = xdgDataDir()
		}
	}

	if workDir, err = filepath.Abs(workDir); err != nil {
		return emptyString, fmt.Errorf("failed to determine absolute path of working directory: %w", err)
	}

	// Create the directory if it doesn't exist (covers first-run after package install).
	if err = os.MkdirAll(workDir, 0o755); err != nil {
		return emptyString, fmt.Errorf("failed to create working directory: %w", err)
	}

	return workDir, nil
}

// isSystemPath reports whether dir is (or is under) a well-known system binary prefix.
func isSystemPath(dir string) bool {
	for _, prefix := range systemPrefixes {
		if dir == prefix || strings.HasPrefix(dir, prefix+"/") {
			return true
		}
	}
	return false
}

// xdgDataDir returns $XDG_DATA_HOME/station-manager, defaulting to
// ~/.local/share/station-manager when XDG_DATA_HOME is unset.
func xdgDataDir() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == emptyString {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.TempDir() // last-resort fallback
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, xdgAppName)
}
