package archive

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/fsperm"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

const (
	creatingSuffix = ".creating"
	maxLabelLen    = 64
	maxLogbookName = 64
)

// RequestError is a refused request: the caller's input, not the station's
// state. The HTTP layer maps Code to 400.
type RequestError struct {
	Code    string
	Message string
}

func (e *RequestError) Error() string { return e.Code + ": " + e.Message }

// CreateRequest names a new managed archive by its semantics only — never a
// path (ADR 0071 path confinement). RequestKey makes creation idempotent: a
// retried request with the same key returns the archive the first one made.
type CreateRequest struct {
	RequestKey      string
	Label           string
	LogbookName     string
	LogbookCallsign string
}

// CreateResult is the provisioned archive: its catalogue entry (inactive) and
// its file. The initial logbook is read from the file like any other (its id
// is the file's default), so a durable retry returns exactly this.
type CreateResult struct {
	Entry  types.QsoArchiveConfig
	Path   string
	Reused bool
}

// Manager is the single-flight archive manager (ADR 0071): it provisions
// managed archives and is the catalogue's only writer besides adoption.
type Manager struct {
	cfg           *config.Service
	logger        *logging.Service
	validCallsign func(string) bool

	mu sync.Mutex
}

// NewManager wires the manager. validCallsign is the station's canonical
// callsign rule (the QSO service owns it; injected so this package does not
// import the service that imports it).
func NewManager(cfg *config.Service, logger *logging.Service, validCallsign func(string) bool) *Manager {
	return &Manager{cfg: cfg, logger: logger, validCallsign: validCallsign}
}

// Create provisions a managed archive, in ADR 0071's order:
//
//  1. mint the archive and initial-logbook identities;
//  2. build the file as <managed>/<id>.db.creating with the log migration set;
//  3. seed the logbook and default, write the identity, check integrity,
//     close to a single file;
//  4. set ST-6 modes and rename into place;
//  5. add the INACTIVE catalogue entry.
//
// A failure before 5 removes the file: the active archive is untouched and no
// half-built archive is selectable. A failure AT 5 removes the placed file for
// the same reason. Single-flight: one creation at a time.
func (m *Manager) Create(ctx context.Context, req CreateRequest) (CreateResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Idempotency is DURABLE: the key is stored on the catalogue entry, so a
	// retry through a restarted daemon (or after a crash past the catalogue
	// commit) finds the archive it already made.
	if req.RequestKey != "" {
		snap := m.cfg.Snapshot()
		for i := range snap.QsoArchives {
			if e := snap.QsoArchives[i]; e.RequestKey == req.RequestKey {
				return CreateResult{Entry: e, Path: PathFor(snap, e), Reused: true}, nil
			}
		}
	}
	req.Label = strings.TrimSpace(req.Label)
	req.LogbookName = strings.TrimSpace(req.LogbookName)
	req.LogbookCallsign = strings.ToUpper(strings.TrimSpace(req.LogbookCallsign))
	if err := m.validate(req); err != nil {
		return CreateResult{}, err
	}

	snap := m.cfg.Snapshot()
	id := utils.NewUUIDv7()
	dir := ManagedDir(snap)
	// ST-6 from the first byte, by the symlink-aware rule (internal/fsperm): the
	// managed directory must be a real directory contained in the working
	// directory — a symlink, or a parent that resolves outside, is refused before
	// anything is created or chmod'd through it. The directory is then tightened
	// even when it already existed (MkdirAll leaves a 0755 directory alone), and
	// the file is created 0600 here so SQLite opens an existing owner-only file
	// instead of creating one under the umask.
	if err := ensureContainedManagedDir(snap.DataDir, dir); err != nil {
		return CreateResult{}, err
	}
	final := filepath.Join(dir, id+".db")
	tmp := final + creatingSuffix
	if err := m.precreateArchiveFile(tmp); err != nil {
		return CreateResult{}, err
	}

	if err := m.build(ctx, snap, tmp, id, req); err != nil {
		return CreateResult{}, m.withCleanup(err, tmp)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return CreateResult{}, m.withCleanup(fmt.Errorf("set archive file mode: %w", err), tmp)
	}
	if err := os.Rename(tmp, final); err != nil {
		return CreateResult{}, m.withCleanup(fmt.Errorf("place archive file: %w", err), tmp)
	}

	entry := types.QsoArchiveConfig{ID: id, Label: req.Label, Ownership: types.QsoArchiveOwnershipManaged, RequestKey: req.RequestKey}
	// Update, not UpdateInMemoryThenPersist: the file is the source of truth and
	// a failed write must leave memory untouched — no selectable half-built archive.
	dur, err := m.cfg.Update(func(c *config.Config) error {
		c.QsoArchives = append(c.QsoArchives, entry)
		return nil
	})
	if err != nil {
		return CreateResult{}, m.withCleanup(fmt.Errorf("record the archive in the catalogue: %w", err), final)
	}
	res := CreateResult{Entry: entry, Path: final}
	ev := m.logger.InfoWith().Str("archive_id", id).Str("label", req.Label).Str("path", final)
	if dur == config.DurabilityUncertain {
		ev = ev.Bool("durability_uncertain", true)
	}
	ev.Msg("archive: managed archive created (inactive until activated)")
	return res, nil
}

