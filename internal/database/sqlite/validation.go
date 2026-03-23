package sqlite

import (
	"sync"

	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
	"github.com/go-playground/validator/v10"
)

var validate *validator.Validate
var once sync.Once

func validateConfig(cfg *types.DatastoreConfig) error {
	const op errors.Op = "sqlite.validateConfig"
	if cfg == nil {
		return errors.New(op).Msg(errMsgNilConfig)
	}

	once.Do(func() {
		validate = validator.New(validator.WithRequiredStructEnabled())
	})

	if err := validate.Struct(cfg); err != nil {
		return errors.New(op).Err(err).Msg(errMsgConfigInvalid)
	}

	return nil
}
