package desktopid

// W-0027 C2a: the operating credential. A per-install secret in its own protected directory,
// published as one unit, validated on every use, refused (never repaired) when anything is
// wrong, and never printed.

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func credPath(r string) string { return filepath.Join(r, CredentialDirName, CredentialFileName) }

func mustEnsureCredential(t *testing.T, r string) Credential {
	t.Helper()
	c, d, err := EnsureCredential(r)
	if err != nil {
		t.Fatalf("EnsureCredential: %v", err)
	}
	if d != Durable {
		t.Fatalf("EnsureCredential durability = %v, want Durable", d)
	}
	return c
}

// credValue reads the published value straight from the file, as a client does.
func credValue(t *testing.T, r string) string {
	t.Helper()
	b, err := os.ReadFile(credPath(r))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCredential_CreatesAProtectedCredential(t *testing.T) {
	r := root(t)
	c := mustEnsureCredential(t, r)

	dir := filepath.Join(r, CredentialDirName)
	fi, err := os.Lstat(dir)
	if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		t.Fatalf("credential directory: %v %v; want a 0700 directory", fi, err)
	}
	if c.Dir != dir {
		t.Errorf("Credential.Dir = %q, want %q", c.Dir, dir)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != CredentialFileName {
		t.Fatalf("credential directory holds %v; want exactly %s", entries, CredentialFileName)
	}
	file, err := os.Lstat(credPath(r))
	if err != nil || !file.Mode().IsRegular() || file.Mode().Perm() != 0o600 {
		t.Fatalf("credential file: %v %v; want a regular 0600 file", file, err)
	}

	v := credValue(t, r)
	raw, err := base64.RawURLEncoding.Strict().DecodeString(v)
	if err != nil || len(raw) != 32 || len(v) != 43 {
		t.Fatalf("the file holds %d characters decoding to %d bytes (%v); want 43 base64url characters for 32 bytes, nothing else",
			len(v), len(raw), err)
	}
	if !c.Matches(v) {
		t.Fatal("the credential does not match the value in its own file")
	}
}

func TestCredential_MatchesOnlyItsExactValue(t *testing.T) {
	r := root(t)
	c := mustEnsureCredential(t, r)
	v := credValue(t, r)
	flipped := "A" + v[1:]
	if v[0] == 'A' {
		flipped = "B" + v[1:]
	}
	for name, presented := range map[string]string{
		"empty":              "",
		"one character off":  flipped,
		"a prefix":           v[:42],
		"with a suffix":      v + "A",
		"with a newline":     v + "\n",
		"with a space":       " " + v,
		"two values, joined": v + ", " + v,
	} {
		if c.Matches(presented) {
			t.Errorf("the credential matched %s", name)
		}
	}
}

// A credential that was never loaded matches nothing, the empty string included: a server
// holding no credential must refuse, not accept an absent header.
func TestCredential_TheZeroValueMatchesNothing(t *testing.T) {
	var zero Credential
	for _, presented := range []string{"", "A", strings.Repeat("A", 43)} {
		if zero.Matches(presented) {
			t.Fatalf("the zero credential matched %q", presented)
		}
	}
}

func TestCredential_ReusesThePublishedCredential(t *testing.T) {
	r := root(t)
	mustEnsureCredential(t, r)
	v := credValue(t, r)
	before := snapshot(t, r)
	again := mustEnsureCredential(t, r)
	if !again.Matches(v) {
		t.Fatal("a second start made a different credential")
	}
	loaded, err := LoadCredential(r)
	if err != nil || !loaded.Matches(v) {
		t.Fatalf("LoadCredential: %v; want the published credential", err)
	}
	if after := snapshot(t, r); after != before {
		t.Fatalf("reusing the credential changed the files:\n%s\n---\n%s", before, after)
	}
}

func TestCredential_EachInstallHasItsOwn(t *testing.T) {
	a, b := root(t), root(t)
	mustEnsureCredential(t, a)
	mustEnsureCredential(t, b)
	if credValue(t, a) == credValue(t, b) {
		t.Fatal("two installs made the same credential")
	}
}

// The credential is its own unit beside the identity: neither disturbs the other, in either
// order, and the identity directory still holds exactly its two files.
func TestCredential_LivesBesideTheIdentity(t *testing.T) {
	for _, order := range []string{"identity first", "credential first"} {
		t.Run(order, func(t *testing.T) {
			r := root(t)
			if order == "identity first" {
				mustEnsure(t, r)
				mustEnsureCredential(t, r)
			} else {
				mustEnsureCredential(t, r)
				mustEnsure(t, r)
			}
			if _, err := Load(r); err != nil {
				t.Fatalf("the identity is refused beside the credential: %v", err)
			}
			if _, err := LoadCredential(r); err != nil {
				t.Fatalf("the credential is refused beside the identity: %v", err)
			}
			id := mustEnsure(t, r)
			if strings.Contains(credValue(t, r), id.Fingerprint) || strings.Contains(id.Fingerprint, credValue(t, r)) {
				t.Fatal("the credential and the certificate's fingerprint are not independent")
			}
		})
	}
}

// No formatting verb prints the value.
func TestCredential_IsNeverPrinted(t *testing.T) {
	r := root(t)
	c := mustEnsureCredential(t, r)
	v := credValue(t, r)
	for _, out := range []string{
		fmt.Sprintf("%v", c), fmt.Sprintf("%+v", c), fmt.Sprintf("%#v", c), fmt.Sprintf("%s", c),
		fmt.Sprintf("%v", &c), fmt.Sprintf("%+v", &c), fmt.Sprintf("%#v", &c), fmt.Sprint(c), fmt.Sprintf("%q", c),
	} {
		if strings.Contains(out, v) {
			t.Fatalf("formatting printed the credential: %s", out)
		}
	}
}

// credRefused asserts that EnsureCredential and LoadCredential refuse, name the given path,
// never print the published value or the file's present content, and change nothing.
func credRefused(t *testing.T, r, names string, secrets ...string) {
	t.Helper()
	before := snapshot(t, r)
	_, _, err := EnsureCredential(r)
	if err == nil {
		t.Fatal("EnsureCredential accepted a credential it must refuse")
	}
	_, lerr := LoadCredential(r)
	if lerr == nil {
		t.Fatal("LoadCredential accepted a credential it must refuse")
	}
	for _, e := range []error{err, lerr} {
		if !strings.Contains(e.Error(), names) {
			t.Errorf("error %q does not name %q", e, names)
		}
		for _, s := range secrets {
			if s != "" && strings.Contains(e.Error(), s) {
				t.Errorf("error %q prints the credential file's content", e)
			}
		}
	}
	if after := snapshot(t, r); after != before {
		t.Fatalf("a refusal changed the files (never repair):\n%s\n---\n%s", before, after)
	}
}

func TestCredentialRefuses_AMissingFile(t *testing.T) {
	r := root(t)
	mustEnsureCredential(t, r)
	if err := os.Remove(credPath(r)); err != nil {
		t.Fatal(err)
	}
	credRefused(t, r, credPath(r))
}

func TestCredentialRefuses_AnUnexpectedFile(t *testing.T) {
	r := root(t)
	mustEnsureCredential(t, r)
	v := credValue(t, r)
	if err := os.WriteFile(filepath.Join(r, CredentialDirName, "credential.bak"), []byte(v), 0o600); err != nil {
		t.Fatal(err)
	}
	credRefused(t, r, "credential.bak", v)
}

func TestCredentialRefuses_MalformedContent(t *testing.T) {
	good := strings.Repeat("A", 43)
	cases := map[string]string{
		"empty":                    "",
		"one character short":      good[:42],
		"one character long":       good + "A",
		"a trailing newline":       good + "\n",
		"a leading space":          " " + good[:42],
		"standard-alphabet '+'":    "+" + good[1:],
		"standard-alphabet '/'":    "/" + good[1:],
		"padding":                  good[:42] + "=",
		"non-canonical final bits": good[:42] + "B", // 43 characters whose unused bits are not zero
		"binary":                   string(make([]byte, 43)),
		"two lines":                good[:21] + "\n" + good[:21],
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			r := root(t)
			mustEnsureCredential(t, r)
			published := credValue(t, r)
			if err := os.WriteFile(credPath(r), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			secret := content
			if len(strings.TrimSpace(secret)) < 8 {
				secret = "" // too short to search an error for meaningfully
			}
			credRefused(t, r, credPath(r), published, secret)
		})
	}
}

