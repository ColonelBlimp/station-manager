// Package fsperm carries the one private-state filesystem policy for local artifacts that
// hold operator/QSO data (ST-6, docs/reviews/internal-security-trust-boundary-audit.md).
//
// Application-owned paths — those the daemon lays out under its own working directory — are
// tightened to owner-only and the result is verified (a hard requirement: startup fails if
// the mode cannot be established). Operator-supplied paths OUTSIDE the working directory are
// never mutated — the operator may have pointed the daemon at a deliberately-shared location
// — but a group/world-accessible one earns a high-signal warning. Containment is decided by
// resolving symlinks on both sides (never a lexical prefix), and a symlink is never chmod'd
// through (operator ruling 2026-08-16).
package fsperm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Contained reports whether target is inside root after resolving symlinks on BOTH — a
// symlink-aware containment test, not a lexical prefix. Both paths must exist. A target
// equal to root is reported as NOT contained (the working-directory root is deliberately
// left at its own mode and must never be tightened as if it were a child).
func Contained(root, target string) (bool, error) {
	rr, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false, fmt.Errorf("resolving root: %w", err)
	}
	rt, err := filepath.EvalSymlinks(target)
	if err != nil {
		return false, fmt.Errorf("resolving target: %w", err)
	}
	rel, err := filepath.Rel(rr, rt)
	if err != nil {
		return false, nil
	}
	if rel == "." { // target IS the root
		return false, nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false, nil
	}
	return true, nil
}

// SecureApplicationPath enforces the private-state policy on an EXISTING path, tightening it
// to want (e.g. 0600 for a data file, 0700 for a directory):
//
//   - Application-owned (contained in workDir per Contained, not itself a symlink, euid-
//     owned): chmod to want and re-verify. A failure to establish the mode is returned as an
//     error — a hard requirement.
//   - Otherwise (external/operator-supplied, a symlink, or not euid-owned): NOT mutated. A
//     non-empty warning is returned when the path grants any group/other access, so the
//     operator can tighten it by hand.
//
// A missing path is not an error (nothing to secure) — the caller decides which paths must
// exist. want is interpreted by its permission bits only.
func SecureApplicationPath(workDir, path string, want os.FileMode) (warn string, err error) {
	lst, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	appOwned := false
	if lst.Mode()&os.ModeSymlink == 0 && euidOwns(lst) {
		if contained, cErr := Contained(workDir, path); cErr == nil && contained {
			appOwned = true
		}
	}

	if appOwned {
		if err := os.Chmod(path, want.Perm()); err != nil {
			return "", fmt.Errorf("chmod %s to %04o: %w", filepath.Base(path), want.Perm(), err)
		}
		fi, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if fi.Mode().Perm() != want.Perm() {
			return "", fmt.Errorf("could not establish mode %04o on %s (got %04o)",
				want.Perm(), filepath.Base(path), fi.Mode().Perm())
		}
		return "", nil
	}

	// External/symlink/foreign-owned: never mutated; warn if it leaks to group/other.
	if lst.Mode().Perm()&0o077 != 0 {
		return "an operator-supplied path holding station data is accessible to group/other; " +
			"tighten it by hand — the daemon leaves paths outside its working directory unchanged", nil
	}
	return "", nil
}

func euidOwns(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Geteuid()
}
