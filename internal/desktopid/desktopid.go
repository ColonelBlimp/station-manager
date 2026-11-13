// Package desktopid keeps smd's TLS identity for the desktop app (ADR 0103 point 1; W-0027
// C1b): a self-signed ECDSA P-256 certificate for 127.0.0.1 and its private key, which the
// desktop shell pins by the SHA-256 of the certificate's DER encoding.
//
// The certificate is the pin, so its location is as protected as the key's:
//   - The identity lives in a dedicated directory under the working directory, mode 0700,
//     holding exactly key.pem (0600) and cert.pem (not group/other writable), all owned by
//     the running user. No path component above it may be replaceable by another user.
//   - It is validated on every use. Anything else (a partial, mismatched or malformed pair,
//     an unexpected file, a symlink, a foreign owner, an unsafe mode) is refused with an
//     error naming the path. It is never repaired or replaced: replacing the identity is a
//     deliberate act (delete the directory; the next start creates a new one).
//   - It is created as one unit: written and synced in a private temporary directory, which
//     is itself synced, published with a single rename, and the parent synced. A failure
//     before the rename publishes nothing (the remnant is removed at the next start); if only
//     the final sync fails, the identity is published with its durability unconfirmed.
//
// The shell never opens the private key; only this package does (ADR 0103).
package desktopid

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

const (
	// DirName is the identity directory, under the working directory.
	DirName = "desktop-identity"
	// KeyName and CertName are the only files the identity directory may hold.
	KeyName  = "key.pem"
	CertName = "cert.pem"

	tmpPrefix = ".desktop-identity.tmp-"
	validity  = 10 // years (operator ruling 2026-11-10; dates are not enforced against the pin)
)

// Durability reports whether a newly created identity's publication is known to be durable.
type Durability int

const (
	// Durable: the identity existed already, or was created with every barrier confirmed.
	Durable Durability = iota
	// DurabilityUnconfirmed: the identity is published and valid, but the final sync of its
	// parent directory failed, so it may not survive a power loss. The caller reports it; the
	// identity is never silently replaced.
	DurabilityUnconfirmed
)

func (d Durability) String() string {
	if d == DurabilityUnconfirmed {
		return "unconfirmed"
	}
	return "durable"
}

// Identity is a validated identity, ready to serve TLS.
type Identity struct {
	Dir         string          // the identity directory
	CertDER     []byte          // the certificate, DER-encoded
	Fingerprint string          // SHA-256 of CertDER, upper-case hex pairs joined by colons
	TLS         tls.Certificate // certificate and key, for a TLS listener
}

// currentUID is the owner every identity path must have (root is also accepted for
// ancestors). A variable so a test can make every path appear foreign.
var currentUID = os.Geteuid

// Fingerprint returns the SHA-256 of a DER-encoded certificate in the shell's format
// (Node's X509Certificate.fingerprint256: upper-case hex pairs joined by colons).
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":")
}

// Ensure returns the identity under root, creating it if there is none. An existing identity
// is validated and never regenerated or repaired.
func Ensure(root string) (Identity, Durability, error) {
	return ensureWith(root, osFS{}, time.Now())
}

// Load validates and returns the existing identity under root.
func Load(root string) (Identity, error) {
	const op errors.Op = "desktopid.Load"
	real, err := trustedRoot(root)
	if err != nil {
		return Identity{}, errors.New(op).WithErr(err)
	}
	return loadAt(op, real)
}