func TestCredentialRefuses_UnsafeModes(t *testing.T) {
	cases := []struct {
		name, path string
		mode       os.FileMode
	}{
		{"credential directory not 0700", CredentialDirName, 0o755},
		{"credential directory group-searchable", CredentialDirName, 0o710},
		{"file readable by group", filepath.Join(CredentialDirName, CredentialFileName), 0o640},
		{"file readable by others", filepath.Join(CredentialDirName, CredentialFileName), 0o604},
		{"file executable (not exactly 0600)", filepath.Join(CredentialDirName, CredentialFileName), 0o700},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := root(t)
			mustEnsureCredential(t, r)
			v := credValue(t, r)
			if err := os.Chmod(filepath.Join(r, c.path), c.mode); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(filepath.Join(r, CredentialDirName), 0o700) })
			credRefused(t, r, filepath.Join(r, c.path), v)
		})
	}
}

func TestCredentialRefuses_AnAncestorAnotherUserCouldReplace(t *testing.T) {
	r := root(t)
	mustEnsureCredential(t, r)
	v := credValue(t, r)
	if err := os.Chmod(r, 0o770); err != nil {
		t.Fatal(err)
	}
	credRefused(t, r, r, v)
}

func TestCredentialRefuses_ForeignOwnership(t *testing.T) {
	r := root(t)
	mustEnsureCredential(t, r)
	v := credValue(t, r)
	saved := currentUID
	currentUID = func() int { return saved() + 4242 }
	t.Cleanup(func() { currentUID = saved })
	credRefused(t, r, "owned by", v)
}

