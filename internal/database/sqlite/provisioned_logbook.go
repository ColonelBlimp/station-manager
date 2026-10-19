package sqlite

import (
	"context"
	stderr "errors"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite/adapters"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
	"github.com/aarondl/null/v8"
	"github.com/aarondl/sqlboiler/v4/boil"
)

// InsertProvisionedLogbookWithContext inserts a logbook carrying the UUID the
// caller gives: the cloud logbook's own (W-0021 Q7, ADR 0071 refinement
// 2026-10-16). Only whole-archive provisioning calls it, and only on the file
// it is building: once the file holds its archive identity the call refuses,
// so no other path can claim a chosen identity. Everywhere else the UUID is
// minted (InsertLogbookWithContext).
//
// The name keeps the local rule: a name already in the file is
// ErrDuplicateName. A UUID already in the file is refused too.
func (s *Service) InsertProvisionedLogbookWithContext(ctx context.Context, logbook types.Logbook) (int64, error) {
	const op errors.Op = "sqlite.Service.InsertProvisionedLogbookWithContext"
	if err := checkService(op, s); err != nil {
		return 0, err
	}
	if !utils.IsValidUUIDv7(logbook.UUID) {
		return 0, errors.New(op).WithMsg("a provisioned logbook needs a UUIDv7")
	}
	if _, err := s.ArchiveIdentityWithContext(ctx); err == nil {
		return 0, errors.New(op).WithMsg("the file already holds its archive identity; a logbook UUID is accepted only while it is being built")
	} else if !stderr.Is(err, errors.ErrNotFound) {
		return 0, errors.New(op).WithErr(err).WithMsg("read the archive identity")
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return 0, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	// The name is checked first, so a duplicate name is ErrDuplicateName and
	// any other uniqueness failure is the UUID's.
	var named int
	if err := h.QueryRowContext(ctx, `SELECT COUNT(*) FROM logbook WHERE name = ?`, logbook.Name).Scan(&named); err != nil {
		return 0, errors.New(op).WithErr(err).WithMsg("check the logbook name")
	}
	if named > 0 {
		return 0, errors.New(op).WithErr(errors.ErrDuplicateName)
	}
	model, err := adapters.LogbookTypeToModel(logbook)
	if err != nil {
		return 0, errors.New(op).WithErr(err)
	}
	model.UUID = null.StringFrom(logbook.UUID)
	if err := model.Insert(ctx, h, boil.Infer()); err != nil {
		if isUniqueConstraintError(err) {
			return 0, errors.New(op).WithErr(err).WithMsgf("logbook %s is already in the file", logbook.UUID)
		}
		return 0, errors.New(op).WithErr(err).WithMsg("insert the provisioned logbook")
	}
	return model.ID, nil
}
