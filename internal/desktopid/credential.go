package desktopid

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// The operating credential (ADR 0102 ruling 1; W-0027 C2a): a per-install secret the desktop
// app presents so that smd can refuse every other client. It keeps out other users and other
// machines; a process of the same user that reads the file is inside the accepted limit.
//
// It has the identity's discipline, by the same code:
//   - It lives in its own directory beside the identity (the identity directory admits
//     exactly its two files), mode 0700, holding exactly one file, mode 0600, all owned by
//     the running user. No path component above it may be replaceable by another user.
//   - It is validated on every use and refused, never repaired or replaced, when anything is
//     wrong. Replacing it is a deliberate act: delete the directory; the next start makes a
//     new one.
//   - It is created as one unit (publishIfAbsent).
//
// The value is independent random bytes: it is not derived from the certificate, whose
// fingerprint is public to any client. No error, and no formatting of a Credential, ever
// prints it.
const (
	// CredentialDirName is the credential directory, under the working directory.
	CredentialDirName = "desktop-credential"
	// CredentialFileName is the only file the credential directory may hold. Its whole
	// content is the value a client presents: 43 base64url characters, nothing else.
	CredentialFileName = "credential"

	credTmpPrefix   = ".desktop-credential.tmp-"
	credentialBytes = 32
)

// credentialChars is the length of the file: credentialBytes in unpadded base64url.
var credentialChars = base64.RawURLEncoding.EncodedLen(credentialBytes)

var credentialUnit = unit{dir: CredentialDirName, tmpPrefix: credTmpPrefix, noun: "credential"}

// Credential is a validated operating credential. Its value is deliberately unexported and
// has no accessor: smd only compares against it.
type Credential struct {
	Dir   string // the credential directory
	value string // the text a client presents
}

// Matches reports whether presented is this credential, in constant time for a value of the
// right length. A Credential that was never loaded matches nothing, the empty string included.
func (c Credential) Matches(presented string) bool {
	if c.value == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(c.value)) == 1
}

// String and GoString keep the value out of every formatting verb.
func (c Credential) String() string   { return "desktop operating credential (value not shown)" }
func (c Credential) GoString() string { return c.String() }

// EnsureCredential returns the credential under root, creating it if there is none. An
// existing credential is validated and never regenerated or repaired.
func EnsureCredential(root string) (Credential, Durability, error) {
	return ensureCredentialWith(root, osFS{})
}

// LoadCredential validates and returns the existing credential under root.
func LoadCredential(root string) (Credential, error) {
	const op errors.Op = "desktopid.LoadCredential"
	real, err := trustedRoot(root)
	if err != nil {
		return Credential{}, errors.New(op).WithErr(err)
	}
	return loadCredentialAt(op, real)
}

func ensureCredentialWith(root string, fs fsOps) (Credential, Durability, error) {
	const op errors.Op = "desktopid.EnsureCredential"
	real, err := trustedRoot(root)
	if err != nil {
		return Credential{}, Durable, errors.New(op).WithErr(err)
	}
	durability, err := publishIfAbsent(op, real, credentialUnit, fs, func() ([]unitFile, error) {
		raw := make([]byte, credentialBytes)
		if _, rerr := rand.Read(raw); rerr != nil {
			return nil, rerr
		}
		return []unitFile{{CredentialFileName, []byte(base64.RawURLEncoding.EncodeToString(raw)), 0o600}}, nil
	})
	if err != nil {
		return Credential{}, Durable, err
	}
	c, err := loadCredentialAt(op, real) // existing or just published: validated, never recreated
	return c, durability, err
}

// loadCredentialAt validates and returns the credential under real, a root already resolved
// and checked by trustedRoot. Every error names a path and none carries any of the file's
// content.
func loadCredentialAt(op errors.Op, real string) (Credential, error) {
	dir := filepath.Join(real, CredentialDirName)
	if err := checkEntry(dir, "the credential directory", func(fi os.FileInfo) error {
		if !fi.IsDir() {
			return fmt.Errorf("%s is not a directory", dir)
		}
		if fi.Mode().Perm() != 0o700 {
			return fmt.Errorf("%s has mode %04o, want 0700", dir, fi.Mode().Perm())
		}
		return nil
	}); err != nil {
		return Credential{}, errors.New(op).WithErr(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Credential{}, errors.New(op).WithErr(err)
	}
	for _, e := range entries {
		if e.Name() != CredentialFileName {
			return Credential{}, errors.New(op).WithMsgf("unexpected file %s in the credential directory",
				filepath.Join(dir, e.Name()))
		}
	}
	path := filepath.Join(dir, CredentialFileName)
	malformed := fmt.Errorf("%s is not a valid credential: it must hold exactly %d base64url characters",
		path, credentialChars)
	if err := checkEntry(path, "the credential file", func(fi os.FileInfo) error {
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		if fi.Mode().Perm() != 0o600 {
			return fmt.Errorf("%s has mode %04o, want 0600", path, fi.Mode().Perm())
		}
		return nil
	}); err != nil {
		return Credential{}, errors.New(op).WithErr(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Credential{}, errors.New(op).WithErr(err)
	}
	if !canonicalCredential(b) {
		return Credential{}, errors.New(op).WithErr(malformed)
	}
	return Credential{Dir: dir, value: string(b)}, nil
}

// canonicalCredential reports whether b is exactly the unpadded base64url text of
// credentialBytes bytes. The length is checked on the raw bytes, so a line break the decoder
// would skip cannot hide in it, and the strict decoding refuses a final character whose unused
// bits are set: one secret has exactly one text.
func canonicalCredential(b []byte) bool {
	if len(b) != credentialChars {
		return false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(string(b))
	return err == nil && len(raw) == credentialBytes
}