func TestCredentialRefuses_AFileOwnedByAnotherUser(t *testing.T) {
	r := root(t)
	mustEnsureCredential(t, r)
	v := credValue(t, r)
	saved := ownerOf
	ownerOf = func(fi os.FileInfo) (int, bool) {
		if fi.Name() == CredentialFileName {
			return currentUID() + 4242, true
		}
		return saved(fi)
	}
	t.Cleanup(func() { ownerOf = saved })
	credRefused(t, r, credPath(r), v)
}

func TestCredentialRefuses_Symlinks(t *testing.T) {
	t.Run("the directory is a symlink", func(t *testing.T) {
		r, elsewhere := root(t), root(t)
		mustEnsureCredential(t, elsewhere)
		v := credValue(t, elsewhere)
		if err := os.Symlink(filepath.Join(elsewhere, CredentialDirName), filepath.Join(r, CredentialDirName)); err != nil {
			t.Fatal(err)
		}
		credRefused(t, r, filepath.Join(r, CredentialDirName), v)
	})
	t.Run("the file is a symlink", func(t *testing.T) {
		r := root(t)
		mustEnsureCredential(t, r)
		v := credValue(t, r)
		target := filepath.Join(r, "elsewhere")
		if err := os.Rename(credPath(r), target); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, credPath(r)); err != nil {
			t.Fatal(err)
		}
		credRefused(t, r, credPath(r), v)
	})
}

