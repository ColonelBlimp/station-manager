package smcloud

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// accountFingerprintVersion opens the fingerprint's message; a change to the
// encoding takes a new version.
const accountFingerprintVersion = "sm-adoption-account/v1"

// AccountFingerprint identifies the station account an adoption is confirmed
// under (ADR 0091): HMAC-SHA256 keyed by the token over the version, the
// archive UUID (lower case) and the URL as the client sends to it (trimmed,
// trailing "/" removed), each prefixed by its length, as hex. The archive UUID
// is public, so it is never the key. A replaced account, a rotated token or an
// edited URL gives another fingerprint; a trailing slash does not. The value is
// a verifier derived from the token: it is stored and compared, never served.
// An account without a URL or a token, or no archive, is an error that names
// no value.
func AccountFingerprint(archiveID string, station json.RawMessage) (string, error) {
	const op errors.Op = "smcloud.AccountFingerprint"
	var creds credentials
	if len(station) == 0 || json.Unmarshal(station, &creds) != nil {
		return "", errors.New(op).WithMsg("the station account's credentials cannot be read")
	}
	archive := strings.ToLower(strings.TrimSpace(archiveID))
	url := strings.TrimRight(strings.TrimSpace(creds.URL), "/")
	switch {
	case archive == "":
		return "", errors.New(op).WithMsg("no archive UUID")
	case url == "":
		return "", errors.New(op).WithMsg("the station account has no url")
	case creds.Token == "":
		return "", errors.New(op).WithMsg("the station account has no token")
	}
	var msg []byte
	for _, field := range []string{accountFingerprintVersion, archive, url} {
		msg = binary.BigEndian.AppendUint64(msg, uint64(len(field)))
		msg = append(msg, field...)
	}
	mac := hmac.New(sha256.New, []byte(creds.Token))
	mac.Write(msg)
	return hex.EncodeToString(mac.Sum(nil)), nil
}
