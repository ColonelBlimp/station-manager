package main

import (
	"context"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// seedDestinationBindings records the open archive's binding decision (ADR
// 0082 part 4). A managed or external archive receives none: the empty seed
// is marked once so no later start seeds it from whatever config.json
// carries. The adopted (legacy) archive is left undecided here — its seed
// copies the station's forwarder entries and renames extra-logbook queue rows,
// which must land in the same commit as routing and workers by binding, or
// the renamed rows would have no worker while routing still follows config.
// A failure fails the start: the decision gates what later starts may seed.
func seedDestinationBindings(ctx context.Context, db *sqlite.Service, paths archive.Paths, logger *logging.Service) error {
	const op errors.Op = "smd.seedDestinationBindings"
	if paths.Entry == nil || paths.Entry.Ownership == types.QsoArchiveOwnershipLegacy {
		return nil
	}
	res, err := db.SeedLogbookDestinationsWithContext(ctx, nil)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("record the empty binding seed")
	}
	if res.Seeded {
		logger.InfoWith().Str("archive_id", paths.Entry.ID).Str("ownership", string(paths.Entry.Ownership)).
			Msg("startup: destination bindings decided — none; a new archive starts with every destination off")
	}
	return nil
}
