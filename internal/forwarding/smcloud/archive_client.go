package smcloud

import (
	"bytes"
	"context"
	"encoding/json"
	stderr "errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/securehttp"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// The whole-archive reads (W-0021 Q7, ADR 0071 refinement): the listing a
// provisioning client chooses from, and one archive whole. Nothing reaches
// the caller unless it is complete, addressed as asked, and every row
// resolves to exactly one logbook of the head with the counts it declares.

// archiveExportLevel is the archive_export capability level this build reads;
// a server at this level or above serves it.
const archiveExportLevel = 1

// archiveExportLimit caps the whole-archive export; a variable only so tests
// can exercise the cap without buffering 256 MiB.
var archiveExportLimit int64 = maxExportBytes

// archiveTransport replaces the client's transport when set; tests use it to
// observe the client reading a response body. Nil is http.DefaultTransport.
var archiveTransport http.RoundTripper

var (
	// ErrArchiveExportUnsupported: the server's GET /v1/version advertises no
	// archive_export, or a level below archiveExportLevel.
	ErrArchiveExportUnsupported = stderr.New("the SM Cloud server does not support whole-archive export")
	// ErrArchiveNotFound is the export's 404 from a server that supports it.
	ErrArchiveNotFound = stderr.New("the SM Cloud server has no such archive")
)

// CloudArchiveLogbook is one logbook of a cloud archive. Exactly one of UUID
// and LegacyName is set: LegacyName, the stored name, only for a legacy
// logbook never adopted (it may be empty). Count includes tombstones.
type CloudArchiveLogbook struct {
	UUID       *string
	LegacyName *string
	Label      string
	Callsign   string
	Count      int
	Tombstones int
}

// CloudArchive is one archive of the tenant: its UUID, display label, legacy
// marker and logbooks.
type CloudArchive struct {
	UUID     string
	Label    string
	Legacy   bool
	Logbooks []CloudArchiveLogbook
}

// ArchiveExportRecord is one row of a whole-archive export. Logbook is the
// index of its logbook in ArchiveExport.Logbooks; Qso is the payload exactly
// as the server sent it.
type ArchiveExportRecord struct {
	UUID       string
	Logbook    int
	ModifiedAt time.Time
	Revision   int64
	DeletedAt  *time.Time
	Qso        json.RawMessage
}

// ArchiveExport is GET /v1/archives/{archive}/export, validated.
type ArchiveExport struct {
	CloudArchive
	Qsos []ArchiveExportRecord
}

// wireField is a member of the wire document, telling an absent member from a
// null one from a value. A member may appear once: a repeat is refused before
// it can change what the first occurrence set. Only typed members are
// wireFields; the QSO payload's own contents stay opaque.
type wireField[T any] struct {
	Set  bool
	Null bool
	V    T
}

var errRepeatedMember = stderr.New("a member appears more than once")

func (f *wireField[T]) UnmarshalJSON(b []byte) error {
	if f.Set {
		return errRepeatedMember
	}
	f.Set = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		f.Null = true
		return nil
	}
	return json.Unmarshal(b, &f.V)
}

// has reports a member present with a value.
func (f wireField[T]) has() bool { return f.Set && !f.Null }

type wireLogbook struct {
	UUID       wireField[string] `json:"uuid"`
	LegacyName wireField[string] `json:"legacy_name"`
	Label      wireField[string] `json:"label"`
	Callsign   wireField[string] `json:"callsign"`
	Count      wireField[int]    `json:"count"`
	Tombstones wireField[int]    `json:"tombstones"`
}

type wireArchive struct {
	UUID     wireField[string]        `json:"uuid"`
	Label    wireField[string]        `json:"label"`
	Legacy   wireField[bool]          `json:"legacy"`
	Logbooks wireField[[]wireLogbook] `json:"logbooks"`
}

type wireListing struct {
	Archives wireField[[]wireArchive] `json:"archives"`
}

type wireRow struct {
	UUID              wireField[string]          `json:"uuid"`
	LogbookUUID       wireField[string]          `json:"logbook_uuid"`
	LogbookLegacyName wireField[string]          `json:"logbook_legacy_name"`
	ModifiedAt        wireField[time.Time]       `json:"modified_at"`
	Revision          wireField[int64]           `json:"revision"`
	DeletedAt         wireField[time.Time]       `json:"deleted_at"`
	Qso               wireField[json.RawMessage] `json:"qso"`
}

type wireExport struct {
	Archive  wireField[wireArchive]   `json:"archive"`
	Logbooks wireField[[]wireLogbook] `json:"logbooks"`
	Qsos     wireField[[]wireRow]     `json:"qsos"`
}

