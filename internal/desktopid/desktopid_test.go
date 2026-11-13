package desktopid

// W-0027 C1b, ADR 0103 point 1: smd's TLS identity for the desktop app, kept in a protected
// directory, published as one unit, validated on every use, and refused (never repaired)
// when anything is wrong.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

// root gives each test a private, owner-only working directory.
func root(t *testing.T) string {
	t.Helper()
	r := filepath.Join(t.TempDir(), "wd")
	if err := os.Mkdir(r, 0o700); err != nil {
		t.Fatal(err)
	}
	return r
}

// snapshot records every path, mode and content under dir, to prove a refusal changed nothing.
func snapshot(t *testing.T, dir string) string {
	t.Helper()
	var lines []string
	_ = filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		line := fmt.Sprintf("%s %v", p, fi.Mode())
		if fi.Mode().IsRegular() {
			b, _ := os.ReadFile(p)
			line += fmt.Sprintf(" %x", sha256.Sum256(b))
		}
		lines = append(lines, line)
		return nil
	})
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func mustEnsure(t *testing.T, r string) Identity {
	t.Helper()
	id, d, err := Ensure(r)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if d != Durable {
		t.Fatalf("Ensure durability = %v, want Durable", d)
	}
	return id
}

func TestEnsure_CreatesAProtectedIdentity(t *testing.T) {
	r := root(t)
	id := mustEnsure(t, r)

	dir := filepath.Join(r, DirName)
	fi, err := os.Lstat(dir)
	if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		t.Fatalf("identity directory: %v %v; want a 0700 directory", fi, err)
	}
	key, err := os.Lstat(filepath.Join(dir, KeyName))
	if err != nil || key.Mode().Perm() != 0o600 {
		t.Fatalf("key: %v %v; want 0600", key, err)
	}
	cert, err := os.Lstat(filepath.Join(dir, CertName))
	if err != nil || cert.Mode().Perm()&0o022 != 0 {
		t.Fatalf("certificate: %v %v; want not group/other writable", cert, err)
	}

	c, err := x509.ParseCertificate(id.CertDER)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.IPAddresses) != 1 || !c.IPAddresses[0].Equal(net.ParseIP("127.0.0.1")) {
		t.Errorf("certificate IPs = %v, want exactly 127.0.0.1", c.IPAddresses)
	}
	years := c.NotAfter.Sub(c.NotBefore).Hours() / 24 / 365
	if years < 9.9 || years > 10.1 {
		t.Errorf("certificate validity = %.2f years, want 10 (operator ruling 2026-11-10)", years)
	}
	if pub, ok := c.PublicKey.(*ecdsa.PublicKey); !ok || pub.Curve != elliptic.P256() {
		t.Errorf("public key = %T, want ECDSA P-256", c.PublicKey)
	}
	if id.Fingerprint != Fingerprint(id.CertDER) || len(id.Fingerprint) != 95 {
		t.Errorf("fingerprint %q: want the colon-separated SHA-256 of the DER", id.Fingerprint)
	}
	if len(id.TLS.Certificate) == 0 || id.TLS.PrivateKey == nil {
		t.Error("the TLS certificate is not usable for serving")
	}
}

func TestEnsure_ReusesThePublishedIdentity(t *testing.T) {
	r := root(t)
	first := mustEnsure(t, r)
	before := snapshot(t, r)
	second := mustEnsure(t, r)
	if first.Fingerprint != second.Fingerprint {
		t.Fatalf("a second Ensure regenerated the identity: %s then %s", first.Fingerprint, second.Fingerprint)
	}
	if after := snapshot(t, r); after != before {
		t.Fatalf("a second Ensure changed the files:\n%s\n---\n%s", before, after)
	}
}

func TestFingerprint_IsSHA256OverDER_UpperHexWithColons(t *testing.T) {
	der := []byte("not a certificate, just bytes")
	sum := sha256.Sum256(der)
	var parts []string
	for _, b := range sum {
		parts = append(parts, fmt.Sprintf("%02X", b))
	}
	if got, want := Fingerprint(der), strings.Join(parts, ":"); got != want {
		t.Fatalf("Fingerprint = %s, want %s (the shell's format: Node's X509Certificate.fingerprint256)", got, want)
	}
}

