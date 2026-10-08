package smcloud

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/forwarding"
)

// The account a confirmation belongs to (ADR 0091, W-0021 5F.3 commit 4b1):
// HMAC-SHA256 keyed by the station token over a versioned, length-prefixed
// encoding of the archive UUID and the URL the client sends to.

const fpArchive = "019fd5c5-efcc-7193-be4f-1fee532ee3a1"

func station(url, token string) json.RawMessage {
	raw, _ := json.Marshal(map[string]string{"url": url, "token": token})
	return raw
}

func mustFingerprint(t *testing.T, archive string, creds json.RawMessage) string {
	t.Helper()
	fp, err := AccountFingerprint(archive, creds)
	if err != nil {
		t.Fatalf("AccountFingerprint: %v", err)
	}
	return fp
}

func TestAccountFingerprint_IsTheDocumentedEncoding(t *testing.T) {
	var msg []byte
	for _, field := range []string{"sm-adoption-account/v1", fpArchive, "https://cloud.example"} {
		msg = binary.BigEndian.AppendUint64(msg, uint64(len(field)))
		msg = append(msg, field...)
	}
	mac := hmac.New(sha256.New, []byte("secret-token"))
	mac.Write(msg)
	want := hex.EncodeToString(mac.Sum(nil))
	if got := mustFingerprint(t, fpArchive, station("https://cloud.example", "secret-token")); got != want {
		t.Fatalf("fingerprint = %s; want %s", got, want)
	}
}

func TestAccountFingerprint_DistinguishesAccounts(t *testing.T) {
	base := mustFingerprint(t, fpArchive, station("https://cloud.example", "tok-a"))
	for name, c := range map[string]struct {
		archive string
		creds   json.RawMessage
		same    bool
	}{
		"another token (rotation)":        {fpArchive, station("https://cloud.example", "tok-b"), false},
		"another URL (replacement)":       {fpArchive, station("https://other.example", "tok-a"), false},
		"another archive":                 {"019fd5c5-efcc-7193-be4f-1fee532ee3a2", station("https://cloud.example", "tok-a"), false},
		"a trailing slash and spaces":     {fpArchive, station("  https://cloud.example/ ", "tok-a"), true},
		"the archive UUID in upper case":  {" 019FD5C5-EFCC-7193-BE4F-1FEE532EE3A1", station("https://cloud.example", "tok-a"), true},
		"a padded token is another token": {fpArchive, station("https://cloud.example", " tok-a"), false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := mustFingerprint(t, c.archive, c.creds); (got == base) != c.same {
				t.Fatalf("fingerprint equal = %v; want %v", got == base, c.same)
			}
		})
	}
	t.Run("field boundaries cannot shift", func(t *testing.T) {
		a := mustFingerprint(t, "ab", station("c", "tok"))
		b := mustFingerprint(t, "a", station("bc", "tok"))
		if a == b {
			t.Fatal(`("ab","c") and ("a","bc") share a fingerprint`)
		}
	})
	t.Run("not keyed by the public archive UUID", func(t *testing.T) {
		mac := hmac.New(sha256.New, []byte(fpArchive))
		mac.Write([]byte("tok-a"))
		if hex.EncodeToString(mac.Sum(nil)) == base {
			t.Fatal("the fingerprint is keyed by the archive UUID")
		}
	})
}

func TestAccountFingerprint_RefusesAnIncompleteAccount(t *testing.T) {
	for name, c := range map[string]struct {
		archive string
		creds   json.RawMessage
	}{
		"no token":       {fpArchive, station("https://cloud.example", "")},
		"no URL":         {fpArchive, station("  ", "tok")},
		"no archive":     {" ", station("https://cloud.example", "tok")},
		"not an object":  {fpArchive, json.RawMessage(`"tok"`)},
		"no credentials": {fpArchive, nil},
	} {
		t.Run(name, func(t *testing.T) {
			if fp, err := AccountFingerprint(c.archive, c.creds); err == nil || fp != "" {
				t.Fatalf("AccountFingerprint = %q, %v; want an error", fp, err)
			}
		})
	}
	t.Run("the error never carries the token", func(t *testing.T) {
		_, err := AccountFingerprint(" ", station("https://cloud.example", "hunter2-secret"))
		if err == nil {
			t.Fatal("want an error")
		}
		if strings.Contains(err.Error(), "hunter2-secret") {
			t.Fatalf("error carries the token: %v", err)
		}
	})
}

func TestAccountFingerprint_IsRegisteredForTheType(t *testing.T) {
	fn, ok := forwarding.AccountFingerprintFor(Type)
	if !ok {
		t.Fatal("no account fingerprint registered for smcloud")
	}
	creds := station("https://cloud.example", "tok-a")
	got, err := fn(fpArchive, creds)
	if err != nil || got != mustFingerprint(t, fpArchive, creds) {
		t.Fatalf("registered fingerprint = %q, %v; want AccountFingerprint's", got, err)
	}
}
