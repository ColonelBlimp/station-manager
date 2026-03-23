package config

import (
	"github.com/Station-Manager/errors"
	"os"
)

func writeDataToFile(data []byte, path string) error {
	const op errors.Op = "config.writeDataToFile"
	// Use restrictive file permissions by default: owner read/write, group read
	if err := os.WriteFile(path, data, 0o640); err != nil {
		return errors.New(op).Err(err)
	}
	return nil
}
