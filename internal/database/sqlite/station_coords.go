package sqlite

import (
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// coordsInsideGrid is retained for the package's own tests, which assert the
// stored pair really does fall inside the locator rather than merely differing
// from what went in. The reconciliation itself moved to
// adapters.ReconcileStationCoords so the QSO-row write path can share it — see
// the note there on why there must be exactly one implementation.
func coordsInsideGrid(grid, lat, lon string) bool {
	return utils.CoordsInsideGrid(grid, lat, lon)
}
