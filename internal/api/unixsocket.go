package api

// ST-5 (docs/reviews/internal-security-trust-boundary-audit.md) — a Unix-socket listener
// is authorised entirely by filesystem permissions, but net.Listen creates the socket with
// 0777 & ^umask, so under a permissive umask any local principal could connect and reach
// the full read/config/RF surface. This hardens the Unix path (operator rulings 2026-08-16):
//
//   - The immediate parent directory must be euid-owned, a real directory (not a symlink),
//     and inaccessible to group/other. It is created 0700 when absent and validated
//     fatally when present — a private parent closes the race between net.Listen creating
//     the socket and the subsequent chmod, during which the socket briefly carries the
//     umask-derived mode. An unsafe operator-supplied parent is fatal, not advisory.
//   - After bind, the socket is chmod'd 0600 and then verified (is-socket, euid-owned,
//     mode 0600); binding does not proceed to Serve until verification succeeds, and on
//     any failure the socket is unlinked.
//
// Unix-only: SM ships as a Linux daemon (systemd/RPM), and the euid/owner checks use
// syscall.Stat_t. The whole feature only runs for protocol=unix.

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// socketMode is the required (and enforced) mode for a Unix listener socket.
const socketMode os.FileMode = 0o600

// socketDirMode is the required (and created) mode for a Unix socket's parent directory.
const socketDirMode os.FileMode = 0o700

// prepareUnixSocketParent makes the immediate parent of socketPath an owner-private
// directory and refuses to continue if that guarantee cannot be established. It runs
// BEFORE net.Listen so the socket is never created inside a group/other-accessible
// directory (closing the bind→chmod race). MkdirAll(0700) creates a missing parent
// privately and no-ops on an existing one (it never loosens or mutates an existing
// directory's mode), after which the parent is validated fatally.
func prepareUnixSocketParent(socketPath string) error {
	parent := filepath.Dir(socketPath)
	if err := os.MkdirAll(parent, socketDirMode); err != nil {
		return fmt.Errorf("creating socket parent directory: %w", err)
	}
	// Lstat (not Stat): the immediate parent itself must not be a symlink.
	fi, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("inspecting socket parent directory: %w", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("socket parent directory is a symlink; refusing (a private, real directory is required)")
	}
	if !fi.IsDir() {
		return fmt.Errorf("socket parent path is not a directory")
	}
	// Inaccessible to group/other — no r/w/x bits for anyone but the owner, not merely
	// non-writable.
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("socket parent directory is accessible to group/other (mode %04o); it must be owner-only (0700)",
			fi.Mode().Perm())
	}
	if err := ensureEuidOwned(fi); err != nil {
		return fmt.Errorf("socket parent directory: %w", err)
	}
	return nil
}

// secureUnixSocket chmods the freshly bound socket to 0600 and verifies the result —
// it is a socket, euid-owned, and exactly mode 0600. A caller that gets an error must
// unlink the socket and refuse to serve.
func secureUnixSocket(socketPath string) error {
	if err := os.Chmod(socketPath, socketMode); err != nil {
		return fmt.Errorf("chmod socket to %04o: %w", socketMode, err)
	}
	fi, err := os.Lstat(socketPath)
	if err != nil {
		return fmt.Errorf("stat socket after chmod: %w", err)
	}
	if fi.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("bound path is not a socket after bind")
	}
	if fi.Mode().Perm() != socketMode {
		return fmt.Errorf("socket mode is %04o after chmod, want %04o", fi.Mode().Perm(), socketMode)
	}
	if err := ensureEuidOwned(fi); err != nil {
		return fmt.Errorf("socket: %w", err)
	}
	return nil
}

// ensureEuidOwned reports an error unless fi is owned by the effective uid of this
// process — the principal the socket permissions are meant to bound authorisation to.
func ensureEuidOwned(fi os.FileInfo) error {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot determine owner (unsupported platform)")
	}
	if euid := os.Geteuid(); int(st.Uid) != euid {
		return fmt.Errorf("not owned by the daemon user (owner uid %d, want %d)", st.Uid, euid)
	}
	return nil
}
