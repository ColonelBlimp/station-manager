package utils

import (
	"os"
	"path/filepath"
	"strings"
)

// ExecName returns the name of the current executable, with an option to strip its file extension (if it exists).
// It resolves symlinks to determine the actual path of the executable when possible.
// The parameter stripExt specifies whether to remove the file extension from the executable name.
// It returns the extracted name as a string and an error if retrieval fails.
// All path components are stripped from the returned name.
func ExecName(stripExt bool) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	// Try to resolve symlinks (best-effort)
	if actual, err2 := filepath.EvalSymlinks(exe); err2 == nil {
		exe = actual
	}
	name := filepath.Base(exe)
	if stripExt {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	return name, nil
}
