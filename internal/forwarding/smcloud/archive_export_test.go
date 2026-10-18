package smcloud

import (
	"bytes"
	"context"
	stderr "errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

/*
   W-0021 Q7 commit 3 (ADR 0071 refinement 2026-10-16/17): the station reads a
   cloud archive whole. Nothing the client cannot prove complete, correctly
   addressed and unambiguous reaches the caller, and what it returns keeps
   every byte commit 5's round-trip check compares.

     CA1  capability, a cumulative level: both calls read GET /v1/version
          without the token, then exactly their one archive route, at
          archive_export 1 or 2. Absent, 0 or negative is
          ErrArchiveExportUnsupported; malformed (a string, a fraction, null,
          not an object), not 200, or a redirect (another origin never
          contacted, the same origin not followed) is an error that is not;
          each refuses with no further request: no archive route, no older
          export.
     CA2  the listing returns every archive exactly, the legacy archive kept
          (legacy true, its unadopted logbooks by legacy name with the empty
          callsign they start with, equal labels, an empty name distinct from
          none, an empty logbook); a legacy name spelled like another
          logbook's UUID is a separate logbook.
     CA3  the listing is refused, with no archives returned, unless complete
          and unambiguous: archives array, each archive's uuid, label and
          legacy marker, each logbook's identity (exactly one of a UUID or a
          legacy name), label, callsign and counts (non-negative, tombstones
          no more than rows); UUIDs other than UUIDv7 (malformed, or a valid
          v4); duplicate archive or logbook UUIDs in any case, duplicate
          legacy names in one archive.
     CA4  the export asks GET /v1/archives/{U}/export with the token and
          returns the head, every logbook and every row: each row's exact
          logbook (by index into Logbooks), revision, sub-second modified_at,
          tombstone, and its payload byte for byte, unknown nested fields and
          integers beyond float64 included. A row naming its logbook UUID in
          another case resolves to it, and an archive asked in upper case is
          requested by its canonical lower-case UUID. A legacy name spelled
          like another logbook's UUID resolves separately. Empty arrays are a
          valid empty archive. Every expected row appears exactly once. The
          payload is opaque: a key repeated inside it comes back verbatim.
     CA5  identity and membership refusals: another or no archive, no or a
          null legacy marker, duplicate logbook or QSO UUIDs in any case,
          duplicate legacy names, a row naming no head logbook, neither or
          both identifiers, an empty legacy name where the head has none, an
          absent name where the head has "", a row without uuid, modified_at
          or qso; a valid UUIDv4 archive, logbook or QSO; a typed member
          repeated (a head or row identifier, null then a value or a value
          then null, with counts that agree; the legacy marker; qso).
     CA6  counts: each logbook's row and tombstone counts must equal its
          rows, an empty logbook's included; equal totals with the rows or
          only the tombstones counted under the wrong logbook are refused,
          each count within its bounds; a missing count on a logbook whose
          true count is zero; a negative tombstone count.
     CA7  completeness: logbooks or qsos missing or null, in documents
          otherwise empty as well as populated; a document cut off after a
          valid row or after the array, trailing data, a second document;
          each refused with no result.
     CA8  transport: 401, 403 and 5xx name the status and never the token or
          the body; the export's 404 is ErrArchiveNotFound; no redirect is
          followed (another origin is never contacted, nor another path on
          this one); a cancellation before the answer, or once the client is
          in its body read (a barrier on the client's response body), returns
          context.Canceled and no result; a refusal never quotes a payload.
     CA9  size: the listing reads at most maxResponseBytes (1 MiB) and the
          export archiveExportLimit (maxExportBytes, 256 MiB): a valid
          document of exactly the limit is read; one byte more, or a valid
          document within the limit followed by more bytes beyond it, is
          refused, never read as the bytes up to the limit. The bodies stream
          with no Content-Length, so the declared length cannot stand in for
          the bound.
*/

const (
	caArchive   = "0197f9a0-0000-7000-8000-0000000a0001"
	caSameLabel = "0197f9a0-0000-7000-8000-0000000a0002"
	caHome      = "0197f9a0-0000-7000-8000-0000000a0003"
	caOther     = "0197f9a0-0000-7000-8000-0000000a00ff"
	caLB1       = "0197f9a0-0000-7000-8000-0000000b0001"
	caLB2       = "0197f9a0-0000-7000-8000-0000000b0002"
	caLB3       = "0197f9a0-0000-7000-8000-0000000b0003"
	caHomeLB    = "0197f9a0-0000-7000-8000-0000000b0004"
	caQ1        = "0197f9a0-0000-7000-8000-0000000c0001"
	caQ2        = "0197f9a0-0000-7000-8000-0000000c0002"
	caQ3        = "0197f9a0-0000-7000-8000-0000000c0003"
	caQ4        = "0197f9a0-0000-7000-8000-0000000c0004"
	caQ5        = "0197f9a0-0000-7000-8000-0000000c0005"
	caQ6        = "0197f9a0-0000-7000-8000-0000000c0006"
	caQ7        = "0197f9a0-0000-7000-8000-0000000c0007"

	// Valid UUIDs of version 4, not 7.
	caV4Archive = "0197f9a0-0000-4000-8000-0000000a0004"
	caV4LB      = "0197f9a0-0000-4000-8000-0000000b0005"
	caV4Q       = "0197f9a0-0000-4000-8000-0000000c0008"

	// caPayload is a QSO this build does not fully model: an unknown nested
	// field with integers float64 cannot hold. It must come back byte for byte.
	caPayload = `{"call":"DL9UW","band":"20m","x_future":{"deep":{"big":9007199254740993,"huge":123456789012345678901234567890,"list":[1,{"k":null}]}}}`
	caAt      = "2026-07-17T06:00:00.123456Z"
	caGoneAt  = "2026-07-17T07:00:00.654321Z"
)

var (
	caAtTime     = time.Date(2026, 7, 17, 6, 0, 0, 123456000, time.UTC)
	caGoneAtTime = time.Date(2026, 7, 17, 7, 0, 0, 654321000, time.UTC)
)

// caLB is a logbook object: identity is the raw JSON of its identity members
// (e.g. `"uuid":"…"` or `"uuid":null,"legacy_name":"portable"`).
func caLB(identity, label string, count, tombstones int) string {
	return `{` + identity + `,"label":"` + label + `","callsign":"7Q5MLV","count":` + strconv.Itoa(count) + `,"tombstones":` + strconv.Itoa(tombstones) + `}`
}

// caLegacyLB is an unadopted legacy logbook, with the empty callsign such a
// logbook starts with (migration 0008).
func caLegacyLB(name, label string, count, tombstones int) string {
	return `{` + caByName(name) + `,"label":"` + label + `","callsign":"","count":` + strconv.Itoa(count) + `,"tombstones":` + strconv.Itoa(tombstones) + `}`
}

func caByUUID(uuid string) string { return `"uuid":"` + uuid + `"` }
func caByName(name string) string { return `"uuid":null,"legacy_name":"` + name + `"` }

// caRow is a row object: logbook is the raw JSON of its logbook members.
func caRow(uuid, logbook string, revision int, gone bool) string {
	at, del := caAt, ""
	if gone {
		at, del = caGoneAt, `,"deleted_at":"`+caGoneAt+`"`
	}
	rev := ""
	if revision != 0 {
		rev = `,"revision":` + strconv.Itoa(revision)
	}
	return `{"uuid":"` + uuid + `",` + logbook + `,"modified_at":"` + at + `"` + rev + del + `,"qso":` + caPayload + `}`
}

func caInUUID(uuid string) string { return `"logbook_uuid":"` + uuid + `"` }
func caInName(name string) string { return `"logbook_uuid":null,"logbook_legacy_name":"` + name + `"` }

func caHead(uuid, label string, legacy bool) string {
	return `"archive":{"uuid":"` + uuid + `","label":"` + label + `","legacy":` + strconv.FormatBool(legacy) + `}`
}

func caDoc(head string, logbooks, rows []string) string {
	return `{` + head + `,"logbooks":[` + strings.Join(logbooks, ",") + `],"qsos":[` + strings.Join(rows, ",") + `]}` + "\n"
}

// The managed archive: LB1 with a live row and a tombstone, LB2 empty.
func caManagedLogbooks() []string {
	return []string{caLB(caByUUID(caLB1), "main", 2, 1), caLB(caByUUID(caLB2), "empty", 0, 0)}
}

func caManagedRows() []string {
	return []string{caRow(caQ1, caInUUID(caLB1), 3, false), caRow(caQ2, caInUUID(caLB1), 4, true)}
}

func caManagedDoc() string {
	return caDoc(caHead(caArchive, "Contest", false), caManagedLogbooks(), caManagedRows())
}

// The adopted legacy archive: Home's default adopted, three not adopted under
// one label, one of them with an empty stored name.
func caHomeLogbooks() []string {
	return []string{
		caLB(caByUUID(caHomeLB), "Default", 1, 0),
		caLegacyLB("portable", "Field", 1, 0),
		caLegacyLB("contest", "Field", 2, 1),
		caLegacyLB("", "Field", 1, 0),
	}
}

func caHomeRows() []string {
	return []string{
		caRow(caQ3, caInUUID(caHomeLB), 0, false),
		caRow(caQ4, caInName("portable"), 0, false),
		caRow(caQ5, caInName("contest"), 0, false),
		caRow(caQ6, caInName("contest"), 0, true),
		caRow(caQ7, caInName(""), 0, false),
	}
}

func caHomeDoc() string {
	return caDoc(caHead(caHome, "Home", true), caHomeLogbooks(), caHomeRows())
}

func caListing(archives ...string) string {
	return `{"archives":[` + strings.Join(archives, ",") + `]}` + "\n"
}

func caListed(uuid, label string, legacy bool, logbooks []string) string {
	return `{"uuid":"` + uuid + `","label":"` + label + `","legacy":` + strconv.FormatBool(legacy) + `,"logbooks":[` + strings.Join(logbooks, ",") + `]}`
}

func caGoodListing() string {
	return caListing(
		caListed(caArchive, "Contest", false, caManagedLogbooks()),
		caListed(caSameLabel, "Contest", false, []string{caLB(caByUUID(caLB3), "main", 1, 0)}),
		caListed(caHome, "Home", true, caHomeLogbooks()),
	)
}

// caServer is a fake SM Cloud: /v1/version answers version (status and body),
// the archive routes answer what routes holds, and every request is recorded.
type caServer struct {
	*httptest.Server
	mu      sync.Mutex
	reqs    []*http.Request
	version http.HandlerFunc
	routes  map[string]http.HandlerFunc
}

func caVersion(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func caAnswer(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func newCAServer(t *testing.T, listing, export string) *caServer {
	t.Helper()
	s := &caServer{
		version: caVersion(`{"version":"test","identity_protocol":1,"archive_export":1}`),
		routes: map[string]http.HandlerFunc{
			"/v1/archives":                          caAnswer(http.StatusOK, listing),
			"/v1/archives/" + caArchive + "/export": caAnswer(http.StatusOK, export),
			"/v1/archives/" + caHome + "/export":    caAnswer(http.StatusOK, export),
		},
	}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.reqs = append(s.reqs, r.Clone(context.Background()))
		version, h := s.version, s.routes[r.URL.Path]
		s.mu.Unlock()
		switch {
		case r.URL.Path == "/v1/version":
			version(w, r)
		case h != nil:
			h(w, r)
		default:
			caAnswer(http.StatusNotFound, `{"code":"not_found"}`)(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *caServer) paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.reqs))
	for _, r := range s.reqs {
		out = append(out, r.Method+" "+r.URL.Path)
	}
	return out
}

func (s *caServer) request(path string) *http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.reqs {
		if r.URL.Path == path {
			return r
		}
	}
	return nil
}

type caCall struct {
	route string
	run   func(ctx context.Context, s *caServer) (bool, error)
}

// caCalls are the two client calls, each with the one archive route it asks.
var caCalls = map[string]caCall{
	"ListArchives": {"/v1/archives", func(ctx context.Context, s *caServer) (bool, error) {
		got, err := ListArchives(ctx, testConfig(s.URL))
		return got != nil, err
	}},
	"FetchArchiveExport": {"/v1/archives/" + caArchive + "/export", func(ctx context.Context, s *caServer) (bool, error) {
		got, err := FetchArchiveExport(ctx, testConfig(s.URL), caArchive)
		return got != nil, err
	}},
}

func TestArchiveClient_CA1_TheCapabilityGatesEveryArchiveRequest(t *testing.T) {
	for call, c := range caCalls {
		for _, level := range []string{"1", "2"} {
			t.Run(call+"/archive_export "+level, func(t *testing.T) {
				s := newCAServer(t, caGoodListing(), caManagedDoc())
				s.version = caVersion(`{"version":"test","identity_protocol":1,"archive_export":` + level + `}`)
				if ok, err := c.run(context.Background(), s); err != nil || !ok {
					t.Fatalf("archive_export %s: %v (result %v); want the archive read", level, err, ok)
				}
				if got := s.paths(); len(got) != 2 || got[0] != "GET /v1/version" || got[1] != "GET "+c.route {
					t.Fatalf("requests %v; want exactly GET /v1/version, then GET %s", got, c.route)
				}
				if v := s.request("/v1/version"); v.Header.Get("Authorization") != "" {
					t.Fatalf("GET /v1/version carried the token")
				}
			})
		}
		other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("another origin was contacted: %s %s (auth %q)", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}))
		t.Cleanup(other.Close)
		for name, v := range map[string]struct {
			version     http.HandlerFunc
			unsupported bool
		}{
			"absent":        {caVersion(`{"version":"test","identity_protocol":1}`), true},
			"zero":          {caVersion(`{"identity_protocol":1,"archive_export":0}`), true},
			"negative":      {caVersion(`{"identity_protocol":1,"archive_export":-1}`), true},
			"a string":      {caVersion(`{"identity_protocol":1,"archive_export":"1"}`), false},
			"a fraction":    {caVersion(`{"identity_protocol":1,"archive_export":1.5}`), false},
			"null":          {caVersion(`{"identity_protocol":1,"archive_export":null}`), false},
			"not an object": {caVersion(`[{"archive_export":1}]`), false},
			"not 200": {func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"archive_export":1}`))
			}, false},
			"a redirect to another origin": {func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, other.URL+"/v1/version", http.StatusTemporaryRedirect)
			}, false},
			"a redirect on the same origin": {func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/moved/v1/version", http.StatusTemporaryRedirect)
			}, false},
		} {
			t.Run(call+"/"+name, func(t *testing.T) {
				s := newCAServer(t, caGoodListing(), caManagedDoc())
				s.version = v.version
				s.routes["/moved/v1/version"] = caVersion(`{"identity_protocol":1,"archive_export":1}`)
				ok, err := c.run(context.Background(), s)
				if err == nil || ok {
					t.Fatalf("version %s: err %v, result %v; want refused with no result", name, err, ok)
				}
				if stderr.Is(err, ErrArchiveExportUnsupported) != v.unsupported {
					t.Fatalf("version %s: err %v; ErrArchiveExportUnsupported %v, want %v", name, err, stderr.Is(err, ErrArchiveExportUnsupported), v.unsupported)
				}
				if got := s.paths(); len(got) != 1 || got[0] != "GET /v1/version" {
					t.Fatalf("version %s: requests %v; want only GET /v1/version (no redirect followed, no archive route, no older export)", name, got)
				}
			})
		}
	}
}

func ptr(s string) *string { return &s }

func wantArchiveLogbooks(t *testing.T, where string, got []CloudArchiveLogbook, want []CloudArchiveLogbook) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d logbooks %+v; want %d", where, len(got), got, len(want))
	}
	for i := range want {
		g, w := got[i], want[i]
		same := func(a, b *string) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
		if !same(g.UUID, w.UUID) || !same(g.LegacyName, w.LegacyName) || g.Label != w.Label || g.Callsign != w.Callsign ||
			g.Count != w.Count || g.Tombstones != w.Tombstones {
			t.Fatalf("%s: logbook %d = %s; want %s", where, i, caFmtLB(g), caFmtLB(w))
		}
	}
}

func caFmtLB(lb CloudArchiveLogbook) string {
	id := "uuid <none>"
	if lb.UUID != nil {
		id = "uuid " + *lb.UUID
	}
	if lb.LegacyName != nil {
		id += " legacy_name " + strconv.Quote(*lb.LegacyName)
	}
	return id + " " + lb.Label + " " + lb.Callsign + " " + strconv.Itoa(lb.Count) + "/" + strconv.Itoa(lb.Tombstones)
}

var (
	caManagedWant = []CloudArchiveLogbook{
		{UUID: ptr(caLB1), Label: "main", Callsign: "7Q5MLV", Count: 2, Tombstones: 1},
		{UUID: ptr(caLB2), Label: "empty", Callsign: "7Q5MLV"},
	}
	caHomeWant = []CloudArchiveLogbook{
		{UUID: ptr(caHomeLB), Label: "Default", Callsign: "7Q5MLV", Count: 1},
		{LegacyName: ptr("portable"), Label: "Field", Count: 1},
		{LegacyName: ptr("contest"), Label: "Field", Count: 2, Tombstones: 1},
		{LegacyName: ptr(""), Label: "Field", Count: 1},
	}
)

func TestArchiveClient_CA2_TheListing(t *testing.T) {
	s := newCAServer(t, caGoodListing(), "")
	got, err := ListArchives(context.Background(), testConfig(s.URL))
	if err != nil {
		t.Fatalf("ListArchives: %v", err)
	}
	if r := s.request("/v1/archives"); r == nil || r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer tok-123" {
		t.Fatalf("requests %v; want GET /v1/archives with the bearer token", s.paths())
	}
	if len(got) != 3 {
		t.Fatalf("%d archives %+v; want 3", len(got), got)
	}
	for i, w := range []struct {
		uuid, label string
		legacy      bool
		logbooks    []CloudArchiveLogbook
	}{
		{caArchive, "Contest", false, caManagedWant},
		{caSameLabel, "Contest", false, []CloudArchiveLogbook{{UUID: ptr(caLB3), Label: "main", Callsign: "7Q5MLV", Count: 1}}},
		{caHome, "Home", true, caHomeWant},
	} {
		a := got[i]
		if a.UUID != w.uuid || a.Label != w.label || a.Legacy != w.legacy {
			t.Fatalf("archive %d = %s %q legacy %v; want %s %q legacy %v", i, a.UUID, a.Label, a.Legacy, w.uuid, w.label, w.legacy)
		}
		wantArchiveLogbooks(t, "archive "+w.uuid, a.Logbooks, w.logbooks)
	}
	t.Run("a legacy name spelled like another logbook's UUID", func(t *testing.T) {
		s := newCAServer(t, caListing(caListed(caHome, "Home", true, caNamespaceLogbooks())), "")
		got, err := ListArchives(context.Background(), testConfig(s.URL))
		if err != nil || len(got) != 1 {
			t.Fatalf("ListArchives: %v, %d archives; want the one archive", err, len(got))
		}
		wantArchiveLogbooks(t, "Home", got[0].Logbooks, caNamespaceWant)
	})
}

// caNamespaceLogbooks: an adopted logbook, and an unadopted one whose legacy
// name is that logbook's UUID text; distinct identifier namespaces.
func caNamespaceLogbooks() []string {
	return []string{caLB(caByUUID(caLB1), "main", 1, 0), caLegacyLB(caLB1, "Field", 1, 1)}
}

var caNamespaceWant = []CloudArchiveLogbook{
	{UUID: ptr(caLB1), Label: "main", Callsign: "7Q5MLV", Count: 1},
	{LegacyName: ptr(caLB1), Label: "Field", Count: 1, Tombstones: 1},
}

func TestArchiveClient_CA3_OnlyACompleteUnambiguousListing(t *testing.T) {
	managed := caListed(caArchive, "Contest", false, caManagedLogbooks())
	home := caListed(caHome, "Home", true, caHomeLogbooks())
	lb := func(identity string) string {
		return caListing(caListed(caArchive, "Contest", false, []string{identity}))
	}
	for name, body := range map[string]string{
		"no archives array":           `{}`,
		"null archives":               `{"archives":null}`,
		"an archive without uuid":     caListing(`{"label":"Contest","legacy":false,"logbooks":[]}`),
		"an invalid archive uuid":     caListing(`{"uuid":"nope","label":"Contest","legacy":false,"logbooks":[]}`),
		"an archive without label":    caListing(`{"uuid":"` + caArchive + `","legacy":false,"logbooks":[]}`),
		"an archive without legacy":   caListing(`{"uuid":"` + caArchive + `","label":"Contest","logbooks":[]}`),
		"a null legacy marker":        caListing(`{"uuid":"` + caArchive + `","label":"Contest","legacy":null,"logbooks":[]}`),
		"an archive without logbooks": caListing(`{"uuid":"` + caArchive + `","label":"Contest","legacy":false}`),
		"null logbooks":               caListing(`{"uuid":"` + caArchive + `","label":"Contest","legacy":false,"logbooks":null}`),
		"a duplicate archive in another case": caListing(managed,
			caListed(strings.ToUpper(caArchive), "Other", false, nil)),
		"a duplicate logbook uuid in another case": caListing(managed,
			caListed(caSameLabel, "Contest", false, []string{caLB(caByUUID(strings.ToUpper(caLB1)), "main", 0, 0)})),
		"a duplicate legacy name": caListing(caListed(caHome, "Home", true,
			[]string{caLegacyLB("portable", "Field", 0, 0), caLegacyLB("portable", "Other", 0, 0)})),
		"a logbook with neither identifier": lb(caLB(`"uuid":null`, "Field", 0, 0)),
		"a logbook without the uuid member": lb(caLB(`"legacy_name":"portable"`, "Field", 0, 0)),
		"a logbook with a null legacy name": lb(caLB(`"uuid":null,"legacy_name":null`, "Field", 0, 0)),
		"a logbook with both identifiers":   lb(caLB(`"uuid":"`+caLB1+`","legacy_name":"main"`, "main", 0, 0)),
		"an invalid logbook uuid":           lb(caLB(`"uuid":"nope"`, "main", 0, 0)),
		"a UUIDv4 archive":                  caListing(caListed(caV4Archive, "Contest", false, nil)),
		"a UUIDv4 logbook":                  lb(caLB(caByUUID(caV4LB), "main", 0, 0)),
		"a negative tombstone count":        lb(caLB(caByUUID(caLB1), "main", 0, -1)),
		"a logbook whose uuid repeats":      lb(caLB(`"uuid":null,"uuid":"`+caLB1+`","legacy_name":"portable"`, "Field", 0, 0)),
		"a logbook without label":           lb(`{"uuid":"` + caLB1 + `","callsign":"7Q5MLV","count":0,"tombstones":0}`),
		"a logbook without callsign":        lb(`{"uuid":"` + caLB1 + `","label":"main","count":0,"tombstones":0}`),
		"a logbook without count":           lb(`{"uuid":"` + caLB1 + `","label":"main","callsign":"7Q5MLV","tombstones":0}`),
		"a logbook without tombstones":      lb(`{"uuid":"` + caLB1 + `","label":"main","callsign":"7Q5MLV","count":0}`),
		"a negative count":                  lb(caLB(caByUUID(caLB1), "main", -1, 0)),
		"more tombstones than rows":         lb(caLB(caByUUID(caLB1), "main", 1, 2)),
		"cut off":                           strings.TrimSuffix(caListing(managed, home), "]}\n"),
		"trailing data":                     caListing(managed) + `{"archives":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			s := newCAServer(t, body, "")
			got, err := ListArchives(context.Background(), testConfig(s.URL))
			if err == nil || got != nil {
				t.Fatalf("err %v, %d archive(s); want the listing refused with none", err, len(got))
			}
		})
	}
	t.Run("control: the full listing", func(t *testing.T) {
		s := newCAServer(t, caGoodListing(), "")
		if got, err := ListArchives(context.Background(), testConfig(s.URL)); err != nil || len(got) != 3 {
			t.Fatalf("control: %v, %d archives; want the unmutated listing read", err, len(got))
		}
	})
	t.Run("control: no archives", func(t *testing.T) {
		s := newCAServer(t, caListing(), "")
		got, err := ListArchives(context.Background(), testConfig(s.URL))
		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("an empty listing: %v, %+v; want accepted, empty and not nil", err, got)
		}
	})
}

