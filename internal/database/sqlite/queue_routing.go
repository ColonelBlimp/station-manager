package sqlite

import (
	"context"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/status"
	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// LogbookIDsByQsoIDsWithContext maps each given QSO row id to its logbook id
// (rows that do not exist are absent from the map). The stamp-sync enqueue
// routes by the QSO's logbook (ADR 0082 part 6) and has only row ids in hand.
func (s *Service) LogbookIDsByQsoIDsWithContext(ctx context.Context, qsoIDs []int64) (map[int64]int64, error) {
	const op errors.Op = "sqlite.Service.LogbookIDsByQsoIDsWithContext"
	out := make(map[int64]int64, len(qsoIDs))
	if len(qsoIDs) == 0 {
		return out, nil
	}
	if err := checkService(op, s); err != nil {
		return nil, err
	}
	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()
	args := make([]any, 0, len(qsoIDs))
	for _, id := range qsoIDs {
		args = append(args, id)
	}
	q := `SELECT id, logbook_id FROM qso WHERE id IN (?` + strings.Repeat(",?", len(qsoIDs)-1) + `)`
	rows, err := h.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("query logbook ids")
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, lb int64
		if err := rows.Scan(&id, &lb); err != nil {
			return nil, errors.New(op).WithErr(err).WithMsg("scan logbook id")
		}
		out[id] = lb
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New(op).WithErr(err)
	}
	return out, nil
}

// QueuedForwarderNamesWithContext lists the distinct forwarder names that
// still hold undrained rows (pending, in progress or failed). The workers node
// compares them with the active archive's bindings: a name no binding carries
// has no worker and its rows are discarded loudly (ADR 0039's rule, per
// binding); `uploaded` rows are not counted here and are never touched.
func (s *Service) QueuedForwarderNamesWithContext(ctx context.Context) ([]string, error) {
	const op errors.Op = "sqlite.Service.QueuedForwarderNamesWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}
	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()
	rows, err := h.QueryContext(ctx, `SELECT DISTINCT forwarder_name FROM qso_upload WHERE status IN (?, ?, ?) ORDER BY forwarder_name`,
		status.Pending.String(), status.InProgress.String(), status.Failed.String())
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("query queued forwarder names")
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, errors.New(op).WithErr(err).WithMsg("scan forwarder name")
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New(op).WithErr(err)
	}
	return out, nil
}
