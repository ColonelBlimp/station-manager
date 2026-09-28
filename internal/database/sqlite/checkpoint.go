package sqlite

import (
	"context"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// CheckpointTruncateWithContext runs PRAGMA wal_checkpoint(TRUNCATE): every WAL
// frame is copied into the database file and the WAL is truncated to zero, so
// the main file is the whole archive. It fails when a reader or writer kept the
// checkpoint from completing (busy), leaving frames in the WAL. Used before a
// clean close, so the file's change signature is taken of the finished file
// (ADR 0084).
func (s *Service) CheckpointTruncateWithContext(ctx context.Context) error {
	const op errors.Op = "sqlite.Service.CheckpointTruncateWithContext"
	if err := checkService(op, s); err != nil {
		return err
	}
	h, err := s.getOpenHandle(op)
	if err != nil {
		return err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()
	var busy, logFrames, checkpointed int
	if err := h.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &checkpointed); err != nil {
		return errors.New(op).WithErr(err).WithMsg("wal checkpoint")
	}
	if busy != 0 {
		return errors.New(op).WithMsgf("wal checkpoint incomplete (busy; %d of %d frames)", checkpointed, logFrames)
	}
	return nil
}
