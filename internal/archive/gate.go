package archive

import "github.com/ColonelBlimp/station-manager/internal/types"

// ForwardingGateReason is the operator-facing reason forwarding is off in an
// archive that is not the adopted one. It goes on the wire and in logs.
const ForwardingGateReason = "forwarding is off in this archive until per-logbook bindings exist"

// ForwardingAdmitted is the INTERIM forwarding gate (W-0021 slice 2, review
// finding 1): until the ADR 0056 per-logbook bindings ship, QSO forwarding is
// admitted only in the adopted archive — the installation's own file, the one
// the configured forwarder credentials have always belonged to. Any other
// archive (a managed contest file, an external file) enqueues nothing to any
// destination, starts no worker, no reconciler and no re-arm, so a new archive
// can never upload into the home archive's cloud logbook or accounts by
// accident. entry is the archive being written: nil (a not-yet-adopted
// install) is the adopted file by another name. The bindings retire this gate
// by replacing it with explicit routing.
func ForwardingAdmitted(entry *types.QsoArchiveConfig) bool {
	return entry == nil || entry.Ownership == types.QsoArchiveOwnershipLegacy
}
