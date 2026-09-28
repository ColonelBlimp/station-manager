package archive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Archive summaries (ADR 0084): what each archive holds — its logbooks, their
// callsigns and QSO counts — kept so Settings → Archives can list an archive's
// contents without opening a closed file. They live in one station-global
// sidecar, never beside an archive (an external archive's directory belongs to
// the operator), and the file is DISCARDABLE derived state: losing or rejecting
// it costs a summary, never a start or a QSO. So reading never fails — an
// unreadable file is a cache miss with a reason for the log.

// SummariesFileName is the sidecar's name in the station-global directory.
const SummariesFileName = "qso-archive-summaries.json"

// summariesFormat is the sidecar's own format version, independent of config's.
// A file with any other value is a cache miss, not a migration.
const summariesFormat = 1

// Cache-miss reasons ReadSummaries reports, for the log. The empty string is a hit.
const (
	MissMissing     = "missing"
	MissCorrupt     = "corrupt"
	MissUnsupported = "unsupported format"
)

// SummariesPath is <data_dir>/db/qso-archive-summaries.json.
func SummariesPath(cfg config.Config) string {
	return filepath.Join(GlobalDir(cfg), SummariesFileName)
}

// LogbookSummary is one logbook as the archive held it.
type LogbookSummary struct {
	UUID     string `json:"uuid"`
	Name     string `json:"name"`
	Callsign string `json:"callsign"`
	QSOCount int64  `json:"qso_count"`
}

// FileSignature identifies the archive file a summary was taken from. The
// comparison is device, inode and ctime: an ordinary write and any metadata
// change — restoring an mtime included — move ctime, and inode numbers are
// unique only within a filesystem, hence the device. Size and mtime are kept for
// diagnostics only: under WAL a QSO write leaves the main file's mtime unchanged
// until checkpoint. It is a change signature, not an integrity proof (ADR 0084).
type FileSignature struct {
	Dev     uint64 `json:"dev"`
	Ino     uint64 `json:"ino"`
	CtimeNs int64  `json:"ctime_ns"`
	Size    int64  `json:"size"`
	MtimeNs int64  `json:"mtime_ns"`
}

// ArchiveSummary is one archive's contents and the file they were read from.
type ArchiveSummary struct {
	Logbooks  []LogbookSummary `json:"logbooks"`
	Signature FileSignature    `json:"signature"`
	TakenAt   time.Time        `json:"taken_at"`
}

// Current reports whether the archive file still matches the one this summary
// was taken from. False means the counts may be out of date.
func (a ArchiveSummary) Current(now FileSignature) bool {
	s := a.Signature
	return s.Dev == now.Dev && s.Ino == now.Ino && s.CtimeNs == now.CtimeNs
}

// Summaries maps an archive's UUID to its summary.
type Summaries map[string]ArchiveSummary

type summariesFile struct {
	Format   int       `json:"format"`
	Archives Summaries `json:"archives"`
}

// ReadSummaries reads the sidecar. It never fails: a missing, corrupt or
// unsupported-format file yields an empty, usable set and the reason.
func ReadSummaries(path string) (Summaries, string) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Summaries{}, MissMissing
	}
	if err != nil {
		return Summaries{}, MissCorrupt
	}
	var f summariesFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return Summaries{}, MissCorrupt
	}
	if f.Format != summariesFormat {
		return Summaries{}, MissUnsupported
	}
	if f.Archives == nil {
		f.Archives = Summaries{}
	}
	return f.Archives, ""
}

// WriteSummaries replaces the sidecar atomically with owner-only permissions: a
// temporary file in the same directory, written and synced, renamed over the
// old one, then the directory synced — so a crash leaves the old file or the new
// one, never a torn one.
func WriteSummaries(path string, s Summaries) error {
	if s == nil {
		s = Summaries{}
	}
	raw, err := json.MarshalIndent(summariesFile{Format: summariesFormat, Archives: s}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode archive summaries: %w", err)
	}
	dir := filepath.Dir(path)
	// CreateTemp creates the file 0600 — owner-only from the first byte.
	tmp, err := os.CreateTemp(dir, SummariesFileName+".*.tmp")
	if err != nil {
		return fmt.Errorf("create archive summaries temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func(cause error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return cause
	}
	if _, err := tmp.Write(raw); err != nil {
		return cleanup(fmt.Errorf("write archive summaries: %w", err))
	}
	if err := tmp.Sync(); err != nil {
		return cleanup(fmt.Errorf("sync archive summaries: %w", err))
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close archive summaries: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("place archive summaries: %w", err)
	}
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("sync %s: %w", dir, err)
	}
	return nil
}

// SummarySource is what a summary is built from: the open archive's logbooks
// and their live QSO counts (the sqlite service satisfies it).
type SummarySource interface {
	FetchAllLogbooksWithContext(ctx context.Context) ([]types.Logbook, error)
	FetchQsoCountByLogbookIdWithContext(ctx context.Context, id int64, missingFromPrefix string, notEmailed bool) (int64, error)
}

// BuildLogbookSummaries reads each logbook and its QSO count from an open
// archive, in the store's logbook order. An archive with no logbooks is an
// empty, non-nil list.
func BuildLogbookSummaries(ctx context.Context, src SummarySource) ([]LogbookSummary, error) {
	lbs, err := src.FetchAllLogbooksWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("read logbooks: %w", err)
	}
	out := make([]LogbookSummary, 0, len(lbs))
	for _, lb := range lbs {
		n, err := src.FetchQsoCountByLogbookIdWithContext(ctx, lb.ID, "", false)
		if err != nil {
			return nil, fmt.Errorf("count QSOs in logbook %d: %w", lb.ID, err)
		}
		out = append(out, LogbookSummary{UUID: lb.UUID, Name: lb.Name, Callsign: lb.Callsign, QSOCount: n})
	}
	return out, nil
}

// mergeMu serialises the sidecar's read-modify-write: two archive operations
// merging at once (a close and a creation, say) must not lose an entry.
var mergeMu sync.Mutex

// MergeSummary records one archive's summary in the sidecar, keeping every
// other archive's. An unreadable sidecar is replaced (derived state).
func MergeSummary(path, archiveID string, s ArchiveSummary) error {
	mergeMu.Lock()
	defer mergeMu.Unlock()
	all, _ := ReadSummaries(path)
	all[archiveID] = s
	return WriteSummaries(path, all)
}

// RecordClosedArchive merges the summary of an archive whose file is closed and
// final: the signature is taken now, so it must be called after the checkpoint
// and close, or the next open reads the summary as stale.
func RecordClosedArchive(sidecar, archiveID, dbPath string, logbooks []LogbookSummary, at time.Time) error {
	sig, err := SignatureOf(dbPath)
	if err != nil {
		return fmt.Errorf("archive file signature: %w", err)
	}
	return MergeSummary(sidecar, archiveID, ArchiveSummary{Logbooks: logbooks, Signature: sig, TakenAt: at.UTC()})
}