// refused asserts that Ensure (and Load) refuse, name the given file, and change nothing.
func refused(t *testing.T, r, names string) {
	t.Helper()
	before := snapshot(t, r)
	_, _, err := Ensure(r)
	if err == nil {
		t.Fatal("Ensure accepted an identity it must refuse")
	}
	if !strings.Contains(err.Error(), names) {
		t.Errorf("error %q does not name %q", err, names)
	}
	if _, lerr := Load(r); lerr == nil {
		t.Error("Load accepted an identity it must refuse")
	}
	if after := snapshot(t, r); after != before {
		t.Fatalf("a refusal changed the files (never repair):\n%s\n---\n%s", before, after)
	}
}

func TestRefuses_APartialIdentity(t *testing.T) {
	t.Run("key without certificate", func(t *testing.T) {
		r := root(t)
		mustEnsure(t, r)
		_ = os.Remove(filepath.Join(r, DirName, CertName))
		refused(t, r, CertName)
	})
	t.Run("certificate without key", func(t *testing.T) {
		r := root(t)
		mustEnsure(t, r)
		_ = os.Remove(filepath.Join(r, DirName, KeyName))
		refused(t, r, KeyName)
	})
	t.Run("empty identity directory", func(t *testing.T) {
		r := root(t)
		if err := os.Mkdir(filepath.Join(r, DirName), 0o700); err != nil {
			t.Fatal(err)
		}
		refused(t, r, KeyName)
	})
}

func TestRefuses_AMismatchedOrMalformedPair(t *testing.T) {
	t.Run("certificate from another identity", func(t *testing.T) {
		a, b := root(t), root(t)
		mustEnsure(t, a)
		mustEnsure(t, b)
		other, _ := os.ReadFile(filepath.Join(b, DirName, CertName))
		if err := os.WriteFile(filepath.Join(a, DirName, CertName), other, 0o644); err != nil {
			t.Fatal(err)
		}
		refused(t, a, "do not belong together")
	})
	t.Run("malformed certificate", func(t *testing.T) {
		r := root(t)
		mustEnsure(t, r)
		if err := os.WriteFile(filepath.Join(r, DirName, CertName), []byte("garbage"), 0o644); err != nil {
			t.Fatal(err)
		}
		refused(t, r, CertName)
	})
	t.Run("malformed key", func(t *testing.T) {
		r := root(t)
		mustEnsure(t, r)
		if err := os.WriteFile(filepath.Join(r, DirName, KeyName), []byte("garbage"), 0o600); err != nil {
			t.Fatal(err)
		}
		refused(t, r, KeyName)
	})
}

func TestRefuses_AnUnexpectedFile(t *testing.T) {
	r := root(t)
	mustEnsure(t, r)
	if err := os.WriteFile(filepath.Join(r, DirName, "extra.pem"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	refused(t, r, "extra.pem")
}

func TestRefuses_UnsafeModes(t *testing.T) {
	cases := []struct {
		name, path string
		mode       os.FileMode
		names      string
	}{
		{"identity directory not 0700", DirName, 0o755, DirName},
		{"key readable by group", filepath.Join(DirName, KeyName), 0o640, KeyName},
		{"key executable (not exactly 0600)", filepath.Join(DirName, KeyName), 0o700, KeyName},
		{"certificate writable by others", filepath.Join(DirName, CertName), 0o666, CertName},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := root(t)
			mustEnsure(t, r)
			if err := os.Chmod(filepath.Join(r, c.path), c.mode); err != nil {
				t.Fatal(err)
			}
			refused(t, r, c.names)
		})
	}
}

