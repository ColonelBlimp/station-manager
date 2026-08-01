// Package origin defines the upload queue provenance enum — what CAUSED a
// queue entry to exist.
//
// Deliberately distinct from the action enum, which says what MUTATION is being
// forwarded. The two are orthogonal and both are needed: a QSO the operator
// deleted enqueues action=delete, origin=edit, while a reconcile repairing that
// same missed delete re-enqueues action=delete, origin=reconcile. Recording only
// the action leaves "why is this forwarder busy?" answerable solely by
// cross-referencing other message types (docs/reviews/forwarding-logging-gaps.md
// F1).
package origin

import "fmt"

// Origin identifies what caused an upload queue entry to be created.
type Origin string

const (
	// Live — a QSO logged through the normal submit path.
	Live Origin = "live"
	// Import — a bulk ADIF import, including its record-by-record fallback.
	Import Origin = "import"
	// Edit — an operator mutation of a stored QSO (update or delete).
	Edit Origin = "edit"
	// Manual — an operator-triggered backfill to one destination.
	Manual Origin = "manual"
	// StampSync — a row-mirror re-enqueue after a post-upload ADIF stamp
	// bumped the QSO's revision.
	StampSync Origin = "stamp_sync"
	// Reconcile — a repair enqueued by the SM Cloud reconciler.
	Reconcile Origin = "reconcile"
	// Legacy — a row carried over by migration 0007, enqueued before this
	// column existed. Never assigned by a producer.
	Legacy Origin = "legacy"
)

func (o Origin) String() string {
	return string(o)
}

// Parse converts a string to an Origin. Returns an error for unknown values.
//
// The Go-side guard exists alongside the column's CHECK constraint, not instead
// of it: this one fails at the call site with the offending value named, before
// a transaction is opened.
func Parse(s string) (Origin, error) {
	switch s {
	case "live":
		return Live, nil
	case "import":
		return Import, nil
	case "edit":
		return Edit, nil
	case "manual":
		return Manual, nil
	case "stamp_sync":
		return StampSync, nil
	case "reconcile":
		return Reconcile, nil
	case "legacy":
		return Legacy, nil
	default:
		return "", fmt.Errorf("unknown upload origin: %q", s)
	}
}
