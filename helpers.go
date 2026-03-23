package cat

import (
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
	"github.com/go-playground/validator/v10"
)

var validate *validator.Validate

func init() {
	validate = validator.New(validator.WithRequiredStructEnabled())
}

func validateConfig(cfg *types.RigConfig) error {
	const op errors.Op = "cat.validateConfig"

	err := validate.Struct(cfg)
	if err != nil {
		return errors.New(op).Err(err)
	}

	return nil
}