func TestRefuses_AnAncestorAnotherUserCouldReplace(t *testing.T) {
	// The working directory itself group-writable (no sticky bit): another member of the
	// group could rename the identity directory away and put their own in its place.
	r := root(t)
	mustEnsure(t, r)
	if err := os.Chmod(r, 0o770); err != nil {
		t.Fatal(err)
	}
	refused(t, r, r)
}

func TestAccepts_AStickyWorldWritableAncestor(t *testing.T) {
	// /tmp-style directories (world-writable with the sticky bit) protect their entries:
	// only the owner may rename or remove them. t.TempDir lives under one.
	parent := t.TempDir()
	sticky := filepath.Join(parent, "sticky")
	if err := os.Mkdir(sticky, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sticky, os.ModeSticky|0o777); err != nil {
		t.Fatal(err)
	}
	r := filepath.Join(sticky, "wd")
	if err := os.Mkdir(r, 0o700); err != nil {
		t.Fatal(err)
	}
	mustEnsure(t, r)
}

func TestRefuses_ForeignOwnership(t *testing.T) {
	r := root(t)
	mustEnsure(t, r)
	saved := currentUID
	currentUID = func() int { return saved() + 4242 } // every file now belongs to "someone else"
	t.Cleanup(func() { currentUID = saved })
	refused(t, r, "owned by")
}

func TestRefuses_AnAncestorOwnedByAnotherUser(t *testing.T) {
	// The identity's own files are ours; one directory above the working directory belongs to
	// another (non-root) user, who could therefore replace the whole path.
	r := root(t)
	mustEnsure(t, r)
	foreignParent := filepath.Base(filepath.Dir(r))
	saved := ownerOf
	ownerOf = func(fi os.FileInfo) (int, bool) {
		if fi.IsDir() && fi.Name() == foreignParent {
			return currentUID() + 4242, true
		}
		return saved(fi)
	}
	t.Cleanup(func() { ownerOf = saved })
	refused(t, r, "neither the running user")
}

func TestRefuses_AnIdentityFileOwnedByAnotherUser(t *testing.T) {
	// Every directory is ours; only the key appears to belong to another user.
	r := root(t)
	mustEnsure(t, r)
	saved := ownerOf
	ownerOf = func(fi os.FileInfo) (int, bool) {
		if fi.Name() == KeyName {
			return currentUID() + 4242, true
		}
		return saved(fi)
	}
	t.Cleanup(func() { ownerOf = saved })
	refused(t, r, KeyName+" is owned by uid")
}

