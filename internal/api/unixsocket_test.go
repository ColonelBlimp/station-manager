package api

// ST-5 — bindListener must make a Unix socket owner-only regardless of the ambient umask,
// and must refuse (fatally, before any socket is created) a parent directory or an ancestry
// that another local user could tamper with. Operator rulings 2026-08-16.

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// privSocketDir returns a fresh 0700 directory whose ancestry is safe (os.MkdirTemp
// creates 0700 under /tmp, which is sticky), with cleanup. It skips when the resulting
// socket path would exceed the AF_UNIX sun_path limit (~108 bytes) under a long TMPDIR —
// a real constraint on some CI runners (codex e66a33ab P2). A short base + short leaf keep
// well clear on normal systems.
func privSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "smd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if len(filepath.Join(dir, "s")) > 100 {
		t.Skipf("TMPDIR makes the unix socket path too long for AF_UNIX (%d bytes)", len(filepath.Join(dir, "s")))
	}
	return dir
}

// AC-5.1: the bound socket is mode 0600 under every umask (000/002/022/077).
func TestBindListener_UnixSocketOwnerOnlyUnderAnyUmask(t *testing.T) {
	for _, um := range []int{0o000, 0o002, 0o022, 0o077} {
		t.Run(fmt.Sprintf("umask_%04o", um), func(t *testing.T) {
			old := syscall.Umask(um)
			defer syscall.Umask(old)

			sock := filepath.Join(privSocketDir(t), "s")
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

// AC-5.3: a group/other-accessible immediate parent is fatal, and no socket is created
// (the check runs before net.Listen, closing the bind→chmod race).
func TestBindListener_UnsafeParentIsFatalNoSocket(t *testing.T) {
	shared := filepath.Join(privSocketDir(t), "shared")
	if err := os.Mkdir(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o755); err != nil { // force, regardless of umask
		t.Fatal(err)
	}
	sock := filepath.Join(shared, "s")
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
	base := privSocketDir(t)
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(link, "s")
	s := &Server{protocol: "unix"}

	if ln, err := s.bindListener(sock); err == nil {
		ln.Close()
		t.Fatal("bindListener accepted a symlinked parent directory")
	}
}

// P1 (codex e66a33ab): a world-writable, NON-sticky ANCESTOR is fatal even when the
// immediate parent is a clean 0700 — that ancestor would let another local user rename
// and swap the validated parent between the check and net.Listen. A sticky world-writable
// ancestor (like /tmp, exercised by every other test here) is accepted.
func TestBindListener_UnsafeAncestorIsFatal(t *testing.T) {
	base := privSocketDir(t)
	gp := filepath.Join(base, "gp")
	if err := os.Mkdir(gp, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(gp, 0o777); err != nil { // world-writable, NO sticky bit
		t.Fatal(err)
	}
	parent := filepath.Join(gp, "p")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(parent, "s")
	s := &Server{protocol: "unix"}

	if ln, err := s.bindListener(sock); err == nil {
		ln.Close()
		t.Fatal("bindListener accepted a world-writable non-sticky ancestor")
	}
	if _, statErr := os.Lstat(sock); !os.IsNotExist(statErr) {
		t.Errorf("a socket was created despite the unsafe ancestor")
	}
}
