package sqlite

import (
	"context"
	"encoding/json"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite/adapters"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite/models"
	"github.com/ColonelBlimp/station-manager/internal/database/txutil"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"

	"github.com/aarondl/sqlboiler/v4/boil"
	"github.com/aarondl/sqlboiler/v4/queries/qm"
	boiltypes "github.com/aarondl/sqlboiler/v4/types"
)

// operatorEventRetentionPerCategory bounds operator_event: the store keeps the
// newest N rows per category and prunes the rest oldest-first on every insert.
// W-0001 / ADR 0076. Enforced inside RecordOperatorEvent's own transaction,
// never inside a QSO transaction.
const operatorEventRetentionPerCategory = 500

// OperatorEventInput is one typed, bounded operator-facing event to record. The
// producing boundary supplies these fields; the store stamps occurred_at (UTC,
// via the column default) and enforces retention. Detail must be valid JSON and
// must NEVER carry raw third-party/provider error text (ADR 0076).
type OperatorEventInput struct {
	Category string
	Kind     string
	Severity string
	Build    string
	Detail   json.RawMessage
}

// RecordOperatorEvent appends one event and prunes its category to the newest
// operatorEventRetentionPerCategory rows (oldest-first), in a SINGLE
// transaction OWNED BY the store.
//
// It deliberately takes no caller-supplied *sql.Tx: a notification-history
// write must never join a QSO transaction, so a history-store failure is
// observable but can neither roll back nor block a QSO write (ADR 0076; "the
// only thing that stops logging is a broken local DB"). Insert and prune share
// one transaction so the ring never advances partially.
func (s *Service) RecordOperatorEvent(ctx context.Context, ev OperatorEventInput) (err error) {
	const op errors.Op = "sqlite.Service.RecordOperatorEvent"
	if cerr := checkService(op, s); cerr != nil {
		return cerr
	}
	if ev.Category == "" {
		return errors.New(op).WithMsg("category is empty")
	}
	if ev.Kind == "" {
		return errors.New(op).WithMsg("kind is empty")
	}
	if ev.Severity == "" {
		return errors.New(op).WithMsg("severity is empty")
	}
	if ev.Build == "" {
		return errors.New(op).WithMsg("build is empty")
	}
	if !json.Valid(ev.Detail) {
		return errors.New(op).WithMsg("detail is not valid JSON")
	}

	h, herr := s.getOpenHandle(op)
	if herr != nil {
		return herr
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	tx, berr := h.BeginTx(ctx, nil)
	if berr != nil {
		return errors.New(op).WithErr(berr).WithMsg("begin tx")
	}
	defer txutil.Rollback(tx, &err)

	row := &models.OperatorEvent{
		Category: ev.Category,
		Kind:     ev.Kind,
		Severity: ev.Severity,
		Build:    ev.Build,
		Detail:   boiltypes.JSON(ev.Detail),
	}
	if err = row.Insert(ctx, tx, boil.Infer()); err != nil {
		return errors.New(op).WithErr(err).WithMsg("insert operator_event")
	}

	// Prune oldest-first: keep only the newest N ids in this category. Runs in
	// the SAME transaction as the insert, so a prune failure rolls the insert
	// back too — the ring never advances partially. id (AUTOINCREMENT) is the
	// monotonic arrival order eviction is defined against.
	if _, err = tx.ExecContext(ctx, `
DELETE FROM operator_event
WHERE  category = ?
  AND  id NOT IN (
        SELECT id FROM operator_event
        WHERE  category = ?
        ORDER BY id DESC
        LIMIT ?
  )`, ev.Category, ev.Category, operatorEventRetentionPerCategory); err != nil {
		return errors.New(op).WithErr(err).WithMsg("prune operator_event")
	}

	if err = tx.Commit(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("commit operator_event")
	}
	return nil
}

// FetchOperatorEventsByCategoryWithContext returns the newest `limit`
// operator_event rows for a category, newest first (by id, the monotonic
// arrival order). Read half of the W-0001 notification-history surface; it
// performs no writes.
//
// limit must be in [1, operatorEventRetentionPerCategory]. A limit outside that
// range is an operation-tagged error, never a silent clamp: a caller asking for
// more than the ring can ever hold has a bug, and quietly returning fewer would
// hide it.
func (s *Service) FetchOperatorEventsByCategoryWithContext(ctx context.Context, category string, limit int) ([]types.OperatorEvent, error) {
	const op errors.Op = "sqlite.Service.FetchOperatorEventsByCategoryWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}
	if category == "" {
		return nil, errors.New(op).WithMsg("category is empty")
	}
	if limit < 1 || limit > operatorEventRetentionPerCategory {
		return nil, errors.New(op).WithMsgf(
			"limit %d out of range [1, %d]", limit, operatorEventRetentionPerCategory)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	rows, err := models.OperatorEvents(
		models.OperatorEventWhere.Category.EQ(category),
		qm.OrderBy("id DESC"),
		qm.Limit(limit),
	).All(ctx, h)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("fetch operator_event by category")
	}

	out := make([]types.OperatorEvent, 0, len(rows))
	for _, r := range rows {
		ev, er := adapters.OperatorEventModelToType(r)
		if er != nil {
			return nil, errors.New(op).WithErr(er)
		}
		out = append(out, ev)
	}
	return out, nil
}
