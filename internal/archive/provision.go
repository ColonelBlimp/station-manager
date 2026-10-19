package archive

import (
	"context"
	"encoding/json"
	stderr "errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// Whole-archive provisioning (W-0021 Q7, ADR 0071 refinement 2026-10-16):
// re-create cloud archive U as a NEW managed file from its validated export,
// carrying U's UUID and its logbooks' UUIDs, every row round-tripped through
// the restore adapter, the worker's read and the forwarder's projection.
// Nothing is ever written into a file that exists.

// RowRestorer writes one restored QSO into the file under construction: the
// restore adapter (qsoservice.Restore), injected as validCallsign is so this
// package does not import the QSO service. A row it does not store is an
// error.
type RowRestorer func(ctx context.Context, db *sqlite.Service, logbookID int64, qso types.Qso) error

// SetRowRestorer wires the restore adapter Provision writes rows with.
func (m *Manager) SetRowRestorer(r RowRestorer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.restorer = r
}

// ProvisionRequest is the validated whole-archive export and the default
// logbook's UUID, which must name a member; it may be empty when the export
// has one logbook.
type ProvisionRequest struct {
	Export         *smcloud.ArchiveExport
	DefaultLogbook string
}

// PlannedLogbook is one logbook the file will hold: its cloud identity and
// metadata, its rows (tombstones included) and whether it is the default.
type PlannedLogbook struct {
	UUID, Name, Callsign string
	Rows, Tombstones     int
	Default              bool
}

// ProvisionPlan is what the preflight accepted: U, its managed path, the kept
// managed entry (nil when one will be added) and the logbooks.
type ProvisionPlan struct {
	ArchiveUUID string
	Path        string
	Entry       *types.QsoArchiveConfig
	Logbooks    []PlannedLogbook
}

// ProvisionResult is a published archive: its entry, whether the entry was
// added, and what is uncertain about the outcome. LeftoverTemp names a
// temporary name that could not be removed after an existing entry's file was
// published; DiagnoseCreatingArtefacts reports it at the next start.
type ProvisionResult struct {
	Plan                ProvisionPlan
	Entry               types.QsoArchiveConfig
	Added               bool
	DurabilityUncertain bool
	LeftoverTemp        string
}

// ProvisionRefusal is a provisioning refused for a stated reason, before
// anything of it was published. Code is stable; Path names the file a residue
// or destination refusal is about. Neither carries a value from the export.
type ProvisionRefusal struct {
	Code    string
	Path    string
	Message string
}

func (e *ProvisionRefusal) Error() string { return e.Code + ": " + e.Message }

// RoundTripMismatch names a row that would not push back unchanged and the
// fields that differ: uuid, logbook, revision, modified_at, deleted_at, or
// qso.<key> for a payload key. Never their values.
type RoundTripMismatch struct {
	UUID   string
	Fields []string
}

// RoundTripError refuses a provisioning whose rows would not push back
// unchanged; every mismatching row is listed, in export order.
type RoundTripError struct{ Rows []RoundTripMismatch }

func (e *RoundTripError) Error() string {
	parts := make([]string, 0, len(e.Rows))
	for _, r := range e.Rows {
		parts = append(parts, r.UUID+" ("+strings.Join(r.Fields, ", ")+")")
	}
	return fmt.Sprintf("%d row(s) do not round-trip: %s", len(e.Rows), strings.Join(parts, "; "))
}

// removeFile removes one file; a seam so a test can fail a removal.
var removeFile = os.Remove

func refuse(code, path, format string, args ...any) *ProvisionRefusal {
	return &ProvisionRefusal{Code: code, Path: path, Message: fmt.Sprintf(format, args...)}
}

// uuidPattern matches a UUID's shape: such a string names an identity and may
// be quoted in a refusal; anything else is referred to by position.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// PreflightProvision checks req as Provision would before creating anything,
// and reports the plan: the dry run. It needs no restore adapter and writes
// nothing.
func (m *Manager) PreflightProvision(req ProvisionRequest) (ProvisionPlan, error) {
	return m.preflight(m.cfg.Snapshot(), req)
}

// preflight refuses everything that can be refused before the temporary file
// exists: the export's own defects, the legacy archive, the default logbook,
// local constraints on the metadata, a catalogue entry for U that is not
// managed, and any file or crash residue at U's path.
func (m *Manager) preflight(snap config.Config, req ProvisionRequest) (ProvisionPlan, error) {
	e := req.Export
	if e == nil {
		return ProvisionPlan{}, refuse("no_export", "", "there is no export to provision from")
	}
	if e.Legacy {
		return ProvisionPlan{}, refuse("legacy_archive", "", "the archive is the station account's legacy archive (Home); provisioning it is not supported")
	}
	rows, tombstones, err := validExport(e)
	if err != nil {
		return ProvisionPlan{}, err
	}
	if len(e.Logbooks) == 0 {
		return ProvisionPlan{}, refuse("no_logbooks", "", "the archive has no logbook")
	}
	def, err := defaultLogbook(e, req.DefaultLogbook)
	if err != nil {
		return ProvisionPlan{}, err
	}
	id := strings.ToLower(e.UUID)
	plan := ProvisionPlan{ArchiveUUID: id, Path: filepath.Join(ManagedDir(snap), id+".db")}
	labels := map[string]string{}
	for i, lb := range e.Logbooks {
		uuid := *lb.UUID
		switch {
		case strings.TrimSpace(lb.Label) == "" || len(lb.Label) > maxLogbookName:
			return ProvisionPlan{}, refuse("invalid_logbook_label", "", "logbook %s has no usable label: it is empty or longer than %d characters", uuid, maxLogbookName)
		case labels[lb.Label] != "":
			return ProvisionPlan{}, refuse("logbook_label_collision", "", "logbooks %s and %s have the same label, which a local logbook name cannot share", labels[lb.Label], uuid)
		case lb.Callsign == "" || len(lb.Callsign) > 32 || !m.validCallsign(lb.Callsign):
			return ProvisionPlan{}, refuse("invalid_logbook_callsign", "", "logbook %s has no valid callsign", uuid)
		}
		labels[lb.Label] = uuid
		plan.Logbooks = append(plan.Logbooks, PlannedLogbook{UUID: uuid, Name: lb.Label, Callsign: lb.Callsign,
			Rows: rows[i], Tombstones: tombstones[i], Default: i == def})
	}
	if entry := snap.QsoArchiveByID(id); entry != nil {
		if entry.Ownership != types.QsoArchiveOwnershipManaged {
			return ProvisionPlan{}, refuse("entry_not_managed", "", "archive %s is in the catalogue as a %s archive at its recorded path; only a managed entry can be re-created", id, entry.Ownership)
		}
		kept := *entry
		plan.Entry = &kept
	} else if strings.TrimSpace(e.Label) == "" || len(e.Label) > maxLabelLen {
		return ProvisionPlan{}, refuse("invalid_archive_label", "", "the archive has no usable label for a new catalogue entry: it is empty or longer than %d characters", maxLabelLen)
	}
	for _, r := range []struct{ suffix, code string }{
		{"", "archive_file_exists"}, {"-wal", "archive_file_exists"}, {"-shm", "archive_file_exists"},
		{creatingSuffix, "temp_file_exists"}, {creatingSuffix + "-wal", "temp_file_exists"}, {creatingSuffix + "-shm", "temp_file_exists"},
	} {
		p := plan.Path + r.suffix
		if _, err := os.Lstat(p); err == nil {
			return ProvisionPlan{}, refuse(r.code, p, "%s already exists; it is never overwritten or removed by provisioning", p)
		} else if !os.IsNotExist(err) {
			return ProvisionPlan{}, fmt.Errorf("inspect %s: %w", p, err)
		}
	}
	return plan, nil
}

// validExport checks what a constructed export could get wrong and the
// client's validation would have refused: identities, membership, required
// row metadata, and counts. It returns each logbook's rows and tombstones.
func validExport(e *smcloud.ArchiveExport) (rows, tombstones []int, err error) {
	bad := func(format string, args ...any) ([]int, []int, error) {
		return nil, nil, refuse("invalid_export", "", format, args...)
	}
	if !utils.IsValidUUIDv7(e.UUID) {
		return bad("the archive UUID is not a UUIDv7")
	}
	logbooks := map[string]bool{}
	for i, lb := range e.Logbooks {
		if lb.UUID == nil {
			return nil, nil, refuse("logbook_without_uuid", "", "logbook %d of the archive has no UUID (a legacy logbook never adopted)", i)
		}
		u := *lb.UUID
		if !utils.IsValidUUIDv7(u) {
			return bad("logbook %d%s is not named by a UUIDv7", i, quoted(u))
		}
		if logbooks[strings.ToLower(u)] {
			return bad("logbook %s appears twice", u)
		}
		logbooks[strings.ToLower(u)] = true
	}
	rows, tombstones = make([]int, len(e.Logbooks)), make([]int, len(e.Logbooks))
	seen := map[string]bool{}
	for i, r := range e.Qsos {
		switch {
		case !utils.IsValidUUIDv7(r.UUID):
			return bad("row %d%s has no UUIDv7", i, quoted(r.UUID))
		case seen[strings.ToLower(r.UUID)]:
			return bad("row %s appears twice", r.UUID)
		case r.Logbook < 0 || r.Logbook >= len(e.Logbooks):
			return bad("row %s names logbook index %d, outside the archive's %d logbooks", r.UUID, r.Logbook, len(e.Logbooks))
		case r.ModifiedAt.IsZero():
			return bad("row %s has no modified_at", r.UUID)
		case len(r.Qso) == 0:
			return bad("row %s has no qso payload", r.UUID)
		}
		seen[strings.ToLower(r.UUID)] = true
		var top map[string]json.RawMessage
		if json.Unmarshal(r.Qso, &top) != nil || top == nil {
			return bad("row %s: its qso payload is not a JSON object", r.UUID)
		}
		if raw, ok := top["uuid"]; ok {
			var named string
			if json.Unmarshal(raw, &named) != nil || !strings.EqualFold(strings.TrimSpace(named), r.UUID) {
				return bad("row %s: its qso.uuid names another QSO", r.UUID)
			}
		}
		rows[r.Logbook]++
		if r.DeletedAt != nil {
			tombstones[r.Logbook]++
		}
	}
	for i, lb := range e.Logbooks {
		switch {
		case lb.Count != rows[i]:
			return bad("logbook %s declares count %d; the archive has %d rows in it", *lb.UUID, lb.Count, rows[i])
		case lb.Tombstones != tombstones[i]:
			return bad("logbook %s declares tombstones %d; the archive has %d", *lb.UUID, lb.Tombstones, tombstones[i])
		}
	}
	return rows, tombstones, nil
}

// quoted returns " (uuid)" for a string shaped like a UUID, else nothing: an
// identity may be named, an arbitrary value never.
func quoted(s string) string {
	if uuidPattern.MatchString(s) {
		return " (" + s + ")"
	}
	return ""
}

// defaultLogbook resolves the default: the named member, or the only logbook.
func defaultLogbook(e *smcloud.ArchiveExport, named string) (int, error) {
	if named == "" {
		if len(e.Logbooks) == 1 {
			return 0, nil
		}
		return 0, refuse("default_logbook_required", "", "the archive has %d logbooks; name the default one", len(e.Logbooks))
	}
	for i, lb := range e.Logbooks {
		if strings.EqualFold(*lb.UUID, named) {
			return i, nil
		}
	}
	return 0, refuse("default_logbook_not_member", "", "logbook%s is not a member of the archive", quoted(named))
}

// Provision re-creates cloud archive U as a managed file (ADR 0071 refinement,
// PA4). The preflight refuses before the temporary file exists. Construction
// refusals (storage, the round trip) come before placement and remove the
// temporary file. The file is placed with a link that never replaces an
// existing name. For a new entry, publication is the catalogue write: a
// failure before it removes the placed file. For an existing managed entry,
// publication is the placement itself: after it nothing is undone, and a
// temporary name left behind or an unconfirmed directory sync is reported.
// A cleanup that cannot remove a file names it after the cause.
func (m *Manager) Provision(ctx context.Context, req ProvisionRequest) (ProvisionResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.restorer == nil {
		return ProvisionResult{}, refuse("restorer_unavailable", "", "no restore adapter is wired; this process cannot provision")
	}
	snap := m.cfg.Snapshot()
	plan, err := m.preflight(snap, req)
	if err != nil {
		return ProvisionResult{}, err
	}
	e := req.Export
	res := ProvisionResult{Plan: plan, Added: plan.Entry == nil}
	if plan.Entry != nil {
		res.Entry = *plan.Entry
	} else {
		res.Entry = types.QsoArchiveConfig{ID: plan.ArchiveUUID, Label: e.Label, Ownership: types.QsoArchiveOwnershipManaged}
	}

	dir := ManagedDir(snap)
	if err := ensureContainedManagedDir(snap.DataDir, dir); err != nil {
		return ProvisionResult{}, err
	}
	final := plan.Path
	tmp := final + creatingSuffix
	if err := m.precreateArchiveFile(tmp); err != nil {
		return ProvisionResult{}, err
	}
	localIDs := make([]int64, len(e.Logbooks))
	logbooks, err := m.build(ctx, snap, tmp, plan.ArchiveUUID, buildSpec{
		logbooks: func(ctx context.Context, svc *sqlite.Service) (int64, error) {
			var def int64
			for i, lb := range plan.Logbooks {
				id, err := svc.InsertProvisionedLogbookWithContext(ctx, types.Logbook{UUID: lb.UUID, Name: lb.Name, Callsign: lb.Callsign})
				if err != nil {
					return 0, fmt.Errorf("logbook %s: %w", lb.UUID, err)
				}
				localIDs[i] = id
				if lb.Default {
					def = id
				}
			}
			return def, nil
		},
		rows: func(ctx context.Context, svc *sqlite.Service) error {
			if err := m.restoreRows(ctx, svc, e, localIDs); err != nil {
				return err
			}
			return checkRoundTrip(ctx, svc, e, localIDs)
		},
	})
	if err != nil {
		return ProvisionResult{}, m.withCleanup(err, tmp)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return ProvisionResult{}, m.withCleanup(fmt.Errorf("set archive file mode: %w", err), tmp)
	}
	// No-replace placement: a destination that appeared since the preflight
	// is never overwritten.
	if err := os.Link(tmp, final); err != nil {
		if stderr.Is(err, fs.ErrExist) {
			return ProvisionResult{}, m.withCleanup(refuse("destination_appeared", final, "%s appeared after the preflight; it was not replaced", final), tmp)
		}
		return ProvisionResult{}, m.withCleanup(fmt.Errorf("place archive file: %w", err), tmp)
	}
	if res.Added {
		if err := m.publishNewEntry(&res, snap, final, tmp); err != nil {
			return ProvisionResult{}, err
		}
	} else {
		m.finishPublishedPlacement(&res, snap, final, tmp)
	}
	// Derived state, as creation records it: a failure costs the summary,
	// never the archive.
	if err := RecordClosedArchive(SummariesPath(snap), plan.ArchiveUUID, final, logbooks, time.Now()); err != nil {
		m.logger.WarnWith().Err(err).Str("archive_id", plan.ArchiveUUID).Msg("archive: could not record the provisioned archive's summary")
	}
	ev := m.logger.InfoWith().Str("archive_id", plan.ArchiveUUID).Str("path", final).Bool("entry_added", res.Added).Int("qsos", len(e.Qsos))
	if res.DurabilityUncertain {
		ev = ev.Bool("durability_uncertain", true)
	}
	if res.LeftoverTemp != "" {
		ev = ev.Str("leftover", res.LeftoverTemp)
	}
	ev.Msg("archive: provisioned from SM Cloud")
	return res, nil
}

// publishNewEntry finishes a new entry's placement and publishes it with the
// catalogue write. Before that write every failure removes this invocation's
// files.
func (m *Manager) publishNewEntry(res *ProvisionResult, snap config.Config, final, tmp string) error {
	if err := removeFile(tmp); err != nil {
		return m.withCleanup(fmt.Errorf("remove the temporary name: %w", err), final, tmp)
	}
	dir := ManagedDir(snap)
	for _, d := range dirsUpTo(dir, snap.DataDir) {
		if err := syncDir(d); err != nil {
			return m.withCleanup(fmt.Errorf("sync %s: %w", d, err), final)
		}
	}
	dur, err := m.cfg.Update(func(c *config.Config) error {
		c.QsoArchives = append(c.QsoArchives, res.Entry)
		return nil
	})
	if err != nil {
		return m.withCleanup(fmt.Errorf("record the archive in the catalogue: %w", err), final)
	}
	res.DurabilityUncertain = dur == config.DurabilityUncertain
	return nil
}

// finishPublishedPlacement completes an existing entry's placement, already
// published by the link: nothing is undone, and what could not be finished is
// reported.
func (m *Manager) finishPublishedPlacement(res *ProvisionResult, snap config.Config, final, tmp string) {
	if err := removeFile(tmp); err != nil {
		res.LeftoverTemp = tmp
		m.logger.WarnWith().Err(err).Str("path", tmp).Str("archive", final).
			Msg("archive: the provisioned archive is in place; its temporary name could not be removed and is reported at the next start")
	}
	for _, d := range dirsUpTo(ManagedDir(snap), snap.DataDir) {
		if err := syncDir(d); err != nil {
			res.DurabilityUncertain = true
			m.logger.WarnWith().Err(err).Str("path", d).Msg("archive: the provisioned archive is in place; its directory sync failed, so its durability is uncertain")
			return
		}
	}
}

// restoreError is a row the restore adapter refused. It names the row and the
// kind of refusal, and deliberately keeps no cause: the adapter's errors can
// quote the row's values (a malformed frequency, say), and a logger walking
// the chain would print them.
type restoreError struct {
	uuid   string
	unique bool
}

func (e *restoreError) Error() string {
	if e.unique {
		return "row " + e.uuid + ": the restore adapter refused it under a local uniqueness constraint"
	}
	return "row " + e.uuid + ": the restore adapter refused it"
}

// restoreRows writes every row through the restore adapter into its logbook,
// with the cloud's UUID, revision, modified_at and tombstone.
func (m *Manager) restoreRows(ctx context.Context, svc *sqlite.Service, e *smcloud.ArchiveExport, localIDs []int64) error {
	for _, r := range e.Qsos {
		var q types.Qso
		if err := json.Unmarshal(r.Qso, &q); err != nil {
			return fmt.Errorf("row %s: its payload does not decode as a QSO", r.UUID)
		}
		q.UUID, q.ModifiedAt, q.Revision, q.DeletedAt = r.UUID, r.ModifiedAt, r.Revision, time.Time{}
		if r.DeletedAt != nil {
			q.DeletedAt = *r.DeletedAt
		}
		if err := m.restorer(ctx, svc, localIDs[r.Logbook], q); err != nil {
			return &restoreError{uuid: r.UUID, unique: sqlite.IsUniqueConstraintError(err)}
		}
	}
	return nil
}

// checkRoundTrip reads every row back as the forwarding worker reads it and
// builds the record the forwarder would send (smcloud.UploadFor). The record
// must equal the cloud's: identity, membership, revision, modified_at,
// tombstone, and the payload under jsonb equality.
func checkRoundTrip(ctx context.Context, svc *sqlite.Service, e *smcloud.ArchiveExport, localIDs []int64) error {
	logbookOf := map[int64]string{}
	for i, id := range localIDs {
		logbookOf[id] = strings.ToLower(*e.Logbooks[i].UUID)
	}
	var bad []RoundTripMismatch
	for _, r := range e.Qsos {
		fields, err := roundTripFields(ctx, svc, r, strings.ToLower(*e.Logbooks[r.Logbook].UUID), logbookOf)
		if err != nil {
			return err
		}
		if len(fields) > 0 {
			bad = append(bad, RoundTripMismatch{UUID: r.UUID, Fields: fields})
		}
	}
	if len(bad) > 0 {
		return &RoundTripError{Rows: bad}
	}
	return nil
}

func roundTripFields(ctx context.Context, svc *sqlite.Service, r smcloud.ArchiveExportRecord, logbook string, logbookOf map[int64]string) ([]string, error) {
	stored, err := svc.FetchQsoByUUIDIncludingDeletedWithContext(ctx, r.UUID)
	if stderr.Is(err, errors.ErrNotFound) {
		return []string{"uuid"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("row %s: read it back: %w", r.UUID, err)
	}
	// The worker's read for the row's state (internal/forwarding/worker): a
	// live row as an insert, a tombstone as a delete.
	act := forwarding.Action(action.Insert)
	var read types.Qso
	if stored.DeletedAt.IsZero() {
		read, err = svc.FetchQsoByIdWithContext(ctx, stored.ID)
	} else {
		act = action.Delete
		read, err = svc.FetchQsoByIDIncludingDeletedWithContext(ctx, stored.ID)
	}
	if err != nil {
		return nil, fmt.Errorf("row %s: read it as the worker does: %w", r.UUID, err)
	}
	up, err := smcloud.UploadFor(read, act)
	if err != nil {
		return nil, fmt.Errorf("row %s: build its upload record: %w", r.UUID, err)
	}
	var fields []string
	if !strings.EqualFold(read.UUID, r.UUID) {
		fields = append(fields, "uuid")
	}
	if logbookOf[read.LogbookID] != logbook {
		fields = append(fields, "logbook")
	}
	if up.Revision != r.Revision {
		fields = append(fields, "revision")
	}
	if !up.ModifiedAt.Equal(r.ModifiedAt) {
		fields = append(fields, "modified_at")
	}
	if (up.DeletedAt == nil) != (r.DeletedAt == nil) || (up.DeletedAt != nil && !up.DeletedAt.Equal(*r.DeletedAt)) {
		fields = append(fields, "deleted_at")
	}
	payload, err := payloadFields(up.Qso, r.Qso)
	if err != nil {
		return nil, fmt.Errorf("row %s: %w", r.UUID, err)
	}
	return append(fields, payload...), nil
}

// payloadFields names the top-level payload keys whose values differ under
// jsonb equality, or that one side lacks, in key order.
func payloadFields(local, cloud json.RawMessage) ([]string, error) {
	var a, b map[string]json.RawMessage
	if json.Unmarshal(local, &a) != nil || json.Unmarshal(cloud, &b) != nil {
		return nil, stderr.New("a payload is not a JSON object")
	}
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	var out []string
	for k := range keys {
		va, oka := a[k]
		vb, okb := b[k]
		if !oka || !okb {
			out = append(out, "qso."+k)
			continue
		}
		eq, err := jsonbEqual(va, vb)
		if err != nil {
			return nil, fmt.Errorf("qso.%s cannot be compared: %w", k, err)
		}
		if !eq {
			out = append(out, "qso."+k)
		}
	}
	sort.Strings(out)
	return out, nil
}
