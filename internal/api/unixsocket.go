package api

// ST-5 (docs/reviews/internal-security-trust-boundary-audit.md) — a Unix-socket listener
// is authorised entirely by filesystem permissions, but net.Listen creates the socket with
// 0777 & ^umask, so under a permissive umask any local principal could connect and reach
// the full read/config/RF surface. This hardens the Unix path (operator rulings 2026-08-16):
//
//   - The immediate parent directory must be euid-owned, a real directory (not a symlink),
//     and inaccessible to group/other (0700). It is created 0700 when absent and validated
//     fatally when present. The WHOLE ancestry up to "/" is then validated too — otherwise
//     a local user who can write an ancestor could rename the validated parent and swap in
//     their own directory between net.Listen creating the socket and the subsequent chmod,
//     during which the socket briefly carries the umask-derived mode (codex e66a33ab P1).
//     This prevents replacement by another local user; it is not protection against root or
//     the daemon's own uid. An unsafe operator-supplied path is fatal, not advisory.
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
	// The immediate parent being 0700 is not enough: if any ANCESTOR is writable by
	// another local user, they can rename the validated parent and swap in their own
	// directory between this check and net.Listen (codex e66a33ab P1). Validate the whole
	// chain from the grandparent up to "/".
	if err := validateSocketAncestry(filepath.Dir(parent)); err != nil {
		return err
	}
	return nil
}

// validateSocketAncestry walks from dir up to "/" and refuses any component another
// local user could tamper with — closing the rename TOCTOU that immediate-parent
// validation alone leaves open. Each ancestor must be a non-symlink directory (symlink
// components are rejected outright — the simpler, reliable containment) owned by root or
// the effective uid, and either NOT group/other-writable OR world-writable WITH the
// sticky bit. The sticky exception is load-bearing and correct: a sticky directory such
// as /tmp (mode 01777) does NOT let a non-owner rename another user's entry, so it cannot
// be used to swap a validated euid-owned directory; without it every ancestry rooted in
// /tmp (i.e. essentially all of them) would be rejected while offering no real protection.
//
// This prevents REPLACEMENT by another local user. It is NOT protection against root or
// the daemon's own uid, which can always tamper with the daemon's own files.
func validateSocketAncestry(dir string) error {
	for cur := filepath.Clean(dir); ; {
		fi, err := os.Lstat(cur)
		if err != nil {
			return fmt.Errorf("inspecting the socket parent's ancestry: %w", err)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("the socket path ancestry contains a symlink; the whole path must be real, owner-private directories")
		}
		if !fi.IsDir() {
			return fmt.Errorf("the socket path ancestry contains a non-directory component")
		}
		if err := ensureRootOrEuidOwned(fi); err != nil {
			return fmt.Errorf("socket path ancestry: %w", err)
		}
		if fi.Mode().Perm()&0o022 != 0 && fi.Mode()&os.ModeSticky == 0 {
			return fmt.Errorf("a directory in the socket path ancestry is writable by group/other without the sticky bit; " +
				"another local user could replace it")
		}
		parent := filepath.Dir(cur)
		if parent == cur { // reached the filesystem root
			return nil
		}
		cur = parent
	}
}

// ensureRootOrEuidOwned reports an error unless fi is owned by root or the effective uid —
// the two principals that legitimately control the daemon's path. A directory owned by any
// other user in the ancestry is a tampering vector.
func ensureRootOrEuidOwned(fi os.FileInfo) error {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot determine owner (unsupported platform)")
	}
	if uid := int(st.Uid); uid != 0 && uid != os.Geteuid() {
		return fmt.Errorf("a directory is owned by neither root nor the daemon user")
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