type caWantRow struct {
	logbook  int
	revision int64
	at       time.Time
	gone     bool
}

func wantExportRows(t *testing.T, e *ArchiveExport, want map[string]caWantRow) {
	t.Helper()
	if len(e.Qsos) != len(want) {
		t.Fatalf("%d rows; want %d", len(e.Qsos), len(want))
	}
	seen := map[string]bool{}
	for _, q := range e.Qsos {
		w, ok := want[q.UUID]
		if !ok {
			t.Fatalf("row %s not wanted", q.UUID)
		}
		if seen[q.UUID] {
			t.Fatalf("row %s returned twice", q.UUID)
		}
		seen[q.UUID] = true
		if q.Logbook != w.logbook {
			t.Fatalf("row %s is in logbook %d; want %d", q.UUID, q.Logbook, w.logbook)
		}
		if q.Revision != w.revision || !q.ModifiedAt.Equal(w.at) || q.ModifiedAt.Nanosecond() != w.at.Nanosecond() {
			t.Fatalf("row %s revision %d modified_at %s; want %d at %s", q.UUID, q.Revision,
				q.ModifiedAt.Format(time.RFC3339Nano), w.revision, w.at.Format(time.RFC3339Nano))
		}
		if (q.DeletedAt != nil) != w.gone || (w.gone && !q.DeletedAt.Equal(caGoneAtTime)) {
			t.Fatalf("row %s deleted_at %v; want tombstone %v at %s", q.UUID, q.DeletedAt, w.gone, caGoneAt)
		}
		if !bytes.Equal(q.Qso, []byte(caPayload)) {
			t.Fatalf("row %s payload\n %s\nwant byte for byte\n %s", q.UUID, q.Qso, caPayload)
		}
	}
	for uuid := range want {
		if !seen[uuid] {
			t.Fatalf("row %s missing", uuid)
		}
	}
}

