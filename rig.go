package types

type RigConfig struct {
	ID           int64        `koanf:"id"`
	Name         string       `koanf:"name"`
	Model        string       `koanf:"model"`
	Terminator   string       `koanf:"terminator"` // Terminator defines the character used to signal the end of a command.
	CatCommands  []CatCommand `koanf:"commands"`
	CatStates    []CatState   `koanf:"states"`
	SerialConfig SerialConfig `koanf:"serial_port"`
	CatConfig    CatConfig    `koanf:"cat"`
}
