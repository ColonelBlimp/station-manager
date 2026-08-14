package evidence

import (
	"database/sql"
	"os"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// RelocateArchive returns the path the evidence archive should open, first moving
// an archive that still sits at the legacy location (oldPath) into newPath. The
// daemon historically kept evidence.db in the working-directory root
// (world-readable, mode 0644, in a 0755 dir); it now belongs alongside the log
// databases under db/ (owner-only, mode 0700), so it is not readable by other
// local users. This runs at Start, before the archive is opened, and the daemon
// is not otherwise running.
//
// Crash-safe by construction: the WAL is FOLDED into the main file first
// (wal_checkpoint(TRUNCATE) with no competing connection), so the archive is a
// SINGLE file to move — one os.Rename is atomic, and no crash can strand a WAL
// still holding committed, uncheckpointed records (codex 81bf9abe P1). If the WAL
// cannot be fully folded, or the move fails, the archive is left at the legacy
// path (never orphaned or split). No-op when already migrated or on a fresh install.
func RelocateArchive(oldPath, newPath string, log logging.Logger) string {
	if log == nil {
		log = logging.Noop()
	}
	if _, err := os.Stat(newPath); err == nil {
		return newPath // already in place
	}
	if _, err := os.Stat(oldPath); err != nil {
		return newPath // nothing at the legacy location (fresh install)
	}

	// Fold the WAL into the main file so there is a single file to move. If it
	// cannot be fully folded, do NOT split-move — a single-file move would then
	// drop the unfolded records; keep the whole set at the legacy path.
	if !foldWAL(oldPath, log) {
		return oldPath
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		log.ErrorWith().Err(err).Str("from", oldPath).Str("to", newPath).
			Msg("evidence: could not relocate archive into db/; keeping the existing location")
		return oldPath
	}
	// After the fold + clean close the WAL/SHM siblings are empty or already gone;
	// remove any that linger so the legacy location is left clean (best-effort — a
	// missed one is a harmless empty file, and the moved main holds every record).
	_ = os.Remove(oldPath + "-wal")
	_ = os.Remove(oldPath + "-shm")
	// Owner-only, belt-and-suspenders over the 0700 db/ directory that already gates
	// access (the WAL/SHM siblings the daemon recreates inherit the umask).
	_ = os.Chmod(newPath, 0o600)
	log.InfoWith().Str("from", oldPath).Str("to", newPath).
		Msg("evidence: archive relocated into db/")
	return newPath
}

// foldWAL checkpoints the archive's WAL into its main file and truncates it, so the
// main file alone holds every committed record. Returns false (leaving the archive
// untouched) if the fold could not fully complete — the caller then keeps the
// matched set where it is rather than moving a main file without its WAL.
func foldWAL(path string, log logging.Logger) bool {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(2000)")
	if err != nil {
		log.WarnWith().Err(err).Str("path", path).
			Msg("evidence: could not open archive to fold its WAL before relocation")
		return false
	}
	defer func() { _ = db.Close() }()
	var busy, logFrames, checkpointed int64
	if err := db.QueryRow(`PRAGMA wal_checkpoint(TRUNCATE)`).
		Scan(&busy, &logFrames, &checkpointed); err != nil {
		log.WarnWith().Err(err).Str("path", path).
			Msg("evidence: could not fold the archive's WAL before relocation")
		return false
	}
	if busy != 0 {
		// A frame could not be folded (should not happen with no other connection);
		// the WAL still holds records, so a single-file move would lose them.
		log.WarnWith().Int64("wal_frames", logFrames).Str("path", path).
			Msg("evidence: archive WAL not fully folded; leaving it at the legacy location")
		return false
	}
	return true
}