func TestArchiveClient_CA4_TheExport(t *testing.T) {
	t.Run("managed", func(t *testing.T) {
		s := newCAServer(t, "", caManagedDoc())
		e, err := FetchArchiveExport(context.Background(), testConfig(s.URL), caArchive)
		if err != nil {
			t.Fatalf("FetchArchiveExport: %v", err)
		}
		path := "/v1/archives/" + caArchive + "/export"
		if r := s.request(path); r == nil || r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer tok-123" {
			t.Fatalf("requests %v; want GET %s with the bearer token", s.paths(), path)
		}
		if e.UUID != caArchive || e.Label != "Contest" || e.Legacy {
			t.Fatalf("head = %s %q legacy %v; want Contest, not legacy", e.UUID, e.Label, e.Legacy)
		}
		wantArchiveLogbooks(t, "head", e.Logbooks, caManagedWant)
		wantExportRows(t, e, map[string]caWantRow{
			caQ1: {0, 3, caAtTime, false},
			caQ2: {0, 4, caGoneAtTime, true},
		})
	})
	t.Run("the adopted legacy archive", func(t *testing.T) {
		s := newCAServer(t, "", caHomeDoc())
		e, err := FetchArchiveExport(context.Background(), testConfig(s.URL), caHome)
		if err != nil {
			t.Fatalf("FetchArchiveExport: %v", err)
		}
		if e.UUID != caHome || !e.Legacy {
			t.Fatalf("head = %s legacy %v; want Home with the legacy marker", e.UUID, e.Legacy)
		}
		wantArchiveLogbooks(t, "head", e.Logbooks, caHomeWant)
		wantExportRows(t, e, map[string]caWantRow{
			caQ3: {0, 0, caAtTime, false},
			caQ4: {1, 0, caAtTime, false},
			caQ5: {2, 0, caAtTime, false},
			caQ6: {2, 0, caGoneAtTime, true},
			caQ7: {3, 0, caAtTime, false},
		})
	})
	t.Run("a logbook UUID in another case", func(t *testing.T) {
		s := newCAServer(t, "", caDoc(caHead(caArchive, "Contest", false), caManagedLogbooks(),
			[]string{caRow(caQ1, caInUUID(strings.ToUpper(caLB1)), 3, false), caRow(caQ2, caInUUID(caLB1), 4, true)}))
		// Asked in upper case, the request carries the canonical lower-case UUID.
		e, err := FetchArchiveExport(context.Background(), testConfig(s.URL), strings.ToUpper(caArchive))
		if err != nil || s.request("/v1/archives/"+caArchive+"/export") == nil {
			t.Fatalf("FetchArchiveExport(%s): %v; requests %v; want the canonical lower-case path asked and read", strings.ToUpper(caArchive), err, s.paths())
		}
		wantExportRows(t, e, map[string]caWantRow{caQ1: {0, 3, caAtTime, false}, caQ2: {0, 4, caGoneAtTime, true}})
	})
	t.Run("a legacy name spelled like another logbook's UUID", func(t *testing.T) {
		s := newCAServer(t, "", caDoc(caHead(caHome, "Home", true), caNamespaceLogbooks(),
			[]string{caRow(caQ1, caInUUID(caLB1), 0, false), caRow(caQ2, caInName(caLB1), 0, true)}))
		e, err := FetchArchiveExport(context.Background(), testConfig(s.URL), caHome)
		if err != nil {
			t.Fatalf("FetchArchiveExport: %v", err)
		}
		wantArchiveLogbooks(t, "head", e.Logbooks, caNamespaceWant)
		wantExportRows(t, e, map[string]caWantRow{caQ1: {0, 0, caAtTime, false}, caQ2: {1, 0, caGoneAtTime, true}})
	})
	t.Run("a payload repeating a key inside it", func(t *testing.T) {
		const payload = `{"call":"DL9UW","call":"G4ABC","band":"20m"}`
		s := newCAServer(t, "", caDoc(caHead(caArchive, "Contest", false), []string{caLB(caByUUID(caLB1), "main", 1, 0)},
			[]string{`{"uuid":"` + caQ1 + `","logbook_uuid":"` + caLB1 + `","modified_at":"` + caAt + `","qso":` + payload + `}`}))
		e, err := FetchArchiveExport(context.Background(), testConfig(s.URL), caArchive)
		if err != nil || len(e.Qsos) != 1 || string(e.Qsos[0].Qso) != payload {
			t.Fatalf("FetchArchiveExport: %v; want the payload returned verbatim, opaque: %s", err, payload)
		}
	})
	t.Run("an empty archive", func(t *testing.T) {
		s := newCAServer(t, "", caDoc(caHead(caArchive, "Contest", false), nil, nil))
		e, err := FetchArchiveExport(context.Background(), testConfig(s.URL), caArchive)
		if err != nil || e == nil || e.Logbooks == nil || e.Qsos == nil || len(e.Logbooks)+len(e.Qsos) != 0 {
			t.Fatalf("empty arrays: %v, %+v; want a valid empty archive with empty, non-nil arrays", err, e)
		}
	})
}

