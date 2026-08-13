package pskreporter

import (
	"os"
	"strconv"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// loadOrCreateIdentifier returns the IPFIX observation-domain identifier — the
// datagram header's per-sender "random identifier". PSK Reporter's protocol
// (pskdev.html) says it "should be constant for any particular sender", so we
// persist it across restarts rather than re-minting it each boot: a station that
// re-randomises on every process looks to the collector like a stream of distinct
// senders, which is exactly the churn a CGNAT'd reporter (a fresh source port per
// report, outside the client's control) already maximises.
//
// path == "" (the ft8-psk-probe CLI, unit tests) keeps a fresh in-memory random
// id. Everything else is best-effort, because reporting is best-effort by
// contract and a state-file problem must never block Start:
//   - a valid persisted id is returned verbatim (no re-mint);
//   - a missing or unparseable file is regenerated via gen and written back
//     (self-heal — the next boot then reads it);
//   - a write failure falls back to the in-memory id + a Warn.
//
// gen is injected (rand.Uint32 in production) so tests are deterministic.
func loadOrCreateIdentifier(path string, gen func() uint32, log logging.Logger) uint32 {
	if log == nil {
		log = logging.Noop()
	}
	if strings.TrimSpace(path) == "" {
		return gen()
	}
	if b, err := os.ReadFile(path); err == nil {
		if v, perr := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 32); perr == nil {
			return uint32(v)
		}
		// Present but unparseable — fall through to regenerate + overwrite.
	}
	id := gen()
	// 0600 is cautious, not required — the id is public (it rides every datagram);
	// it just keeps WorkingDir files uniformly owner-only.
	if err := os.WriteFile(path, []byte(strconv.FormatUint(uint64(id), 10)+"\n"), 0o600); err != nil {
		log.WarnWith().Err(err).Str("path", path).
			Msg("pskreporter: could not persist sender identifier (using an in-memory id this session)")
	}
	return id
}