func (m *Manager) validate(req CreateRequest) error {
	switch {
	case req.RequestKey == "":
		return &RequestError{Code: "missing_required_field", Message: "request_key is required"}
	case req.Label == "":
		return &RequestError{Code: "missing_required_field", Message: "label is required"}
	case len(req.Label) > maxLabelLen:
		return &RequestError{Code: "invalid_field_value", Message: fmt.Sprintf("label must be at most %d characters", maxLabelLen)}
	case req.LogbookName == "":
		return &RequestError{Code: "missing_required_field", Message: "logbook name is required"}
	case len(req.LogbookName) > maxLogbookName:
		return &RequestError{Code: "invalid_field_value", Message: fmt.Sprintf("logbook name must be at most %d characters", maxLogbookName)}
	case req.LogbookCallsign == "":
		return &RequestError{Code: "missing_required_field", Message: "logbook callsign is required"}
	case !m.validCallsign(req.LogbookCallsign):
		return &RequestError{Code: "invalid_field_value", Message: "logbook callsign must be 3-32 characters and contain at least one digit"}
	}
	return nil
}

// closeFile is the pre-create's close, a seam so a test can fail it.
var closeFile = func(f *os.File) error { return f.Close() }

// precreateArchiveFile creates the .creating file 0600 and empty, so SQLite
// opens an owner-only file instead of creating one under the umask. A close
// failure removes the file through withCleanup — a removal failure is never
// discarded.
func (m *Manager) precreateArchiveFile(tmp string) error {
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create archive file: %w", err)
	}
	if err := closeFile(f); err != nil {
		return m.withCleanup(fmt.Errorf("create archive file: %w", err), tmp)
	}
	return nil
}

// ensureContainedManagedDir proves the managed directory is — or will be — a
// real, euid-owned directory inside workDir after resolving symlinks, and only
// then creates and tightens it to 0700. The check runs on the deepest EXISTING
// ancestor first, so a parent that is a symlink to outside (<data_dir>/db →
// elsewhere) is refused before MkdirAll could create anything out there. fsperm
// never chmods through a symlink; this refuses rather than warns, because the
// daemon is about to WRITE here.
func ensureContainedManagedDir(workDir, dir string) error {
	for p := dir; ; p = filepath.Dir(p) {
		lst, err := os.Lstat(p)
		if os.IsNotExist(err) {
			if p == filepath.Dir(p) {
				break
			}
			continue // not created yet; check its parent
		}
		if err != nil {
			return fmt.Errorf("inspect %s: %w", p, err)
		}
		if lst.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s on the managed archive path is a symlink; refusing to create archives through it", p)
		}
		if p != workDir {
			contained, cErr := fsperm.Contained(workDir, p)
			if cErr != nil {
				return fmt.Errorf("resolve %s: %w", p, cErr)
			}
			if !contained {
				return fmt.Errorf("%s on the managed archive path resolves outside the working directory (a symlink on its path); refusing", p)
			}
		}
		break // the deepest existing ancestor is a real, contained directory
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create managed archive directory: %w", err)
	}
	contained, err := fsperm.Contained(workDir, dir)
	if err != nil {
		return fmt.Errorf("resolve managed archive directory: %w", err)
	}
	if !contained {
		return fmt.Errorf("managed archive directory %s resolves outside the working directory (a symlink on its path); refusing", dir)
	}
	if _, err := fsperm.SecureApplicationPath(workDir, dir, 0o700); err != nil {
		return fmt.Errorf("secure managed archive directory: %w", err)
	}
	if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o700 {
		return fmt.Errorf("managed archive directory %s is not owner-only (%v); refusing", dir, err)
	}
	return nil
}