// caRefused fetches archive U's export and requires it refused with no
// result and no payload in the error.
func caRefused(t *testing.T, body string) {
	t.Helper()
	caRefusedFor(t, caArchive, body)
}

// caRefusedFor asks archive's export, answered with body.
func caRefusedFor(t *testing.T, archive, body string) {
	t.Helper()
	s := newCAServer(t, "", body)
	s.routes["/v1/archives/"+archive+"/export"] = caAnswer(http.StatusOK, body)
	got, err := FetchArchiveExport(context.Background(), testConfig(s.URL), archive)
	if err == nil || got != nil {
		t.Fatalf("err %v, export %v; want refused with no result", err, got != nil)
	}
	if strings.Contains(err.Error(), "DL9UW") || strings.Contains(err.Error(), "9007199254740993") {
		t.Fatalf("the refusal quotes a payload: %q", err)
	}
}

// caAccepted is the control for a refusal table: the unmutated document is
// read.
func caAccepted(t *testing.T, archive, body string) {
	t.Helper()
	s := newCAServer(t, "", body)
	s.routes["/v1/archives/"+caOther+"/export"] = s.routes["/v1/archives/"+caArchive+"/export"]
	if got, err := FetchArchiveExport(context.Background(), testConfig(s.URL), archive); err != nil || got == nil {
		t.Fatalf("control: err %v; want the unmutated document read", err)
	}
}

