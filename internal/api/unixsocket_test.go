package api

// ST-5 — bindListener must make a Unix socket owner-only regardless of the ambient umask,
// and must refuse (fatally, before any socket is created) a parent directory that is not
// an owner-private real directory. Operator rulings 2026-08-16.

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// AC-5.1: the bound socket is mode 0600 under every umask (000/002/022/077).
func TestBindListener_UnixSocketOwnerOnlyUnderAnyUmask(t *testing.T) {
	for _, um := range []int{0o000, 0o002, 0o022, 0o077} {
		t.Run(fmt.Sprintf("umask_%04o", um), func(t *testing.T) {
			old := syscall.Umask(um)
			defer syscall.Umask(old)

			// A non-existent immediate parent, so prepareUnixSocketParent creates it
			// 0700 (umask-immune, since 0700 has no group/other bits for the umask to
			// clear) — the realistic default-path case. This isolates the assertion to
			// the socket's own mode.
			sock := filepath.Join(t.TempDir(), "run", "smd.sock")
			s := &Server{protocol: "unix"}

			ln, err := s.bindListener(sock)
			if err != nil {
				t.Fatalf("bindListener: %v", err)
			}
			defer ln.Close()

			fi, err := os.Lstat(sock)
			if err != nil {
				t.Fatal(err)
			}
			if fi.Mode()&os.ModeSocket == 0 {
				t.Fatalf("bound path is not a socket")
			}
			if fi.Mode().Perm() != 0o600 {
				t.Errorf("umask %04o: socket mode = %04o, want 0600", um, fi.Mode().Perm())
			}
		})
	}
}

// AC-5.3: a group/other-accessible parent is fatal, and no socket is created (the check
// runs before net.Listen, closing the bind→chmod race).
func TestBindListener_UnsafeParentIsFatalNoSocket(t *testing.T) {
	dir := t.TempDir()
	shared := filepath.Join(dir, "shared")
	if err := os.Mkdir(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o755); err != nil { // force, regardless of umask
		t.Fatal(err)
	}
	sock := filepath.Join(shared, "smd.sock")
	s := &Server{protocol: "unix"}

	ln, err := s.bindListener(sock)
	if err == nil {
		ln.Close()
		t.Fatal("bindListener accepted a group/other-accessible parent directory")
	}
	if _, statErr := os.Lstat(sock); !os.IsNotExist(statErr) {
		t.Errorf("a socket was created despite the unsafe parent; the check must run before net.Listen")
	}
}

// AC-5.3: a symlinked parent is fatal (a private, real directory is required).
func TestBindListener_SymlinkParentIsFatal(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(link, "smd.sock")
	s := &Server{protocol: "unix"}

	if ln, err := s.bindListener(sock); err == nil {
		ln.Close()
		t.Fatal("bindListener accepted a symlinked parent directory")
	}
}
