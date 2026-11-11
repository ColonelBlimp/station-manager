package desktopid

import "os"

// fsOps is the small, unexported file-system seam the identity's creation is parameterised
// over, so its durability barriers are deterministically testable: production uses osFS;
// tests inject a failure at any one barrier and record their order. It belongs to this
// package's creation path alone (ADR 0103: its own handling, not the config writer's).
type fsOps interface {
	WriteFile(path string, data []byte, perm os.FileMode) error
	SyncFile(path string) error
	SyncDir(dir string) error
	Rename(oldpath, newpath string) error
}

type osFS struct{}

// WriteFile creates path exclusively (it must not exist) with perm and writes data.
func (osFS) WriteFile(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// SyncFile flushes path's data to stable storage.
func (osFS) SyncFile(path string) error { return syncPath(path) }

// SyncDir flushes a directory, persisting its entries: a file's own fsync does not persist
// the directory entry that names it (fsync(2)).
func (osFS) SyncDir(dir string) error { return syncPath(dir) }

func (osFS) Rename(oldpath, newpath string) error { return os.Rename(oldpath, newpath) }

func syncPath(p string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