func TestArchiveClient_CA5_IdentityAndMembership(t *testing.T) {
	caAccepted(t, caArchive, caManagedDoc())
	caAccepted(t, caHome, caHomeDoc())
	head := caHead(caArchive, "Contest", false)
	lbs, rows := caManagedLogbooks(), caManagedRows()
	// Each case has one defect. Its counts are those a client that put an
	// unresolved row in the first logbook would find correct, so counting
	// cannot stand in for membership.
	legacyLBs := []string{caLegacyLB("portable", "Field", 2, 0), caLegacyLB("contest", "Field", 0, 0)}
	with := func(extra string) []string { return append(append([]string{}, rows...), extra) }
	plusOne := []string{caLB(caByUUID(caLB1), "main", 3, 1), caLB(caByUUID(caLB2), "empty", 0, 0)}
	for name, body := range map[string]string{
		"another archive":          caDoc(caHead(caOther, "Contest", false), lbs, rows),
		"no archive":               `{"logbooks":[` + strings.Join(lbs, ",") + `],"qsos":[` + strings.Join(rows, ",") + `]}`,
		"an archive without uuid":  `{"archive":{"label":"Contest","legacy":false},"logbooks":[],"qsos":[]}`,
		"no legacy marker":         `{"archive":{"uuid":"` + caArchive + `","label":"Contest"},"logbooks":[],"qsos":[]}`,
		"a null legacy marker":     `{"archive":{"uuid":"` + caArchive + `","label":"Contest","legacy":null},"logbooks":[],"qsos":[]}`,
		"an archive without label": `{"archive":{"uuid":"` + caArchive + `","legacy":false},"logbooks":[],"qsos":[]}`,
		"a duplicate logbook uuid in another case": caDoc(head,
			append(append([]string{}, lbs...), caLB(caByUUID(strings.ToUpper(caLB2)), "again", 0, 0)), rows),
		"a duplicate legacy name": caDoc(head,
			[]string{caLegacyLB("portable", "Field", 1, 0), caLegacyLB("portable", "Other", 0, 0)},
			[]string{caRow(caQ4, caInName("portable"), 0, false)}),
		"a duplicate qso uuid in another case": caDoc(head, plusOne,
			with(caRow(strings.ToUpper(caQ1), caInUUID(caLB1), 3, false))),
		"a row in a logbook outside the head": caDoc(head, lbs,
			[]string{caRow(caQ1, caInUUID(caLB3), 3, false), caRow(caQ2, caInUUID(caLB1), 4, true)}),
		"a row in a logbook outside the head, counted as dropped": caDoc(head,
			[]string{caLB(caByUUID(caLB1), "main", 1, 1), caLB(caByUUID(caLB2), "empty", 0, 0)},
			[]string{caRow(caQ1, caInUUID(caLB3), 3, false), caRow(caQ2, caInUUID(caLB1), 4, true)}),
		"a row under a legacy name outside the head": caDoc(head, legacyLBs,
			[]string{caRow(caQ4, caInName("portable"), 0, false), caRow(caQ5, caInName("Contest"), 0, false)}),
		"a row naming no logbook": caDoc(head, plusOne,
			with(`{"uuid":"`+caQ3+`","modified_at":"`+caAt+`","qso":`+caPayload+`}`)),
		"a row with a null uuid and no legacy name": caDoc(head, legacyLBs,
			[]string{caRow(caQ4, caInName("portable"), 0, false), caRow(caQ5, `"logbook_uuid":null`, 0, false)}),
		"a row with both identifiers": caDoc(head, plusOne,
			with(caRow(caQ3, `"logbook_uuid":"`+caLB1+`","logbook_legacy_name":"main"`, 0, false))),
		"an empty legacy name where the head has none": caDoc(head,
			[]string{caLegacyLB("portable", "Field", 1, 0)},
			[]string{caRow(caQ4, caInName(""), 0, false)}),
		"an absent legacy name where the head has an empty one": caDoc(head,
			[]string{caLegacyLB("", "Field", 1, 0)},
			[]string{caRow(caQ4, `"logbook_uuid":null,"logbook_legacy_name":null`, 0, false)}),
		"a head logbook with neither identifier": caDoc(head,
			[]string{caLB(`"uuid":null`, "Field", 0, 0)}, nil),
		"a head logbook with a null legacy name": caDoc(head,
			[]string{caLB(`"uuid":null,"legacy_name":null`, "Field", 0, 0)}, nil),
		"a head logbook with both identifiers": caDoc(head,
			[]string{caLB(`"uuid":"`+caLB1+`","legacy_name":"main"`, "main", 0, 0)}, nil),
		"a row without uuid": caDoc(head, plusOne,
			with(`{"logbook_uuid":"`+caLB1+`","modified_at":"`+caAt+`","qso":`+caPayload+`}`)),
		"a row with an invalid uuid": caDoc(head, plusOne, with(caRow("nope", caInUUID(caLB1), 0, false))),
		"a row without modified_at": caDoc(head, plusOne,
			with(`{"uuid":"`+caQ3+`","logbook_uuid":"`+caLB1+`","qso":`+caPayload+`}`)),
		"a row without qso": caDoc(head, plusOne,
			with(`{"uuid":"`+caQ3+`","logbook_uuid":"`+caLB1+`","modified_at":"`+caAt+`"}`)),
		"a row with a null qso": caDoc(head, plusOne,
			with(`{"uuid":"`+caQ3+`","logbook_uuid":"`+caLB1+`","modified_at":"`+caAt+`","qso":null}`)),
		"a UUIDv4 logbook": caDoc(head,
			[]string{caLB(caByUUID(caV4LB), "main", 1, 0)},
			[]string{caRow(caQ1, caInUUID(caV4LB), 0, false)}),
		"a UUIDv4 qso": caDoc(head, plusOne, with(caRow(caV4Q, caInUUID(caLB1), 0, false))),
		// A repeated member, with counts that agree whichever occurrence wins.
		"a head logbook's uuid repeated, null then a UUID": caDoc(head,
			[]string{caLB(`"uuid":null,"uuid":"`+caLB1+`","legacy_name":"portable"`, "Field", 1, 0)},
			[]string{caRow(caQ4, caInName("portable"), 0, false)}),
		"a head logbook's uuid repeated, a UUID then null": caDoc(head,
			[]string{caLB(`"uuid":"`+caLB1+`","uuid":null,"legacy_name":"portable"`, "Field", 1, 0)},
			[]string{caRow(caQ4, caInName("portable"), 0, false)}),
		"a row's logbook_uuid repeated, null then a UUID": caDoc(head,
			[]string{caLB(caByUUID(caLB1), "main", 0, 0), caLegacyLB("portable", "Field", 1, 0)},
			[]string{caRow(caQ4, `"logbook_uuid":null,"logbook_uuid":"`+caLB1+`","logbook_legacy_name":"portable"`, 0, false)}),
		"a row's logbook_uuid repeated, a UUID then null": caDoc(head,
			[]string{caLB(caByUUID(caLB1), "main", 0, 0), caLegacyLB("portable", "Field", 1, 0)},
			[]string{caRow(caQ4, `"logbook_uuid":"`+caLB1+`","logbook_uuid":null,"logbook_legacy_name":"portable"`, 0, false)}),
		"a repeated legacy marker": `{"archive":{"uuid":"` + caArchive + `","label":"Contest","legacy":true,"legacy":false},"logbooks":[],"qsos":[]}`,
		"a row's qso repeated": caDoc(head, plusOne,
			with(`{"uuid":"`+caQ3+`","logbook_uuid":"`+caLB1+`","modified_at":"`+caAt+`","qso":{"call":"G4ABC"},"qso":`+caPayload+`}`)),
	} {
		t.Run(name, func(t *testing.T) { caRefused(t, body) })
	}
	t.Run("a UUIDv4 archive", func(t *testing.T) {
		caRefusedFor(t, caV4Archive, caDoc(caHead(caV4Archive, "Contest", false), lbs, rows))
	})
}

