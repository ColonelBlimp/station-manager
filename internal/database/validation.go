package database

import (
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
	"github.com/go-playground/validator/v10"
	"sync"
)

var validate *validator.Validate
var once sync.Once

func validateConfig(cfg *types.DatastoreConfig) error {
	const op errors.Op = "database.validateConfig"
	if cfg == nil {
		return errors.New(op).Msg(errMsgNilConfig)
	}

	once.Do(func() {
		validate = validator.New(validator.WithRequiredStructEnabled())
	})

	if err := validate.Struct(cfg); err != nil {
		return errors.New(op).Err(err).Msg(errMsgConfigInvalid)
	}

	// Driver-specific policies
	if cfg.Driver == PostgresDriver {
		if cfg.MaxOpenConns < 5 {
			return errors.New(op).Msg("Postgres MaxOpenConns must be at least 5")
		}
	}

	return nil
}
