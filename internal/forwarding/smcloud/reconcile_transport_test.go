package smcloud

// ST-4b — the reconcile client (GETs manifests / repairs rows with the same bearer
// token) must carry the same-origin redirect policy. Pins the production construction
// path (NewReconciler); the policy + no-leak Do sanitisation are proven in
// internal/securehttp. Uses the local stack only — no cloud/Postgres needed to build the
// Reconciler (New validates the loopback URL but makes no request).

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

func TestNewReconciler_HardensClient(t *testing.T) {
	qsoSvc, dbSvc, logSvc, fc := newLocalStack(t, "http://127.0.0.1:1")
	lbID, err := dbSvc.InsertLogbook(types.Logbook{Name: "Main", Callsign: "7Q5MLV"})
	require.NoError(t, err)

	rec, err := NewReconciler(fc, lbID, dbSvc, qsoSvc, logSvc)
	require.NoError(t, err)
	if rec.client.CheckRedirect == nil {
		t.Error("NewReconciler did not install the same-origin redirect policy (ST-4b)")
	}
}
