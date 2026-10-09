package smcloud

import (
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// IdentityTarget names the archive and logbook an adopted binding uploads to
// (ADR 0088, ADR 0090 T1), with the display values the server keeps beside
// them: the archive's catalogue label, the logbook's name and callsign.
type IdentityTarget struct {
	ArchiveUUID  string
	ArchiveLabel string
	LogbookUUID  string
	LogbookLabel string
	Callsign     string
}

// identityPutRequest mirrors the server's strict identity envelope
// (internal/cloud/server IdentityPutRequest): the display values and the
// QSOs, never the cloud logbook name.
type identityPutRequest struct {
	ArchiveLabel string      `json:"archive_label"`
	LogbookLabel string      `json:"logbook_label"`
	Callsign     string      `json:"callsign"`
	Qsos         []qsoUpload `json:"qsos"`
}

// NewIdentity constructs the identity forwarder of an adopted binding: the
// account validated as New validates it, every upload PUT to
// /v1/archives/{archive}/logbooks/{logbook}/qsos, never by name, and a 404
// kept pending as an unavailable endpoint (T5). A target without a valid
// archive or logbook UUIDv7 is refused; there is no fallback to the name.
func NewIdentity(fc types.ForwarderConfig, target IdentityTarget) (*Forwarder, error) {
	const op errors.Op = "smcloud.NewIdentity"
	archiveUUID, logbookUUID := strings.ToLower(target.ArchiveUUID), strings.ToLower(target.LogbookUUID)
	if !utils.IsValidUUIDv7(archiveUUID) || !utils.IsValidUUIDv7(logbookUUID) {
		return nil, errors.New(op).WithMsg("the adopted binding needs the archive's and the logbook's UUIDv7")
	}
	built, err := New(fc)
	if err != nil {
		return nil, errors.New(op).WithErr(err)
	}
	f := built.(*Forwarder)
	f.putURL = strings.TrimSuffix(f.putURL, "/v1/qsos") + "/v1/archives/" + archiveUUID + "/logbooks/" + logbookUUID + "/qsos"
	f.identity = true
	f.target = target
	return f, nil
}