// build migrates the .creating file with the LOG set only, seeds the initial
// logbook and the identity, checks integrity, and closes to a single file.
func (m *Manager) build(ctx context.Context, snap config.Config, tmp, id string, req CreateRequest) error {
	c := snap
	c.Datastore.Path = tmp
	c.Datastore.Options = nil
	svc := &sqlite.Service{ConfigService: config.New(c), LoggerService: m.logger}
	if err := svc.Initialize(); err != nil {
		return fmt.Errorf("provision: %w", err)
	}
	svc.SetMigrationSets(sqlite.MigrationSetLog)
	if err := svc.Open(); err != nil {
		return fmt.Errorf("provision: open: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = svc.Close()
		}
	}()
	if err := svc.Migrate(); err != nil {
		return fmt.Errorf("provision: migrate: %w", err)
	}
	lbID, err := svc.InsertLogbookWithContext(ctx, types.Logbook{Name: req.LogbookName, Callsign: req.LogbookCallsign})
	if err != nil {
		return fmt.Errorf("provision: seed logbook: %w", err)
	}
	if _, err := svc.WriteArchiveIdentityWithContext(ctx, id, lbID); err != nil {
		return fmt.Errorf("provision: write identity: %w", err)
	}
	if err := svc.CheckIntegrityWithContext(ctx); err != nil {
		return fmt.Errorf("provision: %w", err)
	}
	closed = true
	if err := svc.Close(); err != nil {
		return fmt.Errorf("provision: close: %w", err)
	}
	for _, side := range []string{tmp + "-wal", tmp + "-shm"} {
		if _, statErr := os.Stat(side); statErr == nil {
			return fmt.Errorf("provision: %s survived the close; the file is not self-contained", filepath.Base(side))
		}
	}
	return nil
}

// withCleanup removes a failed creation's file (and sidecars) and folds a
// removal failure into the returned error — a file that cannot be removed is
// logged and named, never silently left, and DiagnoseCreatingArtefacts reports
// it at the next start as an unlisted archive file.
func (m *Manager) withCleanup(cause error, path string) error {
	if err := m.removeWithSidecars(path); err != nil {
		return fmt.Errorf("%w; and the file could not be removed (%v) — it is unlisted and will be reported at the next start", cause, err)
	}
	return cause
}

func (m *Manager) removeWithSidecars(path string) error {
	var first error
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			m.logger.ErrorWith().Err(err).Str("path", p).Msg("archive: could not remove a failed creation's file")
			if first == nil {
				first = err
			}
		}
	}
	return first
}

// DiagnoseCreatingArtefacts lists what an interrupted or failed creation left
// in the managed directory: `.creating` files, and `.db` files the catalogue
// does not list (a crash between placing the file and the catalogue write, or
// a cleanup that could not remove it). They are never archives — the catalogue
// never learned of them — so they are reported for the operator to remove,
// not deleted: a file that was mid-build may still be the operator's clue to
// what happened. Runs at daemon start.
func DiagnoseCreatingArtefacts(cfg config.Config, logger *logging.Service) []string {
	dir := ManagedDir(cfg)
	var found []string
	creating, _ := filepath.Glob(filepath.Join(dir, "*"+creatingSuffix))
	for _, p := range creating {
		logger.WarnWith().Str("path", p).
			Msg("archive: a .creating file was left by an interrupted archive creation; it is not an archive and can be removed")
		found = append(found, p)
	}
	listed := make(map[string]struct{}, len(cfg.QsoArchives))
	for _, e := range cfg.QsoArchives {
		if e.Ownership == types.QsoArchiveOwnershipManaged {
			listed[PathFor(cfg, e)] = struct{}{}
		}
	}
	dbs, _ := filepath.Glob(filepath.Join(dir, "*.db"))
	for _, p := range dbs {
		if _, ok := listed[p]; ok {
			continue
		}
		logger.WarnWith().Str("path", p).
			Msg("archive: an archive file in the managed directory is not in the catalogue (a failed creation left it); it is not served and can be removed")
		found = append(found, p)
	}
	return found
}