// ListArchives reads GET /v1/archives: every archive of the tenant that has a
// UUID, the legacy archive included. Any defect refuses the whole listing.
func ListArchives(ctx context.Context, account types.ForwarderConfig) ([]CloudArchive, error) {
	const op errors.Op = "smcloud.ListArchives"
	c, err := newArchiveClient(account)
	if err != nil {
		return nil, errors.New(op).WithErr(err)
	}
	if err := c.checkLevel(ctx); err != nil {
		return nil, errors.New(op).WithErr(err)
	}
	body, err := c.read(ctx, "/v1/archives", true, maxResponseBytes)
	if err != nil {
		return nil, errors.New(op).WithErr(err)
	}
	var doc wireListing
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, errors.New(op).WithMsg("the listing is not a complete, well-formed document")
	}
	out, err := validListing(doc)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("the listing is refused")
	}
	return out, nil
}

// FetchArchiveExport reads archive archiveUUID whole, from one server
// snapshot. It returns the export only when the head names that archive and
// every row resolves to exactly one head logbook with the counts the head
// declares; otherwise nothing.
func FetchArchiveExport(ctx context.Context, account types.ForwarderConfig, archiveUUID string) (*ArchiveExport, error) {
	const op errors.Op = "smcloud.FetchArchiveExport"
	if !utils.IsValidUUIDv7(archiveUUID) {
		return nil, errors.New(op).WithMsg("the archive is not named by a UUIDv7")
	}
	archiveUUID = strings.ToLower(archiveUUID)
	c, err := newArchiveClient(account)
	if err != nil {
		return nil, errors.New(op).WithErr(err)
	}
	if err := c.checkLevel(ctx); err != nil {
		return nil, errors.New(op).WithErr(err)
	}
	body, err := c.read(ctx, "/v1/archives/"+archiveUUID+"/export", true, archiveExportLimit)
	if stderr.Is(err, errNotFound) {
		return nil, errors.New(op).WithErr(ErrArchiveNotFound)
	}
	if err != nil {
		return nil, errors.New(op).WithErr(err)
	}
	var doc wireExport
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, errors.New(op).WithMsg("the export is not a complete, well-formed document")
	}
	out, err := validExport(doc, archiveUUID)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("the export is refused")
	}
	return out, nil
}

// archiveClient reads the whole-archive routes. It never follows a redirect:
// a 3xx is refused, so the bearer token is never sent on.
type archiveClient struct {
	base   string
	token  string
	client *http.Client
}

