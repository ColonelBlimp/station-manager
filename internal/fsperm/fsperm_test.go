package fsperm

// ST-6 — the private-state policy: tighten application-owned data paths to owner-only and
// verify; never mutate operator-supplied external paths but warn when they leak to
// group/other; decide containment by resolving symlinks, never a lexical prefix.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContained_SymlinkAware(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "db", "log.db")
	if err := os.MkdirAll(filepath.Dir(inside), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if ok, err := Contained(root, inside); err != nil || !ok {
		t.Errorf("Contained(root, inside) = %v, %v; want true", ok, err)
	}
	// The root itself is NOT contained (must not be tightened as a child).
	if ok, _ := Contained(root, root); ok {
		t.Error("Contained(root, root) = true; want false")
	}
	// A sibling directory outside root.
	outside := t.TempDir()
	extFile := filepath.Join(outside, "log.db")
	if err := os.WriteFile(extFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, _ := Contained(root, extFile); ok {
		t.Error("Contained(root, external) = true; want false")
	}
	// A symlink under root pointing OUTSIDE must not count as contained (symlink-aware,
	// not lexical prefix).
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if ok, _ := Contained(root, filepath.Join(link, "log.db")); ok {
		t.Error("Contained via an escaping symlink = true; want false (lexical prefix would wrongly pass)")
	}
}

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func TestSecureApplicationPath_TightensAppOwned(t *testing.T) {
	work := t.TempDir()
	// A data file laid out under the working dir at a permissive 0644.
	f := filepath.Join(work, "db", "log.db")
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	warn, err := SecureApplicationPath(work, f, 0o600)
	if err != nil || warn != "" {
		t.Fatalf("app-owned file: warn=%q err=%v; want tightened silently", warn, err)
	}
	if mode(t, f) != 0o600 {
		t.Errorf("app-owned file mode = %04o, want 0600 (effective, not just the arg)", mode(t, f))
	}

	// An app-owned directory tightens to 0700.
	d := filepath.Join(work, "exports")
	if err := os.Mkdir(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := SecureApplicationPath(work, d, 0o700); err != nil {
		t.Fatal(err)
	}
	if mode(t, d) != 0o700 {
		t.Errorf("app-owned dir mode = %04o, want 0700", mode(t, d))
	}
}

func TestSecureApplicationPath_ExternalWarnsNotMutated(t *testing.T) {
	work := t.TempDir()
	external := filepath.Join(t.TempDir(), "log.db") // a DIFFERENT tree, operator-supplied
	if err := os.WriteFile(external, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	warn, err := SecureApplicationPath(work, external, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if warn == "" {
		t.Error("group/other-accessible external path produced no warning")
	}
	if mode(t, external) != 0o644 {
		t.Errorf("external path was mutated to %04o; it must be left unchanged (0644)", mode(t, external))
	}

	// A private external path (0600) is left alone with NO warning.
	priv := filepath.Join(t.TempDir(), "log.db")
	if err := os.WriteFile(priv, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if warn, err := SecureApplicationPath(work, priv, 0o600); err != nil || warn != "" {
		t.Errorf("private external path: warn=%q err=%v; want silent", warn, err)
	}
}

func TestSecureApplicationPath_MissingIsNoop(t *testing.T) {
	work := t.TempDir()
	if warn, err := SecureApplicationPath(work, filepath.Join(work, "nope.db"), 0o600); err != nil || warn != "" {
		t.Errorf("missing path: warn=%q err=%v; want no-op", warn, err)
	}
}
