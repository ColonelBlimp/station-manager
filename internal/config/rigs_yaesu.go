package config

import "github.com/Station-Manager/types"

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