func TestRefuses_Symlinks(t *testing.T) {
	t.Run("identity directory is a symlink", func(t *testing.T) {
		elsewhere := root(t)
		mustEnsure(t, elsewhere)
		r := root(t)
		if err := os.Symlink(filepath.Join(elsewhere, DirName), filepath.Join(r, DirName)); err != nil {
			t.Fatal(err)
		}
		refused(t, r, DirName+" is a symlink")
	})
	t.Run("certificate is a symlink", func(t *testing.T) {
		r := root(t)
		mustEnsure(t, r)
		c := filepath.Join(r, DirName, CertName)
		moved := filepath.Join(r, "real-cert.pem")
		if err := os.Rename(c, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(moved, c); err != nil {
			t.Fatal(err)
		}
		refused(t, r, CertName+" is a symlink")
	})
}

// --- creation barriers -------------------------------------------------------------

// faultFS records each barrier and can fail one of them.
type faultFS struct {
	osFS
	calls  []string
	failAt string
}

func (f *faultFS) step(name string) error {
	f.calls = append(f.calls, name)
	if name == f.failAt {
		return errors.New("injected failure at " + name)
	}
	return nil
}

func (f *faultFS) WriteFile(path string, data []byte, perm os.FileMode) error {
	if err := f.step("write " + filepath.Base(path)); err != nil {
		return err
	}
	return f.osFS.WriteFile(path, data, perm)
}

func (f *faultFS) SyncFile(path string) error {
	if err := f.step("sync " + filepath.Base(path)); err != nil {
		return err
	}
	return f.osFS.SyncFile(path)
}

func (f *faultFS) SyncDir(dir string) error {
	name := "sync parent"
	if b := filepath.Base(dir); strings.HasPrefix(b, tmpPrefix) || strings.HasPrefix(b, credTmpPrefix) {
		name = "sync prepared dir"
	}
	if err := f.step(name); err != nil {
		return err
	}
	return f.osFS.SyncDir(dir)
}

func (f *faultFS) Rename(oldpath, newpath string) error {
	if err := f.step("rename"); err != nil {
		return err
	}
	return f.osFS.Rename(oldpath, newpath)
}

var barrierOrder = []string{
	"write " + KeyName, "sync " + KeyName,
	"write " + CertName, "sync " + CertName,
	"sync prepared dir", "rename", "sync parent",
}

func TestCreate_BarriersInOrder(t *testing.T) {
	r := root(t)
	f := &faultFS{}
	if _, d, err := ensureWith(r, f, time.Now()); err != nil || d != Durable {
		t.Fatalf("ensure: %v %v", d, err)
	}
	if strings.Join(f.calls, ", ") != strings.Join(barrierOrder, ", ") {
		t.Fatalf("barriers = %v\nwant       %v", f.calls, barrierOrder)
	}
}

func TestCreate_AFailureBeforeTheRenamePublishesNothing(t *testing.T) {
	for _, step := range barrierOrder[:6] { // every step up to and including the rename
		t.Run(step, func(t *testing.T) {
			r := root(t)
			f := &faultFS{failAt: step}
			_, _, err := ensureWith(r, f, time.Now())
			if err == nil {
				t.Fatal("a failure before publication was not reported")
			}
			if _, serr := os.Lstat(filepath.Join(r, DirName)); !os.IsNotExist(serr) {
				t.Fatalf("an identity was published despite a failure at %q", step)
			}
			// The next start creates a complete identity and removes the unpublished remnant.
			mustEnsure(t, r)
			entries, _ := os.ReadDir(r)
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), tmpPrefix) {
					t.Errorf("unpublished remnant %q left behind after the next start", e.Name())
				}
			}
		})
	}
}

func TestCreate_AFailedParentSyncIsPublishedButUnconfirmed(t *testing.T) {
	r := root(t)
	f := &faultFS{failAt: "sync parent"}
	id, d, err := ensureWith(r, f, time.Now())
	if err != nil {
		t.Fatalf("a failed final sync must not be reported as a failure: %v", err)
	}
	if d != DurabilityUnconfirmed {
		t.Fatalf("durability = %v, want DurabilityUnconfirmed", d)
	}
	again := mustEnsure(t, r)
	if again.Fingerprint != id.Fingerprint {
		t.Fatal("the published identity was replaced after an unconfirmed sync (never silently replaced)")
	}
}

func TestCreate_LeavesOtherFilesAlone(t *testing.T) {
	r := root(t)
	other := filepath.Join(r, "config.json")
	if err := os.WriteFile(other, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	notOurs := filepath.Join(r, "desktop-identity-backup")
	if err := os.Mkdir(notOurs, 0o700); err != nil {
		t.Fatal(err)
	}
	mustEnsure(t, r)
	for _, p := range []string{other, notOurs} {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("%s was disturbed: %v", p, err)
		}
	}
}

func TestPEM_KeyIsPKCS8(t *testing.T) {
	r := root(t)
	mustEnsure(t, r)
	b, _ := os.ReadFile(filepath.Join(r, DirName, KeyName))
	blk, _ := pem.Decode(b)
	if blk == nil || blk.Type != "PRIVATE KEY" {
		t.Fatalf("key PEM block = %v, want PRIVATE KEY (PKCS#8)", blk)
	}
	if _, err := x509.ParsePKCS8PrivateKey(blk.Bytes); err != nil {
		t.Fatal(err)
	}
}