func newArchiveClient(account types.ForwarderConfig) (*archiveClient, error) {
	f, err := New(account)
	if err != nil {
		return nil, err
	}
	fwd := f.(*Forwarder)
	return &archiveClient{
		base:  strings.TrimSuffix(fwd.putURL, "/v1/qsos"),
		token: fwd.token,
		// As FetchScopedExport: a whole archive on a slow link; the caller's
		// ctx is the real bound.
		client: &http.Client{
			Transport:     archiveTransport,
			Timeout:       10 * time.Minute,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// checkLevel reads GET /v1/version, without the token, and requires
// archive_export at archiveExportLevel or above.
func (c *archiveClient) checkLevel(ctx context.Context) error {
	rctx, cancel := context.WithTimeout(ctx, supportTimeout)
	defer cancel()
	body, err := c.read(rctx, "/v1/version", false, maxResponseBytes)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if !bytes.HasPrefix(bytes.TrimSpace(body), []byte("{")) || json.Unmarshal(body, &fields) != nil {
		return stderr.New("GET /v1/version: the answer is not a JSON object")
	}
	raw, ok := fields["archive_export"]
	if !ok {
		return ErrArchiveExportUnsupported
	}
	var level *int
	if err := json.Unmarshal(raw, &level); err != nil || level == nil {
		return stderr.New("GET /v1/version: archive_export is not an integer")
	}
	if *level < archiveExportLevel {
		return ErrArchiveExportUnsupported
	}
	return nil
}

// errNotFound is read's 404; the export reports it as ErrArchiveNotFound.
var errNotFound = stderr.New("HTTP 404")

// read sends one GET and returns a 200's body of at most limit bytes. Errors
// name the path and the status only, never the token or the body.
func (c *archiveClient) read(ctx context.Context, path string, auth bool, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, stderr.New("GET " + path + ": the request cannot be built")
	}
	if auth {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := securehttp.Do(c.client, req)
	if err != nil {
		return nil, errors.New("").WithErr(transportCause(ctx, err)).WithMsgf("GET %s: no answer", path)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, errors.New("").WithErr(errNotFound).WithMsgf("GET %s", path)
	default:
		return nil, errors.New("").WithMsgf("GET %s: HTTP %d %s", path, resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	// One byte past the limit tells a body of exactly the limit from a
	// longer one, whatever length the response declares.
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, errors.New("").WithErr(transportCause(ctx, err)).WithMsgf("GET %s: HTTP 200, the answer was cut off", path)
	}
	if int64(len(body)) > limit {
		return nil, errors.New("").WithMsgf("GET %s: the answer is larger than %d bytes", path, limit)
	}
	return body, nil
}

// validListing checks every archive and logbook of the listing. Archive and
// logbook UUIDs are each unique across the tenant, in any case.
func validListing(doc wireListing) ([]CloudArchive, error) {
	if !doc.Archives.has() {
		return nil, stderr.New("no archives array")
	}
	out := make([]CloudArchive, 0, len(doc.Archives.V))
	archives, logbooks := map[string]bool{}, map[string]bool{}
	for i, a := range doc.Archives.V {
		archive, err := validArchive(a)
		if err != nil {
			return nil, errors.New("").WithErr(err).WithMsgf("archive %d", i)
		}
		key := strings.ToLower(archive.UUID)
		if archives[key] {
			return nil, errors.New("").WithMsgf("archive %s listed twice", archive.UUID)
		}
		archives[key] = true
		if !a.Logbooks.has() {
			return nil, errors.New("").WithMsgf("archive %s has no logbooks array", archive.UUID)
		}
		if archive.Logbooks, err = validLogbooks(a.Logbooks.V, logbooks); err != nil {
			return nil, errors.New("").WithErr(err).WithMsgf("archive %s", archive.UUID)
		}
		out = append(out, archive)
	}
	return out, nil
}

// validArchive checks an archive's own members: a UUIDv7, a label and an
// explicit legacy marker.
func validArchive(a wireArchive) (CloudArchive, error) {
	switch {
	case !a.UUID.has() || !utils.IsValidUUIDv7(a.UUID.V):
		return CloudArchive{}, stderr.New("no UUIDv7")
	case !a.Label.has():
		return CloudArchive{}, stderr.New("no label")
	case !a.Legacy.has():
		return CloudArchive{}, stderr.New("no legacy marker")
	}
	return CloudArchive{UUID: a.UUID.V, Label: a.Label.V, Legacy: a.Legacy.V}, nil
}

// validLogbooks checks each logbook: exactly one identifier (a UUIDv7 not
// already in uuids, or with a null uuid a legacy name unique here), a label,
// a callsign, and counts with 0 <= tombstones <= count. Errors name a logbook
// by position, never by its name.
func validLogbooks(in []wireLogbook, uuids map[string]bool) ([]CloudArchiveLogbook, error) {
	out := make([]CloudArchiveLogbook, 0, len(in))
	names := map[string]bool{}
	for i, w := range in {
		lb := CloudArchiveLogbook{Label: w.Label.V, Callsign: w.Callsign.V, Count: w.Count.V, Tombstones: w.Tombstones.V}
		switch {
		case !w.UUID.Set:
			return nil, errors.New("").WithMsgf("logbook %d has no uuid member", i)
		case w.UUID.Null:
			if !w.LegacyName.has() {
				return nil, errors.New("").WithMsgf("logbook %d has neither a UUID nor a legacy name", i)
			}
			if names[w.LegacyName.V] {
				return nil, errors.New("").WithMsgf("logbook %d repeats a legacy name", i)
			}
			names[w.LegacyName.V] = true
			lb.LegacyName = &w.LegacyName.V
		case !utils.IsValidUUIDv7(w.UUID.V):
			return nil, errors.New("").WithMsgf("logbook %d is not named by a UUIDv7", i)
		case w.LegacyName.Set:
			return nil, errors.New("").WithMsgf("logbook %s has both a UUID and a legacy name", w.UUID.V)
		case uuids[strings.ToLower(w.UUID.V)]:
			return nil, errors.New("").WithMsgf("logbook %s listed twice", w.UUID.V)
		default:
			uuids[strings.ToLower(w.UUID.V)] = true
			lb.UUID = &w.UUID.V
		}
		switch {
		case !w.Label.has() || !w.Callsign.has():
			return nil, errors.New("").WithMsgf("logbook %d has no label or callsign", i)
		case !w.Count.has() || !w.Tombstones.has():
			return nil, errors.New("").WithMsgf("logbook %d has no count or tombstones", i)
		case lb.Count < 0 || lb.Tombstones < 0 || lb.Tombstones > lb.Count:
			return nil, errors.New("").WithMsgf("logbook %d has impossible counts", i)
		}
		out = append(out, lb)
	}
	return out, nil
}

// validExport checks the head names archiveUUID, then resolves every row to
// one head logbook and compares each logbook's counts with its rows.
func validExport(doc wireExport, archiveUUID string) (*ArchiveExport, error) {
	if !doc.Archive.has() {
		return nil, stderr.New("no archive")
	}
	head, err := validArchive(doc.Archive.V)
	if err != nil {
		return nil, errors.New("").WithErr(err).WithMsg("the archive")
	}
	if !strings.EqualFold(head.UUID, archiveUUID) {
		return nil, errors.New("").WithMsgf("the export is for archive %s, not %s", head.UUID, archiveUUID)
	}
	if !doc.Logbooks.has() || !doc.Qsos.has() {
		return nil, stderr.New("no logbooks or qsos array")
	}
	if head.Logbooks, err = validLogbooks(doc.Logbooks.V, map[string]bool{}); err != nil {
		return nil, err
	}
	byUUID, byName := map[string]int{}, map[string]int{}
	for i, lb := range head.Logbooks {
		if lb.UUID != nil {
			byUUID[strings.ToLower(*lb.UUID)] = i
		} else {
			byName[*lb.LegacyName] = i
		}
	}
	out := &ArchiveExport{CloudArchive: head, Qsos: make([]ArchiveExportRecord, 0, len(doc.Qsos.V))}
	rows, tombstones := make([]int, len(head.Logbooks)), make([]int, len(head.Logbooks))
	seen := map[string]bool{}
	for i, w := range doc.Qsos.V {
		rec, err := validRow(w, byUUID, byName)
		if err != nil {
			return nil, errors.New("").WithErr(err).WithMsgf("row %d", i)
		}
		key := strings.ToLower(rec.UUID)
		if seen[key] {
			return nil, errors.New("").WithMsgf("row %s exported twice", rec.UUID)
		}
		seen[key] = true
		rows[rec.Logbook]++
		if rec.DeletedAt != nil {
			tombstones[rec.Logbook]++
		}
		out.Qsos = append(out.Qsos, rec)
	}
	for i, lb := range head.Logbooks {
		if lb.Count != rows[i] || lb.Tombstones != tombstones[i] {
			return nil, errors.New("").WithMsgf("logbook %d declares %d rows and %d tombstones; the export has %d and %d",
				i, lb.Count, lb.Tombstones, rows[i], tombstones[i])
		}
	}
	return out, nil
}

// validRow checks a row's own members and resolves its logbook: by UUID in
// any case, or, with a null logbook_uuid, by the exact legacy name.
func validRow(w wireRow, byUUID, byName map[string]int) (ArchiveExportRecord, error) {
	switch {
	case !w.UUID.has() || !utils.IsValidUUIDv7(w.UUID.V):
		return ArchiveExportRecord{}, stderr.New("no UUIDv7")
	case !w.ModifiedAt.has():
		return ArchiveExportRecord{}, errors.New("").WithMsgf("row %s has no modified_at", w.UUID.V)
	case !w.Qso.has():
		return ArchiveExportRecord{}, errors.New("").WithMsgf("row %s has no qso", w.UUID.V)
	}
	rec := ArchiveExportRecord{UUID: w.UUID.V, ModifiedAt: w.ModifiedAt.V, Revision: w.Revision.V, Qso: w.Qso.V}
	if w.DeletedAt.has() {
		rec.DeletedAt = &w.DeletedAt.V
	}
	var ok bool
	switch {
	case !w.LogbookUUID.Set:
		return ArchiveExportRecord{}, errors.New("").WithMsgf("row %s names no logbook", rec.UUID)
	case w.LogbookUUID.Null:
		if w.LogbookLegacyName.has() {
			rec.Logbook, ok = byName[w.LogbookLegacyName.V]
		}
	case w.LogbookLegacyName.Set:
		return ArchiveExportRecord{}, errors.New("").WithMsgf("row %s names its logbook twice", rec.UUID)
	default:
		rec.Logbook, ok = byUUID[strings.ToLower(w.LogbookUUID.V)]
	}
	if !ok {
		return ArchiveExportRecord{}, errors.New("").WithMsgf("row %s is in no logbook of the head", rec.UUID)
	}
	return rec, nil
}