func TestArchiveClient_CA6_EachLogbooksCounts(t *testing.T) {
	head := caHead(caArchive, "Contest", false)
	// Both logbooks have rows: LB1 a live row and a tombstone, LB2 a live row.
	rows := []string{caRow(caQ1, caInUUID(caLB1), 3, false), caRow(caQ2, caInUUID(caLB1), 4, true), caRow(caQ3, caInUUID(caLB2), 1, false)}
	counted := func(lb1Count, lb1Tombs, lb2Count, lb2Tombs int) []string {
		return []string{caLB(caByUUID(caLB1), "main", lb1Count, lb1Tombs), caLB(caByUUID(caLB2), "second", lb2Count, lb2Tombs)}
	}
	caAccepted(t, caArchive, caDoc(head, counted(2, 1, 1, 0), rows))
	caAccepted(t, caArchive, caManagedDoc())
	legacyRows := []string{caRow(caQ4, caInName("portable"), 0, false), caRow(caQ5, caInName("contest"), 0, false),
		caRow(caQ6, caInName("contest"), 0, true)}
	caAccepted(t, caArchive, caDoc(caHead(caArchive, "Home", true),
		[]string{caLegacyLB("portable", "Field", 1, 0), caLegacyLB("contest", "Field", 2, 1)}, legacyRows))

	// Every mutated count stays within its bounds (non-negative, tombstones
	// no more than rows) except the one case named for it, so only the
	// per-logbook comparison can refuse the rest. The first two keep the
	// totals (3 rows, 1 tombstone).
	for name, logbooks := range map[string][]string{
		"rows counted under the wrong logbook":           counted(1, 1, 2, 0),
		"only the tombstone counted under the wrong one": counted(2, 0, 1, 1),
		"too few rows":               counted(1, 1, 1, 0),
		"too few tombstones":         counted(2, 0, 1, 0),
		"too many rows":              counted(2, 1, 2, 0),
		"a negative tombstone count": counted(2, 1, 1, -1),
	} {
		t.Run(name, func(t *testing.T) { caRefused(t, caDoc(head, logbooks, rows)) })
	}
	// The empty logbook's true counts are zero, so a client reading a missing
	// or null field as zero would find them correct.
	managedRows := caManagedRows()
	lb1 := caLB(caByUUID(caLB1), "main", 2, 1)
	for name, empty := range map[string]string{
		"an empty logbook counting a row":     caLB(caByUUID(caLB2), "empty", 1, 0),
		"an empty logbook without count":      `{"uuid":"` + caLB2 + `","label":"empty","callsign":"7Q5MLV","tombstones":0}`,
		"an empty logbook without tombstones": `{"uuid":"` + caLB2 + `","label":"empty","callsign":"7Q5MLV","count":0}`,
		"an empty logbook with a null count":  `{"uuid":"` + caLB2 + `","label":"empty","callsign":"7Q5MLV","count":null,"tombstones":0}`,
	} {
		t.Run(name, func(t *testing.T) { caRefused(t, caDoc(head, []string{lb1, empty}, managedRows)) })
	}
	t.Run("legacy rows counted under the wrong name", func(t *testing.T) {
		caRefused(t, caDoc(caHead(caArchive, "Home", true),
			[]string{caLegacyLB("portable", "Field", 2, 1), caLegacyLB("contest", "Field", 1, 0)}, legacyRows))
	})
}