func TestCreate_RemovesAnUnpublishedRemnantAtTheNextStart(t *testing.T) {
	// A crash before the rename leaves a prepared directory behind; the next start removes it
	// (and only it) before creating the identity.
	r := root(t)
	remnant := filepath.Join(r, tmpPrefix+"123456")
	if err := os.Mkdir(remnant, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remnant, KeyName), []byte("half-written"), 0o600); err != nil {
		t.Fatal(err)
	}
	lookalike := filepath.Join(r, "desktop-identity.tmp-not-ours")
	if err := os.Mkdir(lookalike, 0o700); err != nil {
		t.Fatal(err)
	}
	mustEnsure(t, r)
	if _, err := os.Lstat(remnant); !os.IsNotExist(err) {
		t.Errorf("the unpublished remnant %s was not removed", remnant)
	}
	if _, err := os.Lstat(lookalike); err != nil {
		t.Errorf("%s (not one of ours) was removed", lookalike)
	}
}

func TestRefuses_ANonRegularIdentityFile(t *testing.T) {
	r := root(t)
	mustEnsure(t, r)
	c := filepath.Join(r, DirName, CertName)
	if err := os.Remove(c); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(c, 0o700); err != nil { // a directory where the certificate should be
		t.Fatal(err)
	}
	refused(t, r, CertName+" is not a regular file")
}

func TestCreate_ConcurrentCreatorsShareOneIdentity(t *testing.T) {
	r := root(t)
	const n = 8
	type result struct {
		fp  string
		err error
	}
	out := make(chan result, n)
	for i := 0; i < n; i++ {
		go func() {
			id, _, err := Ensure(r)
			out <- result{id.Fingerprint, err}
		}()
	}
	var first string
	for i := 0; i < n; i++ {
		res := <-out
		if res.err != nil {
			t.Fatalf("a concurrent creator failed: %v", res.err)
		}
		if first == "" {
			first = res.fp
		} else if res.fp != first {
			t.Fatalf("concurrent creators produced different identities: %s and %s", first, res.fp)
		}
	}
	entries, _ := os.ReadDir(r)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), tmpPrefix) {
			t.Errorf("a temporary directory %q was left behind", e.Name())
		}
	}
}

func TestCreate_NeverRemovesACreationInProgress(t *testing.T) {
	// Another creator holds the lock with its prepared directory not yet published: a second
	// Ensure waits for it rather than removing that directory as a remnant.
	r := root(t)
	unlock, err := lockCreation(r)
	if err != nil {
		t.Fatal(err)
	}
	inProgress := filepath.Join(r, tmpPrefix+"in-progress")
	if err := os.Mkdir(inProgress, 0o700); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := Ensure(r)
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Lstat(inProgress); err != nil {
		t.Fatalf("the other creator's directory in progress was removed while it held the lock: %v", err)
	}
	select {
	case err := <-done:
		t.Fatalf("Ensure did not wait for the creation lock (returned %v)", err)
	default:
	}
	unlock()
	if err := <-done; err != nil {
		t.Fatalf("Ensure after the lock was released: %v", err)
	}
}

func TestCreate_NeverPublishesOverAnExistingIdentity(t *testing.T) {
	// Between the absence check and publication another identity appears: the rename must not
	// replace it, and it must survive unchanged.
	r := root(t)
	other := root(t)
	mustEnsure(t, other)
	f := &faultFS{}
	var planted string
	hook := &renameHook{faultFS: f, before: func() {
		if err := os.Rename(filepath.Join(other, DirName), filepath.Join(r, DirName)); err != nil {
			t.Fatal(err)
		}
		planted = snapshot(t, filepath.Join(r, DirName))
	}}
	if _, _, err := ensureWith(r, hook, time.Now()); err == nil {
		t.Fatal("publication over an existing identity was not refused")
	}
	if got := snapshot(t, filepath.Join(r, DirName)); got != planted {
		t.Fatalf("the existing identity was changed by the second publication:\n%s\n---\n%s", planted, got)
	}
}

// renameHook runs before just before the publication rename.
type renameHook struct {
	*faultFS
	before func()
}

func (h *renameHook) Rename(oldpath, newpath string) error {
	h.before()
	return h.faultFS.Rename(oldpath, newpath)
}

