package smcloud

import (
	"context"
	"encoding/json"
	stderr "errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/securehttp"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// maxExportBytes caps the export download. A 10k-QSO logbook is ~15 MB of
// JSON; 256 MB bounds a hostile/misrouted response without ever constraining
// a real log.
const maxExportBytes = 256 << 20

// ExportLogbook is one cloud-side logbook in an export.
type ExportLogbook struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// ExportRecord is one QSO of the export dump: the verbatim payload plus the
// storage-row facts restore preserves (mirrors the cloud server's ExportQso —
// declared locally for the same boundary reason as the PUT envelope).
type ExportRecord struct {
	UUID       string          `json:"uuid"`
	LogbookID  int64           `json:"logbook_id"`
	ModifiedAt time.Time       `json:"modified_at"`
	Revision   int64           `json:"revision,omitempty"`
	DeletedAt  *time.Time      `json:"deleted_at,omitempty"`
	Qso        json.RawMessage `json:"qso"`
}

// Export is the GET /v1/export response — everything the tenant owns.
type Export struct {
	Logbooks []ExportLogbook `json:"logbooks"`
	Qsos     []ExportRecord  `json:"qsos"`
}

// FetchExport pulls the tenant's full export from the smcloud service named
// by fc (the same url/token credentials the forwarder runs on — enabled or
// not; restore only reads). The restore command (smd restore) filters the
// result to its cloud logbook and feeds qsoservice.Restore.
func FetchExport(ctx context.Context, fc types.ForwarderConfig) (*Export, error) {
	const op errors.Op = "smcloud.FetchExport"

	f, err := New(fc) // reuse credential parsing + validation
	if err != nil {
		return nil, errors.New(op).WithErr(err)
	}
	fwd := f.(*Forwarder)
	url := strings.TrimSuffix(fwd.putURL, "/v1/qsos") + "/v1/export"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("build request")
	}
	req.Header.Set("Authorization", "Bearer "+fwd.token)
	req.Header.Set("User-Agent", UserAgent)

	// A full export on a slow link outlasts the forwarder's per-QSO timeout;
	// the caller's ctx is the real bound.
	client := securehttp.NewClient(10 * time.Minute)
	resp, err := securehttp.Do(client, req)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("GET /v1/export")
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxExportBytes))
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("read export")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(op).WithMsgf("GET /v1/export: HTTP %d (body: %s)",
			resp.StatusCode, bodySnippet(body, errorSnippetLen))
	}
	var out Export
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("parse export")
	}
	return &out, nil
}

// CloudLogbookName returns the logbook name the fc credentials target
// (DefaultLogbook when unset) — the restore command's default filter.
func CloudLogbookName(fc types.ForwarderConfig) (string, error) {
	const op errors.Op = "smcloud.CloudLogbookName"
	f, err := New(fc)
	if err != nil {
		return "", errors.New(op).WithErr(err)
	}
	return f.(*Forwarder).logbook, nil
}

// ScopedExportArchive and ScopedExportLogbook are the scoped export's head:
// the cloud archive and logbook the rows belong to, by UUID.
type ScopedExportArchive struct {
	UUID  string `json:"uuid"`
	Label string `json:"label"`
}

type ScopedExportLogbook struct {
	UUID     string `json:"uuid"`
	Label    string `json:"label"`
	Callsign string `json:"callsign"`
}

// ScopedExportRecord is one QSO of a scoped export: the verbatim payload and
// the storage facts restore keeps. It names no numeric logbook id; the head
// names the logbook.
type ScopedExportRecord struct {
	UUID       string          `json:"uuid"`
	ModifiedAt time.Time       `json:"modified_at"`
	Revision   int64           `json:"revision,omitempty"`
	DeletedAt  *time.Time      `json:"deleted_at,omitempty"`
	Qso        json.RawMessage `json:"qso"`
}

// ScopedExport is GET /v1/archives/{archive_uuid}/logbooks/{logbook_uuid}/export
// (ADR 0088): one cloud logbook, by UUID, never another archive's rows.
type ScopedExport struct {
	Archive ScopedExportArchive  `json:"archive"`
	Logbook ScopedExportLogbook  `json:"logbook"`
	Qsos    []ScopedExportRecord `json:"qsos"`
}

// ErrScopedLogbookNotFound is a scoped export's 404.
var ErrScopedLogbookNotFound = stderr.New("the SM Cloud server has no such logbook")

// scopedExportBody is the wire shape, with Qsos a pointer so a document
// without the array is told apart from an empty logbook.
type scopedExportBody struct {
	Archive ScopedExportArchive   `json:"archive"`
	Logbook ScopedExportLogbook   `json:"logbook"`
	Qsos    *[]ScopedExportRecord `json:"qsos"`
}

// FetchScopedExport pulls one cloud logbook's export by the archive and
// logbook UUIDs (W-0021 5F.5, ADR 0088 refinement 2026-10-13): the restore
// source when the target is addressed by identity. It returns an export only
// when the answer is complete and its head names exactly the requested
// archive and logbook; anything else is refused with no rows, so the caller
// never writes from it. A 404 is ErrScopedLogbookNotFound: the server has no
// such logbook (or, on a server without the route, no such path; the caller's
// support check is what tells those apart). Errors name the status only,
// never the token or a server-supplied body.
func FetchScopedExport(ctx context.Context, account types.ForwarderConfig, archiveUUID, logbookUUID string) (*ScopedExport, error) {
	const op errors.Op = "smcloud.FetchScopedExport"
	f, err := New(account)
	if err != nil {
		return nil, errors.New(op).WithErr(err)
	}
	fwd := f.(*Forwarder)
	path := "/v1/archives/" + url.PathEscape(archiveUUID) + "/logbooks/" + url.PathEscape(logbookUUID) + "/export"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(fwd.putURL, "/v1/qsos")+path, nil)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("build request")
	}
	req.Header.Set("Authorization", "Bearer "+fwd.token)
	req.Header.Set("User-Agent", UserAgent)

	// As FetchExport: a whole logbook on a slow link; the caller's ctx bounds it.
	resp, err := securehttp.Do(securehttp.NewClient(10*time.Minute), req)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("GET the scoped export")
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, errors.New(op).WithErr(ErrScopedLogbookNotFound)
	default:
		return nil, errors.New(op).WithMsgf("GET the scoped export: HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxExportBytes))
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("read the scoped export")
	}
	var doc scopedExportBody
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("the scoped export is not a complete document")
	}
	switch {
	case doc.Archive.UUID == "" || doc.Logbook.UUID == "":
		return nil, errors.New(op).WithMsg("the scoped export names no archive or logbook")
	case !strings.EqualFold(doc.Archive.UUID, archiveUUID) || !strings.EqualFold(doc.Logbook.UUID, logbookUUID):
		return nil, errors.New(op).WithMsgf("the scoped export is for archive %s logbook %s, not the requested archive %s logbook %s",
			doc.Archive.UUID, doc.Logbook.UUID, archiveUUID, logbookUUID)
	case doc.Qsos == nil:
		return nil, errors.New(op).WithMsg("the scoped export has no qsos array")
	}
	return &ScopedExport{Archive: doc.Archive, Logbook: doc.Logbook, Qsos: *doc.Qsos}, nil
}
