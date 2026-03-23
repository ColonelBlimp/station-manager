package config

import "github.com/ColonelBlimp/station-manager/internal/types"

var ftdx10RigConfigs = types.RigConfig{
	ID:           0,
	Name:         "",
	Model:        "",
	Terminator:   "",
	CatCommands:  nil,
	CatStates:    nil,
	SerialConfig: types.SerialConfig{},
	CatConfig:    types.CatConfig{},
}