func TestLoad_UsesTheResolvedPathNotASwappableAlias(t *testing.T) {
	// The caller's root is a symlink. After validation, someone swaps it to point at another
	// identity: every read must still come from the validated location (codex-054).
	genuine, substitute := root(t), root(t)
	g := mustEnsure(t, genuine)
	mustEnsure(t, substitute)
	aliases := t.TempDir()
	alias := filepath.Join(aliases, "wd")
	if err := os.Symlink(genuine, alias); err != nil {
		t.Fatal(err)
	}
	saved := ownerOf
	swapped := false
	ownerOf = func(fi os.FileInfo) (int, bool) {
		if fi.Name() == CertName && !swapped { // the last metadata check before the reads
			swapped = true
			if err := os.Remove(alias); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(substitute, alias); err != nil {
				t.Fatal(err)
			}
		}
		return saved(fi)
	}
	t.Cleanup(func() { ownerOf = saved })

	id, err := Load(alias)
	if err != nil {
		t.Fatalf("Load through the alias: %v", err)
	}
	if !swapped {
		t.Fatal("the swap never happened: the test did not reach its point")
	}
	if id.Fingerprint != g.Fingerprint {
		t.Fatalf("Load returned the substitute identity %s after the alias was swapped (want %s)",
			id.Fingerprint, g.Fingerprint)
	}
}

func TestEnsure_ThroughAnAliasCreatesAtTheResolvedLocation(t *testing.T) {
	real := root(t)
	alias := filepath.Join(t.TempDir(), "wd")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	id := mustEnsure(t, alias)
	if want, _ := filepath.EvalSymlinks(filepath.Join(real, DirName)); id.Dir != want {
		t.Fatalf("identity directory = %s, want the resolved %s", id.Dir, want)
	}
}

// lockRefused asserts a first creation refuses a bad existing lock, naming it, and leaves
// everything (the lock included) unchanged with no identity created.
func lockRefused(t *testing.T, r, want string) {
	t.Helper()
	before := snapshot(t, r)
	if _, _, err := Ensure(r); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Ensure with a bad creation lock: %v; want a refusal containing %q", err, want)
	}
	if after := snapshot(t, r); after != before {
		t.Fatalf("a refused lock changed the files:\n%s\n---\n%s", before, after)
	}
}

func TestLock_RefusesASymlinkLock(t *testing.T) {
	r := root(t)
	elsewhere := filepath.Join(t.TempDir(), "someone-elses-lock")
	if err := os.WriteFile(elsewhere, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(r, lockFile)); err != nil {
		t.Fatal(err)
	}
	lockRefused(t, r, lockFile)
}

func TestLock_RefusesAWideModeLock(t *testing.T) {
	r := root(t)
	p := filepath.Join(r, lockFile)
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o666); err != nil {
		t.Fatal(err)
	}
	lockRefused(t, r, "want 0600")
}

func TestLock_RefusesAForeignLock(t *testing.T) {
	r := root(t)
	if err := os.WriteFile(filepath.Join(r, lockFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	saved := ownerOf
	ownerOf = func(fi os.FileInfo) (int, bool) {
		if fi.Name() == lockFile {
			return currentUID() + 4242, true
		}
		return saved(fi)
	}
	t.Cleanup(func() { ownerOf = saved })
	lockRefused(t, r, "is owned by uid")
}

func TestLock_RefusesANonRegularLock(t *testing.T) {
	t.Run("a named pipe (opens, but is not a regular file)", func(t *testing.T) {
		r := root(t)
		if err := syscall.Mkfifo(filepath.Join(r, lockFile), 0o600); err != nil {
			t.Fatal(err)
		}
		lockRefused(t, r, "is not a regular file")
	})
	t.Run("a directory (cannot be opened for writing)", func(t *testing.T) {
		r := root(t)
		if err := os.Mkdir(filepath.Join(r, lockFile), 0o700); err != nil {
			t.Fatal(err)
		}
		lockRefused(t, r, lockFile)
	})
}