// loadAt validates and returns the identity under real, a root already resolved and checked by
// trustedRoot. Every access goes through real, never through the caller's original path.
func loadAt(op errors.Op, real string) (Identity, error) {
	dir := filepath.Join(real, DirName)
	if err := checkEntry(dir, "the identity directory", func(fi os.FileInfo) error {
		if !fi.IsDir() {
			return fmt.Errorf("%s is not a directory", dir)
		}
		if fi.Mode().Perm() != 0o700 {
			return fmt.Errorf("%s has mode %04o, want 0700", dir, fi.Mode().Perm())
		}
		return nil
	}); err != nil {
		return Identity{}, errors.New(op).WithErr(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return Identity{}, errors.New(op).WithErr(err)
	}
	for _, e := range entries {
		if e.Name() != KeyName && e.Name() != CertName {
			return Identity{}, errors.New(op).WithMsgf("unexpected file %s in the identity directory",
				filepath.Join(dir, e.Name()))
		}
	}
	keyPath, certPath := filepath.Join(dir, KeyName), filepath.Join(dir, CertName)
	for _, f := range []struct {
		path  string
		check func(os.FileInfo) error
	}{
		{keyPath, func(fi os.FileInfo) error {
			if fi.Mode().Perm() != 0o600 {
				return fmt.Errorf("%s has mode %04o, want 0600", keyPath, fi.Mode().Perm())
			}
			return nil
		}},
		{certPath, func(fi os.FileInfo) error {
			if fi.Mode().Perm()&0o022 != 0 {
				return fmt.Errorf("%s has mode %04o: writable by group or others", certPath, fi.Mode().Perm())
			}
			return nil
		}},
	} {
		if err := checkEntry(f.path, "an identity file", func(fi os.FileInfo) error {
			if !fi.Mode().IsRegular() {
				return fmt.Errorf("%s is not a regular file", f.path)
			}
			return f.check(fi)
		}); err != nil {
			return Identity{}, errors.New(op).WithErr(err)
		}
	}

	return parsePair(op, keyPath, certPath, dir)
}

// parsePair reads, parses and matches the key and certificate.
func parsePair(op errors.Op, keyPath, certPath, dir string) (Identity, error) {
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return Identity{}, errors.New(op).WithErr(err)
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return Identity{}, errors.New(op).WithErr(err)
	}
	cb, _ := pem.Decode(certPEM)
	if cb == nil || cb.Type != "CERTIFICATE" {
		return Identity{}, errors.New(op).WithMsgf("%s is not a PEM certificate", certPath)
	}
	if _, err := x509.ParseCertificate(cb.Bytes); err != nil {
		return Identity{}, errors.New(op).WithMsgf("%s is not a valid certificate: %v", certPath, err)
	}
	kb, _ := pem.Decode(keyPEM)
	if kb == nil || kb.Type != "PRIVATE KEY" {
		return Identity{}, errors.New(op).WithMsgf("%s is not a PEM private key", keyPath)
	}
	if _, err := x509.ParsePKCS8PrivateKey(kb.Bytes); err != nil {
		return Identity{}, errors.New(op).WithMsgf("%s is not a valid private key: %v", keyPath, err)
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return Identity{}, errors.New(op).WithMsgf("%s and %s do not belong together: %v",
			keyPath, certPath, err)
	}
	return Identity{Dir: dir, CertDER: cb.Bytes, Fingerprint: Fingerprint(cb.Bytes), TLS: pair}, nil
}

// checkEntry refuses a symlink, a missing path or a foreign owner, then applies check.
func checkEntry(path, what string, check func(os.FileInfo) error) error {
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("%s %s is missing", what, path)
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink; %s must not be one", path, what)
	}
	if uid, ok := ownerOf(fi); !ok || uid != currentUID() {
		return fmt.Errorf("%s is owned by uid %d, not the running user (%d)", path, uid, currentUID())
	}
	return check(fi)
}

// trustedRoot resolves root once and checks the resolved path, which every later access then
// uses (codex-054): a symlink in the caller's path is followed only here, so swapping it after
// validation cannot redirect a read. Every directory from the resolved root up to / must be owned
// by the running user or root, and may be writable by group or others only with the sticky bit
// (as /tmp is), which stops others renaming or removing entries they do not own. With no symlink
// left in it, no other user can then replace any part of that path.
func trustedRoot(root string) (string, error) {
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	real, err = filepath.Abs(real)
	if err != nil {
		return "", err
	}
	return real, checkAncestors(real)
}

// checkAncestors checks every directory from p (already resolved) up to /.
func checkAncestors(p string) error {
	for {
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if uid, ok := ownerOf(fi); !ok || (uid != currentUID() && uid != 0) {
			return fmt.Errorf("%s is owned by uid %d, neither the running user (%d) nor root", p, uid, currentUID())
		}
		if fi.Mode().Perm()&0o022 != 0 && fi.Mode()&os.ModeSticky == 0 {
			return fmt.Errorf("%s is writable by group or others without the sticky bit: "+
				"another user could replace the identity", p)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return nil
		}
		p = parent
	}
}

// ownerOf reports a file's owner. A variable so a test can make one path appear to belong to
// another user (changing real ownership needs root).
var ownerOf = statOwner

func statOwner(fi os.FileInfo) (int, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return -1, false
	}
	return int(st.Uid), true
}

func ensureWith(root string, fs fsOps, now time.Time) (Identity, Durability, error) {
	const op errors.Op = "desktopid.Ensure"
	real, err := trustedRoot(root)
	if err != nil {
		return Identity{}, Durable, errors.New(op).WithErr(err)
	}
	// From here on, only the resolved and checked path is used.
	durability, err := publishIfAbsent(op, real, identityUnit, fs, func() ([]unitFile, error) {
		keyPEM, certPEM, gerr := generate(now)
		if gerr != nil {
			return nil, gerr
		}
		return []unitFile{{KeyName, keyPEM, 0o600}, {CertName, certPEM, 0o644}}, nil
	})
	if err != nil {
		return Identity{}, Durable, err
	}
	id, err := loadAt(op, real) // existing or just published: validated, never recreated
	return id, durability, err
}

// unit is one protected directory this package publishes under the working directory: the
// identity, and the operating credential beside it. Both are created by publishIfAbsent, so
// they share one creation discipline rather than two copies of it.
type unit struct {
	dir       string // the published directory's name
	tmpPrefix string // the prefix of its prepared, not yet published, directories
	noun      string // what an error calls it
}