func TestArchiveClient_CA7_OnlyACompleteDocument(t *testing.T) {
	head := caHead(caArchive, "Contest", false)
	lbs, rows := strings.Join(caManagedLogbooks(), ","), strings.Join(caManagedRows(), ",")
	full := caManagedDoc()
	caAccepted(t, caArchive, full)
	caAccepted(t, caArchive, `{`+head+`,"logbooks":[`+caLB(caByUUID(caLB2), "empty", 0, 0)+`],"qsos":[]}`)
	for name, body := range map[string]string{
		"no logbooks array, otherwise empty": `{` + head + `,"qsos":[]}`,
		"null logbooks, otherwise empty":     `{` + head + `,"logbooks":null,"qsos":[]}`,
		"no qsos array, otherwise empty":     `{` + head + `,"logbooks":[` + caLB(caByUUID(caLB2), "empty", 0, 0) + `]}`,
		"null qsos, otherwise empty":         `{` + head + `,"logbooks":[` + caLB(caByUUID(caLB2), "empty", 0, 0) + `],"qsos":null}`,
		"no logbooks array":                  `{` + head + `,"qsos":[` + rows + `]}`,
		"null logbooks":                      `{` + head + `,"logbooks":null,"qsos":[` + rows + `]}`,
		"no qsos array":                      `{` + head + `,"logbooks":[` + lbs + `]}`,
		"null qsos":                          `{` + head + `,"logbooks":[` + lbs + `],"qsos":null}`,
		"cut off after a valid row":          `{` + head + `,"logbooks":[` + lbs + `],"qsos":[` + caManagedRows()[0] + `,`,
		"cut off after the array":            `{` + head + `,"logbooks":[` + lbs + `],"qsos":[` + rows + `]`,
		"cut off mid-row":                    `{` + head + `,"logbooks":[` + lbs + `],"qsos":[` + caManagedRows()[0] + `,{"uuid":"0197f9a0-00`,
		"trailing data":                      full + `garbage`,
		"a second document":                  full + full,
		"empty":                              ``,
	} {
		t.Run(name, func(t *testing.T) { caRefused(t, body) })
	}
}

// caReachedArchiveRoute: the client asked an archive route (not only the
// version), so a refusal after it is the client's own.
func caReachedArchiveRoute(s *caServer) bool {
	for _, p := range s.paths() {
		if p == "GET /v1/archives" || p == "GET /v1/archives/"+caArchive+"/export" {
			return true
		}
	}
	return false
}

func TestArchiveClient_CA8_TransportStatus(t *testing.T) {
	const bodyMarker = "BODY-MARKER-tok-123"
	for call, c := range caCalls {
		run := c.run
		for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable} {
			t.Run(call+"/HTTP "+strconv.Itoa(status), func(t *testing.T) {
				s := newCAServer(t, "", "")
				for p := range s.routes {
					s.routes[p] = caAnswer(status, `{"code":"x","message":"`+bodyMarker+`"}`)
				}
				ok, err := run(context.Background(), s)
				if !caReachedArchiveRoute(s) || err == nil || ok {
					t.Fatalf("HTTP %d: requests %v, err %v, result %v; want the archive route asked, an error and no result", status, s.paths(), err, ok)
				}
				if !strings.Contains(err.Error(), strconv.Itoa(status)) {
					t.Fatalf("HTTP %d: err %q; want the status named", status, err)
				}
				if strings.Contains(err.Error(), "tok-123") || strings.Contains(err.Error(), "BODY-MARKER") {
					t.Fatalf("HTTP %d: the error carries the token or the body: %q", status, err)
				}
				if stderr.Is(err, ErrArchiveNotFound) || stderr.Is(err, ErrArchiveExportUnsupported) {
					t.Fatalf("HTTP %d: err %v; want neither not-found nor unsupported", status, err)
				}
			})
		}
	}
}

func TestArchiveClient_CA8_Redirects(t *testing.T) {
	for call, c := range caCalls {
		run := c.run
		t.Run(call+"/a redirect to another origin", func(t *testing.T) {
			var elsewhere []string
			other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				elsewhere = append(elsewhere, r.URL.Path+" auth="+r.Header.Get("Authorization"))
			}))
			defer other.Close()
			s := newCAServer(t, "", "")
			for p := range s.routes {
				s.routes[p] = func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, other.URL+r.URL.Path, http.StatusTemporaryRedirect)
				}
			}
			ok, err := run(context.Background(), s)
			if !caReachedArchiveRoute(s) {
				t.Fatalf("requests %v; want the archive route asked, and its redirect refused", s.paths())
			}
			if err == nil || ok || len(elsewhere) != 0 {
				t.Fatalf("err %v, result %v, other origin saw %v; want refused and the other origin never contacted", err, ok, elsewhere)
			}
			if strings.Contains(err.Error(), "tok-123") || strings.Contains(err.Error(), other.URL) {
				t.Fatalf("the error carries the token or the target: %q", err)
			}
		})
		t.Run(call+"/a redirect on the same origin", func(t *testing.T) {
			s := newCAServer(t, caGoodListing(), caManagedDoc())
			for p, h := range s.routes {
				s.routes["/moved"+p] = h
				s.routes[p] = func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "/moved"+r.URL.Path, http.StatusTemporaryRedirect)
				}
			}
			ok, err := run(context.Background(), s)
			if !caReachedArchiveRoute(s) {
				t.Fatalf("requests %v; want the archive route asked, and its redirect refused", s.paths())
			}
			if err == nil || ok {
				t.Fatalf("err %v, result %v; want the redirect refused, not followed", err, ok)
			}
			for _, p := range s.paths() {
				if strings.Contains(p, "/moved") {
					t.Fatalf("requests %v; want the redirect never followed", s.paths())
				}
			}
		})
	}
}

