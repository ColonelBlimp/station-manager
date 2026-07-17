package smcloud

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
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
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
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