func TestCredentialRefuses_ANonRegularFile(t *testing.T) {
	r := root(t)
	mustEnsureCredential(t, r)
	if err := os.Remove(credPath(r)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(credPath(r), 0o700); err != nil {
		t.Fatal(err)
	}
	credRefused(t, r, CredentialFileName+" is not a regular file")
}

func TestCredentialRefuses_ADirectoryThatIsAFile(t *testing.T) {
	r := root(t)
	if err := os.WriteFile(filepath.Join(r, CredentialDirName), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	credRefused(t, r, filepath.Join(r, CredentialDirName))
}

// --- creation barriers -------------------------------------------------------------

var credBarrierOrder = []string{
	"write " + CredentialFileName, "sync " + CredentialFileName,
	"sync prepared dir", "rename", "sync parent",
}

func TestCredentialCreate_BarriersInOrder(t *testing.T) {
	r := root(t)
	f := &faultFS{}
	if _, d, err := ensureCredentialWith(r, f); err != nil || d != Durable {
		t.Fatalf("ensure: %v %v", d, err)
	}
	if strings.Join(f.calls, ", ") != strings.Join(credBarrierOrder, ", ") {
		t.Fatalf("barriers = %v\nwant       %v", f.calls, credBarrierOrder)
	}
}

func TestCredentialCreate_AFailureBeforeTheRenamePublishesNothing(t *testing.T) {
	for _, step := range credBarrierOrder[:4] { // every step up to and including the rename
		t.Run(step, func(t *testing.T) {
			r := root(t)
			f := &faultFS{failAt: step}
			if _, _, err := ensureCredentialWith(r, f); err == nil {
				t.Fatal("a failure before publication was not reported")
			}
			if _, serr := os.Lstat(filepath.Join(r, CredentialDirName)); !os.IsNotExist(serr) {
				t.Fatalf("a credential was published despite a failure at %q", step)
			}
			if _, lerr := LoadCredential(r); lerr == nil {
				t.Fatal("LoadCredential found a credential that was never published")
			}
			mustEnsureCredential(t, r) // the next start creates a complete one
			entries, _ := os.ReadDir(r)
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), credTmpPrefix) {
					t.Errorf("unpublished remnant %q left behind after the next start", e.Name())
				}
			}
		})
	}
}

func TestCredentialCreate_AFailedParentSyncIsPublishedButUnconfirmed(t *testing.T) {
	r := root(t)
	f := &faultFS{failAt: "sync parent"}
	c, d, err := ensureCredentialWith(r, f)
	if err != nil {
		t.Fatalf("a failed final sync must not be reported as a failure: %v", err)
	}
	if d != DurabilityUnconfirmed {
		t.Fatalf("durability = %v, want DurabilityUnconfirmed", d)
	}
	v := credValue(t, r)
	if !c.Matches(v) {
		t.Fatal("the unconfirmed credential is not the published one")
	}
	if again := mustEnsureCredential(t, r); !again.Matches(v) {
		t.Fatal("the published credential was replaced after an unconfirmed sync (never silently replaced)")
	}
}

func TestCredentialCreate_RemovesOnlyItsOwnRemnant(t *testing.T) {
	r := root(t)
	remnant := filepath.Join(r, credTmpPrefix+"123456")
	if err := os.Mkdir(remnant, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remnant, CredentialFileName), []byte("half"), 0o600); err != nil {
		t.Fatal(err)
	}
	kept := []string{
		filepath.Join(r, "desktop-credential.tmp-not-ours"),
		filepath.Join(r, "desktop-credential-backup"),
		filepath.Join(r, "config.json"),
	}
	for _, p := range kept[:2] {
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(kept[2], []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustEnsureCredential(t, r)
	if _, err := os.Lstat(remnant); !os.IsNotExist(err) {
		t.Errorf("the unpublished remnant %s was not removed", remnant)
	}
	for _, p := range kept {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("%s (not one of ours) was disturbed: %v", p, err)
		}
	}
}

