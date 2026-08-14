package pskreporter

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// afterReadHook, when non-nil, runs after a genuinely-MISSING read and before the
// atomic create. Tests set it to drive every concurrent caller into the create
// window together so the convergence guarantee is actually exercised (without it
// the OS schedules callers serially enough that even a non-atomic writer
// converges). Nil and zero-overhead in production.
var afterReadHook func()

// loadOrCreateIdentifier returns the IPFIX observation-domain identifier — the
// datagram header's per-sender "random identifier". PSK Reporter's protocol
// (pskdev.html) says it "should be constant for any particular sender", so we
// persist it across restarts rather than re-minting it each boot: a station that
// re-randomises on every process looks to the collector like a stream of distinct
// senders, which is exactly the churn a CGNAT'd reporter (a fresh source port per
// report, outside the client's control) already maximises.
//
// path == "" (the ft8-psk-probe CLI, unit tests) keeps a fresh in-memory random
// id. Otherwise the discriminator is whether the file EXISTS, not whether the read
// succeeded — a corrupt OR an unreadable-but-writable file still exists and is
// healed in place; only a genuinely-missing one is created:
//
//   - VALID persisted id → returned verbatim (no re-mint).
//   - EXISTS but unusable (corrupt, or read fails for any reason other than
//     "not found") → healed by overwriting IN PLACE. That needs only file-write
//     permission, so it repairs even in a read-only data dir (codex 85b55262 /
//     35f7b774 P2). Not atomic across daemons — two healing at once is
//     last-writer-wins, a doubly-exceptional broken-file+concurrent edge with no
//     clean winner-selection primitive (Link can't replace, Rename has no EEXIST).
//   - MISSING → published ATOMICALLY so two daemons started against one working dir
//     converge on a single id instead of each reporting its own (codex 63c94cfa P2
//     — a second smd is NOT prevented: for a unix socket ListenAndServe removes the
//     existing socket and rebinds). Write the id to a temp file and hard-link it
//     into place: os.Link fails EEXIST if the path exists, so exactly one caller
//     wins AND the file appears with full content in one step (bare
//     O_CREATE|O_EXCL would leave an empty-file window a losing caller reads and
//     then heals with its own id).
//
// Best-effort throughout — reporting is best-effort and a state-file problem must
// never block Start: any create/write failure falls back to the in-memory id + a
// Warn. gen is injected (rand.Uint32 in production) so tests are deterministic.
func loadOrCreateIdentifier(path string, gen func() uint32, log logging.Logger) uint32 {
	if log == nil {
		log = logging.Noop()
	}
	if strings.TrimSpace(path) == "" {
		return gen()
	}

	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if v, perr := parseID(raw); perr == nil {
			return v // valid
		}
		return healInPlace(path, gen(), log) // exists but corrupt
	case !os.IsNotExist(err):
		return healInPlace(path, gen(), log) // exists but unreadable (write-only, EACCES, …)
	}

	// Genuinely missing → publish atomically via temp + Link.
	if afterReadHook != nil {
		afterReadHook()
	}
	id := gen()
	tmp, terr := os.CreateTemp(filepath.Dir(path), ".pskid-*")
	if terr != nil {
		warnUnpersisted(log, path, terr) // can't even stage → fail open
		return id
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, werr := tmp.Write(idBytes(id)); werr != nil {
		_ = tmp.Close()
		warnUnpersisted(log, path, werr)
		return id
	}
	if cerr := tmp.Close(); cerr != nil {
		warnUnpersisted(log, path, cerr)
		return id
	}
	if os.Link(tmpName, path) == nil {
		return id // we won the link; path now atomically holds our id
	}
	// Link failed one of two ways:
	//   - EEXIST: a racer won — read and return the winner's fully-written value
	//     below. This is the convergence path.
	//   - unsupported: the filesystem has no hard links (FAT/exFAT, some network
	//     mounts). The fall-through then publishes non-atomically, so two
	//     simultaneous first-time creators there could diverge. ACCEPTED (codex
	//     bb92dfe7 P2): WorkingDir always hosts the SQLite DBs, which need POSIX
	//     fcntl locking — a filesystem without hard links generally lacks that too,
	//     so SM is unusable on it well before this id matters. A hard-link-capable
	//     data dir is effectively guaranteed in any working deployment, and the
	//     race is in any case the first-ever-create edge, transient and self-healing
	//     on the next restart.
	if v, ok := readIdentifier(path); ok {
		return v
	}
	return healInPlace(path, id, log)
}

// healInPlace overwrites path with id and returns it. Overwriting an existing file
// needs only file-write permission, so this repairs a corrupt/unreadable id even
// when the containing directory is not writable. See the concurrency caveat on
// loadOrCreateIdentifier.
func healInPlace(path string, id uint32, log logging.Logger) uint32 {
	if werr := os.WriteFile(path, idBytes(id), 0o600); werr != nil {
		warnUnpersisted(log, path, werr)
	}
	return id
}

// readIdentifier reads a persisted, parseable id. ok is false when the file is
// missing, unreadable, or its contents don't parse as a uint32.
func readIdentifier(path string) (uint32, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	v, perr := parseID(b)
	return v, perr == nil
}

func parseID(b []byte) (uint32, error) {
	v, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 32)
	return uint32(v), err
}

// idBytes renders the id as it is persisted: decimal text + newline. 0600 is
// cautious, not required — the id is public (it rides every datagram); it just
// keeps WorkingDir files uniformly owner-only.
func idBytes(id uint32) []byte { return []byte(strconv.FormatUint(uint64(id), 10) + "\n") }

func warnUnpersisted(log logging.Logger, path string, err error) {
	log.WarnWith().Err(err).Str("path", path).
		Msg("pskreporter: could not persist sender identifier (using an in-memory id this session)")
}