func TestArchiveClient_CA8_Cancellation(t *testing.T) {
	for call, c := range caCalls {
		run := c.run
		t.Run(call+"/cancelled before the answer", func(t *testing.T) {
			s := newCAServer(t, "", "")
			asked := make(chan struct{})
			for p := range s.routes {
				s.routes[p] = func(w http.ResponseWriter, r *http.Request) {
					close(asked)
					<-r.Context().Done()
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() { <-asked; cancel() }()
			ok, err := run(ctx, s)
			if !stderr.Is(err, context.Canceled) || ok {
				t.Fatalf("err %v, result %v; want context.Canceled and no result", err, ok)
			}
		})
	}
	for call, c := range caCalls {
		run := c.run
		t.Run(call+"/cancelled in the body read", func(t *testing.T) {
			s := newCAServer(t, "", "")
			for p := range s.routes {
				s.routes[p] = func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"archive":{"uuid":"` + caArchive + `","label":"Contest","legacy":false}`))
					w.(http.Flusher).Flush()
					<-r.Context().Done()
				}
			}
			barrier := &caBodyBarrier{base: http.DefaultTransport, reading: make(chan struct{})}
			saved := archiveTransport
			archiveTransport = barrier
			t.Cleanup(func() { archiveTransport = saved })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			// The barrier: cancel only once the client reads its archive
			// route's response body.
			go func() { <-barrier.reading; cancel() }()
			ok, err := run(ctx, s)
			if !barrier.entered() {
				t.Fatalf("err %v; the client never read the response body", err)
			}
			if !stderr.Is(err, context.Canceled) || ok {
				t.Fatalf("err %v, result %v; want context.Canceled and no result", err, ok)
			}
		})
	}
}

// caBodyBarrier wraps the client's transport: the first Read of an archive
// route's response body closes reading.
type caBodyBarrier struct {
	base    http.RoundTripper
	reading chan struct{}
	once    sync.Once
}

func (b *caBodyBarrier) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := b.base.RoundTrip(r)
	if err != nil || r.URL.Path == "/v1/version" {
		return resp, err
	}
	resp.Body = &caSignalBody{ReadCloser: resp.Body, b: b}
	return resp, nil
}

func (b *caBodyBarrier) entered() bool {
	select {
	case <-b.reading:
		return true
	default:
		return false
	}
}

type caSignalBody struct {
	io.ReadCloser
	b *caBodyBarrier
}

func (s *caSignalBody) Read(p []byte) (int, error) {
	s.b.once.Do(func() { close(s.b.reading) })
	return s.ReadCloser.Read(p)
}

func TestArchiveClient_CA8_NotFound(t *testing.T) {
	t.Run("the export's 404", func(t *testing.T) {
		s := newCAServer(t, "", "")
		_, err := FetchArchiveExport(context.Background(), testConfig(s.URL), caOther)
		if !stderr.Is(err, ErrArchiveNotFound) {
			t.Fatalf("404: err %v; want ErrArchiveNotFound", err)
		}
	})
}

// caPadded is doc padded with trailing whitespace, which JSON allows, to
// exactly n bytes.
func caPadded(t *testing.T, doc string, n int) string {
	t.Helper()
	if len(doc) > n {
		t.Fatalf("the document (%d bytes) is larger than %d", len(doc), n)
	}
	return doc + strings.Repeat(" ", n-len(doc))
}

// caStreamed answers body with its headers flushed first, so the response
// declares no Content-Length (small bodies would otherwise get one).
func caStreamed(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte(body))
	}
}

// caStreamedServer serves body on route, streamed, and checks the fixture
// itself: the response must declare no length.
func caStreamedServer(t *testing.T, route, body string) *caServer {
	t.Helper()
	s := newCAServer(t, "", "")
	s.routes[route] = caStreamed(body)
	probe := httptest.NewServer(caStreamed(body))
	defer probe.Close()
	resp, err := http.Get(probe.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.ContentLength != -1 || resp.Header.Get("Content-Length") != "" {
		t.Fatalf("fixture: the streamed response declares Content-Length %d; want none", resp.ContentLength)
	}
	return s
}

func TestArchiveClient_CA9_SizeLimits(t *testing.T) {
	if maxResponseBytes != 1<<20 || archiveExportLimit != 256<<20 {
		t.Fatalf("limits: listing %d, export %d; want 1 MiB and 256 MiB", maxResponseBytes, archiveExportLimit)
	}
	// The export at its real limit would buffer 256 MiB per case; its cases
	// run against a lowered limit, the real one pinned above.
	exportLimit := len(caManagedDoc()) + 512
	saved := archiveExportLimit
	archiveExportLimit = int64(exportLimit)
	t.Cleanup(func() { archiveExportLimit = saved })
	for _, c := range []struct {
		name  string
		read  func(t *testing.T, body string) (bool, error)
		doc   string
		limit int
	}{
		{"listing", func(t *testing.T, body string) (bool, error) {
			s := caStreamedServer(t, "/v1/archives", body)
			got, err := ListArchives(context.Background(), testConfig(s.URL))
			return got != nil, err
		}, caGoodListing(), maxResponseBytes},
		{"export", func(t *testing.T, body string) (bool, error) {
			s := caStreamedServer(t, "/v1/archives/"+caArchive+"/export", body)
			got, err := FetchArchiveExport(context.Background(), testConfig(s.URL), caArchive)
			return got != nil, err
		}, caManagedDoc(), exportLimit},
	} {
		t.Run(c.name+"/exactly the limit", func(t *testing.T) {
			if ok, err := c.read(t, caPadded(t, c.doc, c.limit)); err != nil || !ok {
				t.Fatalf("a valid document of exactly %d bytes: %v; want it read", c.limit, err)
			}
		})
		t.Run(c.name+"/one byte over", func(t *testing.T) {
			if ok, err := c.read(t, caPadded(t, c.doc, c.limit+1)); err == nil || ok {
				t.Fatalf("%d bytes: err %v, result %v; want refused", c.limit+1, err, ok)
			}
		})
		t.Run(c.name+"/a valid document within the limit, more beyond", func(t *testing.T) {
			if ok, err := c.read(t, caPadded(t, c.doc, c.limit)+"garbage"); err == nil || ok {
				t.Fatalf("a valid %d-byte prefix and more: err %v, result %v; want refused, not read as the prefix", c.limit, err, ok)
			}
		})
	}
}