var identityUnit = unit{dir: DirName, tmpPrefix: tmpPrefix, noun: "identity"}

// unitFile is one file of a unit, written with exactly this mode.
type unitFile struct {
	name string
	data []byte
	perm os.FileMode
}

// publishIfAbsent creates u under root, a path already resolved and checked by trustedRoot,
// unless it is there already. It never validates, repairs or replaces what exists: the caller
// loads the unit afterwards, whichever way it came to exist.
//
// Creation is serialised by an exclusive lock, so a second creator never removes a first
// creator's directory in progress and never publishes over its result: under the lock it
// finds the published unit and leaves it. The content is made only once creation is certain.
// The unit is written and synced in a private temporary directory, published with a single
// rename, and the parent synced. A failure before the rename publishes nothing; if only the
// final sync fails, the unit is published with its durability unconfirmed.
func publishIfAbsent(op errors.Op, root string, u unit, fs fsOps, content func() ([]unitFile, error)) (Durability, error) {
	dir := filepath.Join(root, u.dir)
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		return Durable, nil // exists (or unreadable): the caller validates it
	}
	unlock, err := lockCreation(root)
	if err != nil {
		return Durable, errors.New(op).WithErr(err)
	}
	defer unlock()
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		return Durable, nil
	}
	removeRemnants(root, u.tmpPrefix) // safe under the lock: no other creation is in progress

	files, err := content()
	if err != nil {
		return Durable, errors.New(op).WithErr(err)
	}
	tmp, err := os.MkdirTemp(root, u.tmpPrefix) // created 0700
	if err != nil {
		return Durable, errors.New(op).WithErr(err)
	}
	notPublished := "the " + u.noun + " was not published"
	if err := prepare(fs, tmp, files); err != nil {
		_ = os.RemoveAll(tmp)
		return Durable, errors.New(op).WithMsg(notPublished).WithErr(err)
	}
	if err := fs.Rename(tmp, dir); err != nil {
		_ = os.RemoveAll(tmp)
		return Durable, errors.New(op).WithMsg(notPublished).WithErr(err)
	}
	if err := fs.SyncDir(root); err != nil {
		return DurabilityUnconfirmed, nil // published and valid; only its durability is in doubt
	}
	return Durable, nil
}

// prepare writes and syncs each file, then syncs the prepared directory, in that order.
func prepare(fs fsOps, tmp string, files []unitFile) error {
	for _, f := range files {
		p := filepath.Join(tmp, f.name)
		if err := fs.WriteFile(p, f.data, f.perm); err != nil {
			return err
		}
		if err := fs.SyncFile(p); err != nil {
			return err
		}
	}
	return fs.SyncDir(tmp)
}

// lockFile is held, exclusively, for the whole of a creation. The identity and the operating
// credential share it: each creation is brief, and one lock means neither can remove the
// other's work in progress.
const lockFile = ".desktop-identity.lock"

// lockCreation takes the creation lock under root (an owner-only file, created if needed) and
// returns its release. It blocks while another creator holds it; creation is brief. An existing
// lock is checked before it is used (codex-054): it is opened without following a symlink, and
// the opened file must be a regular file owned by the running user with mode 0600, or it is
// refused unchanged; another user cannot then make Ensure wait on a lock they planted.
func lockCreation(root string) (func(), error) {
	path := filepath.Join(root, lockFile)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("the creation lock %s: %w", path, err)
	}
	fi, err := f.Stat()
	if err == nil {
		err = checkLock(path, fi)
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

func checkLock(path string, fi os.FileInfo) error {
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("the creation lock %s is not a regular file", path)
	}
	if uid, ok := ownerOf(fi); !ok || uid != currentUID() {
		return fmt.Errorf("the creation lock %s is owned by uid %d, not the running user (%d)", path, uid, currentUID())
	}
	if fi.Mode().Perm() != 0o600 {
		return fmt.Errorf("the creation lock %s has mode %04o, want 0600", path, fi.Mode().Perm())
	}
	return nil
}

// removeRemnants removes one unit's unpublished temporary directories that this user left
// behind (a crash or failure before the rename). It runs only under the creation lock, so it
// never removes a creation in progress. Nothing else is touched.
func removeRemnants(root, tmpPrefix string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), tmpPrefix) || !e.IsDir() {
			continue
		}
		p := filepath.Join(root, e.Name())
		if fi, err := os.Lstat(p); err == nil {
			if uid, ok := ownerOf(fi); ok && uid == currentUID() {
				_ = os.RemoveAll(p)
			}
		}
	}
}

// generate makes a P-256 key and a self-signed certificate for 127.0.0.1, valid for ten years.
func generate(now time.Time) (keyPEM, certPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Station Manager desktop identity"},
		NotBefore:             now,
		NotAfter:              now.AddDate(validity, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	pk, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}