func TestCredentialCreate_ConcurrentCreatorsShareOneCredential(t *testing.T) {
	r := root(t)
	const n = 8
	type result struct {
		c   Credential
		err error
	}
	out := make(chan result, n)
	for i := 0; i < n; i++ {
		go func() {
			c, _, err := EnsureCredential(r)
			out <- result{c, err}
		}()
	}
	var got []Credential
	for i := 0; i < n; i++ {
		res := <-out
		if res.err != nil {
			t.Fatalf("a concurrent creator failed: %v", res.err)
		}
		got = append(got, res.c)
	}
	v := credValue(t, r)
	for i, c := range got {
		if !c.Matches(v) {
			t.Fatalf("creator %d holds a credential other than the published one", i)
		}
	}
	entries, _ := os.ReadDir(r)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), credTmpPrefix) {
			t.Errorf("a temporary directory %q was left behind", e.Name())
		}
	}
}

func TestCredentialCreate_NeverRemovesACreationInProgress(t *testing.T) {
	r := root(t)
	unlock, err := lockCreation(r)
	if err != nil {
		t.Fatal(err)
	}
	inProgress := filepath.Join(r, credTmpPrefix+"in-progress")
	if err := os.Mkdir(inProgress, 0o700); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := EnsureCredential(r)
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Lstat(inProgress); err != nil {
		t.Fatalf("the other creator's directory in progress was removed while it held the lock: %v", err)
	}
	select {
	case err := <-done:
		t.Fatalf("EnsureCredential did not wait for the creation lock (returned %v)", err)
	default:
	}
	unlock()
	if err := <-done; err != nil {
		t.Fatalf("EnsureCredential after the lock was released: %v", err)
	}
}

func TestCredentialCreate_NeverPublishesOverAnExistingCredential(t *testing.T) {
	r, other := root(t), root(t)
	mustEnsureCredential(t, other)
	f := &faultFS{}
	var planted string
	hook := &renameHook{faultFS: f, before: func() {
		if err := os.Rename(filepath.Join(other, CredentialDirName), filepath.Join(r, CredentialDirName)); err != nil {
			t.Fatal(err)
		}
		planted = snapshot(t, filepath.Join(r, CredentialDirName))
	}}
	if _, _, err := ensureCredentialWith(r, hook); err == nil {
		t.Fatal("publication over an existing credential was not refused")
	}
	if got := snapshot(t, filepath.Join(r, CredentialDirName)); got != planted {
		t.Fatalf("the existing credential was changed by the second publication:\n%s\n---\n%s", planted, got)
	}
}

func TestCredentialLoad_UsesTheResolvedPathNotASwappableAlias(t *testing.T) {
	// The caller's root is a symlink. After validation, someone swaps it to point at another
	// install's credential: the read must still come from the validated location.
	genuine, substitute := root(t), root(t)
	mustEnsureCredential(t, genuine)
	mustEnsureCredential(t, substitute)
	want := credValue(t, genuine)
	alias := filepath.Join(t.TempDir(), "wd")
	if err := os.Symlink(genuine, alias); err != nil {
		t.Fatal(err)
	}
	saved := ownerOf
	swapped := false
	ownerOf = func(fi os.FileInfo) (int, bool) {
		if fi.Name() == CredentialFileName && !swapped { // the last metadata check before the read
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

	c, err := LoadCredential(alias)
	if err != nil {
		t.Fatalf("LoadCredential through the alias: %v", err)
	}
	if !swapped {
		t.Fatal("the swap never happened: the test did not reach its point")
	}
	if !c.Matches(want) {
		t.Fatal("LoadCredential returned the substitute credential after the alias was swapped")
	}
}

func TestCredentialEnsure_ThroughAnAliasCreatesAtTheResolvedLocation(t *testing.T) {
	real := root(t)
	alias := filepath.Join(t.TempDir(), "wd")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	c := mustEnsureCredential(t, alias)
	if want, _ := filepath.EvalSymlinks(filepath.Join(real, CredentialDirName)); c.Dir != want {
		t.Fatalf("credential directory = %s, want the resolved %s", c.Dir, want)
	}
}
