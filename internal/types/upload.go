package types

import "time"

type QsoUpload struct {
	ID            int64     `json:"id" boil:"id,bind"`
	ModifiedAt    time.Time `json:"modified_at" boil:"modified_at,bind"`
	QsoID         int64     `json:"qso_id" boil:"qso_id,bind"`
	Service       string    `json:"service" boil:"service,bind"`
	Action        string    `json:"action" boil:"action,bind"`
	Status        string    `json:"status" boil:"status,bind"`
	Attempts      int64     `json:"attempts" boil:"attempts,bind"`
	LastAttemptAt int64     `json:"last_attempt_at" boil:"last_attempt_at,bind"`
	LastError     string    `json:"last_error" boil:"last_error,bind"`
	Qso           Qso
}
