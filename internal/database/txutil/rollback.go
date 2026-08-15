// Package txutil contains transaction cleanup shared by database-owning
// packages. It deliberately has no project dependencies.
package txutil

import (
	"database/sql"
	"errors"
	"fmt"
)

// Rollback is a deferred transaction guard. A successful commit makes
// Rollback return sql.ErrTxDone, which is expected and ignored. On every other
// path a rollback failure is operationally relevant: join it to the primary
// error without replacing that error, or return it when cleanup was the only
// failure.
func Rollback(tx *sql.Tx, resultErr *error) {
	err := tx.Rollback()
	if err == nil || errors.Is(err, sql.ErrTxDone) {
		return
	}
	rollbackErr := fmt.Errorf("rollback transaction: %w", err)
	if *resultErr == nil {
		*resultErr = rollbackErr
		return
	}
	*resultErr = errors.Join(*resultErr, rollbackErr)
}
